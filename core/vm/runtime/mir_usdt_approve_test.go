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

func TestMIRUSDT_Approve_EVMvsMIR_Single(t *testing.T) {
	compiler.EnableOpcodeParse()
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableMIRDebugLogs(true)
		h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
		ethlog.SetDefault(ethlog.NewLogger(h))
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
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

	addr := common.BytesToAddress([]byte("contract_usdt_approve_single"))
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(addr)
	evmM.StateDB.CreateAccount(addr)
	evmB.StateDB.SetCode(addr, code)
	evmM.StateDB.SetCode(addr, code)

	// approve(address,uint256) selector 0x095ea7b3
	zeroAddress := make([]byte, 32)
	input := append([]byte{0x09, 0x5e, 0xa7, 0xb3}, zeroAddress...) // spender
	input = append(input, zeroAddress...)                           // value

	// Base call
	senderB := vm.AccountRef(base.Origin)
	var lastBasePC uint64
	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			lastBasePC = pc
		},
	}
	retB, leftB, errB := evmB.Call(senderB, addr, input, base.GasLimit, uint256.MustFromBig(base.Value))

	// MIR call
	compiler.EnableOpcodeParse()
	senderM := vm.AccountRef(mir.Origin)
	var lastMIRPC uint64
	compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
		if m != nil {
			lastMIRPC = uint64(m.EvmPC())
		}
	})

	// Add MIR gas probe
	vm.SetMIRGasProbe(func(pc uint64, op byte, gasLeft uint64) {
		fmt.Printf("MIR GAS: pc=%d op=%x gasLeft=%d\n", pc, op, gasLeft)
	})
	defer vm.SetMIRGasProbe(nil)

	retM, leftM, errM := evmM.Call(senderM, addr, input, mir.GasLimit, uint256.MustFromBig(mir.Value))
	compiler.SetGlobalMIRTracerExtended(nil)

	t.Logf("Base result: err=%v left=%d lastPC=%d ret=%x", errB, leftB, lastBasePC, retB)
	t.Logf("MIR result: err=%v left=%d lastPC=%d ret=%x", errM, leftM, lastMIRPC, retM)

	if (errB != nil) != (errM != nil) {
		t.Fatalf("error mismatch base=%v mir=%v", errB, errM)
	}
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
		if cat(errB) != cat(errM) {
			t.Fatalf("error category mismatch base=%q mir=%q", cat(errB), cat(errM))
		}
		if leftB != leftM {
			t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
		}
		return
	}
	if leftB != leftM {
		t.Fatalf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if !bytes.Equal(retB, retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
}
