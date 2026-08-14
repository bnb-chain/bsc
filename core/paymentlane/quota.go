package paymentlane

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// RatioDenom mirrors PaymentLane.RATIO_DENOM: Params' first six fields are parts per this.
const RatioDenom = 10_000

// Signal records how congested the parent was, which is all NextLaneSize needs. Build it with
// NewSignalFromParent; the zero value means bootstrap.
type Signal struct {
	parentLaneSize      uint64
	parentSignalGasUsed uint64
	parentGasLimit      uint64
}

// NewSignalFromParent reads the parent's congestion off its own header.
//
// Whether the parent carries a commitment must NOT be inferred from whether decoding failed:
// reading a failure as bootstrap would silently reset the quota to the floor (BEP 3.5.5). The
// test is positive instead - a block outside the mechanism carries EmptyUncleHash, which no
// commitment can equal, its reserved bytes being non-zero. Genesis is covered by the same test.
func NewSignalFromParent(parent *types.Header) (Signal, error) {
	if parent == nil {
		return Signal{}, fmt.Errorf("%w: nil parent header", ErrBadCommitment)
	}
	if parent.UncleHash == types.EmptyUncleHash {
		return Signal{}, nil
	}
	c, err := Decode(parent.UncleHash)
	if err != nil {
		return Signal{}, err
	}
	return newSignal(c, parent.GasUsed, parent.GasLimit), nil
}

// newSignal is unexported so nothing outside can pair a commitment with the wrong gas limit.
func newSignal(parentCommitment Commitment, parentGasUsed, parentGasLimit uint64) Signal {
	// Cannot carry: the terms sum to at most max(parentGasUsed, PaymentGasUsed).
	return Signal{
		parentLaneSize: parentCommitment.LaneSize,
		parentSignalGasUsed: satSub(parentGasUsed, parentCommitment.PaymentGasUsed) + // all general gas
			satSub(parentCommitment.PaymentGasUsed, parentCommitment.LaneSize), // payment beyond the quota
		parentGasLimit: parentGasLimit,
	}
}

// NextLaneSize steps the accumulator: the quota for the block after the one this Signal
// describes, which is both the producer's packing budget and the value it commits. gasLimit is
// that block's.
func (s Signal) NextLaneSize(p Params, gasLimit uint64) uint64 {
	next := s.parentLaneSize

	// A zero parentGasLimit is the bootstrap seed, not a quiet parent: no signal, so no step.
	if s.parentGasLimit != 0 {
		switch {
		case s.congestionAtLeast(p.ExpandTrigger):
			next = satAdd(next, mulDivFloor(p.ExpandStep, gasLimit, RatioDenom))
		case !s.congestionAtLeast(p.ShrinkTrigger):
			next = satSub(next, mulDivFloor(p.ShrinkStep, gasLimit, RatioDenom))
		}
		// else: hysteresis band, the quota holds.
	}

	// Clamp every block, not only when a step fires (BEP 3.4.4).
	ceiling := laneCeiling(p, gasLimit)
	size := min(max(next, laneFloor(p, gasLimit)), ceiling)

	return min(size, reserveCap(gasLimit))
}

// reserveCap is BEP 3.4.4's laneCap, applied LAST and deliberately able to push the quota below
// its own floor: at a low enough GasLimit the quota would exceed what a breathe block holds, and
// that halt does not clear on its own. Uses the protocol constant, never the miner-local
// gasReserved, so both sides agree.
func reserveCap(gasLimit uint64) uint64 {
	return satSub(gasLimit, params.SystemTxsGasHardLimit)
}

func (s Signal) congestionAtLeast(triggerRatio uint64) bool {
	return gte128(s.parentSignalGasUsed, RatioDenom, triggerRatio, s.parentGasLimit)
}

// CheckNextLaneSize adjudicates a committed quota exactly: it is a pure function of the headers
// and the parent post-state, so it settles before any transaction executes - on import and on
// the MEV pre-seal path alike. The payment total is the half needing replay; that is
// Budget.VerifyCommitment's job.
func (s Signal) CheckNextLaneSize(committed uint64, p Params, gasLimit uint64) error {
	if want := s.NextLaneSize(p, gasLimit); committed != want {
		return fmt.Errorf("%w: committed %d, derived %d", ErrQuotaMismatch, committed, want)
	}
	return nil
}

func laneCeiling(p Params, gasLimit uint64) uint64 {
	return min(mulDivFloor(p.MaxRatio, gasLimit, RatioDenom), p.MaxGas)
}

// laneFloor is NOT a lower bound on NextLaneSize's result - reserveCap may push below it.
func laneFloor(p Params, gasLimit uint64) uint64 {
	return min(max(mulDivFloor(p.MinRatio, gasLimit, RatioDenom), p.MinGas), laneCeiling(p, gasLimit))
}

// Bounds reports the three clamps NextLaneSize applies, for metrics rather than consensus.
func Bounds(p Params, gasLimit uint64) (floor, ceiling, safetyCap uint64) {
	return laneFloor(p, gasLimit), laneCeiling(p, gasLimit), reserveCap(gasLimit)
}

// mulDivFloor returns floor(a*b/d) over 128 bits. The guard saturates exactly where bits.Div64
// would panic, at hi >= d, so the caller needs no precondition.
func mulDivFloor(a, b, d uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi >= d {
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, d)
	return q
}

// gte128 reports a*b >= c*d exactly, without division and without floats. 128-bit because both
// products can wrap at a consensus-legal gas limit: 8000 * 2^62 is zero in 64 bits, which turns
// every shrink into an expansion.
func gte128(a, b, c, d uint64) bool {
	ah, al := bits.Mul64(a, b)
	ch, cl := bits.Mul64(c, d)
	if ah != ch {
		return ah > ch
	}
	return al >= cl
}
