package shortcut

import (
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
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
	log.Info("Getting shortcut", "addr", addr.String())
	return shortcutPcRegisters[addr]
}

type Shortcut interface {
	Contract() common.Address
	Shortcut(inputs []byte, origin, caller common.Address, value *uint256.Int) (shortcutPc uint64, gasUsed uint64, stack []uint256.Int, mem []byte, expected bool, err error)
}
