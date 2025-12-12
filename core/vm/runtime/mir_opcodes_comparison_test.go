// Copyright 2024 The go-ethereum Authors
// Simple opcodes comparison test with builder pattern

package runtime

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

// ============================================================================
// STRUCTURES
// ============================================================================

type OpcodeTestCase struct {
	Name        string
	Bytecode    []byte
	Calldata    []byte
	InitialGas  uint64
	Description string
}

type ExecutionResult struct {
	ReturnData      []byte
	GasUsed         uint64
	GasLeft         uint64
	Error           error
	Success         bool
	UsedMIR         bool
	InterpreterInfo string
}

// ============================================================================
// BUILDER FUNCTIONS
// ============================================================================

// encodePush returns minimal PUSH opcode for big-endian value
func encodePush(b []byte) []byte {
	if len(b) == 0 {
		return []byte{byte(vm.PUSH1), 0x00}
	}
	// Trim leading zeros
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

// buildBinaryOpTest: PUSH a; PUSH b; OP; MSTORE; RETURN
func buildBinaryOpTest(name string, op byte, a, b []byte, desc string) OpcodeTestCase {
	code := make([]byte, 0, 100)
	code = append(code, encodePush(a)...)
	code = append(code, encodePush(b)...)
	code = append(code, op)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))
	return OpcodeTestCase{Name: name, Bytecode: code, InitialGas: 100000, Description: desc}
}

// buildUnaryOpTest: PUSH x; OP; MSTORE; RETURN
func buildUnaryOpTest(name string, op byte, x []byte, desc string) OpcodeTestCase {
	code := make([]byte, 0, 64)
	code = append(code, encodePush(x)...)
	code = append(code, op)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))
	return OpcodeTestCase{Name: name, Bytecode: code, InitialGas: 100000, Description: desc}
}

// buildNullaryOpTest: OP; MSTORE; RETURN
func buildNullaryOpTest(name string, op byte, desc string) OpcodeTestCase {
	code := []byte{op, byte(vm.PUSH1), 0x00, byte(vm.MSTORE), byte(vm.PUSH1), 0x20, byte(vm.PUSH1), 0x00, byte(vm.RETURN)}
	return OpcodeTestCase{Name: name, Bytecode: code, InitialGas: 100000, Description: desc}
}

// buildStackOpTest: PUSH values; OP; MSTORE; RETURN
func buildStackOpTest(name string, op byte, pushes [][]byte, desc string) OpcodeTestCase {
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
	return OpcodeTestCase{Name: name, Bytecode: code, InitialGas: 100000, Description: desc}
}

// buildTernaryOpTest: PUSH a; PUSH b; PUSH c; OP; MSTORE; RETURN
func buildTernaryOpTest(name string, op byte, a, b, c []byte, desc string) OpcodeTestCase {
	code := make([]byte, 0, 120)
	code = append(code, encodePush(a)...)
	code = append(code, encodePush(b)...)
	code = append(code, encodePush(c)...)
	code = append(code, op)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))
	return OpcodeTestCase{Name: name, Bytecode: code, InitialGas: 100000, Description: desc}
}

// buildPushTest: Generate PUSH test for PUSHn opcode
// PUSHn pushes n bytes onto stack, then MSTORE and RETURN
func buildPushTest(n int) OpcodeTestCase {
	if n < 1 || n > 32 {
		panic("PUSH size must be between 1 and 32")
	}

	// Generate n bytes of test data (0x01, 0x02, ..., 0x0n)
	data := make([]byte, n)
	for i := 0; i < n; i++ {
		data[i] = byte((i + 1) % 256)
	}

	// PUSHn opcode is 0x60 + (n-1)
	pushOp := byte(0x60 + n - 1)

	// Construct bytecode: PUSHn <data>, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	code := make([]byte, 0, n+10)
	code = append(code, pushOp)
	code = append(code, data...)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 0x20)
	code = append(code, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.RETURN))

	return OpcodeTestCase{
		Name:        fmt.Sprintf("PUSH%d", n),
		Bytecode:    code,
		InitialGas:  100000,
		Description: fmt.Sprintf("Push %d byte(s)", n),
	}
}

// ============================================================================
// EXECUTION FUNCTIONS
// ============================================================================

// needsAdvancedEIPs checks if an opcode requires Shanghai or Cancun fork
func needsAdvancedEIPs(opcodeName string) bool {
	// Shanghai opcodes (EIP-3855)
	if strings.Contains(opcodeName, "PUSH0") {
		return true
	}
	// Cancun opcodes (EIP-1153, EIP-5656, EIP-4844)
	cancunOpcodes := []string{"TLOAD", "TSTORE", "MCOPY", "BLOBHASH", "BLOBBASEFEE"}
	for _, op := range cancunOpcodes {
		if strings.Contains(opcodeName, op) {
			return true
		}
	}
	return false
}

func executeWithEVM(bytecode, calldata []byte, initialGas uint64, opcodeName string) *ExecutionResult {
	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(db, nil)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabase(trieDB, nil))

	caller := common.HexToAddress("0x1000")
	contract := common.HexToAddress("0x2000")
	statedb.CreateAccount(caller)
	statedb.CreateAccount(contract)
	statedb.SetBalance(caller, uint256.NewInt(1e18), tracing.BalanceChangeTouchAccount)
	statedb.SetCode(contract, bytecode)

	// Choose chain config based on required EIPs
	chainConfig := params.TestChainConfig
	if needsAdvancedEIPs(opcodeName) {
		chainConfig = params.MergedTestChainConfig
	}
	blockContext := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        1000,
		Difficulty:  big.NewInt(1),
		GasLimit:    10000000,
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(1), // For BLOBBASEFEE opcode (EIP-4844)
	}

	txContext := vm.TxContext{
		Origin:   caller,
		GasPrice: big.NewInt(1),
	}

	vmConfig := vm.Config{
		EnableOpcodeOptimizations: false,
	}

	evm := vm.NewEVM(blockContext, statedb, chainConfig, vmConfig)
	evm.SetTxContext(txContext)

	ret, gasLeft, err := evm.Call(
		caller,
		contract,
		calldata,
		initialGas,
		uint256.NewInt(0),
	)

	return &ExecutionResult{
		ReturnData:      ret,
		GasUsed:         initialGas - gasLeft,
		GasLeft:         gasLeft,
		Error:           err,
		Success:         err == nil,
		UsedMIR:         false,
		InterpreterInfo: "Base EVM Interpreter (EnableOpcodeOptimizations=false)",
	}
}

func executeWithMIR(bytecode, calldata []byte, initialGas uint64, opcodeName string) *ExecutionResult {
	compiler.EnableOptimization()
	compiler.EnableOpcodeParse()

	if !compiler.IsEnabled() {
		panic("❌ FATAL: compiler.IsEnabled() = false")
	}
	if !compiler.IsOpcodeParseEnabled() {
		panic("❌ FATAL: compiler.IsOpcodeParseEnabled() = false")
	}

	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(db, nil)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabase(trieDB, nil))

	caller := common.HexToAddress("0x1000")
	contract := common.HexToAddress("0x2000")
	statedb.CreateAccount(caller)
	statedb.CreateAccount(contract)
	statedb.SetBalance(caller, uint256.NewInt(1e18), tracing.BalanceChangeTouchAccount)
	statedb.SetCode(contract, bytecode)

	// Choose chain config based on required EIPs
	chainConfig := params.TestChainConfig
	if needsAdvancedEIPs(opcodeName) {
		chainConfig = params.MergedTestChainConfig
	}

	blockContext := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        1000,
		Difficulty:  big.NewInt(1),
		GasLimit:    10000000,
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(1), // For BLOBBASEFEE opcode (EIP-4844)
	}

	txContext := vm.TxContext{
		Origin:   caller,
		GasPrice: big.NewInt(1),
	}

	vmConfig := vm.Config{
		EnableOpcodeOptimizations: false,
		EnableMIR:                 true,
	}

	evm := vm.NewEVM(blockContext, statedb, chainConfig, vmConfig)
	evm.SetTxContext(txContext)

	ret, gasLeft, err := evm.Call(
		caller,
		contract,
		calldata,
		initialGas,
		uint256.NewInt(0),
	)

	codeHash := statedb.GetCodeHash(contract)
	// Force CFG regeneration to pickup latest NOP emission fixes
	compiler.DeleteMIRCFGCache(codeHash)
	cfg := compiler.LoadMIRCFG(codeHash)
	usedMIR := cfg != nil
	interpreterInfo := "Unknown"

	if !usedMIR {
		testCfg, testErr := compiler.TryGenerateMIRCFG(codeHash, bytecode)
		if testErr != nil {
			interpreterInfo = fmt.Sprintf("MIR CFG 生成失败: %v", testErr)
		} else if testCfg != nil {
			cachedCfg := compiler.LoadMIRCFG(codeHash)
			if cachedCfg != nil {
				usedMIR = true
				interpreterInfo = "MIR Interpreter (CFG 生成成功)"
			} else {
				interpreterInfo = "MIR CFG 生成但未缓存"
			}
		} else {
			interpreterInfo = "MIR CFG 为 nil"
		}
	} else {
		interpreterInfo = "MIR Interpreter (CFG cached)"
	}

	if !usedMIR && interpreterInfo == "Unknown" {
		interpreterInfo = "Fallback (Superinstruction or Base EVM)"
	}

	return &ExecutionResult{
		ReturnData:      ret,
		GasUsed:         initialGas - gasLeft,
		GasLeft:         gasLeft,
		Error:           err,
		Success:         err == nil,
		UsedMIR:         usedMIR,
		InterpreterInfo: interpreterInfo,
	}
}

func compareResults(t *testing.T, testName string, evmResult, mirResult *ExecutionResult) bool {
	allMatch := true

	t.Logf("\n  🔍 运行时 Interpreter 验证:")
	t.Logf("     EVM 路径: %s", evmResult.InterpreterInfo)
	t.Logf("     MIR 路径: %s", mirResult.InterpreterInfo)

	if mirResult.UsedMIR {
		t.Logf("  ✅ 确认: MIR Interpreter 正在使用！")
	} else {
		t.Errorf("  ❌ 严重警告: MIR Interpreter 未被使用！")
		t.Errorf("     实际使用的是: %s", mirResult.InterpreterInfo)
		t.Fatalf("  🛑 测试失败: MIR Interpreter 必须被使用，但发生了 fallback！")
	}

	if evmResult.GasUsed != mirResult.GasUsed {
		t.Errorf("  ❌ Gas 不一致: EVM=%d, MIR=%d (差异=%d)",
			evmResult.GasUsed, mirResult.GasUsed, int64(mirResult.GasUsed)-int64(evmResult.GasUsed))
		allMatch = false
	} else {
		t.Logf("  ✅ Gas 一致: %d", evmResult.GasUsed)
	}

	if !bytes.Equal(evmResult.ReturnData, mirResult.ReturnData) {
		t.Errorf("  ❌ Returndata 不一致:")
		t.Errorf("     EVM: %s", hex.EncodeToString(evmResult.ReturnData))
		t.Errorf("     MIR: %s", hex.EncodeToString(mirResult.ReturnData))
		allMatch = false
	} else {
		if len(evmResult.ReturnData) > 0 {
			maxLen := len(evmResult.ReturnData)
			if maxLen > 32 {
				maxLen = 32
			}
			t.Logf("  ✅ Returndata 一致: %s", hex.EncodeToString(evmResult.ReturnData[:maxLen]))
		} else {
			t.Logf("  ✅ Returndata 一致: (empty)")
		}
	}

	evmHasError := evmResult.Error != nil
	mirHasError := mirResult.Error != nil

	if evmHasError != mirHasError {
		t.Errorf("  ❌ 错误状态不一致:")
		t.Errorf("     EVM error: %v", evmResult.Error)
		t.Errorf("     MIR error: %v", mirResult.Error)
		allMatch = false
	} else if evmHasError && mirHasError {
		t.Logf("  ✅ 错误状态一致: 都失败")
	} else {
		t.Logf("  ✅ 错误状态一致: 都成功")
	}

	t.Logf("  ℹ️  Memory 通过 returndata 间接验证")

	return allMatch
}

func runOpcodeTests(t *testing.T, category string, tests []OpcodeTestCase) {
	separator := strings.Repeat("=", 80)
	t.Logf("\n%s", separator)
	t.Logf("Testing Category: %s", category)
	t.Logf("%s", separator)

	totalTests := len(tests)
	passedTests := 0

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			t.Logf("\n--- Test: %s ---", tc.Name)
			t.Logf("Description: %s", tc.Description)
			t.Logf("Bytecode: %x", tc.Bytecode)

			t.Logf("\n🔵 Executing with EVM...")
			evmResult := executeWithEVM(tc.Bytecode, tc.Calldata, tc.InitialGas, tc.Name)
			t.Logf("  Gas used: %d", evmResult.GasUsed)
			t.Logf("  Success: %v", evmResult.Success)

			t.Logf("\n🟢 Executing with MIR...")
			mirResult := executeWithMIR(tc.Bytecode, tc.Calldata, tc.InitialGas, tc.Name)
			t.Logf("  Gas used: %d", mirResult.GasUsed)
			t.Logf("  Success: %v", mirResult.Success)

			t.Logf("\n📊 Comparing Results...")
			allMatch := compareResults(t, tc.Name, evmResult, mirResult)

			if allMatch {
				t.Logf("\n✅ Test PASSED: %s", tc.Name)
				passedTests++
			} else {
				t.Errorf("\n❌ Test FAILED: %s", tc.Name)
			}
		})
	}

	t.Logf("\n%s", separator)
	t.Logf("Category %s: %d/%d tests passed (%.1f%%)",
		category, passedTests, totalTests, float64(passedTests)/float64(totalTests)*100)
	t.Logf("%s\n", separator)
}

// ============================================================================
// TEST CASES - Using Builder Pattern
// ============================================================================

func TestMIRvsEVM_Arithmetic(t *testing.T) {
	tests := []OpcodeTestCase{
		buildBinaryOpTest("ADD", byte(vm.ADD), []byte{0x05}, []byte{0x03}, "5 + 3 = 8"),
		buildBinaryOpTest("SUB", byte(vm.SUB), []byte{0x05}, []byte{0x03}, "5 - 3 = 2"),
		buildBinaryOpTest("MUL", byte(vm.MUL), []byte{0x05}, []byte{0x03}, "5 * 3 = 15"),
		buildBinaryOpTest("DIV", byte(vm.DIV), []byte{0x06}, []byte{0x03}, "6 / 3 = 2"),
		buildBinaryOpTest("SDIV", byte(vm.SDIV), []byte{0xfe}, []byte{0x02}, "-2 / 2 = -1"),
		buildBinaryOpTest("MOD", byte(vm.MOD), []byte{0x07}, []byte{0x03}, "7 % 3 = 1"),
		buildBinaryOpTest("SMOD", byte(vm.SMOD), []byte{0xf9}, []byte{0x03}, "-7 % 3 = -1"),
		// EXP - Dynamic gas: 10 + 50*byteLength(exponent) after EIP-158
		buildBinaryOpTest("EXP_SmallExponent", byte(vm.EXP), []byte{0x03}, []byte{0x02}, "2^3=8, exp=3 (1 byte) - dynamic gas = 10 + 50*1 = 60"),
		buildBinaryOpTest("EXP_ZeroExponent", byte(vm.EXP), []byte{0x00}, []byte{0x02}, "2^0=1, exp=0 (0 bytes) - dynamic gas = 10 + 50*0 = 10"),
		buildBinaryOpTest("EXP_LargeExponent", byte(vm.EXP), []byte{0x10, 0x00}, []byte{0x02}, "2^4096, exp=0x1000 (2 bytes) - dynamic gas = 10 + 50*2 = 110"),
		buildBinaryOpTest("SIGNEXTEND", byte(vm.SIGNEXTEND), []byte{0xff}, []byte{0x00}, "Sign extend 0xff"),
		buildTernaryOpTest("ADDMOD", byte(vm.ADDMOD), []byte{0x05}, []byte{0x04}, []byte{0x03}, "(3+4)%5=2"),
		buildTernaryOpTest("MULMOD", byte(vm.MULMOD), []byte{0x05}, []byte{0x04}, []byte{0x03}, "(3*4)%5=2"),
	}
	runOpcodeTests(t, "Arithmetic", tests)
}

func TestMIRvsEVM_Comparison(t *testing.T) {
	tests := []OpcodeTestCase{
		buildBinaryOpTest("LT", byte(vm.LT), []byte{0x05}, []byte{0x03}, "3 < 5 = 1"),
		buildBinaryOpTest("GT", byte(vm.GT), []byte{0x03}, []byte{0x05}, "5 > 3 = 1"),
		buildBinaryOpTest("SLT", byte(vm.SLT), []byte{0xfe}, []byte{0x01}, "1 < -2 = 0"),
		buildBinaryOpTest("SGT", byte(vm.SGT), []byte{0x01}, []byte{0xfe}, "-2 > 1 = 0"),
		buildBinaryOpTest("EQ", byte(vm.EQ), []byte{0x05}, []byte{0x05}, "5 == 5 = 1"),
		buildUnaryOpTest("ISZERO", byte(vm.ISZERO), []byte{0x00}, "ISZERO(0) = 1"),
	}
	runOpcodeTests(t, "Comparison", tests)
}

func TestMIRvsEVM_Bitwise(t *testing.T) {
	tests := []OpcodeTestCase{
		buildBinaryOpTest("AND", byte(vm.AND), []byte{0x0f}, []byte{0xff}, "0xff & 0x0f = 0x0f"),
		buildBinaryOpTest("OR", byte(vm.OR), []byte{0x0f}, []byte{0xf0}, "0xf0 | 0x0f = 0xff"),
		buildBinaryOpTest("XOR", byte(vm.XOR), []byte{0xff}, []byte{0x0f}, "0x0f ^ 0xff = 0xf0"),
		buildUnaryOpTest("NOT", byte(vm.NOT), []byte{0x00}, "NOT(0) = 0xff...ff"),
		buildBinaryOpTest("SHL", byte(vm.SHL), []byte{0x01}, []byte{0x01}, "1 << 1 = 2"),
		buildBinaryOpTest("SHR", byte(vm.SHR), []byte{0x02}, []byte{0x01}, "2 >> 1 = 1"),
	}
	runOpcodeTests(t, "Bitwise", tests)
}

func TestMIRvsEVM_Stack(t *testing.T) {
	tests := []OpcodeTestCase{
		buildStackOpTest("POP", byte(vm.POP), [][]byte{{0x01}, {0x02}}, "Pop 2, keep 1"),
		buildStackOpTest("DUP1", byte(vm.DUP1), [][]byte{{0x42}}, "Duplicate top"),
		buildStackOpTest("DUP2", byte(vm.DUP2), [][]byte{{0x01}, {0x02}}, "Duplicate 2nd"),
		buildStackOpTest("SWAP1", byte(vm.SWAP1), [][]byte{{0x01}, {0x02}}, "Swap top two"),
		buildStackOpTest("SWAP2", byte(vm.SWAP2), [][]byte{{0x01}, {0x02}, {0x03}}, "Swap top and 3rd"),
	}
	runOpcodeTests(t, "Stack", tests)
}

func TestMIRvsEVM_EnvironmentInfo(t *testing.T) {
	tests := []OpcodeTestCase{
		buildNullaryOpTest("ADDRESS", byte(vm.ADDRESS), "Get contract address"),
		buildNullaryOpTest("ORIGIN", byte(vm.ORIGIN), "Get tx origin"),
		buildNullaryOpTest("CALLER", byte(vm.CALLER), "Get caller"),
		buildNullaryOpTest("CALLVALUE", byte(vm.CALLVALUE), "Get call value"),
		buildNullaryOpTest("CODESIZE", byte(vm.CODESIZE), "Get code size"),
		buildNullaryOpTest("GASPRICE", byte(vm.GASPRICE), "Get gas price"),
		buildNullaryOpTest("RETURNDATASIZE", byte(vm.RETURNDATASIZE), "Get returndata size"),
	}
	runOpcodeTests(t, "EnvironmentInfo", tests)
}

func TestMIRvsEVM_BlockInfo(t *testing.T) {
	tests := []OpcodeTestCase{
		buildNullaryOpTest("COINBASE", byte(vm.COINBASE), "Get coinbase"),
		buildNullaryOpTest("TIMESTAMP", byte(vm.TIMESTAMP), "Get timestamp"),
		buildNullaryOpTest("NUMBER", byte(vm.NUMBER), "Get block number"),
		buildNullaryOpTest("DIFFICULTY", byte(vm.DIFFICULTY), "Get difficulty"),
		buildNullaryOpTest("GASLIMIT", byte(vm.GASLIMIT), "Get gas limit"),
		buildNullaryOpTest("CHAINID", byte(vm.CHAINID), "Get chain ID"),
	}
	runOpcodeTests(t, "BlockInfo", tests)
}

func TestMIRvsEVM_ControlFlow(t *testing.T) {
	tests := []OpcodeTestCase{
		buildNullaryOpTest("PC", byte(vm.PC), "Get PC"),
		buildNullaryOpTest("GAS", byte(vm.GAS), "Get remaining gas"),
	}
	runOpcodeTests(t, "ControlFlow", tests)
}

// Special opcodes with custom bytecode
func TestMIRvsEVM_SpecialOpcodes(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "BYTE",
			Bytecode:    []byte{0x7f, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x60, 0x00, 0x1a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "BYTE(0, 0xabcd...) = 0xab",
		},
		{
			Name:        "SAR",
			Bytecode:    []byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x60, 0x01, 0x1d, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "SAR arithmetic shift",
		},
		{
			Name:        "CALLDATASIZE",
			Bytecode:    []byte{0x36, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			Calldata:    []byte{0x01, 0x02, 0x03, 0x04},
			InitialGas:  100000,
			Description: "Get calldata size",
		},
		{
			Name:        "MSTORE_MLOAD",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0x60, 0x00, 0x51, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Store then load memory",
		},
	}
	runOpcodeTests(t, "SpecialOpcodes", tests)
}

func TestMIRvsEVM_Memory(t *testing.T) {
	tests := []OpcodeTestCase{
		// MLOAD/MSTORE - Dynamic gas: memory expansion (quadratic: newWords*3 + newWords^2/512)
		{
			Name:        "MLOAD_MSTORE_32Bytes",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0x60, 0x00, 0x51, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Store/Load at offset 0 (1 word) - mem expansion = 1*3 + 1^2/512 = 3",
		},
		// MSTORE8 - Dynamic gas: memory expansion
		{
			Name:        "MSTORE8_1Byte",
			Bytecode:    []byte{0x60, 0xff, 0x60, 0x00, 0x53, 0x60, 0x00, 0x51, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Store 1 byte at offset 0 - mem expansion = 1*3 + 1^2/512 = 3",
		},
		// Memory expansion test: larger offset causes quadratic growth
		{
			Name:        "MSTORE_LargeOffset",
			Bytecode:    []byte{0x60, 0x42, 0x61, 0x04, 0x00, 0x52, 0x60, 0x00, 0x51, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // MSTORE at offset 1024
			InitialGas:  100000,
			Description: "MSTORE at offset 1024 (33 words) - mem expansion = 33*3 + 33^2/512 = 99 + 2 = 101",
		},
		// Multiple memory expansions (test cumulative cost)
		{
			Name:        "MSTORE_MultipleExpansions",
			Bytecode:    []byte{0x60, 0x11, 0x60, 0x00, 0x52, 0x60, 0x22, 0x60, 0x20, 0x52, 0x60, 0x33, 0x60, 0x40, 0x52, 0x60, 0x00, 0x51, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "MSTORE at 0, 32, 64 (3 words) - cumulative mem expansion",
		},
		{
			Name:        "MSIZE",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0x59, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get memory size after MSTORE",
		},
	}
	runOpcodeTests(t, "Memory", tests)
}

func TestMIRvsEVM_PushOperations(t *testing.T) {
	// Generate tests for all PUSH1 through PUSH32
	tests := make([]OpcodeTestCase, 0, 32)
	for i := 1; i <= 32; i++ {
		tests = append(tests, buildPushTest(i))
	}
	runOpcodeTests(t, "PushOperations", tests)
}

func TestMIRvsEVM_DupOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		buildStackOpTest("DUP1", byte(vm.DUP1), [][]byte{{0x42}}, "Duplicate top"),
		buildStackOpTest("DUP2", byte(vm.DUP2), [][]byte{{0x01}, {0x02}}, "Duplicate 2nd"),
		buildStackOpTest("DUP3", byte(vm.DUP3), [][]byte{{0x01}, {0x02}, {0x03}}, "Duplicate 3rd"),
		buildStackOpTest("DUP4", byte(vm.DUP4), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}}, "Duplicate 4th"),
		buildStackOpTest("DUP5", byte(vm.DUP5), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}}, "Duplicate 5th"),
		buildStackOpTest("DUP6", byte(vm.DUP6), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}}, "Duplicate 6th"),
		buildStackOpTest("DUP7", byte(vm.DUP7), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}}, "Duplicate 7th"),
		buildStackOpTest("DUP8", byte(vm.DUP8), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}}, "Duplicate 8th"),
		buildStackOpTest("DUP9", byte(vm.DUP9), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}}, "Duplicate 9th"),
		buildStackOpTest("DUP10", byte(vm.DUP10), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}}, "Duplicate 10th"),
		buildStackOpTest("DUP11", byte(vm.DUP11), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}}, "Duplicate 11th"),
		buildStackOpTest("DUP12", byte(vm.DUP12), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}}, "Duplicate 12th"),
		buildStackOpTest("DUP13", byte(vm.DUP13), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}}, "Duplicate 13th"),
		buildStackOpTest("DUP14", byte(vm.DUP14), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}}, "Duplicate 14th"),
		buildStackOpTest("DUP15", byte(vm.DUP15), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}, {0x0f}}, "Duplicate 15th"),
		buildStackOpTest("DUP16", byte(vm.DUP16), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}, {0x0f}, {0x10}}, "Duplicate 16th"),
	}
	runOpcodeTests(t, "DupOperations", tests)
}

func TestMIRvsEVM_SwapOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		buildStackOpTest("SWAP1", byte(vm.SWAP1), [][]byte{{0x01}, {0x02}}, "Swap top two"),
		buildStackOpTest("SWAP2", byte(vm.SWAP2), [][]byte{{0x01}, {0x02}, {0x03}}, "Swap top and 3rd"),
		buildStackOpTest("SWAP3", byte(vm.SWAP3), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}}, "Swap top and 4th"),
		buildStackOpTest("SWAP4", byte(vm.SWAP4), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}}, "Swap top and 5th"),
		buildStackOpTest("SWAP5", byte(vm.SWAP5), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}}, "Swap top and 6th"),
		buildStackOpTest("SWAP6", byte(vm.SWAP6), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}}, "Swap top and 7th"),
		buildStackOpTest("SWAP7", byte(vm.SWAP7), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}}, "Swap top and 8th"),
		buildStackOpTest("SWAP8", byte(vm.SWAP8), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}}, "Swap top and 9th"),
		buildStackOpTest("SWAP9", byte(vm.SWAP9), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}}, "Swap top and 10th"),
		buildStackOpTest("SWAP10", byte(vm.SWAP10), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}}, "Swap top and 11th"),
		buildStackOpTest("SWAP11", byte(vm.SWAP11), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}}, "Swap top and 12th"),
		buildStackOpTest("SWAP12", byte(vm.SWAP12), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}}, "Swap top and 13th"),
		buildStackOpTest("SWAP13", byte(vm.SWAP13), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}}, "Swap top and 14th"),
		buildStackOpTest("SWAP14", byte(vm.SWAP14), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}, {0x0f}}, "Swap top and 15th"),
		buildStackOpTest("SWAP15", byte(vm.SWAP15), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}, {0x0f}, {0x10}}, "Swap top and 16th"),
		buildStackOpTest("SWAP16", byte(vm.SWAP16), [][]byte{{0x01}, {0x02}, {0x03}, {0x04}, {0x05}, {0x06}, {0x07}, {0x08}, {0x09}, {0x0a}, {0x0b}, {0x0c}, {0x0d}, {0x0e}, {0x0f}, {0x10}, {0x11}}, "Swap top and 17th"),
	}
	runOpcodeTests(t, "SwapOperations", tests)
}

func TestMIRvsEVM_Calldata(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "CALLDATALOAD",
			Bytecode:    []byte{0x60, 0x00, 0x35, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			Calldata:    []byte{0xaa, 0xbb, 0xcc, 0xdd, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			InitialGas:  100000,
			Description: "Load 32 bytes from calldata",
		},
		{
			Name:        "CALLDATACOPY",
			Bytecode:    []byte{0x60, 0x04, 0x60, 0x00, 0x60, 0x00, 0x37, 0x60, 0x20, 0x60, 0x00, 0xf3},
			Calldata:    []byte{0xaa, 0xbb, 0xcc, 0xdd},
			InitialGas:  100000,
			Description: "Copy 4 bytes from calldata to memory",
		},
		{
			Name:        "CALLDATASIZE",
			Bytecode:    []byte{0x36, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			Calldata:    []byte{0x01, 0x02, 0x03, 0x04},
			InitialGas:  100000,
			Description: "Get calldata size",
		},
	}
	runOpcodeTests(t, "Calldata", tests)
}

func TestMIRvsEVM_CodeOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "CODECOPY",
			Bytecode:    []byte{0x60, 0x08, 0x60, 0x00, 0x60, 0x20, 0x39, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Copy 8 bytes of code starting at position 0 to memory at offset 0x20",
		},
	}
	runOpcodeTests(t, "Code Operations", tests)
}

func TestMIRvsEVM_Crypto(t *testing.T) {
	tests := []OpcodeTestCase{
		// KECCAK256 - Dynamic gas: 30 + 6*words + memory expansion
		{
			Name:        "KECCAK256_32Bytes",
			Bytecode:    []byte{0x60, 0xaa, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0x20, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Hash 32 bytes (1 word) - dynamic gas = 30 + 6*1 + memExpansion = 36",
		},
		// KECCAK256 with 64 bytes (2 words)
		{
			Name:        "KECCAK256_64Bytes",
			Bytecode:    []byte{0x60, 0xaa, 0x60, 0x00, 0x52, 0x60, 0xbb, 0x60, 0x20, 0x52, 0x60, 0x40, 0x60, 0x00, 0x20, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Hash 64 bytes (2 words) - dynamic gas = 30 + 6*2 + memExpansion = 42",
		},
		// KECCAK256 with 128 bytes (4 words)
		{
			Name:        "KECCAK256_128Bytes",
			Bytecode:    []byte{0x60, 0xaa, 0x60, 0x00, 0x52, 0x60, 0x80, 0x60, 0x00, 0x20, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // hash 128 bytes
			InitialGas:  100000,
			Description: "Hash 128 bytes (4 words) - dynamic gas = 30 + 6*4 + memExpansion = 54",
		},
	}
	runOpcodeTests(t, "Crypto", tests)
}

func TestMIRvsEVM_Storage(t *testing.T) {
	tests := []OpcodeTestCase{
		// SLOAD - Dynamic gas: EIP-2929 warm (100) vs cold (2100)
		{
			Name:        "SLOAD_Single",
			Bytecode:    []byte{0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 0, SLOAD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
			InitialGas:  100000,
			Description: "Load from storage slot 0 once (cold access = 2100 gas)",
		},
		// SLOAD - Warm access test: second access to same slot costs less
		{
			Name:        "SLOAD_WarmAccess",
			Bytecode:    []byte{0x60, 0x00, 0x54, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 0, SLOAD, PUSH1 0, SLOAD, ...
			InitialGas:  100000,
			Description: "Load same slot twice: first=cold(2100), second=warm(100)",
		},
		// SSTORE - Dynamic gas: depends on original/current/new value (9 cases in EIP-2200)
		// Case 1: 0 → non-zero (first write to empty slot = 20000 gas)
		{
			Name:        "SSTORE_ZeroToNonZero",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x55, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 0x42, PUSH1 0, SSTORE, ...
			InitialGas:  100000,
			Description: "Store 0x42 in empty slot 0 (0→non-zero = 20000 gas)",
		},
		// Case 2: Modify existing non-zero value (5000 gas)
		{
			Name:        "SSTORE_ModifyNonZero",
			Bytecode:    []byte{0x60, 0x11, 0x60, 0x00, 0x55, 0x60, 0x22, 0x60, 0x00, 0x55, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  150000,
			Description: "Store 0x11 then 0x22 in slot 0 (modify existing = 5000 gas)",
		},
		// Case 3: Set to zero (delete, 5000 gas + refund)
		{
			Name:        "SSTORE_SetToZero",
			Bytecode:    []byte{0x60, 0x11, 0x60, 0x00, 0x55, 0x60, 0x00, 0x60, 0x00, 0x55, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  150000,
			Description: "Store 0x11 then 0 in slot 0 (delete = 5000 gas + refund)",
		},
		// Case 4: No-op (same value, 200 gas after EIP-2200)
		{
			Name:        "SSTORE_NoOp",
			Bytecode:    []byte{0x60, 0x11, 0x60, 0x00, 0x55, 0x60, 0x11, 0x60, 0x00, 0x55, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  150000,
			Description: "Store 0x11 twice in slot 0 (no-op = 200 gas)",
		},
	}
	runOpcodeTests(t, "Storage", tests)
}

func TestMIRvsEVM_TransientStorage(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "TLOAD",
			Bytecode:    []byte{0x60, 0x00, 0x5c, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Load from transient storage slot 0",
		},
		{
			Name:        "TSTORE",
			Bytecode:    []byte{0x60, 0x99, 0x60, 0x00, 0x5d, 0x60, 0x00, 0x5c, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Store 0x99 in transient slot 0, then load it",
		},
	}
	runOpcodeTests(t, "TransientStorage", tests)
}

func TestMIRvsEVM_MemoryCopy(t *testing.T) {
	tests := []OpcodeTestCase{
		// MCOPY - Dynamic gas: 3 + 3*words + memory expansion
		{
			Name:        "MCOPY_Small",
			Bytecode:    []byte{0x60, 0xaa, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0x60, 0x20, 0x5e, 0x60, 0x20, 0x60, 0x20, 0xf3},
			InitialGas:  100000,
			Description: "Copy 32 bytes (1 word) - dynamic gas = 3 + 3*1 + memExpansion",
		},
		// MCOPY with larger size
		{
			Name:        "MCOPY_64Bytes",
			Bytecode:    []byte{0x60, 0xaa, 0x60, 0x00, 0x52, 0x60, 0x40, 0x60, 0x00, 0x60, 0x40, 0x5e, 0x60, 0x20, 0x60, 0x00, 0xf3}, // copy 64 bytes = 2 words
			InitialGas:  100000,
			Description: "Copy 64 bytes (2 words) - dynamic gas = 3 + 3*2 + memExpansion",
		},
		// CALLDATACOPY - Dynamic gas: 3 + 3*words + memory expansion
		{
			Name:        "CALLDATACOPY_32Bytes",
			Bytecode:    []byte{0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x37, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 32, PUSH1 0, PUSH1 0, CALLDATACOPY, RETURN
			Calldata:    []byte{0xaa, 0xbb, 0xcc, 0xdd},
			InitialGas:  100000,
			Description: "Copy 32 bytes from calldata - dynamic gas = 3 + 3*1 + memExpansion",
		},
		// CODECOPY - Dynamic gas: 3 + 3*words + memory expansion
		{
			Name:        "CODECOPY_32Bytes",
			Bytecode:    []byte{0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x39, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 32, PUSH1 0, PUSH1 0, CODECOPY, RETURN
			InitialGas:  100000,
			Description: "Copy 32 bytes from code - dynamic gas = 3 + 3*1 + memExpansion",
		},
		// RETURNDATACOPY - Dynamic gas: 3 + 3*words + memory expansion
		{
			Name:        "RETURNDATACOPY",
			Bytecode:    []byte{0x60, 0x00, 0x3d, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}, // PUSH1 0, RETURNDATASIZE, PUSH1 0, MSTORE, RETURN
			InitialGas:  100000,
			Description: "RETURNDATASIZE (no copy, just measure) - dynamic gas for RETURNDATASIZE = 3",
		},
	}
	runOpcodeTests(t, "MemoryCopy", tests)
}

func TestMIRvsEVM_BlobOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "BLOBHASH",
			Bytecode:    []byte{0x60, 0x00, 0x49, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get blob hash at index 0",
		},
		{
			Name:        "BLOBBASEFEE",
			Bytecode:    []byte{0x4a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get blob base fee",
		},
	}
	runOpcodeTests(t, "BlobOperations", tests)
}

func TestMIRvsEVM_MissingOpcodes(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "STOP",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0x00},
			InitialGas:  100000,
			Description: "STOP execution",
		},
		{
			Name:        "CODECOPY",
			Bytecode:    []byte{0x60, 0x04, 0x60, 0x00, 0x60, 0x00, 0x39, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Copy code to memory",
		},
		{
			Name:        "RETURNDATACOPY",
			Bytecode:    []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x3e, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Copy returndata to memory (empty)",
		},
		{
			Name:        "BLOCKHASH",
			Bytecode:    []byte{0x60, 0x00, 0x40, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get block hash",
		},
		{
			Name:        "SELFBALANCE",
			Bytecode:    []byte{0x47, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get self balance",
		},
		{
			Name:        "BASEFEE",
			Bytecode:    []byte{0x48, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Get base fee",
		},
		{
			Name:        "PUSH0",
			Bytecode:    []byte{0x5f, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "Push 0",
		},
		{
			Name:        "JUMPDEST",
			Bytecode:    []byte{0x60, 0x05, 0x56, 0x60, 0x42, 0x5b, 0x60, 0x42, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			InitialGas:  100000,
			Description: "JUMP to JUMPDEST",
		},
	}
	runOpcodeTests(t, "MissingOpcodes", tests)
}

func TestMIRvsEVM_InvalidOpcode(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name:        "INVALID",
			Bytecode:    []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0xfe}, // PUSH1 0x42, PUSH1 0x00, MSTORE, INVALID
			InitialGas:  100000,
			Description: "INVALID opcode should consume all gas",
		},
	}
	runOpcodeTests(t, "InvalidOpcode", tests)
}

func TestMIRvsEVM_JumpOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name: "JUMP_Basic",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x07, // PC=0: push jump target (PC=7)
				byte(vm.JUMP),        // PC=2: JUMP to PC=7
				byte(vm.PUSH1), 0x42, // PC=3: PUSH1 0x42 - SKIPPED
				byte(vm.PUSH1), 0x00, // PC=5: PUSH1 0x00 - SKIPPED
				byte(vm.JUMPDEST),    // PC=7: JUMPDEST - jump target
				byte(vm.PUSH1), 0x99, // PC=8: push 0x99
				byte(vm.PUSH1), 0x00, // PC=10: push 0
				byte(vm.MSTORE),      // PC=12: MSTORE
				byte(vm.PUSH1), 0x20, // PC=13: push 32
				byte(vm.PUSH1), 0x00, // PC=15: push 0
				byte(vm.RETURN), // PC=17: RETURN
			},
			InitialGas:  100000,
			Description: "Basic JUMP to JUMPDEST",
		},
		{
			Name: "JUMP_Forward",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x09, // PC=0: push jump target (PC=9)
				byte(vm.JUMP),        // PC=2: JUMP forward
				byte(vm.PUSH1), 0x11, // PC=3-8: SKIPPED instructions
				byte(vm.PUSH1), 0x22,
				byte(vm.PUSH1), 0x33,
				byte(vm.JUMPDEST),    // PC=9: JUMPDEST
				byte(vm.PUSH1), 0xaa, // PC=10: push 0xaa
				byte(vm.PUSH1), 0x00, // PC=12: push 0
				byte(vm.MSTORE),      // PC=14: MSTORE
				byte(vm.PUSH1), 0x20, // PC=15: push 32
				byte(vm.PUSH1), 0x00, // PC=17: push 0
				byte(vm.RETURN), // PC=19: RETURN
			},
			InitialGas:  100000,
			Description: "JUMP forward skipping multiple instructions",
		},
		{
			Name: "JUMP_Loop3",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x03, // PC=0: push 3 (counter)
				byte(vm.JUMPDEST),    // PC=2: JUMPDEST (loop start)
				byte(vm.PUSH1), 0x01, // PC=3: push 1
				byte(vm.SWAP1),       // PC=5: SWAP1
				byte(vm.SUB),         // PC=6: SUB (counter - 1)
				byte(vm.DUP1),        // PC=7: DUP1
				byte(vm.ISZERO),      // PC=8: check if zero
				byte(vm.PUSH1), 0x0f, // PC=9: push exit target (PC=15)
				byte(vm.JUMPI),       // PC=11: JUMPI (exit if zero)
				byte(vm.PUSH1), 0x02, // PC=12: push loop start (PC=2)
				byte(vm.JUMP),        // PC=14: JUMP backward to loop
				byte(vm.JUMPDEST),    // PC=15: JUMPDEST (exit)
				byte(vm.PUSH1), 0x00, // PC=16: push 0
				byte(vm.MSTORE),      // PC=18: MSTORE
				byte(vm.PUSH1), 0x20, // PC=19: push 32
				byte(vm.PUSH1), 0x00, // PC=21: push 0
				byte(vm.RETURN), // PC=23: RETURN
			},
			InitialGas:  100000,
			Description: "JUMP backward loop (3 iterations)",
		},
		{
			Name: "JUMP_Loop5",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x05, // PC=0: push 5 (counter)
				byte(vm.JUMPDEST),    // PC=2: JUMPDEST (loop start)
				byte(vm.PUSH1), 0x01, // PC=3: push 1
				byte(vm.SWAP1),       // PC=5: SWAP1
				byte(vm.SUB),         // PC=6: SUB (counter - 1)
				byte(vm.DUP1),        // PC=7: DUP1
				byte(vm.ISZERO),      // PC=8: check if zero
				byte(vm.PUSH1), 0x0f, // PC=9: push exit target (PC=15)
				byte(vm.JUMPI),       // PC=11: JUMPI (exit if zero)
				byte(vm.PUSH1), 0x02, // PC=12: push loop start (PC=2)
				byte(vm.JUMP),        // PC=14: JUMP backward to loop
				byte(vm.JUMPDEST),    // PC=15: JUMPDEST (exit)
				byte(vm.PUSH1), 0x00, // PC=16: push 0
				byte(vm.MSTORE),      // PC=18: MSTORE
				byte(vm.PUSH1), 0x20, // PC=19: push 32
				byte(vm.PUSH1), 0x00, // PC=21: push 0
				byte(vm.RETURN), // PC=23: RETURN
			},
			InitialGas:  100000,
			Description: "JUMP backward loop (5 iterations)",
		},
	}
	runOpcodeTests(t, "JumpOperations", tests)
}

func TestMIRvsEVM_ReturnOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name: "RETURN_Empty",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x00, // PC=0: push 0 (size)
				byte(vm.PUSH1), 0x00, // PC=2: push 0 (offset)
				byte(vm.RETURN), // PC=4: RETURN (empty)
			},
			InitialGas:  100000,
			Description: "RETURN with empty data",
		},
		{
			Name: "RETURN_SingleByte",
			Bytecode: []byte{
				byte(vm.PUSH1), 0xff, // PC=0: push 0xff
				byte(vm.PUSH1), 0x00, // PC=2: push 0
				byte(vm.MSTORE),      // PC=4: MSTORE
				byte(vm.PUSH1), 0x01, // PC=5: push 1 (size)
				byte(vm.PUSH1), 0x1f, // PC=7: push 31 (offset, last byte of word)
				byte(vm.RETURN), // PC=9: RETURN
			},
			InitialGas:  100000,
			Description: "RETURN single byte",
		},
		{
			Name: "RETURN_LargeData",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x11, // PC=0: push 0x11
				byte(vm.PUSH1), 0x00, // PC=2: push 0
				byte(vm.MSTORE),      // PC=4: MSTORE at offset 0
				byte(vm.PUSH1), 0x22, // PC=5: push 0x22
				byte(vm.PUSH1), 0x20, // PC=7: push 32
				byte(vm.MSTORE),      // PC=9: MSTORE at offset 32
				byte(vm.PUSH1), 0x33, // PC=10: push 0x33
				byte(vm.PUSH1), 0x40, // PC=12: push 64
				byte(vm.MSTORE),      // PC=14: MSTORE at offset 64
				byte(vm.PUSH1), 0x60, // PC=15: push 96 (size)
				byte(vm.PUSH1), 0x00, // PC=17: push 0 (offset)
				byte(vm.RETURN), // PC=19: RETURN 96 bytes
			},
			InitialGas:  100000,
			Description: "RETURN large data (96 bytes)",
		},
		{
			Name: "RETURN_AfterComputation",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x05, // PC=0: push 5
				byte(vm.PUSH1), 0x03, // PC=2: push 3
				byte(vm.ADD),         // PC=4: ADD (5 + 3 = 8)
				byte(vm.PUSH1), 0x02, // PC=5: push 2
				byte(vm.MUL),         // PC=7: MUL (8 * 2 = 16)
				byte(vm.PUSH1), 0x00, // PC=8: push 0
				byte(vm.MSTORE),      // PC=10: MSTORE
				byte(vm.PUSH1), 0x20, // PC=11: push 32
				byte(vm.PUSH1), 0x00, // PC=13: push 0
				byte(vm.RETURN), // PC=15: RETURN
			},
			InitialGas:  100000,
			Description: "RETURN after arithmetic computation",
		},
	}
	runOpcodeTests(t, "ReturnOperations", tests)
}

func TestMIRvsEVM_RevertOperations(t *testing.T) {
	tests := []OpcodeTestCase{
		{
			Name: "REVERT_Empty",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x00, // PC=0: push 0 (size)
				byte(vm.PUSH1), 0x00, // PC=2: push 0 (offset)
				byte(vm.REVERT), // PC=4: REVERT (empty)
			},
			InitialGas:  100000,
			Description: "REVERT with empty data",
		},
		{
			Name: "REVERT_WithReason",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x42, // PC=0: push 0x42 (error reason)
				byte(vm.PUSH1), 0x00, // PC=2: push 0
				byte(vm.MSTORE),      // PC=4: MSTORE
				byte(vm.PUSH1), 0x20, // PC=5: push 32 (size)
				byte(vm.PUSH1), 0x00, // PC=7: push 0 (offset)
				byte(vm.REVERT), // PC=9: REVERT with reason
			},
			InitialGas:  100000,
			Description: "REVERT with error reason",
		},
		{
			Name: "REVERT_AfterStorage",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x99, // PC=0: push 0x99
				byte(vm.PUSH1), 0x00, // PC=2: push 0
				byte(vm.SSTORE),      // PC=4: SSTORE (will be reverted)
				byte(vm.PUSH1), 0xee, // PC=5: push 0xee
				byte(vm.PUSH1), 0x00, // PC=7: push 0
				byte(vm.MSTORE),      // PC=9: MSTORE
				byte(vm.PUSH1), 0x20, // PC=10: push 32
				byte(vm.PUSH1), 0x00, // PC=12: push 0
				byte(vm.REVERT), // PC=14: REVERT
			},
			InitialGas:  100000,
			Description: "REVERT after storage modification",
		},
		{
			Name: "REVERT_ConditionalRevert",
			Bytecode: []byte{
				byte(vm.PUSH1), 0x01, // PC=0: push 1 (condition)
				byte(vm.PUSH1), 0x09, // PC=2: push revert target (PC=9)
				byte(vm.JUMPI),       // PC=4: JUMPI (will jump)
				byte(vm.PUSH1), 0x11, // PC=5: push 0x11 - SKIPPED
				byte(vm.PUSH1), 0x00, // PC=7: push 0 - SKIPPED
				byte(vm.JUMPDEST),    // PC=9: JUMPDEST
				byte(vm.PUSH1), 0xff, // PC=10: push 0xff
				byte(vm.PUSH1), 0x00, // PC=12: push 0
				byte(vm.MSTORE),      // PC=14: MSTORE
				byte(vm.PUSH1), 0x20, // PC=15: push 32
				byte(vm.PUSH1), 0x00, // PC=17: push 0
				byte(vm.REVERT), // PC=19: REVERT
			},
			InitialGas:  100000,
			Description: "REVERT after conditional jump",
		},
	}
	runOpcodeTests(t, "RevertOperations", tests)
}

func TestMIRvsEVM_Summary(t *testing.T) {
	separator := strings.Repeat("=", 80)
	fmt.Println("\n" + separator)
	fmt.Println("📊 MIR vs EVM Simple Opcodes Comparison (Builder Pattern)")
	fmt.Println(separator)
	fmt.Println("Covered: 109 opcodes with builder pattern")
	fmt.Println("Categories:")
	fmt.Println("  - Arithmetic (11), Comparison (6), Bitwise (6)")
	fmt.Println("  - Stack: POP, DUP1-DUP16 (16), SWAP1-SWAP16 (16)")
	fmt.Println("  - Push: PUSH1-PUSH32 (32 complete)")
	fmt.Println("  - Memory (3), Calldata (3), Crypto (1)")
	fmt.Println("  - Environment (7), Block (6), ControlFlow (2)")
	fmt.Println("  - Storage (2), TransientStorage (2)")
	fmt.Println("  - MemoryCopy (1), Blob (2), Special (4)")
	fmt.Println("")
	fmt.Println("Run all tests:")
	fmt.Println("  go test ./tests -v -run '^TestMIRvsEVM_'")
	fmt.Println(separator)
}
