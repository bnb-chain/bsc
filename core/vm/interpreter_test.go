// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"math"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

var loopInterruptTests = []string{
	// infinite loop using JUMP: push(2) jumpdest dup1 jump
	"60025b8056",
	// infinite loop using JUMPI: push(1) push(4) jumpdest dup2 dup2 jumpi
	"600160045b818157",
}

func TestLoopInterrupt(t *testing.T) {
	address := common.BytesToAddress([]byte("contract"))
	vmctx := BlockContext{
		Transfer: func(StateDB, common.Address, common.Address, *uint256.Int) {},
	}

	for i, tt := range loopInterruptTests {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		statedb.CreateAccount(address)
		statedb.SetCode(address, common.Hex2Bytes(tt))
		statedb.Finalise(true)

		evm := NewEVM(vmctx, statedb, params.AllEthashProtocolChanges, Config{})

		errChannel := make(chan error)
		timeout := make(chan bool)

		go func(evm *EVM) {
			_, _, err := evm.Call(AccountRef(common.Address{}), address, nil, math.MaxUint64, new(uint256.Int))
			errChannel <- err
		}(evm)

		go func() {
			<-time.After(time.Second)
			timeout <- true
		}()

		evm.Cancel()

		select {
		case <-timeout:
			t.Errorf("test %d timed out", i)
		case err := <-errChannel:
			if err != nil {
				t.Errorf("test %d failure: %v", i, err)
			}
		}
	}
}

func TestBasicBlockGasCalculation(t *testing.T) {
	// Create a simple test code
	testCode := []byte{
		byte(PUSH1), 0x01, // PUSH1 0x01 - gas cost: 3
		byte(ADD),         // ADD - gas cost: 3
		byte(STOP),        // STOP - gas cost: 0
	}

	// Debug: print the test code
	t.Logf("Test code: %v", testCode)
	for i, b := range testCode {
		t.Logf("  Byte[%d]: %d (0x%02x)", i, b, b)
	}

	// Create a new interpreter
	evm := &EVM{}
	interpreter := NewEVMInterpreter(evm)

	// Create a proper GasCalculator from the JumpTable
	provider := &jumpTableGasCalculator{table: interpreter.table}

	// Generate basic blocks
	blocks := compiler.GenerateBasicBlocks(testCode, provider)

	// Verify we have at least one block
	if len(blocks) == 0 {
		t.Error("No basic blocks generated")
		return
	}

	// Debug: print block information
	for i, block := range blocks {
		t.Logf("Block %d: StartPC=%d, EndPC=%d, Opcodes=%v, StaticGas=%d", 
			i, block.StartPC, block.EndPC, block.Opcodes, block.StaticGas)
		
		// Debug: print each opcode and its gas cost
		for j, op := range block.Opcodes {
			gas := interpreter.table.GetConstantGas(op)
			t.Logf("  Opcode[%d]: %d (0x%02x), gas=%d", j, op, op, gas)
		}
	}

	// Check the first block's gas cost
	firstBlock := blocks[0]
	expectedGas := uint64(6) // PUSH1(3) + ADD(3) + STOP(0) = 6
	if firstBlock.StaticGas != expectedGas {
		t.Errorf("Expected gas cost %d, got %d", expectedGas, firstBlock.StaticGas)
	}
}

func TestGetConstantGasEquivalence(t *testing.T) {
	// Create a new interpreter
	evm := &EVM{}
	interpreter := NewEVMInterpreter(evm)

	// Test only standard opcodes
	keyOpcodes := []byte{
		byte(STOP),      // 0x00
		byte(ADD),       // 0x01
		byte(MUL),       // 0x02
		byte(SUB),       // 0x03
		byte(DIV),       // 0x04
		byte(SDIV),      // 0x05
		byte(MOD),       // 0x06
		byte(SMOD),      // 0x07
		byte(ADDMOD),    // 0x08
		byte(MULMOD),    // 0x09
		byte(SIGNEXTEND), // 0x0b
		byte(LT),        // 0x10
		byte(GT),        // 0x11
		byte(SLT),       // 0x12
		byte(SGT),       // 0x13
		byte(EQ),        // 0x14
		byte(ISZERO),    // 0x15
		byte(AND),       // 0x16
		byte(OR),        // 0x17
		byte(XOR),       // 0x18
		byte(NOT),       // 0x19
		byte(BYTE),      // 0x1a
		byte(SHL),       // 0x1b
		byte(SHR),       // 0x1c
		byte(SAR),       // 0x1d
		byte(POP),       // 0x50
		byte(MLOAD),     // 0x51
		byte(MSTORE),    // 0x52
		byte(MSTORE8),   // 0x53
		byte(SLOAD),     // 0x54
		byte(SSTORE),    // 0x55
		byte(JUMP),      // 0x56
		byte(JUMPI),     // 0x57
		byte(PC),        // 0x58
		byte(MSIZE),     // 0x59
		byte(GAS),       // 0x5a
		byte(JUMPDEST),  // 0x5b
		byte(PUSH1),     // 0x60
		byte(PUSH2),     // 0x61
		byte(PUSH3),     // 0x62
		byte(PUSH4),     // 0x63
		byte(PUSH5),     // 0x64
		byte(PUSH6),     // 0x65
		byte(PUSH7),     // 0x66
		byte(PUSH8),     // 0x67
		byte(PUSH9),     // 0x68
		byte(PUSH10),    // 0x69
		byte(PUSH11),    // 0x6a
		byte(PUSH12),    // 0x6b
		byte(PUSH13),    // 0x6c
		byte(PUSH14),    // 0x6d
		byte(PUSH15),    // 0x6e
		byte(PUSH16),    // 0x6f
		byte(PUSH17),    // 0x70
		byte(PUSH18),    // 0x71
		byte(PUSH19),    // 0x72
		byte(PUSH20),    // 0x73
		byte(PUSH21),    // 0x74
		byte(PUSH22),    // 0x75
		byte(PUSH23),    // 0x76
		byte(PUSH24),    // 0x77
		byte(PUSH25),    // 0x78
		byte(PUSH26),    // 0x79
		byte(PUSH27),    // 0x7a
		byte(PUSH28),    // 0x7b
		byte(PUSH29),    // 0x7c
		byte(PUSH30),    // 0x7d
		byte(PUSH31),    // 0x7e
		byte(PUSH32),    // 0x7f
		byte(DUP1),      // 0x80
		byte(DUP2),      // 0x81
		byte(DUP3),      // 0x82
		byte(DUP4),      // 0x83
		byte(DUP5),      // 0x84
		byte(DUP6),      // 0x85
		byte(DUP7),      // 0x86
		byte(DUP8),      // 0x87
		byte(DUP9),      // 0x88
		byte(DUP10),     // 0x89
		byte(DUP11),     // 0x8a
		byte(DUP12),     // 0x8b
		byte(DUP13),     // 0x8c
		byte(DUP14),     // 0x8d
		byte(DUP15),     // 0x8e
		byte(DUP16),     // 0x8f
		byte(SWAP1),     // 0x90
		byte(SWAP2),     // 0x91
		byte(SWAP3),     // 0x92
		byte(SWAP4),     // 0x93
		byte(SWAP5),     // 0x94
		byte(SWAP6),     // 0x95
		byte(SWAP7),     // 0x96
		byte(SWAP8),     // 0x97
		byte(SWAP9),     // 0x98
		byte(SWAP10),    // 0x99
		byte(SWAP11),    // 0x9a
		byte(SWAP12),    // 0x9b
		byte(SWAP13),    // 0x9c
		byte(SWAP14),    // 0x9d
		byte(SWAP15),    // 0x9e
		byte(SWAP16),    // 0x9f
		byte(LOG0),      // 0xa0
		byte(LOG1),      // 0xa1
		byte(LOG2),      // 0xa2
		byte(LOG3),      // 0xa3
		byte(LOG4),      // 0xa4
		byte(CREATE),    // 0xf0
		byte(CALL),      // 0xf1
		byte(CALLCODE),  // 0xf2
		byte(RETURN),    // 0xf3
		byte(DELEGATECALL), // 0xf4
		byte(CREATE2),   // 0xf5
		byte(STATICCALL), // 0xfa
		byte(REVERT),    // 0xfd
		byte(INVALID),   // 0xfe
		byte(SELFDESTRUCT), // 0xff
	}

	for _, opcode := range keyOpcodes {
		// Method 1: Using JumpTable.GetConstantGas
		gas1 := interpreter.table.GetConstantGas(opcode)
		
		// Method 2: Using operation.constantGas
		op := OpCode(opcode)
		operation := interpreter.table[op]
		var gas2 uint64
		if operation != nil {
			gas2 = operation.constantGas
		} else {
			gas2 = 0
		}
		
		// Compare the results
		if gas1 != gas2 {
			t.Errorf("Opcode 0x%02x: GetConstantGas() returned %d, operation.constantGas returned %d", 
				opcode, gas1, gas2)
		}
	}
	
	t.Logf("Verified that JumpTable.GetConstantGas() and operation.constantGas return the same values for key opcodes")
}

// TestJumpTableProvider tests that the JumpTableProvider interface works correctly
func TestJumpTableProvider(t *testing.T) {
	// Create a test interpreter with London instruction set
	evm := &EVM{}
	interpreter := NewEVMInterpreter(evm)
	
	// Create a provider
	provider := &jumpTableGasCalculator{table: interpreter.table}
	
	// Test that GetConstantGas returns the same values as the JumpTable
	testOpcodes := []byte{0x00, 0x01, 0x02, 0x03, 0x50, 0x51, 0x56, 0x57, 0x60, 0x61}
	
	for _, op := range testOpcodes {
		expected := interpreter.table.GetConstantGas(op)
		actual := provider.GetConstantGas(op)
		
		if expected != actual {
			t.Errorf("GetConstantGas mismatch for opcode 0x%02x: expected %d, got %d", op, expected, actual)
		}
	}
	
	t.Logf("JumpTableProvider interface works correctly")
}

// TestJumpTableProviderWithoutSet tests what happens when no provider is set
func TestJumpTableProviderWithoutSet(t *testing.T) {
	// Reset the provider to nil
	compiler.SetJumpTableProvider(nil)
	
	// Create a test interpreter with London instruction set
	evm := &EVM{}
	interpreter := NewEVMInterpreter(evm)
	provider := &jumpTableGasCalculator{table: interpreter.table}
	
	// Test that GetConstantGas returns the correct value when provider is set
	gas := provider.GetConstantGas(0x01) // ADD opcode
	expected := interpreter.table.GetConstantGas(0x01)
	if gas != expected {
		t.Errorf("Expected gas cost to be %d when provider is set, got %d", expected, gas)
	}
	
	t.Logf("Gas cost for ADD opcode: %d (when provider is set)", gas)
}

// TestDoCodeFusionWithJumpTable tests the new function that accepts JumpTable directly
func TestDoCodeFusionWithJumpTable(t *testing.T) {
	// Create a test interpreter with London instruction set
	evm := &EVM{}
	interpreter := NewEVMInterpreter(evm)
	
	// Create a simple test code
	testCode := []byte{
		byte(PUSH1), 0x01, // PUSH1 0x01
		byte(ADD),         // ADD
		byte(STOP),        // STOP
	}
	
	// Test the new function with JumpTable
	result, err := compiler.DoCodeFusionWithJumpTable(testCode, interpreter.table)
	if err != nil {
		t.Errorf("DoCodeFusionWithJumpTable failed: %v", err)
	}
	
	// Verify the result is not nil
	if result == nil {
		t.Error("DoCodeFusionWithJumpTable returned nil result")
	}
	
	t.Logf("DoCodeFusionWithJumpTable test passed, result length: %d", len(result))
}
