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
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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
	Selector    string   // 4字节函数选择器
	PC          uint64   // 函数入口PC
	GasUsed     uint64   // 预估gas消耗
	StackOps    []string // 栈操作序列
	MemoryOps   []string // 内存操作序列
	StorageOps  []string // 存储操作序列
	InputParams []string // 输入参数
	ReturnData  []string // 返回数据
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
	code.WriteString("\t\"encoding/hex\"\n")
	code.WriteString("\t\"github.com/holiman/uint256\"\n")
	code.WriteString("\t\"github.com/ethereum/go-ethereum/common\"\n")
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
	code.WriteString(fmt.Sprintf("func (s *ShortcutImpl%s) Shortcut(pc uint64, inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, expected bool, err error) {\n", strings.ToUpper(sg.contractAddr.Hex()[2:])))

	// 生成函数选择器逻辑
	if len(sg.selectors) > 0 {
		code.WriteString("\t// 函数选择器分析\n")
		code.WriteString("\tif len(inputs) < 4 {\n")
		code.WriteString("\t\treturn 0, 0, nil, nil, false, nil\n")
		code.WriteString("\t}\n\n")

		code.WriteString("\tselector := hex.EncodeToString(inputs[:4])\n")
		code.WriteString("\tswitch selector {\n")

		for selector, info := range sg.selectors {
			code.WriteString(fmt.Sprintf("\tcase \"%s\":\n", selector))
			code.WriteString(fmt.Sprintf("\t\t// 函数: %s\n", selector))
			code.WriteString(fmt.Sprintf("\t\t// 预估Gas消耗: %d\n", info.GasUsed))
			code.WriteString(fmt.Sprintf("\t\t// 栈操作: %v\n", info.StackOps))
			code.WriteString(fmt.Sprintf("\t\t// 内存操作: %v\n", info.MemoryOps))
			code.WriteString(fmt.Sprintf("\t\t// 存储操作: %v\n", info.StorageOps))
			code.WriteString(fmt.Sprintf("\t\treturn %d, %d, nil, nil, true, nil\n", info.PC, info.GasUsed))
		}

		code.WriteString("\tdefault:\n")
		code.WriteString("\t\treturn 0, 0, nil, nil, false, nil\n")
		code.WriteString("\t}\n")
	} else {
		code.WriteString("\t// 未找到函数选择器\n")
		code.WriteString("\treturn 0, 0, nil, nil, false, nil\n")
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
