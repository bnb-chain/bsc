package runtime_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
)

// encodePush returns opcode bytes for the minimal PUSH to push the given big-endian value.
func encodePush(b []byte) []byte {
	// trim leading zeros
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	b = b[i:]
	if len(b) == 0 {
		return []byte{byte(vm.PUSH1), 0x00}
	}
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	return append([]byte{byte(vm.PUSH1) + byte(len(b)-1)}, b...)
}

// buildTwoOpProgram builds: PUSH a; PUSH b; <op>; PUSH1 0; MSTORE; PUSH1 32; PUSH1 0; RETURN
func buildTwoOpProgram(a []byte, b []byte, op byte) []byte {
	code := make([]byte, 0, 100)
	code = append(code, encodePush(a)...)
	code = append(code, encodePush(b)...)
	code = append(code, op)
	code = append(code, byte(vm.PUSH1), 0x00) // offset 0
	code = append(code, byte(vm.MSTORE))      // store result at 0
	code = append(code, byte(vm.PUSH1), 0x20) // length 32
	code = append(code, byte(vm.PUSH1), 0x00) // offset 0
	code = append(code, byte(vm.RETURN))      // return memory[0:32]
	return code
}

func runCodeReturn(code []byte, enableMIR bool) ([]byte, error) {
	cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: enableMIR}}
	compiler.EnableOpcodeParse()
	if cfg.State == nil {
		cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evm := runtime.NewEnv(cfg)
	addr := common.BytesToAddress([]byte("mir_semantics"))
	sender := vm.AccountRef(cfg.Origin)
	evm.StateDB.CreateAccount(addr)
	evm.StateDB.SetCode(addr, code)
	ret, _, err := evm.Call(sender, addr, nil, cfg.GasLimit, nil)
	return ret, err
}

// TestMIRMatchesEVM_BinaryOps validates that MIR matches EVM for a selection of two-operand ops
// with emphasis on operand order semantics (especially LT/GT/SLT/SGT/SUB/DIV).
func TestMIRMatchesEVM_BinaryOps(t *testing.T) {
	type pair struct{ a, b string }
	// hex-encoded big-endian inputs
	inputs := []pair{
		{"01", "02"}, // a<b
		{"02", "01"}, // a>b
		{"00", "00"}, // equal zeros
		{"ff", "01"}, // large a vs small b
		{"01", "ff"}, // small a vs large b
		{"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "01"}, // -1 (for signed) vs 1
	}
	// opcodes to test (EVM byte values)
	ops := []struct {
		name string
		op   byte
	}{
		{"ADD", byte(vm.ADD)},
		{"SUB", byte(vm.SUB)},
		{"MUL", byte(vm.MUL)},
		{"DIV", byte(vm.DIV)},
		{"SDIV", byte(vm.SDIV)},
		{"MOD", byte(vm.MOD)},
		{"SMOD", byte(vm.SMOD)},
		{"EXP", byte(vm.EXP)},
		{"LT", byte(vm.LT)},
		{"GT", byte(vm.GT)},
		{"SLT", byte(vm.SLT)},
		{"SGT", byte(vm.SGT)},
		{"EQ", byte(vm.EQ)},
		{"AND", byte(vm.AND)},
		{"OR", byte(vm.OR)},
		{"XOR", byte(vm.XOR)},
		{"SHL", byte(vm.SHL)},
		{"SHR", byte(vm.SHR)},
		{"SAR", byte(vm.SAR)},
	}

	for _, o := range ops {
		for _, in := range inputs {
			aBytes, _ := hex.DecodeString(in.a)
			bBytes, _ := hex.DecodeString(in.b)
			code := buildTwoOpProgram(aBytes, bBytes, o.op)

			// Run base EVM
			rb, errB := runCodeReturn(code, false)
			if errB != nil {
				t.Fatalf("%s: EVM exec error: %v", o.name, errB)
			}
			// Run MIR-backed
			rm, errM := runCodeReturn(code, true)
			if errM != nil {
				t.Fatalf("%s: MIR exec error: %v", o.name, errM)
			}
			if string(rb) != string(rm) {
				t.Fatalf("%s: mismatch for a=%s b=%s\nEVM=%x\nMIR=%x", o.name, in.a, in.b, rb, rm)
			}
		}
	}
}

// buildUnaryProgram builds: PUSH x; <op>; MSTORE/RETURN to fetch 32B result
func buildUnaryProgram(x []byte, op byte) []byte {
	code := make([]byte, 0, 64)
	code = append(code, encodePush(x)...)
	code = append(code, op)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))
	return code
}

func TestMIRMatchesEVM_UnaryOps(t *testing.T) {
	ops := []struct {
		name string
		op   byte
		vals [][]byte
	}{
		{"ISZERO", byte(vm.ISZERO), [][]byte{{}, {0x01}, {0xff}}},
		{"NOT", byte(vm.NOT), [][]byte{{}, {0x01}, {0xff}}},
	}
	for _, o := range ops {
		for _, v := range o.vals {
			code := buildUnaryProgram(v, o.op)
			rb, errB := runCodeReturn(code, false)
			if errB != nil {
				t.Fatalf("%s EVM: %v", o.name, errB)
			}
			rm, errM := runCodeReturn(code, true)
			if errM != nil {
				t.Fatalf("%s MIR: %v", o.name, errM)
			}
			if string(rb) != string(rm) {
				t.Fatalf("%s mismatch for x=%x\nEVM=%x\nMIR=%x", o.name, v, rb, rm)
			}
		}
	}
}

// BYTE test: PUSH value; PUSH th; BYTE; store+return
func TestMIRMatchesEVM_Byte(t *testing.T) {
	build := func(val []byte, th byte) []byte {
		code := make([]byte, 0, 64)
		code = append(code, encodePush(val)...)
		code = append(code, byte(vm.PUSH1), th)
		code = append(code, byte(vm.BYTE))
		code = append(code, byte(vm.PUSH1), 0x00)
		code = append(code, byte(vm.MSTORE))
		code = append(code, byte(vm.PUSH1), 0x20)
		code = append(code, byte(vm.PUSH1), 0x00)
		code = append(code, byte(vm.RETURN))
		return code
	}
	cases := []struct {
		val []byte
		th  byte
	}{
		{[]byte{0x12, 0x34}, 0},
		{[]byte{0x12, 0x34}, 1},
		{[]byte{0xaa}, 31},
		{[]byte{0x00}, 0},
	}
	for _, c := range cases {
		code := build(c.val, c.th)
		rb, errB := runCodeReturn(code, false)
		if errB != nil {
			t.Fatalf("BYTE EVM: %v", errB)
		}
		rm, errM := runCodeReturn(code, true)
		if errM != nil {
			t.Fatalf("BYTE MIR: %v", errM)
		}
		if string(rb) != string(rm) {
			t.Fatalf("BYTE mismatch for val=%x th=%d\nEVM=%x\nMIR=%x", c.val, c.th, rb, rm)
		}
	}
}

func TestMIRMatchesEVM_StackOps(t *testing.T) {
	// Build: PUSH a; PUSH b; PUSH c; <stack-op>; MSTORE; RETURN
	build := func(pushes [][]byte, op byte) []byte {
		code := make([]byte, 0, 128)
		for _, p := range pushes {
			code = append(code, encodePush(p)...)
		}
		code = append(code, op)
		code = append(code, byte(vm.PUSH1), 0x00)
		code = append(code, byte(vm.MSTORE))
		code = append(code, byte(vm.PUSH1), 0x20)
		code = append(code, byte(vm.PUSH1), 0x00)
		code = append(code, byte(vm.RETURN))
		return code
	}
	ops := []byte{byte(vm.DUP1), byte(vm.DUP2), byte(vm.SWAP1), byte(vm.SWAP2)}
	pushes := [][]byte{{0x11}, {0x22}, {0x33}}
	for _, o := range ops {
		code := build(pushes, o)
		rb, errB := runCodeReturn(code, false)
		if errB != nil {
			t.Fatalf("stack op 0x%02x EVM: %v", o, errB)
		}
		rm, errM := runCodeReturn(code, true)
		if errM != nil {
			t.Fatalf("stack op 0x%02x MIR: %v", o, errM)
		}
		if string(rb) != string(rm) {
			t.Fatalf("stack op 0x%02x mismatch\nEVM=%x\nMIR=%x", o, rb, rm)
		}
	}
}

func TestMIRMatchesEVM_MemoryOps(t *testing.T) {
	// Write 32 bytes with MSTORE, read back with MLOAD and return
	code := []byte{}
	val := make([]byte, 32)
	for i := range val {
		val[i] = byte(i)
	}
	code = append(code, encodePush(val)...)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MLOAD))
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))

	rb, errB := runCodeReturn(code, false)
	if errB != nil {
		t.Fatalf("memory EVM: %v", errB)
	}
	rm, errM := runCodeReturn(code, true)
	if errM != nil {
		t.Fatalf("memory MIR: %v", errM)
	}
	if string(rb) != string(rm) {
		t.Fatalf("memory ops mismatch\nEVM=%x\nMIR=%x", rb, rm)
	}
}

func TestMIRMatchesEVM_CalldataSize(t *testing.T) {
	prog := []byte{byte(vm.CALLDATASIZE), byte(vm.PUSH1), 0x00, byte(vm.MSTORE), byte(vm.PUSH1), 0x20, byte(vm.PUSH1), 0x00, byte(vm.RETURN)}
	rb, errB := runCodeReturn(prog, false)
	if errB != nil {
		t.Fatalf("calldatasize EVM: %v", errB)
	}
	rm, errM := runCodeReturn(prog, true)
	if errM != nil {
		t.Fatalf("calldatasize MIR: %v", errM)
	}
	if string(rb) != string(rm) {
		t.Fatalf("calldatasize mismatch EVM=%x MIR=%x", rb, rm)
	}
}
