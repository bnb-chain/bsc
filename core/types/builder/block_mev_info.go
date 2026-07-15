// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BEP-675 block-source tagging. Validators encode the winning MEV path and
// builder address into header.RequestsHash; local blocks keep EmptyRequestsHash.

package builder

import (
	"github.com/ethereum/go-ethereum/common"
)

const blockMevInfoVersionOffset = common.HashLength - common.AddressLength - 1 // 11

// BlockMevInfoVersion identifies which submission path produced a block.
// Stored in header.RequestsHash at blockMevInfoVersionOffset.
type BlockMevInfoVersion uint8

const (
	// BlockMevInfoVersionBid identifies blocks produced via legacy SendBid.
	BlockMevInfoVersionBid BlockMevInfoVersion = 1
	// BlockMevInfoVersionBidBlock identifies blocks produced via BEP-675 SendBidBlock.
	BlockMevInfoVersionBidBlock BlockMevInfoVersion = 2
)

// EncodeBlockMevInfo packs (version, builder) into a 32-byte hash suitable for
// header.RequestsHash. Layout:
//
//	[0:blockMevInfoVersionOffset]   = 0       (leading-zero sentinel)
//	[blockMevInfoVersionOffset]     = version (1 = bid, 2 = bidblock)
//	[blockMevInfoVersionOffset+1:]  = builder (20-byte address)
//
// Local blocks must NOT use this encoding; they keep the default
// EmptyRequestsHash so callers can rely on "untagged" == local.
func EncodeBlockMevInfo(version BlockMevInfoVersion, builder common.Address) common.Hash {
	var h common.Hash
	h[blockMevInfoVersionOffset] = byte(version)
	copy(h[blockMevInfoVersionOffset+1:], builder[:])
	return h
}

// DecodeBlockMevInfo recovers the MEV source and builder from h.
// ok=false means callers should treat the block as local.
func DecodeBlockMevInfo(h common.Hash) (version BlockMevInfoVersion, builder common.Address, ok bool) {
	for i := 0; i < blockMevInfoVersionOffset; i++ {
		if h[i] != 0 {
			return 0, common.Address{}, false
		}
	}
	v := BlockMevInfoVersion(h[blockMevInfoVersionOffset])
	if v != BlockMevInfoVersionBid && v != BlockMevInfoVersionBidBlock {
		return 0, common.Address{}, false
	}
	copy(builder[:], h[blockMevInfoVersionOffset+1:])
	if builder == (common.Address{}) {
		return 0, common.Address{}, false
	}
	return v, builder, true
}
