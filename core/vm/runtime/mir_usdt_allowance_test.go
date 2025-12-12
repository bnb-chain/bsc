package runtime_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
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

// TestMIRUSDT_Allowance_EVMvsMIR_Single: install USDT runtime and call allowance (actually increaseAllowance 0x39509351)
// once under base and once under MIR.
func TestMIRUSDT_Allowance_EVMvsMIR_Single(t *testing.T) {
	// Enable MIR opcode parsing
	compiler.EnableOpcodeParse()
	// Optional debug logs (env var)
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)

	// Load USDT runtime bytecode (same source as parity tests)
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT runtime hex failed: %v", err)
	}

	base := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		Debug:       true, // Enable tracing
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
			EnableMIR:                 true,
		},
	}
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}

	// Install code
	addr := common.BytesToAddress([]byte("contract_usdt_allowance_single"))
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(addr)
	evmM.StateDB.CreateAccount(addr)
	evmB.StateDB.SetCode(addr, code)
	evmM.StateDB.SetCode(addr, code)

	// Dump bytecode around PC 1376
	if len(code) > 1400 {
		var sb strings.Builder
		sb.WriteString("Bytecode around 1376: ")
		for i := 1376; i <= 1400; i++ {
			sb.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb.String())
	}
	if len(code) > 2443 {
		var sb strings.Builder
		sb.WriteString("Bytecode around 2440: ")
		for i := 2440; i <= 2450; i++ {
			sb.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb.String())
	}

	// calldata: allowance() selector 0x39509351 (actually increaseAllowance)
	// args: zeroAddress (32 bytes), zeroAddress (32 bytes)
	zeroAddress := make([]byte, 32)
	input := append([]byte{0x39, 0x50, 0x93, 0x51}, zeroAddress...)
	input = append(input, zeroAddress...)

	// Base call
	senderB := base.Origin
	var lastBasePC uint64
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			lastBasePC = pc
			fmt.Printf("BASE: pc=%d op=%x gas=%d cost=%d stack=%v\n", pc, op, gas, cost, scope.StackData())
		},
	}
	// Re-create evmB with the tracer config
	evmB = runtime.NewEnv(base)
	evmB.StateDB.CreateAccount(addr)
	evmB.StateDB.SetCode(addr, code)

	retB, leftB, errB := evmB.Call(senderB, addr, input, base.GasLimit, uint256.MustFromBig(base.Value))

	// MIR call (enable parsing before run)
	compiler.EnableOpcodeParse()
	senderM := mir.Origin

	// Trace last PC for MIR
	var lastMIRPC uint64
	compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
		// Clean up global tracer to prevent test pollution
		defer compiler.SetGlobalMIRTracerExtended(nil)
		if m != nil {
			lastMIRPC = uint64(m.EvmPC())
			ops := m.OperandDebugStrings()
			t.Logf("MIR: pc=%d op=%s operands=%v", lastMIRPC, m.Op().String(), ops)
		}
	})

	// Add MIR gas probe
	vm.SetMIRGasProbe(func(pc uint64, op byte, gasLeft uint64) {
		fmt.Printf("MIR GAS: pc=%d op=%x gasLeft=%d\n", pc, op, gasLeft)
	})
	defer vm.SetMIRGasProbe(nil)

	retM, leftM, errM := evmM.Call(senderM, addr, input, mir.GasLimit, uint256.MustFromBig(mir.Value))
	compiler.SetGlobalMIRTracerExtended(nil)

	t.Logf("Base result: err=%v left=%d lastPC=%d", errB, leftB, lastBasePC)
	t.Logf("MIR result: err=%v left=%d lastPC=%d", errM, leftM, lastMIRPC)

	// Parity on error/no-error
	if (errB != nil) != (errM != nil) {
		t.Fatalf("error mismatch base=%v mir=%v", errB, errM)
	}
	// If both errored, compare error categories
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
		cb, cm := cat(errB), cat(errM)
		if cb != cm {
			t.Fatalf("error category mismatch base=%q (%v) mir=%q (%v)", cb, errB, cm, errM)
		}
		// Gas check is also required even if error occurred, unless it's a specific non-deterministic error.
		// But for revert, gas should be deterministic.
		if leftB != leftM {
			t.Fatalf("gas leftover mismatch (err) base=%d mir=%d", leftB, leftM)
		}
		return
	}
	// Success path: parity on gas and returndata
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if !bytes.Equal(retB, retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}
