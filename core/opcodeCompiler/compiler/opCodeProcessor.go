package compiler

import (
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum/log"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
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
	maxOptimizedOpcode = 0xcf
)

type OpCodeProcessorConfig struct {
	DoOpcodeFusion bool
}

type optimizeTaskType byte

type CodeType uint8

type optimizeTask struct {
	taskType    optimizeTaskType
	hash        common.Hash
	rawCode     []byte
	blockNumber uint64
	txIndex     int
	addr        common.Address
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

func GenOrLoadOptimizedCode(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) {
	if !enabled {
		return
	}
	task := optimizeTask{generate, hash, code, blockNumber, txIndex, addr}
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
		TryGenerateOptimizedCode(task.hash, task.rawCode, task.blockNumber, task.txIndex, task.addr)
	case flush:
		DeleteCodeCache(task.hash)
	}
}

// GenOrRewriteOptimizedCode generate the optimized code and refresh the code cache.
func GenOrRewriteOptimizedCode(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) ([]byte, error) {
	if !enabled {
		return nil, ErrOptimizedDisabled
	}
	processedCode, err := processByteCodes(hash, code, blockNumber, txIndex, addr)
	if err != nil {
		return nil, err
	}
	codeCache.AddCodeCache(hash, processedCode)
	return processedCode, err
}

func TryGenerateOptimizedCode(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) ([]byte, error) {
	processedCode := codeCache.GetCachedCode(hash)
	var err error = nil
	if len(processedCode) == 0 {
		processedCode, err = GenOrRewriteOptimizedCode(hash, code, blockNumber, txIndex, addr)
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

func processByteCodes(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) ([]byte, error) {
	return doOpcodesProcess(hash, code, blockNumber, txIndex, addr)
}

func doOpcodesProcess(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) ([]byte, error) {
	code, err := doCodeFusion(hash, code, blockNumber, txIndex, addr)
	if err != nil {
		return nil, ErrFailPreprocessing
	}
	return code, nil
}

func doCodeFusion(hash common.Hash, code []byte, blockNumber uint64, txIndex int, addr common.Address) ([]byte, error) {
	fusedCode := make([]byte, len(code))
	length := copy(fusedCode, code)
	skipToNext := false
	//length := bytes.LastIndex(code, []byte{byte(INVALID)})

	for i := 0; i < length; i++ {
		cur := i
		skipToNext = false
		if fusedCode[cur] >= minOptimizedOpcode && fusedCode[cur] <= maxOptimizedOpcode {
			log.Error("raw opcode fall in optimized range", "length", length, "cur", cur, "fusedCode[cur]", fusedCode[cur], "blockNumber", blockNumber, "txIndex", txIndex, "addr", addr, "code", hex.EncodeToString(code))
			return code, ErrFailPreprocessing
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
				op := JumpIfZero
				fusedCode[cur] = byte(op)
				fusedCode[cur+1] = byte(Nop)
				fusedCode[cur+4] = byte(Nop)

				startMin := cur + 2
				endMin := cur + 4
				integer := new(uint256.Int)
				integer.SetBytes(common.RightPadBytes(
					fusedCode[startMin:endMin], 2))

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
				op := Push2Jump
				fusedCode[cur] = byte(op)
				fusedCode[cur+3] = byte(Nop)
				skipToNext = true
			}

			if code0 == PUSH2 && code3 == JUMPI {
				op := Push2JumpI
				fusedCode[cur] = byte(op)
				fusedCode[cur+3] = byte(Nop)
				skipToNext = true
			}

			if code0 == PUSH1 && code2 == PUSH1 {
				op := Push1Push1
				fusedCode[cur] = byte(op)
				fusedCode[cur+2] = byte(Nop)
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
