package runtime_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"os"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestMIRParity_USDT_Basic(t *testing.T) {
	if os.Getenv("MIR_PARITY_REAL") != "1" {
		t.Skip("Skipping real-contract parity (set MIR_PARITY_REAL=1 to enable)")
	}
	// Decode USDT bytecode from benchmarks
	realCode, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}

	// Methods to test (keep small for speed)
	zeroAddress := make([]byte, 32)
	methods := []struct {
		name     string
		selector []byte
		args     [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zeroAddress, zeroAddress}},
	}

	// Build base and MIR envs
	// Use BSC config at/after London to match benches and opcode availability
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true, EnableMIR: true, EnableMIRInitcode: true}}

	// Prepare states
	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, a := range m.args {
			input = append(input, a...)
		}

		// Optional detailed gas trace for decimals case
		var baseTrace, mirTrace [][3]uint64 // pc, op, gasLeft
		if m.name == "decimals" {
			vm.SetMIRGasProbe(func(pc uint64, op byte, gasLeft uint64) {
				mirTrace = append(mirTrace, [3]uint64{pc, uint64(op), gasLeft})
			})
		}
		// Base EVM run with tracer to capture exit PC
		var baseLastPC uint64
		baseCfg.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseLastPC = pc
			if m.name == "decimals" {
				baseTrace = append(baseTrace, [3]uint64{pc, uint64(op), gas})
			}
		}}
		baseEnv := runtime.NewEnv(baseCfg)
		baseAddr := common.BytesToAddress([]byte("contract_usdt_base"))
		baseSender := vm.AccountRef(baseCfg.Origin)
		baseEnv.StateDB.CreateAccount(baseAddr)
		baseEnv.StateDB.SetCode(baseAddr, realCode)
		baseRet, baseGasLeft, baseErr := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, uint256.NewInt(0))

		// Enable MIR generation only for the MIR run; base env already created
		compiler.EnableOpcodeParse()
		// MIR run; capture last EVM pc via global MIR tracer
		var mirLastPC uint64
		compiler.SetGlobalMIRTracerExtended(func(mm *compiler.MIR) {
			if mm != nil {
				mirLastPC = uint64(mm.EvmPC())
			}
		})
		mirEnv := runtime.NewEnv(mirCfg)
		mirAddr := common.BytesToAddress([]byte("contract_usdt_mir"))
		mirSender := vm.AccountRef(mirCfg.Origin)
		mirEnv.StateDB.CreateAccount(mirAddr)
		mirEnv.StateDB.SetCode(mirAddr, realCode)
		mirRet, mirGasLeft, mirErr := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, uint256.NewInt(0))
		// If both errored, compare error strings and skip remainder
		if baseErr != nil || mirErr != nil {
			if (baseErr == nil) != (mirErr == nil) || (baseErr != nil && mirErr != nil && baseErr.Error() != mirErr.Error()) {
				t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
			}
			continue
		}
		// Clear tracer to avoid leaking to other tests
		compiler.SetGlobalMIRTracerExtended(nil)

		if string(baseRet) != string(mirRet) {
			t.Fatalf("ret mismatch for %s\nbase: %x\n mir: %x", m.name, baseRet, mirRet)
		}
		if baseGasLeft != mirGasLeft {
			// If we captured traces, print first divergence to aid debugging
			if len(baseTrace) > 0 && len(mirTrace) > 0 {
				max := len(baseTrace)
				if len(mirTrace) < max {
					max = len(mirTrace)
				}
				for i := 0; i < max; i++ {
					b := baseTrace[i]
					mr := mirTrace[i]
					if b[0] != mr[0] || b[2] != mr[2] || b[1] != mr[1] {
						t.Fatalf("gas diverged at step %d pc base=%d mir=%d op base=0x%x mir=0x%x gas base=%d mir=%d", i, b[0], mr[0], b[1], mr[1], b[2], mr[2])
					}
				}
			}
			t.Fatalf("gas mismatch for %s: base %d != mir %d", m.name, baseGasLeft, mirGasLeft)
		}
		if baseLastPC != mirLastPC {
			t.Fatalf("exit pc mismatch for %s: base %d != mir %d", m.name, baseLastPC, mirLastPC)
		}
	}
}

func TestMIRParity_WBNB_Basic(t *testing.T) {
	if os.Getenv("MIR_PARITY_REAL") != "1" {
		t.Skip("Skipping real-contract parity (set MIR_PARITY_REAL=1 to enable)")
	}
	// Decode WBNB bytecode from benchmarks
	code, err := hex.DecodeString(wbnbHex[2:])
	if err != nil {
		t.Fatalf("decode WBNB hex: %v", err)
	}

	zeroAddress := make([]byte, 32)
	methods := []struct {
		name     string
		selector []byte
		args     [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true, EnableMIR: true, EnableMIRInitcode: true}}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, a := range m.args {
			input = append(input, a...)
		}

		// Base EVM run capturing last PC
		var baseLastPC uint64
		baseCfg.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseLastPC = pc
		}}
		baseEnv := runtime.NewEnv(baseCfg)
		baseAddr := common.BytesToAddress([]byte("contract_wbnb_base"))
		baseSender := vm.AccountRef(baseCfg.Origin)
		baseEnv.StateDB.CreateAccount(baseAddr)
		baseEnv.StateDB.SetCode(baseAddr, code)
		baseRet, baseGasLeft, baseErr := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, uint256.NewInt(0))

		// Enable MIR parsing now for MIR run only
		compiler.EnableOpcodeParse()
		// MIR run capturing last EVM pc
		var mirLastPC uint64
		compiler.SetGlobalMIRTracerExtended(func(mm *compiler.MIR) {
			if mm != nil {
				mirLastPC = uint64(mm.EvmPC())
			}
		})
		mirEnv := runtime.NewEnv(mirCfg)
		mirAddr := common.BytesToAddress([]byte("contract_wbnb_mir"))
		mirSender := vm.AccountRef(mirCfg.Origin)
		mirEnv.StateDB.CreateAccount(mirAddr)
		mirEnv.StateDB.SetCode(mirAddr, code)
		mirRet, mirGasLeft, mirErr := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, uint256.NewInt(0))
		if baseErr != nil || mirErr != nil {
			if (baseErr == nil) != (mirErr == nil) || (baseErr != nil && mirErr != nil && baseErr.Error() != mirErr.Error()) {
				t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
			}
			continue
		}
		compiler.SetGlobalMIRTracerExtended(nil)

		if string(baseRet) != string(mirRet) {
			t.Fatalf("ret mismatch for %s\nbase: %x\n mir: %x", m.name, baseRet, mirRet)
		}
		if baseGasLeft != mirGasLeft {
			t.Fatalf("gas mismatch for %s: base %d != mir %d", m.name, baseGasLeft, mirGasLeft)
		}
		if baseLastPC != mirLastPC {
			t.Fatalf("exit pc mismatch for %s: base %d != mir %d", m.name, baseLastPC, mirLastPC)
		}
	}
}

func TestMIRParity_Tiny(t *testing.T) {
	// Tiny runtime: return 32 bytes with 0x01 at the end
	// 0x60 0x01 0x60 0x00 0x52 0x60 0x20 0x60 0x00 0xF3
	code, _ := hex.DecodeString("600160005260206000f3")
	input := []byte{}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 1_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 1_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true, EnableMIR: true, EnableMIRInitcode: true}}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	// Base
	baseEnv := runtime.NewEnv(baseCfg)
	baseAddr := common.BytesToAddress([]byte("tiny_base"))
	baseSender := vm.AccountRef(baseCfg.Origin)
	baseEnv.StateDB.CreateAccount(baseAddr)
	baseEnv.StateDB.SetCode(baseAddr, code)
	baseRet, baseGasLeft, baseErr := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, uint256.NewInt(0))
	if baseErr != nil {
		t.Fatalf("base error: %v", baseErr)
	}

	// MIR
	compiler.EnableOpcodeParse()
	mirEnv := runtime.NewEnv(mirCfg)
	mirAddr := common.BytesToAddress([]byte("tiny_mir"))
	mirSender := vm.AccountRef(mirCfg.Origin)
	mirEnv.StateDB.CreateAccount(mirAddr)
	mirEnv.StateDB.SetCode(mirAddr, code)
	mirRet, mirGasLeft, mirErr := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, uint256.NewInt(0))
	if mirErr != nil {
		t.Fatalf("mir error: %v", mirErr)
	}
	if string(baseRet) != string(mirRet) {
		t.Fatalf("ret mismatch base=%x mir=%x", baseRet, mirRet)
	}
	if baseGasLeft != mirGasLeft {
		t.Fatalf("gas mismatch base=%d mir=%d", baseGasLeft, mirGasLeft)
	}
}

// TestMIRGasTrace_USDT_Decimals collects per-op gas traces for EVM and MIR for the USDT decimals selector
func TestMIRGasTrace_USDT_Decimals(t *testing.T) {
	if os.Getenv("MIR_PARITY_REAL") != "1" {
		t.Skip("Skipping real-contract gas trace (set MIR_PARITY_REAL=1 to enable)")
	}
	// Decode USDT bytecode
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true, EnableMIR: true, EnableMIRInitcode: true}}

	// Input: decimals()
	input := []byte{0x31, 0x3c, 0xe5, 0x67}

	// EVM gas trace: record gas after each opcode
	type entry struct {
		pc  uint64
		op  byte
		gas uint64
	}
	evmTrace := make([]entry, 0, 2048)
	baseCfg.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, _ tracing.OpContext, _ []byte, _ int, _ error) {
		after := gas - cost
		evmTrace = append(evmTrace, entry{pc: pc, op: op, gas: after})
	}}

	// MIR gas trace: use probe installed by adapter
	mirTrace := make([]entry, 0, 2048)
	vm.SetMIRGasProbe(func(pc uint64, op byte, gasLeft uint64) {
		mirTrace = append(mirTrace, entry{pc: pc, op: op, gas: gasLeft})
	})
	defer vm.SetMIRGasProbe(nil)

	// Run both
	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	// Base
	baseEnv := runtime.NewEnv(baseCfg)
	baseAddr := common.BytesToAddress([]byte("usdt_decimals_base"))
	baseSender := vm.AccountRef(baseCfg.Origin)
	baseEnv.StateDB.CreateAccount(baseAddr)
	baseEnv.StateDB.SetCode(baseAddr, code)
	_, baseGasLeft, _ := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, uint256.NewInt(0))

	// MIR
	compiler.EnableOpcodeParse()
	mirEnv := runtime.NewEnv(mirCfg)
	mirAddr := common.BytesToAddress([]byte("usdt_decimals_mir"))
	mirSender := vm.AccountRef(mirCfg.Origin)
	mirEnv.StateDB.CreateAccount(mirAddr)
	mirEnv.StateDB.SetCode(mirAddr, code)
	_, mirGasLeft, _ := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, uint256.NewInt(0))

	// Quick parity check (expect mismatch currently), then print a concise diff around divergence
	if baseGasLeft != mirGasLeft {
		t.Logf("gasLeft mismatch: base=%d mir=%d (delta=%d)", baseGasLeft, mirGasLeft, int64(baseGasLeft)-int64(mirGasLeft))
	}
	// Build maps by pc to last observed gas for simple comparison
	lastEvm := map[uint64]entry{}
	for _, e := range evmTrace {
		lastEvm[e.pc] = e
	}
	lastMir := map[uint64]entry{}
	for _, e := range mirTrace {
		lastMir[e.pc] = e
	}
	// Identify pcs present in one but not the other or with gas deltas
	type diff struct {
		pc       uint64
		evm, mir uint64
		opE, opM byte
	}
	diffs := make([]diff, 0, 128)
	// union of keys
	seen := map[uint64]struct{}{}
	for pc := range lastEvm {
		seen[pc] = struct{}{}
	}
	for pc := range lastMir {
		seen[pc] = struct{}{}
	}
	for pc := range seen {
		ev := lastEvm[pc]
		mr := lastMir[pc]
		if ev.gas != mr.gas {
			diffs = append(diffs, diff{pc: pc, evm: ev.gas, mir: mr.gas, opE: ev.op, opM: mr.op})
		}
	}
	// Sort by pc for readability
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].pc < diffs[j].pc })
	// Print up to first 30 diffs
	for i := 0; i < len(diffs) && i < 30; i++ {
		d := diffs[i]
		t.Logf("pc=%d evmOp=%s mirOp=%s evmGas=%d mirGas=%d", d.pc, vm.OpCode(d.opE).String(), vm.OpCode(d.opM).String(), d.evm, d.mir)
	}
}
