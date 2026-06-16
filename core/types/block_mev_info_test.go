// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package types

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBlockMevInfoEncodeDecode(t *testing.T) {
	builder := common.HexToAddress("0x317aB60A0815F8Db2e6cb3f302C152d2A5ef4854")

	// Round-trip: both versions encode then decode back to the same (version, builder).
	t.Run("round-trip", func(t *testing.T) {
		for _, v := range []BlockMevInfoVersion{BlockMevInfoVersionBid, BlockMevInfoVersionBidBlock} {
			gotV, gotB, ok := DecodeBlockMevInfo(EncodeBlockMevInfo(v, builder))
			if !ok || gotV != v || gotB != builder {
				t.Fatalf("version %d: got (v=%d, builder=%s, ok=%v), want (%d, %s, true)",
					v, gotV, gotB.Hex(), ok, v, builder.Hex())
			}
		}
	})

	// Invalid encodings must decode to ok=false (caller treats as a local/untagged block).
	t.Run("invalid->ok=false", func(t *testing.T) {
		nonzeroSentinel := EncodeBlockMevInfo(BlockMevInfoVersionBidBlock, builder)
		nonzeroSentinel[0] = 1 // leading sentinel bytes [0:11] must all be zero

		versionTooHigh := EncodeBlockMevInfo(BlockMevInfoVersionBidBlock, builder)
		versionTooHigh[blockMevInfoVersionOffset] = 3 // not 1 or 2

		versionZero := EncodeBlockMevInfo(BlockMevInfoVersionBidBlock, builder)
		versionZero[blockMevInfoVersionOffset] = 0

		cases := map[string]common.Hash{
			"nonzero sentinel": nonzeroSentinel,
			"version 3":        versionTooHigh,
			"version 0":        versionZero,
			"zero builder":     EncodeBlockMevInfo(BlockMevInfoVersionBidBlock, common.Address{}),
			"empty hash":       {},
		}
		for name, h := range cases {
			if _, _, ok := DecodeBlockMevInfo(h); ok {
				t.Errorf("%s: ok=true, want false", name)
			}
		}
	})
}
