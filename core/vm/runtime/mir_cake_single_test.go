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

// TestMIRCAKE_Transfer_EVMvsMIR_Single: load CAKE runtime code and call transfer(to, amount)
// once under base EVM and once under MIR (strict), comparing parity (error class, returndata, gas).
func TestMIRCAKE_Transfer_EVMvsMIR_Single(t *testing.T) {
	// Optional MIR logs (env driven)
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}

	// Load CAKE RUNTIME bytecode from test_contract
	raw, err := os.ReadFile("../test_contract/cake_runtime_code.txt")
	if err != nil {
		t.Fatalf("read cake runtime code: %v", err)
	}
	hexStr := string(bytes.TrimSpace(raw))
	if len(hexStr) >= 2 && (hexStr[0:2] == "0x" || hexStr[0:2] == "0X") {
		hexStr = hexStr[2:]
	}
	code, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("decode cake runtime hex: %v", err)
	}

	// Use BSC config at/after London to match available opcodes
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	base := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    15_000_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    15_000_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
			EnableMIR:                 true,
			EnableMIRInitcode:         false,
			MIRStrictNoFallback:       true,
		},
	}

	// Fresh state and install runtime code into a known address
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	tokenAddr := common.BytesToAddress([]byte("contract_cake_single"))
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(tokenAddr)
	evmM.StateDB.CreateAccount(tokenAddr)
	evmB.StateDB.SetCode(tokenAddr, code)
	evmM.StateDB.SetCode(tokenAddr, code)

	// Build calldata for transfer(to, 1)
	selector := []byte{0xa9, 0x05, 0x9c, 0xbb}
	to := make([]byte, 32)
	copy(to[12:], common.BytesToAddress([]byte("recipient_cake")).Bytes())
	amount := make([]byte, 32)
	amount[31] = 1
	input := append(append([]byte{}, selector...), append(to, amount...)...)

	// Base tracer captures last PC (for debugging only)
	var lastBasePC uint64
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			lastBasePC = pc
		},
	}

	// Execute base then MIR
	senderB := vm.AccountRef(base.Origin)
	senderM := vm.AccountRef(mir.Origin)
	retB, leftB, errB := evmB.Call(senderB, tokenAddr, input, base.GasLimit, uint256.MustFromBig(base.Value))
	// Enable MIR parsing only before MIR call
	compiler.EnableOpcodeParse()
	retM, leftM, errM := evmM.Call(senderM, tokenAddr, input, mir.GasLimit, uint256.MustFromBig(mir.Value))

	// Emit errors for inspection (but parity will be enforced)
	if errB != nil {
		t.Logf("Base EVM error: %v (last pc=%d)", errB, lastBasePC)
	}
	if errM != nil {
		t.Logf("MIR error: %v", errM)
	}

	// Parity on error/no-error state
	if (errB != nil) != (errM != nil) {
		t.Fatalf("error mismatch base=%v mir=%v", errB, errM)
	}
	// If both errored, normalize category (revert/badjump/invalid-opcode/other)
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
		return
	}

	// Success parity on gas and returndata
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if !bytes.Equal(retB, retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}
