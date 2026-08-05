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
// payment = L, spilling the overflow, is bin packing under first fit, and first
// fit is not optimal - it rejects blocks the rule permits. On the MEV path the
// transaction set is builder-given and cannot be reordered, so the difference
// shows up as the whole bid being rejected. TestInvariantAdmissionBeatsStaticPools
// pins the counterexample.
//
// Budget deliberately does not store capacity. The only authority on capacity is
// the GasPool itself: the caller passes gasPool.Gas() in as shared. A second copy
// would be an equality somebody has to maintain, and it would have to be
// initialised even when the lane is off, which reintroduces the enabled-branch
// that the zero value exists to avoid. It also means the temporary
// SubGas(PayBidTxGasLimit) reservation on the bid path is respected for free.
//
// Likewise the producer must never clamp LaneSize. The only quantity available to
// clamp against on that side is the miner-local gasReserved estimate, which the
// validator cannot see: clamping makes IdleLane smaller, general gets
// over-packed, and the block is invalid - while the self-check uses the same
// clamped L and therefore cannot catch it. LaneSize must be exactly what
// LaneSize() returned. When the quota genuinely exceeds the available budget,
// Headroom squeezes general to zero and Verify decides whether the block can be
// produced at all; no divergence anywhere.
type Budget struct {
	LaneSize    uint64 // this block's quota, straight from LaneSize()
	PaymentUsed uint64
	GeneralUsed uint64
}

// IdleLane is the part of the quota that payment traffic has not consumed yet.
// General transactions must yield it; payment transactions need not.
//
// Its meaning comes from this identity, true for all unsigned p and L:
//
//	max(paymentUsed, laneSize) == paymentUsed + IdleLane
//
// Substituted into the rule, the rule reads
//
//	system + general + payment + IdleLane <= GasLimit
//
// so IdleLane is the gas consumption of a virtual transaction: it occupies block
// space and competes for capacity exactly like a real one, produces no fee, and
// exists only to hold the door for payment traffic that has not arrived. The
// max() in the BEP is another way of writing that, and the subtraction in
// Headroom is what makes general transactions share a block with it.
//
// During packing it is "how much of the reservation still binds": every payment
// transaction that actually spends lane gas shrinks it one-for-one, because that
// much has moved from reserved-for-payment to spent-by-payment. At seal time it
// is an upper bound on the capacity this block wasted - an upper bound because
// the loss is only real if some general transaction was waiting for that space.
func (b Budget) IdleLane() uint64 { return satSub(b.LaneSize, b.PaymentUsed) }

// Headroom returns the largest gas limit a transaction of the given class may
// have right now. shared is the shared remainder; the producer passes
// gasPool.Gas().
//
// Correctness (gu/pu are the two buckets, C the pool capacity, L the LaneSize):
//
//	general admitted <=> g <= shared - max(L-pu, 0)
//	payment admitted <=> p <= shared
//
//	case pu_final >= L: every admission required the transaction's limit to be
//	  <= shared, i.e. gu+pu <= C after it; actual <= limit, so
//	  gu + max(pu,L) = gu+pu <= C.
//	case pu_final <  L: pu <= pu_final < L throughout, so each general admission
//	  expands to g <= C-(gu+pu)-(L-pu) = C-gu-L, i.e. gu+L <= C after it; payment
//	  admissions do not change gu, so taking the last general admission gives
//	  gu_final+L <= C.
//
// Both headrooms are monotonically non-increasing (gu, pu and max(pu,L) are all
// non-decreasing), which gives three properties the callers rely on:
//
//	(1) every prefix is a valid block - so the packing loop may be interrupted at
//	    any iteration and hand the partial result to the consensus engine;
//	(2) "does not fit now" == "will never fit" - which is what makes
//	    transactionsByPriceAndNonce.Pop() (permanently dropping that account)
//	    correct;
//	(3) when L + r <= C, where r is any gas reserved outside the two buckets,
//	    shared >= IdleLane holds throughout: general cannot steal lane space, and
//	    the quota is still there when packing ends. The reservation has to be in
//	    the condition because the bid path carves PayBidTxGasLimit out of the same
//	    pool the lane lives in, so C=1000, r=25, L=976 already has
//	    shared=975 < IdleLane=976 with nothing packed. Safety does not depend on
//	    this: general headroom merely saturates to zero, so general still cannot
//	    take lane space - what fails past the bound is only the second half, that
//	    the whole quota survives to the end of packing.
//
// The payment headroom is also never smaller than the general one - it omits a
// single non-negative subtrahend, and the two are equal whenever IdleLane is zero -
// so "neither class can fit TxGas" is exactly "shared < TxGas" and the packing
// loop's termination test needs no change.
func (b Budget) Headroom(shared uint64, class Class) uint64 {
	if class == ClassPayment {
		return shared
	}
	return satSub(shared, b.IdleLane())
}

// Admits reports whether a transaction of this class with this gas limit may be
// included. The predicate is tight: from any REACHABLE state,
// TestAdmissionIsExactlyTight proves it agrees bit for bit with "the block is
// still valid after this transaction burns its full limit", so there are no false
// accepts (invalid blocks) and no false rejects (lost revenue, silently).
//
// Reachable is the necessary qualifier, not a hedge. From an already-invalid state
// both headrooms saturate to zero and a zero-gas transaction would be "admitted";
// that state cannot arise, because every admission preserves validity and
// params.TxGas floors every real transaction, which is exactly why the exhaustive
// test skips invalid pre-states rather than asserting something it cannot prove.
func (b Budget) Admits(shared uint64, class Class, gasLimit uint64) bool {
	return gasLimit <= b.Headroom(shared, class)
}

// Account adds a transaction's gas to its class bucket.
//
// delta must be differenced from gasPool.Used(), never taken from
// receipt.GasUsed and never from gasPool.CumulativeUsed(). All three are
// plausible and only Used() is the quantity that feeds header.GasUsed on both the
// producing and importing sides - which is what lets Verify check
// PaymentUsed+GeneralUsed == Used() as an identity instead of trusting a convention.
// Differencing also makes rollback free: when execution fails the pool has
// already been restored from its snapshot, so the delta is naturally zero and the
// buckets need no separate rollback path.
func (b *Budget) Account(class Class, delta uint64) {
	if class == ClassPayment {
		b.PaymentUsed += delta
		return
	}
	b.GeneralUsed += delta
}

// Verify is the producer's post-packing self-check, using the same verdict source
// as everything else.
//
// The two checks are not equally valuable. Given a systemGasUsed equal to the
// reservation the pool was initialised against, the inequality can only fail when
// the quota itself exceeds this block's available budget, and then declining to
// produce is the only correct response - no valid block exists and the parameters
// have to be fixed by governance. (Pass a larger systemGasUsed than that and it can
// of course fail for the ordinary reason, which is what the reservation-bursts-the-
// block case in the tests covers.) The bucket-sum check is the real target: it
// enforces "every apply was accounted for", and that is a discipline maintained
// by people.
//
// Pass gasReserved as systemGasUsed. That is parlia's own estimate
// (EstimateGasReservedForSystemTxs), and it is an empirical margin - the largest
// values seen on mainnet, times a factor - not a bound anything enforces. So
// "whatever passes here also passes on import" holds with the same confidence as
// that margin, no more. It is not a new failure mode: were the real system gas to
// exceed the reservation, the block would bust GasLimit with or without the lane.
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

// VerifyCommitment is the import-side check and the only authoritative
// enforcement point for the bucket values.
//
// The producer's Verify can only attest to itself, and so can the MEV admission
// gate: a dishonest party can always present a self-consistent pair of fake
// buckets that satisfies the inequality identically. Here the buckets come from
// local replay and are compared against the commitment word for word, so a lying
// block cannot become canonical. Without this check "validated blocks obey the
// rule" simply does not hold, however careful the producer side is.
//
// laneSize is deliberately NOT compared here: CheckLaneSize settles it before
// execution, from the parent alone.
//
// The two gas figures must come from the same place on both sides or honest
// blocks get rejected:
//
//	poolUsed      = gp.Used(), user transactions only. Both sides use the same
//	                GasPool method; the initial values differ (the producer
//	                subtracted gasReserved) but that cancels in the difference.
//	systemGasUsed = header.GasUsed after Finalize minus poolUsed, because Parlia
//	                system transactions bypass the pool and are added by Finalize.
//	                The importer has the real value and does not need - and must
//	                not use - DeriveSystemGas, whose inputs are attacker-controlled.
func (b Budget) VerifyCommitment(gasLimit, systemGasUsed, poolUsed uint64, c Commitment) error {
	if b.GeneralUsed != c.GeneralGasUsed || b.PaymentUsed != c.PaymentGasUsed {
		return fmt.Errorf("%w: committed general %d payment %d, replayed general %d payment %d",
			ErrUntruthy, c.GeneralGasUsed, c.PaymentGasUsed, b.GeneralUsed, b.PaymentUsed)
	}
	return b.Verify(gasLimit, systemGasUsed, poolUsed)
}
