package compiler

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// OptimizationPhase defines which types of instructions can be optimized
type OptimizationPhase int

const (
	PhaseBasicMath    OptimizationPhase = 1 // Phase 1: Pure math and comparison operations
	PhasePeepHole3Ops OptimizationPhase = 2 // Phase 2: Three-operand peephole optimizations (ADDMOD, MULMOD)
	PhaseStackOps     OptimizationPhase = 3 // Phase 3: Stack operations (DUP, SWAP)
	PhaseMemoryOps    OptimizationPhase = 4 // Phase 4: Memory and storage operations
)

// Current optimization phase - can be configured
var currentOptimizationPhase OptimizationPhase = PhaseBasicMath

// getOptimizableOps returns the set of operations that can be optimized in the current phase
func getOptimizableOps() map[MirOperation]bool {
	optimizableOps := make(map[MirOperation]bool)

	// Phase 1: Pure math and comparison operations - always safe to optimize
	if currentOptimizationPhase >= PhaseBasicMath {
		// Arithmetic operations (0x01-0x0b) - excluding 3-operand operations
		mathOps := []MirOperation{
			MirADD, MirMUL, MirSUB, MirDIV, MirSDIV, MirMOD, MirSMOD,
			MirEXP, MirSIGNEXT,
		}
		for _, op := range mathOps {
			optimizableOps[op] = true
		}

		// Comparison operations (0x10-0x1d)
		comparisonOps := []MirOperation{
			MirLT, MirGT, MirSLT, MirSGT, MirEQ, MirISZERO,
		}
		for _, op := range comparisonOps {
			optimizableOps[op] = true
		}

		// Bitwise operations (0x16-0x1d)
		bitwiseOps := []MirOperation{
			MirAND, MirOR, MirXOR, MirNOT, MirBYTE, MirSHL, MirSHR, MirSAR,
		}
		for _, op := range bitwiseOps {
			optimizableOps[op] = true
		}

		// Cryptographic operations
		optimizableOps[MirKECCAK256] = true
	}

	// Phase 2: Three-operand peephole optimizations
	if currentOptimizationPhase >= PhasePeepHole3Ops {
		// Operations requiring 3 operands
		threeOpOps := []MirOperation{
			MirADDMOD, MirMULMOD,
		}
		for _, op := range threeOpOps {
			optimizableOps[op] = true
		}
	}

	// Phase 3: Stack operations
	if currentOptimizationPhase >= PhaseStackOps {
		// DUP operations (0x80-0x8f)
		for i := MirDUP1; i <= MirDUP16; i++ {
			optimizableOps[i] = true
		}
		// SWAP operations (0x90-0x9f)
		for i := MirSWAP1; i <= MirSWAP16; i++ {
			optimizableOps[i] = true
		}
	}

	// Phase 4: Memory and storage operations
	if currentOptimizationPhase >= PhaseMemoryOps {
		memoryOps := []MirOperation{
			MirMLOAD, MirMSTORE, MirMSTORE8, MirMSIZE,
			MirSLOAD, MirSSTORE, MirTLOAD, MirTSTORE,
		}
		for _, op := range memoryOps {
			optimizableOps[op] = true
		}
	}

	return optimizableOps
}

// isOptimizable checks if a MIR operation can be optimized in the current phase
func isOptimizable(op MirOperation) bool {
	optimizableOps := getOptimizableOps()
	return optimizableOps[op]
}

// SetOptimizationPhase sets the current optimization phase
func SetOptimizationPhase(phase OptimizationPhase) {
	currentOptimizationPhase = phase
}

// GetOptimizationPhase returns the current optimization phase
func GetOptimizationPhase() OptimizationPhase {
	return currentOptimizationPhase
}

// MIR is register based intermediate representation
type MIR struct {
	op      MirOperation
	oprands []*Value
	meta    []byte
	pc      *uint // Program counter of the original instruction (optional)
	idx     int   // Index within its basic block, set by appendMIR
	// EVM mapping metadata (set during CFG build)
	evmPC uint // byte offset of the originating EVM opcode
	evmOp byte // originating EVM opcode byte value
	// Optional auxiliary MIR attached for diagnostics (e.g., original EVM op at invalid jump target)
	aux *MIR
	// For PHI nodes only: the stack slot index this PHI represents (0 = top of stack)
	phiStackIndex int
	// Pre-encoded operand info to avoid runtime eval
	opKinds       []byte         // 0=const,1=def,2=fallback
	opConst       []*uint256.Int // if const
	opDefIdx      []int          // if def (index into results slice)
	genStackDepth int            // stack depth at generation time (for debugging/dumps)
}

// Op returns the MIR operation code
func (m *MIR) Op() MirOperation { return m.op }

func (m *MIR) Result() *Value {
	if m.op == MirNOP {
		return nil
	}
	return newValue(Variable, m, nil, nil)
}

// GenStackDepth reports the stack depth at MIR generation time (if recorded)
func (m *MIR) GenStackDepth() int { return m.genStackDepth }

// EvmPC returns the byte offset of the corresponding EVM opcode
func (m *MIR) EvmPC() uint { return m.evmPC }

// EvmOp returns the corresponding EVM opcode byte
func (m *MIR) EvmOp() byte { return m.evmOp }

// PhiStackIndex returns the stack slot index for PHI nodes (0=top). -1 if not set.
func (m *MIR) PhiStackIndex() int { return m.phiStackIndex }

// Aux returns the optional auxiliary MIR attached for diagnostics
func (m *MIR) Aux() *MIR { return m.aux }

// Operands returns the operands of this MIR instruction.
// The returned slice should be treated as read-only by callers.
func (m *MIR) Operands() []*Value { return m.oprands }

// OperandDebugStrings returns a best-effort human-readable rendering of operands
// suitable for logging/tracing.
func (m *MIR) OperandDebugStrings() []string {
	if len(m.oprands) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.oprands))
	for _, v := range m.oprands {
		if v == nil {
			out = append(out, "nil")
			continue
		}
		switch v.kind {
		case Konst:
			if v.u != nil {
				out = append(out, "const:0x"+v.u.Hex())
			} else if len(v.payload) == 0 {
				out = append(out, "const:0x0")
			} else {
				out = append(out, "const:0x"+uint256.NewInt(0).SetBytes(v.payload).Hex())
			}
		case Arguments:
			out = append(out, "arg")
		case Variable:
			if v.def != nil {
				out = append(out, "var-")
				out = append(out, fmt.Sprintf("def@%d", v.def.idx))
				out = append(out, fmt.Sprintf("def: evm_pc=%d evm_op=0x%x", v.def.evmPC, v.def.evmOp))
			} else {
				out = append(out, "var-no-def")
			}
		default:
			out = append(out, "unknown")
		}
	}
	return out
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

// newNopMIRWithPC creates a NOP MIR with program counter information
func newNopMIRWithPC(operation MirOperation, original_opnds []*Value, pc uint) *MIR {
	mir := newNopMIR(operation, original_opnds)
	mir.pc = &pc
	return mir
}

// Package-local variables used during CFG build to annotate MIR with EVM mapping.
// These are set in opcodeParser.buildBasicBlock before each MIR creation.
var currentEVMBuildPC uint
var currentEVMBuildOp byte

func newUnaryOpMIR(operation MirOperation, opnd *Value, stack *ValueStack) *MIR {
	if doPeepHole(operation, opnd, nil, stack, nil) {
		return newNopMIR(operation, []*Value{opnd})
	}
	mir := new(MIR)
	mir.op = operation
	opnd.use = append(opnd.use, mir)
	mir.oprands = []*Value{opnd}
	// If the operand is a live-in from a parent BB, tag the defining MIR as global at build time
	if opnd != nil && opnd.liveIn && opnd.def != nil {
		// No direct global table here; interpreter will use globalResults by def pointer.
		// We preserve liveIn on Value to signal cross-BB origin to later passes if needed.
	}
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
	if (opnd1 != nil && opnd1.liveIn && opnd1.def != nil) || (opnd2 != nil && opnd2.liveIn && opnd2.def != nil) {
		// Marker only; interpreter resolves by def pointer.
	}
	return mir
}

// newTernaryOpMIR creates a MIR instruction for 3-operand operations
func newTernaryOpMIR(operation MirOperation, opnd1 *Value, opnd2 *Value, opnd3 *Value, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = operation
	opnd1.use = append(opnd1.use, mir)
	opnd2.use = append(opnd2.use, mir)
	opnd3.use = append(opnd3.use, mir)
	mir.oprands = []*Value{opnd1, opnd2, opnd3}
	return mir
}

// doPeepHole3Ops performs peephole optimizations for 3-operand operations
func doPeepHole3Ops(operation MirOperation, opnd1 *Value, opnd2 *Value, opnd3 *Value, stack *ValueStack, memoryAccessor *MemoryAccessor) bool {
	if opnd1 == nil || opnd2 == nil || opnd3 == nil {
		return false
	}

	// Only optimize if all operands are constants
	if opnd1.kind != Konst || opnd2.kind != Konst || opnd3.kind != Konst {
		return false
	}

	// Check if this operation is optimizable in current phase
	if !isOptimizable(operation) {
		return false
	}

	optimized := false
	val1 := uint256.NewInt(0).SetBytes(opnd1.payload)
	val2 := uint256.NewInt(0).SetBytes(opnd2.payload)
	val3 := uint256.NewInt(0).SetBytes(opnd3.payload)

	switch operation {
	case MirADDMOD:
		// ADDMOD: (val1 + val2) % val3
		if !val3.IsZero() { // Avoid division by zero
			temp := uint256.NewInt(0).Add(val1, val2)
			val1 = temp.Mod(temp, val3)
			optimized = true
		}
	case MirMULMOD:
		// MULMOD: (val1 * val2) % val3
		if !val3.IsZero() { // Avoid division by zero
			temp := uint256.NewInt(0).Mul(val1, val2)
			val1 = temp.Mod(temp, val3)
			optimized = true
		}
	}

	if optimized && val1 != nil {
		// Create a new constant value with the optimized result
		payload := val1.Bytes()
		// Handle special case where Bytes() returns empty slice for zero
		if len(payload) == 0 && val1.IsZero() {
			payload = []byte{0x00}
		}
		newVal := newValue(Konst, nil, nil, payload)
		stack.push(newVal)
	}

	return optimized
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
					val1.SetUint64(0)
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
					val1.SetUint64(0)
				}
			case MirGT:
				isGt := val1.Gt(val2)
				if isGt {
					val1.SetOne()
				} else {
					val1.SetUint64(0)
				}
			case MirSLT:
				isSlt := val1.Slt(val2)
				if isSlt {
					val1.SetOne()
				} else {
					val1.SetUint64(0)
				}
			case MirSGT:
				isSgt := val1.Sgt(val2)
				if isSgt {
					val1.SetOne()
				} else {
					val1.SetUint64(0)
				}
			case MirEQ:
				isEq := val1.Eq(val2)
				if isEq {
					val1.SetOne()
				} else {
					val1.SetUint64(0)
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
				// EVM SHL semantics: result = value << shift
				// Stack order: [ ... shift, value ] (top-first pop order)
				// opnd1 = shift, opnd2 = value
				val1 = val2.Lsh(val2, uint(val1.Uint64()))
			case MirSHR:
				// Logical right shift: result = value >> shift
				val1 = val2.Rsh(val2, uint(val1.Uint64()))
			case MirSAR:
				// Arithmetic right shift: result = value >>> shift (sign-propagating)
				val1 = val2.SRsh(val2, uint(val1.Uint64()))
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
		payload := val1.Bytes()
		// Handle special case where Bytes() returns empty slice for zero
		if len(payload) == 0 && val1.IsZero() {
			payload = []byte{0x00}
		}
		newVal := newValue(Konst, nil, nil, payload)
		stack.push(newVal)
	}

	return optimized
}

func mirTryLoadFromMemory(offset *uint256.Int, size *uint256.Int, mAccessor *MemoryAccessor) Value {
	// This is a simplified version - in a full implementation, you would
	// check if the memory location has been written to and return the value
	return Value{kind: Variable}
}

// constantToPushBytecode converts a constant value to PUSH bytecode
func constantToPushBytecode(payload []byte) []byte {
	if len(payload) == 0 {
		return []byte{byte(PUSH1), 0x00}
	}

	// Remove leading zeros
	trimmed := trimLeadingZeros(payload)
	if len(trimmed) == 0 {
		return []byte{byte(PUSH1), 0x00}
	}

	// Determine appropriate PUSH instruction
	pushSize := len(trimmed)
	if pushSize > 32 {
		pushSize = 32 // Maximum PUSH32
	}

	result := []byte{byte(PUSH1) + byte(pushSize-1)}
	result = append(result, trimmed[:pushSize]...)

	return result
}

// trimLeadingZeros removes leading zero bytes from a byte slice
func trimLeadingZeros(data []byte) []byte {
	for i, b := range data {
		if b != 0 {
			return data[i:]
		}
	}
	return []byte{}
}
