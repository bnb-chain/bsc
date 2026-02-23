// Copyright 2017 The go-ethereum Authors
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

package parlia

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type apiTestChain struct {
	cfg      *params.ChainConfig
	current  *types.Header
	byNumber map[uint64]*types.Header
	byHash   map[common.Hash]*types.Header
}

func (c *apiTestChain) Config() *params.ChainConfig {
	return c.cfg
}

func (c *apiTestChain) CurrentHeader() *types.Header {
	return c.current
}

func (c *apiTestChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	header := c.byHash[hash]
	if header != nil && header.Number.Uint64() == number {
		return header
	}
	return nil
}

func (c *apiTestChain) GetHeaderByNumber(number uint64) *types.Header {
	return c.byNumber[number]
}

func (c *apiTestChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return c.byHash[hash]
}

func (c *apiTestChain) GenesisHeader() *types.Header {
	return c.byNumber[0]
}

func (c *apiTestChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return big.NewInt(0)
}

func (c *apiTestChain) GetHighestVerifiedHeader() *types.Header {
	return c.current
}

func (c *apiTestChain) GetVerifiedBlockByHash(hash common.Hash) *types.Header {
	return c.byHash[hash]
}

func (c *apiTestChain) ChasingHead() *types.Header {
	return c.current
}

func TestAPIGetFinalizedNumberFinalizedAliasReturnsFinalizedHeaderNumber(t *testing.T) {
	cfg := &params.ChainConfig{
		ChainID:    big.NewInt(56),
		PlatoBlock: big.NewInt(0),
		Parlia:     &params.ParliaConfig{},
	}

	genesis := &types.Header{Number: big.NewInt(0)}
	finalizedParent := &types.Header{
		Number:     big.NewInt(9),
		ParentHash: genesis.Hash(),
	}
	finalized := &types.Header{
		Number:     big.NewInt(10),
		ParentHash: finalizedParent.Hash(),
	}
	current := &types.Header{
		Number:     big.NewInt(11),
		ParentHash: finalized.Hash(),
	}

	chain := &apiTestChain{
		cfg:     cfg,
		current: current,
		byNumber: map[uint64]*types.Header{
			0:  genesis,
			9:  finalizedParent,
			10: finalized,
			11: current,
		},
		byHash: map[common.Hash]*types.Header{
			genesis.Hash():         genesis,
			finalizedParent.Hash(): finalizedParent,
			finalized.Hash():       finalized,
			current.Hash():         current,
		},
	}

	engine := New(cfg, nil, nil, genesis.Hash())
	engine.recentSnaps.Add(current.Hash(), &Snapshot{
		config: cfg.Parlia,
		Number: current.Number.Uint64(),
		Hash:   current.Hash(),
		Attestation: &types.VoteData{
			SourceNumber: finalized.Number.Uint64(),
			SourceHash:   finalized.Hash(),
			TargetNumber: finalizedParent.Number.Uint64(),
			TargetHash:   finalizedParent.Hash(),
		},
	})
	engine.recentSnaps.Add(finalized.Hash(), &Snapshot{
		config: cfg.Parlia,
		Number: finalized.Number.Uint64(),
		Hash:   finalized.Hash(),
		Attestation: &types.VoteData{
			SourceNumber: finalizedParent.Number.Uint64(),
			SourceHash:   finalizedParent.Hash(),
			TargetNumber: finalizedParent.Number.Uint64(),
			TargetHash:   finalizedParent.Hash(),
		},
	})

	api := &API{
		chain:  chain,
		parlia: engine,
	}

	number := rpc.FinalizedBlockNumber
	got, err := api.GetFinalizedNumber(&number)
	if err != nil {
		t.Fatalf("GetFinalizedNumber returned unexpected error: %v", err)
	}

	want := finalized.Number.Uint64()
	if got != want {
		t.Fatalf("GetFinalizedNumber(%d) = %d, want %d", rpc.FinalizedBlockNumber, got, want)
	}
}
