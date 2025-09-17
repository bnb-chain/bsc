package vm

import (
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm           *EVM
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
		evm:           evm,
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
		
		// The last result becomes the return value
		ret = result
	}
	
	return ret, nil
}

// setupExecutionEnvironment configures the MIR interpreter with contract-specific data
func (adapter *MIRInterpreterAdapter) setupExecutionEnvironment(contract *Contract, input []byte) {
	env := adapter.mirInterpreter.GetEnv()
	
	// Set calldata
	env.Calldata = input
	
	// Set contract balance if available
	if contract.Value() != nil {
		env.SelfBalance = contract.Value().Uint64()
	}
	
	// Set gas price from transaction context
	if adapter.evm.TxContext.GasPrice != nil {
		env.GasPrice = adapter.evm.TxContext.GasPrice.Uint64()
	}
}

// CanRun checks if this adapter can run the given contract
func (adapter *MIRInterpreterAdapter) CanRun(contract *Contract) bool {
	return contract.HasMIRCode()
}