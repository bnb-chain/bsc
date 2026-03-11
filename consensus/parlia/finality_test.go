// Copyright 2017 The bnb-chain Authors
// This file is part of the bnb-chain library.
//
// The bnb-chain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The bnb-chain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the bnb-chain library. If not, see <http://www.gnu.org/licenses/>.

package parlia

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

type finalizedHeaderChain struct {
	cfg      *params.ChainConfig
	current  *types.Header
	byHash   map[common.Hash]*types.Header
	byNumber map[uint64]*types.Header
}

func (c *finalizedHeaderChain) Config() *params.ChainConfig {
	return c.cfg
}

func (c *finalizedHeaderChain) CurrentHeader() *types.Header {
	return c.current
}

func (c *finalizedHeaderChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	header := c.byHash[hash]
	if header != nil && header.Number.Uint64() == number {
		return header
	}
	return nil
}

func (c *finalizedHeaderChain) GetHeaderByNumber(number uint64) *types.Header {
	return c.byNumber[number]
}

func (c *finalizedHeaderChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return c.byHash[hash]
}

func (c *finalizedHeaderChain) GenesisHeader() *types.Header {
	return c.byNumber[0]
}

func (c *finalizedHeaderChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return big.NewInt(0)
}

func (c *finalizedHeaderChain) GetHighestVerifiedHeader() *types.Header {
	return c.current
}

func (c *finalizedHeaderChain) GetVerifiedBlockByHash(hash common.Hash) *types.Header {
	return c.byHash[hash]
}

func (c *finalizedHeaderChain) ChasingHead() *types.Header {
	return c.current
}

type fixedVotePool struct {
	n int
}

func (v *fixedVotePool) FetchVotesByBlockHash(targetBlockHash common.Hash, sourceBlockNum uint64) []*types.VoteEnvelope {
	votes := make([]*types.VoteEnvelope, v.n)
	for i := range votes {
		votes[i] = &types.VoteEnvelope{}
	}
	return votes
}

func makeValidatorSet(size int) map[common.Address]*ValidatorInfo {
	validators := make(map[common.Address]*ValidatorInfo, size)
	for i := 0; i < size; i++ {
		addr := common.BigToAddress(big.NewInt(int64(i + 1)))
		validators[addr] = &ValidatorInfo{}
	}
	return validators
}

func TestGetFinalizedHeaderUsesParentSnapshotForFastFinalityQuorum(t *testing.T) {
	cfg := &params.ChainConfig{
		ChainID:    big.NewInt(56),
		PlatoBlock: big.NewInt(0),
		Parlia:     &params.ParliaConfig{},
	}

	genesis := &types.Header{
		Number: big.NewInt(0),
	}
	targetParent := &types.Header{
		Number:     big.NewInt(10),
		ParentHash: common.Hash{},
	}
	finalized := &types.Header{
		Number:     big.NewInt(9),
		ParentHash: genesis.Hash(),
	}
	targetParent.ParentHash = finalized.Hash()
	target := &types.Header{
		Number:     big.NewInt(11),
		ParentHash: targetParent.Hash(),
	}

	chain := &finalizedHeaderChain{
		cfg:     cfg,
		current: target,
		byHash: map[common.Hash]*types.Header{
			genesis.Hash():      genesis,
			targetParent.Hash(): targetParent,
			finalized.Hash():    finalized,
			target.Hash():       target,
		},
		byNumber: map[uint64]*types.Header{
			0:  genesis,
			9:  finalized,
			10: targetParent,
			11: target,
		},
	}

	parentSnap := &Snapshot{
		config:           cfg.Parlia,
		Number:           10,
		Hash:             targetParent.Hash(),
		Validators:       makeValidatorSet(4),
		Attestation:      &types.VoteData{SourceNumber: 9, SourceHash: finalized.Hash(), TargetNumber: 9, TargetHash: finalized.Hash()},
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
	}
	headerSnap := &Snapshot{
		config:           cfg.Parlia,
		Number:           11,
		Hash:             target.Hash(),
		Validators:       makeValidatorSet(2),
		Attestation:      &types.VoteData{SourceNumber: 9, SourceHash: finalized.Hash(), TargetNumber: 10, TargetHash: targetParent.Hash()},
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
	}

	engine := New(cfg, nil, nil, genesis.Hash())
	engine.recentSnaps.Add(targetParent.Hash(), parentSnap)
	engine.recentSnaps.Add(target.Hash(), headerSnap)
	engine.VotePool = &fixedVotePool{n: 2}

	finalizedHeader := engine.GetFinalizedHeader(chain, target)
	if finalizedHeader == nil {
		t.Fatal("expected finalized header, got nil")
	}
	if finalizedHeader.Number.Uint64() != finalized.Number.Uint64() {
		t.Fatalf("expected fallback finalized block %d, got %d", finalized.Number.Uint64(), finalizedHeader.Number.Uint64())
	}
}

var _ consensus.ChainHeaderReader = (*finalizedHeaderChain)(nil)

func TestGetVoteInterval(t *testing.T) {
	pasteurTime := uint64(1000)
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(56),
		LondonBlock: big.NewInt(0),
		PasteurTime: &pasteurTime,
		Parlia:      &params.ParliaConfig{},
	}
	genesis := &types.Header{Number: big.NewInt(0)}
	engine := New(cfg, nil, nil, genesis.Hash())

	prePasteur := &types.Header{Number: big.NewInt(10), Time: 999}
	if got := engine.GetVoteInterval(prePasteur); got != 1 {
		t.Fatalf("expected vote interval 1 pre-Pasteur, got %d", got)
	}
	if !engine.IsVotingBlock(prePasteur) {
		t.Fatal("pre-Pasteur: every block should be a voting block")
	}

	postPasteur := &types.Header{Number: big.NewInt(10), Time: 1000}
	if got := engine.GetVoteInterval(postPasteur); got != pasteurVoteInterval {
		t.Fatalf("expected vote interval %d post-Pasteur, got %d", pasteurVoteInterval, got)
	}
}

func TestIsVotingBlock(t *testing.T) {
	pasteurTime := uint64(0)
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(56),
		LondonBlock: big.NewInt(0),
		PasteurTime: &pasteurTime,
		Parlia:      &params.ParliaConfig{},
	}
	genesis := &types.Header{Number: big.NewInt(0)}
	engine := New(cfg, nil, nil, genesis.Hash())

	tests := []struct {
		blockNum uint64
		isVoting bool
	}{
		{0, true},
		{1, false},
		{2, true},
		{3, false},
		{4, true},
		{100, true},
		{101, false},
	}
	for _, tc := range tests {
		header := &types.Header{Number: big.NewInt(int64(tc.blockNum)), Time: 1}
		got := engine.IsVotingBlock(header)
		if got != tc.isVoting {
			t.Errorf("IsVotingBlock(%d) = %v, want %v", tc.blockNum, got, tc.isVoting)
		}
	}
}

func TestGetAncestorGenerationDepthPasteur(t *testing.T) {
	pasteurTime := uint64(0)
	fermiTime := uint64(0)
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(56),
		LondonBlock: big.NewInt(0),
		FermiTime:   &fermiTime,
		PasteurTime: &pasteurTime,
		Parlia:      &params.ParliaConfig{},
	}
	genesis := &types.Header{Number: big.NewInt(0)}
	engine := New(cfg, nil, nil, genesis.Hash())

	header := &types.Header{Number: big.NewInt(10), Time: 1}
	depth := engine.GetAncestorGenerationDepth(header)
	if depth != pasteurVoteAncestorDepth {
		t.Fatalf("expected ancestor depth %d for Pasteur, got %d", pasteurVoteAncestorDepth, depth)
	}
}

func TestUpdateAttestationVoteInterval(t *testing.T) {
	pasteurTime := uint64(0)
	fermiTime := uint64(0)
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(56),
		LondonBlock: big.NewInt(0),
		LubanBlock:  big.NewInt(0),
		FermiTime:   &fermiTime,
		PasteurTime: &pasteurTime,
		Parlia:      &params.ParliaConfig{},
	}

	makeHeaderWithAttestation := func(number uint64, attestation *types.VoteAttestation) *types.Header {
		attBytes, err := rlp.EncodeToBytes(attestation)
		if err != nil {
			t.Fatalf("failed to encode attestation: %v", err)
		}
		extra := make([]byte, extraVanity+len(attBytes)+extraSeal)
		copy(extra[extraVanity:], attBytes)
		return &types.Header{
			Number: big.NewInt(int64(number)),
			Time:   1,
			Extra:  extra,
		}
	}

	// Case 1: consecutive voting blocks (source=2, target=4, gap=N=2) → full attestation replacement
	snap := &Snapshot{
		config:      cfg.Parlia,
		EpochLength: 200,
		Attestation: &types.VoteData{
			SourceNumber: 0,
			SourceHash:   common.Hash{0x01},
			TargetNumber: 2,
			TargetHash:   common.Hash{0x02},
		},
	}
	header4 := makeHeaderWithAttestation(4, &types.VoteAttestation{
		Data: &types.VoteData{
			SourceNumber: 2,
			SourceHash:   common.Hash{0x02},
			TargetNumber: 4,
			TargetHash:   common.Hash{0x04},
		},
	})
	snap.updateAttestation(header4, cfg)
	if snap.Attestation.SourceNumber != 2 || snap.Attestation.TargetNumber != 4 {
		t.Fatalf("consecutive: expected attestation (source=2, target=4), got (source=%d, target=%d)",
			snap.Attestation.SourceNumber, snap.Attestation.TargetNumber)
	}

	// Case 2: non-consecutive voting blocks (source=2, target=6, gap=4≠N=2) → only target advances
	snap2 := &Snapshot{
		config:      cfg.Parlia,
		EpochLength: 200,
		Attestation: &types.VoteData{
			SourceNumber: 0,
			SourceHash:   common.Hash{0x01},
			TargetNumber: 2,
			TargetHash:   common.Hash{0x02},
		},
	}
	header6 := makeHeaderWithAttestation(6, &types.VoteAttestation{
		Data: &types.VoteData{
			SourceNumber: 2,
			SourceHash:   common.Hash{0x02},
			TargetNumber: 6,
			TargetHash:   common.Hash{0x06},
		},
	})
	snap2.updateAttestation(header6, cfg)
	if snap2.Attestation.SourceNumber != 0 {
		t.Fatalf("non-consecutive: expected source=0 (not advanced), got %d", snap2.Attestation.SourceNumber)
	}
	if snap2.Attestation.TargetNumber != 6 {
		t.Fatalf("non-consecutive: expected target=6, got %d", snap2.Attestation.TargetNumber)
	}
}

func TestGetFinalizedHeaderWithVoteInterval(t *testing.T) {
	pasteurTime := uint64(0)
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(56),
		LondonBlock: big.NewInt(0),
		PlatoBlock:  big.NewInt(0),
		PasteurTime: &pasteurTime,
		Parlia:      &params.ParliaConfig{},
	}

	genesis := &types.Header{Number: big.NewInt(0)}
	block1 := &types.Header{Number: big.NewInt(1), Time: 1, ParentHash: genesis.Hash()}

	block2 := &types.Header{Number: big.NewInt(2), Time: 1, ParentHash: block1.Hash()}
	block3 := &types.Header{Number: big.NewInt(3), Time: 1, ParentHash: block2.Hash()}
	block4 := &types.Header{Number: big.NewInt(4), Time: 1, ParentHash: block3.Hash()}

	chain := &finalizedHeaderChain{
		cfg:     cfg,
		current: block4,
		byHash: map[common.Hash]*types.Header{
			genesis.Hash(): genesis,
			block1.Hash():  block1,
			block2.Hash():  block2,
			block3.Hash():  block3,
			block4.Hash():  block4,
		},
		byNumber: map[uint64]*types.Header{
			0: genesis,
			1: block1,
			2: block2,
			3: block3,
			4: block4,
		},
	}

	// Snapshot at block 4: justified=2, finalized=0
	snap4 := &Snapshot{
		config:     cfg.Parlia,
		Number:     4,
		Hash:       block4.Hash(),
		Validators: makeValidatorSet(3),
		Attestation: &types.VoteData{
			SourceNumber: 0, SourceHash: genesis.Hash(),
			TargetNumber: 2, TargetHash: block2.Hash(),
		},
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
	}
	snap3 := &Snapshot{
		config:     cfg.Parlia,
		Number:     3,
		Hash:       block3.Hash(),
		Validators: makeValidatorSet(3),
		Attestation: &types.VoteData{
			SourceNumber: 0, SourceHash: genesis.Hash(),
			TargetNumber: 2, TargetHash: block2.Hash(),
		},
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
	}

	engine := New(cfg, nil, nil, genesis.Hash())
	engine.recentSnaps.Add(block4.Hash(), snap4)
	engine.recentSnaps.Add(block3.Hash(), snap3)
	engine.VotePool = &fixedVotePool{n: 2}

	// block4 is a voting block and justified+N==4: early finality should promote block2
	finalizedHeader := engine.GetFinalizedHeader(chain, block4)
	if finalizedHeader == nil {
		t.Fatal("expected finalized header, got nil")
	}
	if finalizedHeader.Number.Uint64() != 2 {
		t.Fatalf("expected finalized block 2, got %d", finalizedHeader.Number.Uint64())
	}

	// block3 is not a voting block: should fall back to snapshot source (block 0)
	finalizedFromBlock3 := engine.GetFinalizedHeader(chain, block3)
	if finalizedFromBlock3 == nil {
		t.Fatal("expected finalized header from block3, got nil")
	}
	if finalizedFromBlock3.Number.Uint64() != 0 {
		t.Fatalf("expected finalized block 0 from non-voting block, got %d", finalizedFromBlock3.Number.Uint64())
	}
}
