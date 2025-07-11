package compiler

import (
	"errors"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
)

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
	taskNumber := runtime.NumCPU() * 3 / 8
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
	return doOpcodesProcess(code)
}

func doOpcodesProcess(code []byte) ([]byte, error) {
	code, err := doCodeFusion(code)
	if err != nil {
		return nil, ErrFailPreprocessing
	}
	return code, nil
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

		//if length > cur+15 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code2 := ByteCode(fusedCode[cur+2])
		//	code3 := ByteCode(fusedCode[cur+3])
		//	code5 := ByteCode(fusedCode[cur+5])
		//	code6 := ByteCode(fusedCode[cur+6])
		//	code7 := ByteCode(fusedCode[cur+7])
		//	code12 := ByteCode(fusedCode[cur+12])
		//	code13 := ByteCode(fusedCode[cur+13])
		//
		//	if code0 == PUSH1 && code2 == CALLDATALOAD && code3 == PUSH1 && code5 == SHR &&
		//		code6 == DUP1 && code7 == PUSH4 && code12 == GT && code13 == PUSH2 {
		//		op := Push1CalldataloadPush1ShrDup1Push4GtPush2
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		fusedCode[cur+5] = byte(Nop)
		//		fusedCode[cur+6] = byte(Nop)
		//		fusedCode[cur+7] = byte(Nop)
		//		fusedCode[cur+12] = byte(Nop)
		//		fusedCode[cur+13] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if skipToNext {
		//		i += 15
		//		continue
		//	}
		//}
		//
		//if length > cur+12 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code1 := ByteCode(fusedCode[cur+1])
		//	code3 := ByteCode(fusedCode[cur+3])
		//	code4 := ByteCode(fusedCode[cur+4])
		//	code5 := ByteCode(fusedCode[cur+5])
		//	code6 := ByteCode(fusedCode[cur+6])
		//	code7 := ByteCode(fusedCode[cur+7])
		//	code8 := ByteCode(fusedCode[cur+8])
		//	code9 := ByteCode(fusedCode[cur+9])
		//	code10 := ByteCode(fusedCode[cur+10])
		//	code11 := ByteCode(fusedCode[cur+11])
		//	code12 := ByteCode(fusedCode[cur+12])
		//
		//	if code0 == SWAP1 && code1 == PUSH1 && code3 == DUP1 && code4 == NOT &&
		//		code5 == SWAP2 && code6 == ADD && code7 == AND && code8 == DUP2 &&
		//		code9 == ADD && code10 == SWAP1 && code11 == DUP2 && code12 == LT {
		//		op := Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		fusedCode[cur+4] = byte(Nop)
		//		fusedCode[cur+5] = byte(Nop)
		//		fusedCode[cur+6] = byte(Nop)
		//		fusedCode[cur+7] = byte(Nop)
		//		fusedCode[cur+8] = byte(Nop)
		//		fusedCode[cur+9] = byte(Nop)
		//		fusedCode[cur+10] = byte(Nop)
		//		fusedCode[cur+11] = byte(Nop)
		//		fusedCode[cur+12] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if skipToNext {
		//		i += 12
		//		continue
		//	}
		//}
		//
		//if length > cur+9 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code1 := ByteCode(fusedCode[cur+1])
		//	code6 := ByteCode(fusedCode[cur+6])
		//	code7 := ByteCode(fusedCode[cur+7])
		//
		//	if code0 == DUP1 && code1 == PUSH4 && code6 == EQ && code7 == PUSH2 {
		//		op := Dup1Push4EqPush2
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+6] = byte(Nop)
		//		fusedCode[cur+7] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if skipToNext {
		//		i += 9
		//		continue
		//	}
		//}
		//
		//if length > cur+7 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code2 := ByteCode(fusedCode[cur+2])
		//	code4 := ByteCode(fusedCode[cur+4])
		//	code6 := ByteCode(fusedCode[cur+6])
		//	code7 := ByteCode(fusedCode[cur+7])
		//
		//	if code0 == PUSH1 && code2 == PUSH1 && code4 == PUSH1 && code6 == SHL && code7 == SUB {
		//		op := Push1Push1Push1SHLSub
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+4] = byte(Nop)
		//		fusedCode[cur+6] = byte(Nop)
		//		fusedCode[cur+7] = byte(Nop)
		//		skipToNext = true
		//	}
		//	if skipToNext {
		//		i += 7
		//		continue
		//	}
		//}
		//
		//if length > cur+5 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code1 := ByteCode(fusedCode[cur+1])
		//	code2 := ByteCode(fusedCode[cur+2])
		//	code3 := ByteCode(fusedCode[cur+3])
		//	code4 := ByteCode(fusedCode[cur+4])
		//	code5 := ByteCode(fusedCode[cur+5])
		//
		//	if code0 == AND && code1 == DUP2 && code2 == ADD && code3 == SWAP1 && code4 == DUP2 && code5 == LT {
		//		op := AndDup2AddSwap1Dup2LT
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		fusedCode[cur+4] = byte(Nop)
		//		fusedCode[cur+5] = byte(Nop)
		//		skipToNext = true
		//	}
		//	if skipToNext {
		//		i += 5
		//		continue
		//	}
		//}

		//if length > cur+4 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code1 := ByteCode(fusedCode[cur+1])
		//	code2 := ByteCode(fusedCode[cur+2])
		//	code3 := ByteCode(fusedCode[cur+3])
		//	code4 := ByteCode(fusedCode[cur+4])
		//	if code0 == AND && code1 == SWAP1 && code2 == POP && code3 == SWAP2 && code4 == SWAP1 {
		//		op := AndSwap1PopSwap2Swap1
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		fusedCode[cur+4] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	// Test zero and Jump. target offset at code[2-3]
		//	if code0 == ISZERO && code1 == PUSH2 && code4 == JUMPI {
		//		op := JumpIfZero
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+4] = byte(Nop)
		//
		//		skipToNext = true
		//	}
		//
		//	//if code0 == DUP2 && code1 == MSTORE && code2 == PUSH1 && code4 == ADD {
		//	//	op := Dup2MStorePush1Add
		//	//	fusedCode[cur] = byte(op)
		//	//	fusedCode[cur+1] = byte(Nop)
		//	//	fusedCode[cur+2] = byte(Nop)
		//	//	fusedCode[cur+4] = byte(Nop)
		//	//
		//	//	skipToNext = true
		//	//}
		//
		//	if skipToNext {
		//		i += 4
		//		continue
		//	}
		//}

		//if length > cur+3 {
		//	code0 := ByteCode(fusedCode[cur+0])
		//	code1 := ByteCode(fusedCode[cur+1])
		//	code2 := ByteCode(fusedCode[cur+2])
		//	code3 := ByteCode(fusedCode[cur+3])
		//	if code0 == SWAP2 && code1 == SWAP1 && code2 == POP && code3 == JUMP {
		//		op := Swap2Swap1PopJump
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if code0 == SWAP1 && code1 == POP && code2 == SWAP2 && code3 == SWAP1 {
		//		op := Swap1PopSwap2Swap1
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if code0 == POP && code1 == SWAP2 && code2 == SWAP1 && code3 == POP {
		//		op := PopSwap2Swap1Pop
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+1] = byte(Nop)
		//		fusedCode[cur+2] = byte(Nop)
		//		fusedCode[cur+3] = byte(Nop)
		//		skipToNext = true
		//	}
		//	// push and jump
		//	if code0 == PUSH2 && code3 == JUMP {
		//		op := Push2Jump
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+3] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if code0 == PUSH2 && code3 == JUMPI {
		//		op := Push2JumpI
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+3] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	if code0 == PUSH1 && code2 == PUSH1 {
		//		op := Push1Push1
		//		fusedCode[cur] = byte(op)
		//		fusedCode[cur+2] = byte(Nop)
		//		skipToNext = true
		//	}
		//
		//	//if code0 == ISZERO && code1 == PUSH2 {
		//	//	op := IsZeroPush2
		//	//	fusedCode[cur] = byte(op)
		//	//	fusedCode[cur+1] = byte(Nop)
		//	//	skipToNext = true
		//	//}
		//
		//	if skipToNext {
		//		i += 3
		//		continue
		//	}
		//}

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
