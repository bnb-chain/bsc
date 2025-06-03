package compiler

// This is copied from vm/opcodes.go.

// ByteCode is an EVM ByteCode
type ByteCode byte

// 0x0 range - arithmetic ops.
const (
	STOP       ByteCode = 0x0
	ADD        ByteCode = 0x1
	MUL        ByteCode = 0x2
	SUB        ByteCode = 0x3
	DIV        ByteCode = 0x4
	SDIV       ByteCode = 0x5
	MOD        ByteCode = 0x6
	SMOD       ByteCode = 0x7
	ADDMOD     ByteCode = 0x8
	MULMOD     ByteCode = 0x9
	EXP        ByteCode = 0xa
	SIGNEXTEND ByteCode = 0xb
)

// 0x10 range - comparison ops.
const (
	LT     ByteCode = 0x10
	GT     ByteCode = 0x11
	SLT    ByteCode = 0x12
	SGT    ByteCode = 0x13
	EQ     ByteCode = 0x14
	ISZERO ByteCode = 0x15
	AND    ByteCode = 0x16
	OR     ByteCode = 0x17
	XOR    ByteCode = 0x18
	NOT    ByteCode = 0x19
	BYTE   ByteCode = 0x1a
	SHL    ByteCode = 0x1b
	SHR    ByteCode = 0x1c
	SAR    ByteCode = 0x1d
)

// 0x20 range - crypto.
const (
	KECCAK256 ByteCode = 0x20
)

// 0x30 range - closure state.
const (
	ADDRESS        ByteCode = 0x30
	BALANCE        ByteCode = 0x31
	ORIGIN         ByteCode = 0x32
	CALLER         ByteCode = 0x33
	CALLVALUE      ByteCode = 0x34
	CALLDATALOAD   ByteCode = 0x35
	CALLDATASIZE   ByteCode = 0x36
	CALLDATACOPY   ByteCode = 0x37
	CODESIZE       ByteCode = 0x38
	CODECOPY       ByteCode = 0x39
	GASPRICE       ByteCode = 0x3a
	EXTCODESIZE    ByteCode = 0x3b
	EXTCODECOPY    ByteCode = 0x3c
	RETURNDATASIZE ByteCode = 0x3d
	RETURNDATACOPY ByteCode = 0x3e
	EXTCODEHASH    ByteCode = 0x3f
)

// 0x40 range - block operations.
const (
	BLOCKHASH   ByteCode = 0x40
	COINBASE    ByteCode = 0x41
	TIMESTAMP   ByteCode = 0x42
	NUMBER      ByteCode = 0x43
	DIFFICULTY  ByteCode = 0x44
	RANDOM      ByteCode = 0x44 // Same as DIFFICULTY
	PREVRANDAO  ByteCode = 0x44 // Same as DIFFICULTY
	GASLIMIT    ByteCode = 0x45
	CHAINID     ByteCode = 0x46
	SELFBALANCE ByteCode = 0x47
	BASEFEE     ByteCode = 0x48
)

// 0x50 range - 'storage' and execution.
const (
	POP      ByteCode = 0x50
	MLOAD    ByteCode = 0x51
	MSTORE   ByteCode = 0x52
	MSTORE8  ByteCode = 0x53
	SLOAD    ByteCode = 0x54
	SSTORE   ByteCode = 0x55
	JUMP     ByteCode = 0x56
	JUMPI    ByteCode = 0x57
	PC       ByteCode = 0x58
	MSIZE    ByteCode = 0x59
	GAS      ByteCode = 0x5a
	JUMPDEST ByteCode = 0x5b
	PUSH0    ByteCode = 0x5f
)

// 0x60 range - pushes.
const (
	PUSH1 ByteCode = 0x60 + iota
	PUSH2
	PUSH3
	PUSH4
	PUSH5
	PUSH6
	PUSH7
	PUSH8
	PUSH9
	PUSH10
	PUSH11
	PUSH12
	PUSH13
	PUSH14
	PUSH15
	PUSH16
	PUSH17
	PUSH18
	PUSH19
	PUSH20
	PUSH21
	PUSH22
	PUSH23
	PUSH24
	PUSH25
	PUSH26
	PUSH27
	PUSH28
	PUSH29
	PUSH30
	PUSH31
	PUSH32
)

// 0x80 range - dups.
const (
	DUP1 = 0x80 + iota
	DUP2
	DUP3
	DUP4
	DUP5
	DUP6
	DUP7
	DUP8
	DUP9
	DUP10
	DUP11
	DUP12
	DUP13
	DUP14
	DUP15
	DUP16
)

// 0x90 range - swaps.
const (
	SWAP1 = 0x90 + iota
	SWAP2
	SWAP3
	SWAP4
	SWAP5
	SWAP6
	SWAP7
	SWAP8
	SWAP9
	SWAP10
	SWAP11
	SWAP12
	SWAP13
	SWAP14
	SWAP15
	SWAP16
)

// 0xa0 range - logging ops.
const (
	LOG0 ByteCode = 0xa0 + iota
	LOG1
	LOG2
	LOG3
	LOG4
)

// 0xd0 range - customized instructions.
const (
	Nop ByteCode = 0xd0 + iota
	AndSwap1PopSwap2Swap1
	Swap2Swap1PopJump
	Swap1PopSwap2Swap1
	PopSwap2Swap1Pop
	Push2Jump
	Push2JumpI
	Push1Push1
	Push1Add
	Push1Shl
	Push1Dup1
	Swap1Pop
	PopJump
	Pop2
	Swap2Swap1
	Swap2Pop
	Dup2LT
	JumpIfZero // 0xe2
)

// 0xf0 range - closures.
const (
	CREATE       ByteCode = 0xf0
	CALL         ByteCode = 0xf1
	CALLCODE     ByteCode = 0xf2
	RETURN       ByteCode = 0xf3
	DELEGATECALL ByteCode = 0xf4
	CREATE2      ByteCode = 0xf5

	STATICCALL   ByteCode = 0xfa
	REVERT       ByteCode = 0xfd
	INVALID      ByteCode = 0xfe
	SELFDESTRUCT ByteCode = 0xff
)

// 0xb0 range.
const (
	TLOAD  ByteCode = 0xb3
	TSTORE ByteCode = 0xb4
)
