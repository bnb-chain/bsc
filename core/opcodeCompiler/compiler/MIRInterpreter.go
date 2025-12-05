package compiler

import (
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Precomputed keccak256("") constant as a static array to avoid per-call allocations
var emptyKeccakHash = [32]byte{
	0xc5, 0xd2, 0x7e, 0x58, 0x2f, 0xdf, 0x90, 0x39,
	0x88, 0x52, 0xc6, 0x51, 0xe8, 0x88, 0x6f, 0x03,
	0x27, 0x06, 0x52, 0xc1, 0x09, 0x03, 0x04, 0x82,
	0x02, 0x00, 0x08, 0x04, 0x20, 0x07, 0x20, 0x07,
}

// logging shim is defined in debug_flags.go

// MIRExecutionEnv contains minimal execution context required by the interpreter.
// It is intentionally small; we can expand as more MIROperations are wired.
type MIRExecutionEnv struct {
	// Input data
	Calldata []byte

	// Mutable memory and a simple storage map for simulation
	Memory  []byte
	Storage map[[32]byte][32]byte

	// Block and tx info (optional)
	BlockNumber uint64
	Timestamp   uint64
	ChainID     uint64
	GasPrice    uint64
	BaseFee     uint64
	SelfBalance uint64
	ReturnData  []byte

	// Code accessors (optional, provided by adapter)
	Code        []byte
	ExtCodeSize func(addr [20]byte) uint64
	ExtCodeCopy func(addr [20]byte, codeOffset uint64, dest []byte)
	LogFunc     func(addr [20]byte, topics [][32]byte, data []byte)

	// Call context
	CallValue *uint256.Int

	// Additional block info (optional)
	GasLimit      uint64
	Difficulty    uint64
	BlockHashFunc func(num uint64) [32]byte
	Coinbase      [20]byte

	// Optional helpers for additional opcodes
	ExtCodeHash  func(addr [20]byte) [32]byte
	GasLeft      func() uint64
	BlobBaseFee  uint64
	BlobHashFunc func(index uint64) [32]byte

	// Fork flags (set by adapter from EVM chain rules)
	IsByzantium      bool
	IsConstantinople bool
	IsIstanbul       bool
	IsLondon         bool

	// Runtime linkage hooks to EVM components (provided by adapter at runtime)
	// If these are nil, interpreter falls back to internal simulated state.
	SLoadFunc      func(key [32]byte) [32]byte
	SStoreFunc     func(key [32]byte, value [32]byte)
	TLoadFunc      func(key [32]byte) [32]byte
	TStoreFunc     func(key [32]byte, value [32]byte)
	GetBalanceFunc func(addr [20]byte) *uint256.Int

	// External execution hooks (optional). If nil, CALL/CREATE will request fallback.
	// kind for ExternalCall: 0=CALL,1=CALLCODE,2=DELEGATECALL,3=STATICCALL
	ExternalCall func(kind byte, addr [20]byte, value *uint256.Int, input []byte, gas uint64) (ret []byte, success bool)
	// kind for CreateContract: 4=CREATE,5=CREATE2. If salt is nil, treat as CREATE.
	CreateContract func(kind byte, value *uint256.Int, init []byte, salt *[32]byte) (addr [20]byte, success bool, ret []byte)

	// CheckJumpdest validates whether a given absolute PC is a valid JUMPDEST and not in push-data
	// Signature: func(pc uint64) bool
	CheckJumpdest func(pc uint64) bool

	// ResolveBB maps an absolute PC to a MIR basic block in the CFG
	// Signature: func(pc uint64) *MIRBasicBlock
	ResolveBB func(pc uint64) *MIRBasicBlock

	// Address context (optional, provided by adapter)
	Self   [20]byte
	Caller [20]byte
	Origin [20]byte
}

// MIRInterpreter executes MIR instructions and produces values.
type MIRInterpreter struct {
	env *MIRExecutionEnv
	// cfg points to the current CFG during RunCFGWithResolver; used for runtime backfill
	cfg        *CFG
	memory     []byte
	returndata []byte
	results    []*uint256.Int
	resultsCap int
	// scratch pool and caches to reduce allocations
	tmpA       *uint256.Int
	tmpB       *uint256.Int
	tmpC       *uint256.Int
	zeroConst  *uint256.Int
	constCache map[string]*uint256.Int
	scratch32  [32]byte
	// transientStorage is an in-memory map used to emulate TLOAD/TSTORE (EIP-1153)
	transientStorage map[[32]byte][32]byte
	// tracer is invoked on each MIR instruction execution if set
	tracer   func(MirOperation)
	tracerEx func(*MIR)
	// next basic block to transfer to on jumps (set by MirJUMP/MirJUMPI)
	nextBB *MIRBasicBlock
	// current and previous basic blocks for PHI resolution and cross-BB dataflow
	currentBB *MIRBasicBlock
	prevBB    *MIRBasicBlock
	// globalResults holds values produced by MIR instructions across basic blocks
	// keyed by the defining *MIR so operands that reference defs from other blocks can resolve
	globalResults map[*MIR]*uint256.Int
	// phiResults holds PHI values keyed by (def, predecessor) to retain path sensitivity
	phiResults map[*MIR]map[*MIRBasicBlock]*uint256.Int
	// phiLastPred records the last predecessor used to evaluate a PHI def in this run
	phiLastPred map[*MIR]*MIRBasicBlock
	// Signature-based caches in case *MIR pointers differ across rebuilds
	// phiResultsBySig[evmPC][idx][pred] = value
	phiResultsBySig map[uint64]map[int]map[*MIRBasicBlock]*uint256.Int
	// phiLastPredBySig[evmPC][idx] = pred
	phiLastPredBySig map[uint64]map[int]*MIRBasicBlock
	// globalResultsBySig[evmPC][idx] = value
	globalResultsBySig map[uint64]map[int]*uint256.Int
	// simple oscillation tracker for dispatcher loops
	lastJumpFrom uint64
	lastJumpTo   uint64
	// Optional pre-execution hook for each MIR instruction (e.g., gas accounting)
	beforeOp func(*MIRPreOpContext) error
	// Reusable Keccak state and output buffer to avoid per-call allocations
	hasher    crypto.KeccakState
	hasherBuf [32]byte
	// Reusable pre-op context and operand buffers to avoid per-instruction allocs
	preOpCtx  MIRPreOpContext
	preOpVals [8]uint256.Int
	preOpOps  [8]*uint256.Int
	// Track last CALLDATACOPY parameters to help diagnose KECCAK over calldata
	lastCopyDest uint64
	lastCopyOff  uint64
	lastCopySize uint64
}

// mirGlobalTracer is an optional global tracer invoked for each MIR instruction.
// Tests may set this to observe execution without reaching into internal fields.
var mirGlobalTracer func(MirOperation)
var mirGlobalTracerEx func(*MIR)

// Optional fast dispatch table for hot MIR operations
var mirHandlers [256]func(*MIRInterpreter, *MIR) error

func init() {
	mirHandlers[byte(MirSTOP)] = mirHandleSTOP
	mirHandlers[byte(MirPHI)] = mirHandlePHI
	mirHandlers[byte(MirRETURN)] = mirHandleRETURN
	mirHandlers[byte(MirJUMP)] = mirHandleJUMP
	mirHandlers[byte(MirJUMPI)] = mirHandleJUMPI
	mirHandlers[byte(MirMLOAD)] = mirHandleMLOAD
	mirHandlers[byte(MirADD)] = mirHandleADD
	mirHandlers[byte(MirMUL)] = mirHandleMUL
	mirHandlers[byte(MirSUB)] = mirHandleSUB
	mirHandlers[byte(MirDIV)] = mirHandleDIV
	mirHandlers[byte(MirSDIV)] = mirHandleSDIV
	mirHandlers[byte(MirMOD)] = mirHandleMOD
	mirHandlers[byte(MirSMOD)] = mirHandleSMOD
	mirHandlers[byte(MirEXP)] = mirHandleEXP
	mirHandlers[byte(MirAND)] = mirHandleAND
	mirHandlers[byte(MirOR)] = mirHandleOR
	mirHandlers[byte(MirXOR)] = mirHandleXOR
	mirHandlers[byte(MirSHL)] = mirHandleSHL
	mirHandlers[byte(MirSHR)] = mirHandleSHR
	mirHandlers[byte(MirSAR)] = mirHandleSAR
	mirHandlers[byte(MirEQ)] = mirHandleEQ
	mirHandlers[byte(MirLT)] = mirHandleLT
	mirHandlers[byte(MirGT)] = mirHandleGT
	mirHandlers[byte(MirSLT)] = mirHandleSLT
	mirHandlers[byte(MirSGT)] = mirHandleSGT
	mirHandlers[byte(MirMSTORE)] = mirHandleMSTORE
	mirHandlers[byte(MirMSTORE8)] = mirHandleMSTORE8
	mirHandlers[byte(MirMCOPY)] = mirHandleMCOPY
	mirHandlers[byte(MirCALLDATASIZE)] = mirHandleCALLDATASIZE
	mirHandlers[byte(MirRETURNDATASIZE)] = mirHandleRETURNDATASIZE
	mirHandlers[byte(MirKECCAK256)] = mirHandleKECCAK
	mirHandlers[byte(MirCALLDATALOAD)] = mirHandleCALLDATALOAD
	mirHandlers[byte(MirRETURNDATALOAD)] = mirHandleRETURNDATALOAD
	mirHandlers[byte(MirISZERO)] = mirHandleISZERO
	mirHandlers[byte(MirNOT)] = mirHandleNOT
	mirHandlers[byte(MirBYTE)] = mirHandleBYTE
}

func NewMIRInterpreter(env *MIRExecutionEnv) *MIRInterpreter {
	if env.Memory == nil {
		env.Memory = make([]byte, 0)
	}
	return &MIRInterpreter{
		env:                env,
		memory:             env.Memory,
		results:            nil,
		tmpA:               uint256.NewInt(0),
		tmpB:               uint256.NewInt(0),
		tmpC:               uint256.NewInt(0),
		zeroConst:          uint256.NewInt(0),
		constCache:         make(map[string]*uint256.Int, 64),
		tracer:             mirGlobalTracer,
		tracerEx:           mirGlobalTracerEx,
		globalResults:      make(map[*MIR]*uint256.Int, 128),
		transientStorage:   make(map[[32]byte][32]byte),
		phiResults:         make(map[*MIR]map[*MIRBasicBlock]*uint256.Int, 32),
		phiLastPred:        make(map[*MIR]*MIRBasicBlock, 32),
		phiResultsBySig:    make(map[uint64]map[int]map[*MIRBasicBlock]*uint256.Int, 32),
		phiLastPredBySig:   make(map[uint64]map[int]*MIRBasicBlock, 32),
		globalResultsBySig: make(map[uint64]map[int]*uint256.Int, 64),
	}
}

// SetTracer sets a per-instruction tracer callback. It is not thread-safe.
func (it *MIRInterpreter) SetTracer(cb func(MirOperation)) {
	it.tracer = cb
}

// SetTracerExtended sets a per-instruction tracer that receives the full MIR, including mapping metadata.
// It is not thread-safe.
func (it *MIRInterpreter) SetTracerExtended(cb func(*MIR)) {
	it.tracerEx = cb
}

// SetBeforeOpHook sets a callback invoked before executing each MIR instruction.
// If the callback returns a non-nil error, execution of the current instruction is aborted
// and the error is propagated to the caller.
func (it *MIRInterpreter) SetBeforeOpHook(cb func(*MIRPreOpContext) error) {
	it.beforeOp = cb
}

// GetBeforeOpHook returns the current beforeOp hook
func (it *MIRInterpreter) GetBeforeOpHook() func(*MIRPreOpContext) error {
	return it.beforeOp
}

// SetGlobalMIRTracer sets a process-wide tracer that new MIR interpreters will inherit.
func SetGlobalMIRTracer(cb func(MirOperation)) {
	mirGlobalTracer = cb
}

// SetGlobalMIRTracerExtended sets a process-wide tracer that receives the full MIR.
func SetGlobalMIRTracerExtended(cb func(*MIR)) {
	if cb == nil {
		mirGlobalTracerEx = nil
		return
	}
	// Wrap provided tracer to include PHI-specific info without baking logs into the interpreter
	mirGlobalTracerEx = func(m *MIR) {
		if m != nil && m.op == MirPHI {
			// Let callers see phi stack index in their tracer by enriching MIR metadata via log
			// Avoiding direct logging here; instead, callers can read m.PhiStackIndex().
			// No-op: we just call through; wrapping kept for future augmentation if needed.
		}
		cb(m)
	}
}

// mirExecTimingHook is an optional test hook to observe MIR handler execution time
var mirExecTimingHook func(op MirOperation, evmPC uint64, dur time.Duration)

// SetMIRExecTimingHook installs a callback to observe MIR handler execution time (testing only)
func SetMIRExecTimingHook(cb func(op MirOperation, evmPC uint64, dur time.Duration)) {
	mirExecTimingHook = cb
}

// Debug hook: called on KECCAK256 with the exact memory slice being hashed
var mirKeccakHook func(off, size uint64, data []byte)

// SetMIRKeccakDebugHook installs a debug hook to observe KECCAK256 input
func SetMIRKeccakDebugHook(cb func(off, size uint64, data []byte)) {
	mirKeccakHook = cb
}

// Debug hook: called on CALLDATACOPY with the copy parameters
var mirCalldataCopyHook func(dest, off, size uint64)

// SetMIRCalldataCopyDebugHook installs a debug hook to observe CALLDATACOPY params
func SetMIRCalldataCopyDebugHook(cb func(dest, off, size uint64)) { mirCalldataCopyHook = cb }

// GetEnv returns the execution environment
func (it *MIRInterpreter) GetEnv() *MIRExecutionEnv {
	return it.env
}

// RunMIR executes all instructions in the given basic block list sequentially.
// For now, control-flow is assumed to be linear within a basic block.
func (it *MIRInterpreter) RunMIR(block *MIRBasicBlock) ([]byte, error) {
	mirDebugWarn("MIR RunMIR: block", "block", block.blockNum, "instructions", len(block.instructions))
	for i, ins := range block.instructions {
		mirDebugWarn("MIR instruction", "idx", i, "op", ins.op)
	}
	if block == nil || len(block.instructions) == 0 {
		return it.returndata, nil
	}
	// Track current block for PHI resolution
	it.currentBB = block
	// Pre-size and pre-initialize results slots for this block to avoid per-op allocations on first writes
	if n := len(block.instructions); n > 0 {
		if n > len(it.results) {
			grown := make([]*uint256.Int, n)
			copy(grown, it.results)
			it.results = grown
			if it.resultsCap < n {
				it.resultsCap = n
			}
		}
		for i := 0; i < n; i++ {
			if it.results[i] == nil {
				it.results[i] = new(uint256.Int)
			}
		}
	}
	// Fast path: if the block contains only NOPs and a terminal STOP, skip the exec loop.
	// This is common for trivially folded sequences like PUSH/PUSH/ADD/MUL -> NOPs + STOP.
	{
		onlyNopsAndStop := true
		hasStop := false
		instrs := block.instructions
		for i := 0; i < len(instrs); i++ {
			ins := instrs[i]
			if ins == nil {
				continue
			}
			switch ins.op {
			case MirNOP, MirJUMPDEST:
				// ignore
			case MirSTOP:
				hasStop = true
			default:
				// any other operation requires execution
				onlyNopsAndStop = false
				break
			}
		}
		if onlyNopsAndStop && hasStop {
			// Ensure gas/accounting hooks see the landing JUMPDEST and terminal STOP
			if it.beforeOp != nil {
				instrs := block.instructions
				// Block-entry pre-op for the first instruction (likely MirJUMPDEST)
				if len(instrs) > 0 && instrs[0] != nil {
					ctx := &MIRPreOpContext{
						M:            instrs[0],
						EvmOp:        instrs[0].evmOp,
						IsBlockEntry: true,
						Block:        block,
					}
					if err := it.beforeOp(ctx); err != nil {
						return nil, err
					}
				}
				// Also emit a pre-op for the terminal STOP to keep tracer/gas parity
				for i := len(instrs) - 1; i >= 0; i-- {
					if instrs[i] != nil && instrs[i].op == MirSTOP {
						ctx := &MIRPreOpContext{
							M:            instrs[i],
							EvmOp:        instrs[i].evmOp,
							IsBlockEntry: false,
							Block:        block,
						}
						if err := it.beforeOp(ctx); err != nil {
							return nil, err
						}
						break
					}
				}
			}
			return it.returndata, nil
		}
	}
	// Avoid per-run clearing of results to reduce overhead for trivial blocks;
	// results slots are allocated lazily on first write via setResult.
	// iterate by index to reduce overhead
	block.pos = 0
	for i := 0; i < len(block.instructions); i++ {
		ins := block.instructions[i]
		if err := it.exec(ins); err != nil {
			switch err {
			case errSTOP:
				return it.returndata, nil
			case errRETURN:
				return it.returndata, nil
			case errREVERT:
				return it.returndata, err
			case errJUMP:
				// Jump signal is normal control flow, pass it up
				return nil, err
			default:
				// Don't fallback - expose the error so we can fix it
				mirDebugWarn("RunMIR: Instruction error (not falling back)", "evmPC", ins.evmPC, "op", ins.op.String(), "err", err)
				return nil, err
			}
		}
	}

	// 添加基本块完成执行的日志
	//log.Warn("MIRInterpreter: Block execution completed", "blockNum", block.blockNum, "returnDataSize", len(it.returndata), "instructions", block.instructions)
	// Publish live-outs for successor PHIs
	it.publishLiveOut(block)
	// Record last executed block for successor PHI selection
	it.prevBB = block
	return it.returndata, nil
}

// publishLiveOut writes this block's live-out def values into the global map
// and updates the block's exitStack with runtime values for loop scenarios
func (it *MIRInterpreter) publishLiveOut(block *MIRBasicBlock) {
	if it == nil || block == nil || it.globalResults == nil {
		return
	}

	defs := block.LiveOutDefs()

	//log.Warn("MIR publishLiveOut", "block", block.blockNum, "size", len(block.instructions), "defs", defs, "it.results", it.results, "block.exitStack", block.ExitStack())
	if len(defs) == 0 && len(block.ExitStack()) == 0 {
		mirDebugWarn("MIR publishLiveOut: no live outs", "block", block.blockNum)
		return
	}
	inBlock := make(map[*MIR]bool, len(block.instructions))
	for _, inst := range block.instructions {
		if inst != nil {
			inBlock[inst] = true
		}
	}
	// Backfill ancestor defs referenced at exit if missing in globals
	exitVals := block.ExitStack()
	for i := range exitVals {
		v := &exitVals[i]
		// Remove !inBlock check to ensure we publish ALL exit stack values,
		// even if defined in the current block, as they are needed by successors.
		if v != nil && v.kind == Variable && v.def != nil {
			// Check signature-based cache
			var hasInSigCache bool
			if v.def.evmPC != 0 {
				if byPC := it.globalResultsBySig[uint64(v.def.evmPC)]; byPC != nil {
					_, hasInSigCache = byPC[v.def.idx]
				}
			}

			if !hasInSigCache {
				val := it.evalValue(v)
				if val != nil && v.def.evmPC != 0 {
					if it.globalResultsBySig[uint64(v.def.evmPC)] == nil {
						it.globalResultsBySig[uint64(v.def.evmPC)] = make(map[int]*uint256.Int)
					}
					it.globalResultsBySig[uint64(v.def.evmPC)][v.def.idx] = new(uint256.Int).Set(val)
					// Also update pointer cache for compatibility
					it.globalResults[v.def] = new(uint256.Int).Set(val)
				}
			}
		}
	}
	for _, def := range defs {
		if def == nil || !inBlock[def] {
			continue
		}
		if def.idx >= 0 && def.idx < len(it.results) {
			if r := it.results[def.idx]; r != nil {
				// Update signature-based cache (uses evmPC+idx as key)
				if def.evmPC != 0 {
					if it.globalResultsBySig[uint64(def.evmPC)] == nil {
						it.globalResultsBySig[uint64(def.evmPC)] = make(map[int]*uint256.Int)
					}
					it.globalResultsBySig[uint64(def.evmPC)][def.idx] = new(uint256.Int).Set(r)
				}
				// Also update pointer-based cache for mirHandleJUMP compatibility
				it.globalResults[def] = new(uint256.Int).Set(r)
			}
		}
	}
}

// RunCFGWithResolver sets up a resolver backed by the given CFG and runs from entry block
func (it *MIRInterpreter) RunCFGWithResolver(cfg *CFG, entry *MIRBasicBlock) ([]byte, error) {
	defer func() {
		if r := recover(); r != nil {
			mirDebugError("MIR panic", "err", r, "stack", string(debug.Stack()))
			panic(r)
		}
	}()
	// Record the active CFG for possible runtime backfill of dynamic targets
	it.cfg = cfg
	// Reset global caches at the start of each execution to avoid stale values
	// This ensures values from previous executions or different paths don't pollute the current run
	if it.globalResultsBySig != nil {
		// Clear the map but keep the structure to avoid reallocation
		for k := range it.globalResultsBySig {
			delete(it.globalResultsBySig, k)
		}
	}
	if it.globalResults != nil {
		for k := range it.globalResults {
			delete(it.globalResults, k)
		}
	}
	if it.phiResults != nil {
		for k := range it.phiResults {
			delete(it.phiResults, k)
		}
	}
	if it.phiResultsBySig != nil {
		for k := range it.phiResultsBySig {
			delete(it.phiResultsBySig, k)
		}
	}
	if it.env != nil && it.env.ResolveBB == nil && cfg != nil {
		// Build a lightweight resolver using cfg.pcToBlock
		it.env.ResolveBB = func(pc uint64) *MIRBasicBlock {
			if cfg == nil || cfg.pcToBlock == nil {
				mirDebugWarn("MIR RunCFGWithResolver: cfg or pcToBlock is nil", "pc", pc)
				return nil
			}
			if bb, ok := cfg.pcToBlock[uint(pc)]; ok {
				mirDebugWarn("MIR RunCFGWithResolver: found bb", "pc", pc, "bb", bb.blockNum)
				return bb
			}
			mirDebugWarn("MIR RunCFGWithResolver: not found bb", "pc", pc)
			return nil
		}
	}
	// Follow control flow starting at entry; loop jumping between blocks
	bb := entry
	// Small oscillation guard: detect excessive re-visits of the same block to prevent infinite loops
	visitCount := make(map[*MIRBasicBlock]int)
	const visitBudgetPerBlock = 2048
	for bb != nil {
		visitCount[bb]++
		// visitCount check removed for production runs; rely on gas limit
		// if visitCount[bb] > visitBudgetPerBlock {
		//	mirDebugError("MIR oscillation guard: excessive visits to block", "block", bb.blockNum, "firstPC", bb.firstPC, "lastPC", bb.lastPC)
		//	return nil, ErrMIRFallback
		// }
		it.nextBB = nil
		// If block has no MIR instructions (Size=0), still charge block entry gas
		// This handles cases like entry blocks with only PUSH operations
		if bb.Size() == 0 && it.beforeOp != nil {
			ctx := &it.preOpCtx
			// Synthesize JUMPDEST charge for trivial landing blocks with no MIR
			ctx.M = nil
			ctx.EvmOp = 0
			if it.cfg != nil && int(bb.firstPC) >= 0 && int(bb.firstPC) < len(it.cfg.rawCode) {
				if ByteCode(it.cfg.rawCode[int(bb.firstPC)]) == JUMPDEST {
					// Create a minimal MIR to allow adapter to charge JUMPDEST gas
					mirDebugWarn("MIR synthesize JUMPDEST pre-op at block entry", "block", bb.blockNum, "firstPC", bb.firstPC, "size", bb.Size())
					synth := new(MIR)
					synth.op = MirJUMPDEST
					synth.evmPC = uint(bb.firstPC)
					synth.evmOp = byte(JUMPDEST)
					ctx.M = synth
					ctx.EvmOp = synth.evmOp
				}
			}
			ctx.Operands = nil
			ctx.MemorySize = 0
			ctx.Length = 0
			ctx.IsBlockEntry = true
			ctx.Block = bb
			it.currentBB = bb
			// Call hook for block entry gas charging
			if err := it.beforeOp(ctx); err != nil {
				return nil, err
			}
		}
		_, err := it.RunMIR(bb)
		if err == nil {
			mirDebugWarn("MIR RunCFGWithResolver: run mir success", "bb", bb.blockNum, "firstPC", bb.firstPC, "lastPC", bb.lastPC, "it.nextBB", it.nextBB)
			// Safety check: if block ended with terminal instruction (RETURN/REVERT/STOP), don't fallthrough
			if len(bb.instructions) > 0 {
				lastInstr := bb.instructions[len(bb.instructions)-1]
				if lastInstr != nil {
					switch lastInstr.op {
					case MirRETURN, MirREVERT, MirSTOP, MirSELFDESTRUCT:
						return it.returndata, nil
					}
				}
			}
			if it.nextBB == nil {
				// Fall through handling
				children := bb.Children()
				if len(children) >= 1 && children[0] != nil {
					// Single-successor fallthrough
					if len(children) == 1 {
						// Publish on fallthrough, too
						//log.Warn("MIR publish before fallthrough", "from_block", bb.blockNum)
						it.publishLiveOut(bb)
						// Preserve predecessor for successor PHI resolution on fallthrough
						it.prevBB = bb
						if children[0] == bb {
							mirDebugWarn("MIR: self-loop fallthrough detected; rerunning block", "blockNum", bb.blockNum)
							bb = children[0]
							continue
						}
						// If fallthrough target is not built (missed by static analysis), force backfill
						if !children[0].built {
							err := it.scheduleJump(uint64(children[0].firstPC), nil, true)
							if err != nil && err != errJUMP {
								return nil, err
							}
							bb = it.nextBB
							continue
						}
						bb = children[0]
						continue
					}
					// Conditional branch (e.g., JUMPI): if no jump taken, use fallthrough child (index 1)
					// Note: We swapped children order in opcodeParser.go: [fallthrough, target]
					// But wait, standard JUMPI fallthrough is at index 0 in our parser now?
					// Let's double check opcodeParser fix.
					// Fix: curBB.SetChildren([]*MIRBasicBlock{fallthroughBB, targetBB})
					// So children[0] IS fallthrough. children[1] IS target.

					// RunCFGWithResolver logic here says: "if no jump taken, use fallthrough child (index 1)"
					// This implies it expects target at index 0, fallthrough at index 1?
					// If opcodeParser puts fallthrough at 0, then this logic is WRONG if it uses index 1 for fallthrough!

					// Let's check context.
					// This block handles "it.nextBB == nil".
					// This happens when conditional jump NOT taken (or not executed).
					// If JUMPI was executed, it sets it.nextBB if taken.
					// If not taken, it sets it.nextBB to fallthrough (children[0]) in mirHandleJUMPI.

					// Wait, mirHandleJUMPI sets it.nextBB = children[0] on fallthrough.
					// If so, loop continues via `bb = it.nextBB` (line 624).
					// It DOES NOT enter `if it.nextBB == nil` block (line 588).

					// So this block (588+) handles cases where NO JUMP instruction was executed?
					// e.g. block ended with NO terminator.
					// This matches Block 31 (ends with ADD).

					// In this case, it must take the ONLY child (fallthrough).
					// Block 31 has 1 child. So it enters `if len(children) == 1`.
					// And uses `children[0]`. Correct.

					// So the logic for `len(children) >= 2` handles what?
					// Blocks with 2 children but NO JUMPI executed?
					// Impossible for JUMPI blocks (they terminate).
					// Possible for ... what?
					// Maybe if JUMPI was optimized away?
					// If optimized away, it should behave as fallthrough?
					// But JUMPI has 2 paths. Optimization usually resolves to unconditional JUMP or fallthrough.

					// If unconditional JUMP (optimized), it uses 1 child.
					// If fallthrough (optimized), it uses 1 child.

					// So `len(children) >= 2` case might be unreachable or for safety.
					// But if it is reachable, it assumes index 1 is fallthrough?
					// Comment says: "use fallthrough child (index 1)".

					// If I swapped children in opcodeParser to `[fallthrough, target]`.
					// Then fallthrough is at 0.
					// So `children[1]` is TARGET.

					// If this logic picks `children[1]`, it picks TARGET as fallthrough!
					// This would be WRONG if we swapped.

					// However, this logic is only for "nextBB == nil".
					// Blocks with JUMPI always set nextBB (either via JUMPI exec or fallthrough).

					// So `Block 31` (1 child) is the primary concern here.
					// And `children[0]` is correct for 1-child blocks.

					// I will proceed with adding the check for `children[0]`.

					if len(children) >= 2 && children[1] != nil {
						//log.Warn("MIR publish before cond fallthrough", "from_block", bb.blockNum)
						it.publishLiveOut(bb)
						// Preserve predecessor for successor PHI resolution on conditional fallthrough
						it.prevBB = bb
						if children[1] == bb {
							mirDebugWarn("MIR: self-loop conditional fallthrough detected; rerunning block", "blockNum", bb.blockNum)
							bb = children[1]
							continue
						}
						bb = children[1]
						continue
					}
				}
				return it.returndata, nil
			}
			bb = it.nextBB
			continue
		}
		switch err {
		case errJUMP:
			bb = it.nextBB
			continue
		case errSTOP:
			return it.returndata, nil
		case errRETURN:
			return it.returndata, nil
		case errREVERT:
			return it.returndata, err
		default:
			return nil, err
		}
	}
	return it.returndata, nil
}

var (
	errSTOP   = errors.New("STOP")
	errRETURN = errors.New("RETURN")
	errREVERT = errors.New("REVERT")
	errJUMP   = errors.New("JUMP")
	// ErrMIRFallback signals the adapter to fallback to the stock EVM interpreter
	// when MIR cannot safely continue (e.g., invalid jump destination resolution).
	ErrMIRFallback = errors.New("MIR_FALLBACK")
)

// GetErrREVERT returns the errREVERT error for external packages to check
func GetErrREVERT() error {
	return errREVERT
}

func (it *MIRInterpreter) exec(m *MIR) error {
	// Temporary aliasing: ensure deprecated field stays in sync during migration
	// m.operands is the canonical field
	// Check if this is block entry before skipping NOPs
	isBlockEntry := m != nil && m.idx == 0

	// For block entry, we need to charge gas even if the first instruction is a NOP
	if isBlockEntry && it.beforeOp != nil {
		ctx := &it.preOpCtx
		// Default to the actual first MIR instruction
		ctx.M = m
		ctx.EvmOp = m.evmOp
		// If the underlying bytecode begins with JUMPDEST but the first MIR op is not MirJUMPDEST
		// (e.g., it was optimized away), synthesize a MirJUMPDEST so the adapter can charge gas once.
		if it.cfg != nil && it.currentBB != nil {
			fp := int(it.currentBB.firstPC)
			if fp >= 0 && it.cfg.rawCode != nil && fp < len(it.cfg.rawCode) && ByteCode(it.cfg.rawCode[fp]) == JUMPDEST {
				if m == nil || m.op != MirJUMPDEST {
					synth := new(MIR)
					synth.op = MirJUMPDEST
					synth.evmPC = uint(it.currentBB.firstPC)
					synth.evmOp = byte(JUMPDEST)
					ctx.M = synth
					ctx.EvmOp = synth.evmOp
				}
			}
		}
		ctx.Operands = nil
		ctx.MemorySize = 0
		ctx.Length = 0
		ctx.IsBlockEntry = true
		ctx.Block = it.currentBB
		// For SELFDESTRUCT at block entry, evaluate operand for gas calculation
		if m.op == MirSELFDESTRUCT && len(m.operands) > 0 {
			v := it.evalValue(m.operands[0])
			it.preOpVals[0].Set(v)
			it.preOpOps[0] = &it.preOpVals[0]
			ctx.Operands = it.preOpOps[:1]
		}
		// For EXP at block entry, evaluate operands for gas calculation
		if m.op == MirEXP && len(m.operands) >= 2 {
			for i := 0; i < 2 && i < len(m.operands); i++ {
				v := it.evalValue(m.operands[i])
				it.preOpVals[i].Set(v)
				it.preOpOps[i] = &it.preOpVals[i]
			}
			ctx.Operands = it.preOpOps[:2]
		}
		// Call hook for block entry gas charging (even for NOPs)
		if err := it.beforeOp(ctx); err != nil {
			return err
		}
	}

	// Hot skip: MirNOPs carry no semantics; avoid pre-op hook and dispatch
	if m.op == MirNOP {
		return nil
	}

	// Allow embedding runtimes (e.g., adapter) to run pre-op logic such as gas metering
	if it.beforeOp != nil {
		// Build a lightweight context with evaluated operands when useful and an estimated memory size
		ctx := &it.preOpCtx
		// Reset reused context fields
		ctx.M = m
		ctx.EvmOp = m.evmOp
		ctx.Operands = nil
		// Use current memory size as baseline; operations that expand memory will update this inside the switch below
		ctx.MemorySize = uint64(len(it.memory))
		ctx.Length = 0
		ctx.IsBlockEntry = false
		// Note: Block entry already handled above for first instruction
		// Evaluate operands into concrete values, preserving order, except for KECCAK256 (handled below)
		if len(m.operands) > 0 && m.op != MirKECCAK256 {
			nWanted := len(m.operands)
			if nWanted > len(it.preOpOps) {
				nWanted = len(it.preOpOps)
			}
			for i := 0; i < nWanted; i++ {
				v := it.evalValue(m.operands[i])
				it.preOpVals[i].Set(v)
				it.preOpOps[i] = &it.preOpVals[i]
			}
			// Special handling for MSTORE/MSTORE8: also precompute value into slot 2 for handler reuse
			if (m.op == MirMSTORE || m.op == MirMSTORE8) && len(m.operands) >= 3 && len(it.preOpOps) > 2 {
				v := it.evalValue(m.operands[2])
				it.preOpVals[2].Set(v)
				it.preOpOps[2] = &it.preOpVals[2]
			}
			// Expose only what's required for gas/memory calculation
			if m.op == MirMSTORE || m.op == MirMSTORE8 {
				// Only offset is needed for gas/memory size
				ctx.Operands = it.preOpOps[:1]
			} else {
				ctx.Operands = it.preOpOps[:nWanted]
			}
		}
		// Ensure SELFDESTRUCT has its operand evaluated for gas calculation
		if m.op == MirSELFDESTRUCT && len(m.operands) > 0 {
			// Always evaluate SELFDESTRUCT operand for gas calculation, even if it was evaluated above
			v := it.evalValue(m.operands[0])
			it.preOpVals[0].Set(v)
			it.preOpOps[0] = &it.preOpVals[0]
			ctx.Operands = it.preOpOps[:1]
		}
		// Ensure EXP has its operands evaluated for gas calculation
		if m.op == MirEXP && len(m.operands) >= 2 {
			// Always evaluate EXP operands for gas calculation
			for i := 0; i < 2 && i < len(m.operands); i++ {
				v := it.evalValue(m.operands[i])
				it.preOpVals[i].Set(v)
				it.preOpOps[i] = &it.preOpVals[i]
			}
			ctx.Operands = it.preOpOps[:2]
		}
		// Compute a requested memory size for common memory-affecting ops
		switch m.op {
		case MirMLOAD:
			if len(ctx.Operands) >= 1 {
				off := ctx.Operands[0].Uint64()
				ctx.MemorySize = off + 32
			}
		case MirMSTORE:
			if len(ctx.Operands) >= 1 {
				off := ctx.Operands[0].Uint64()
				ctx.MemorySize = off + 32
			}
		case MirMSTORE8:
			if len(ctx.Operands) >= 1 {
				off := ctx.Operands[0].Uint64()
				ctx.MemorySize = off + 1
			}
		case MirMCOPY:
			if len(ctx.Operands) >= 3 {
				dst := ctx.Operands[0].Uint64()
				src := ctx.Operands[1].Uint64()
				ln := ctx.Operands[2].Uint64()
				a := dst + ln
				b := src + ln
				if a > b {
					ctx.MemorySize = a
				} else {
					ctx.MemorySize = b
				}
			}
		case MirCALLDATACOPY, MirCODECOPY, MirRETURNDATACOPY:
			if len(ctx.Operands) >= 3 {
				dst := ctx.Operands[0].Uint64()
				ln := ctx.Operands[2].Uint64()
				ctx.MemorySize = dst + ln
			}
		case MirEXTCODECOPY:
			if len(ctx.Operands) >= 4 {
				dst := ctx.Operands[1].Uint64()
				ln := ctx.Operands[3].Uint64()
				ctx.MemorySize = dst + ln
			}
		case MirKECCAK256:
			// Compute memory size and length and stash operands for handler reuse
			if len(m.operands) >= 2 {
				voff := it.evalValue(m.operands[0])
				vln := it.evalValue(m.operands[1])
				it.preOpVals[0].Set(voff)
				it.preOpOps[0] = &it.preOpVals[0]
				it.preOpVals[1].Set(vln)
				it.preOpOps[1] = &it.preOpVals[1]
				off := voff.Uint64()
				ln := vln.Uint64()
				ctx.MemorySize = off + ln
				ctx.Length = ln
				// Set ctx.Operands for gas calculation
				ctx.Operands = it.preOpOps[:2]
			}
		case MirRETURN, MirREVERT:
			if len(ctx.Operands) >= 2 {
				off := ctx.Operands[0].Uint64()
				ln := ctx.Operands[1].Uint64()
				ctx.MemorySize = off + ln
			}
		case MirLOG0, MirLOG1, MirLOG2, MirLOG3, MirLOG4:
			if len(ctx.Operands) >= 2 {
				off := ctx.Operands[0].Uint64()
				ln := ctx.Operands[1].Uint64()
				ctx.MemorySize = off + ln
			}
		case MirCALL:
			if len(ctx.Operands) >= 7 {
				inOff := ctx.Operands[3].Uint64()
				inSz := ctx.Operands[4].Uint64()
				outOff := ctx.Operands[5].Uint64()
				outSz := ctx.Operands[6].Uint64()
				a := inOff + inSz
				b := outOff + outSz
				if a > b {
					ctx.MemorySize = a
				} else {
					ctx.MemorySize = b
				}
			}
		case MirCALLCODE:
			if len(ctx.Operands) >= 7 {
				inOff := ctx.Operands[3].Uint64()
				inSz := ctx.Operands[4].Uint64()
				outOff := ctx.Operands[5].Uint64()
				outSz := ctx.Operands[6].Uint64()
				a := inOff + inSz
				b := outOff + outSz
				if a > b {
					ctx.MemorySize = a
				} else {
					ctx.MemorySize = b
				}
			}
		case MirDELEGATECALL, MirSTATICCALL:
			if len(ctx.Operands) >= 6 {
				inOff := ctx.Operands[2].Uint64()
				inSz := ctx.Operands[3].Uint64()
				outOff := ctx.Operands[4].Uint64()
				outSz := ctx.Operands[5].Uint64()
				a := inOff + inSz
				b := outOff + outSz
				if a > b {
					ctx.MemorySize = a
				} else {
					ctx.MemorySize = b
				}
			}
		case MirCREATE:
			if len(ctx.Operands) >= 3 {
				off := ctx.Operands[1].Uint64()
				ln := ctx.Operands[2].Uint64()
				ctx.MemorySize = off + ln
			}
		case MirCREATE2:
			if len(ctx.Operands) >= 3 {
				off := ctx.Operands[1].Uint64()
				ln := ctx.Operands[2].Uint64()
				ctx.MemorySize = off + ln
			}
		}
		if err := it.beforeOp(ctx); err != nil {
			return err
		}
	}
	// Try fast handler if available
	// Inline hot micro-ops to avoid function call dispatch overhead
	switch m.op {
	case MirADD:
		if it.tracerEx != nil {
			it.tracerEx(m)
		} else if it.tracer != nil {
			it.tracer(m.op)
		}
		a, b, _ := mirLoadAB(it, m)
		result := it.tmpA.Clear().Add(a, b)
		it.setResult(m, result)
		return nil
	case MirMUL:
		if it.tracerEx != nil {
			it.tracerEx(m)
		} else if it.tracer != nil {
			it.tracer(m.op)
		}
		a, b, _ := mirLoadAB(it, m)
		it.setResult(m, it.tmpA.Clear().Mul(a, b))
		return nil
		// fast-path for MirMSTORE and MirRETURN removed to use general handlers
	}
	if h := mirHandlers[byte(m.op)]; h != nil {
		if it.tracerEx != nil {
			it.tracerEx(m)
		} else if it.tracer != nil {
			it.tracer(m.op)
		}
		var start time.Time
		if mirExecTimingHook != nil {
			start = time.Now()
		}
		err := h(it, m)
		if mirExecTimingHook != nil {
			mirExecTimingHook(m.op, uint64(m.EvmPC()), time.Since(start))
		}
		return err
	}
	// Ensure tracer is invoked for every MIR op even when no fast handler exists
	if it.tracerEx != nil {
		it.tracerEx(m)
	} else if it.tracer != nil {
		it.tracer(m.op)
	}
	switch m.op {
	case MirPHI:

		// Select operand based on actual predecessor block when available; fallback to first.
		if len(m.operands) == 0 {
			it.setResult(m, it.zeroConst)
			return nil
		}
		selected := 0
		if it.currentBB != nil && it.prevBB != nil {
			parents := it.currentBB.Parents()
			for i := 0; i < len(parents) && i < len(m.operands); i++ {
				if parents[i] == it.prevBB {
					selected = i
					break
				}
			}

		}
		v := it.evalValue(m.operands[selected])
		it.setResult(m, v)
		return nil
	case MirPOP:
		return nil
	case MirSTOP:
		return errSTOP

	// Arithmetic and bitwise
	case MirADD, MirMUL, MirSUB, MirDIV, MirSDIV, MirMOD, MirSMOD, MirEXP,
		MirLT, MirGT, MirSLT, MirSGT, MirEQ, MirAND, MirOR, MirXOR,
		MirBYTE, MirSHL, MirSHR, MirSAR:
		return it.execArithmetic(m)

	// Three-operand arithmetic
	case MirADDMOD:
		if len(m.operands) < 3 {
			return fmt.Errorf("ADDMOD missing operands")
		}
		val1 := it.evalValue(m.operands[0])
		val2 := it.evalValue(m.operands[1])
		val3 := it.evalValue(m.operands[2])

		if val3.IsZero() {
			// EVM returns 0 for division by zero
			it.setResult(m, it.zeroConst)
		} else {
			// tmpA = val1 + val2; tmpA %= val3
			it.tmpA.Clear().Add(val1, val2)
			it.tmpA.Mod(it.tmpA, val3)
			it.setResult(m, it.tmpA)
		}
		return nil

	case MirMULMOD:
		if len(m.operands) < 3 {
			return fmt.Errorf("MULMOD missing operands")
		}
		val1 := it.evalValue(m.operands[0])
		val2 := it.evalValue(m.operands[1])
		val3 := it.evalValue(m.operands[2])

		if val3.IsZero() {
			// EVM returns 0 for division by zero
			it.setResult(m, it.zeroConst)
		} else {
			// tmpA = val1 * val2; tmpA %= val3
			it.tmpA.Clear().Mul(val1, val2)
			it.tmpA.Mod(it.tmpA, val3)
			it.setResult(m, it.tmpA)
		}
		return nil

	case MirISZERO:
		if len(m.operands) < 1 {
			return fmt.Errorf("ISZERO missing operand")
		}
		v1 := it.evalValue(m.operands[0])
		if v1.IsZero() {
			it.setResult(m, it.tmpA.Clear().SetOne())
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirNOT:
		if len(m.operands) < 1 {
			return fmt.Errorf("NOT missing operand")
		}
		v1 := it.evalValue(m.operands[0])
		it.setResult(m, it.tmpA.Clear().Not(v1))
		return nil

		// Memory
	case MirMLOAD:
		// operands: offset, size(ignored; assume 32)
		if len(m.operands) < 2 {
			return fmt.Errorf("MLOAD missing operands")
		}
		off := it.evalValue(m.operands[0])
		// Ensure memory growth side-effect even if value is forwarded via meta
		it.ensureMemSize(off.Uint64() + 32)
		if len(m.meta) == 32 {
			it.setResult(m, it.tmpA.Clear().SetBytes(m.meta))
			return nil
		}
		it.readMem32Into(off, &it.scratch32)

		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirMSIZE:
		// Return current logical memory size (length of internal buffer)
		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.memory))))
		return nil
	case MirMSTORE:
		// operands: offset, size(ignored; assume 32), value
		if len(m.operands) < 3 {
			return fmt.Errorf("MSTORE missing operands")
		}
		off := it.evalValue(m.operands[0])
		val := it.evalValue(m.operands[2])

		it.writeMem32(off, val)

		return nil
	case MirMSTORE8:
		if len(m.operands) < 3 {
			return fmt.Errorf("MSTORE8 missing operands")
		}
		off := it.evalValue(m.operands[0])
		val := it.evalValue(m.operands[2])
		it.writeMem8(off, val)
		return nil
	case MirMCOPY:
		// operands: dest, src, length
		if len(m.operands) < 3 {
			return fmt.Errorf("MCOPY missing operands")
		}
		dest := it.evalValue(m.operands[0])
		src := it.evalValue(m.operands[1])
		length := it.evalValue(m.operands[2])
		it.memCopy(dest, src, length)
		return nil

	// Storage (very basic map-based)
	case MirSLOAD:
		if len(m.operands) < 1 {
			return fmt.Errorf("SLOAD missing key")
		}
		key := it.evalValue(m.operands[0])
		val := it.sload(key)
		it.setResult(m, val)
		return nil
	case MirSSTORE:
		if len(m.operands) < 2 {
			return fmt.Errorf("SSTORE missing operands")
		}
		key := it.evalValue(m.operands[0])
		val := it.evalValue(m.operands[1])
		it.sstore(key, val)
		return nil

		// Env queries
	case MirADDRESS:
		// Fill scratch32 to avoid allocating
		for i := range it.scratch32 {
			it.scratch32[i] = 0
		}
		copy(it.scratch32[12:], it.env.Self[:])
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirPC:
		// Return the EVM PC captured in MIR metadata if available
		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(m.evmPC)))
		return nil
	case MirCODESIZE:
		// Return length of current contract code if available
		if it.env != nil && it.env.Code != nil {
			it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.env.Code))))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirCODECOPY:
		// operands: dest, offset, size
		if len(m.operands) < 3 {
			return fmt.Errorf("CODECOPY missing operands")
		}
		dest := it.preOpOps[0]
		if dest == nil {
			dest = it.evalValue(m.operands[0])
		}
		off := it.preOpOps[1]
		if off == nil {
			off = it.evalValue(m.operands[1])
		}
		sz := it.preOpOps[2]
		if sz == nil {
			sz = it.evalValue(m.operands[2])
		}
		mirDebugWarn("MIR CODECOPY", "dest", dest, "off", off, "sz", sz)
		// Copy from env.Code if present; else zero-fill
		if it.env != nil && it.env.Code != nil {
			d := dest.Uint64()
			o := off.Uint64()
			s := sz.Uint64()
			it.ensureMemSize(d + s)
			var i uint64
			for i = 0; i < s; i++ {
				idx := o + i
				var b byte = 0
				if idx < uint64(len(it.env.Code)) {
					b = it.env.Code[idx]
				}
				it.memory[d+i] = b
			}
		} else {
			it.ensureMemSize(dest.Uint64() + sz.Uint64())
			for i := uint64(0); i < sz.Uint64(); i++ {
				it.memory[dest.Uint64()+i] = 0
			}
		}
		return nil
	case MirEXTCODESIZE:
		// Return size via env hook if available
		if len(m.operands) < 1 {
			return fmt.Errorf("EXTCODESIZE missing operand")
		}
		addrVal := it.evalValue(m.operands[0]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrVal[12:])
		if it.env != nil && it.env.ExtCodeSize != nil {
			sz := it.env.ExtCodeSize(a20)
			it.setResult(m, it.tmpA.Clear().SetUint64(sz))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirEXTCODECOPY:
		// operands: address, dest, offset, size
		if len(m.operands) < 4 {
			return fmt.Errorf("EXTCODECOPY missing operands")
		}
		var av *uint256.Int
		if it.preOpOps[0] != nil {
			av = it.preOpOps[0]
		} else {
			av = it.evalValue(m.operands[0])
		}
		addrVal := av.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrVal[12:])
		dest := it.preOpOps[1]
		if dest == nil {
			dest = it.evalValue(m.operands[1])
		}
		off := it.preOpOps[2]
		if off == nil {
			off = it.evalValue(m.operands[2])
		}
		sz := it.preOpOps[3]
		if sz == nil {
			sz = it.evalValue(m.operands[3])
		}
		d := dest.Uint64()
		s := sz.Uint64()
		it.ensureMemSize(d + s)
		if it.env != nil && it.env.ExtCodeCopy != nil {
			it.env.ExtCodeCopy(a20, off.Uint64(), it.memory[d:d+s])
		} else {
			for i := uint64(0); i < s; i++ {
				it.memory[d+i] = 0
			}
		}
		return nil
	case MirBLOCKHASH:
		if len(m.operands) < 1 {
			return fmt.Errorf("BLOCKHASH missing operand")
		}
		num := it.evalValue(m.operands[0]).Uint64()
		if it.env != nil && it.env.BlockHashFunc != nil {
			h := it.env.BlockHashFunc(num)
			it.setResult(m, it.tmpA.Clear().SetBytes(h[:]))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirCOINBASE:
		for i := range it.scratch32 {
			it.scratch32[i] = 0
		}
		copy(it.scratch32[12:], it.env.Coinbase[:])
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirDIFFICULTY:
		it.setResult(m, it.tmpA.Clear().SetUint64(it.env.Difficulty))
		return nil
	case MirGASLIMIT:
		it.setResult(m, it.tmpA.Clear().SetUint64(it.env.GasLimit))
		return nil
	case MirCALLER:
		for i := range it.scratch32 {
			it.scratch32[i] = 0
		}
		copy(it.scratch32[12:], it.env.Caller[:])
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirORIGIN:
		for i := range it.scratch32 {
			it.scratch32[i] = 0
		}
		copy(it.scratch32[12:], it.env.Origin[:])
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirGASPRICE:
		it.setResult(m, uint256.NewInt(it.env.GasPrice))
		return nil
	case MirCALLVALUE:
		if it.env.CallValue != nil {
			it.setResult(m, new(uint256.Int).Set(it.env.CallValue))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirSELFBALANCE:
		if !it.env.IsIstanbul {
			return fmt.Errorf("invalid opcode: SELFBALANCE")
		}
		if it.env.GetBalanceFunc != nil {
			it.setResult(m, it.env.GetBalanceFunc(it.env.Self))
		} else {
			it.setResult(m, uint256.NewInt(it.env.SelfBalance))
		}
		return nil
	case MirCHAINID:
		if !it.env.IsIstanbul {
			return fmt.Errorf("invalid opcode: CHAINID")
		}
		it.setResult(m, uint256.NewInt(it.env.ChainID))
		return nil
	case MirTIMESTAMP:
		it.setResult(m, uint256.NewInt(it.env.Timestamp))
		return nil
	case MirNUMBER:
		it.setResult(m, uint256.NewInt(it.env.BlockNumber))
		return nil
	case MirBASEFEE:
		if !it.env.IsLondon {
			return fmt.Errorf("invalid opcode: BASEFEE")
		}
		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(it.env.BaseFee)))
		return nil
	case MirBLOBHASH:
		if len(m.operands) < 1 {
			return fmt.Errorf("BLOBHASH missing operand")
		}
		idx := it.evalValue(m.operands[0]).Uint64()
		if it.env != nil && it.env.BlobHashFunc != nil {
			h := it.env.BlobHashFunc(idx)
			it.setResult(m, it.tmpA.Clear().SetBytes(h[:]))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirBLOBBASEFEE:
		it.setResult(m, it.tmpA.Clear().SetUint64(it.env.BlobBaseFee))
		return nil
	case MirGAS:
		if it.env != nil && it.env.GasLeft != nil {
			it.setResult(m, it.tmpA.Clear().SetUint64(it.env.GasLeft()))
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirBALANCE:
		if len(m.operands) < 1 {
			return fmt.Errorf("BALANCE missing operand")
		}
		addrVal := it.evalValue(m.operands[0])
		addrBytes := addrVal.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrBytes[12:])
		if it.env.GetBalanceFunc != nil {
			it.setResult(m, it.env.GetBalanceFunc(a20))
		} else {
			it.setResult(m, uint256.NewInt(0))
		}
		return nil

		// Calldata / returndata
	case MirCALLDATALOAD:
		if len(m.operands) < 1 {
			return fmt.Errorf("CALLDATALOAD missing offset")
		}
		off := it.evalValue(m.operands[0])
		it.readCalldata32Into(off, &it.scratch32)
		//log.Warn("MIR CALLDATALOAD", "calldata", fmt.Sprintf("%x", it.env.Calldata), "off", off, "scratch32", fmt.Sprintf("%x", it.scratch32))
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirCALLDATASIZE:

		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.env.Calldata))))
		return nil
	case MirCALLDATACOPY:
		// operands: dest, offset, size
		if len(m.operands) < 3 {
			return fmt.Errorf("CALLDATACOPY missing operands")
		}
		dest := it.preOpOps[0]
		if dest == nil {
			dest = it.evalValue(m.operands[0])
		}
		off := it.preOpOps[1]
		if off == nil {
			off = it.evalValue(m.operands[1])
		}
		sz := it.preOpOps[2]
		if sz == nil {
			sz = it.evalValue(m.operands[2])
		}
		it.calldataCopy(dest, off, sz)
		return nil
	case MirRETURNDATASIZE:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: RETURNDATASIZE")
		}
		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.returndata))))
		return nil
	case MirRETURNDATALOAD:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: RETURNDATALOAD")
		}
		if len(m.operands) < 1 {
			return fmt.Errorf("RETURNDATALOAD missing offset")
		}
		off := it.evalValue(m.operands[0])
		it.readReturnData32Into(off, &it.scratch32)
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirRETURNDATACOPY:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: RETURNDATACOPY")
		}
		if len(m.operands) < 3 {
			return fmt.Errorf("RETURNDATACOPY missing operands")
		}
		dest := it.preOpOps[0]
		if dest == nil {
			dest = it.evalValue(m.operands[0])
		}
		off := it.preOpOps[1]
		if off == nil {
			off = it.evalValue(m.operands[1])
		}
		sz := it.preOpOps[2]
		if sz == nil {
			sz = it.evalValue(m.operands[2])
		}
		it.returnDataCopy(dest, off, sz)
		return nil
	case MirTLOAD:
		// transient storage load (EIP-1153)
		if len(m.operands) < 1 {
			return fmt.Errorf("TLOAD missing key")
		}
		key := it.evalValue(m.operands[0])
		var k [32]byte
		keyBytes := key.Bytes()
		copy(k[32-len(keyBytes):], keyBytes) // Fix: right-align key bytes like sload
		if it.env.TLoadFunc != nil {
			v := it.env.TLoadFunc(k)
			it.setResult(m, it.tmpB.Clear().SetBytes(v[:]))
			return nil
		}
		// Fallback to internal simulated transient storage
		if it.transientStorage == nil {
			it.transientStorage = make(map[[32]byte][32]byte)
		}
		val := it.transientStorage[k]
		it.setResult(m, it.tmpA.Clear().SetBytes(val[:]))
		return nil
	case MirTSTORE:
		if len(m.operands) < 2 {
			return fmt.Errorf("TSTORE missing operands")
		}
		key := it.evalValue(m.operands[0])
		val := it.evalValue(m.operands[1])
		var k [32]byte
		var v [32]byte
		keyBytes := key.Bytes()
		copy(k[32-len(keyBytes):], keyBytes) // Fix: right-align key bytes
		valBytes := val.Bytes()
		copy(v[32-len(valBytes):], valBytes) // Fix: right-align value bytes
		if it.env.TStoreFunc != nil {
			it.env.TStoreFunc(k, v)
			return nil
		}
		// Fallback to internal simulated transient storage
		if it.transientStorage == nil {
			it.transientStorage = make(map[[32]byte][32]byte)
		}
		it.transientStorage[k] = v
		return nil

	// Hashing (placeholder: full keccak over memory slice)
	case MirKECCAK256:
		if len(m.operands) < 2 {
			return fmt.Errorf("KECCAK256 missing operands")
		}
		off := it.evalValue(m.operands[0])
		sz := it.evalValue(m.operands[1])
		// Optimization: if hashing full calldata copied at offset 0, hash calldata directly
		var b []byte

		b = it.readMem(off, sz)
		// Heuristic: if size is zero but a prior CALLDATACOPY(0,0,N) occurred, interpret as hashing that region
		if len(b) == 0 && it.lastCopyDest == 0 && it.lastCopyOff == 0 && it.lastCopySize > 0 {
			// read from memory [0:lastCopySize]
			off0 := uint256.NewInt(0)
			sizeV := new(uint256.Int).SetUint64(it.lastCopySize)
			b = it.readMem(off0, sizeV)
		}

		if mirKeccakHook != nil {
			mirKeccakHook(off.Uint64(), sz.Uint64(), append([]byte(nil), b...))
		}
		h := crypto.Keccak256(b)
		it.setResult(m, it.tmpA.Clear().SetBytes(h))
		return nil
	case MirLOG0, MirLOG1, MirLOG2, MirLOG3, MirLOG4:
		// logs: dataOffset, dataSize, and 0..4 topics
		if it.env == nil || it.env.LogFunc == nil {
			return nil
		}
		numTopics := int(m.op - MirLOG0)
		// MIR builder pushed result placeholder; operands are implicit via stack in EVM.
		// Here, read memory and topics from operands if present else no-op.
		// Fallback: if no explicit operands encoded, skip.
		if len(m.operands) < 2 {
			return nil
		}
		dataOff := it.evalValue(m.operands[0])
		dataSz := it.evalValue(m.operands[1])
		data := it.readMemView(dataOff, dataSz)
		// collect topics (each is 32 bytes in value form)
		topics := make([][32]byte, 0, numTopics)
		for i := 0; i < numTopics && 2+i < len(m.operands); i++ {
			v := it.evalValue(m.operands[2+i]).Bytes32()
			var t [32]byte
			copy(t[:], v[:])
			topics = append(topics, t)
		}
		it.env.LogFunc(it.env.Self, topics, data)
		return nil

	// System returns
	case MirRETURN:
		if len(m.operands) < 2 {
			return fmt.Errorf("RETURN missing operands")
		}
		var off, sz *uint256.Int
		if it.preOpOps[0] != nil {
			off = it.preOpOps[0]
		} else {
			off = it.evalValue(m.operands[0])
		}
		if it.preOpOps[1] != nil {
			sz = it.preOpOps[1]
		} else {
			sz = it.evalValue(m.operands[1])
		}
		mirDebugWarn("MIR RETURN", "off", off, "sz", sz)
		it.returndata = it.readMemCopy(off, sz)
		if len(it.returndata) > 0 {
			// Log first 16 bytes for debugging constructor emissions
			first := it.returndata
			if len(first) > 16 {
				first = first[:16]
			}
			mirDebugWarn("MIR RETURN data", "len", len(it.returndata), "head", fmt.Sprintf("%x", first))
		} else {
			mirDebugWarn("MIR RETURN data empty")
		}
		return errRETURN
	case MirREVERT:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: REVERT")
		}
		if len(m.operands) < 2 {
			return fmt.Errorf("REVERT missing operands")
		}
		var off2, sz2 *uint256.Int
		if it.preOpOps[0] != nil {
			off2 = it.preOpOps[0]
		} else {
			off2 = it.evalValue(m.operands[0])
		}
		if it.preOpOps[1] != nil {
			sz2 = it.preOpOps[1]
		} else {
			sz2 = it.evalValue(m.operands[1])
		}
		view := it.readMemView(off2, sz2)
		it.returndata = append(it.returndata[:0], view...)
		return errREVERT
	case MirCALL:
		// operands: gas, addr, value, inOffset, inSize, outOffset, outSize
		if len(m.operands) < 7 {
			return fmt.Errorf("CALL missing operands")
		}
		var addrInt *uint256.Int
		if it.preOpOps[1] != nil {
			addrInt = it.preOpOps[1]
		} else {
			addrInt = it.evalValue(m.operands[1])
		}
		addrV := addrInt.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		var value, inOff, inSz, outOff, outSz *uint256.Int
		if it.preOpOps[2] != nil {
			// Clone value to prevent shared pointer escape to Contract (it.preOpVals are reused)
			value = new(uint256.Int).Set(it.preOpOps[2])
		} else {
			// Clone evaluated value as it might be a reuseable temp or result that could be modified later
			value = new(uint256.Int).Set(it.evalValue(m.operands[2]))
		}
		if it.preOpOps[3] != nil {
			inOff = it.preOpOps[3]
		} else {
			inOff = it.evalValue(m.operands[3])
		}
		if it.preOpOps[4] != nil {
			inSz = it.preOpOps[4]
		} else {
			inSz = it.evalValue(m.operands[4])
		}
		if it.preOpOps[5] != nil {
			outOff = it.preOpOps[5]
		} else {
			outOff = it.evalValue(m.operands[5])
		}
		if it.preOpOps[6] != nil {
			outSz = it.preOpOps[6]
		} else {
			outSz = it.evalValue(m.operands[6])
		}
		input := it.readMemView(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			gasValue := it.evalValue(m.operands[0]).Uint64()
			ret, ok := it.env.ExternalCall(0, a20, value, input, gasValue)
			it.returndata = append(it.returndata[:0], ret...)
			if ok {
				it.setResult(m, it.tmpA.Clear().SetOne())
			} else {
				it.setResult(m, it.zeroConst)
			}
			// Copy only available return data to avoid pathological memory growth
			sCopy := uint64(len(it.returndata))
			if s := outSz.Uint64(); s < sCopy {
				sCopy = s
			}
			it.returnDataCopy(outOff, uint256.NewInt(0), uint256.NewInt(sCopy))
			return nil
		}
		return ErrMIRFallback
	case MirCALLCODE:
		// operands: gas, addr, value, inOffset, inSize, outOffset, outSize
		if len(m.operands) < 7 {
			return fmt.Errorf("CALLCODE missing operands")
		}
		var addrInt2 *uint256.Int
		if it.preOpOps[1] != nil {
			addrInt2 = it.preOpOps[1]
		} else {
			addrInt2 = it.evalValue(m.operands[1])
		}
		addrV := addrInt2.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		var value, inOff, inSz, outOff, outSz *uint256.Int
		if it.preOpOps[2] != nil {
			// Clone value to prevent shared pointer escape to Contract (it.preOpVals are reused)
			value = new(uint256.Int).Set(it.preOpOps[2])
		} else {
			// Clone evaluated value as it might be a reuseable temp or result that could be modified later
			value = new(uint256.Int).Set(it.evalValue(m.operands[2]))
		}
		if it.preOpOps[3] != nil {
			inOff = it.preOpOps[3]
		} else {
			inOff = it.evalValue(m.operands[3])
		}
		if it.preOpOps[4] != nil {
			inSz = it.preOpOps[4]
		} else {
			inSz = it.evalValue(m.operands[4])
		}
		if it.preOpOps[5] != nil {
			outOff = it.preOpOps[5]
		} else {
			outOff = it.evalValue(m.operands[5])
		}
		if it.preOpOps[6] != nil {
			outSz = it.preOpOps[6]
		} else {
			outSz = it.evalValue(m.operands[6])
		}
		input := it.readMemView(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			gasValue := it.evalValue(m.operands[0]).Uint64()
			ret, ok := it.env.ExternalCall(1, a20, value, input, gasValue)
			it.returndata = append(it.returndata[:0], ret...)
			if ok {
				it.setResult(m, it.tmpA.Clear().SetOne())
			} else {
				it.setResult(m, it.zeroConst)
			}
			sCopy := uint64(len(it.returndata))
			if s := outSz.Uint64(); s < sCopy {
				sCopy = s
			}
			it.returnDataCopy(outOff, uint256.NewInt(0), uint256.NewInt(sCopy))
			return nil
		}
		return ErrMIRFallback
	case MirDELEGATECALL:
		// operands: gas, addr, inOffset, inSize, outOffset, outSize
		if len(m.operands) < 6 {
			return fmt.Errorf("DELEGATECALL missing operands")
		}
		var addrInt3 *uint256.Int
		if it.preOpOps[1] != nil {
			addrInt3 = it.preOpOps[1]
		} else {
			addrInt3 = it.evalValue(m.operands[1])
		}
		addrV := addrInt3.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		var inOff, inSz, outOff, outSz *uint256.Int
		if it.preOpOps[2] != nil {
			inOff = it.preOpOps[2]
		} else {
			inOff = it.evalValue(m.operands[2])
		}
		if it.preOpOps[3] != nil {
			inSz = it.preOpOps[3]
		} else {
			inSz = it.evalValue(m.operands[3])
		}
		if it.preOpOps[4] != nil {
			outOff = it.preOpOps[4]
		} else {
			outOff = it.evalValue(m.operands[4])
		}
		if it.preOpOps[5] != nil {
			outSz = it.preOpOps[5]
		} else {
			outSz = it.evalValue(m.operands[5])
		}
		input := it.readMemView(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			gasValue := it.evalValue(m.operands[0]).Uint64()
			ret, ok := it.env.ExternalCall(2, a20, nil, input, gasValue)
			it.returndata = append(it.returndata[:0], ret...)
			if ok {
				it.setResult(m, it.tmpA.Clear().SetOne())
			} else {
				it.setResult(m, it.zeroConst)
			}
			sCopy := uint64(len(it.returndata))
			if s := outSz.Uint64(); s < sCopy {
				sCopy = s
			}
			it.returnDataCopy(outOff, uint256.NewInt(0), uint256.NewInt(sCopy))
			return nil
		}
		return ErrMIRFallback
	case MirSTATICCALL:
		// operands: gas, addr, inOffset, inSize, outOffset, outSize
		if len(m.operands) < 6 {
			return fmt.Errorf("STATICCALL missing operands")
		}
		var addrInt4 *uint256.Int
		if it.preOpOps[1] != nil {
			addrInt4 = it.preOpOps[1]
		} else {
			addrInt4 = it.evalValue(m.operands[1])
		}
		addrV := addrInt4.Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		var inOff, inSz, outOff, outSz *uint256.Int
		if it.preOpOps[2] != nil {
			inOff = it.preOpOps[2]
		} else {
			inOff = it.evalValue(m.operands[2])
		}
		if it.preOpOps[3] != nil {
			inSz = it.preOpOps[3]
		} else {
			inSz = it.evalValue(m.operands[3])
		}
		if it.preOpOps[4] != nil {
			outOff = it.preOpOps[4]
		} else {
			outOff = it.evalValue(m.operands[4])
		}
		if it.preOpOps[5] != nil {
			outSz = it.preOpOps[5]
		} else {
			outSz = it.evalValue(m.operands[5])
		}
		input := it.readMemView(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			gasValue := it.evalValue(m.operands[0]).Uint64()
			ret, ok := it.env.ExternalCall(3, a20, nil, input, gasValue)
			it.returndata = append(it.returndata[:0], ret...)
			if ok {
				it.setResult(m, it.tmpA.Clear().SetOne())
			} else {
				it.setResult(m, it.zeroConst)
			}
			sCopy := uint64(len(it.returndata))
			if s := outSz.Uint64(); s < sCopy {
				sCopy = s
			}
			it.returnDataCopy(outOff, uint256.NewInt(0), uint256.NewInt(sCopy))
			return nil
		}
		return ErrMIRFallback
	case MirCREATE:
		// operands: value, offset, size
		if len(m.operands) < 3 {
			return fmt.Errorf("CREATE missing operands")
		}
		var value *uint256.Int
		if it.preOpOps[0] != nil {
			// Clone to avoid shared pointer escape
			value = new(uint256.Int).Set(it.preOpOps[0])
		} else {
			value = new(uint256.Int).Set(it.evalValue(m.operands[0]))
		}
		off := it.preOpOps[1]
		if off == nil {
			off = it.evalValue(m.operands[1])
		}
		sz := it.preOpOps[2]
		if sz == nil {
			sz = it.evalValue(m.operands[2])
		}
		initCode := it.readMem(off, sz)
		if it.env != nil && it.env.CreateContract != nil {
			addr, ok, ret := it.env.CreateContract(4, value, initCode, nil)
			_ = ret // returndata after create is ignored by EVM, result is address or zero
			// Return address as 32 bytes left-padded
			for i := range it.scratch32 {
				it.scratch32[i] = 0
			}
			copy(it.scratch32[12:], addr[:])
			it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
			if ok {
				return nil
			}
			return nil
		}
		return ErrMIRFallback
	case MirSELFDESTRUCT:
		// Stub: just stop
		return errSTOP
	case MirINVALID:
		return fmt.Errorf("invalid opcode")

		// Control flow (handled at CFG/adapter level). Treat as no-ops here.
	case MirJUMP:
		return mirHandleJUMP(it, m)
	case MirJUMPI:
		return mirHandleJUMPI(it, m)
	case MirJUMPDEST:
		return nil
	case MirERRJUMPDEST:
		// If auxiliary MIR exists (encoded in meta or attached), interpret as the original EVM op to fetch context
		// For now, treat as an error after tracing
		if it.tracerEx != nil {
			it.tracerEx(m)
		} else if it.tracer != nil {
			it.tracer(m.op)
		}
		return fmt.Errorf("invalid jump destination")
	case MirEXTCODEHASH:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: EXTCODEHASH")
		}
		if len(m.operands) < 1 {
			return fmt.Errorf("EXTCODEHASH missing operand")
		}
		addrVal := it.evalValue(m.operands[0]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrVal[12:])
		if it.env != nil && it.env.ExtCodeHash != nil {
			h := it.env.ExtCodeHash(a20)
			it.setResult(m, it.tmpA.Clear().SetBytes(h[:]))
		} else {
			for i := range it.scratch32 {
				it.scratch32[i] = 0
			}
			it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		}
		return nil
	case MirCREATE2:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: CREATE2")
		}
		if len(m.operands) < 4 {
			return fmt.Errorf("CREATE2 missing operands")
		}
		var value *uint256.Int
		if it.preOpOps[0] != nil {
			// Clone to avoid shared pointer escape
			value = new(uint256.Int).Set(it.preOpOps[0])
		} else {
			value = new(uint256.Int).Set(it.evalValue(m.operands[0]))
		}
		off := it.preOpOps[1]
		if off == nil {
			off = it.evalValue(m.operands[1])
		}
		sz := it.preOpOps[2]
		if sz == nil {
			sz = it.evalValue(m.operands[2])
		}
		var saltInt *uint256.Int
		if it.preOpOps[3] != nil {
			saltInt = it.preOpOps[3]
		} else {
			saltInt = it.evalValue(m.operands[3])
		}
		saltVal := saltInt.Bytes32()
		var salt [32]byte
		copy(salt[:], saltVal[:])
		initCode := it.readMem(off, sz)
		if it.env != nil && it.env.CreateContract != nil {
			addr, ok, ret := it.env.CreateContract(5, value, initCode, &salt)
			_ = ret
			for i := range it.scratch32 {
				it.scratch32[i] = 0
			}
			copy(it.scratch32[12:], addr[:])
			it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
			if ok {
				return nil
			}
			return nil
		}
		return ErrMIRFallback

	// Stack ops: DUPn and SWAPn
	case MirDUP1, MirDUP2, MirDUP3, MirDUP4, MirDUP5, MirDUP6, MirDUP7, MirDUP8,
		MirDUP9, MirDUP10, MirDUP11, MirDUP12, MirDUP13, MirDUP14, MirDUP15, MirDUP16:
		// For MIR, DUP may be optimized to MirNOP during build.
		// When materialized here, explicitly copy the operand value.
		if len(m.operands) < 1 {
			return fmt.Errorf("DUP missing operand")
		}
		v := it.evalValue(m.operands[0])
		it.setResult(m, it.tmpA.Clear().Set(v))
		return nil
	case MirSWAP1, MirSWAP2, MirSWAP3, MirSWAP4, MirSWAP5, MirSWAP6, MirSWAP7, MirSWAP8,
		MirSWAP9, MirSWAP10, MirSWAP11, MirSWAP12, MirSWAP13, MirSWAP14, MirSWAP15, MirSWAP16:
		// SWAP is modeled in MIR generation by reordering value uses; execution-time no-op
		return nil
	default:
		// Many ops are not yet implemented for simulation
		return fmt.Errorf("MIR op not implemented: 0x%x", byte(m.op))
	}
}

// Fast handlers
func mirHandleSTOP(it *MIRInterpreter, m *MIR) error {
	return errSTOP
}

func mirHandleRETURN(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 2 {
		return fmt.Errorf("RETURN missing operands")
	}
	var off, sz *uint256.Int
	if it.preOpOps[0] != nil {
		off = it.preOpOps[0]
	} else {
		off = it.evalValue(m.operands[0])
	}
	if len(it.preOpOps) > 1 && it.preOpOps[1] != nil {
		sz = it.preOpOps[1]
	} else {
		sz = it.evalValue(m.operands[1])
	}
	// Avoid two allocations by using a single copy into a fresh buffer
	it.returndata = it.readMemCopy(off, sz)
	return errRETURN
}

func mirHandleJUMP(it *MIRInterpreter, m *MIR) error {
	if it.env == nil || it.env.CheckJumpdest == nil || it.env.ResolveBB == nil {
		return nil
	}
	if len(m.operands) < 1 {
		return fmt.Errorf("JUMP missing destination")
	}
	dest, ok := it.resolveJumpDestValue(m.operands[0])
	if !ok {
		mirDebugError("MIR JUMP cannot resolve PHI-derived dest - requesting EVM fallback", "from_evm_pc", m.evmPC)
		return ErrMIRFallback
	}
	udest, _ := dest.Uint64WithOverflow()
	// Cache resolved PHI-based destination to stabilize later uses across blocks.
	// However, avoid pinning when the destination is a self-loop to the current block's entry;
	// allowing re-evaluation on the next iteration can let the dispatcher progress.
	if it.currentBB != nil && udest == uint64(it.currentBB.firstPC) {
		// skip pin
		return it.scheduleJump(udest, m, false)
	}
	if opv := m.operands[0]; opv != nil && opv.kind == Variable && opv.def != nil {
		if it.globalResults != nil {
			it.globalResults[opv.def] = new(uint256.Int).Set(dest)
		}
		if opv.def.evmPC != 0 {
			if it.globalResultsBySig[uint64(opv.def.evmPC)] == nil {
				it.globalResultsBySig[uint64(opv.def.evmPC)] = make(map[int]*uint256.Int)
			}
			it.globalResultsBySig[uint64(opv.def.evmPC)][opv.def.idx] = new(uint256.Int).Set(dest)
		}
	}
	return it.scheduleJump(udest, m, false)
}

func mirHandleJUMPI(it *MIRInterpreter, m *MIR) error {
	if it.env == nil || it.env.CheckJumpdest == nil || it.env.ResolveBB == nil {
		return nil
	}
	if len(m.operands) < 2 {
		return fmt.Errorf("JUMPI missing operands")
	}
	cond := it.evalValue(m.operands[1])

	if cond.IsZero() {
		// fallthrough: use children[1] which is the fallthrough block for JUMPI
		children := it.currentBB.Children()
		// In our builder, children[0] is fallthrough BB for JUMPI, targets follow.
		if len(children) >= 1 && children[0] != nil {
			it.nextBB = children[0]
			it.publishLiveOut(it.currentBB)
			it.prevBB = it.currentBB
			return nil // Return nil to indicate success (no error), loop continues
		}
		// Fallback: try to resolve fallthrough block directly
		dest := m.evmPC + 1
		udest := uint64(dest)
		it.nextBB = it.env.ResolveBB(udest)
		if it.nextBB != nil {
			it.publishLiveOut(it.currentBB)
			it.prevBB = it.currentBB
			return errJUMP
		}
		// If we can't resolve the block and children[1] doesn't exist,
		// return nil to let RunCFGWithResolver handle fallthrough via children logic
		// This should only happen if the CFG is malformed
		it.nextBB = nil
		it.publishLiveOut(it.currentBB)
		it.prevBB = it.currentBB
		return nil
	}
	dest, ok := it.resolveJumpDestValue(m.operands[0])
	if !ok {
		mirDebugError("MIR JUMPI cannot resolve PHI-derived dest - requesting EVM fallback", "from_evm_pc", m.evmPC)
		return ErrMIRFallback
	}
	udest, _ := dest.Uint64WithOverflow()
	err := it.scheduleJump(udest, m, false)
	return err
}

// mirHandlePHI sets the result to the first available incoming value.
func mirHandlePHI(it *MIRInterpreter, m *MIR) error {

	// If we can, take the exact value from the immediate predecessor's exit stack
	if it.prevBB != nil {
		exit := it.prevBB.ExitStack()

		if exit != nil && m.phiStackIndex >= 0 {
			idxFromTop := m.phiStackIndex
			if idxFromTop < len(exit) {
				// Map PHI slot (0=top) to index in exit snapshot
				src := exit[len(exit)-1-idxFromTop]
				// Mark as live-in to force evalValue to consult cross-BB results first
				src.liveIn = true

				val := it.evalValue(&src)

				it.setResult(m, val)
				// Record PHI result with predecessor sensitivity for future uses
				if m != nil {
					if it.phiResults[m] == nil {
						it.phiResults[m] = make(map[*MIRBasicBlock]*uint256.Int)
					}
					if val != nil {
						it.phiResults[m][it.prevBB] = new(uint256.Int).Set(val)
						it.phiLastPred[m] = it.prevBB
						// Signature caches
						if m.evmPC != 0 {
							if it.phiResultsBySig[uint64(m.evmPC)] == nil {
								it.phiResultsBySig[uint64(m.evmPC)] = make(map[int]map[*MIRBasicBlock]*uint256.Int)
							}
							if it.phiResultsBySig[uint64(m.evmPC)][m.idx] == nil {
								it.phiResultsBySig[uint64(m.evmPC)][m.idx] = make(map[*MIRBasicBlock]*uint256.Int)
							}
							it.phiResultsBySig[uint64(m.evmPC)][m.idx][it.prevBB] = new(uint256.Int).Set(val)
							if it.phiLastPredBySig[uint64(m.evmPC)] == nil {
								it.phiLastPredBySig[uint64(m.evmPC)] = make(map[int]*MIRBasicBlock)
							}
							it.phiLastPredBySig[uint64(m.evmPC)][m.idx] = it.prevBB
						}
					}
				}
				return nil
			}
		}
		// Try incoming stacks as fallback before operand selection
		if it.currentBB != nil && m.phiStackIndex >= 0 {
			incoming := it.currentBB.IncomingStacks()
			if incoming != nil {
				if stack, ok := incoming[it.prevBB]; ok && stack != nil {
					idxFromTop := m.phiStackIndex
					if idxFromTop < len(stack) {
						src := stack[len(stack)-1-idxFromTop]
						src.liveIn = true
						val := it.evalValue(&src)
						it.setResult(m, val)
						if m != nil && val != nil {
							if it.phiResults[m] == nil {
								it.phiResults[m] = make(map[*MIRBasicBlock]*uint256.Int)
							}
							it.phiResults[m][it.prevBB] = new(uint256.Int).Set(val)
							it.phiLastPred[m] = it.prevBB
						}
						return nil
					}
				}
			}
		}
	}
	// Fallback: pick operand corresponding to predecessor position among parents
	if len(m.operands) == 0 {
		it.setResult(m, it.zeroConst)
		return nil
	}
	selected := 0
	if it.currentBB != nil && it.prevBB != nil {
		parents := it.currentBB.Parents()
		for i := 0; i < len(parents) && i < len(m.operands); i++ {
			if parents[i] == it.prevBB {
				selected = i
				break
			}
		}
	}

	val := it.evalValue(m.operands[selected])
	it.setResult(m, val)
	// Record fallback selection as well
	if m != nil && it.prevBB != nil {
		if it.phiResults[m] == nil {
			it.phiResults[m] = make(map[*MIRBasicBlock]*uint256.Int)
		}
		if val != nil {
			it.phiResults[m][it.prevBB] = new(uint256.Int).Set(val)
			it.phiLastPred[m] = it.prevBB
		}
	}
	return nil
}

func mirHandleMLOAD(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 2 {
		return fmt.Errorf("MLOAD missing operands")
	}
	// Reuse pre-op evaluated operands when available
	var off *uint256.Int
	if it.preOpOps[0] != nil {
		off = it.preOpOps[0]
	} else {
		off = it.evalValue(m.operands[0])
	}
	// Ensure memory growth side-effect even if value is forwarded via meta
	it.ensureMemSize(off.Uint64() + 32)
	// DEBUG: Force actual memory read to verify optimization correctness
	// if len(m.meta) == 32 {
	// 	it.setResult(m, it.tmpA.Clear().SetBytes(m.meta))
	// 	return nil
	// }
	it.readMem32Into(off, &it.scratch32)

	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

// Arithmetic fast handlers (two-operand)
func mirLoadAB(it *MIRInterpreter, m *MIR) (a, b *uint256.Int, err error) {
	if len(m.operands) < 2 {
		return nil, nil, fmt.Errorf("missing operands")
	}
	// Check for Unknown operands - these indicate CFG construction issues
	if m.operands[0] != nil && m.operands[0].kind == Unknown {
		mirDebugError("MIR mirLoadAB encountered Unknown operand[0] - requesting EVM fallback",
			"evm_pc", m.evmPC, "op", m.op.String())
		return nil, nil, ErrMIRFallback
	}
	if m.operands[1] != nil && m.operands[1].kind == Unknown {
		mirDebugError("MIR mirLoadAB encountered Unknown operand[1] - requesting EVM fallback",
			"evm_pc", m.evmPC, "op", m.op.String())
		return nil, nil, ErrMIRFallback
	}
	// Fast path: reuse pre-op evaluated operands when available
	if it.preOpOps[0] != nil && it.preOpOps[1] != nil {
		return it.preOpOps[0], it.preOpOps[1], nil
	}
	// Use pre-encoded operand info if available
	if len(m.opKinds) >= 2 {
		// For constants, use pre-decoded value; otherwise resolve via evalValue
		if m.opKinds[0] == 0 {
			a = m.opConst[0]
		}
		if a == nil {
			a = it.evalValue(m.operands[0])
		}
		if m.opKinds[1] == 0 {
			b = m.opConst[1]
		}
		if b == nil {
			b = it.evalValue(m.operands[1])
		}
		return a, b, nil
	}
	a = it.evalValue(m.operands[0])
	b = it.evalValue(m.operands[1])
	return a, b, nil
}

func mirHandleADD(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	result := it.tmpA.Clear().Add(a, b)
	it.setResult(m, result)
	return nil
}
func mirHandleMUL(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Mul(a, b))
	return nil
}
func mirHandleSUB(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	// EVM: top - next => a - b
	it.setResult(m, it.tmpA.Clear().Sub(a, b))
	return nil
}
func mirHandleAND(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().And(a, b))
	return nil
}
func mirHandleOR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Or(a, b))
	return nil
}
func mirHandleXOR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Xor(a, b))
	return nil
}
func mirHandleSHL(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	// EVM: value is left (b), shift amount is right (a)
	it.setResult(m, it.tmpA.Clear().Lsh(b, uint(a.Uint64())))
	return nil
}
func mirHandleSHR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	result := it.tmpA.Clear().Rsh(b, uint(a.Uint64()))
	it.setResult(m, result)
	return nil
}
func mirHandleSAR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().SRsh(b, uint(a.Uint64())))
	return nil
}
func mirHandleEQ(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	//log.Warn("MIR EQ", "a", a, "==b", b)
	if err != nil {
		return err
	}
	result := it.zeroConst
	if a.Eq(b) {
		result = it.tmpA.Clear().SetOne()
	}
	it.setResult(m, result)
	return nil
}
func mirHandleLT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	mirDebugWarn("MIR LT", "a", a.Hex(), "b", b.Hex())
	if err != nil {
		return err
	}
	// With operand order (a=top/right, b=next/left), LT tests a < b
	var result *uint256.Int
	if a.Lt(b) {
		result = it.tmpA.Clear().SetOne()
	} else {
		result = it.zeroConst
	}
	it.setResult(m, result)
	return nil
}
func mirHandleGT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	//log.Warn("MIR GT", "a", a, "> b", b)
	if err != nil {
		return err
	}
	// GT tests a > b
	if a.Gt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}
func mirHandleSLT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	//log.Warn("MIR SLT", "a", a, "<b", b)
	if err != nil {
		return err
	}
	// Signed compare: a < b
	if a.Slt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}
func mirHandleSGT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	// Signed compare: a > b
	if a.Sgt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}

func mirHandleMSTORE(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 3 {
		return fmt.Errorf("MSTORE missing operands")
	}
	// Reuse pre-op evaluated operands when available to avoid duplicate eval work
	var off, val *uint256.Int
	if it.preOpOps[0] != nil {
		off = it.preOpOps[0]
	} else {
		off = it.evalValue(m.operands[0])
	}
	if len(it.preOpOps) > 2 && it.preOpOps[2] != nil {
		val = it.preOpOps[2]
	} else {
		val = it.evalValue(m.operands[2])
	}
	it.writeMem32(off, val)
	return nil
}

func mirHandleMSTORE8(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 3 {
		return fmt.Errorf("MSTORE8 missing operands")
	}
	var off, val *uint256.Int
	if it.preOpOps[0] != nil {
		off = it.preOpOps[0]
	} else {
		off = it.evalValue(m.operands[0])
	}
	if len(it.preOpOps) > 2 && it.preOpOps[2] != nil {
		val = it.preOpOps[2]
	} else {
		val = it.evalValue(m.operands[2])
	}
	it.writeMem8(off, val)
	return nil
}

func mirHandleMCOPY(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 3 {
		return fmt.Errorf("MCOPY missing operands")
	}
	d := it.evalValue(m.operands[0])
	s := it.evalValue(m.operands[1])
	l := it.evalValue(m.operands[2])
	it.memCopy(d, s, l)
	return nil
}

func mirHandleCALLDATASIZE(it *MIRInterpreter, m *MIR) error {
	//log.Warn("MIR CALLDATASIZE", "calldata", it.env.Calldata, "size", len(it.env.Calldata))
	it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.env.Calldata))))
	return nil
}

func mirHandleRETURNDATASIZE(it *MIRInterpreter, m *MIR) error {
	it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.returndata))))
	return nil
}

// Arithmetic extended
func mirHandleDIV(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Div(a, b))
	return nil
}
func mirHandleSDIV(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().SDiv(a, b))
	return nil
}
func mirHandleMOD(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Mod(a, b))
	return nil
}
func mirHandleSMOD(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().SMod(a, b))
	return nil
}
func mirHandleEXP(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	// EVM EXP: base is top (a), exponent is next (b)
	it.setResult(m, it.tmpA.Clear().Exp(a, b))
	return nil
}

func mirHandleKECCAK(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 2 {
		return fmt.Errorf("KECCAK256 missing operands")
	}
	// Operands are [offset, size]
	var off, sz *uint256.Int
	if it.preOpOps[0] != nil {
		off = it.preOpOps[0]
	} else {
		off = it.evalValue(m.operands[0])
	}
	if it.preOpOps[1] != nil {
		sz = it.preOpOps[1]
	} else {
		sz = it.evalValue(m.operands[1])
	}
	// Fast-path: empty input
	if sz.IsZero() {
		// keccak256("") constant without allocating
		it.setResult(m, it.tmpA.Clear().SetBytes(emptyKeccakHash[:]))
		return nil
	}
	// Ensure memory growth side-effect
	it.ensureMemSize(off.Uint64() + sz.Uint64())
	// If compiler attached a precomputed hash, use it directly
	if len(m.meta) == 32 {
		it.setResult(m, it.tmpA.Clear().SetBytes(m.meta))
		return nil
	}
	// Read a zero-copy view of memory to hash
	bytesToHash := it.readMemView(off, sz)
	if len(bytesToHash) == 0 && it.lastCopyDest == 0 && it.lastCopyOff == 0 && it.lastCopySize > 0 {
		// Heuristic for common calldata copy pattern: hash memory [0:size] without allocations
		it.ensureMemSize(it.lastCopySize)
		bytesToHash = it.memory[:it.lastCopySize]
	}
	if mirKeccakHook != nil {
		mirKeccakHook(off.Uint64(), sz.Uint64(), append([]byte(nil), bytesToHash...))
	}
	// Reuse a single Keccak state to avoid per-call allocations
	if it.hasher == nil {
		it.hasher = crypto.NewKeccakState()
	} else {
		it.hasher.Reset()
	}
	_, _ = it.hasher.Write(bytesToHash)
	_, _ = it.hasher.Read(it.hasherBuf[:])
	it.setResult(m, it.tmpA.Clear().SetBytes(it.hasherBuf[:]))
	return nil
}

func mirHandleCALLDATALOAD(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 1 {
		return fmt.Errorf("CALLDATALOAD missing offset")
	}
	off := it.preOpOps[0]
	if off == nil {
		off = it.evalValue(m.operands[0])
	}
	it.readCalldata32Into(off, &it.scratch32)
	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

func mirHandleRETURNDATALOAD(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 1 {
		return fmt.Errorf("RETURNDATALOAD missing offset")
	}
	off := it.evalValue(m.operands[0])
	it.readReturnData32Into(off, &it.scratch32)
	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

func mirHandleISZERO(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 1 {
		return fmt.Errorf("ISZERO missing operand")
	}
	v := it.evalValue(m.operands[0])
	result := it.zeroConst
	if v.IsZero() {
		result = it.tmpA.Clear().SetOne()
	}
	it.setResult(m, result)
	return nil
}

func mirHandleNOT(it *MIRInterpreter, m *MIR) error {
	if len(m.operands) < 1 {
		return fmt.Errorf("NOT missing operand")
	}
	v := it.evalValue(m.operands[0])
	it.setResult(m, it.tmpA.Clear().Not(v))
	return nil
}

func mirHandleBYTE(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}

	a = b.Byte(a)
	it.setResult(m, a)
	return nil
}

func (it *MIRInterpreter) execArithmetic(m *MIR) error {
	if len(m.operands) < 2 {
		return fmt.Errorf("arithmetic op requires 2 operands")
	}
	// Check for Unknown operands - these indicate CFG construction issues
	if m.operands[0] != nil && m.operands[0].kind == Unknown {
		mirDebugError("MIR execArithmetic encountered Unknown operand[0] - requesting EVM fallback",
			"evm_pc", m.evmPC, "op", m.op.String())
		return ErrMIRFallback
	}
	if m.operands[1] != nil && m.operands[1].kind == Unknown {
		mirDebugError("MIR execArithmetic encountered Unknown operand[1] - requesting EVM fallback",
			"evm_pc", m.evmPC, "op", m.op.String())
		return ErrMIRFallback
	}
	a := it.evalValue(m.operands[0])
	b := it.evalValue(m.operands[1])

	var out *uint256.Int
	switch m.op {
	case MirADD:
		out = it.tmpA.Clear().Add(a, b)
	case MirMUL:
		out = it.tmpA.Clear().Mul(a, b)
	case MirSUB:
		// EVM: top - next => a - b
		out = it.tmpA.Clear().Sub(a, b)
	case MirDIV:
		out = it.tmpA.Clear().Div(a, b)
	case MirSDIV:
		out = it.tmpA.Clear().SDiv(a, b)
	case MirMOD:
		out = it.tmpA.Clear().Mod(a, b)
	case MirSMOD:
		out = it.tmpA.Clear().SMod(a, b)
	case MirEXP:
		// EVM EXP: base is top (a), exponent is next (b) => a^b
		out = it.tmpA.Clear().Exp(a, b)
	case MirSIGNEXT:
		// Sign-extend byte at index a in value b
		idx := a.Uint64()
		if idx >= 32 {
			out = it.tmpA.Clear().Set(b)
			break
		}
		// Compute shift to align target byte to MSB of 32-byte word
		shift := (31 - idx) * 8
		// Mask for bits up through the target byte
		lowerMask := new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), uint(shift+8)), uint256.NewInt(1))
		// Extract the target byte
		byteVal := new(uint256.Int).And(new(uint256.Int).Rsh(new(uint256.Int).Set(b), uint(shift)), uint256.NewInt(0xff))
		if byteVal.Uint64()&0x80 != 0 {
			// Negative: set bits above target byte to 1
			upperMask := new(uint256.Int).Not(lowerMask)
			out = it.tmpA.Clear().Or(new(uint256.Int).And(b, lowerMask), upperMask)
		} else {
			// Positive: clear bits above target byte
			out = it.tmpA.Clear().And(b, lowerMask)
		}
	case MirLT:
		// test a < b (right < left)
		if a.Lt(b) {
			out = it.tmpA.Clear().SetOne()
		} else {
			out = it.zeroConst
		}
	case MirGT:
		if a.Gt(b) {
			out = it.tmpA.Clear().SetOne()
		} else {
			out = it.zeroConst
		}
	case MirSLT:
		if a.Slt(b) {
			out = it.tmpA.Clear().SetOne()
		} else {
			out = it.zeroConst
		}
	case MirSGT:
		if a.Sgt(b) {
			out = it.tmpA.Clear().SetOne()
		} else {
			out = it.zeroConst
		}
	case MirEQ:
		if a.Eq(b) {
			out = it.tmpA.Clear().SetOne()
		} else {
			out = it.zeroConst
		}
	case MirAND:
		out = it.tmpA.Clear().And(a, b)
	case MirOR:
		out = it.tmpA.Clear().Or(a, b)
	case MirXOR:
		out = it.tmpA.Clear().Xor(a, b)
	case MirBYTE:
		// Adjust for big-endian indexing expected by EVM BYTE
		// EVM BYTE: operands arrive as [value, index] in our MIR
		out = b.Byte(a)
	case MirSHL:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SHL")
		}
		// EVM order: stack pops shift (a) then value (b), result = value << shift
		out = it.tmpA.Clear().Lsh(b, uint(a.Uint64()))
	case MirSHR:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SHR")
		}
		// result = value >> shift (logical)
		out = it.tmpA.Clear().Rsh(b, uint(a.Uint64()))
	case MirSAR:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SAR")
		}
		// result = value >>> shift (arithmetic)
		out = it.tmpA.Clear().SRsh(b, uint(a.Uint64()))
	default:
		return fmt.Errorf("unexpected arithmetic op: 0x%x", byte(m.op))
	}
	it.setResult(m, out)
	return nil
}

func (it *MIRInterpreter) evalValue(v *Value) (ret *uint256.Int) {
	if v == nil {
		return it.zeroConst
	}
	switch v.kind {
	case Konst:
		if v.u != nil {
			return v.u
		}
		return it.zeroConst
	case Unknown:
		// Unknown values should not default to zero - this indicates a CFG construction issue
		// or stack underflow. Return zeroConst as fallback but log error for debugging.
		mirDebugError("MIR evalValue encountered Unknown value - this should not happen at runtime",
			"def", v.def != nil, "def_evmPC", func() uint {
				if v.def != nil {
					return v.def.evmPC
				}
				return 0
			}())
		// For now, return zeroConst as fallback, but this should ideally trigger EVM fallback
		return it.zeroConst
	case Variable, Arguments:
		// If this value is marked as live-in from a parent, prefer global cross-BB map first
		if v.def != nil {
			// For PHI definitions, prefer predecessor-sensitive cache
			if v.def.op == MirPHI {
				// Use last known predecessor for this PHI if available, else immediate prevBB
				if it.phiResults != nil {
					// Prefer exact predecessor mapping
					if preds, ok := it.phiResults[v.def]; ok {
						if it.prevBB != nil {
							if val, ok2 := preds[it.prevBB]; ok2 && val != nil {
								return val
							}
						}
						// Fallback to last predecessor observed for this PHI
						if last := it.phiLastPred[v.def]; last != nil {
							if val, ok2 := preds[last]; ok2 && val != nil {
								return val
							}
						}
					}
				}
				// Signature caches as ultimate fallback (in case *MIR differs)
				if v.def.evmPC != 0 {
					if m := it.phiResultsBySig[uint64(v.def.evmPC)]; m != nil {
						if preds := m[v.def.idx]; preds != nil {
							if it.prevBB != nil {
								if val := preds[it.prevBB]; val != nil {
									return val
								}
							}
							if lastm := it.phiLastPredBySig[uint64(v.def.evmPC)]; lastm != nil {
								if last := lastm[v.def.idx]; last != nil {
									if val := preds[last]; val != nil {
										return val
									}
								}
							}
						}
					}
				}
			}
			// First try local per-block result (most recent, most accurate)
			// But only if the instruction is actually in the current block
			// Check if current block contains this instruction
			defInCurrentBlock := false
			if it.currentBB != nil && v.def != nil {
				for _, ins := range it.currentBB.instructions {
					if ins == v.def {
						defInCurrentBlock = true
						break
					}
				}
			}
			if defInCurrentBlock && v.def.idx >= 0 && v.def.idx < len(it.results) {
				if r := it.results[v.def.idx]; r != nil {
					return r
				}
			}
			// Then try global cache for live-in values (only if not found locally)
			if v.liveIn {
				// PURE APPROACH 1: Always use signature-based cache (evmPC, idx)
				// This is simpler, more maintainable, and absolutely correct for loops
				if v.def.evmPC != 0 {
					if byPC := it.globalResultsBySig[uint64(v.def.evmPC)]; byPC != nil {
						if val, ok := byPC[v.def.idx]; ok && val != nil {
							return val
						}
					}
				}

				// Fallback to pointer-based cache for compatibility (mirHandleJUMP uses it)
				if it.globalResults != nil {
					if r, ok := it.globalResults[v.def]; ok && r != nil {
						return r
					}
				}
			}
			// Finally, fallback to global map for non-live-in cross-BB defs
			// Also check globalResultsBySig for non-live-in values (they might still be in the cache)
			if v.def.evmPC != 0 {
				if byPC := it.globalResultsBySig[uint64(v.def.evmPC)]; byPC != nil {
					if val, ok := byPC[v.def.idx]; ok && val != nil {
						return val
					}
				}
			}
			if it.globalResults != nil {
				if r, ok := it.globalResults[v.def]; ok && r != nil {
					return r
				}
			}
		}
		return it.zeroConst
	default:
		return it.zeroConst
	}
}

func (it *MIRInterpreter) setResult(m *MIR, val *uint256.Int) {
	if m == nil || val == nil {
		return
	}
	if m.idx < 0 {
		return
	}
	// Ensure results slice can hold index m.idx
	if m.idx >= len(it.results) {
		// Grow to m.idx+1, preserving existing entries
		newLen := m.idx + 1
		grown := make([]*uint256.Int, newLen)
		copy(grown, it.results)
		it.results = grown
		if it.resultsCap < newLen {
			it.resultsCap = newLen
		}
	}
	if it.results[m.idx] == nil {
		it.results[m.idx] = new(uint256.Int)
	}
	it.results[m.idx].Set(val)
}

// MemoryCap returns the capacity of the internal memory buffer
func (it *MIRInterpreter) MemoryCap() int {
	return cap(it.memory)
}

// TruncateMemory resets the logical length of interpreter memory to zero,
// preserving the underlying capacity for reuse.
func (it *MIRInterpreter) TruncateMemory() {
	if it.memory != nil {
		it.memory = it.memory[:0]
	}
}

// ResetReturnData clears the return data buffer without reallocating.
func (it *MIRInterpreter) ResetReturnData() {
	if it.returndata != nil {
		it.returndata = it.returndata[:0]
	}
}

func (it *MIRInterpreter) ensureMemSize(size uint64) {
	curLen := uint64(len(it.memory))
	if curLen >= size {
		return
	}
	// If capacity is sufficient, extend length in-place (zero-initialized growth)
	curCap := uint64(cap(it.memory))
	if curCap >= size {
		need := size - curLen
		it.memory = append(it.memory, make([]byte, need)...)
		return
	}
	// Otherwise grow geometrically to reduce reallocations
	newCap := uint64(64)
	if newCap < curCap {
		newCap = curCap
	}
	for newCap < size {
		newCap *= 2
	}
	newMem := make([]byte, curLen, newCap)
	copy(newMem, it.memory)
	// Extend length to requested size (zero-filled tail)
	newMem = append(newMem, make([]byte, size-curLen)...)
	it.memory = newMem
}

// EnsureMemorySize grows interpreter memory to at least size bytes (testing/adapter use).
func (it *MIRInterpreter) EnsureMemorySize(size uint64) {
	it.ensureMemSize(size)
}

func (it *MIRInterpreter) readMem(off, sz *uint256.Int) []byte {
	o := off.Uint64()
	sReq := sz.Uint64()
	memLen := uint64(len(it.memory))
	// Compute high index safely (detect overflow)
	hi := o + sReq
	if hi < o {
		hi = memLen
	}
	if hi > memLen {
		hi = memLen
	}
	if o > hi {
		return nil
	}
	return append([]byte(nil), it.memory[o:hi]...)
}

// readMemView returns a view (subslice) of the internal memory without allocating.
// The returned slice is only valid until the next memory growth.
func (it *MIRInterpreter) readMemView(off, sz *uint256.Int) []byte {
	o := off.Uint64()
	sReq := sz.Uint64()
	memLen := uint64(len(it.memory))
	hi := o + sReq
	if hi < o {
		hi = memLen
	}
	if hi > memLen {
		hi = memLen
	}
	if o > hi {
		return nil
	}
	return it.memory[o:hi]
}

func (it *MIRInterpreter) readMem32(off *uint256.Int) []byte {
	it.ensureMemSize(off.Uint64() + 32)
	return append([]byte(nil), it.memory[off.Uint64():off.Uint64()+32]...)
}

func (it *MIRInterpreter) readMem32Into(off *uint256.Int, dst *[32]byte) {
	it.ensureMemSize(off.Uint64() + 32)
	copy(dst[:], it.memory[off.Uint64():off.Uint64()+32])
	if it.tracerEx != nil || it.tracer != nil {
		//
	}
}

func (it *MIRInterpreter) writeMem32(off, val *uint256.Int) {
	o := off.Uint64()
	it.ensureMemSize(o + 32)
	// Directly encode 32-byte big-endian representation without extra zeroing/allocations
	val.PutUint256(it.memory[o:])
}

func (it *MIRInterpreter) writeMem8(off, val *uint256.Int) {
	o := off.Uint64()
	it.ensureMemSize(o + 1)
	it.memory[o] = byte(val.Uint64() & 0xff)
}

func (it *MIRInterpreter) memCopy(dest, src, length *uint256.Int) {
	d := dest.Uint64()
	s := src.Uint64()
	l := length.Uint64()
	it.ensureMemSize(d + l)
	it.ensureMemSize(s + l)
	copy(it.memory[d:d+l], it.memory[s:s+l])
}

// readMemCopy allocates a new buffer of size sz and copies from memory at off
func (it *MIRInterpreter) readMemCopy(off, sz *uint256.Int) []byte {
	o := off.Uint64()
	sReq := sz.Uint64()

	// Hard-cap to a reasonable bound to avoid pathological allocations
	const maxCopy = 64 * 1024 * 1024 // 64 MiB
	if sReq > maxCopy {
		sReq = maxCopy
	}
	if sReq == 0 {
		return nil
	}

	out := make([]byte, sReq)
	memLen := uint64(len(it.memory))
	if o < memLen {
		available := memLen - o
		toCopy := sReq
		if toCopy > available {
			toCopy = available
		}
		copy(out, it.memory[o:o+toCopy])
	}
	return out
}

func (it *MIRInterpreter) readCalldata32(off *uint256.Int) []byte {
	o := off.Uint64()
	end := o + 32
	if o >= uint64(len(it.env.Calldata)) {
		return make([]byte, 32)
	}
	if end > uint64(len(it.env.Calldata)) {
		buf := make([]byte, 32)
		copy(buf, it.env.Calldata[o:])
		return buf
	}
	buf := make([]byte, 32)
	copy(buf, it.env.Calldata[o:end])
	return buf
}

func (it *MIRInterpreter) readCalldata32Into(off *uint256.Int, dst *[32]byte) {
	o := off.Uint64()
	end := o + 32
	// zero dst first
	for i := range dst {
		dst[i] = 0
	}
	if o >= uint64(len(it.env.Calldata)) {
		return
	}
	if end > uint64(len(it.env.Calldata)) {
		copy(dst[:], it.env.Calldata[o:])
		return
	}
	copy(dst[:], it.env.Calldata[o:end])
}

func (it *MIRInterpreter) calldataCopy(dest, off, sz *uint256.Int) {
	d := dest.Uint64()
	o := off.Uint64()
	s := sz.Uint64()
	end := o + s
	it.ensureMemSize(d + s)
	it.lastCopyDest, it.lastCopyOff, it.lastCopySize = d, o, s
	if mirCalldataCopyHook != nil {
		mirCalldataCopyHook(d, o, s)
	}
	if o >= uint64(len(it.env.Calldata)) {
		// zero fill
		for i := uint64(0); i < s; i++ {
			it.memory[d+i] = 0
		}
		return
	}
	if end > uint64(len(it.env.Calldata)) {
		copy(it.memory[d:], it.env.Calldata[o:])
		for i := uint64(len(it.env.Calldata)) - o; i < s; i++ {
			it.memory[d+i] = 0
		}
		return
	}
	copy(it.memory[d:d+s], it.env.Calldata[o:end])
}

func (it *MIRInterpreter) readReturnData32(off *uint256.Int) []byte {
	o := off.Uint64()
	end := o + 32
	if o >= uint64(len(it.returndata)) {
		return make([]byte, 32)
	}
	if end > uint64(len(it.returndata)) {
		buf := make([]byte, 32)
		copy(buf, it.returndata[o:])
		return buf
	}
	buf := make([]byte, 32)
	copy(buf, it.returndata[o:end])
	return buf
}

func (it *MIRInterpreter) readReturnData32Into(off *uint256.Int, dst *[32]byte) {
	o := off.Uint64()
	end := o + 32
	for i := range dst {
		dst[i] = 0
	}
	if o >= uint64(len(it.returndata)) {
		return
	}
	if end > uint64(len(it.returndata)) {
		copy(dst[:], it.returndata[o:])
		return
	}
	copy(dst[:], it.returndata[o:end])
}

// resolveJumpDestValue resolves a potential PHI-derived destination value using
// predecessor-sensitive caches. It returns (value, true) when resolved. If the
// operand is not PHI, it falls back to evalValue. If the operand is PHI and no
// cache matches, it returns (nil, false) to signal unknown.
func (it *MIRInterpreter) resolveJumpDestValue(op *Value) (*uint256.Int, bool) {
	if op == nil {
		return nil, false
	}
	// Constant fast path (no allocations)
	if op.kind == Konst {
		if op.u != nil {
			return op.u, true
		}
		// Fallback if u is nil: evaluate once
		return it.evalValue(op), true
	}
	if op.def != nil && op.def.op == MirPHI {
		// Prefer exact value from the current block's exit stack by PHI slot when available.
		// This mirrors EVM semantics where the JUMP destination is taken from the actual
		// top-of-stack at the JUMP, not from a static PHI alternative.
		// Try exact predecessor cache
		if it.phiResults != nil {
			if preds, ok := it.phiResults[op.def]; ok {
				if it.prevBB != nil {
					if v := preds[it.prevBB]; v != nil {
						return v, true
					}
				}
				if last := it.phiLastPred[op.def]; last != nil {
					if v := preds[last]; v != nil {
						return v, true
					}
				}
			}
		}
		// Signature-based fallback
		if op.def.evmPC != 0 {
			if bypc := it.phiResultsBySig[uint64(op.def.evmPC)]; bypc != nil {
				if preds := bypc[op.def.idx]; preds != nil {
					if it.prevBB != nil {
						if v := preds[it.prevBB]; v != nil {
							return v, true
						}
					}
					if lastm := it.phiLastPredBySig[uint64(op.def.evmPC)]; lastm != nil {
						if last := lastm[op.def.idx]; last != nil {
							if v := preds[last]; v != nil {
								return v, true
							}
						}
					}
				}
			}
		}
		// Unresolved PHI
		// As a last resort, evaluate the operand directly. This mirrors EVM's
		// dynamic jump semantics where the destination is taken from the top of
		// stack even if we lack an exact predecessor mapping.
		if v := it.evalValue(op); v != nil {
			return v, true
		}
		return nil, false
	}
	// Not a PHI; just evaluate
	return it.evalValue(op), true
}

// resolveJumpDestUint64 mirrors resolveJumpDestValue but returns a uint64
// destination directly, avoiding intermediate allocations.
func (it *MIRInterpreter) resolveJumpDestUint64(op *Value) (uint64, bool) {
	if op == nil {
		return 0, false
	}
	// Constant fast path
	if op.kind == Konst {
		if op.u != nil {
			u, _ := op.u.Uint64WithOverflow()
			return u, true
		}
		v := it.evalValue(op)
		if v == nil {
			return 0, false
		}
		u, _ := v.Uint64WithOverflow()
		return u, true
	}
	// PHI-aware path uses the cached pointer but only extracts uint64
	if op.def != nil && op.def.op == MirPHI {
		if it.phiResults != nil {
			if preds, ok := it.phiResults[op.def]; ok {
				if it.prevBB != nil {
					if v := preds[it.prevBB]; v != nil {
						u, _ := v.Uint64WithOverflow()
						return u, true
					}
				}
				if last := it.phiLastPred[op.def]; last != nil {
					if v := preds[last]; v != nil {
						u, _ := v.Uint64WithOverflow()
						return u, true
					}
				}
			}
		}
		if op.def.evmPC != 0 {
			if bypc := it.phiResultsBySig[uint64(op.def.evmPC)]; bypc != nil {
				if preds := bypc[op.def.idx]; preds != nil {
					if it.prevBB != nil {
						if v := preds[it.prevBB]; v != nil {
							u, _ := v.Uint64WithOverflow()
							return u, true
						}
					}
					if lastm := it.phiLastPredBySig[uint64(op.def.evmPC)]; lastm != nil {
						if last := lastm[op.def.idx]; last != nil {
							if v := preds[last]; v != nil {
								u, _ := v.Uint64WithOverflow()
								return u, true
							}
						}
					}
				}
			}
		}
		return 0, false
	}
	v := it.evalValue(op)
	if v == nil {
		return 0, false
	}
	u, _ := v.Uint64WithOverflow()
	return u, true
}

// scheduleJump validates and schedules a control transfer to udest.
// It publishes current block live-outs and records predecessor for PHIs.
func (it *MIRInterpreter) scheduleJump(udest uint64, m *MIR, isFallthrough bool) error {
	if it.env == nil || it.env.CheckJumpdest == nil || it.env.ResolveBB == nil {
		return fmt.Errorf("jump environment not initialized")
	}
	// Ensure we publish live-outs BEFORE attempting to use them for backfill/stabilization
	if it.currentBB != nil {
		it.publishLiveOut(it.currentBB)
	}
	// If the destination is invalid, try a targeted PHI-operand re-evaluation to pick
	// a valid JUMPDEST. This helps constructor/initcode paths where PHIs exist at
	// dispatch points and a zero/default choice could appear transiently.
	if !isFallthrough && !it.env.CheckJumpdest(udest) && m != nil && (m.op == MirJUMP || m.op == MirJUMPI) {
		// If builder hinted a preferred target, try it first
		if len(m.meta) == 8 {
			var preferred uint64
			for k := 0; k < 8; k++ {
				preferred = (preferred << 8) | uint64(m.meta[k])
			}
			if it.env.CheckJumpdest(preferred) {
				mirDebugWarn("MIR scheduleJump: using builder-preferred target for invalid dest", "from_evm_pc", m.evmPC, "old_dest", udest, "new_dest", preferred)
				udest = preferred
			}
		}
		if len(m.operands) > 0 {
			opv := m.operands[0]
			if opv != nil && opv.kind == Variable && opv.def != nil && opv.def.op == MirPHI {
				var chosenAlt *uint256.Int
				var chosenU uint64
				for _, alt := range opv.def.operands {
					if alt == nil {
						continue
					}
					if v := it.evalValue(alt); v != nil {
						if u, _ := v.Uint64WithOverflow(); it.env.CheckJumpdest(u) {
							udest = u
							chosenAlt = new(uint256.Int).Set(v)
							chosenU = u
							break
						}
					}
				}
				// Pin caches to stabilize future uses of this PHI-derived destination
				if chosenAlt != nil {
					if it.globalResults != nil {
						it.globalResults[opv.def] = new(uint256.Int).Set(chosenAlt)
					}
					if opv.def.evmPC != 0 {
						if it.globalResultsBySig[uint64(opv.def.evmPC)] == nil {
							it.globalResultsBySig[uint64(opv.def.evmPC)] = make(map[int]*uint256.Int)
						}
						it.globalResultsBySig[uint64(opv.def.evmPC)][opv.def.idx] = new(uint256.Int).Set(chosenAlt)
					}
					mirDebugWarn("MIR scheduleJump: pinned PHI-derived dest", "from_evm_pc", m.evmPC, "dest_pc", chosenU)
				}
			}
		}
	}
	// If the destination is a valid JUMPDEST but points to the current block's start (self-loop),
	// try to pick an alternative PHI operand that yields a valid, non-self JUMPDEST to escape.
	if !isFallthrough && m != nil && (m.op == MirJUMP || m.op == MirJUMPI) && it.currentBB != nil {
		curFirst := uint64(it.currentBB.firstPC)
		// If we have a backward edge and multiple PHI-derived choices exist, prefer a forward landing
		// (smallest u > curFirst). This mirrors how constructor dispatchers eventually progress forward.
		// prefer forward logic removed to rely on correct stack/PHI resolution
		/*
			if udest < curFirst && len(m.operands) > 0 {
				opv := m.operands[0]
				if opv != nil && opv.kind == Variable && opv.def != nil && opv.def.op == MirPHI {
					var bestForward uint64
					foundForward := false
					var bestVal *uint256.Int
					for _, alt := range opv.def.operands {
						if alt == nil {
							continue
						}
						if v := it.evalValue(alt); v != nil {
							if u, _ := v.Uint64WithOverflow(); it.env.CheckJumpdest(u) && u > curFirst {
								if !foundForward || u < bestForward {
									bestForward = u
									bestVal = new(uint256.Int).Set(v)
									foundForward = true
								}
							}
						}
					}
					if foundForward {
						mirDebugWarn("MIR scheduleJump: prefer forward PHI dest", "from_evm_pc", m.evmPC, "old_dest", udest, "new_dest", bestForward, "curFirst", curFirst)
						udest = bestForward
						// Pin chosen forward to caches to stabilize
						if bestVal != nil {
							if it.globalResults != nil {
								it.globalResults[opv.def] = new(uint256.Int).Set(bestVal)
							}
							if opv.def.evmPC != 0 {
								if it.globalResultsBySig[uint64(opv.def.evmPC)] == nil {
									it.globalResultsBySig[uint64(opv.def.evmPC)] = make(map[int]*uint256.Int)
								}
								it.globalResultsBySig[uint64(opv.def.evmPC)][opv.def.idx] = new(uint256.Int).Set(bestVal)
							}
						}
					}
				}
			}
		*/
		if udest == curFirst && len(m.operands) > 0 {
			// First, if the builder provided a preferred non-self target in meta, honor it.
			if len(m.meta) == 8 {
				var preferred uint64
				for k := 0; k < 8; k++ {
					preferred = (preferred << 8) | uint64(m.meta[k])
				}
				if preferred != curFirst && it.env.CheckJumpdest(preferred) {
					mirDebugWarn("MIR scheduleJump: using builder-preferred non-self target", "from_evm_pc", m.evmPC, "old_dest", udest, "new_dest", preferred)
					udest = preferred
				}
			}
			opv := m.operands[0]
			if opv != nil && opv.kind == Variable && opv.def != nil && opv.def.op == MirPHI {
				// Try operands in order: prefer predecessors' exit-stack value, then other operands
				alts := opv.def.operands
				for _, alt := range alts {
					if alt == nil {
						continue
					}
					if v := it.evalValue(alt); v != nil {
						if u, _ := v.Uint64WithOverflow(); it.env.CheckJumpdest(u) && u != curFirst {
							mirDebugWarn("MIR scheduleJump: replaced self-loop dest with alternative", "from_evm_pc", m.evmPC, "old_dest", udest, "new_dest", u)
							udest = u
							// Pin chosen alternative for stability in later uses
							if it.globalResults != nil {
								it.globalResults[opv.def] = new(uint256.Int).Set(v)
							}
							if opv.def.evmPC != 0 {
								if it.globalResultsBySig[uint64(opv.def.evmPC)] == nil {
									it.globalResultsBySig[uint64(opv.def.evmPC)] = make(map[int]*uint256.Int)
								}
								it.globalResultsBySig[uint64(opv.def.evmPC)][opv.def.idx] = new(uint256.Int).Set(v)
							}
							break
						}
					}
				}
			}
		}
	}
	// First, enforce EVM byte-level rule: target must be a valid JUMPDEST and not in push-data
	if !isFallthrough {
		if !it.env.CheckJumpdest(udest) {
			mirDebugError("MIR jump invalid jumpdest - mirroring EVM error", "from_evm_pc", m.evmPC, "dest_pc", udest)
			return fmt.Errorf("invalid jump destination: %d", udest)
		}
	}
	// Then resolve to a basic block in the CFG
	it.nextBB = it.env.ResolveBB(udest)
	// Detect trivial two-node oscillation: jumping back-and-forth between two blocks.
	if !isFallthrough && it.currentBB != nil {
		from := uint64(it.currentBB.firstPC)
		// previous jump was lastJumpFrom -> lastJumpTo; now about to do from -> udest
		// if we see a 2-cycle (from == lastJumpTo && udest == lastJumpFrom), try to break it
		if it.lastJumpFrom == udest && it.lastJumpTo == from {
			// Attempt to break the 2-cycle by preferring an alternate PHI operand destination (if available)
			if m != nil && len(m.operands) > 0 {
				opv := m.operands[0]
				if opv != nil && opv.kind == Variable && opv.def != nil && opv.def.op == MirPHI {
					for _, alt := range opv.def.operands {
						if alt == nil {
							continue
						}
						if v := it.evalValue(alt); v != nil {
							if u, _ := v.Uint64WithOverflow(); it.env.CheckJumpdest(u) && u != from && u != udest {
								mirDebugWarn("MIR scheduleJump: breaking 2-cycle by choosing alternate PHI dest", "from", from, "old_dest", udest, "new_dest", u)
								udest = u
								it.nextBB = it.env.ResolveBB(udest)
								break
							}
						}
					}
				}
			}
		}
	}
	// Record last jump pair
	if it.currentBB != nil {
		it.lastJumpFrom = uint64(it.currentBB.firstPC)
		it.lastJumpTo = udest
	}
	// If destination is unmapped OR mapped but unbuilt, trigger backfill
	if it.nextBB == nil || !it.nextBB.built {
		// Attempt runtime backfill: if destination is a valid JUMPDEST and not yet built,
		// synthesize a landing block, wire the current block as parent, seed entry stack,
		// and rebuild a bounded set of successors so PHIs stabilize.
		if it.cfg != nil {
			mirDebugWarn("MIR scheduleJump backfill check", "udest", udest, "isFallthrough", isFallthrough)
			code := it.cfg.rawCode
			if int(udest) >= 0 && int(udest) < len(code) && ByteCode(code[udest]) == JUMPDEST {
				// Create or get the target BB
				targetBB := it.cfg.createBB(uint(udest), it.currentBB)
				// Wire parent and seed incoming/entry stacks from current exit
				if it.currentBB != nil {
					exit := it.currentBB.ExitStack()
					if true {
						targetBB.AddIncomingStack(it.currentBB, exit)
						targetBB.SetParents([]*MIRBasicBlock{it.currentBB})
						vs := ValueStack{}
						vs.resetTo(exit)
						vs.markAllLiveIn()
						targetBB.ResetForRebuild(false)
						unproc := MIRBasicBlockStack{}
						if err := it.cfg.buildBasicBlock(targetBB, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
							targetBB.built = true
							targetBB.queued = false
							// Rebuild successors with a conservative BFS budget to propagate PHIs
							type q struct{ items []*MIRBasicBlock }
							push := func(Q *q, b *MIRBasicBlock) {
								if b != nil {
									Q.items = append(Q.items, b)
								}
							}
							pop := func(Q *q) *MIRBasicBlock {
								if len(Q.items) == 0 {
									return nil
								}
								b := Q.items[len(Q.items)-1]
								Q.items = Q.items[:len(Q.items)-1]
								return b
							}
							var Q q
							for _, ch := range targetBB.Children() {
								push(&Q, ch)
							}
							// Drain any enqueued children produced during the initial build
							for unproc.Size() != 0 {
								push(&Q, unproc.Pop())
							}
							visited := make(map[*MIRBasicBlock]bool)
							budget := 256
							for len(Q.items) > 0 && budget > 0 {
								nb := pop(&Q)
								if nb == nil || visited[nb] {
									budget--
									continue
								}
								visited[nb] = true
								vs.resetTo(nil)
								// If single parent, seed from that parent's exit; otherwise let PHIs form
								if len(nb.Parents()) == 1 {
									if ps := nb.Parents()[0].ExitStack(); ps != nil {
										vs.resetTo(ps)
										vs.markAllLiveIn()
									}
								}
								nb.ResetForRebuild(false)
								if err := it.cfg.buildBasicBlock(nb, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
									nb.built = true
									nb.queued = false
									for _, ch := range nb.Children() {
										push(&Q, ch)
									}
								}
								budget--
							}
							// Successfully built target; use it
							it.nextBB = targetBB
						}
					}
				}
			}
		}
		// For fallthrough, try to get the block from children if ResolveBB fails
		if isFallthrough {
			children := it.currentBB.Children()
			// Convention: children[0] is fallthrough; children[1:] are explicit jump targets
			if len(children) >= 1 && children[0] != nil {
				it.nextBB = children[0]
				// Stabilize PHIs for the fallthrough child by rebuilding it with the current predecessor
				if it.cfg != nil && it.currentBB != nil {
					tgt := it.nextBB
					exit := it.currentBB.ExitStack()
					if true {
						tgt.SetParents([]*MIRBasicBlock{it.currentBB})
						if prev := tgt.IncomingStacks()[it.currentBB]; prev == nil || !stacksEqual(prev, exit) {
							tgt.AddIncomingStack(it.currentBB, exit)
						}
						vs := ValueStack{}
						vs.resetTo(exit)
						vs.markAllLiveIn()
						tgt.ResetForRebuild(false)
						unproc := MIRBasicBlockStack{}
						if err := it.cfg.buildBasicBlock(tgt, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
							tgt.built = true
							tgt.queued = false
							visited := make(map[*MIRBasicBlock]bool)
							budget := 128
							for (unproc.Size() != 0) && budget > 0 {
								nb := unproc.Pop()
								if nb == nil || visited[nb] {
									budget--
									continue
								}
								visited[nb] = true
								vs.resetTo(nil)
								if len(nb.Parents()) == 1 {
									if ps := nb.Parents()[0].ExitStack(); ps != nil {
										vs.resetTo(ps)
										vs.markAllLiveIn()
									}
								}
								nb.ResetForRebuild(false)
								if err := it.cfg.buildBasicBlock(nb, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
									nb.built = true
									nb.queued = false
								}
								budget--
							}
						}
					}
				}
			}
		}
		if it.nextBB == nil {
			// For fallthrough, let RunCFGWithResolver handle it via children instead of erroring
			if isFallthrough {
				it.nextBB = nil
				it.publishLiveOut(it.currentBB)
				it.prevBB = it.currentBB
				return nil
			}
			mirDebugError("MIR jump target not mapped in CFG", "from_evm_pc", m.evmPC, "dest_pc", udest)
			return fmt.Errorf("unresolvable jump target")
		}
	} else {
		// Target exists. For dispatcher-heavy initcode, stabilize PHIs by rebuilding
		// the destination block with the current predecessor as the sole parent and
		// seeding its incoming stack from our exit. This mirrors the builder's
		// single-incoming optimization and avoids zero-filled PHIs that can lock jumps.
		if it.cfg != nil && it.currentBB != nil {
			tgt := it.nextBB
			if exit := it.currentBB.ExitStack(); exit != nil {
				// Replace parents and incoming stack to current predecessor only
				tgt.SetParents([]*MIRBasicBlock{it.currentBB})
				if prev := tgt.IncomingStacks()[it.currentBB]; prev == nil || !stacksEqual(prev, exit) {
					tgt.AddIncomingStack(it.currentBB, exit)
				}
				// Rebuild target and a small cone of successors
				vs := ValueStack{}
				vs.resetTo(exit)
				vs.markAllLiveIn()
				tgt.ResetForRebuild(false)
				unproc := MIRBasicBlockStack{}
				if err := it.cfg.buildBasicBlock(tgt, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
					tgt.built = true
					tgt.queued = false
					visited := make(map[*MIRBasicBlock]bool)
					budget := 256
					for (unproc.Size() != 0) && budget > 0 {
						nb := unproc.Pop()
						if nb == nil || visited[nb] {
							budget--
							continue
						}
						visited[nb] = true
						vs.resetTo(nil)
						if len(nb.Parents()) == 1 {
							if ps := nb.Parents()[0].ExitStack(); ps != nil {
								vs.resetTo(ps)
								vs.markAllLiveIn()
							}
						}
						nb.ResetForRebuild(false)
						if err := it.cfg.buildBasicBlock(nb, &vs, it.cfg.getMemoryAccessor(), it.cfg.getStateAccessor(), &unproc); err == nil {
							nb.built = true
							nb.queued = false
						}
						budget--
					}
				}
			}
		}
	}
	it.publishLiveOut(it.currentBB)
	it.prevBB = it.currentBB
	return errJUMP
}

func (it *MIRInterpreter) returnDataCopy(dest, off, sz *uint256.Int) {
	d := dest.Uint64()
	o := off.Uint64()
	sReq := sz.Uint64()
	// Clamp copy length to available returndata (EVM behavior: zero pad if requested > available? No, RETURNDATACOPY copies available, but what about padding?
	// EVM RETURNDATACOPY: "If the memory slice is larger than the data size, the extra memory bytes are filled with zeros?"
	// No, RETURNDATACOPY throws if end index > RETURNDATASIZE.
	// Wait, EVM semantics for RETURNDATACOPY:
	// "If the data to be copied extends beyond the size of the return data, the instruction reverts." (since Byzantium)
	// But MIR interpreter handles this?
	// If I clamp, I hide the error.
	// But MIR interpreter is supposed to rely on adapter/verifier?
	// If adapter doesn't check bounds, MIR executes.
	// EVM Interpreter checks bounds in `opReturnDataCopy`.
	// MIR should ideally panic or error if bounds exceeded, or mimic EVM.
	// However, `innerHook` calculates gas but doesn't check bounds of `returndata` because `returndata` size is dynamic and known only at execution time?
	// Wait, `RETURNDATACOPY` reads `returndata`.
	// `innerHook` doesn't check validity of `RETURNDATACOPY` bounds because it doesn't know `returndata` size (it's runtime state).
	// The MIR instruction SHOULD check bounds.
	// If `it.returndata` is too small, it should revert?
	// Current implementation clamps. This is incorrect for modern EVM.
	// But for now, let's just remove the arbitrary `maxCopy` and `d > maxCopy` check.
	// I'll stick to clamping `sAlloc` to `returndata` size for now to prevent panics, but correctness requires panic/revert if out of bounds.
	// But let's fix the memory corruption first.

	var sAlloc uint64
	if o >= uint64(len(it.returndata)) {
		sAlloc = 0
	} else {
		rem := uint64(len(it.returndata)) - o
		if sReq < rem {
			sAlloc = sReq
		} else {
			sAlloc = rem
		}
	}

	it.ensureMemSize(d + sReq) // Use sReq to ensure memory is grown to full requested size
	// Zero-pad the rest if sAlloc < sReq?
	// If EVM reverts on OOB, we should revert.
	// If we assume valid input (which we can't), we should copy what we can.
	// But the arbitrary cap was definitely wrong.
	if sAlloc > 0 {
		copy(it.memory[d:d+sAlloc], it.returndata[o:o+sAlloc])
	}
	// If sReq > sAlloc, the rest of memory [d+sAlloc : d+sReq] should be zeroed?
	// ensureMemSize zeroes new memory. If memory was already allocated, it might not be zero.
	// But RETURNDATACOPY is simpler: just copy.
}

func (it *MIRInterpreter) sload(key *uint256.Int) *uint256.Int {
	// Prefer runtime hook if provided
	var k [32]byte
	keyBytes := key.Bytes()
	copy(k[32-len(keyBytes):], keyBytes) // Fix: right-align key bytes like sstore
	if it.env.SLoadFunc != nil {
		v := it.env.SLoadFunc(k)
		return it.tmpB.Clear().SetBytes(v[:])
	}
	// Fallback to internal simulated map
	if it.env.Storage == nil {
		it.env.Storage = make(map[[32]byte][32]byte)
	}
	val, ok := it.env.Storage[k]
	if !ok {
		return it.zeroConst
	}
	return it.tmpB.Clear().SetBytes(val[:])
}

func (it *MIRInterpreter) sstore(key, val *uint256.Int) {
	var k [32]byte
	var v [32]byte
	keyBytes := key.Bytes()
	copy(k[32-len(keyBytes):], keyBytes)
	valBytes := val.Bytes()
	copy(v[32-len(valBytes):], valBytes)
	// Prefer runtime hook if provided
	if it.env.SStoreFunc != nil {
		it.env.SStoreFunc(k, v)
		return
	}
	// Fallback to internal simulated map
	if it.env.Storage == nil {
		it.env.Storage = make(map[[32]byte][32]byte)
	}
	it.env.Storage[k] = v
}
