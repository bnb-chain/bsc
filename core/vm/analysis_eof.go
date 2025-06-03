// Copyright 2024 The go-ethereum Authors
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

// eofCodeBitmap collects data locations in code.
func eofCodeBitmap(code []byte) bitvec {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will push zeroes onto the
	// bitvector outside the bounds of the actual code.
	bits := make(bitvec, len(code)/8+1+4)
	return eofCodeBitmapInternal(code, bits)
}

// eofCodeBitmapInternal is the internal implementation of codeBitmap for EOF
// code validation.
func eofCodeBitmapInternal(code, bits bitvec) bitvec {
	for pc := uint64(0); pc < uint64(len(code)); {
		var (
			op      = OpCode(code[pc])
			numbits uint16
		)
		pc++

		// handle super instruction.
		step, processed := codeBitmapForSI(code, pc, op, &bits)
		if processed {
			pc += step
			continue
		}

		if op == RJUMPV {
			// RJUMPV is unique as it has a variable sized operand.
			// The total size is determined by the count byte which
			// immediate follows RJUMPV. Truncation will be caught
			// in other validation steps -- for now, just return a
			// valid bitmap for as much of the code as is
			// available.
			end := uint64(len(code))
			if pc >= end {
				// Count missing, no more bits to mark.
				return bits
			}
			numbits = uint16(code[pc])*2 + 3
			if pc+uint64(numbits) > end {
				// Jump table is truncated, mark as many bits
				// as possible.
				numbits = uint16(end - pc)
			}
		} else {
			numbits = uint16(Immediates(op))
			if numbits == 0 {
				continue
			}
		}

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

func codeBitmapForSI(code []byte, pc uint64, op OpCode, bits *bitvec) (step uint64, processed bool) {
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
	default:
		return 0, false
	}
	return step, processed
}
