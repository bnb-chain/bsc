// Copyright 2025 The go-ethereum Authors
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

package txpool

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-eth-kzg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestValidateTransactionEIP2681(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	head := &types.Header{
		Number:     big.NewInt(1),
		GasLimit:   5000000,
		Time:       1,
		Difficulty: big.NewInt(1),
	}

	signer := types.LatestSigner(params.TestChainConfig)

	// Create validation options
	opts := &ValidationOptions{
		Config:       params.TestChainConfig,
		Accept:       0xFF, // Accept all transaction types
		MaxSize:      32 * 1024,
		MaxBlobCount: 6,
		MinTip:       big.NewInt(0),
	}

	tests := []struct {
		name    string
		nonce   uint64
		wantErr error
	}{
		{
			name:    "normal nonce",
			nonce:   42,
			wantErr: nil,
		},
		{
			name:    "max allowed nonce (2^64-2)",
			nonce:   math.MaxUint64 - 1,
			wantErr: nil,
		},
		{
			name:    "EIP-2681 nonce overflow (2^64-1)",
			nonce:   math.MaxUint64,
			wantErr: core.ErrNonceMax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := createTestTransaction(key, tt.nonce)
			err := ValidateTransaction(tx, head, signer, opts)

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateTransaction() error = %v, wantErr nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateTransaction() error = nil, wantErr %v", tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateTransaction() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

// createTestTransaction creates a basic transaction for testing
func createTestTransaction(key *ecdsa.PrivateKey, nonce uint64) *types.Transaction {
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")

	txdata := &types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(1),
		Data:     nil,
	}

	tx := types.NewTx(txdata)
	signedTx, _ := types.SignTx(tx, types.HomesteadSigner{}, key)
	return signedTx
}

func randFieldElement() [32]byte {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		panic("failed to get random field element")
	}
	var r fr.Element
	r.SetBytes(bytes)
	return gokzg4844.SerializeScalar(r)
}

func randBlob() kzg4844.Blob {
	var blob kzg4844.Blob
	for i := 0; i < len(blob); i += gokzg4844.SerializedScalarSize {
		fieldElementBytes := randFieldElement()
		copy(blob[i:i+gokzg4844.SerializedScalarSize], fieldElementBytes[:])
	}
	return blob
}

func validBlobSidecar(n int) *types.BlobTxSidecar {
	blobs := make([]kzg4844.Blob, n)
	commitments := make([]kzg4844.Commitment, n)
	proofs := make([]kzg4844.Proof, n)
	for i := 0; i < n; i++ {
		blobs[i] = randBlob()
		c, _ := kzg4844.BlobToCommitment(&blobs[i])
		commitments[i] = c
		p, _ := kzg4844.ComputeBlobProof(&blobs[i], c)
		proofs[i] = p
	}
	return types.NewBlobTxSidecar(types.BlobSidecarVersion0, blobs, commitments, proofs)
}

func signedBlobTx(key *ecdsa.PrivateKey, nonce uint64, sidecar *types.BlobTxSidecar, blobFeeCap uint64) *types.Transaction {
	blobtx := &types.BlobTx{
		ChainID:    uint256.MustFromBig(params.MainnetChainConfig.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(1000),
		Gas:        21000,
		BlobFeeCap: uint256.NewInt(blobFeeCap),
		BlobHashes: sidecar.BlobHashes(),
		Value:      uint256.NewInt(0),
		Sidecar:    sidecar,
	}
	tx := types.NewTx(blobtx)
	signed, _ := types.SignTx(tx, types.NewCancunSigner(params.MainnetChainConfig.ChainID), key)
	return signed
}

// TestValidateBlobTx_InvalidProof verifies that ValidateBlobTx rejects a
// transaction with a tampered KZG proof.
func TestValidateBlobTx_InvalidProof(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sidecar := validBlobSidecar(1)
	sidecar.Proofs[0][0] ^= 0xff
	tx := signedBlobTx(key, 0, sidecar, 1)

	if err := ValidateBlobTx(tx, nil, nil); err == nil {
		t.Fatal("ValidateBlobTx should fail with tampered proof")
	}
}
