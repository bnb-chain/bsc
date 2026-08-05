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
	"math/bits"
)

// Budget carries this block's quota and the two class buckets, and expresses the
// block validity rule as an admission predicate.
//
// Why not two gas pools: splitting the budget statically into general = C-L and
// payment = L, spilling the overflow, is bin packing under first fit, which
// rejects blocks the rule permits - and on the MEV path the transaction set is
// builder-given and cannot be reordered, so that shows up as the whole bid being
// rejected. TestInvariantAdmissionBeatsStaticPools pins the counterexample.
//
// Capacity is deliberately not stored: the caller passes gasPool.Gas() in as
// shared, so the GasPool stays the only authority and the bid path's temporary
// SubGas(PayBidTxGasLimit) reservation is respected for free.
type Budget struct {
	LaneSize    uint64 // this block's quota, straight from LaneSize()
	PaymentUsed uint64
	GeneralUsed uint64
}

// IdleLane is the part of the quota that payment traffic has not consumed yet.
// General transactions must yield it; payment transactions need not.
//
// It is what turns the BEP's max() into a sum, for all unsigned p and L:
//
//	max(paymentUsed, laneSize) == paymentUsed + IdleLane
//
// so the rule reads system + general + payment + IdleLane <= GasLimit and IdleLane
// behaves like a fee-less transaction holding the door for payment traffic that has
// not arrived. At seal time it is an upper bound on the capacity this block wasted.
func (b Budget) IdleLane() uint64 { return satSub(b.LaneSize, b.PaymentUsed) }

// Headroom returns the largest gas limit a transaction of the given class may
// have right now. shared is the shared remainder; the producer passes
// gasPool.Gas().
//
// With gu/pu the two buckets, C the pool capacity and L the LaneSize:
//
//	general admitted <=> g <= shared - max(L-pu, 0)
//	payment admitted <=> p <= shared
//
// Both are monotonically non-increasing, which is what the callers rely on:
// every prefix is a valid block, so the packing loop may be interrupted at any
// iteration; and "does not fit now" == "will never fit", which is what makes
// transactionsByPriceAndNonce.Pop() (permanently dropping that account) correct.
// TestAdmissionIsExactlyTight proves the predicate agrees bit for bit with "the
// block is still valid after this transaction burns its full limit".
//
// One bound is worth naming: shared >= IdleLane holds throughout only while
// L + r <= C, where r is gas reserved outside the two buckets - the bid path carves
// PayBidTxGasLimit out of the same pool, so C=1000, r=25, L=976 already has
// shared=975 < IdleLane=976 with nothing packed. Safety does not depend on it:
// general headroom merely saturates to zero, so general still cannot take lane
// space. What fails past the bound is only that the whole quota survives to the end
// of packing.
//
// Payment headroom is never smaller than general - it omits a non-negative
// subtrahend - so "neither class can fit TxGas" is exactly "shared < TxGas" and the
// packing loop's termination test needs no change.
func (b Budget) Headroom(shared uint64, class Class) uint64 {
	if class == ClassPayment {
		return shared
	}
	return satSub(shared, b.IdleLane())
}

// Admits reports whether a transaction of this class with this gas limit may be
// included: no false accepts (invalid blocks) and no false rejects (lost revenue).
//
// Only from a REACHABLE state, and that qualifier is not a hedge: from an
// already-invalid state both headrooms saturate to zero and a zero-gas transaction
// would be "admitted". Unreachable, because every admission preserves validity and
// params.TxGas floors every real transaction - which is why the exhaustive test
// skips invalid pre-states instead of asserting something it cannot prove.
func (b Budget) Admits(shared uint64, class Class, gasLimit uint64) bool {
	return gasLimit <= b.Headroom(shared, class)
}

// Account adds a transaction's gas to its class bucket. delta must be differenced
// from gasPool.Used(); core.LaneState.AccountFrom is the only correct caller and
// says why there.
func (b *Budget) Account(class Class, delta uint64) {
	if class == ClassPayment {
		b.PaymentUsed += delta
		return
	}
	b.GeneralUsed += delta
}

// Verify is the producer's post-packing self-check.
//
// The bucket-sum check is the valuable half: it enforces "every apply was accounted
// for", a discipline maintained by people. The inequality mostly restates what
// admission already guaranteed.
//
// Pass the REAL system gas - header.GasUsed after Finalize minus poolUsed - and not
// parlia's EstimateGasReservedForSystemTxs. The quota is clamped to fit the reserved
// pool, so against the estimate the inequality is unfailable; against the real figure
// it also catches system gas overrunning the reservation, which the lane newly makes
// reachable at high fill and which parlia's own GasLimit < GasUsed guard cannot see,
// having no idle-lane term.
func (b Budget) Verify(gasLimit, systemGasUsed, poolUsed uint64) error {
	// bits.Add64 rather than a plain sum, to match the rest of the package. A
	// wrapped pair is unreachable here - both buckets are accumulated from pool
	// deltas - but with a plain sum it could compare equal to poolUsed, pass this
	// check, and then be reported as ErrViolated by CheckInequality, pointing the
	// reader at the inequality when the real defect is the accounting.
	sum, carry := bits.Add64(b.PaymentUsed, b.GeneralUsed, 0)
	if carry != 0 || sum != poolUsed {
		return fmt.Errorf("%w: payment %d general %d pool %d",
			ErrBucketMismatch, b.PaymentUsed, b.GeneralUsed, poolUsed)
	}
	return CheckInequality(gasLimit, systemGasUsed, b.GeneralUsed, b.PaymentUsed, b.LaneSize)
}

// VerifyCommitment is the import-side check and the only authoritative enforcement
// point for the bucket values: the producer's Verify and the MEV admission gate can
// each only attest to themselves, since a dishonest party is free to present a
// self-consistent pair of fake buckets that satisfies the inequality identically.
// Here the buckets come from local replay and are compared word for word, so without
// this check "validated blocks obey the rule" does not hold at all.
//
// laneSize is deliberately NOT compared here: CheckLaneSize settles it before
// execution, from the parent alone.
//
// Both gas figures must come from the same place on both sides or honest blocks get
// rejected: poolUsed is gp.Used() (user transactions only - the two sides start the
// pool at different values but that cancels in the difference), and systemGasUsed is
// header.GasUsed after Finalize minus poolUsed, because Parlia system transactions
// bypass the pool. The importer holds the real value and must not reach for
// DeriveSystemGas, whose inputs are attacker-controlled.
func (b Budget) VerifyCommitment(gasLimit, systemGasUsed, poolUsed uint64, c Commitment) error {
	if b.GeneralUsed != c.GeneralGasUsed || b.PaymentUsed != c.PaymentGasUsed {
		return fmt.Errorf("%w: committed general %d payment %d, replayed general %d payment %d",
			ErrUntruthy, c.GeneralGasUsed, c.PaymentGasUsed, b.GeneralUsed, b.PaymentUsed)
	}
	return b.Verify(gasLimit, systemGasUsed, poolUsed)
}
