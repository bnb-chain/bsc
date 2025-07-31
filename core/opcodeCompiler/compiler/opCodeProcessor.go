package compiler

import (
	"errors"
	"reflect"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// GasCalculator interface for calculating gas costs
type GasCalculator interface {
	GetConstantGas(op byte) uint64
}

// JumpTableAdapter adapts a JumpTable to GasCalculator interface
type JumpTableAdapter struct {
	JumpTable interface{}
}

func (jta *JumpTableAdapter) GetConstantGas(op byte) uint64 {
	// Use reflection to call the JumpTable's GetConstantGas method
	// This avoids importing the vm package
	if jta.JumpTable == nil {
		return 0
	}

	// Try to call GetConstantGas method via reflection
	val := reflect.ValueOf(jta.JumpTable)
	if val.IsValid() && !val.IsNil() {
		method := val.MethodByName("GetConstantGas")
		if method.IsValid() {
			args := []reflect.Value{reflect.ValueOf(op)}
			results := method.Call(args)
			if len(results) > 0 {
				return results[0].Uint()
			}
		}
	}
	return 0
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
	gasCalc  GasCalculator
}

func init() {
	taskChannel = make(chan optimizeTask, taskChannelSize)
	taskNumber := runtime.NumCPU() / 8 // No need to use too many threads.
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

func GenOrLoadOptimizedCode(hash common.Hash, code []byte, gasCalc GasCalculator) {
	if !enabled {
		return
	}
	task := optimizeTask{generate, hash, code, gasCalc}
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
		TryGenerateOptimizedCode(task.hash, task.rawCode, task.gasCalc)
	case flush:
		DeleteCodeCache(task.hash)
	}
}

// GenOrRewriteOptimizedCode generate the optimized code and refresh the code cache.
func GenOrRewriteOptimizedCode(hash common.Hash, code []byte, gasCalc GasCalculator) ([]byte, error) {
	if !enabled {
		return nil, ErrOptimizedDisabled
	}

	// Process byte codes with gas calculation and caching
	processedCode, err := DoCFGBasedOpcodeFusion(code, gasCalc, hash)
	if err != nil {
		return nil, err
	}
	codeCache.AddCodeCache(hash, processedCode)
	return processedCode, err
}

func TryGenerateOptimizedCode(hash common.Hash, code []byte, gasCalc GasCalculator) ([]byte, error) {
	processedCode := codeCache.GetCachedCode(hash)
	var err error = nil
	if len(processedCode) == 0 {
		processedCode, err = GenOrRewriteOptimizedCode(hash, code, gasCalc) // Pass nil for gasCalc for now
	}
	return processedCode, err
}

func DeleteCodeCache(hash common.Hash) {
	if !enabled {
		return
	}
	// 清理所有相关cache
	codeCache.RemoveCachedCode(hash)
	codeCache.RemoveBlockCache(hash)
}

// GetBlockByPC 从cache获取PC对应的BasicBlock
func GetBlockByPC(codeHash common.Hash, pc uint64) (*BasicBlock, bool) {
	return codeCache.GetCachedBlock(codeHash, pc)
}

// GetBlockStaticGas 获取block的静态gas
func GetBlockStaticGas(codeHash common.Hash, pc uint64) (uint64, bool) {
	if block, found := GetBlockByPC(codeHash, pc); found {
		return block.StaticGas, true
	}
	return 0, false
}

// DoCFGBasedOpcodeFusion performs opcode fusion within basic blocks, skipping blocks of type "others"
func DoCFGBasedOpcodeFusion(code []byte, gasCalc GasCalculator, hash common.Hash) ([]byte, error) {
	// Generate basic blocks
	blocks := GenerateBasicBlocks(code, gasCalc)
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
			skip, steps := CalculateSkipSteps(code, int(pc))
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
			skip, steps := CalculateSkipSteps(code, int(pc))
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
			skip, steps := CalculateSkipSteps(code, i)
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

	// Check for conditional fallthrough (previous block ends with JUMPI or CALL related)
	if blockIndex > 0 {
		prevBlock := blocks[blockIndex-1]
		if len(prevBlock.Opcodes) > 0 {
			lastOp := ByteCode(prevBlock.Opcodes[len(prevBlock.Opcodes)-1])
			if lastOp == JUMPI ||
				lastOp == CALL ||
				lastOp == CALLCODE ||
				lastOp == DELEGATECALL ||
				lastOp == STATICCALL ||
				lastOp == EXTCALL ||
				lastOp == EXTDELEGATECALL ||
				lastOp == EXTSTATICCALL ||
				lastOp == GAS ||
				lastOp == SSTORE {
				return "conditional fallthrough"
			}
		}
	}

	// Default categorization
	return "others"
}

func CalculateSkipSteps(code []byte, cur int) (skip bool, steps int) {
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
		skip, steps := CalculateSkipSteps(code, int(pc))
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
		skip, steps := CalculateSkipSteps(code, int(pc))
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

	// 构建cache（如果enabled且blocks不为空）
	if enabled && len(blocks) > 0 {
		// 计算codeHash
		codeHash := crypto.Keccak256Hash(code)

		// 构建PC到Block的映射
		pcToBlock := make(map[uint64]*BasicBlock)
		totalGas := uint64(0)

		for i := range blocks {
			block := &blocks[i]
			totalGas += block.StaticGas // 使用已计算的static gas

			// 为block内的每个PC创建映射
			for pc := block.StartPC; pc < block.EndPC; pc++ {
				pcToBlock[pc] = block
			}
		}

		// 添加到cache
		codeCache.AddBlockCache(codeHash, pcToBlock)
	}

	return blocks
}

// calculateBlockStaticGas calculates the total static gas cost for a basic block
func calculateBlockStaticGas(block *BasicBlock, gasCalc GasCalculator) uint64 {
	totalGas := uint64(0)

	// Use the same logic as GenerateBasicBlocks to parse opcodes correctly
	code := block.Opcodes
	pc := uint64(0)

	for pc < uint64(len(code)) {
		op := ByteCode(code[pc])

		// Use CalculateSkipSteps to parse instructions consistently
		skip, steps := CalculateSkipSteps(code, int(pc))

		// Calculate gas for the opcode (only for valid opcodes)
		if op <= 0xff && gasCalc != nil {
			totalGas += gasCalc.GetConstantGas(byte(op))
			//log.Error("check get opcode static gas in processor", "op", op, "gasCalc.GetConstantGas(byte(op))", gasCalc.GetConstantGas(byte(op)))
		}

		// Skip the entire instruction (opcode + data)
		if skip {
			pc += uint64(steps) + 1
		} else {
			pc++
		}
	}

	return totalGas
}

// isBlockTerminator checks if an opcode terminates a basic block
func isBlockTerminator(op ByteCode) bool {
	switch op {
	// Unconditional terminators or explicit halts
	case STOP, RETURN, REVERT, SELFDESTRUCT:
		return true

	// Unconditional / conditional jumps that alter the control-flow within the same contract
	case JUMP, JUMPI, GAS, SSTORE, RJUMP, RJUMPI, RJUMPV, CALLF, RETF, JUMPF:
		return true

	// External message calls — these transfer control to another context and therefore
	// must terminate the current basic block for correct static-gas accounting
	case CALL, CALLCODE, DELEGATECALL, STATICCALL,
		EXTCALL, EXTDELEGATECALL, EXTSTATICCALL:
		return true

	// Contract creation opcodes have similar control-flow behaviour (external call & potential revert)
	// so we also treat them as block terminators
	case CREATE, CREATE2:
		return true

	default:
		return false
	}
}
