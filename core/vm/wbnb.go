package vm

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	math "github.com/ethereum/go-ethereum/common/math"
	typespkg "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
)

func (evm *EVM) wbnbContract(contract *Contract) (ret []byte, err error, execpt bool) {
	memory := NewMemory()
	stack := newstack()
	defer memory.Free()
	defer returnStack(stack)

	// shortcuts
	stateDB := evm.StateDB
	_ = stateDB
	_ = sha3.NewLegacyKeccak256
	_ = common.Address{}

	// jump table for gas metadata
	table := evm.interpreter.table
	charge := func(op OpCode) (uint64, error) {
		operation := table[op]
		cost := operation.constantGas
		if contract.Gas < cost {
			return 0, ErrOutOfGas
		}
		contract.Gas -= cost
		var memorySize uint64
		if operation.dynamicGas != nil {
			if operation.memorySize != nil {
				size, overflow := operation.memorySize(stack)
				if overflow {
					return 0, ErrGasUintOverflow
				}
				var overflow2 bool
				if memorySize, overflow2 = math.SafeMul(toWordSize(size), 32); overflow2 {
					return 0, ErrGasUintOverflow
				}
			}
			dyn, derr := operation.dynamicGas(evm, contract, stack, memory, memorySize)
			if derr != nil {
				return 0, fmt.Errorf("%w: %v", ErrOutOfGas, derr)
			}
			if contract.Gas < dyn {
				return 0, ErrOutOfGas
			}
			contract.Gas -= dyn
			if memorySize > 0 {
				memory.Resize(memorySize)
			}
			cost += dyn
		}
		return cost, nil
	}
	// PC: 0x0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x60}))
	// PC: 0x2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x4, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x7, Opcode: CALLDATASIZE
	if _, err = charge(CALLDATASIZE); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetUint64(uint64(len(contract.Input))))
	// PC: 0x8, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x9, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xaf}))
	// PC: 0xc, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xf, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x10, Opcode: PUSH29
	if _, err = charge(PUSH29); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}))
	// PC: 0x2e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x2f, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0x30, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff}))
	// PC: 0x35, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x36, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x37, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x06, 0xfd, 0xde, 0x03}))
	// PC: 0x3c, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x3d, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xb9}))
	// PC: 0x40, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x41, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x42, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x09, 0x5e, 0xa7, 0xb3}))
	// PC: 0x47, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x48, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x47}))
	// PC: 0x4b, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x4c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4d, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x18, 0x16, 0x0d, 0xdd}))
	// PC: 0x52, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x53, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0xa1}))
	// PC: 0x56, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x57, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x58, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x23, 0xb8, 0x72, 0xdd}))
	// PC: 0x5d, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x5e, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0xca}))
	// PC: 0x61, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x62, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x63, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x2e, 0x1a, 0x7d, 0x4d}))
	// PC: 0x68, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x69, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x43}))
	// PC: 0x6c, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x6d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x6e, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x31, 0x3c, 0xe5, 0x67}))
	// PC: 0x73, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x74, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x66}))
	// PC: 0x77, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x78, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x79, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x70, 0xa0, 0x82, 0x31}))
	// PC: 0x7e, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x7f, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x95}))
	// PC: 0x82, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x83, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x84, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x95, 0xd8, 0x9b, 0x41}))
	// PC: 0x89, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x8a, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0xe2}))
	// PC: 0x8d, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x8e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x8f, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xa9, 0x05, 0x9c, 0xbb}))
	// PC: 0x94, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x95, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0x70}))
	// PC: 0x98, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x99, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x9a, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xd0, 0xe3, 0x0d, 0xb0}))
	// PC: 0x9f, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xa0, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0xca}))
	// PC: 0xa3, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xa4, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xa5, Opcode: PUSH4
	if _, err = charge(PUSH4); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xdd, 0x62, 0xed, 0x3e}))
	// PC: 0xaa, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xab, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0xd4}))
	// PC: 0xae, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}

JUMPDEST_175:
	// PC: 0xaf, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb0, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xb7}))
	// PC: 0xb3, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04, 0x40}))
	// PC: 0xb6, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_183:
	// PC: 0xb7, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb8, Opcode: STOP
	if _, err = charge(STOP); err != nil {
		return nil, err, true
	}
	return nil, nil, true

JUMPDEST_185:
	// PC: 0xb9, Opcode: JUMPDEST
	// jump destination
	// PC: 0xba, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0xbb, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xbc, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xc4}))
	// PC: 0xbf, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xc0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xc2, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xc3, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_196:
	// PC: 0xc4, Opcode: JUMPDEST
	// jump destination
	// PC: 0xc5, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xcc}))
	// PC: 0xc8, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04, 0xdd}))
	// PC: 0xcb, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_204:
	// PC: 0xcc, Opcode: JUMPDEST
	// jump destination
	// PC: 0xcd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xcf, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xd0, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xd1, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xd2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xd4, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xd5, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xd6, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xd7, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xd8, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xd9, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xda, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xdb, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xdc, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xdd, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xde, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xdf, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xe0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xe2, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xe3, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xe4, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xe5, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xe6, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xe7, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xe8, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xea, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xeb, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xec, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xed, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xee, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xef, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))

JUMPDEST_241:
	// PC: 0xf1, Opcode: JUMPDEST
	// jump destination
	// PC: 0xf2, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xf3, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xf4, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xf5, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xf6, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x0c}))
	// PC: 0xf9, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xfa, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xfb, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xfc, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xfd, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xfe, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xff, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x100, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x101, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x102, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x104, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x105, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x106, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x107, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x108, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00, 0xf1}))
	// PC: 0x10b, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_268:
	// PC: 0x10c, Opcode: JUMPDEST
	// jump destination
	// PC: 0x10d, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x10e, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x10f, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x110, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x111, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x112, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x113, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x114, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x115, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x116, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x117, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0x119, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x11a, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x11b, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x11c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x39}))
	// PC: 0x11f, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x120, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x121, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x122, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x123, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x124, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x125, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x127, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x128, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x12a, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x12b, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0x12e, Opcode: EXP
	if _, err = charge(EXP); err != nil {
		return nil, err, true
	}
	{
		base, exponent := stack.pop(), stack.peek()
		exponent.Exp(&base, exponent)
	}
	// PC: 0x12f, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x130, Opcode: NOT
	if _, err = charge(NOT); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		x.Not(x)
	}
	// PC: 0x131, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x132, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x133, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x134, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x136, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x137, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x138, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()

JUMPDEST_313:
	// PC: 0x139, Opcode: JUMPDEST
	// jump destination
	// PC: 0x13a, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x13b, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x13c, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x13d, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x13e, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x13f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x141, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x142, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x143, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x144, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x145, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x146, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_327:
	// PC: 0x147, Opcode: JUMPDEST
	// jump destination
	// PC: 0x148, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x149, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x14a, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x52}))
	// PC: 0x14d, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x14e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x150, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x151, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_338:
	// PC: 0x152, Opcode: JUMPDEST
	// jump destination
	// PC: 0x153, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x87}))
	// PC: 0x156, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x158, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x159, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x15a, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x15b, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x170, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x171, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x172, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x174, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x175, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x176, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x177, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x178, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x179, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x17a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x17b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x17d, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x17e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x17f, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x180, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x181, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x182, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x183, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x05, 0x7b}))
	// PC: 0x186, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_391:
	// PC: 0x187, Opcode: JUMPDEST
	// jump destination
	// PC: 0x188, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x18a, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x18b, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x18c, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x18d, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x18e, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x18f, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x190, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x191, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x192, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x193, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x195, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x196, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x197, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x198, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x199, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x19b, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x19c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x19d, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x19e, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x19f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x1a0, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_417:
	// PC: 0x1a1, Opcode: JUMPDEST
	// jump destination
	// PC: 0x1a2, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x1a3, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x1a4, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0xac}))
	// PC: 0x1a7, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x1a8, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x1aa, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1ab, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_428:
	// PC: 0x1ac, Opcode: JUMPDEST
	// jump destination
	// PC: 0x1ad, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0xb4}))
	// PC: 0x1b0, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x06, 0x6d}))
	// PC: 0x1b3, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_436:
	// PC: 0x1b4, Opcode: JUMPDEST
	// jump destination
	// PC: 0x1b5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x1b7, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x1b8, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1b9, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x1ba, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x1bb, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x1bc, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x1be, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x1bf, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x1c0, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x1c1, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x1c2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x1c4, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x1c5, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1c6, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x1c7, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x1c8, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x1c9, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_458:
	// PC: 0x1ca, Opcode: JUMPDEST
	// jump destination
	// PC: 0x1cb, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x1cc, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x1cd, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0xd5}))
	// PC: 0x1d0, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x1d1, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x1d3, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1d4, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_469:
	// PC: 0x1d5, Opcode: JUMPDEST
	// jump destination
	// PC: 0x1d6, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x29}))
	// PC: 0x1d9, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x1db, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1dc, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1dd, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x1de, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x1f3, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x1f4, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x1f5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x1f7, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x1f8, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x1f9, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x1fa, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x1fb, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x1fc, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x1fd, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x212, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x213, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x214, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x216, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x217, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x218, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x219, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x21a, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x21b, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x21c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x21d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x21f, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x220, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x221, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x222, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x223, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x224, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x225, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x06, 0x8c}))
	// PC: 0x228, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_553:
	// PC: 0x229, Opcode: JUMPDEST
	// jump destination
	// PC: 0x22a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x22c, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x22d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x22e, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x22f, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x230, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x231, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x232, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x233, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x234, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x235, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x237, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x238, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x239, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x23a, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x23b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x23d, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x23e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x23f, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x240, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x241, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x242, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_579:
	// PC: 0x243, Opcode: JUMPDEST
	// jump destination
	// PC: 0x244, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x245, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x246, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x4e}))
	// PC: 0x249, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x24a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x24c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x24d, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_590:
	// PC: 0x24e, Opcode: JUMPDEST
	// jump destination
	// PC: 0x24f, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x64}))
	// PC: 0x252, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x254, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x255, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x256, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x257, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x258, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x25a, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x25b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x25c, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x25d, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x25e, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x25f, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x260, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x09, 0xd9}))
	// PC: 0x263, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_612:
	// PC: 0x264, Opcode: JUMPDEST
	// jump destination
	// PC: 0x265, Opcode: STOP
	if _, err = charge(STOP); err != nil {
		return nil, err, true
	}
	return nil, nil, true

JUMPDEST_614:
	// PC: 0x266, Opcode: JUMPDEST
	// jump destination
	// PC: 0x267, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x268, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x269, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x71}))
	// PC: 0x26c, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x26d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x26f, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x270, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_625:
	// PC: 0x271, Opcode: JUMPDEST
	// jump destination
	// PC: 0x272, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0x79}))
	// PC: 0x275, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0x05}))
	// PC: 0x278, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_633:
	// PC: 0x279, Opcode: JUMPDEST
	// jump destination
	// PC: 0x27a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x27c, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x27d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x27e, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x27f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff}))
	// PC: 0x281, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x282, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff}))
	// PC: 0x284, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x285, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x286, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x287, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x289, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x28a, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x28b, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x28c, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x28d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x28f, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x290, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x291, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x292, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x293, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x294, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_661:
	// PC: 0x295, Opcode: JUMPDEST
	// jump destination
	// PC: 0x296, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x297, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x298, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0xa0}))
	// PC: 0x29b, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x29c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x29e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x29f, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_672:
	// PC: 0x2a0, Opcode: JUMPDEST
	// jump destination
	// PC: 0x2a1, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0xcc}))
	// PC: 0x2a4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x2a6, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2a7, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2a8, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x2a9, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x2be, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x2bf, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x2c0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x2c2, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x2c3, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x2c4, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x2c5, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x2c6, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x2c7, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x2c8, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0x18}))
	// PC: 0x2cb, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_716:
	// PC: 0x2cc, Opcode: JUMPDEST
	// jump destination
	// PC: 0x2cd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x2cf, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x2d0, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2d1, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x2d2, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x2d3, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x2d4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x2d6, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x2d7, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x2d8, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x2d9, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x2da, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x2dc, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x2dd, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2de, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x2df, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x2e0, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x2e1, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_738:
	// PC: 0x2e2, Opcode: JUMPDEST
	// jump destination
	// PC: 0x2e3, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x2e4, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x2e5, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0xed}))
	// PC: 0x2e8, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x2e9, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x2eb, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2ec, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_749:
	// PC: 0x2ed, Opcode: JUMPDEST
	// jump destination
	// PC: 0x2ee, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02, 0xf5}))
	// PC: 0x2f1, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0x30}))
	// PC: 0x2f4, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_757:
	// PC: 0x2f5, Opcode: JUMPDEST
	// jump destination
	// PC: 0x2f6, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x2f8, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x2f9, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2fa, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x2fb, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x2fd, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x2fe, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x2ff, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x300, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x301, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x302, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x303, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x304, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x305, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x306, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x307, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x308, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x309, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x30b, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x30c, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x30d, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x30e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x30f, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x310, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x311, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x313, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x314, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x315, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x316, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x317, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x318, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))

JUMPDEST_794:
	// PC: 0x31a, Opcode: JUMPDEST
	// jump destination
	// PC: 0x31b, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x31c, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x31d, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x31e, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x31f, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0x35}))
	// PC: 0x322, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x323, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x324, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x325, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x326, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x327, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x328, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x329, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x32a, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x32b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x32d, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x32e, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x32f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x330, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x331, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0x1a}))
	// PC: 0x334, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_821:
	// PC: 0x335, Opcode: JUMPDEST
	// jump destination
	// PC: 0x336, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x337, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x338, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x339, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x33a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x33b, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x33c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x33d, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x33e, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x33f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x340, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0x342, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x343, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x344, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x345, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0x62}))
	// PC: 0x348, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x349, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x34a, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x34b, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x34c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x34d, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x34e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x350, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x351, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x353, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x354, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0x357, Opcode: EXP
	if _, err = charge(EXP); err != nil {
		return nil, err, true
	}
	{
		base, exponent := stack.pop(), stack.peek()
		exponent.Exp(&base, exponent)
	}
	// PC: 0x358, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x359, Opcode: NOT
	if _, err = charge(NOT); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		x.Not(x)
	}
	// PC: 0x35a, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x35b, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x35c, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x35d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x35f, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x360, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x361, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()

JUMPDEST_866:
	// PC: 0x362, Opcode: JUMPDEST
	// jump destination
	// PC: 0x363, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x364, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x365, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x366, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x367, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x368, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x36a, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x36b, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x36c, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x36d, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x36e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x36f, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_880:
	// PC: 0x370, Opcode: JUMPDEST
	// jump destination
	// PC: 0x371, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x372, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x373, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0x7b}))
	// PC: 0x376, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x377, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x379, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x37a, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_891:
	// PC: 0x37b, Opcode: JUMPDEST
	// jump destination
	// PC: 0x37c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0xb0}))
	// PC: 0x37f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x381, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x382, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x383, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x384, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x399, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x39a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x39b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x39d, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x39e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x39f, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x3a0, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3a1, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3a2, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x3a3, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3a4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x3a6, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x3a7, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3a8, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x3a9, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3aa, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x3ab, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x3ac, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xce}))
	// PC: 0x3af, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_944:
	// PC: 0x3b0, Opcode: JUMPDEST
	// jump destination
	// PC: 0x3b1, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x3b3, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x3b4, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3b5, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x3b6, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x3b7, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x3b8, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x3b9, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x3ba, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x3bb, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x3bc, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x3be, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x3bf, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x3c0, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x3c1, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x3c2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x3c4, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x3c5, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3c6, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x3c7, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x3c8, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3c9, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_970:
	// PC: 0x3ca, Opcode: JUMPDEST
	// jump destination
	// PC: 0x3cb, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0xd2}))
	// PC: 0x3ce, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04, 0x40}))
	// PC: 0x3d1, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_978:
	// PC: 0x3d2, Opcode: JUMPDEST
	// jump destination
	// PC: 0x3d3, Opcode: STOP
	if _, err = charge(STOP); err != nil {
		return nil, err, true
	}
	return nil, nil, true

JUMPDEST_980:
	// PC: 0x3d4, Opcode: JUMPDEST
	// jump destination
	// PC: 0x3d5, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x3d6, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x3d7, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03, 0xdf}))
	// PC: 0x3da, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x3db, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x3dd, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3de, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_991:
	// PC: 0x3df, Opcode: JUMPDEST
	// jump destination
	// PC: 0x3e0, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04, 0x2a}))
	// PC: 0x3e3, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x3e5, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3e6, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x3e7, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x3e8, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x3fd, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x3fe, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x3ff, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x401, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x402, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x403, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x404, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x405, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x406, Opcode: CALLDATALOAD
	if _, err = charge(CALLDATALOAD); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if off, ov := x.Uint64WithOverflow(); !ov {
			data := getData(contract.Input, off, 32)
			x.SetBytes(data)
		} else {
			x.Clear()
		}
	}
	// PC: 0x407, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x41c, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x41d, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x41e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x420, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x421, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x422, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x423, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x424, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x425, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x426, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xe3}))
	// PC: 0x429, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1066:
	// PC: 0x42a, Opcode: JUMPDEST
	// jump destination
	// PC: 0x42b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x42d, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x42e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x42f, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x430, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x431, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x432, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x434, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x435, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x436, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x437, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x438, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x43a, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x43b, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x43c, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x43d, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x43e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x43f, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}

JUMPDEST_1088:
	// PC: 0x440, Opcode: JUMPDEST
	// jump destination
	// PC: 0x441, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x442, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0x444, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x446, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x447, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x45c, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x45d, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x472, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x473, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x474, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x475, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x477, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x478, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x479, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x47a, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x47b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x47d, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x47e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x480, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x481, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x483, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x484, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x485, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x486, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x487, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x488, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x489, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x48a, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x48b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x48c, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0x48d, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x48e, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x48f, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x4a4, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x4a5, Opcode: PUSH32
	if _, err = charge(PUSH32); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xe1, 0xff, 0xfc, 0xc4, 0x92, 0x3d, 0x04, 0xb5, 0x59, 0xf4, 0xd2, 0x9a, 0x8b, 0xfc, 0x6c, 0xda, 0x04, 0xeb, 0x5b, 0x0d, 0x3c, 0x46, 0x07, 0x51, 0xc2, 0x40, 0x2c, 0x5c, 0x5c, 0xc9, 0x10, 0x9c}))
	// PC: 0x4c6, Opcode: CALLVALUE
	if _, err = charge(CALLVALUE); err != nil {
		return nil, err, true
	}
	stack.push(contract.value)
	// PC: 0x4c7, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x4c9, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x4ca, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4cb, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x4cc, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x4cd, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x4ce, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x4d0, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x4d1, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x4d2, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x4d3, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x4d4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x4d6, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x4d7, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4d8, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x4d9, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x4da, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x4db, Opcode: LOG2
	if _, err = charge(LOG2); err != nil {
		return nil, err, true
	}
	{
		mstart, msize := stack.pop(), stack.pop()
		size := int(2)
		topics := make([]common.Hash, size)
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}
		d := memory.GetCopy(mstart.Uint64(), msize.Uint64())
		evm.StateDB.AddLog(&typespkg.Log{Address: contract.Address(), Topics: topics, Data: d, BlockNumber: evm.Context.BlockNumber.Uint64()})
	}
	// PC: 0x4dc, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1245:
	// PC: 0x4dd, Opcode: JUMPDEST
	// jump destination
	// PC: 0x4de, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x4e0, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4e1, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x4e2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x4e4, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x4e5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x4e7, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x4e8, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x4e9, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0x4ec, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0x4ed, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x4ee, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x4ef, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02}))
	// PC: 0x4f1, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x4f2, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0x4f3, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4f4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0x4f6, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x4f7, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x4f9, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x4fa, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x4fb, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0x4fc, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0x4fd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x4ff, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x500, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x502, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x503, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x504, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x505, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x506, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x508, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x509, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x50a, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x50b, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x50c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x50d, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x50e, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x50f, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x510, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x512, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x513, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x514, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x515, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x516, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x518, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x519, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x51b, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x51c, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x51d, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0x520, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0x521, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x522, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x523, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02}))
	// PC: 0x525, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x526, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0x527, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x528, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x529, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x05, 0x73}))
	// PC: 0x52c, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x52d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x52e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0x530, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x531, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x05, 0x48}))
	// PC: 0x534, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x535, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0x538, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x539, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x53a, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x53b, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0x53c, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0x53d, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x53e, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x53f, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x540, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x542, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x543, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x544, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x05, 0x73}))
	// PC: 0x547, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1352:
	// PC: 0x548, Opcode: JUMPDEST
	// jump destination
	// PC: 0x549, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x54a, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x54b, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x54c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x54d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x54f, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x550, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x552, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x554, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x555, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()

JUMPDEST_1366:
	// PC: 0x556, Opcode: JUMPDEST
	// jump destination
	// PC: 0x557, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x558, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x559, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x55a, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x55b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x55c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x55e, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x55f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x560, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x562, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x563, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x564, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0x565, Opcode: GT
	if _, err = charge(GT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Gt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x566, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x05, 0x56}))
	// PC: 0x569, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x56a, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x56b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x56c, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x56d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0x56f, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x570, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x571, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x572, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()

JUMPDEST_1395:
	// PC: 0x573, Opcode: JUMPDEST
	// jump destination
	// PC: 0x574, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x575, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x576, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x577, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x578, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x579, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x57a, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1403:
	// PC: 0x57b, Opcode: JUMPDEST
	// jump destination
	// PC: 0x57c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x57e, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x57f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x581, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x583, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x584, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x599, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x59a, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x5af, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x5b0, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x5b1, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x5b2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x5b4, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x5b5, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x5b6, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x5b7, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x5b8, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x5ba, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x5bb, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x5bd, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x5be, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x5c0, Opcode: DUP6
	if _, err = charge(DUP6); err != nil {
		return nil, err, true
	}
	stack.dup(6)
	// PC: 0x5c1, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x5d6, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x5d7, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x5ec, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x5ed, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x5ee, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x5ef, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x5f1, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x5f2, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x5f3, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x5f4, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x5f5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x5f7, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x5f8, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x5fa, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x5fb, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x5fc, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x5fd, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0x5fe, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x5ff, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x600, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x615, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x616, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x617, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x62c, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x62d, Opcode: PUSH32
	if _, err = charge(PUSH32); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x8c, 0x5b, 0xe1, 0xe5, 0xeb, 0xec, 0x7d, 0x5b, 0xd1, 0x4f, 0x71, 0x42, 0x7d, 0x1e, 0x84, 0xf3, 0xdd, 0x03, 0x14, 0xc0, 0xf7, 0xb2, 0x29, 0x1e, 0x5b, 0x20, 0x0a, 0xc8, 0xc7, 0xc3, 0xb9, 0x25}))
	// PC: 0x64e, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x64f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x651, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x652, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x653, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x654, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x655, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x656, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x658, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x659, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x65a, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x65b, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x65c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x65e, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x65f, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x660, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x661, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x662, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x663, Opcode: LOG3
	if _, err = charge(LOG3); err != nil {
		return nil, err, true
	}
	{
		mstart, msize := stack.pop(), stack.pop()
		size := int(3)
		topics := make([]common.Hash, size)
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}
		d := memory.GetCopy(mstart.Uint64(), msize.Uint64())
		evm.StateDB.AddLog(&typespkg.Log{Address: contract.Address(), Topics: topics, Data: d, BlockNumber: evm.Context.BlockNumber.Uint64()})
	}
	// PC: 0x664, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x666, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x667, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x668, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x669, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x66a, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x66b, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x66c, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1645:
	// PC: 0x66d, Opcode: JUMPDEST
	// jump destination
	// PC: 0x66e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x670, Opcode: ADDRESS
	if _, err = charge(ADDRESS); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Address().Bytes()))
	// PC: 0x671, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x686, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x687, Opcode: BALANCE
	if _, err = charge(BALANCE); err != nil {
		return nil, err, true
	}
	{
		slot := stack.peek()
		addr := common.Address(slot.Bytes20())
		slot.Set(evm.StateDB.GetBalance(addr))
	}
	// PC: 0x688, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x689, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x68a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x68b, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_1676:
	// PC: 0x68c, Opcode: JUMPDEST
	// jump destination
	// PC: 0x68d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x68f, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x690, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0x692, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x694, Opcode: DUP7
	if _, err = charge(DUP7); err != nil {
		return nil, err, true
	}
	stack.dup(7)
	// PC: 0x695, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x6aa, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x6ab, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x6c0, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x6c1, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x6c2, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x6c3, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x6c5, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x6c6, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x6c7, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x6c8, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x6c9, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x6cb, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x6cc, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x6ce, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x6cf, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x6d0, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x6d1, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x6d2, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x6d3, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x6d4, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x06, 0xdc}))
	// PC: 0x6d7, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x6d8, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x6da, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x6db, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_1756:
	// PC: 0x6dc, Opcode: JUMPDEST
	// jump destination
	// PC: 0x6dd, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x6de, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x6f3, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x6f4, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x6f5, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x70a, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x70b, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x70c, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x70d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x70e, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x70f, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x07, 0xb4}))
	// PC: 0x712, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x713, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x714, Opcode: PUSH32
	if _, err = charge(PUSH32); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x735, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x737, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x739, Opcode: DUP7
	if _, err = charge(DUP7); err != nil {
		return nil, err, true
	}
	stack.dup(7)
	// PC: 0x73a, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x74f, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x750, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x765, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x766, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x767, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x768, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x76a, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x76b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x76c, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x76d, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x76e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x770, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x771, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x773, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x774, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x776, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x777, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x78c, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x78d, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x7a2, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x7a3, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x7a4, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x7a5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x7a7, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x7a8, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x7a9, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x7aa, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x7ab, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x7ad, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x7ae, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x7b0, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x7b1, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x7b2, Opcode: EQ
	if _, err = charge(EQ); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Eq(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x7b3, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}

JUMPDEST_1972:
	// PC: 0x7b4, Opcode: JUMPDEST
	// jump destination
	// PC: 0x7b5, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x7b6, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x08, 0xcf}))
	// PC: 0x7b9, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x7ba, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x7bb, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x7bd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x7bf, Opcode: DUP7
	if _, err = charge(DUP7); err != nil {
		return nil, err, true
	}
	stack.dup(7)
	// PC: 0x7c0, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x7d5, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x7d6, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x7eb, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x7ec, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x7ed, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x7ee, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x7f0, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x7f1, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x7f2, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x7f3, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x7f4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x7f6, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x7f7, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x7f9, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x7fa, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x7fc, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x7fd, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x812, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x813, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x828, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x829, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x82a, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x82b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x82d, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x82e, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x82f, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x830, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x831, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x833, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x834, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x836, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x837, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x838, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0x839, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x83a, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x83b, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0x83c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x08, 0x44}))
	// PC: 0x83f, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0x840, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x842, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x843, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_2116:
	// PC: 0x844, Opcode: JUMPDEST
	// jump destination
	// PC: 0x845, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x846, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0x848, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x84a, Opcode: DUP7
	if _, err = charge(DUP7); err != nil {
		return nil, err, true
	}
	stack.dup(7)
	// PC: 0x84b, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x860, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x861, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x876, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x877, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x878, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x879, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x87b, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x87c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x87d, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x87e, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x87f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x881, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x882, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x884, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x885, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x887, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x888, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x89d, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x89e, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x8b3, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x8b4, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x8b5, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x8b6, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x8b8, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x8b9, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x8ba, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x8bb, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x8bc, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x8be, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x8bf, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x8c1, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x8c2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x8c4, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x8c5, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x8c6, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x8c7, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x8c8, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x8c9, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x8ca, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x8cb, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x8cc, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x8cd, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0x8ce, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()

JUMPDEST_2255:
	// PC: 0x8cf, Opcode: JUMPDEST
	// jump destination
	// PC: 0x8d0, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x8d1, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0x8d3, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x8d5, Opcode: DUP7
	if _, err = charge(DUP7); err != nil {
		return nil, err, true
	}
	stack.dup(7)
	// PC: 0x8d6, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x8eb, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x8ec, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x901, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x902, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x903, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x904, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x906, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x907, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x908, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x909, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x90a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x90c, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x90d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x90f, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x910, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x912, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x913, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x914, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x915, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x916, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x917, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x918, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x919, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x91a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x91b, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0x91c, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x91d, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x91e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0x920, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x922, Opcode: DUP6
	if _, err = charge(DUP6); err != nil {
		return nil, err, true
	}
	stack.dup(6)
	// PC: 0x923, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x938, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x939, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x94e, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x94f, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x950, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x951, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x953, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x954, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x955, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x956, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x957, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x959, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x95a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x95c, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0x95d, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x95f, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x960, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x961, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0x962, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x963, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x964, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x965, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x966, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x967, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x968, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0x969, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x96a, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x96b, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x980, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x981, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x982, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x997, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x998, Opcode: PUSH32
	if _, err = charge(PUSH32); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xdd, 0xf2, 0x52, 0xad, 0x1b, 0xe2, 0xc8, 0x9b, 0x69, 0xc2, 0xb0, 0x68, 0xfc, 0x37, 0x8d, 0xaa, 0x95, 0x2b, 0xa7, 0xf1, 0x63, 0xc4, 0xa1, 0x16, 0x28, 0xf5, 0x5a, 0x4d, 0xf5, 0x23, 0xb3, 0xef}))
	// PC: 0x9b9, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0x9ba, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x9bc, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x9bd, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x9be, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0x9bf, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0x9c0, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0x9c1, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0x9c3, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0x9c4, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x9c5, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9c6, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9c7, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0x9c9, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0x9ca, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x9cb, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0x9cc, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0x9cd, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x9ce, Opcode: LOG3
	if _, err = charge(LOG3); err != nil {
		return nil, err, true
	}
	{
		mstart, msize := stack.pop(), stack.pop()
		size := int(3)
		topics := make([]common.Hash, size)
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}
		d := memory.GetCopy(mstart.Uint64(), msize.Uint64())
		evm.StateDB.AddLog(&typespkg.Log{Address: contract.Address(), Topics: topics, Data: d, BlockNumber: evm.Context.BlockNumber.Uint64()})
	}
	// PC: 0x9cf, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0x9d1, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0x9d2, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9d3, Opcode: SWAP4
	if _, err = charge(SWAP4); err != nil {
		return nil, err, true
	}
	stack.swap4()
	// PC: 0x9d4, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0x9d5, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9d6, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9d7, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0x9d8, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_2521:
	// PC: 0x9d9, Opcode: JUMPDEST
	// jump destination
	// PC: 0x9da, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0x9db, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0x9dd, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0x9df, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0x9e0, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0x9f5, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0x9f6, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0xa0b, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xa0c, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa0d, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xa0e, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xa10, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xa11, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xa12, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa13, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xa14, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xa16, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xa17, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa19, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xa1a, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xa1b, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xa1c, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xa1d, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xa1e, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xa1f, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0a, 0x27}))
	// PC: 0xa22, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xa23, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa25, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xa26, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_2599:
	// PC: 0xa27, Opcode: JUMPDEST
	// jump destination
	// PC: 0xa28, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xa29, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0xa2b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa2d, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0xa2e, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0xa43, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xa44, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0xa59, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xa5a, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa5b, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xa5c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xa5e, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xa5f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xa60, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa61, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xa62, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xa64, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xa65, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa67, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xa68, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa6a, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xa6b, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xa6c, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xa6d, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xa6e, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0xa6f, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xa70, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xa71, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa72, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xa73, Opcode: SSTORE
	if _, err = charge(SSTORE); err != nil {
		return nil, err, true
	}
	{
		if evm.interpreter.readOnly {
			return nil, ErrWriteProtection, true
		}
		loc := stack.pop()
		val := stack.pop()
		evm.StateDB.SetState(contract.Address(), loc.Bytes32(), val.Bytes32())
	}
	// PC: 0xa74, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xa75, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0xa76, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0xa8b, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xa8c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x08, 0xfc}))
	// PC: 0xa8f, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xa90, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xa91, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xa92, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xa93, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0xa94, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xa95, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xa97, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xa98, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xa9a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xa9c, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xa9d, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xa9e, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xa9f, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xaa0, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xaa1, Opcode: DUP6
	if _, err = charge(DUP6); err != nil {
		return nil, err, true
	}
	stack.dup(6)
	// PC: 0xaa2, Opcode: DUP9
	if _, err = charge(DUP9); err != nil {
		return nil, err, true
	}
	stack.dup(9)
	// PC: 0xaa3, Opcode: DUP9
	if _, err = charge(DUP9); err != nil {
		return nil, err, true
	}
	stack.dup(9)
	// PC: 0xaa4, Opcode: CALL
	// Unimplemented opcode CALL -> fallback to interpreter
	return nil, nil, false
	// PC: 0xaa5, Opcode: SWAP4
	if _, err = charge(SWAP4); err != nil {
		return nil, err, true
	}
	stack.swap4()
	// PC: 0xaa6, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xaa7, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xaa8, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xaa9, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xaaa, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xaab, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xaac, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0a, 0xb4}))
	// PC: 0xaaf, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xab0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xab2, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xab3, Opcode: REVERT
	if _, err = charge(REVERT); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, ErrExecutionReverted, true
	}

JUMPDEST_2740:
	// PC: 0xab4, Opcode: JUMPDEST
	// jump destination
	// PC: 0xab5, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0xab6, Opcode: PUSH20
	if _, err = charge(PUSH20); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}))
	// PC: 0xacb, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xacc, Opcode: PUSH32
	if _, err = charge(PUSH32); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x7f, 0xcf, 0x53, 0x2c, 0x15, 0xf0, 0xa6, 0xdb, 0x0b, 0xd6, 0xd0, 0xe0, 0x38, 0xbe, 0xa7, 0x1d, 0x30, 0xd8, 0x08, 0xc7, 0xd9, 0x8c, 0xb3, 0xbf, 0x72, 0x68, 0xa9, 0x5b, 0xf5, 0x08, 0x1b, 0x65}))
	// PC: 0xaed, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xaee, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xaf0, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xaf1, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xaf2, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xaf3, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xaf4, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xaf5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xaf7, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xaf8, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xaf9, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xafa, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xafb, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xafd, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xafe, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xaff, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb00, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xb01, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb02, Opcode: LOG2
	if _, err = charge(LOG2); err != nil {
		return nil, err, true
	}
	{
		mstart, msize := stack.pop(), stack.pop()
		size := int(2)
		topics := make([]common.Hash, size)
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}
		d := memory.GetCopy(mstart.Uint64(), msize.Uint64())
		evm.StateDB.AddLog(&typespkg.Log{Address: contract.Address(), Topics: topics, Data: d, BlockNumber: evm.Context.BlockNumber.Uint64()})
	}
	// PC: 0xb03, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xb04, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_2821:
	// PC: 0xb05, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb06, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02}))
	// PC: 0xb08, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xb0a, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb0b, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xb0c, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb0d, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0xb10, Opcode: EXP
	if _, err = charge(EXP); err != nil {
		return nil, err, true
	}
	{
		base, exponent := stack.pop(), stack.peek()
		exponent.Exp(&base, exponent)
	}
	// PC: 0xb11, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb12, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0xb13, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0xff}))
	// PC: 0xb15, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xb16, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb17, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_2840:
	// PC: 0xb18, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb19, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x03}))
	// PC: 0xb1b, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xb1d, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xb1e, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb1f, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xb21, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xb22, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xb24, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xb26, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xb27, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xb29, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb2a, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xb2b, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb2c, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xb2d, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xb2e, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb2f, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_2864:
	// PC: 0xb30, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb31, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xb33, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb34, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xb35, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xb37, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb38, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xb3a, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xb3b, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xb3c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0xb3f, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0xb40, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xb41, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xb42, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02}))
	// PC: 0xb44, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb45, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0xb46, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb47, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0xb49, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb4a, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xb4c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb4d, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb4e, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0xb4f, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0xb50, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xb52, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb53, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xb55, Opcode: MLOAD
	if _, err = charge(MLOAD); err != nil {
		return nil, err, true
	}
	{
		off := stack.peek()
		data := memory.GetPtr(off.Uint64(), 32)
		off.SetBytes(data)
	}
	// PC: 0xb56, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb57, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb58, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb59, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xb5b, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xb5c, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb5d, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0xb5e, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb5f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb60, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb61, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb62, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xb63, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xb65, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb66, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xb67, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb68, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xb69, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xb6b, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xb6c, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xb6e, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xb6f, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xb70, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0xb73, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0xb74, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xb75, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xb76, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x02}))
	// PC: 0xb78, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xb79, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0xb7a, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb7b, Opcode: ISZERO
	if _, err = charge(ISZERO); err != nil {
		return nil, err, true
	}
	{
		x := stack.peek()
		if x.IsZero() {
			x.SetOne()
		} else {
			x.Clear()
		}
	}
	// PC: 0xb7c, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xc6}))
	// PC: 0xb7f, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xb80, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb81, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0xb83, Opcode: LT
	if _, err = charge(LT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Lt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xb84, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0x9b}))
	// PC: 0xb87, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xb88, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01, 0x00}))
	// PC: 0xb8b, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xb8c, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xb8d, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xb8e, Opcode: DIV
	if _, err = charge(DIV); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Div(&x, y)
	}
	// PC: 0xb8f, Opcode: MUL
	if _, err = charge(MUL); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Mul(&x, y)
	}
	// PC: 0xb90, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xb91, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xb92, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb93, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xb95, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb96, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb97, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xc6}))
	// PC: 0xb9a, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_2971:
	// PC: 0xb9b, Opcode: JUMPDEST
	// jump destination
	// PC: 0xb9c, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xb9d, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xb9e, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xb9f, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xba0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xba2, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xba3, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xba5, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xba7, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xba8, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()

JUMPDEST_2985:
	// PC: 0xba9, Opcode: JUMPDEST
	// jump destination
	// PC: 0xbaa, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xbab, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xbac, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xbad, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xbae, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xbaf, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x01}))
	// PC: 0xbb1, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xbb2, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xbb3, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xbb5, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xbb6, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xbb7, Opcode: DUP4
	if _, err = charge(DUP4); err != nil {
		return nil, err, true
	}
	stack.dup(4)
	// PC: 0xbb8, Opcode: GT
	if _, err = charge(GT); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		if x.Gt(y) {
			y.SetOne()
		} else {
			y.Clear()
		}
	}
	// PC: 0xbb9, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xa9}))
	// PC: 0xbbc, Opcode: JUMPI
	if _, err = charge(JUMPI); err != nil {
		return nil, err, true
	}
	{
		pos, cond := stack.pop(), stack.pop()
		if !cond.IsZero() {
			if !contract.validJumpdest(&pos) {
				return nil, ErrInvalidJump, true
			}
			switch pos.Uint64() {
			case 0xcc:
				goto JUMPDEST_204
			case 0x264:
				goto JUMPDEST_612
			case 0x42a:
				goto JUMPDEST_1066
			case 0x1a1:
				goto JUMPDEST_417
			case 0x229:
				goto JUMPDEST_553
			case 0x279:
				goto JUMPDEST_633
			case 0x3b0:
				goto JUMPDEST_944
			case 0x9d9:
				goto JUMPDEST_2521
			case 0xb18:
				goto JUMPDEST_2840
			case 0x2cc:
				goto JUMPDEST_716
			case 0x6dc:
				goto JUMPDEST_1756
			case 0x7b4:
				goto JUMPDEST_1972
			case 0xb9:
				goto JUMPDEST_185
			case 0x2a0:
				goto JUMPDEST_672
			case 0x2ed:
				goto JUMPDEST_749
			case 0x335:
				goto JUMPDEST_821
			case 0x66d:
				goto JUMPDEST_1645
			case 0xb30:
				goto JUMPDEST_2864
			case 0x1ca:
				goto JUMPDEST_458
			case 0x31a:
				goto JUMPDEST_794
			case 0x370:
				goto JUMPDEST_880
			case 0xab4:
				goto JUMPDEST_2740
			case 0x844:
				goto JUMPDEST_2116
			case 0xbc6:
				goto JUMPDEST_3014
			case 0x266:
				goto JUMPDEST_614
			case 0xc4:
				goto JUMPDEST_196
			case 0x24e:
				goto JUMPDEST_590
			case 0x147:
				goto JUMPDEST_327
			case 0x187:
				goto JUMPDEST_391
			case 0x3df:
				goto JUMPDEST_991
			case 0x57b:
				goto JUMPDEST_1403
			case 0xb9b:
				goto JUMPDEST_2971
			case 0xbce:
				goto JUMPDEST_3022
			case 0xaf:
				goto JUMPDEST_175
			case 0xbe3:
				goto JUMPDEST_3043
			case 0x152:
				goto JUMPDEST_338
			case 0x1ac:
				goto JUMPDEST_428
			case 0x68c:
				goto JUMPDEST_1676
			case 0x139:
				goto JUMPDEST_313
			case 0x2e2:
				goto JUMPDEST_738
			case 0x548:
				goto JUMPDEST_1352
			case 0x573:
				goto JUMPDEST_1395
			case 0x1b4:
				goto JUMPDEST_436
			case 0xb7:
				goto JUMPDEST_183
			case 0x362:
				goto JUMPDEST_866
			case 0x8cf:
				goto JUMPDEST_2255
			case 0xa27:
				goto JUMPDEST_2599
			case 0xb05:
				goto JUMPDEST_2821
			case 0xbdb:
				goto JUMPDEST_3035
			case 0x243:
				goto JUMPDEST_579
			case 0x2f5:
				goto JUMPDEST_757
			case 0x3d2:
				goto JUMPDEST_978
			case 0x3d4:
				goto JUMPDEST_980
			case 0x440:
				goto JUMPDEST_1088
			case 0x4dd:
				goto JUMPDEST_1245
			case 0x556:
				goto JUMPDEST_1366
			case 0xf1:
				goto JUMPDEST_241
			case 0x10c:
				goto JUMPDEST_268
			case 0x1d5:
				goto JUMPDEST_469
			case 0x271:
				goto JUMPDEST_625
			case 0x295:
				goto JUMPDEST_661
			case 0x37b:
				goto JUMPDEST_891
			case 0xba9:
				goto JUMPDEST_2985
			case 0x3ca:
				goto JUMPDEST_970
			default:
				return nil, ErrInvalidJump, true
			}
		}
	}
	// PC: 0xbbd, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xbbe, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xbbf, Opcode: SUB
	if _, err = charge(SUB); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Sub(&x, y)
	}
	// PC: 0xbc0, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x1f}))
	// PC: 0xbc2, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xbc3, Opcode: DUP3
	if _, err = charge(DUP3); err != nil {
		return nil, err, true
	}
	stack.dup(3)
	// PC: 0xbc4, Opcode: ADD
	if _, err = charge(ADD); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.Add(&x, y)
	}
	// PC: 0xbc5, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()

JUMPDEST_3014:
	// PC: 0xbc6, Opcode: JUMPDEST
	// jump destination
	// PC: 0xbc7, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbc8, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbc9, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbca, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbcb, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbcc, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xbcd, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_3022:
	// PC: 0xbce, Opcode: JUMPDEST
	// jump destination
	// PC: 0xbcf, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xbd1, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x0b, 0xdb}))
	// PC: 0xbd4, Opcode: CALLER
	if _, err = charge(CALLER); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes(contract.Caller().Bytes()))
	// PC: 0xbd5, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0xbd6, Opcode: DUP5
	if _, err = charge(DUP5); err != nil {
		return nil, err, true
	}
	stack.dup(5)
	// PC: 0xbd7, Opcode: PUSH2
	if _, err = charge(PUSH2); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x06, 0x8c}))
	// PC: 0xbda, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_3035:
	// PC: 0xbdb, Opcode: JUMPDEST
	// jump destination
	// PC: 0xbdc, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xbdd, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbde, Opcode: SWAP3
	if _, err = charge(SWAP3); err != nil {
		return nil, err, true
	}
	stack.swap3()
	// PC: 0xbdf, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xbe0, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbe1, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xbe2, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		default:
			return nil, ErrInvalidJump, true
		}
	}

JUMPDEST_3043:
	// PC: 0xbe3, Opcode: JUMPDEST
	// jump destination
	// PC: 0xbe4, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x04}))
	// PC: 0xbe6, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xbe8, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xbe9, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xbea, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xbec, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xbed, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xbef, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xbf1, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xbf2, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x20}))
	// PC: 0xbf4, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xbf5, Opcode: DUP1
	if _, err = charge(DUP1); err != nil {
		return nil, err, true
	}
	stack.dup(1)
	// PC: 0xbf6, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xbf8, Opcode: MSTORE
	if _, err = charge(MSTORE); err != nil {
		return nil, err, true
	}
	{
		off, val := stack.pop(), stack.pop()
		memory.Set32(off.Uint64(), &val)
	}
	// PC: 0xbf9, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x40}))
	// PC: 0xbfb, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xbfd, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xbfe, Opcode: PUSH1
	if _, err = charge(PUSH1); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x00}))
	// PC: 0xc00, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xc01, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xc02, Opcode: SWAP2
	if _, err = charge(SWAP2); err != nil {
		return nil, err, true
	}
	stack.swap2()
	// PC: 0xc03, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xc04, Opcode: POP
	if _, err = charge(POP); err != nil {
		return nil, err, true
	}
	_ = stack.pop()
	// PC: 0xc05, Opcode: SLOAD
	if _, err = charge(SLOAD); err != nil {
		return nil, err, true
	}
	{
		loc := stack.peek()
		val := evm.StateDB.GetState(contract.Address(), loc.Bytes32())
		loc.SetBytes(val.Bytes())
	}
	// PC: 0xc06, Opcode: DUP2
	if _, err = charge(DUP2); err != nil {
		return nil, err, true
	}
	stack.dup(2)
	// PC: 0xc07, Opcode: JUMP
	if _, err = charge(JUMP); err != nil {
		return nil, err, true
	}
	{
		pos := stack.pop()
		if !contract.validJumpdest(&pos) {
			return nil, ErrInvalidJump, true
		}
		switch pos.Uint64() {
		case 0x139:
			goto JUMPDEST_313
		case 0x2e2:
			goto JUMPDEST_738
		case 0x548:
			goto JUMPDEST_1352
		case 0x573:
			goto JUMPDEST_1395
		case 0x1b4:
			goto JUMPDEST_436
		case 0xb7:
			goto JUMPDEST_183
		case 0x362:
			goto JUMPDEST_866
		case 0x8cf:
			goto JUMPDEST_2255
		case 0xa27:
			goto JUMPDEST_2599
		case 0xb05:
			goto JUMPDEST_2821
		case 0xbdb:
			goto JUMPDEST_3035
		case 0x243:
			goto JUMPDEST_579
		case 0x2f5:
			goto JUMPDEST_757
		case 0x3d2:
			goto JUMPDEST_978
		case 0x3d4:
			goto JUMPDEST_980
		case 0x440:
			goto JUMPDEST_1088
		case 0x4dd:
			goto JUMPDEST_1245
		case 0x556:
			goto JUMPDEST_1366
		case 0xf1:
			goto JUMPDEST_241
		case 0x10c:
			goto JUMPDEST_268
		case 0x1d5:
			goto JUMPDEST_469
		case 0x271:
			goto JUMPDEST_625
		case 0x295:
			goto JUMPDEST_661
		case 0x37b:
			goto JUMPDEST_891
		case 0xba9:
			goto JUMPDEST_2985
		case 0x3ca:
			goto JUMPDEST_970
		case 0xcc:
			goto JUMPDEST_204
		case 0x264:
			goto JUMPDEST_612
		case 0x42a:
			goto JUMPDEST_1066
		case 0x1a1:
			goto JUMPDEST_417
		case 0x229:
			goto JUMPDEST_553
		case 0x279:
			goto JUMPDEST_633
		case 0x3b0:
			goto JUMPDEST_944
		case 0x9d9:
			goto JUMPDEST_2521
		case 0xb18:
			goto JUMPDEST_2840
		case 0x2cc:
			goto JUMPDEST_716
		case 0x6dc:
			goto JUMPDEST_1756
		case 0x7b4:
			goto JUMPDEST_1972
		case 0xb9:
			goto JUMPDEST_185
		case 0x2a0:
			goto JUMPDEST_672
		case 0x2ed:
			goto JUMPDEST_749
		case 0x335:
			goto JUMPDEST_821
		case 0x66d:
			goto JUMPDEST_1645
		case 0xb30:
			goto JUMPDEST_2864
		case 0x1ca:
			goto JUMPDEST_458
		case 0x31a:
			goto JUMPDEST_794
		case 0x370:
			goto JUMPDEST_880
		case 0xab4:
			goto JUMPDEST_2740
		case 0x844:
			goto JUMPDEST_2116
		case 0xbc6:
			goto JUMPDEST_3014
		case 0x266:
			goto JUMPDEST_614
		case 0xc4:
			goto JUMPDEST_196
		case 0x24e:
			goto JUMPDEST_590
		case 0x147:
			goto JUMPDEST_327
		case 0x187:
			goto JUMPDEST_391
		case 0x3df:
			goto JUMPDEST_991
		case 0x57b:
			goto JUMPDEST_1403
		case 0xb9b:
			goto JUMPDEST_2971
		case 0xbce:
			goto JUMPDEST_3022
		case 0xaf:
			goto JUMPDEST_175
		case 0xbe3:
			goto JUMPDEST_3043
		case 0x152:
			goto JUMPDEST_338
		case 0x1ac:
			goto JUMPDEST_428
		case 0x68c:
			goto JUMPDEST_1676
		default:
			return nil, ErrInvalidJump, true
		}
	}
	// PC: 0xc08, Opcode: STOP
	if _, err = charge(STOP); err != nil {
		return nil, err, true
	}
	return nil, nil, true
	// PC: 0xc09, Opcode: LOG1
	if _, err = charge(LOG1); err != nil {
		return nil, err, true
	}
	{
		mstart, msize := stack.pop(), stack.pop()
		size := int(1)
		topics := make([]common.Hash, size)
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = addr.Bytes32()
		}
		d := memory.GetCopy(mstart.Uint64(), msize.Uint64())
		evm.StateDB.AddLog(&typespkg.Log{Address: contract.Address(), Topics: topics, Data: d, BlockNumber: evm.Context.BlockNumber.Uint64()})
	}
	// PC: 0xc0a, Opcode: PUSH6
	if _, err = charge(PUSH6); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x62, 0x7a, 0x7a, 0x72, 0x30, 0x58}))
	// PC: 0xc11, Opcode: KECCAK256
	if _, err = charge(KECCAK256); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.peek()
		data := memory.GetPtr(off.Uint64(), size.Uint64())
		hasher := crypto.NewKeccakState()
		hasher.Write(data)
		var out [32]byte
		hasher.Read(out[:])
		size.SetBytes(out[:])
	}
	// PC: 0xc12, Opcode: RETURN
	if _, err = charge(RETURN); err != nil {
		return nil, err, true
	}
	{
		off, size := stack.pop(), stack.pop()
		ret = append([]byte(nil), memory.GetPtr(off.Uint64(), size.Uint64())...)
		return ret, nil, true
	}
	// PC: 0xc13, Opcode: AND
	if _, err = charge(AND); err != nil {
		return nil, err, true
	}
	{
		x, y := stack.pop(), stack.peek()
		y.And(&x, y)
	}
	// PC: 0xc14, Opcode: SWAP1
	if _, err = charge(SWAP1); err != nil {
		return nil, err, true
	}
	stack.swap1()
	// PC: 0xc15, Opcode: BALANCE
	if _, err = charge(BALANCE); err != nil {
		return nil, err, true
	}
	{
		slot := stack.peek()
		addr := common.Address(slot.Bytes20())
		slot.Set(evm.StateDB.GetBalance(addr))
	}
	// PC: 0xc16, Opcode: DUP6
	if _, err = charge(DUP6); err != nil {
		return nil, err, true
	}
	stack.dup(6)
	// PC: 0xc17, Opcode: GASLIMIT
	if _, err = charge(GASLIMIT); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetUint64(evm.Context.GasLimit))
	// PC: 0xc18, Opcode: SIGNEXTEND
	// Unimplemented opcode SIGNEXTEND -> fallback to interpreter
	return nil, nil, false
	// PC: 0xc19, Opcode: PUSH15
	if _, err = charge(PUSH15); err != nil {
		return nil, err, true
	}
	stack.push(new(uint256.Int).SetBytes([]byte{0x96, 0x71, 0x0c, 0xce, 0x10, 0x1f, 0xd2, 0xb4, 0xb4, 0x7d, 0x5b, 0x70, 0xdc, 0x11, 0xe0}))
	// PC: 0xc29, Opcode: STOP
	if _, err = charge(STOP); err != nil {
		return nil, err, true
	}
	return nil, nil, true
}
