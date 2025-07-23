package compiler_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestDebugFusion(t *testing.T) {
	// Simple test case: PUSH1 0x01, PUSH1 0x02, ADD, STOP
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00}

	fmt.Printf("Original code: %x\n", code)

	// Apply fusion
	fusedCode, err := compiler.DoCFGBasedOpcodeFusion(code)
	if err != nil {
		t.Fatalf("Fusion failed: %v", err)
	}

	fmt.Printf("Fused code: %x\n", fusedCode)

	// Check if fusion occurred
	for i := 0; i < len(fusedCode); i++ {
		if fusedCode[i] >= 0xb0 && fusedCode[i] <= 0xc8 {
			fmt.Printf("Found optimized opcode at position %d: 0x%02x\n", i, fusedCode[i])
		}
	}

	// Test with EVM - first without optimizations
	cfg := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
		},
	}

	// Execute the original code
	ret, _, err := runtime.Execute(code, nil, cfg)
	if err != nil {
		t.Fatalf("Original EVM execution failed: %v", err)
	}

	fmt.Printf("Original EVM execution successful: ret=%x\n", ret)

	// Test with EVM - with optimizations
	cfgOpt := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
		},
	}

	// Create EVM directly to check interpreter
	evm := runtime.NewEnv(cfgOpt)
	fmt.Printf("EVM config: EnableOpcodeOptimizations=%v\n", evm.Config.EnableOpcodeOptimizations)
	fmt.Printf("EVM interpreter: %T\n", evm.Interpreter())

	// Check if the optimized interpreter is being used
	if evm.Config.EnableOpcodeOptimizations {
		fmt.Printf("Optimized interpreter should be used\n")
	} else {
		fmt.Printf("Base interpreter should be used\n")
	}

	// Execute the fused code using EVM directly to test optimized code loading
	evm2 := runtime.NewEnv(cfgOpt)

	// Set up state database
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	evm2.StateDB = statedb

	// Create account and set original code (not fused code)
	address := common.BytesToAddress([]byte("contract"))
	statedb.CreateAccount(address)
	statedb.SetCode(address, code) // Use original code, let EVM load optimized version

	// Call the contract
	ret2, _, err := evm2.Call(vm.AccountRef(cfgOpt.Origin), address, nil, cfgOpt.GasLimit, uint256.MustFromBig(cfgOpt.Value))
	if err != nil {
		t.Fatalf("Fused EVM execution failed: %v", err)
	}

	fmt.Printf("Fused EVM execution successful: ret=%x\n", ret2)
}

func TestOptimizedOpcodeDirectly(t *testing.T) {
	// Test that optimized opcodes cannot be executed directly
	// This is the expected behavior - optimized opcodes should only be executed
	// when loaded through the optimized code loading mechanism
	code := []byte{0xb7, 0x01, 0x02, 0x00} // Push1Push1, data1, data2, STOP

	fmt.Printf("Testing Push1Push1 directly: %x\n", code)

	// Try different chain configurations
	chainConfigs := []*params.ChainConfig{
		params.AllEthashProtocolChanges,
		params.MainnetChainConfig,
		params.TestChainConfig,
	}

	allFailed := true
	for i, chainConfig := range chainConfigs {
		fmt.Printf("Testing with chain config %d: %s\n", i, chainConfig.ChainID)

		cfg := &runtime.Config{
			ChainConfig: chainConfig,
			GasLimit:    10_000_000,
			Origin:      common.Address{},
			BlockNumber: big.NewInt(1),
			Value:       big.NewInt(0),
			EVMConfig: vm.Config{
				EnableOpcodeOptimizations: true,
			},
		}

		// Execute the code
		ret, _, err := runtime.Execute(code, nil, cfg)
		if err != nil {
			fmt.Printf("Chain config %d failed: %v\n", i, err)
		} else {
			fmt.Printf("Chain config %d succeeded: ret=%x\n", i, ret)
			allFailed = false
		}
	}

	// This is the expected behavior - optimized opcodes should not be executable directly
	if allFailed {
		fmt.Printf("All chain configurations failed as expected - optimized opcodes cannot be executed directly\n")
	} else {
		t.Fatalf("Unexpected success - optimized opcodes should not be executable directly")
	}
}
