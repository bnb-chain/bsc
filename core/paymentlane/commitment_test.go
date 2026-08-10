package paymentlane

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestCommitmentRoundTrip(t *testing.T) {
	for _, c := range []Commitment{
		{},
		{LaneSize: 1},
		{LaneSize: 2_000_000, PaymentGasUsed: 1_500_000},
		{LaneSize: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		got, err := Decode(Encode(c))
		require.NoError(t, err)
		require.Equal(t, c, got)
	}

	// Check field order directly so a symmetric round trip cannot hide a swap.
	h := Encode(Commitment{LaneSize: 0x0102030405060708, PaymentGasUsed: 0x1112131415161718})
	require.Equal(t, common.HexToHash("0x01020304050607081112131415161718"+strings.Repeat("00", 16)), h)
}

// Zero is a legal commitment and still verifies only where the derived quota is zero.
func TestTheAllZeroCommitmentIsLegal(t *testing.T) {
	require.Equal(t, common.Hash{}, Encode(Commitment{}))

	got, err := Decode(common.Hash{})
	require.NoError(t, err, "the all-zero commitment must decode, not fail")
	require.Equal(t, Commitment{}, got)
	require.True(t, (&types.Header{}).IsEmptyUncleHash(), "and it must be tagged as a commitment")

	// Zero quota still has to match the derived quota.
	p := defaultParams()
	require.ErrorIs(t, Signal{}.CheckNextLaneSize(0, p, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, Signal{}.CheckNextLaneSize(0, p, params.SystemTxsGasHardLimit))
}

// Reserved bytes are the framing; every non-zero bit must fail.
func TestDecodeRejectsMalformed(t *testing.T) {
	valid := Encode(Commitment{LaneSize: 4_400_000, PaymentGasUsed: 1_000_000})

	t.Run("every reserved bit must be rejected", func(t *testing.T) {
		for i := 16; i < 32; i++ {
			for bit := 0; bit < 8; bit++ {
				h := valid
				h[i] = 1 << bit
				_, err := Decode(h)
				require.ErrorIs(t, err, ErrBadCommitment, "reserved byte %d bit %d must be rejected", i, bit)
			}
		}
	})

	t.Run("the pre-activation empty-list hash is rejected", func(t *testing.T) {
		// EmptyUncleHash must stay outside the commitment encoding.
		_, err := Decode(types.EmptyUncleHash)
		require.ErrorIs(t, err, ErrBadCommitment)
		require.NotEqual(t, [16]byte{}, [16]byte(types.EmptyUncleHash[16:]),
			"EmptyUncleHash's tail must stay non-zero, or it would decode as a commitment")
	})
}

// Keep types.Header.IsEmptyUncleHash and Decode on the same framing rule.
func TestLaneCommitmentTagAgreesWithDecode(t *testing.T) {
	// Check both directions across reserved bytes and random payloads.
	var h common.Hash
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 64; round++ {
		rng.Read(h[:16])
		for i := 16; i < 32; i++ {
			for _, b := range []byte{0, 1, 0xff} {
				for j := 16; j < 32; j++ {
					h[j] = 0
				}
				h[i] = b
				_, err := Decode(h)
				require.Equal(t, err == nil, (&types.Header{UncleHash: h}).IsEmptyUncleHash(),
					"reserved byte %d = %#x on payload %x", i, b, h[:16])
			}
		}
	}

	// Real commitments are tagged, including the all-zero one.
	for _, c := range []Commitment{
		{},
		{LaneSize: 2_000_000},
		{LaneSize: 4_400_000, PaymentGasUsed: 1},
		{LaneSize: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		encoded := Encode(c)
		require.True(t, (&types.Header{UncleHash: encoded}).IsEmptyUncleHash(), "%x", encoded)
	}

	// EmptyUncleHash stays tagged as "no uncles", not a lane commitment.
	_, err := Decode(types.EmptyUncleHash)
	require.ErrorIs(t, err, ErrBadCommitment)
	require.True(t, (&types.Header{UncleHash: types.EmptyUncleHash}).IsEmptyUncleHash())

	// A real uncle hash is neither.
	uncles := types.CalcUncleHash([]*types.Header{{Number: common.Big1}})
	require.False(t, (&types.Header{UncleHash: uncles}).IsEmptyUncleHash())
	require.False(t, (&types.Header{UncleHash: Encode(Commitment{LaneSize: 1})}).UncleHashMatches(uncles))
	require.True(t, (&types.Header{UncleHash: uncles}).UncleHashMatches(uncles))
}

// Accept producer-reachable commitments and reject only forged ones.
func TestCheckHeaderBoundsRejectsOnlyForgeries(t *testing.T) {
	const gasUsed, gasLimit = 3_000_000, 55_000_000

	// Producer-reachable shapes.
	for _, c := range []Commitment{
		{},
		{LaneSize: 2_000_000, PaymentGasUsed: 900_000},
		{LaneSize: 2_000_000, PaymentGasUsed: gasUsed},
		{LaneSize: gasLimit - gasUsed, PaymentGasUsed: 0}, // the rule at exact equality
	} {
		require.NoErrorf(t, c.CheckHeaderBounds(gasUsed, gasLimit), "reachable commitment %+v", c)
	}

	// Each check gets its own witness.
	require.ErrorIs(t, Commitment{PaymentGasUsed: gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrUntruthy)
	require.ErrorIs(t, Commitment{LaneSize: gasLimit + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated)
	// The rule itself: both bounds pass, and the sum still bursts the block by one gas.
	require.ErrorIs(t,
		Commitment{LaneSize: gasLimit - gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated,
		"the accounting rule must be evaluated here, not deferred to execution")
}
