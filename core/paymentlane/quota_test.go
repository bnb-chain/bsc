package paymentlane

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuotaIsTheRatioOfTheGasLimit pins BEP-703 3.4.1's arithmetic, including the 73-bit product
// the spec calls out: a 64-bit intermediate wraps there and hands back a quota nobody else derives.
func TestQuotaIsTheRatioOfTheGasLimit(t *testing.T) {
	for _, tc := range []struct {
		ratio, gasLimit, want uint64
		why                   string
	}{
		{500, 55_000_000, 2_750_000, "3.4.4's worked example: the default 5% of mainnet's gas limit"},
		{1_000, 55_000_000, 5_500_000, "the maximum ratio is 10%"},
		{1, 55_000_000, 5_500, "the smallest settable ratio"},
		{500, 0, 0, "no gas limit, no reservation"},
		{500, 30_000_001, 1_500_000, "truncates toward zero rather than rounding"},
		{1_000, math.MaxInt64, 922_337_203_685_477_580, "the product needs 73 bits and must not wrap"},
	} {
		require.Equalf(t, tc.want, Quota(tc.ratio, tc.gasLimit), "Quota(%d, %d): %s", tc.ratio, tc.gasLimit, tc.why)

		// The same value an arbitrary-precision reference computes.
		ref := new(big.Int).Mul(new(big.Int).SetUint64(tc.ratio), new(big.Int).SetUint64(tc.gasLimit))
		require.Equal(t, ref.Div(ref, big.NewInt(RatioDenom)).Uint64(), Quota(tc.ratio, tc.gasLimit))
	}
}

// TestCheckRatioGuardsAtFullWidth is 3.6.1's guard: it must reject on the uint256 the getter
// returned, never on a value narrowed to 64 bits first.
func TestCheckRatioGuardsAtFullWidth(t *testing.T) {
	for _, ok := range []uint64{1, 500, MaxLaneRatio} {
		got, err := CheckRatio(new(big.Int).SetUint64(ok))
		require.NoError(t, err)
		require.Equal(t, ok, got)
	}

	// 2^64 + 500 narrows to 500, which is inside the guard; at full width it is not.
	wraps := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(500))
	for _, bad := range []*big.Int{nil, big.NewInt(0), big.NewInt(MaxLaneRatio + 1), wraps} {
		_, err := CheckRatio(bad)
		require.ErrorIsf(t, err, ErrCorruptConfig, "ratio %v must fail the guard", bad)
	}
}
