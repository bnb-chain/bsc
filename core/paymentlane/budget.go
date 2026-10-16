package paymentlane

// Budget states the block validity rule as an admission predicate.
type Budget struct {
	PaymentLaneQuota uint64
	PaymentLaneUsed  uint64
}

func (b Budget) IdleLane() uint64 { return satSub(b.PaymentLaneQuota, b.PaymentLaneUsed) }

// MaxAvailableGas is the largest gas limit a SINGLE transaction of this lane type may declare.
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

// Verify checks a finished block.
func (b Budget) Verify(gasLimit, gasUsed uint64) error {
	return CheckInequality(gasLimit, gasUsed, b.PaymentLaneUsed, b.PaymentLaneQuota)
}
