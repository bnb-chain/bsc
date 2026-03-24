// Copyright 2014 The go-ethereum Authors
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

const (
	set2BitsMask = uint16(0b11)
	set3BitsMask = uint16(0b111)
	set4BitsMask = uint16(0b1111)
	set5BitsMask = uint16(0b1_1111)
	set6BitsMask = uint16(0b11_1111)
	set7BitsMask = uint16(0b111_1111)
)

// BitVec is a bit vector which maps bytes in a program.
// An unset bit means the byte is an opcode, a set bit means
// it's data (i.e. argument of PUSHxx).
type BitVec []byte

func (bits BitVec) set1(pos uint64) {
	bits[pos/8] |= 1 << (pos % 8)
}

func (bits BitVec) setN(flag uint16, pos uint64) {
	a := flag << (pos % 8)
	bits[pos/8] |= byte(a)
	if b := byte(a >> 8); b != 0 {
		bits[pos/8+1] = b
	}
}

func (bits BitVec) set8(pos uint64) {
	a := byte(0xFF << (pos % 8))
	bits[pos/8] |= a
	bits[pos/8+1] = ^a
}

func (bits BitVec) set16(pos uint64) {
	a := byte(0xFF << (pos % 8))
	bits[pos/8] |= a
	bits[pos/8+1] = 0xFF
	bits[pos/8+2] = ^a
}

// codeSegment checks if the position is in a code segment.
func (bits *BitVec) codeSegment(pos uint64) bool {
	return (((*bits)[pos/8] >> (pos % 8)) & 1) == 0
}

// codeBitmap collects data locations in code.
func codeBitmap(code []byte) BitVec {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will set bits on the
	// bitvector outside the bounds of the actual code.
	bits := make(BitVec, len(code)/8+1+4)
	return codeBitmapInternal(code, bits)
}

// codeBitmapInternal is the internal implementation of codeBitmap.
// It exists for the purpose of being able to run benchmark tests
// without dynamic allocations affecting the results.
func codeBitmapInternal(code, bits BitVec) BitVec {
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])
		pc++
		if int8(op) < int8(PUSH1) { // If not PUSH (the int8(op) > int(PUSH32) is always false).
			continue
		}
		numbits := op - PUSH1 + 1
		if numbits >= 8 {
			for ; numbits >= 16; numbits -= 16 {
				bits.set16(pc)
				pc += 16
			}
			for ; numbits >= 8; numbits -= 8 {
				bits.set8(pc)
				pc += 8
			}
		}
		switch numbits {
		case 1:
			bits.set1(pc)
			pc += 1
		case 2:
			bits.setN(set2BitsMask, pc)
			pc += 2
		case 3:
			bits.setN(set3BitsMask, pc)
			pc += 3
		case 4:
			bits.setN(set4BitsMask, pc)
			pc += 4
		case 5:
			bits.setN(set5BitsMask, pc)
			pc += 5
		case 6:
			bits.setN(set6BitsMask, pc)
			pc += 6
		case 7:
			bits.setN(set7BitsMask, pc)
			pc += 7
		}
	}
	return bits
}

// codeBitmap collects data locations in code.
func codeBitmapWhitSI(code []byte) BitVec {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will set bits on the
	// bitvector outside the bounds of the actual code.
	bits := make(BitVec, len(code)/8+1+4)
	return codeBitmapInternalWhitSI(code, bits)
}

// codeBitmapInternal is the internal implementation of codeBitmap.
// It exists for the purpose of being able to run benchmark tests
// without dynamic allocations affecting the results.
func codeBitmapInternalWhitSI(code, bits BitVec) BitVec {
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])
		pc++

		// handle super instruction.
		step, processed := codeBitmapForSI(code, pc, op, &bits)
		if processed {
			pc += step
			continue
		}

		if int8(op) < int8(PUSH1) { // If not PUSH (the int8(op) > int(PUSH32) is always false).
			continue
		}
		numbits := op - PUSH1 + 1
		if numbits >= 8 {
			for ; numbits >= 16; numbits -= 16 {
				bits.set16(pc)
				pc += 16
			}
			for ; numbits >= 8; numbits -= 8 {
				bits.set8(pc)
				pc += 8
			}
		}
		switch numbits {
		case 1:
			bits.set1(pc)
			pc += 1
		case 2:
			bits.setN(set2BitsMask, pc)
			pc += 2
		case 3:
			bits.setN(set3BitsMask, pc)
			pc += 3
		case 4:
			bits.setN(set4BitsMask, pc)
			pc += 4
		case 5:
			bits.setN(set5BitsMask, pc)
			pc += 5
		case 6:
			bits.setN(set6BitsMask, pc)
			pc += 6
		case 7:
			bits.setN(set7BitsMask, pc)
			pc += 7
		}
	}
	return bits
}

func codeBitmapForSI(code []byte, pc uint64, op OpCode, bits *BitVec) (step uint64, processed bool) {
	// pc points to the data pointer for push, or the next op for opcode
	// bits marks the data bytes pointed by [pc]
	switch op {
	case Push2Jump, Push2JumpI:
		bits.setN(set2BitsMask, pc)
		step = 3
		processed = true
	case Push1Push1:
		bits.set1(pc)
		bits.set1(pc + 2)
		step = 3
		processed = true
	case Push1Add, Push1Shl, Push1Dup1:
		bits.set1(pc)
		step = 2
		processed = true
	case JumpIfZero:
		bits.setN(set2BitsMask, pc+1)
		step = 4
		processed = true
	case IsZeroPush2:
		bits.setN(set2BitsMask, pc+1)
		step = 3
		processed = true
	case Dup2MStorePush1Add:
		bits.set1(pc + 2)
		step = 4
		processed = true
	case Dup1Push4EqPush2:
		bits.setN(set4BitsMask, pc+1)
		bits.setN(set2BitsMask, pc+7)
		step = 9
		processed = true
	case Push1CalldataloadPush1ShrDup1Push4GtPush2:
		bits.set1(pc)
		bits.set1(pc + 3)
		bits.setN(set4BitsMask, pc+7)
		bits.setN(set2BitsMask, pc+13)
		step = 15
		processed = true
	case Push1Push1Push1SHLSub:
		bits.set1(pc)
		bits.set1(pc + 2)
		bits.set1(pc + 4)
		step = 7
		processed = true
	case Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT:
		bits.set1(pc + 1)
		step = 12
		processed = true
	case Swap2Swap1Dup3SubSwap2Dup3GtPush2:
		bits.setN(set2BitsMask, pc+7)
		step = 9
		processed = true
	case SubSLTIsZeroPush2:
		bits.setN(set2BitsMask, pc+3)
		step = 5
		processed = true
	default:
		return 0, false
	}
	return step, processed
}
