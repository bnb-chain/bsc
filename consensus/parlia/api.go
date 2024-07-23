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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// API is a user facing RPC API to allow query snapshot and validators
type API struct {
	chain  consensus.ChainHeaderReader
	parlia *Parlia
}

// GetSnapshot retrieves the state snapshot at a given block.
func (api *API) GetSnapshot(number *rpc.BlockNumber) (*Snapshot, error) {
	header := api.getHeader(number)
	// Ensure we have an actually valid block and return its snapshot
	if header == nil {
		return nil, errUnknownBlock
	}
	return api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
}

// GetSnapshotAtHash retrieves the state snapshot at a given block.
func (api *API) GetSnapshotAtHash(hash common.Hash) (*Snapshot, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, errUnknownBlock
	}
	return api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
}

// GetValidators retrieves the list of validators at the specified block.
func (api *API) GetValidators(number *rpc.BlockNumber) ([]common.Address, error) {
	header := api.getHeader(number)
	// Ensure we have an actually valid block and return the validators from its snapshot
	if header == nil {
		return nil, errUnknownBlock
	}
	snap, err := api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return nil, err
	}
	return snap.validators(), nil
}

// GetValidatorsAtHash retrieves the list of validators at the specified block.
func (api *API) GetValidatorsAtHash(hash common.Hash) ([]common.Address, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, errUnknownBlock
	}
	snap, err := api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return nil, err
	}
	return snap.validators(), nil
}

func (api *API) GetJustifiedNumber(number *rpc.BlockNumber) (uint64, error) {
	header := api.getHeader(number)
	// Ensure we have an actually valid block and return the validators from its snapshot
	if header == nil {
		return 0, errUnknownBlock
	}
	snap, err := api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil || snap.Attestation == nil {
		return 0, err
	}
	return snap.Attestation.TargetNumber, nil
}

func (api *API) GetTurnLength(number *rpc.BlockNumber) (uint8, error) {
	header := api.getHeader(number)
	// Ensure we have an actually valid block and return the validators from its snapshot
	if header == nil {
		return 0, errUnknownBlock
	}
	snap, err := api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil || snap.TurnLength == 0 {
		return 0, err
	}
	return snap.TurnLength, nil
}

func (api *API) GetFinalizedNumber(number *rpc.BlockNumber) (uint64, error) {
	header := api.getHeader(number)
	// Ensure we have an actually valid block and return the validators from its snapshot
	if header == nil {
		return 0, errUnknownBlock
	}
	snap, err := api.parlia.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil || snap.Attestation == nil {
		return 0, err
	}
	return snap.Attestation.SourceNumber, nil
}

func (api *API) getHeader(number *rpc.BlockNumber) (header *types.Header) {
	currentHeader := api.chain.CurrentHeader()

	if number == nil || *number == rpc.LatestBlockNumber {
		header = currentHeader // current if none requested
	} else if *number == rpc.SafeBlockNumber {
		justifiedNumber, _, err := api.parlia.GetJustifiedNumberAndHash(api.chain, []*types.Header{currentHeader})
		if err != nil {
			return nil
		}
		header = api.chain.GetHeaderByNumber(justifiedNumber)
	} else if *number == rpc.FinalizedBlockNumber {
		header = api.parlia.GetFinalizedHeader(api.chain, currentHeader)
	} else if *number == rpc.PendingBlockNumber {
		return nil // no pending blocks on bsc
	} else if *number == rpc.EarliestBlockNumber {
		header = api.chain.GetHeaderByNumber(0)
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}
	return
}
