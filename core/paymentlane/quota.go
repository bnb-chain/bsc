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
// An exact mirror, deliberately: widening this to 4000 moves the halt boundary from 25M
// to 33.3M of GasLimit, so a mismatch must fail the test rather than be absorbed.
const maxLaneRatio = 2_000

// ---------------------------------------------------------------------------
// Deviations from the BEP-703 text.
//
// AUTHORITY: bnb-chain/BEPs PR #703 at its published head. The BEP is not being amended
// to accommodate this implementation, so every entry below is a divergence a second
// implementation would hit, and the recursion has memory (see LaneSize): one block of
// disagreement is permanent.
//
// PINNED BY THE BEP - do not change without amending the text:
//
//   - Expansion comparison: signal >= expandTrigger (3.4.3). Shrink is the strict
//     signal < shrinkTrigger, so a signal landing on the threshold holds and the band
//     is [shrinkTrigger, expandTrigger); 3.4.3 states both operators are normative.
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
//   - The inequality, in 3.3's two-term form. generalGasUsed is header.GasUsed less
//     paymentGasUsed - 3.5.2 says so - so it covers Parlia's system transactions, and
//     CheckInequality needs no third term. Do not reintroduce one: a separate
//     systemGasUsed argument was how an earlier version came to keep system gas out of
//     the congestion signal, which is the divergence described below.
//   - BOTH terms of the congestion signal (3.4.2): general gas taken as the header
//     residual, plus payment gas beyond the quota. Each guards the same structural
//     property from a different side - a saturated block always expands, because at the
//     rule's equality the numerator is exactly GasLimit - laneSize and invariant (5) of
//     3.6 caps laneSize at RATIO_DENOM - EXPAND_TRIGGER_RATIO for that reason. Dropping
//     the residual makes a breathe block read as quiet; dropping the overflow makes a
//     payment-dominated full block read as quiet. newSignal spells both out.
//   - The commitment layout of 3.5.2: laneSize at [0:8], paymentGasUsed at [8:16],
//     [16:32] zero. General gas is not committed because it is derivable, and the zero
//     tail is what tells these bytes apart from an uncle-list hash. Do not spend a byte
//     of that window on a version tag - the reachable all-zero commitment is legal, and
//     what rejects a header that never had one written is CheckLaneSize.
//
// THE DEVIATIONS. Items 1 to 3 are RECORDED BUT NOT RULED - they are here so that a
// reader diffing this file against the text finds them already known. For all three the
// expected resolution is an amendment rather than a change here: 1 and 3 close a hole the
// text leaves open, and closing it for category 1 alone would leave the same hole open
// through a listed token, while 2 declines a clause whose hole the text leaves open
// elsewhere anyway. That is a preference, not a decision. Items 4 and 5 record what the
// text does not contain at all.
//
//  1. A ZERO-VALUE BARE TRANSFER IS GENERAL. Section 3.2's category 1 carries no
//     value condition - neither the table nor the pseudocode - so a value-0 call to
//     a code-less account is payment there and general here (Classify gate 6). A
//     transfer that moves nothing is not a payment; the price is that this is the
//     cheapest possible way for an implementation reading only the BEP to disagree,
//     on traffic anyone can produce for 21000 gas.
//
//  2. THERE IS NO PRECOMPILE EXCLUSION. Section 3.2 requires "to is not a precompile
//     address", because a precompile holds no code yet a call to one runs it, and a
//     precompile that rejects its input consumes every unit handed to it. There is no
//     such gate here, so one wei to 0x09 with empty data is general per the text and
//     PAYMENT here. The clause belongs to the same family as gates 2 and 3 - keep shapes
//     that are not payments out of a pool general traffic cannot enter at any price - and
//     the reason to decline this one and keep those is what the shape buys the sender. A
//     BlobTx buys DA and a SetCodeTx buys authorisations, both at lane priority, which is
//     worth attacking for; a call to a rejecting precompile buys nothing but burnt gas at
//     full price, so the only payoff is denying the lane to other payments - and section 6
//     already accepts exactly that denial by a flood of ordinary transfers at that same
//     price. Against that, every implementable form of the clause costs something real:
//     the exact set is fork-dependent and IsInBSC-dependent and grows a branch every fork,
//     and an address-range approximation of it swallows every code-less address below its
//     top, which was this entry's previous content. Expected resolution is an amendment
//     striking the clause from 3.2, which also wants a word on category 1's prose
//     definition, since "executes no contract code" is what the clause implements.
//
//  3. THE LISTED-CONTRACT LOOKUP IS SUBORDINATE TO GATES 2 AND 3. Section 3.2's
//     pseudocode tests the payment-contract list first and unconditionally, and
//     section 3.7 states it as "every transaction whose to is listed is a payment
//     transaction, whatever function it calls". Here a listed destination must still
//     pass the transaction-type allowlist and the empty-access-list test. Both close a
//     way to consume the whole lane without executing bytecode - bulk 7702
//     authorisations, DA space at lane price, intrinsic access-list gas - and closing
//     them for category 1 only would leave both open through a listed token. The
//     everyday divergence is not a BlobTx but an ordinary type-0x01 or 0x02 transfer to
//     a listed token carrying an access list: payment per the text, general here.
//
//  4. THE CONTRACT'S ABSOLUTE PARAMETER BOUNDS ARE NOT IN THE BEP. Section 3.6
//     gives invariants (1)-(6), and 3.4 bounds every ratio by RatioDenom. Nothing else.
//     The deployed contract also caps maxRatio at MAX_LANE_RATIO and bounds both
//     triggers, both steps and both absolute gas parameters. The no-reachable-halt
//     argument documented on maxLaneRatio rests on the first of those, so it rests
//     on a bound the text does not contain: an implementation reading only section
//     3.6 would accept a governance tuple this file treats as impossible.
//
//  5. THE GasLimit SAFETY CLAMP IS NOT IN THE BEP AT ALL. See the last step of
//     LaneSize for why it exists and why it is the smallest defence against an
//     unrecoverable halt. It is also what makes laneSize == 0 - and therefore the
//     all-zero commitment - reachable, on any chain at or below SystemTxsGasHardLimit.
//
// Not a deviation, but the question comes up: payBidTx is usually PAYMENT class. The MEV
// rebate transaction is an ordinary externally-signed transfer with empty calldata and no
// structural marker, so the mechanical predicate decides it like any other - payment for
// the common shape, an EOA payee and a non-zero fee, general when the payee is a contract
// or when the fee is zero, which is what BuilderFeeCeil defaults to. The BEP says the
// same for the same shapes; only item 1 makes the zero-fee case differ. Either way there
// is no consensus risk, both sides running the same predicate over the same bytes - but a
// client must NOT reclassify it unilaterally, since that is what would make the two
// sides' payment totals disagree and produce a BAD_BLOCK with no indicative log.
//
// Numbering is not stable across corrections - reference an entry by its heading.
// ---------------------------------------------------------------------------

// Signal is everything about block n-1 that laneSize(n) reads.
//
// The fields are unexported and ParentSignal is the only exported constructor. The
// invariant that buys is narrow but exactly the one that matters: no exported path
// pairs a commitment with an arbitrary gas limit. It does NOT mean every Signal is
// well-formed - the zero value is constructible anywhere and is meaningful (it is the
// bootstrap seed), and ParentSignal given a grandparent that is not the parent's
// parent will return a Signal built on the wrong gate.
type Signal struct {
	laneSize      uint64 // laneSize(n-1), the recursion state
	signalGasUsed uint64 // signal numerator, per section 3.4.2
	gasLimit      uint64 // signal denominator; GasLimit(n-1), never GasLimit(n)
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
// The three nil cases each mean something different:
//
//	parent nil                    the caller failed to resolve a header.
//	grandparent nil, parent 0     the parent is genesis. Legal, and the only case in
//	                              which it is - the parent predates Gauss, so it
//	                              carries no commitment and the recursion seeds.
//	grandparent nil, parent > 0   the caller failed to resolve the GRANDparent. Also
//	                              an error, and the explicit number test is the whole
//	                              reason: without it this falls through to the zero
//	                              Signal, LaneSize maps that to the floor, and the
//	                              quota silently resets instead of the block being
//	                              rejected.
//
// A parent the rules did not bind - Gauss not yet active for the grandparent, so the
// parent is the activation block or older - yields the zero Signal, which LaneSize maps
// to the floor.
func ParentSignal(config *params.ChainConfig, grandparent, parent *types.Header, commitment common.Hash) (Signal, error) {
	if parent == nil {
		return Signal{}, fmt.Errorf("%w: nil parent header", ErrBadCommitment)
	}
	if grandparent == nil && parent.Number.Sign() != 0 {
		return Signal{}, fmt.Errorf("%w: nil grandparent for parent %d", ErrBadCommitment, parent.Number)
	}
	if grandparent == nil || !config.IsGauss(grandparent.Number, grandparent.Time) {
		return Signal{}, nil
	}
	c, err := Decode(commitment)
	if err != nil {
		return Signal{}, err
	}
	return newSignal(&c, parent.GasUsed, parent.GasLimit), nil
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
//
// The numerator is section 3.4.2 verbatim: all general gas, plus the payment gas that
// overflowed the quota. Both terms matter, and each guards a different half of one
// structural property - that a SATURATED block always expands. Substituting the block
// rule at equality gives signalGasUsed == GasLimit - laneSize, and invariant (5) of
// section 3.6 caps laneSize at exactly RATIO_DENOM - EXPAND_TRIGGER_RATIO for that
// reason, so a full block clears the expand trigger by construction.
//
// Drop the payment overflow and a full block whose payment traffic dominates reads as
// quiet: general 35M plus payment 20M fills a 55M block, yet the numerator would be 35M,
// under the 38.5M shrink trigger, and the payment floor would be cut in the very
// congestion it exists for. Drop system gas - by taking a committed general figure that
// excludes it instead of the header residual - and a breathe block does the same, because
// its user pool is only GasLimit less SystemTxsGasHardLimit and can never reach the
// trigger at all.
//
// satSub rather than an error on parentGasUsed < PaymentGasUsed: the pair has already
// been through CheckHeaderBounds at header verification, so the difference cannot be
// negative on a chain-connected parent, and ParentSignal must stay total for the same
// reason LaneSize does.
func newSignal(prev *Commitment, parentGasUsed, parentGasLimit uint64) Signal {
	if prev == nil {
		return Signal{}
	}
	return Signal{
		laneSize: prev.LaneSize,
		signalGasUsed: satAdd(
			satSub(parentGasUsed, prev.PaymentGasUsed), // all general gas
			satSub(prev.PaymentGasUsed, prev.LaneSize), // payment beyond the quota
		),
		gasLimit: parentGasLimit,
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
// Note this totality is LaneSize's alone: ParentSignal errors on a nil parent.
//
// The producer must not clamp the result; see core.LaneState.SetQuota for why.
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
		case gte128(s.signalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit):
			next = satAdd(next, mulDivFloor(p.ExpandStep, gasLimit, RatioDenom))
		case !gte128(s.signalGasUsed, RatioDenom, p.ShrinkTrigger, s.gasLimit):
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
// Budget.VerifyCommitment on purpose: the quota is a pure function of the parent header
// and the parent post-state, so it is settled before a single transaction executes - on
// import, and on the MEV pre-seal path before a validator signs a builder's block. The
// buckets are what genuinely needs replay.
//
// The distinction is what each check can prove. The quota is not a value a builder gets
// to choose, so it is adjudicated exactly. Its payment total is not: the pre-seal gate can
// only bound it from above, by what the block's payment-class transactions declare, and exact
// equality waits for the importer.
func CheckLaneSize(committed uint64, p Params, s Signal, gasLimit uint64) error {
	if want := LaneSize(p, s, gasLimit); committed != want {
		return fmt.Errorf("%w: committed %d, derived %d", ErrQuotaMismatch, committed, want)
	}
	return nil
}

// laneCeiling and laneFloor are the two clamp bounds for this block. Unexported: no
// metric reports them - the miner reports laneSize and idleLane, which are what an
// operator can act on - and they are a pure function of the parameters, which anyone
// can recompute. Note laneFloor is not a lower bound on LaneSize's result: the safety
// clamp may go below it.
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
