// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
)

// IsRejectedBidBlock is the attribution signal behind escalating builder
// lockouts: it must answer "the chain judged this block invalid", not "an
// import failed". Only reportBadBlock feeds it, so a block that never reached
// validation must not show up here.

func testBidBlock(t *testing.T, number uint64, builder common.Address) *types.Block {
	t.Helper()
	tag := buildertypes.EncodeBlockMevInfo(buildertypes.BlockMevInfoVersionBidBlock, builder)
	return types.NewBlockWithHeader(&types.Header{
		Number:       new(big.Int).SetUint64(number),
		RequestsHash: &tag,
	})
}

func TestIsRejectedBidBlockOnlyAfterRejection(t *testing.T) {
	bc := &BlockChain{}
	builder := common.HexToAddress("0xb0")
	block := testBidBlock(t, 1_000_001, builder)

	if bc.IsRejectedBidBlock(block.Hash()) {
		t.Fatal("a block the chain never rejected must not be reported as bad")
	}

	// countBadBidBlock is what reportBadBlock calls once validation has failed.
	countBadBidBlock(block)

	if !bc.IsRejectedBidBlock(block.Hash()) {
		t.Fatal("a rejected BidBlock must be reported as bad")
	}
}

// A block without the BEP-675 tag is not a BidBlock, so it never becomes an
// attribution signal even when the chain rejects it.
func TestIsRejectedBidBlockIgnoresUntaggedBlocks(t *testing.T) {
	bc := &BlockChain{}
	block := types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1_000_002)})

	countBadBidBlock(block)

	if bc.IsRejectedBidBlock(block.Hash()) {
		t.Fatal("an untagged bad block must not be attributed to a builder")
	}
}
