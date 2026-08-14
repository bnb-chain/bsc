package paymentlane

import (
	"math"
	"math/big"
	"math/bits"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// defaultParams is the shipped tuple before governance writes anything.
func defaultParams() Params {
	return Params{
		MinRatio: 200, MaxRatio: 800,
		ExpandTrigger: 8_000, ShrinkTrigger: 7_000,
		ExpandStep: 200, ShrinkStep: 50,
		MinGas: 2_000_000, MaxGas: 8_000_000,
	}
}

// Contract constants mirrored into the tests.
const (
	maxLaneRatio        = 2_000
	triggerGapMin       = 1_000
	ratioGapMin         = 500
	minExpandTriggerRat = 5_000
	minShrinkTriggerRat = 2_000
	maxStepRatio        = 1_000
	minLaneGas          = 21_000
	maxLaneGas          = 1_000_000_000
)

// contractLegal mirrors PaymentLane._validateParams.
func contractLegal(p Params) bool {
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

// legalLattice enumerates a coarse grid of contract-legal parameter tuples.
func legalLattice() []Params {
	var out []Params
	for _, minRatio := range []uint64{1, 200, 1_500} {
		for _, maxRatio := range []uint64{501, 800, maxLaneRatio} {
			for _, expandTrigger := range []uint64{5_000, 8_000, 9_499} {
				for _, shrinkTrigger := range []uint64{2_000, 7_000, 8_499} {
					for _, expandStep := range []uint64{2, 200, 1_000} {
						for _, shrinkStep := range []uint64{1, 50, 999} {
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

// gasLimits covers production, devnet, boundary, and rounding-sensitive values.
func gasLimits() []uint64 {
	return []uint64{
		params.MinGasLimit, 7_000_000, 20_000_000, 24_999_999, 25_000_000,
		30_000_000, 35_000_000, 40_000_000, 54_999_999, 55_000_000, 55_009_999, 70_000_000,
		params.MaxGasLimit,
	}
}

// TestClampIsExhaustive checks the clamp over the legal lattice and gas limits.
func TestClampIsExhaustive(t *testing.T) {
	require.Len(t, legalLattice(), 504)
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			ceiling, floor := laneCeiling(p, gl), laneFloor(p, gl)

			require.LessOrEqual(t, floor, ceiling, "the floor must be taken against the ceiling: params %s gasLimit %d", p, gl)

			for _, parentLane := range []uint64{0, floor - 1, floor, ceiling, ceiling + 1} {
				s := bandSignal(p)
				s.parentLaneSize = parentLane
				got := s.NextLaneSize(p, gl)

				require.LessOrEqual(t, got, satSub(gl, params.SystemTxsGasHardLimit),
					"quota plus the system reservation must fit: params %s gasLimit %d parentLane %d", p, gl, parentLane)
				require.LessOrEqual(t, got, mulDivFloor(maxLaneRatio, gl, RatioDenom),
					"quota above MAX_LANE_RATIO: params %s gasLimit %d parentLane %d", p, gl, parentLane)

				if satSub(gl, params.SystemTxsGasHardLimit) >= ceiling {
					require.GreaterOrEqual(t, got, floor,
						"quota below the floor: params %s gasLimit %d parentLane %d", p, gl, parentLane)
					require.LessOrEqual(t, got, ceiling,
						"quota above the ceiling: params %s gasLimit %d parentLane %d", p, gl, parentLane)

					if parentLane >= floor && parentLane <= ceiling {
						require.Equal(t, parentLane, got,
							"a quota already inside [floor, ceiling] must hold when nothing steps: params %s gasLimit %d", p, gl)
					}
				}
			}
		}
	}
}

// bandSignal sits at the bottom edge of the hysteresis band.
func bandSignal(p Params) Signal {
	const gl = RatioDenom // so signalGasUsed is literally the ratio in bps
	return Signal{parentSignalGasUsed: p.ShrinkTrigger, parentGasLimit: gl}
}

// TestRoundingIsMultiplyFirst keeps the multiply-first rounding order.
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
	}
}

// TestMulDivFloorMatchesExactArithmetic checks the 128-bit path against big.Int.
func TestMulDivFloorMatchesExactArithmetic(t *testing.T) {
	for _, ratio := range []uint64{0, 1, 50, 200, 1_000, 2_000, RatioDenom} {
		for _, gl := range gasLimits() {
			want := new(big.Int).Mul(new(big.Int).SetUint64(ratio), new(big.Int).SetUint64(gl))
			want.Div(want, big.NewInt(RatioDenom))
			require.True(t, want.IsUint64(), "test bound: %s should fit uint64", want)
			require.Equal(t, want.Uint64(), mulDivFloor(ratio, gl, RatioDenom), "ratio %d gasLimit %d", ratio, gl)
		}
	}
	require.Equal(t, uint64(math.MaxUint64), mulDivFloor(math.MaxUint64, math.MaxUint64, RatioDenom))
}

// TestSignalCannotOverflow checks that newSignal's two terms cannot carry.
func TestSignalCannotOverflow(t *testing.T) {
	maxU := uint64(math.MaxUint64)
	for _, gasUsed := range []uint64{0, 1, 21_000, 55_000_000, maxU / 2, maxU - 1, maxU} {
		for _, payment := range []uint64{0, 1, 21_000, 55_000_000, maxU / 2, maxU - 1, maxU} {
			for _, lane := range []uint64{0, 1, 55_000_000, maxU / 2, maxU} {
				sum, carry := bits.Add64(satSub(gasUsed, payment), satSub(payment, lane), 0)
				require.Zerof(t, carry, "gasUsed=%d payment=%d lane=%d: the terms carried", gasUsed, payment, lane)

				s := newSignal(Commitment{LaneSize: lane, PaymentGasUsed: payment}, gasUsed, 55_000_000)
				require.Equal(t, sum, s.parentSignalGasUsed)
			}
		}
	}
}

// TestSignalComparisonIsExactAt64BitOverflow checks the 128-bit comparison path.
func TestSignalComparisonIsExactAt64BitOverflow(t *testing.T) {
	gl := uint64(1) << 62 // a legal gas limit: the consensus cap is 2^63-1
	naive := uint64(8_000) * gl
	require.Zero(t, naive, "premise of this test: the naive product wraps")

	thr := new(big.Int).Mul(big.NewInt(8_000), new(big.Int).SetUint64(gl))
	thr.Add(thr, big.NewInt(RatioDenom-1))
	thr.Div(thr, big.NewInt(RatioDenom)) // ceil(0.8*gl)
	require.True(t, thr.IsUint64())
	at := thr.Uint64()

	require.False(t, gte128(at-1, RatioDenom, 8_000, gl))
	require.True(t, gte128(at, RatioDenom, 8_000, gl))

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

// TestTriggerComparisonBoundaries checks the exact trigger comparisons.
func TestTriggerComparisonBoundaries(t *testing.T) {
	p := defaultParams()
	const gl = 55_000_000
	step := mulDivFloor(p.ExpandStep, gl, RatioDenom)
	const base = uint64(3_000_000) // inside [floor, ceiling] at 55M, so only the step moves it
	shrunk := base - mulDivFloor(p.ShrinkStep, gl, RatioDenom)

	for _, tc := range []struct {
		bps  uint64
		want uint64
		why  string
	}{
		{p.ExpandTrigger, base + step, "at the expand trigger: expands, because the test is >="},
		{p.ExpandTrigger - 1, base, "one bp below: hysteresis band, holds"},
		{p.ShrinkTrigger, base, "at the shrink trigger: band, because the test is <"},
		{p.ShrinkTrigger - 1, shrunk, "one bp below: shrinks"},
		{0, shrunk, "an empty parent block shrinks"},
	} {
		s := Signal{parentLaneSize: base, parentSignalGasUsed: tc.bps, parentGasLimit: RatioDenom}
		require.Equal(t, tc.want, s.NextLaneSize(p, gl), tc.why)

		atRealGasLimit := Signal{parentLaneSize: base, parentSignalGasUsed: mulDivFloor(tc.bps, gl, RatioDenom), parentGasLimit: gl}
		require.Equal(t, tc.want, atRealGasLimit.NextLaneSize(p, gl), "%s (against a 55M parent)", tc.why)
	}
}

// TestStepSaturates keeps wrapped step updates from re-entering the valid range.
func TestStepSaturates(t *testing.T) {
	p := stepAboveFloorParams()
	require.True(t, contractLegal(p), "the premise is a tuple governance could set")

	const gl = 55_000_000
	step := mulDivFloor(p.ExpandStep, gl, RatioDenom)
	floor, ceiling := laneFloor(p, gl), laneCeiling(p, gl)
	require.Greater(t, step, floor, "premise: only then can a wrapped sum land in the window")

	target := floor + (ceiling-floor)/2
	prev := target - step // wraps around in uint64
	require.Greater(t, prev, ceiling, "premise: prev is far outside the window")
	require.Equal(t, target, prev+step, "premise: the naive sum lands inside the window")

	s := Signal{parentLaneSize: prev, parentSignalGasUsed: p.ExpandTrigger, parentGasLimit: RatioDenom}
	got := s.NextLaneSize(p, gl)
	require.Equal(t, ceiling, got, "satAdd must saturate and clamp to the ceiling, not wrap to %d", target)
	require.NotEqual(t, target, got)

	s = Signal{parentLaneSize: 0, parentSignalGasUsed: 0, parentGasLimit: RatioDenom}
	require.Equal(t, floor, s.NextLaneSize(p, gl), "shrinking from zero must not underflow")
}

// TestClampAppliesEveryBlock checks that the clamp runs on every block, not only on steps.
func TestClampAppliesEveryBlock(t *testing.T) {
	p := defaultParams()
	band := bandSignal(p)

	gl := uint64(70_000_000)
	lane := laneCeiling(p, gl)
	require.Equal(t, uint64(5_600_000), lane)

	seenClamped := false
	for i := 0; i < 1_500 && gl > 20_000_000; i++ {
		gl -= gl / params.GasLimitBoundDivisor
		band.parentLaneSize = lane
		lane = band.NextLaneSize(p, gl)

		require.LessOrEqual(t, lane, mulDivFloor(maxLaneRatio, gl, RatioDenom),
			"the quota's share of the block exceeded MAX_LANE_RATIO at gasLimit %d", gl)
		require.Equal(t, min(laneCeiling(p, gl), satSub(gl, params.SystemTxsGasHardLimit)), lane,
			"in the band only the ceiling and the safety clamp may move the quota, gasLimit %d", gl)
		if gl < 21_700_000 {
			seenClamped = true
			require.Equal(t, satSub(gl, params.SystemTxsGasHardLimit), lane,
				"below the crossover the safety clamp must be what sets the quota, gasLimit %d", gl)
		}
	}
	require.True(t, seenClamped, "the walk must reach the safety-clamp region")
}

// TestBootstrapIsTheZeroSignal checks that activation opens at the floor via the normal rule.
func TestBootstrapIsTheZeroSignal(t *testing.T) {
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			want := min(laneFloor(p, gl), satSub(gl, params.SystemTxsGasHardLimit))
			require.Equal(t, want, Signal{}.NextLaneSize(p, gl), "params %s gasLimit %d", p, gl)
		}
	}

	require.Equal(t, uint64(2_000_000), Signal{}.NextLaneSize(defaultParams(), 55_000_000))
}

// stepAboveFloorParams makes the bootstrap and saturation guards observable.
func stepAboveFloorParams() Params {
	p := Params{
		MinRatio: 1, MaxRatio: 501, ExpandTrigger: 8_000, ShrinkTrigger: 2_000,
		ExpandStep: 1_000, ShrinkStep: 50, MinGas: 21_000, MaxGas: 8_000_000,
	}
	return p
}

// TestNoHaltIsReachable checks that the safety clamp always leaves a producible block.
func TestNoHaltIsReachable(t *testing.T) {
	for _, p := range legalLattice() {
		for _, gl := range gasLimits() {
			got := Signal{}.NextLaneSize(p, gl)
			require.LessOrEqual(t, got, satSub(gl, params.SystemTxsGasHardLimit),
				"params %s gasLimit %d", p, gl)
		}
	}

	p := defaultParams()
	const small = 20_000_000
	unclamped := min(max(uint64(0), laneFloor(p, small)), laneCeiling(p, small))
	require.Greater(t, unclamped+params.SystemTxsGasHardLimit, uint64(small),
		"premise: at %d gas the unclamped quota plus the reservation exceeds the block", small)
	require.Zero(t, Signal{}.NextLaneSize(p, small), "the clamp must switch the lane off rather than halt the chain")

	for _, gl := range []uint64{35_000_000, 40_000_000, 55_000_000, 70_000_000} {
		require.Equal(t, laneFloor(p, gl), Signal{}.NextLaneSize(p, gl), "the safety clamp must not bind at gasLimit %d", gl)
	}
}

// TestTheRulesSkipTheActivationBlock checks that the activation block itself is exempt.
func TestTheRulesSkipTheActivationBlock(t *testing.T) {
	forkTime := uint64(1_800_000_000)
	config := *params.BSCChainConfig // copy: never mutate the shared mainnet config
	config.JennerTime = &forkTime

	require.NotNil(t, config.LondonBlock)
	base := config.LondonBlock.Uint64() + 1_000_000
	num := func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }

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
			require.Equal(t, tc.want, config.IsJenner(num(tc.parentNum), tc.parentTime))
		})
	}

	noJenner := *params.BSCChainConfig
	noJenner.JennerTime = nil
	require.False(t, noJenner.IsJenner(num(base), forkTime-3))

	require.False(t, config.IsJenner(common.Big0, forkTime),
		"below LondonBlock the lane must not apply")
}

// TestNewSignalFromParentDecidesTheBoundaryAndDenominator checks the boundary test and the
// parent-derived inputs.
func TestNewSignalFromParentDecidesTheBoundaryAndDenominator(t *testing.T) {
	// The boundary test rests on this: the sentinel and a commitment cannot collide.
	_, err := Decode(types.EmptyUncleHash)
	require.ErrorIs(t, err, ErrBadCommitment, "EmptyUncleHash must never decode as a commitment")

	hdr := func(uncleHash common.Hash, gasLimit, gasUsed uint64) *types.Header {
		return &types.Header{
			Number: big.NewInt(1_000_000), GasLimit: gasLimit, GasUsed: gasUsed, UncleHash: uncleHash,
		}
	}
	commitment := Encode(Commitment{LaneSize: 2_400_000, PaymentGasUsed: 1_000})

	t.Run("a parent that is a lane block yields its commitment", func(t *testing.T) {
		s, err := NewSignalFromParent(hdr(commitment, 40_000_000, 30_001_000))
		require.NoError(t, err)
		require.Equal(t, uint64(2_400_000), s.parentLaneSize)
		require.Equal(t, uint64(30_000_000), s.parentSignalGasUsed)
		require.Equal(t, uint64(40_000_000), s.parentGasLimit,
			"the denominator must be the PARENT's gas limit, never the child's")
	})

	t.Run("a parent outside the mechanism is the bootstrap seed", func(t *testing.T) {
		for name, parent := range map[string]*types.Header{
			"the activation block": hdr(types.EmptyUncleHash, 40_000_000, 30_000_000),
			"genesis":              {Number: common.Big0, GasLimit: 55_000_000, UncleHash: types.EmptyUncleHash},
		} {
			s, err := NewSignalFromParent(parent)
			require.NoError(t, err, name)
			require.Equal(t, Signal{}, s, name)
		}
	})

	t.Run("a corrupt commitment is an error, never a seed", func(t *testing.T) {
		corrupt := commitment
		corrupt[31] = 0xff
		s, err := NewSignalFromParent(hdr(corrupt, 40_000_000, 0))
		require.ErrorIs(t, err, ErrBadCommitment)
		require.Equal(t, Signal{}, s, "the zero value must not be usable as a seed after an error")
	})

	t.Run("a nil parent is an error", func(t *testing.T) {
		s, err := NewSignalFromParent(nil)
		require.ErrorIs(t, err, ErrBadCommitment)
		require.Equal(t, Signal{}, s)
	})
}
