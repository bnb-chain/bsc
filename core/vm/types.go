package vm

// BasicBlock represents a sequence of opcodes that can be executed linearly
// without any jumps in or out except at the beginning and end.
type BasicBlock struct {
	StartPC    uint64  // Program counter where this block starts
	EndPC      uint64  // Program counter where this block ends (exclusive)
	Opcodes    []byte  // The actual opcodes in this block
	JumpTarget *uint64 // If this block ends with a jump, the target PC
	IsJumpDest bool    // Whether this block starts with a JUMPDEST
	StaticGas  uint64  // Pre-calculated static gas cost for this block
}

// GasCalculator interface to avoid circular dependency
type GasCalculator interface {
	GetConstantGas(op byte) uint64
} 