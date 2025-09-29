package vm

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm            *EVM
	mirInterpreter *compiler.MIRInterpreter
	currentSelf    common.Address
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

	// Selector-based direct dispatch is disabled; always fall back to default entry.

	// No selector: execute the first basic block only
	bbs := cfg.GetBasicBlocks()
	if len(bbs) > 0 && bbs[0] != nil && bbs[0].Size() > 0 {
		result, err := adapter.mirInterpreter.RunCFGWithResolver(cfg, bbs[0])
		if len(result) > 0 || err != nil {
			return result, err
		}
	}
	// If nothing returned from the entry, fallback to EVM to preserve semantics
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

	// Do not override any tracer set by tests; leave as-is.

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
