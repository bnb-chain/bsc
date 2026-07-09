package vm

import "strings"

// superInstructionMap maps super-instruction opcodes to the slice of ordinary opcodes
// they were fused from.  The mapping comes from the fusion patterns implemented in
// core/opcodeCompiler/compiler/opCodeProcessor.go (applyFusionPatterns).  When that file
// is updated with new fusion rules, this map should be kept in sync.
var superInstructionMap = map[OpCode][]OpCode{
	AndSwap1PopSwap2Swap1: {AND, SWAP1, POP, SWAP2, SWAP1},
	Swap2Swap1PopJump:     {SWAP2, SWAP1, POP, JUMP},
	Swap1PopSwap2Swap1:    {SWAP1, POP, SWAP2, SWAP1},
	PopSwap2Swap1Pop:      {POP, SWAP2, SWAP1, POP},
	Push2Jump:             {PUSH2, JUMP}, // PUSH2 embeds 2-byte immediate
	Push2JumpI:            {PUSH2, JUMPI},
	Push1Push1:            {PUSH1, PUSH1},
	Push1Add:              {PUSH1, ADD},
	Push1Shl:              {PUSH1, SHL},
	Push1Dup1:             {PUSH1, DUP1},
	Swap1Pop:              {SWAP1, POP},
	PopJump:               {POP, JUMP},
	Pop2:                  {POP, POP},
	Swap2Swap1:            {SWAP2, SWAP1},
	Swap2Pop:              {SWAP2, POP},
	Dup2LT:                {DUP2, LT},
	JumpIfZero:            {ISZERO, PUSH2, JUMPI}, // PUSH2 embeds 2-byte immediate
	IsZeroPush2:           {ISZERO, PUSH2},
	Dup2MStorePush1Add:    {DUP2, MSTORE, PUSH1, ADD},
	Dup1Push4EqPush2:      {DUP1, PUSH4, EQ, PUSH2},
	Push1CalldataloadPush1ShrDup1Push4GtPush2:      {PUSH1, CALLDATALOAD, PUSH1, SHR, DUP1, PUSH4, GT, PUSH2},
	Push1Push1Push1SHLSub:                          {PUSH1, PUSH1, PUSH1, SHL, SUB},
	AndDup2AddSwap1Dup2LT:                          {AND, DUP2, ADD, SWAP1, DUP2, LT},
	Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT: {SWAP1, PUSH1, DUP1, NOT, SWAP2, ADD, AND, DUP2, ADD, SWAP1, DUP2, LT},
	Dup3And:                           {DUP3, AND},
	Swap2Swap1Dup3SubSwap2Dup3GtPush2: {SWAP2, SWAP1, DUP3, SUB, SWAP2, DUP3, GT, PUSH2},
	Swap1Dup2:                         {SWAP1, DUP2},
	SHRSHRDup1MulDup1:                 {SHR, SHR, DUP1, MUL, DUP1},
	Swap3PopPopPop:                    {SWAP3, POP, POP, POP},
	SubSLTIsZeroPush2:                 {SUB, SLT, ISZERO, PUSH2},
	Dup11MulDup3SubMulDup1:            {DUP11, MUL, DUP3, SUB, MUL, DUP1},
}

// DecomposeSuperInstruction returns the underlying opcode sequence of a fused
// super-instruction.  If the provided opcode is not a super-instruction (or is
// unknown), the second return value will be false.
func DecomposeSuperInstruction(op OpCode) ([]OpCode, bool) {
	seq, ok := superInstructionMap[op]
	return seq, ok
}

// DecomposeSuperInstructionByName works like DecomposeSuperInstruction but takes the
// textual name (case-insensitive) instead of the opcode constant.
func DecomposeSuperInstructionByName(name string) ([]OpCode, bool) {
	op := StringToOp(strings.ToUpper(name))
	return DecomposeSuperInstruction(op)
}
