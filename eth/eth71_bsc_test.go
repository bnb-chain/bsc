// Copyright 2020 The go-ethereum Authors
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

package eth

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	ethproto "github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// eth/71 adds BAL exchange without changing BSC's full-blob, version-0 relay.
func TestBSCBlobRelayVersions(t *testing.T) {
	blob := new(kzg4844.Blob)
	commitment, err := kzg4844.BlobToCommitment(blob)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := kzg4844.ComputeBlobProof(blob, commitment)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := types.NewBlobTxSidecar(types.BlobSidecarVersion0, []kzg4844.Blob{*blob}, []kzg4844.Commitment{commitment}, []kzg4844.Proof{proof})
	tx := types.NewTx(&types.BlobTx{
		ChainID: uint256.NewInt(56), GasTipCap: uint256.NewInt(1), GasFeeCap: uint256.NewInt(1), Gas: 21000,
		BlobFeeCap: uint256.NewInt(1), BlobHashes: []common.Hash{kzg4844.CalcBlobHashV1(sha256.New(), &commitment)}, Sidecar: sidecar,
	})
	if err := txpool.ValidateBlobTx(tx, &types.Header{Number: big.NewInt(0)}, &txpool.ValidationOptions{Config: params.BSCChainConfig}); err != nil {
		t.Fatalf("invalid BSC version-0 fixture: %v", err)
	}
	for _, version := range []uint{ethproto.ETH68, ethproto.ETH70, ethproto.ETH71} {
		t.Run(fmt.Sprintf("eth/%d", version), func(t *testing.T) {
			peer := ethproto.NewPeer(version, p2p.NewPeer(enode.ID{}, "blob-relay", nil), nil, nil, nil, params.BSCChainConfig)
			defer peer.Close()
			if err := handleTransactions(peer, []*types.Transaction{tx}, false); err != nil {
				t.Fatalf("full version-0 blob rejected: %v", err)
			}
			missing := sidecar.Copy()
			missing.Blobs = nil
			if err := handleTransactions(peer, []*types.Transaction{tx.WithBlobTxSidecar(missing)}, false); err == nil {
				t.Fatal("accepted version-0 blob without its payload")
			}
			if err := handleTransactions(peer, []*types.Transaction{tx}, true); err == nil {
				t.Fatal("accepted direct blob broadcast")
			}
		})
	}
}
