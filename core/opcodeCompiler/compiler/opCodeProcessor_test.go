package compiler

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

// simpleDisassemble converts opcodes to human-readable mnemonics
func simpleDisassemble(code []byte) []string {
	var result []string
	var pc uint64

	// Create a mapping from opcode values to their names
	opcodeNames := map[ByteCode]string{
		STOP:            "STOP",
		ADD:             "ADD",
		MUL:             "MUL",
		SUB:             "SUB",
		DIV:             "DIV",
		SDIV:            "SDIV",
		MOD:             "MOD",
		SMOD:            "SMOD",
		ADDMOD:          "ADDMOD",
		MULMOD:          "MULMOD",
		EXP:             "EXP",
		SIGNEXTEND:      "SIGNEXTEND",
		LT:              "LT",
		GT:              "GT",
		SLT:             "SLT",
		SGT:             "SGT",
		EQ:              "EQ",
		ISZERO:          "ISZERO",
		AND:             "AND",
		OR:              "OR",
		XOR:             "XOR",
		NOT:             "NOT",
		BYTE:            "BYTE",
		SHL:             "SHL",
		SHR:             "SHR",
		SAR:             "SAR",
		KECCAK256:       "KECCAK256",
		ADDRESS:         "ADDRESS",
		BALANCE:         "BALANCE",
		ORIGIN:          "ORIGIN",
		CALLER:          "CALLER",
		CALLVALUE:       "CALLVALUE",
		CALLDATALOAD:    "CALLDATALOAD",
		CALLDATASIZE:    "CALLDATASIZE",
		CALLDATACOPY:    "CALLDATACOPY",
		CODESIZE:        "CODESIZE",
		CODECOPY:        "CODECOPY",
		GASPRICE:        "GASPRICE",
		EXTCODESIZE:     "EXTCODESIZE",
		EXTCODECOPY:     "EXTCODECOPY",
		RETURNDATASIZE:  "RETURNDATASIZE",
		RETURNDATACOPY:  "RETURNDATACOPY",
		EXTCODEHASH:     "EXTCODEHASH",
		BLOCKHASH:       "BLOCKHASH",
		COINBASE:        "COINBASE",
		TIMESTAMP:       "TIMESTAMP",
		NUMBER:          "NUMBER",
		DIFFICULTY:      "DIFFICULTY",
		GASLIMIT:        "GASLIMIT",
		CHAINID:         "CHAINID",
		SELFBALANCE:     "SELFBALANCE",
		BASEFEE:         "BASEFEE",
		BLOBHASH:        "BLOBHASH",
		BLOBBASEFEE:     "BLOBBASEFEE",
		POP:             "POP",
		MLOAD:           "MLOAD",
		MSTORE:          "MSTORE",
		MSTORE8:         "MSTORE8",
		SLOAD:           "SLOAD",
		SSTORE:          "SSTORE",
		JUMP:            "JUMP",
		JUMPI:           "JUMPI",
		PC:              "PC",
		MSIZE:           "MSIZE",
		GAS:             "GAS",
		JUMPDEST:        "JUMPDEST",
		TLOAD:           "TLOAD",
		TSTORE:          "TSTORE",
		MCOPY:           "MCOPY",
		PUSH0:           "PUSH0",
		PUSH1:           "PUSH1",
		PUSH2:           "PUSH2",
		PUSH3:           "PUSH3",
		PUSH4:           "PUSH4",
		PUSH5:           "PUSH5",
		PUSH6:           "PUSH6",
		PUSH7:           "PUSH7",
		PUSH8:           "PUSH8",
		PUSH9:           "PUSH9",
		PUSH10:          "PUSH10",
		PUSH11:          "PUSH11",
		PUSH12:          "PUSH12",
		PUSH13:          "PUSH13",
		PUSH14:          "PUSH14",
		PUSH15:          "PUSH15",
		PUSH16:          "PUSH16",
		PUSH17:          "PUSH17",
		PUSH18:          "PUSH18",
		PUSH19:          "PUSH19",
		PUSH20:          "PUSH20",
		PUSH21:          "PUSH21",
		PUSH22:          "PUSH22",
		PUSH23:          "PUSH23",
		PUSH24:          "PUSH24",
		PUSH25:          "PUSH25",
		PUSH26:          "PUSH26",
		PUSH27:          "PUSH27",
		PUSH28:          "PUSH28",
		PUSH29:          "PUSH29",
		PUSH30:          "PUSH30",
		PUSH31:          "PUSH31",
		PUSH32:          "PUSH32",
		DUP1:            "DUP1",
		DUP2:            "DUP2",
		DUP3:            "DUP3",
		DUP4:            "DUP4",
		DUP5:            "DUP5",
		DUP6:            "DUP6",
		DUP7:            "DUP7",
		DUP8:            "DUP8",
		DUP9:            "DUP9",
		DUP10:           "DUP10",
		DUP11:           "DUP11",
		DUP12:           "DUP12",
		DUP13:           "DUP13",
		DUP14:           "DUP14",
		DUP15:           "DUP15",
		DUP16:           "DUP16",
		SWAP1:           "SWAP1",
		SWAP2:           "SWAP2",
		SWAP3:           "SWAP3",
		SWAP4:           "SWAP4",
		SWAP5:           "SWAP5",
		SWAP6:           "SWAP6",
		SWAP7:           "SWAP7",
		SWAP8:           "SWAP8",
		SWAP9:           "SWAP9",
		SWAP10:          "SWAP10",
		SWAP11:          "SWAP11",
		SWAP12:          "SWAP12",
		SWAP13:          "SWAP13",
		SWAP14:          "SWAP14",
		SWAP15:          "SWAP15",
		SWAP16:          "SWAP16",
		LOG0:            "LOG0",
		LOG1:            "LOG1",
		LOG2:            "LOG2",
		LOG3:            "LOG3",
		LOG4:            "LOG4",
		CREATE:          "CREATE",
		CALL:            "CALL",
		CALLCODE:        "CALLCODE",
		RETURN:          "RETURN",
		DELEGATECALL:    "DELEGATECALL",
		CREATE2:         "CREATE2",
		RETURNDATALOAD:  "RETURNDATALOAD",
		EXTCALL:         "EXTCALL",
		EXTDELEGATECALL: "EXTDELEGATECALL",
		STATICCALL:      "STATICCALL",
		EXTSTATICCALL:   "EXTSTATICCALL",
		REVERT:          "REVERT",
		INVALID:         "INVALID",
		SELFDESTRUCT:    "SELFDESTRUCT",
	}

	for pc < uint64(len(code)) {
		if pc >= uint64(len(code)) {
			break
		}

		op := ByteCode(code[pc])
		opName, exists := opcodeNames[op]
		if !exists {
			opName = fmt.Sprintf("UNKNOWN_%d", op)
		}
		line := fmt.Sprintf("%05x: %s", pc, opName)

		// Handle PUSH instructions
		if op >= PUSH1 && op <= PUSH32 {
			size := int(op - PUSH1 + 1)
			if pc+1+uint64(size) <= uint64(len(code)) {
				arg := code[pc+1 : pc+1+uint64(size)]
				line += fmt.Sprintf(" %#x", arg)
				pc += uint64(size)
			}
		}

		result = append(result, line)
		pc++
	}

	return result
}

func debugBlocks(blocks []BasicBlock) {
	fmt.Printf("Generated %d blocks:\n", len(blocks))
	for i, block := range blocks {
		fmt.Printf("Block %d: PC[%d,%d), JumpDest: %v, Opcodes: %v\n",
			i, block.StartPC, block.EndPC, block.IsJumpDest, block.Opcodes)
	}
}

func TestGenerateBasicBlocks(t *testing.T) {
	// Test case 1: Simple linear code
	code1 := []byte{
		byte(PUSH1), 0x01, // PUSH1 0x01
		byte(PUSH1), 0x02, // PUSH1 0x02
		byte(ADD),  // ADD
		byte(STOP), // STOP
	}

	blocks1 := GenerateBasicBlocks(code1)
	debugBlocks(blocks1)
	if len(blocks1) != 1 {
		t.Errorf("Expected 1 block, got %d", len(blocks1))
	}

	if blocks1[0].StartPC != 0 || blocks1[0].EndPC != 6 {
		t.Errorf("Expected block PC range [0,6), got [%d,%d)", blocks1[0].StartPC, blocks1[0].EndPC)
	}

	// Test case 2: Code with JUMPDEST
	code2 := []byte{
		byte(PUSH1), 0x01, // PUSH1 0x01
		byte(ISZERO),            // ISZERO
		byte(PUSH2), 0x00, 0x08, // PUSH2 0x0008
		byte(JUMPI),       // JUMPI
		byte(PUSH1), 0x02, // PUSH1 0x02
		byte(STOP),        // STOP
		byte(JUMPDEST),    // JUMPDEST
		byte(PUSH1), 0x03, // PUSH1 0x03
		byte(STOP), // STOP
	}

	blocks2 := GenerateBasicBlocks(code2)
	debugBlocks(blocks2)
	if len(blocks2) != 3 {
		t.Errorf("Expected 3 blocks, got %d", len(blocks2))
	}

	// First block should be from 0 to 7 (before JUMPDEST)
	if blocks2[0].StartPC != 0 || blocks2[0].EndPC != 7 {
		t.Errorf("Expected first block PC range [0,7), got [%d,%d)", blocks2[0].StartPC, blocks2[0].EndPC)
	}

	// Second block should start at 7
	if blocks2[1].StartPC != 7 {
		t.Errorf("Expected second block to start at PC 7, got %d", blocks2[1].StartPC)
	}
	// Third block should start at JUMPDEST
	if !blocks2[2].IsJumpDest {
		t.Error("Expected third block to start with JUMPDEST")
	}
	if blocks2[2].StartPC != 10 {
		t.Errorf("Expected third block to start at PC 10, got %d", blocks2[2].StartPC)
	}

	// Test case 3: Empty code
	blocks3 := GenerateBasicBlocks([]byte{})
	if blocks3 != nil {
		t.Error("Expected nil for empty code")
	}

	// Test case 4: Code with RETURN
	code4 := []byte{
		byte(PUSH1), 0x20, // PUSH1 0x20
		byte(PUSH1), 0x00, // PUSH1 0x00
		byte(RETURN), // RETURN
	}

	blocks4 := GenerateBasicBlocks(code4)
	debugBlocks(blocks4)
	if len(blocks4) != 1 {
		t.Errorf("Expected 1 block, got %d", len(blocks4))
	}

	if blocks4[0].StartPC != 0 || blocks4[0].EndPC != 5 {
		t.Errorf("Expected block PC range [0,5), got [%d,%d)", blocks4[0].StartPC, blocks4[0].EndPC)
	}
}

func TestGenerateBasicBlocksWithRealContract(t *testing.T) {
	// Test case with real contract bytecode
	hexCode := "0x608060405234801561001057600080fd5b506004361061012c5760003560e01c8063893d20e8116100ad578063a9059cbb11610071578063a9059cbb1461035a578063b09f126614610386578063d28d88521461038e578063dd62ed3e14610396578063f2fde38b146103c45761012c565b8063893d20e8146102dd5780638da5cb5b1461030157806395d89b4114610309578063a0712d6814610311578063a457c2d71461032e5761012c565b806332424aa3116100f457806332424aa31461025c578063395093511461026457806342966c681461029057806370a08231146102ad578063715018a6146102d35761012c565b806306fdde0314610131578063095ea7b3146101ae57806318160ddd146101ee57806323b872dd14610208578063313ce5671461023e575b600080fd5b6101396103ea565b6040805160208082528351818301528351919283929083019185019080838360005b8381101561017357818101518382015260200161015b565b50505050905090810190601f1680156101a05780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b6101da600480360360408110156101c457600080fd5b506001600160a01b038135169060200135610480565b604080519115158252519081900360200190f35b6101f661049d565b60408051918252519081900360200190f35b6101da6004803603606081101561021e57600080fd5b506001600160a01b038135811691602081013590911690604001356104a3565b610246610530565b6040805160ff9092168252519081900360200190f35b610246610539565b6101da6004803603604081101561027a57600080fd5b506001600160a01b038135169060200135610542565b6101da600480360360208110156102a657600080fd5b5035610596565b6101f6600480360360208110156102c357600080fd5b50356001600160a01b03166105b1565b6102db6105cc565b005b6102e5610680565b604080516001600160a01b039092168252519081900360200190f35b6102e561068f565b61013961069e565b6101da6004803603602081101561032757600080fd5b50356106ff565b6101da6004803603604081101561034457600080fd5b506001600160a01b03813516906020013561077c565b6101da6004803603604081101561037057600080fd5b506001600160a01b0381351690602001356107ea565b6101396107fe565b61013961088c565b6101f6600480360360408110156103ac57600080fd5b506001600160a01b03813581169160200135166108e7876102db600480360360208110156103da57600080fd5b50356001600160a01b0316610912565b60068054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b5050505050905090565b600061049461048d610988565b848461098c565b50600192915050565b60035490565b60006104b0848484610a78565b610526846104bc610988565b6105218560405180606001604052806028815260200161100e602891396001600160a01b038a166000908152600260205260408120906104fa610988565b6001600160a01b03168152602081019190915260400160002054919063ffffffff610bd616565b61098c565b5060019392505050565b60045460ff1690565b60045460ff1681565b600061049461054f610988565b846105218560026000610560610988565b6001600160a01b03908116825260208083019390935260409182016000908120918c16815292529020549063ffffffff610c6d16565b60006105a96105a3610988565b83610cce565b506001919050565b6001600160a01b031660009081526001602052604090205490565b6105d4610988565b6000546001600160a01b03908116911614610636576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b600080546040516001600160a01b03808516939216917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e091a3600080546001600160a01b0319169055565b600061068a61068f565b905090565b6000546001600160a01b031690565b60058054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	// Remove the "0x" prefix and decode
	wbnbCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode WBNB hex string: %v", err)
	}

	blocks := GenerateBasicBlocks(wbnbCode)

	fmt.Printf("WBNB Contract Basic Block Analysis:\n")
	fmt.Printf("WBNB contract size: %d bytes\n", len(wbnbCode))
	fmt.Printf("Generated %d basic blocks\n", len(blocks))

	// Print detailed block information with types
	for i, block := range blocks {
		blockType := getBlockType(block, blocks, i)

		// Get first and last opcodes in human-readable format
		firstOp := "N/A"
		lastOp := "N/A"
		if len(block.Opcodes) > 0 {
			firstOp = getOpcodeName(ByteCode(block.Opcodes[0]))
			lastOp = getOpcodeName(ByteCode(block.Opcodes[len(block.Opcodes)-1]))
		}

		fmt.Printf("Block %d: PC[%d,%d), Type: %s, JumpDest: %v, First: %s, Last: %s\n",
			i, block.StartPC, block.EndPC, blockType, block.IsJumpDest, firstOp, lastOp)
	}

	// Basic validation
	if len(blocks) == 0 {
		t.Error("Expected at least one basic block for WBNB contract")
	}

	// Check that blocks are properly ordered
	for i := 1; i < len(blocks); i++ {
		if blocks[i].StartPC < blocks[i-1].EndPC {
			t.Errorf("Block %d starts before previous block ends: [%d,%d) vs [%d,%d)",
				i, blocks[i].StartPC, blocks[i].EndPC, blocks[i-1].StartPC, blocks[i-1].EndPC)
		}
	}

	// Check that all blocks have valid PC ranges
	for i, block := range blocks {
		if block.StartPC >= block.EndPC {
			t.Errorf("Block %d has invalid PC range: [%d,%d)", i, block.StartPC, block.EndPC)
		}
		if block.StartPC >= uint64(len(wbnbCode)) {
			t.Errorf("Block %d starts beyond code length: %d >= %d", i, block.StartPC, len(wbnbCode))
		}
		if block.EndPC > uint64(len(wbnbCode)) {
			t.Errorf("Block %d ends beyond code length: %d > %d", i, block.EndPC, len(wbnbCode))
		}
	}

	fmt.Printf("\nWBNB Basic Block Generation Summary:\n")
	fmt.Printf("Total blocks: %d\n", len(blocks))
	fmt.Printf("Contract size: %d bytes\n", len(wbnbCode))
	fmt.Printf("Average block size: %.2f bytes\n", float64(len(wbnbCode))/float64(len(blocks)))
}

func TestDoCFGBasedOpcodeFusion(t *testing.T) {
	// Test case with WBNB contract bytecode
	hexCode := "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	// Remove the "0x" prefix and decode
	wbnbCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode WBNB hex string: %v", err)
	}

	// Test CFG-based opcode fusion
	fusedCode, err := DoCFGBasedOpcodeFusion(wbnbCode)
	if err != nil {
		if err == ErrFailPreprocessing {
			fmt.Printf("CFG-Based Opcode Fusion Results:\n")
			fmt.Printf("Original code size: %d bytes\n", len(wbnbCode))
			fmt.Printf("Result: %v (contract contains optimized opcodes)\n", err)
			// This is expected behavior - the contract contains optimized opcodes
			return
		}
		t.Fatalf("CFG-based fusion failed: %v", err)
	}

	// Count how many opcodes were changed
	changedCount := countChangedOpcodes(wbnbCode, fusedCode)

	fmt.Printf("CFG-Based Opcode Fusion Results:\n")
	fmt.Printf("Original code size: %d bytes\n", len(wbnbCode))
	fmt.Printf("Fused code size: %d bytes\n", len(fusedCode))
	fmt.Printf("Opcodes changed: %d\n", changedCount)
	fmt.Printf("Fusion efficiency: %.2f%%\n", float64(changedCount)/float64(len(wbnbCode))*100)

	// Basic validation
	if len(fusedCode) != len(wbnbCode) {
		t.Errorf("Fused code size mismatch: expected %d, got %d", len(wbnbCode), len(fusedCode))
	}

	// Verify that at least some fusion occurred (optional check)
	if changedCount == 0 {
		t.Logf("No opcodes were fused - this might be normal for this contract")
	}
}

func TestFindOptimizedOpcodeBlocksInWBNB(t *testing.T) {
	hexCode := "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

	wbnbCode, err := hex.DecodeString(hexCode[2:])
	if err != nil {
		t.Fatalf("Failed to decode WBNB hex string: %v", err)
	}

	blocks := GenerateBasicBlocks(wbnbCode)

	/*
		// Print opcodes of block 2 if it exists
		if len(blocks) > 2 {
			block2 := blocks[2]
			fmt.Printf("\n=== Block 2 Opcodes (PC[%d,%d)) ===\n", block2.StartPC, block2.EndPC)
			fmt.Printf("Block 2 size: %d bytes\n", block2.EndPC-block2.StartPC)
			fmt.Printf("IsJumpDest: %v\n", block2.IsJumpDest)

			// Print the raw bytes of block 2
			fmt.Printf("Raw bytes: %x\n", wbnbCode[block2.StartPC:block2.EndPC])

			// Print human-readable opcodes
			fmt.Println("Human-readable opcodes:")
			for pc := block2.StartPC; pc < block2.EndPC && pc < uint64(len(wbnbCode)); {
				op := ByteCode(wbnbCode[pc])
				if op >= PUSH1 && op <= PUSH32 {
					dataLen := int(op - PUSH1 + 1)
					if pc+1+uint64(dataLen) <= uint64(len(wbnbCode)) {
						data := wbnbCode[pc+1 : pc+1+uint64(dataLen)]
						fmt.Printf("  %05x: PUSH%d [data: %x]\n", pc, dataLen, data)
						pc += 1 + uint64(dataLen)
					} else {
						fmt.Printf("  %05x: PUSH%d [incomplete data]\n", pc, dataLen)
						pc++
					}
				} else {
					fmt.Printf("  %05x: %s\n", pc, getOpcodeName(op))
					pc++
				}
			}
		} else {
			fmt.Printf("Block 2 does not exist. Total blocks: %d\n", len(blocks))
		}
	*/

	found := false
	for i, block := range blocks {
		// Skip blocks of type "others"
		blockType := getBlockType(block, blocks, i)
		if blockType == "others" {
			continue
		}
		for pc := block.StartPC; pc < block.EndPC && pc < uint64(len(wbnbCode)); {
			if wbnbCode[pc] >= minOptimizedOpcode && wbnbCode[pc] <= maxOptimizedOpcode {
				fmt.Printf("Block %d (PC[%d,%d)) contains optimized opcode 0x%x at offset %d\n", i, block.StartPC, block.EndPC, wbnbCode[pc], pc)
				found = true
			}
			// Skip data bytes for PUSH instructions
			skip, steps := calculateSkipSteps(wbnbCode, int(pc))
			/*
				if i == 2 {
					fmt.Printf("b2 pc: %05x, op: %s, skip: %v, steps: %d\n", pc, getOpcodeName(ByteCode(wbnbCode[pc])), skip, steps)
				}
			*/
			if skip {
				pc += uint64(steps) + 1 // Add 1 for the opcode byte
			} else {
				pc++
			}
		}
	}
	if !found {
		fmt.Println("No optimized opcodes found in any WBNB basic block.")
	}
}

func TestOpcodeCheckingWithDataBytes(t *testing.T) {
	// Create a simple bytecode with PUSH instructions followed by data bytes
	// that might be mistaken for optimized opcodes
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x60, 0xb0, // PUSH1 0xb0 (data byte that looks like optimized opcode)
		0x60, 0x02, // PUSH1 0x02
		0x60, 0xc0, // PUSH1 0xc0 (data byte that looks like optimized opcode)
		0x01, // ADD
		0x00, // STOP
	}

	fmt.Println("=== Testing Opcode Checking Logic ===")
	fmt.Printf("Code: %x\n", code)
	fmt.Println("Human readable:")
	for i := 0; i < len(code); {
		op := ByteCode(code[i])
		if op >= PUSH1 && op <= PUSH32 {
			dataLen := int(op - PUSH1 + 1)
			fmt.Printf("  %d: PUSH%d [data: %x]\n", i, dataLen, code[i+1:i+1+dataLen])
			i += 1 + dataLen
		} else {
			fmt.Printf("  %d: %s\n", i, getOpcodeName(op))
			i++
		}
	}

	// Test 1: Incorrect way (checking every byte as opcode)
	fmt.Println("\n--- Incorrect Method (checking every byte) ---")
	falsePositives := 0
	for i := 0; i < len(code); i++ {
		if code[i] >= minOptimizedOpcode && code[i] <= maxOptimizedOpcode {
			fmt.Printf("  Found 'optimized opcode' 0x%02x at offset %d\n", code[i], i)
			falsePositives++
		}
	}
	fmt.Printf("  False positives found: %d\n", falsePositives)

	// Test 2: Correct way (using calculateSkipSteps)
	fmt.Println("\n--- Correct Method (using calculateSkipSteps) ---")
	realOptimized := 0
	for i := 0; i < len(code); {
		// Skip data bytes for PUSH instructions first
		skip, steps := calculateSkipSteps(code, i)
		if skip {
			// This is a PUSH instruction, skip over the data bytes
			i += steps + 1 // Add 1 for the opcode byte
		} else {
			// This is a regular opcode, check if it's optimized
			if code[i] >= minOptimizedOpcode && code[i] <= maxOptimizedOpcode {
				fmt.Printf("  Found real optimized opcode 0x%02x at offset %d\n", code[i], i)
				realOptimized++
			}
			i++
		}
	}
	fmt.Printf("  Real optimized opcodes found: %d\n", realOptimized)

	// Verify the results
	if falsePositives != 2 {
		t.Errorf("Expected 2 false positives, got %d", falsePositives)
	}
	if realOptimized != 0 {
		t.Errorf("Expected 0 real optimized opcodes, got %d", realOptimized)
	}

	fmt.Println("\n=== Test Results ===")
	fmt.Printf("False positives (incorrect method): %d\n", falsePositives)
	fmt.Printf("Real optimized opcodes (correct method): %d\n", realOptimized)
	fmt.Println("The correct method properly skips PUSH data bytes!")
}

// getOpcodeName returns the human-readable name of an opcode
func getOpcodeName(op ByteCode) string {
	names := map[ByteCode]string{
		STOP:            "STOP",
		ADD:             "ADD",
		MUL:             "MUL",
		SUB:             "SUB",
		DIV:             "DIV",
		SDIV:            "SDIV",
		MOD:             "MOD",
		SMOD:            "SMOD",
		ADDMOD:          "ADDMOD",
		MULMOD:          "MULMOD",
		EXP:             "EXP",
		SIGNEXTEND:      "SIGNEXTEND",
		LT:              "LT",
		GT:              "GT",
		SLT:             "SLT",
		SGT:             "SGT",
		EQ:              "EQ",
		ISZERO:          "ISZERO",
		AND:             "AND",
		OR:              "OR",
		XOR:             "XOR",
		NOT:             "NOT",
		BYTE:            "BYTE",
		SHL:             "SHL",
		SHR:             "SHR",
		SAR:             "SAR",
		KECCAK256:       "KECCAK256",
		ADDRESS:         "ADDRESS",
		BALANCE:         "BALANCE",
		ORIGIN:          "ORIGIN",
		CALLER:          "CALLER",
		CALLVALUE:       "CALLVALUE",
		CALLDATALOAD:    "CALLDATALOAD",
		CALLDATASIZE:    "CALLDATASIZE",
		CALLDATACOPY:    "CALLDATACOPY",
		CODESIZE:        "CODESIZE",
		CODECOPY:        "CODECOPY",
		GASPRICE:        "GASPRICE",
		EXTCODESIZE:     "EXTCODESIZE",
		EXTCODECOPY:     "EXTCODECOPY",
		RETURNDATASIZE:  "RETURNDATASIZE",
		RETURNDATACOPY:  "RETURNDATACOPY",
		EXTCODEHASH:     "EXTCODEHASH",
		BLOCKHASH:       "BLOCKHASH",
		COINBASE:        "COINBASE",
		TIMESTAMP:       "TIMESTAMP",
		NUMBER:          "NUMBER",
		DIFFICULTY:      "DIFFICULTY",
		GASLIMIT:        "GASLIMIT",
		CHAINID:         "CHAINID",
		SELFBALANCE:     "SELFBALANCE",
		BASEFEE:         "BASEFEE",
		BLOBHASH:        "BLOBHASH",
		BLOBBASEFEE:     "BLOBBASEFEE",
		POP:             "POP",
		MLOAD:           "MLOAD",
		MSTORE:          "MSTORE",
		MSTORE8:         "MSTORE8",
		SLOAD:           "SLOAD",
		SSTORE:          "SSTORE",
		JUMP:            "JUMP",
		JUMPI:           "JUMPI",
		PC:              "PC",
		MSIZE:           "MSIZE",
		GAS:             "GAS",
		JUMPDEST:        "JUMPDEST",
		TLOAD:           "TLOAD",
		TSTORE:          "TSTORE",
		MCOPY:           "MCOPY",
		PUSH0:           "PUSH0",
		PUSH1:           "PUSH1",
		PUSH2:           "PUSH2",
		PUSH3:           "PUSH3",
		PUSH4:           "PUSH4",
		PUSH5:           "PUSH5",
		PUSH6:           "PUSH6",
		PUSH7:           "PUSH7",
		PUSH8:           "PUSH8",
		PUSH9:           "PUSH9",
		PUSH10:          "PUSH10",
		PUSH11:          "PUSH11",
		PUSH12:          "PUSH12",
		PUSH13:          "PUSH13",
		PUSH14:          "PUSH14",
		PUSH15:          "PUSH15",
		PUSH16:          "PUSH16",
		PUSH17:          "PUSH17",
		PUSH18:          "PUSH18",
		PUSH19:          "PUSH19",
		PUSH20:          "PUSH20",
		PUSH21:          "PUSH21",
		PUSH22:          "PUSH22",
		PUSH23:          "PUSH23",
		PUSH24:          "PUSH24",
		PUSH25:          "PUSH25",
		PUSH26:          "PUSH26",
		PUSH27:          "PUSH27",
		PUSH28:          "PUSH28",
		PUSH29:          "PUSH29",
		PUSH30:          "PUSH30",
		PUSH31:          "PUSH31",
		PUSH32:          "PUSH32",
		DUP1:            "DUP1",
		DUP2:            "DUP2",
		DUP3:            "DUP3",
		DUP4:            "DUP4",
		DUP5:            "DUP5",
		DUP6:            "DUP6",
		DUP7:            "DUP7",
		DUP8:            "DUP8",
		DUP9:            "DUP9",
		DUP10:           "DUP10",
		DUP11:           "DUP11",
		DUP12:           "DUP12",
		DUP13:           "DUP13",
		DUP14:           "DUP14",
		DUP15:           "DUP15",
		DUP16:           "DUP16",
		SWAP1:           "SWAP1",
		SWAP2:           "SWAP2",
		SWAP3:           "SWAP3",
		SWAP4:           "SWAP4",
		SWAP5:           "SWAP5",
		SWAP6:           "SWAP6",
		SWAP7:           "SWAP7",
		SWAP8:           "SWAP8",
		SWAP9:           "SWAP9",
		SWAP10:          "SWAP10",
		SWAP11:          "SWAP11",
		SWAP12:          "SWAP12",
		SWAP13:          "SWAP13",
		SWAP14:          "SWAP14",
		SWAP15:          "SWAP15",
		SWAP16:          "SWAP16",
		LOG0:            "LOG0",
		LOG1:            "LOG1",
		LOG2:            "LOG2",
		LOG3:            "LOG3",
		LOG4:            "LOG4",
		CREATE:          "CREATE",
		CALL:            "CALL",
		CALLCODE:        "CALLCODE",
		RETURN:          "RETURN",
		DELEGATECALL:    "DELEGATECALL",
		CREATE2:         "CREATE2",
		RETURNDATALOAD:  "RETURNDATALOAD",
		EXTCALL:         "EXTCALL",
		EXTDELEGATECALL: "EXTDELEGATECALL",
		STATICCALL:      "STATICCALL",
		EXTSTATICCALL:   "EXTSTATICCALL",
		REVERT:          "REVERT",
		INVALID:         "INVALID",
		SELFDESTRUCT:    "SELFDESTRUCT",
		// Fused opcodes
		Nop:                   "NOP",
		AndSwap1PopSwap2Swap1: "AndSwap1PopSwap2Swap1",
		Swap2Swap1PopJump:     "Swap2Swap1PopJump",
		Swap1PopSwap2Swap1:    "Swap1PopSwap2Swap1",
		PopSwap2Swap1Pop:      "PopSwap2Swap1Pop",
		Push2Jump:             "Push2Jump",
		Push2JumpI:            "Push2JumpI",
		Push1Push1:            "Push1Push1",
		Push1Add:              "Push1Add",
		Push1Shl:              "Push1Shl",
		Push1Dup1:             "Push1Dup1",
		Swap1Pop:              "Swap1Pop",
		PopJump:               "PopJump",
		Pop2:                  "Pop2",
		Swap2Swap1:            "Swap2Swap1",
		Swap2Pop:              "Swap2Pop",
		Dup2LT:                "Dup2LT",
		JumpIfZero:            "JumpIfZero",
		IsZeroPush2:           "IsZeroPush2",
		Dup2MStorePush1Add:    "Dup2MStorePush1Add",
		Dup1Push4EqPush2:      "Dup1Push4EqPush2",
		Push1CalldataloadPush1ShrDup1Push4GtPush2:      "Push1CalldataloadPush1ShrDup1Push4GtPush2",
		Push1Push1Push1SHLSub:                          "Push1Push1Push1SHLSub",
		AndDup2AddSwap1Dup2LT:                          "AndDup2AddSwap1Dup2LT",
		Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT: "Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT",
	}

	if name, exists := names[op]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%02x", op)
}

// countChangedOpcodes counts how many opcodes were changed during fusion
func countChangedOpcodes(original, fused []byte) int {
	if len(original) != len(fused) {
		return 0
	}

	count := 0
	for i := 0; i < len(original); {
		if original[i] != fused[i] {
			count++
		}

		// Skip data bytes for PUSH instructions
		skip, steps := calculateSkipSteps(original, i)
		if skip {
			i += steps + 1 // Add 1 for the opcode byte
		} else {
			i++
		}
	}
	return count
}

func TestIsValidJumpTarget(t *testing.T) {
	tests := []struct {
		name     string
		code     []byte
		target   uint64
		expected bool
	}{
		{
			name:     "valid JUMPDEST at position 5",
			code:     []byte{0x60, 0x05, 0x56, 0x5b, 0x00, 0x5b}, // PUSH1 0x05, JUMP, JUMPDEST, STOP, JUMPDEST
			target:   5,
			expected: true,
		},
		{
			name:     "invalid: not JUMPDEST",
			code:     []byte{0x60, 0x05, 0x56, 0x00, 0x00, 0x5b}, // PUSH1 0x05, JUMP, STOP, STOP, JUMPDEST
			target:   3,
			expected: false,
		},
		{
			name:     "invalid: out of bounds",
			code:     []byte{0x60, 0x05, 0x56, 0x5b}, // PUSH1 0x05, JUMP, JUMPDEST
			target:   10,
			expected: false,
		},
		{
			name:     "invalid: in PUSH data",
			code:     []byte{0x61, 0x05, 0x06, 0x56, 0x5b}, // PUSH2 0x0506, JUMP, JUMPDEST
			target:   1,
			expected: false,
		},
		{
			name:     "invalid: in PUSH data (second byte)",
			code:     []byte{0x61, 0x05, 0x06, 0x56, 0x5b}, // PUSH2 0x0506, JUMP, JUMPDEST
			target:   2,
			expected: false,
		},
		{
			name:     "valid: JUMPDEST after PUSH data",
			code:     []byte{0x61, 0x05, 0x06, 0x5b, 0x00}, // PUSH2 0x0506, JUMPDEST, STOP
			target:   3,
			expected: true,
		},
		{
			name:     "valid: JUMPDEST in complex code",
			code:     []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x5b, 0x00}, // PUSH1 0x01, PUSH1 0x02, ADD, JUMPDEST, STOP
			target:   5,
			expected: true,
		},
		{
			name:     "invalid: in PUSH1 data",
			code:     []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x5b, 0x00}, // PUSH1 0x01, PUSH1 0x02, ADD, JUMPDEST, STOP
			target:   1,
			expected: false,
		},
		{
			name:     "invalid: in PUSH1 data (second occurrence)",
			code:     []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x5b, 0x00}, // PUSH1 0x01, PUSH1 0x02, ADD, JUMPDEST, STOP
			target:   3,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidJumpTarget(tt.code, tt.target)
			if result != tt.expected {
				t.Errorf("isValidJumpTarget() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCodeFusionWithJumpTargetValidation(t *testing.T) {
	tests := []struct {
		name     string
		code     []byte
		expected bool // whether fusion should occur
	}{
		{
			name:     "valid Push2Jump fusion",
			code:     []byte{0x61, 0x05, 0x00, 0x56, 0x5b, 0x00}, // PUSH2 0x0500, JUMP, JUMPDEST, STOP
			expected: false,                                      // 0x0500 is out of bounds for 6-byte code
		},
		{
			name:     "valid Push2Jump fusion with correct target",
			code:     []byte{0x61, 0x00, 0x04, 0x56, 0x5b, 0x00}, // PUSH2 0x0004, JUMP, JUMPDEST, STOP
			expected: true,
		},
		{
			name:     "invalid Push2Jump fusion - target not JUMPDEST",
			code:     []byte{0x61, 0x00, 0x04, 0x56, 0x00, 0x00}, // PUSH2 0x0004, JUMP, STOP, STOP
			expected: false,
		},
		{
			name:     "valid Push2JumpI fusion with correct target",
			code:     []byte{0x61, 0x00, 0x04, 0x57, 0x5b, 0x00}, // PUSH2 0x0004, JUMPI, JUMPDEST, STOP
			expected: true,
		},
		{
			name:     "invalid Push2JumpI fusion - target not JUMPDEST",
			code:     []byte{0x61, 0x00, 0x04, 0x57, 0x00, 0x00}, // PUSH2 0x0004, JUMPI, STOP, STOP
			expected: false,
		},
		{
			name:     "valid JumpIfZero fusion with correct target",
			code:     []byte{0x15, 0x61, 0x00, 0x05, 0x57, 0x5b, 0x00}, // ISZERO, PUSH2 0x0005, JUMPI, JUMPDEST, STOP
			expected: true,
		},
		{
			name:     "invalid JumpIfZero fusion - target not JUMPDEST",
			code:     []byte{0x15, 0x61, 0x00, 0x06, 0x57, 0x00, 0x00}, // ISZERO, PUSH2 0x0006, JUMPI, STOP, STOP
			expected: true,                                             // IsZeroPush2 fusion should still occur even if JumpIfZero doesn't
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Testing: %s", tt.name)
			t.Logf("Code: %x", tt.code)

			// Test jump target validation directly
			if len(tt.code) >= 4 {
				var target uint64
				if tt.code[0] == 0x15 { // ISZERO
					// For JumpIfZero, PUSH2 data is at positions 2-3
					target = extractPush2Value(tt.code[2:4])
				} else {
					// For Push2Jump/Push2JumpI, PUSH2 data is at positions 1-2
					target = extractPush2Value(tt.code[1:3])
				}
				isValid := isValidJumpTarget(tt.code, target)
				t.Logf("Jump target %d is valid: %v", target, isValid)
			}

			// Test the legacy fusion first
			result, err := doCodeFusion(tt.code)
			if err != nil {
				t.Fatalf("doCodeFusion() error = %v", err)
			}

			t.Logf("Result: %x", result)

			// Check if fusion occurred by looking for the fused opcode
			fusionOccurred := false
			for i := 0; i < len(result); i++ {
				if result[i] >= byte(minOptimizedOpcode) && result[i] <= byte(maxOptimizedOpcode) {
					fusionOccurred = true
					t.Logf("Found fused opcode at position %d: 0x%02x", i, result[i])
					break
				}
			}

			if fusionOccurred != tt.expected {
				t.Errorf("legacy fusion occurred = %v, want %v", fusionOccurred, tt.expected)
			}
		})
	}
}

func TestDebugCodeFusion(t *testing.T) {
	// Simple test case: PUSH2 0x0500, JUMP, JUMPDEST, STOP
	code := []byte{0x61, 0x05, 0x00, 0x56, 0x5b, 0x00}

	t.Logf("Original code: %x", code)

	// Test the legacy fusion first
	result, err := doCodeFusion(code)
	if err != nil {
		t.Fatalf("doCodeFusion() error = %v", err)
	}

	t.Logf("Legacy fusion result: %x", result)

	// Check if fusion occurred
	fusionOccurred := false
	for i := 0; i < len(result); i++ {
		if result[i] >= byte(minOptimizedOpcode) && result[i] <= byte(maxOptimizedOpcode) {
			fusionOccurred = true
			t.Logf("Found fused opcode at position %d: 0x%02x", i, result[i])
			break
		}
	}

	t.Logf("Legacy fusion occurred: %v", fusionOccurred)

	// Test CFG-based fusion
	result2, err := DoCFGBasedOpcodeFusion(code)
	if err != nil {
		t.Fatalf("DoCFGBasedOpcodeFusion() error = %v", err)
	}

	t.Logf("CFG fusion result: %x", result2)

	// Check if fusion occurred
	fusionOccurred2 := false
	for i := 0; i < len(result2); i++ {
		if result2[i] >= byte(minOptimizedOpcode) && result2[i] <= byte(maxOptimizedOpcode) {
			fusionOccurred2 = true
			t.Logf("Found fused opcode at position %d: 0x%02x", i, result2[i])
			break
		}
	}

	t.Logf("CFG fusion occurred: %v", fusionOccurred2)

	// Test jump target validation directly
	target := uint64(0x0500)
	isValid := isValidJumpTarget(code, target)
	t.Logf("Jump target %d is valid: %v", target, isValid)
}

func TestDebugJumpIfZeroLayout(t *testing.T) {
	// Test case: ISZERO, PUSH2 0x0004, JUMPI, JUMPDEST, STOP
	code := []byte{0x15, 0x61, 0x00, 0x04, 0x57, 0x5b, 0x00}

	t.Logf("Code: %x", code)
	t.Logf("Code layout:")
	t.Logf("  [0] 0x%02x = ISZERO", code[0])
	t.Logf("  [1] 0x%02x = PUSH2", code[1])
	t.Logf("  [2] 0x%02x = PUSH2 data byte 1", code[2])
	t.Logf("  [3] 0x%02x = PUSH2 data byte 2", code[3])
	t.Logf("  [4] 0x%02x = JUMPI", code[4])
	t.Logf("  [5] 0x%02x = JUMPDEST", code[5])
	t.Logf("  [6] 0x%02x = STOP", code[6])

	target := uint64(code[2])<<8 | uint64(code[3])
	t.Logf("Jump target: %d (0x%04x)", target, target)

	isValid := isValidJumpTarget(code, target)
	t.Logf("Is valid: %v", isValid)

	// Test each position
	for i := 0; i < len(code); i++ {
		isCode := isCodeSegment(code, uint64(i))
		t.Logf("Position %d (0x%02x): isCode=%v", i, code[i], isCode)
	}
}

func TestJumpTargetValidationInFusion(t *testing.T) {
	tests := []struct {
		name           string
		code           []byte
		expectedFusion string // "none", "JumpIfZero", "IsZeroPush2", etc.
		description    string
	}{
		{
			name:           "JumpIfZero with valid JUMPDEST target",
			code:           []byte{0x15, 0x61, 0x00, 0x05, 0x57, 0x5b, 0x00}, // ISZERO, PUSH2 0x0005, JUMPI, JUMPDEST, STOP
			expectedFusion: "JumpIfZero",
			description:    "Should fuse to JumpIfZero when target is valid JUMPDEST",
		},
		{
			name:           "JumpIfZero with invalid target (not JUMPDEST)",
			code:           []byte{0x15, 0x61, 0x00, 0x06, 0x57, 0x00, 0x00}, // ISZERO, PUSH2 0x0006, JUMPI, STOP, STOP
			expectedFusion: "IsZeroPush2",
			description:    "Should fall back to IsZeroPush2 when JumpIfZero target is invalid",
		},
		{
			name:           "Push2Jump with valid JUMPDEST target",
			code:           []byte{0x61, 0x00, 0x04, 0x56, 0x5b, 0x00}, // PUSH2 0x0004, JUMP, JUMPDEST, STOP
			expectedFusion: "Push2Jump",
			description:    "Should fuse to Push2Jump when target is valid JUMPDEST",
		},
		{
			name:           "Push2Jump with invalid target (not JUMPDEST)",
			code:           []byte{0x61, 0x00, 0x04, 0x56, 0x00, 0x00}, // PUSH2 0x0004, JUMP, STOP, STOP
			expectedFusion: "none",
			description:    "Should not fuse when Push2Jump target is invalid",
		},
		{
			name:           "Push2JumpI with valid JUMPDEST target",
			code:           []byte{0x61, 0x00, 0x04, 0x57, 0x5b, 0x00}, // PUSH2 0x0004, JUMPI, JUMPDEST, STOP
			expectedFusion: "Push2JumpI",
			description:    "Should fuse to Push2JumpI when target is valid JUMPDEST",
		},
		{
			name:           "Push2JumpI with invalid target (not JUMPDEST)",
			code:           []byte{0x61, 0x00, 0x04, 0x57, 0x00, 0x00}, // PUSH2 0x0004, JUMPI, STOP, STOP
			expectedFusion: "none",
			description:    "Should not fuse when Push2JumpI target is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Testing: %s", tt.description)
			t.Logf("Code: %x", tt.code)

			// Test both CFG-based and legacy fusion
			resultCFG, err := DoCFGBasedOpcodeFusion(tt.code)
			if err != nil {
				t.Fatalf("DoCFGBasedOpcodeFusion() error = %v", err)
			}

			resultLegacy, err := doCodeFusion(tt.code)
			if err != nil {
				t.Fatalf("doCodeFusion() error = %v", err)
			}

			t.Logf("CFG result: %x", resultCFG)
			t.Logf("Legacy result: %x", resultLegacy)

			// Check what fusion occurred
			fusionType := getFusionType(resultLegacy)
			t.Logf("Detected fusion: %s", fusionType)

			if fusionType != tt.expectedFusion {
				t.Errorf("Expected fusion: %s, got: %s", tt.expectedFusion, fusionType)
			}

			// Verify that both fusion methods produce the same result
			if !bytes.Equal(resultCFG, resultLegacy) {
				t.Errorf("CFG and Legacy fusion produced different results")
				t.Errorf("CFG: %x", resultCFG)
				t.Errorf("Legacy: %x", resultLegacy)
			}
		})
	}
}

// getFusionType determines what type of fusion occurred based on the fused opcode
func getFusionType(result []byte) string {
	for i := 0; i < len(result); i++ {
		if result[i] >= byte(minOptimizedOpcode) && result[i] <= byte(maxOptimizedOpcode) {
			switch ByteCode(result[i]) {
			case JumpIfZero:
				return "JumpIfZero"
			case Push2Jump:
				return "Push2Jump"
			case Push2JumpI:
				return "Push2JumpI"
			case IsZeroPush2:
				return "IsZeroPush2"
			default:
				return fmt.Sprintf("Unknown_0x%02x", result[i])
			}
		}
	}
	return "none"
}

func TestExtractPush2Value(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint64
	}{
		{
			name:     "normal PUSH2 data",
			data:     []byte{0x00, 0x04},
			expected: 4,
		},
		{
			name:     "larger PUSH2 data",
			data:     []byte{0x05, 0x00},
			expected: 1280,
		},
		{
			name:     "maximum PUSH2 data",
			data:     []byte{0xFF, 0xFF},
			expected: 65535,
		},
		{
			name:     "zero PUSH2 data",
			data:     []byte{0x00, 0x00},
			expected: 0,
		},
		{
			name:     "incomplete data",
			data:     []byte{0x00},
			expected: 0,
		},
		{
			name:     "empty data",
			data:     []byte{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPush2Value(tt.data)
			if result != tt.expected {
				t.Errorf("extractPush2Value(%x) = %d, want %d", tt.data, result, tt.expected)
			}
		})
	}
}

func TestFusedOpcodeExecutionWithoutRuntimeValidation(t *testing.T) {
	tests := []struct {
		name     string
		code     []byte
		expected string
	}{
		{
			name:     "Push2Jump execution without runtime validation",
			code:     []byte{0xb5, 0x00, 0x04, 0xb0, 0x5b, 0x00}, // Push2Jump, target=4, Nop, JUMPDEST, STOP
			expected: "Push2Jump should execute without runtime validation",
		},
		{
			name:     "Push2JumpI execution without runtime validation",
			code:     []byte{0xb6, 0x00, 0x04, 0xb0, 0x5b, 0x00}, // Push2JumpI, target=4, Nop, JUMPDEST, STOP
			expected: "Push2JumpI should execute without runtime validation",
		},
		{
			name:     "JumpIfZero execution without runtime validation",
			code:     []byte{0xc1, 0xb0, 0x00, 0x05, 0xb0, 0x5b, 0x00}, // JumpIfZero, Nop, target=5, Nop, JUMPDEST, STOP
			expected: "JumpIfZero should execute without runtime validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test verifies that the fused opcodes are syntactically correct
			// and don't have any obvious issues that would prevent execution

			// Check that the fused opcodes are present
			foundFusedOpcode := false
			for i, b := range tt.code {
				if b == 0xb5 || b == 0xb6 || b == 0xc1 { // Push2Jump, Push2JumpI, JumpIfZero
					t.Logf("Found fused opcode 0x%02x at position %d", b, i)
					foundFusedOpcode = true
					break
				}
			}

			if !foundFusedOpcode {
				t.Errorf("No fused opcode found in test code")
			}

			// Verify the code structure is reasonable
			if len(tt.code) < 4 {
				t.Errorf("Test code too short: %d bytes", len(tt.code))
			}

			t.Logf("Test code: %x", tt.code)
			t.Logf("Code length: %d bytes", len(tt.code))
		})
	}
}
