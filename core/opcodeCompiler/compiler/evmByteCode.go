package compiler

import "fmt"

// This is copied from vm/opcodes.go.

// ByteCode is an EVM ByteCode
type ByteCode byte

func (b ByteCode) byteCodeToString() string {
	switch b {
	// 0x00 arithmetic
	case STOP:
		return "STOP"
	case ADD:
		return "ADD"
	case MUL:
		return "MUL"
	case SUB:
		return "SUB"
	case DIV:
		return "DIV"
	case SDIV:
		return "SDIV"
	case MOD:
		return "MOD"
	case SMOD:
		return "SMOD"
	case ADDMOD:
		return "ADDMOD"
	case MULMOD:
		return "MULMOD"
	case EXP:
		return "EXP"
	case SIGNEXTEND:
		return "SIGNEXTEND"
	}
	// 0x10 comparisons
	switch b {
	case LT:
		return "LT"
	case GT:
		return "GT"
	case SLT:
		return "SLT"
	case SGT:
		return "SGT"
	case EQ:
		return "EQ"
	case ISZERO:
		return "ISZERO"
	case AND:
		return "AND"
	case OR:
		return "OR"
	case XOR:
		return "XOR"
	case NOT:
		return "NOT"
	case BYTE:
		return "BYTE"
	case SHL:
		return "SHL"
	case SHR:
		return "SHR"
	case SAR:
		return "SAR"
	}
	// 0x20 crypto
	if b == KECCAK256 {
		return "KECCAK256"
	}
	// 0x30 state
	switch b {
	case ADDRESS:
		return "ADDRESS"
	case BALANCE:
		return "BALANCE"
	case ORIGIN:
		return "ORIGIN"
	case CALLER:
		return "CALLER"
	case CALLVALUE:
		return "CALLVALUE"
	case CALLDATALOAD:
		return "CALLDATALOAD"
	case CALLDATASIZE:
		return "CALLDATASIZE"
	case CALLDATACOPY:
		return "CALLDATACOPY"
	case CODESIZE:
		return "CODESIZE"
	case CODECOPY:
		return "CODECOPY"
	case GASPRICE:
		return "GASPRICE"
	case EXTCODESIZE:
		return "EXTCODESIZE"
	case EXTCODECOPY:
		return "EXTCODECOPY"
	case RETURNDATASIZE:
		return "RETURNDATASIZE"
	case RETURNDATACOPY:
		return "RETURNDATACOPY"
	case EXTCODEHASH:
		return "EXTCODEHASH"
	}
	// 0x40 block
	switch b {
	case BLOCKHASH:
		return "BLOCKHASH"
	case COINBASE:
		return "COINBASE"
	case TIMESTAMP:
		return "TIMESTAMP"
	case NUMBER:
		return "NUMBER"
	case DIFFICULTY:
		return "DIFFICULTY"
	case GASLIMIT:
		return "GASLIMIT"
	case CHAINID:
		return "CHAINID"
	case SELFBALANCE:
		return "SELFBALANCE"
	case BASEFEE:
		return "BASEFEE"
	case BLOBHASH:
		return "BLOBHASH"
	case BLOBBASEFEE:
		return "BLOBBASEFEE"
	}
	// 0x50 range
	switch b {
	case POP:
		return "POP"
	case MLOAD:
		return "MLOAD"
	case MSTORE:
		return "MSTORE"
	case MSTORE8:
		return "MSTORE8"
	case SLOAD:
		return "SLOAD"
	case SSTORE:
		return "SSTORE"
	case JUMP:
		return "JUMP"
	case JUMPI:
		return "JUMPI"
	case PC:
		return "PC"
	case MSIZE:
		return "MSIZE"
	case GAS:
		return "GAS"
	case JUMPDEST:
		return "JUMPDEST"
	case TLOAD:
		return "TLOAD"
	case TSTORE:
		return "TSTORE"
	case MCOPY:
		return "MCOPY"
	case PUSH0:
		return "PUSH0"
	}
	// 0x60 pushes
	if b >= PUSH1 && b <= PUSH32 {
		return fmt.Sprintf("PUSH%d", int(b-PUSH1)+1)
	}
	// 0x80 dups
	if b >= DUP1 && b <= DUP16 {
		return fmt.Sprintf("DUP%d", int(b-DUP1)+1)
	}
	// 0x90 swaps
	if b >= SWAP1 && b <= SWAP16 {
		return fmt.Sprintf("SWAP%d", int(b-SWAP1)+1)
	}
	// 0xa0 logs
	if b >= LOG0 && b <= LOG4 {
		return fmt.Sprintf("LOG%d", int(b-LOG0))
	}
	// 0xd0 EOF data
	switch b {
	case DATALOAD:
		return "DATALOAD"
	case DATALOADN:
		return "DATALOADN"
	case DATASIZE:
		return "DATASIZE"
	case DATACOPY:
		return "DATACOPY"
	}
	// 0xe0 EOF control
	switch b {
	case RJUMP:
		return "RJUMP"
	case RJUMPI:
		return "RJUMPI"
	case RJUMPV:
		return "RJUMPV"
	case CALLF:
		return "CALLF"
	case RETF:
		return "RETF"
	case JUMPF:
		return "JUMPF"
	case DUPN:
		return "DUPN"
	case SWAPN:
		return "SWAPN"
	case EXCHANGE:
		return "EXCHANGE"
	case EOFCREATE:
		return "EOFCREATE"
	case RETURNCONTRACT:
		return "RETURNCONTRACT"
	}
	// 0xf0 system
	switch b {
	case CREATE:
		return "CREATE"
	case CALL:
		return "CALL"
	case CALLCODE:
		return "CALLCODE"
	case RETURN:
		return "RETURN"
	case DELEGATECALL:
		return "DELEGATECALL"
	case CREATE2:
		return "CREATE2"
	case RETURNDATALOAD:
		return "RETURNDATALOAD"
	case EXTCALL:
		return "EXTCALL"
	case EXTDELEGATECALL:
		return "EXTDELEGATECALL"
	case STATICCALL:
		return "STATICCALL"
	case EXTSTATICCALL:
		return "EXTSTATICCALL"
	case REVERT:
		return "REVERT"
	case INVALID:
		return "INVALID"
	case SELFDESTRUCT:
		return "SELFDESTRUCT"
	}
	// Fallback for custom/fused and unknown
	return fmt.Sprintf("0x%02x", byte(b))
}

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
	BLOBHASH    ByteCode = 0x49
	BLOBBASEFEE ByteCode = 0x4a
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
	TLOAD    ByteCode = 0x5c
	TSTORE   ByteCode = 0x5d
	MCOPY    ByteCode = 0x5e
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
	DUP1 ByteCode = 0x80 + iota
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
	SWAP1 ByteCode = 0x90 + iota
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

// 0xb0 range - customized instructions.
const (
	Nop ByteCode = 0xb0 + iota
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
	JumpIfZero

	IsZeroPush2
	Dup2MStorePush1Add
	Dup1Push4EqPush2
	Push1CalldataloadPush1ShrDup1Push4GtPush2
	Push1Push1Push1SHLSub
	AndDup2AddSwap1Dup2LT
	Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT
	Dup3And
	Swap2Swap1Dup3SubSwap2Dup3GtPush2
	Swap1Dup2
	SHRSHRDup1MulDup1
	Swap3PopPopPop
	SubSLTIsZeroPush2
	Dup11MulDup3SubMulDup1 // 0xcf
)

// 0xd0 range - eof operations.
const (
	DATALOAD  ByteCode = 0xd0
	DATALOADN ByteCode = 0xd1
	DATASIZE  ByteCode = 0xd2
	DATACOPY  ByteCode = 0xd3
)

// 0xe0 range - eof operations.
const (
	RJUMP          ByteCode = 0xe0
	RJUMPI         ByteCode = 0xe1
	RJUMPV         ByteCode = 0xe2
	CALLF          ByteCode = 0xe3
	RETF           ByteCode = 0xe4
	JUMPF          ByteCode = 0xe5
	DUPN           ByteCode = 0xe6
	SWAPN          ByteCode = 0xe7
	EXCHANGE       ByteCode = 0xe8
	EOFCREATE      ByteCode = 0xec
	RETURNCONTRACT ByteCode = 0xee
)

// 0xf0 range - closures.
const (
	CREATE       ByteCode = 0xf0
	CALL         ByteCode = 0xf1
	CALLCODE     ByteCode = 0xf2
	RETURN       ByteCode = 0xf3
	DELEGATECALL ByteCode = 0xf4
	CREATE2      ByteCode = 0xf5

	RETURNDATALOAD  ByteCode = 0xf7
	EXTCALL         ByteCode = 0xf8
	EXTDELEGATECALL ByteCode = 0xf9

	STATICCALL    ByteCode = 0xfa
	EXTSTATICCALL ByteCode = 0xfb
	REVERT        ByteCode = 0xfd
	INVALID       ByteCode = 0xfe
	SELFDESTRUCT  ByteCode = 0xff
)
