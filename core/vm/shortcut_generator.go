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
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	eth_math "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

type Selector struct {
	Selector [4]byte
	JumpDest uint64
}

type Jumper struct {
	JumpDest uint64
}

type DtreeNode[T any] struct {
	children map[OpCode]*DtreeNode[T]
	val      OpCode
	getMeta  func([]byte) *T // jump dest or selector
}

func (n *DtreeNode[T]) match(seq []byte, idx int) *T {
	if len(seq[idx:]) == 0 {
		return nil
	}
	if n.val != Nop && OpCode(seq[idx]) != n.val {
		return nil
	} else if n.getMeta != nil {
		return n.getMeta(seq)
	} else {
		for _, child := range n.children {
			if m := child.match(seq, idx+1); m != nil {
				return m
			}
		}
	}
	return nil
}

type DTree[T any] struct {
	root *DtreeNode[T]
}

func (t *DTree[T]) match(seq []byte, idx int) *T {
	for _, node := range t.root.children {
		return node.match(seq[idx:], 0)
	}
	return nil
}

type Pattern[T any] struct {
	Pattern    []OpCode
	MetaGetter func([]byte) *T
}

type Meta struct {
	Selector [4]byte
	JumpDest uint64
	Pattern  []OpCode
}

var (
	SelectorPattern = []Pattern[Meta]{
		{[]OpCode{DUP1, PUSH4, Nop, Nop, Nop, Nop, EQ, PUSH2, Nop, Nop, JUMPI}, func(ops []byte) *Meta {
			return &Meta{
				Selector: [4]byte(ops[2:6]),
				JumpDest: big.NewInt(0).SetBytes(ops[8:10]).Uint64(),
			}
		}},
	}
)

var (
	JumpPattern = []Pattern[Meta]{
		{[]OpCode{PUSH2, Nop, Nop, JUMP}, func(ops []byte) *Meta {
			return &Meta{
				JumpDest: big.NewInt(0).SetBytes(ops[1:3]).Uint64(),
			}
		}},
		{[]OpCode{PUSH2, Nop, Nop, JUMPI}, func(ops []byte) *Meta {
			return &Meta{
				JumpDest: big.NewInt(0).SetBytes(ops[1:3]).Uint64(),
			}
		}},
		{[]OpCode{PUSH3, Nop, Nop, Nop, JUMP}, func(ops []byte) *Meta {
			return &Meta{
				JumpDest: big.NewInt(0).SetBytes(ops[1:3]).Uint64(),
			}
		}},
		{[]OpCode{PUSH3, Nop, Nop, Nop, JUMPI}, func(ops []byte) *Meta {
			return &Meta{
				JumpDest: big.NewInt(0).SetBytes(ops[1:3]).Uint64(),
			}
		}},
	}
)

var (
	SelectorTree *DTree[Meta]
	JumperTree   *DTree[Meta]
)

func buildDTree(patterns []Pattern[Meta]) *DTree[Meta] {
	ret := &DTree[Meta]{
		root: &DtreeNode[Meta]{
			children: make(map[OpCode]*DtreeNode[Meta]),
			getMeta:  nil,
			val:      Nop,
		},
	}

	for _, pattern := range patterns {
		currentNode := ret.root
		for idx, op := range pattern.Pattern {
			if currentNode.children == nil {
				currentNode.children = make(map[OpCode]*DtreeNode[Meta])
			}

			if _, ok := currentNode.children[op]; !ok {
				currentNode.children[op] = &DtreeNode[Meta]{
					children: make(map[OpCode]*DtreeNode[Meta]),
					val:      op,
				}
			}

			currentNode = currentNode.children[op]

			if idx == len(pattern.Pattern)-1 {
				currentNode.getMeta = func(ops []byte) *Meta {
					meta := pattern.MetaGetter(ops)
					meta.Pattern = pattern.Pattern
					return meta
				}
			}
		}
	}

	return ret
}

func init() {
	SelectorTree = buildDTree(SelectorPattern)
	JumperTree = buildDTree(JumpPattern)
}

// ShortcutGenerator 用于生成Shortcut实现代码
type ShortcutGenerator struct {
	contractAddr common.Address
	opcodes      []byte
	jumpDest     []uint64
	selectors    map[string]*FunctionSelector
	gasCosts     map[string]uint64
}

// FunctionSelector 表示函数选择器信息
type FunctionSelector struct {
	Selector string // 4字节函数选择器
	PC       uint64 // 函数入口PC

	StackOps    []string // 栈操作序列
	MemoryOps   []string // 内存操作序列
	StorageOps  []string // 存储操作序列
	InputParams []string // 输入参数
	ReturnData  []string // 返回数据

	// from simulate execution
	GasUsed uint64 // 预估gas消耗
	Stack   []uint256.Int
	Memory  Memory
	SimErr  error
}

func (f *FunctionSelector) getSelectorBts() []byte {
	return hexutil.MustDecode(f.Selector)
}

// NewShortcutGenerator 创建新的ShortcutGenerator
func NewShortcutGenerator(contractAddr common.Address, opcodes []byte) *ShortcutGenerator {
	return &ShortcutGenerator{
		contractAddr: contractAddr,
		opcodes:      opcodes,
		jumpDest:     make([]uint64, len(opcodes)),
		selectors:    make(map[string]*FunctionSelector),
		gasCosts:     make(map[string]uint64),
	}
}

// GenerateShortcutCode 生成Shortcut实现代码
func (sg *ShortcutGenerator) GenerateShortcutCode() (string, error) {
	// 1. 分析opcode，识别函数选择器和入口点
	if err := sg.analyzeOpcodes(); err != nil {
		return "", fmt.Errorf("分析opcode失败: %v", err)
	}

	// 2. 生成Go代码
	return sg.generateGoCode(), nil
}

// analyzeOpcodes 分析opcode序列，识别函数选择器和入口点
func (sg *ShortcutGenerator) analyzeOpcodes() error {
	for i := 0; i < len(sg.opcodes); i++ {
		// 检查是否为函数选择器模式
		selectorMeta := SelectorTree.match(sg.opcodes, i)
		if selectorMeta != nil {
			selector := hexutil.Encode(selectorMeta.Selector[:])
			sg.selectors[selector] = &FunctionSelector{
				Selector: selector,
				PC:       selectorMeta.JumpDest,
			}
		}

		jumpMeta := JumperTree.match(sg.opcodes, i)
		if jumpMeta != nil {

		}
	}
	block := types.NewBlock(&types.Header{
		ParentHash:       common.Hash{},
		UncleHash:        common.Hash{},
		Coinbase:         common.Address{},
		Root:             common.Hash{},
		TxHash:           common.Hash{},
		ReceiptHash:      common.Hash{},
		Bloom:            types.Bloom{},
		Difficulty:       nil,
		Number:           big.NewInt(60000000),
		GasLimit:         0,
		GasUsed:          0,
		Time:             uint64(time.Now().Unix()),
		Extra:            nil,
		MixDigest:        common.Hash{},
		Nonce:            types.BlockNonce{},
		BaseFee:          nil,
		WithdrawalsHash:  nil,
		BlobGasUsed:      nil,
		ExcessBlobGas:    nil,
		ParentBeaconRoot: nil,
		RequestsHash:     nil,
	}, nil, nil, trie.NewStackTrie(nil))

	for _, selector := range sg.selectors {
		gas, _, stk, mem, _, err := analyzeCall(sg.contractAddr, sg.opcodes, selector.getSelectorBts(), selector.PC, block)
		if err != nil {
			selector.SimErr = err
		} else {
			selector.GasUsed = gas
			selector.Stack = make([]uint256.Int, len(stk.data))
			selector.Memory = Memory{
				store:       make([]byte, mem.Len()),
				lastGasCost: mem.lastGasCost,
			}

			for i, frame := range stk.data {
				selector.Stack[i].SetBytes(frame.Bytes())
			}

			copy(selector.Memory.store, mem.store)
		}
	}

	return nil
}

// getPushSize 获取PUSH操作码的数据大小
func (sg *ShortcutGenerator) getPushSize(op OpCode) int {
	if op == PUSH0 {
		return 0
	}
	if PUSH1 <= op && op <= PUSH32 {
		return int(op - PUSH1 + 1)
	}
	return 0
}

// generateGoCode 生成Go代码
func (sg *ShortcutGenerator) generateGoCode() string {
	var code strings.Builder

	// 生成包声明和导入
	code.WriteString("package impl\n\n")
	code.WriteString("import (\n")
	code.WriteString("\t\"errors\"\n")
	code.WriteString("\t\"github.com/holiman/uint256\"\n")
	code.WriteString("\t\"github.com/ethereum/go-ethereum/common\"\n")
	code.WriteString("\t\"github.com/ethereum/go-ethereum/common/hexutil\"\n")
	code.WriteString("\t\"github.com/ethereum/go-ethereum/core/opcodeCompiler/shortcut\"\n")
	code.WriteString(")\n\n")

	// 生成init函数
	code.WriteString("func init() {\n")
	code.WriteString(fmt.Sprintf("\timpl := new(ShortcutImpl%s)\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))
	code.WriteString("\tshortcut.RegisterShortcut(impl.Contract(), impl)\n")
	code.WriteString("}\n\n")

	// 生成结构体
	code.WriteString(fmt.Sprintf("type ShortcutImpl%s struct {\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))
	code.WriteString("}\n\n")

	// 生成Contract方法
	code.WriteString(fmt.Sprintf("func (s *ShortcutImpl%s) Contract() common.Address {\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))
	code.WriteString(fmt.Sprintf("\treturn common.HexToAddress(\"%s\")\n", sg.contractAddr.Hex()))
	code.WriteString("}\n\n")

	// 生成Shortcut方法
	code.WriteString(fmt.Sprintf("func (s *ShortcutImpl%s) Shortcut(inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, lastGasCost uint64, expected bool, err error) {\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))

	// 生成函数选择器逻辑
	if len(sg.selectors) > 0 {
		code.WriteString("\t// 函数选择器分析\n")
		code.WriteString("\tif len(inputs) < 4 {\n")
		code.WriteString("\t\treturn 0, 0, nil, nil, 0, false, nil\n")
		code.WriteString("\t}\n\n")

		code.WriteString("\tselector := string(inputs[:4])\n")
		code.WriteString("\tswitch selector {\n")

		for selector, info := range sg.selectors {
			if info.SimErr != nil {
				code.WriteString(fmt.Sprintf("\t\t // case %s has sim error %s \n", selector, info.SimErr.Error()))
				continue
			}

			stk, mem := info.Stack, info.Memory

			stkStr := "\n\t\t\t[]uint256.Int{\n"
			for _, item := range stk {
				stkStr += fmt.Sprintf("\t\t\t\t{%d, %d, %d, %d}, \n", item[0], item[1], item[2], item[3])
			}
			stkStr += "\t\t\t}"

			//memStr := fmt.Sprintf("\n\t\t\thexutil.MustDecode(\"%s\")", hexutil.Encode(mem.Data()))
			memStr := "[]byte{"
			for idx, item := range mem.Data() {
				if idx%32 == 0 {
					memStr += "\n\t\t"
				}
				memStr += fmt.Sprintf(" 0x%x", item)
				if idx != len(mem.Data())-1 {
					memStr += ","
				}
			}
			memStr += "}"

			selectorBts := info.getSelectorBts()

			selectorBtsStr := fmt.Sprintf("string([]byte{0x%x, 0x%x, 0x%x, 0x%x})", selectorBts[0], selectorBts[1], selectorBts[2], selectorBts[3])

			code.WriteString(fmt.Sprintf("\tcase %s:\n", selectorBtsStr))
			code.WriteString(fmt.Sprintf("\t\t// 函数: %s\n", selector))
			code.WriteString(fmt.Sprintf("\t\t// 预估Gas消耗: %d\n", info.GasUsed))
			code.WriteString(fmt.Sprintf("\t\t// 栈操作: %v\n", info.StackOps))
			code.WriteString(fmt.Sprintf("\t\t// 内存操作: %v\n", info.MemoryOps))
			code.WriteString(fmt.Sprintf("\t\t// 存储操作: %v\n", info.StorageOps))
			code.WriteString(fmt.Sprintf("\t\treturn %d, %d, %s, %s, %d, true, nil\n", info.PC, info.GasUsed, stkStr, memStr, mem.lastGasCost))
		}

		code.WriteString("\tdefault:\n")
		code.WriteString("\t\treturn 0, 0, nil, nil, 0, false, nil\n")
		code.WriteString("\t}\n")
	} else {
		code.WriteString("\t// 未找到函数选择器\n")
		code.WriteString("\treturn 0, 0, nil, nil, 0, false, nil\n")
	}

	code.WriteString("}\n\n")

	// 生成ShortcutV2方法
	code.WriteString(fmt.Sprintf("func (s *ShortcutImpl%s) ShortcutV2(\n\tinputs []byte, origin, caller common.Address, value *uint256.Int,\n\tshortcutPc *uint64, gasUsed *uint64, stack *[]uint256.Int, mem *[]byte, lastGasCost *uint64,\n) (expected bool, err error) {\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))

	// 生成函数选择器逻辑
	if len(sg.selectors) > 0 {
		code.WriteString("\t// 函数选择器分析\n")
		code.WriteString("\tif len(inputs) < 4 {\n")
		code.WriteString("\t\treturn false, nil\n")
		code.WriteString("\t}\n\n")

		code.WriteString("\tselector := string(inputs[:4])\n")
		code.WriteString("\tswitch selector {\n")

		for selector, info := range sg.selectors {
			if info.SimErr != nil {
				code.WriteString(fmt.Sprintf("\t\t // case %s has sim error %s \n", selector, info.SimErr.Error()))
				continue
			}

			stk, mem := info.Stack, info.Memory

			stkStr := "\n\t\t\t[]uint256.Int{\n"
			for _, item := range stk {
				stkStr += fmt.Sprintf("\t\t\t\t{%d, %d, %d, %d}, \n", item[0], item[1], item[2], item[3])
			}
			stkStr += "\t\t\t}"

			//memStr := fmt.Sprintf("\n\t\t\thexutil.MustDecode(\"%s\")", hexutil.Encode(mem.Data()))
			memStr := "[]byte{"
			for idx, item := range mem.Data() {
				if idx%32 == 0 {
					memStr += "\n\t\t"
				}
				memStr += fmt.Sprintf(" 0x%x", item)
				if idx != len(mem.Data())-1 {
					memStr += ","
				}
			}
			memStr += "}"

			selectorBts := info.getSelectorBts()

			selectorBtsStr := fmt.Sprintf("string([]byte{0x%x, 0x%x, 0x%x, 0x%x})", selectorBts[0], selectorBts[1], selectorBts[2], selectorBts[3])

			code.WriteString(fmt.Sprintf("\tcase %s:\n", selectorBtsStr))
			code.WriteString(fmt.Sprintf("\t\t// 函数: %s\n", selector))
			code.WriteString(fmt.Sprintf("\t\t// 预估Gas消耗: %d\n", info.GasUsed))
			code.WriteString(fmt.Sprintf("\t\t// 栈操作: %v\n", info.StackOps))
			code.WriteString(fmt.Sprintf("\t\t// 内存操作: %v\n", info.MemoryOps))
			code.WriteString(fmt.Sprintf("\t\t// 存储操作: %v\n", info.StorageOps))
			//code.WriteString(fmt.Sprintf("\t\treturn %d, %d, %s, %s, %d, true, nil\n", info.PC, info.GasUsed, stkStr, memStr, mem.lastGasCost))
			code.WriteString(fmt.Sprintf("\t\t*shortcutPc = %d\n", info.PC))
			code.WriteString(fmt.Sprintf("\t\t*gasUsed = %d\n", info.GasUsed))
			code.WriteString(fmt.Sprintf("\t\t*stack = %s\n", stkStr))
			code.WriteString(fmt.Sprintf("\t\t*mem = %s\n", memStr))
			code.WriteString(fmt.Sprintf("\t\t*lastGasCost = %d\n", mem.lastGasCost))

			code.WriteString(fmt.Sprintf("\t\treturn true, nil\n"))
		}

		code.WriteString("\tdefault:\n")
		code.WriteString("\t\treturn false, nil\n")
		code.WriteString("\t}\n")
	} else {
		code.WriteString("\t// 未找到函数选择器\n")
		code.WriteString("\treturn false, nil\n")
	}

	code.WriteString("}\n")

	return code.String()
}

// GenerateShortcutForContract 为指定合约生成Shortcut代码
func GenerateShortcutForContract(contractAddr common.Address, opcodes []byte) (string, error) {
	generator := NewShortcutGenerator(contractAddr, opcodes)
	return generator.GenerateShortcutCode()
}

// 示例用法
func ExampleUsage() {
	// 示例合约opcode (简单的transfer函数)
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// 这是一个简化的transfer函数opcode序列
	// PUSH4 0xa9059cbb (transfer函数选择器)
	// DUP1
	// PUSH4 0xffffffff
	// AND
	// EQ
	// PUSH2 0x0010 (跳转到PC=16)
	// JUMPI
	// ... 其他代码
	opcodes := []byte{
		0x63, 0xa9, 0x05, 0x9c, 0xbb, // PUSH4 0xa9059cbb
		0x80,                         // DUP1
		0x63, 0xff, 0xff, 0xff, 0xff, // PUSH4 0xffffffff
		0x16,             // AND
		0x14,             // EQ
		0x61, 0x00, 0x10, // PUSH2 0x0010
		0x57, // JUMPI
		// ... 更多opcode
	}

	code, err := GenerateShortcutForContract(contractAddr, opcodes)
	if err != nil {
		fmt.Printf("生成失败: %v\n", err)
		return
	}

	fmt.Println("生成的Shortcut代码:")
	fmt.Println(code)
}

var (
	simOpBlacklist = map[OpCode]bool{
		SSTORE: true,
		SLOAD:  true,
		LOG0:   true,
		LOG1:   true,
		LOG2:   true,
		LOG3:   true,
	}
)

func analyzeCall(addr common.Address, code []byte, input []byte, endPc uint64, block *types.Block) (gasUsed, opsUsed uint64, stack *Stack, mem *Memory, dynamicOps map[uint64]OpCode, err error) {
	//dynamicOps = make(map[uint64]OpCode)
	statedb := MockStateDB{}
	vmctx := BlockContext{
		Coinbase:    common.Address{},
		BlockNumber: block.Number(),
		Time:        block.Time(),
		Random:      &types.EmptyCodeHash,
	}
	evm := NewEVM(vmctx, statedb, params.BSCChainConfig, Config{})

	caller := AccountRef(common.Address{})
	codeHash := crypto.Keccak256Hash(code)

	initGas := uint64(math.MaxUint64) / 2
	interpreter := NewEVMInterpreter(evm)
	evm.interpreter = interpreter

	// At this point, we use a copy of address. If we don't, the go compiler will
	// leak the 'contract' to the outer scope, and make allocation for 'contract'
	// even if the actual execution ends on RunPrecompiled above.
	addrCopy := addr
	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	contract := GetContract(caller, AccountRef(addrCopy), new(uint256.Int), initGas)
	contract.SetCallCode(&addrCopy, codeHash, code)
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in Homestead this also counts for code storage gas errors.
	gasUsed, opsUsed, stk, mem, dynamicOps, err := evm.interpreter.RunUntilPc(contract, input, true, endPc)

	if statedb.touched && endPc != 0 {
		return 0, 0, nil, nil, dynamicOps, errors.New("sim err: statedb touched")
	}
	return gasUsed, opsUsed, stk, mem, dynamicOps, err
}

func (in *EVMInterpreter) RunUntilPc(contract *Contract, input []byte, readOnly bool, endPc uint64) (gasUsed, opsUsed uint64, stack_ *Stack, mem_ *Memory, dynamicOps map[uint64]OpCode, err error) {
	// Increment the call depth which is restricted to 1024
	dynamicOps = make(map[uint64]OpCode)
	in.evm.depth++
	defer func() { in.evm.depth-- }()

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
		return 0, 0, nil, nil, dynamicOps, errors.New("code is empty")
	}

	var (
		op    OpCode        // current opcode
		mem   = NewMemory() // bound memory
		stack = newstack()  // local stack

		callContext = &ScopeContext{
			Memory:   mem,
			Stack:    stack,
			Contract: contract,
		}

		// For optimisation reason we're using uint64 as the program counter.
		// It's theoretically possible to go above 2^64. The YP defines the PC
		// to be uint256. Practically much less so feasible.
		pc        = uint64(0) // program counter
		cost      uint64
		totalCost uint64
		// copies used by tracer
		//pcCopy  uint64 // needed for the deferred EVMLogger
		//gasCopy uint64 // for EVMLogger to log gas remaining before execution
		//logged  bool   // deferred EVMLogger should ignore already logged steps
		res []byte // result of the opcode execution function
		//debug = in.evm.Config.Tracer != nil
	)

	_ = res

	// Don't move this deferred function, it's placed before the OnOpcode-deferred method,
	// so that it gets executed _after_: the OnOpcode needs the stacks before
	// they are returned to the pools
	//defer func() {
	//	returnStack(stack)
	//	mem.Free()
	//}()
	contract.Input = input

	// The Interpreter main run loop (contextual). This loop runs until either an
	// explicit STOP, RETURN or SELFDESTRUCT is executed, an error occurred during
	// the execution of one of the operations or until the done flag is set by the
	// parent context.

	var ops uint64
	for {
		if pc == endPc && endPc != 0 {
			break
		}

		if in.evm.chainRules.IsEIP4762 && !contract.IsDeployment && !contract.IsSystemCall {
			// if the PC ends up in a new "chunk" of verkleized code, charge the
			// associated costs.
			contractAddr := contract.Address()
			contract.Gas -= in.evm.TxContext.AccessEvents.CodeChunksRangeGas(contractAddr, pc, 1, uint64(len(contract.Code)), false)
		}

		// Get the operation from the jump table and validate the stack to ensure there are
		// enough stack items available to perform the operation.
		op = contract.GetOp(pc)

		if _, found := simOpBlacklist[op]; found && endPc != 0 {
			return 0, 0, nil, nil, dynamicOps, errors.New(fmt.Sprintf("op %s is not blacklisted", op.String()))
		}

		operation := in.table[op]

		if operation.dynamicGas != nil && endPc != 0 {
			switch op {
			case MSTORE:
				fmt.Println("debug")
			default:
				return 0, 0, nil, nil, dynamicOps, errors.New("dynamic gas is not supported")
			}
		}

		cost = operation.constantGas // For tracing
		// Validate stack
		if sLen := stack.len(); sLen < operation.minStack {
			return 0, 0, nil, nil, dynamicOps, &ErrStackUnderflow{stackLen: sLen, required: operation.minStack}
		} else if sLen > operation.maxStack {
			return 0, 0, nil, nil, dynamicOps, &ErrStackOverflow{stackLen: sLen, limit: operation.maxStack}
		}
		// for tracing: this gas consumption event is emitted below in the debug section.
		if contract.Gas < cost {
			return 0, 0, nil, nil, dynamicOps, ErrOutOfGas
		} else {
			contract.Gas -= cost
		}

		// All ops with a dynamic memory usage also has a dynamic gas cost.
		var memorySize uint64
		if operation.dynamicGas != nil {
			dynamicOps[pc] = op
			// calculate the new memory size and expand the memory to fit
			// the operation
			// Memory check needs to be done prior to evaluating the dynamic gas portion,
			// to detect calculation overflows
			if operation.memorySize != nil {
				memSize, overflow := operation.memorySize(stack)
				if overflow {
					return 0, 0, nil, nil, dynamicOps, ErrGasUintOverflow
				}
				// memory is expanded in words of 32 bytes. Gas
				// is also calculated in words.
				if memorySize, overflow = eth_math.SafeMul(toWordSize(memSize), 32); overflow {
					return 0, 0, nil, nil, dynamicOps, ErrGasUintOverflow
				}
			}
			// Consume the gas and return an error if not enough gas is available.
			// cost is explicitly set so that the capture state defer method can get the proper cost
			// cost is explicitly set so that the capture state defer method can get the proper cost
			var dynamicCost uint64
			dynamicCost, err = operation.dynamicGas(in.evm, contract, stack, mem, memorySize)
			cost += dynamicCost // for tracing
			if err != nil {
				return 0, 0, nil, nil, dynamicOps, fmt.Errorf("%w: %v", ErrOutOfGas, err)
			}
			// for tracing: this gas consumption event is emitted below in the debug section.
			if contract.Gas < dynamicCost {
				return 0, 0, nil, nil, dynamicOps, ErrOutOfGas
			} else {
				contract.Gas -= dynamicCost
			}
		}

		totalCost += cost

		if memorySize > 0 {
			mem.Resize(memorySize)
		}

		res, err = operation.execute(&pc, in, callContext)
		if err != nil {
			break
		}

		pc++
		ops++
	}

	if err == errStopToken {
		err = nil // clear stop token error
	}

	if pc != endPc && endPc != 0 {
		return 0, 0, nil, nil, dynamicOps, errors.New("sim err: unexpected end pc")
	}

	return totalCost, ops, stack, mem, dynamicOps, err
}
