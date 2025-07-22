package impl

import (
	"encoding/hex"
	"github.com/holiman/uint256"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/shortcut"
)

func init() {
	impl := new(ShortcutImpl55D398326F99059FF775485246999027B3197955)
	shortcut.RegisterShortcut(impl.Contract(), impl)
}

type ShortcutImpl55D398326F99059FF775485246999027B3197955 struct {
}

func (s *ShortcutImpl55D398326F99059FF775485246999027B3197955) Contract() common.Address {
	return common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
}

func (s *ShortcutImpl55D398326F99059FF775485246999027B3197955) Shortcut(pc uint64, inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, expected bool, err error) {
	// 函数选择器分析
	if len(inputs) < 4 {
		return 0, 0, nil, nil, false, nil
	}

	selector := hex.EncodeToString(inputs[:4])
	switch selector {
	case "0x23b872dd":
		// 函数: 0x23b872dd
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 520, 0, nil, nil, true, nil
	case "0xa9059cbb":
		// 函数: 0xa9059cbb
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 858, 0, nil, nil, true, nil
	case "0xb09f1266":
		// 函数: 0xb09f1266
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 902, 0, nil, nil, true, nil
	case "0xd28d8852":
		// 函数: 0xd28d8852
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 910, 0, nil, nil, true, nil
	case "0xa0712d68":
		// 函数: 0xa0712d68
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 785, 0, nil, nil, true, nil
	case "0x095ea7b3":
		// 函数: 0x095ea7b3
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 430, 0, nil, nil, true, nil
	case "0x313ce567":
		// 函数: 0x313ce567
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 574, 0, nil, nil, true, nil
	case "0xf2fde38b":
		// 函数: 0xf2fde38b
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 964, 0, nil, nil, true, nil
	case "0x893d20e8":
		// 函数: 0x893d20e8
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 733, 0, nil, nil, true, nil
	case "0x39509351":
		// 函数: 0x39509351
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 612, 0, nil, nil, true, nil
	case "0x18160ddd":
		// 函数: 0x18160ddd
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 494, 0, nil, nil, true, nil
	case "0xdd62ed3e":
		// 函数: 0xdd62ed3e
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 918, 0, nil, nil, true, nil
	case "0x8da5cb5b":
		// 函数: 0x8da5cb5b
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 769, 0, nil, nil, true, nil
	case "0xa457c2d7":
		// 函数: 0xa457c2d7
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 814, 0, nil, nil, true, nil
	case "0x70a08231":
		// 函数: 0x70a08231
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 685, 0, nil, nil, true, nil
	case "0x06fdde03":
		// 函数: 0x06fdde03
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 305, 0, nil, nil, true, nil
	case "0x95d89b41":
		// 函数: 0x95d89b41
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 777, 0, nil, nil, true, nil
	case "0x32424aa3":
		// 函数: 0x32424aa3
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 604, 0, nil, nil, true, nil
	case "0x42966c68":
		// 函数: 0x42966c68
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 656, 0, nil, nil, true, nil
	case "0x715018a6":
		// 函数: 0x715018a6
		// 预估Gas消耗: 0
		// 栈操作: []
		// 内存操作: []
		// 存储操作: []
		return 723, 0, nil, nil, true, nil
	default:
		return 0, 0, nil, nil, false, nil
	}
}
