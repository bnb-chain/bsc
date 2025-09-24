package compiler

import (
	"github.com/holiman/uint256"
)

// bitmap is a bit map which maps basicblock in to a bit
type bitmap []byte

func (bits *bitmap) ensure(pos uint64) {
	need := int(pos/8) + 1
	if need <= len(*bits) {
		return
	}
	*bits = append(*bits, make([]byte, need-len(*bits))...)
}

func (bits *bitmap) set1(pos uint64) {
	bits.ensure(pos)
	(*bits)[pos/8] |= 1 << (pos % 8)
}

func (bits *bitmap) setN(flag uint16, pos uint64) {
	bits.ensure(pos + 8)
	a := flag << (pos % 8)
	(*bits)[pos/8] |= byte(a)
	if b := byte(a >> 8); b != 0 {
		(*bits)[pos/8+1] = b
	}
}

// checks if the position is in a code segment.
func (bits *bitmap) isBitSet(pos uint64) bool {
	idx := int(pos / 8)
	if idx >= len(*bits) {
		return false
	}
	return (((*bits)[idx] >> (pos % 8)) & 1) == 1
}

type MIRBasicBlock struct {
	blockNum       uint
	firstPC        uint
	lastPC         uint
	parentsBitmap  *bitmap
	childrenBitmap *bitmap
	parents        []*MIRBasicBlock
	children       []*MIRBasicBlock
	instructions   []*MIR
	pos            int
}

func (b *MIRBasicBlock) Size() uint {
	return uint(len(b.instructions))
}

// Instructions returns the MIR instructions within this basic block
func (b *MIRBasicBlock) Instructions() []*MIR {
	return b.instructions
}

func (b *MIRBasicBlock) FirstPC() uint {
	return b.firstPC
}

func (b *MIRBasicBlock) SetFirstPC(firstPC uint) {
	b.firstPC = firstPC
}

func (b *MIRBasicBlock) LastPC() uint {
	return b.lastPC
}

func (b *MIRBasicBlock) SetLastPC(lastPC uint) {
	b.lastPC = lastPC
}

func (b *MIRBasicBlock) Parents() []*MIRBasicBlock {
	return b.parents
}

func (b *MIRBasicBlock) SetParents(parents []*MIRBasicBlock) {
	for _, parent := range parents {
		if !b.parentsBitmap.isBitSet(uint64(parent.blockNum)) {
			b.parentsBitmap.set1(uint64(parent.blockNum))
			b.parents = append(b.parents, parent)
		}
	}
}

func (b *MIRBasicBlock) Children() []*MIRBasicBlock {
	return b.children
}

func (b *MIRBasicBlock) SetChildren(children []*MIRBasicBlock) {
	for _, child := range children {
		if !b.childrenBitmap.isBitSet(uint64(child.blockNum)) {
			b.childrenBitmap.set1(uint64(child.blockNum))
			b.children = append(b.children, child)
		}
	}
}

func (b *MIRBasicBlock) CreateVoidMIR(op MirOperation) (mir *MIR) {
	mir = newVoidMIR(op)
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) appendMIR(mir *MIR) *MIR {
	mir.idx = len(b.instructions)
	// Pre-encode operand info to avoid runtime eval costs
	if len(mir.oprands) > 0 {
		mir.opKinds = make([]byte, len(mir.oprands))
		mir.opConst = make([]*uint256.Int, len(mir.oprands))
		mir.opDefIdx = make([]int, len(mir.oprands))
		for i, v := range mir.oprands {
			if v == nil {
				mir.opKinds[i] = 2
				continue
			}
			switch v.kind {
			case Konst:
				mir.opKinds[i] = 0
				mir.opConst[i] = v.u
			case Variable, Arguments:
				mir.opKinds[i] = 1
				if v.def != nil {
					mir.opDefIdx[i] = v.def.idx
				} else {
					mir.opDefIdx[i] = -1
				}
			default:
				mir.opKinds[i] = 2
			}
		}
	}
	b.instructions = append(b.instructions, mir)
	return mir
}

func (b *MIRBasicBlock) CreateUnaryOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd1 := stack.pop()
	mir = newUnaryOpMIR(op, &opnd1, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		stack.push(mir.Result())
	}
	// If mir.op == MirNOP, doPeepHole already pushed the optimized constant to stack

	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBinOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd2 := stack.pop()
	opnd1 := stack.pop()
	mir = newBinaryOpMIR(op, &opnd1, &opnd2, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		stack.push(mir.Result())
	}
	// If mir.op == MirNOP, doPeepHole already pushed the optimized constant to stack

	return b.appendMIR(mir)
}

// CreateTernaryOpMIR creates a MIR instruction for 3-operand operations like ADDMOD, MULMOD
func (b *MIRBasicBlock) CreateTernaryOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd3 := stack.pop() // Modulus (third operand)
	opnd2 := stack.pop() // Second operand
	opnd1 := stack.pop() // First operand

	// Try peephole optimization for 3-operand operations
	if doPeepHole3Ops(op, &opnd1, &opnd2, &opnd3, stack, nil) {
		// Operation was optimized away, create a NOP MIR for tracking
		mir = newNopMIR(op, []*Value{&opnd1, &opnd2, &opnd3})
		return b.appendMIR(mir)
	}

	// Create regular ternary operation MIR
	mir = newTernaryOpMIR(op, &opnd1, &opnd2, &opnd3, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		stack.push(mir.Result())
	}
	// If mir.op == MirNOP, doPeepHole3Ops already pushed the optimized constant to stack

	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBinOpMIRWithMA(op MirOperation, stack *ValueStack, accessor *MemoryAccessor) *MIR {
	opnd2 := stack.pop()
	opnd1 := stack.pop()

	// Record memory reads when applicable
	if accessor != nil {
		switch op {
		case MirKECCAK256:
			// For KECCAK256, operands are [offset, size] where
			// stack before pops is [..., size, offset(top)] so
			// opnd2 = offset, opnd1 = size
			accessor.recordLoad(opnd2, opnd1)
		}
	}

	// Peephole with memory knowledge, e.g., KECCAK256 over known bytes
	if accessor != nil && op == MirKECCAK256 {
		// Ensure operand order [offset, size] -> (opnd2, opnd1)
		if doPeepHole(op, &opnd2, &opnd1, stack, accessor) {
			mir := newNopMIR(op, []*Value{&opnd2, &opnd1})
			return b.appendMIR(mir)
		}
	}
	// For KECCAK256 ensure operands are [offset, size]
	if op == MirKECCAK256 {
		// operands: [offset, size] -> (opnd2, opnd1)
		mir := newBinaryOpMIR(op, &opnd2, &opnd1, stack)
		opnd2.use = append(opnd2.use, mir)
		opnd1.use = append(opnd1.use, mir)
		stack.push(mir.Result())
		return b.appendMIR(mir)
	}
	mir := newBinaryOpMIR(op, &opnd1, &opnd2, stack)
	opnd1.use = append(opnd1.use, mir)
	opnd2.use = append(opnd2.use, mir)

	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) newMemoryLoadMIR(offset *Value, size *Value, accessor *MemoryAccessor, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirMLOAD
	mir.oprands = []*Value{offset, size}
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) newKeccakMIR(data *Value, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirKECCAK256
	mir.oprands = []*Value{data}
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func NewMIRBasicBlock(blockNum, pc uint, parent *MIRBasicBlock) *MIRBasicBlock {
	bb := new(MIRBasicBlock)
	bb.blockNum = blockNum
	bb.firstPC = pc
	bb.parentsBitmap = &bitmap{0}  // Initialize with at least 1 byte
	bb.childrenBitmap = &bitmap{0} // Initialize with at least 1 byte
	bb.instructions = []*MIR{}
	if parent != nil {
		bb.SetParents([]*MIRBasicBlock{parent})
	}
	return bb
}

type MIRBasicBlockStack struct {
	data []*MIRBasicBlock
}

func (s *MIRBasicBlockStack) Push(ptr *MIRBasicBlock) {
	s.data = append(s.data, ptr)
}

func (s *MIRBasicBlockStack) Pop() *MIRBasicBlock {
	val := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return val
}

func (s *MIRBasicBlockStack) Size() int {
	return len(s.data)
}

func (b *MIRBasicBlock) CreateStackOpMIR(op MirOperation, stack *ValueStack) *MIR {
	// For DUP operations
	if op >= MirDUP1 && op <= MirDUP16 {
		n := int(op - MirDUP1 + 1) // DUP1 = 1, DUP2 = 2, etc.
		return b.CreateDupMIR(n, stack)
	}

	// For SWAP operations
	if op >= MirSWAP1 && op <= MirSWAP16 {
		n := int(op - MirSWAP1 + 1) // SWAP1 = 1, SWAP2 = 2, etc.
		return b.CreateSwapMIR(n, stack)
	}

	// Fallback for other stack operations
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateDupMIR(n int, stack *ValueStack) *MIR {
	// DUPn duplicates the nth stack item (1-indexed) to the top
	// Stack before: [..., item_n, ..., item_2, item_1]
	// Stack after:  [..., item_n, ..., item_2, item_1, item_n]

	if stack.size() < n {
		// Not enough items on stack - create non-optimized MIR
		mir := new(MIR)
		mir.op = MirOperation(0x80 + byte(n-1))
		stack.push(mir.Result())
		return b.appendMIR(mir)
	}

	// Get the value to duplicate (n-1 because stack is 0-indexed from top)
	dupValue := stack.peek(n - 1)

	// Check if we can optimize this DUP operation
	if isOptimizable(MirOperation(0x80+byte(n-1))) && dupValue.kind == Konst {
		// If the value to duplicate is a constant, we can optimize
		// by directly pushing the constant value
		optimizedValue := newValue(Konst, nil, nil, dupValue.payload)
		stack.push(optimizedValue)

		// Create a NOP MIR to mark this optimization
		mir := newNopMIR(MirOperation(0x80+byte(n-1)), []*Value{dupValue})
		return b.appendMIR(mir)
	}

	// For non-constant values, perform the actual duplication
	duplicatedValue := *dupValue // Copy the value
	stack.push(&duplicatedValue)

	// Create MIR instruction with the source value as operand
	mir := new(MIR)
	mir.op = MirOperation(0x80 + byte(n-1))
	mir.oprands = []*Value{dupValue}
	dupValue.use = append(dupValue.use, mir)

	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateSwapMIR(n int, stack *ValueStack) *MIR {
	// SWAPn swaps the top stack item with the nth stack item (1-indexed)
	// Stack before: [..., item_n+1, item_n, ..., item_2, item_1]
	// Stack after:  [..., item_n+1, item_1, ..., item_2, item_n]

	if stack.size() <= n {
		// Not enough items on stack - create non-optimized MIR
		mir := new(MIR)
		mir.op = MirOperation(0x90 + byte(n-1))
		return b.appendMIR(mir)
	}

	// Check if we can optimize this SWAP operation
	topValue := stack.peek(0)  // item_1 (top of stack)
	swapValue := stack.peek(n) // item_n+1 (the item to swap with)

	if isOptimizable(MirOperation(0x90+byte(n-1))) &&
		topValue.kind == Konst && swapValue.kind == Konst {
		// Both values are constants, we can optimize by directly swapping
		stack.swap(0, n)

		// Create a NOP MIR to mark this optimization
		mir := newNopMIR(MirOperation(0x90+byte(n-1)), []*Value{topValue, swapValue})
		return b.appendMIR(mir)
	}

	// For non-constant values, perform the actual swap
	stack.swap(0, n)

	// Create MIR instruction with both values as operands
	mir := new(MIR)
	mir.op = MirOperation(0x90 + byte(n-1))
	mir.oprands = []*Value{topValue, swapValue}
	topValue.use = append(topValue.use, mir)
	swapValue.use = append(swapValue.use, mir)

	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateMemoryOpMIR(op MirOperation, stack *ValueStack, accessor *MemoryAccessor) *MIR {
	mir := new(MIR)
	mir.op = op

	// Common sizes
	size32 := newValue(Konst, nil, nil, []byte{0x20})
	size1 := newValue(Konst, nil, nil, []byte{0x01})

	switch op {
	case MirMLOAD:
		// pops: offset
		offset := stack.pop()
		if accessor != nil {
			accessor.recordLoad(offset, *size32)
		}
		mir.oprands = []*Value{&offset, size32}
	case MirMSTORE:
		// pops: offset (top), value
		offset := stack.pop()
		value := stack.pop()
		if accessor != nil {
			// Record the actual 32-byte memory representation for constants
			if value.kind == Konst {
				padded := make([]byte, 32)
				if len(value.payload) > 0 {
					copy(padded[32-len(value.payload):], value.payload)
				}
				accessor.recordStore(offset, *size32, Value{kind: Konst, payload: padded, u: uint256.NewInt(0).SetBytes(padded)})
			} else {
				accessor.recordStore(offset, *size32, value)
			}
		}
		mir.oprands = []*Value{&offset, size32, &value}
	case MirMSTORE8:
		// pops: offset (top), value
		offset := stack.pop()
		value := stack.pop()
		if accessor != nil {
			// Record only the lowest byte for MSTORE8 if constant
			if value.kind == Konst {
				b := byte(0)
				if len(value.payload) > 0 {
					b = value.payload[len(value.payload)-1]
				}
				accessor.recordStore(offset, *size1, Value{kind: Konst, payload: []byte{b}, u: uint256.NewInt(0).SetUint64(uint64(b))})
			} else {
				accessor.recordStore(offset, *size1, value)
			}
		}
		mir.oprands = []*Value{&offset, size1, &value}
	case MirMSIZE:
		// no memory access recorded
	default:
		// leave unmodified for other memory ops
	}

	// Only push result for producing ops
	switch op {
	case MirMLOAD, MirMSIZE:
		stack.push(mir.Result())
	}
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateStorageOpMIR(op MirOperation, stack *ValueStack, accessor *StateAccessor) *MIR {
	mir := new(MIR)
	mir.op = op

	switch op {
	case MirSLOAD:
		key := stack.pop()
		if accessor != nil {
			accessor.recordStateLoad(key)
		}
		mir.oprands = []*Value{&key}
	case MirSSTORE:
		// EVM pops key (top) then value
		key := stack.pop()
		value := stack.pop()
		if accessor != nil {
			accessor.recordStateStore(key, value)
		}
		mir.oprands = []*Value{&key, &value}
	case MirTLOAD:
		key := stack.pop()
		if accessor != nil {
			accessor.recordStateLoad(key)
		}
		mir.oprands = []*Value{&key}
	case MirTSTORE:
		value := stack.pop()
		key := stack.pop()
		if accessor != nil {
			accessor.recordStateStore(key, value)
		}
		mir.oprands = []*Value{&key, &value}
	default:
		// no-op
	}

	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBlockInfoMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op

	// Populate operands based on the specific block/tx info operation
	switch op {
	// No-operand producers
	case MirADDRESS, MirORIGIN, MirCALLER, MirCALLVALUE,
		MirCALLDATASIZE, MirCODESIZE, MirGASPRICE,
		MirRETURNDATASIZE, MirPC, MirGAS,
		MirDATASIZE, MirBLOBBASEFEE:
		// No stack pops; just produce a result

	case MirBALANCE:
		// pops: address
		addr := stack.pop()
		mir.oprands = []*Value{&addr}

	case MirCALLDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.oprands = []*Value{&offset}

	case MirCALLDATACOPY:
		// pops: dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		mir.oprands = []*Value{&dest, &offset, &size}

	case MirCODECOPY:
		// pops: dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		mir.oprands = []*Value{&dest, &offset, &size}

	case MirEXTCODESIZE:
		// pops: address
		addr := stack.pop()
		mir.oprands = []*Value{&addr}

	case MirEXTCODECOPY:
		// pops: address, dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		addr := stack.pop()
		mir.oprands = []*Value{&addr, &dest, &offset, &size}

	case MirRETURNDATACOPY:
		// pops: dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		mir.oprands = []*Value{&dest, &offset, &size}

	case MirEXTCODEHASH:
		// pops: address
		addr := stack.pop()
		mir.oprands = []*Value{&addr}

	// EOF data operations
	case MirDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.oprands = []*Value{&offset}

	case MirDATALOADN:
		// Immediate-indexed load in EOF; not modeled via stack here

	case MirDATACOPY:
		// pops: dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		mir.oprands = []*Value{&dest, &offset, &size}

	case MirRETURNDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.oprands = []*Value{&offset}

	case MirBLOBHASH:
		// pops: index
		index := stack.pop()
		mir.oprands = []*Value{&index}

	default:
		// leave operands empty for any not explicitly handled
	}

	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBlockOpMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	// Only MirBLOCKHASH consumes one stack operand (block number). Others are producers.
	if op == MirBLOCKHASH {
		blk := stack.pop()
		mir.oprands = []*Value{&blk}
	}
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateJumpMIR(op MirOperation, stack *ValueStack, bbStack *MIRBasicBlockStack) *MIR {
	mir := new(MIR)
	mir.op = op

	// EVM semantics:
	// - JUMP consumes 1 operand: destination
	// - JUMPI consumes 2 operands: destination and condition
	// Stack top holds the last pushed item; pop order reflects that.
	switch op {
	case MirJUMP:
		dest := stack.pop()
		mir.oprands = []*Value{&dest}
	case MirJUMPI:
		dest := stack.pop()
		cond := stack.pop()
		mir.oprands = []*Value{&dest, &cond}
	default:
		// Other jump-like ops not implemented here
	}

	// JUMP/JUMPI do not produce a stack value; do not push a result
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateControlFlowMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateSystemOpMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateLogMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreatePushMIR(n int, value []byte, stack *ValueStack) *MIR {
	stack.push(newValue(Konst, nil, nil, value))
	return nil
}

func (bb *MIRBasicBlock) GetNextOp() *MIR {
	if bb.pos >= len(bb.instructions) {
		return nil
	}
	op := bb.instructions[bb.pos]
	bb.pos++
	return op
}

func (ma *MemoryAccessor) isRecentStore(offset, size Value) bool {
	// Simplified implementation
	return false
}

func (ma *MemoryAccessor) isSameLocation(offset Value) bool {
	// Simplified implementation
	return false
}

func (sa *StateAccessor) isRecentStore(key Value) bool {
	// Simplified implementation
	return false
}

func (sa *StateAccessor) isSameKey(key Value) bool {
	// Simplified implementation
	return false
}
