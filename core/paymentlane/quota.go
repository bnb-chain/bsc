// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package paymentlane

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// RatioDenom mirrors PaymentLane.RATIO_DENOM: the four ratio parameters and the
// trigger/step pairs are parts per this.
const RatioDenom = 10_000

// maxLaneRatio mirrors PaymentLane.MAX_LANE_RATIO.
//
// Nothing reads this constant. The bound it mirrors - laneSize <= GasLimit/5, which
// the no-reachable-halt argument rests on - is enforced by the CONTRACT's validator,
// so what this exists for is TestConstantsMatchDeployedBytecode, the only thing that
// would notice a contract-side widening.
//
// An exact mirror, deliberately without the slack maxPaymentContractsRead is given:
// widening this to 4000 moves the halt boundary from 25M to 33.3M of GasLimit, so a
// mismatch must fail the test rather than be absorbed.
const maxLaneRatio = 2_000

// ---------------------------------------------------------------------------
// Deviations from the BEP-703 text.
//
// AUTHORITY: bnb-chain/BEPs PR #703 at its published head, read 2026-08-05. The text
// has been amended since this table was first written, and the amendments went the
// implementation's way: most of what the table listed is now specified rather than
// merely unpinned. Those entries moved to PINNED BY THE BEP, because each is still
// something a reader could "tidy up" - what changed is that doing so now contradicts
// the text instead of filling a gap in it, and re-listing one as a deviation invites
// an amendment that undoes what the text got right.
//
// Anything left implicit is a future consensus split for a second implementation: the
// recursion has memory (see LaneSize), so one block of disagreement is permanent.
//
// PINNED BY THE BEP - do not change without amending the text:
//
//   - Expansion comparison: signal >= expandTrigger (3.4.3).
//   - Multiply first, then floor-divide (3.4: "multiplies before dividing and
//     truncates toward zero").
//   - Which block's GasLimit: the PARENT's for the signal denominator, THIS block's
//     for the step (3.4.3, last paragraph) and for both bounds (3.4.4).
//   - Clamp every block, not only when a step fires (3.4.4, last paragraph).
//   - 128-bit products (3.4: "MUST use widened intermediates"); the accumulator
//     itself stays uint64 with saturating add and subtract. 10000 * 2^62 wraps to
//     zero in 64 bits, which would turn a shrink into an expansion.
//   - floor = min(max(minRatio*GL, minGas), ceiling): the ceiling prevails, so an
//     empty intersection is unrepresentable (3.4.4, which notes the same). Do not
//     restore the "absolute bounds prevail" reading an earlier draft had - it gives
//     a 2M-gas block a 2M floor, i.e. 100% of itself.
//   - Recursion in gas space, the step scaled by this block's GasLimit (3.4.3).
//   - Activation: the rules bind from activation+1 and the activation block is
//     exempt in every respect, carrying no commitment (3.4.5). It must stay the ONLY
//     exempt post-Gauss block, or ParentSignal's depth-1 discriminator breaks.
//   - The inequality. 3.3's two-term form is this file's three-term form: under
//     3.5.2's own definition general = header.GasUsed - payment, so
//     system + general + max(payment, lane) is exactly what 3.3 states. What differs
//     is the signal (item 1) and the header payload (item 5), not the rule.
//   - Shrink comparison: the strict signal < shrinkTrigger, so a signal landing on
//     the threshold holds rather than shrinking, and the band is
//     [shrinkTrigger, expandTrigger). CAVEAT: the published head still reads <=; the
//     amendment is drafted, not pushed. Until it lands this diverges from the
//     published text - resolve it THERE, do not "fix" the operator here.
//
// THE DEVIATIONS. Item 1 is ruled deliberate; items 6 and 8 were settled when they
// were written; item 7 records an incompleteness in the text, not a difference in
// behaviour. Items 2 to 5 are RECORDED BUT NOT RULED - they are here so that a reader
// diffing this file against the text finds them already known. For 2 to 4 the
// expected resolution is an amendment rather than a change here, since each closes a
// real hole and closing it for category 1 alone would leave the same hole open
// through a listed token; that is a preference, not a decision. Item 5 waits on the
// carrier decision, which is open for its own reasons.
//
//  1. THE CONGESTION SIGNAL OMITS THE PAYMENT OVERFLOW TERM. Section 3.4.2 defines
//     signalGasUsed = generalGasUsed + max(0, paymentGasUsed - paymentLaneSize).
//     Signal carries generalGasUsed only: newSignal decodes the parent's
//     paymentGasUsed and drops it. So the lane expands on general congestion alone,
//     and the traffic the lane privileges can never grow it - which also means
//     section 6's "sustained payment traffic beyond the quota does raise the signal
//     and expand the reservation" does not hold here. RULED DELIBERATE (2026-08-05).
//     Highest-value item in this table: the two signals differ on every block where
//     payment overflowed its quota, and one differing block offsets the accumulator
//     forever.
//
//  2. A ZERO-VALUE BARE TRANSFER IS GENERAL. Section 3.2's category 1 carries no
//     value condition - neither the table nor the pseudocode - so a value-0 call to
//     a code-less account is payment there and general here (Classify gate 7). A
//     transfer that moves nothing is not a payment; the price is that this is the
//     cheapest possible way for an implementation reading only the BEP to disagree,
//     on traffic anyone can produce for 21000 gas.
//
//  3. THE RESERVED RANGE IS WIDER THAN "NOT A PRECOMPILE". Section 3.2 excludes
//     precompile addresses. isReserved excludes everything at or below
//     maxReservedAddress, which additionally covers every Parlia system contract
//     and every unused address below 0x10000; see maxReservedAddress for why a
//     monotone range beats the precompile set. Reachable divergence: one wei to
//     0x0, or to any code-less address in that window - payment per the text,
//     general here.
//
//  4. THE LISTED-CONTRACT LOOKUP IS SUBORDINATE TO GATES 2-4. Section 3.2's
//     pseudocode tests the payment-contract list first and unconditionally, and
//     section 3.7 states it as "every transaction whose to is listed is a payment
//     transaction, whatever function it calls". Here a listed destination must
//     still pass the transaction-type allowlist, the empty-access-list test and the
//     reserved range. Each of those closes a way to consume the whole lane without
//     executing bytecode - bulk 7702 authorisations, DA space at lane price,
//     intrinsic access-list gas - and closing them for category 1 only would leave
//     every one of them open through a listed token. The everyday divergence is not
//     a BlobTx but an ordinary type-0x01 or 0x02 transfer to a listed token
//     carrying an access list: payment per the text, general here.
//
//  5. THE COMMITMENT CARRIES THREE VALUES AND A VERSION BYTE. Section 3.5.2 commits
//     two - laneSize at [0:8], paymentGasUsed at [8:16], [16:32] reserved and
//     mandatory zero - and derives generalGasUsed from header.GasUsed. Encode puts
//     generalGasUsed at [8:16], paymentGasUsed at [16:24] and commitVersion at
//     [24], i.e. inside the text's mandatory-zero window, so a conformant client
//     rejects every header this produces. Committing both buckets and deriving
//     systemGasUsed is what keeps a breathe block's system gas out of the
//     congestion signal; see Commitment and Encode. Section 3.5.3's choice of
//     header.UncleHash was adopted for BSC (2026-08-05), so the field name is no longer
//     a deviation - the LAYOUT still is, and no conformant client accepts these headers
//     until 3.5.2 is amended.
//
//  6. payBidTx IS USUALLY PAYMENT CLASS. The MEV rebate transaction is an ordinary
//     externally-signed transfer with empty calldata and no structural marker, so the
//     mechanical predicate decides it like any other - payment for the common shape, an
//     EOA payee and a non-zero fee, general when the payee is a contract (a splitter or
//     a Safe) or when the fee is zero, which is what BuilderFeeCeil defaults to. Either
//     way there is no consensus risk - both sides run the same predicate over the same
//     bytes - only an economic leak of about 25000 gas per MEV block. A client must NOT
//     reclassify it unilaterally: that is what would make the two sides' buckets
//     disagree and produce a BAD_BLOCK with no indicative log.
//
//  7. THE CONTRACT'S ABSOLUTE PARAMETER BOUNDS ARE NOT IN THE BEP. Section 3.6
//     gives invariants (1)-(6), and 3.4 bounds every ratio by RatioDenom. Nothing else. The
//     deployed contract also caps maxRatio at MAX_LANE_RATIO and bounds both
//     triggers, both steps and both absolute gas parameters. The no-reachable-halt
//     argument documented on maxLaneRatio rests on the first of those, so it rests
//     on a bound the text does not contain: an implementation reading only section
//     3.6 would accept a governance tuple this file treats as impossible.
//
//  8. THE GasLimit SAFETY CLAMP IS NOT IN THE BEP AT ALL. See the last step of
//     LaneSize for why it exists and why it is the smallest defence against an
//     unrecoverable halt.
//
// Two claims this table used to make are simply false and must not come back: that
// the type allowlist and the empty-access-list test are absent from the text (both
// are category-1 conditions in 3.2, verbatim), and that the BEP names no header field
// and no byte layout (3.5.2 gives both). Numbering is not stable across corrections
// like these - reference an entry by its heading.
// ---------------------------------------------------------------------------

// Applies reports whether the BEP-703 rules bind the block whose parent is given.
//
// Argument order matches the tree's convention for this shape - compare
// eip1559.VerifyEIP1559Header and eip4844.VerifyEIP4844Header, both
// (config, parent, header) - because an inverted call compiles silently and would
// answer for the parent block, which at the activation boundary is exactly the
// case this function exists to catch.
//
// The rules start at Gauss+1, not at the activation block: post-Feynman the Gauss
// upgrade runs from Finalize/FinalizeAndAssemble, i.e. after every user
// transaction, so while the activation block executes the contract has no code and
// no parameters can be read.
//
// A nil parent means there is no parent - the genesis header - which cannot be a
// lane block, so the answer is false. That is a real state, not a caller mistake.
// A nil header is a caller mistake and this panics on it, deliberately: unlike
// "no parent", "no block being asked about" has no meaning, and silently answering
// false there would disable a consensus rule instead of reporting a bug. Callers
// that must tell "no parent" apart from "not a lane block" have to check first.
//
// header.Time must already be final: parlia's Prepare rewrites it, so evaluating
// this earlier can gate the wrong side of the boundary.
//
// CAVEAT, and it is a real one. This reuses the same predicate pair that installs
// the code (upgradeBuildInSystemContract gates on IsOnGauss), which makes the two
// agree on every chain whose Gauss timestamp falls after its London block. It does
// NOT make them agree unconditionally: if LondonBlock is 0 and GaussTime is at or
// before the genesis timestamp, then IsGauss already holds at genesis, IsOnGauss
// therefore never fires, the contract is never installed - and this function still
// returns true from block 1. LoadParams cannot detect that, because an absent
// account and an untouched one are both all-zero storage. The result is a chain
// running the lane against a code-less address on hardcoded defaults, with
// governance unable to change them. Real networks are safe (mainnet's LondonBlock
// is 31,302,048 and the devnet template's is 8), so this is a constraint on new
// chain configurations, not a live defect - but it must be checked when Gauss is
// scheduled. TestAppliesCannotDetectAnUninstalledContract pins the behaviour.
func Applies(config *params.ChainConfig, parent, header *types.Header) bool {
	if parent == nil {
		return false
	}
	return config.IsGauss(header.Number, header.Time) &&
		!config.IsOnGauss(header.Number, parent.Time, header.Time)
}

// Signal is everything about block n-1 that laneSize(n) reads.
//
// The fields are unexported and ParentSignal is the only exported constructor. The
// invariant that buys is narrow but exactly the one that matters: no exported path
// pairs a commitment with an arbitrary gas limit. It does NOT mean every Signal is
// well-formed - the zero value is constructible anywhere and is meaningful (it is the
// bootstrap seed), and ParentSignal given a grandparent that is not the parent's
// parent will return a Signal built on the wrong gate.
type Signal struct {
	laneSize       uint64 // laneSize(n-1), the recursion state
	generalGasUsed uint64 // signal numerator
	gasLimit       uint64 // signal denominator; GasLimit(n-1), never GasLimit(n)
}

// ParentSignal derives the recursion input for the block after parent.
//
// commitment is the 32 bytes the carrier field holds in parent. The caller passes
// it because which field carries it is not this package's decision; everything
// else is decided here on purpose:
//
//   - whether the parent is expected to carry a commitment at all, which needs the
//     GRANDparent and must never be inferred from a decode failure - a failure
//     cannot tell a legitimate bootstrap apart from a corrupt commitment;
//   - that the signal denominator is the PARENT's gas limit and not the child's.
//
// Both were previously the caller's job at three consensus-critical sites with nothing
// keeping them equal, and one site getting either wrong on one block is a permanent
// divergence - so they belong here rather than in a comment asking callers to be
// careful.
//
// The three header arguments each have a distinct meaning when nil:
//
//	parent nil                    the caller failed to resolve a header. An error,
//	                              not a panic - unlike Applies, this has a channel
//	                              for it.
//	grandparent nil, parent 0     the parent is genesis. Legal, and the only case in
//	                              which it is.
//	grandparent nil, parent > 0   the caller failed to resolve the GRANDparent. Also
//	                              an error, and the explicit number test is the whole
//	                              reason: Applies answers false for a nil parent, so
//	                              otherwise this falls through to the zero Signal,
//	                              LaneSize maps that to the floor, and the quota
//	                              silently resets instead of the block being rejected.
//
// A parent the lane does not apply to yields the zero Signal, which LaneSize maps to
// the floor - the bootstrap seed.
func ParentSignal(config *params.ChainConfig, grandparent, parent *types.Header, commitment common.Hash) (Signal, error) {
	if parent == nil {
		return Signal{}, fmt.Errorf("%w: nil parent header", ErrBadCommitment)
	}
	if grandparent == nil && parent.Number.Sign() != 0 {
		return Signal{}, fmt.Errorf("%w: nil grandparent for parent %d", ErrBadCommitment, parent.Number)
	}
	if !Applies(config, grandparent, parent) {
		return Signal{}, nil
	}
	c, err := Decode(commitment)
	if err != nil {
		return Signal{}, err
	}
	return newSignal(&c, parent.GasLimit), nil
}

// newSignal is the low-level constructor. Unexported: reaching it from outside
// would reintroduce the possibility of pairing a commitment with the wrong gas
// limit, which is the whole reason ParentSignal exists.
//
// prev nil yields the zero Signal, and LaneSize maps that to the floor - as a
// consequence of the general function, not as a second formula. A bootstrap
// special case would be a second definition of a consensus value that executes
// once per network and therefore can never be regression-tested on the network
// where it matters.
func newSignal(prev *Commitment, parentGasLimit uint64) Signal {
	if prev == nil {
		return Signal{}
	}
	return Signal{
		laneSize:       prev.LaneSize,
		generalGasUsed: prev.GeneralGasUsed,
		gasLimit:       parentGasLimit,
	}
}

// LaneSize is block n's payment lane quota: the producer's packing budget and the
// value it commits, one number with two uses - so there is no pair of derived
// values that must agree.
//
// gasLimit is header.GasLimit of block n; the parent's is already inside s.
//
// Total by construction: defined for every input, no panic, and no error return.
// The absence of an error return is deliberate - an error here invites a
// caller-side fallback, and a fallback on this path is a chain split.
//
// Note this totality is LaneSize's alone. Applies panics on a nil header and
// ParentSignal errors on a nil parent; both are documented at their own definitions.
//
// The producer must not clamp the result; see Budget.
func LaneSize(p Params, s Signal, gasLimit uint64) uint64 {
	next := s.laneSize

	// A parent with no capacity carries no signal. This guard is what makes the
	// zero Signal the bootstrap seed, and it is live on every network at exactly
	// block Gauss+1. Without it, 0 >= expandTrigger*0 holds and the seed takes the
	// expansion branch. That is invisible under the shipped defaults, where the
	// resulting step of 1.1M is clamped back up to the 2M floor and the answer is
	// unchanged - but it diverges for any governance-legal tuple whose step exceeds
	// its floor, which is why TestBootstrapIsTheZeroSignal has to range over the
	// whole lattice to see it.
	if s.gasLimit != 0 {
		switch {
		case gte128(s.generalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit):
			next = satAdd(next, mulDivFloor(p.ExpandStep, gasLimit, RatioDenom))
		case !gte128(s.generalGasUsed, RatioDenom, p.ShrinkTrigger, s.gasLimit):
			next = satSub(next, mulDivFloor(p.ShrinkStep, gasLimit, RatioDenom))
		}
		// else: hysteresis band, the quota holds.
	}

	// Clamp unconditionally, every block, after the step - the BEP pins this (3.4.4).
	//
	// The floor is taken against the ceiling (3.4.4), so floor <= ceiling
	// always holds, the two clamp orders coincide, and the empty-intersection corner
	// does not exist. Note that inner min is redundant for THIS result - the outer
	// min(..., ceiling) dominates it - and is kept because it is the contract's
	// formula and because Floor() alone does need it.
	ceiling := laneCeiling(p, gasLimit)
	size := min(max(next, laneFloor(p, gasLimit)), ceiling)

	// Safety clamp, applied LAST and deliberately able to push the quota below its
	// own floor. See THE GasLimit SAFETY CLAMP in the registry above.
	//
	// MAX_LANE_RATIO makes size <= GasLimit/5 unconditional, so the quota can only
	// exceed what a block can actually hold once GasLimit drops below about 25M -
	// and then it does so on breathe blocks, where Parlia reserves
	// SystemTxsGasHardLimit. That state is unrecoverable rather than merely bad:
	// isBreatheBlock is sticky once the parent falls in the previous UTC day, and
	// the quota depends only on (parent, header), so every candidate block fails
	// identically, forever, with no protocol path out.
	//
	// This uses the protocol constant and never the miner-local gasReserved, so
	// both sides compute the same number. Production and the devnet run far above
	// the boundary (55M and 35M against 25M), so it is inert there; it exists so
	// that a chain whose validators lower the gas limit degrades to a smaller - or
	// absent - lane instead of stopping. The alternative, rejecting blocks below a
	// GasLimit floor, can itself halt a chain that is already below it.
	return min(size, satSub(gasLimit, params.SystemTxsGasHardLimit))
}

// CheckLaneSize reports whether a committed quota is the one the rules require.
//
// This is the check that makes the recursion truthful, and it is separate from
// Budget.VerifyCommitment on purpose: the quota is a pure function of the parent
// header and the parent post-state, so it can be settled BEFORE executing a single
// transaction - including on the MEV admission path, before a validator signs.
// The buckets are the part that genuinely needs replay.
//
// The distinction matters for what each check can prove. A dishonest builder can
// always present self-consistent fake buckets, so the pre-seal gate cannot
// adjudicate those. It CAN adjudicate the quota, because the quota is not a value
// the builder gets to choose.
func CheckLaneSize(committed uint64, p Params, s Signal, gasLimit uint64) error {
	if want := LaneSize(p, s, gasLimit); committed != want {
		return fmt.Errorf("%w: committed %d, derived %d", ErrQuotaMismatch, committed, want)
	}
	return nil
}

// Ceiling returns the upper clamp bound for this block.
//
// Used by tests and by the devnet read-path check, not by any metric: the miner reports
// laneSize and idleLane, which are what an operator can act on, and the bounds are a pure
// function of the parameters that anyone can recompute.
func Ceiling(p Params, gasLimit uint64) uint64 { return laneCeiling(p, gasLimit) }

// Floor returns the lower clamp bound for this block. See Ceiling on who uses these.
// Note it is not a lower bound on LaneSize's result: the safety clamp may go below it.
func Floor(p Params, gasLimit uint64) uint64 { return laneFloor(p, gasLimit) }

func laneCeiling(p Params, gasLimit uint64) uint64 {
	return min(mulDivFloor(p.MaxRatio, gasLimit, RatioDenom), p.MaxGas)
}

func laneFloor(p Params, gasLimit uint64) uint64 {
	return min(max(mulDivFloor(p.MinRatio, gasLimit, RatioDenom), p.MinGas), laneCeiling(p, gasLimit))
}

// mulDivFloor returns floor(a*b/d) over 128 bits, saturating.
//
// Overflow proof: GasLimit < 2^63 (consensus-checked) and every ratio is at most
// RatioDenom, so a*b < 2^77 and the 128-bit product is exact. bits.Div64 panics
// when the high word is at or above the divisor, which the guard covers - and
// since d is only ever the non-zero constant RatioDenom, the guard is the only
// precondition and the caller has none.
func mulDivFloor(a, b, d uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi >= d {
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, d)
	return q
}

// gte128 reports a*b >= c*d exactly, without division and without floats.
func gte128(a, b, c, d uint64) bool {
	ah, al := bits.Mul64(a, b)
	ch, cl := bits.Mul64(c, d)
	if ah != ch {
		return ah > ch
	}
	return al >= cl
}
