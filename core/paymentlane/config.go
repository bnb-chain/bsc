package paymentlane

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// ContractAddress is the PaymentLane system contract, installed by the Gauss fork. Spelled out
// rather than taken from systemcontracts, which would stop this package being a leaf.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

// Params is the eight governable values of BEP-703 section 3.6 as one decoded tuple.
// The first six are parts per RatioDenom, MinGas and MaxGas absolute gas.
type Params struct {
	MinRatio      uint64
	MaxRatio      uint64
	ExpandTrigger uint64
	ShrinkTrigger uint64
	ExpandStep    uint64
	ShrinkStep    uint64
	MinGas        uint64
	MaxGas        uint64
}

func (p Params) String() string {
	return fmt.Sprintf("minRatio %d maxRatio %d expandTrigger %d shrinkTrigger %d expandStep %d shrinkStep %d minGas %d maxGas %d",
		p.MinRatio, p.MaxRatio, p.ExpandTrigger, p.ShrinkTrigger, p.ExpandStep, p.ShrinkStep, p.MinGas, p.MaxGas)
}
