// Copyright 2015 The go-ethereum Authors
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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func opAdd(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Add(&x, y)
	return nil, nil
}

func opSub(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Sub(&x, y)
	return nil, nil
}

func opMul(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Mul(&x, y)
	return nil, nil
}

func opDiv(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Div(&x, y)
	return nil, nil
}

func opSdiv(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.SDiv(&x, y)
	return nil, nil
}

func opMod(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Mod(&x, y)
	return nil, nil
}

func opSmod(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.SMod(&x, y)
	return nil, nil
}

func opExp(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	base, exponent := scope.Stack.pop(), scope.Stack.peek()
	exponent.Exp(&base, exponent)
	return nil, nil
}

func opSignExtend(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	back, num := scope.Stack.pop(), scope.Stack.peek()
	num.ExtendSign(num, &back)
	return nil, nil
}

func opNot(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	x.Not(x)
	return nil, nil
}

func opLt(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Lt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}

func opGt(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Gt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}

func opSlt(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Slt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}

func opSgt(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Sgt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}

func opEq(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Eq(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}

func opIszero(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	if x.IsZero() {
		x.SetOne()
	} else {
		x.Clear()
	}
	return nil, nil
}

func opAnd(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.And(&x, y)
	return nil, nil
}

func opOr(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Or(&x, y)
	return nil, nil
}

func opXor(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.peek()
	y.Xor(&x, y)
	return nil, nil
}

func opByte(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	th, val := scope.Stack.pop(), scope.Stack.peek()
	val.Byte(&th)
	return nil, nil
}

func opAddmod(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop2()
	z := scope.Stack.peek()
	z.AddMod(&x, &y, z)
	return nil, nil
}

func opMulmod(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop2()
	z := scope.Stack.peek()
	z.MulMod(&x, &y, z)
	return nil, nil
}

// opSHL implements Shift Left
// The SHL instruction (shift left) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the left by arg1 number of bits.
func opSHL(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	// Note, second operand is left in the stack; accumulate result into it, and no need to push it afterwards
	shift, value := scope.Stack.pop(), scope.Stack.peek()
	if shift.LtUint64(256) {
		value.Lsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
	return nil, nil
}

// opSHR implements Logical Shift Right
// The SHR instruction (logical shift right) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the right by arg1 number of bits with zero fill.
func opSHR(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	// Note, second operand is left in the stack; accumulate result into it, and no need to push it afterwards
	shift, value := scope.Stack.pop(), scope.Stack.peek()
	if shift.LtUint64(256) {
		value.Rsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
	return nil, nil
}

// opSAR implements Arithmetic Shift Right
// The SAR instruction (arithmetic shift right) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the right by arg1 number of bits with sign extension.
func opSAR(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	shift, value := scope.Stack.pop(), scope.Stack.peek()
	if shift.GtUint64(256) {
		if value.Sign() >= 0 {
			value.Clear()
		} else {
			// Max negative shift: all bits set
			value.SetAllOne()
		}
		return nil, nil
	}
	n := uint(shift.Uint64())
	value.SRsh(value, n)
	return nil, nil
}

func opKeccak256(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	offset, size := scope.Stack.pop(), scope.Stack.peek()
	data := scope.Memory.GetPtr(offset.Uint64(), size.Uint64())

	interpreter.hasher.Reset()
	interpreter.hasher.Write(data)
	interpreter.hasher.Read(interpreter.hasherBuf[:])

	if interpreter.evm.Config.EnablePreimageRecording {
		interpreter.evm.StateDB.AddPreimage(interpreter.hasherBuf, data)
	}
	size.SetBytes(interpreter.hasherBuf[:])
	return nil, nil
}

func opAddress(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetBytes(scope.Contract.Address().Bytes()))
	return nil, nil
}

func opBalance(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	address := common.Address(slot.Bytes20())
	slot.Set(interpreter.evm.StateDB.GetBalance(address))
	return nil, nil
}

func opOrigin(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetBytes(interpreter.evm.Origin.Bytes()))
	return nil, nil
}

func opCaller(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetBytes(scope.Contract.Caller().Bytes()))
	return nil, nil
}

func opCallValue(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(scope.Contract.value)
	return nil, nil
}

func opCallDataLoad(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	if offset, overflow := x.Uint64WithOverflow(); !overflow {
		data := getData(scope.Contract.Input, offset, 32)
		x.SetBytes(data)
	} else {
		x.Clear()
	}
	return nil, nil
}

func opCallDataSize(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(uint64(len(scope.Contract.Input))))
	return nil, nil
}

func opCallDataCopy(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		memOffset, dataOffset = scope.Stack.pop2()
		length                = scope.Stack.pop()
	)
	dataOffset64, overflow := dataOffset.Uint64WithOverflow()
	if overflow {
		dataOffset64 = math.MaxUint64
	}
	// These values are checked for overflow during gas cost calculation
	memOffset64 := memOffset.Uint64()
	length64 := length.Uint64()
	scope.Memory.Set(memOffset64, length64, getData(scope.Contract.Input, dataOffset64, length64))

	return nil, nil
}

func opReturnDataSize(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(uint64(len(interpreter.returnData))))
	return nil, nil
}

func opReturnDataCopy(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		memOffset, dataOffset = scope.Stack.pop2()
		length                = scope.Stack.pop()
	)

	offset64, overflow := dataOffset.Uint64WithOverflow()
	if overflow {
		return nil, ErrReturnDataOutOfBounds
	}
	// we can reuse dataOffset now (aliasing it for clarity)
	var end = dataOffset
	end.Add(&dataOffset, &length)
	end64, overflow := end.Uint64WithOverflow()
	if overflow || uint64(len(interpreter.returnData)) < end64 {
		return nil, ErrReturnDataOutOfBounds
	}
	scope.Memory.Set(memOffset.Uint64(), length.Uint64(), interpreter.returnData[offset64:end64])
	return nil, nil
}

func opExtCodeSize(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	slot.SetUint64(uint64(interpreter.evm.StateDB.GetCodeSize(slot.Bytes20())))
	return nil, nil
}

func opCodeSize(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	code := scope.Contract.Code
	if scope.Contract.optimized {
		code = interpreter.evm.resolveCode(*scope.Contract.CodeAddr)
	}
	scope.Stack.push(new(uint256.Int).SetUint64(uint64(len(code))))
	return nil, nil
}

func opCodeCopy(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		memOffset, codeOffset = scope.Stack.pop2()
		length                = scope.Stack.pop()
	)
	uint64CodeOffset, overflow := codeOffset.Uint64WithOverflow()
	if overflow {
		uint64CodeOffset = math.MaxUint64
	}
	code := scope.Contract.Code
	if scope.Contract.optimized {
		code = interpreter.evm.resolveCode(*scope.Contract.CodeAddr)
	}
	codeCopy := getData(code, uint64CodeOffset, length.Uint64())
	scope.Memory.Set(memOffset.Uint64(), length.Uint64(), codeCopy)
	return nil, nil
}

func opExtCodeCopy(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		stack      = scope.Stack
		a          = stack.pop()
		memOffset  = stack.pop()
		codeOffset = stack.pop()
		length     = stack.pop()
	)
	uint64CodeOffset, overflow := codeOffset.Uint64WithOverflow()
	if overflow {
		uint64CodeOffset = math.MaxUint64
	}
	addr := common.Address(a.Bytes20())
	code := interpreter.evm.StateDB.GetCode(addr)
	codeCopy := getData(code, uint64CodeOffset, length.Uint64())
	scope.Memory.Set(memOffset.Uint64(), length.Uint64(), codeCopy)

	return nil, nil
}

// opExtCodeHash returns the code hash of a specified account.
// There are several cases when the function is called, while we can relay everything
// to `state.GetCodeHash` function to ensure the correctness.
//
//  1. Caller tries to get the code hash of a normal contract account, state
//     should return the relative code hash and set it as the result.
//
//  2. Caller tries to get the code hash of a non-existent account, state should
//     return common.Hash{} and zero will be set as the result.
//
//  3. Caller tries to get the code hash for an account without contract code, state
//     should return emptyCodeHash(0xc5d246...) as the result.
//
//  4. Caller tries to get the code hash of a precompiled account, the result should be
//     zero or emptyCodeHash.
//
// It is worth noting that in order to avoid unnecessary create and clean, all precompile
// accounts on mainnet have been transferred 1 wei, so the return here should be
// emptyCodeHash. If the precompile account is not transferred any amount on a private or
// customized chain, the return value will be zero.
//
//  5. Caller tries to get the code hash for an account which is marked as self-destructed
//     in the current transaction, the code hash of this account should be returned.
//
//  6. Caller tries to get the code hash for an account which is marked as deleted, this
//     account should be regarded as a non-existent account and zero should be returned.
func opExtCodeHash(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	address := common.Address(slot.Bytes20())
	if interpreter.evm.StateDB.Empty(address) {
		slot.Clear()
	} else {
		slot.SetBytes(interpreter.evm.StateDB.GetCodeHash(address).Bytes())
	}
	return nil, nil
}

func opGasprice(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	v, _ := uint256.FromBig(interpreter.evm.GasPrice)
	scope.Stack.push(v)
	return nil, nil
}

func opBlockhash(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	num := scope.Stack.peek()
	num64, overflow := num.Uint64WithOverflow()
	if overflow {
		num.Clear()
		return nil, nil
	}

	var upper, lower uint64
	upper = interpreter.evm.Context.BlockNumber.Uint64()
	if upper < 257 {
		lower = 0
	} else {
		lower = upper - 256
	}
	if num64 >= lower && num64 < upper {
		res := interpreter.evm.Context.GetHash(num64)
		if witness := interpreter.evm.StateDB.Witness(); witness != nil {
			witness.AddBlockHash(num64)
		}
		if tracer := interpreter.evm.Config.Tracer; tracer != nil && tracer.OnBlockHashRead != nil {
			tracer.OnBlockHashRead(num64, res)
		}
		num.SetBytes(res[:])
	} else {
		num.Clear()
	}
	return nil, nil
}

func opCoinbase(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetBytes(interpreter.evm.Context.Coinbase.Bytes()))
	return nil, nil
}

func opTimestamp(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(interpreter.evm.Context.Time))
	return nil, nil
}

func opNumber(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	v, _ := uint256.FromBig(interpreter.evm.Context.BlockNumber)
	scope.Stack.push(v)
	return nil, nil
}

func opDifficulty(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	v, _ := uint256.FromBig(interpreter.evm.Context.Difficulty)
	scope.Stack.push(v)
	return nil, nil
}

func opRandom(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	v := new(uint256.Int).SetBytes(interpreter.evm.Context.Random.Bytes())
	scope.Stack.push(v)
	return nil, nil
}

func opGasLimit(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(interpreter.evm.Context.GasLimit))
	return nil, nil
}

func opPop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.pop()
	return nil, nil
}

func opMload(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	v := scope.Stack.peek()
	offset := v.Uint64()
	v.SetBytes(scope.Memory.GetPtr(offset, 32))
	return nil, nil
}

func opMstore(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	mStart, val := scope.Stack.pop2()
	scope.Memory.Set32(mStart.Uint64(), &val)
	return nil, nil
}

func opMstore8(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	off, val := scope.Stack.pop2()
	scope.Memory.store[off.Uint64()] = byte(val.Uint64())
	return nil, nil
}

func opSload(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	loc := scope.Stack.peek()
	hash := common.Hash(loc.Bytes32())
	val := interpreter.evm.StateDB.GetState(scope.Contract.Address(), hash)
	loc.SetBytes(val.Bytes())
	return nil, nil
}

func opSstore(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.readOnly {
		return nil, ErrWriteProtection
	}
	loc, val := scope.Stack.pop2()
	interpreter.evm.StateDB.SetState(scope.Contract.Address(), loc.Bytes32(), val.Bytes32())
	return nil, nil
}

func opJump(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.evm.abort.Load() {
		return nil, errStopToken
	}
	pos := scope.Stack.pop()
	if !scope.Contract.validJumpdest(&pos) {
		return nil, ErrInvalidJump
	}
	*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	return nil, nil
}

func opJumpi(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.evm.abort.Load() {
		return nil, errStopToken
	}
	pos, cond := scope.Stack.pop2()
	if !cond.IsZero() {
		if !scope.Contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump
		}
		*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	}
	return nil, nil
}

func opJumpdest(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	return nil, nil
}

func opPc(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(*pc))
	return nil, nil
}

func opMsize(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(uint64(scope.Memory.Len())))
	return nil, nil
}

func opGas(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(new(uint256.Int).SetUint64(scope.Contract.Gas))
	return nil, nil
}

func opSwap1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap1()
	return nil, nil
}

func opSwap2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap2()
	return nil, nil
}

func opSwap3(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap3()
	return nil, nil
}

func opSwap4(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap4()
	return nil, nil
}

func opSwap5(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap5()
	return nil, nil
}

func opSwap6(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap6()
	return nil, nil
}

func opSwap7(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap7()
	return nil, nil
}

func opSwap8(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap8()
	return nil, nil
}

func opSwap9(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap9()
	return nil, nil
}

func opSwap10(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap10()
	return nil, nil
}

func opSwap11(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap11()
	return nil, nil
}

func opSwap12(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap12()
	return nil, nil
}

func opSwap13(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap13()
	return nil, nil
}

func opSwap14(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap14()
	return nil, nil
}

func opSwap15(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap15()
	return nil, nil
}

func opSwap16(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap16()
	return nil, nil
}

func opCreate(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.readOnly {
		return nil, ErrWriteProtection
	}
	var (
		value, offset = scope.Stack.pop2()
		size          = scope.Stack.pop()
		input         = scope.Memory.GetCopy(offset.Uint64(), size.Uint64())
		gas           = scope.Contract.Gas
	)
	if interpreter.evm.chainRules.IsEIP150 {
		gas -= gas / 64
	}

	// reuse size int for stackvalue
	stackvalue := size

	scope.Contract.UseGas(gas, interpreter.evm.Config.Tracer, tracing.GasChangeCallContractCreation)

	res, addr, returnGas, suberr := interpreter.evm.Create(scope.Contract.Address(), input, gas, &value)
	// Push item on the stack based on the returned error. If the ruleset is
	// homestead we must check for CodeStoreOutOfGasError (homestead only
	// rule) and treat as an error, if the ruleset is frontier we must
	// ignore this error and pretend the operation was successful.
	if interpreter.evm.chainRules.IsHomestead && suberr == ErrCodeStoreOutOfGas {
		stackvalue.Clear()
	} else if suberr != nil && suberr != ErrCodeStoreOutOfGas {
		stackvalue.Clear()
	} else {
		stackvalue.SetBytes(addr.Bytes())
	}
	scope.Stack.push(&stackvalue)

	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	if suberr == ErrExecutionReverted {
		interpreter.returnData = res // set REVERT data to return data buffer
		return res, nil
	}
	interpreter.returnData = nil // clear dirty return data buffer
	return nil, nil
}

func opCreate2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.readOnly {
		return nil, ErrWriteProtection
	}
	var (
		endowment, offset = scope.Stack.pop2()
		size, salt        = scope.Stack.pop2()
		input             = scope.Memory.GetCopy(offset.Uint64(), size.Uint64())
		gas               = scope.Contract.Gas
	)

	// Apply EIP150
	gas -= gas / 64
	scope.Contract.UseGas(gas, interpreter.evm.Config.Tracer, tracing.GasChangeCallContractCreation2)
	// reuse size int for stackvalue
	stackvalue := size
	res, addr, returnGas, suberr := interpreter.evm.Create2(scope.Contract.Address(), input, gas,
		&endowment, &salt)
	// Push item on the stack based on the returned error.
	if suberr != nil {
		stackvalue.Clear()
	} else {
		stackvalue.SetBytes(addr.Bytes())
	}
	scope.Stack.push(&stackvalue)
	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	if suberr == ErrExecutionReverted {
		interpreter.returnData = res // set REVERT data to return data buffer
		return res, nil
	}
	interpreter.returnData = nil // clear dirty return data buffer
	return nil, nil
}

func opCall(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	stack := scope.Stack
	// Pop gas. The actual gas in interpreter.evm.callGasTemp.
	// We can use this as a temporary value
	temp := stack.pop()
	gas := interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, value, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.Address(addr.Bytes20())
	// Get the arguments from the memory.
	args := scope.Memory.GetPtr(inOffset.Uint64(), inSize.Uint64())

	if interpreter.readOnly && !value.IsZero() {
		return nil, ErrWriteProtection
	}
	if !value.IsZero() {
		gas += params.CallStipend
	}
	ret, returnGas, err := interpreter.evm.Call(scope.Contract.Address(), toAddr, args, gas, &value)

	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}
	stack.push(&temp)
	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}

	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	interpreter.returnData = ret
	return ret, nil
}

func opCallCode(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	// Pop gas. The actual gas is in interpreter.evm.callGasTemp.
	stack := scope.Stack
	// We use it as a temporary value
	temp := stack.pop()
	gas := interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, value, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.Address(addr.Bytes20())
	// Get arguments from the memory.
	args := scope.Memory.GetPtr(inOffset.Uint64(), inSize.Uint64())

	if !value.IsZero() {
		gas += params.CallStipend
	}

	ret, returnGas, err := interpreter.evm.CallCode(scope.Contract.Address(), toAddr, args, gas, &value)
	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}
	stack.push(&temp)
	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}

	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	interpreter.returnData = ret
	return ret, nil
}

func opDelegateCall(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	stack := scope.Stack
	// Pop gas. The actual gas is in interpreter.evm.callGasTemp.
	// We use it as a temporary value
	temp := stack.pop()
	gas := interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.Address(addr.Bytes20())
	// Get arguments from the memory.
	args := scope.Memory.GetPtr(inOffset.Uint64(), inSize.Uint64())

	ret, returnGas, err := interpreter.evm.DelegateCall(scope.Contract.Caller(), scope.Contract.Address(), toAddr, args, gas, scope.Contract.value)
	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}
	stack.push(&temp)
	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}

	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	interpreter.returnData = ret
	return ret, nil
}

func opStaticCall(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	// Pop gas. The actual gas is in interpreter.evm.callGasTemp.
	stack := scope.Stack
	// We use it as a temporary value
	temp := stack.pop()
	gas := interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.Address(addr.Bytes20())
	// Get arguments from the memory.
	args := scope.Memory.GetPtr(inOffset.Uint64(), inSize.Uint64())

	ret, returnGas, err := interpreter.evm.StaticCall(scope.Contract.Address(), toAddr, args, gas)
	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}
	stack.push(&temp)
	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}

	scope.Contract.RefundGas(returnGas, interpreter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	interpreter.returnData = ret
	return ret, nil
}

func opReturn(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	offset, size := scope.Stack.pop2()
	ret := scope.Memory.GetCopy(offset.Uint64(), size.Uint64())

	return ret, errStopToken
}

func opRevert(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	offset, size := scope.Stack.pop2()
	ret := scope.Memory.GetCopy(offset.Uint64(), size.Uint64())

	interpreter.returnData = ret
	return ret, ErrExecutionReverted
}

func opUndefined(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	return nil, &ErrInvalidOpCode{opcode: OpCode(scope.Contract.Code[*pc])}
}

func opStop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	return nil, errStopToken
}

func opSelfdestruct(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.readOnly {
		return nil, ErrWriteProtection
	}
	beneficiary := scope.Stack.pop()
	balance := interpreter.evm.StateDB.GetBalance(scope.Contract.Address())
	interpreter.evm.StateDB.AddBalance(beneficiary.Bytes20(), balance, tracing.BalanceIncreaseSelfdestruct)
	interpreter.evm.StateDB.SelfDestruct(scope.Contract.Address())
	if tracer := interpreter.evm.Config.Tracer; tracer != nil {
		if tracer.OnEnter != nil {
			tracer.OnEnter(interpreter.evm.depth, byte(SELFDESTRUCT), scope.Contract.Address(), beneficiary.Bytes20(), []byte{}, 0, balance.ToBig())
		}
		if tracer.OnExit != nil {
			tracer.OnExit(interpreter.evm.depth, []byte{}, 0, nil, false)
		}
	}
	return nil, errStopToken
}

func opSelfdestruct6780(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if interpreter.readOnly {
		return nil, ErrWriteProtection
	}
	beneficiary := scope.Stack.pop()
	balance := interpreter.evm.StateDB.GetBalance(scope.Contract.Address())
	interpreter.evm.StateDB.SubBalance(scope.Contract.Address(), balance, tracing.BalanceDecreaseSelfdestruct)
	interpreter.evm.StateDB.AddBalance(beneficiary.Bytes20(), balance, tracing.BalanceIncreaseSelfdestruct)
	interpreter.evm.StateDB.SelfDestruct6780(scope.Contract.Address())
	if tracer := interpreter.evm.Config.Tracer; tracer != nil {
		if tracer.OnEnter != nil {
			tracer.OnEnter(interpreter.evm.depth, byte(SELFDESTRUCT), scope.Contract.Address(), beneficiary.Bytes20(), []byte{}, 0, balance.ToBig())
		}
		if tracer.OnExit != nil {
			tracer.OnExit(interpreter.evm.depth, []byte{}, 0, nil, false)
		}
	}
	return nil, errStopToken
}

// following functions are used by the instruction jump  table

// make log instruction function
func makeLog(size int) executionFunc {
	return func(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
		if interpreter.readOnly {
			return nil, ErrWriteProtection
		}
		topics := make([]common.Hash, size)
		stack := scope.Stack
		mStart, mSize := stack.pop(), stack.pop()
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}

		d := scope.Memory.GetCopy(mStart.Uint64(), mSize.Uint64())
		interpreter.evm.StateDB.AddLog(&types.Log{
			Address: scope.Contract.Address(),
			Topics:  topics,
			Data:    d,
			// This is a non-consensus field, but assigned here because
			// core/state doesn't know the current block number.
			BlockNumber: interpreter.evm.Context.BlockNumber.Uint64(),
		})

		return nil, nil
	}
}

// opPush1 is a specialized version of pushN
func opPush1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	*pc += 1
	if *pc < codeLen {
		scope.Stack.push(integer.SetUint64(uint64(scope.Contract.Code[*pc])))
	} else {
		scope.Stack.push(integer.Clear())
	}
	return nil, nil
}

// opPush2 is a specialized version of pushN
func opPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	if *pc+2 < codeLen {
		scope.Stack.push(integer.SetBytes2(scope.Contract.Code[*pc+1 : *pc+3]))
	} else if *pc+1 < codeLen {
		scope.Stack.push(integer.SetUint64(uint64(scope.Contract.Code[*pc+1]) << 8))
	} else {
		scope.Stack.push(integer.Clear())
	}
	*pc += 2
	return nil, nil
}

// make push instruction function
func makePush(size uint64, pushByteSize int) executionFunc {
	return func(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
		var (
			codeLen = len(scope.Contract.Code)
			start   = min(codeLen, int(*pc+1))
			end     = min(codeLen, start+pushByteSize)
		)
		a := new(uint256.Int).SetBytes(scope.Contract.Code[start:end])

		// Missing bytes: pushByteSize - len(pushData)
		if missing := pushByteSize - (end - start); missing > 0 {
			a.Lsh(a, uint(8*missing))
		}
		scope.Stack.push(a)
		*pc += size
		return nil, nil
	}
}

// make dup instruction function
func makeDup(size int) executionFunc {
	return func(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
		scope.Stack.dup(size)
		return nil, nil
	}
}

// fused instructions
// opAndSwap1PopSwap2Swap1 implements the fused instruction of And and `move to the stack bottom`.
func opAndSwap1PopSwap2Swap1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	a, b := scope.Stack.pop2()
	c, d, e := scope.Stack.peek(), scope.Stack.Back(1), scope.Stack.Back(2)
	r := a.And(&a, &b)
	*c = *d
	*d = *e
	*e = *r
	*pc += 4
	return nil, nil
}

// opSwap2Swap1PopJump
func opSwap2Swap1PopJump(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	a, _ := scope.Stack.pop2()
	c := scope.Stack.peek()
	dest := *c
	*c = a
	if !scope.Contract.validJumpdest(&dest) {
		return nil, ErrInvalidJump
	}
	*pc = dest.Uint64() - 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opSwap1PopSwap2Swap1
func opSwap1PopSwap2Swap1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	a, _ := scope.Stack.pop2()
	c, d := scope.Stack.pop(), scope.Stack.peek()
	scope.Stack.push(d)
	*d = a
	scope.Stack.push(&c)
	*pc += 3 // pc will be increased by the interpreter loop
	return nil, nil
}

// opPopSwap2Swap1Pop
func opPopSwap2Swap1Pop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	_, b := scope.Stack.pop2()
	_, d := scope.Stack.pop(), scope.Stack.peek()
	scope.Stack.push(d)
	*d = b
	*pc += 3 // pc will be increased by the interpreter loop
	return nil, nil
}

// opPush2Jump
func opPush2Jump(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = len(scope.Contract.Code)
		integer = new(uint256.Int)
		pos     = integer.Clear()
	)

	startMin := codeLen
	if int(*pc+1) < startMin {
		startMin = int(*pc + 1)
	}

	endMin := codeLen
	if startMin+2 < endMin {
		endMin = startMin + 2
	}

	pos = integer.SetBytes(common.RightPadBytes(
		scope.Contract.Code[startMin:endMin], 2))

	if !scope.Contract.validJumpdest(pos) {
		return nil, ErrInvalidJump
	}
	*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opPush2JumpI
func opPush2JumpI(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = len(scope.Contract.Code)
		integer = new(uint256.Int)
		pos     = integer.Clear()
	)

	startMin := codeLen
	if int(*pc+1) < startMin {
		startMin = int(*pc + 1)
	}

	endMin := codeLen
	if startMin+2 < endMin {
		endMin = startMin + 2
	}

	pos = integer.SetBytes(common.RightPadBytes(
		scope.Contract.Code[startMin:endMin], 2))

	cond := scope.Stack.pop()
	if !cond.IsZero() {
		if !scope.Contract.validJumpdest(pos) {
			return nil, ErrInvalidJump
		}
		*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	} else {
		*pc += 3
	}
	return nil, nil
}

// opPush1Push1
func opPush1Push1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		a, b    = new(uint256.Int), new(uint256.Int)
	)
	*pc += 3
	if *pc < codeLen {
		a = a.SetUint64(uint64(scope.Contract.Code[*pc-2]))
		b = b.SetUint64(uint64(scope.Contract.Code[*pc]))
	}
	scope.Stack.push2(a, b)
	return nil, nil
}

// opPush1Add
func opPush1Add(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		a       = new(uint256.Int)
	)
	*pc += 1
	if *pc < codeLen {
		a = a.SetUint64(uint64(scope.Contract.Code[*pc]))
	}
	b := scope.Stack.pop()
	scope.Stack.push(b.Add(a, &b))
	*pc += 1
	return nil, nil
}

// opPush1Shl
func opPush1Shl(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		shift   = new(uint256.Int)
	)
	*pc += 1
	if *pc < codeLen {
		shift = shift.SetUint64(uint64(scope.Contract.Code[*pc]))
	} else {
		shift = shift.Clear()
	}
	value := scope.Stack.peek()

	if shift.LtUint64(256) {
		value.Lsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
	*pc += 1
	return nil, nil
}

// opPush1Dup1
func opPush1Dup1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var (
		codeLen = uint64(len(scope.Contract.Code))
		value   = new(uint256.Int)
	)
	*pc += 1
	if *pc < codeLen {
		value = value.SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	scope.Stack.push2(value, value)
	*pc += 1
	return nil, nil
}

// opSwap1Pop
func opSwap1Pop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	a, b := scope.Stack.pop(), scope.Stack.peek()
	*b = a
	*pc += 1
	return nil, nil
}

// opPopJump
func opPopJump(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	_, pos := scope.Stack.pop2()
	if !scope.Contract.validJumpdest(&pos) {
		return nil, ErrInvalidJump
	}
	*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opPop2
func opPop2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	_, _ = scope.Stack.pop2()
	*pc += 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opSwap2Swap1
func opSwap2Swap1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	a, b, c := *scope.Stack.peek(), *scope.Stack.Back(1), *scope.Stack.Back(2)
	// to b, c, a
	*scope.Stack.peek() = b
	*scope.Stack.Back(1) = c
	*scope.Stack.Back(2) = a
	*pc += 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opSwap2Pop
func opSwap2Pop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	// a,b,c -> b,a
	a := scope.Stack.peek()
	*scope.Stack.Back(2) = *a
	scope.Stack.pop()
	*pc += 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opDup2Lt
func opDup2LT(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	y := scope.Stack.Back(1)
	if y.Lt(x) {
		x.SetOne()
	} else {
		x.Clear()
	}
	*pc += 1 // pc will be increased by the interpreter loop
	return nil, nil
}

// opJumpIfZero
func opJumpIfZero(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	value := scope.Stack.pop()
	integer := new(uint256.Int)
	pos := new(uint256.Int)

	if value.IsZero() {
		codeLen := len(scope.Contract.Code)
		*pc += 2
		startMin := codeLen
		if int(*pc) < startMin {
			startMin = int(*pc)
		}

		endMin := codeLen
		if startMin+2 < endMin {
			endMin = startMin + 2
		}

		pos = integer.SetBytes(common.RightPadBytes(
			scope.Contract.Code[startMin:endMin], 2))

		if !scope.Contract.validJumpdest(pos) {
			return nil, ErrInvalidJump
		}
		*pc = pos.Uint64() - 1 // pc will be increased by the interpreter loop
	} else {
		*pc += 4
	}
	return nil, nil
}

// opNop has no behavior.
func opNop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	if !scope.Contract.optimized {
		return nil, ErrInvalidOptimizedCode
	}
	return nil, nil
}

// opIsZeroPush2 is a super instruction
func opIsZeroPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	if x.IsZero() {
		x.SetOne()
	} else {
		x.Clear()
	}

	*pc += 1
	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	if *pc+2 < codeLen {
		scope.Stack.push(integer.SetBytes2(scope.Contract.Code[*pc+1 : *pc+3]))
	} else if *pc+1 < codeLen {
		scope.Stack.push(integer.SetUint64(uint64(scope.Contract.Code[*pc+1]) << 8))
	} else {
		scope.Stack.push(integer.Clear())
	}
	*pc += 2
	return nil, nil
}

// DUP2 MSTORE PUSH1 ADD
func opDup2MStorePush1Add(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	var mStart, val uint256.Int

	if scope.Stack.len() >= 2 {
		mStart, val = scope.Stack.data[scope.Stack.len()-2], scope.Stack.pop()
	}
	scope.Memory.Set32(mStart.Uint64(), &val)
	*pc += 3

	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	if *pc < codeLen {
		integer.SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	x := scope.Stack.peek()
	x.Add(x, integer)

	*pc += 1

	return nil, nil
}

// DUP1 PUSH4 EQ PUSH2
func opDup1Push4EqPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.dup(1)
	*pc += 1
	_, err := makePush(4, 4)(pc, interpreter, scope)
	if err != nil {
		return nil, err
	}

	*pc += 1
	x, y := scope.Stack.pop(), scope.Stack.peek()
	if x.Eq(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	*pc += 1
	_, err = makePush(2, 2)(pc, interpreter, scope)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

// PUSH1 CALLDATALOAD PUSH1 SHR DUP1 PUSH4 GT PUSH2
func opPush1CalldataloadPush1ShrDup1Push4GtPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	codeLen := uint64(len(scope.Contract.Code))
	var x = new(uint256.Int)
	*pc += 1
	if *pc < codeLen {
		x = new(uint256.Int).SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	if offset, overflow := x.Uint64WithOverflow(); !overflow {
		data := getData(scope.Contract.Input, offset, 32)
		x.SetBytes(data)
	} else {
		x.Clear()
	}

	*pc += 3
	var shift = new(uint256.Int)
	if *pc < codeLen {
		shift = new(uint256.Int).SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	if shift.LtUint64(256) {
		x.Rsh(x, uint(shift.Uint64()))
	} else {
		x.Clear()
	}
	scope.Stack.push(x)
	*pc += 2 // dup1
	scope.Stack.dup(1)
	*pc += 1 // push4

	var (
		start = min(codeLen, *pc+1)
		end   = min(codeLen, start+4)
	)
	a := new(uint256.Int).SetBytes(scope.Contract.Code[start:end])

	// Missing bytes: pushByteSize - len(pushData)
	if missing := 4 - (end - start); missing > 0 {
		a.Lsh(a, uint(8*missing))
	}
	*pc += 5 // GT
	p := scope.Stack.peek()
	if a.Gt(p) {
		p.SetOne()
	} else {
		p.Clear()
	}
	*pc += 1 // push2
	_, err := makePush(2, 2)(pc, interpreter, scope)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// PUSH1 PUSH1 PUSH1 SHL SUB
func opPush1Push1Push1SHLSub(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	codeLen := uint64(len(scope.Contract.Code))
	var integer1, integer2, integer3 = new(uint256.Int), new(uint256.Int), new(uint256.Int)
	*pc += 1
	if *pc < codeLen {
		integer1 = new(uint256.Int).SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	*pc += 2
	if *pc < codeLen {
		integer2 = new(uint256.Int).SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	*pc += 2
	if *pc < codeLen {
		integer3 = new(uint256.Int).SetUint64(uint64(scope.Contract.Code[*pc]))
	}

	if integer3.LtUint64(256) {
		integer2.Lsh(integer2, uint(integer3.Uint64()))
	} else {
		integer2.Clear()
	}

	integer1.Sub(integer2, integer1)
	scope.Stack.push(integer1)
	*pc += 2

	return nil, nil
}

// AND DUP2 ADD SWAP1 DUP2 LT
func opAndDup2AddSwap1Dup2LT(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.Back(0)
	y.And(&x, y)
	z := scope.Stack.Back(1)
	y.Add(z, y)
	tmpy := *y
	tmpz := *z
	scope.Stack.swap1()
	if tmpy.Lt(&tmpz) {
		scope.Stack.Back(0).SetOne()
	} else {
		scope.Stack.Back(0).Clear()
	}

	*pc += 5
	return nil, nil
}

// SWAP1 PUSH1 DUP1 NOT SWAP2 ADD AND DUP2 ADD SWAP1 DUP2 LT
func opSwap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	codeLen := uint64(len(scope.Contract.Code))
	scope.Stack.swap1()

	*pc += 2
	var integer1 = new(uint256.Int)
	if *pc < codeLen {
		integer1.SetUint64(uint64(scope.Contract.Code[*pc]))
	}
	scope.Stack.push(integer1)
	scope.Stack.dup(1)
	x := scope.Stack.peek()
	x.Not(x)
	scope.Stack.swap2()
	a, b := scope.Stack.pop(), scope.Stack.pop()
	b.Add(&a, &b)
	c := scope.Stack.peek()
	c.And(&b, c)
	e := scope.Stack.Back(1)
	c.Add(e, c)
	scope.Stack.swap1()
	g, h := *c, scope.Stack.peek()
	if g.Lt(h) {
		h.SetOne()
	} else {
		h.Clear()
	}

	*pc += 10
	return nil, nil
}

// opDup3And
func opDup3And(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.data[scope.Stack.len()-3]
	y := scope.Stack.peek()
	y.And(&x, y)
	*pc += 1
	return nil, nil
}

// opSwap2Swap1Dup3SubSwap2Dup3GtPush2
func opSwap2Swap1Dup3SubSwap2Dup3GtPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap2()
	scope.Stack.swap1()
	x := scope.Stack.data[scope.Stack.len()-3]
	y := scope.Stack.peek()
	y.Sub(&x, y)
	scope.Stack.swap2()
	x = scope.Stack.data[scope.Stack.len()-3]
	y = scope.Stack.peek()
	if x.Gt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	*pc += 7
	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	if *pc+2 < codeLen {
		scope.Stack.push(integer.SetBytes2(scope.Contract.Code[*pc+1 : *pc+3]))
	} else if *pc+1 < codeLen {
		scope.Stack.push(integer.SetUint64(uint64(scope.Contract.Code[*pc+1]) << 8))
	} else {
		scope.Stack.push(integer.Clear())
	}
	*pc += 2
	return nil, nil
}

func opSwap1Dup2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap1()
	scope.Stack.dup(2)
	*pc += 1
	return nil, nil
}

func opSHRSHRDup1MulDup1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	shift, value := scope.Stack.pop(), scope.Stack.pop()
	if shift.LtUint64(256) {
		value.Rsh(&value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}

	value2 := scope.Stack.peek()
	if value.LtUint64(256) {
		value2.Rsh(value2, uint(value.Uint64()))
	} else {
		value2.Clear()
	}

	value3 := *value2
	value2.Mul(value2, &value3)
	scope.Stack.dup(1)
	*pc += 4
	return nil, nil
}

func opSwap3PopPopPop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	scope.Stack.swap3()
	scope.Stack.pop2()
	scope.Stack.pop()
	*pc += 3
	return nil, nil
}

func opSubSLTIsZeroPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x, y := scope.Stack.pop(), scope.Stack.pop()
	y.Sub(&x, &y)
	z := scope.Stack.peek()
	if y.Slt(z) {
		z.SetOne()
	} else {
		z.Clear()
	}

	if z.IsZero() {
		z.SetOne()
	} else {
		z.Clear()
	}
	*pc += 3
	var (
		codeLen = uint64(len(scope.Contract.Code))
		integer = new(uint256.Int)
	)
	if *pc+2 < codeLen {
		scope.Stack.push(integer.SetBytes2(scope.Contract.Code[*pc+1 : *pc+3]))
	} else if *pc+1 < codeLen {
		scope.Stack.push(integer.SetUint64(uint64(scope.Contract.Code[*pc+1]) << 8))
	} else {
		scope.Stack.push(integer.Clear())
	}
	*pc += 2
	return nil, nil
}

func opDup11MulDup3SubMulDup1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	x := scope.Stack.data[scope.Stack.len()-11]
	y := scope.Stack.pop()
	y.Mul(&x, &y)

	x = scope.Stack.data[scope.Stack.len()-2]
	y.Sub(&x, &y)

	z := scope.Stack.peek()
	z.Mul(&y, z)
	scope.Stack.dup(1)
	*pc += 5
	return nil, nil
}
