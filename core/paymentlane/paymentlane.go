// Package paymentlane implements the BEP-703 payment lane rules. One inequality per block:
//
//	header.GasUsed + max(0, paymentLaneQuota - paymentGasUsed) <= GasLimit
//
// Section 3.3's rule with generalGasUsed as the header residual, so Parlia's system
// transactions count as general gas. The subtrahend is the idle quota (Budget.IdleLane).
//
// Nothing here is committed to the block: the quota is a pure function of the parent post-state's
// ratio and this block's gas limit, so every node derives the same value independently.

package paymentlane

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
)

// ContractAddress is the PaymentLane system contract, installed by the Jenner fork.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

// BEP-703 section 3.6.1's constants, mirroring PaymentLane.sol. Drift here rejects blocks peers
// accept. The default ratio is deliberately NOT mirrored: getPaymentLaneRatio() applies it, which
// is why section 3.6.4 forbids reading the storage slot instead.
const (
	RatioDenom   = 10_000
	MaxLaneRatio = 1_000 // the lane may never exceed 10% of the gas limit
)

type LaneType uint8

const (
	GeneralLane LaneType = iota
	PaymentLane
)

func (c LaneType) String() string {
	if c == PaymentLane {
		return "paymentLane"
	}
	return "generalLane"
}

var (
	ErrViolated      = errors.New("payment lane inequality violated")
	ErrCorruptConfig = errors.New("payment lane config mismatch")
	// Local fault, not the peer's: this node could not read the required lane state.
	ErrStateUnavailable = errors.New("payment lane state unavailable")
)

// CheckRatio is BEP-703 section 3.6.1's guard, evaluated at the getter's full uint256 width: a
// value narrowed to 64 bits first can land inside the guard when the value returned did not.
func CheckRatio(ratio *big.Int) (uint64, error) {
	if ratio == nil || !ratio.IsUint64() || ratio.Uint64() == 0 || ratio.Uint64() > MaxLaneRatio {
		return 0, fmt.Errorf("%w: 3.6.1 ratio guard 0 < %v <= %d", ErrCorruptConfig, ratio, MaxLaneRatio)
	}
	return ratio.Uint64(), nil
}

// Quota is BEP-703 section 3.4.1: ratio(h-1) * GasLimit(h) / RatioDenom, truncated toward zero.
// The product is taken over 128 bits - it needs up to 73 at a consensus-legal gas limit - and the
// guard saturates exactly where bits.Div64 would panic, so no precondition is left to the caller.
func Quota(ratio, gasLimit uint64) uint64 {
	hi, lo := bits.Mul64(ratio, gasLimit)
	if hi >= RatioDenom {
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, RatioDenom)
	return q
}

// CheckInequality is the block validity rule. gasUsed must be the block's real total, system
// gas included.
func CheckInequality(gasLimit, gasUsed, paymentGasUsed, paymentLaneQuota uint64) error {
	sum, carry := bits.Add64(gasUsed, satSub(paymentLaneQuota, paymentGasUsed), 0)
	if carry != 0 || sum > gasLimit {
		return fmt.Errorf("%w: gas used %d payment %d quota %d limit %d",
			ErrViolated, gasUsed, paymentGasUsed, paymentLaneQuota, gasLimit)
	}
	return nil
}

// satSub is saturating subtraction.
func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}
