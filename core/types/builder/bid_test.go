// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package builder

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestBidBlockArgsToDecodedBidBlockNormalizesNilSidecars(t *testing.T) {
	args := &BidBlockArgs{
		BidBlock: &BidBlock{
			Header: &types.Header{
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
	list := bal.NewConstructionBlockAccessList()
	list.NonceChange(common.Address{1}, 1, 1)
	encoded, err := rlp.EncodeToBytes(list.ToEncodingObj())
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(encoded)
	args := &BidBlockArgs{
		BidBlock: &BidBlock{
			BlockAccessList: encoded,
			Header: &types.Header{
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
	(*decoded.BlockAccessList)[0].NonceChanges[0].PostNonce = 2
	if !bytes.Equal(args.BidBlock.BlockAccessList, original) {
		t.Fatal("decoded BAL must not share data with the original BidBlock")
	}

	if args.BidBlock.Header.Number.Uint64() != 1 {
		t.Fatalf("original header number mutated: got %d, want 1", args.BidBlock.Header.Number.Uint64())
	}
	if args.BidBlock.Header.Extra[0] != 1 {
		t.Fatalf("original header extra mutated: got %d, want 1", args.BidBlock.Header.Extra[0])
	}
}

func TestBidBlockRLPAccessList(t *testing.T) {
	legacy := struct {
		Header       *types.Header
		Transactions []hexutil.Bytes
		Sidecars     types.BlobSidecars
	}{Header: &types.Header{Number: common.Big1, Difficulty: common.Big1}}
	encoded, err := rlp.EncodeToBytes(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var block BidBlock
	if err := rlp.DecodeBytes(encoded, &block); err != nil || block.BlockAccessList != nil {
		t.Fatalf("legacy BidBlock decode: %v", err)
	}
	roundtrip, err := rlp.EncodeToBytes(&block)
	if err != nil || !bytes.Equal(encoded, roundtrip) {
		t.Fatalf("legacy BidBlock encoding changed: %v", err)
	}
	list := bal.NewConstructionBlockAccessList()
	list.NonceChange(common.Address{1}, 1, 1)
	block.BlockAccessList, err = rlp.EncodeToBytes(list.ToEncodingObj())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BidBlock
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil || !bytes.Equal(decoded.BlockAccessList, block.BlockAccessList) {
		t.Fatalf("BidBlock BAL did not survive RLP roundtrip: %v", err)
	}
	args := &BidBlockArgs{BidBlock: &decoded}
	payload, err := args.ToDecodedBidBlock(common.Address{})
	if err != nil || payload.BlockAccessList == nil || payload.BlockAccessList.Hash() != list.ToEncodingObj().Hash() {
		t.Fatalf("BidBlock BAL did not survive decoding: %v", err)
	}
	args.BidBlock.BlockAccessList = []byte{0xff}
	if _, err := args.ToDecodedBidBlock(common.Address{}); err == nil {
		t.Fatal("accepted malformed BAL RLP")
	}
}
