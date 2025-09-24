package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/holiman/uint256"
)

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm            *EVM
	mirInterpreter *compiler.MIRInterpreter
}

// NewMIRInterpreterAdapter creates a new MIR interpreter adapter for EVM
func NewMIRInterpreterAdapter(evm *EVM) *MIRInterpreterAdapter {
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
		// Use current contract address from env.Self
		addr := common.BytesToAddress(env.Self[:])
		return evm.StateDB.GetState(addr, common.BytesToHash(key[:]))
	}
	env.SStoreFunc = func(key [32]byte, value [32]byte) {
		addr := common.BytesToAddress(env.Self[:])
		evm.StateDB.SetState(addr, common.BytesToHash(key[:]), common.BytesToHash(value[:]))
	}
	env.GetBalanceFunc = func(addr20 [20]byte) *uint256.Int {
		addr := common.BytesToAddress(addr20[:])
		b := evm.StateDB.GetBalance(addr)
		if b == nil {
			return uint256.NewInt(0)
		}
		return new(uint256.Int).Set(b)
	}

	mirInterpreter := compiler.NewMIRInterpreter(env)

	return &MIRInterpreterAdapter{
		evm:            evm,
		mirInterpreter: mirInterpreter,
	}
}

// Run executes the contract using MIR interpreter
// This method should match the signature of EVMInterpreter.Run
func (adapter *MIRInterpreterAdapter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	// Check if we have MIR-optimized code
	if !contract.HasMIRCode() {
		// Fallback to regular EVM interpreter
		return adapter.evm.Interpreter().Run(contract, input, readOnly)
	}

	// Get the MIR CFG from the contract (type assertion)
	cfgInterface := contract.GetMIRCFG()
	cfg, ok := cfgInterface.(*compiler.CFG)
	if !ok || cfg == nil {
		// Fallback if no valid MIR CFG available
		return adapter.evm.Interpreter().Run(contract, input, readOnly)
	}

	// Set up MIR execution environment with contract-specific data
	adapter.setupExecutionEnvironment(contract, input)

	// If calldata contains a 4-byte selector, try direct dispatch to the entry basic block
	// to avoid scanning all basic blocks for real-world contracts with large dispatch tables.
	if len(input) >= 4 {
		selector := uint32(input[0])<<24 | uint32(input[1])<<16 | uint32(input[2])<<8 | uint32(input[3])
		if idx := cfg.EntryIndexForSelector(selector); idx >= 0 {
			bb := cfg.GetBasicBlocks()[idx]
			if bb != nil && bb.Size() > 0 {
				result, err := adapter.mirInterpreter.RunMIR(bb)
				if err == nil && len(result) > 0 {
					return result, nil
				}
			}
		}
	}

	// Fallback: execute each basic block sequentially (linear flow assumption)
	for _, bb := range cfg.GetBasicBlocks() {
		if bb == nil || bb.Size() == 0 {
			continue
		}

		result, err := adapter.mirInterpreter.RunMIR(bb)
		if err != nil {
			// Handle MIR execution errors - fallback to regular interpreter
			return adapter.evm.Interpreter().Run(contract, input, readOnly)
		}

		// If this block produced return data, stop and return it immediately
		if len(result) > 0 {
			return result, nil
		}
	}

	return ret, nil
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
		copy(env.Self[:], addr[:])
		copy(env.Caller[:], caller[:])
		copy(env.Origin[:], origin[:])
	}

	// Set contract balance from StateDB
	// SelfBalance is read lazily via GetBalanceFunc when needed

	// Set gas price from transaction context
	if adapter.evm.TxContext.GasPrice != nil {
		env.GasPrice = adapter.evm.TxContext.GasPrice.Uint64()
	}
}

// CanRun checks if this adapter can run the given contract
func (adapter *MIRInterpreterAdapter) CanRun(contract *Contract) bool {
	return contract.HasMIRCode()
}
