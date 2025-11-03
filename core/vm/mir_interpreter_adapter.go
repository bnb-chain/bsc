package vm

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/tracing"
	coretypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// mirGasProbe is an optional test hook to observe MIR gas after each instruction
var mirGasProbe func(pc uint64, op byte, gasLeft uint64)

// SetMIRGasProbe installs a callback to observe MIR gas after each instruction (testing only)
func SetMIRGasProbe(cb func(pc uint64, op byte, gasLeft uint64)) {
	mirGasProbe = cb
}

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm            *EVM
	mirInterpreter *compiler.MIRInterpreter
	currentSelf    common.Address
	table          *JumpTable
	memShadow      *Memory
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
	// Initialize a shadow memory for dynamic memory gas accounting
	adapter.memShadow = NewMemory()
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
	innerHook := func(ctx *compiler.MIRPreOpContext) error {
		if ctx == nil || ctx.M == nil {
			return nil
		}
		// Removed block-entry eliminated-op precharge; per-op accounting will be done via emitted MIR (including NOPs for PUSH)
		// Charge constant gas for this emitted opcode
		// Determine originating EVM opcode for this MIR
		evmOp := OpCode(ctx.EvmOp)
		// Emit tracer OnOpcode before charging to maintain step-count parity
		if adapter.evm != nil && adapter.evm.Config.Tracer != nil && adapter.evm.Config.Tracer.OnOpcode != nil {
			scope := &ScopeContext{Memory: adapter.memShadow, Stack: nil, Contract: contract}
			adapter.evm.Config.Tracer.OnOpcode(uint64(ctx.M.EvmPC()), byte(evmOp), contract.Gas, 0, scope, nil, adapter.evm.depth, nil)
		}
		// Charge per-op constant gas for the originating EVM opcode, but skip PHI (compiler artifact)
		if ctx.M.Op() != compiler.MirPHI {
			if adapter.table != nil && (*adapter.table)[evmOp] != nil {
				constGas := (*adapter.table)[evmOp].constantGas
				if constGas > 0 {
					if contract.Gas < constGas {
						return ErrOutOfGas
					}
					contract.Gas -= constGas
				}
			}
		}
		// Dynamic gas metering
		// Ensure shadow memory reflects prior expansions
		if adapter.memShadow == nil {
			adapter.memShadow = NewMemory()
		}
		// Helper: resize shadow memory after charging
		resizeShadow := func(sz uint64) {
			if sz > 0 {
				adapter.memShadow.Resize(sz)
			}
		}
		// Helper: toWordSize
		toWord := func(x uint64) uint64 { return (x + 31) / 32 }
		switch evmOp {
		case SLOAD:
			// EIP-2929 SLOAD dynamic gas
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // peek -> slot
					gas, err := gasSLoadEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							// Mirror base interpreter behavior: do not surface overflow from dynamic calc here
							// Leave callGasTemp as-is; the call execution path will handle effective gas
							err = nil
						}
						return err
					}
					if contract.Gas < gas {
						return ErrOutOfGas
					}
					contract.Gas -= gas
				}
			}
		case BALANCE, EXTCODESIZE, EXTCODEHASH:
			// EIP-2929 account warm/cold surcharge
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // peek -> address
					gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						}
						return err
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case EXP:
			// Dynamic gas based on exponent byte length
			if len(ctx.Operands) >= 2 {
				exp := ctx.Operands[1]
				expBytes := uint64((exp.BitLen() + 7) / 8)
				perByte := params.ExpByteFrontier
				if adapter.evm.chainRules.IsEIP158 {
					perByte = params.ExpByteEIP158
				}
				add := params.ExpGas + perByte*expBytes
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
			}
		case MLOAD, MSTORE, MSTORE8, RETURN, REVERT, CREATE, CREATE2:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					}
					return err
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				// EIP-3860 initcode per-word gas for CREATE/CREATE2
				if evmOp == CREATE || evmOp == CREATE2 {
					// operands: value, offset, size, (salt)
					if len(ctx.Operands) >= 3 {
						size := ctx.Operands[2].Uint64()
						if size > params.MaxInitCodeSize {
							return fmt.Errorf("%w: size %d", ErrMaxInitCodeSizeExceeded, size)
						}
						more := params.InitCodeWordGas * toWord(size)
						if contract.Gas < more {
							return ErrOutOfGas
						}
						contract.Gas -= more
					}
				}
				resizeShadow(ctx.MemorySize)
			}
		case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				// copy gas per word: size is operand[2]
				var size uint64
				if len(ctx.Operands) >= 3 {
					size = ctx.Operands[2].Uint64()
				}
				copyGas := toWord(size) * params.CopyGas
				add := gas + copyGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
			}
		case EXTCODECOPY:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				var size uint64
				if len(ctx.Operands) >= 4 {
					size = ctx.Operands[3].Uint64()
				}
				copyGas := toWord(size) * params.CopyGas
				add := gas + copyGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
			}
			// EIP-2929 cold-warm surcharge for EXTCODECOPY
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // address
					gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						} else {
							return err
						}
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case MCOPY:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				var size uint64
				if len(ctx.Operands) >= 3 {
					size = ctx.Operands[2].Uint64()
				}
				copyGas := toWord(size) * params.CopyGas
				add := gas + copyGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
			}
		case KECCAK256:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						// align with base interpreter: don't surface overflow from call gas calc here
					} else {
						return err
					}
				}
				var size uint64
				if len(ctx.Operands) >= 2 {
					size = ctx.Operands[1].Uint64()
				}
				wordGas := toWord(size) * params.Keccak256WordGas
				add := gas + wordGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
			}
		case LOG0, LOG1, LOG2, LOG3, LOG4:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
					} else {
						return err
					}
				}
				// Topics and data costs
				n := int(evmOp - LOG0)
				add := gas + uint64(n)*params.LogTopicGas
				var size uint64
				if len(ctx.Operands) >= 2 {
					size = ctx.Operands[1].Uint64()
				}
				// LogDataGas is per byte
				add += size * params.LogDataGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
			}
		case SELFDESTRUCT:
			// Charge dynamic gas according to fork rules
			var gas uint64
			var err error
			if adapter.evm.chainRules.IsLondon {
				gas, err = gasSelfdestructEIP3529(adapter.evm, contract, newstack(), adapter.memShadow, 0)
			} else if adapter.evm.chainRules.IsBerlin {
				gas, err = gasSelfdestructEIP2929(adapter.evm, contract, newstack(), adapter.memShadow, 0)
			}
			if err != nil {
				return err
			}
			if gas > 0 {
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case SSTORE:
			// Build a tiny stack where Back(0)=key, Back(1)=value per gasSStore contract
			if len(ctx.Operands) >= 2 {
				st := newstack()
				// push value then key so Back(0)=key, Back(1)=value
				st.push(new(uint256.Int).Set(ctx.Operands[1]))
				st.push(new(uint256.Int).Set(ctx.Operands[0]))
				gas, err := gasSStore(adapter.evm, contract, st, adapter.memShadow, 0)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
					} else {
						return err
					}
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case CALL, CALLCODE, DELEGATECALL, STATICCALL:
			// Use vm gas calculators to set evm.callGasTemp and deduct dynamic gas
			// Build stack so Back(0)=requestedGas, Back(1)=addr, Back(2)=value (if present)
			st := newstack()
			// Ensure ctx.Operands length checks per variant
			switch evmOp {
			case CALL, CALLCODE:
				if len(ctx.Operands) < 7 {
					return nil
				}
				reqGas := new(uint256.Int).Set(ctx.Operands[0])
				addr := new(uint256.Int)
				// address at operands[1]
				addr.Set(ctx.Operands[1])
				val := new(uint256.Int).Set(ctx.Operands[2])
				st.push(val)    // Back(2)
				st.push(addr)   // Back(1)
				st.push(reqGas) // Back(0)
				var dyn uint64
				var err error
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == CALL {
						dyn, err = gasCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasCallCodeEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				} else {
					if evmOp == CALL {
						dyn, err = gasCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasCallCode(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				}
				if err != nil {
					return err
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				resizeShadow(ctx.MemorySize)
			case DELEGATECALL, STATICCALL:
				if len(ctx.Operands) < 6 {
					return nil
				}
				st := newstack()
				reqGas := new(uint256.Int).Set(ctx.Operands[0])
				st.push(reqGas) // Back(0)
				var dyn uint64
				var err error
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasStaticCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				} else {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasStaticCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				}
				if err != nil {
					return err
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				resizeShadow(ctx.MemorySize)
			}
		}
		// Test probe: allow tests to observe gas left after each MIR step
		if mirGasProbe != nil {
			mirGasProbe(uint64(ctx.M.EvmPC()), ctx.EvmOp, contract.Gas)
		}
		return nil
	}
	adapter.mirInterpreter.SetBeforeOpHook(func(ctx *compiler.MIRPreOpContext) error {
		err := innerHook(ctx)
		if err != nil && errors.Is(err, ErrGasUintOverflow) {
			return nil
		}
		return err
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
		// Use computed callGasTemp (set during pre-op hook) and apply stipend if value transferred
		gas := adapter.evm.callGasTemp
		if (kind == 0 || kind == 1) && value != nil && !value.IsZero() {
			gas += params.CallStipend
		}
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
		// Refund leftover like stock interpreter
		contract.RefundGas(leftover, adapter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)
		if err != nil {
			return out, false
		}
		return out, true
	}

	// Wire CREATE and CREATE2 to stock EVM
	env.CreateContract = func(kind byte, value *uint256.Int, init []byte, salt *[32]byte) (addr [20]byte, success bool, ret []byte) {
		gas := contract.Gas
		// Apply EIP-150: parent gas reduction by 1/64 before passing to child
		gas -= gas / 64
		// Deduct from parent before call, matching opCreate/opCreate2
		contract.UseGas(gas, adapter.evm.Config.Tracer, tracing.GasChangeCallContractCreation)
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
		// Refund leftover like stock interpreter
		contract.RefundGas(leftover, adapter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)
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
