// Package paymentlane implements the BEP-703 payment lane rules. One inequality per block:
//
//	header.GasUsed + max(0, laneSize - paymentGasUsed) <= GasLimit
//
// That is section 3.3's rule with generalGasUsed replaced by header.GasUsed less
// paymentGasUsed, so Parlia's system transactions count as general gas. The subtrahend is the
// idle quota (Budget.IdleLane), which is never reclaimed for general traffic - reclaiming it
// would make excluding payment transactions free for the producer.

package paymentlane

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
)

type Class uint8

const (
	ClassGeneral Class = iota
	ClassPayment
)

func (c Class) String() string {
	if c == ClassPayment {
		return "payment"
	}
	return "general"
}

var (
	ErrViolated           = errors.New("payment lane inequality violated")
	ErrPaymentExceedsPool = errors.New("payment lane payment gas exceeds the block's user-transaction gas")
	ErrBadCommitment      = errors.New("payment lane commitment is malformed")
	ErrUntruthy           = errors.New("payment lane commitment is not truthful")
	ErrCorruptConfig      = errors.New("payment lane config mismatch")
	ErrQuotaMismatch      = errors.New("payment lane quota does not match the parent derivation")
	// Local fault, not the peer's: this node could not read the required lane state.
	ErrStateUnavailable = errors.New("payment lane state unavailable")
)

// Commitment is the two values BEP-703 section 3.5.2 commits. Do not add generalGasUsed: it is
// header.GasUsed less PaymentGasUsed, which consensus already checks. LaneSize cannot be
// derived from headers at all - rebuilding it needs each parent's CLASSIFIED payment total.
type Commitment struct {
	LaneSize       uint64
	PaymentGasUsed uint64
}

// Encode packs a Commitment into 32 bytes:
//
//	[0:8]   laneSize        uint64 big-endian
//	[8:16]  paymentGasUsed  uint64 big-endian
//	[16:32] reserved, always zero
func Encode(c Commitment) common.Hash {
	var h common.Hash
	binary.BigEndian.PutUint64(h[0:8], c.LaneSize)
	binary.BigEndian.PutUint64(h[8:16], c.PaymentGasUsed)
	return h
}

// Decode is the inverse of Encode.
func Decode(h common.Hash) (Commitment, error) {
	for _, b := range h[16:] {
		if b != 0 {
			return Commitment{}, fmt.Errorf("%w: reserved bytes set", ErrBadCommitment)
		}
	}
	return Commitment{
		LaneSize:       binary.BigEndian.Uint64(h[0:8]),
		PaymentGasUsed: binary.BigEndian.Uint64(h[8:16]),
	}, nil
}

// CheckHeaderBounds refuses a forged header from its fields alone, before the body is executed.
func (c Commitment) CheckHeaderBounds(gasUsed, gasLimit uint64) error {
	if c.PaymentGasUsed > gasUsed {
		return fmt.Errorf("%w: committed payment %d exceeds header gas used %d",
			ErrUntruthy, c.PaymentGasUsed, gasUsed)
	}
	if c.LaneSize > gasLimit {
		return fmt.Errorf("%w: committed lane size %d exceeds header gas limit %d",
			ErrViolated, c.LaneSize, gasLimit)
	}
	return CheckInequality(gasLimit, gasUsed, c.PaymentGasUsed, c.LaneSize)
}

// CheckInequality is the block validity rule, called by the producer, header verification and
// the importer alike. gasUsed must be the block's REAL total, system gas included.
func CheckInequality(gasLimit, gasUsed, paymentGasUsed, laneSize uint64) error {
	sum, carry := bits.Add64(gasUsed, satSub(laneSize, paymentGasUsed), 0)
	if carry != 0 || sum > gasLimit {
		return fmt.Errorf("%w: gas used %d payment %d lane %d limit %d",
			ErrViolated, gasUsed, paymentGasUsed, laneSize, gasLimit)
	}
	return nil
}

// satSub is saturating subtraction; all lane arithmetic is unsigned.
func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// satAdd is saturating addition, capped at MaxUint64.
func satAdd(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return sum
}
