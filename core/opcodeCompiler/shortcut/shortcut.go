package shortcut

import (
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
)

var (
	shortcutPcRegisters map[common.Address]Shortcut
)

func RegisterShortcut(addr common.Address, s Shortcut) {
	if shortcutPcRegisters == nil {
		shortcutPcRegisters = make(map[common.Address]Shortcut)
	}
	shortcutPcRegisters[s.Contract()] = s
}

type Shortcut interface {
	Contract() common.Address
	Shortcut(pc uint64, inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, expected bool, err error)
}
