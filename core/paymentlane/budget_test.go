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
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Shared scaffolding: reproduce the producer-side packing loop over a bare Budget.
//
// The real parlia packing path cannot be driven from a unit test (worker_test.go's
// engine switch only knows clique/ethash, and the one parlia test is a t.Skip), so
// the admission algebra and every invariant the packing loop relies on are pinned
// here instead.
// ---------------------------------------------------------------------------

type txSpec struct {
	class  Class
	limit  uint64 // gas limit, what admission is decided on
	actual uint64 // gas actually burned, what is accounted (<= limit, models a refund)
}

// laneRun feeds seq to the admission predicate in order, skipping whatever does
// not fit (the equivalent of txs.Pop()).
//
// reserve models the bid path's SubGas(PayBidTxGasLimit): an external reservation
// that belongs to neither bucket. Every step asserts the four invariants; the
// accepted indices and the final Budget are returned.
func laneRun(t *testing.T, capacity, laneSize, reserve uint64, seq []txSpec) ([]int, Budget) {
	t.Helper()
	b := Budget{LaneSize: laneSize}
	shared := func() uint64 { return satSub(capacity, b.PaymentUsed+b.GeneralUsed+reserve) }

	prev := map[Class]uint64{ClassGeneral: math.MaxUint64, ClassPayment: math.MaxUint64}
	var taken []int
	for i, tx := range seq {
		if !b.Admits(shared(), tx.class, tx.limit) {
			continue
		}
		b.Account(tx.class, tx.actual)
		taken = append(taken, i)

		// (1) the buckets sum to what the pool consumed.
		require.Equalf(t, capacity-reserve-shared(), b.PaymentUsed+b.GeneralUsed,
			"tx %d: buckets disagree with the pool: payment=%d general=%d shared=%d",
			i, b.PaymentUsed, b.GeneralUsed, shared())

		// (2) every prefix is a valid block - commitTransactions may be cut short by
		//     interruptCh on any iteration and hand the partial result to the
		//     consensus engine.
		//
		//     This presupposes the quota itself fits: when L > capacity no valid block
		//     exists at all (not even an empty one), so the invariant is vacuous under
		//     that configuration and the producer's self-check is what catches it.
		if laneSize+reserve <= capacity {
			require.NoErrorf(t, CheckInequality(capacity, 0, b.GeneralUsed, b.PaymentUsed, laneSize),
				"tx %d: prefix is not a valid block", i)
		}

		// (3) both headrooms are monotonically non-increasing - this is what makes
		//     Pop(), which drops the account permanently, correct.
		for _, c := range []Class{ClassGeneral, ClassPayment} {
			h := b.Headroom(shared(), c)
			require.LessOrEqualf(t, h, prev[c],
				"tx %d: headroom for class %s rose %d -> %d, Pop() no longer holds",
				i, c, prev[c], h)
			prev[c] = h
		}

		// (4) while L+reserve <= capacity, general traffic cannot steal lane space.
		if laneSize+reserve <= capacity {
			require.GreaterOrEqualf(t, shared(), b.IdleLane(),
				"tx %d: shared(%d) < IdleLane(%d), lane space was taken over",
				i, shared(), b.IdleLane())
		}
	}
	return taken, b
}

// ---------------------------------------------------------------------------
// Admission algebra
// ---------------------------------------------------------------------------

// TestZeroValueClassIsGeneral pins the constant order in paymentlane.go, which
// nothing else in the suite can see: every other test names its classes, so
// reordering the iota block is invisible to them while silently changing what a
// default-constructed Class means.
//
// The lane's zero-regression property rests on this. A zero Budget plus a zero
// Class has to degrade into the upstream admission predicate for everything, so
// that before activation - and on any path that forgot to classify - there is
// nothing to gate and nothing lands in the payment bucket.
func TestZeroValueClassIsGeneral(t *testing.T) {
	var unset Class
	require.Equal(t, ClassGeneral, unset, "the zero Class must be ClassGeneral")
	require.Equal(t, "general", unset.String())

	// Unclassified gas must land in the general bucket, never in the payment one:
	// crediting payment would shrink IdleLane, widen the general headroom, and let
	// the producer overpack a block the validator then rejects.
	var b Budget
	b.Account(unset, 500)
	require.Equal(t, uint64(500), b.GeneralUsed)
	require.Equal(t, uint64(0), b.PaymentUsed)

	// A zero Budget is the upstream predicate for both classes.
	require.Equal(t, uint64(1000), b.Headroom(1000, unset))
	require.Equal(t, uint64(1000), b.Headroom(1000, ClassPayment))

	// And with a quota present, the zero class is the one that has to yield it.
	withLane := Budget{LaneSize: 300}
	require.Equal(t, uint64(700), withLane.Headroom(1000, unset))
}

// TestAdmissionInvariants is where the whole safety argument for the packing loop
// lands: random sequences, asserted step by step.
func TestAdmissionInvariants(t *testing.T) {
	const capacity = 1000
	for seed := int64(0); seed < 3000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		// Covers both degenerate endpoints of laneSize, 0 and capacity.
		laneSize := uint64(rng.Intn(capacity + 1))
		seq := make([]txSpec, 200)
		for i := range seq {
			limit := uint64(1 + rng.Intn(300))
			seq[i] = txSpec{
				class:  Class(rng.Intn(2)),
				limit:  limit,
				actual: uint64(rng.Intn(int(limit) + 1)),
			}
		}
		laneRun(t, capacity, laneSize, 0, seq)
	}
}

// TestAdmissionIsExactlyTight proves exhaustively that Admits agrees bit for bit
// with "the block is still valid after this transaction burns its full gas limit".
//
// TestAdmissionInvariants only rules out false accepts (which would produce invalid
// blocks); what this adds is the absence of false rejects - and a false reject
// raises no error at all, it only shows up as validator revenue quietly going
// missing, which is the harder half to notice.
func TestAdmissionIsExactlyTight(t *testing.T) {
	const capacity = 40
	for laneSize := uint64(0); laneSize <= capacity; laneSize += 7 {
		for pu := uint64(0); pu <= capacity; pu += 3 {
			for gu := uint64(0); gu+pu <= capacity; gu += 3 {
				b := Budget{LaneSize: laneSize, PaymentUsed: pu, GeneralUsed: gu}
				// Only reachable states are enumerated: asking whether the predicate
				// is exact makes no sense from a state that is already invalid. With
				// L=7, for instance, gu=36 is simply unreachable - general admission
				// pins gu at C-L=33.
				if CheckInequality(capacity, 0, gu, pu, laneSize) != nil {
					continue
				}
				shared := capacity - pu - gu
				for _, class := range []Class{ClassGeneral, ClassPayment} {
					for g := uint64(0); g <= capacity; g++ {
						after := b
						after.Account(class, g)
						legal := CheckInequality(capacity, 0,
							after.GeneralUsed, after.PaymentUsed, laneSize) == nil
						if got := b.Admits(shared, class, g); got != legal {
							t.Fatalf("L=%d pu=%d gu=%d class=%s g=%d: Admits=%v but valid-after-full-burn=%v",
								laneSize, pu, gu, class, g, got, legal)
						}
					}
				}
			}
		}
	}
}

// TestGeneralHeadroomFlatBelowLane pins how "the quota is a floor" shows up in the
// admission algebra: payment growth inside the quota does not squeeze general at
// all, and only past the quota do the two classes compete gas for gas.
func TestGeneralHeadroomFlatBelowLane(t *testing.T) {
	const capacity, laneSize = 1000, 300
	for _, pu := range []uint64{0, 1, laneSize / 2, laneSize - 1, laneSize} {
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, uint64(capacity-laneSize), b.Headroom(capacity-pu, ClassGeneral),
			"pu=%d (inside the quota): general headroom must be constant", pu)
	}
	// Past the quota, every extra gas payment burns takes one gas from general.
	for _, over := range []uint64{1, 2, 100} {
		pu := uint64(laneSize) + over
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, capacity-pu, b.Headroom(capacity-pu, ClassGeneral),
			"pu=L+%d: general headroom", over)
	}
}

// TestIdleLaneBoundaries covers the endpoints of IdleLane, in particular
// IdleLane > shared: there the saturating subtraction has to floor the headroom at
// 0, whereas a bare subtraction underflows to somewhere near 2^64 and the predicate
// stops meaning anything.
func TestIdleLaneBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		laneSize, pu, shared  uint64
		wantIdle, wantGeneral uint64
	}{
		{"quota exactly filled, general gets the whole shared remainder back", 100, 100, 500, 0, 500},
		{"quota one gas short of full", 100, 99, 500, 1, 499},
		{"overfilling the quota does not wrap IdleLane", 100, 150, 500, 0, 500},
		{"IdleLane exactly equals the shared remainder, no general tx gets in", 100, 0, 100, 100, 0},
		{"IdleLane exceeds the shared remainder, saturates to 0", 900, 0, 100, 900, 0},
		{"zero quota, the lane degenerates", 0, 0, 500, 0, 500},
	} {
		b := Budget{LaneSize: tc.laneSize, PaymentUsed: tc.pu}
		require.Equalf(t, tc.wantIdle, b.IdleLane(), "%s: IdleLane", tc.name)
		require.Equalf(t, tc.wantGeneral, b.Headroom(tc.shared, ClassGeneral),
			"%s: general headroom", tc.name)
		require.Equalf(t, tc.shared, b.Headroom(tc.shared, ClassPayment),
			"%s: payment headroom must equal the shared remainder", tc.name)
	}
}

// TestPaymentPredicateIsTheLooserOne guards the producer-side loop termination test
// that was deliberately left UNCHANGED.
//
// worker.go keeps upstream's `gasPool.Gas() < params.TxGas`, whose correctness rests
// on Headroom(general) <= Headroom(payment) holding always - that is what makes
// "neither class can fit TxGas" the same statement as "the shared remainder is
// below TxGas". Break this and the termination test has to change with it.
func TestPaymentPredicateIsTheLooserOne(t *testing.T) {
	for laneSize := uint64(0); laneSize <= 200; laneSize += 13 {
		for pu := uint64(0); pu <= 200; pu += 11 {
			for shared := uint64(0); shared <= 200; shared += 7 {
				b := Budget{LaneSize: laneSize, PaymentUsed: pu}
				if b.Headroom(shared, ClassGeneral) > b.Headroom(shared, ClassPayment) {
					t.Fatalf("L=%d pu=%d shared=%d: general headroom is the wider one",
						laneSize, pu, shared)
				}
			}
		}
	}
}

// TestLaneSizeExceedsCapacity covers "the quota is larger than this block's
// available budget".
//
// The producer deliberately does not clamp: the only quantity available to clamp
// against there is the miner-local gasReserved, which the validator cannot see, so
// clamping is a consensus divergence. Two things therefore have to hold - general
// is squeezed out entirely, and the self-check can tell whether this block can be
// produced at all.
func TestLaneSizeExceedsCapacity(t *testing.T) {
	const capacity = 1000
	taken, b := laneRun(t, capacity, capacity+1, 0, []txSpec{
		{ClassGeneral, 1, 1},
		{ClassPayment, 500, 500},
		{ClassGeneral, 1, 1},
	})
	require.Equal(t, []int{1}, taken, "only the payment transaction should have been admitted")

	// The self-check treats capacity as the gasLimit: a quota that does not fit means
	// refusing to produce, i.e. giving up the slot rather than sealing a bad block.
	require.ErrorIs(t, b.Verify(capacity, 0, b.PaymentUsed+b.GeneralUsed), ErrViolated,
		"a quota larger than capacity must make the self-check report ErrViolated")

	// A quota exactly equal to capacity: the empty block is still valid.
	require.NoError(t, (Budget{LaneSize: capacity}).Verify(capacity, 0, 0),
		"with the quota exactly equal to capacity the empty block must be valid")
}

// TestPayBidTxAlwaysFitsAfterLaneAdmission pins the algebraic closure of the bid
// path: the SubGas(PayBidTxGasLimit) reservation held during the loop guarantees
// payBidTx still fits once AddGas gives it back.
//
// Note that payBidTx is NOT special-cased any more - the classifier decides (see the
// argument in bid_simulator.go: the validator cannot recognise it, so a miner that
// unilaterally calls it general makes the two sides' buckets disagree). In reality
// it therefore classifies as payment and fits unconditionally. What is asserted here
// is the stronger general case: should a future BEP exclusion clause reclassify it
// as general, the threshold below is the precondition the quota has to satisfy.
func TestPayBidTxAlwaysFitsAfterLaneAdmission(t *testing.T) {
	const capacity, payBidTxGas = 1000, 25
	for seed := int64(0); seed < 2000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		// The quota must leave room for payBidTx itself; see the threshold assertion
		// below.
		laneSize := uint64(rng.Intn(capacity - payBidTxGas + 1))
		seq := make([]txSpec, 100)
		for i := range seq {
			limit := uint64(1 + rng.Intn(200))
			seq[i] = txSpec{Class(rng.Intn(2)), limit, uint64(rng.Intn(int(limit) + 1))}
		}
		_, b := laneRun(t, capacity, laneSize, payBidTxGas, seq)

		// After AddGas returns the reservation.
		shared := capacity - b.PaymentUsed - b.GeneralUsed
		require.GreaterOrEqualf(t, b.Headroom(shared, ClassGeneral), uint64(payBidTxGas),
			"seed %d: payBidTx no longer fits at L=%d", seed, laneSize)
	}
	// The threshold sits exactly at capacity - payBidTxGas.
	b := Budget{LaneSize: capacity - payBidTxGas + 1}
	require.Less(t, b.Headroom(capacity, ClassGeneral), uint64(payBidTxGas),
		"past a quota of capacity-payBidTxGas, payBidTx is supposed to stop fitting")
}

// TestPackingIsOrderSensitive records a fact: the same set of transactions packs to
// a different total depending on arrival order. That is not a bug (bid order is
// given by the builder, local order by the tip sort), but any reasoning of the form
// "order does not matter so it may be computed in another order" is wrong.
func TestPackingIsOrderSensitive(t *testing.T) {
	const capacity, laneSize = 100, 50
	g := txSpec{ClassGeneral, 50, 50}
	p := txSpec{ClassPayment, 60, 60}

	_, paymentFirst := laneRun(t, capacity, laneSize, 0, []txSpec{p, g})
	_, generalFirst := laneRun(t, capacity, laneSize, 0, []txSpec{g, p})

	require.Equal(t, uint64(60), paymentFirst.PaymentUsed+paymentFirst.GeneralUsed,
		"payment first: total packed gas")
	require.Equal(t, uint64(50), generalFirst.PaymentUsed+generalFirst.GeneralUsed,
		"general first: total packed gas")
}

// TestInvariantAdmissionBeatsStaticPools pins the counterexample behind choosing
// "one pool plus an inequality predicate" over "two gas pools": a static split is
// bin packing under first fit, and first fit rejects blocks the rule permits.
func TestInvariantAdmissionBeatsStaticPools(t *testing.T) {
	const capacity, laneSize = 200, 100
	seq := []txSpec{
		{ClassPayment, 60, 60}, {ClassPayment, 50, 50},
		{ClassPayment, 50, 50}, {ClassGeneral, 40, 40},
	} // sums to 200, exactly the capacity

	// Two-pool greedy: 60 -> payment (40 left), 50 does not fit payment -> general
	// (50 left), 50 -> general (0 left), 40 has nowhere to go -> fails.
	paymentPool, generalPool := uint64(laneSize), uint64(capacity-laneSize)
	staticOK := true
	for _, tx := range seq {
		switch {
		case tx.class == ClassPayment && paymentPool >= tx.limit:
			paymentPool -= tx.limit
		case generalPool >= tx.limit:
			generalPool -= tx.limit
		default:
			staticOK = false
		}
	}
	require.False(t, staticOK,
		"the counterexample no longer bites: two-pool greedy accepted this sequence, rebuild it")

	taken, _ := laneRun(t, capacity, laneSize, 0, seq)
	require.Len(t, taken, len(seq),
		"inequality admission should take all of them, it only took %v", taken)
}

// ---------------------------------------------------------------------------
// The rule itself
// ---------------------------------------------------------------------------

// TestLaneIsFloorNotCeiling covers the boundary between the two regimes of §3.3,
// one case per clause of the rule text.
func TestLaneIsFloorNotCeiling(t *testing.T) {
	const limit, lane = 100, 20
	for _, tc := range []struct {
		name             string
		general, payment uint64
		wantErr          bool
	}{
		{"payment exactly fills the quota (the three terms sum to exactly GasLimit)", 80, 20, false},
		{"payment one gas over, general one gas under", 79, 21, false},
		{"payment one gas short does not hand the freed quota to general", 81, 19, true},
		{"with no payment demand the quota idles", 80, 0, false},
		{"general does not get the idling quota", 81, 0, true},
		{"the quota is a floor, not a ceiling: payment may take the whole block", 0, 100, false},
	} {
		err := CheckInequality(limit, 0, tc.general, tc.payment, lane)
		if tc.wantErr {
			require.ErrorIsf(t, err, ErrViolated, "%s: expected ErrViolated", tc.name)
		} else {
			require.NoErrorf(t, err, "%s: expected a valid block", tc.name)
		}
	}
}

// TestOverflowIsNotAWayIn guards the overflow attack surface of the bid admission
// gate.
//
// A bid block's two buckets come straight out of 32 header bytes and are fully
// builder-controlled. Naive addition lets a pair like general=2^64-1 / payment=1
// wrap back to a small value and therefore *pass* the check, which retires the whole
// "reject a rule-violating bid before signing" gate and hands the attacker back the
// ability to make a chosen validator lose a slot at zero cost.
func TestOverflowIsNotAWayIn(t *testing.T) {
	const gasLimit = 70_000_000
	maxU := uint64(math.MaxUint64)

	for _, tc := range []struct{ system, general, payment uint64 }{
		{maxU, 1, 0},
		{1, maxU, 0},
		{gasLimit, maxU, 1},
		{0, maxU/2 + 1, maxU/2 + 1},
	} {
		require.ErrorIsf(t, CheckInequality(gasLimit, tc.system, tc.general, tc.payment, 0), ErrViolated,
			"CheckInequality(system=%d general=%d payment=%d) must be a violation",
			tc.system, tc.general, tc.payment)
	}

	for _, tc := range []struct{ general, payment uint64 }{
		{maxU, 1},
		{maxU/2 + 1, maxU/2 + 1},
		{gasLimit + 1, 0},
	} {
		_, err := DeriveSystemGas(gasLimit, tc.general, tc.payment)
		require.ErrorIsf(t, err, ErrBucketOverflow,
			"DeriveSystemGas(general=%d payment=%d) must be a bucket overflow",
			tc.general, tc.payment)
	}

	// The ordinary path must still back out the right system gas.
	got, err := DeriveSystemGas(1000, 600, 300)
	require.NoError(t, err)
	require.Equal(t, uint64(100), got)
}

// TestVerifyFailureTriggers fixes which accounting mistake maps to which error.
//
// The bucket-sum half is the valuable one and nothing else in this file exercises
// it: the admission tests keep the buckets and the pool in step by construction, so
// they can never make it fire. What it enforces is "every apply was accounted for",
// a discipline maintained by people rather than by the type system, and when it does
// trip on the producer side this single error is all the log will contain - so it
// has to point at the cause.
func TestVerifyFailureTriggers(t *testing.T) {
	for _, tc := range []struct {
		name             string
		b                Budget
		poolUsed, system uint64
		wantErr          error
	}{
		{"consistent and valid", Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 60}, 80, 0, nil},
		{"buckets below the pool: an apply went unaccounted", Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 50}, 80, 0, ErrBucketMismatch},
		{"buckets above the pool: an external reservation was charged to a transaction", Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 70}, 80, 0, ErrBucketMismatch},
		{"accounting consistent but the quota does not fit this block", Budget{LaneSize: 200}, 0, 0, ErrViolated},
		{"the system-transaction reservation bursts the block", Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 60}, 80, 21, ErrViolated},
		{"when both fail, the bucket sum is reported first (it is the one naming the cause)", Budget{LaneSize: 200, PaymentUsed: 5}, 99, 0, ErrBucketMismatch},
	} {
		err := tc.b.Verify(100, tc.system, tc.poolUsed)
		if tc.wantErr == nil {
			require.NoErrorf(t, err, "%s: expected to pass", tc.name)
		} else {
			require.ErrorIsf(t, err, tc.wantErr, "%s", tc.name)
		}
	}
}

// TestVerifyCommitmentComparesBothBuckets closes what was the largest hole in this
// suite: VerifyCommitment - the function the package calls the only authoritative
// enforcement point for the bucket values - had no test caller at all, so it could
// be gutted entirely with a green run.
func TestVerifyCommitmentComparesBothBuckets(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 60}
	const gasLimit, system, pool = 100, 0, 80
	truthful := Commitment{LaneSize: 20, GeneralGasUsed: 60, PaymentGasUsed: 20}

	require.NoError(t, b.VerifyCommitment(gasLimit, system, pool, truthful))

	for _, tc := range []struct {
		name string
		lie  Commitment
	}{
		{"general understated", Commitment{LaneSize: 20, GeneralGasUsed: 59, PaymentGasUsed: 20}},
		{"general overstated", Commitment{LaneSize: 20, GeneralGasUsed: 61, PaymentGasUsed: 20}},
		{"payment understated", Commitment{LaneSize: 20, GeneralGasUsed: 60, PaymentGasUsed: 19}},
		{"payment overstated", Commitment{LaneSize: 20, GeneralGasUsed: 60, PaymentGasUsed: 21}},
		{"buckets swapped", Commitment{LaneSize: 20, GeneralGasUsed: 20, PaymentGasUsed: 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := b.VerifyCommitment(gasLimit, system, pool, tc.lie)
			require.ErrorIs(t, err, ErrUntruthy)
		})
	}
}

// TestVerifyCommitmentIgnoresLaneSize records a deliberate division of labour, so
// nobody "fixes" it by adding the comparison here.
//
// The quota is a pure function of the parent header and the parent post-state, so it
// is settled by CheckLaneSize before any transaction executes - including on the MEV
// admission path, before a validator signs. Replay cannot adjudicate it, and a
// committed quota that disagrees is not a bucket problem.
func TestVerifyCommitmentIgnoresLaneSize(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20, GeneralUsed: 60}
	absurd := Commitment{LaneSize: 999_999, GeneralGasUsed: 60, PaymentGasUsed: 20}
	require.NoError(t, b.VerifyCommitment(100, 0, 80, absurd),
		"VerifyCommitment must not police LaneSize; CheckLaneSize does")

	// And the check that does police it rejects exactly that.
	p, s := defaultParams(), Signal{}
	require.ErrorIs(t, CheckLaneSize(absurd.LaneSize, p, s, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, CheckLaneSize(LaneSize(p, s, 55_000_000), p, s, 55_000_000))
}

// TestVerifyCommitmentStillEnforcesTheRule: agreeing with a lie is not enough, the
// agreed-upon numbers must also satisfy the inequality.
func TestVerifyCommitmentStillEnforcesTheRule(t *testing.T) {
	b := Budget{LaneSize: 90, PaymentUsed: 10, GeneralUsed: 60}
	c := Commitment{LaneSize: 90, GeneralGasUsed: 60, PaymentGasUsed: 10}
	// system 0 + general 60 + max(10, 90) = 150 > 100.
	require.ErrorIs(t, b.VerifyCommitment(100, 0, 70, c), ErrViolated)
}
