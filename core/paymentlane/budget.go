package paymentlane

import "fmt"

// Budget tracks scalar payment gas before Amsterdam and per-resource gas afterwards.
type Budget struct {
	PaymentLaneQuota uint64
	PaymentLaneUsed  uint64 // pre-Amsterdam

	PaymentExecutionUsed uint64 // Amsterdam, before refunds
	PaymentStateUsed     uint64 // Amsterdam, before refunds
}

// IdleLane returns the unused payment quota before Amsterdam.
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

// Verify checks a finished block before Amsterdam.
func (b Budget) Verify(gasLimit, gasUsed uint64) error {
	return CheckInequality(gasLimit, gasUsed, b.PaymentLaneUsed, b.PaymentLaneQuota)
}

// RecordUsedAmsterdam books payment gas separately in each resource.
func (b *Budget) RecordUsedAmsterdam(laneType LaneType, execution, state uint64) {
	if laneType == PaymentLane {
		b.PaymentExecutionUsed += execution
		b.PaymentStateUsed += state
	}
}

// IdleLaneAmsterdam returns the unused payment quota in each resource.
func (b Budget) IdleLaneAmsterdam() (execution, state uint64) {
	return satSub(b.PaymentLaneQuota, b.PaymentExecutionUsed), satSub(b.PaymentLaneQuota, b.PaymentStateUsed)
}

// VerifyAmsterdam checks both resources against the limit after system gas.
func (b Budget) VerifyAmsterdam(gasLimit, execution, state uint64) error {
	if err := CheckInequality(gasLimit, execution, b.PaymentExecutionUsed, b.PaymentLaneQuota); err != nil {
		return fmt.Errorf("execution: %w", err)
	}
	if err := CheckInequality(gasLimit, state, b.PaymentStateUsed, b.PaymentLaneQuota); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}
