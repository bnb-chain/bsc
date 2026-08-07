// Package paymentlane implements the BEP-703 payment lane rules.
//
// The rule is one inequality per block:
//
//	generalGasUsed + max(paymentGasUsed, laneSize) <= GasLimit
//
// generalGasUsed is header.GasUsed less paymentGasUsed, so it covers Parlia's system
// transactions too - they are general transactions under section 3.2 like any other.
// Substituting it gives the form every check here actually evaluates:
//
//	header.GasUsed + max(0, laneSize - paymentGasUsed) <= GasLimit
//
// The quota is a floor, not a ceiling: payment gas beyond the quota competes for
// the remaining space on equal terms with general traffic. Unused quota idles and
// is not reclaimed - if the shortfall flowed back to general, excluding payment
// transactions would be free for the block producer and the floor would be
// vacuous exactly when it is needed most.
//
// This package is scaffolding: it holds the fixed, reusable primitives that the
// block producer, the block importer and the MEV paths all evaluate. It does not
// wire itself into any of them. Everything here is either a pure function or a
// value bound to one parent state root, so the producing and validating sides
// cannot disagree by construction rather than by discipline.
//
// # Where the commitment lives
//
// laneSize(n) is a recursive quantity: the accumulated sum of every +/-step since
// activation. Recomputing it from chain history is not merely expensive, it needs every
// intervening block's CLASSIFIED paymentGasUsed - i.e. full re-execution against
// historical state, not headers alone - so it must be carried in the parent header. This
// package defines how the two committed values pack into 32 bytes (Encode/Decode) and
// still does not name the field, so the rules stay independent of it. The carrier chosen
// for BSC is header.UncleHash, wired in core and core/types; the only reason to know that
// here is that the encoding's zero tail is what tells a commitment apart from an
// uncle-list hash, EmptyUncleHash included.
//
// # Deviations from the BEP text
//
// See the registry in quota.go. The BEP is not being amended right now, so every
// place this implementation makes a choice the text does not pin down is recorded
// there, in one block, so a future amendment can be lifted from it verbatim.
package paymentlane

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
)

// Class is the lane class of a transaction.
//
// Parlia system transactions are general transactions - they call a system contract
// with non-empty calldata, so section 3.2 puts them in the general class like any
// other such call - but they are never labelled: core.StateProcessor splits them out
// of the user-transaction loop, they never pass through the GasPool, and Finalize adds
// their gas straight to header.GasUsed. General gas is the residual there, so they are
// counted without being tagged, and a ClassSystem would have nothing to tag.
//
// There are exactly two values. "Not yet classified" is not a state: the caller
// must have settled the class before accounting, so Budget.RecordUsed cannot fail.
type Class uint8

const (
	// ClassGeneral must be the zero value. The lane's zero-regression property
	// depends on it: a zero Budget degrades the admission predicate into the
	// upstream one for every class, so before activation there is nothing to gate.
	// Reordering these constants makes a default-constructed Class mean payment.
	ClassGeneral Class = iota
	ClassPayment
)

func (c Class) String() string {
	if c == ClassPayment {
		return "payment"
	}
	return "general"
}

var (
	ErrViolated         = errors.New("payment lane inequality violated")
	ErrBucketMismatch   = errors.New("payment lane payment bucket exceeds the transaction pool total")
	ErrBadCommitment    = errors.New("payment lane commitment is malformed")
	ErrUntruthy         = errors.New("payment lane commitment does not match replayed buckets")
	ErrCorruptConfig    = errors.New("payment lane config storage layout mismatch")
	ErrQuotaMismatch    = errors.New("payment lane quota does not match the parent derivation")
	ErrStateUnavailable = errors.New("payment lane state unavailable")
)

// Commitment is the per-block recursion state plus the payment bucket, exactly the
// two values BEP-703 section 3.5.2 commits.
//
// generalGasUsed has no field: it is header.GasUsed less PaymentGasUsed, which makes it
// unforgeable rather than merely redundant - a producer cannot misstate it without
// misstating header.GasUsed, and that is already checked in consensus. Committing it
// instead would only be necessary to keep some part of the block's gas OUT of the
// congestion signal, and no part of it is out.
type Commitment struct {
	LaneSize       uint64
	PaymentGasUsed uint64
}

// Encode packs a Commitment into 32 bytes:
//
//	[0:8]   laneSize        uint64 big-endian
//	[8:16]  paymentGasUsed  uint64 big-endian
//	[16:32] reserved, always zero
//
// The committed quota is the absolute gas value, not a ratio, so the recursion
// runs in gas space and the committed number is exactly the one the producer used
// to pack. Committing a ratio instead would add a second derived value that must
// agree with the first, with nothing enforcing it.
//
// The zero tail is what distinguishes these 32 bytes from an uncle-list hash, so it is
// load-bearing framing rather than padding; see types.UncleHashMatches.
func Encode(c Commitment) common.Hash {
	var h common.Hash
	binary.BigEndian.PutUint64(h[0:8], c.LaneSize)
	binary.BigEndian.PutUint64(h[8:16], c.PaymentGasUsed)
	return h
}

// Decode is the inverse of Encode. The reserved-tail check is the whole of the
// framing: without it an unrelated future use of the same 32 bytes, or a
// pre-activation empty-list hash, would be read as lane accounting.
func Decode(h common.Hash) (Commitment, error) {
	for _, b := range h[16:] {
		if b != 0 {
			return Commitment{}, fmt.Errorf("%w: reserved bytes set", ErrBadCommitment)
		}
	}
	return Commitment{
		LaneSize:       binary.BigEndian.Uint64(h[0:8]),
		PaymentGasUsed: binary.BigEndian.Uint64(h[8:16]),
	}, nil
}

// CheckHeaderBounds is the whole of section 3.5.4's header-verification duty: the two
// bounds, then the accounting rule on the committed values. Every input is a field of
// the header or of its commitment, so no execution and no state are needed.
//
// The two bounds come first because each is the tighter, more legible error when it is
// the one that fails. Only the first of them can change a verdict: given payment <=
// gasUsed, an oversized quota already violates the rule, so removing the gas-limit bound
// changes which error comes back and nothing else. That is why no mutation of it fails a
// test, and TestTheGasLimitBoundOnlyChangesTheDiagnosis holds the claim to account.
//
// None of the three can reject a block a correct producer made: the payment bucket is a
// sum of pool deltas and so cannot exceed the pool total, LaneSize is clamped below the
// gas limit, and the rule is the same inequality the producer checked before sealing.
// What they buy is the blocks a correct producer did NOT make: without them a header
// carrying absurd values is refused only once the block behind it has been fetched and
// executed - one free execution per forged header.
func (c Commitment) CheckHeaderBounds(gasUsed, gasLimit uint64) error {
	if c.PaymentGasUsed > gasUsed {
		return fmt.Errorf("%w: committed payment %d exceeds header gas used %d",
			ErrUntruthy, c.PaymentGasUsed, gasUsed)
	}
	if c.LaneSize > gasLimit {
		return fmt.Errorf("%w: committed lane size %d exceeds header gas limit %d",
			ErrViolated, c.LaneSize, gasLimit)
	}
	return CheckInequality(gasLimit, gasUsed, c.PaymentGasUsed, c.LaneSize)
}

// CheckInequality is the BEP-703 block validity rule, and the single source of
// the verdict: the producer's pre-seal self-check, the header-verification bound and
// the import-side enforcement all call it, so the three cannot drift apart.
//
// gasUsed must be the block's real total, system-transaction gas included. That is what
// makes the rule complete in one term: general gas never appears explicitly because it
// is gasUsed minus payment, so there is no third bucket to keep in step.
//
// The addition must not overflow. Producer-side inputs are bounded by the gas limit, but
// at header verification both committed values come straight from an attacker, and a
// wrapped sum would land on a small value and therefore *pass*. CheckHeaderBounds bounds
// both before calling in; the carry check is what makes that ordering unnecessary to
// remember.
func CheckInequality(gasLimit, gasUsed, paymentGasUsed, laneSize uint64) error {
	sum, carry := bits.Add64(gasUsed, satSub(laneSize, paymentGasUsed), 0)
	if carry != 0 || sum > gasLimit {
		return fmt.Errorf("%w: gas used %d payment %d lane %d limit %d",
			ErrViolated, gasUsed, paymentGasUsed, laneSize, gasLimit)
	}
	return nil
}

// satSub is saturating subtraction; all lane arithmetic is unsigned.
func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// satAdd is saturating addition.
func satAdd(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return sum
}
