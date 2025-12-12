package runtime_test

import (
	"fmt"
	"math/big"
	"testing"

	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Simple arithmetic bytecode: PUSH1 1; PUSH1 2; ADD; PUSH1 3; MUL; STOP
var simpleAddMul = []byte{byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.PUSH1), 0x03, byte(compiler.MUL), byte(compiler.STOP)}

// Compute 1+2, then *3, then return the 32-byte value (9)
var addMulReturn = []byte{
	byte(compiler.PUSH1), 0x01,
	byte(compiler.PUSH1), 0x02,
	byte(compiler.ADD),
	byte(compiler.PUSH1), 0x03,
	byte(compiler.MUL),
	// store at memory[0x00..0x20]
	byte(compiler.PUSH1), 0x00,
	byte(compiler.MSTORE),
	// return 32 bytes from 0x00
	byte(compiler.PUSH1), 0x20,
	byte(compiler.PUSH1), 0x00,
	byte(compiler.RETURN),
}

// Storage write/read and return: SSTORE 0x00 <- 0x01; SLOAD 0x00; RETURN 32 bytes
var storageStoreLoadReturn = []byte{
	byte(compiler.PUSH1), 0x00, // key
	byte(compiler.PUSH1), 0x01, // value
	byte(compiler.SSTORE),
	byte(compiler.PUSH1), 0x00, // key
	byte(compiler.SLOAD),
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.MSTORE),
	byte(compiler.PUSH1), 0x20, // size
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.RETURN),
}

// Keccak over memory[0..32] with constant 0x2a; return the 32-byte hash
var keccakMemReturn = []byte{
	byte(compiler.PUSH1), 0x2a, // value
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.MSTORE),
	byte(compiler.PUSH1), 0x20, // size
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.KECCAK256),
	byte(compiler.PUSH1), 0x00, // store hash at 0
	byte(compiler.MSTORE),
	byte(compiler.PUSH1), 0x20,
	byte(compiler.PUSH1), 0x00,
	byte(compiler.RETURN),
}

// Copy calldata to memory, keccak it, return 32-byte hash
var calldataKeccakReturn = []byte{
	byte(compiler.PUSH1), 0x00, // dest
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.CALLDATASIZE),
	byte(compiler.SWAP2), // reorder to [dest(top), offset, size]
	byte(compiler.CALLDATACOPY),
	byte(compiler.CALLDATASIZE), // size
	byte(compiler.PUSH1), 0x00,  // offset
	byte(compiler.KECCAK256),
	byte(compiler.PUSH1), 0x00, // store at 0
	byte(compiler.MSTORE),
	byte(compiler.PUSH1), 0x20,
	byte(compiler.PUSH1), 0x00,
	byte(compiler.RETURN),
}

func BenchmarkMIRVsEVM_AddMul(b *testing.B) {
	// Base EVM interpreter
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
		},
	}

	// MIR path: enable opcode optimizations and MIR opcode parsing
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
			EnableMIR:                 true,
		},
	}

	b.Run("EVM_Base", func(b *testing.B) {
		// Fresh StateDB
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		// Build a proper env with State
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract"))
		sender := cfgBase.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, simpleAddMul)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Execute the contract
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				// ignore reverts; simple code should not revert
				continue
			}
		}
	})

	b.Run("MIR_Interpreter", func(b *testing.B) {
		// Ensure MIR opcode parsing is enabled so MIR CFG is generated
		compiler.EnableOpcodeParse()
		// Ensure a fresh StateDB exists
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		// Fresh env with optimizations enabled
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract"))
		sender := cfgMIR.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, simpleAddMul)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				continue
			}
		}
	})
}

func BenchmarkMIRVsEVM_AddMulReturn(b *testing.B) {
	// Base EVM interpreter
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	// MIR path
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	b.Run("EVM_Base_Return", func(b *testing.B) {
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract"))
		sender := cfgBase.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, addMulReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				b.Fatalf("base call err: %v", err)
			}
		}
	})

	b.Run("MIR_Interpreter_Return", func(b *testing.B) {
		compiler.EnableOpcodeParse()
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract"))
		sender := cfgMIR.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, addMulReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				b.Fatalf("mir call err: %v", err)
			}
		}
	})
}

func BenchmarkMIRVsEVM_Storage(b *testing.B) {
	cfgBase := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{}}
	cfgMIR := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

	b.Run("EVM_Base_Storage", func(b *testing.B) {
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract_storage"))
		sender := cfgBase.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, storageStoreLoadReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				b.Fatalf("base storage err: %v", err)
			}
		}
	})

	b.Run("MIR_Interpreter_Storage", func(b *testing.B) {
		compiler.EnableOpcodeParse()
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract_storage"))
		sender := cfgMIR.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, storageStoreLoadReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				b.Fatalf("mir storage err: %v", err)
			}
		}
	})
}

func BenchmarkMIRVsEVM_Keccak(b *testing.B) {
	cfgBase := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{}}
	cfgMIR := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

	b.Run("EVM_Base_Keccak", func(b *testing.B) {
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract_keccak"))
		sender := cfgBase.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, keccakMemReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				b.Fatalf("base keccak err: %v", err)
			}
		}
	})

	b.Run("MIR_Interpreter_Keccak", func(b *testing.B) {
		compiler.EnableOpcodeParse()
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract_keccak"))
		sender := cfgMIR.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, keccakMemReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				b.Fatalf("mir keccak err: %v", err)
			}
		}
	})
}

func BenchmarkMIRVsEVM_CalldataKeccak(b *testing.B) {
	cfgBase := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{}}
	cfgMIR := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}
	input := make([]byte, 96)
	for i := range input {
		input[i] = byte(i)
	}

	b.Run("EVM_Base_CalldataKeccak", func(b *testing.B) {
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract_calldata"))
		sender := cfgBase.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, calldataKeccakReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, input, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				b.Fatalf("base calldata err: %v", err)
			}
		}
	})

	b.Run("MIR_Interpreter_CalldataKeccak", func(b *testing.B) {
		compiler.EnableOpcodeParse()
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract_calldata"))
		sender := cfgMIR.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, calldataKeccakReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, input, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				b.Fatalf("mir calldata err: %v", err)
			}
		}
	})
}

func TestMIRVsEVM_Functional(t *testing.T) {
	// Base and MIR configs
	base := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{}}
	mir := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}
	compiler.EnableOpcodeParse()

	// helper to run code and return output
	run := func(cfg *runtime.Config, code []byte, input []byte, addrLabel string) ([]byte, error) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		address := common.BytesToAddress([]byte(addrLabel))
		sender := cfg.Origin
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, code)
		ret, _, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		return ret, err
	}

	// cases: addMulReturn, storageStoreLoadReturn, keccakMemReturn, calldataKeccakReturn
	t.Run("addMulReturn", func(t *testing.T) {
		rb, err := run(base, addMulReturn, nil, "addr_am")
		if err != nil {
			t.Fatalf("base err: %v", err)
		}
		rm, err := run(mir, addMulReturn, nil, "addr_am")
		if err != nil {
			t.Fatalf("mir err: %v", err)
		}
		if string(rb) != string(rm) {
			t.Fatalf("mismatch: base %x mir %x", rb, rm)
		}
	})

	t.Run("storage", func(t *testing.T) {
		rb, err := run(base, storageStoreLoadReturn, nil, "addr_st")
		if err != nil {
			t.Fatalf("base err: %v", err)
		}
		rm, err := run(mir, storageStoreLoadReturn, nil, "addr_st")
		if err != nil {
			t.Fatalf("mir err: %v", err)
		}
		if string(rb) != string(rm) {
			t.Fatalf("mismatch: base %x mir %x", rb, rm)
		}
	})

	t.Run("keccak", func(t *testing.T) {
		rb, err := run(base, keccakMemReturn, nil, "addr_km")
		if err != nil {
			t.Fatalf("base err: %v", err)
		}
		rm, err := run(mir, keccakMemReturn, nil, "addr_km")
		if err != nil {
			t.Fatalf("mir err: %v", err)
		}
		if string(rb) != string(rm) {
			t.Fatalf("mismatch: base %x mir %x", rb, rm)
		}
	})

	t.Run("calldata_keccak", func(t *testing.T) {
		input := make([]byte, 96)
		for i := range input {
			input[i] = byte(i)
		}
		// capture MIR keccak input for debugging and assert MIR executed
		var mirKeccakSlice []byte
		var lastCopy [3]uint64
		compiler.SetMIRCalldataCopyDebugHook(func(dest, off, size uint64) { lastCopy = [3]uint64{dest, off, size} })
		sawMIR := false
		compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
			if m != nil {
				sawMIR = true
			}
		})
		compiler.SetMIRKeccakDebugHook(func(off, size uint64, data []byte) {
			// Only capture the first one
			if mirKeccakSlice == nil {
				mirKeccakSlice = append([]byte(nil), data...)
			}
		})
		// Ensure MIR CFG is regenerated for this bytecode
		compiler.DeleteMIRCFGCache(crypto.Keccak256Hash(calldataKeccakReturn))
		// Attach base tracer to capture KECCAK inputs
		var baseOff, baseSize uint64
		var baseSlice []byte
		var baseHash []byte
		// capture last 32 opcode events with stack top snapshot
		var events []string
		var baseCallInputLen int
		var lastWasKeccak bool
		baseTrace := &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			if vm.OpCode(op) == vm.KECCAK256 {
				stack := scope.StackData()
				if len(stack) >= 2 {
					// Pre-op: top-of-stack is offset, next is size
					off := stack[len(stack)-1].Uint64()
					size := stack[len(stack)-2].Uint64()
					baseOff, baseSize = off, size
					lastWasKeccak = true
					if ci := scope.CallInput(); ci != nil {
						baseCallInputLen = len(ci)
					}
				}
			} else if lastWasKeccak {
				// One step after KECCAK, memory should be expanded; capture now
				mem := scope.MemoryData()
				if baseOff+baseSize <= uint64(len(mem)) {
					baseSlice = append([]byte(nil), mem[baseOff:baseOff+baseSize]...)
					baseHash = crypto.Keccak256(baseSlice)
				}
				lastWasKeccak = false
			}
			// stack snapshot (top 4)
			sd := scope.StackData()
			top := ""
			for i := 0; i < 4 && i < len(sd); i++ {
				v := sd[len(sd)-1-i]
				top += fmt.Sprintf("[%d]=%s ", i, v.String())
			}
			ev := fmt.Sprintf("pc=%d op=%s depth=%d top=%s", pc, vm.OpCode(op).String(), depth, top)
			events = append(events, ev)
			if len(events) > 32 {
				events = events[len(events)-32:]
			}
		}}
		// Recreate env with tracer
		baseTr := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false, Tracer: baseTrace}}
		baseTr.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		evmBase := runtime.NewEnv(baseTr)
		addrBase := common.BytesToAddress([]byte("addr_ck_b"))
		sender := baseTr.Origin
		evmBase.StateDB.CreateAccount(addrBase)
		evmBase.StateDB.SetCode(addrBase, calldataKeccakReturn)
		rb, _, err := evmBase.Call(sender, addrBase, input, baseTr.GasLimit, uint256.MustFromBig(baseTr.Value))
		if err != nil {
			t.Fatalf("base err: %v", err)
		}
		rm, err := run(mir, calldataKeccakReturn, input, "addr_ck")
		if err != nil {
			t.Fatalf("mir err: %v", err)
		}
		// If mismatch, log the MIR keccak input to help diagnose
		if !sawMIR {
			t.Fatalf("MIR path not executed for calldata_keccak")
		}
		exp := crypto.Keccak256(input)
		if string(rb) != string(exp) || string(rm) != string(exp) {
			t.Fatalf("unexpected: base %x mir %x exp %x (base_off=%d base_size=%d base_hash=%x base_slice=%x base_callinput_len=%d | last_copy dest=%d off=%d size=%d mir_keccak_input=%x)\nbase last ops:\n%s", rb, rm, exp, baseOff, baseSize, baseHash, baseSlice, baseCallInputLen, lastCopy[0], lastCopy[1], lastCopy[2], mirKeccakSlice, strings.Join(events, "\n"))
		}
		compiler.SetGlobalMIRTracerExtended(nil)
		compiler.SetMIRKeccakDebugHook(nil)
		compiler.SetMIRCalldataCopyDebugHook(nil)
	})
}

func TestAddMulReturn_BaseAndMIR(t *testing.T) {
	if cfg, err := compiler.GenerateMIRCFG(common.Hash{}, addMulReturn); err == nil && cfg != nil {
		ops := make([]byte, 0)
		for _, bb := range cfg.GetBasicBlocks() {
			for _, mir := range bb.Instructions() {
				if mir != nil {
					ops = append(ops, byte(mir.Op()))
				}
			}
		}
		t.Logf("MIR ops addMulReturn: % x", ops)
	}
	// Base
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	if cfgBase.State == nil {
		cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evm := runtime.NewEnv(cfgBase)
	addr := common.BytesToAddress([]byte("contract"))
	sender := cfgBase.Origin
	evm.StateDB.CreateAccount(addr)
	evm.StateDB.SetCode(addr, addMulReturn)
	ret, _, err := evm.Call(sender, addr, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
	if err != nil {
		t.Fatalf("base call err: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("unexpected ret len %d", len(ret))
	}
	got := uint256.NewInt(0).SetBytes(ret)
	if !got.Eq(uint256.NewInt(9)) {
		t.Fatalf("base expected 9, got %s", got.String())
	}

	// MIR
	compiler.EnableOpcodeParse()
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}
	if cfgMIR.State == nil {
		cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evm2 := runtime.NewEnv(cfgMIR)
	evm2.StateDB.CreateAccount(addr)
	evm2.StateDB.SetCode(addr, addMulReturn)
	ret2, _, err2 := evm2.Call(sender, addr, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
	if err2 != nil {
		t.Fatalf("mir call err: %v", err2)
	}
	if len(ret2) != 32 {
		t.Fatalf("unexpected ret len (mir) %d", len(ret2))
	}
	got2 := uint256.NewInt(0).SetBytes(ret2)
	if !got2.Eq(uint256.NewInt(9)) {
		t.Fatalf("mir expected 9, got %s", got2.String())
	}
}
