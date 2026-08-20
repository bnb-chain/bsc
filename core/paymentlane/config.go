package paymentlane

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// ContractAddress is the PaymentLane system contract, installed by the Jenner fork.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

// GovernanceParams bundles the eight governable values from BEP-703 section 3.6.1.
// The first six are ratios in parts per RatioDenom; MinGas and MaxGas are absolute gas amounts.
type GovernanceParams struct {
	MinRatio      uint64
	MaxRatio      uint64
	ExpandTrigger uint64
	ShrinkTrigger uint64
	ExpandStep    uint64
	ShrinkStep    uint64
	MinGas        uint64
	MaxGas        uint64
}

func (p GovernanceParams) String() string {
	return fmt.Sprintf("minRatio %d maxRatio %d expandTrigger %d shrinkTrigger %d expandStep %d shrinkStep %d minGas %d maxGas %d",
		p.MinRatio, p.MaxRatio, p.ExpandTrigger, p.ShrinkTrigger, p.ExpandStep, p.ShrinkStep, p.MinGas, p.MaxGas)
}
