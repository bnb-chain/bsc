package paymentlane

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
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

// NewSignalFromParent reads the parent's congestion off the chain, as the Signal NextLaneSize
// turns into the next block's quota: the quota and payment gas the parent committed, its gas
// used, and its own gas limit as the denominator.
//
// Whether the parent carries a commitment at all is decided by the GRANDparent's activation,
// never by whether decoding succeeded. A nil grandparent is legal only for a genesis parent -
// anywhere else it is an unresolved header, and treating it as bootstrap would silently reset the
// quota to the floor.
func NewSignalFromParent(
	config *params.ChainConfig,
	grandparent, parent *types.Header,
	parentCommitment common.Hash,
) (Signal, error) {
	if parent == nil {
		return Signal{}, fmt.Errorf("%w: nil parent header", ErrBadCommitment)
	}
	if grandparent == nil && parent.Number.Sign() != 0 {
		return Signal{}, fmt.Errorf("%w: nil grandparent for parent %d", ErrBadCommitment, parent.Number)
	}
	if grandparent == nil || !config.IsGauss(grandparent.Number, grandparent.Time) {
		return Signal{}, nil
	}
	c, err := Decode(parentCommitment)
	if err != nil {
		return Signal{}, err
	}
	return newSignal(&c, parent.GasUsed, parent.GasLimit), nil
}

// newSignal is the low-level constructor, unexported so nothing outside can pair a commitment
// with the wrong gas limit. A nil parentCommitment is the bootstrap seed, which NextLaneSize maps
// to the floor as a consequence of the general formula rather than a special case.
func newSignal(parentCommitment *Commitment, parentGasUsed, parentGasLimit uint64) Signal {
	if parentCommitment == nil {
		return Signal{}
	}
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

	// Safety clamp, outside the BEP and LAST, deliberately able to push the quota below its own
	// floor. Below 25M of GasLimit the quota can exceed what a breathe block holds, and that halt
	// would be unrecoverable: isBreatheBlock is sticky while the head stays in the previous UTC
	// day, so every candidate block is again a breathe block and fails identically. Uses the
	// protocol constant, never the miner-local gasReserved, so both sides agree.
	return min(size, satSub(gasLimit, params.SystemTxsGasHardLimit))
}

func (s Signal) congestionAtLeast(triggerRatio uint64) bool {
	return gte128(s.parentSignalGasUsed, RatioDenom, triggerRatio, s.parentGasLimit)
}

// CheckNextLaneSize adjudicates a committed quota EXACTLY, because it is a pure function of the
// headers and the parent post-state - settled before any transaction executes, on import and on
// the MEV pre-seal path. The payment total is the half that needs replay instead; that is
// Budget.VerifyCommitment's job.
func (s Signal) CheckNextLaneSize(committed uint64, p Params, gasLimit uint64) error {
	if want := s.NextLaneSize(p, gasLimit); committed != want {
		return fmt.Errorf("%w: committed %d, derived %d", ErrQuotaMismatch, committed, want)
	}
	return nil
}

// laneCeiling and laneFloor are the clamp bounds, from the parameters and the gas limit of the
// block being computed. laneFloor is NOT a lower bound on NextLaneSize's result - the safety
// clamp may push below it.
func laneCeiling(p Params, gasLimit uint64) uint64 {
	return min(mulDivFloor(p.MaxRatio, gasLimit, RatioDenom), p.MaxGas)
}

func laneFloor(p Params, gasLimit uint64) uint64 {
	return min(max(mulDivFloor(p.MinRatio, gasLimit, RatioDenom), p.MinGas), laneCeiling(p, gasLimit))
}

// mulDivFloor returns floor(a*b/d) over 128 bits, saturating instead of panicking when the
// quotient does not fit. The guard covers bits.Div64's panic at hi >= d, so the caller has no
// precondition at all.
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
