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
	"github.com/ethereum/go-ethereum/internal/compiler"
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

	// Generate basic blocks
	blocks := compiler.GenerateBasicBlocks(testCode, interpreter.table)

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
