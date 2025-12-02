package pool

import (
	"github.com/ethereum/go-ethereum/pool/processor"
)

func init() {
	processor.RegisterHandler(string(LPTypePancakeV2), processor.NewPancakeSwapV2Handler)
	processor.RegisterHandler(string(LPTypePancakeV3), processor.NewPancakeSwapV3Handler)
}
