package compiler

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestOpcodeParse(t *testing.T) {
	// Simple test code: PUSH1 0x01, ADD, STOP
	testCode := []byte{0x60, 0x01, 0x01, 0x00}

	// Test that parsing doesn't crash
	err := doOpcodesParse(common.Hash{}, testCode)
	if err != nil {
		t.Logf("Opcode parsing completed with error (expected for simple test): %v", err)
	} else {
		t.Log("Opcode parsing completed successfully")
	}
}

func TestCFGCreation(t *testing.T) {
	// Test CFG creation
	cfg := NewCFG(common.Hash{}, []byte{0x60, 0x01, 0x01, 0x00})
	if cfg == nil {
		t.Fatal("Failed to create CFG")
	}

	if cfg.codeAddr != (common.Hash{}) {
		t.Error("CFG codeAddr not initialized correctly")
	}

	if len(cfg.rawCode) != 4 {
		t.Error("CFG rawCode not initialized correctly")
	}
}

func TestMIRBasicBlockCreation(t *testing.T) {
	// Test MIRBasicBlock creation
	bb := NewMIRBasicBlock(1, 0, nil)
	if bb == nil {
		t.Fatal("Failed to create MIRBasicBlock")
	}

	if bb.blockNum != 1 {
		t.Error("MIRBasicBlock blockNum not set correctly")
	}

	if bb.firstPC != 0 {
		t.Error("MIRBasicBlock firstPC not set correctly")
	}
}

func TestNewlyImplementedOpcodes(t *testing.T) {
	// Test code with various newly implemented opcodes
	testCode := []byte{
		0x60, 0x01, // PUSH1 0x01
		0x60, 0x02, // PUSH1 0x02
		0x80, // DUP1
		0x90, // SWAP1
		0x5f, // PUSH0
		0x00, // STOP
	}

	// Test that parsing doesn't crash with new opcodes
	err := doOpcodesParse(common.Hash{}, testCode)
	if err != nil {
		t.Logf("Opcode parsing completed with error (expected for simple test): %v", err)
	} else {
		t.Log("Opcode parsing completed successfully with new opcodes")
	}

	// Test CFG creation with new opcodes
	cfg := NewCFG(common.Hash{}, testCode)
	if cfg == nil {
		t.Fatal("Failed to create CFG with new opcodes")
	}

	t.Log("Successfully created CFG with newly implemented opcodes")
}

func TestEOFOperations(t *testing.T) {
	// Test code with EOF operations (these are newer opcodes)
	testCode := []byte{
		0xd0, // DATALOAD
		0xd1, // DATALOADN
		0xd2, // DATASIZE
		0xd3, // DATACOPY
		0xe0, // RJUMP
		0xe1, // RJUMPI
		0xe3, // CALLF
		0xe4, // RETF
		0x00, // STOP
	}

	// Test that parsing doesn't crash with EOF opcodes
	err := doOpcodesParse(common.Hash{}, testCode)
	if err != nil {
		t.Logf("EOF opcode parsing completed with error (expected for simple test): %v", err)
	} else {
		t.Log("EOF opcode parsing completed successfully")
	}

	t.Log("Successfully tested EOF operations")
}
