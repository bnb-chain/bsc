// Package paymentlane implements the BEP-703 payment lane rules. One inequality per block:
//
//	header.GasUsed + max(0, paymentLaneQuota - paymentGasUsed) <= GasLimit
//
// Section 3.3's rule with generalGasUsed as the header residual, so Parlia's system transactions
// count as general gas. Nothing is committed to the block: the quota is a pure function of the
// parent post-state's ratio and this block's gas limit, so every node derives it independently.

package paymentlane

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// ContractAddress is the PaymentLane system contract, installed by the Jenner fork.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

const (
	RatioDenom                = 10_000
	MaxLaneRatio              = 1_000   // the lane may never exceed 10% of the gas limit
	MaxListedContracts uint64 = 100_000 // the contract enforces it on governance writes
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
	ErrViolated         = errors.New("payment lane inequality violated")
	ErrCorruptConfig    = errors.New("payment lane config mismatch")
	ErrStateUnavailable = errors.New("payment lane state unavailable")
)

// CheckRatio checks the ratio is legal, at the getter's full uint256 width: a value narrowed to
// 64 bits first can land inside the guard when the value returned did not.
func CheckRatio(ratio *big.Int) (uint64, error) {
	if ratio == nil || !ratio.IsUint64() || ratio.Uint64() == 0 || ratio.Uint64() > MaxLaneRatio {
		return 0, fmt.Errorf("%w: ratio 0 < %v <= %d", ErrCorruptConfig, ratio, MaxLaneRatio)
	}
	return ratio.Uint64(), nil
}

// Quota is paymentLaneQuota(h) = PAYMENT_LANE_RATIO(h−1) × GasLimit(h) / RATIO_DENOM.
func Quota(ratio, gasLimit uint64) uint64 {
	hi, lo := bits.Mul64(ratio, gasLimit)
	if hi >= RatioDenom {
		// Unreachable: hi <= ratio-1 for any gasLimit, and CheckRatio caps ratio well below
		// RatioDenom. Getting here means the ratio never passed that guard.
		log.Error("Payment lane quota overflowed", "ratio", ratio, "max", MaxLaneRatio, "gasLimit", gasLimit)
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, RatioDenom)
	return q
}

// CheckInequality is the block validity rule.
func CheckInequality(gasLimit, gasUsed, paymentGasUsed, paymentLaneQuota uint64) error {
	sum, carry := bits.Add64(gasUsed, satSub(paymentLaneQuota, paymentGasUsed), 0)
	if carry != 0 || sum > gasLimit {
		return fmt.Errorf("%w: gas used %d payment %d quota %d limit %d",
			ErrViolated, gasUsed, paymentGasUsed, paymentLaneQuota, gasLimit)
	}
	return nil
}

func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}
