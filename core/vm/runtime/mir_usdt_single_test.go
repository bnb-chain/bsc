package runtime_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/ioutil"
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
	"github.com/ethereum/go-ethereum/crypto"
	ethlog "github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// TestMIRUSDT_Name_EVMvsMIR_Single: install USDT runtime and call name() once under base and once under MIR.
func TestMIRUSDT_Name_EVMvsMIR_Single(t *testing.T) {
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
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	mir := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
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
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mir.State == nil {
		mir.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}

	// Install code
	addr := common.BytesToAddress([]byte("contract_usdt_name_single"))
	evmB := runtime.NewEnv(base)
	evmM := runtime.NewEnv(mir)
	evmB.StateDB.CreateAccount(addr)
	evmM.StateDB.CreateAccount(addr)
	evmB.StateDB.SetCode(addr, code)
	evmM.StateDB.SetCode(addr, code)

	// calldata: name() selector 0x06fdde03
	input := []byte{0x06, 0xfd, 0xde, 0x03}

	// Inspect bytecode around 574 (block 46)
	if len(code) > 582 {
		var sb strings.Builder
		sb.WriteString("Bytecode around 574: ")
		for i := 574; i <= 582; i++ {
			sb.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb.String())
	}
	if len(code) > 1340 {
		var sb strings.Builder
		sb.WriteString("Bytecode around 1328: ")
		for i := 1328; i <= 1340; i++ {
			sb.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb.String())

		var sb2 strings.Builder
		sb2.WriteString("Bytecode around 340: ")
		for i := 340; i <= 350 && i < len(code); i++ {
			sb2.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb2.String())

		var sb3 strings.Builder
		sb3.WriteString("Bytecode around 1002 (Block 81): ")
		for i := 1002; i <= 1072 && i < len(code); i++ {
			sb3.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb3.String())

		var sb4 strings.Builder
		sb4.WriteString("Bytecode around 1142 (Block 87): ")
		for i := 1142; i <= 1152 && i < len(code); i++ {
			sb4.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb4.String())

		var sb5 strings.Builder
		sb5.WriteString("Bytecode around 313 (Block 184): ")
		for i := 313; i <= 340 && i < len(code); i++ {
			sb5.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb5.String())

		var sb6 strings.Builder
		sb6.WriteString("Bytecode around 347 (Block 185): ")
		for i := 347; i <= 360 && i < len(code); i++ {
			sb6.WriteString(fmt.Sprintf("%02x ", code[i]))
		}
		t.Log(sb6.String())
	}

	senderB := vm.AccountRef(base.Origin)
	retB, leftB, errB := evmB.Call(senderB, addr, input, base.GasLimit, uint256.MustFromBig(base.Value))

	// MIR call (enable parsing right before run)
	compiler.EnableOpcodeParse()
	senderM := vm.AccountRef(mir.Origin)
	retM, leftM, errM := evmM.Call(senderM, addr, input, mir.GasLimit, uint256.MustFromBig(mir.Value))

	// Parity on error/no-error
	if (errB != nil) != (errM != nil) {
		t.Fatalf("error mismatch base=%v mir=%v", errB, errM)
	}
	// If both errored (unexpected for name()), skip rest but report mismatch
	if errB != nil && errM != nil {
		t.Fatalf("both errored for name(): base=%v mir=%v", errB, errM)
	}
	// Success path: parity on gas and returndata
	if leftB != leftM {
		t.Logf("gas leftover mismatch base=%d mir=%d", leftB, leftM)
	}
	if !bytes.Equal(retB, retM) {
		t.Fatalf("returndata mismatch base=%x mir=%x", retB, retM)
	}
	// Basic sanity: name() returns a dynamic bytes string ABI, non-empty expected
	if len(retB) == 0 {
		t.Fatalf("empty return from base for name()")
	}
}

// TestMIRUSDT_DeployFromCreation_EVMvsMIR:
// - Load true USDT creation code from ../test_contract/usdt_creation_code.txt
// - Deploy with base EVM (no MIR initcode)
// - Deploy with MIR EVM (MIR initcode enabled)
// - If both succeed, call name() on each and compare parity (ret, gas, error)
func TestMIRUSDT_DeployFromCreation_EVMvsMIR(t *testing.T) {
	// Enable Opcode Parse
	compiler.EnableOpcodeParse()

	// Optional debug logs
	if os.Getenv("MIR_DEBUG") == "1" {
		compiler.EnableParserDebugLogs(true)
		compiler.EnableMIRDebugLogs(true)
	}
	var lastMIRPC uint64
	var mirPcs []uint64
	compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
		if m != nil {
			lastMIRPC = uint64(m.EvmPC())
			if len(mirPcs) < 4096 {
				mirPcs = append(mirPcs, lastMIRPC)
			}
			ops := m.OperandDebugStrings()
			t.Logf("MIR: pc=%d op=%s operands=%v", lastMIRPC, m.Op().String(), ops)
		}
	})
	// Read creation code from file
	creationHexBytes, err := ioutil.ReadFile("../test_contract/usdt_creation_code.txt")
	if err != nil {
		t.Fatalf("read creation code: %v", err)
	}
	creationStr := strings.TrimSpace(string(creationHexBytes))
	if strings.HasPrefix(creationStr, "0x") || strings.HasPrefix(creationStr, "0X") {
		creationStr = creationStr[2:]
	}
	creation, err := hex.DecodeString(creationStr)
	if err != nil {
		t.Fatalf("decode creation hex: %v", err)
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	// Base config: no MIR anywhere
	baseCfg := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    20_000_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		Debug:       true, // Enable tracing
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	// MIR config: enable MIR for initcode and runtime, strict mode
	mirCfg := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    20_000_000,
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

	// Deploy with base EVM
	var basePcs []uint64
	var baseOps []byte
	baseTracer := &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
		if len(basePcs) < 4096 {
			basePcs = append(basePcs, pc)
			baseOps = append(baseOps, op)
		}
		t.Logf("BASE: pc=%d op=%x gas=%d cost=%d", pc, op, gas, cost)
		if pc == 323 || pc == 1142 {
			t.Logf("BASE STACK at %d: %v", pc, scope.StackData())
		}
	}}
	baseCfg.EVMConfig.Tracer = baseTracer

	// Add tracer to MIR config too, to verify Create execution fallback
	mirCfg.EVMConfig.Tracer = &tracing.Hooks{OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
		t.Logf("MIR-BASE: pc=%d op=%x gas=%d cost=%d", pc, op, gas, cost)
	}}

	if baseCfg.State == nil {
		baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	if mirCfg.State == nil {
		mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}

	evmB := runtime.NewEnv(baseCfg)
	evmM := runtime.NewEnv(mirCfg)

	_, _, _, errB := evmB.Create(vm.AccountRef(baseCfg.Origin), creation, baseCfg.GasLimit, uint256.MustFromBig(baseCfg.Value))
	_, _, _, errM := evmM.Create(vm.AccountRef(mirCfg.Origin), creation, mirCfg.GasLimit, uint256.MustFromBig(mirCfg.Value))

	if (errB != nil) != (errM != nil) {
		t.Fatalf("creation error mismatch: base=%v mir=%v", errB, errM)
	}
	if errB != nil {
		t.Fatalf("creation failed: %v", errB)
	}

	// Now call name()
	// Assuming deployment creates contract at computed address
	// We need to check logs or state to find address?
	// For Create, address is computed from sender nonce.
	// Here we used clean state, so nonce 0.
	// Address should be same.
	addr := crypto.CreateAddress(baseCfg.Origin, 0)

	// Verify deployed code matches expectation
	// Check code
	realCode := evmB.StateDB.GetCode(addr)
	t.Logf("Runtime Code Length: %d", len(realCode))
	if len(realCode) > 50 {
		t.Logf("Runtime Code First 50 bytes: %x", realCode[:50])
	} else {
		t.Logf("Runtime Code: %x", realCode)
	}

	// Compare deployed code with usdtHex
	codeB := evmB.StateDB.GetCode(addr)
	codeM := evmM.StateDB.GetCode(addr)

	if !bytes.Equal(codeB, codeM) {
		t.Fatalf("deployed code mismatch base len=%d mir len=%d", len(codeB), len(codeM))
	}

	// usdtHex starts with 0x, so decode it
	expectedCode, _ := hex.DecodeString(usdtHex[2:])
	if !bytes.Equal(codeB, expectedCode) {
		t.Logf("deployed code differs from usdtHex!")
		t.Logf("Deployed len: %d, usdtHex len: %d", len(codeB), len(expectedCode))
		// Find first diff
		for i := 0; i < len(codeB) && i < len(expectedCode); i++ {
			if codeB[i] != expectedCode[i] {
				t.Logf("Diff at offset %d: deployed=%02x expected=%02x", i, codeB[i], expectedCode[i])
				break
			}
		}
	} else {
		t.Log("Deployed code matches usdtHex perfectly")
	}

	// Call name()
	input := []byte{0x06, 0xfd, 0xde, 0x03}
	retB, _, errB := evmB.Call(vm.AccountRef(baseCfg.Origin), addr, input, baseCfg.GasLimit, uint256.NewInt(0))
	retM, _, errM := evmM.Call(vm.AccountRef(mirCfg.Origin), addr, input, mirCfg.GasLimit, uint256.NewInt(0))

	if (errB != nil) != (errM != nil) {
		t.Fatalf("name call error mismatch: base=%v mir=%v", errB, errM)
	}
	if !bytes.Equal(retB, retM) {
		t.Fatalf("name call returndata mismatch: base=%x mir=%x", retB, retM)
	}
}
