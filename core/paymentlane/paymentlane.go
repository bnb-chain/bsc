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

// Package paymentlane implements the BEP-703 payment lane rules.
//
// The rule is one inequality per block:
//
//	systemGasUsed + generalGasUsed + max(paymentGasUsed, laneSize) <= GasLimit
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
// activation. Recomputing it from chain history is not merely expensive, it needs
// every intervening block's CLASSIFIED generalGasUsed - i.e. full re-execution
// against historical state, not headers alone - so it must be carried in the parent
// header. This package defines how the three committed values pack into 32 bytes
// (Encode/Decode) and still does not name the field, so the rules stay independent of
// it. The carrier chosen for BSC is header.UncleHash, wired in core and core/types; the
// only reason to know that here is that Encode's range must avoid EmptyUncleHash, which
// is why commitVersion exists.
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
// Parlia system transactions are not classified: core.StateProcessor splits them
// out of the user-transaction loop before accounting, they never pass through the
// GasPool, and their gas is added straight to header.GasUsed by Finalize. In the
// inequality they are the separate systemGasUsed term, which is derived
// arithmetic and never a per-transaction label - that is why there is no
// ClassSystem here and why adding one would have nothing to tag.
//
// There are exactly two values. "Not yet classified" is not a state: the caller
// must have settled the class before accounting, so Budget.Account cannot fail.
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
	ErrBucketMismatch   = errors.New("payment lane buckets do not sum to gas used")
	ErrBucketOverflow   = errors.New("payment lane buckets exceed header gas used")
	ErrBadCommitment    = errors.New("payment lane commitment is malformed")
	ErrUntruthy         = errors.New("payment lane commitment does not match replayed buckets")
	ErrCorruptConfig    = errors.New("payment lane config storage layout mismatch")
	ErrQuotaMismatch    = errors.New("payment lane quota does not match the parent derivation")
	ErrStateUnavailable = errors.New("payment lane state unavailable")
)

// commitVersion keeps Encode's range clear of both common.Hash{} and any
// all-zero-plus-sentinel value a carrier might use to mean "unset".
//
// This is a correctness requirement, not an extensibility hook. Commitment{0,0,0} is
// reachable - an empty block whose GasLimit is at or below SystemTxsGasHardLimit, so
// the safety clamp takes the quota to zero - and would otherwise encode to the zero
// hash, which every plausible carrier treats as "the caller never wrote this field".
// The failure mode is a block that seals locally and is rejected network-wide, with
// nothing in the header or body checks able to say why.
const commitVersion byte = 1

// Commitment is the per-block recursion state plus the two class buckets.
//
// Two explicit buckets rather than one bucket plus a derivation: a breathe block's
// validator-set update is the largest system-transaction cost on the chain -
// parlia records 12.16M gas as the maximum observed on mainnet - which is over 20pp
// of a 55M block. Folding that into generalGasUsed would fake a congestion spike,
// and therefore a quota expansion, once a day. systemGasUsed is derived instead,
// via DeriveSystemGas.
type Commitment struct {
	LaneSize       uint64
	GeneralGasUsed uint64
	PaymentGasUsed uint64
}

// Encode packs a Commitment into 32 bytes:
//
//	[0:8]   laneSize        uint64 big-endian
//	[8:16]  generalGasUsed  uint64 big-endian
//	[16:24] paymentGasUsed  uint64 big-endian
//	[24]    version, always commitVersion
//	[25:32] reserved, must be zero
//
// The committed quota is the absolute gas value, not a ratio, so the recursion
// runs in gas space and the committed number is exactly the one the producer used
// to pack. Committing a ratio instead would add a second derived value that must
// agree with the first, with nothing enforcing it.
func Encode(c Commitment) common.Hash {
	var h common.Hash
	binary.BigEndian.PutUint64(h[0:8], c.LaneSize)
	binary.BigEndian.PutUint64(h[8:16], c.GeneralGasUsed)
	binary.BigEndian.PutUint64(h[16:24], c.PaymentGasUsed)
	h[24] = commitVersion
	return h
}

// Decode is the inverse of Encode. It checks the version byte and the reserved
// bytes; without those, an unset carrier field or an unrelated future use of the
// same 32 bytes would be silently read as lane accounting.
func Decode(h common.Hash) (Commitment, error) {
	if h[24] != commitVersion {
		return Commitment{}, fmt.Errorf("%w: version %d", ErrBadCommitment, h[24])
	}
	for _, b := range h[25:32] {
		if b != 0 {
			return Commitment{}, fmt.Errorf("%w: reserved bytes set", ErrBadCommitment)
		}
	}
	return Commitment{
		LaneSize:       binary.BigEndian.Uint64(h[0:8]),
		GeneralGasUsed: binary.BigEndian.Uint64(h[8:16]),
		PaymentGasUsed: binary.BigEndian.Uint64(h[16:24]),
	}, nil
}

// CheckInequality is the BEP-703 block validity rule, and the single source of
// the verdict: the producer's pre-seal self-check, the MEV admission gate and the
// import-side enforcement all call it, so the three cannot drift apart.
//
// The addition must not overflow. Producer-side inputs are bounded by GasLimit,
// but on the MEV admission path both buckets come straight from a builder-
// controlled commitment. A wrapped sum would land on a small value and therefore
// *pass*, which would defeat the gate entirely.
func CheckInequality(gasLimit, systemGasUsed, generalGasUsed, paymentGasUsed, laneSize uint64) error {
	sum, carry := bits.Add64(systemGasUsed, generalGasUsed, 0)
	if carry == 0 {
		sum, carry = bits.Add64(sum, max(paymentGasUsed, laneSize), 0)
	}
	if carry != 0 || sum > gasLimit {
		return fmt.Errorf("%w: system %d general %d payment %d lane %d limit %d",
			ErrViolated, systemGasUsed, generalGasUsed, paymentGasUsed, laneSize, gasLimit)
	}
	return nil
}

// DeriveSystemGas backs systemGasUsed out of header.GasUsed and the two
// committed buckets.
//
// Overflow here is a reachable attack surface, not a theoretical one: with
// general = 2^64-1 and payment = 1 the naive sum wraps to 0, passes a plain
// "general+payment > headerGasUsed" test, and the following subtraction then
// reports systemGasUsed as headerGasUsed itself - three checks bypassed at once.
// Every caller that derives systemGasUsed FROM A COMMITMENT must come through
// here rather than writing its own subtraction. The importer is not such a caller:
// it holds the real system gas and must not use this - see Budget.VerifyCommitment,
// which spells out where each figure comes from.
func DeriveSystemGas(headerGasUsed, generalGasUsed, paymentGasUsed uint64) (uint64, error) {
	sum, carry := bits.Add64(generalGasUsed, paymentGasUsed, 0)
	if carry != 0 || sum > headerGasUsed {
		return 0, fmt.Errorf("%w: general %d payment %d header gas used %d",
			ErrBucketOverflow, generalGasUsed, paymentGasUsed, headerGasUsed)
	}
	return headerGasUsed - sum, nil
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
