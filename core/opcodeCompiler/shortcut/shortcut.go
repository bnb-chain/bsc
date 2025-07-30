package shortcut

import (
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/shortcut/impl"
	"github.com/ethereum/go-ethereum/log"
)

var (
	shortcutPcRegisters map[common.Address]Shortcut
)

func RegisterShortcut(addr common.Address, s Shortcut) {
	if shortcutPcRegisters == nil {
		shortcutPcRegisters = make(map[common.Address]Shortcut)
	}
	log.Info("Registering shortcut", "addr", addr.String())
	shortcutPcRegisters[addr] = s
}

func GetShortcut(addr common.Address) Shortcut {
	//log.Info("Getting shortcut", "addr", addr.String())
	return shortcutPcRegisters[addr]
}

func GetShortcutV2(addr common.Address) Shortcut {
	switch string(addr.Bytes()) {
	case string([]byte{0x55, 0xD3, 0x98, 0x32, 0x6F, 0x99, 0x05, 0x9F, 0xF7, 0x75, 0x48, 0x52, 0x46, 0x99, 0x90, 0x27, 0xB3, 0x19, 0x79, 0x55}):
		return &impl.ShortcutImpl55D398326F99059FF775485246999027B3197955{}
	default:
		return nil
	}
}

type Shortcut interface {
	Contract() common.Address
	Shortcut(inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, lastGasCost uint64, expected bool, err error)
	ShortcutV2(
		inputs []byte, origin, caller common.Address, value *uint256.Int,
		shortcutPc *uint64, gasUsed *uint64, stack *[]uint256.Int, mem *[]byte, lastGasCost *uint64,
	) (expected bool, err error)
}
