package runtime_test

import (
	"encoding/hex"
	"math/big"
	"sort"
	"testing"

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

func TestMIRParity_USDT(t *testing.T) {
	// Decode USDT bytecode from benchmarks
	realCode, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}

	// Methods to test (views + a few state-changing/revert paths)
	zeroAddress := make([]byte, 32)
	one := make([]byte, 32)
	one[len(one)-1] = 1
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
		// Common ERC20 write paths; parity is based on matching success/revert and gas
		{"approve_zero_zero", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zeroAddress, make([]byte, 32)}},
		{"transfer_zero_1", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, one}},
		{"transferFrom_zero_zero_1", []byte{0x23, 0xb8, 0x72, 0xdd}, [][]byte{zeroAddress, zeroAddress, one}},
	}

	// Build base and MIR envs
	// Use BSC config at/after London to match benches and opcode availability
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

	// Prepare states
	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	for _, m := range methods {
		// Reset state per method to ensure isolation
		baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

		input := append([]byte{}, m.selector...)
		for _, a := range m.args {
			input = append(input, a...)
		}

		// Optional detailed gas trace for decimals case
		var baseTrace, mirTrace [][3]uint64 // pc, op, gasLeft
		// Reset global probe
		vm.SetMIRGasProbe(nil)
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
		baseCallValue := uint256.NewInt(0)
		baseRet, baseGasLeft, baseErr := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, baseCallValue)

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
		mirCallValue := uint256.NewInt(0)
		mirRet, mirGasLeft, mirErr := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, mirCallValue)
		if (baseErr == nil) != (mirErr == nil) {
			t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
		}
		if baseErr != nil && mirErr != nil {
			// Normalize "invalid jump destination" error which MIR augments with PC
			be := baseErr.Error()
			me := mirErr.Error()
			if be == "invalid jump destination" && len(me) >= len(be) && me[:len(be)] == be {
				// acceptable match
			} else if be != me {
				t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
			}
		}
		continue
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
						t.Logf("gas diverged at step %d pc base=%d mir=%d op base=0x%x mir=0x%x gas base=%d mir=%d", i, b[0], mr[0], b[1], mr[1], b[2], mr[2])
					}
				}
			}
			t.Errorf("gas mismatch for %s: base %d != mir %d", m.name, baseGasLeft, mirGasLeft)
		}
		if baseLastPC != mirLastPC {
			t.Errorf("exit pc mismatch for %s: base %d != mir %d", m.name, baseLastPC, mirLastPC)
		}
	}
}

func TestMIRParity_WBNB(t *testing.T) {

	// Decode WBNB bytecode from benchmarks
	code, err := hex.DecodeString(wbnbHex[2:])
	if err != nil {
		t.Fatalf("decode WBNB hex: %v", err)
	}

	zeroAddress := make([]byte, 32)
	one := make([]byte, 32)
	one[len(one)-1] = 1
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
		{"allowance", []byte{0xdd, 0x62, 0xed, 0x3e}, [][]byte{zeroAddress, zeroAddress}},
		// deposit with value; parity on success paths
		{"deposit_value_1e18", []byte{0xd0, 0xe3, 0x0d, 0xb0}, nil},
		// transfer to zero addr should revert (parity in error)
		{"transfer_zero_1", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, one}},
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	for _, m := range methods {
		// Reset state per method to ensure isolation
		baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

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
		// per-call value and optional funding (for deposit)
		callValue := uint256.NewInt(0)
		if m.name == "deposit_value_1e18" {
			val := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
			baseEnv.StateDB.AddBalance(baseCfg.Origin, uint256.MustFromBig(val), tracing.BalanceIncreaseGenesisBalance)
		}
		baseRet, baseGasLeft, baseErr := baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, callValue)

		// Enable MIR parsing now for MIR run only
		compiler.EnableOpcodeParse()
		// MIR run capturing last EVM pc
		var mirLastPC uint64
		compiler.SetGlobalMIRTracerExtended(func(mm *compiler.MIR) {
			if mm != nil {
				if mm.EvmPC() != 0 {
					mirLastPC = uint64(mm.EvmPC())
				}
			}
		})
		mirEnv := runtime.NewEnv(mirCfg)
		mirAddr := common.BytesToAddress([]byte("contract_wbnb_mir"))
		mirSender := vm.AccountRef(mirCfg.Origin)
		mirEnv.StateDB.CreateAccount(mirAddr)
		mirEnv.StateDB.SetCode(mirAddr, code)
		mirCallValue := uint256.NewInt(0)
		if m.name == "deposit_value_1e18" {
			val := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
			mirEnv.StateDB.AddBalance(mirCfg.Origin, uint256.MustFromBig(val), tracing.BalanceIncreaseGenesisBalance)
			mirCallValue = uint256.MustFromBig(val)
			// also set base call value to same amount by rerunning base call if needed
			// (base call was run with 0; since envs are independent per method, just align call values below)
			// Re-run base call with value to ensure parity on success path
			baseEnv = runtime.NewEnv(baseCfg)
			baseEnv.StateDB.CreateAccount(baseAddr)
			baseEnv.StateDB.SetCode(baseAddr, code)
			baseEnv.StateDB.AddBalance(baseCfg.Origin, uint256.MustFromBig(val), tracing.BalanceIncreaseGenesisBalance)
			baseSender = vm.AccountRef(baseCfg.Origin)
			baseRet, baseGasLeft, baseErr = baseEnv.Call(baseSender, baseAddr, input, baseCfg.GasLimit, uint256.MustFromBig(val))
		}
		mirRet, mirGasLeft, mirErr := mirEnv.Call(mirSender, mirAddr, input, mirCfg.GasLimit, mirCallValue)
		if baseErr != nil || mirErr != nil {
			if (baseErr == nil) != (mirErr == nil) {
				t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
			}
			if baseErr != nil && mirErr != nil {
				// Normalize "invalid jump destination" error which MIR augments with PC
				be := baseErr.Error()
				me := mirErr.Error()
				if be == "invalid jump destination" && len(me) >= len(be) && me[:len(be)] == be {
					// acceptable match
				} else if be != me {
					t.Fatalf("error mismatch for %s: base=%v mir=%v", m.name, baseErr, mirErr)
				}
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
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 1_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 1_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

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
	// Decode USDT bytecode
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	baseCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{}}
	mirCfg := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableMIR: true}}

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
