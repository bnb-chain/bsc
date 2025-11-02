package vm

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	coretypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm            *EVM
	mirInterpreter *compiler.MIRInterpreter
	currentSelf    common.Address
	table          *JumpTable
}

// NewMIRInterpreterAdapter creates a new MIR interpreter adapter for EVM
func NewMIRInterpreterAdapter(evm *EVM) *MIRInterpreterAdapter {
	// Create adapter early so closures can reference cached fields
	adapter := &MIRInterpreterAdapter{evm: evm}

	// Create MIR execution environment from EVM context
	env := &compiler.MIRExecutionEnv{
		Memory:      make([]byte, 0, 1024),
		Storage:     make(map[[32]byte][32]byte),
		BlockNumber: evm.Context.BlockNumber.Uint64(),
		Timestamp:   evm.Context.Time,
		ChainID:     evm.ChainConfig().ChainID.Uint64(),
		GasPrice:    0, // Will be set from transaction context
		BaseFee:     0, // Will be set from block context
		SelfBalance: 0, // Will be set from contract context
	}

	// Set values from contexts if available
	if evm.Context.BaseFee != nil {
		env.BaseFee = evm.Context.BaseFee.Uint64()
	}

	// Install runtime linkage hooks once; they read dynamic data from env/evm
	env.SLoadFunc = func(key [32]byte) [32]byte {
		// Use cached currentSelf to avoid per-access address conversions
		return evm.StateDB.GetState(adapter.currentSelf, common.BytesToHash(key[:]))
	}
	env.SStoreFunc = func(key [32]byte, value [32]byte) {
		evm.StateDB.SetState(adapter.currentSelf, common.BytesToHash(key[:]), common.BytesToHash(value[:]))
	}
	env.GetBalanceFunc = func(addr20 [20]byte) *uint256.Int {
		addr := common.BytesToAddress(addr20[:])
		b := evm.StateDB.GetBalance(addr)
		if b == nil {
			return uint256.NewInt(0)
		}
		return new(uint256.Int).Set(b)
	}

	adapter.mirInterpreter = compiler.NewMIRInterpreter(env)
	// Build a jump table matching current chain rules for gas accounting
	switch {
	case evm.chainRules.IsVerkle:
		adapter.table = &verkleInstructionSet
	case evm.chainRules.IsPrague:
		adapter.table = &pragueInstructionSet
	case evm.chainRules.IsCancun:
		adapter.table = &cancunInstructionSet
	case evm.chainRules.IsShanghai:
		adapter.table = &shanghaiInstructionSet
	case evm.chainRules.IsMerge:
		adapter.table = &mergeInstructionSet
	case evm.chainRules.IsLondon:
		adapter.table = &londonInstructionSet
	case evm.chainRules.IsBerlin:
		adapter.table = &berlinInstructionSet
	case evm.chainRules.IsIstanbul:
		adapter.table = &istanbulInstructionSet
	case evm.chainRules.IsConstantinople:
		adapter.table = &constantinopleInstructionSet
	case evm.chainRules.IsByzantium:
		adapter.table = &byzantiumInstructionSet
	case evm.chainRules.IsEIP158:
		adapter.table = &spuriousDragonInstructionSet
	case evm.chainRules.IsEIP150:
		adapter.table = &tangerineWhistleInstructionSet
	case evm.chainRules.IsHomestead:
		adapter.table = &homesteadInstructionSet
	default:
		adapter.table = &frontierInstructionSet
	}
	return adapter
}

// Run executes the contract using MIR interpreter
// This method should match the signature of EVMInterpreter.Run
func (adapter *MIRInterpreterAdapter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	// Check if we have MIR-optimized code
	if !contract.HasMIRCode() {
		// Fallback to regular EVM interpreter
		return adapter.evm.Interpreter().Run(contract, input, readOnly)
	}

	// Pre-flight fork gating: if the bytecode contains opcodes not enabled at the current fork,
	// mirror EVM behavior by returning invalid opcode errors instead of running MIR.
	rules := adapter.evm.chainRules
	code := contract.Code
	if !rules.IsConstantinople {
		if bytes.IndexByte(code, byte(SHR)) >= 0 || bytes.IndexByte(code, byte(SHL)) >= 0 || bytes.IndexByte(code, byte(SAR)) >= 0 {
			return nil, fmt.Errorf("invalid opcode: SHR")
		}
	}

	// Get the MIR CFG from the contract (type assertion)
	cfgInterface := contract.GetMIRCFG()
	cfg, ok := cfgInterface.(*compiler.CFG)
	if !ok || cfg == nil {
		// Fallback if no valid MIR CFG available
		log.Error("MIR fallback: invalid CFG, using EVM interpreter", "addr", contract.Address(), "codehash", contract.CodeHash)
		return adapter.evm.Interpreter().Run(contract, input, readOnly)
	}

	// Set up MIR execution environment with contract-specific data
	adapter.setupExecutionEnvironment(contract, input)

	// Wire gas left getter so MirGAS can read it if needed
	if adapter.mirInterpreter != nil && adapter.mirInterpreter.GetEnv() != nil {
		env := adapter.mirInterpreter.GetEnv()
		env.GasLeft = func() uint64 { return contract.Gas }
	}

	// Install a pre-op hook to charge constant gas per opcode and any eliminated-op constants per block entry
	var curBlock *compiler.MIRBasicBlock
	adapter.mirInterpreter.SetBeforeOpHook(func(m *compiler.MIR) error {
		if m == nil {
			return nil
		}
		// Resolve the owning basic block for this EVM pc
		if cfg != nil {
			if bb := cfg.BlockByPC(uint(m.EvmPC())); bb != nil {
				if curBlock != bb {
					// On block entry, charge constant gas for eliminated ops: (total - emitted) * const
					evmCounts := bb.EVMOpCounts()
					emittedCounts := bb.EmittedOpCounts()
					var elimGas uint64
					for i := 0; i < 256; i++ {
						cnt := evmCounts[byte(i)]
						emitted := emittedCounts[byte(i)]
						if cnt > emitted {
							op := OpCode(byte(i))
							if adapter.table != nil && (*adapter.table)[op] != nil {
								constGas := (*adapter.table)[op].constantGas
								if constGas > 0 {
									elimGas += constGas * uint64(cnt-emitted)
								}
							}
						}
					}
					if elimGas > 0 {
						if contract.Gas < elimGas {
							return ErrOutOfGas
						}
						contract.Gas -= elimGas
					}
					curBlock = bb
				}
			}
		}
		// Charge constant gas for this emitted opcode
		op := OpCode(m.EvmOp())
		if adapter.table != nil && (*adapter.table)[op] != nil {
			constGas := (*adapter.table)[op].constantGas
			if constGas > 0 {
				if contract.Gas < constGas {
					return ErrOutOfGas
				}
				contract.Gas -= constGas
			}
		}
		return nil
	})

	// Selector-based direct dispatch is disabled; always fall back to default entry.

	// No selector: execute the first basic block only
	bbs := cfg.GetBasicBlocks()
	if len(bbs) > 0 && bbs[0] != nil && bbs[0].Size() > 0 {
		result, err := adapter.mirInterpreter.RunCFGWithResolver(cfg, bbs[0])
		if err != nil {
			if err == compiler.ErrMIRFallback {
				if adapter.evm.Config.MIRStrictNoFallback {
					// Strict mode: do not fallback; surface the error for debugging.
					return nil, fmt.Errorf("MIR strict mode: no fallback (reason=%w)", err)
				}
				log.Error("MIR fallback requested by interpreter, using EVM interpreter", "addr", contract.Address(), "pc", 0)
				return adapter.evm.baseInterpreter.Run(contract, input, readOnly)
			}
			// Preserve returndata on error (e.g., REVERT) to match EVM semantics
			return result, err
		}
		// If MIR executed without error, return whatever returndata was produced.
		// An empty result (e.g., STOP) should not trigger fallback; mirror EVM semantics
		// where a STOP simply returns empty bytes.
		return result, nil
	}
	// If nothing returned from the entry, fallback to EVM to preserve semantics
	if adapter.evm.Config.MIRStrictNoFallback {
		return nil, fmt.Errorf("MIR strict mode: entry block produced no result")
	}
	log.Error("MIR fallback: entry block produced no result, using EVM interpreter", "addr", contract.Address())
	return adapter.evm.Interpreter().Run(contract, input, readOnly)
}

// setupExecutionEnvironment configures the MIR interpreter with contract-specific data
func (adapter *MIRInterpreterAdapter) setupExecutionEnvironment(contract *Contract, input []byte) {
	env := adapter.mirInterpreter.GetEnv()

	// Set calldata
	env.Calldata = input

	// Reset interpreter transient state to avoid per-call allocations
	// Reuse memory backing store by truncating length to zero
	if adapter.mirInterpreter != nil {
		// Reset memory view
		if adapter.mirInterpreter.MemoryCap() > 0 {
			adapter.mirInterpreter.TruncateMemory()
		}
		// Reset return data
		adapter.mirInterpreter.ResetReturnData()
	}

	// Set address context
	{
		addr := contract.Address()
		caller := contract.Caller()
		origin := adapter.evm.TxContext.Origin
		adapter.currentSelf = addr
		copy(env.Self[:], addr[:])
		copy(env.Caller[:], caller[:])
		copy(env.Origin[:], origin[:])
	}

	// Set gas price from transaction context
	if adapter.evm.TxContext.GasPrice != nil {
		env.GasPrice = adapter.evm.TxContext.GasPrice.Uint64()
	}

	// Set call value for CALLVALUE op
	if contract != nil && contract.Value() != nil {
		// MIR will clone when reading
		env.CallValue = contract.Value()
	} else {
		env.CallValue = uint256.NewInt(0)
	}

	// Provide code for CODE* ops
	env.Code = contract.Code

	// External code accessors
	env.ExtCodeSize = func(addr [20]byte) uint64 {
		a := common.BytesToAddress(addr[:])
		// Best-effort: return bytecode length from state
		code := adapter.evm.StateDB.GetCode(a)
		if code == nil {
			return 0
		}
		return uint64(len(code))
	}
	env.ExtCodeCopy = func(addr [20]byte, codeOffset uint64, dest []byte) {
		a := common.BytesToAddress(addr[:])
		code := adapter.evm.StateDB.GetCode(a)
		if code == nil {
			for i := range dest {
				dest[i] = 0
			}
			return
		}
		for i := uint64(0); i < uint64(len(dest)); i++ {
			idx := codeOffset + i
			if idx < uint64(len(code)) {
				dest[i] = code[idx]
			} else {
				dest[i] = 0
			}
		}
	}

	// Log function to route logs back into EVM
	env.LogFunc = func(addr [20]byte, topics [][32]byte, data []byte) {
		a := common.BytesToAddress(addr[:])
		// Convert topics to common.Hash slice
		hashes := make([]common.Hash, len(topics))
		for i := range topics {
			hashes[i] = common.BytesToHash(topics[i][:])
		}

		// EXTCODEHASH via StateDB
		env.ExtCodeHash = func(addr [20]byte) [32]byte {
			a := common.BytesToAddress(addr[:])
			h := adapter.evm.StateDB.GetCodeHash(a)
			var out [32]byte
			copy(out[:], h[:])
			return out
		}

		// GAS left is not directly exposed here; leave nil to signal unavailability

		// Blob fields (EIP-4844): base fee and blob hashes
		if adapter.evm.Context.BlobBaseFee != nil {
			env.BlobBaseFee = adapter.evm.Context.BlobBaseFee.Uint64()
		}
		env.BlobHashFunc = func(index uint64) [32]byte {
			if index < uint64(len(adapter.evm.TxContext.BlobHashes)) {
				h := adapter.evm.TxContext.BlobHashes[index]
				var out [32]byte
				copy(out[:], h[:])
				return out
			}
			return [32]byte{}
		}
		adapter.evm.StateDB.AddLog(&coretypes.Log{
			Address:     a,
			Topics:      hashes,
			Data:        append([]byte(nil), data...),
			BlockNumber: adapter.evm.Context.BlockNumber.Uint64(),
		})
	}

	// Wire external execution to stock EVM for CALL-family ops
	env.ExternalCall = func(kind byte, addr20 [20]byte, value *uint256.Int, callInput []byte) (ret []byte, success bool) {
		to := common.BytesToAddress(addr20[:])
		// Heuristic: use current frame gas as upper bound. Gas accounting is handled by stock EVM.
		gas := contract.Gas
		var (
			out      []byte
			leftover uint64
			err      error
		)
		switch kind {
		case 0: // CALL
			out, leftover, err = adapter.evm.Call(contract, to, callInput, gas, value)
		case 1: // CALLCODE
			out, leftover, err = adapter.evm.CallCode(contract, to, callInput, gas, value)
		case 2: // DELEGATECALL
			out, leftover, err = adapter.evm.DelegateCall(contract, to, callInput, gas)
		case 3: // STATICCALL
			out, leftover, err = adapter.evm.StaticCall(contract, to, callInput, gas)
		default:
			return nil, false
		}
		_ = leftover
		if err != nil {
			return out, false
		}
		return out, true
	}

	// Wire CREATE and CREATE2 to stock EVM
	env.CreateContract = func(kind byte, value *uint256.Int, init []byte, salt *[32]byte) (addr [20]byte, success bool, ret []byte) {
		gas := contract.Gas
		var (
			out      []byte
			newAddr  common.Address
			leftover uint64
			err      error
		)
		if kind == 4 { // CREATE
			out, newAddr, leftover, err = adapter.evm.Create(contract, init, gas, value)
		} else { // CREATE2
			var saltU *uint256.Int
			if salt != nil {
				saltU = new(uint256.Int).SetBytes(salt[:])
			} else {
				saltU = uint256.NewInt(0)
			}
			out, newAddr, leftover, err = adapter.evm.Create2(contract, init, gas, value, saltU)
		}
		copy(addr[:], newAddr[:])
		_ = leftover
		if err != nil {
			return addr, false, out
		}
		return addr, true, out
	}

	// Do not override any tracer set by tests; leave as-is.

	// Block info
	env.GasLimit = adapter.evm.Context.GasLimit
	if adapter.evm.Context.Difficulty != nil {
		env.Difficulty = adapter.evm.Context.Difficulty.Uint64()
	}
	copy(env.Coinbase[:], adapter.evm.Context.Coinbase[:])
	env.BlockHashFunc = func(num uint64) [32]byte {
		// Use Context.GetHash if available; else return zero
		h := adapter.evm.Context.GetHash(num)
		var out [32]byte
		copy(out[:], h[:])
		return out
	}

	// Set fork flags from chain rules
	rules := adapter.evm.chainRules
	env.IsByzantium = rules.IsByzantium
	env.IsConstantinople = rules.IsConstantinople
	env.IsIstanbul = rules.IsIstanbul
	env.IsLondon = rules.IsLondon
	// Optionally extend with newer flags if MIR grows support for those ops
	// No-op if not referenced in interpreter.

	// Install jumpdest checker using EVM contract helpers
	env.CheckJumpdest = func(pc uint64) bool {
		// Must be within bounds and at a JUMPDEST and code segment
		if pc >= uint64(len(contract.Code)) {
			return false
		}
		if OpCode(contract.Code[pc]) != JUMPDEST {
			return false
		}
		return contract.isCode(pc)
	}
}

// CanRun checks if this adapter can run the given contract
func (adapter *MIRInterpreterAdapter) CanRun(contract *Contract) bool {
	return contract.HasMIRCode()
}
