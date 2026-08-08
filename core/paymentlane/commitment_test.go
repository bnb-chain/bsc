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

	// The field layout itself, so swapping the two uint64s is caught rather than hidden
	// by a symmetric round trip.
	h := Encode(Commitment{LaneSize: 0x0102030405060708, PaymentGasUsed: 0x1112131415161718})
	require.Equal(t, common.HexToHash("0x01020304050607081112131415161718"+strings.Repeat("00", 16)), h)
}

// TestTheAllZeroCommitmentIsLegal records a deliberate reversal.
//
// An earlier encoding carried a version byte for the sole purpose of keeping Encode's
// range clear of the zero hash, on the theory that a 32-byte carrier might read zero as
// "never written". Two things retire that: the reserved tail is now the framing test, so
// the zero hash is recognised as a commitment rather than mistaken for an empty field;
// and nothing accepts a wrong quota anyway, because CheckNextLaneSize compares the committed
// value against the one derived from the parent. The version byte protected only what
// that comparison already protects, at the cost of sitting inside the sixteen bytes the
// BEP requires to be zero.
//
// Reachable, not hypothetical: laneSize is zero exactly when GasLimit is at or below
// SystemTxsGasHardLimit, which is every chain left at params.GenesisGasLimit.
func TestTheAllZeroCommitmentIsLegal(t *testing.T) {
	require.Equal(t, common.Hash{}, Encode(Commitment{}))

	got, err := Decode(common.Hash{})
	require.NoError(t, err, "the all-zero commitment must decode, not fail")
	require.Equal(t, Commitment{}, got)
	require.True(t, (&types.Header{}).IsEmptyUncleHash(), "and it must be tagged as a commitment")

	// It is still not a licence to omit the field: a zero quota only verifies where the
	// derivation says zero.
	p := defaultParams()
	require.ErrorIs(t, Signal{}.CheckNextLaneSize(0, p, 55_000_000), ErrQuotaMismatch)
	require.NoError(t, Signal{}.CheckNextLaneSize(0, p, params.SystemTxsGasHardLimit))
}

// TestDecodeRejectsMalformed walks every reserved byte over every bit, so the framing
// check cannot be weakened silently.
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
		// This is the one value the framing test has to exclude: a header still carrying
		// EmptyUncleHash must not read as lane accounting. It ends in bytes that are not
		// zero, which is exactly why the reserved tail can be the whole test.
		_, err := Decode(types.EmptyUncleHash)
		require.ErrorIs(t, err, ErrBadCommitment)
		require.NotEqual(t, [16]byte{}, [16]byte(types.EmptyUncleHash[16:]),
			"EmptyUncleHash's tail must stay non-zero, or it would decode as a commitment")
	})
}

// TestBootstrapIsNeverInferredFromADecodeFailure records the rule the activation
// semantics depend on: "the parent carries no commitment" must be decided by the caller
// from the fork state, never inferred from Decode.
//
// The reason is sharper than it used to be. Decode now accepts the all-zero value, so a
// failure no longer even coincides with "nothing was written" - it means corruption and
// only corruption. Treating it as a bootstrap seed would silently reset the quota to the
// floor on a corrupt parent instead of rejecting the block.
func TestBootstrapIsNeverInferredFromADecodeFailure(t *testing.T) {
	// The bootstrap seed comes from an explicit nil and nothing else.
	require.Equal(t, Signal{}, newSignal(nil, 0, 55_000_000))

	corrupt := Encode(Commitment{LaneSize: 999})
	corrupt[31] = 0xff
	_, err := Decode(corrupt)
	require.ErrorIs(t, err, ErrBadCommitment)
}

// TestLaneCommitmentTagAgreesWithDecode is the bridge that keeps core/types' framing
// test and Decode from drifting apart.
//
// The two now test the same condition - the reserved tail - in two packages, because
// core/types cannot import this one. Equivalence therefore holds by construction, and
// this test is what turns "by construction" into something a future edit cannot break:
// if either side gains a check the other lacks, the failure is otherwise asymmetric and
// silent. A tag that is too narrow makes the body and propagation layers quietly drop
// every lane block; one that is too wide lets a header whose commitment Decode rejects
// still pass as "claims no uncles".
func TestLaneCommitmentTagAgreesWithDecode(t *testing.T) {
	// Both directions, over the framing bytes exhaustively and over random payloads that
	// neither side may look at.
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

	// Real commitments are tagged, including the reachable all-zero one.
	for _, c := range []Commitment{
		{},
		{LaneSize: 2_000_000},
		{LaneSize: 4_400_000, PaymentGasUsed: 1},
		{LaneSize: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		encoded := Encode(c)
		require.True(t, (&types.Header{UncleHash: encoded}).IsEmptyUncleHash(), "%x", encoded)
	}

	// The carrier's own empty value must not read as a commitment, or a pre-activation
	// header would decode as lane accounting - while still claiming no uncles, which it
	// does by plain equality.
	_, err := Decode(types.EmptyUncleHash)
	require.ErrorIs(t, err, ErrBadCommitment)
	require.True(t, (&types.Header{UncleHash: types.EmptyUncleHash}).IsEmptyUncleHash())

	// A real uncle list hash is neither, and the relaxation must not reach it.
	uncles := types.CalcUncleHash([]*types.Header{{Number: common.Big1}})
	require.False(t, (&types.Header{UncleHash: uncles}).IsEmptyUncleHash())
	require.False(t, types.UncleHashMatches(Encode(Commitment{LaneSize: 1}), uncles))
	require.True(t, types.UncleHashMatches(uncles, uncles))
}

// TestCheckHeaderBoundsRejectsOnlyForgeries pins both directions: a commitment a
// correct producer could have made must pass, and the two absurd ones must not.
func TestCheckHeaderBoundsRejectsOnlyForgeries(t *testing.T) {
	const gasUsed, gasLimit = 3_000_000, 55_000_000

	// Shapes a correct producer can reach: payment is a part of the block's gas, and the
	// rule holds with the idle quota on top.
	for _, c := range []Commitment{
		{},
		{LaneSize: 2_000_000, PaymentGasUsed: 900_000},
		{LaneSize: 2_000_000, PaymentGasUsed: gasUsed},
		{LaneSize: gasLimit - gasUsed, PaymentGasUsed: 0}, // the rule at exact equality
	} {
		require.NoErrorf(t, c.CheckHeaderBounds(gasUsed, gasLimit), "reachable commitment %+v", c)
	}

	// Each of the three checks, and each on its own witness.
	require.ErrorIs(t, Commitment{PaymentGasUsed: gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrUntruthy)
	require.ErrorIs(t, Commitment{LaneSize: gasLimit + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated)
	// The rule itself: both bounds pass, and the sum still bursts the block by one gas.
	require.ErrorIs(t,
		Commitment{LaneSize: gasLimit - gasUsed + 1}.CheckHeaderBounds(gasUsed, gasLimit), ErrViolated,
		"the accounting rule must be evaluated here, not deferred to execution")
}

// TestTheGasLimitBoundOnlyChangesTheDiagnosis explains a mutation that survives on
// purpose, so nobody spends an afternoon looking for the missing test.
//
// Deleting the LaneSize <= gasLimit check from CheckHeaderBounds cannot change any
// verdict: given payment <= gasUsed (the check above it) and the accounting rule, an
// oversized quota already violates the rule. Both cases:
//
//	payment >= lane: lane <= payment <= gasUsed <= gasLimit
//	payment <  lane: lane - payment <= gasLimit - gasUsed, and payment <= gasUsed,
//	                 so lane <= gasLimit - gasUsed + payment <= gasLimit
//
// It stays because section 3.5.4 lists it, and because "committed lane size 60000000
// exceeds header gas limit 55000000" is an actionable log line where the generic
// inequality violation is not. This test is what keeps the claim honest: if a future
// edit makes the bound load-bearing, the exhaustive sweep below stops agreeing.
func TestTheGasLimitBoundOnlyChangesTheDiagnosis(t *testing.T) {
	const gasLimit = 24
	for gasUsed := uint64(0); gasUsed <= gasLimit+4; gasUsed++ {
		for payment := uint64(0); payment <= gasLimit+4; payment++ {
			for lane := uint64(0); lane <= gasLimit+4; lane++ {
				c := Commitment{LaneSize: lane, PaymentGasUsed: payment}
				withBound := c.CheckHeaderBounds(gasUsed, gasLimit)

				// The same function with the bound removed.
				var withoutBound error
				if c.PaymentGasUsed > gasUsed {
					withoutBound = ErrUntruthy
				} else {
					withoutBound = CheckInequality(gasLimit, gasUsed, c.PaymentGasUsed, c.LaneSize)
				}

				require.Equalf(t, withoutBound == nil, withBound == nil,
					"gasUsed=%d payment=%d lane=%d: the bound changed the verdict, not just the message",
					gasUsed, payment, lane)
			}
		}
	}
}
