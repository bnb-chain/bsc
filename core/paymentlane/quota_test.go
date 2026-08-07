package paymentlane

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// defaultParams is the tuple every chain runs until governance writes something,
// i.e. the DEFAULT_* set. TestDefaultsMatchDeployedBytecode pins it to the blob.
func defaultParams() Params {
	return Params{
		MinRatio: defaultMinRatio, MaxRatio: defaultMaxRatio,
		ExpandTrigger: defaultExpandTrigger, ShrinkTrigger: defaultShrinkTrigger,
		ExpandStep: defaultExpandStep, ShrinkStep: defaultShrinkStep,
		MinGas: defaultMinGas, MaxGas: defaultMaxGas,
	}
}

// contractLegal mirrors PaymentLane._validateParams: the eight absolute bounds of
// stage one and the six coupled invariants of stage two.
//
// Duplicated here on purpose. It keeps the exhaustive tests below over exactly the
// configurations governance can actually produce - a test that ranges over
// impossible tuples proves less, and one that ranges over too few proves nothing
// about the boundaries. It also documents the invariants next to the code that
// depends on them.
// The contract's protocol constants, mirrored for contractLegal. All seven are public
// getters on the contract and all seven are pinned against the deployed blob by
// TestConstantsMatchDeployedBytecode - they have to be, because a contract-side widening
// that this file does not follow silently narrows every exhaustive test below instead of
// failing one.
const (
	triggerGapMin       = 1_000
	ratioGapMin         = 500
	minExpandTriggerRat = 5_000
	minShrinkTriggerRat = 2_000
	maxStepRatio        = 1_000
	minLaneGas          = 21_000
	maxLaneGas          = 1_000_000_000
)

func contractLegal(p Params) bool {
	// stage one
	switch {
	case p.MinRatio == 0 || p.MinRatio > maxLaneRatio:
		return false
	case p.MaxRatio < ratioGapMin || p.MaxRatio > maxLaneRatio:
		return false
	case p.ExpandTrigger < minExpandTriggerRat || p.ExpandTrigger > RatioDenom:
		return false
	case p.ShrinkTrigger < minShrinkTriggerRat || p.ShrinkTrigger > RatioDenom:
		return false
	case p.ExpandStep == 0 || p.ExpandStep > maxStepRatio:
		return false
	case p.ShrinkStep == 0 || p.ShrinkStep > maxStepRatio:
		return false
	case p.MinGas < minLaneGas || p.MinGas > maxLaneGas:
		return false
	case p.MaxGas < minLaneGas || p.MaxGas > maxLaneGas:
		return false
	}
	// stage two, the six BEP invariants
	switch {
	case p.ExpandTrigger < p.ShrinkTrigger+triggerGapMin:
		return false
	case p.ExpandStep <= p.ShrinkStep:
		return false
	case p.MaxRatio < p.MinRatio+ratioGapMin:
		return false
	case p.MaxGas <= p.MinGas:
		return false
	case p.MaxRatio+p.ExpandTrigger > RatioDenom:
		return false
	case p.ExpandStep+p.ShrinkTrigger > p.ExpandTrigger:
		return false
	}
	return true
}

func TestDefaultParamsAreContractLegal(t *testing.T) {
	// The whole "no client-side sanitizer is needed" argument rests on this: after
	// the zero substitution the tuple is either the defaults or something the
	// contract validated, and the defaults must therefore be valid themselves.
	require.True(t, contractLegal(defaultParams()))
}

// legalLattice enumerates contract-legal parameter tuples across a coarse grid
// that includes every stage-one boundary value reachable once the six stage-two
// invariants are applied. (shrinkTrigger's own upper bound, RATIO_DENOM, is not
// among them: invariants (1) and (5) make it unreachable.)
func legalLattice() []Params {
	var out []Params
	for _, minRatio := range []uint64{1, 200, 1_500, maxLaneRatio} {
		for _, maxRatio := range []uint64{500, 800, maxLaneRatio} {
			for _, expandTrigger := range []uint64{5_000, 8_000, RatioDenom} {
				for _, shrinkTrigger := range []uint64{2_000, 7_000} {
					for _, expandStep := range []uint64{2, 200, 1_000} {
						for _, shrinkStep := range []uint64{1, 50} {
							for _, minGas := range []uint64{21_000, 2_000_000} {
								for _, maxGas := range []uint64{8_000_000, 1_000_000_000} {
									p := Params{minRatio, maxRatio, expandTrigger, shrinkTrigger, expandStep, shrinkStep, minGas, maxGas}
									if contractLegal(p) {
										out = append(out, p)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return out
}

// gasLimits spans the production value, the devnet value, values that are NOT
// multiples of RatioDenom (which is the only regime where the rounding rule is
// observable), the halt boundary and the consensus maximum.
func gasLimits() []uint64 {
	return []uint64{
		params.MinGasLimit, 7_000_000, 20_000_000, 24_999_999, 25_000_000,
		30_000_000, 40_000_000, 54_999_999, 55_000_000, 55_009_999, 70_000_000,
		params.MaxGasLimit,
	}
}

func TestLegalLatticeIsNotEmpty(t *testing.T) {
	// Guards the exhaustive tests below against silently becoming no-ops if
	// contractLegal is ever tightened by mistake.
	require.Greater(t, len(legalLattice()), 50)
}

// TestClampIsExhaustive checks the clamp over every tuple of the contract-legal
// lattice below (300 of them), a wide
// span of gas limits and the interesting predecessor values, asserting the three
// properties the rest of the design depends on.
func TestClampIsExhaustive(t *testing.T) {
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			ceiling, floor := laneCeiling(p, gl), laneFloor(p, gl)

			// floor is defined against ceiling, so an empty intersection - and BEP
			// 3.4's tie-break rule for it - cannot exist.
			require.LessOrEqual(t, floor, ceiling, "params %s gasLimit %d", p, gl)

			for _, prev := range []uint64{
				0, 1, floor - 1, floor, floor + 1, ceiling - 1, ceiling, ceiling + 1, math.MaxUint64,
			} {
				// A parent that is in the hysteresis band, so the step is zero and only
				// the clamp acts.
				s := Signal{laneSize: prev, signalGasUsed: 75, gasLimit: 100}
				if !inBand(p, s) {
					s = bandSignal(p)
					s.laneSize = prev
				}
				got := LaneSize(p, s, gl)

				// The unconditional bound the no-halt argument rests on.
				require.LessOrEqual(t, got, satSub(gl, params.SystemTxsGasHardLimit),
					"safety clamp violated: params %s gasLimit %d prev %d", p, gl, prev)
				require.LessOrEqual(t, got, mulDivFloor(maxLaneRatio, gl, RatioDenom),
					"quota above MAX_LANE_RATIO: params %s gasLimit %d prev %d", p, gl, prev)

				// Absent the safety clamp the result is inside [floor, ceiling].
				if satSub(gl, params.SystemTxsGasHardLimit) >= ceiling {
					require.GreaterOrEqual(t, got, floor, "params %s gasLimit %d prev %d", p, gl, prev)
					require.LessOrEqual(t, got, ceiling, "params %s gasLimit %d prev %d", p, gl, prev)

					// Idempotence: a value already inside the range, with no step, holds.
					if prev >= floor && prev <= ceiling {
						require.Equal(t, prev, got, "clamp is not idempotent in the band: params %s gasLimit %d prev %d", p, gl, prev)
					}
				}
			}
		}
	}
}

// inBand reports whether s puts the quota in the hysteresis band for p.
func inBand(p Params, s Signal) bool {
	return !gte128(s.signalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit) &&
		gte128(s.signalGasUsed, RatioDenom, p.ShrinkTrigger, s.gasLimit)
}

// bandSignal builds a parent signal that lands strictly inside the hysteresis
// band. The band is non-empty for every legal tuple because TRIGGER_GAP_MIN
// guarantees expandTrigger >= shrinkTrigger + 1000.
func bandSignal(p Params) Signal {
	const gl = RatioDenom // so signalGasUsed is literally the ratio in bps
	return Signal{signalGasUsed: p.ShrinkTrigger, gasLimit: gl}
}

func TestBandSignalIsActuallyInTheBand(t *testing.T) {
	for _, p := range legalLattice() {
		require.True(t, inBand(p, bandSignal(p)), "params %s", p)
	}
}

// TestRoundingIsMultiplyFirst pins the order of operations. Divide-first agrees
// whenever GasLimit is a multiple of RatioDenom, which is the steady state, so this
// divergence is invisible to every test that does not deliberately pick a gas limit
// in transit - and it is permanent once it happens, because the quota is an
// accumulator.
func TestRoundingIsMultiplyFirst(t *testing.T) {
	for _, tc := range []struct {
		gasLimit, ratio, want uint64
	}{
		{55_009_999, 200, 1_100_199},
		{55_009_999, 800, 4_400_799},
		{55_009_999, 2_000, 11_001_999},
		{54_999_999, 800, 4_399_999},
		{55_000_000, 800, 4_400_000}, // the steady state, where both agree
	} {
		got := mulDivFloor(tc.ratio, tc.gasLimit, RatioDenom)
		require.Equal(t, tc.want, got, "gasLimit %d ratio %d", tc.gasLimit, tc.ratio)
		if divideFirst := tc.gasLimit / RatioDenom * tc.ratio; divideFirst != tc.want {
			// Assert the wrong order really is wrong here, so the case cannot pass
			// for the wrong reason.
			require.NotEqual(t, divideFirst, got)
		}
	}
}

// TestMulDivFloorMatchesExactArithmetic checks the 128-bit path against big.Int at
// endpoint-inclusive samples of both operands, including the consensus maximum gas
// limit where a 64-bit implementation wraps.
func TestMulDivFloorMatchesExactArithmetic(t *testing.T) {
	for _, ratio := range []uint64{0, 1, 50, 200, 1_000, 2_000, RatioDenom} {
		for _, gl := range gasLimits() {
			want := new(big.Int).Mul(new(big.Int).SetUint64(ratio), new(big.Int).SetUint64(gl))
			want.Div(want, big.NewInt(RatioDenom))
			require.True(t, want.IsUint64(), "test bound: %s should fit uint64", want)
			require.Equal(t, want.Uint64(), mulDivFloor(ratio, gl, RatioDenom), "ratio %d gasLimit %d", ratio, gl)
		}
	}
	// Saturation rather than a panic when the quotient cannot be represented.
	require.Equal(t, uint64(math.MaxUint64), mulDivFloor(math.MaxUint64, math.MaxUint64, RatioDenom))
}

// TestSignalComparisonIsExactAt64BitOverflow is the reason the comparison is
// 128-bit. The consensus bound on GasLimit is 2^63-1, not the ~5.5e7 production
// runs at, and 10000*2^62 wraps to zero in 64 bits - which flips a shrink into an
// expansion.
func TestSignalComparisonIsExactAt64BitOverflow(t *testing.T) {
	gl := uint64(1) << 62 // a legal gas limit: the consensus cap is 2^63-1
	// 10000 * 2^62 overflows uint64 and wraps to exactly zero. Computed through a
	// variable so the compiler cannot reject it as a constant overflow - which is
	// itself the point: a 64-bit implementation would silently produce this.
	naive := uint64(RatioDenom) * gl
	require.Zero(t, naive, "premise of this test: the naive product wraps")

	// The exact 80% threshold. Computed with big.Int on purpose: gl/10*8 is NOT
	// 80% of 2^62, because 2^62 has no factor of 5, and getting that wrong is how a
	// boundary test ends up asserting the wrong side of the boundary.
	thr := new(big.Int).Mul(big.NewInt(8_000), new(big.Int).SetUint64(gl))
	thr.Add(thr, big.NewInt(RatioDenom-1))
	thr.Div(thr, big.NewInt(RatioDenom)) // ceil(0.8*gl)
	require.True(t, thr.IsUint64())
	at := thr.Uint64()

	// One gas below the threshold: must NOT expand.
	require.False(t, gte128(at-1, RatioDenom, 8_000, gl))
	// At the threshold: must expand, since the expansion comparison is >=.
	require.True(t, gte128(at, RatioDenom, 8_000, gl))

	// Cross-check the whole predicate against exact arithmetic on a spread of
	// values, at gas limits where 64-bit arithmetic is unreliable.
	for _, gl := range []uint64{1 << 40, 1 << 50, 1 << 62, params.MaxGasLimit} {
		for _, trigger := range []uint64{2_000, 5_000, 8_000, RatioDenom} {
			for _, num := range []uint64{0, 1, gl / 4, gl / 2, gl - 1, gl} {
				lhs := new(big.Int).Mul(new(big.Int).SetUint64(num), big.NewInt(RatioDenom))
				rhs := new(big.Int).Mul(new(big.Int).SetUint64(trigger), new(big.Int).SetUint64(gl))
				require.Equal(t, lhs.Cmp(rhs) >= 0, gte128(num, RatioDenom, trigger, gl),
					"num %d trigger %d gasLimit %d", num, trigger, gl)
			}
		}
	}
}

// TestTriggerComparisonBoundaries pins both comparison operators at the exact boundary
// block,
// which is the only place the choice of operator is observable - and one such
// block offsets the accumulator forever.
func TestTriggerComparisonBoundaries(t *testing.T) {
	p := defaultParams()
	const gl = 55_000_000
	// signalGasUsed expressed against a parent gas limit of RatioDenom makes the
	// numerator literally the ratio in bps.
	step := mulDivFloor(p.ExpandStep, gl, RatioDenom)
	base := uint64(3_000_000)

	for _, tc := range []struct {
		bps  uint64
		want uint64
		why  string
	}{
		{defaultExpandTrigger, base + step, "at the expand trigger: expands, because the test is >="},
		{defaultExpandTrigger - 1, base, "one bp below: hysteresis band, holds"},
		{defaultShrinkTrigger, base, "at the shrink trigger: band, because the test is <"},
		{defaultShrinkTrigger - 1, base - mulDivFloor(p.ShrinkStep, gl, RatioDenom), "one bp below: shrinks"},
	} {
		s := Signal{laneSize: base, signalGasUsed: tc.bps, gasLimit: RatioDenom}
		require.Equal(t, tc.want, LaneSize(p, s, gl), tc.why)
	}
}

// TestStepSaturates covers the saturating addition.
//
// The bound is subtle and a test written with the DEFAULT parameters passes even
// without satAdd: a wrapped sum is always strictly less than the step, so it can
// only land inside [floor, ceiling] when step > floor - and by default the step is
// 1.1M against a floor of 2M. Governance-legal parameters can invert that, so the
// case is reachable and this test uses such a tuple.
func TestStepSaturates(t *testing.T) {
	p := stepAboveFloorParams()
	require.True(t, contractLegal(p), "the premise is a tuple governance could set")

	const gl = 55_000_000
	step := mulDivFloor(p.ExpandStep, gl, RatioDenom)
	floor, ceiling := laneFloor(p, gl), laneCeiling(p, gl)
	require.Greater(t, step, floor, "premise: only then can a wrapped sum land in the window")

	// Pick a predecessor whose naive sum wraps to a value inside the window.
	target := floor + (ceiling-floor)/2
	prev := target - step // wraps around in uint64
	require.Greater(t, prev, ceiling, "premise: prev is far outside the window")
	require.Equal(t, target, prev+step, "premise: the naive sum lands inside the window")

	s := Signal{laneSize: prev, signalGasUsed: p.ExpandTrigger, gasLimit: RatioDenom}
	got := LaneSize(p, s, gl)
	require.Equal(t, ceiling, got, "satAdd must saturate and clamp to the ceiling, not wrap to %d", target)
	require.NotEqual(t, target, got)

	// Saturating subtraction, the mirror case.
	s = Signal{laneSize: 0, signalGasUsed: 0, gasLimit: RatioDenom}
	require.Equal(t, floor, LaneSize(p, s, gl), "shrinking from zero must not underflow")
}

// TestClampAppliesEveryBlock pins the every-block clamp (BEP 3.4.4). A GasLimit walk-down
// alone breaks the
// "clamp only when a step fires" reading, with no governance action involved: the
// quota holds its gas value while the ratio ceiling falls under it, so its share of
// the block grows past MAX_LANE_RATIO.
func TestClampAppliesEveryBlock(t *testing.T) {
	p := defaultParams()
	// Traffic parked in the hysteresis band the whole way, which BEP invariant (1)
	// guarantees is possible and governance cannot tune away.
	band := bandSignal(p)

	// Start at the ceiling for 70M and walk the gas limit down as CalcGasLimit
	// would, by 1/1024 per block.
	gl := uint64(70_000_000)
	lane := laneCeiling(p, gl)
	require.Equal(t, uint64(5_600_000), lane)

	// 70M -> 20M is 1283 decrements of 1/1024, so the bound has to allow for that;
	// a shorter loop silently never reaches the region this test exists for.
	seenBelow := false
	for i := 0; i < 2_000 && gl > 7_000_000; i++ {
		gl -= gl / params.GasLimitBoundDivisor
		band.laneSize = lane
		lane = LaneSize(p, band, gl)

		require.LessOrEqual(t, lane, mulDivFloor(maxLaneRatio, gl, RatioDenom),
			"the quota's share of the block exceeded MAX_LANE_RATIO at gasLimit %d", gl)
		// In the band the quota does not step, so the only forces on it are the
		// ratio ceiling and - below about 21.74M, where the system reservation
		// leaves less room than 8% of the block - the safety clamp.
		require.Equal(t, min(laneCeiling(p, gl), satSub(gl, params.SystemTxsGasHardLimit)), lane,
			"in the band only the ceiling and the safety clamp may move the quota, gasLimit %d", gl)
		if gl <= 20_000_000 {
			seenBelow = true
			require.LessOrEqual(t, lane, uint64(1_600_000), "at gasLimit %d the 8%% ceiling is 1.6M", gl)
		}
	}
	require.True(t, seenBelow, "the walk must actually reach the interesting region")

	// And the counterfactual: holding the quota (clamping only on a step) would have
	// left it at 5.6M, i.e. 28% of a 20M block, above MAX_LANE_RATIO.
	require.Greater(t, uint64(5_600_000), mulDivFloor(maxLaneRatio, 20_000_000, RatioDenom))
}

// TestBootstrapIsTheZeroSignal pins the activation semantics. The lane opens at its
// floor as a consequence of the general function, not as a second formula.
func TestBootstrapIsTheZeroSignal(t *testing.T) {
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			want := min(laneFloor(p, gl), satSub(gl, params.SystemTxsGasHardLimit))
			require.Equal(t, want, LaneSize(p, Signal{}, gl), "params %s gasLimit %d", p, gl)
		}
	}

	// newSignal is the only way to reach it, and nil means "the parent carries no
	// commitment" - which is true for exactly one block per network.
	require.Equal(t, Signal{}, newSignal(nil, 0, 55_000_000))

	// The concrete number a chain sees at Gauss+1 with the shipped defaults: a 2M
	// gas step change in general capacity, which governance cannot soften because
	// it cannot write parameters before the contract's code exists.
	require.Equal(t, uint64(2_000_000), LaneSize(defaultParams(), Signal{}, 55_000_000))
}

// TestZeroSignalGuardIsWhatMakesTheSeedTheFloor isolates the guard: without it,
// 0 >= expandTrigger*0 holds vacuously, the seed takes the expansion branch, and the
// lane opens at one step instead of at its floor.
//
// It must NOT use the shipped defaults. There the unguarded step is 1.1M, which the
// clamp raises back to the 2M floor, so guarded and unguarded agree and the test
// would pass against a broken implementation - measured, and the reason this test
// previously proved nothing. The divergence is only visible where step > floor,
// which governance-legal parameters allow.
func TestZeroSignalGuardIsWhatMakesTheSeedTheFloor(t *testing.T) {
	p := stepAboveFloorParams()
	const gl = 55_000_000
	floor, step := laneFloor(p, gl), mulDivFloor(p.ExpandStep, gl, RatioDenom)
	require.Greater(t, step, floor, "premise: only then are the two branches distinguishable")

	require.True(t, gte128(0, RatioDenom, p.ExpandTrigger, 0),
		"premise: with a zero denominator the expansion test holds vacuously")

	got := LaneSize(p, Signal{}, gl)
	require.Equal(t, floor, got)
	// The value an unguarded implementation would produce: the step, clamped.
	require.NotEqual(t, min(step, laneCeiling(p, gl)), got)
}

// stepAboveFloorParams is a governance-legal tuple whose expansion step exceeds its
// own floor. Several tests need it, because that is the only regime in which the
// saturating add and the zero-signal guard are observable at all.
func stepAboveFloorParams() Params {
	p := Params{
		MinRatio: 1, MaxRatio: 501, ExpandTrigger: 8_000, ShrinkTrigger: 2_000,
		ExpandStep: 1_000, ShrinkStep: 50, MinGas: 21_000, MaxGas: 8_000_000,
	}
	return p
}

// TestNoHaltIsReachable pins both sides of the halt boundary rather than only the
// safe one.
func TestNoHaltIsReachable(t *testing.T) {
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			got := LaneSize(p, Signal{}, gl)
			// With the safety clamp, an empty block is always valid: the quota plus the
			// worst-case system reservation fits.
			require.LessOrEqual(t, got, satSub(gl, params.SystemTxsGasHardLimit),
				"params %s gasLimit %d", p, gl)
		}
	}

	// And the boundary is real: without the clamp, a 20M-gas breathe block would
	// have no valid form at all, because the reservation alone consumes the block.
	p := defaultParams()
	const small = 20_000_000
	unclamped := min(max(uint64(0), laneFloor(p, small)), laneCeiling(p, small))
	require.Greater(t, unclamped+params.SystemTxsGasHardLimit, uint64(small),
		"premise: at %d gas the unclamped quota plus the reservation exceeds the block", small)
	require.Zero(t, LaneSize(p, Signal{}, small), "the clamp must switch the lane off rather than halt the chain")

	// Production and the devnet are far above the boundary, so the clamp is inert
	// there and the lane is genuinely exercised.
	for _, gl := range []uint64{40_000_000, 55_000_000, 70_000_000} {
		require.Equal(t, laneFloor(p, gl), LaneSize(p, Signal{}, gl), "the safety clamp must not bind at gasLimit %d", gl)
	}
}

// TestTheRulesSkipTheActivationBlock pins the activation semantics (BEP 3.4.5): the
// PaymentLane code is installed at the END of the Gauss block, so the rules cannot bind
// there - which is why every enforcement point asks about the PARENT.
func TestTheRulesSkipTheActivationBlock(t *testing.T) {
	forkTime := uint64(1_800_000_000)
	config := *params.BSCChainConfig // copy: never mutate the shared mainnet config
	config.GaussTime = &forkTime

	// Past LondonBlock: IsGauss is gated on IsLondon, so numbers below it answer false
	// for a reason that has nothing to do with the activation rule - which is exactly
	// how this test first passed for the wrong reason.
	require.NotNil(t, config.LondonBlock)
	base := config.LondonBlock.Uint64() + 1_000_000
	num := func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }

	// Every case names the block being judged; the arguments are its PARENT.
	for _, tc := range []struct {
		name                  string
		parentNum, parentTime uint64
		want                  bool
	}{
		{"before the fork", base, forkTime - 6, false},
		{"the activation block itself", base, forkTime - 3, false},
		{"activation + 1", base + 1, forkTime, true},
		{"long after", base + 500, forkTime + 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, config.IsGauss(num(tc.parentNum), tc.parentTime))
		})
	}

	// Unscheduled means off, on every network.
	noGauss := *params.BSCChainConfig
	noGauss.GaussTime = nil
	require.False(t, noGauss.IsGauss(num(base), forkTime-3))

	// And the inherited precondition, pinned so it cannot surprise anyone: IsGauss
	// requires IsLondon, so a chain whose height is still below LondonBlock does not
	// activate the lane however the timestamp is set. Harmless on the real networks
	// (mainnet passed that height long ago, and the test chains set it to 0) but a
	// short-lived custom devnet with a non-zero LondonBlock would silently never
	// switch the lane on.
	require.False(t, config.IsGauss(common.Big0, forkTime),
		"below LondonBlock the lane must not apply")
}

// TestSignalIsSection342Verbatim pins both terms of the congestion signal's numerator,
// each on a case where dropping it flips the verdict.
//
// Both omissions have the same shape - a saturated block reading as a quiet one - and
// each was in this implementation at some point, so both directions are asserted rather
// than just the formula.
func TestSignalIsSection342Verbatim(t *testing.T) {
	const parentGasLimit = 55_000_000

	t.Run("the denominator is the parent's gas limit", func(t *testing.T) {
		c := &Commitment{LaneSize: 4_400_000, PaymentGasUsed: 1_000_000}
		s := newSignal(c, 45_000_000, parentGasLimit)
		require.Equal(t, c.LaneSize, s.laneSize)
		require.Equal(t, uint64(parentGasLimit), s.gasLimit, "never the child's gas limit")
	})

	t.Run("general gas is the header residual, system gas included", func(t *testing.T) {
		// A breathe block: 12.16M of validator-set update on top of 33M of user general
		// gas. Taking a user-only figure would read 33M, under the 70% shrink trigger,
		// and cut the payment floor on a block that is 82% full.
		const systemGas, userGeneral = 12_160_000, 33_000_000
		c := &Commitment{LaneSize: 2_000_000}
		s := newSignal(c, systemGas+userGeneral, parentGasLimit)
		require.Equal(t, uint64(systemGas+userGeneral), s.signalGasUsed)

		p := defaultParams()
		require.True(t, gte128(s.signalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit),
			"a block this full must clear the expand trigger")
		require.False(t, gte128(userGeneral, RatioDenom, p.ExpandTrigger, s.gasLimit),
			"and it must be the system gas that carries it over - otherwise this proves nothing")
	})

	t.Run("payment beyond the quota counts too", func(t *testing.T) {
		// A full block dominated by payment traffic: general 35M + payment 20M fills a
		// 55M block. Without the overflow term the numerator would be 35M, under the
		// shrink trigger, and the floor would shrink in the congestion it exists for.
		const general, payment, lane = 35_000_000, 20_000_000, 4_400_000
		c := &Commitment{LaneSize: lane, PaymentGasUsed: payment}
		s := newSignal(c, general+payment, parentGasLimit)
		require.Equal(t, uint64(general+payment-lane), s.signalGasUsed)

		p := defaultParams()
		require.True(t, gte128(s.signalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit))
		require.False(t, gte128(general, RatioDenom, p.ShrinkTrigger, s.gasLimit),
			"the general term alone would have shrunk")
	})

	t.Run("a saturated block always expands", func(t *testing.T) {
		// The structural property both terms exist for. At the rule's equality the
		// numerator is exactly GasLimit - laneSize, and invariant (5) of section 3.6 caps
		// laneSize at RATIO_DENOM - EXPAND_TRIGGER_RATIO for precisely this reason.
		p := defaultParams()
		require.LessOrEqual(t, p.MaxRatio+p.ExpandTrigger, uint64(RatioDenom), "invariant (5)")
		for _, lane := range []uint64{0, 1, 2_000_000, 4_400_000} {
			for _, payment := range []uint64{0, lane, lane + 1, 20_000_000, parentGasLimit - lane} {
				general := parentGasLimit - max(payment, lane) // saturate the rule
				c := &Commitment{LaneSize: lane, PaymentGasUsed: payment}
				s := newSignal(c, general+payment, parentGasLimit)
				require.Equal(t, parentGasLimit-lane, s.signalGasUsed,
					"lane %d payment %d: a saturated block's signal is GasLimit-laneSize", lane, payment)
				require.True(t, gte128(s.signalGasUsed, RatioDenom, p.ExpandTrigger, s.gasLimit),
					"lane %d payment %d: a saturated block must expand", lane, payment)
			}
		}
	})
}

// TestMulDivFloorGuardBoundary covers hi == d, the exact case the doc comment names
// as the panic source. A guard written hi > d panics there instead of saturating.
//
// Unreachable with consensus-legal inputs (ratio <= RatioDenom and GasLimit < 2^63
// give hi <= 4999), but Params is exported and the function promises no precondition
// on the caller, so the boundary is part of the contract.
func TestMulDivFloorGuardBoundary(t *testing.T) {
	require.Equal(t, uint64(math.MaxUint64), mulDivFloor(RatioDenom+1, math.MaxUint64, RatioDenom),
		"hi == d must saturate, not panic")
	// One below the boundary must still divide.
	got := mulDivFloor(RatioDenom, math.MaxUint64, RatioDenom)
	require.Equal(t, uint64(math.MaxUint64), got)
	require.NotPanics(t, func() { LaneSize(Params{MaxRatio: RatioDenom + 1, MaxGas: 8_000_000}, Signal{}, math.MaxUint64) })
}

// TestActivationAtTheLondonBoundary covers the IsLondon half of IsGauss, which every
// other activation test leaves inert by sitting far above LondonBlock. On a chain whose
// Gauss timestamp lands at LondonBlock, the boundary asserted here is the difference
// between exempting the activation block and binding it.
func TestActivationAtTheLondonBoundary(t *testing.T) {
	forkTime := uint64(1_800_000_000)
	config := *params.BSCChainConfig
	config.GaussTime = &forkTime
	london := big.NewInt(1_000)
	config.LondonBlock = london
	config.BerlinBlock = london

	n := london.Int64()
	// Below London the lane never applies, whatever the timestamp says.
	require.False(t, config.IsGauss(big.NewInt(n-2), forkTime))
	// The London block is the first IsGauss block here, so it is the activation block
	// and is exempt: as a parent it still answers false.
	require.False(t, config.IsGauss(big.NewInt(n-1), forkTime))
	// And the block after it is the first lane block.
	require.True(t, config.IsGauss(big.NewInt(n), forkTime+3))
}

// TestLaneCannotDetectAnUninstalledContract pins the one configuration in which
// "the rules are on" and "the contract exists" genuinely come apart.
//
// With LondonBlock at 0 and GaussTime at or before the genesis timestamp, IsGauss
// already holds at genesis, so IsOnGauss never fires and upgradeBuildInSystemContract
// never installs the code - while the rules bind from block 1. LoadParams cannot see
// it, because an absent account and an untouched one are both all-zero storage. The
// chain then runs the lane on hardcoded defaults with governance unable to change
// them, silently.
//
// This is a constraint on new chain configurations, not a live defect: mainnet's
// LondonBlock is 31,302,048 and the devnet genesis template's is 8. It is pinned so
// that whoever schedules Gauss on a fresh chain has to confront it.
func TestLaneCannotDetectAnUninstalledContract(t *testing.T) {
	zero := uint64(0)
	config := *params.BSCChainConfig
	config.GaussTime = &zero
	config.LondonBlock = common.Big0
	config.BerlinBlock = common.Big0

	// IsOnGauss never fires, so the Gauss upgrade never runs.
	for n := int64(1); n <= 200; n++ {
		require.False(t, config.IsOnGauss(big.NewInt(n), uint64(n-1), uint64(n)),
			"block %d must not be the activation block", n)
	}
	// Yet the rules bind from block 1, because genesis already answers true as a parent.
	require.True(t, config.IsGauss(common.Big0, 1_000))

	// And the read path cannot tell a code-less account from an untouched one.
	got, err := LoadParams(mapReader{})
	require.NoError(t, err)
	require.Equal(t, defaultParams(), got,
		"absent storage is indistinguishable from untouched storage, which is why this configuration must be rejected before Gauss is scheduled")

	// A sane LondonBlock puts the boundary back where it belongs.
	sane := config
	sane.LondonBlock = big.NewInt(8)
	sane.BerlinBlock = big.NewInt(8)
	require.True(t, sane.IsOnGauss(big.NewInt(8), 7, 8), "with LondonBlock 8 the activation block is 8")
	require.False(t, sane.IsGauss(big.NewInt(7), 7), "the activation block itself is exempt")
	require.True(t, sane.IsGauss(big.NewInt(8), 8))
}

// TestParentSignalDerivesBothTheGateAndTheDenominator is why ParentSignal exists
// rather than leaving three call sites to assemble a Signal.
func TestParentSignalDerivesBothTheGateAndTheDenominator(t *testing.T) {
	forkTime := uint64(1_800_000_000)
	config := *params.BSCChainConfig
	config.GaussTime = &forkTime
	base := config.LondonBlock.Uint64() + 1_000_000

	hdr := func(num, time, gasLimit, gasUsed uint64) *types.Header {
		return &types.Header{
			Number: new(big.Int).SetUint64(num), Time: time, GasLimit: gasLimit, GasUsed: gasUsed,
		}
	}
	commitment := Encode(Commitment{LaneSize: 2_400_000, PaymentGasUsed: 1_000})

	t.Run("a parent that is a lane block yields its commitment", func(t *testing.T) {
		grandparent := hdr(base, forkTime, 55_000_000, 0)
		parent := hdr(base+1, forkTime+3, 40_000_000, 30_001_000)
		s, err := ParentSignal(&config, grandparent, parent, commitment)
		require.NoError(t, err)
		require.Equal(t, uint64(2_400_000), s.laneSize)
		// The parent's gas used less its committed payment: 30,001,000 - 1,000. The
		// overflow term is zero here, payment being far under the quota.
		require.Equal(t, uint64(30_000_000), s.signalGasUsed)
		require.Equal(t, uint64(40_000_000), s.gasLimit,
			"the denominator must be the PARENT's gas limit, never the child's")
	})

	t.Run("the activation block carries no commitment", func(t *testing.T) {
		// grandparent is pre-fork, so parent is the activation block and is exempt.
		grandparent := hdr(base, forkTime-3, 55_000_000, 0)
		parent := hdr(base+1, forkTime, 40_000_000, 0)
		// The carrier bytes are garbage here, and that must not matter.
		s, err := ParentSignal(&config, grandparent, parent, common.Hash{0xff})
		require.NoError(t, err)
		require.Equal(t, Signal{}, s)
	})

	t.Run("genesis has no grandparent", func(t *testing.T) {
		s, err := ParentSignal(&config, nil, hdr(0, forkTime, 40_000_000, 0), common.Hash{})
		require.NoError(t, err)
		require.Equal(t, Signal{}, s)
	})

	t.Run("a corrupt commitment is an error, never a seed", func(t *testing.T) {
		grandparent := hdr(base, forkTime, 55_000_000, 0)
		parent := hdr(base+1, forkTime+3, 40_000_000, 0)
		corrupt := commitment
		corrupt[31] = 0xff
		s, err := ParentSignal(&config, grandparent, parent, corrupt)
		require.ErrorIs(t, err, ErrBadCommitment)
		require.Equal(t, Signal{}, s, "the zero value must not be usable as a seed after an error")
	})
}

// TestClampArmsHaveAbsoluteAnchors pins the two clamp arms that no other test can
// see, and it is the repair for a regression the Ceiling/Floor refactor introduced.
//
// LaneSize now calls the same laneCeiling/laneFloor that Ceiling/Floor delegate to,
// so every LaneSize-vs-Ceiling/Floor assertion became a self-comparison: the whole
// lattice loop of TestBootstrapIsTheZeroSignal is now an identity. Measured
// consequence - six mutants that the pre-refactor shape killed from either side
// survived afterwards, all of them in the two arms that do NOT bind under the shipped
// defaults. With the defaults, MaxGas binds only above ~100M of gas limit, and the
// MinRatio arm likewise, so no other test reaches them; they go live the moment
// governance writes a small MinGas/MaxGas (the legal minimum is 21,000) or the gas
// limit is raised.
//
// Absolute expected numbers, computed by hand, are the only oracle left.
func TestClampArmsHaveAbsoluteAnchors(t *testing.T) {
	t.Run("the absolute MaxGas cap binds above the ratio ceiling", func(t *testing.T) {
		// 2000 bps of 70M is 14,000,000, so MaxGas is what binds.
		p := Params{MinRatio: 1, MaxRatio: 2_000, ExpandTrigger: 5_000, ShrinkTrigger: 2_000,
			ExpandStep: 200, ShrinkStep: 50, MinGas: 21_000, MaxGas: 8_000_000}
		require.True(t, contractLegal(p))
		require.Equal(t, uint64(8_000_000), laneCeiling(p, 70_000_000))
		require.Equal(t, uint64(14_000_000), mulDivFloor(p.MaxRatio, 70_000_000, RatioDenom),
			"premise: the ratio arm is larger, so only MaxGas can be what binds")
		// And through LaneSize: a predecessor above the cap must come back to it.
		s := Signal{laneSize: 20_000_000, signalGasUsed: p.ShrinkTrigger, gasLimit: RatioDenom}
		require.Equal(t, uint64(8_000_000), LaneSize(p, s, 70_000_000))
	})

	t.Run("the ratio ceiling binds below the absolute cap", func(t *testing.T) {
		p := defaultParams()
		// 800 bps of 55M is 4,400,000, below MaxGas of 8,000,000.
		require.Equal(t, uint64(4_400_000), laneCeiling(p, 55_000_000))
	})

	t.Run("the MinRatio arm binds above MinGas", func(t *testing.T) {
		// 200 bps of 55M is 1,100,000, above MinGas of 21,000.
		p := Params{MinRatio: 200, MaxRatio: 800, ExpandTrigger: 8_000, ShrinkTrigger: 7_000,
			ExpandStep: 200, ShrinkStep: 50, MinGas: 21_000, MaxGas: 8_000_000}
		require.True(t, contractLegal(p))
		require.Equal(t, uint64(1_100_000), laneFloor(p, 55_000_000))
		require.Equal(t, uint64(1_100_000), LaneSize(p, Signal{}, 55_000_000))
	})

	t.Run("MinGas binds above the MinRatio arm", func(t *testing.T) {
		// The shipped defaults: 200 bps of 55M is 1,100,000, below MinGas of 2,000,000.
		require.Equal(t, uint64(2_000_000), laneFloor(defaultParams(), 55_000_000))
	})

	t.Run("both arms at once, hand-computed", func(t *testing.T) {
		p := Params{MinRatio: 1_500, MaxRatio: 2_000, ExpandTrigger: 5_000, ShrinkTrigger: 2_000,
			ExpandStep: 200, ShrinkStep: 50, MinGas: 21_000, MaxGas: 1_000_000_000}
		require.True(t, contractLegal(p))
		// ceiling = min(2000*40M/1e4, 1e9) = min(8,000,000, 1e9) = 8,000,000
		// floor   = min(max(1500*40M/1e4, 21000), ceiling) = min(6,000,000, 8,000,000)
		require.Equal(t, uint64(8_000_000), laneCeiling(p, 40_000_000))
		require.Equal(t, uint64(6_000_000), laneFloor(p, 40_000_000))
		require.Equal(t, uint64(6_000_000), LaneSize(p, Signal{}, 40_000_000))
	})
}

// TestCheckLaneSizeUsesTheSignal covers the mutation that would reject every lane
// block on the chain.
//
// CheckLaneSize is the enforcement point for the recursion, so an implementation that
// ignored its Signal and compared against the bootstrap floor instead would reject
// every block whose quota had ever stepped - a chain halt. The other CheckLaneSize
// assertions all use the zero Signal, so the mutation is invisible to them.
func TestCheckLaneSizeUsesTheSignal(t *testing.T) {
	p := defaultParams()
	const gl = 55_000_000
	// A parent sitting mid-range with a congested signal, so the quota steps up and
	// lands away from both the floor and the bootstrap value.
	stepped := Signal{laneSize: 3_000_000, signalGasUsed: defaultExpandTrigger, gasLimit: RatioDenom}
	want := LaneSize(p, stepped, gl)
	require.NotEqual(t, LaneSize(p, Signal{}, gl), want, "premise: the stepped quota differs from the bootstrap one")
	require.Equal(t, uint64(4_100_000), want, "3.0M + 2% of 55M, inside [2.0M, 4.4M]")

	require.NoError(t, CheckLaneSize(want, p, stepped, gl))
	require.ErrorIs(t, CheckLaneSize(want+1, p, stepped, gl), ErrQuotaMismatch)
	require.ErrorIs(t, CheckLaneSize(want-1, p, stepped, gl), ErrQuotaMismatch)
	// And specifically: the bootstrap value must be rejected for a stepped parent.
	require.ErrorIs(t, CheckLaneSize(LaneSize(p, Signal{}, gl), p, stepped, gl), ErrQuotaMismatch)
}

// TestNilHeaderHandlingIsSpecified covers ParentSignal's guards themselves, which were
// entirely uncovered: the intended test passed for the wrong reason, because on
// BSCChainConfig LondonBlock is 31,302,048 so IsGauss(0, .) is false and the nil path
// was never reached. A nil grandparent means two different things, and that is the whole
// point of the guards.
func TestNilHeaderHandlingIsSpecified(t *testing.T) {
	forkTime := uint64(1_800_000_000)
	config := *params.BSCChainConfig
	config.GaussTime = &forkTime
	config.LondonBlock = common.Big0
	config.BerlinBlock = common.Big0
	hdr := &types.Header{Number: big.NewInt(10), Time: forkTime + 3, GasLimit: 55_000_000}

	// A nil parent has nothing to answer, and ParentSignal has an error channel for it.
	s, err := ParentSignal(&config, hdr, nil, common.Hash{})
	require.Error(t, err)
	require.Equal(t, Signal{}, s)

	// A nil grandparent is legal for genesis and an error for anything else. Both
	// halves matter: the second is the one that keeps an unresolved grandparent from
	// silently becoming the bootstrap seed, which would reset the quota to the floor
	// on a parent that had stepped, forever.
	genesis := &types.Header{Number: common.Big0, Time: forkTime - 3, GasLimit: 55_000_000}
	s, err = ParentSignal(&config, nil, genesis, common.Hash{})
	require.NoError(t, err)
	require.Equal(t, Signal{}, s)

	s, err = ParentSignal(&config, nil, hdr, common.Hash{})
	require.ErrorIs(t, err, ErrBadCommitment, "an unresolved grandparent must not be read as genesis")
	require.Equal(t, Signal{}, s)
}
