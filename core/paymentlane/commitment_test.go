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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestCommitmentRoundTrip(t *testing.T) {
	for _, c := range []Commitment{
		{},
		{LaneSize: 1},
		{LaneSize: 2_000_000, GeneralGasUsed: 44_000_000, PaymentGasUsed: 1_500_000},
		{LaneSize: math.MaxUint64, GeneralGasUsed: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		got, err := Decode(Encode(c))
		require.NoError(t, err)
		require.Equal(t, c, got)
	}

	// The field layout itself, so a reordering of the three uint64s is caught rather
	// than hidden by a symmetric round trip.
	h := Encode(Commitment{LaneSize: 0x0102030405060708, GeneralGasUsed: 0x1112131415161718, PaymentGasUsed: 0x2122232425262728})
	require.Equal(t, common.HexToHash("0x0102030405060708111213141516171821222324252627280100000000000000"), h)
}

// TestEncodeNeverProducesASentinelValue is why the version byte exists.
//
// Commitment{0,0,0} - an empty block with a zero quota, the most common shape right
// after activation - would otherwise encode to the zero hash, which any plausible
// 32-byte carrier treats as "the caller never wrote this field". The failure mode is
// a block that seals locally and is rejected network-wide, with neither the header
// nor the body checks able to point at a cause.
//
// EmptyUncleHash is included because it is the concrete sentinel of the carrier
// currently under consideration; the property is that Encode's range excludes every
// value a carrier might use for "unset", not just the zero hash.
func TestEncodeNeverProducesASentinelValue(t *testing.T) {
	require.NotEqual(t, common.Hash{}, Encode(Commitment{}))
	require.NotEqual(t, types.EmptyUncleHash, Encode(Commitment{}))

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 10_000; i++ {
		c := Commitment{LaneSize: rng.Uint64(), GeneralGasUsed: rng.Uint64(), PaymentGasUsed: rng.Uint64()}
		h := Encode(c)
		require.NotEqual(t, common.Hash{}, h)
		require.NotEqual(t, types.EmptyUncleHash, h)
	}
}

// TestDecodeRejectsMalformed walks the version byte over all 256 values and every
// reserved byte over every bit, so the two checks cannot be weakened silently.
func TestDecodeRejectsMalformed(t *testing.T) {
	valid := Encode(Commitment{LaneSize: 4_400_000, GeneralGasUsed: 44_000_000, PaymentGasUsed: 1_000_000})

	t.Run("the version byte must be exact", func(t *testing.T) {
		for v := 0; v < 256; v++ {
			h := valid
			h[24] = byte(v)
			_, err := Decode(h)
			if byte(v) == commitVersion {
				require.NoError(t, err)
				continue
			}
			require.ErrorIs(t, err, ErrBadCommitment, "version %d must be rejected", v)
		}
	})

	t.Run("every reserved bit must be rejected", func(t *testing.T) {
		for i := 25; i < 32; i++ {
			for bit := 0; bit < 8; bit++ {
				h := valid
				h[i] = 1 << bit
				_, err := Decode(h)
				require.ErrorIs(t, err, ErrBadCommitment, "reserved byte %d bit %d must be rejected", i, bit)
			}
		}
	})

	t.Run("the sentinels are rejected", func(t *testing.T) {
		// Both must fail, and for the version byte rather than by accident: this is
		// what makes "the carrier overwrote our field" a deterministic, diagnosable
		// failure instead of a strange accounting drift.
		_, err := Decode(common.Hash{})
		require.ErrorIs(t, err, ErrBadCommitment)
		_, err = Decode(types.EmptyUncleHash)
		require.ErrorIs(t, err, ErrBadCommitment)
		require.NotEqual(t, commitVersion, types.EmptyUncleHash[24],
			"EmptyUncleHash must not collide with the version byte, or a carrier overwrite would decode as valid")
	})
}

// TestDecodeIsTheOnlyBootstrapDiscriminator records the rule the activation
// semantics depend on: "the parent carries no commitment" must be decided by the
// caller, never inferred from a decode failure, because a failure cannot tell a
// legitimate bootstrap apart from a corrupt commitment.
func TestDecodeIsTheOnlyBootstrapDiscriminator(t *testing.T) {
	// Encode can never produce the sentinel, so "sentinel present" and "valid
	// commitment present" are disjoint - which is what lets a caller distinguish the
	// two at depth 1, using only the parent header.
	_, err := Decode(types.EmptyUncleHash)
	require.Error(t, err)

	// And a corrupt commitment is a hard error, not a seed. newSignal only produces
	// the bootstrap signal from an explicit nil.
	require.Equal(t, Signal{}, newSignal(nil, 55_000_000))
	corrupt := Encode(Commitment{LaneSize: 999})
	corrupt[24] = 0xff
	_, err = Decode(corrupt)
	require.ErrorIs(t, err, ErrBadCommitment)
}

// TestLaneCommitmentTagAgreesWithDecode is the bridge that keeps core/types' framing
// test and Decode from drifting apart.
//
// The two implement the same framing test in two packages, because core/types cannot
// import this one - and if they ever disagree the failure is asymmetric and silent:
// a tag that is too narrow makes the body and propagation layers reject or quietly
// drop every lane block (eth/protocols/eth logs it as an uncle problem), while a tag
// that is too wide lets a header whose commitment Decode rejects still pass as
// "claims no uncles". Neither shows up as a test failure anywhere else.
func TestLaneCommitmentTagAgreesWithDecode(t *testing.T) {
	// Equivalence over the framing bytes, exhaustively in the version byte and over
	// every reserved position, against a fixed non-zero payload that neither side may
	// look at.
	var h common.Hash
	rand.New(rand.NewSource(1)).Read(h[:])
	for v := 0; v < 256; v++ {
		h[24] = byte(v)
		for i := 25; i < 32; i++ {
			for _, b := range []byte{0, 1, 0xff} {
				h[i] = b
				_, err := Decode(h)
				// h is never EmptyUncleHash here (the payload below is a fixed
				// non-zero fill), which is what makes ClaimsNoUncles a faithful
				// probe for the framing test rather than for the empty carrier.
				require.NotEqual(t, types.EmptyUncleHash, h)
				require.Equal(t, err == nil, (&types.Header{UncleHash: h}).ClaimsNoUncles(),
					"version %d, reserved byte %d = %#x", v, i, b)
			}
			h[i] = 0
		}
	}

	// Real commitments are tagged, and so is the reachable all-zero one.
	for _, c := range []Commitment{
		{},
		{LaneSize: 2_000_000},
		{LaneSize: 4_400_000, GeneralGasUsed: 49_600_000, PaymentGasUsed: 1},
		{LaneSize: math.MaxUint64, GeneralGasUsed: math.MaxUint64, PaymentGasUsed: math.MaxUint64},
	} {
		encoded := Encode(c)
		require.True(t, (&types.Header{UncleHash: encoded}).ClaimsNoUncles(), "%x", encoded)
	}

	// The carrier's own empty value must not read as a commitment, or a pre-activation
	// header would decode as lane accounting.
	_, err := Decode(types.EmptyUncleHash)
	require.ErrorIs(t, err, ErrBadCommitment)
	require.True(t, (&types.Header{UncleHash: types.EmptyUncleHash}).ClaimsNoUncles())

	// A real uncle list hash is neither, and the relaxation must not reach it: the
	// body's uncle hash is non-empty, so only exact equality can match.
	uncles := types.CalcUncleHash([]*types.Header{{Number: common.Big1}})
	require.False(t, (&types.Header{UncleHash: uncles}).ClaimsNoUncles())
	require.False(t, types.UncleHashMatches(Encode(Commitment{}), uncles))
	require.True(t, types.UncleHashMatches(uncles, uncles))
}
