package compiler

import (
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// MIR is register based intermediate representation
type MIR struct {
	op      MirOperation
	oprands []*Value
	meta    []byte
}

func (m *MIR) Result() *Value {
	if m.op == MirNOP {
		return nil
	}
	return newValue(Variable, m, nil, nil)
}

func newVoidMIR(operation MirOperation) *MIR {
	mir := new(MIR)
	mir.op = operation
	mir.oprands = nil
	return mir
}

func newNopMIR(operation MirOperation, original_opnds []*Value) *MIR {
	mir := new(MIR)
	mir.op = MirNOP
	mir.oprands = original_opnds
	mir.meta = []byte{byte(operation)}
	return mir
}

func newUnaryOpMIR(operation MirOperation, opnd *Value, stack *ValueStack) *MIR {
	if doPeepHole(operation, opnd, nil, stack, nil) {
		return newNopMIR(operation, []*Value{opnd})
	}
	mir := new(MIR)
	mir.op = operation
	opnd.use = append(opnd.use, mir)
	mir.oprands = []*Value{opnd}
	return mir
}

func newBinaryOpMIR(operation MirOperation, opnd1 *Value, opnd2 *Value, stack *ValueStack) *MIR {
	if doPeepHole(operation, opnd1, opnd2, stack, nil) {
		return newNopMIR(operation, []*Value{opnd1, opnd2})
	}
	mir := new(MIR)
	mir.op = operation
	opnd1.use = append(opnd1.use, mir)
	opnd2.use = append(opnd2.use, mir)
	mir.oprands = []*Value{opnd1, opnd2}
	return mir
}

func doPeepHole(operation MirOperation, opnd1 *Value, opnd2 *Value, stack *ValueStack, memoryAccessoraccessor *MemoryAccessor) bool {
	optimized := true
	var val1 *uint256.Int

	if opnd1.kind == Konst {
		val1 = uint256.NewInt(0).SetBytes(opnd1.payload)
		if opnd2 == nil {
			switch operation {
			case MirNOT:
				val1 = val1.Not(val1)
			case MirISZERO:
				isZero := val1.IsZero()
				if isZero {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			}
		} else if opnd2.kind == Konst {
			val2 := uint256.NewInt(0).SetBytes(opnd2.payload)
			switch operation {
			case MirADD:
				val1 = val1.Add(val1, val2)
			case MirMUL:
				val1 = val1.Mul(val1, val2)
			case MirSUB:
				val1 = val1.Sub(val1, val2)
			case MirDIV:
				val1 = val1.Div(val1, val2)
			case MirSDIV:
				val1 = val1.SDiv(val1, val2)
			case MirMOD:
				val1 = val1.Mod(val1, val2)
			case MirSMOD:
				val1 = val1.SMod(val1, val2)
			case MirEXP:
				val1 = val1.Exp(val1, val2)
			case MirSIGNEXT:
				val1 = val1.ExtendSign(val2, val1)
			case MirLT:
				isLt := val1.Lt(val2)
				if isLt {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			case MirGT:
				isGt := val1.Gt(val2)
				if isGt {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			case MirSLT:
				isSlt := val1.Slt(val2)
				if isSlt {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			case MirSGT:
				isSgt := val1.Sgt(val2)
				if isSgt {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			case MirEQ:
				isEq := val1.Eq(val2)
				if isEq {
					val1.SetOne()
				} else {
					val1.Clear()
				}
			case MirAND:
				val1 = val1.And(val1, val2)
			case MirOR:
				val1 = val1.Or(val1, val2)
			case MirXOR:
				val1 = val1.Xor(val1, val2)
			case MirBYTE:
				val1 = val1.Byte(val2)
			case MirSHL:
				val1 = val1.Lsh(val1, uint(val2.Uint64()))
			case MirSHR:
				val1 = val1.Rsh(val1, uint(val2.Uint64()))
			case MirSAR:
				val1 = val1.SRsh(val1, uint(val2.Uint64()))
			case MirKECCAK256:
				// KECCAK256 takes offset (val1) and size (val2) as operands
				// Check if the memory range is known and can be loaded
				if memoryAccessoraccessor != nil {
					// Try to load data from memory at the specified offset and size
					memData := memoryAccessoraccessor.getValueWithOffset(val1, val2)
					if memData.kind == Konst && len(memData.payload) > 0 {
						// Calculate Keccak256 hash of the known data
						hash := crypto.Keccak256(memData.payload)
						val1 = uint256.NewInt(0).SetBytes(hash)
					} else {
						optimized = false
					}
				} else {
					optimized = false
				}
			default:
				optimized = false
			}
		} else {
			optimized = false
		}
	} else {
		optimized = false
	}

	if optimized && val1 != nil {
		// Create a new constant value with the optimized result
		newVal := newValue(Konst, nil, nil, val1.Bytes())
		stack.push(newVal)
	}

	return optimized
}

func mirTryLoadFromMemory(offset *uint256.Int, size *uint256.Int, mAccessor *MemoryAccessor) Value {
	// This is a simplified version - in a full implementation, you would
	// check if the memory location has been written to and return the value
	return Value{kind: Variable}
}
