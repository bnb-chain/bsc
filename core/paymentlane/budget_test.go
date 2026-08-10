package paymentlane

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Shared scaffolding: the producer's packing loop over a bare Budget. The real parlia
// path cannot be driven from a unit test (worker_test.go's engine switch only knows
// clique/ethash), so the admission algebra is pinned here instead.
// ---------------------------------------------------------------------------

type txSpec struct {
	class  Class
	limit  uint64 // gas limit, what admission is decided on
	actual uint64 // gas actually burned, what is accounted (<= limit, models a refund)
}

// laneRun feeds seq to the admission predicate in order, skipping whatever does not fit
// (the equivalent of txs.Pop()), and asserts the four invariants at every step. It returns
// the accepted indices, the final Budget and the pool total.
//
// reserve models the bid path's SubGas(PayBidTxGasLimit), a reservation belonging to
// neither class. General gas is tracked here because Budget does not carry it - on the real
// paths it is header.GasUsed less payment.
func laneRun(t *testing.T, capacity, laneSize, reserve uint64, seq []txSpec) ([]int, Budget, uint64) {
	t.Helper()
	b := Budget{LaneSize: laneSize}
	var generalUsed uint64
	poolUsed := func() uint64 { return b.PaymentUsed + generalUsed }
	shared := func() uint64 { return satSub(capacity, poolUsed()+reserve) }

	prev := map[Class]uint64{ClassGeneral: math.MaxUint64, ClassPayment: math.MaxUint64}
	var taken []int
	for i, tx := range seq {
		if !b.Admits(shared(), tx.class, tx.limit) {
			continue
		}
		b.RecordUsed(tx.class, tx.actual)
		if tx.class == ClassGeneral {
			generalUsed += tx.actual
		}
		taken = append(taken, i)

		// (1) the accounted gas agrees with what the pool consumed.
		require.Equalf(t, capacity-reserve-shared(), poolUsed(),
			"tx %d: accounting disagrees with the pool: payment=%d general=%d shared=%d",
			i, b.PaymentUsed, generalUsed, shared())

		// (2) every prefix is a valid block - interruptCh may cut commitTransactions short
		//     on any iteration and hand the partial result to the engine. Guarded because
		//     a quota that does not fit has no valid block at all, not even an empty one;
		//     the producer's self-check is what catches that.
		if laneSize+reserve <= capacity {
			require.NoErrorf(t, CheckInequality(capacity, poolUsed(), b.PaymentUsed, laneSize),
				"tx %d: prefix is not a valid block", i)
		}

		// (3) both bounds are monotonically non-increasing - this is what makes
		//     Pop(), which drops the account permanently, correct.
		for _, c := range []Class{ClassGeneral, ClassPayment} {
			m := b.MaxAvailableGas(shared(), c)
			require.LessOrEqualf(t, m, prev[c],
				"tx %d: MaxAvailableGas for class %s rose %d -> %d, Pop() no longer holds",
				i, c, prev[c], m)
			prev[c] = m
		}

		// (4) while L+reserve <= capacity, general traffic cannot steal lane space.
		if laneSize+reserve <= capacity {
			require.GreaterOrEqualf(t, shared(), b.IdleLane(),
				"tx %d: shared(%d) < IdleLane(%d), lane space was taken over",
				i, shared(), b.IdleLane())
		}
	}
	return taken, b, poolUsed()
}

// ---------------------------------------------------------------------------
// Admission algebra
// ---------------------------------------------------------------------------

// TestZeroValueClassIsGeneral pins the iota order in paymentlane.go, which nothing else
// in the suite can see: every other test names its classes. The zero-regression property
// rests on it - before activation, and on any path that forgot to classify, a zero Budget
// and a zero Class must degrade into the upstream predicate with nothing booked as payment.
func TestZeroValueClassIsGeneral(t *testing.T) {
	var unset Class
	require.Equal(t, ClassGeneral, unset, "the zero Class must be ClassGeneral")
	require.Equal(t, "general", unset.String())

	// Unclassified gas must not land in PaymentUsed: that would shrink IdleLane, widen
	// general's MaxAvailableGas and let the producer overpack a block the validator rejects.
	var b Budget
	b.RecordUsed(unset, 500)
	require.Equal(t, uint64(0), b.PaymentUsed, "unclassified gas must not be booked as payment")
	require.Equal(t, uint64(0), b.IdleLane(), "and it must not move the quota either")

	// A zero Budget is the upstream predicate for both classes.
	require.Equal(t, uint64(1000), b.MaxAvailableGas(1000, unset))
	require.Equal(t, uint64(1000), b.MaxAvailableGas(1000, ClassPayment))

	// And with a quota present, the zero class is the one that has to yield it.
	withLane := Budget{LaneSize: 300}
	require.Equal(t, uint64(700), withLane.MaxAvailableGas(1000, unset))
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

// TestAdmissionIsExactlyTight proves exhaustively that Admits agrees bit for bit with "the
// block is still valid after this transaction burns its full gas limit". TestAdmissionInvariants
// rules out only false accepts; what this adds is the absence of false rejects, which raise no
// error at all and show up merely as validator revenue quietly going missing.
func TestAdmissionIsExactlyTight(t *testing.T) {
	const capacity = 40
	for laneSize := uint64(0); laneSize <= capacity; laneSize += 7 {
		for pu := uint64(0); pu <= capacity; pu += 3 {
			for gu := uint64(0); gu+pu <= capacity; gu += 3 {
				b := Budget{LaneSize: laneSize, PaymentUsed: pu}
				// Reachable states only: exactness makes no sense from an already
				// invalid state. At L=7, say, gu=36 cannot be reached - general
				// admission pins gu at C-L=33.
				if CheckInequality(capacity, gu+pu, pu, laneSize) != nil {
					continue
				}
				shared := capacity - pu - gu
				for _, class := range []Class{ClassGeneral, ClassPayment} {
					for g := uint64(0); g <= capacity; g++ {
						after, afterGeneral := b, gu
						after.RecordUsed(class, g)
						if class == ClassGeneral {
							afterGeneral += g
						}
						legal := CheckInequality(capacity,
							afterGeneral+after.PaymentUsed, after.PaymentUsed, laneSize) == nil
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

// TestGeneralMaxAvailableGasFlatBelowLane pins how "the quota is a floor" shows up in
// the admission algebra: payment growth inside the quota does not squeeze general at
// all, and only past the quota do the two classes compete gas for gas.
func TestGeneralMaxAvailableGasFlatBelowLane(t *testing.T) {
	const capacity, laneSize = 1000, 300
	for _, pu := range []uint64{0, 1, laneSize / 2, laneSize - 1, laneSize} {
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, uint64(capacity-laneSize), b.MaxAvailableGas(capacity-pu, ClassGeneral),
			"pu=%d (inside the quota): general's MaxAvailableGas must be constant", pu)
	}
	// Past the quota, every extra gas payment burns takes one gas from general.
	for _, over := range []uint64{1, 2, 100} {
		pu := uint64(laneSize) + over
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, capacity-pu, b.MaxAvailableGas(capacity-pu, ClassGeneral),
			"pu=L+%d: general's MaxAvailableGas", over)
	}
}

// TestIdleLaneBoundaries covers the endpoints of IdleLane, above all IdleLane > shared:
// there the saturating subtraction must floor the bound at 0, where a bare subtraction
// would underflow to near 2^64 and the predicate would stop meaning anything.
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
		require.Equalf(t, tc.wantGeneral, b.MaxAvailableGas(tc.shared, ClassGeneral),
			"%s: general's MaxAvailableGas", tc.name)
		require.Equalf(t, tc.shared, b.MaxAvailableGas(tc.shared, ClassPayment),
			"%s: payment's MaxAvailableGas must equal the shared remainder", tc.name)
	}
}

// TestPaymentPredicateIsTheLooserOne guards worker.go's untouched termination test,
// upstream's `gasPool.Gas() < params.TxGas`. It is only equivalent to "neither class can
// fit TxGas" while MaxAvailableGas(general) <= MaxAvailableGas(payment) holds always; break
// that and the termination test has to change with it.
func TestPaymentPredicateIsTheLooserOne(t *testing.T) {
	for laneSize := uint64(0); laneSize <= 200; laneSize += 13 {
		for pu := uint64(0); pu <= 200; pu += 11 {
			for shared := uint64(0); shared <= 200; shared += 7 {
				b := Budget{LaneSize: laneSize, PaymentUsed: pu}
				if b.MaxAvailableGas(shared, ClassGeneral) > b.MaxAvailableGas(shared, ClassPayment) {
					t.Fatalf("L=%d pu=%d shared=%d: general's MaxAvailableGas is the wider one",
						laneSize, pu, shared)
				}
			}
		}
	}
}

// TestLaneSizeExceedsCapacity covers a quota larger than this block's budget. The producer
// must not clamp - the only quantity to clamp against is the miner-local gasReserved, which
// the validator cannot see - so two things have to hold instead: general is squeezed out
// entirely, and the self-check can tell that this block cannot be produced.
func TestLaneSizeExceedsCapacity(t *testing.T) {
	const capacity = 1000
	taken, b, poolUsed := laneRun(t, capacity, capacity+1, 0, []txSpec{
		{ClassGeneral, 1, 1},
		{ClassPayment, 500, 500},
		{ClassGeneral, 1, 1},
	})
	require.Equal(t, []int{1}, taken, "only the payment transaction should have been admitted")

	// The self-check treats capacity as the gasLimit: a quota that does not fit means
	// refusing to produce, i.e. giving up the slot rather than sealing a bad block.
	require.ErrorIs(t, b.Verify(capacity, poolUsed, poolUsed), ErrViolated,
		"a quota larger than capacity must make the self-check report ErrViolated")

	// A quota exactly equal to capacity: the empty block is still valid.
	require.NoError(t, (Budget{LaneSize: capacity}).Verify(capacity, 0, 0),
		"with the quota exactly equal to capacity the empty block must be valid")
}

// TestPayBidTxAlwaysFitsAfterLaneAdmission pins the algebraic closure of the bid path: the
// SubGas(PayBidTxGasLimit) reservation held during the loop guarantees payBidTx still fits
// once AddGas gives it back.
//
// payBidTx is not special-cased, the classifier decides (bid_simulator.go argues why), so in
// reality it is payment and fits unconditionally. Asserted here is the stronger general case,
// where the threshold below is the precondition the quota has to satisfy.
func TestPayBidTxAlwaysFitsAfterLaneAdmission(t *testing.T) {
	const capacity, payBidTxGas = 1000, 25
	for seed := int64(0); seed < 2000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		// The quota must leave room for payBidTx itself; see the threshold assertion below.
		laneSize := uint64(rng.Intn(capacity - payBidTxGas + 1))
		seq := make([]txSpec, 100)
		for i := range seq {
			limit := uint64(1 + rng.Intn(200))
			seq[i] = txSpec{Class(rng.Intn(2)), limit, uint64(rng.Intn(int(limit) + 1))}
		}
		_, b, poolUsed := laneRun(t, capacity, laneSize, payBidTxGas, seq)

		// After AddGas returns the reservation.
		shared := capacity - poolUsed
		require.GreaterOrEqualf(t, b.MaxAvailableGas(shared, ClassGeneral), uint64(payBidTxGas),
			"seed %d: payBidTx no longer fits at L=%d", seed, laneSize)
	}
	// The threshold sits exactly at capacity - payBidTxGas.
	b := Budget{LaneSize: capacity - payBidTxGas + 1}
	require.Less(t, b.MaxAvailableGas(capacity, ClassGeneral), uint64(payBidTxGas),
		"past a quota of capacity-payBidTxGas, payBidTx is supposed to stop fitting")
}

// TestPackingIsOrderSensitive records that the same transactions pack to a different total
// depending on arrival order. Not a bug (builders fix bid order, the tip sort fixes local
// order), but no reasoning may assume the total can be recomputed in another order.
func TestPackingIsOrderSensitive(t *testing.T) {
	const capacity, laneSize = 100, 50
	g := txSpec{ClassGeneral, 50, 50}
	p := txSpec{ClassPayment, 60, 60}

	_, _, paymentFirst := laneRun(t, capacity, laneSize, 0, []txSpec{p, g})
	_, _, generalFirst := laneRun(t, capacity, laneSize, 0, []txSpec{g, p})

	require.Equal(t, uint64(60), paymentFirst, "payment first: total packed gas")
	require.Equal(t, uint64(50), generalFirst, "general first: total packed gas")
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

	taken, _, _ := laneRun(t, capacity, laneSize, 0, seq)
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
		err := CheckInequality(limit, tc.general+tc.payment, tc.payment, lane)
		if tc.wantErr {
			require.ErrorIsf(t, err, ErrViolated, "%s: expected ErrViolated", tc.name)
		} else {
			require.NoErrorf(t, err, "%s: expected a valid block", tc.name)
		}
	}
}

// TestOverflowIsNotAWayIn guards the overflow surface of the header-verification check. Both
// committed values come straight out of 32 attacker-controlled header bytes, and under naive
// addition a gasUsed near 2^64 wraps back to a small value and *passes*, retiring the whole
// "reject a rule-violating header before executing it" gate.
func TestOverflowIsNotAWayIn(t *testing.T) {
	const gasLimit = 70_000_000
	maxU := uint64(math.MaxUint64)

	for _, tc := range []struct{ gasUsed, payment, lane uint64 }{
		{maxU, 0, 0},
		{gasLimit, 0, maxU},
		{maxU/2 + 1, 0, maxU/2 + 1},
		{gasLimit + 1, 0, 0},
	} {
		require.ErrorIsf(t, CheckInequality(gasLimit, tc.gasUsed, tc.payment, tc.lane), ErrViolated,
			"CheckInequality(gasUsed=%d payment=%d lane=%d) must be a violation",
			tc.gasUsed, tc.payment, tc.lane)
	}

	// A quota at 2^64-1 with payment right behind it must not saturate its way to a
	// pass: satSub gives zero, so the verdict has to come from gasUsed alone.
	require.NoError(t, CheckInequality(gasLimit, 1000, maxU, maxU))
}

// TestVerifyFailureTriggers fixes which accounting mistake maps to which error, and is the
// only test exercising the PaymentUsed <= poolUsed bound - the admission tests keep the two
// in step by construction. That bound enforces "no apply booked twice, no out-of-band
// reservation booked at all", a discipline people maintain rather than the type system.
func TestVerifyFailureTriggers(t *testing.T) {
	for _, tc := range []struct {
		name              string
		b                 Budget
		gasUsed, poolUsed uint64
		wantErr           error
	}{
		{"consistent and valid", Budget{LaneSize: 20, PaymentUsed: 20}, 80, 80, nil},
		{"payment booked beyond the pool total", Budget{LaneSize: 20, PaymentUsed: 81}, 80, 80, ErrPaymentExceedsPool},
		{"accounting consistent but the quota does not fit this block", Budget{LaneSize: 200}, 0, 0, ErrViolated},
		{"system gas overran the reservation and burst the block", Budget{LaneSize: 20, PaymentUsed: 20}, 101, 80, ErrViolated},
		{"when both fail, the pool bound is reported first (it names the cause)", Budget{LaneSize: 200, PaymentUsed: 100}, 99, 99, ErrPaymentExceedsPool},
	} {
		err := tc.b.Verify(100, tc.gasUsed, tc.poolUsed)
		if tc.wantErr == nil {
			require.NoErrorf(t, err, "%s: expected to pass", tc.name)
		} else {
			require.ErrorIsf(t, err, tc.wantErr, "%s", tc.name)
		}
	}
}

// TestVerifyRejectsSwappedTotals keeps the argument-order tripwire from being deleted as dead
// code. poolUsed is gasUsed less system gas, so nothing but a swapped call can reach it - which
// is exactly the mistake that would otherwise evaluate the rule against too small a total.
func TestVerifyRejectsSwappedTotals(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20}
	require.NoError(t, b.Verify(100, 80, 80), "equal totals are the no-system-gas case")
	require.ErrorContains(t, b.Verify(100, 80, 101), "exceeds block total")
}

// TestVerifyCommitmentComparesThePaymentFigure covers the only authoritative check on the
// committed accounting. One comparison is the whole of it: general gas is header.GasUsed less
// payment, so a lie about it is a lie about the header total, which block validation catches.
func TestVerifyCommitmentComparesThePaymentFigure(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20}
	const gasLimit, gasUsed, pool = 100, 80, 80

	require.NoError(t, b.VerifyCommitment(gasLimit, gasUsed, pool, Commitment{LaneSize: 20, PaymentGasUsed: 20}))

	for _, tc := range []struct {
		name string
		lie  Commitment
	}{
		{"payment understated", Commitment{LaneSize: 20, PaymentGasUsed: 19}},
		{"payment overstated", Commitment{LaneSize: 20, PaymentGasUsed: 21}},
		{"payment claimed as the whole block", Commitment{LaneSize: 20, PaymentGasUsed: 80}},
		{"payment claimed as zero", Commitment{LaneSize: 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, b.VerifyCommitment(gasLimit, gasUsed, pool, tc.lie), ErrUntruthy)
		})
	}
}

// TestVerifyCommitmentIgnoresLaneSize records a deliberate division of labour, so nobody
// "fixes" it by adding the comparison here: the quota is a pure function of the parent, so
// CheckNextLaneSize settles it before any transaction executes. Replay cannot adjudicate it.
func TestVerifyCommitmentIgnoresLaneSize(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20}
	absurd := Commitment{LaneSize: 999_999, PaymentGasUsed: 20}
	require.NoError(t, b.VerifyCommitment(100, 80, 80, absurd),
		"VerifyCommitment must not police LaneSize; CheckNextLaneSize does")

	// And the check that does police it rejects exactly that.
	p, s := defaultParams(), Signal{}
	require.ErrorIs(t, s.CheckNextLaneSize(absurd.LaneSize, p, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, s.CheckNextLaneSize(s.NextLaneSize(p, 55_000_000), p, 55_000_000))
}

// TestVerifyCommitmentStillEnforcesTheRule: agreeing with a lie is not enough, the
// agreed-upon numbers must also satisfy the inequality.
func TestVerifyCommitmentStillEnforcesTheRule(t *testing.T) {
	b := Budget{LaneSize: 90, PaymentUsed: 10}
	c := Commitment{LaneSize: 90, PaymentGasUsed: 10}
	// gasUsed 70 + max(0, 90-10) = 150 > 100.
	require.ErrorIs(t, b.VerifyCommitment(100, 70, 70, c), ErrViolated)
}
