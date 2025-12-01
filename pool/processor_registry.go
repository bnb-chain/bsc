package pool

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/pool/processor"
)

func init() {
	v2 := processor.NewPancakeSwapV2Processor()
	v3 := processor.NewPancakeSwapV3Processor()

	processor.RegisterProcessor(string(LPTypePancakeV2), processor.Func(func(receipt *types.Receipt) (*processor.State, bool, error) {
		state, ok, err := v2.Process(receipt)
		if err != nil || !ok {
			return nil, ok, err
		}
		return &processor.State{V2: state}, true, nil
	}))

	processor.RegisterProcessor(string(LPTypePancakeV3), processor.Func(func(receipt *types.Receipt) (*processor.State, bool, error) {
		state, ok, err := v3.Process(receipt)
		if err != nil || !ok {
			return nil, ok, err
		}
		return &processor.State{V3: state}, true, nil
	}))
}
