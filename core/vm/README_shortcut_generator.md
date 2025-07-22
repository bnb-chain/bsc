# Shortcut Generator

## 概述

`shortcut_generator.go` 是一个用于静态分析智能合约opcode序列并生成实现Shortcut接口的Go代码的工具。它能够：

1. 分析合约opcode序列，识别函数选择器和入口点
2. 计算每个函数的预估gas消耗
3. 记录栈操作、内存操作和存储操作序列
4. 生成符合Shortcut接口的Go代码

## 主要功能

### 1. 函数选择器识别
- 识别典型的函数选择器模式：`PUSH4 selector + DUP1 + PUSH4 0xffffffff + AND + EQ + PUSH2 target + JUMPI`
- 支持ERC20、ERC721等标准合约的函数选择器

### 2. Gas消耗计算
- 为每个操作码计算预估gas消耗
- 支持所有EVM操作码的gas计算
- 包括基础操作、存储操作、跳转操作等

### 3. 操作序列分析
- **栈操作**：记录PUSH、POP、DUP、SWAP等栈操作
- **内存操作**：记录MSTORE、MLOAD、CALLDATACOPY等内存操作
- **存储操作**：记录SSTORE、SLOAD等存储操作

### 4. 代码生成
- 生成符合Shortcut接口的Go代码
- 自动注册到shortcut系统中
- 支持多个函数选择器的switch语句

## 使用方法

### 基本用法

```go
import (
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/core/vm"
)

// 创建生成器
contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
opcodes := []byte{/* 合约opcode序列 */}
generator := vm.NewShortcutGenerator(contractAddr, opcodes)

// 生成代码
code, err := generator.GenerateShortcutCode()
if err != nil {
    // 处理错误
}

// 保存到文件
err = vm.GenerateShortcutFile(contractAddr, opcodes, "output/dir")
```

### 示例：ERC20代币

```go
// ERC20代币合约地址
contractAddr := common.HexToAddress("0x55d398326f99059ff775485246999027b3197955")

// ERC20合约opcode序列
opcodes := []byte{
    // totalSupply()
    0x63, 0x18, 0x16, 0x0d, 0xdd, // PUSH4 0x18160ddd
    0x80,                           // DUP1
    0x63, 0xff, 0xff, 0xff, 0xff, // PUSH4 0xffffffff
    0x16,                           // AND
    0x14,                           // EQ
    0x61, 0x00, 0x30,              // PUSH2 0x0030
    0x57,                           // JUMPI
    
    // balanceOf(address)
    0x63, 0x70, 0xa0, 0x82, 0x31, // PUSH4 0x70a08231
    // ... 更多opcode
}

// 生成Shortcut代码
code, err := vm.GenerateShortcutForContract(contractAddr, opcodes)
```

## 生成的文件结构

生成的Go文件包含：

```go
package impl

import (
    "encoding/hex"
    "github.com/holiman/uint256"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/core/opcodeCompiler/shortcut"
)

func init() {
    impl := new(ShortcutImpl0x55d398326f99059ff775485246999027b3197955)
    shortcut.RegisterShortcut(impl.Contract(), impl)
}

type ShortcutImpl0x55d398326f99059ff775485246999027b3197955 struct {
}

func (s *ShortcutImpl0x55d398326f99059ff775485246999027b3197955) Contract() common.Address {
    return common.HexToAddress("0x55d398326f99059ff775485246999027b3197955")
}

func (s *ShortcutImpl0x55d398326f99059ff775485246999027b3197955) Shortcut(pc uint64, inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, expected bool, err error) {
    // 函数选择器分析
    if len(inputs) < 4 {
        return 0, 0, nil, nil, false, nil
    }

    selector := hex.EncodeToString(inputs[:4])
    switch selector {
    case "18160ddd":
        // 函数: totalSupply()
        // 预估Gas消耗: 105
        // 栈操作: [PUSH1(0x00) SLOAD PUSH1(0x00) MSTORE PUSH1(0x20) PUSH1(0x00) RETURN]
        // 内存操作: [MSTORE]
        // 存储操作: [SLOAD]
        return 48, 105, nil, nil, true, nil
    case "70a08231":
        // 函数: balanceOf(address)
        // 预估Gas消耗: 108
        return 80, 108, nil, nil, true, nil
    // ... 更多函数
    default:
        return 0, 0, nil, nil, false, nil
    }
}
```

## 支持的操作码

### 基础操作
- `ADD`, `SUB`, `MUL`, `DIV`, `MOD`
- `LT`, `GT`, `EQ`, `ISZERO`
- `AND`, `OR`, `XOR`, `NOT`

### 栈操作
- `PUSH0` - `PUSH32`
- `POP`
- `DUP1` - `DUP16`
- `SWAP1` - `SWAP16`

### 内存操作
- `MLOAD`, `MSTORE`, `MSTORE8`
- `CALLDATALOAD`, `CALLDATASIZE`, `CALLDATACOPY`

### 存储操作
- `SLOAD`, `SSTORE`

### 跳转操作
- `JUMP`, `JUMPI`, `JUMPDEST`

### 控制流
- `STOP`, `RETURN`, `REVERT`

## 测试

运行测试：

```bash
cd bsc/core/vm
go test -v -run TestShortcutGenerator
```

运行性能测试：

```bash
go test -bench=BenchmarkShortcutGenerator
```

## 示例

查看 `shortcut_generator_example.go` 文件中的完整示例：

- ERC20代币合约示例
- NFT合约示例
- 文件生成示例

## 注意事项

1. **opcode序列格式**：输入的opcode序列必须是有效的EVM字节码
2. **函数选择器模式**：目前支持标准的函数选择器分发模式
3. **Gas计算**：gas消耗是预估值，可能与实际执行有差异
4. **错误处理**：生成器会处理无效的opcode序列并返回错误

## 扩展

可以通过以下方式扩展功能：

1. **支持更多函数选择器模式**：修改 `isFunctionSelectorPattern` 方法
2. **优化Gas计算**：更新 `calculateGasCost` 方法
3. **添加更多操作分析**：扩展 `analyzeFunctionBody` 方法
4. **支持合约验证**：添加合约字节码验证功能 