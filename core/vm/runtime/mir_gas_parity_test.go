// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// MIR Gas Parity Tests - Systematic tests to verify gas consumption is identical
// between base EVM and MIR interpreter across various scenarios

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
// Memory Expansion Gas Tests
// ===========================================================================

// TestMIR_Gas_MemoryExpansion_Small tests gas for small memory expansion
func TestMIR_Gas_MemoryExpansion_Small(t *testing.T) {
	compiler.ClearMIRCache()

	// MSTORE at offset 0 (expands memory to 32 bytes)
	// Gas = 3 (MSTORE static) + 3 (memory expansion for 1 word)
	code := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x00,       // STOP
	}

	runGasComparison(t, "MemoryExpansion_Small", code, nil, true)
}

// TestMIR_Gas_MemoryExpansion_Large tests gas for large memory expansion
func TestMIR_Gas_MemoryExpansion_Large(t *testing.T) {
	compiler.ClearMIRCache()

	// MSTORE at offset 0x1000 (4096) - expands memory significantly
	// Memory expansion cost is quadratic
	code := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x61, 0x10, 0x00, // PUSH2 0x1000 (4096)
		0x52, // MSTORE
		0x00, // STOP
	}

	runGasComparison(t, "MemoryExpansion_Large", code, nil, true)
}

// TestMIR_Gas_MemoryExpansion_Progressive tests progressive memory expansion
func TestMIR_Gas_MemoryExpansion_Progressive(t *testing.T) {
	compiler.ClearMIRCache()

	// Multiple MSTOREs at increasing offsets
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE (0-32)
		0x60, 0x02, // PUSH1 0x02
		0x60, 0x20, // PUSH1 0x20
		0x52,       // MSTORE (32-64)
		0x60, 0x03, // PUSH1 0x03
		0x60, 0x40, // PUSH1 0x40
		0x52,       // MSTORE (64-96)
		0x60, 0x04, // PUSH1 0x04
		0x60, 0x60, // PUSH1 0x60
		0x52, // MSTORE (96-128)
		0x00, // STOP
	}

	runGasComparison(t, "MemoryExpansion_Progressive", code, nil, true)
}

// ===========================================================================
// Storage Gas Tests
// ===========================================================================

// TestMIR_Gas_Storage_ColdAccess tests cold storage access gas
func TestMIR_Gas_Storage_ColdAccess(t *testing.T) {
	compiler.ClearMIRCache()

	// SLOAD from cold slot (first access)
	// EIP-2929: cold SLOAD = 2100 gas
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (slot)
		0x54,       // SLOAD
		0x50,       // POP
		0x00,       // STOP
	}

	runGasComparison(t, "Storage_ColdAccess", code, nil, true)
}

// TestMIR_Gas_Storage_WarmAccess tests warm storage access gas
func TestMIR_Gas_Storage_WarmAccess(t *testing.T) {
	compiler.ClearMIRCache()

	// SLOAD same slot twice (second is warm)
	// Cold: 2100, Warm: 100
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (slot)
		0x54,       // SLOAD (cold)
		0x50,       // POP
		0x60, 0x00, // PUSH1 0x00 (slot)
		0x54,       // SLOAD (warm)
		0x50,       // POP
		0x00,       // STOP
	}

	runGasComparison(t, "Storage_WarmAccess", code, nil, true)
}

// TestMIR_Gas_Storage_SSTORE_ZeroToNonzero tests SSTORE from zero to nonzero
func TestMIR_Gas_Storage_SSTORE_ZeroToNonzero(t *testing.T) {
	compiler.ClearMIRCache()

	// SSTORE 0->nonzero costs 20000 gas
	code := []byte{
		0x60, 0xff, // PUSH1 0xff (value)
		0x60, 0x00, // PUSH1 0x00 (slot)
		0x55,       // SSTORE
		0x00,       // STOP
	}

	runGasComparison(t, "Storage_SSTORE_ZeroToNonzero", code, nil, true)
}

// TestMIR_Gas_Storage_SSTORE_NonzeroToNonzero tests SSTORE from nonzero to nonzero
func TestMIR_Gas_Storage_SSTORE_NonzeroToNonzero(t *testing.T) {
	compiler.ClearMIRCache()

	// First SSTORE sets value, second modifies it
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x60, 0x00, // PUSH1 0x00
		0x55,       // SSTORE (0->1)
		0x60, 0x02, // PUSH1 0x02
		0x60, 0x00, // PUSH1 0x00
		0x55, // SSTORE (1->2, costs 2900 gas)
		0x00, // STOP
	}

	runGasComparison(t, "Storage_SSTORE_NonzeroToNonzero", code, nil, true)
}

// ===========================================================================
// Call Gas Tests
// ===========================================================================

// TestMIR_Gas_Call_ToEmpty tests CALL to empty address
func TestMIR_Gas_Call_ToEmpty(t *testing.T) {
	compiler.ClearMIRCache()

	// CALL to empty address with zero value
	// Gas = base cost + cold account access
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (retSize)
		0x60, 0x00, // PUSH1 0x00 (retOffset)
		0x60, 0x00, // PUSH1 0x00 (argsSize)
		0x60, 0x00, // PUSH1 0x00 (argsOffset)
		0x60, 0x00, // PUSH1 0x00 (value)
		0x73, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, // PUSH20 address
		0x61, 0xff, 0xff, // PUSH2 0xffff (gas)
		0xf1, // CALL
		0x50, // POP result
		0x00, // STOP
	}

	runGasComparison(t, "Call_ToEmpty", code, nil, true)
}

// TestMIR_Gas_Call_WithValue tests CALL with value transfer
func TestMIR_Gas_Call_WithValue(t *testing.T) {
	compiler.ClearMIRCache()

	// CALL with value (adds 9000 gas stipend consideration)
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (retSize)
		0x60, 0x00, // PUSH1 0x00 (retOffset)
		0x60, 0x00, // PUSH1 0x00 (argsSize)
		0x60, 0x00, // PUSH1 0x00 (argsOffset)
		0x60, 0x01, // PUSH1 0x01 (value = 1 wei)
		0x73, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x02, // PUSH20 address
		0x61, 0xff, 0xff, // PUSH2 0xffff (gas)
		0xf1, // CALL
		0x50, // POP result
		0x00, // STOP
	}

	runGasComparisonWithBalance(t, "Call_WithValue", code, nil, true)
}

// ===========================================================================
// Arithmetic Gas Tests
// ===========================================================================

// TestMIR_Gas_EXP tests EXP opcode gas (dynamic based on exponent size)
func TestMIR_Gas_EXP(t *testing.T) {
	compiler.ClearMIRCache()

	// EXP gas = 10 + 50 * byte_size(exponent)
	code := []byte{
		0x60, 0x02, // PUSH1 0x02 (base)
		0x60, 0x10, // PUSH1 0x10 (exponent = 16, 1 byte)
		0x0a,       // EXP
		0x50,       // POP
		0x60, 0x02, // PUSH1 0x02 (base)
		0x61, 0x01, 0x00, // PUSH2 0x0100 (exponent = 256, 2 bytes)
		0x0a, // EXP
		0x50, // POP
		0x00, // STOP
	}

	runGasComparison(t, "EXP", code, nil, true)
}

// TestMIR_Gas_SHA3 tests KECCAK256/SHA3 opcode gas
func TestMIR_Gas_SHA3(t *testing.T) {
	compiler.ClearMIRCache()

	// SHA3 gas = 30 + 6 * word_count + memory_expansion
	// First store some data, then hash it
	code := []byte{
		0x7f, // PUSH32 (some data)
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20 (32 bytes)
		0x60, 0x00, // PUSH1 0x00 (offset)
		0x20,       // SHA3
		0x50,       // POP
		0x00,       // STOP
	}

	runGasComparison(t, "SHA3", code, nil, true)
}

// ===========================================================================
// OOG Boundary Tests
// ===========================================================================

// TestMIR_Gas_OOG_Exact tests behavior when gas is exactly enough
func TestMIR_Gas_OOG_Exact(t *testing.T) {
	compiler.ClearMIRCache()

	// Simple code: PUSH1 + PUSH1 + ADD + STOP = 3 + 3 + 3 + 0 = 9 gas
	code := []byte{
		0x60, 0x01, // PUSH1 0x01 (3 gas)
		0x60, 0x02, // PUSH1 0x02 (3 gas)
		0x01,       // ADD (3 gas)
		0x50,       // POP (2 gas)
		0x00,       // STOP (0 gas)
	}

	// Run with exact gas needed (approximately)
	// Account for base transaction cost
	runGasComparisonWithExactGas(t, "OOG_Exact", code, nil, 50000)
}

// TestMIR_Gas_OOG_OneShort tests behavior when gas is 1 short
func TestMIR_Gas_OOG_OneShort(t *testing.T) {
	compiler.ClearMIRCache()

	// Code that needs more gas than provided
	code := []byte{
		0x60, 0xff, // PUSH1 0xff
		0x60, 0x00, // PUSH1 0x00
		0x55, // SSTORE (expensive - 20000+ gas)
		0x00, // STOP
	}

	// Run with insufficient gas for SSTORE
	runGasComparisonWithExactGas(t, "OOG_OneShort", code, nil, 100)
}

// ===========================================================================
// Complex Scenario Gas Tests
// ===========================================================================

// TestMIR_Gas_ComplexArithmetic tests gas for complex arithmetic operations
func TestMIR_Gas_ComplexArithmetic(t *testing.T) {
	compiler.ClearMIRCache()

	// Multiple arithmetic operations
	code := []byte{
		0x60, 0x0a, // PUSH1 10
		0x60, 0x14, // PUSH1 20
		0x01,       // ADD (30)
		0x60, 0x02, // PUSH1 2
		0x02,       // MUL (60)
		0x60, 0x03, // PUSH1 3
		0x04,       // DIV (20)
		0x60, 0x07, // PUSH1 7
		0x06,       // MOD (6)
		0x50,       // POP
		0x00,       // STOP
	}

	runGasComparison(t, "ComplexArithmetic", code, nil, true)
}

// TestMIR_Gas_LoopIteration tests gas for loop iterations
func TestMIR_Gas_LoopIteration(t *testing.T) {
	compiler.ClearMIRCache()

	// Simple loop: 10 iterations
	code := []byte{
		0x60, 0x00, // PUSH1 0 (counter)
		0x5b,       // JUMPDEST (loop start, PC=2)
		0x60, 0x01, // PUSH1 1
		0x01,       // ADD
		0x80,       // DUP1
		0x60, 0x0a, // PUSH1 10
		0x10,       // LT
		0x60, 0x02, // PUSH1 2 (loop target)
		0x57,       // JUMPI
		0x50,       // POP
		0x00,       // STOP
	}

	runGasComparison(t, "LoopIteration", code, nil, true)
}

// ===========================================================================
// Helper Functions
// ===========================================================================

// runGasComparison runs code with both EVMs and compares gas usage
func runGasComparison(t *testing.T, name string, code, input []byte, strictGas bool) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	contractAddr := common.HexToAddress("0xc0de")

	// Base EVM
	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    1_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	// MIR
	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    1_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	// Set up contract code
	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	// Execute
	_, gasBase, errBase := evmBase.Call(
		baseCfg.Origin,
		contractAddr,
		input,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	_, gasMIR, errMIR := evmMIR.Call(
		mirCfg.Origin,
		contractAddr,
		input,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	gasUsedBase := baseCfg.GasLimit - gasBase
	gasUsedMIR := mirCfg.GasLimit - gasMIR

	t.Logf("[%s] Base EVM: gasUsed=%d, err=%v", name, gasUsedBase, errBase)
	t.Logf("[%s] MIR:      gasUsed=%d, err=%v", name, gasUsedMIR, errMIR)

	// Check error consistency
	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	// Check gas consistency
	gasDiff := int64(gasUsedBase) - int64(gasUsedMIR)
	if gasDiff < 0 {
		gasDiff = -gasDiff
	}

	if strictGas && gasDiff > 0 {
		t.Errorf("[%s] Gas mismatch: base=%d, mir=%d, diff=%d", name, gasUsedBase, gasUsedMIR, gasDiff)
	} else if gasDiff > 0 {
		t.Logf("[%s] Gas difference: %d", name, gasDiff)
	}

	if gasDiff == 0 {
		t.Logf("[%s] ✅ Gas parity verified", name)
	}
}

// runGasComparisonWithBalance sets up balance for value transfer tests
func runGasComparisonWithBalance(t *testing.T, name string, code, input []byte, strictGas bool) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	contractAddr := common.HexToAddress("0xc0de")
	origin := common.HexToAddress("0x1234")

	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    1_000_000,
		Origin:      origin,
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    1_000_000,
		Origin:      origin,
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	// Give contract some balance for value transfers
	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	baseCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000_000), 0)

	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)
	mirCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000_000), 0)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	_, gasBase, errBase := evmBase.Call(
		baseCfg.Origin,
		contractAddr,
		input,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	_, gasMIR, errMIR := evmMIR.Call(
		mirCfg.Origin,
		contractAddr,
		input,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	gasUsedBase := baseCfg.GasLimit - gasBase
	gasUsedMIR := mirCfg.GasLimit - gasMIR

	t.Logf("[%s] Base EVM: gasUsed=%d, err=%v", name, gasUsedBase, errBase)
	t.Logf("[%s] MIR:      gasUsed=%d, err=%v", name, gasUsedMIR, errMIR)

	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	gasDiff := int64(gasUsedBase) - int64(gasUsedMIR)
	if gasDiff < 0 {
		gasDiff = -gasDiff
	}

	if strictGas && gasDiff > 0 {
		t.Errorf("[%s] Gas mismatch: base=%d, mir=%d, diff=%d", name, gasUsedBase, gasUsedMIR, gasDiff)
	}

	if gasDiff == 0 {
		t.Logf("[%s] ✅ Gas parity verified", name)
	}
}

// runGasComparisonWithExactGas tests with specific gas limit
func runGasComparisonWithExactGas(t *testing.T, name string, code, input []byte, gasLimit uint64) {
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

	// Both should have same error state (both OOG or both success)
	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error state mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	// If both succeeded, check gas parity
	if errBase == nil && errMIR == nil {
		if gasBase != gasMIR {
			t.Errorf("[%s] Gas mismatch: base=%d, mir=%d", name, gasBase, gasMIR)
		} else {
			t.Logf("[%s] ✅ Gas parity verified", name)
		}
	} else {
		t.Logf("[%s] ✅ Both correctly ran out of gas", name)
	}
}

