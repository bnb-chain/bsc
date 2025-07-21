package vm

import (
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

// opDup3And
func opDup3And(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	log.Error("DEBUG", "instruction", "opDup3And")
	x := scope.Stack.data[scope.Stack.len()-3]
	y := scope.Stack.peek()
	y.And(&x, y)
	*pc += 1
	return nil, nil
}

// opSwap2Swap1Dup3SubSwap2Dup3GtPush2
func opSwap2Swap1Dup3SubSwap2Dup3GtPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	log.Error("DEBUG", "instruction", "opSwap2Swap1Dup3SubSwap2Dup3GtPush2")
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
	log.Error("DEBUG", "instruction", "opSwap1Dup2")
	scope.Stack.swap1()
	scope.Stack.dup(2)
	*pc += 1
	return nil, nil
}

func opSHRSHRDup1MulDup1(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	log.Error("DEBUG", "instruction", "opSHRSHRDup1MulDup1")
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

	value2.Mul(value2, value2)
	scope.Stack.dup(1)
	*pc += 4
	return nil, nil
}

func opSwap3PopPopPop(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	log.Error("DEBUG", "instruction", "opSwap3PopPopPop")
	scope.Stack.swap3()
	scope.Stack.pop2()
	scope.Stack.pop()
	*pc += 3
	return nil, nil
}

func opSubSLTIsZeroPush2(pc *uint64, interpreter *EVMInterpreter, scope *ScopeContext) ([]byte, error) {
	log.Error("DEBUG", "instruction", "opSubSLTIsZeroPush2")
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
	log.Error("DEBUG", "instruction", "opDup11MulDup3SubMulDup1")
	x := scope.Stack.data[scope.Stack.len()-11]
	y := scope.Stack.pop()
	y.Mul(&x, &y)

	x = scope.Stack.data[scope.Stack.len()-3]
	y.Sub(&x, &y)

	z := scope.Stack.peek()
	z.Mul(&y, z)
	scope.Stack.dup(1)
	*pc += 5
	return nil, nil
}
