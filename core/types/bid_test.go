// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBidBlockArgsToDecodedBidBlockNormalizesNilSidecars(t *testing.T) {
	args := &BidBlockArgs{
		BidBlock: &BidBlock{
			Header: &Header{
				Difficulty: big.NewInt(1),
				Number:     big.NewInt(1),
				Extra:      make([]byte, 32),
			},
		},
	}

	decoded, err := args.ToDecodedBidBlock(common.Address{0x1})
	if err != nil {
		t.Fatalf("ToDecodedBidBlock failed: %v", err)
	}
	if decoded.Sidecars == nil {
		t.Fatal("nil sidecars should be normalized to an empty slice")
	}
	if len(decoded.Sidecars) != 0 {
		t.Fatalf("sidecars length mismatch: got %d, want 0", len(decoded.Sidecars))
	}
}
