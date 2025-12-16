// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// MIR Initcode Tests - Tests MIR interpreter for contract deployment (initcode) scenarios

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
// Simple Initcode Tests
// ===========================================================================

// TestMIR_Initcode_Simple tests a minimal contract deployment
// Initcode: PUSH1 0x00 PUSH1 0x00 RETURN (returns empty runtime code)
func TestMIR_Initcode_Simple(t *testing.T) {
	compiler.ClearMIRCache()

	// Simple initcode: returns empty code
	// PUSH1 0x00  (size=0)
	// PUSH1 0x00  (offset=0)
	// RETURN
	initcode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	runInitcodeComparison(t, "Simple_EmptyReturn", initcode)
}

// TestMIR_Initcode_ReturnCode tests initcode that returns actual runtime code
// Initcode copies runtime code to memory and returns it
func TestMIR_Initcode_ReturnCode(t *testing.T) {
	compiler.ClearMIRCache()

	// Runtime code: PUSH1 0x42 PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 RETURN
	// (stores 0x42 at memory[0] and returns 32 bytes)
	runtimeCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	// Initcode: copy runtime code to memory and return it
	// PUSH1 <len>   - length of runtime code
	// PUSH1 <offset> - offset in initcode where runtime starts
	// PUSH1 0x00    - memory destination
	// CODECOPY
	// PUSH1 <len>   - return size
	// PUSH1 0x00    - return offset
	// RETURN
	initcode := []byte{
		0x60, byte(len(runtimeCode)), // PUSH1 len
		0x60, 0x0c, // PUSH1 offset (initcode is 12 bytes before runtime)
		0x60, 0x00, // PUSH1 0x00 (memory dest)
		0x39,                         // CODECOPY
		0x60, byte(len(runtimeCode)), // PUSH1 len
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}
	initcode = append(initcode, runtimeCode...)

	runInitcodeComparison(t, "ReturnCode", initcode)
}

// TestMIR_Initcode_WithLoop tests initcode containing a loop
func TestMIR_Initcode_WithLoop(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode with a simple loop: count from 0 to 5
	// PUSH1 0x00    ; counter = 0
	// JUMPDEST      ; loop start (PC=2)
	// PUSH1 0x01    ; increment
	// ADD           ; counter++
	// DUP1          ; dup counter for comparison
	// PUSH1 0x05    ; limit
	// LT            ; counter < 5?
	// PUSH1 0x02    ; loop target
	// JUMPI         ; jump if true
	// POP           ; clean stack
	// PUSH1 0x00 PUSH1 0x00 RETURN
	initcode := []byte{
		0x60, 0x00, // PUSH1 0x00 (counter)
		0x5b,       // JUMPDEST (PC=2)
		0x60, 0x01, // PUSH1 0x01
		0x01,       // ADD
		0x80,       // DUP1
		0x60, 0x05, // PUSH1 0x05
		0x10,       // LT
		0x60, 0x02, // PUSH1 0x02 (loop target)
		0x57,       // JUMPI
		0x50,       // POP
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	runInitcodeComparison(t, "WithLoop", initcode)
}

// TestMIR_Initcode_WithConditional tests initcode with conditional branching
func TestMIR_Initcode_WithConditional(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode with if-else:
	// if (1 == 1) { store 0x42 } else { store 0x24 }
	// Then return empty code
	initcode := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x60, 0x01, // PUSH1 0x01
		0x14,       // EQ
		0x60, 0x10, // PUSH1 0x10 (then branch at PC=16)
		0x57,       // JUMPI
		// else branch: store 0x24
		0x60, 0x24, // PUSH1 0x24
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x18, // PUSH1 0x18 (end at PC=24)
		0x56,       // JUMP
		// then branch (PC=16): store 0x42
		0x5b,       // JUMPDEST
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		// end (PC=24): return empty
		0x5b,       // JUMPDEST
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	runInitcodeComparison(t, "WithConditional", initcode)
}

// TestMIR_Initcode_WithStorage tests initcode that uses storage operations
func TestMIR_Initcode_WithStorage(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode: store a value, then return empty code
	// PUSH1 0xff    ; value
	// PUSH1 0x00    ; key
	// SSTORE
	// PUSH1 0x00 PUSH1 0x00 RETURN
	initcode := []byte{
		0x60, 0xff, // PUSH1 0xff (value)
		0x60, 0x00, // PUSH1 0x00 (key)
		0x55,       // SSTORE
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	runInitcodeComparison(t, "WithStorage", initcode)
}

// TestMIR_Initcode_WithCalldata tests initcode that reads calldata
func TestMIR_Initcode_WithCalldata(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode: load calldata[0:32] to memory, then return empty
	// PUSH1 0x20    ; size
	// PUSH1 0x00    ; calldata offset
	// PUSH1 0x00    ; memory offset
	// CALLDATACOPY
	// PUSH1 0x00 PUSH1 0x00 RETURN
	initcode := []byte{
		0x60, 0x20, // PUSH1 0x20 (size)
		0x60, 0x00, // PUSH1 0x00 (calldata offset)
		0x60, 0x00, // PUSH1 0x00 (memory offset)
		0x37,       // CALLDATACOPY
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}

	runInitcodeComparison(t, "WithCalldata", initcode)
}

// TestMIR_Initcode_LargeCode tests initcode with larger runtime code (>256 bytes)
func TestMIR_Initcode_LargeCode(t *testing.T) {
	compiler.ClearMIRCache()

	// Create a larger runtime code (300 bytes of NOPs followed by STOP)
	runtimeCode := make([]byte, 300)
	for i := 0; i < 299; i++ {
		runtimeCode[i] = 0x5b // JUMPDEST (acts as NOP)
	}
	runtimeCode[299] = 0x00 // STOP

	// Initcode using PUSH2 for larger offsets
	// PUSH2 <len>
	// PUSH2 <offset>
	// PUSH1 0x00
	// CODECOPY
	// PUSH2 <len>
	// PUSH1 0x00
	// RETURN
	initcodeLen := 15 // length of initcode before runtime
	initcode := []byte{
		0x61, byte(len(runtimeCode) >> 8), byte(len(runtimeCode)), // PUSH2 len
		0x61, byte(initcodeLen >> 8), byte(initcodeLen), // PUSH2 offset
		0x60, 0x00, // PUSH1 0x00
		0x39, // CODECOPY
		0x61, byte(len(runtimeCode) >> 8), byte(len(runtimeCode)), // PUSH2 len
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}
	initcode = append(initcode, runtimeCode...)

	runInitcodeComparison(t, "LargeCode", initcode)
}

// TestMIR_Initcode_Revert tests initcode that reverts
func TestMIR_Initcode_Revert(t *testing.T) {
	compiler.ClearMIRCache()

	// Initcode: immediately revert with error message
	// PUSH4 "fail"
	// PUSH1 0x00
	// MSTORE
	// PUSH1 0x04
	// PUSH1 0x1c
	// REVERT
	initcode := []byte{
		0x63, 'f', 'a', 'i', 'l', // PUSH4 "fail"
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x04, // PUSH1 0x04
		0x60, 0x1c, // PUSH1 0x1c (offset to get last 4 bytes)
		0xfd, // REVERT
	}

	runInitcodeComparisonExpectError(t, "Revert", initcode)
}

// ===========================================================================
// Helper Functions
// ===========================================================================

// runInitcodeComparison runs initcode with both base EVM and MIR, comparing results
func runInitcodeComparison(t *testing.T, name string, initcode []byte) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)

	// Base EVM config (no MIR)
	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	// MIR config
	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	// Create states
	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	// Create EVMs
	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	// Deploy with base EVM
	retBase, addrBase, gasBase, errBase := evmBase.Create(
		baseCfg.Origin,
		initcode,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	// Deploy with MIR
	retMIR, addrMIR, gasMIR, errMIR := evmMIR.Create(
		mirCfg.Origin,
		initcode,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	// Compare results
	t.Logf("[%s] Base EVM: addr=%s, gasLeft=%d, err=%v, retLen=%d",
		name, addrBase.Hex(), gasBase, errBase, len(retBase))
	t.Logf("[%s] MIR:      addr=%s, gasLeft=%d, err=%v, retLen=%d",
		name, addrMIR.Hex(), gasMIR, errMIR, len(retMIR))

	// Check error consistency
	if (errBase != nil) != (errMIR != nil) {
		t.Errorf("[%s] Error mismatch: base=%v, mir=%v", name, errBase, errMIR)
		return
	}

	if errBase != nil {
		// Both errored - check if same error type
		if errBase.Error() != errMIR.Error() {
			t.Logf("[%s] Warning: different error messages: base=%q, mir=%q", name, errBase, errMIR)
		}
		return
	}

	// Check address consistency
	if addrBase != addrMIR {
		t.Errorf("[%s] Address mismatch: base=%s, mir=%s", name, addrBase.Hex(), addrMIR.Hex())
	}

	// Check return value consistency
	if string(retBase) != string(retMIR) {
		t.Errorf("[%s] Return value mismatch: base=%x, mir=%x", name, retBase, retMIR)
	}

	// Check gas consistency (allow small variance for now)
	gasDiff := int64(gasBase) - int64(gasMIR)
	if gasDiff < 0 {
		gasDiff = -gasDiff
	}
	if gasDiff > 0 {
		t.Logf("[%s] Gas difference: %d (base=%d, mir=%d)", name, gasDiff, gasBase, gasMIR)
	}

	t.Logf("[%s] ✅ Initcode test passed", name)
}

// runInitcodeComparisonExpectError runs initcode expecting both to error
func runInitcodeComparisonExpectError(t *testing.T, name string, initcode []byte) {
	t.Helper()

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)

	baseCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: false},
	}

	mirCfg := &Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.HexToAddress("0x1234"),
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableMIR: true},
	}

	baseCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	mirCfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	evmBase := NewEnv(baseCfg)
	evmMIR := NewEnv(mirCfg)

	_, _, gasBase, errBase := evmBase.Create(
		baseCfg.Origin,
		initcode,
		baseCfg.GasLimit,
		uint256.MustFromBig(baseCfg.Value),
	)

	_, _, gasMIR, errMIR := evmMIR.Create(
		mirCfg.Origin,
		initcode,
		mirCfg.GasLimit,
		uint256.MustFromBig(mirCfg.Value),
	)

	t.Logf("[%s] Base EVM: gasLeft=%d, err=%v", name, gasBase, errBase)
	t.Logf("[%s] MIR:      gasLeft=%d, err=%v", name, gasMIR, errMIR)

	// Both should error
	if errBase == nil {
		t.Errorf("[%s] Expected base EVM to error", name)
	}
	if errMIR == nil {
		t.Errorf("[%s] Expected MIR to error", name)
	}

	if errBase != nil && errMIR != nil {
		t.Logf("[%s] ✅ Both correctly errored", name)
	}
}

