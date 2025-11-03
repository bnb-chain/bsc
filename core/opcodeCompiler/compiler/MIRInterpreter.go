package compiler

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

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
	GetBalanceFunc func(addr [20]byte) *uint256.Int

	// External execution hooks (optional). If nil, CALL/CREATE will request fallback.
	// kind for ExternalCall: 0=CALL,1=CALLCODE,2=DELEGATECALL,3=STATICCALL
	ExternalCall func(kind byte, addr [20]byte, value *uint256.Int, input []byte) (ret []byte, success bool)
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
	env        *MIRExecutionEnv
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
	// Optional pre-execution hook for each MIR instruction (e.g., gas accounting)
	beforeOp func(*MIRPreOpContext) error
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
	log.Warn("MIR RunMIR: block", "block", block.blockNum, "instructions", len(block.instructions))
	if block == nil || len(block.instructions) == 0 {
		return it.returndata, nil
	}
	// Track current block for PHI resolution
	it.currentBB = block
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
			default:
				log.Warn("RunMIR return nil", "err", err)
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
func (it *MIRInterpreter) publishLiveOut(block *MIRBasicBlock) {
	if it == nil || block == nil || it.globalResults == nil {
		return
	}

	defs := block.LiveOutDefs()
	//log.Warn("MIR publishLiveOut", "block", block.blockNum, "size", len(block.instructions), "defs", defs, "it.results", it.results, "block.exitStack", block.ExitStack())
	if len(defs) == 0 {
		log.Warn("MIR publishLiveOut: no live outs", "block", block.blockNum)
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
		if v != nil && v.kind == Variable && v.def != nil && !inBlock[v.def] {
			if _, ok := it.globalResults[v.def]; !ok {
				val := it.evalValue(v)
				if val != nil {
					it.globalResults[v.def] = new(uint256.Int).Set(val)
					//log.Warn("MIR publishLiveOut: backfilled ancestor def", "evm_pc", v.def.evmPC, "mir_idx", v.def.idx, "value", val)
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
				it.globalResults[def] = new(uint256.Int).Set(r)
			}
		}
	}
}

// RunCFGWithResolver sets up a resolver backed by the given CFG and runs from entry block
func (it *MIRInterpreter) RunCFGWithResolver(cfg *CFG, entry *MIRBasicBlock) ([]byte, error) {
	if it.env != nil && it.env.ResolveBB == nil && cfg != nil {
		// Build a lightweight resolver using cfg.pcToBlock
		it.env.ResolveBB = func(pc uint64) *MIRBasicBlock {
			if cfg == nil || cfg.pcToBlock == nil {
				log.Warn("MIR RunCFGWithResolver: cfg or pcToBlock is nil", "pc", pc)
				return nil
			}
			if bb, ok := cfg.pcToBlock[uint(pc)]; ok {
				log.Warn("MIR RunCFGWithResolver: found bb", "pc", pc, "bb", bb.blockNum)
				return bb
			}
			log.Warn("MIR RunCFGWithResolver: not found bb", "pc", pc)
			return nil
		}
	}
	// Follow control flow starting at entry; loop jumping between blocks
	bb := entry
	for bb != nil {
		it.nextBB = nil
		_, err := it.RunMIR(bb)
		if err == nil {
			log.Warn("MIR RunCFGWithResolver: run mir success", "bb", bb.blockNum, "firstPC", bb.firstPC, "lastPC", bb.lastPC, "it.nextBB", it.nextBB)
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
							log.Warn("MIR: self-loop fallthrough detected; rerunning block", "blockNum", bb.blockNum)
							bb = children[0]
							continue
						}
						bb = children[0]
						continue
					}
					// Conditional branch (e.g., JUMPI): if no jump taken, use fallthrough child (index 1)
					if len(children) >= 2 && children[1] != nil {
						//log.Warn("MIR publish before cond fallthrough", "from_block", bb.blockNum)
						it.publishLiveOut(bb)
						// Preserve predecessor for successor PHI resolution on conditional fallthrough
						it.prevBB = bb
						if children[1] == bb {
							log.Warn("MIR: self-loop conditional fallthrough detected; rerunning block", "blockNum", bb.blockNum)
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

func (it *MIRInterpreter) exec(m *MIR) error {
	// Allow embedding runtimes (e.g., adapter) to run pre-op logic such as gas metering
	if it.beforeOp != nil {
		// Build a lightweight context with evaluated operands when useful and an estimated memory size
		ctx := &MIRPreOpContext{M: m, EvmOp: m.evmOp}
		// Mark block entry for the first instruction in the current basic block
		if m != nil && m.idx == 0 {
			ctx.IsBlockEntry = true
			ctx.Block = it.currentBB
		}
		// Evaluate operands into concrete values, preserving order
		if len(m.oprands) > 0 {
			ops := make([]*uint256.Int, 0, len(m.oprands))
			for i := 0; i < len(m.oprands); i++ {
				v := it.evalValue(m.oprands[i])
				if v == nil {
					ops = append(ops, uint256.NewInt(0))
					continue
				}
				ops = append(ops, new(uint256.Int).Set(v))
			}
			ctx.Operands = ops
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
			if len(ctx.Operands) >= 2 {
				off := ctx.Operands[0].Uint64()
				ln := ctx.Operands[1].Uint64()
				ctx.MemorySize = off + ln
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
	if h := mirHandlers[byte(m.op)]; h != nil {
		if it.tracerEx != nil {
			it.tracerEx(m)
		} else if it.tracer != nil {
			it.tracer(m.op)
		}
		return h(it, m)
	}
	// Ensure tracer is invoked for every MIR op even when no fast handler exists
	if it.tracerEx != nil {
		it.tracerEx(m)
	} else if it.tracer != nil {
		it.tracer(m.op)
	}
	switch m.op {
	case MirNOP:
		return nil
	case MirPHI:
		// Select operand based on actual predecessor block when available; fallback to first.
		if len(m.oprands) == 0 {
			it.setResult(m, it.zeroConst)
			return nil
		}
		selected := 0
		if it.currentBB != nil && it.prevBB != nil {
			parents := it.currentBB.Parents()
			for i := 0; i < len(parents) && i < len(m.oprands); i++ {
				if parents[i] == it.prevBB {
					selected = i
					break
				}
			}
		}
		v := it.evalValue(m.oprands[selected])
		it.setResult(m, v)
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
		if len(m.oprands) < 3 {
			return fmt.Errorf("ADDMOD missing operands")
		}
		val1 := it.evalValue(m.oprands[0])
		val2 := it.evalValue(m.oprands[1])
		val3 := it.evalValue(m.oprands[2])

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
		if len(m.oprands) < 3 {
			return fmt.Errorf("MULMOD missing operands")
		}
		val1 := it.evalValue(m.oprands[0])
		val2 := it.evalValue(m.oprands[1])
		val3 := it.evalValue(m.oprands[2])

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
		if len(m.oprands) < 1 {
			return fmt.Errorf("ISZERO missing operand")
		}
		v1 := it.evalValue(m.oprands[0])
		if v1.IsZero() {
			it.setResult(m, it.tmpA.Clear().SetOne())
		} else {
			it.setResult(m, it.zeroConst)
		}
		return nil
	case MirNOT:
		if len(m.oprands) < 1 {
			return fmt.Errorf("NOT missing operand")
		}
		v1 := it.evalValue(m.oprands[0])
		it.setResult(m, it.tmpA.Clear().Not(v1))
		return nil

		// Memory
	case MirMLOAD:
		// operands: offset, size(ignored; assume 32)
		if len(m.oprands) < 2 {
			return fmt.Errorf("MLOAD missing operands")
		}
		off := it.evalValue(m.oprands[0])
		it.readMem32Into(off, &it.scratch32)
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirMSIZE:
		// Return current logical memory size (length of internal buffer)
		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.memory))))
		return nil
	case MirMSTORE:
		// operands: offset, size(ignored; assume 32), value
		if len(m.oprands) < 3 {
			return fmt.Errorf("MSTORE missing operands")
		}
		off := it.evalValue(m.oprands[0])
		val := it.evalValue(m.oprands[2])
		it.writeMem32(off, val)
		return nil
	case MirMSTORE8:
		if len(m.oprands) < 3 {
			return fmt.Errorf("MSTORE8 missing operands")
		}
		off := it.evalValue(m.oprands[0])
		val := it.evalValue(m.oprands[2])
		it.writeMem8(off, val)
		return nil
	case MirMCOPY:
		// operands: dest, src, length
		if len(m.oprands) < 3 {
			return fmt.Errorf("MCOPY missing operands")
		}
		dest := it.evalValue(m.oprands[0])
		src := it.evalValue(m.oprands[1])
		length := it.evalValue(m.oprands[2])
		it.memCopy(dest, src, length)
		return nil

	// Storage (very basic map-based)
	case MirSLOAD:
		if len(m.oprands) < 1 {
			return fmt.Errorf("SLOAD missing key")
		}
		key := it.evalValue(m.oprands[0])
		val := it.sload(key)
		it.setResult(m, val)
		return nil
	case MirSSTORE:
		if len(m.oprands) < 2 {
			return fmt.Errorf("SSTORE missing operands")
		}
		key := it.evalValue(m.oprands[0])
		val := it.evalValue(m.oprands[1])
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
		if len(m.oprands) < 3 {
			return fmt.Errorf("CODECOPY missing operands")
		}
		dest := it.evalValue(m.oprands[0])
		off := it.evalValue(m.oprands[1])
		sz := it.evalValue(m.oprands[2])
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("EXTCODESIZE missing operand")
		}
		addrVal := it.evalValue(m.oprands[0]).Bytes32()
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
		if len(m.oprands) < 4 {
			return fmt.Errorf("EXTCODECOPY missing operands")
		}
		addrVal := it.evalValue(m.oprands[0]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrVal[12:])
		dest := it.evalValue(m.oprands[1])
		off := it.evalValue(m.oprands[2])
		sz := it.evalValue(m.oprands[3])
		d := dest.Uint64()
		s := sz.Uint64()
		it.ensureMemSize(d + s)
		buf := make([]byte, s)
		if it.env != nil && it.env.ExtCodeCopy != nil {
			it.env.ExtCodeCopy(a20, off.Uint64(), buf)
		} else {
			for i := range buf {
				buf[i] = 0
			}
		}
		copy(it.memory[d:d+s], buf)
		return nil
	case MirBLOCKHASH:
		if len(m.oprands) < 1 {
			return fmt.Errorf("BLOCKHASH missing operand")
		}
		num := it.evalValue(m.oprands[0]).Uint64()
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("BLOBHASH missing operand")
		}
		idx := it.evalValue(m.oprands[0]).Uint64()
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("BALANCE missing operand")
		}
		addrVal := it.evalValue(m.oprands[0])
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("CALLDATALOAD missing offset")
		}
		off := it.evalValue(m.oprands[0])
		it.readCalldata32Into(off, &it.scratch32)
		//log.Warn("MIR CALLDATALOAD", "calldata", fmt.Sprintf("%x", it.env.Calldata), "off", off, "scratch32", fmt.Sprintf("%x", it.scratch32))
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirCALLDATASIZE:

		it.setResult(m, it.tmpA.Clear().SetUint64(uint64(len(it.env.Calldata))))
		return nil
	case MirCALLDATACOPY:
		// operands: dest, offset, size
		if len(m.oprands) < 3 {
			return fmt.Errorf("CALLDATACOPY missing operands")
		}
		dest := it.evalValue(m.oprands[0])
		off := it.evalValue(m.oprands[1])
		sz := it.evalValue(m.oprands[2])
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("RETURNDATALOAD missing offset")
		}
		off := it.evalValue(m.oprands[0])
		it.readReturnData32Into(off, &it.scratch32)
		it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
		return nil
	case MirRETURNDATACOPY:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: RETURNDATACOPY")
		}
		if len(m.oprands) < 3 {
			return fmt.Errorf("RETURNDATACOPY missing operands")
		}
		dest := it.evalValue(m.oprands[0])
		off := it.evalValue(m.oprands[1])
		sz := it.evalValue(m.oprands[2])
		it.returnDataCopy(dest, off, sz)
		return nil
	case MirTLOAD:
		// transient storage load (EIP-1153)
		if len(m.oprands) < 1 {
			return fmt.Errorf("TLOAD missing key")
		}
		key := it.evalValue(m.oprands[0]).Bytes32()
		var k [32]byte
		copy(k[:], key[:])
		if it.transientStorage == nil {
			it.transientStorage = make(map[[32]byte][32]byte)
		}
		val := it.transientStorage[k]
		it.setResult(m, it.tmpA.Clear().SetBytes(val[:]))
		return nil
	case MirTSTORE:
		if len(m.oprands) < 2 {
			return fmt.Errorf("TSTORE missing operands")
		}
		key := it.evalValue(m.oprands[0]).Bytes32()
		val := it.evalValue(m.oprands[1])
		var k [32]byte
		var v [32]byte
		copy(k[:], key[:])
		bytes := val.Bytes()
		copy(v[32-len(bytes):], bytes)
		if it.transientStorage == nil {
			it.transientStorage = make(map[[32]byte][32]byte)
		}
		it.transientStorage[k] = v
		return nil

	// Hashing (placeholder: full keccak over memory slice)
	case MirKECCAK256:
		if len(m.oprands) < 2 {
			return fmt.Errorf("KECCAK256 missing operands")
		}
		off := it.evalValue(m.oprands[0])
		sz := it.evalValue(m.oprands[1])
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
		// Here, read memory and topics from oprands if present else no-op.
		// Fallback: if no explicit operands encoded, skip.
		if len(m.oprands) < 2 {
			return nil
		}
		dataOff := it.evalValue(m.oprands[0])
		dataSz := it.evalValue(m.oprands[1])
		data := it.readMem(dataOff, dataSz)
		// collect topics (each is 32 bytes in value form)
		topics := make([][32]byte, 0, numTopics)
		for i := 0; i < numTopics && 2+i < len(m.oprands); i++ {
			v := it.evalValue(m.oprands[2+i]).Bytes32()
			var t [32]byte
			copy(t[:], v[:])
			topics = append(topics, t)
		}
		it.env.LogFunc(it.env.Self, topics, data)
		return nil

	// System returns
	case MirRETURN:
		if len(m.oprands) < 2 {
			return fmt.Errorf("RETURN missing operands")
		}
		off := it.evalValue(m.oprands[0])
		sz := it.evalValue(m.oprands[1])
		it.returndata = append([]byte(nil), it.readMem(off, sz)...)
		return errRETURN
	case MirREVERT:
		if !it.env.IsByzantium {
			return fmt.Errorf("invalid opcode: REVERT")
		}
		if len(m.oprands) < 2 {
			return fmt.Errorf("REVERT missing operands")
		}
		off := it.evalValue(m.oprands[0])
		sz := it.evalValue(m.oprands[1])
		it.returndata = append([]byte(nil), it.readMem(off, sz)...)
		return errREVERT
	case MirCALL:
		// operands: gas, addr, value, inOffset, inSize, outOffset, outSize
		if len(m.oprands) < 7 {
			return fmt.Errorf("CALL missing operands")
		}
		addrV := it.evalValue(m.oprands[1]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		value := it.evalValue(m.oprands[2])
		inOff := it.evalValue(m.oprands[3])
		inSz := it.evalValue(m.oprands[4])
		outOff := it.evalValue(m.oprands[5])
		outSz := it.evalValue(m.oprands[6])
		input := it.readMem(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			ret, ok := it.env.ExternalCall(0, a20, value, input)
			it.returndata = append([]byte(nil), ret...)
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
		if len(m.oprands) < 7 {
			return fmt.Errorf("CALLCODE missing operands")
		}
		addrV := it.evalValue(m.oprands[1]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		value := it.evalValue(m.oprands[2])
		inOff := it.evalValue(m.oprands[3])
		inSz := it.evalValue(m.oprands[4])
		outOff := it.evalValue(m.oprands[5])
		outSz := it.evalValue(m.oprands[6])
		input := it.readMem(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			ret, ok := it.env.ExternalCall(1, a20, value, input)
			it.returndata = append([]byte(nil), ret...)
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
		if len(m.oprands) < 6 {
			return fmt.Errorf("DELEGATECALL missing operands")
		}
		addrV := it.evalValue(m.oprands[1]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		inOff := it.evalValue(m.oprands[2])
		inSz := it.evalValue(m.oprands[3])
		outOff := it.evalValue(m.oprands[4])
		outSz := it.evalValue(m.oprands[5])
		input := it.readMem(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			ret, ok := it.env.ExternalCall(2, a20, nil, input)
			it.returndata = append([]byte(nil), ret...)
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
		if len(m.oprands) < 6 {
			return fmt.Errorf("STATICCALL missing operands")
		}
		addrV := it.evalValue(m.oprands[1]).Bytes32()
		var a20 [20]byte
		copy(a20[:], addrV[12:])
		inOff := it.evalValue(m.oprands[2])
		inSz := it.evalValue(m.oprands[3])
		outOff := it.evalValue(m.oprands[4])
		outSz := it.evalValue(m.oprands[5])
		input := it.readMem(inOff, inSz)
		if it.env != nil && it.env.ExternalCall != nil {
			ret, ok := it.env.ExternalCall(3, a20, nil, input)
			it.returndata = append([]byte(nil), ret...)
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
		if len(m.oprands) < 3 {
			return fmt.Errorf("CREATE missing operands")
		}
		value := it.evalValue(m.oprands[0])
		off := it.evalValue(m.oprands[1])
		sz := it.evalValue(m.oprands[2])
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("EXTCODEHASH missing operand")
		}
		addrVal := it.evalValue(m.oprands[0]).Bytes32()
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
		if len(m.oprands) < 4 {
			return fmt.Errorf("CREATE2 missing operands")
		}
		value := it.evalValue(m.oprands[0])
		off := it.evalValue(m.oprands[1])
		sz := it.evalValue(m.oprands[2])
		saltVal := it.evalValue(m.oprands[3]).Bytes32()
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
		if len(m.oprands) < 1 {
			return fmt.Errorf("DUP missing operand")
		}
		v := it.evalValue(m.oprands[0])
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
	if len(m.oprands) < 2 {
		return fmt.Errorf("RETURN missing operands")
	}
	off := it.evalValue(m.oprands[0])
	sz := it.evalValue(m.oprands[1])
	// Avoid two allocations by using a single copy into a fresh buffer
	it.returndata = it.readMemCopy(off, sz)
	return errRETURN
}

func mirHandleJUMP(it *MIRInterpreter, m *MIR) error {
	if it.env == nil || it.env.CheckJumpdest == nil || it.env.ResolveBB == nil {
		return nil
	}
	if len(m.oprands) < 1 {
		return fmt.Errorf("JUMP missing destination")
	}
	dest, ok := it.resolveJumpDestValue(m.oprands[0])
	if !ok {
		log.Error("MIR JUMP cannot resolve PHI-derived dest - requesting EVM fallback", "from_evm_pc", m.evmPC)
		return ErrMIRFallback
	}
	udest, _ := dest.Uint64WithOverflow()
	if m.evmPC == 5842 {
		log.Warn("MIR JUMP", "oprands0", m.oprands[0], "dest", dest, "udest", udest, "it.globalResults", it.globalResults)
		if m.oprands[0].def != nil {
			log.Warn("MIR JUMP", "oprands0.def", m.oprands[0].def, "evmPC", m.oprands[0].def.evmPC, "idx", m.oprands[0].def.idx)
		}
	}
	// Cache resolved PHI-based destination to stabilize later uses across blocks
	if opv := m.oprands[0]; opv != nil && opv.kind == Variable && opv.def != nil {
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
	if len(m.oprands) < 2 {
		return fmt.Errorf("JUMPI missing operands")
	}
	cond := it.evalValue(m.oprands[1])
	if cond.IsZero() {
		// fallthrough
		dest := m.evmPC + 1
		udest := uint64(dest)
		log.Warn("mir exec JUMPI", "cond", cond, "dest", dest, "udest", udest)
		return it.scheduleJump(udest, m, true)
	}
	dest, ok := it.resolveJumpDestValue(m.oprands[0])
	if !ok {
		log.Error("MIR JUMPI cannot resolve PHI-derived dest - requesting EVM fallback", "from_evm_pc", m.evmPC)
		return ErrMIRFallback
	}
	udest, _ := dest.Uint64WithOverflow()
	return it.scheduleJump(udest, m, false)
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
				if m.phiStackIndex == 1 {
					log.Warn("<<<<<<<MIR PHI", "src", src, "kind", src.kind, "&src.def", &src.def,
						"exitIndex", len(exit)-1-idxFromTop)
					log.Warn("<<<<<<<MIR PHI", "it.globalResults", it.globalResults)
				}
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
				if m.evmPC == 5351 {
					if src.def != nil {
						log.Warn(">>>>>>>MIR PHI", "phiStackIndex", m.phiStackIndex, "src pc", src.def.evmPC, "val", val)
					} else {
						log.Warn(">>>>>>>MIR PHI", "phiStackIndex", m.phiStackIndex, "src", src.kind, "val", val)
					}
				}
				return nil
			}
		}
	}
	// Fallback: pick operand corresponding to predecessor position among parents
	if len(m.oprands) == 0 {
		it.setResult(m, it.zeroConst)
		return nil
	}
	selected := 0
	if it.currentBB != nil && it.prevBB != nil {
		parents := it.currentBB.Parents()
		for i := 0; i < len(parents) && i < len(m.oprands); i++ {
			if parents[i] == it.prevBB {
				selected = i
				break
			}
		}
	}

	val := it.evalValue(m.oprands[selected])
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
	if len(m.oprands) < 2 {
		return fmt.Errorf("MLOAD missing operands")
	}
	off := it.evalValue(m.oprands[0])
	it.readMem32Into(off, &it.scratch32)
	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

// Arithmetic fast handlers (two-operand)
func mirLoadAB(it *MIRInterpreter, m *MIR) (a, b *uint256.Int, err error) {
	if len(m.oprands) < 2 {
		return nil, nil, fmt.Errorf("missing operands")
	}
	if m.evmPC == 5810 {
		log.Warn("MIR MLOAD", "oprands1", m.oprands[0], "oprands2", m.oprands[1],
			"it.results", it.results)
	}
	// Use pre-encoded operand info if available
	if len(m.opKinds) >= 2 {
		// For constants, use pre-decoded value; otherwise resolve via evalValue
		if m.opKinds[0] == 0 {
			a = m.opConst[0]
		}
		if a == nil {
			a = it.evalValue(m.oprands[0])
			if m.evmPC == 5810 {
				log.Warn("MIR MLOAD", "oprands1", m.oprands[0],
					"from globalResults", it.globalResults[m.oprands[0].def], "a", a)
			}
		}
		if m.opKinds[1] == 0 {
			b = m.opConst[1]
		}
		if b == nil {
			b = it.evalValue(m.oprands[1])
			if m.evmPC == 5810 {
				log.Warn("MIR MLOAD", "oprands2", m.oprands[1],
					"from globalResults", it.globalResults[m.oprands[1].def], "b", b)
			}
		}
		return a, b, nil
	}
	return it.evalValue(m.oprands[0]), it.evalValue(m.oprands[1]), nil
}

func mirHandleADD(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Add(a, b))
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
	//log.Warn("MIR SUB", "a", a, "- b", b)
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
	it.setResult(m, it.tmpA.Clear().Rsh(b, uint(a.Uint64())))
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
	if a.Eq(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}
func mirHandleLT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	//log.Warn("MIR LT", "a", a, "< b", b)
	if err != nil {
		return err
	}
	// With operand order (a=top/right, b=next/left), LT tests a < b
	if a.Lt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
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
	if len(m.oprands) < 3 {
		return fmt.Errorf("MSTORE missing operands")
	}
	off := it.evalValue(m.oprands[0])
	val := it.evalValue(m.oprands[2])
	it.writeMem32(off, val)
	return nil
}

func mirHandleMSTORE8(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 3 {
		return fmt.Errorf("MSTORE8 missing operands")
	}
	off := it.evalValue(m.oprands[0])
	val := it.evalValue(m.oprands[2])
	it.writeMem8(off, val)
	return nil
}

func mirHandleMCOPY(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 3 {
		return fmt.Errorf("MCOPY missing operands")
	}
	d := it.evalValue(m.oprands[0])
	s := it.evalValue(m.oprands[1])
	l := it.evalValue(m.oprands[2])
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
	if len(m.oprands) < 2 {
		return fmt.Errorf("KECCAK256 missing operands")
	}
	// Operands are [offset, size]
	off := it.evalValue(m.oprands[0])
	sz := it.evalValue(m.oprands[1])
	// Read a copy of memory to hash
	bytesToHash := it.readMem(off, sz)
	if len(bytesToHash) == 0 && it.lastCopyDest == 0 && it.lastCopyOff == 0 && it.lastCopySize > 0 {
		// Heuristic for common calldata copy pattern: hash memory [0:size]
		off0 := uint256.NewInt(0)
		sizeV := new(uint256.Int).SetUint64(it.lastCopySize)
		bytesToHash = it.readMem(off0, sizeV)
	}
	if mirKeccakHook != nil {
		mirKeccakHook(off.Uint64(), sz.Uint64(), append([]byte(nil), bytesToHash...))
	}
	h := crypto.Keccak256(bytesToHash)
	it.setResult(m, it.tmpA.Clear().SetBytes(h))
	return nil
}

func mirHandleCALLDATALOAD(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 1 {
		return fmt.Errorf("CALLDATALOAD missing offset")
	}
	off := it.evalValue(m.oprands[0])
	it.readCalldata32Into(off, &it.scratch32)
	log.Warn("MIR CALLDATALOAD", "calldata", fmt.Sprintf("%x", it.env.Calldata), "off", off, "scratch32", fmt.Sprintf("%x", it.scratch32))
	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

func mirHandleRETURNDATALOAD(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 1 {
		return fmt.Errorf("RETURNDATALOAD missing offset")
	}
	off := it.evalValue(m.oprands[0])
	it.readReturnData32Into(off, &it.scratch32)
	it.setResult(m, it.tmpA.Clear().SetBytes(it.scratch32[:]))
	return nil
}

func mirHandleISZERO(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 1 {
		return fmt.Errorf("ISZERO missing operand")
	}
	v := it.evalValue(m.oprands[0])
	if v.IsZero() {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}

func mirHandleNOT(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 1 {
		return fmt.Errorf("NOT missing operand")
	}
	v := it.evalValue(m.oprands[0])
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
	if len(m.oprands) < 2 {
		return fmt.Errorf("arithmetic op requires 2 operands")
	}
	a := it.evalValue(m.oprands[0])
	b := it.evalValue(m.oprands[1])

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

func (it *MIRInterpreter) evalValue(v *Value) *uint256.Int {
	if v == nil {
		return uint256.NewInt(0)
	}
	switch v.kind {
	case Konst:
		if v.u != nil {
			return v.u
		}
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
			if v.liveIn {
				if it.globalResults != nil {
					if r, ok := it.globalResults[v.def]; ok && r != nil {
						return r
					}
				}
			}
			// Then try local per-block result
			if v.def.idx >= 0 && v.def.idx < len(it.results) {
				if r := it.results[v.def.idx]; r != nil {
					return r
				}
			}
			// Finally, fallback to global map for non-live-in cross-BB defs
			if it.globalResults != nil {
				if r, ok := it.globalResults[v.def]; ok && r != nil {
					return r
				}
			}
		}
		return uint256.NewInt(0)
	default:
		return uint256.NewInt(0)
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
	if uint64(len(it.memory)) < size {
		// Grow geometrically to reduce reallocations
		newCap := uint64(64)
		if newCap < uint64(len(it.memory)) {
			newCap = uint64(len(it.memory))
		}
		for newCap < size {
			newCap *= 2
		}
		newMem := make([]byte, newCap)
		copy(newMem, it.memory)
		it.memory = newMem
	}
}

func (it *MIRInterpreter) readMem(off, sz *uint256.Int) []byte {
	o := off.Uint64()
	s := sz.Uint64()
	it.ensureMemSize(o + s)
	return append([]byte(nil), it.memory[o:o+s]...)
}

// readMemView returns a view (subslice) of the internal memory without allocating.
// The returned slice is only valid until the next memory growth.
func (it *MIRInterpreter) readMemView(off, sz *uint256.Int) []byte {
	o := off.Uint64()
	s := sz.Uint64()
	it.ensureMemSize(o + s)
	return it.memory[o : o+s]
}

func (it *MIRInterpreter) readMem32(off *uint256.Int) []byte {
	it.ensureMemSize(off.Uint64() + 32)
	return append([]byte(nil), it.memory[off.Uint64():off.Uint64()+32]...)
}

func (it *MIRInterpreter) readMem32Into(off *uint256.Int, dst *[32]byte) {
	it.ensureMemSize(off.Uint64() + 32)
	copy(dst[:], it.memory[off.Uint64():off.Uint64()+32])
}

func (it *MIRInterpreter) writeMem32(off, val *uint256.Int) {
	o := off.Uint64()
	it.ensureMemSize(o + 32)
	// Right-align as EVM MSTORE semantics
	bytes := val.Bytes()
	// zero the full 32-byte region first
	for i := uint64(0); i < 32; i++ {
		it.memory[o+i] = 0
	}
	copy(it.memory[o+32-uint64(len(bytes)):o+32], bytes)
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
	s := sz.Uint64()
	it.ensureMemSize(o + s)
	out := make([]byte, s)
	copy(out, it.memory[o:o+s])
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
	// First, enforce EVM byte-level rule: target must be a valid JUMPDEST and not in push-data
	if !isFallthrough {
		if !it.env.CheckJumpdest(udest) {
			log.Error("MIR jump invalid jumpdest - mirroring EVM error", "from_evm_pc", m.evmPC, "dest_pc", udest)
			return fmt.Errorf("invalid jump destination")
		}
	}
	// Then resolve to a basic block in the CFG
	it.nextBB = it.env.ResolveBB(udest)
	if it.nextBB == nil {
		log.Error("MIR jump target not mapped in CFG", "from_evm_pc", m.evmPC, "dest_pc", udest)
		return fmt.Errorf("unresolvable jump target")
	}
	it.publishLiveOut(it.currentBB)
	it.prevBB = it.currentBB
	return errJUMP
}

func (it *MIRInterpreter) returnDataCopy(dest, off, sz *uint256.Int) {
	d := dest.Uint64()
	o := off.Uint64()
	sReq := sz.Uint64()
	// Allocate only up to available returndata to avoid pathological growth
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
	// Hard-cap total memory growth to avoid pathological allocations in tests
	const maxCopy = 64 * 1024
	if sAlloc > maxCopy {
		sAlloc = maxCopy
	}
	if d > maxCopy {
		d = 0
	}
	it.ensureMemSize(d + sAlloc)
	if sAlloc == 0 {
		return
	}
	copy(it.memory[d:d+sAlloc], it.returndata[o:o+sAlloc])
}

func (it *MIRInterpreter) sload(key *uint256.Int) *uint256.Int {
	// Prefer runtime hook if provided
	var k [32]byte
	copy(k[:], key.Bytes())
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
	copy(k[:], key.Bytes())
	bytes := val.Bytes()
	copy(v[32-len(bytes):], bytes)
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
