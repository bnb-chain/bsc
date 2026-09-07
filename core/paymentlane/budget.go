package paymentlane

// Budget states the block validity rule as an admission predicate. Payment is the only tracked
// figure, general gas being the header residual. Capacity is passed in per call rather than
// stored, so no stale capacity can be held.
type Budget struct {
	PaymentLaneQuota uint64
	PaymentLaneUsed  uint64
}

func (b Budget) IdleLane() uint64 { return satSub(b.PaymentLaneQuota, b.PaymentLaneUsed) }

// MaxAvailableGas is the largest gas limit a SINGLE transaction of this lane type may declare,
// given shared as the shared remainder (the producer passes gasPool.Gas()): payment may take all
// of it, general must leave the idle quota untouched.
func (b Budget) MaxAvailableGas(shared uint64, laneType LaneType) uint64 {
	if laneType == PaymentLane {
		return shared
	}
	return satSub(shared, b.IdleLane())
}

func (b Budget) Admits(shared uint64, laneType LaneType, txGasLimit uint64) bool {
	return txGasLimit <= b.MaxAvailableGas(shared, laneType)
}

func (b *Budget) RecordUsed(laneType LaneType, delta uint64) {
	if laneType == PaymentLane {
		b.PaymentLaneUsed += delta
	}
}

// Verify checks a finished block. PaymentLaneUsed is always this node's own replay - no header
// field carries the producer's word for it - so the rule inequality is the whole check.
func (b Budget) Verify(gasLimit, gasUsed uint64) error {
	return CheckInequality(gasLimit, gasUsed, b.PaymentLaneUsed, b.PaymentLaneQuota)
}
