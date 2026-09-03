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
		{PaymentLaneQuota: 1},
		{PaymentLaneQuota: 2_000_000, PaymentGasUsed: 1_500_000},
		{PaymentLaneQuota: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		got, err := Decode(Encode(c))
		require.NoError(t, err)
		require.Equal(t, c, got)
	}

	// Check field order directly so a symmetric round trip cannot hide a swap.
	h := Encode(Commitment{PaymentLaneQuota: 0x0102030405060708, PaymentGasUsed: 0x1112131415161718})
	require.Equal(t, common.HexToHash("0x01020304050607081112131415161718"+strings.Repeat("00", 16)), h)
}

// Zero is a legal commitment and still verifies only where the derived quota is zero.
func TestTheAllZeroCommitmentIsLegal(t *testing.T) {
	require.Equal(t, common.Hash{}, Encode(Commitment{}))

	got, err := Decode(common.Hash{})
	require.NoError(t, err, "the all-zero commitment must decode, not fail")
	require.Equal(t, Commitment{}, got)
	require.False(t, (&types.Header{}).IsEmptyUncleHash(), "the default uncle-root helpers stay exact")
	require.True(t, (&types.Header{}).BEP703CommitsNoUncles(), "BEP-703 explicitly treats the all-zero commitment as no uncles")

	// Zero quota still has to match the derived quota.
	p := defaultGovernanceParams()
	require.ErrorIs(t, Signal{}.CheckNextLaneQuota(0, p, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, Signal{}.CheckNextLaneQuota(0, p, params.SystemTxsGasHardLimit))
}

// Reserved bytes are the framing; every non-zero bit must fail.
func TestDecodeRejectsMalformed(t *testing.T) {
	valid := Encode(Commitment{PaymentLaneQuota: 4_400_000, PaymentGasUsed: 1_000_000})

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

// Keep the BEP-703 tag in step with Decode. EmptyUncleHash stays the separate legacy encoding.
func TestBEP703CommitmentTagAgreesWithDecode(t *testing.T) {
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
				require.Equal(t, err == nil, (&types.Header{UncleHash: h}).BEP703CommitsNoUncles(),
					"reserved byte %d = %#x on payload %x", i, b, h[:16])
			}
		}
	}

	// Real commitments are tagged, including the all-zero one.
	for _, c := range []Commitment{
		{},
		{PaymentLaneQuota: 2_000_000},
		{PaymentLaneQuota: 4_400_000, PaymentGasUsed: 1},
		{PaymentLaneQuota: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		encoded := Encode(c)
		header := &types.Header{UncleHash: encoded}
		require.False(t, header.IsEmptyUncleHash(), "%x", encoded)
		require.True(t, header.BEP703CommitsNoUncles(), "%x", encoded)
	}

	// EmptyUncleHash stays tagged as "no uncles", not a lane commitment.
	_, err := Decode(types.EmptyUncleHash)
	require.ErrorIs(t, err, ErrBadCommitment)
	require.True(t, (&types.Header{UncleHash: types.EmptyUncleHash}).IsEmptyUncleHash())
	require.True(t, (&types.Header{UncleHash: types.EmptyUncleHash}).BEP703CommitsNoUncles())

	// A real uncle hash is neither.
	uncles := types.CalcUncleHash([]*types.Header{{Number: common.Big1}})
	require.False(t, (&types.Header{UncleHash: uncles}).IsEmptyUncleHash())
	require.False(t, (&types.Header{UncleHash: uncles}).BEP703CommitsNoUncles())
}

// Accept producer-reachable commitments and reject only forged ones.
func TestCheckHeaderBoundsRejectsOnlyForgeries(t *testing.T) {
	const gasUsed, gasLimit = 3_000_000, 55_000_000

	// Producer-reachable shapes.
	for _, c := range []Commitment{
		{},
		{PaymentLaneQuota: 2_000_000, PaymentGasUsed: 900_000},
		{PaymentLaneQuota: 2_000_000, PaymentGasUsed: gasUsed},
		{PaymentLaneQuota: gasLimit - gasUsed, PaymentGasUsed: 0}, // the rule at exact equality
	} {
		require.NoErrorf(t, c.CheckHeaderBounds(gasUsed, gasLimit), "reachable commitment %+v", c)
	}

	// Each check gets its own witness.
	require.ErrorIs(t, Commitment{PaymentGasUsed: gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrUntruthy)
	require.ErrorIs(t, Commitment{PaymentLaneQuota: gasLimit + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated)
	// The rule itself: both bounds pass, and the sum still bursts the block by one gas.
	require.ErrorIs(t,
		Commitment{PaymentLaneQuota: gasLimit - gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated,
		"the accounting rule must be evaluated here, not deferred to execution")
}
