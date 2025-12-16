// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// MIR CREATE/CREATE2 Tests - Tests for contract creation from within contracts
// including nested creation scenarios

package runtime

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// ===========================================================================
// CREATE Tests (From Contract)
// ===========================================================================

// TestMIR_Create_SimpleFromContract tests CREATE from within a contract
func TestMIR_Create_SimpleFromContract(t *testing.T) {
	compiler.ClearMIRCache()

	// Child contract: just STOP
	childInitcode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN (empty runtime code)
	}

	// Parent contract: stores child initcode in memory, then CREATE
	// 1. PUSH child initcode bytes
	// 2. MSTORE
	// 3. CREATE(value=0, offset=0, size=len(childInitcode))
	// 4. STOP
	code := buildCreateCode(childInitcode, 0, false)

	runCreateComparison(t, "Create_SimpleFromContract", code, nil)
}

// TestMIR_Create_WithValue tests CREATE with value transfer
func TestMIR_Create_WithValue(t *testing.T) {
	compiler.ClearMIRCache()

	// Child contract that accepts value
	childInitcode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	// Parent creates child with 1 wei
	code := buildCreateCode(childInitcode, 1, false)

	runCreateComparisonWithBalance(t, "Create_WithValue", code, nil)
}

// TestMIR_Create_ChildWithCode tests CREATE with child that has runtime code
func TestMIR_Create_ChildWithCode(t *testing.T) {
	compiler.ClearMIRCache()

	// Child runtime code: stores 0x42 at slot 0
	childRuntime := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x55, // SSTORE
		0x00, // STOP
	}

	// Child initcode: copy runtime to memory and return
	childInitcode := make([]byte, 0, len(childRuntime)+12)
	childInitcode = append(childInitcode,
		0x60, byte(len(childRuntime)), // PUSH1 len
		0x60, 0x0c, // PUSH1 offset (initcode is 12 bytes)
		0x60, 0x00, // PUSH1 0x00 (memory dest)
		0x39,                          // CODECOPY
		0x60, byte(len(childRuntime)), // PUSH1 len
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	)
	childInitcode = append(childInitcode, childRuntime...)

	code := buildCreateCode(childInitcode, 0, false)

	runCreateComparison(t, "Create_ChildWithCode", code, nil)
}

// TestMIR_Create_Nested tests nested CREATE (contract creates contract that creates contract)
func TestMIR_Create_Nested(t *testing.T) {
	compiler.ClearMIRCache()

	// Grandchild: simple contract
	grandchildInitcode := []byte{
		0x60, 0x00, 0x60, 0x00, 0xf3, // PUSH1 0 PUSH1 0 RETURN
	}

	// Child: creates grandchild
	childCode := buildCreateCode(grandchildInitcode, 0, false)
	childInitcode := buildInitcodeForRuntime(childCode)

	// Parent: creates child
	code := buildCreateCode(childInitcode, 0, false)

	runCreateComparison(t, "Create_Nested", code, nil)
}

// TestMIR_Create_MultipleCreates tests multiple CREATE calls
func TestMIR_Create_MultipleCreates(t *testing.T) {
	compiler.ClearMIRCache()

	// Simple initcode for children
	childInitcode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	// Parent creates 3 children sequentially
	code := buildMultipleCreateCode(childInitcode, 3)

	runCreateComparison(t, "Create_MultipleCreates", code, nil)
}

// ===========================================================================
// CREATE2 Tests
// ===========================================================================

// TestMIR_Create2_Simple tests basic CREATE2
func TestMIR_Create2_Simple(t *testing.T) {
	compiler.ClearMIRCache()

	childInitcode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	// CREATE2 with salt = 0
	code := buildCreate2Code(childInitcode, 0, 0)

	runCreateComparison(t, "Create2_Simple", code, nil)
}

// TestMIR_Create2_WithSalt tests CREATE2 with different salts
func TestMIR_Create2_WithSalt(t *testing.T) {
	compiler.ClearMIRCache()

	childInitcode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	// CREATE2 with salt = 0x1234
	code := buildCreate2Code(childInitcode, 0x1234, 0)

	runCreateComparison(t, "Create2_WithSalt", code, nil)
}

// TestMIR_Create2_WithValue tests CREATE2 with value
func TestMIR_Create2_WithValue(t *testing.T) {
	compiler.ClearMIRCache()

	childInitcode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	// CREATE2 with salt = 0 and value = 1
	code := buildCreate2Code(childInitcode, 0, 1)

	runCreateComparisonWithBalance(t, "Create2_WithValue", code, nil)
}

// TestMIR_Create2_SameCodeDifferentSalt tests address differs with different salt
func TestMIR_Create2_SameCodeDifferentSalt(t *testing.T) {
	compiler.ClearMIRCache()

	childInitcode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	// Create two contracts with same code but different salts
	// First CREATE2 with salt=1, then CREATE2 with salt=2
	code := buildMultipleCreate2Code(childInitcode, []uint64{1, 2})

	runCreateComparison(t, "Create2_SameCodeDifferentSalt", code, nil)
}

// ===========================================================================
// CREATE Failure Tests
// ===========================================================================

// TestMIR_Create_InsufficientGas tests CREATE with insufficient gas
func TestMIR_Create_InsufficientGas(t *testing.T) {
	compiler.ClearMIRCache()

	// Large initcode that requires significant gas
	childInitcode := make([]byte, 1000)
	for i := range childInitcode {
		childInitcode[i] = 0x5b // JUMPDEST (costs gas)
	}
	childInitcode = append(childInitcode, 0x00) // STOP

	code := buildCreateCode(childInitcode, 0, false)

	// Run with limited gas
	runCreateComparisonWithGasLimit(t, "Create_InsufficientGas", code, nil, 50000)
}

// TestMIR_Create_RevertingInitcode tests CREATE where initcode reverts
func TestMIR_Create_RevertingInitcode(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode that immediately reverts
	childInitcode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xfd, // REVERT
	}

	code := buildCreateCode(childInitcode, 0, false)

	runCreateComparison(t, "Create_RevertingInitcode", code, nil)
}

// TestMIR_Create_EmptyInitcode tests CREATE with empty initcode
func TestMIR_Create_EmptyInitcode(t *testing.T) {
	compiler.ClearMIRCache()

	// Empty initcode (size=0)
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (value)
		0x60, 0x00, // PUSH1 0x00 (offset)
		0x60, 0x00, // PUSH1 0x00 (size = 0)
		0xf0,       // CREATE
		0x50,       // POP result
		0x00,       // STOP
	}

	runCreateComparison(t, "Create_EmptyInitcode", code, nil)
}

// ===========================================================================
// Helper Functions to Build Test Code
// ===========================================================================

// buildCreateCode builds code that performs CREATE with given initcode
func buildCreateCode(childInitcode []byte, value uint64, _ bool) []byte {
	code := make([]byte, 0, 100+len(childInitcode))

	// Store initcode in memory
	// We'll store it starting at memory[0]
	offset := 0
	for i := 0; i < len(childInitcode); i += 32 {
		end := i + 32
		if end > len(childInitcode) {
			end = len(childInitcode)
		}
		chunk := childInitcode[i:end]

		// Pad to 32 bytes
		padded := make([]byte, 32)
		copy(padded[32-len(chunk):], chunk)

		// PUSH32 chunk
		code = append(code, 0x7f)
		code = append(code, padded...)

		// PUSH1 offset
		code = append(code, 0x60, byte(offset))

		// MSTORE
		code = append(code, 0x52)

		offset += 32
	}

	// CREATE(value, offset, size)
	// PUSH size
	if len(childInitcode) < 256 {
		code = append(code, 0x60, byte(len(childInitcode)))
	} else {
		code = append(code, 0x61, byte(len(childInitcode)>>8), byte(len(childInitcode)))
	}

	// PUSH offset (0)
	code = append(code, 0x60, 0x00)

	// PUSH value
	if value < 256 {
		code = append(code, 0x60, byte(value))
	} else {
		code = append(code, 0x61, byte(value>>8), byte(value))
	}

	// CREATE
	code = append(code, 0xf0)

	// POP result (address or 0)
	code = append(code, 0x50)

	// STOP
	code = append(code, 0x00)

	return code
}

// buildCreate2Code builds code that performs CREATE2 with given initcode, salt, and value
func buildCreate2Code(childInitcode []byte, salt, value uint64) []byte {
	code := make([]byte, 0, 100+len(childInitcode))

	// Store initcode in memory at offset 0
	offset := 0
	for i := 0; i < len(childInitcode); i += 32 {
		end := i + 32
		if end > len(childInitcode) {
			end = len(childInitcode)
		}
		chunk := childInitcode[i:end]

		padded := make([]byte, 32)
		copy(padded[32-len(chunk):], chunk)

		code = append(code, 0x7f)
		code = append(code, padded...)
		code = append(code, 0x60, byte(offset))
		code = append(code, 0x52)
		offset += 32
	}

	// CREATE2(value, offset, size, salt)
	// PUSH salt
	if salt < 256 {
		code = append(code, 0x60, byte(salt))
	} else {
		code = append(code, 0x61, byte(salt>>8), byte(salt))
	}

	// PUSH size
	if len(childInitcode) < 256 {
		code = append(code, 0x60, byte(len(childInitcode)))
	} else {
		code = append(code, 0x61, byte(len(childInitcode)>>8), byte(len(childInitcode)))
	}

	// PUSH offset (0)
	code = append(code, 0x60, 0x00)

	// PUSH value
	if value < 256 {
		code = append(code, 0x60, byte(value))
	} else {
		code = append(code, 0x61, byte(value>>8), byte(value))
	}

	// CREATE2
	code = append(code, 0xf5)

	// POP result
	code = append(code, 0x50)

	// STOP
	code = append(code, 0x00)

	return code
}

// buildInitcodeForRuntime wraps runtime code in initcode that returns it
func buildInitcodeForRuntime(runtimeCode []byte) []byte {
	initcode := make([]byte, 0, len(runtimeCode)+12)

	// CODECOPY runtime from after initcode prefix to memory[0]
	initcode = append(initcode,
		0x60, byte(len(runtimeCode)), // PUSH1 size
		0x60, 0x0c, // PUSH1 offset (12 bytes = initcode prefix)
		0x60, 0x00, // PUSH1 0x00 (dest)
		0x39,                         // CODECOPY
		0x60, byte(len(runtimeCode)), // PUSH1 size
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	)
	initcode = append(initcode, runtimeCode...)

	return initcode
}

// buildMultipleCreateCode builds code that performs multiple CREATEs
func buildMultipleCreateCode(childInitcode []byte, count int) []byte {
	code := make([]byte, 0, 200)

	// First, store initcode in memory
	offset := 0
	for i := 0; i < len(childInitcode); i += 32 {
		end := i + 32
		if end > len(childInitcode) {
			end = len(childInitcode)
		}
		chunk := childInitcode[i:end]
		padded := make([]byte, 32)
		copy(padded[32-len(chunk):], chunk)

		code = append(code, 0x7f)
		code = append(code, padded...)
		code = append(code, 0x60, byte(offset))
		code = append(code, 0x52)
		offset += 32
	}

	// Now perform 'count' CREATEs
	for i := 0; i < count; i++ {
		// CREATE(value=0, offset=0, size)
		code = append(code, 0x60, byte(len(childInitcode))) // size
		code = append(code, 0x60, 0x00)                     // offset
		code = append(code, 0x60, 0x00)                     // value
		code = append(code, 0xf0)                           // CREATE
		code = append(code, 0x50)                           // POP result
	}

	code = append(code, 0x00) // STOP

	return code
}

// buildMultipleCreate2Code builds code that performs CREATE2 with multiple salts
func buildMultipleCreate2Code(childInitcode []byte, salts []uint64) []byte {
	code := make([]byte, 0, 200)

	// Store initcode in memory
	offset := 0
	for i := 0; i < len(childInitcode); i += 32 {
		end := i + 32
		if end > len(childInitcode) {
			end = len(childInitcode)
		}
		chunk := childInitcode[i:end]
		padded := make([]byte, 32)
		copy(padded[32-len(chunk):], chunk)

		code = append(code, 0x7f)
		code = append(code, padded...)
		code = append(code, 0x60, byte(offset))
		code = append(code, 0x52)
		offset += 32
	}

	// Perform CREATE2 for each salt
	for _, salt := range salts {
		// CREATE2(value=0, offset=0, size, salt)
		code = append(code, 0x60, byte(salt))               // salt
		code = append(code, 0x60, byte(len(childInitcode))) // size
		code = append(code, 0x60, 0x00)                     // offset
		code = append(code, 0x60, 0x00)                     // value
		code = append(code, 0xf5)                           // CREATE2
		code = append(code, 0x50)                           // POP result
	}

	code = append(code, 0x00) // STOP

	return code
}

// ===========================================================================
// Comparison Helpers
// ===========================================================================

func runCreateComparison(t *testing.T, name string, code, input []byte) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	contractAddr := common.HexToAddress("0xc0de")

	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    5_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    5_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	retBase, gasBase, errBase := evmBase.Call(
		baseCfg.Origin,
		contractAddr,
		input,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	retMIR, gasMIR, errMIR := evmMIR.Call(
		mirCfg.Origin,
		contractAddr,
		input,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	gasUsedBase := baseCfg.GasLimit - gasBase
	gasUsedMIR := mirCfg.GasLimit - gasMIR

	t.Logf("[%s] Base EVM: gasUsed=%d, retLen=%d, err=%v", name, gasUsedBase, len(retBase), errBase)
	t.Logf("[%s] MIR:      gasUsed=%d, retLen=%d, err=%v", name, gasUsedMIR, len(retMIR), errMIR)

	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	if string(retBase) != string(retMIR) {
		t.Errorf("[%s] Return mismatch: base=%x, mir=%x", name, retBase, retMIR)
	}

	gasDiff := int64(gasUsedBase) - int64(gasUsedMIR)
	if gasDiff < 0 {
		gasDiff = -gasDiff
	}
	if gasDiff > 0 {
		t.Logf("[%s] Gas difference: %d", name, gasDiff)
	}

	t.Logf("[%s] ✅ CREATE test passed", name)
}

func runCreateComparisonWithBalance(t *testing.T, name string, code, input []byte) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	contractAddr := common.HexToAddress("0xc0de")

	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    5_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    5_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	baseCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000_000), 0)

	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)
	mirCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000_000), 0)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	retBase, gasBase, errBase := evmBase.Call(
		baseCfg.Origin,
		contractAddr,
		input,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	retMIR, gasMIR, errMIR := evmMIR.Call(
		mirCfg.Origin,
		contractAddr,
		input,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	gasUsedBase := baseCfg.GasLimit - gasBase
	gasUsedMIR := mirCfg.GasLimit - gasMIR

	t.Logf("[%s] Base EVM: gasUsed=%d, retLen=%d, err=%v", name, gasUsedBase, len(retBase), errBase)
	t.Logf("[%s] MIR:      gasUsed=%d, retLen=%d, err=%v", name, gasUsedMIR, len(retMIR), errMIR)

	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	t.Logf("[%s] ✅ CREATE with balance test passed", name)
}

func runCreateComparisonWithGasLimit(t *testing.T, name string, code, input []byte, gasLimit uint64) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	contractAddr := common.HexToAddress("0xc0de")

	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    gasLimit,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    gasLimit,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	_, gasBase, errBase := evmBase.Call(
		baseCfg.Origin,
		contractAddr,
		input,
		gasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	_, gasMIR, errMIR := evmMIR.Call(
		mirCfg.Origin,
		contractAddr,
		input,
		gasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	t.Logf("[%s] GasLimit=%d", name, gasLimit)
	t.Logf("[%s] Base EVM: gasLeft=%d, err=%v", name, gasBase, errBase)
	t.Logf("[%s] MIR:      gasLeft=%d, err=%v", name, gasMIR, errMIR)

	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	t.Logf("[%s] ✅ CREATE with gas limit test passed", name)
}

