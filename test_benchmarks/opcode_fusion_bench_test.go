package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

// Method selector to name mapping for USDT contract
var usdtMethodSelectors = map[string]string{
	"0x06fdde03": "name()",
	"0x095ea7b3": "approve(address,uint256)",
	"0x18160ddd": "totalSupply()",
	"0x23b872dd": "transferFrom(address,address,uint256)",
	"0x313ce567": "decimals()",
	"0x32424aa3": "owner()",
	"0x39509351": "allowance(address,address)",
	"0x42966c68": "mint(uint256)",
	"0x70a08231": "balanceOf(address)",
	"0x715018a6": "renounceOwnership()",
	"0x893d20e8": "owner()",
	"0x8da5cb5b": "owner()",
	"0x95d89b41": "symbol()",
	"0xa0712d68": "mint(uint256)",
	"0xa457c2d7": "balanceOf(address)",
	"0xa9059cbb": "transfer(address,uint256)",
	"0xb09f1266": "name()",
	"0xd28d8852": "symbol()",
	"0xdd62ed3e": "allowance(address,address)",
	"0xf2fde38b": "transferOwnership(address)",
}

// Method selector to name mapping for WBNB contract
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

func extractMethodSelectors(code []byte) []string {
	var selectors []string
	// Look for PUSH4 followed by 4 bytes (function selectors)
	for i := 0; i < len(code)-7; i++ {
		if code[i] == 0x63 { // PUSH4
			selector := fmt.Sprintf("0x%02x%02x%02x%02x", code[i+1], code[i+2], code[i+3], code[i+4])
			selectors = append(selectors, selector)
		}
	}
	return selectors
}

func printAllMethods(code []byte) {
	fmt.Println("All method selectors found in USDT contract:")
	selectors := extractMethodSelectors(code)

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueSelectors := []string{}
	for _, selector := range selectors {
		if !seen[selector] {
			seen[selector] = true
			uniqueSelectors = append(uniqueSelectors, selector)
		}
	}

	for _, selector := range uniqueSelectors {
		methodName := usdtMethodSelectors[selector]
		if methodName == "" {
			methodName = "unknown"
		}
		fmt.Printf("  %s -> %s\n", selector, methodName)
	}
}

func printOpcodes(label string, code []byte) {
	fmt.Printf("%s opcodes:\n", label)
	for pc := 0; pc < len(code); {
		op := vm.OpCode(code[pc])
		name := op.String()
		// If it's a fused opcode, highlight it
		if op >= vm.OpCode(0xb0) && op <= vm.OpCode(0xc8) {
			name = "[FUSED] " + name
		}
		fmt.Printf("0x%04x: %-20s (0x%02x)\n", pc, name, code[pc])
		pc++
		// Handle PUSHn
		if op >= vm.PUSH1 && op <= vm.PUSH32 {
			n := int(op - vm.PUSH1 + 1)
			pc += n
		}
	}
}

// BytecodeTracer is a custom tracer that prints executed bytecode
type BytecodeTracer struct {
	executedBytes []byte
	pc            uint64
}

func NewBytecodeTracer() *BytecodeTracer {
	return &BytecodeTracer{
		executedBytes: make([]byte, 0),
		pc:            0,
	}
}

func (t *BytecodeTracer) Hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnOpcode: t.OnOpcode,
	}
}

func (t *BytecodeTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	// Record the executed bytecode
	t.executedBytes = append(t.executedBytes, op)
	t.pc = pc

	// Print the opcode being executed
	opcodeName := getOpcodeName(op)
	fmt.Printf("EXEC: PC=%d, OP=%s (0x%02x), Gas=%d, Cost=%d\n", pc, opcodeName, op, gas, cost)

	// For PUSH opcodes, also print the data
	if op >= 0x60 && op <= 0x7f { // PUSH1 to PUSH32
		n := int(op - 0x60 + 1)
		if len(scope.ContractCode()) > int(pc)+n {
			fmt.Printf("     PUSH data: ")
			for i := 1; i <= n; i++ {
				if int(pc)+i < len(scope.ContractCode()) {
					fmt.Printf("0x%02x ", scope.ContractCode()[pc+uint64(i)])
				}
			}
			fmt.Printf("\n")
		}
	}
}

func (t *BytecodeTracer) GetExecutedBytes() []byte {
	return t.executedBytes
}

// getOpcodeName returns a human-readable name for a bytecode opcode
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
		0x0a: "EXP",
		0x0b: "SIGNEXTEND",
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
		0x20: "KECCAK256",
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
		0x46: "CHAINID",
		0x47: "SELFBALANCE",
		0x48: "BASEFEE",
		0x49: "BLOBHASH",
		0x4a: "BLOBBASEFEE",
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
		0x5c: "TLOAD",
		0x5d: "TSTORE",
		0x5e: "MCOPY",
		0x5f: "PUSH0",
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
	}

	if name, exists := names[opcode]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%02x", opcode)
}

func findNameMethodOffset(code []byte) int {
	// The selector for name() is 0x06fdde03
	// Look for PUSH4 06fdde03 (0x63 0x06 0xfd 0xde 0x03) followed by PUSH2 (0x61) with the offset
	for i := 0; i < len(code)-7; i++ {
		if code[i] == 0x63 && code[i+1] == 0x06 && code[i+2] == 0xfd && code[i+3] == 0xde && code[i+4] == 0x03 {
			// Look for the next PUSH2 (0x61) which is the jump destination
			for j := i + 5; j < i+12 && j < len(code)-2; j++ {
				if code[j] == 0x61 {
					offset := int(code[j+1])<<8 | int(code[j+2])
					return offset
				}
			}
		}
	}
	return -1
}

func BenchmarkOpCodeFusionPerformance(b *testing.B) {
	// Example EVM bytecode sequence (can be replaced with more realistic contract code)
	code := []byte{
		byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.PUSH1), 0x03, byte(compiler.MUL), byte(compiler.STOP),
	}

	// Apply fusion to get optimized code
	fusedCode, err := compiler.DoCodeFusion(append([]byte{}, code...))
	if err != nil {
		b.Fatalf("doCodeFusion failed: %v", err)
	}

	// Print original and fused code comparison
	fmt.Printf("\n=== Original Code  ===\n")
	for i := 0; i < len(code); i++ {
		if i%16 == 0 {
			fmt.Printf("\n%04x: ", i)
		}
		fmt.Printf("%02x ", code[i])
	}

	// Print human-readable opcode names for original code
	fmt.Printf("\n\n=== Original Code (Human Readable) ===\n")
	for i := 0; i < len(code); {
		opcodeName := getOpcodeName(code[i])
		fmt.Printf("0x%04x: %s (0x%02x)", i, opcodeName, code[i])

		// Handle PUSH opcodes - show the data
		if code[i] >= 0x60 && code[i] <= 0x7f { // PUSH1 to PUSH32
			n := int(code[i] - 0x60 + 1)
			if i+1+n <= len(code) {
				fmt.Printf(" [data: ")
				for j := 1; j <= n; j++ {
					if j > 1 {
						fmt.Printf(" ")
					}
					fmt.Printf("0x%02x", code[i+j])
				}
				fmt.Printf("]")
			}
		}
		fmt.Printf("\n")
		i++

		// Skip data bytes for PUSH opcodes
		if code[i-1] >= 0x60 && code[i-1] <= 0x7f {
			n := int(code[i-1] - 0x60 + 1)
			i += n
		}
	}
	fmt.Printf("\n\n=== Fused Code ===\n")
	for i := 0; i < len(fusedCode); i++ {
		if i%16 == 0 {
			fmt.Printf("\n%04x: ", i)
		}
		fmt.Printf("%02x ", fusedCode[i])
	}

	// Print human-readable opcode names for fused code
	fmt.Printf("\n\n=== Fused Code (Human Readable) ===\n")
	for i := 0; i < len(fusedCode); {
		opcodeName := getOpcodeName(fusedCode[i])
		fmt.Printf("0x%04x: %s (0x%02x)", i, opcodeName, fusedCode[i])

		// Handle PUSH opcodes - show the data
		if fusedCode[i] >= 0x60 && fusedCode[i] <= 0x7f { // PUSH1 to PUSH32
			n := int(fusedCode[i] - 0x60 + 1)
			if i+1+n <= len(fusedCode) {
				fmt.Printf(" [data: ")
				for j := 1; j <= n; j++ {
					if j > 1 {
						fmt.Printf(" ")
					}
					fmt.Printf("0x%02x", fusedCode[i+j])
				}
				fmt.Printf("]")
			}
		}
		fmt.Printf("\n")
		i++

		// Skip data bytes for PUSH opcodes
		if fusedCode[i-1] >= 0x60 && fusedCode[i-1] <= 0x7f {
			n := int(fusedCode[i-1] - 0x60 + 1)
			i += n
		}
	}

	// Create tracers for execution tracing
	//originalTracer := NewBytecodeTracer()
	//fusedTracer := NewBytecodeTracer()

	cfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
			//Tracer:                    originalTracer.Hooks(),
		},
	}

	cfg_fuse := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
			//Tracer:                    fusedTracer.Hooks(),
		},
	}

	b.Run("OriginalCode", func(b *testing.B) {
		// Create a single EVM instance for all iterations
		// Set up StateDB for the config
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfg.Origin)

		// Reset the EVM for each iteration
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Set up the contract code for each iteration
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, code)

			// Execute the contract
			_, _, err := evm.Call(sender, address, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
			if err != nil {
				b.Fatalf("EVM execution failed (original): %v", err)
			}
		}
	})

	b.Run("FusedCode", func(b *testing.B) {
		// Create a single EVM instance for all iterations
		// Set up StateDB for the config
		if cfg_fuse.State == nil {
			cfg_fuse.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg_fuse)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfg_fuse.Origin)

		// Reset the EVM for each iteration
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Set up the contract code for each iteration
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, code)

			// Execute the contract
			_, _, err := evm.Call(sender, address, nil, cfg_fuse.GasLimit, uint256.MustFromBig(cfg_fuse.Value))
			if err != nil {
				b.Fatalf("EVM execution failed (fused): %v", err)
			}
		}
	})
}

func BenchmarkOpCodeFusionWithUSDTContract(b *testing.B) {
	// USDT contract bytecode from BSCScan
	hexCode := "0x608060405234801561001057600080fd5b506004361061012c5760003560e01c8063893d20e8116100ad578063a9059cbb11610071578063a9059cbb1461035a578063b09f126614610386578063d28d88521461038e578063dd62ed3e14610396578063f2fde38b146103c45761012c565b8063893d20e8146102dd5780638da5cb5b1461030157806395d89b4114610309578063a0712d6814610311578063a457c2d71461032e5761012c565b806332424aa3116100f457806332424aa31461025c578063395093511461026457806342966c681461029057806370a08231146102ad578063715018a6146102d35761012c565b806306fdde0314610131578063095ea7b3146101ae57806318160ddd146101ee57806323b872dd14610208578063313ce5671461023e575b600080fd5b6101396103ea565b6040805160208082528351818301528351919283929083019185019080838360005b8381101561017357818101518382015260200161015b565b50505050905090810190601f1680156101a05780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b6101da600480360360408110156101c457600080fd5b506001600160a01b038135169060200135610480565b604080519115158252519081900360200190f35b6101f661049d565b60408051918252519081900360200190f35b6101da6004803603606081101561021e57600080fd5b506001600160a01b038135811691602081013590911690604001356104a3565b610246610530565b6040805160ff9092168252519081900360200190f35b610246610539565b6101da6004803603604081101561027a57600080fd5b506001600160a01b038135169060200135610542565b6101da600480360360208110156102a657600080fd5b5035610596565b6101f6600480360360208110156102c357600080fd5b50356001600160a01b03166105b1565b6102db6105cc565b005b6102e5610680565b604080516001600160a01b039092168252519081900360200190f35b6102e561068f565b61013961069e565b6101da6004803603602081101561032757600080fd5b50356106ff565b6101da6004803603604081101561034457600080fd5b506001600160a01b03813516906020013561077c565b6101da6004803603604081101561037057600080fd5b506001600160a01b0381351690602001356107ea565b6101396107fe565b61013961088c565b6101f6600480360360408110156103ac57600080fd5b506001600160a01b03813581169160200135166108e7565b6102db600480360360208110156103da57600080fd5b50356001600160a01b0316610912565b60068054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b5050505050905090565b600061049461048d610988565b848461098c565b50600192915050565b60035490565b60006104b0848484610a78565b610526846104bc610988565b6105218560405180606001604052806028815260200161100e602891396001600160a01b038a166000908152600260205260408120906104fa610988565b6001600160a01b03168152602081019190915260400160002054919063ffffffff610bd616565b61098c565b5060019392505050565b60045460ff1690565b60045460ff1681565b600061049461054f610988565b846105218560026000610560610988565b6001600160a01b03908116825260208083019390935260409182016000908120918c16815292529020549063ffffffff610c6d16565b60006105a96105a3610988565b83610cce565b506001919050565b6001600160a01b031660009081526001602052604090205490565b6105d4610988565b6000546001600160a01b03908116911614610636576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b600080546040516001600160a01b03909116907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0908390a3600080546001600160a01b0319169055565b600061068a61068f565b905090565b6000546001600160a01b031690565b60058054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b505050505081565b6006805460408051602060026001851615610100026000190190941693909304601f810184900484028201840190925281815292918301828280156108845780601f1061085957610100808354040283529160200191610884565b6001600160a01b03918216600090815260026020908152604080832093909416825291909152205490565b61091a610988565b6000546001600160a01b0390811691161461097c576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b61098581610ebc565b50565b3390565b6001600160a01b0383166109d15760405162461bcd60e51b8152600401808060200182810382526024815260200180610fc46024913960400191505060405180910390fd5b6001600160a01b038216610a165760405162461bcd60e51b81526004018080602001828103825260228152602001806110e76022913960400191505060405180910390fd5b6001600160a01b03808416600081815260026020908152604080832094871680845294825291829020859055815185815291517f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259281900390910190a3505050565b6001600160a01b038316610abd5760405162461bcd60e51b8152600401808060200182810382526025815260200180610f9f6025913960400191505060405180910390fd5b6001600160a01b038216610b025760405162461bcd60e51b815260040180806020018281038252602381526020018061105c6023913960400191505060405180910390fd5b610b4581604051806060016040528060268152602001611036602691396001600160a01b038616600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038085166000908152600160205260408082209390935590841681522054610b7a908263ffffffff610c6d16565b6001600160a01b0380841660008181526001602090815260409182902094909455805185815290519193928716927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a3505050565b60008184841115610c655760405162461bcd60e51b81526004018080602001828103825283818151815260200191508051906020019080838360005b83811015610c2a578181015183820152602001610c12565b50505050905090810190601f168015610c575780820380516001836020036101000a031916815260200191505b509250505060405180910390fd5b505050900390565b600082820183811015610cc7576040805162461bcd60e51b815260206004820152601b60248201527f536166654d6174683a206164646974696f6e206f766572666c6f770000000000604482015290519081900360640190fd5b9392505050565b6001600160a01b038216610d135760405162461bcd60e51b81526004018080602001828103825260218152602001806110a46021913960400191505060405180910390fd5b610d56816040518060600160405280602281526020016110c5602291396001600160a01b038516600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038316600090815260016020526040902055600354610d82908263ffffffff610f5c16565b6003556040805182815290516000916001600160a01b038516917fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181900360200190a35050565b6001600160a01b038216610e255760405162461bcd60e51b815260040180806020018281038252601f60248201527f42455032303a206d696e7420746f20746865207a65726f206164647265737300604482015290519081900360640190fd5b600354610e38908263ffffffff610c6d16565b6003556001600160a01b038316600090815260016020526040902054610e64908263ffffffff610c6d16565b6001600160a01b03831660008181526001602090815260408083209490945583518581529351929391927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a35050565b6001600160a01b038116610f015760405162461bcd60e51b8152600401808060200182810382526026815260200180610fe86026913960400191505060405180910390fd5b600080546040516001600160a01b03808516939216917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e091a3600080546001600160a01b0319166001600160a01b0392909216919091179055565b6000610cc783836040518060400160405280601e81526020017f536166654d6174683a207375627472616374696f6e206f766572666c6f770000815250610bd656fe42455032303a207472616e736665722066726f6d20746865207a65726f206164647265737342455032303a20617070726f76652066726f6d20746865207a65726f20616464726573734f776e61626c653a206e6577206f776e657220697320746865207a65726f206164647265737342455032303a207472616e7366657220616d6f756e74206578636565647320616c6c6f77616e636542455032303a207472616e7366657220616d6f756e7420657863656564732062616c616e636542455032303a207472616e7366657220746f20746865207a65726f206164647265737342455032303a2064656372656173656420616c6c6f77616e63652062656c6f77207a65726f42455032303a206275726e2066726f6d20746865207a65726f206164647265737342455032303a206275726e20616d6f756e7420657863656564732062616c616e636542455032303a20617070726f766520746f20746865207a65726f2061646472657373a265627a7a72315820cbbd570ae478f6b7abf9c9a5c8c6884cf3f64dded74f7ec3e9b6d0b41122eaff64736f6c63430005100032"

	// Remove the "0x" prefix and decode
	realCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		b.Fatalf("Failed to decode hex string: %v", err)
	}

	// Print all available methods in the USDT contract
	// printAllMethods(realCode)

	// Apply fusion to get optimized code
	fusedCode, err := compiler.DoCodeFusion(append([]byte{}, realCode...))
	if err != nil {
		// If fusion fails (e.g., code already contains optimized opcodes), use original code
		fmt.Printf("doCodeFusion failed: %v, using original code for fused tests\n", err)
		fusedCode = realCode
	}
	_ = fusedCode // Use the variable to avoid unused variable warning

	// Create tracer for USDT contract execution
	//usdtTracer := NewBytecodeTracer()

	cfg := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
			//Tracer:                    usdtTracer.Hooks(),
		},
	}

	cfgFuse := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
			//Tracer:                    usdtTracer.Hooks(),
		},
	}

	// Prepare dummy data for parameters
	zeroAddress := make([]byte, 32)
	copy(zeroAddress[12:], make([]byte, 20)) // 20-byte address at the end
	oneUint := make([]byte, 32)
	oneUint[31] = 1

	methods := []struct {
		name     string
		selector []byte
		args     [][]byte // encoded arguments
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, oneUint}},
		{"transferFrom", []byte{0x23, 0xb8, 0x72, 0xdd}, [][]byte{zeroAddress, zeroAddress, oneUint}},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zeroAddress, oneUint}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zeroAddress, zeroAddress}},
		{"owner", []byte{0x8d, 0xa5, 0xcb, 0x5b}, nil},
		{"transferOwnership", []byte{0xf2, 0xfd, 0xe3, 0x8b}, [][]byte{zeroAddress}},
		{"renounceOwnership", []byte{0x71, 0x50, 0x18, 0xa6}, nil},
		{"mint", []byte{0xa0, 0x71, 0x2d, 0x68}, [][]byte{oneUint}},
	}

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, arg := range m.args {
			input = append(input, arg...)
		}

		b.Run("OriginalCode_"+m.name, func(b *testing.B) {
			// Set up StateDB for the config
			if cfg.State == nil {
				cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfg)
			address := common.BytesToAddress([]byte("contract"))
			sender := vm.AccountRef(cfg.Origin)
			// Set up the contract code for each iteration
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, realCode)
			// Reset the EVM for each iteration
			b.ResetTimer()
			for i := 0; i < b.N; i++ {

				// Execute the contract
				_, _, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
				if err != nil {
					// Many methods will revert (e.g., mint if not owner), so just ignore
					continue
				}
			}
		})

		b.Run("FusedCode_"+m.name, func(b *testing.B) {
			// Set up StateDB for the config
			if cfgFuse.State == nil {
				cfgFuse.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfgFuse)
			address := common.BytesToAddress([]byte("contract"))
			sender := vm.AccountRef(cfgFuse.Origin)
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, realCode)
			// Reset the EVM for each iteration
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Set up the contract code for each iteration

				// Execute the contract
				_, _, err := evm.Call(sender, address, input, cfgFuse.GasLimit, uint256.MustFromBig(cfgFuse.Value))
				if err != nil {
					continue
				}
			}
		})
	}
	/*
		// After the test cases, print the opcodes for the name() method only
		nameOffset := findNameMethodOffset(realCode)

		if nameOffset != -1 {
			fmt.Printf("\nDisassembly for name() method (offset 0x%x):\n", nameOffset)
			printOpcodes("Original name()", realCode[nameOffset:])
			printOpcodes("Fused name()", fusedCode[nameOffset:])
		} else {
			fmt.Println("Could not find name() method offset.")
		}
	*/
}

func BenchmarkOpCodeFusionWithWBNBContract(b *testing.B) {
	// WBNB contract bytecode from BSCScan
	hexCode := "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	// Remove the "0x" prefix and decode
	realCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		b.Fatalf("Failed to decode hex string: %v", err)
	}

	// Apply fusion to get optimized code
	fusedCode, err := compiler.DoCodeFusion(append([]byte{}, realCode...))
	if err != nil {
		// If fusion fails (e.g., code already contains optimized opcodes), use original code
		fmt.Printf("doCodeFusion failed: %v, using original code for fused tests\n", err)
		fusedCode = realCode
	}
	_ = fusedCode // Use the variable to avoid unused variable warning

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

	cfgFuse := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
		},
	}

	// Prepare dummy data for parameters
	zeroAddress := make([]byte, 32)
	copy(zeroAddress[12:], make([]byte, 20)) // 20-byte address at the end
	oneUint := make([]byte, 32)
	oneUint[31] = 1

	methods := []struct {
		name     string
		selector []byte
		args     [][]byte // encoded arguments
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, oneUint}},
		{"transferFrom", []byte{0x23, 0xb8, 0x72, 0xdd}, [][]byte{zeroAddress, zeroAddress, oneUint}},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zeroAddress, oneUint}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zeroAddress, zeroAddress}},
		{"withdraw", []byte{0x2e, 0x1a, 0x7d, 0x4d}, [][]byte{oneUint}},
		{"deposit", []byte{0xd0, 0xe3, 0x0d, 0xb0}, nil},
	}

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, arg := range m.args {
			input = append(input, arg...)
		}

		b.Run("OriginalCode_"+m.name, func(b *testing.B) {
			// Set up StateDB for the config
			if cfg.State == nil {
				cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfg)
			address := common.BytesToAddress([]byte("contract"))
			sender := vm.AccountRef(cfg.Origin)
			// Set up the contract code for each iteration
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, realCode)
			// Reset the EVM for each iteration
			b.ResetTimer()
			for i := 0; i < b.N; i++ {

				// Execute the contract
				_, _, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
				if err != nil {
					// Many methods will revert (e.g., withdraw if no balance), so just ignore
					continue
				}
			}
		})

		b.Run("FusedCode_"+m.name, func(b *testing.B) {
			// Set up StateDB for the config
			if cfgFuse.State == nil {
				cfgFuse.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfgFuse)
			address := common.BytesToAddress([]byte("contract"))
			sender := vm.AccountRef(cfgFuse.Origin)
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, fusedCode)
			// Reset the EVM for each iteration
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Set up the contract code for each iteration

				// Execute the contract
				_, _, err := evm.Call(sender, address, input, cfgFuse.GasLimit, uint256.MustFromBig(cfgFuse.Value))
				if err != nil {
					continue
				}
			}
		})
	}
}

func TestPrintWBNBMethods(t *testing.T) {
	// WBNB contract bytecode from BSCScan
	hexCode := "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	// Remove the "0x" prefix and decode
	realCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode hex string: %v", err)
	}

	// Print all available methods in the WBNB contract
	fmt.Println("All method selectors found in WBNB contract:")
	selectors := extractMethodSelectors(realCode)

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueSelectors := []string{}
	for _, selector := range selectors {
		if !seen[selector] {
			seen[selector] = true
			uniqueSelectors = append(uniqueSelectors, selector)
		}
	}

	for _, selector := range uniqueSelectors {
		methodName := wbnbMethodSelectors[selector]
		if methodName == "" {
			methodName = "unknown"
		}
		fmt.Printf("  %s -> %s\n", selector, methodName)
	}
}

func TestPrintUSDTMethods(t *testing.T) {
	// USDT contract bytecode from BSCScan
	hexCode := "0x608060405234801561001057600080fd5b506004361061012c5760003560e01c8063893d20e8116100ad578063a9059cbb11610071578063a9059cbb1461035a578063b09f126614610386578063d28d88521461038e578063dd62ed3e14610396578063f2fde38b146103c45761012c565b8063893d20e8146102dd5780638da5cb5b1461030157806395d89b4114610309578063a0712d6814610311578063a457c2d71461032e5761012c565b806332424aa3116100f457806332424aa31461025c578063395093511461026457806342966c681461029057806370a08231146102ad578063715018a6146102d35761012c565b806306fdde0314610131578063095ea7b3146101ae57806318160ddd146101ee57806323b872dd14610208578063313ce5671461023e575b600080fd5b6101396103ea565b6040805160208082528351818301528351919283929083019185019080838360005b8381101561017357818101518382015260200161015b565b50505050905090810190601f1680156101a05780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b6101da600480360360408110156101c457600080fd5b506001600160a01b038135169060200135610480565b604080519115158252519081900360200190f35b6101f661049d565b60408051918252519081900360200190f35b6101da6004803603606081101561021e57600080fd5b506001600160a01b038135811691602081013590911690604001356104a3565b610246610530565b6040805160ff9092168252519081900360200190f35b610246610539565b6101da6004803603604081101561027a57600080fd5b506001600160a01b038135169060200135610542565b6101da600480360360208110156102a657600080fd5b5035610596565b6101f6600480360360208110156102c357600080fd5b50356001600160a01b03166105b1565b6102db6105cc565b005b6102e5610680565b604080516001600160a01b039092168252519081900360200190f35b6102e561068f565b61013961069e565b6101da6004803603602081101561032757600080fd5b50356106ff565b6101da6004803603604081101561034457600080fd5b506001600160a01b03813516906020013561077c565b6101da6004803603604081101561037057600080fd5b506001600160a01b0381351690602001356107ea565b6101396107fe565b61013961088c565b6101f6600480360360408110156103ac57600080fd5b506001600160a01b03813581169160200135166108e7565b6102db600480360360208110156103da57600080fd5b50356001600160a01b0316610912565b60068054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b5050505050905090565b600061049461048d610988565b848461098c565b50600192915050565b60035490565b60006104b0848484610a78565b610526846104bc610988565b6105218560405180606001604052806028815260200161100e602891396001600160a01b038a166000908152600260205260408120906104fa610988565b6001600160a01b03168152602081019190915260400160002054919063ffffffff610bd616565b61098c565b5060019392505050565b60045460ff1690565b60045460ff1681565b600061049461054f610988565b846105218560026000610560610988565b6001600160a01b03908116825260208083019390935260409182016000908120918c16815292529020549063ffffffff610c6d16565b60006105a96105a3610988565b83610cce565b506001919050565b6001600160a01b031660009081526001602052604090205490565b6105d4610988565b6000546001600160a01b03908116911614610636576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b600080546040516001600160a01b03909116907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0908390a3600080546001600160a01b0319169055565b600061068a61068f565b905090565b6000546001600160a01b031690565b60058054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b505050505081565b6006805460408051602060026001851615610100026000190190941693909304601f810184900484028201840190925281815292918301828280156108845780601f1061085957610100808354040283529160200191610884565b6001600160a01b03918216600090815260026020908152604080832093909416825291909152205490565b61091a610988565b6000546001600160a01b0390811691161461097c576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b61098581610ebc565b50565b3390565b6001600160a01b0383166109d15760405162461bcd60e51b8152600401808060200182810382526024815260200180610fc46024913960400191505060405180910390fd5b6001600160a01b038216610a165760405162461bcd60e51b81526004018080602001828103825260228152602001806110e76022913960400191505060405180910390fd5b6001600160a01b03808416600081815260026020908152604080832094871680845294825291829020859055815185815291517f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259281900390910190a3505050565b6001600160a01b038316610abd5760405162461bcd60e51b8152600401808060200182810382526025815260200180610f9f6025913960400191505060405180910390fd5b6001600160a01b038216610b025760405162461bcd60e51b815260040180806020018281038252602381526020018061105c6023913960400191505060405180910390fd5b610b4581604051806060016040528060268152602001611036602691396001600160a01b038616600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038085166000908152600160205260408082209390935590841681522054610b7a908263ffffffff610c6d16565b6001600160a01b0380841660008181526001602090815260409182902094909455805185815290519193928716927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a3505050565b60008184841115610c655760405162461bcd60e51b81526004018080602001828103825283818151815260200191508051906020019080838360005b83811015610c2a578181015183820152602001610c12565b50505050905090810190601f168015610c575780820380516001836020036101000a031916815260200191505b509250505060405180910390fd5b505050900390565b600082820183811015610cc7576040805162461bcd60e51b815260206004820152601b60248201527f536166654d6174683a206164646974696f6e206f766572666c6f770000000000604482015290519081900360640190fd5b9392505050565b6001600160a01b038216610d135760405162461bcd60e51b81526004018080602001828103825260218152602001806110a46021913960400191505060405180910390fd5b610d56816040518060600160405280602281526020016110c5602291396001600160a01b038516600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038316600090815260016020526040902055600354610d82908263ffffffff610f5c16565b6003556040805182815290516000916001600160a01b038516917fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181900360200190a35050565b6001600160a01b038216610e255760405162461bcd60e51b815260040180806020018281038252601f60248201527f42455032303a206d696e7420746f20746865207a65726f206164647265737300604482015290519081900360640190fd5b600354610e38908263ffffffff610c6d16565b6003556001600160a01b038316600090815260016020526040902054610e64908263ffffffff610c6d16565b6001600160a01b03831660008181526001602090815260408083209490945583518581529351929391927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a35050565b6001600160a01b038116610f015760405162461bcd60e51b8152600401808060200182810382526026815260200180610fe86026913960400191505060405180910390fd5b600080546040516001600160a01b03808516939216917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e091a3600080546001600160a01b0319166001600160a01b0392909216919091179055565b6000610cc783836040518060400160405280601e81526020017f536166654d6174683a207375627472616374696f6e206f766572666c6f770000815250610bd656fe42455032303a207472616e736665722066726f6d20746865207a65726f206164647265737342455032303a20617070726f76652066726f6d20746865207a65726f20616464726573734f776e61626c653a206e6577206f776e657220697320746865207a65726f206164647265737342455032303a207472616e7366657220616d6f756e74206578636565647320616c6c6f77616e636542455032303a207472616e7366657220616d6f756e7420657863656564732062616c616e636542455032303a207472616e7366657220746f20746865207a65726f206164647265737342455032303a2064656372656173656420616c6c6f77616e63652062656c6f77207a65726f42455032303a206275726e2066726f6d20746865207a65726f206164647265737342455032303a206275726e20616d6f756e7420657863656564732062616c616e636542455032303a20617070726f766520746f20746865207a65726f2061646472657373a265627a7a72315820cbbd570ae478f6b7abf9c9a5c8c6884cf3f64dded74f7ec3e9b6d0b41122eaff64736f6c63430005100032"

	// Remove the "0x" prefix and decode
	realCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode hex string: %v", err)
	}

	// Print all available methods in the USDT contract
	printAllMethods(realCode)
}

func TestBytecodeTracer(t *testing.T) {
	// Simple test to demonstrate the tracer functionality
	code := []byte{
		byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.STOP),
	}

	tracer := NewBytecodeTracer()
	cfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		EVMConfig: vm.Config{
			Tracer: tracer.Hooks(),
		},
	}

	fmt.Printf("=== Test Bytecode Execution Trace ===\n")
	_, _, err := runtime.Execute(code, nil, cfg)
	if err != nil {
		t.Fatalf("EVM execution failed: %v", err)
	}

	fmt.Printf("Executed bytes: %x\n", tracer.GetExecutedBytes())
}
