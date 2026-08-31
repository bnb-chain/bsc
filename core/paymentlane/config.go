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

// BEP-703 3.6.2's protocol constants, mirroring PaymentLane.sol. Drift here rejects blocks peers accept.
const (
	TriggerGapMin         = 1_000
	RatioGapMin           = 500
	MaxLaneRatio          = 2_000
	MinExpandTriggerRatio = 5_000
	MinShrinkTriggerRatio = 2_000
	MaxStepRatio          = 1_000
	MinLaneGas            = 21_000
	MaxLaneGas            = 1_000_000_000
)

// Validate mirrors PaymentLane.sol's _isValid: the eight range guards, then BEP-703 3.6.2's six
// invariants. The guards must stay first - they are what bounds the invariants' sums.
func (p GovernanceParams) Validate() error {
	fail := func(guard string) error {
		return fmt.Errorf("%w: 3.6.2 %s: %s", ErrCorruptConfig, guard, p)
	}
	switch {
	case p.MinRatio == 0 || p.MinRatio > MaxLaneRatio:
		return fail("minRatio")
	case p.MaxRatio > MaxLaneRatio:
		return fail("maxRatio")
	case p.ExpandTrigger < MinExpandTriggerRatio || p.ExpandTrigger > RatioDenom:
		return fail("expandTrigger")
	case p.ShrinkTrigger < MinShrinkTriggerRatio || p.ShrinkTrigger > RatioDenom:
		return fail("shrinkTrigger")
	case p.ExpandStep == 0 || p.ExpandStep > MaxStepRatio:
		return fail("expandStep")
	case p.ShrinkStep == 0 || p.ShrinkStep > MaxStepRatio:
		return fail("shrinkStep")
	case p.MinGas < MinLaneGas || p.MinGas > MaxLaneGas:
		return fail("minGas")
	case p.MaxGas < MinLaneGas || p.MaxGas > MaxLaneGas:
		return fail("maxGas")
	case p.ExpandTrigger < p.ShrinkTrigger+TriggerGapMin:
		return fail("invariant (1)")
	case p.ExpandStep <= p.ShrinkStep:
		return fail("invariant (2)")
	case p.MaxRatio < p.MinRatio+RatioGapMin:
		return fail("invariant (3)")
	case p.MaxGas <= p.MinGas:
		return fail("invariant (4)")
	case p.MaxRatio+p.ExpandTrigger > RatioDenom:
		return fail("invariant (5)")
	case p.ExpandStep+p.ShrinkTrigger > p.ExpandTrigger:
		return fail("invariant (6)")
	}
	return nil
}

func (p GovernanceParams) String() string {
	return fmt.Sprintf("minRatio %d maxRatio %d expandTrigger %d shrinkTrigger %d expandStep %d shrinkStep %d minGas %d maxGas %d",
		p.MinRatio, p.MaxRatio, p.ExpandTrigger, p.ShrinkTrigger, p.ExpandStep, p.ShrinkStep, p.MinGas, p.MaxGas)
}
