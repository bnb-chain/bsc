package compiler

import (
	"github.com/ethereum/go-ethereum/crypto"
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
	initDepth      int
	parentsBitmap  *bitmap
	childrenBitmap *bitmap
	parents        []*MIRBasicBlock
	children       []*MIRBasicBlock
	instructions   []*MIR
	pos            int
	// EVM opcode accounting for gas parity
	// evmOpCounts counts every original EVM opcode encountered while building this block
	// emittedOpCounts counts only those opcodes for which a MIR instruction was emitted
	evmOpCounts     map[byte]uint32
	emittedOpCounts map[byte]uint32
	// SSA-like stack modeling
	entryStack     []Value
	exitStack      []Value
	incomingStacks map[*MIRBasicBlock][]Value
	// Precomputed live-outs: definitions (MIR) whose values are live at block exit
	liveOutDefs []*MIR
	// Build bookkeeping
	built  bool // set true after first successful build
	queued bool // true if currently enqueued for (re)build
}

func (b *MIRBasicBlock) Size() uint {
	return uint(len(b.instructions))
}

// Instructions returns the MIR instructions within this basic block
func (b *MIRBasicBlock) Instructions() []*MIR {
	return b.instructions
}

// EVMOpCounts returns a copy of the opcode counts encountered while building this block.
func (b *MIRBasicBlock) EVMOpCounts() map[byte]uint32 {
	if b == nil || b.evmOpCounts == nil {
		return nil
	}
	out := make(map[byte]uint32, len(b.evmOpCounts))
	for k, v := range b.evmOpCounts {
		out[k] = v
	}
	return out
}

// EmittedOpCounts returns a copy of the opcode counts that resulted in MIR emission.
func (b *MIRBasicBlock) EmittedOpCounts() map[byte]uint32 {
	if b == nil || b.emittedOpCounts == nil {
		return nil
	}
	out := make(map[byte]uint32, len(b.emittedOpCounts))
	for k, v := range b.emittedOpCounts {
		out[k] = v
	}
	return out
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

func (b *MIRBasicBlock) InitDepth() int {
	return b.initDepth
}

func (b *MIRBasicBlock) SetInitDepth(d int) {
	b.initDepth = d
}

func (b *MIRBasicBlock) SetInitDepthMax(d int) {
	if d > b.initDepth {
		b.initDepth = d
	}
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
	// Do not emit runtime MIR for NOP; gas is accounted via block aggregation
	if mir.op == MirNOP {
		return nil
	}
	return b.appendMIR(mir)
}

func (b *MIRBasicBlock) appendMIR(mir *MIR) *MIR {
	mir.idx = len(b.instructions)
	// Attach EVM mapping captured by the CFG builder
	mir.evmPC = currentEVMBuildPC
	mir.evmOp = currentEVMBuildOp
	// Record that we emitted a MIR for this originating EVM opcode
	if b.emittedOpCounts != nil {
		b.emittedOpCounts[currentEVMBuildOp]++
	}
	// Pre-encode operand info to avoid runtime eval costs
	if len(mir.operands) > 0 {
		mir.opKinds = make([]byte, len(mir.operands))
		mir.opConst = make([]*uint256.Int, len(mir.operands))
		mir.opDefIdx = make([]int, len(mir.operands))
		for i, v := range mir.operands {
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
	// Record generation-time stack depth for debugging/dumps
	// Note: this uses the current stack size after any pushes done above callers.
	mir.genStackDepth = 0
	// Best-effort: if the basic block is appending after a push, ValueStack.size()
	// will include it; otherwise it reflects post-pop size. Good enough for traces.
	// We cannot read stack here without passing it, so we leave as 0 and set in callers that have the stack.
	b.instructions = append(b.instructions, mir)
	return mir
}

func (b *MIRBasicBlock) CreateUnaryOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd1 := stack.pop()
	mir = newUnaryOpMIR(op, &opnd1, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		// Only push for producer ops; copy ops do not produce a stack item
		switch op {
		case MirCALLDATACOPY, MirCODECOPY, MirEXTCODECOPY, MirRETURNDATACOPY, MirDATACOPY:
			// no push
		default:
			stack.push(mir.Result())
		}
	}
	// If mir.op == MirNOP, doPeepHole already pushed the optimized constant to stack
	// Still emit the NOP so that runtime gas accounting can charge for the original opcode
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	return mir
}

func (b *MIRBasicBlock) CreateBinOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	opnd1 := stack.pop()
	opnd2 := stack.pop()
	mir = newBinaryOpMIR(op, &opnd1, &opnd2, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		stack.push(mir.Result())
	}
	// If mir.op == MirNOP, doPeepHole already pushed the optimized constant to stack
	if mir.op == MirNOP {
		// Do not emit runtime MIR for NOP; gas is accounted via block aggregation
		return nil
	}
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

// CreateTernaryOpMIR creates a MIR instruction for 3-operand operations like ADDMOD, MULMOD
func (b *MIRBasicBlock) CreateTernaryOpMIR(op MirOperation, stack *ValueStack) (mir *MIR) {
	// EVM stack before ternary ops like ADDMOD/MULMOD: [..., third, second, first(top)]
	// Pop order: first(top) -> second -> third
	opndA := stack.pop() // first (top)
	opndB := stack.pop() // second
	opndC := stack.pop() // third (e.g., modulus)

	// Try peephole optimization for 3-operand operations
	if doPeepHole3Ops(op, &opndA, &opndB, &opndC, stack, nil) {
		// Optimized away; do not emit MirNOP
		return nil
	}

	// Create regular ternary operation MIR in (first, second, third) order
	mir = newTernaryOpMIR(op, &opndA, &opndB, &opndC, stack)

	// Only push result if the operation wasn't optimized away (MirNOP)
	if mir.op != MirNOP {
		stack.push(mir.Result())
	}
	// If mir.op == MirNOP, doPeepHole3Ops already pushed the optimized constant to stack
	if mir.op == MirNOP {
		// Do not emit runtime MIR for NOP; gas is accounted via block aggregation
		return nil
	}
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	return mir
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

	// Disable KECCAK256 peephole for correctness with dynamic calldata-driven inputs
	// (kept for future re-enable once proven fully safe across call paths)
	// For KECCAK256 ensure operands are [offset, size]
	if op == MirKECCAK256 {
		// operands: [offset, size] -> (opnd2, opnd1)
		mir := newBinaryOpMIR(op, &opnd2, &opnd1, stack)
		// If the memory accessor knows the exact slice content (constant), precompute hash
		if accessor != nil && opnd2.kind == Konst && opnd1.kind == Konst {
			offU := uint256.NewInt(0).SetBytes(opnd2.payload)
			szU := uint256.NewInt(0).SetBytes(opnd1.payload)
			if val := accessor.getValueWithOffset(offU, szU); val.kind == Konst && len(val.payload) > 0 {
				h := crypto.Keccak256(val.payload)
				// Attach precomputed 32-byte hash; interpreter will use it while still charging gas
				mir.meta = h
			}
		}
		opnd2.use = append(opnd2.use, mir)
		opnd1.use = append(opnd1.use, mir)
		stack.push(mir.Result())
		mir = b.appendMIR(mir)
		mir.genStackDepth = stack.size()
		return mir
	}
	mir := newBinaryOpMIR(op, &opnd1, &opnd2, stack)
	opnd1.use = append(opnd1.use, mir)
	opnd2.use = append(opnd2.use, mir)

	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) newMemoryLoadMIR(offset *Value, size *Value, accessor *MemoryAccessor, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirMLOAD
	mir.operands = []*Value{offset, size}
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) newKeccakMIR(data *Value, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirKECCAK256
	mir.operands = []*Value{data}
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func NewMIRBasicBlock(blockNum, pc uint, parent *MIRBasicBlock) *MIRBasicBlock {
	bb := new(MIRBasicBlock)
	bb.blockNum = blockNum
	bb.firstPC = pc
	bb.initDepth = 0
	bb.parentsBitmap = &bitmap{0}  // Initialize with at least 1 byte
	bb.childrenBitmap = &bitmap{0} // Initialize with at least 1 byte
	bb.instructions = []*MIR{}
	bb.evmOpCounts = make(map[byte]uint32)
	bb.emittedOpCounts = make(map[byte]uint32)
	bb.entryStack = nil
	bb.exitStack = nil
	bb.incomingStacks = make(map[*MIRBasicBlock][]Value)
	bb.built = false
	bb.queued = false
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
		// Diagnostics: stack size before DUP
		if stack.size() < n {
			parserDebugWarn("MIR DUP depth underflow - emitting NOP", "need", n, "have", stack.size(), "bb", b.blockNum, "bbFirst", b.firstPC)
		}
		return b.CreateDupMIR(n, stack)
	}

	// For SWAP operations
	if op >= MirSWAP1 && op <= MirSWAP16 {
		n := int(op - MirSWAP1 + 1) // SWAP1 = 1, SWAP2 = 2, etc.
		// Diagnostics: stack size before SWAP
		if stack.size() <= n {
			parserDebugWarn("MIR SWAP depth underflow - emitting NOP", "need", n+1, "have", stack.size(), "bb", b.blockNum, "bbFirst", b.firstPC)
		}
		return b.CreateSwapMIR(n, stack)
	}

	// Fallback for other stack operations
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	return mir
}

func (b *MIRBasicBlock) CreateDupMIR(n int, stack *ValueStack) *MIR {
	// DUPn duplicates the nth stack item (1-indexed) to the top
	// Stack before: [..., item_n, ..., item_2, item_1]
	// Stack after:  [..., item_n, ..., item_2, item_1, item_n]

	if stack.size() < n {
		// Depth underflow: no-op emission
		return nil
	}

	// Get the value to duplicate (n-1 because stack is 0-indexed from top)
	dupValue := stack.peek(n - 1)

	// Check if we can optimize this DUP operation
	if isOptimizable(MirOperation(0x80+byte(n-1))) && dupValue.kind == Konst {
		// If the value to duplicate is a constant, duplicate by pushing same constant
		optimizedValue := newValue(Konst, nil, nil, dupValue.payload)
		stack.push(optimizedValue)
		// No runtime MIR for DUP; gas handled via per-block opcode counts
		return nil
	}

	// For non-constant values, perform the actual duplication on the stack
	duplicatedValue := *dupValue // Copy the value
	stack.push(&duplicatedValue)

	// No runtime MIR for DUP; gas handled via per-block opcode counts
	return nil
}

func (b *MIRBasicBlock) CreateSwapMIR(n int, stack *ValueStack) *MIR {
	// SWAPn swaps the top stack item with the nth stack item (1-indexed)
	// Stack before: [..., item_n+1, item_n, ..., item_2, item_1]
	// Stack after:  [..., item_n+1, item_1, ..., item_2, item_n]

	if stack.size() <= n {
		// Depth underflow: no-op emission
		return nil
	}

	// Check if we can optimize this SWAP operation
	topValue := stack.peek(0)  // item_1 (top of stack)
	swapValue := stack.peek(n) // item_n+1 (the item to swap with)
	// Diagnostics: before swap snapshot
	// removed verbose SWAP diagnostics

	if isOptimizable(MirOperation(0x90+byte(n-1))) &&
		topValue.kind == Konst && swapValue.kind == Konst {
		// Both values are constants, just swap in stack
		stack.swap(0, n)
		// No runtime MIR for SWAP; gas handled via per-block opcode counts
		return nil
	}

	// For non-constant values, perform the actual swap on the stack
	stack.swap(0, n)
	// Diagnostics: after swap snapshot removed
	// No runtime MIR for SWAP; gas handled via per-block opcode counts
	return nil
}

// CreatePhiMIR creates a PHI node merging incoming stack values.
func (b *MIRBasicBlock) CreatePhiMIR(ops []*Value, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = MirPHI
	mir.operands = ops
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	return mir
}

// AddIncomingStack records a parent's exit stack as an incoming stack for this block.
func (b *MIRBasicBlock) AddIncomingStack(parent *MIRBasicBlock, values []Value) {
	if parent == nil || values == nil {
		return
	}
	// Copy to decouple from caller mutations
	copied := make([]Value, len(values))
	copy(copied, values)
	b.incomingStacks[parent] = copied
}

// IncomingStacks returns the recorded incoming stacks by parent.
func (b *MIRBasicBlock) IncomingStacks() map[*MIRBasicBlock][]Value {
	return b.incomingStacks
}

// SetExitStack records the block's exit stack snapshot.
func (b *MIRBasicBlock) SetExitStack(values []Value) {
	if values == nil {
		b.exitStack = nil
		b.liveOutDefs = nil
		return
	}
	copied := make([]Value, len(values))
	copy(copied, values)
	b.exitStack = copied
	// Recompute liveOutDefs from exit stack: collect defs for variable values
	if len(values) == 0 {
		b.liveOutDefs = nil
		return
	}
	defs := make([]*MIR, 0, len(values))
	for i := range values {
		v := values[i]
		if v.kind == Variable && v.def != nil {
			defs = append(defs, v.def)
		}
	}
	b.liveOutDefs = defs
}

// ExitStack returns the block's exit stack snapshot.
func (b *MIRBasicBlock) ExitStack() []Value { return b.exitStack }

// SetEntryStack sets the precomputed entry stack snapshot.
func (b *MIRBasicBlock) SetEntryStack(values []Value) {
	if values == nil {
		b.entryStack = nil
		return
	}
	copied := make([]Value, len(values))
	copy(copied, values)
	b.entryStack = copied
}

// EntryStack returns the block's entry stack snapshot.
func (b *MIRBasicBlock) EntryStack() []Value { return b.entryStack }

// LiveOutDefs returns the MIR definitions that are live at block exit.
func (b *MIRBasicBlock) LiveOutDefs() []*MIR { return b.liveOutDefs }

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
			// Attempt simple forwarding: if a prior MSTORE wrote this exact 32-byte range with a constant
			// record, attach it to MIR meta for runtime to use, but still emit MLOAD for gas parity/mem growth.
			if offset.kind == Konst {
				offU := uint256.NewInt(0).SetBytes(offset.payload)
				szU := uint256.NewInt(0).SetUint64(32)
				if v := accessor.getValueWithOffset(offU, szU); v.kind == Konst {
					padded := make([]byte, 32)
					if len(v.payload) > 0 {
						copy(padded[32-len(v.payload):], v.payload)
					}
					mir.meta = padded
				}
			}
			accessor.recordLoad(offset, *size32)
		}
		mir.operands = []*Value{&offset, size32}
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
		mir.operands = []*Value{&offset, size32, &value}
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
		mir.operands = []*Value{&offset, size1, &value}
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
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateStorageOpMIR(op MirOperation, stack *ValueStack, accessor *StateAccessor) *MIR {
	mir := new(MIR)
	mir.op = op

	switch op {
	case MirSLOAD:
		key := stack.pop()
		// Record load and produce generic result (forwarding disabled for parity)
		if accessor != nil {
			accessor.recordStateLoad(key)
		}
		stack.push(mir.Result())
		mir.operands = []*Value{&key}
	case MirSSTORE:
		// EVM pops key (top) then value
		key := stack.pop()
		value := stack.pop()
		if accessor != nil {
			accessor.recordStateStore(key, value)
		}
		mir.operands = []*Value{&key, &value}
	case MirTLOAD:
		key := stack.pop()
		if accessor != nil {
			accessor.recordStateLoad(key)
		}
		mir.operands = []*Value{&key}
	case MirTSTORE:
		// EVM pops key (top) then value, same as SSTORE
		key := stack.pop()
		value := stack.pop()
		if accessor != nil {
			accessor.recordStateStore(key, value)
		}
		mir.operands = []*Value{&key, &value}
	default:
		// no-op
	}

	// Push generic result for all except handled separately above
	if op != MirSLOAD {
		stack.push(mir.Result())
	}
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
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
		mir.operands = []*Value{&addr}

	case MirCALLDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.operands = []*Value{&offset}

	case MirCALLDATACOPY:
		// pops (EVM order): dest(memOffset), offset(dataOffset), size(length)
		dest := stack.pop()
		offset := stack.pop()
		size := stack.pop()
		mir.operands = []*Value{&dest, &offset, &size}

	case MirCODECOPY:
		// pops (EVM order): dest(memOffset), offset(codeOffset), size(length)
		dest := stack.pop()
		offset := stack.pop()
		size := stack.pop()
		mir.operands = []*Value{&dest, &offset, &size}

	case MirEXTCODESIZE:
		// pops: address
		addr := stack.pop()
		mir.operands = []*Value{&addr}

	case MirEXTCODECOPY:
		// EVM stack (top to bottom): address, destOffset, offset, size
		// Pop order: address (first/top), dest, offset, size (last/bottom)
		addr := stack.pop()
		dest := stack.pop()
		offset := stack.pop()
		size := stack.pop()
		mir.operands = []*Value{&addr, &dest, &offset, &size}

	case MirRETURNDATACOPY:
		// pops (EVM order): dest(memOffset), offset(returnDataOffset), size(length)
		dest := stack.pop()
		offset := stack.pop()
		size := stack.pop()
		mir.operands = []*Value{&dest, &offset, &size}

	case MirEXTCODEHASH:
		// pops: address
		addr := stack.pop()
		mir.operands = []*Value{&addr}

	// EOF data operations
	case MirDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.operands = []*Value{&offset}

	case MirDATALOADN:
		// Immediate-indexed load in EOF; not modeled via stack here

	case MirDATACOPY:
		// pops: dest, offset, size
		size := stack.pop()
		offset := stack.pop()
		dest := stack.pop()
		mir.operands = []*Value{&dest, &offset, &size}

	case MirRETURNDATALOAD:
		// pops: offset
		offset := stack.pop()
		mir.operands = []*Value{&offset}

	case MirBLOBHASH:
		// pops: index
		index := stack.pop()
		mir.operands = []*Value{&index}

	default:
		// leave operands empty for any not explicitly handled
	}

	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateBlockOpMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	// Only MirBLOCKHASH consumes one stack operand (block number). Others are producers.
	if op == MirBLOCKHASH {
		blk := stack.pop()
		mir.operands = []*Value{&blk}
	}
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateJumpMIR(op MirOperation, stack *ValueStack, bbStack *MIRBasicBlockStack) *MIR {
	mir := new(MIR)
	mir.op = op

	// EVM semantics:
	// - JUMP consumes 1 operand: destination
	// - JUMPI consumes 2 operands: destination and condition
	// Stack top holds the last pushed item; pop order reflects that.
	// Diagnostics: snapshot top 8 stack items before consuming jump operands
	{
		sz := stack.size()
		max := sz
		if max > 8 {
			max = 8
		}
		for i := 0; i < max; i++ {
			v := stack.peek(i)
			if v == nil {
				continue
			}
			if v.def != nil {
				parserDebugWarn("MIR JUMP stack", "bb", b.blockNum, "firstPC", b.firstPC, "idxFromTop", i, "val", v.DebugString(), "def_evm_pc", v.def.evmPC, "def_idx", v.def.idx)
			} else {
				parserDebugWarn("MIR JUMP stack", "bb", b.blockNum, "firstPC", b.firstPC, "idxFromTop", i, "val", v.DebugString())
			}
		}
	}

	switch op {
	case MirJUMP:
		dest := stack.pop()
		mir.operands = []*Value{&dest}
	case MirJUMPI:
		dest := stack.pop()
		cond := stack.pop()
		mir.operands = []*Value{&dest, &cond}
	default:
		// Other jump-like ops not implemented here
	}

	// JUMP/JUMPI do not produce a stack value; do not push a result
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateControlFlowMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateSystemOpMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op
	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

func (b *MIRBasicBlock) CreateLogMIR(op MirOperation, stack *ValueStack) *MIR {
	mir := new(MIR)
	mir.op = op

	// Calculate number of topics based on LOG operation
	numTopics := int(op - MirLOG0)

	// EVM pops in order: dataOffset, dataSize, topic1, topic2, ..., topicN
	// (stack top has dataOffset, then dataSize, then topics)
	// Total operands: 2 (offset+size) + numTopics
	totalOperands := 2 + numTopics

	// Pop all values - they come in the right order!
	operands := make([]*Value, totalOperands)
	for i := 0; i < totalOperands; i++ {
		val := stack.pop()
		operands[i] = &val
	}
	mir.operands = operands

	stack.push(mir.Result())
	mir = b.appendMIR(mir)
	mir.genStackDepth = stack.size()
	// noisy generation logging removed
	return mir
}

// stacksEqual reports whether two Value slices are equal element-wise using Value semantics.
// Constants are compared by numeric value, variables by stable def identity (op, evmPC, phiSlot).
func stacksEqual(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		va := &a[i]
		vb := &b[i]
		if !equalValueForFlow(va, vb) {
			return false
		}
	}
	return true
}

// equalValueForFlow compares two Values with stable criteria across rebuilds.
// - Konst: compare numeric equality (uint256)
// - Variable: if both have defs, compare def.op, def.evmPC and def.phiStackIndex; else require both nil
// - Arguments/Unknown: equal if kinds match
func equalValueForFlow(a, b *Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case Konst:
		// Compare numeric value
		var av, bv *uint256.Int
		if a.u != nil {
			av = a.u
		} else {
			av = uint256.NewInt(0).SetBytes(a.payload)
		}
		if b.u != nil {
			bv = b.u
		} else {
			bv = uint256.NewInt(0).SetBytes(b.payload)
		}
		return av != nil && bv != nil && av.Eq(bv)
	case Variable:
		if a.def == nil || b.def == nil {
			return a.def == nil && b.def == nil
		}
		if a.def.op != b.def.op {
			return false
		}
		if a.def.evmPC != b.def.evmPC {
			return false
		}
		if a.def.phiStackIndex != b.def.phiStackIndex {
			return false
		}
		return true
	case Arguments, Unknown:
		return true
	default:
		return false
	}
}

// ResetForRebuild clears transient build artifacts so the block can be rebuilt cleanly
// without duplicating MIR instructions. It preserves structural CFG data and entry/incoming
// stacks so PHIs can be regenerated deterministically.
func (b *MIRBasicBlock) ResetForRebuild(preserveEntry bool) {
	// Clear previously generated instructions and iteration cursor
	b.instructions = nil
	b.pos = 0
	// Clear exit-related metadata; it will be recomputed during rebuild
	b.lastPC = 0
	b.exitStack = nil
	b.liveOutDefs = nil
	// Optionally preserve entry stack snapshot; most rebuilds depend on it
	if !preserveEntry {
		b.entryStack = nil
	}
	// Do not touch parents, children or incomingStacks here; they represent CFG topology
}

func (b *MIRBasicBlock) CreatePushMIR(n int, value []byte, stack *ValueStack) *MIR {
	stack.push(newValue(Konst, nil, nil, value))
	// No runtime MIR for PUSH; gas handled via per-block opcode counts
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
