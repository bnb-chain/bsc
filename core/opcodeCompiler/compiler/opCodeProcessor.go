package compiler

import (
	"errors"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
)

var (
	enabled            bool
	opcodeParseEnabled bool
	codeCache          *OpCodeCache
	taskChannel        chan optimizeTask
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
	maxOptimizedOpcode = 0xc8
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

func EnableOpcodeParse() {
	if opcodeParseEnabled {
		return
	}
	opcodeParseEnabled = true
}

// IsOpcodeParseEnabled returns whether opcode parsing (MIR optimization) is enabled
func IsOpcodeParseEnabled() bool {
	return opcodeParseEnabled
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

	var processedCode []byte
	var err error

	EnableOpcodeParse() // todo: for test only, create a flag

	// Step 1: Apply MIR-based optimizations first
	if opcodeParseEnabled {
		mirOptimizedCode, mirErr := parseOpCodeWithOptimization(hash, code)
		if mirErr == nil && len(mirOptimizedCode) > 0 {
			// Use MIR-optimized code as input for superinstruction optimization
			processedCode, err = processByteCodes(mirOptimizedCode)
		} else {
			// Fallback to original code if MIR optimization fails
			doOpcodesParse(hash, code)
			processedCode, err = processByteCodes(code)
		}
	} else {
		// Original path - only superinstruction optimization
		processedCode, err = processByteCodes(code)
	}

	if err != nil {
		return nil, err
	}

	codeCache.AddCodeCache(hash, processedCode)
	return processedCode, err
}

// GenOrRewriteOptimizedCodeWithoutMIR generates optimized code using only superinstruction optimization (no MIR)
func GenOrRewriteOptimizedCodeWithoutMIR(hash common.Hash, code []byte) ([]byte, error) {
	if !enabled {
		return nil, ErrOptimizedDisabled
	}

	var processedCode []byte
	var err error

	// Only use superinstruction optimization, skip MIR optimization
	processedCode, err = processByteCodes(code)

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
	return DoCFGBasedOpcodeFusion(code)
}

func doOpcodesProcess(code []byte) ([]byte, error) {
	code, err := doCodeFusion(code)
	if err != nil {
		return nil, ErrFailPreprocessing
	}
	return code, nil
}

// Exported version of doCodeFusion for use in benchmarks and external tests
func DoCodeFusion(code []byte) ([]byte, error) {
	// return doCodeFusion(code)
	return DoCFGBasedOpcodeFusion(code)
}

// DoCFGBasedOpcodeFusion performs opcode fusion within basic blocks, skipping blocks of type "others"
func DoCFGBasedOpcodeFusion(code []byte) ([]byte, error) {
	// Generate basic blocks
	blocks := GenerateBasicBlocks(code)
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
			// Extract jump target from PUSH2 data (bytes 2-3)
			if cur+3 < len(code) {
				target := extractPush2Value(code[cur+2 : cur+4])
				// Only do code fusion if the jump target is valid
				if isValidJumpTarget(code, target) {
					op := JumpIfZero
					code[cur] = byte(op)
					code[cur+1] = byte(Nop)
					code[cur+4] = byte(Nop)
					return 4
				}
			}
		}

		if code0 == DUP2 && code1 == MSTORE && code2 == PUSH1 && code4 == ADD {
			op := Dup2MStorePush1Add
			code[cur] = byte(op)
			code[cur+1] = byte(Nop)
			code[cur+2] = byte(Nop)
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
			// Extract jump target from PUSH2 data (bytes 1-2)
			if cur+2 < len(code) {
				target := extractPush2Value(code[cur+1 : cur+3])
				// Only do code fusion if the jump target is valid
				if isValidJumpTarget(code, target) {
					op := Push2Jump
					code[cur] = byte(op)
					code[cur+3] = byte(Nop)
					return 3
				}
			}
		}

		if code0 == PUSH2 && code3 == JUMPI {
			// Extract jump target from PUSH2 data (bytes 1-2)
			if cur+2 < len(code) {
				target := extractPush2Value(code[cur+1 : cur+3])
				// Only do code fusion if the jump target is valid
				if isValidJumpTarget(code, target) {
					op := Push2JumpI
					code[cur] = byte(op)
					code[cur+3] = byte(Nop)
					return 3
				}
			}
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

func doCodeFusion(code []byte) ([]byte, error) {
	fusedCode := make([]byte, len(code))
	length := copy(fusedCode, code)
	skipToNext := false
	for i := 0; i < length; i++ {
		cur := i
		skipToNext = false

		// todo: perf issue found with these logic, comment for now
		//if fusedCode[cur] == byte(INVALID) {
		//	return fusedCode, nil
		//}
		//if fusedCode[cur] >= minOptimizedOpcode && fusedCode[cur] <= maxOptimizedOpcode {
		//	return code, ErrFailPreprocessing
		//}

		if length > cur+15 {
			code0 := ByteCode(fusedCode[cur+0])
			code2 := ByteCode(fusedCode[cur+2])
			code3 := ByteCode(fusedCode[cur+3])
			code5 := ByteCode(fusedCode[cur+5])
			code6 := ByteCode(fusedCode[cur+6])
			code7 := ByteCode(fusedCode[cur+7])
			code12 := ByteCode(fusedCode[cur+12])
			code13 := ByteCode(fusedCode[cur+13])

			if code0 == PUSH1 && code2 == CALLDATALOAD && code3 == PUSH1 && code5 == SHR &&
				code6 == DUP1 && code7 == PUSH4 && code12 == GT && code13 == PUSH2 {
				op := Push1CalldataloadPush1ShrDup1Push4GtPush2
				fusedCode[cur] = byte(op)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				fusedCode[cur+5] = byte(Nop)
				fusedCode[cur+6] = byte(Nop)
				fusedCode[cur+7] = byte(Nop)
				fusedCode[cur+12] = byte(Nop)
				fusedCode[cur+13] = byte(Nop)
				skipToNext = true
			}

			if skipToNext {
				i += 15
				continue
			}
		}

		if length > cur+12 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])
			code3 := ByteCode(fusedCode[cur+3])
			code4 := ByteCode(fusedCode[cur+4])
			code5 := ByteCode(fusedCode[cur+5])
			code6 := ByteCode(fusedCode[cur+6])
			code7 := ByteCode(fusedCode[cur+7])
			code8 := ByteCode(fusedCode[cur+8])
			code9 := ByteCode(fusedCode[cur+9])
			code10 := ByteCode(fusedCode[cur+10])
			code11 := ByteCode(fusedCode[cur+11])
			code12 := ByteCode(fusedCode[cur+12])

			if code0 == SWAP1 && code1 == PUSH1 && code3 == DUP1 && code4 == NOT &&
				code5 == SWAP2 && code6 == ADD && code7 == AND && code8 == DUP2 &&
				code9 == ADD && code10 == SWAP1 && code11 == DUP2 && code12 == LT {
				op := Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)
				fusedCode[cur+5] = byte(Nop)
				fusedCode[cur+6] = byte(Nop)
				fusedCode[cur+7] = byte(Nop)
				fusedCode[cur+8] = byte(Nop)
				fusedCode[cur+9] = byte(Nop)
				fusedCode[cur+10] = byte(Nop)
				fusedCode[cur+11] = byte(Nop)
				fusedCode[cur+12] = byte(Nop)
				skipToNext = true
			}

			if skipToNext {
				i += 12
				continue
			}
		}

		if length > cur+9 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])
			code6 := ByteCode(fusedCode[cur+6])
			code7 := ByteCode(fusedCode[cur+7])

			if code0 == DUP1 && code1 == PUSH4 && code6 == EQ && code7 == PUSH2 {
				op := Dup1Push4EqPush2
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+6] = byte(Nop)
				fusedCode[cur+7] = byte(Nop)
				skipToNext = true
			}

			if skipToNext {
				i += 9
				continue
			}
		}

		if length > cur+7 {
			code0 := ByteCode(fusedCode[cur+0])
			code2 := ByteCode(fusedCode[cur+2])
			code4 := ByteCode(fusedCode[cur+4])
			code6 := ByteCode(fusedCode[cur+6])
			code7 := ByteCode(fusedCode[cur+7])

			if code0 == PUSH1 && code2 == PUSH1 && code4 == PUSH1 && code6 == SHL && code7 == SUB {
				op := Push1Push1Push1SHLSub
				fusedCode[cur] = byte(op)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)
				fusedCode[cur+6] = byte(Nop)
				fusedCode[cur+7] = byte(Nop)
				skipToNext = true
			}
			if skipToNext {
				i += 7
				continue
			}
		}

		if length > cur+5 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])
			code2 := ByteCode(fusedCode[cur+2])
			code3 := ByteCode(fusedCode[cur+3])
			code4 := ByteCode(fusedCode[cur+4])
			code5 := ByteCode(fusedCode[cur+5])

			if code0 == AND && code1 == DUP2 && code2 == ADD && code3 == SWAP1 && code4 == DUP2 && code5 == LT {
				op := AndDup2AddSwap1Dup2LT
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)
				fusedCode[cur+5] = byte(Nop)
				skipToNext = true
			}
			if skipToNext {
				i += 5
				continue
			}
		}

		if length > cur+4 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])
			code2 := ByteCode(fusedCode[cur+2])
			code3 := ByteCode(fusedCode[cur+3])
			code4 := ByteCode(fusedCode[cur+4])
			if code0 == AND && code1 == SWAP1 && code2 == POP && code3 == SWAP2 && code4 == SWAP1 {
				op := AndSwap1PopSwap2Swap1
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)
				skipToNext = true
			}

			// Test zero and Jump. target offset at code[2-3]
			if code0 == ISZERO && code1 == PUSH2 && code4 == JUMPI {
				// Extract jump target from PUSH2 data (bytes 2-3)
				if cur+3 < len(fusedCode) {
					target := extractPush2Value(fusedCode[cur+2 : cur+4])
					// Only do code fusion if the jump target is valid
					if isValidJumpTarget(fusedCode, target) {
						op := JumpIfZero
						fusedCode[cur] = byte(op)
						fusedCode[cur+1] = byte(Nop)
						fusedCode[cur+4] = byte(Nop)
						skipToNext = true
					}
				}
			}

			if code0 == DUP2 && code1 == MSTORE && code2 == PUSH1 && code4 == ADD {
				op := Dup2MStorePush1Add
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)

				skipToNext = true
			}

			if skipToNext {
				i += 4
				continue
			}
		}

		if length > cur+3 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])
			code2 := ByteCode(fusedCode[cur+2])
			code3 := ByteCode(fusedCode[cur+3])
			if code0 == SWAP2 && code1 == SWAP1 && code2 == POP && code3 == JUMP {
				op := Swap2Swap1PopJump
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				skipToNext = true
			}

			if code0 == SWAP1 && code1 == POP && code2 == SWAP2 && code3 == SWAP1 {
				op := Swap1PopSwap2Swap1
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				skipToNext = true
			}

			if code0 == POP && code1 == SWAP2 && code2 == SWAP1 && code3 == POP {
				op := PopSwap2Swap1Pop
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+2] = byte(Nop)
				fusedCode[cur+3] = byte(Nop)
				skipToNext = true
			}
			// push and jump
			if code0 == PUSH2 && code3 == JUMP {
				// Extract jump target from PUSH2 data (bytes 1-2)
				if cur+2 < len(fusedCode) {
					target := extractPush2Value(fusedCode[cur+1 : cur+3])
					// Only do code fusion if the jump target is valid
					if isValidJumpTarget(fusedCode, target) {
						op := Push2Jump
						fusedCode[cur] = byte(op)
						fusedCode[cur+3] = byte(Nop)
						skipToNext = true
					}
				}
			}

			if code0 == PUSH2 && code3 == JUMPI {
				// Extract jump target from PUSH2 data (bytes 1-2)
				if cur+2 < len(fusedCode) {
					target := extractPush2Value(fusedCode[cur+1 : cur+3])
					// Only do code fusion if the jump target is valid
					if isValidJumpTarget(fusedCode, target) {
						op := Push2JumpI
						fusedCode[cur] = byte(op)
						fusedCode[cur+3] = byte(Nop)
						skipToNext = true
					}
				}
			}

			if code0 == PUSH1 && code2 == PUSH1 {
				op := Push1Push1
				fusedCode[cur] = byte(op)
				fusedCode[cur+2] = byte(Nop)
				skipToNext = true
			}

			// Note: JumpIfZero pattern (ISZERO + PUSH2 + JUMPI) should be checked before IsZeroPush2
			// to avoid the simpler IsZeroPush2 pattern (ISZERO + PUSH2) from matching first
			if code0 == ISZERO && code1 == PUSH2 {
				op := IsZeroPush2
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if skipToNext {
				i += 3
				continue
			}
		}

		if length > cur+2 {
			code0 := ByteCode(fusedCode[cur+0])
			_ = ByteCode(fusedCode[cur+1])
			code2 := ByteCode(fusedCode[cur+2])
			if code0 == PUSH1 {
				if code2 == ADD {
					op := Push1Add
					fusedCode[cur] = byte(op)
					fusedCode[cur+2] = byte(Nop)
					skipToNext = true
				}
				if code2 == SHL {
					op := Push1Shl
					fusedCode[cur] = byte(op)
					fusedCode[cur+2] = byte(Nop)
					skipToNext = true
				}

				if code2 == DUP1 {
					op := Push1Dup1
					fusedCode[cur] = byte(op)
					fusedCode[cur+2] = byte(Nop)
					skipToNext = true
				}
			}
			if skipToNext {
				i += 2
				continue
			}
		}

		if length > cur+1 {
			code0 := ByteCode(fusedCode[cur+0])
			code1 := ByteCode(fusedCode[cur+1])

			if code0 == SWAP1 && code1 == POP {
				op := Swap1Pop
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}
			if code0 == POP && code1 == JUMP {
				op := PopJump
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if code0 == POP && code1 == POP {
				op := Pop2
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if code0 == SWAP2 && code1 == SWAP1 {
				op := Swap2Swap1
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if code0 == SWAP2 && code1 == POP {
				op := Swap2Pop
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if code0 == DUP2 && code1 == LT {
				op := Dup2LT
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				skipToNext = true
			}

			if skipToNext {
				i++
				continue
			}
		}

		skip, steps := calculateSkipSteps(fusedCode, cur)
		if skip {
			i += steps
			continue
		}
	}
	return fusedCode, nil
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
}

// GenerateBasicBlocks takes a byte array of opcodes and returns an array of BasicBlocks.
// This function parses the opcodes to identify basic blocks - sequences of instructions
// that can be executed linearly without jumps in the middle.
func GenerateBasicBlocks(code []byte) []BasicBlock {
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
			blocks = append(blocks, *currentBlock)
			currentBlock = nil
		}
	}
	// If there's a block in progress, add it
	if currentBlock != nil && len(currentBlock.Opcodes) > 0 {
		currentBlock.EndPC = pc
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

// extractPush2Value extracts a 16-bit value from PUSH2 data bytes, consistent with EVM behavior
func extractPush2Value(data []byte) uint64 {
	if len(data) < 2 {
		return 0
	}
	// EVM uses big-endian encoding for PUSH2 data
	return uint64(data[0])<<8 | uint64(data[1])
}

// isValidJumpTarget checks if the given position is a valid jump destination.
// A valid jump destination must be a JUMPDEST opcode and must be in a code segment (not data).
func isValidJumpTarget(code []byte, target uint64) bool {
	// Check bounds
	if target >= uint64(len(code)) {
		return false
	}

	// Check if the target is a JUMPDEST opcode
	if ByteCode(code[target]) != JUMPDEST {
		return false
	}

	// Check if the target is in a code segment (not data)
	return isCodeSegment(code, target)
}

// isCodeSegment checks if the given position is in a code segment (not data).
// This is a simplified version of the VM's code bitmap analysis.
func isCodeSegment(code []byte, pos uint64) bool {
	var pc uint64
	for pc < uint64(len(code)) && pc <= pos {
		op := ByteCode(code[pc])
		pc++

		// Handle super instructions that contain data
		step, processed := codeBitmapForSI(code, pc, op)
		if processed {
			pc += step
			continue
		}

		// Handle PUSH instructions (mark data bytes)
		if op >= PUSH1 && op <= PUSH32 {
			numbits := uint64(op - PUSH1 + 1)
			// If the target position is within the data bytes of this PUSH, it's not code
			if pos >= pc && pos < pc+numbits {
				return false
			}
			pc += numbits
		}
	}

	// If we reach here, the position is in a code segment
	return true
}

// codeBitmapForSI is a simplified version for jump target validation
func codeBitmapForSI(code []byte, pc uint64, op ByteCode) (step uint64, processed bool) {
	switch op {
	case Push2Jump, Push2JumpI:
		step = 3
		processed = true
	case Push1Push1:
		step = 3
		processed = true
	case Push1Add, Push1Shl, Push1Dup1:
		step = 2
		processed = true
	case JumpIfZero:
		step = 4
		processed = true
	case IsZeroPush2:
		step = 3
		processed = true
	case Dup2MStorePush1Add:
		step = 4
		processed = true
	case Dup1Push4EqPush2:
		step = 9
		processed = true
	case Push1CalldataloadPush1ShrDup1Push4GtPush2:
		step = 15
		processed = true
	case Push1Push1Push1SHLSub:
		step = 7
		processed = true
	case Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT:
		step = 12
		processed = true
	default:
		return 0, false
	}
	return step, processed
}
