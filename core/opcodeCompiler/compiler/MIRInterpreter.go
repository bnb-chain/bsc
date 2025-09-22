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

	// Runtime linkage hooks to EVM components (provided by adapter at runtime)
	// If these are nil, interpreter falls back to internal simulated state.
	SLoadFunc      func(key [32]byte) [32]byte
	SStoreFunc     func(key [32]byte, value [32]byte)
	GetBalanceFunc func(addr [20]byte) *uint256.Int

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
	results    map[*MIR]*uint256.Int
}

func NewMIRInterpreter(env *MIRExecutionEnv) *MIRInterpreter {
	if env.Memory == nil {
		env.Memory = make([]byte, 0)
	}
	return &MIRInterpreter{env: env, memory: env.Memory, results: make(map[*MIR]*uint256.Int)}
}

// GetEnv returns the execution environment
func (it *MIRInterpreter) GetEnv() *MIRExecutionEnv {
	return it.env
}

// RunMIR executes all instructions in the given basic block list sequentially.
// For now, control-flow is assumed to be linear within a basic block.
func (it *MIRInterpreter) RunMIR(block *MIRBasicBlock) ([]byte, error) {
	for block.pos = 0; ; {
		ins := block.GetNextOp()
		if ins == nil {
			break
		}
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
	log.Debug("MIRInterpreter: Block execution completed", "blockNum", block.blockNum, "returnDataSize", len(it.returndata), "instructions", block.instructions)
	return it.returndata, nil
}

var (
	errSTOP   = errors.New("STOP")
	errRETURN = errors.New("RETURN")
	errREVERT = errors.New("REVERT")
)

func (it *MIRInterpreter) exec(m *MIR) error {
	switch m.op {
	case MirNOP:
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
			it.setResult(m, uint256.NewInt(0))
		} else {
			temp := uint256.NewInt(0).Add(val1, val2)
			result := temp.Mod(temp, val3)
			it.setResult(m, result)
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
			it.setResult(m, uint256.NewInt(0))
		} else {
			temp := uint256.NewInt(0).Mul(val1, val2)
			result := temp.Mod(temp, val3)
			it.setResult(m, result)
		}
		return nil

	case MirISZERO:
		if len(m.oprands) < 1 {
			return fmt.Errorf("ISZERO missing operand")
		}
		v1 := it.evalValue(m.oprands[0])
		if v1.IsZero() {
			it.setResult(m, uint256.NewInt(1))
		} else {
			it.setResult(m, uint256.NewInt(0))
		}
		return nil
	case MirNOT:
		if len(m.oprands) < 1 {
			return fmt.Errorf("NOT missing operand")
		}
		v1 := it.evalValue(m.oprands[0])
		it.setResult(m, uint256.NewInt(0).Not(v1))
		return nil

	// Memory
	case MirMLOAD:
		// operands: offset, size(ignored; assume 32)
		if len(m.oprands) < 2 {
			return fmt.Errorf("MLOAD missing operands")
		}
		off := it.evalValue(m.oprands[0])
		// size := it.evalValue(m.oprands[1]) // 32
		b := it.readMem32(off)
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
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
		b := make([]byte, 32)
		copy(b[12:], it.env.Self[:])
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
		return nil
	case MirCALLER:
		b := make([]byte, 32)
		copy(b[12:], it.env.Caller[:])
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
		return nil
	case MirORIGIN:
		b := make([]byte, 32)
		copy(b[12:], it.env.Origin[:])
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
		return nil
	case MirGASPRICE:
		it.setResult(m, uint256.NewInt(it.env.GasPrice))
		return nil
	case MirSELFBALANCE:
		if it.env.GetBalanceFunc != nil {
			it.setResult(m, it.env.GetBalanceFunc(it.env.Self))
		} else {
			it.setResult(m, uint256.NewInt(it.env.SelfBalance))
		}
		return nil
	case MirCHAINID:
		it.setResult(m, uint256.NewInt(it.env.ChainID))
		return nil
	case MirTIMESTAMP:
		it.setResult(m, uint256.NewInt(it.env.Timestamp))
		return nil
	case MirNUMBER:
		it.setResult(m, uint256.NewInt(it.env.BlockNumber))
		return nil
	case MirBASEFEE:
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
		b := it.readCalldata32(off)
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
		return nil
	case MirCALLDATASIZE:
		it.setResult(m, uint256.NewInt(uint64(len(it.env.Calldata))))
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
		it.setResult(m, uint256.NewInt(uint64(len(it.returndata))))
		return nil
	case MirRETURNDATALOAD:
		if len(m.oprands) < 1 {
			return fmt.Errorf("RETURNDATALOAD missing offset")
		}
		off := it.evalValue(m.oprands[0])
		b := it.readReturnData32(off)
		it.setResult(m, uint256.NewInt(0).SetBytes(b))
		return nil
	case MirRETURNDATACOPY:
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
		it.setResult(m, uint256.NewInt(0).SetBytes(h))
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
		if len(m.oprands) < 2 {
			return fmt.Errorf("REVERT missing operands")
		}
		off := it.evalValue(m.oprands[0])
		sz := it.evalValue(m.oprands[1])
		it.returndata = append([]byte(nil), it.readMem(off, sz)...)
		return errREVERT
	default:
		// Many ops are not yet implemented for simulation
		return fmt.Errorf("MIR op not implemented: 0x%x", byte(m.op))
	}
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
		out = uint256.NewInt(0).Add(a, b)
	case MirMUL:
		out = uint256.NewInt(0).Mul(a, b)
	case MirSUB:
		out = uint256.NewInt(0).Sub(a, b)
	case MirDIV:
		out = uint256.NewInt(0).Div(a, b)
	case MirSDIV:
		out = uint256.NewInt(0).SDiv(a, b)
	case MirMOD:
		out = uint256.NewInt(0).Mod(a, b)
	case MirSMOD:
		out = uint256.NewInt(0).SMod(a, b)
	case MirEXP:
		out = uint256.NewInt(0).Exp(a, b)
	case MirLT:
		if a.Lt(b) {
			out = uint256.NewInt(1)
		} else {
			out = uint256.NewInt(0)
		}
	case MirGT:
		if a.Gt(b) {
			out = uint256.NewInt(1)
		} else {
			out = uint256.NewInt(0)
		}
	case MirSLT:
		if a.Slt(b) {
			out = uint256.NewInt(1)
		} else {
			out = uint256.NewInt(0)
		}
	case MirSGT:
		if a.Sgt(b) {
			out = uint256.NewInt(1)
		} else {
			out = uint256.NewInt(0)
		}
	case MirEQ:
		if a.Eq(b) {
			out = uint256.NewInt(1)
		} else {
			out = uint256.NewInt(0)
		}
	case MirAND:
		out = uint256.NewInt(0).And(a, b)
	case MirOR:
		out = uint256.NewInt(0).Or(a, b)
	case MirXOR:
		out = uint256.NewInt(0).Xor(a, b)
	case MirBYTE:
		out = a.Byte(b)
	case MirSHL:
		out = uint256.NewInt(0).Lsh(a, uint(b.Uint64()))
	case MirSHR:
		out = uint256.NewInt(0).Rsh(a, uint(b.Uint64()))
	case MirSAR:
		out = uint256.NewInt(0).SRsh(a, uint(b.Uint64()))
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
		return uint256.NewInt(0).SetBytes(v.payload)
	case Variable, Arguments:
		if v.def != nil {
			if r, ok := it.results[v.def]; ok {
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
	it.results[m] = new(uint256.Int).Set(val)
}

func (it *MIRInterpreter) ensureMemSize(size uint64) {
	if uint64(len(it.memory)) < size {
		newMem := make([]byte, size)
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

func (it *MIRInterpreter) readMem32(off *uint256.Int) []byte {
	it.ensureMemSize(off.Uint64() + 32)
	return append([]byte(nil), it.memory[off.Uint64():off.Uint64()+32]...)
}

func (it *MIRInterpreter) writeMem32(off, val *uint256.Int) {
	o := off.Uint64()
	it.ensureMemSize(o + 32)
	bytes := val.Bytes()
	// right-align to 32 bytes
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
		return uint256.NewInt(0).SetBytes(v[:])
	}
	// Fallback to internal simulated map
	if it.env.Storage == nil {
		it.env.Storage = make(map[[32]byte][32]byte)
	}
	val, ok := it.env.Storage[k]
	if !ok {
		return uint256.NewInt(0)
	}
	return uint256.NewInt(0).SetBytes(val[:])
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
