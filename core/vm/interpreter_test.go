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
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
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
		statedb.SetCode(address, common.Hex2Bytes(tt), tracing.CodeChangeUnspecified)
		statedb.Finalise(true)

		evm := NewEVM(vmctx, statedb, params.AllEthashProtocolChanges, Config{})

		errChannel := make(chan error)
		timeout := make(chan bool)

		go func(evm *EVM) {
			_, _, err := evm.Call(common.Address{}, address, nil, math.MaxUint64, new(uint256.Int))
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

func BenchmarkInterpreter(b *testing.B) {
	var (
		statedb, _        = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		evm               = NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: 1, Random: &common.Hash{}}, statedb, params.MergedTestChainConfig, Config{})
		startGas   uint64 = 100_000_000
		value             = uint256.NewInt(0)
		stack             = newstack()
		mem               = NewMemory()
		contract          = GetContract(common.Address{}, common.Address{}, value, startGas, nil)
	)
	stack.push(uint256.NewInt(123))
	stack.push(uint256.NewInt(123))
	gasSStoreEIP3529 = makeGasSStoreFunc(params.SstoreClearsScheduleRefundEIP3529)
	for b.Loop() {
		gasSStoreEIP3529(evm, contract, stack, mem, 1234)
	}
}

func TestSuperInstructionFallbackContinuesExecution(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: 1, Random: &common.Hash{}}, statedb, params.MergedTestChainConfig, Config{})
	interpreter := NewEVMInterpreter(evm)
	interpreter.CopyAndInstallSuperInstruction()
	interpreter.table[Push1Push1].constantGas = 10_000

	contract := GetContract(common.Address{}, common.Address{}, uint256.NewInt(0), 100, nil)
	contract.Code = []byte{
		byte(Push1Push1), 0x01, byte(Nop), 0x02,
		byte(ADD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract.SetOptimizedForTest()

	ret, err := interpreter.Run(contract, nil, false)
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes([]byte{0x03}, 32), ret)
}

func TestSuperInstructionFallbackContinuesTracing(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	var traced []OpCode
	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: 1, Random: &common.Hash{}}, statedb, params.MergedTestChainConfig, Config{
		Tracer: &tracing.Hooks{
			OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
				traced = append(traced, OpCode(op))
			},
		},
	})
	interpreter := NewEVMInterpreter(evm)
	interpreter.CopyAndInstallSuperInstruction()
	interpreter.table[Push1Push1].constantGas = 10_000

	contract := GetContract(common.Address{}, common.Address{}, uint256.NewInt(0), 100, nil)
	contract.Code = []byte{byte(Push1Push1), 0x01, byte(Nop), 0x02, byte(STOP)}
	contract.SetOptimizedForTest()

	_, err := interpreter.Run(contract, nil, false)
	require.NoError(t, err)
	require.Equal(t, []OpCode{PUSH1, PUSH1, STOP}, traced)
}

func TestSuperInstructionFallbackChargesVerkleCodeChunks(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	chainConfig := *params.MergedTestChainConfig
	chainConfig.PragueTime = nil
	chainConfig.OsakaTime = nil
	verkleTime := uint64(0)
	chainConfig.VerkleTime = &verkleTime

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: 1, Random: &common.Hash{}}, statedb, &chainConfig, Config{})
	evm.SetTxContext(TxContext{})

	interpreter := NewEVMInterpreter(evm)
	interpreter.CopyAndInstallSuperInstruction()
	interpreter.table[Push2Jump].constantGas = 10_000

	code := make([]byte, 33)
	code[0] = byte(JUMPDEST)
	code[1] = byte(PUSH1)
	code[2] = 0x1b
	code[3] = byte(JUMP)
	code[4] = byte(JUMPDEST)
	code[5] = byte(STOP)
	code[27] = byte(JUMPDEST)
	code[28] = byte(Push2Jump)
	code[29] = 0x00
	code[30] = 0x04
	code[31] = byte(Nop)
	code[32] = byte(STOP)

	contract := GetContract(common.Address{}, common.Address{}, uint256.NewInt(0), 5_000, nil)
	contract.Code = code
	contract.SetOptimizedForTest()

	_, err := interpreter.Run(contract, nil, false)
	require.NoError(t, err)
	require.Len(t, evm.TxContext.AccessEvents.Keys(), 2)
}
