package paymentlane

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

type txSpec struct {
	class  Class
	limit  uint64 // gas limit, what admission is decided on
	actual uint64 // gas actually burned, what is accounted (<= limit, models a refund)
}

// laneRun simulates admission over a synthetic sequence; reserve models bid-path gas reservation.
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

		// Accounting must match the pool.
		require.Equalf(t, capacity-reserve-shared(), poolUsed(),
			"tx %d: accounting disagrees with the pool: payment=%d general=%d shared=%d",
			i, b.PaymentUsed, generalUsed, shared())

		// Every accepted prefix must stay valid when the quota fits.
		if laneSize+reserve <= capacity {
			require.NoErrorf(t, CheckInequality(capacity, poolUsed(), b.PaymentUsed, laneSize),
				"tx %d: prefix is not a valid block", i)
		}

		// MaxAvailableGas must never increase.
		for _, c := range []Class{ClassGeneral, ClassPayment} {
			m := b.MaxAvailableGas(shared(), c)
			require.LessOrEqualf(t, m, prev[c],
				"tx %d: MaxAvailableGas for class %s rose %d -> %d, Pop() no longer holds",
				i, c, prev[c], m)
			prev[c] = m
		}

		// General traffic must not consume idle lane space.
		if laneSize+reserve <= capacity {
			require.GreaterOrEqualf(t, shared(), b.IdleLane(),
				"tx %d: shared(%d) < IdleLane(%d), lane space was taken over",
				i, shared(), b.IdleLane())
		}
	}
	return taken, b, poolUsed()
}

// TestZeroValueClassIsGeneral keeps the zero-value fallback aligned with the upstream path.
func TestZeroValueClassIsGeneral(t *testing.T) {
	var unset Class
	require.Equal(t, ClassGeneral, unset, "the zero Class must be ClassGeneral")
	require.Equal(t, "general", unset.String())

	var b Budget
	b.RecordUsed(unset, 500)
	require.Equal(t, uint64(0), b.PaymentUsed, "unclassified gas must not be booked as payment")
	require.Equal(t, uint64(0), b.IdleLane(), "and it must not move the quota either")

	require.Equal(t, uint64(1000), b.MaxAvailableGas(1000, unset))
	require.Equal(t, uint64(1000), b.MaxAvailableGas(1000, ClassPayment))

	withLane := Budget{LaneSize: 300}
	require.Equal(t, uint64(700), withLane.MaxAvailableGas(1000, unset))
}

// TestAdmissionInvariants checks the packing loop invariants over random sequences.
func TestAdmissionInvariants(t *testing.T) {
	const capacity = 1000
	for seed := int64(0); seed < 3000; seed++ {
		rng := rand.New(rand.NewSource(seed))
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

// TestAdmissionIsExactlyTight checks that Admits matches post-transaction validity exactly.
func TestAdmissionIsExactlyTight(t *testing.T) {
	const capacity = 40
	for laneSize := uint64(0); laneSize <= capacity; laneSize += 7 {
		for pu := uint64(0); pu <= capacity; pu += 3 {
			for gu := uint64(0); gu+pu <= capacity; gu += 3 {
				b := Budget{LaneSize: laneSize, PaymentUsed: pu}
				// Skip unreachable states.
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

// TestGeneralMaxAvailableGasFlatBelowLane checks the general bound below and above the quota.
func TestGeneralMaxAvailableGasFlatBelowLane(t *testing.T) {
	const capacity, laneSize = 1000, 300
	for _, pu := range []uint64{0, 1, laneSize / 2, laneSize - 1, laneSize} {
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, uint64(capacity-laneSize), b.MaxAvailableGas(capacity-pu, ClassGeneral),
			"pu=%d (inside the quota): general's MaxAvailableGas must be constant", pu)
	}
	for _, over := range []uint64{1, 2, 100} {
		pu := uint64(laneSize) + over
		b := Budget{LaneSize: laneSize, PaymentUsed: pu}
		require.Equalf(t, capacity-pu, b.MaxAvailableGas(capacity-pu, ClassGeneral),
			"pu=L+%d: general's MaxAvailableGas", over)
	}
}

// TestIdleLaneBoundaries checks the saturating IdleLane edges.
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

// TestPaymentPredicateIsTheLooserOne keeps the worker termination check sound.
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

// TestLaneSizeExceedsCapacity checks the over-capacity quota behavior.
func TestLaneSizeExceedsCapacity(t *testing.T) {
	const capacity = 1000
	taken, b, poolUsed := laneRun(t, capacity, capacity+1, 0, []txSpec{
		{ClassGeneral, 1, 1},
		{ClassPayment, 500, 500},
		{ClassGeneral, 1, 1},
	})
	require.Equal(t, []int{1}, taken, "only the payment transaction should have been admitted")

	require.ErrorIs(t, b.Verify(capacity, poolUsed, poolUsed), ErrViolated,
		"a quota larger than capacity must make the self-check report ErrViolated")

	require.NoError(t, (Budget{LaneSize: capacity}).Verify(capacity, 0, 0),
		"with the quota exactly equal to capacity the empty block must be valid")
}

// TestPayBidTxAlwaysFitsAfterLaneAdmission checks that the reserved gas comes back for payBidTx.
func TestPayBidTxAlwaysFitsAfterLaneAdmission(t *testing.T) {
	const capacity, payBidTxGas = 1000, 25
	for seed := int64(0); seed < 2000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		laneSize := uint64(rng.Intn(capacity - payBidTxGas + 1))
		seq := make([]txSpec, 100)
		for i := range seq {
			limit := uint64(1 + rng.Intn(200))
			seq[i] = txSpec{Class(rng.Intn(2)), limit, uint64(rng.Intn(int(limit) + 1))}
		}
		_, b, poolUsed := laneRun(t, capacity, laneSize, payBidTxGas, seq)

		shared := capacity - poolUsed
		require.GreaterOrEqualf(t, b.MaxAvailableGas(shared, ClassGeneral), uint64(payBidTxGas),
			"seed %d: payBidTx no longer fits at L=%d", seed, laneSize)
	}
	b := Budget{LaneSize: capacity - payBidTxGas + 1}
	require.Less(t, b.MaxAvailableGas(capacity, ClassGeneral), uint64(payBidTxGas),
		"past a quota of capacity-payBidTxGas, payBidTx is supposed to stop fitting")
}

// TestPackingIsOrderSensitive records that packing depends on order.
func TestPackingIsOrderSensitive(t *testing.T) {
	const capacity, laneSize = 100, 50
	g := txSpec{ClassGeneral, 50, 50}
	p := txSpec{ClassPayment, 60, 60}

	_, _, paymentFirst := laneRun(t, capacity, laneSize, 0, []txSpec{p, g})
	_, _, generalFirst := laneRun(t, capacity, laneSize, 0, []txSpec{g, p})

	require.Equal(t, uint64(60), paymentFirst, "payment first: total packed gas")
	require.Equal(t, uint64(50), generalFirst, "general first: total packed gas")
}

// TestInvariantAdmissionBeatsStaticPools keeps the single-pool design choice explicit.
func TestInvariantAdmissionBeatsStaticPools(t *testing.T) {
	const capacity, laneSize = 200, 100
	seq := []txSpec{
		{ClassPayment, 60, 60}, {ClassPayment, 50, 50},
		{ClassPayment, 50, 50}, {ClassGeneral, 40, 40},
	} // sums to 200, exactly the capacity

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

// TestLaneIsFloorNotCeiling covers the rule boundary cases.
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

// TestOverflowIsNotAWayIn checks the overflow-sensitive header cases.
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

	require.NoError(t, CheckInequality(gasLimit, 1000, maxU, maxU))
}

// TestVerifyFailureTriggers keeps the Verify error mapping stable.
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

// TestVerifyRejectsSwappedTotals catches swapped Verify arguments.
func TestVerifyRejectsSwappedTotals(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20}
	require.NoError(t, b.Verify(100, 80, 80), "equal totals are the no-system-gas case")
	require.ErrorContains(t, b.Verify(100, 80, 101), "exceeds block total")
}

// TestVerifyCommitmentComparesThePaymentFigure checks the committed payment total.
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

// TestVerifyCommitmentIgnoresLaneSize leaves quota checking to CheckNextLaneSize.
func TestVerifyCommitmentIgnoresLaneSize(t *testing.T) {
	b := Budget{LaneSize: 20, PaymentUsed: 20}
	absurd := Commitment{LaneSize: 999_999, PaymentGasUsed: 20}
	require.NoError(t, b.VerifyCommitment(100, 80, 80, absurd),
		"VerifyCommitment must not police LaneSize; CheckNextLaneSize does")

	p, s := defaultParams(), Signal{}
	require.ErrorIs(t, s.CheckNextLaneSize(absurd.LaneSize, p, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, s.CheckNextLaneSize(s.NextLaneSize(p, 55_000_000), p, 55_000_000))
}

// TestVerifyCommitmentStillEnforcesTheRule keeps VerifyCommitment checking the inequality.
func TestVerifyCommitmentStillEnforcesTheRule(t *testing.T) {
	b := Budget{LaneSize: 90, PaymentUsed: 10}
	c := Commitment{LaneSize: 90, PaymentGasUsed: 10}
	require.ErrorIs(t, b.VerifyCommitment(100, 70, 70, c), ErrViolated)
}
