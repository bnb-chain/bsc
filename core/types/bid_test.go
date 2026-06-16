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

func TestBidBlockArgsToDecodedBidBlockCopiesHeader(t *testing.T) {
	args := &BidBlockArgs{
		BidBlock: &BidBlock{
			Header: &Header{
				Difficulty: big.NewInt(1),
				Number:     big.NewInt(1),
				Extra:      []byte{1, 2, 3},
			},
		},
	}

	decoded, err := args.ToDecodedBidBlock(common.Address{0x1})
	if err != nil {
		t.Fatalf("ToDecodedBidBlock failed: %v", err)
	}
	if decoded.Header == args.BidBlock.Header {
		t.Fatal("decoded BidBlock header must not share the original header pointer")
	}

	decoded.Header.Number.SetUint64(2)
	decoded.Header.Extra[0] = 9

	if args.BidBlock.Header.Number.Uint64() != 1 {
		t.Fatalf("original header number mutated: got %d, want 1", args.BidBlock.Header.Number.Uint64())
	}
	if args.BidBlock.Header.Extra[0] != 1 {
		t.Fatalf("original header extra mutated: got %d, want 1", args.BidBlock.Header.Extra[0])
	}
}
