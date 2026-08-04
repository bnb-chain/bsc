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
// No production code reads it, and that is worth stating plainly rather than
// implying otherwise: the bound laneSize <= GasLimit/5 - which the no-reachable-halt
// argument rests on - is enforced by the CONTRACT's validator, not here. This
// constant exists so TestConstantsMatchDeployedBytecode can assert the two sides
// still agree, which is the only thing that would notice a contract-side widening.
//
// Deliberately NOT given the "let the client be laxer so governance can be widened
// by a contract upgrade alone" slack that maxWhitelistLen has. Widening this to
// 4000 would silently move the halt boundary from 25M to 33.3M of GasLimit, so a
// mismatch here must fail the test rather than be absorbed.
const maxLaneRatio = 2_000

// ---------------------------------------------------------------------------
// Deviations from the BEP-703 text.
//
// The BEP is not being amended right now, so every place this implementation
// makes a choice the text does not pin down is recorded here, in one block, so a
// future amendment can be lifted from it. Anything left implicit is a future
// consensus split for a second client implementation - the recursion has memory
// (see the hysteresis-band argument on LaneSize), so a single block of
// disagreement is permanent, not transient.
//
//  1. COMPARISON STRICTNESS. The text gives no operators. This uses
//     signal >= expandTrigger for expansion and signal < shrinkTrigger for
//     shrinkage. The two tests are disjoint for any expandTrigger >= shrinkTrigger
//     purely from the operators; what TRIGGER_GAP_MIN buys is a NON-EMPTY
//     hysteresis band between them. So the only divergence is a block whose signal
//     lands exactly on a boundary - and that one block offsets the accumulator
//     forever. Highest-value item here.
//
//  2. ROUNDING AND ORDER OF OPERATIONS. Multiply first, then floor-divide:
//     floor(ratio * GasLimit / RatioDenom). This follows the contract header
//     comment. Divide-first differs by up to ratio-1 gas and, critically, agrees
//     whenever GasLimit is a multiple of RatioDenom - which is the steady state -
//     so the divergence is invisible except while GasLimit is in transit.
//
//  3. WHICH BLOCK'S GasLimit. Parlia permits +/-1/1024 per block, so the parent's
//     and this block's differ routinely. Signal denominator: the PARENT's, because
//     it has to be the same block as the numerator; otherwise the ratio can exceed
//     1 whenever the limit fell and expansion fires on an uncongested parent. Step,
//     ceiling and floor: THIS block's, because laneSize(n) is a budget inside
//     block n's GasLimit, and that is what makes the bound below exact rather than
//     off by a factor of 1024/1023.
//
//  4. CLAMP EVERY BLOCK, not only when a step fires. The text only describes the
//     step. A GasLimit walk-down alone breaks the other reading: with traffic
//     parked in the hysteresis band the whole way, a lane sitting at the 8% ceiling
//     of a 70M block holds 5.6M gas while the ceiling falls under it, reaching 28%
//     of a 20M block - above MAX_LANE_RATIO - and then dropping to that block's
//     1.6M ceiling in one step, a factor of 3.5, instead of gradually. (Continue
//     the walk to a 7M block and the same jump is a factor of ten.)
//
//  5. INTEGER WIDTH. Every PRODUCT is computed over 128 bits (gte128,
//     mulDivFloor); the accumulator itself stays uint64 with saturating add and
//     subtract. The products are what needs the width: the consensus bound on
//     GasLimit is 2^63-1, not the ~5.5e7 production runs at, and 10000 * 2^62
//     wraps to zero in 64 bits - which would turn a shrink into an expansion. A
//     consensus function must not rest on an unenforced bound, and must not carry
//     a rejecting branch to enforce one.
//
//  6. THE CEILING PREVAILS, NOT THE ABSOLUTE BOUNDS. Section 3.4 says the
//     absolute bounds win when the two ranges do not intersect. The contract
//     instead defines floor as min(max(minRatio*GL, minGas), ceiling), which makes
//     an empty intersection unrepresentable. This is a BEHAVIOURAL difference, not
//     a restatement: on a 2M-gas block with the shipped defaults, "absolute
//     prevails" would put the floor at minGas = 2M, i.e. 100% of the block, while
//     this yields ceiling = floor = 160,000, i.e. the 8% maxRatio. The contract's
//     order is the safe one and is what this implements.
//
//  7. GAS-SPACE RECURSION. Section 3.3 describes stepping a ratio. This steps the
//     absolute gas value, with the step scaled by the current GasLimit
//     (stepGas = stepRatio * GasLimit / RatioDenom). Equivalent while GasLimit is
//     constant; the gas-space form has no hidden second value that must agree with
//     the committed one. A fixed gas step, by contrast, would make expansion
//     relatively slower as the chain grows, which is what the ratio design exists
//     to avoid.
//
//  8. ACTIVATION SEMANTICS. The text does not say when the rules begin. Because
//     post-Feynman system-contract upgrades run at the END of the fork block, the
//     PaymentLane code does not exist while that block executes; so the rules
//     first bind at Gauss+1, the activation block is exempt in every respect, and
//     it is the ONLY exempt post-Gauss block. A second exemption would break the
//     depth-1 discriminator that tells "parent legitimately carries no
//     commitment" apart from "parent commitment is corrupt".
//
//  9. THE PAYMENT CLASS IS NARROWER THAN SECTION 3.1's CATEGORY 1. Two extra
//     conditions, both in Classify and neither in the BEP text: the transaction
//     type must be 0x00, 0x01 or 0x02 (so BlobTx and SetCodeTx are general, as is
//     every future type by default), and the access list must be empty. Each
//     closes a way to consume the whole lane without executing bytecode - bulk
//     7702 authorisations, DA space at lane price, intrinsic access-list gas. This
//     is the deviation most likely to split a second client: an implementation
//     reading only the BEP admits those transfers, and the two sides' buckets
//     disagree on the first one.
//
// 10. payBidTx IS PAYMENT CLASS. The MEV rebate transaction is an ordinary
//     externally-signed transfer with empty calldata and no structural marker, so
//     the mechanical predicate makes it payment on both sides. There is no
//     consensus risk in that - both sides run the same predicate over the same
//     bytes - only an economic leak of about 25000 gas per MEV block. A client
//     must NOT reclassify it unilaterally: that is what would make the two sides'
//     buckets disagree and produce a BAD_BLOCK with no indicative log.
//
// 11. THE COMMITMENT ENCODING. The BEP names no header field and no byte layout.
//     The three committed values, their order, the version byte and the rejection
//     of any non-zero reserved byte are all choices made here; see Encode.
//
// 12. TWO BUCKETS COMMITTED, systemGasUsed DERIVED. The BEP's inequality has three
//     terms; this commits general and payment and derives system by subtraction
//     from header.GasUsed. That fixes what a header must carry, and it keeps a
//     breathe block's system gas out of the congestion signal.
//
// 13. THE GasLimit SAFETY CLAMP IS NOT IN THE BEP AT ALL. See the last step of
//     LaneSize for why it exists and why it is the smallest defence against an
//     unrecoverable halt.
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
// Both were previously the caller's job at three separate consensus-critical
// sites, with nothing keeping them equal. Since the recursion has memory, one
// site getting either wrong on one block is a permanent divergence, so the two
// steps belong here rather than in a comment telling three callers to be careful.
//
// A nil grandparent means the parent is genesis. When the parent is not a lane
// block the result is the zero Signal, which LaneSize maps to the floor.
//
// A nil parent is an error rather than a panic: unlike Applies, this function has an
// error channel, and a nil parent here means the caller failed to resolve a header -
// exactly the condition an error exists to report.
func ParentSignal(config *params.ChainConfig, grandparent, parent *types.Header, commitment common.Hash) (Signal, error) {
	if parent == nil {
		return Signal{}, fmt.Errorf("%w: nil parent header", ErrBadCommitment)
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

	// Clamp unconditionally, every block, after the step (deviation 4).
	//
	// The floor is taken against the ceiling (deviation 6), so floor <= ceiling
	// always holds, the two clamp orders coincide, and the empty-intersection corner
	// does not exist. Note that inner min is redundant for THIS result - the outer
	// min(..., ceiling) dominates it - and is kept because it is the contract's
	// formula and because Floor() alone does need it.
	ceiling := laneCeiling(p, gasLimit)
	size := min(max(next, laneFloor(p, gasLimit)), ceiling)

	// Safety clamp, applied LAST and deliberately able to push the quota below its
	// own floor (deviation 13; not in the BEP).
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

// Ceiling returns the upper clamp bound for this block, for metrics and logging.
func Ceiling(p Params, gasLimit uint64) uint64 { return laneCeiling(p, gasLimit) }

// Floor returns the lower clamp bound for this block, for metrics and logging.
// Note it is not a lower bound on LaneSize's result: the safety clamp may go below
// it.
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
