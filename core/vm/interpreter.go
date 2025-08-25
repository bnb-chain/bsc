// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	"strings"
)

// Config are the configuration options for the Interpreter
type Config struct {
	Tracer                    *tracing.Hooks
	NoBaseFee                 bool  // Forces the EIP-1559 baseFee to 0 (needed for 0 price calls)
	EnablePreimageRecording   bool  // Enables recording of SHA3/keccak preimages
	ExtraEips                 []int // Additional EIPS that are to be enabled
	EnableOpcodeOptimizations bool  // Enable opcode optimization

	StatelessSelfValidation bool // Generate execution witnesses and self-check against them (testing purpose)
}

// ScopeContext contains the things that are per-call, such as stack and memory,
// but not transients like pc and gas
type ScopeContext struct {
	Memory   *Memory
	Stack    *Stack
	Contract *Contract
}

// MemoryData returns the underlying memory slice. Callers must not modify the contents
// of the returned data.
func (ctx *ScopeContext) MemoryData() []byte {
	if ctx.Memory == nil {
		return nil
	}
	return ctx.Memory.Data()
}

// StackData returns the stack data. Callers must not modify the contents
// of the returned data.
func (ctx *ScopeContext) StackData() []uint256.Int {
	if ctx.Stack == nil {
		return nil
	}
	return ctx.Stack.Data()
}

// Caller returns the current caller.
func (ctx *ScopeContext) Caller() common.Address {
	return ctx.Contract.Caller()
}

// Address returns the address where this scope of execution is taking place.
func (ctx *ScopeContext) Address() common.Address {
	return ctx.Contract.Address()
}

// CallValue returns the value supplied with this call.
func (ctx *ScopeContext) CallValue() *uint256.Int {
	return ctx.Contract.Value()
}

// CallInput returns the input/calldata with this call. Callers must not modify
// the contents of the returned data.
func (ctx *ScopeContext) CallInput() []byte {
	return ctx.Contract.Input
}

// ContractCode returns the code of the contract being executed.
func (ctx *ScopeContext) ContractCode() []byte {
	return ctx.Contract.Code
}

// EVMInterpreter represents an EVM interpreter
type EVMInterpreter struct {
	evm   *EVM
	table *JumpTable

	hasher    crypto.KeccakState // Keccak256 hasher instance shared across opcodes
	hasherBuf common.Hash        // Keccak256 hasher result array shared across opcodes

	readOnly   bool   // Whether to throw on stateful modifications
	returnData []byte // Last CALL's return data for subsequent reuse
}

// NewEVMInterpreter returns a new instance of the Interpreter.
func NewEVMInterpreter(evm *EVM) *EVMInterpreter {
	// If jump table was not initialised we set the default one.
	var table *JumpTable
	switch {
	case evm.chainRules.IsVerkle:
		// TODO replace with proper instruction set when fork is specified
		table = &verkleInstructionSet
	case evm.chainRules.IsPrague:
		table = &pragueInstructionSet
	case evm.chainRules.IsCancun:
		table = &cancunInstructionSet
	case evm.chainRules.IsShanghai:
		table = &shanghaiInstructionSet
	case evm.chainRules.IsMerge:
		table = &mergeInstructionSet
	case evm.chainRules.IsLondon:
		table = &londonInstructionSet
	case evm.chainRules.IsBerlin:
		table = &berlinInstructionSet
	case evm.chainRules.IsIstanbul:
		table = &istanbulInstructionSet
	case evm.chainRules.IsConstantinople:
		table = &constantinopleInstructionSet
	case evm.chainRules.IsByzantium:
		table = &byzantiumInstructionSet
	case evm.chainRules.IsEIP158:
		table = &spuriousDragonInstructionSet
	case evm.chainRules.IsEIP150:
		table = &tangerineWhistleInstructionSet
	case evm.chainRules.IsHomestead:
		table = &homesteadInstructionSet
	default:
		table = &frontierInstructionSet
	}

	var extraEips []int
	if len(evm.Config.ExtraEips) > 0 {
		// Deep-copy jumptable to prevent modification of opcodes in other tables
		table = copyJumpTable(table)
	}
	for _, eip := range evm.Config.ExtraEips {
		if err := EnableEIP(eip, table); err != nil {
			// Disable it, so caller can check if it's activated or not
			log.Error("EIP activation failed", "eip", eip, "error", err)
		} else {
			extraEips = append(extraEips, eip)
		}
	}
	evm.Config.ExtraEips = extraEips
	return &EVMInterpreter{evm: evm, table: table}
}

func (in *EVMInterpreter) CopyAndInstallSuperInstruction() {
	table := copyJumpTable(in.table)
	in.table = createOptimizedOpcodeTable(table)
}

// Run loops and evaluates the contract's code with the given input data and returns
// the return byte-slice and an error if one occurred.
//
// It's important to note that any errors returned by the interpreter should be
// considered a revert-and-consume-all-gas operation except for
// ErrExecutionReverted which means revert-and-keep-gas-left.
func (in *EVMInterpreter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	// Increment the call depth which is restricted to 1024
	in.evm.depth++
	defer func() { in.evm.depth-- }()

	// Frame enter log
	if in.evm.Context.BlockNumber.Uint64() == 50897372 && in.evm.StateDB.TxIndex() == 291 {
		depthNow := in.evm.depth

		log.Error("[FRAME ENTER]", "depth", depthNow, "gas", contract.Gas, "codeHash", contract.CodeHash.String())
		// Frame exit log
		defer func() {
			log.Error("[FRAME EXIT]", "depth", depthNow, "gas", contract.Gas, "codeHash", contract.CodeHash.String())
		}()
	}

	// Make sure the readOnly is only set if we aren't in readOnly yet.
	// This also makes sure that the readOnly flag isn't removed for child calls.
	if readOnly && !in.readOnly {
		in.readOnly = true
		defer func() { in.readOnly = false }()
	}

	// Reset the previous call's return data. It's unimportant to preserve the old buffer
	// as every returning call will return new data anyway.
	in.returnData = nil

	// Don't bother with the execution if there's no code.
	if len(contract.Code) == 0 {
		return nil, nil
	}

	var (
		op          OpCode        // current opcode
		mem         = NewMemory() // bound memory
		stack       = newstack()  // local stack
		callContext = &ScopeContext{
			Memory:   mem,
			Stack:    stack,
			Contract: contract,
		}
		// For optimisation reason we're using uint64 as the program counter.
		// It's theoretically possible to go above 2^64. The YP defines the PC
		// to be uint256. Practically much less so feasible.
		pc                = uint64(0) // program counter
		cost              uint64
		blockChargeActive bool   // static gas precharge mode flag
		totalCost         uint64 // for debug only
		// copies used by tracer
		pcCopy          uint64 // needed for the deferred EVMLogger
		gasCopy         uint64 // for EVMLogger to log gas remaining before execution
		logged          bool   // deferred EVMLogger should ignore already logged steps
		res             []byte // result of the opcode execution function
		debug           = in.evm.Config.Tracer != nil
		currentBlock    *compiler.BasicBlock // 当前block（缓存）
		nextBlockPC     uint64               // 下一个block的起始PC（用于边界检测）
		totalDynamicGas uint64               // 本次调用累积的动态gas
	)
	// initialise blockChargeActive to whether opcode optimizations are enabled
	blockChargeActive = in.evm.Config.EnableOpcodeOptimizations

	// Don't move this deferred function, it's placed before the OnOpcode-deferred method,
	// so that it gets executed _after_: the OnOpcode needs the stacks before
	// they are returned to the pools
	defer func() {
		returnStack(stack)
		mem.Free()
	}()
	contract.Input = input

	if debug {
		defer func() { // this deferred method handles exit-with-error
			if err == nil {
				return
			}
			if !logged && in.evm.Config.Tracer.OnOpcode != nil {
				in.evm.Config.Tracer.OnOpcode(pcCopy, byte(op), gasCopy, cost, callContext, in.returnData, in.evm.depth, VMErrorFromErr(err))
			}
			if logged && in.evm.Config.Tracer.OnFault != nil {
				in.evm.Config.Tracer.OnFault(pcCopy, byte(op), gasCopy, cost, callContext, in.evm.depth, VMErrorFromErr(err))
			}
		}()
	}
	// The Interpreter main run loop (contextual). This loop runs until either an
	// explicit STOP, RETURN or SELFDESTRUCT is executed, an error occurred during
	// the execution of one of the operations or until the done flag is set by the
	// parent context.
	for {
		if debug {
			// Capture pre-execution values for tracing.
			logged, pcCopy, gasCopy = false, pc, contract.Gas
		}

		if in.evm.chainRules.IsEIP4762 && !contract.IsDeployment && !contract.IsSystemCall {
			// if the PC ends up in a new "chunk" of verkleized code, charge the
			// associated costs.
			contractAddr := contract.Address()
			contract.Gas -= in.evm.TxContext.AccessEvents.CodeChunksRangeGas(contractAddr, pc, 1, uint64(len(contract.Code)), false)
		}

		if blockChargeActive {
			// 只在以下情况检查block边界：
			// 1. 当前block为空（首次执行）
			// 2. PC超出了当前block范围（向前或向后）
			if currentBlock == nil || pc >= nextBlockPC || pc <= currentBlock.StartPC {
				if block, found := compiler.GetBlockByPC(contract.CodeHash, pc); found {
					// 先确认余额是否足够支付 staticGas
					if contract.Gas >= block.StaticGas {
						contract.Gas -= block.StaticGas
						// 扣费成功后，再正式切换 currentBlock
						currentBlock = block
						nextBlockPC = block.EndPC
					} else {
						blockChargeActive = false
						currentBlock = nil
					}
				} else {
					// cache 缺失
					blockChargeActive = false
					currentBlock = nil
				}
			}
		}

		// Get the operation from the jump table and validate the stack to ensure there are
		// enough stack items available to perform the operation.
		op = contract.GetOp(pc)
		operation := in.table[op]
		// Validate stack
		if sLen := stack.len(); sLen < operation.minStack {
			return nil, &ErrStackUnderflow{stackLen: sLen, required: operation.minStack}
		} else if sLen > operation.maxStack {
			return nil, &ErrStackOverflow{stackLen: sLen, limit: operation.maxStack}
		}
		// for tracing: this gas consumption event is emitted below in the debug section.
		// Only charge gas if we haven't already charged the pre-calculated static gas
		cost = operation.constantGas // For tracing todo: move into if
		totalCost += cost
		// New debug line: print opcode, pc and its static gas each step
		log.Error("[OP STATIC]", "pc", pc, "opcode", op.String(), "staticGas", cost, "totalCost", totalCost)
		// 暂不打印，改为在动态 gas 处理后统一输出（保证包含 dynamic 与 chunk 等影响后的净消耗）
		if !blockChargeActive {

			if contract.Gas < cost {
				// 如果是超指令，尝试拆分执行，尽量与 disable-path 失败情况对齐，如果不是超指令，不需要做任何事
				if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
					// refund all pre-reduced basic block gas until before this pc (so pc-1)
					in.refundUnusedBlockGas(contract, pc-1, currentBlock)
					if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
						// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
						blockChargeActive = false
						currentBlock = nil
						continue
					}
				}
				log.Error("Out of gas", "pc", pc, "required", cost, "available", contract.Gas, "contract.CodeHash", contract.CodeHash.String())
				return nil, ErrOutOfGas
			} else {
				contract.Gas -= cost
			}
		}

		// All ops with a dynamic memory usage also has a dynamic gas cost.
		var memorySize uint64
		if operation.dynamicGas != nil {
			// calculate the new memory size and expand the memory to fit
			// the operation
			// Memory check needs to be done prior to evaluating the dynamic gas portion,
			// to detect calculation overflows
			if operation.memorySize != nil {
				memSize, overflow := operation.memorySize(stack)
				if overflow {
					if blockChargeActive {
						if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
							in.refundUnusedBlockGas(contract, pc-1, currentBlock)
							if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
								// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
								blockChargeActive = false
								currentBlock = nil
								continue
							}
						} else {
							in.refundUnusedBlockGas(contract, pc, currentBlock)
						}
					}
					return nil, ErrGasUintOverflow
				}
				// memory is expanded in words of 32 bytes. Gas
				// is also calculated in words.
				if memorySize, overflow = math.SafeMul(toWordSize(memSize), 32); overflow {
					if blockChargeActive {
						if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
							in.refundUnusedBlockGas(contract, pc-1, currentBlock)
							if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
								// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
								blockChargeActive = false
								currentBlock = nil
								continue
							}
						} else {
							in.refundUnusedBlockGas(contract, pc, currentBlock)
						}
					}
					return nil, ErrGasUintOverflow
				}
			}
			// Consume the gas and return an error if not enough gas is available.
			// cost is explicitly set so that the capture state defer method can get the proper cost
			// cost is explicitly set so that the capture state defer method can get the proper cost
			var dynamicCost uint64
			dynamicCost, err = operation.dynamicGas(in.evm, contract, stack, mem, memorySize)
			// 如果首次尝试因静态预扣导致 OOG，则退回未用静态 gas 后重试一次
			if err != nil {
				if blockChargeActive {
					if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
						in.refundUnusedBlockGas(contract, pc-1, currentBlock)
						if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
							// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
							blockChargeActive = false
							currentBlock = nil
							continue
						}
					} else {
						in.refundUnusedBlockGas(contract, pc, currentBlock)
					}
				}
				return nil, fmt.Errorf("%w: %v", ErrOutOfGas, err)
			}
			cost += dynamicCost // for tracing
			totalDynamicGas += dynamicCost
			// for tracing: this gas consumption event is emitted below in the debug section.
			if contract.Gas < dynamicCost {
				if blockChargeActive {
					if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
						in.refundUnusedBlockGas(contract, pc-1, currentBlock)
						if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
							// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
							blockChargeActive = false
							currentBlock = nil
							continue
						}
					} else {
						in.refundUnusedBlockGas(contract, pc, currentBlock)
					}
				}
				return nil, ErrOutOfGas
			} else {
				contract.Gas -= dynamicCost
			}
		}

		// Do tracing before potential memory expansion
		if debug {
			if in.evm.Config.Tracer.OnGasChange != nil {
				in.evm.Config.Tracer.OnGasChange(gasCopy, gasCopy-cost, tracing.GasChangeCallOpCode)
			}
			if in.evm.Config.Tracer.OnOpcode != nil {
				in.evm.Config.Tracer.OnOpcode(pc, byte(op), gasCopy, cost, callContext, in.returnData, in.evm.depth, VMErrorFromErr(err))
				logged = true
			}
		}
		if memorySize > 0 {
			mem.Resize(memorySize)
		}

		// execute the operation
		res, err = operation.execute(&pc, in, callContext)
		if err != nil {
			if blockChargeActive {
				if seq, isSuper := DecomposeSuperInstruction(op); isSuper {
					in.refundUnusedBlockGas(contract, pc-1, currentBlock)
					if err := in.tryFallbackForSuperInstruction(&pc, seq, contract, stack, mem, callContext); err == nil {
						// fallback 成功执行到真正 OOG 或全部跑完，继续主循环
						blockChargeActive = false
						currentBlock = nil
						continue
					}
				} else {
					in.refundUnusedBlockGas(contract, pc, currentBlock)
				}
			}
			break
		}
		pc++
	}

	if err == errStopToken {
		err = nil // clear stop token error
	}

	return res, err
}

// calculateUsedBlockGas calculates the gas cost for opcodes from startPC to endPC (inclusive)
func (in *EVMInterpreter) calculateUsedBlockGas(contract *Contract, startPC, endPC uint64) uint64 {
	if startPC > endPC {
		return 0
	}

	totalGas := uint64(0)
	pc := startPC

	for pc <= endPC {
		op := contract.GetOp(pc)
		operation := in.table[op]

		// Add static gas for this opcode (only if operation exists)
		if operation != nil {
			totalGas += operation.constantGas
		}

		// 遇到控制流转移或终止类指令，代表本 block 的执行在此处终止。
		// 退款只应计算到“真实执行到的最后一条指令”为止，
		// 因此在累计完本条指令的静态 gas 后立即停止扫描。
		switch op {
		case JUMP, JUMPI, STOP, RETURN, REVERT, INVALID,
			Swap2Swap1PopJump, // SWAP2SWAP1POPJUMP - 超指令，内部包含跳转
			Push2JumpI,        // PUSH2JUMPI - 超指令，内部包含条件跳转
			PopJump,           // POPJUMP    - 超指令，内部包含跳转
			JumpIfZero:        // JUMPIFZERO - 超指令，内部包含条件跳转
			return totalGas
		}

		// Prefer compiler's skip for PUSH 和部分已覆盖的超指令
		if skip, steps := compiler.CalculateSkipSteps(contract.Code, int(pc)); skip {
			pc += uint64(steps) + 1
			continue
		}

		// 未被 CalculateSkipSteps 覆盖的指令：对齐 Run() 中各超指令的 PC 前进规则
		switch op {
		// 已覆盖但为完整性列出：
		// 超指令与自定义：步进与 instructions.go 中一致（内部自增 + 解释器循环自增）
		case Nop:
			pc += 1 // opNop: 仅解释器自增
			continue
		case AndSwap1PopSwap2Swap1:
			pc += 5 // *pc += 4 + 解释器自增
			continue
		case Swap1PopSwap2Swap1:
			pc += 4 // *pc += 3 + 解释器自增
			continue
		case PopSwap2Swap1Pop:
			pc += 4 // *pc += 3 + 解释器自增
			continue
			// 带跳转的超指令（无法在静态重放中解析跳转目的地），交由前置 CalculateSkipSteps 处理其立即数，
			// 此处不做专门跳转模拟（以保持线性扫描）。
		case Swap2Pop:
			pc += 2 // *pc += 1，然后解释器 +1
			continue
		case Swap2Swap1:
			pc += 2
			continue
		case Swap1Pop:
			pc += 2
			continue
		case Pop2:
			pc += 2
			continue
		case Dup2LT:
			pc += 2
			continue
		case Push1Add:
			pc += 3 // *pc +=1(读取立即数) + *pc +=1(指令消耗) + 解释器+1
			continue
		case Push1Shl:
			pc += 3
			continue
		case Push1Dup1:
			pc += 3
			continue
		case Push1Push1:
			pc += 4 // *pc +=3 + 解释器+1
			continue
		case IsZeroPush2:
			pc += 4 // *pc +=1 + push2(*pc +=2) + 解释器+1
			continue
		case Dup2MStorePush1Add:
			pc += 5 // *pc +=3 + *pc +=1(读取PUSH1立即数) + 解释器+1
			continue
		case Dup1Push4EqPush2:
			pc += 10 // +1(dup1) +4(push4) +1(eq) +2(push2) +1(解释器) +1(loop increment)
			continue
		case Push1CalldataloadPush1ShrDup1Push4GtPush2:
			pc += 16 // 1+3+2+1+5+1+2 +1(解释器)
			continue
		case Push1Push1Push1SHLSub:
			pc += 8 // 1+2+2+2 +1(解释器)
			continue
		case AndDup2AddSwap1Dup2LT:
			pc += 6 // *pc +=5 +1(解释器)
			continue
		case Swap1Push1Dup1NotSwap2AddAndDup2AddSwap1Dup2LT:
			pc += 13 // *pc +=12 +1(解释器)
			continue
		case Dup3And:
			pc += 2 // *pc +=1 +1(解释器)
			continue
		case Swap2Swap1Dup3SubSwap2Dup3GtPush2:
			pc += 10 // *pc +=7 +2(push2) +1(解释器)
			continue
		case Swap1Dup2:
			pc += 2 // *pc +=1 +1(解释器)
			continue
		case SHRSHRDup1MulDup1:
			pc += 5 // *pc +=4 +1(解释器)
			continue
		case Swap3PopPopPop:
			pc += 4 // *pc +=3 +1(解释器)
			continue
		case SubSLTIsZeroPush2:
			pc += 6 // *pc +=3 +2(push2) +1(解释器)
			continue
		case Dup11MulDup3SubMulDup1:
			pc += 6 // *pc +=5 +1(解释器)
			continue
		}

		// 默认：单字节指令
		pc++
	}

	return totalGas
}

// refundUnusedBlockGas refunds unused block gas when optimization is enabled and current op not superinstruction
func (in *EVMInterpreter) refundUnusedBlockGas(contract *Contract, pc uint64, currentBlock *compiler.BasicBlock) uint64 {
	if currentBlock == nil {
		return 0
	}

	var actualUsedGas uint64
	actualUsedGas = in.calculateUsedBlockGas(contract, currentBlock.StartPC, pc)
	if actualUsedGas >= currentBlock.StaticGas {
		return 0
	}

	usedGasDiff := currentBlock.StaticGas - actualUsedGas
	// Debug log: show refund calculation for low-noise target tx
	//debugLowNoise := in.evm.Context.BlockNumber.Uint64() == 50897362 && in.evm.StateDB.TxIndex() == 184
	//if debugLowNoise {
	//	log.Error("[REFUND]", "blockStart", currentBlock.StartPC, "pc", pc, "staticGas", currentBlock.StaticGas, "actualUsed", actualUsedGas, "refund", usedGasDiff, "gasBeforeRefund", contract.Gas)
	//}
	//beforeGas := contract.Gas
	contract.Gas += usedGasDiff
	//if debugLowNoise {
	//	log.Error("[GAS]", "action", "Refund", "blockStart", currentBlock.StartPC, "delta", int64(usedGasDiff), "before", beforeGas, "after", contract.Gas, "depth", in.evm.depth)
	//}
	// 追踪关键帧的退款操作
	//isTargetFrameRefund := debugLowNoise && (in.evm.depth == 2)
	//if debugLowNoise && usedGasDiff > 0 && isTargetFrameRefund {
	//	// 注意：退款意味着实际消耗的 gas 比预扣的少，所以这里显示的是实际净消耗
	//	log.Error("[FRAME_GAS]", "action", "Refund", "depth", in.evm.depth, "blockStart", currentBlock.StartPC, "actualUsed", actualUsedGas, "staticGas", currentBlock.StaticGas, "refund", usedGasDiff, "netConsumption", currentBlock.StaticGas-usedGasDiff, "before", beforeGas, "after", contract.Gas, "enableOpt", true)
	//}
	return usedGasDiff
}

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

func (in *EVMInterpreter) executeSingleOpcode(pc *uint64, op OpCode, contract *Contract, stack *Stack, mem *Memory, callCtx *ScopeContext) error {
	operation := in.table[op]
	if operation == nil {
		return fmt.Errorf("unknown opcode %02x", op)
	}

	// -------- 常量费检查 --------
	if contract.Gas < operation.constantGas {
		return ErrOutOfGas
	}
	contract.Gas -= operation.constantGas

	// -------- 动态费与内存扩张 --------
	var memorySize uint64
	if operation.memorySize != nil {
		memSize, overflow := operation.memorySize(stack)
		if overflow {
			return ErrGasUintOverflow
		}
		if memorySize, overflow = math.SafeMul(toWordSize(memSize), 32); overflow {
			return ErrGasUintOverflow
		}
	}

	if operation.dynamicGas != nil {
		dyn, err := operation.dynamicGas(in.evm, contract, stack, mem, memorySize)
		if err != nil {
			return err
		}
		if contract.Gas < dyn {
			return ErrOutOfGas
		}
		contract.Gas -= dyn
	}

	if memorySize > 0 {
		mem.Resize(memorySize)
	}

	// -------- 真正执行 --------
	_, err := operation.execute(pc, in, callCtx)
	return err
}

// tryFallbackForSuperInstruction 将超指令拆分为普通指令并依次执行，直到真正耗尽 gas 或全部成功。
// 返回 nil 表示已成功执行到超指令末尾或中途 OOG（并已正确更新 pc / gas），上层应继续主循环。
func (in *EVMInterpreter) tryFallbackForSuperInstruction(pc *uint64, seq []OpCode, contract *Contract, stack *Stack, mem *Memory, callCtx *ScopeContext) error {
	startPC := *pc

	log.Error("[FALLBACK]", "start", startPC, "seqLen", len(seq))

	for _, sub := range seq {
		log.Error("[FALLBACK-EXEC]", "pc", *pc, "op", sub.String(), "gasBefore", contract.Gas)
		if err := in.executeSingleOpcode(pc, sub, contract, stack, mem, callCtx); err != nil {
			log.Error("[FALLBACK-EXEC]", "op", sub.String(), "err", err, "gasLeft", contract.Gas)
			return err // OutOfGas 或其他错误，上层会如常处理
		}
		log.Error("[FALLBACK-EXEC]", "ok", true, "nextPC", *pc, "gasAfter", contract.Gas)
	}
	return nil
}
