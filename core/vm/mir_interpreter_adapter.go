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

	// Execute each basic block in sequence
	// For now, we assume linear execution (no complex control flow)
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

	// Set address context
	var self20, caller20, origin20 [20]byte
	copy(self20[:], contract.Address().Bytes())
	copy(caller20[:], contract.Caller().Bytes())
	copy(origin20[:], adapter.evm.TxContext.Origin.Bytes())
	env.Self = self20
	env.Caller = caller20
	env.Origin = origin20

	// Set contract balance from StateDB
	{
		bal := adapter.evm.StateDB.GetBalance(contract.Address())
		if bal != nil {
			env.SelfBalance = bal.Uint64()
		} else {
			env.SelfBalance = 0
		}
	}

	// Set gas price from transaction context
	if adapter.evm.TxContext.GasPrice != nil {
		env.GasPrice = adapter.evm.TxContext.GasPrice.Uint64()
	}

	// Link storage hooks to StateDB
	env.SLoadFunc = func(key [32]byte) [32]byte {
		addr := contract.Address()
		val := adapter.evm.StateDB.GetState(addr, common.BytesToHash(key[:]))
		return val
	}
	env.SStoreFunc = func(key [32]byte, value [32]byte) {
		addr := contract.Address()
		adapter.evm.StateDB.SetState(addr, common.BytesToHash(key[:]), common.BytesToHash(value[:]))
	}

	// Balance query
	env.GetBalanceFunc = func(addr20 [20]byte) *uint256.Int {
		addr := common.BytesToAddress(addr20[:])
		b := adapter.evm.StateDB.GetBalance(addr)
		if b == nil {
			return uint256.NewInt(0)
		}
		return new(uint256.Int).Set(b)
	}
}

// CanRun checks if this adapter can run the given contract
func (adapter *MIRInterpreterAdapter) CanRun(contract *Contract) bool {
	return contract.HasMIRCode()
}
