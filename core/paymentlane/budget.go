package paymentlane

import (
	"fmt"
)

// Budget states the block validity rule as an admission predicate. Payment is the only tracked
// figure, general gas being the header residual. Capacity is passed in per call rather than
// stored, so no stale capacity can be held.
type Budget struct {
	LaneSize    uint64 // this block's quota, straight from NextLaneSize
	PaymentUsed uint64
}

// IdleLane is the part of the quota that payment traffic has not consumed yet.
func (b Budget) IdleLane() uint64 { return satSub(b.LaneSize, b.PaymentUsed) }

// MaxAvailableGas is the largest gas limit a SINGLE transaction of this class may
// declare, given shared as the shared remainder (the producer passes gasPool.Gas()):
// payment may take all of it, general must leave the idle quota untouched.
func (b Budget) MaxAvailableGas(shared uint64, class Class) uint64 {
	if class == ClassPayment {
		return shared
	}
	return satSub(shared, b.IdleLane())
}

// Admits reports whether this transaction may be included.
func (b Budget) Admits(shared uint64, class Class, txGasLimit uint64) bool {
	return txGasLimit <= b.MaxAvailableGas(shared, class)
}

// RecordUsed adds payment gas to PaymentUsed.
func (b *Budget) RecordUsed(class Class, delta uint64) {
	if class == ClassPayment {
		b.PaymentUsed += delta
	}
}

// Verify checks a finished block: poolUsed <= gasUsed, PaymentUsed <= poolUsed, then the rule
// inequality.
func (b Budget) Verify(gasLimit, gasUsed, poolUsed uint64) error {
	// Unreachable with the arguments in order; it catches them swapped.
	if poolUsed > gasUsed {
		return fmt.Errorf("payment lane pool used %d exceeds block total %d", poolUsed, gasUsed)
	}
	if b.PaymentUsed > poolUsed {
		return fmt.Errorf("%w: payment %d pool %d", ErrPaymentExceedsPool, b.PaymentUsed, poolUsed)
	}
	return CheckInequality(gasLimit, gasUsed, b.PaymentUsed, b.LaneSize)
}

// VerifyCommitment is the only authoritative check on the committed payment figure: it compares
// it against local replay, which no self-check can do. poolUsed is gp.Used() over user
// transactions. LaneSize is CheckNextLaneSize's job, not this one.
func (b Budget) VerifyCommitment(gasLimit, gasUsed, poolUsed uint64, c Commitment) error {
	if b.PaymentUsed != c.PaymentGasUsed {
		return fmt.Errorf("%w: committed payment %d, replayed %d",
			ErrUntruthy, c.PaymentGasUsed, b.PaymentUsed)
	}
	return b.Verify(gasLimit, gasUsed, poolUsed)
}
