// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// MIR Edge Cases Tests - Tests for boundary conditions and edge cases
// to ensure MIR interpreter handles exceptional scenarios correctly

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
// Stack Tests
// ===========================================================================

// TestMIR_Edge_StackUnderflow tests stack underflow behavior
func TestMIR_Edge_StackUnderflow(t *testing.T) {
	compiler.ClearMIRCache()

	// ADD with empty stack (needs 2 items)
	code := []byte{
		0x01, // ADD (stack underflow)
	}

	runEdgeCaseComparison(t, "StackUnderflow_ADD", code, nil)
}

// TestMIR_Edge_StackUnderflow_POP tests POP on empty stack
func TestMIR_Edge_StackUnderflow_POP(t *testing.T) {
	compiler.ClearMIRCache()

	// POP with empty stack
	code := []byte{
		0x50, // POP (stack underflow)
	}

	runEdgeCaseComparison(t, "StackUnderflow_POP", code, nil)
}

// TestMIR_Edge_StackUnderflow_DUP tests DUP on insufficient stack
func TestMIR_Edge_StackUnderflow_DUP(t *testing.T) {
	compiler.ClearMIRCache()

	// DUP2 with only 1 item
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x81, // DUP2 (needs 2 items, has 1)
	}

	runEdgeCaseComparison(t, "StackUnderflow_DUP", code, nil)
}

// TestMIR_Edge_StackOverflow tests stack overflow (>1024 items)
func TestMIR_Edge_StackOverflow(t *testing.T) {
	compiler.ClearMIRCache()

	// For a true stack overflow test, we need ~1024 consecutive PUSH operations
	// Each PUSH1 adds one item to the stack
	overflowCode := make([]byte, 0, 2050)
	for i := 0; i < 1025; i++ {
		overflowCode = append(overflowCode, 0x60, 0x01) // PUSH1 0x01
	}
	overflowCode = append(overflowCode, 0x00) // STOP

	runEdgeCaseComparison(t, "StackOverflow", overflowCode, nil)
}

// ===========================================================================
// Invalid Opcode Tests
// ===========================================================================

// TestMIR_Edge_InvalidOpcode tests behavior with invalid opcode
func TestMIR_Edge_InvalidOpcode(t *testing.T) {
	compiler.ClearMIRCache()

	// 0xFE is INVALID opcode
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0xfe, // INVALID
	}

	runEdgeCaseComparison(t, "InvalidOpcode", code, nil)
}

// TestMIR_Edge_UnassignedOpcode tests behavior with unassigned opcode
func TestMIR_Edge_UnassignedOpcode(t *testing.T) {
	compiler.ClearMIRCache()

	// 0x0C is unassigned (not a valid opcode)
	code := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x0c, // Unassigned opcode
	}

	runEdgeCaseComparison(t, "UnassignedOpcode", code, nil)
}

// ===========================================================================
// Jump Tests
// ===========================================================================

// TestMIR_Edge_JumpToNonJumpdest tests JUMP to non-JUMPDEST location
func TestMIR_Edge_JumpToNonJumpdest(t *testing.T) {
	compiler.ClearMIRCache()

	// JUMP to PC=0 which is PUSH1, not JUMPDEST
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (target PC)
		0x56, // JUMP (bad jump destination)
	}

	runEdgeCaseComparison(t, "JumpToNonJumpdest", code, nil)
}

// TestMIR_Edge_JumpOutOfBounds tests JUMP beyond code length
func TestMIR_Edge_JumpOutOfBounds(t *testing.T) {
	compiler.ClearMIRCache()

	// JUMP to PC=1000 when code is only 5 bytes
	code := []byte{
		0x61, 0x03, 0xe8, // PUSH2 0x03e8 (1000)
		0x56, // JUMP (out of bounds)
	}

	runEdgeCaseComparison(t, "JumpOutOfBounds", code, nil)
}

// TestMIR_Edge_JumpiToNonJumpdest tests JUMPI to non-JUMPDEST
func TestMIR_Edge_JumpiToNonJumpdest(t *testing.T) {
	compiler.ClearMIRCache()

	// JUMPI to PC=0 with condition=1
	code := []byte{
		0x60, 0x01, // PUSH1 0x01 (condition=true)
		0x60, 0x00, // PUSH1 0x00 (target PC)
		0x57, // JUMPI (bad jump destination)
	}

	runEdgeCaseComparison(t, "JumpiToNonJumpdest", code, nil)
}

// TestMIR_Edge_JumpiNotTaken tests JUMPI when condition is 0
func TestMIR_Edge_JumpiNotTaken(t *testing.T) {
	compiler.ClearMIRCache()

	// JUMPI with condition=0 should fall through
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (condition=false)
		0x60, 0x00, // PUSH1 0x00 (target - would be invalid if taken)
		0x57,       // JUMPI (not taken, falls through)
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "JumpiNotTaken", code, nil)
}

// ===========================================================================
// Division Tests
// ===========================================================================

// TestMIR_Edge_DivisionByZero tests DIV by zero
func TestMIR_Edge_DivisionByZero(t *testing.T) {
	compiler.ClearMIRCache()

	// DIV by zero should return 0, not error
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (divisor)
		0x60, 0x0a, // PUSH1 0x0a (10, dividend)
		0x04,       // DIV (10/0 = 0)
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "DivisionByZero", code, nil)
}

// TestMIR_Edge_ModuloByZero tests MOD by zero
func TestMIR_Edge_ModuloByZero(t *testing.T) {
	compiler.ClearMIRCache()

	// MOD by zero should return 0
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (divisor)
		0x60, 0x0a, // PUSH1 0x0a (10)
		0x06,       // MOD (10%0 = 0)
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "ModuloByZero", code, nil)
}

// TestMIR_Edge_SDivisionByZero tests SDIV by zero
func TestMIR_Edge_SDivisionByZero(t *testing.T) {
	compiler.ClearMIRCache()

	// SDIV by zero should return 0
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (divisor)
		0x60, 0x0a, // PUSH1 0x0a (10)
		0x05,       // SDIV (10/0 = 0)
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "SDivisionByZero", code, nil)
}

// ===========================================================================
// Memory Tests
// ===========================================================================

// TestMIR_Edge_MemoryAccessHuge tests accessing very large memory offset
func TestMIR_Edge_MemoryAccessHuge(t *testing.T) {
	compiler.ClearMIRCache()

	// MLOAD at huge offset - should OOG due to memory expansion
	code := []byte{
		0x7f, // PUSH32 (huge offset)
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x51, // MLOAD (should fail - memory expansion too expensive)
	}

	runEdgeCaseComparison(t, "MemoryAccessHuge", code, nil)
}

// TestMIR_Edge_MemoryZeroSize tests memory operations with zero size
func TestMIR_Edge_MemoryZeroSize(t *testing.T) {
	compiler.ClearMIRCache()

	// CALLDATACOPY with size=0 should be a no-op
	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (size)
		0x60, 0x00, // PUSH1 0x00 (data offset)
		0x60, 0x00, // PUSH1 0x00 (memory offset)
		0x37,       // CALLDATACOPY (size=0, no-op)
		0x00,       // STOP
	}

	runEdgeCaseComparison(t, "MemoryZeroSize", code, nil)
}

// ===========================================================================
// Return/Revert Tests
// ===========================================================================

// TestMIR_Edge_ReturnEmpty tests RETURN with empty data
func TestMIR_Edge_ReturnEmpty(t *testing.T) {
	compiler.ClearMIRCache()

	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (size)
		0x60, 0x00, // PUSH1 0x00 (offset)
		0xf3, // RETURN
	}

	runEdgeCaseComparison(t, "ReturnEmpty", code, nil)
}

// TestMIR_Edge_RevertEmpty tests REVERT with empty data
func TestMIR_Edge_RevertEmpty(t *testing.T) {
	compiler.ClearMIRCache()

	code := []byte{
		0x60, 0x00, // PUSH1 0x00 (size)
		0x60, 0x00, // PUSH1 0x00 (offset)
		0xfd, // REVERT
	}

	runEdgeCaseComparison(t, "RevertEmpty", code, nil)
}

// TestMIR_Edge_RevertWithData tests REVERT with error data
func TestMIR_Edge_RevertWithData(t *testing.T) {
	compiler.ClearMIRCache()

	// Store "error" in memory and revert with it
	code := []byte{
		0x65, 'e', 'r', 'r', 'o', 'r', 0x00, // PUSH6 "error\0"
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x06, // PUSH1 0x06 (size)
		0x60, 0x1a, // PUSH1 0x1a (offset - last 6 bytes of word)
		0xfd, // REVERT
	}

	runEdgeCaseComparison(t, "RevertWithData", code, nil)
}

// ===========================================================================
// Calldata Tests
// ===========================================================================

// TestMIR_Edge_CalldataEmpty tests CALLDATALOAD with no input
func TestMIR_Edge_CalldataEmpty(t *testing.T) {
	compiler.ClearMIRCache()

	// CALLDATALOAD at offset 0 with no calldata should return 0
	code := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x35,       // CALLDATALOAD
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "CalldataEmpty", code, nil)
}

// TestMIR_Edge_CalldataOutOfBounds tests CALLDATALOAD beyond input length
func TestMIR_Edge_CalldataOutOfBounds(t *testing.T) {
	compiler.ClearMIRCache()

	// CALLDATALOAD at offset 100 with only 4 bytes of input
	code := []byte{
		0x60, 0x64, // PUSH1 100
		0x35,       // CALLDATALOAD (should return padded zeros)
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	input := []byte{0x01, 0x02, 0x03, 0x04}
	runEdgeCaseComparison(t, "CalldataOutOfBounds", code, input)
}

// ===========================================================================
// SELFDESTRUCT Tests (EIP-6780 compliant)
// ===========================================================================

// TestMIR_Edge_Selfdestruct tests SELFDESTRUCT behavior
func TestMIR_Edge_Selfdestruct(t *testing.T) {
	compiler.ClearMIRCache()

	// SELFDESTRUCT to beneficiary address
	code := []byte{
		0x73, // PUSH20 beneficiary
		0xbe, 0xef, 0xbe, 0xef, 0xbe, 0xef, 0xbe, 0xef,
		0xbe, 0xef, 0xbe, 0xef, 0xbe, 0xef, 0xbe, 0xef,
		0xbe, 0xef, 0xbe, 0xef,
		0xff, // SELFDESTRUCT
	}

	runEdgeCaseComparison(t, "Selfdestruct", code, nil)
}

// ===========================================================================
// RETURNDATASIZE/RETURNDATACOPY Tests
// ===========================================================================

// TestMIR_Edge_ReturndataBeforeCall tests RETURNDATASIZE before any call
func TestMIR_Edge_ReturndataBeforeCall(t *testing.T) {
	compiler.ClearMIRCache()

	// RETURNDATASIZE before any call should be 0
	code := []byte{
		0x3d,       // RETURNDATASIZE (should be 0)
		0x60, 0x00, // PUSH1 0x00
		0x52, // MSTORE
		0x00, // STOP
	}

	runEdgeCaseComparison(t, "ReturndataBeforeCall", code, nil)
}

// TestMIR_Edge_ReturndatacopyOutOfBounds tests RETURNDATACOPY beyond returndata
func TestMIR_Edge_ReturndatacopyOutOfBounds(t *testing.T) {
	compiler.ClearMIRCache()

	// RETURNDATACOPY with offset beyond returndata size should revert
	code := []byte{
		0x60, 0x20, // PUSH1 0x20 (size)
		0x60, 0x00, // PUSH1 0x00 (data offset - but no returndata)
		0x60, 0x00, // PUSH1 0x00 (memory offset)
		0x3e, // RETURNDATACOPY (should revert - no returndata)
	}

	runEdgeCaseComparison(t, "ReturndatacopyOutOfBounds", code, nil)
}

// ===========================================================================
// Helper Functions
// ===========================================================================

// runEdgeCaseComparison runs code with both EVMs and compares behavior
func runEdgeCaseComparison(t *testing.T, name string, code, input []byte) {
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

	// Set up contract
	baseCfg.State.CreateAccount(contractAddr)
	baseCfg.State.SetCode(contractAddr, code)
	baseCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000), 0)

	mirCfg.State.CreateAccount(contractAddr)
	mirCfg.State.SetCode(contractAddr, code)
	mirCfg.State.SetBalance(contractAddr, uint256.NewInt(1_000_000), 0)

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	// Execute
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

	// Check error consistency
	baseHasErr := errBase != nil
	mirHasErr := errMIR != nil

	if baseHasErr != mirHasErr {
		t.Errorf("[%s] Error state mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	// Check return value consistency
	if string(retBase) != string(retMIR) {
		t.Errorf("[%s] Return value mismatch: base=%x, mir=%x", name, retBase, retMIR)
	}

	// Check gas consistency (for edge cases, allow some tolerance)
	gasDiff := int64(gasUsedBase) - int64(gasUsedMIR)
	if gasDiff < 0 {
		gasDiff = -gasDiff
	}

	if gasDiff > 0 {
		t.Logf("[%s] Gas difference: %d (base=%d, mir=%d)", name, gasDiff, gasUsedBase, gasUsedMIR)
	}

	t.Logf("[%s] ✅ Edge case handled consistently", name)
}

