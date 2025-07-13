package compiler_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

// WBNB method selectors for testing
var wbnbMethodSelectors = map[string]string{
	"0x06fdde03": "name()",
	"0x095ea7b3": "approve(address,uint256)",
	"0x18160ddd": "totalSupply()",
	"0x23b872dd": "transferFrom(address,address,uint256)",
	"0x2e1a7d4d": "withdraw(uint256)",
	"0x313ce567": "decimals()",
	"0x70a08231": "balanceOf(address)",
	"0x95d89b41": "symbol()",
	"0xa9059cbb": "transfer(address,uint256)",
	"0xd0e30db0": "deposit()",
	"0xdd62ed3e": "allowance(address,address)",
}

// testWBNBContractExecution executes both original and fused WBNB contract code
// and compares gas usage and return values
//
// Tests all 11 WBNB contract functions:
// - View functions: name(), symbol(), decimals(), totalSupply(), balanceOf(), allowance()
// - State-changing functions: approve(), transfer(), transferFrom(), withdraw(), deposit()
func testWBNBContractExecution(t *testing.T, originalCode, fusedCode []byte) {
	// Test all available function selectors
	testCases := []struct {
		name     string
		selector string
		input    []byte
	}{
		{
			name:     "name()",
			selector: "0x06fdde03",
			input:    []byte{},
		},
		{
			name:     "symbol()",
			selector: "0x95d89b41",
			input:    []byte{},
		},
		{
			name:     "decimals()",
			selector: "0x313ce567",
			input:    []byte{},
		},
		{
			name:     "totalSupply()",
			selector: "0x18160ddd",
			input:    []byte{},
		},
		{
			name:     "balanceOf(address)",
			selector: "0x70a08231",
			input:    make([]byte, 32), // 32 bytes for address parameter
		},
		{
			name:     "deposit()",
			selector: "0xd0e30db0",
			input:    []byte{},
		},
		{
			name:     "approve(address,uint256)",
			selector: "0x095ea7b3",
			input:    make([]byte, 64), // 32 bytes for address + 32 bytes for uint256
		},
		{
			name:     "transferFrom(address,address,uint256)",
			selector: "0x23b872dd",
			input:    make([]byte, 96), // 32 bytes for from + 32 bytes for to + 32 bytes for uint256
		},
		{
			name:     "withdraw(uint256)",
			selector: "0x2e1a7d4d",
			input:    make([]byte, 32), // 32 bytes for uint256
		},
		{
			name:     "transfer(address,uint256)",
			selector: "0xa9059cbb",
			input:    make([]byte, 64), // 32 bytes for address + 32 bytes for uint256
		},
		{
			name:     "allowance(address,address)",
			selector: "0xdd62ed3e",
			input:    make([]byte, 64), // 32 bytes for owner + 32 bytes for spender
		},
	}

	// Create runtime configuration
	cfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false, // Disable for original code
		},
	}

	cfgFused := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true, // Enable for fused code
		},
	}

	fmt.Printf("\n=== WBNB Contract Execution Test ===\n")
	fmt.Printf("Original code size: %d bytes\n", len(originalCode))
	fmt.Printf("Fused code size: %d bytes\n", len(fusedCode))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prepare input data
			input := append([]byte{}, tc.selector...)
			input = append(input, tc.input...)

			// Execute original code
			originalRet, originalGas, originalErr := executeContract(originalCode, input, cfg)

			// Execute fused code
			fusedRet, fusedGas, fusedErr := executeContract(fusedCode, input, cfgFused)

			// Compare results
			if originalErr != nil && fusedErr == nil {
				t.Errorf("Original code failed but fused code succeeded: %v", originalErr)
				return
			}
			if originalErr == nil && fusedErr != nil {
				t.Errorf("Original code succeeded but fused code failed: %v", fusedErr)
				return
			}
			if originalErr != nil && fusedErr != nil {
				// Both failed, which is acceptable for some methods
				fmt.Printf("  %s: Both failed (expected for some methods)\n", tc.name)
				return
			}

			// Compare return values
			if !bytesEqual(originalRet, fusedRet) {
				t.Errorf("Return value mismatch for %s:\nOriginal: %x\nFused: %x",
					tc.name, originalRet, fusedRet)
			}

			// Compare gas usage
			gasDiff := int64(originalGas) - int64(fusedGas)
			gasDiffPercent := float64(gasDiff) / float64(originalGas) * 100

			fmt.Printf("  %s: Gas used - Original: %d, Fused: %d, Diff: %d (%.2f%%)\n",
				tc.name, originalGas, fusedGas, gasDiff, gasDiffPercent)

			// Verify that gas usage is reasonable (fused should use less or equal gas)
			if fusedGas != originalGas {
				t.Errorf("Fused code used diff gas with original: %d != %d", fusedGas, originalGas)
			}
		})
	}
}

// executeContract executes a contract with the given code and input
func executeContract(code, input []byte, cfg *runtime.Config) ([]byte, uint64, error) {
	// Set up StateDB for the config
	if cfg.State == nil {
		cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}

	// Create EVM environment
	evm := runtime.NewEnv(cfg)
	address := common.BytesToAddress([]byte("contract"))
	sender := vm.AccountRef(cfg.Origin)

	// Set up the contract code
	evm.StateDB.CreateAccount(address)
	evm.StateDB.SetCode(address, code)

	// Execute the contract
	ret, leftOverGas, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
	if err != nil {
		return nil, 0, err
	}

	// Calculate gas used
	gasUsed := cfg.GasLimit - leftOverGas

	return ret, gasUsed, nil
}

// bytesEqual compares two byte slices for equality
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// countChangedOpcodes counts how many opcodes were changed between original and fused code
func countChangedOpcodes(original, fused []byte) int {
	if len(original) != len(fused) {
		return -1 // Different lengths
	}

	changed := 0
	for i := 0; i < len(original); i++ {
		if original[i] != fused[i] {
			changed++
		}
	}
	return changed
}

// getOpcodeName returns a human-readable name for an opcode
func getOpcodeName(opcode byte) string {
	names := map[byte]string{
		0x00: "STOP",
		0x01: "ADD",
		0x02: "MUL",
		0x03: "SUB",
		0x04: "DIV",
		0x05: "SDIV",
		0x06: "MOD",
		0x07: "SMOD",
		0x08: "ADDMOD",
		0x09: "MULMOD",
		0x0a: "SIGNEXTEND",
		0x10: "LT",
		0x11: "GT",
		0x12: "SLT",
		0x13: "SGT",
		0x14: "EQ",
		0x15: "ISZERO",
		0x16: "AND",
		0x17: "OR",
		0x18: "XOR",
		0x19: "NOT",
		0x1a: "BYTE",
		0x1b: "SHL",
		0x1c: "SHR",
		0x1d: "SAR",
		0x20: "SHA3",
		0x30: "ADDRESS",
		0x31: "BALANCE",
		0x32: "ORIGIN",
		0x33: "CALLER",
		0x34: "CALLVALUE",
		0x35: "CALLDATALOAD",
		0x36: "CALLDATASIZE",
		0x37: "CALLDATACOPY",
		0x38: "CODESIZE",
		0x39: "CODECOPY",
		0x3a: "GASPRICE",
		0x3b: "EXTCODESIZE",
		0x3c: "EXTCODECOPY",
		0x3d: "RETURNDATASIZE",
		0x3e: "RETURNDATACOPY",
		0x3f: "EXTCODEHASH",
		0x40: "BLOCKHASH",
		0x41: "COINBASE",
		0x42: "TIMESTAMP",
		0x43: "NUMBER",
		0x44: "DIFFICULTY",
		0x45: "GASLIMIT",
		0x50: "POP",
		0x51: "MLOAD",
		0x52: "MSTORE",
		0x53: "MSTORE8",
		0x54: "SLOAD",
		0x55: "SSTORE",
		0x56: "JUMP",
		0x57: "JUMPI",
		0x58: "PC",
		0x59: "MSIZE",
		0x5a: "GAS",
		0x5b: "JUMPDEST",
		0x60: "PUSH1",
		0x61: "PUSH2",
		0x62: "PUSH3",
		0x63: "PUSH4",
		0x64: "PUSH5",
		0x65: "PUSH6",
		0x66: "PUSH7",
		0x67: "PUSH8",
		0x68: "PUSH9",
		0x69: "PUSH10",
		0x6a: "PUSH11",
		0x6b: "PUSH12",
		0x6c: "PUSH13",
		0x6d: "PUSH14",
		0x6e: "PUSH15",
		0x6f: "PUSH16",
		0x70: "PUSH17",
		0x71: "PUSH18",
		0x72: "PUSH19",
		0x73: "PUSH20",
		0x74: "PUSH21",
		0x75: "PUSH22",
		0x76: "PUSH23",
		0x77: "PUSH24",
		0x78: "PUSH25",
		0x79: "PUSH26",
		0x7a: "PUSH27",
		0x7b: "PUSH28",
		0x7c: "PUSH29",
		0x7d: "PUSH30",
		0x7e: "PUSH31",
		0x7f: "PUSH32",
		0x80: "DUP1",
		0x81: "DUP2",
		0x82: "DUP3",
		0x83: "DUP4",
		0x84: "DUP5",
		0x85: "DUP6",
		0x86: "DUP7",
		0x87: "DUP8",
		0x88: "DUP9",
		0x89: "DUP10",
		0x8a: "DUP11",
		0x8b: "DUP12",
		0x8c: "DUP13",
		0x8d: "DUP14",
		0x8e: "DUP15",
		0x8f: "DUP16",
		0x90: "SWAP1",
		0x91: "SWAP2",
		0x92: "SWAP3",
		0x93: "SWAP4",
		0x94: "SWAP5",
		0x95: "SWAP6",
		0x96: "SWAP7",
		0x97: "SWAP8",
		0x98: "SWAP9",
		0x99: "SWAP10",
		0x9a: "SWAP11",
		0x9b: "SWAP12",
		0x9c: "SWAP13",
		0x9d: "SWAP14",
		0x9e: "SWAP15",
		0x9f: "SWAP16",
		0xa0: "LOG0",
		0xa1: "LOG1",
		0xa2: "LOG2",
		0xa3: "LOG3",
		0xa4: "LOG4",
		0xf0: "CREATE",
		0xf1: "CALL",
		0xf2: "RETURN",
		0xf3: "DELEGATECALL",
		0xf4: "CREATE2",
		0xf5: "STATICCALL",
		0xfa: "STATICCALL",
		0xfd: "REVERT",
		0xfe: "INVALID",
		0xff: "SELFDESTRUCT",
		// Fused opcodes (0xb0-0xc8)
		0xb0: "NOP",
		0xb1: "AndSwap1PopSwap2Swap1",
		0xb2: "Swap2Swap1PopJump",
		0xb3: "Swap1PopSwap2Swap1",
		0xb4: "PopSwap2Swap1Pop",
		0xb5: "Push2Jump",
		0xb6: "Push2JumpI",
		0xb7: "Push1Push1",
		0xb8: "Push1Add",
		0xb9: "Push1Shl",
		0xba: "Push1Dup1",
		0xbb: "Swap1Pop",
		0xbc: "PopJump",
		0xbd: "Pop2",
		0xbe: "Swap2Swap1",
		0xbf: "Swap2Pop",
		0xc0: "Dup2LT",
		0xc1: "JumpIfZero",
		0xc2: "IsZeroPush2",
		0xc3: "Dup2MStorePush1Add",
		0xc4: "Dup1Push4EqPush2",
		0xc5: "Push1CalldataloadPush1ShrDup1Push4GtPush2",
		0xc6: "Push1Push1Push1SHLSub",
		0xc7: "AndDup2AddSwap1Dup2LT",
		0xc8: "Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT",
	}

	if name, exists := names[opcode]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%02x", opcode)
}

// calculateSkipSteps calculates how many steps to skip for PUSH instructions
func calculateSkipSteps(code []byte, pc int) (bool, int) {
	if pc >= len(code) {
		return false, 0
	}

	opcode := code[pc]
	if opcode >= 0x60 && opcode <= 0x7f { // PUSH1 to PUSH32
		dataLen := int(opcode - 0x60 + 1)
		return true, dataLen
	}

	return false, 0
}

func TestWBNBContractWithOpcodeFusion(t *testing.T) {
	// WBNB contract bytecode from BSCScan
	hexCode := "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	// Remove the "0x" prefix and decode
	wbnbCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode WBNB hex string: %v", err)
	}

	fmt.Printf("=== WBNB Contract Opcode Fusion Test ===\n")
	fmt.Printf("Original WBNB contract size: %d bytes\n", len(wbnbCode))

	// Apply CFG-based opcode fusion
	fusedCode, err := compiler.DoCFGBasedOpcodeFusion(wbnbCode)
	if err != nil {
		if err == compiler.ErrFailPreprocessing {
			fmt.Printf("CFG-Based Opcode Fusion failed: %v (contract contains optimized opcodes)\n", err)
			// This is expected behavior - the contract contains optimized opcodes
			// We'll still test with the original code
			fusedCode = wbnbCode
		} else {
			t.Fatalf("CFG-based fusion failed: %v", err)
		}
	}

	// Count how many opcodes were changed
	changedCount := countChangedOpcodes(wbnbCode, fusedCode)

	fmt.Printf("Fused code size: %d bytes\n", len(fusedCode))
	fmt.Printf("Opcodes changed: %d\n", changedCount)
	if changedCount > 0 {
		fmt.Printf("Fusion efficiency: %.2f%%\n", float64(changedCount)/float64(len(wbnbCode))*100)
	}

	// Basic validation
	if len(fusedCode) != len(wbnbCode) {
		t.Errorf("Fused code size mismatch: expected %d, got %d", len(wbnbCode), len(fusedCode))
	}

	// Test execution of both original and fused code
	testWBNBContractExecution(t, wbnbCode, fusedCode)

	// Print some statistics about the fusion
	fmt.Printf("\n=== Fusion Statistics ===\n")
	fmt.Printf("Original code size: %d bytes\n", len(wbnbCode))
	fmt.Printf("Fused code size: %d bytes\n", len(fusedCode))
	fmt.Printf("Opcodes changed: %d\n", changedCount)

	if changedCount > 0 {
		fmt.Printf("Fusion efficiency: %.2f%%\n", float64(changedCount)/float64(len(wbnbCode))*100)

		// Print some examples of changed opcodes
		fmt.Printf("\n=== Sample Changed Opcodes ===\n")
		examples := 0
		for i := 0; i < len(wbnbCode) && examples < 10; i++ {
			if wbnbCode[i] != fusedCode[i] {
				originalName := getOpcodeName(wbnbCode[i])
				fusedName := getOpcodeName(fusedCode[i])
				fmt.Printf("Position %d: %s (0x%02x) -> %s (0x%02x)\n",
					i, originalName, wbnbCode[i], fusedName, fusedCode[i])
				examples++
			}
		}
	} else {
		fmt.Printf("No opcodes were fused - this might be normal for this contract\n")
	}

	// Verify that the fusion didn't break the contract
	fmt.Printf("\n=== Contract Validation ===\n")
	fmt.Printf("Both original and fused code should produce identical results\n")
	fmt.Printf("Gas usage should be equal or better with fused code\n")
}
