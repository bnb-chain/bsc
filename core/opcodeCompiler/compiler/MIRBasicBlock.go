package compiler

// bitmap is a bit map which maps basicblock in to a bit
type bitmap []byte

func (bits bitmap) set1(pos uint64) {
	bits[pos/8] |= 1 << (pos % 8)
}

func (bits bitmap) setN(flag uint16, pos uint64) {
	a := flag << (pos % 8)
	bits[pos/8] |= byte(a)
	if b := byte(a >> 8); b != 0 {
		bits[pos/8+1] = b
	}
}

// checks if the position is in a code segment.
func (bits *bitmap) isBitSet(pos uint64) bool {
	return (((*bits)[pos/8] >> (pos % 8)) & 1) == 0
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
	b.instructions = append(b.instructions, mir)
	return mir
}

func (b *MIRBasicBlock) CreateUnaryOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd1 := stack.pop()
	mir = newUnaryOpMIR(op, &opnd1, stack)
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBinOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd2 := stack.pop()
	opnd1 := stack.pop()
	mir = newBinaryOpMIR(op, &opnd1, &opnd2, stack)
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBinOpMIRWithMA(op MirOperation, stack *ValueStack, accessor *MemoryAccessor) *MIR {
	opnd2 := stack.pop()
	opnd1 := stack.pop()

	// Record memory reads when applicable
	if accessor != nil {
		switch op {
		case MirKECCAK256:
			// opnd1: offset, opnd2: size
			accessor.recordLoad(opnd1, opnd2)
		}
	}

	// Peephole with memory knowledge, e.g., KECCAK256 over known bytes
	if accessor != nil && op == MirKECCAK256 {
		if doPeepHole(op, &opnd1, &opnd2, stack, accessor) {
			mir := newNopMIR(op, []*Value{&opnd1, &opnd2})
			return b.appendMIR(mir)
		}
	}

	mir := newBinaryOpMIR(op, &opnd1, &opnd2, stack)
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
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateDupMIR(n int, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirOperation(0x80 + byte(n-1)) // MirDUP1 = 0x80, MirDUP2 = 0x81, etc.
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateSwapMIR(n int, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirOperation(0x90 + byte(n-1)) // MirSWAP1 = 0x90, MirSWAP2 = 0x91, etc.
	stack.push(mir.Result())
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
		// pops: value, offset
		value := stack.pop()
		offset := stack.pop()
		if accessor != nil {
			accessor.recordStore(offset, *size32, value)
		}
		mir.oprands = []*Value{&offset, size32, &value}
	case MirMSTORE8:
		// pops: value, offset
		value := stack.pop()
		offset := stack.pop()
		if accessor != nil {
			accessor.recordStore(offset, *size1, value)
		}
		mir.oprands = []*Value{&offset, size1, &value}
	case MirMSIZE:
		// no memory access recorded
	default:
		// leave unmodified for other memory ops
	}

	stack.push(mir.Result())
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
		value := stack.pop()
		key := stack.pop()
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
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateBlockOpMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) CreateJumpMIR(op MirOperation, stack *ValueStack, bbStack *MIRBasicBlockStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
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
