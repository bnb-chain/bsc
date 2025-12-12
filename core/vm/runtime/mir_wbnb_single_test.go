package runtime_test

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	ethlog "github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// TestWBNB_Transfer_EVMvsMIR_Debug only runs the WBNB transfer selector once,
// compares base EVM vs MIR (errors and returndata), and emits debug logs.
func TestWBNB_Transfer_EVMvsMIR_Debug(t *testing.T) {
	// Make sure MIR opcode parsing is enabled
	compiler.EnableOpcodeParse()
	// Enable MIR debug logs only when MIR_DEBUG=1
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}

	// Read WBNB creation bytecode from file, derive runtime via simulated create.
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		t.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		t.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}

	// Prepare configs
	base := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	// Install contract code into fresh state for each config
	address := common.BytesToAddress([]byte("contract_wbnb_single"))
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	// Attach base tracer BEFORE creating env so it takes effect
	var baseGasSeq []uint64
	var basePCSeq []uint64
	var baseOpSeq []byte
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseGasSeq = append(baseGasSeq, gas)
			basePCSeq = append(basePCSeq, pc)
			baseOpSeq = append(baseOpSeq, op)
		},
	}
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(address)
	evmM.StateDB.CreateAccount(address)
	evmB.StateDB.SetCode(address, runtimeCode)
	evmM.StateDB.SetCode(address, runtimeCode)
	senderB := base.Origin
	senderM := mir.Origin

	// Build transfer(to, amount) calldata
	selector := []byte{0xa9, 0x05, 0x9c, 0xbb}
	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1
	input := append(append([]byte{}, selector...), append(zeroAddress, oneUint...)...)

	// Optional: basic EVM tracer to know last PC
	var lastBasePC uint64
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			lastBasePC = pc
		},
	}

	// Run both
	retB, leftB, errB := evmB.Call(senderB, address, input, base.GasLimit, uint256.MustFromBig(base.Value))
	retM, leftM, errM := evmM.Call(senderM, address, input, mir.GasLimit, uint256.MustFromBig(mir.Value))

	// Emit any errors for inspection
	if errB != nil {
		t.Logf("Base EVM error: %v (last pc=%d)", errB, lastBasePC)
	}
	if errM != nil {
		t.Logf("MIR error: %v", errM)
	}

	// Compare parity: both error/no-error states must match
	if (errB != nil) != (errM != nil) {
		t.Fatalf("error mismatch base=%v mir=%v", errB, errM)
	}
	// If both errored, compare error categories (normalize revert/opcode/jumpdest)
	if errB != nil && errM != nil {
		cat := func(e error) string {
			s := strings.ToLower(e.Error())
			switch {
			case strings.Contains(s, "revert"):
				return "revert"
			case strings.Contains(s, "invalid jump destination"):
				return "badjump"
			case strings.Contains(s, "invalid opcode"):
				return "invalid-opcode"
			default:
				return "other"
			}
		}
		if cb, cm := cat(errB), cat(errM); cb != cm {
			t.Fatalf("error category mismatch base=%q (%v) mir=%q (%v)", cb, errB, cm, errM)
		}
	}
	if errB == nil {
		if leftB != leftM {
			t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
		}
		if string(retB) != string(retM) {
			t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
		}
	}
}

// TestWBNB_View_Name_EVMvsMIR_Success: pure view success parity
func TestWBNB_View_Name_EVMvsMIR_Success(t *testing.T) {
	compiler.EnableOpcodeParse()
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}
	// derive runtime code from creation
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		t.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		t.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}
	// base and mir cfgs
	base := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}
	// setup states
	address := common.BytesToAddress([]byte("contract_wbnb_single"))
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	// Attach base tracer BEFORE creating env so it takes effect
	var baseGasSeq []uint64
	var basePCSeq []uint64
	var baseOpSeq []byte
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseGasSeq = append(baseGasSeq, gas)
			basePCSeq = append(basePCSeq, pc)
			baseOpSeq = append(baseOpSeq, op)
		},
	}
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(address)
	evmM.StateDB.CreateAccount(address)
	evmB.StateDB.SetCode(address, runtimeCode)
	evmM.StateDB.SetCode(address, runtimeCode)
	senderB := base.Origin
	senderM := mir.Origin

	// name()
	input := []byte{0x06, 0xfd, 0xde, 0x03}
	retB, leftB, errB := evmB.Call(senderB, address, input, base.GasLimit, uint256.NewInt(0))
	retM, leftM, errM := evmM.Call(senderM, address, input, mir.GasLimit, uint256.NewInt(0))
	if errB != nil || errM != nil {
		t.Fatalf("unexpected error base=%v mir=%v", errB, errM)
	}
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if string(retB) != string(retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}

// TestWBNB_Deposit_Then_Transfer_EVMvsMIR_Success: deposit then transfer to non-zero recipient
func TestWBNB_Deposit_Then_Transfer_EVMvsMIR_Success(t *testing.T) {
	compiler.EnableOpcodeParse()
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}
	// derive runtime code from creation
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		t.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		t.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}
	// base and mir cfgs
	base := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}
	// setup states
	address := common.BytesToAddress([]byte("contract_wbnb_single"))
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	// Attach base tracer BEFORE creating env so it takes effect
	var baseGasSeq []uint64
	var basePCSeq []uint64
	var baseOpSeq []byte
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseGasSeq = append(baseGasSeq, gas)
			basePCSeq = append(basePCSeq, pc)
			baseOpSeq = append(baseOpSeq, op)
		},
	}
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(address)
	evmM.StateDB.CreateAccount(address)
	evmB.StateDB.SetCode(address, runtimeCode)
	evmM.StateDB.SetCode(address, runtimeCode)
	// fund sender native coins for deposit value
	fund := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 ether
	evmB.StateDB.AddBalance(base.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	evmM.StateDB.AddBalance(mir.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	senderB := base.Origin
	senderM := mir.Origin

	// deposit(value=1e18)
	depositSel := []byte{0xd0, 0xe3, 0x0d, 0xb0}
	if _, _, err := evmB.Call(senderB, address, depositSel, base.GasLimit, uint256.MustFromBig(fund)); err != nil {
		t.Fatalf("base deposit failed: %v", err)
	}
	// Perform MIR-side deposit using base interpreter against the same state, then switch to MIR for transfer.
	mirDepositCfg := *mir
	mirDepositCfg.EVMConfig.EnableOpcodeOptimizations = false
	mirDepositCfg.EVMConfig.EnableMIR = false
	mirDepositCfg.State = mir.State
	envMD := runtime.NewEnv(&mirDepositCfg)
	if _, _, err := envMD.Call(senderM, address, depositSel, mir.GasLimit, uint256.MustFromBig(fund)); err != nil {
		t.Fatalf("mir deposit (base path) failed: %v", err)
	}

	// transfer(to!=0, amount<=deposit)
	to := make([]byte, 32)
	to[12] = 0x12
	to[31] = 0x34
	amount := make([]byte, 32)
	amount[31] = 0x01 // 1 wei
	txSel := []byte{0xa9, 0x05, 0x9c, 0xbb}
	input := append(append([]byte{}, txSel...), append(to, amount...)...)
	retB, leftB, errB := evmB.Call(senderB, address, input, base.GasLimit, uint256.NewInt(0))
	retM, leftM, errM := evmM.Call(senderM, address, input, mir.GasLimit, uint256.NewInt(0))
	if errB != nil || errM != nil {
		t.Fatalf("unexpected error after transfer base=%v mir=%v", errB, errM)
	}
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if string(retB) != string(retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}

// Benchmark: WBNB name() EVM vs MIR
func BenchmarkWBNB_View_Name(b *testing.B) {
	compiler.EnableOpcodeParse()
	// derive runtime code from creation
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		b.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		b.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		b.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}

	address := common.BytesToAddress([]byte("contract_wbnb_bench"))
	input := []byte{0x06, 0xfd, 0xde, 0x03} // name()

	// Base EVM
	b.Run("EVM_Base_name", func(b *testing.B) {
		cfg := &runtime.Config{
			ChainConfig: params.MainnetChainConfig,
			GasLimit:    10_000_000,
			Origin:      common.Address{},
			BlockNumber: big.NewInt(15_000_000),
			Value:       big.NewInt(0),
			EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
		}
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		env := runtime.NewEnv(cfg)
		env.StateDB.CreateAccount(address)
		env.StateDB.SetCode(address, runtimeCode)
		sender := cfg.Origin
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := env.Call(sender, address, input, cfg.GasLimit, uint256.NewInt(0)); err != nil {
				b.Fatalf("base name() failed: %v", err)
			}
		}
	})

	// MIR
	b.Run("EVM_MIR_name", func(b *testing.B) {
		cfg := &runtime.Config{
			ChainConfig: params.MainnetChainConfig,
			GasLimit:    10_000_000,
			Origin:      common.Address{},
			BlockNumber: big.NewInt(15_000_000),
			Value:       big.NewInt(0),
			EVMConfig:   vm.Config{EnableMIR: true},
		}
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		env := runtime.NewEnv(cfg)
		env.StateDB.CreateAccount(address)
		env.StateDB.SetCode(address, runtimeCode)
		sender := cfg.Origin
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := env.Call(sender, address, input, cfg.GasLimit, uint256.NewInt(0)); err != nil {
				b.Fatalf("mir name() failed: %v", err)
			}
		}
	})
}

// TestWBNB_Deposit_EVMvsMIR_Success: deposit only, success and parity with MIR fallback allowed
func TestWBNB_Deposit_EVMvsMIR_Parity(t *testing.T) {
	compiler.EnableOpcodeParse()
	compiler.EnableMIRDebugLogs(true)
	// read creation code and derive runtime
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		t.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		t.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}

	// base and mir cfgs
	base := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}
	address := common.BytesToAddress([]byte("contract_wbnb_deposit"))
	// init states
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	// Attach base tracer BEFORE creating env so it takes effect (for base tail capture)
	var baseGasSeq []uint64
	var basePCSeq []uint64
	var baseOpSeq []byte
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			baseGasSeq = append(baseGasSeq, gas)
			basePCSeq = append(basePCSeq, pc)
			baseOpSeq = append(baseOpSeq, op)
		},
	}
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(address)
	evmM.StateDB.CreateAccount(address)
	evmB.StateDB.SetCode(address, runtimeCode)
	evmM.StateDB.SetCode(address, runtimeCode)
	// fund sender
	fund := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 ether
	evmB.StateDB.AddBalance(base.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	evmM.StateDB.AddBalance(mir.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	senderB := base.Origin
	senderM := mir.Origin

	// deposit selector; value=1e18
	depositSel := []byte{0xd0, 0xe3, 0x0d, 0xb0}
	var mirGasSeq []uint64
	var mirPCSeq []uint64
	var mirOpSeq []byte
	vm.SetMIRGasProbe(func(pc uint64, op byte, gasLeft uint64) {
		mirGasSeq = append(mirGasSeq, gasLeft)
		mirPCSeq = append(mirPCSeq, pc)
		mirOpSeq = append(mirOpSeq, op)
	})
	retB, leftB, errB := evmB.Call(senderB, address, depositSel, base.GasLimit, uint256.MustFromBig(fund))
	retM, leftM, errM := evmM.Call(senderM, address, depositSel, mir.GasLimit, uint256.MustFromBig(fund))
	// reset hooks
	vm.SetMIRGasProbe(nil)
	base.EVMConfig.Tracer = nil
	if errB != nil || errM != nil {
		t.Fatalf("unexpected error base=%v mir=%v", errB, errM)
	}
	if leftB != leftM {
		// Print concise end-of-trace to spot the delta
		t.Logf("gas leftover mismatch base=%d mir=%d (delta=%d)", leftB, leftM, int64(leftM)-int64(leftB))
		n := 10
		if n > len(baseGasSeq) {
			n = len(baseGasSeq)
		}
		if n > 0 {
			t.Logf("Base last %d steps:", n)
			for i := len(baseGasSeq) - n; i < len(baseGasSeq); i++ {
				t.Logf("  pc=%d op=0x%02x gas(before)=%d", basePCSeq[i], baseOpSeq[i], baseGasSeq[i])
			}
		}
		m := 10
		if m > len(mirGasSeq) {
			m = len(mirGasSeq)
		}
		if m > 0 {
			t.Logf("MIR last %d steps (gas after):", m)
			for i := len(mirGasSeq) - m; i < len(mirGasSeq); i++ {
				t.Logf("  pc=%d op=0x%02x gas(after)=%d", mirPCSeq[i], mirOpSeq[i], mirGasSeq[i])
			}
		}
		t.Fatalf("gas mismatch")
	}
	if string(retB) != string(retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}

// TestWBNB_Deposit_EVMvsMIR_Success: deposit mints WBNB equal to msg.value, then balanceOf(sender) must match
func TestWBNB_Deposit_EVMvsMIR_Success(t *testing.T) {
	compiler.EnableOpcodeParse()
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}
	// derive runtime code from creation
	raw, err := os.ReadFile("../test_contract/wbnb_creation_code.txt")
	if err != nil {
		t.Fatalf("read wbnb creation code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	creation, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode wbnb creation hex: %v", err)
	}
	deployCfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	runtimeCode, _, _, err := runtime.Create(creation, deployCfg)
	if err != nil || len(runtimeCode) == 0 {
		t.Fatalf("failed to derive WBNB runtime code via create: err=%v len=%d", err, len(runtimeCode))
	}
	// base and mir cfgs
	base := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(15_000_000),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}
	// setup states
	address := common.BytesToAddress([]byte("contract_wbnb_deposit"))
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(address)
	evmM.StateDB.CreateAccount(address)
	evmB.StateDB.SetCode(address, runtimeCode)
	evmM.StateDB.SetCode(address, runtimeCode)
	// fund sender native coins for deposit value
	fund := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 ether
	evmB.StateDB.AddBalance(base.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	evmM.StateDB.AddBalance(mir.Origin, uint256.MustFromBig(fund), tracing.BalanceIncreaseGenesisBalance)
	senderB := base.Origin
	senderM := mir.Origin

	// deposit(value=1e18)
	depositSel := []byte{0xd0, 0xe3, 0x0d, 0xb0}
	retB, leftB, errB := evmB.Call(senderB, address, depositSel, base.GasLimit, uint256.MustFromBig(fund))
	retM, leftM, errM := evmM.Call(senderM, address, depositSel, mir.GasLimit, uint256.MustFromBig(fund))
	if errB != nil || errM != nil {
		t.Fatalf("deposit error base=%v mir=%v", errB, errM)
	}
	// gas/returndata parity on success
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if string(retB) != string(retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
	// balanceOf(sender) should equal fund (32-byte BE)
	balSel := []byte{0x70, 0xa0, 0x82, 0x31}
	addrArg := make([]byte, 32)
	copy(addrArg[12:], base.Origin.Bytes())
	input := append([]byte{}, balSel...)
	input = append(input, addrArg...)
	bretB, _, berrB := evmB.Call(senderB, address, input, base.GasLimit, uint256.NewInt(0))
	bretM, _, berrM := evmM.Call(senderM, address, input, mir.GasLimit, uint256.NewInt(0))
	if berrB != nil || berrM != nil {
		t.Fatalf("balanceOf error base=%v mir=%v", berrB, berrM)
	}
	if string(bretB) != string(bretM) {
		t.Fatalf("balanceOf returndata mismatch base=%x mir=%x", bretB, bretM)
	}
	// Optional: verify equals fund
	if len(bretB) == 32 {
		if new(big.Int).SetBytes(bretB).Cmp(fund) != 0 {
			t.Fatalf("unexpected balanceOf base=%s want=%s", new(big.Int).SetBytes(bretB).String(), fund.String())
		}
	}
}
