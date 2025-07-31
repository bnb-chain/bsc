package compiler

import (
	"errors"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
)

// GasCalculator interface for calculating gas costs
type GasCalculator interface {
	GetConstantGas(op byte) uint64
}

var (
	enabled     bool
	codeCache   *OpCodeCache
	taskChannel chan optimizeTask
)

var (
	ErrFailPreprocessing = errors.New("fail to do preprocessing")
	ErrOptimizedDisabled = errors.New("opcode optimization is disabled")
)

const taskChannelSize = 1024 * 1024

const (
	generate optimizeTaskType = 1
	flush    optimizeTaskType = 2

	minOptimizedOpcode = 0xb0
	maxOptimizedOpcode = 0xcf
)

type OpCodeProcessorConfig struct {
	DoOpcodeFusion bool
}

type optimizeTaskType byte

type CodeType uint8

type optimizeTask struct {
	taskType optimizeTaskType
	hash     common.Hash
	rawCode  []byte
}

func init() {
	taskChannel = make(chan optimizeTask, taskChannelSize)
	taskNumber := runtime.NumCPU() * 1 / 8 // No need to use too many threads.
	if taskNumber < 1 {
		taskNumber = 1
	}
	codeCache = getOpCodeCacheInstance()

	for i := 0; i < taskNumber; i++ {
		go taskProcessor()
	}
}

func EnableOptimization() {
	if enabled {
		return
	}
	enabled = true
}

func DisableOptimization() {
	enabled = false
}

func IsEnabled() bool {
	return enabled
}

func LoadOptimizedCode(hash common.Hash) []byte {
	if !enabled {
		return nil
	}
	processedCode := codeCache.GetCachedCode(hash)
	return processedCode
}

func LoadBitvec(codeHash common.Hash) []byte {
	if !enabled {
		return nil
	}
	bitvec := codeCache.GetCachedBitvec(codeHash)
	return bitvec
}

func StoreBitvec(codeHash common.Hash, bitvec []byte) {
	if !enabled {
		return
	}
	codeCache.AddBitvecCache(codeHash, bitvec)
}

func GenOrLoadOptimizedCode(hash common.Hash, code []byte) {
	if !enabled {
		return
	}
	task := optimizeTask{generate, hash, code}
	taskChannel <- task
}

func taskProcessor() {
	for {
		task := <-taskChannel
		// Process the message here
		handleOptimizationTask(task)
	}
}

func handleOptimizationTask(task optimizeTask) {
	switch task.taskType {
	case generate:
		TryGenerateOptimizedCode(task.hash, task.rawCode)
	case flush:
		DeleteCodeCache(task.hash)
	}
}

// GenOrRewriteOptimizedCode generate the optimized code and refresh the code cache.
func GenOrRewriteOptimizedCode(hash common.Hash, code []byte) ([]byte, error) {
	if !enabled {
		return nil, ErrOptimizedDisabled
	}
	processedCode, err := processByteCodes(code)
	if err != nil {
		return nil, err
	}
	codeCache.AddCodeCache(hash, processedCode)
	return processedCode, err
}

func TryGenerateOptimizedCode(hash common.Hash, code []byte) ([]byte, error) {
	processedCode := codeCache.GetCachedCode(hash)
	var err error = nil
	if len(processedCode) == 0 {
		processedCode, err = GenOrRewriteOptimizedCode(hash, code)
	}
	return processedCode, err
}

func DeleteCodeCache(hash common.Hash) {
	if !enabled {
		return
	}
	// flush in case there are invalid cached code
	codeCache.RemoveCachedCode(hash)
}

func processByteCodes(code []byte) ([]byte, error) {
	//return doOpcodesProcess(code)
	return DoCFGBasedOpcodeFusion(code)
}

// Exported version of doCodeFusion for use in benchmarks and external tests
func DoCodeFusion(code []byte) ([]byte, error) {
	// return doCodeFusion(code)
	return DoCFGBasedOpcodeFusion(code)
}

// DoCFGBasedOpcodeFusion performs opcode fusion within basic blocks, skipping blocks of type "others"
func DoCFGBasedOpcodeFusion(code []byte) ([]byte, error) {
	// Generate basic blocks
	blocks := GenerateBasicBlocks(code, nil) // Pass nil for now, as gasCalc is not yet available here
	if len(blocks) == 0 {
		return nil, ErrFailPreprocessing
	}

	// Create a copy of the original code (only after checking for optimized opcodes)
	fusedCode := make([]byte, len(code))
	copy(fusedCode, code)

	// Process each basic block
	for i, block := range blocks {
		// Skip blocks of type "others"
		blockType := getBlockType(block, blocks, i)
		if blockType == "others" {
			continue
		}

		// Check if the block contains optimized opcodes in the original code
		hasOptimized := false
		for pc := block.StartPC; pc < block.EndPC && pc < uint64(len(code)); {
			if code[pc] >= minOptimizedOpcode && code[pc] <= maxOptimizedOpcode {
				hasOptimized = true
				break
			}
			// Skip data bytes for PUSH instructions
			skip, steps := calculateSkipSteps(code, int(pc))
			if skip {
				pc += uint64(steps) + 1 // Add 1 for the opcode byte
			} else {
				pc++
			}
		}
		if hasOptimized {
			// If any block being processed contains optimized opcodes, return nil, ErrFailPreprocessing
			return nil, ErrFailPreprocessing
		}

		// Check if the block contains INVALID opcodes in the original code
		hasInvalid := false
		for pc := block.StartPC; pc < block.EndPC && pc < uint64(len(code)); {
			if ByteCode(code[pc]) == INVALID {
				hasInvalid = true
				break
			}
			// Skip data bytes for PUSH instructions
			skip, steps := calculateSkipSteps(code, int(pc))
			if skip {
				pc += uint64(steps) + 1 // Add 1 for the opcode byte
			} else {
				pc++
			}
		}
		if hasInvalid {
			// Skip processing this block if it contains INVALID opcodes
			continue
		}

		// Apply fusion within this block
		err := fuseBlock(fusedCode, block)
		if err != nil {
			return code, err
		}
	}

	return fusedCode, nil
}

// fuseBlock applies opcode fusion to a single basic block
func fuseBlock(code []byte, block BasicBlock) error {
	startPC := int(block.StartPC)
	endPC := int(block.EndPC)

	// Process the block's opcodes
	for i := startPC; i < endPC; {
		if i >= len(code) {
			break
		}

		// Apply fusion patterns within the block
		skipSteps := applyFusionPatterns(code, i, endPC)
		if skipSteps > 0 {
			i += skipSteps + 1 // Add 1 for the opcode byte
		} else {
			// Skip data bytes for PUSH instructions
			skip, steps := calculateSkipSteps(code, i)
			if skip {
				i += steps + 1 // Add 1 for the opcode byte
			} else {
				i++
			}
		}
	}

	return nil
}

// applyFusionPatterns applies known fusion patterns and returns the number of steps to skip
func applyFusionPatterns(code []byte, cur int, endPC int) int {
	length := len(code)

	// Pattern 1: 15-byte pattern
	if length > cur+15 && cur+15 < endPC {
		code0 := ByteCode(code[cur+0])
		code2 := ByteCode(code[cur+2])
		code3 := ByteCode(code[cur+3])
		code5 := ByteCode(code[cur+5])
		code6 := ByteCode(code[cur+6])
		code7 := ByteCode(code[cur+7])
		code12 := ByteCode(code[cur+12])
		code13 := ByteCode(code[cur+13])

		if code0 == PUSH1 && code2 == CALLDATALOAD && code3 == PUSH1 && code5 == SHR &&
			code6 == DUP1 && code7 == PUSH4 && code12 == GT && code13 == PUSH2 {
			op := Push1CalldataloadPush1ShrDup1Push4GtPush2
			code[cur] = byte(op)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+5] = byte(Nop)
			code[cur+6] = byte(Nop)
			code[cur+7] = byte(Nop)
			code[cur+12] = byte(Nop)
			code[cur+13] = byte(Nop)
			return 15
		}
	}

	// Pattern 2: 12-byte pattern
	if length > cur+12 && cur+12 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])
		code3 := ByteCode(code[cur+3])
		code4 := ByteCode(code[cur+4])
		code5 := ByteCode(code[cur+5])
		code6 := ByteCode(code[cur+6])
		code7 := ByteCode(code[cur+7])
		code8 := ByteCode(code[cur+8])
		code9 := ByteCode(code[cur+9])
		code10 := ByteCode(code[cur+10])
		code11 := ByteCode(code[cur+11])
		code12 := ByteCode(code[cur+12])

		if code0 == SWAP1 && code1 == PUSH1 && code3 == DUP1 && code4 == NOT &&
			code5 == SWAP2 && code6 == ADD && code7 == AND && code8 == DUP2 &&
			code9 == ADD && code10 == SWAP1 && code11 == DUP2 && code12 == LT {
			op := Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			code[cur+5] = byte(Nop)
			code[cur+6] = byte(Nop)
			code[cur+7] = byte(Nop)
			code[cur+8] = byte(Nop)
			code[cur+9] = byte(Nop)
			code[cur+10] = byte(Nop)
			code[cur+11] = byte(Nop)
			code[cur+12] = byte(Nop)
			return 12
		}
	}

	// Pattern 3: 9-byte pattern
	if length > cur+9 && cur+9 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])
		code2 := ByteCode(code[cur+2])
		code3 := ByteCode(code[cur+3])
		code4 := ByteCode(code[cur+4])
		code5 := ByteCode(code[cur+5])
		code6 := ByteCode(code[cur+6])
		code7 := ByteCode(code[cur+7])

		if code0 == DUP1 && code1 == PUSH4 && code6 == EQ && code7 == PUSH2 {
			op := Dup1Push4EqPush2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+6] = byte(Nop)
			code[cur+7] = byte(Nop)
			return 9
		}
		if code0 == SWAP2 && code1 == SWAP1 && code2 == DUP3 && code3 == SUB && code4 == SWAP2 && code5 == DUP3 && code6 == GT && code7 == PUSH2 {
			op := Swap2Swap1Dup3SubSwap2Dup3GtPush2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			code[cur+5] = byte(Nop)
			code[cur+6] = byte(Nop)
			code[cur+7] = byte(Nop)
			return 9
		}
	}

	// Pattern 4: 7-byte pattern
	if length > cur+7 && cur+7 < endPC {
		code0 := ByteCode(code[cur+0])
		code2 := ByteCode(code[cur+2])
		code4 := ByteCode(code[cur+4])
		code6 := ByteCode(code[cur+6])
		code7 := ByteCode(code[cur+7])

		if code0 == PUSH1 && code2 == PUSH1 && code4 == PUSH1 && code6 == SHL && code7 == SUB {
			op := Push1Push1Push1SHLSub
			code[cur] = byte(op)
			code[cur+2] = byte(Nop)
			code[cur+4] = byte(Nop)
			code[cur+6] = byte(Nop)
			code[cur+7] = byte(Nop)
			return 7
		}
	}

	// Pattern 5: 5-byte pattern
	if length > cur+5 && cur+5 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])
		code2 := ByteCode(code[cur+2])
		code3 := ByteCode(code[cur+3])
		code4 := ByteCode(code[cur+4])
		code5 := ByteCode(code[cur+5])

		if code0 == AND && code1 == DUP2 && code2 == ADD && code3 == SWAP1 && code4 == DUP2 && code5 == LT {
			op := AndDup2AddSwap1Dup2LT
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			code[cur+5] = byte(Nop)
			return 5
		}

		if code0 == SUB && code1 == SLT && code2 == ISZERO && code3 == PUSH2 {
			op := SubSLTIsZeroPush2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			return 5
		}

		if code0 == DUP11 && code1 == MUL && code2 == DUP3 && code3 == SUB && code4 == MUL && code5 == DUP1 {
			op := Dup11MulDup3SubMulDup1
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			code[cur+5] = byte(Nop)
			return 5
		}
	}

	// Pattern 6: 4-byte pattern
	if length > cur+4 && cur+4 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])
		code2 := ByteCode(code[cur+2])
		code3 := ByteCode(code[cur+3])
		code4 := ByteCode(code[cur+4])

		if code0 == AND && code1 == SWAP1 && code2 == POP && code3 == SWAP2 && code4 == SWAP1 {
			op := AndSwap1PopSwap2Swap1
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			return 4
		}

		// Test zero and Jump. target offset at code[2-3]
		if code0 == ISZERO && code1 == PUSH2 && code4 == JUMPI {
			op := JumpIfZero
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+4] = byte(Nop)
			return 4
		}

		if code0 == DUP2 && code1 == MSTORE && code2 == PUSH1 && code4 == ADD {
			op := Dup2MStorePush1Add
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+4] = byte(Nop)
			return 4
		}

		if code0 == SHR && code1 == SHR && code2 == DUP1 && code3 == MUL && code4 == DUP1 {
			op := SHRSHRDup1MulDup1
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			code[cur+4] = byte(Nop)
			return 4
		}
	}

	// Pattern 7: 3-byte pattern
	if length > cur+3 && cur+3 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])
		code2 := ByteCode(code[cur+2])
		code3 := ByteCode(code[cur+3])

		if code0 == SWAP2 && code1 == SWAP1 && code2 == POP && code3 == JUMP {
			op := Swap2Swap1PopJump
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			return 3
		}

		if code0 == SWAP1 && code1 == POP && code2 == SWAP2 && code3 == SWAP1 {
			op := Swap1PopSwap2Swap1
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			return 3
		}

		if code0 == POP && code1 == SWAP2 && code2 == SWAP1 && code3 == POP {
			op := PopSwap2Swap1Pop
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			return 3
		}

		// push and jump
		if code0 == PUSH2 && code3 == JUMP {
			op := Push2Jump
			code[cur] = byte(op)
			code[cur+3] = byte(Nop)
			return 3
		}

		if code0 == PUSH2 && code3 == JUMPI {
			op := Push2JumpI
			code[cur] = byte(op)
			code[cur+3] = byte(Nop)
			return 3
		}

		if code0 == PUSH1 && code2 == PUSH1 {
			op := Push1Push1
			code[cur] = byte(op)
			code[cur+2] = byte(Nop)
			return 3
		}

		if code0 == ISZERO && code1 == PUSH2 {
			op := IsZeroPush2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 3
		}

		if code0 == SWAP3 && code1 == POP && code2 == POP && code3 == POP {
			op := Swap3PopPopPop
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
			code[cur+3] = byte(Nop)
			return 3
		}
	}

	// Pattern 8: 2-byte pattern
	if length > cur+2 && cur+2 < endPC {
		code0 := ByteCode(code[cur+0])
		code2 := ByteCode(code[cur+2])

		if code0 == PUSH1 {
			if code2 == ADD {
				op := Push1Add
				code[cur] = byte(op)
				code[cur+2] = byte(Nop)
				return 2
			}
			if code2 == SHL {
				op := Push1Shl
				code[cur] = byte(op)
				code[cur+2] = byte(Nop)
				return 2
			}
			if code2 == DUP1 {
				op := Push1Dup1
				code[cur] = byte(op)
				code[cur+2] = byte(Nop)
				return 2
			}
		}
	}

	// Pattern 9: 1-byte pattern
	if length > cur+1 && cur+1 < endPC {
		code0 := ByteCode(code[cur+0])
		code1 := ByteCode(code[cur+1])

		if code0 == SWAP1 && code1 == POP {
			op := Swap1Pop
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == POP && code1 == JUMP {
			op := PopJump
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == POP && code1 == POP {
			op := Pop2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == SWAP2 && code1 == SWAP1 {
			op := Swap2Swap1
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == SWAP2 && code1 == POP {
			op := Swap2Pop
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == DUP2 && code1 == LT {
			op := Dup2LT
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == DUP3 && code1 == AND {
			op := Dup3And
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
		if code0 == SWAP1 && code1 == DUP2 {
			op := Swap1Dup2
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			return 1
		}
	}

	return 0
}

// getBlockType categorizes a basic block based on its content
func getBlockType(block BasicBlock, blocks []BasicBlock, blockIndex int) string {
	if len(block.Opcodes) == 0 {
		return "Empty"
	}

	// Check for entry basic block (first block)
	if block.StartPC == 0 {
		return "entryBB"
	}

	// Check for jump destination blocks (begin with JUMPDEST)
	if block.IsJumpDest {
		return "JumpDest"
	}

	// Check for conditional fallthrough (previous block ends with JUMPI)
	if blockIndex > 0 {
		prevBlock := blocks[blockIndex-1]
		if len(prevBlock.Opcodes) > 0 {
			lastOp := ByteCode(prevBlock.Opcodes[len(prevBlock.Opcodes)-1])
			if lastOp == JUMPI {
				return "conditional fallthrough"
			}
		}
	}

	// Default categorization
	return "others"
}

func calculateSkipSteps(code []byte, cur int) (skip bool, steps int) {
	inst := ByteCode(code[cur])
	if inst >= PUSH1 && inst <= PUSH32 {
		// skip the data.
		steps = int(inst - PUSH1 + 1)
		skip = true
		return skip, steps
	}

	switch inst {
	case Push2Jump, Push2JumpI:
		steps = 3
		skip = true
	case Push1Push1:
		steps = 3
		skip = true
	case Push1Add, Push1Shl, Push1Dup1:
		steps = 2
		skip = true
	case JumpIfZero:
		steps = 4
		skip = true
	default:
		return false, 0
	}
	return skip, steps
}

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

// GenerateBasicBlocks takes a byte array of opcodes and returns an array of BasicBlocks.
// This function parses the opcodes to identify basic blocks - sequences of instructions
// that can be executed linearly without jumps in the middle.
func GenerateBasicBlocks(code []byte, gasCalc GasCalculator) []BasicBlock {
	if len(code) == 0 {
		return nil
	}

	var blocks []BasicBlock
	jumpDests := make(map[uint64]bool)
	var pc uint64

	// First pass: identify all JUMPDEST locations
	for pc < uint64(len(code)) {
		op := ByteCode(code[pc])
		if op == JUMPDEST {
			jumpDests[pc] = true
		}
		skip, steps := calculateSkipSteps(code, int(pc))
		if skip {
			pc += uint64(steps) + 1 // Add 1 for the opcode byte
		} else {
			pc++
		}
	}

	// Second pass: build basic blocks
	pc = 0
	var currentBlock *BasicBlock
	for pc < uint64(len(code)) {
		op := ByteCode(code[pc])

		// Start a new block if we encounter INVALID or if we're at a JUMPDEST
		if op == INVALID || jumpDests[pc] {
			if currentBlock != nil && len(currentBlock.Opcodes) > 0 {
				currentBlock.EndPC = pc
				// Calculate static gas for the completed block
				currentBlock.StaticGas = calculateBlockStaticGas(currentBlock, gasCalc)
				blocks = append(blocks, *currentBlock)
			}
			currentBlock = &BasicBlock{
				StartPC:    pc,
				IsJumpDest: op == JUMPDEST, // Fix: set IsJumpDest if first opcode is JUMPDEST
			}
		} else if currentBlock == nil {
			currentBlock = &BasicBlock{
				StartPC:    pc,
				IsJumpDest: op == JUMPDEST, // Fix: set IsJumpDest if first opcode is JUMPDEST
			}
		}

		// Determine instruction length
		skip, steps := calculateSkipSteps(code, int(pc))
		instLen := uint64(1)
		if skip {
			instLen += uint64(steps)
		}
		// Check bounds before accessing
		if pc+instLen > uint64(len(code)) {
			// If we can't read the full instruction, just add what we can
			instLen = uint64(len(code)) - pc
		}
		// Add instruction bytes to block
		currentBlock.Opcodes = append(currentBlock.Opcodes, code[pc:pc+instLen]...)
		pc += instLen

		// If this is a block terminator (other than INVALID since we already handled it), end the block
		if isBlockTerminator(op) {
			currentBlock.EndPC = pc
			// Calculate static gas for the completed block
			currentBlock.StaticGas = calculateBlockStaticGas(currentBlock, gasCalc)
			blocks = append(blocks, *currentBlock)
			currentBlock = nil
		}
	}
	// If there's a block in progress, add it
	if currentBlock != nil && len(currentBlock.Opcodes) > 0 {
		currentBlock.EndPC = pc
		// Calculate static gas for the last block
		currentBlock.StaticGas = calculateBlockStaticGas(currentBlock, gasCalc)
		blocks = append(blocks, *currentBlock)
	}
	return blocks
}

// isBlockTerminator checks if an opcode terminates a basic block
func isBlockTerminator(op ByteCode) bool {
	switch op {
	case STOP, RETURN, REVERT, SELFDESTRUCT:
		return true
	case JUMP, JUMPI:
		return true
	case RJUMP, RJUMPI, RJUMPV:
		return true
	case CALLF, RETF, JUMPF:
		return true
	default:
		return false
	}
}

// calculateBlockStaticGas calculates the total static gas cost for a basic block
func calculateBlockStaticGas(block *BasicBlock, gasCalc GasCalculator) uint64 {
	totalGas := uint64(0)
	
	// Iterate through all bytes in the block
	for i := 0; i < len(block.Opcodes); i++ {
		op := ByteCode(block.Opcodes[i])
		
		// Only calculate gas for actual opcodes, not data bytes
		if op >= PUSH1 && op <= PUSH32 {
			// This is a PUSH opcode, calculate its gas
			if gasCalc != nil {
				totalGas += gasCalc.GetConstantGas(byte(op))
			}
			// Skip the data bytes in the next iteration
			skipBytes := int(op - PUSH1 + 1)
			i += skipBytes
		} else if op <= 0xff {
			// This is a regular opcode, calculate its gas
			if gasCalc != nil {
				totalGas += gasCalc.GetConstantGas(byte(op))
			}
		}
		// Data bytes (op > 0xff) are ignored for gas calculation
	}
	
	return totalGas
}
