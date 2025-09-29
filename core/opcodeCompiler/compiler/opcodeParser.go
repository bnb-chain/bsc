package compiler

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// debugDumpBB logs a basic block and its MIR instructions for diagnostics.
func debugDumpBB(prefix string, bb *MIRBasicBlock) {
	if bb == nil {
		return
	}
	entryH := -1
	if bb.entryStack != nil {
		entryH = len(bb.entryStack)
	}
	log.Warn("MIR BB", "where", prefix, "num", bb.blockNum, "firstPC", bb.firstPC, "lastPC", bb.lastPC, "initDepth", bb.initDepth, "entryH", entryH, "parents.len", len(bb.parents))
	ins := bb.Instructions()
	for i, m := range ins {
		if m == nil {
			continue
		}
		log.Warn("  MIR op", "idx", i, "op", m.Op().String(), "genStack", m.GenStackDepth())
	}
}

// debugFormatValue renders a Value in a compact debug form.
func debugFormatValue(v *Value) string {
	if v == nil {
		return "nil"
	}
	switch v.kind {
	case Konst:
		if v.u != nil {
			return fmt.Sprintf("const:0x%x", v.u.Bytes())
		}
		return fmt.Sprintf("const:0x%x", v.payload)
	case Arguments:
		return "arg"
	case Variable:
		if v.def != nil {
			return fmt.Sprintf("var:def@%d", v.def.idx)
		}
		return "var"
	default:
		return "unknown"
	}
}

// debugDumpMIR logs one MIR with its operands rendered as stack values.
func debugDumpMIR(m *MIR) {
	if m == nil {
		return
	}
	ops := ""
	if len(m.oprands) > 0 {
		for i, v := range m.oprands {
			if i != 0 {
				ops += ", "
			}
			ops += debugFormatValue(v)
		}
	}
	// best-effort decode of EVM opcode stored in meta for NOP markers, etc.
	evm := ""
	if len(m.meta) > 0 {
		evm = ByteCode(m.meta[0]).byteCodeToString()
	}
	// pc if known
	var pc interface{} = nil
	if m.pc != nil {
		pc = *m.pc
	}
	if ops == "" {
		if evm == "" {
			log.Warn("  MIR op", "idx", m.idx, "op", m.Op().String(), "pc", pc, "genStack", m.GenStackDepth())
		} else {
			log.Warn("  MIR op", "idx", m.idx, "op", m.Op().String(), "pc", pc, "evm", evm, "genStack", m.GenStackDepth())
		}
	} else {
		if evm == "" {
			log.Warn("  MIR op", "idx", m.idx, "op", m.Op().String(), "pc", pc, "genStack", m.GenStackDepth(), "ops", ops)
		} else {
			log.Warn("  MIR op", "idx", m.idx, "op", m.Op().String(), "pc", pc, "evm", evm, "genStack", m.GenStackDepth(), "ops", ops)
		}
	}
}

// debugDumpBBFull logs a BB header and all MIRs with operand stack values.
func debugDumpBBFull(where string, bb *MIRBasicBlock) {
	if bb == nil {
		return
	}
	entryH := -1
	if bb.entryStack != nil {
		entryH = len(bb.entryStack)
	}
	log.Warn("MIR BB", "where", where, "num", bb.blockNum, "firstPC", bb.firstPC, "lastPC", bb.lastPC, "initDepth", bb.initDepth, "entryH", entryH, "parents", len(bb.parents))
	for _, m := range bb.Instructions() {
		debugDumpMIR(m)
	}
}

// debugDumpParents logs all parents of a basic block with their MIR instructions.
func debugDumpParents(bb *MIRBasicBlock) {
	if bb == nil {
		return
	}
	for idx, p := range bb.Parents() {
		label := fmt.Sprintf("parent[%d]", idx)
		debugDumpBB(label, p)
	}
}

// debugDumpAncestors recursively dumps all ancestors (grandparents to the beginning), avoiding cycles.
func debugDumpAncestors(bb *MIRBasicBlock, visited map[*MIRBasicBlock]bool, root uint) {
	if bb == nil {
		return
	}
	// Depth-first traversal of ancestors back to the beginning
	for _, p := range bb.Parents() {
		if !visited[p] {
			visited[p] = true
			debugDumpAncestors(p, visited, bb.blockNum)
			debugDumpBB(fmt.Sprintf("ancestor of %d", bb.blockNum), p)
		}
	}
}

// debugDumpAncestryDOT builds a DOT graph for the ancestry of bb and returns it.
func debugDumpAncestryDOT(bb *MIRBasicBlock) string {
	if bb == nil {
		return ""
	}
	// Collect nodes and edges with DFS
	visited := make(map[*MIRBasicBlock]bool)
	nodes := make(map[*MIRBasicBlock]bool)
	edges := [][2]*MIRBasicBlock{}
	var dfs func(*MIRBasicBlock)
	dfs = func(x *MIRBasicBlock) {
		if x == nil || visited[x] {
			return
		}
		visited[x] = true
		nodes[x] = true
		for _, p := range x.Parents() {
			edges = append(edges, [2]*MIRBasicBlock{p, x})
			dfs(p)
			nodes[p] = true
		}
	}
	dfs(bb)
	// Build DOT
	buf := "digraph MIR_Ancestry {\n  rankdir=TB; node [shape=box, fontname=Courier];\n"
	for n := range nodes {
		label := fmt.Sprintf("BB%d\\nPC:%d..%d\\nparents:%d\\nins:%d", n.blockNum, n.firstPC, n.lastPC, len(n.parents), len(n.Instructions()))
		buf += fmt.Sprintf("  \"BB_%d\" [label=\"%s\"];\n", n.blockNum, label)
	}
	for _, e := range edges {
		buf += fmt.Sprintf("  \"BB_%d\" -> \"BB_%d\";\n", e[0].blockNum, e[1].blockNum)
	}
	buf += "}\n"
	return buf
}

// debugWriteDOTIfRequested writes DOT to a temp file if MIR_DUMP_DOT=1
func debugWriteDOTIfRequested(dot string, tag string) {
	if dot == "" || os.Getenv("MIR_DUMP_DOT") != "1" {
		return
	}
	name := fmt.Sprintf("mir_ancestry_%s_%d.dot", tag, os.Getpid())
	path := os.TempDir() + string(os.PathSeparator) + name
	_ = os.WriteFile(path, []byte(dot), 0644)
	log.Warn("MIR DOT written", "path", path)
}

// CFG is the IR record the control flow of the contract.
// It records not only the control flow info but also the state and memory accesses.
// CFG is mapping to <addr, code> pair, and there is no need to record CFG for every contract
// since it is just an IR, although the analyzing/compiling results are saved to the related cache.
type CFG struct {
	codeAddr        common.Hash
	rawCode         []byte
	basicBlocks     []*MIRBasicBlock
	basicBlockCount uint
	memoryAccessor  *MemoryAccessor
	stateAccessor   *StateAccessor
	// Fast lookup helpers, built on demand
	selectorIndex map[uint32]*MIRBasicBlock // 4-byte selector -> entry basic block
	pcToBlock     map[uint]*MIRBasicBlock   // bytecode PC -> basic block
}

func NewCFG(hash common.Hash, code []byte) (c *CFG) {
	c = &CFG{}
	c.codeAddr = hash
	c.rawCode = code
	c.basicBlocks = []*MIRBasicBlock{}
	c.basicBlockCount = 0
	c.selectorIndex = nil
	c.pcToBlock = make(map[uint]*MIRBasicBlock)
	return c
}

func (c *CFG) getMemoryAccessor() *MemoryAccessor {
	if c.memoryAccessor == nil {
		c.memoryAccessor = new(MemoryAccessor)
	}
	return c.memoryAccessor
}

func (c *CFG) getStateAccessor() *StateAccessor {
	if c.stateAccessor == nil {
		c.stateAccessor = new(StateAccessor)
	}
	return c.stateAccessor
}

// createEntryBB create an empty bb that contains (method/invoke) entry info.
func (c *CFG) createEntryBB() *MIRBasicBlock {
	entryBB := NewMIRBasicBlock(0, 0, nil)
	c.basicBlockCount++
	return entryBB
}

// createBB create a normal bb.
func (c *CFG) createBB(pc uint, parent *MIRBasicBlock) *MIRBasicBlock {
	if c.pcToBlock != nil {
		if existing, ok := c.pcToBlock[pc]; ok {
			if parent != nil {
				existing.SetParents([]*MIRBasicBlock{parent})
			}
			return existing
		}
	}
	bb := NewMIRBasicBlock(c.basicBlockCount, pc, parent)
	c.basicBlocks = append(c.basicBlocks, bb)
	c.basicBlockCount++
	if c.pcToBlock != nil {
		c.pcToBlock[pc] = bb
	}
	return bb
}

func (c *CFG) reachEndBB() {
	// reach the end of BasicBlock.
	// TODO - zlin:  check the child is backward only.
}

// GenerateMIRCFG generates a MIR Control Flow Graph for the given bytecode
func GenerateMIRCFG(hash common.Hash, code []byte) (*CFG, error) {
	if len(code) == 0 {
		return nil, fmt.Errorf("empty code")
	}

	cfg := NewCFG(hash, code)

	// memoryAccessor is instance local at runtime
	var memoryAccessor *MemoryAccessor = cfg.getMemoryAccessor()
	// stateAccessor is global but we analyze it in processor granularity
	var stateAccessor *StateAccessor = cfg.getStateAccessor()

	entryBB := cfg.createEntryBB()
	startBB := cfg.createBB(0, entryBB)
	valueStack := ValueStack{}
	// generate CFG.
	unprcessedBBs := MIRBasicBlockStack{}
	unprcessedBBs.Push(startBB)

	// Guard against pathological CFG explosions in large contracts.
	// Adapt the budget to the contract size: set to raw bytecode length.
	// This keeps analysis proportional to program size and avoids premature truncation.
	maxBasicBlocks := len(code)
	if maxBasicBlocks <= 0 {
		maxBasicBlocks = 1
	}
	processedUnique := 0

	for unprcessedBBs.Size() != 0 {
		if processedUnique >= maxBasicBlocks {
			log.Warn("MIR CFG build budget reached", "blocks", processedUnique)
			break
		}
		curBB := unprcessedBBs.Pop()
		if curBB == nil {
			continue
		}
		// Avoid duplicate queue entries
		curBB.queued = false
		// Track unique blocks processed
		if !curBB.built {
			processedUnique++
		}
		// Seed entry stack for this block. If it has recorded entry snapshot, use it; else, if it
		// has exactly one parent with an exit snapshot, inherit it; if multiple parents, ensure
		// PHI nodes are materialized in buildBasicBlock.
		if es := curBB.EntryStack(); es != nil {
			valueStack.resetTo(es)
		} else if len(curBB.Parents()) == 1 {
			if ps := curBB.Parents()[0].ExitStack(); ps != nil {
				valueStack.resetTo(ps)
			} else {
				valueStack.resetTo(nil)
			}
		} else {
			// No known entry; clear stack to start fresh and let PHI creation fill in
			valueStack.resetTo(nil)
		}
		// Only rebuild if entry height changed or block wasn't built before
		currentEntryH := -1
		if es := curBB.EntryStack(); es != nil {
			currentEntryH = len(es)
		}
		if !curBB.built || curBB.lastEntryHeight != currentEntryH {
			err := cfg.buildBasicBlock(curBB, &valueStack, memoryAccessor, stateAccessor, &unprcessedBBs)
			if err != nil {
				log.Error(err.Error())
				return nil, err
			}
			curBB.built = true
			curBB.lastEntryHeight = currentEntryH
		}
	}
	log.Warn("===================CFG DUMP=============================")
	// Dump all basic blocks and their MIR instructions (with operands) for debugging
	for i, bb := range cfg.basicBlocks {
		if bb == nil {
			continue
		}
		where := fmt.Sprintf("bb[%d]", i)
		debugDumpBBFull(where, bb)
	}
	log.Warn("===================CFG DUMP END=============================")
	return cfg, nil
}

// GetBasicBlocks returns the basic blocks in this CFG
func (c *CFG) GetBasicBlocks() []*MIRBasicBlock {
	return c.basicBlocks
}

// buildPCIndex builds a map from PC to basic block for quick lookups.
func (c *CFG) buildPCIndex() {
	if c.pcToBlock != nil {
		return
	}
	m := make(map[uint]*MIRBasicBlock, len(c.basicBlocks))
	for _, bb := range c.basicBlocks {
		if bb == nil {
			continue
		}
		m[bb.FirstPC()] = bb
	}
	c.pcToBlock = m
}

// EnsureSelectorIndexBuilt scans raw bytecode for PUSH4 <selector> and a nearby
// PUSH2 <offset>, then maps selector to the basic block at that offset.
func (c *CFG) EnsureSelectorIndexBuilt() {
	if c.selectorIndex != nil {
		return
	}
	c.buildPCIndex()
	idx := make(map[uint32]*MIRBasicBlock)
	code := c.rawCode
	for i := 0; i+7 < len(code); i++ {
		if code[i] == 0x63 { // PUSH4
			sel := uint32(code[i+1])<<24 | uint32(code[i+2])<<16 | uint32(code[i+3])<<8 | uint32(code[i+4])
			for j := i + 5; j < i+12 && j+2 < len(code); j++ {
				if code[j] == 0x61 { // PUSH2
					off := (uint(code[j+1]) << 8) | uint(code[j+2])
					if bb, ok := c.pcToBlock[uint(off)]; ok {
						if _, exists := idx[sel]; !exists {
							idx[sel] = bb
						}
					}
					break
				}
			}
		}
	}
	c.selectorIndex = idx
}

// EntryIndexForSelector returns the index within basicBlocks for a selector if known, else -1.
func (c *CFG) EntryIndexForSelector(selector uint32) int {
	c.EnsureSelectorIndexBuilt()
	if c.selectorIndex == nil {
		return -1
	}
	if bb, ok := c.selectorIndex[selector]; ok {
		for i, b := range c.basicBlocks {
			if b == bb {
				return i
			}
		}
	}
	return -1
}

// Compilation-time bytecode generation functions removed
// MIR CFGs are now cached and executed directly by MIRInterpreterAdapter at runtime
func (c *CFG) buildBasicBlock(curBB *MIRBasicBlock, valueStack *ValueStack, memoryAccessor *MemoryAccessor, stateAccessor *StateAccessor, unprcessedBBs *MIRBasicBlockStack) error {
	// Get the raw code from the CFG
	code := c.rawCode
	if code == nil || len(code) == 0 {
		return fmt.Errorf("empty code for basic block")
	}

	// Start processing from the basic block's firstPC position
	i := int(curBB.firstPC)
	if i >= len(code) {
		return fmt.Errorf("invalid starting position %d for basic block", i)
	}

	// Process each byte in the code starting from firstPC
	// Maintain a local depth counter to validate DUP/SWAP semantics without
	// relying on MIR-level stack mutations (which can be optimized to NOPs).
	depth := curBB.InitDepth()
	depthKnown := false

	// If this block has multiple parents and recorded incoming stacks, insert PHI nodes to form a unified
	// entry stack and seed the current stack accordingly.
	if len(curBB.Parents()) > 1 && len(curBB.IncomingStacks()) > 0 {
		// Determine the maximum stack height among incoming paths
		maxH := 0
		for _, st := range curBB.IncomingStacks() {
			if l := len(st); l > maxH {
				maxH = l
			}
		}
		// Build PHIs from top to bottom so stack order is preserved
		tmp := ValueStack{}
		for i := 0; i < maxH; i++ {
			// Collect ith from top across parents if available
			var ops []*Value
			for _, p := range curBB.Parents() {
				st := curBB.IncomingStacks()[p]
				if st != nil && len(st) > i {
					// stack top is end; index from top
					v := st[len(st)-1-i]
					vv := v // copy
					ops = append(ops, &vv)
				} else {
					// missing value -> unknown placeholder
					ops = append(ops, newValue(Unknown, nil, nil, nil))
				}
			}
			_ = curBB.CreatePhiMIR(ops, &tmp)
		}
		// tmp now has maxH values pushed in top-down creation order; assign as entry
		curBB.SetEntryStack(tmp.clone())
		valueStack.resetTo(curBB.EntryStack())
		depth = len(curBB.EntryStack())
		depthKnown = true
	} else if es := curBB.EntryStack(); es != nil {
		valueStack.resetTo(es)
		depth = len(es)
		depthKnown = true
	} else if len(curBB.Parents()) == 1 {
		// Single-parent path with inherited stack seeded by the caller.
		// Align local depth tracker with the actual entry stack to avoid
		// spurious DUP/SWAP underflow warnings and ensure exact EVM parity.
		depth = valueStack.size()
		depthKnown = true
	}
	for i < len(code) {
		op := ByteCode(code[i])

		// Handle PUSH operations
		if op >= PUSH1 && op <= PUSH32 {
			size := int(op - PUSH1 + 1)
			if i+size >= len(code) {
				return fmt.Errorf("invalid PUSH operation at position %d", i)
			}
			_ = curBB.CreatePushMIR(size, code[i+1:i+1+size], valueStack)
			depth++
			depthKnown = true
			i += size + 1 // +1 for the opcode itself
			continue
		}

		// Handle other operations
		var mir *MIR
		switch op {
		case STOP:
			mir = curBB.CreateVoidMIR(MirSTOP)
			curBB.SetLastPC(uint(i))
			return nil
		case ADD:
			mir = curBB.CreateBinOpMIR(MirADD, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case MUL:
			mir = curBB.CreateBinOpMIR(MirMUL, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SUB:
			mir = curBB.CreateBinOpMIR(MirSUB, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case DIV:
			mir = curBB.CreateBinOpMIR(MirDIV, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SDIV:
			mir = curBB.CreateBinOpMIR(MirSDIV, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case MOD:
			mir = curBB.CreateBinOpMIR(MirMOD, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SMOD:
			mir = curBB.CreateBinOpMIR(MirSMOD, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case ADDMOD:
			mir = curBB.CreateTernaryOpMIR(MirADDMOD, valueStack)
			if depth >= 3 {
				depth -= 2
			} else {
				depth = 0
			}
		case MULMOD:
			mir = curBB.CreateTernaryOpMIR(MirMULMOD, valueStack)
			if depth >= 3 {
				depth -= 2
			} else {
				depth = 0
			}
		case EXP:
			mir = curBB.CreateBinOpMIR(MirEXP, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SIGNEXTEND:
			mir = curBB.CreateBinOpMIR(MirSIGNEXT, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case LT:
			mir = curBB.CreateBinOpMIR(MirLT, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case GT:
			mir = curBB.CreateBinOpMIR(MirGT, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SLT:
			mir = curBB.CreateBinOpMIR(MirSLT, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SGT:
			mir = curBB.CreateBinOpMIR(MirSGT, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case EQ:
			mir = curBB.CreateBinOpMIR(MirEQ, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case ISZERO:
			mir = curBB.CreateUnaryOpMIR(MirISZERO, valueStack)
		case AND:
			mir = curBB.CreateBinOpMIR(MirAND, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case OR:
			mir = curBB.CreateBinOpMIR(MirOR, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case XOR:
			mir = curBB.CreateBinOpMIR(MirXOR, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case NOT:
			mir = curBB.CreateUnaryOpMIR(MirNOT, valueStack)
		case BYTE:
			mir = curBB.CreateBinOpMIR(MirBYTE, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SHL:
			mir = curBB.CreateBinOpMIR(MirSHL, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SHR:
			mir = curBB.CreateBinOpMIR(MirSHR, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case SAR:
			mir = curBB.CreateBinOpMIR(MirSAR, valueStack)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case KECCAK256:
			mir = curBB.CreateBinOpMIRWithMA(MirKECCAK256, valueStack, memoryAccessor)
			if depth >= 2 {
				depth--
			} else {
				depth = 0
			}
		case ADDRESS:
			mir = curBB.CreateBlockInfoMIR(MirADDRESS, valueStack)
			depth++
			depthKnown = true
		case BALANCE:
			mir = curBB.CreateBlockInfoMIR(MirBALANCE, valueStack)
		case ORIGIN:
			mir = curBB.CreateBlockInfoMIR(MirORIGIN, valueStack)
			depth++
			depthKnown = true
		case CALLER:
			mir = curBB.CreateBlockInfoMIR(MirCALLER, valueStack)
			depth++
			depthKnown = true
		case CALLVALUE:
			mir = curBB.CreateBlockInfoMIR(MirCALLVALUE, valueStack)
			depth++
			depthKnown = true
		case CALLDATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATALOAD, valueStack)
		case CALLDATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATASIZE, valueStack)
			depth++
			depthKnown = true
		case CALLDATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATACOPY, valueStack)
			if depth >= 3 {
				depth -= 3
			} else {
				depth = 0
			}
		case CODESIZE:
			mir = curBB.CreateBlockInfoMIR(MirCODESIZE, valueStack)
			depth++
			depthKnown = true
		case CODECOPY:
			mir = curBB.CreateBlockInfoMIR(MirCODECOPY, valueStack)
			if depth >= 3 {
				depth -= 3
			} else {
				depth = 0
			}
		case GASPRICE:
			mir = curBB.CreateBlockInfoMIR(MirGASPRICE, valueStack)
			depth++
			depthKnown = true
		case EXTCODESIZE:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODESIZE, valueStack)
		case EXTCODECOPY:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODECOPY, valueStack)
			if depth >= 4 {
				depth -= 4
			} else {
				depth = 0
			}
		case RETURNDATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATASIZE, valueStack)
			depth++
			depthKnown = true
		case RETURNDATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATACOPY, valueStack)
			if depth >= 3 {
				depth -= 3
			} else {
				depth = 0
			}
		case EXTCODEHASH:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODEHASH, valueStack)
		case BLOCKHASH:
			mir = curBB.CreateBlockOpMIR(MirBLOCKHASH, valueStack)
		case COINBASE:
			mir = curBB.CreateBlockOpMIR(MirCOINBASE, valueStack)
			depth++
			depthKnown = true
		case TIMESTAMP:
			mir = curBB.CreateBlockOpMIR(MirTIMESTAMP, valueStack)
			depth++
			depthKnown = true
		case NUMBER:
			mir = curBB.CreateBlockOpMIR(MirNUMBER, valueStack)
			depth++
			depthKnown = true
		case DIFFICULTY:
			mir = curBB.CreateBlockOpMIR(MirDIFFICULTY, valueStack)
			depth++
			depthKnown = true
		case GASLIMIT:
			mir = curBB.CreateBlockOpMIR(MirGASLIMIT, valueStack)
			depth++
		case CHAINID:
			mir = curBB.CreateBlockOpMIR(MirCHAINID, valueStack)
			depth++
		case SELFBALANCE:
			mir = curBB.CreateBlockOpMIR(MirSELFBALANCE, valueStack)
			depth++
		case BASEFEE:
			mir = curBB.CreateBlockOpMIR(MirBASEFEE, valueStack)
			depth++
		case POP:
			_ = valueStack.pop()
			mir = nil
			if depth > 0 {
				depth--
			} else {
				depth = 0
			}
		case MLOAD:
			mir = curBB.CreateMemoryOpMIR(MirMLOAD, valueStack, memoryAccessor)
		case MSTORE:
			mir = curBB.CreateMemoryOpMIR(MirMSTORE, valueStack, memoryAccessor)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
		case MSTORE8:
			mir = curBB.CreateMemoryOpMIR(MirMSTORE8, valueStack, memoryAccessor)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
		case SLOAD:
			mir = curBB.CreateStorageOpMIR(MirSLOAD, valueStack, stateAccessor)
		case SSTORE:
			mir = curBB.CreateStorageOpMIR(MirSSTORE, valueStack, stateAccessor)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
		case JUMP:
			mir = curBB.CreateJumpMIR(MirJUMP, valueStack, nil)
			if depth >= 1 {
				depth--
			} else {
				depth = 0
			}
			if mir != nil {
				// Create a new basic block for the jump target
				if len(mir.oprands) > 0 && mir.oprands[0].payload != nil {
					// Interpret payload as big-endian integer of arbitrary length
					var targetPC uint64
					for _, b := range mir.oprands[0].payload {
						targetPC = (targetPC << 8) | uint64(b)
					}
					if targetPC < uint64(len(code)) {
						// Determine existence and whether this edge is newly added
						var hadParentBefore bool
						existingBB, targetExists := c.pcToBlock[uint(targetPC)]
						if targetExists && existingBB != nil {
							for _, p := range existingBB.Parents() {
								if p == curBB {
									hadParentBefore = true
									break
								}
							}
						}
						targetBB := c.createBB(uint(targetPC), curBB)
						targetBB.SetInitDepthMax(depth)
						// Only target is a child of current block for unconditional JUMP
						curBB.SetChildren([]*MIRBasicBlock{targetBB})
						// Record exit stack for current block and pass as incoming to target
						curBB.SetExitStack(valueStack.clone())
						targetBB.AddIncomingStack(curBB, curBB.ExitStack())
						// Optionally register fallthrough block at i+1 but do not enqueue or link
						if _, ok := c.pcToBlock[uint(i+1)]; !ok {
							fall := c.createBB(uint(i+1), nil)
							fall.SetInitDepthMax(depth)
						} else {
							if fall, ok2 := c.pcToBlock[uint(i+1)]; ok2 {
								fall.SetInitDepthMax(depth)
							}
						}
						// Enqueue target if newly created, or if it existed but just gained a new parent
						if !targetExists || (targetExists && !hadParentBefore) {
							// Rebuild only if entry height changed to avoid churn
							newEntryH := depth
							if targetBB.EntryStack() != nil {
								newEntryH = len(targetBB.EntryStack())
							}
							if !targetBB.queued && targetBB.lastEntryHeight != newEntryH {
								targetBB.queued = true
								targetBB.lastEntryHeight = newEntryH
								unprcessedBBs.Push(targetBB)
							}
						}
						return nil
					}
				}
			}
			return nil
		case JUMPI:
			mir = curBB.CreateJumpMIR(MirJUMPI, valueStack, nil)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
			if mir != nil {
				// Create new basic blocks for both true (target) and false (fallthrough) paths
				if len(mir.oprands) > 0 && mir.oprands[0].payload != nil {
					// Interpret payload as big-endian integer of arbitrary length
					var targetPC uint64
					for _, b := range mir.oprands[0].payload {
						targetPC = (targetPC << 8) | uint64(b)
					}
					if targetPC < uint64(len(code)) {
						// Determine existence and whether either edge is newly added
						var hadTargetParentBefore bool
						var hadFallParentBefore bool
						existingTarget, targetExists := c.pcToBlock[uint(targetPC)]
						if targetExists && existingTarget != nil {
							for _, p := range existingTarget.Parents() {
								if p == curBB {
									hadTargetParentBefore = true
									break
								}
							}
						}
						existingFall, fallExists := c.pcToBlock[uint(i+1)]
						if fallExists && existingFall != nil {
							for _, p := range existingFall.Parents() {
								if p == curBB {
									hadFallParentBefore = true
									break
								}
							}
						}
						// Create blocks for target and fallthrough
						targetBB := c.createBB(uint(targetPC), curBB)
						fallthroughBB := c.createBB(uint(i+1), curBB)
						targetBB.SetInitDepthMax(depth)
						fallthroughBB.SetInitDepthMax(depth)
						curBB.SetChildren([]*MIRBasicBlock{targetBB, fallthroughBB})
						// Record exit stack and add as incoming to both successors
						curBB.SetExitStack(valueStack.clone())
						targetBB.AddIncomingStack(curBB, curBB.ExitStack())
						fallthroughBB.AddIncomingStack(curBB, curBB.ExitStack())
						if !targetExists || (targetExists && !hadTargetParentBefore) {
							newEntryH := depth
							if targetBB.EntryStack() != nil {
								newEntryH = len(targetBB.EntryStack())
							}
							if !targetBB.queued && targetBB.lastEntryHeight != newEntryH {
								targetBB.queued = true
								targetBB.lastEntryHeight = newEntryH
								unprcessedBBs.Push(targetBB)
							}
						}
						if !fallExists || (fallExists && !hadFallParentBefore) {
							newEntryHF := depth
							if fallthroughBB.EntryStack() != nil {
								newEntryHF = len(fallthroughBB.EntryStack())
							}
							if !fallthroughBB.queued && fallthroughBB.lastEntryHeight != newEntryHF {
								fallthroughBB.queued = true
								fallthroughBB.lastEntryHeight = newEntryHF
								unprcessedBBs.Push(fallthroughBB)
							}
						}
						return nil
					}
				}
			}
			return nil
		case RJUMP:
			// mir = curBB.CreateJumpMIR(MirRJUMP, valueStack, nil)
			// return nil
			panic("not implemented")
		case RJUMPI:
			// mir = curBB.CreateJumpMIR(MirRJUMPI, valueStack, nil)
			// return nil
			panic("not implemented")
		case RJUMPV:
			// mir = curBB.CreateJumpMIR(MirRJUMPV, valueStack, nil)
			// return nil
			panic("not implemented")
		case JUMPDEST:
			// If we hit a JUMPDEST, we should create a new basic block
			// unless this is the first instruction
			if curBB.Size() > 0 {
				newBB := c.createBB(uint(i), curBB)
				newBB.SetInitDepth(depth)
				curBB.SetChildren([]*MIRBasicBlock{newBB})
				// Record exit stack and feed as incoming to fallthrough
				curBB.SetExitStack(valueStack.clone())
				newBB.AddIncomingStack(curBB, curBB.ExitStack())
				unprcessedBBs.Push(newBB)
				return nil
			}

			mir = curBB.CreateVoidMIR(MirJUMPDEST)
			// Ensure generation-time stack depth reflects current entry depth
			if mir != nil {
				mir.genStackDepth = valueStack.size()
			}
		case PC:
			mir = curBB.CreateBlockInfoMIR(MirPC, valueStack)
			depth++
		case MSIZE:
			mir = curBB.CreateMemoryOpMIR(MirMSIZE, valueStack, memoryAccessor)
			depth++
		case GAS:
			mir = curBB.CreateBlockInfoMIR(MirGAS, valueStack)
			depth++
		case BLOBHASH:
			mir = curBB.CreateBlockInfoMIR(MirBLOBHASH, valueStack)
		case BLOBBASEFEE:
			mir = curBB.CreateBlockInfoMIR(MirBLOBBASEFEE, valueStack)
			depth++
		case TLOAD:
			mir = curBB.CreateStorageOpMIR(MirTLOAD, valueStack, stateAccessor)
		case TSTORE:
			mir = curBB.CreateStorageOpMIR(MirTSTORE, valueStack, stateAccessor)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
		case MCOPY:
			// MCOPY takes 3 operands: dest, src, length
			length := valueStack.pop()
			src := valueStack.pop()
			dest := valueStack.pop()
			mir = new(MIR)
			mir.op = MirMCOPY
			mir.oprands = []*Value{&dest, &src, &length}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(src, length)
				memoryAccessor.recordStore(dest, length, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			if depth >= 3 {
				depth -= 3
			} else {
				depth = 0
			}
		case PUSH0:
			_ = curBB.CreatePushMIR(0, []byte{}, valueStack)
			depth++
		case LOG0:
			mir = curBB.CreateLogMIR(MirLOG0, valueStack)
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
		case LOG1:
			mir = curBB.CreateLogMIR(MirLOG1, valueStack)
			if depth >= 3 {
				depth -= 3
			} else {
				depth = 0
			}
		case LOG2:
			mir = curBB.CreateLogMIR(MirLOG2, valueStack)
			if depth >= 4 {
				depth -= 4
			} else {
				depth = 0
			}
		case LOG3:
			mir = curBB.CreateLogMIR(MirLOG3, valueStack)
			if depth >= 5 {
				depth -= 5
			} else {
				depth = 0
			}
		case LOG4:
			mir = curBB.CreateLogMIR(MirLOG4, valueStack)
			if depth >= 6 {
				depth -= 6
			} else {
				depth = 0
			}
		case CREATE:
			// CREATE takes 3 operands: value, offset, size
			size := valueStack.pop()
			offset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCREATE
			mir.oprands = []*Value{&value, &offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			curBB.SetExitStack(valueStack.clone())
			fallthroughBB.AddIncomingStack(curBB, curBB.ExitStack())
			if depth >= 3 {
				depth = depth - 3 + 1
			} else {
				depth = 1
			}
			return nil
		case CREATE2:
			// CREATE2 takes 4 operands: value, offset, size, salt
			salt := valueStack.pop()
			size := valueStack.pop()
			offset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCREATE2
			mir.oprands = []*Value{&value, &offset, &size, &salt}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			curBB.SetExitStack(valueStack.clone())
			fallthroughBB.AddIncomingStack(curBB, curBB.ExitStack())
			if depth >= 4 {
				depth = depth - 4 + 1
			} else {
				depth = 1
			}
			return nil
		case CALL:
			// CALL takes 7 operands: gas, addr, value, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALL
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			curBB.SetExitStack(valueStack.clone())
			fallthroughBB.AddIncomingStack(curBB, curBB.ExitStack())
			if depth >= 7 {
				depth = depth - 7 + 1
			} else {
				depth = 1
			}
			return nil
		case CALLCODE:
			// CALLCODE takes same operands as CALL
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALLCODE
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			if depth >= 7 {
				depth = depth - 7 + 1
			} else {
				depth = 1
			}
			return nil
		case RETURN:
			// RETURN takes 2 operands: offset (top), size
			offset := valueStack.pop()
			size := valueStack.pop()
			mir = new(MIR)
			mir.op = MirRETURN
			mir.oprands = []*Value{&offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
			return nil
		case DELEGATECALL:
			// DELEGATECALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirDELEGATECALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			if depth >= 6 {
				depth = depth - 6 + 1
			} else {
				depth = 1
			}
			return nil
		case STATICCALL:
			// STATICCALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirSTATICCALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			if depth >= 6 {
				depth = depth - 6 + 1
			} else {
				depth = 1
			}
			return nil
		case REVERT:
			// REVERT takes 2 operands: offset, size
			size := valueStack.pop()
			offset := valueStack.pop()
			mir = new(MIR)
			mir.op = MirREVERT
			mir.oprands = []*Value{&offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			if depth >= 2 {
				depth -= 2
			} else {
				depth = 0
			}
			return nil
		case INVALID:
			mir = curBB.CreateVoidMIR(MirINVALID)
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
		case SELFDESTRUCT:
			// SELFDESTRUCT takes 1 operand: address
			addr := valueStack.pop()
			mir = new(MIR)
			mir.op = MirSELFDESTRUCT
			mir.oprands = []*Value{&addr}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
			// Stack operations - DUP1 to DUP16 (fail CFG build if stack too shallow)
		case DUP1, DUP2, DUP3, DUP4, DUP5, DUP6, DUP7, DUP8, DUP9, DUP10, DUP11, DUP12, DUP13, DUP14, DUP15, DUP16:
			n := int(op - DUP1 + 1)
			if depthKnown && depth < n {
				log.Warn("MIR DUP depth underflow - emitting NOP", "need", n, "have", depth, "pc", i, "bb", curBB.blockNum, "bbFirst", curBB.firstPC, "bbInit", curBB.InitDepth())
				// Debug dump current BB and ancestors to diagnose
				debugDumpBB("current", curBB)
				debugDumpAncestors(curBB, make(map[*MIRBasicBlock]bool), curBB.blockNum)
				debugWriteDOTIfRequested(debugDumpAncestryDOT(curBB), "dup")
				log.Warn("----------- MIR DUP depth underflow---------------------")
			}
			mir = curBB.CreateStackOpMIR(MirOperation(0x80+byte(n-1)), valueStack)
			if depth >= n {
				depth++
			}
			// Stack operations - SWAP1 to SWAP16 (fail CFG build if stack too shallow)
		case SWAP1, SWAP2, SWAP3, SWAP4, SWAP5, SWAP6, SWAP7, SWAP8, SWAP9, SWAP10, SWAP11, SWAP12, SWAP13, SWAP14, SWAP15, SWAP16:
			n := int(op - SWAP1 + 1)
			if depthKnown && depth <= n {
				log.Warn("MIR SWAP depth underflow - emitting NOP", "need", n+1, "have", depth, "pc", i, "bb", curBB.blockNum, "bbFirst", curBB.firstPC, "bbInit", curBB.InitDepth())
				// Debug dump current BB and ancestors to diagnose
				debugDumpBB("current", curBB)
				debugDumpAncestors(curBB, make(map[*MIRBasicBlock]bool), curBB.blockNum)
				debugWriteDOTIfRequested(debugDumpAncestryDOT(curBB), "swap")
			}
			mir = curBB.CreateStackOpMIR(MirOperation(0x90+byte(n-1)), valueStack)
		// EOF operations
		case DATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirDATALOAD, valueStack)
		case DATALOADN:
			mir = curBB.CreateBlockInfoMIR(MirDATALOADN, valueStack)
		case DATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirDATASIZE, valueStack)
		case DATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirDATACOPY, valueStack)
		case CALLF:
			// CALLF takes 2 operands: gas, function_id
			functionID := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALLF
			mir.oprands = []*Value{&gas, &functionID}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			// Depth: pop2 push1
			if depth >= 2 {
				depth = depth - 2 + 1
			} else {
				depth = 1
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			fallthroughBB.SetInitDepth(depth)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case RETF:
			mir = curBB.CreateVoidMIR(MirRETF)
			return nil
		case JUMPF:
			mir = curBB.CreateJumpMIR(MirJUMPF, valueStack, nil)
			return nil
		case DUPN:
			mir = curBB.CreateStackOpMIR(MirDUPN, valueStack)
		case SWAPN:
			mir = curBB.CreateStackOpMIR(MirSWAPN, valueStack)
		case EXCHANGE:
			mir = curBB.CreateStackOpMIR(MirEXCHANGE, valueStack)
		case EOFCREATE:
			// EOFCREATE takes 4 operands: value, code_offset, code_size, salt
			salt := valueStack.pop()
			codeSize := valueStack.pop()
			codeOffset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEOFCREATE
			mir.oprands = []*Value{&value, &codeOffset, &codeSize, &salt}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			// Depth: pop4 push1
			if depth >= 4 {
				depth = depth - 4 + 1
			} else {
				depth = 1
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			fallthroughBB.SetInitDepth(depth)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case RETURNCONTRACT:
			mir = curBB.CreateVoidMIR(MirRETURNCONTRACT)
		// Additional opcodes
		case RETURNDATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATALOAD, valueStack)
		case EXTCALL:
			// EXTCALL takes 7 operands: gas, addr, value, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTCALL
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case EXTDELEGATECALL:
			// EXTDELEGATECALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTDELEGATECALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case EXTSTATICCALL:
			// EXTSTATICCALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTSTATICCALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		default:
			// Tolerate customized/fused opcodes (0xb0-0xcf) by treating them as NOPs
			if op >= 0xb0 && op <= 0xcf {
				_ = curBB.CreateVoidMIR(MirNOP)
				// continue scanning
				break
			}
			// For any other unknown opcode, terminate the current basic block
			// as if hitting INVALID (unreachable metadata/data regions)
			_ = curBB.CreateVoidMIR(MirINVALID)
			curBB.SetLastPC(uint(i))
			return nil
		}

		// Record source pc on generated MIR (best-effort; some early-return paths set it earlier or remain nil)
		if mir != nil {
			pcCopy := uint(i)
			mir.pc = &pcCopy
		}

		i++
	}
	return nil
}
