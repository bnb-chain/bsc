// Copyright 2022 The go-ethereum Authors
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

// just for prysm compile pass
package engine

import (
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// BlobsBundle holds the blobs of an execution payload
type BlobsBundle struct {
	Commitments []types.KZGCommitment `json:"commitments" gencodec:"required"`
	Blobs       []types.Blob          `json:"blobs"       gencodec:"required"`
	Proofs      []types.KZGProof      `json:"proofs"      gencodec:"required"`
}

func BlockToBlobData(block *types.Block) (*BlobsBundle, error) {
	blobsBundle := &BlobsBundle{
		Blobs:       []types.Blob{},
		Commitments: []types.KZGCommitment{},
		Proofs:      []types.KZGProof{},
	}
	for i, tx := range block.Transactions() {
		if tx.Type() == types.BlobTxType {
			versionedHashes, commitments, blobs, proofs := tx.BlobWrapData()
			if len(versionedHashes) != len(commitments) || len(versionedHashes) != len(blobs) || len(blobs) != len(proofs) {
				return nil, fmt.Errorf("tx %d in block %s has inconsistent blobs (%d) / commitments (%d)"+
					" / versioned hashes (%d) / proofs (%d)", i, block.Hash(), len(blobs), len(commitments), len(versionedHashes), len(proofs))
			}

			blobsBundle.Blobs = append(blobsBundle.Blobs, blobs...)
			blobsBundle.Commitments = append(blobsBundle.Commitments, commitments...)
			blobsBundle.Proofs = append(blobsBundle.Proofs, proofs...)
		}
	}
	return blobsBundle, nil
}

func BlockToSidecars(block *types.Block) ([]*types.Sidecar, error) {
	//blobsBundle := &BlobsBundle{
	//	Blobs:       []types.Blob{},
	//	Commitments: []types.KZGCommitment{},
	//	Proofs:      []types.KZGProof{},
	//}

	sidecars := make([]*types.Sidecar, 0)

	for i, tx := range block.Transactions() {
		if tx.Type() == types.BlobTxType {
			log.Info("Blob Tx detected...........!!!!!!!!!!!")
			versionedHashes, commitments, blobs, proofs := tx.BlobWrapData()
			if len(versionedHashes) != len(commitments) || len(versionedHashes) != len(blobs) || len(blobs) != len(proofs) {
				return nil, fmt.Errorf("tx %d in block %s has inconsistent blobs (%d) / commitments (%d)"+
					" / versioned hashes (%d) / proofs (%d)", i, block.Hash(), len(blobs), len(commitments), len(versionedHashes), len(proofs))
			}
			blockRoot := block.Root()
			blockParentRoot := block.ParentHash()
			for index, blob := range blobs {
				sidecar := &types.Sidecar{
					BlockRoot:       blockRoot,
					BlockParentRoot: blockParentRoot,
					Index:           block.NumberU64(),
					Blob:            blob,
					KZGCommitment:   commitments[index],
					KZGProof:        proofs[index],
				}
				sidecars = append(sidecars, sidecar)
			}
			log.Info("Sidecars: ", sidecars[0])

			//blobsBundle.Blobs = append(blobsBundle.Blobs, blobs...)
			//blobsBundle.Commitments = append(blobsBundle.Commitments, commitments...)
			//blobsBundle.Proofs = append(blobsBundle.Proofs, proofs...)
		}
	}

	return sidecars, nil
}

//func TransactionsToSidecars(txs []*types.Transaction, )
