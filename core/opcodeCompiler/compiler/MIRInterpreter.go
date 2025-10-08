package compiler

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

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

	// Call context
	CallValue *uint256.Int

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
	// tracer is invoked on each MIR instruction execution if set
	tracer   func(MirOperation)
	tracerEx func(*MIR)
	// next basic block to transfer to on jumps (set by MirJUMP/MirJUMPI)
	nextBB *MIRBasicBlock
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
		env:        env,
		memory:     env.Memory,
		results:    nil,
		tmpA:       uint256.NewInt(0),
		tmpB:       uint256.NewInt(0),
		tmpC:       uint256.NewInt(0),
		zeroConst:  uint256.NewInt(0),
		constCache: make(map[string]*uint256.Int, 64),
		tracer:     mirGlobalTracer,
		tracerEx:   mirGlobalTracerEx,
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

// SetGlobalMIRTracer sets a process-wide tracer that new MIR interpreters will inherit.
func SetGlobalMIRTracer(cb func(MirOperation)) {
	mirGlobalTracer = cb
}

// SetGlobalMIRTracerExtended sets a process-wide tracer that receives the full MIR.
func SetGlobalMIRTracerExtended(cb func(*MIR)) {
	mirGlobalTracerEx = cb
}

// GetEnv returns the execution environment
func (it *MIRInterpreter) GetEnv() *MIRExecutionEnv {
	return it.env
}

// RunMIR executes all instructions in the given basic block list sequentially.
// For now, control-flow is assumed to be linear within a basic block.
func (it *MIRInterpreter) RunMIR(block *MIRBasicBlock) ([]byte, error) {
	if block == nil || len(block.instructions) == 0 {
		return it.returndata, nil
	}
	// Reuse results backing storage to avoid per-run allocations
	if it.resultsCap < len(block.instructions) {
		it.results = make([]*uint256.Int, len(block.instructions))
		for i := 0; i < len(block.instructions); i++ {
			it.results[i] = new(uint256.Int)
		}
		it.resultsCap = len(block.instructions)
	} else {
		// Ensure slots exist and clear them for reuse
		for i := 0; i < len(block.instructions); i++ {
			if it.results[i] == nil {
				it.results[i] = new(uint256.Int)
			} else {
				it.results[i].Clear()
			}
		}
	}
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
				return nil, err
			}
		}
	}

	// 添加基本块完成执行的日志
	log.Warn("MIRInterpreter: Block execution completed", "blockNum", block.blockNum, "returnDataSize", len(it.returndata), "instructions", block.instructions)
	return it.returndata, nil
}

// RunCFGWithResolver sets up a resolver backed by the given CFG and runs from entry block
func (it *MIRInterpreter) RunCFGWithResolver(cfg *CFG, entry *MIRBasicBlock) ([]byte, error) {
	if it.env != nil && it.env.ResolveBB == nil && cfg != nil {
		// Build a lightweight resolver using cfg.pcToBlock
		it.env.ResolveBB = func(pc uint64) *MIRBasicBlock {
			if cfg == nil || cfg.pcToBlock == nil {
				return nil
			}
			if bb, ok := cfg.pcToBlock[uint(pc)]; ok {
				return bb
			}
			return nil
		}
	}
	// Follow control flow starting at entry; loop jumping between blocks
	bb := entry
	for bb != nil {
		it.nextBB = nil
		_, err := it.RunMIR(bb)
		if err == nil {
			if it.nextBB == nil {
				// Fall through: if this block has exactly one child, continue into it
				children := bb.Children()
				if len(children) == 1 && children[0] != nil {
					// Guard against self-loop fallthrough to avoid infinite loop
					if children[0] == bb {
						return it.returndata, nil
					}
					bb = children[0]
					continue
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
)

func (it *MIRInterpreter) exec(m *MIR) error {
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
		// Evaluate first available incoming as a simple strategy; more advanced selection
		// would consider predecessor edge, but adapter runs single blocks only for now.
		if len(m.oprands) == 0 {
			it.setResult(m, it.zeroConst)
			return nil
		}
		v := it.evalValue(m.oprands[0])
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
		it.setResult(m, uint256.NewInt(it.env.BaseFee))
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

	// Hashing (placeholder: full keccak over memory slice)
	case MirKECCAK256:
		if len(m.oprands) < 2 {
			return fmt.Errorf("KECCAK256 missing operands")
		}
		off := it.evalValue(m.oprands[0])
		sz := it.evalValue(m.oprands[1])
		b := it.readMem(off, sz)
		h := crypto.Keccak256(b)
		it.setResult(m, it.tmpA.Clear().SetBytes(h))
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

	// Control flow (handled at CFG/adapter level). Treat as no-ops here.
	case MirJUMP:
		// If env can validate/resolve jumpdest, do it here to mirror EVM behavior
		if it.env != nil && it.env.CheckJumpdest != nil && it.env.ResolveBB != nil {
			if len(m.oprands) < 1 {
				return fmt.Errorf("JUMP missing destination")
			}
			dest := it.evalValue(m.oprands[0])
			udest, _ := dest.Uint64WithOverflow()
			if !it.env.CheckJumpdest(udest) {
				return fmt.Errorf("invalid jump destination")
			}
			// Resolve and schedule transfer
			it.nextBB = it.env.ResolveBB(udest)
			if it.nextBB == nil {
				return fmt.Errorf("unresolvable jump target")
			}
			return errJUMP
		}
		return nil
	case MirJUMPI:
		// If env can validate/resolve jumpdest, do it here only when condition != 0
		if it.env != nil && it.env.CheckJumpdest != nil && it.env.ResolveBB != nil {
			if len(m.oprands) < 2 {
				return fmt.Errorf("JUMPI missing operands")
			}
			dest := it.evalValue(m.oprands[0])
			cond := it.evalValue(m.oprands[1])
			if !cond.IsZero() {
				udest, _ := dest.Uint64WithOverflow()
				if !it.env.CheckJumpdest(udest) {
					return fmt.Errorf("invalid jump destination")
				}
				it.nextBB = it.env.ResolveBB(udest)
				if it.nextBB == nil {
					return fmt.Errorf("unresolvable jump target")
				}
				return errJUMP
			}
		}
		return nil
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
		return fmt.Errorf("MIR op not implemented: EXTCODEHASH")
	case MirCREATE2:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: CREATE2")
		}
		return fmt.Errorf("MIR op not implemented: CREATE2")

	// Stack ops: DUPn and SWAPn
	case MirDUP1, MirDUP2, MirDUP3, MirDUP4, MirDUP5, MirDUP6, MirDUP7, MirDUP8,
		MirDUP9, MirDUP10, MirDUP11, MirDUP12, MirDUP13, MirDUP14, MirDUP15, MirDUP16:
		// For MIR, DUP has a single operand pointing to the value to duplicate
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

// mirHandlePHI sets the result to the first available incoming value.
func mirHandlePHI(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) == 0 {
		it.setResult(m, it.zeroConst)
		return nil
	}
	v := it.evalValue(m.oprands[0])
	it.setResult(m, v)
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
	// Use pre-encoded operand info if available
	if len(m.opKinds) >= 2 {
		// first operand
		switch m.opKinds[0] {
		case 0:
			a = m.opConst[0]
		case 1:
			idx := m.opDefIdx[0]
			if idx >= 0 && idx < len(it.results) {
				a = it.results[idx]
			}
		}
		if a == nil {
			a = it.evalValue(m.oprands[0])
		}
		// second operand
		switch m.opKinds[1] {
		case 0:
			b = m.opConst[1]
		case 1:
			idx := m.opDefIdx[1]
			if idx >= 0 && idx < len(it.results) {
				b = it.results[idx]
			}
		}
		if b == nil {
			b = it.evalValue(m.oprands[1])
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
	if err != nil {
		return err
	}
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
	it.setResult(m, it.tmpA.Clear().Lsh(a, uint(b.Uint64())))
	return nil
}
func mirHandleSHR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().Rsh(a, uint(b.Uint64())))
	return nil
}
func mirHandleSAR(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	it.setResult(m, it.tmpA.Clear().SRsh(a, uint(b.Uint64())))
	return nil
}
func mirHandleEQ(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
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
	if err != nil {
		return err
	}
	if a.Lt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}
func mirHandleGT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
	if a.Gt(b) {
		it.setResult(m, it.tmpA.Clear().SetOne())
	} else {
		it.setResult(m, it.zeroConst)
	}
	return nil
}
func mirHandleSLT(it *MIRInterpreter, m *MIR) error {
	a, b, err := mirLoadAB(it, m)
	if err != nil {
		return err
	}
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
	it.setResult(m, it.tmpA.Clear().Exp(a, b))
	return nil
}

func mirHandleKECCAK(it *MIRInterpreter, m *MIR) error {
	if len(m.oprands) < 2 {
		return fmt.Errorf("KECCAK256 missing operands")
	}
	// Some builders may emit [size, offset]; ensure we use (offset,size)
	var off, sz *uint256.Int
	// Heuristic: treat common patterns and prefer (offset,size)
	aval := it.evalValue(m.oprands[0])
	bval := it.evalValue(m.oprands[1])
	// If first looks like size (e.g., 32) and second small (e.g., 0), flip
	if (aval.Uint64() == 32 && bval.Uint64() < 32) || (aval.Uint64() != 0 && bval.Uint64() == 0) {
		off, sz = bval, aval
	} else {
		off, sz = aval, bval
	}
	bytesToHash := it.readMemView(off, sz)
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
	it.setResult(m, a.Byte(b))
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
		out = it.tmpA.Clear().Exp(a, b)
	case MirLT:
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
		out = a.Byte(b)
	case MirSHL:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SHL")
		}
		out = it.tmpA.Clear().Lsh(a, uint(b.Uint64()))
	case MirSHR:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SHR")
		}
		out = it.tmpA.Clear().Rsh(a, uint(b.Uint64()))
	case MirSAR:
		if !it.env.IsConstantinople {
			return fmt.Errorf("invalid opcode: SAR")
		}
		out = it.tmpA.Clear().SRsh(a, uint(b.Uint64()))
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
		if v.def != nil && v.def.idx >= 0 && v.def.idx < len(it.results) {
			if r := it.results[v.def.idx]; r != nil {
				return r
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
	if m.idx >= 0 && m.idx < len(it.results) {
		if it.results[m.idx] == nil {
			it.results[m.idx] = new(uint256.Int)
		}
		it.results[m.idx].Set(val)
	}
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

func (it *MIRInterpreter) returnDataCopy(dest, off, sz *uint256.Int) {
	d := dest.Uint64()
	o := off.Uint64()
	s := sz.Uint64()
	end := o + s
	it.ensureMemSize(d + s)
	if o >= uint64(len(it.returndata)) {
		for i := uint64(0); i < s; i++ {
			it.memory[d+i] = 0
		}
		return
	}
	if end > uint64(len(it.returndata)) {
		copy(it.memory[d:], it.returndata[o:])
		for i := uint64(len(it.returndata)) - o; i < s; i++ {
			it.memory[d+i] = 0
		}
		return
	}
	copy(it.memory[d:d+s], it.returndata[o:end])
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
