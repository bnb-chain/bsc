package runtime_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"testing"
	"time"

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

// addMulReturn: (1 + 2) * 3 -> return 32 bytes memory[0:32]
var addMulReturnSimple = []byte{
	byte(compiler.PUSH1), 0x01, // 1
	byte(compiler.PUSH1), 0x02, // 2
	byte(compiler.ADD),         // 3
	byte(compiler.PUSH1), 0x03, // 3
	byte(compiler.MUL),         // 9
	byte(compiler.PUSH1), 0x00, // store at 0
	byte(compiler.MSTORE),
	byte(compiler.PUSH1), 0x20, // size
	byte(compiler.PUSH1), 0x00, // offset
	byte(compiler.RETURN),
}

func TestMIRVsEVM_AddMulReturn_Simple(t *testing.T) {
	// Base EVM config (no MIR)
	base := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
	base.EVMConfig = vm.Config{EnableOpcodeOptimizations: false}
	// MIR config
	mir := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
	mir.EVMConfig = vm.Config{EnableMIR: true}

	// Aggregation helpers
	type agg struct {
		total time.Duration
		count int
	}
	evmAgg := map[byte]agg{}
	var evmLast struct {
		pc   uint64
		op   byte
		t    time.Time
		have bool
	}

	// EVM tracer to time per-opcode execution (time between OnOpcode calls)
	base.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, _ uint64, _ uint64, _ tracing.OpContext, _ []byte, _ int, _ error) {
		now := time.Now()
		if evmLast.have {
			a := evmAgg[evmLast.op]
			a.total += now.Sub(evmLast.t)
			a.count++
			evmAgg[evmLast.op] = a
		}
		evmLast = struct {
			pc   uint64
			op   byte
			t    time.Time
			have bool
		}{pc, op, now, true}
	}}

	// MIR timing via tracer (same method as base). Also record MIR exit PC via global tracer.
	mirAgg := map[byte]agg{}
	var mirLast struct {
		pc   uint64
		op   byte
		t    time.Time
		have bool
	}
	var mirLastPC uint64
	compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
		// Clean up global tracer to prevent test pollution
		defer compiler.SetGlobalMIRTracerExtended(nil)
		if m != nil {
			mirLastPC = uint64(m.EvmPC())
		}
	})
	t.Cleanup(func() {
		// Reset hooks after test
		compiler.SetGlobalMIRTracerExtended(nil)
	})
	// Clean up global tracer to prevent test pollution
	defer compiler.SetGlobalMIRTracerExtended(nil)

	// Helper to run code and return output, gasLeft, last executed pc
	run := func(cfg *runtime.Config, code []byte, addrLabel string) ([]byte, uint64, uint64, error) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		address := common.BytesToAddress([]byte(addrLabel))
		sender := vm.AccountRef(cfg.Origin)
		var lastPC uint64
		// Install per-run tracer to track lastPC and collect per-op timings for MIR runs
		cfg.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, _ uint64, _ uint64, _ tracing.OpContext, _ []byte, _ int, _ error) {
			lastPC = pc
			now := time.Now()
			if cfg.EVMConfig.EnableMIR {
				if mirLast.have {
					a := mirAgg[mirLast.op]
					a.total += now.Sub(mirLast.t)
					a.count++
					mirAgg[mirLast.op] = a
				}
				mirLast = struct {
					pc   uint64
					op   byte
					t    time.Time
					have bool
				}{pc, op, now, true}
			} else {
				if evmLast.have {
					a := evmAgg[evmLast.op]
					a.total += now.Sub(evmLast.t)
					a.count++
					evmAgg[evmLast.op] = a
				}
				evmLast = struct {
					pc   uint64
					op   byte
					t    time.Time
					have bool
				}{pc, op, now, true}
			}
		}}
		evm := runtime.NewEnv(cfg)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, code)
		ret, gasLeft, err := evm.Call(sender, address, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		if lastPC == 0 {
			// fall back to mirLastPC for MIR runs
			lastPC = mirLastPC
		}
		return ret, gasLeft, lastPC, err
	}

	// Run base
	rb, gb, pcb, err := run(base, addMulReturnSimple, "addr_amr_base")
	if err != nil {
		t.Fatalf("base err: %v", err)
	}
	// Flush last opcode duration (end of execution) by simulating a step boundary
	if evmLast.have {
		a := evmAgg[evmLast.op]
		a.total += time.Since(evmLast.t)
		a.count++
		evmAgg[evmLast.op] = a
	}

	// Run MIR
	// Enable opcode parsing for MIR CFG
	compiler.EnableOpcodeParse()
	rm, gm, pcm, err := run(mir, addMulReturnSimple, "addr_amr_mir")
	if err != nil {
		t.Fatalf("mir err: %v", err)
	}
	// Flush last MIR opcode duration
	if mirLast.have {
		a := mirAgg[mirLast.op]
		a.total += time.Since(mirLast.t)
		a.count++
		mirAgg[mirLast.op] = a
	}

	if string(rb) != string(rm) {
		t.Fatalf("return mismatch: base %x mir %x", rb, rm)
	}
	if gb != gm {
		t.Fatalf("gas mismatch: base %d mir %d", gb, gm)
	}
	if pcb != pcm {
		t.Fatalf("exit pc mismatch: base %d mir %d", pcb, pcm)
	}

	// Summaries
	type kvE struct {
		op byte
		a  agg
	}
	var listE []kvE
	for op, a := range evmAgg {
		if a.count > 0 {
			listE = append(listE, kvE{op, a})
		}
	}
	sort.Slice(listE, func(i, j int) bool { return listE[i].a.total > listE[j].a.total })

	type kvM struct {
		op byte
		a  agg
	}
	var listM []kvM
	for op, a := range mirAgg {
		if a.count > 0 {
			listM = append(listM, kvM{op, a})
		}
	}
	sort.Slice(listM, func(i, j int) bool { return listM[i].a.total > listM[j].a.total })

	// Print top hotspots
	top := func(n int, tot time.Duration, hdr string, dump func(int) string) {
		t.Log(hdr)
		for i := 0; i < n; i++ {
			line := dump(i)
			if line == "" {
				break
			}
			t.Log(line)
		}
	}

	var evmTotal time.Duration
	for _, e := range listE {
		evmTotal += e.a.total
	}
	top(10, evmTotal, "EVM opcode hotspots (op total count avg):", func(i int) string {
		if i >= len(listE) {
			return ""
		}
		e := listE[i]
		avg := time.Duration(0)
		if e.a.count > 0 {
			avg = e.a.total / time.Duration(e.a.count)
		}
		return fmt.Sprintf("op=0x%02x total=%s count=%d avg=%s", e.op, e.a.total, e.a.count, avg)
	})

	var mirTotal time.Duration
	for _, m := range listM {
		mirTotal += m.a.total
	}
	top(10, mirTotal, "MIR opcode hotspots (op total count avg):", func(i int) string {
		if i >= len(listM) {
			return ""
		}
		m := listM[i]
		avg := time.Duration(0)
		if m.a.count > 0 {
			avg = m.a.total / time.Duration(m.a.count)
		}
		return fmt.Sprintf("op=0x%02x total=%s count=%d avg=%s", m.op, m.a.total, m.a.count, avg)
	})
}

// Lightweight, apples-to-apples per-op breakdown for AddMulReturn without heavy logging.
// Measures time between opcode callbacks into fixed arrays to keep overhead minimal.
func TestProfile_AddMulReturn_Apple(t *testing.T) {
	// Program
	code := addMulReturnSimple
	// Common config fields (no tracers)
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}

	type agg struct {
		gasTotal  time.Duration
		execTotal time.Duration
		gasCount  uint64
		execCount uint64
	}
	var evmAgg [256]agg
	var mirAgg [256]agg

	// Install timing hooks (testing only)
	vm.SetEVMTimingHook(func(pc uint64, op byte, gasDur time.Duration, execDur time.Duration) {
		a := evmAgg[op]
		if gasDur > 0 {
			a.gasTotal += gasDur
			a.gasCount++
		}
		if execDur > 0 {
			a.execTotal += execDur
			a.execCount++
		}
		evmAgg[op] = a
		_ = pc
	})
	vm.SetMIRGasTimingHook(func(pc uint64, op byte, dur time.Duration) {
		a := mirAgg[op]
		a.gasTotal += dur
		a.gasCount++
		mirAgg[op] = a
		_ = pc
	})
	compiler.SetMIRExecTimingHook(func(_op compiler.MirOperation, evmPC uint64, dur time.Duration) {
		// Map MIR handler time back to originating EVM opcode via evmPC
		var op byte
		if evmPC < uint64(len(code)) {
			op = code[evmPC]
		}
		a := mirAgg[op]
		a.execTotal += dur
		a.execCount++
		mirAgg[op] = a
	})
	t.Cleanup(func() {
		vm.SetEVMTimingHook(nil)
		vm.SetMIRGasTimingHook(nil)
		compiler.SetMIRExecTimingHook(nil)
	})

	runProfile := func(cfg *runtime.Config, label string) (ret []byte, gasLeft uint64, err error) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, code)
		// Warm-up once to build MIR CFG if needed
		_, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		// Run N iterations
		const iters = 20000
		for i := 0; i < iters; i++ {
			ret, gasLeft, err = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
			if err != nil {
				return ret, gasLeft, err
			}
		}
		return ret, gasLeft, nil
	}

	// Base
	rb, _, err := runProfile(mkCfg(false), "amr_prof_base")
	if err != nil {
		t.Fatalf("base err: %v", err)
	}
	// MIR
	compiler.EnableOpcodeParse()
	rm, _, err := runProfile(mkCfg(true), "amr_prof_mir")
	if err != nil {
		t.Fatalf("mir err: %v", err)
	}
	if string(rb) != string(rm) {
		t.Fatalf("ret mismatch: %x vs %x", rb, rm)
	}

	// Summarize relevant ops
	used := []byte{byte(compiler.PUSH1), byte(compiler.ADD), byte(compiler.MUL), byte(compiler.MSTORE), byte(compiler.RETURN)}
	t.Log("AddMulReturn per-op totals over 20k calls (gasTotal/execTotal, counts, avgs):")
	for _, op := range used {
		be := evmAgg[op]
		me := mirAgg[op]
		var bgAvg, beAvg, mgAvg, meAvg time.Duration
		if be.gasCount > 0 {
			bgAvg = be.gasTotal / time.Duration(be.gasCount)
		}
		if be.execCount > 0 {
			beAvg = be.execTotal / time.Duration(be.execCount)
		}
		if me.gasCount > 0 {
			mgAvg = me.gasTotal / time.Duration(me.gasCount)
		}
		if me.execCount > 0 {
			meAvg = me.execTotal / time.Duration(me.execCount)
		}
		t.Logf("op=0x%02x base gas=%s(%d) avg=%s exec=%s(%d) avg=%s | mir gas=%s(%d) avg=%s exec=%s(%d) avg=%s",
			op,
			be.gasTotal, be.gasCount, bgAvg,
			be.execTotal, be.execCount, beAvg,
			me.gasTotal, me.gasCount, mgAvg,
			me.execTotal, me.execCount, meAvg,
		)
	}
}

// Quick apples-to-apples perf check (ns/op) for AddMulReturn without tracer overhead.
func TestPerf_AddMulReturn_Apple(t *testing.T) {
	code := addMulReturnSimple
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, iters int) (time.Duration, []byte) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, code)
		// warm
		_, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		var ret []byte
		for i := 0; i < iters; i++ {
			ret, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start), ret
	}
	const N = 200000
	dBase, _ := runN(mkCfg(false), "perf_amr_base", N)
	compiler.EnableOpcodeParse()
	dMir, _ := runN(mkCfg(true), "perf_amr_mir", N)
	t.Logf("AddMulReturn ns/op: EVM=%.1f, MIR=%.1f (iters=%d)", float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N), N)
}

func TestPerf_Storage_Apple(t *testing.T) {
	code := storageStoreLoadReturn
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, iters int) (time.Duration, []byte) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, code)
		// warm
		_, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		var ret []byte
		for i := 0; i < iters; i++ {
			ret, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start), ret
	}
	const N = 100000
	dBase, _ := runN(mkCfg(false), "perf_storage_base", N)
	compiler.EnableOpcodeParse()
	dMir, _ := runN(mkCfg(true), "perf_storage_mir", N)
	t.Logf("Storage ns/op: EVM=%.1f, MIR=%.1f (iters=%d)", float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N), N)
}

func TestPerf_KeccakMemory_Apple(t *testing.T) {
	code := keccakMemReturn
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, iters int) (time.Duration, []byte) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, code)
		// warm
		_, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		var ret []byte
		for i := 0; i < iters; i++ {
			ret, _, _ = evm.Call(sender, addr, nil, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start), ret
	}
	const N = 100000
	dBase, _ := runN(mkCfg(false), "perf_keccakm_base", N)
	compiler.EnableOpcodeParse()
	dMir, _ := runN(mkCfg(true), "perf_keccakm_mir", N)
	t.Logf("KeccakMemory ns/op: EVM=%.1f, MIR=%.1f (iters=%d)", float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N), N)
}

func TestPerf_CalldataKeccak_Apple(t *testing.T) {
	code := calldataKeccakReturn
	input := make([]byte, 96)
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, iters int) (time.Duration, []byte) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, code)
		// warm
		_, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		var ret []byte
		for i := 0; i < iters; i++ {
			ret, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start), ret
	}
	const N = 100000
	dBase, _ := runN(mkCfg(false), "perf_cdk_base", N)
	compiler.EnableOpcodeParse()
	dMir, _ := runN(mkCfg(true), "perf_cdk_mir", N)
	t.Logf("CalldataKeccak ns/op: EVM=%.1f, MIR=%.1f (iters=%d)", float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N), N)
}

// USDT contract perf per selector (ns/op) apples-to-apples.
func TestPerf_USDT_Apple(t *testing.T) {
	codeBytes, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode usdt: %v", err)
	}
	zero := make([]byte, 32)
	one := make([]byte, 32)
	one[31] = 1
	another := make([]byte, 32)
	another[31] = 1
	methods := []struct {
		name string
		sel  []byte
		args [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zero}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zero, zero}},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zero, one}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zero, one}},
		{"transferFrom", []byte{0x23, 0xb8, 0x72, 0xdd}, [][]byte{zero, another, one}},
		{"allowance_std", []byte{0xdd, 0x62, 0xed, 0x3e}, [][]byte{zero, another}},
	}
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, input []byte, iters int) time.Duration {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, codeBytes)
		// warm
		_, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		for i := 0; i < iters; i++ {
			_, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start)
	}
	const N = 20000
	compiler.EnableOpcodeParse()
	for _, m := range methods {
		in := append([]byte{}, m.sel...)
		for _, a := range m.args {
			in = append(in, a...)
		}
		dBase := runN(mkCfg(false), "perf_usdt_base_"+m.name, in, N)
		dMir := runN(mkCfg(true), "perf_usdt_mir_"+m.name, in, N)
		t.Logf("USDT %s ns/op: EVM=%.1f, MIR=%.1f", m.name, float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N))
	}
}

// WBNB contract perf per selector (ns/op) apples-to-apples.
func TestPerf_WBNB_Apple(t *testing.T) {
	codeBytes, err := hex.DecodeString(wbnbHex[2:])
	if err != nil {
		t.Fatalf("decode wbnb: %v", err)
	}
	zero := make([]byte, 32)
	one := make([]byte, 32)
	one[31] = 1
	methods := []struct {
		name string
		sel  []byte
		args [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zero}},
		{"deposit", []byte{0xd0, 0xe3, 0x0d, 0xb0}, nil},
		{"withdraw", []byte{0x2e, 0x1a, 0x7d, 0x4d}, [][]byte{one}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zero, one}},
	}
	mkCfg := func(enableMIR bool) *runtime.Config {
		cfg := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, Value: big.NewInt(0), BlockNumber: big.NewInt(1)}
		cfg.EVMConfig = vm.Config{EnableOpcodeOptimizations: false, EnableMIR: enableMIR}
		return cfg
	}
	runN := func(cfg *runtime.Config, label string, input []byte, iters int) time.Duration {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		addr := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(addr)
		evm.StateDB.SetCode(addr, codeBytes)
		// warm
		_, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		start := time.Now()
		for i := 0; i < iters; i++ {
			_, _, _ = evm.Call(sender, addr, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		}
		return time.Since(start)
	}
	const N = 20000
	compiler.EnableOpcodeParse()
	for _, m := range methods {
		in := append([]byte{}, m.sel...)
		for _, a := range m.args {
			in = append(in, a...)
		}
		dBase := runN(mkCfg(false), "perf_wbnb_base_"+m.name, in, N)
		dMir := runN(mkCfg(true), "perf_wbnb_mir_"+m.name, in, N)
		t.Logf("WBNB %s ns/op: EVM=%.1f, MIR=%.1f", m.name, float64(dBase.Nanoseconds())/float64(N), float64(dMir.Nanoseconds())/float64(N))
	}
}
