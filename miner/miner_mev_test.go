package miner

import (
	"crypto/rand"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-eth-kzg"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

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

func makeSignedBlobTx(nonce uint64, sidecar *types.BlobTxSidecar) *types.Transaction {
	key, _ := crypto.GenerateKey()
	blobtx := &types.BlobTx{
		ChainID:    uint256.MustFromBig(params.MainnetChainConfig.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(1000),
		Gas:        21000,
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: sidecar.BlobHashes(),
		Value:      uint256.NewInt(0),
		Sidecar:    sidecar,
	}
	tx := types.NewTx(blobtx)
	signed, _ := types.SignTx(tx, types.NewCancunSigner(params.MainnetChainConfig.ChainID), key)
	return signed
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

// TestStartAsyncBlobValidation_InvalidProof verifies that the async blob
// validation goroutine correctly detects a tampered blob proof.
func TestStartAsyncBlobValidation_InvalidProof(t *testing.T) {
	sidecar := validBlobSidecar(1)
	sidecar.Proofs[0][0] ^= 0xff

	tx := makeSignedBlobTx(0, sidecar)
	bid := &types.Bid{
		Txs: types.Transactions{tx},
	}
	startAsyncBlobValidation(bid)

	ch, ok := bid.BlobValResults[tx.Hash()]
	if !ok {
		t.Fatal("expected BlobValResults to contain the blob tx")
	}
	if err := <-ch; err == nil {
		t.Fatal("expected error for invalid KZG proof")
	}
}
