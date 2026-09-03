package paymentlane

import (
	"fmt"
)

// Budget states the block validity rule as an admission predicate. Payment is the only tracked
// figure, general gas being the header residual. Capacity is passed in per call rather than
// stored, so no stale capacity can be held.
type Budget struct {
	PaymentLaneQuota uint64 // this block's quota, straight from NextLaneQuota
	PaymentLaneUsed  uint64
}

// IdleLane is the part of the quota that payment traffic has not consumed yet.
func (b Budget) IdleLane() uint64 { return satSub(b.PaymentLaneQuota, b.PaymentLaneUsed) }

// MaxAvailableGas is the largest gas limit a SINGLE transaction of this lane type may
// declare, given shared as the shared remainder (the producer passes gasPool.Gas()):
// payment may take all of it, general must leave the idle quota untouched.
func (b Budget) MaxAvailableGas(shared uint64, laneType LaneType) uint64 {
	if laneType == PaymentLane {
		return shared
	}
	return satSub(shared, b.IdleLane())
}

// Admits reports whether this transaction may be included.
func (b Budget) Admits(shared uint64, laneType LaneType, txGasLimit uint64) bool {
	return txGasLimit <= b.MaxAvailableGas(shared, laneType)
}

// RecordUsed adds payment gas to PaymentLaneUsed.
func (b *Budget) RecordUsed(laneType LaneType, delta uint64) {
	if laneType == PaymentLane {
		b.PaymentLaneUsed += delta
	}
}

// Verify checks a finished block: poolUsed <= gasUsed, PaymentLaneUsed <= poolUsed, then the rule
// inequality.
func (b Budget) Verify(gasLimit, gasUsed, poolUsed uint64) error {
	// Unreachable with the arguments in order; it catches them swapped.
	if poolUsed > gasUsed {
		return fmt.Errorf("payment lane pool used %d exceeds block total %d", poolUsed, gasUsed)
	}
	if b.PaymentLaneUsed > poolUsed {
		return fmt.Errorf("%w: payment %d pool %d", ErrPaymentExceedsPool, b.PaymentLaneUsed, poolUsed)
	}
	return CheckInequality(gasLimit, gasUsed, b.PaymentLaneUsed, b.PaymentLaneQuota)
}

// VerifyCommitment is the only authoritative check on the committed payment figure: it compares
// it against local replay, which no self-check can do. poolUsed is gp.Used() over user
// transactions. PaymentLaneQuota is CheckNextLaneQuota's job, not this one.
func (b Budget) VerifyCommitment(gasLimit, gasUsed, poolUsed uint64, c Commitment) error {
	if b.PaymentLaneUsed != c.PaymentGasUsed {
		return fmt.Errorf("%w: committed payment %d, replayed %d",
			ErrUntruthy, c.PaymentGasUsed, b.PaymentLaneUsed)
	}
	return b.Verify(gasLimit, gasUsed, poolUsed)
}
