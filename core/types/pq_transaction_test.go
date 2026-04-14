package types

import (
	"math/big"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
)

func TestPQTxSignAndSender(t *testing.T) {
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	chainID := big.NewInt(97)
	signer := NewPQSigner(chainID)
	to := common.HexToAddress("0x0102030405060708090a0b0c0d0e0f1011121314")
	from := crypto.PQPubkeyToAddress(pubKey)
	restore := SetPQRegistryBackend(func(addr common.Address) []byte {
		if addr == from {
			return pubKey
		}
		return nil
	})
	defer restore()

	tx := NewTx(&PQTxData{
		ChainID:  new(big.Int).Set(chainID),
		Nonce:    7,
		GasPrice: big.NewInt(5),
		Gas:      21000,
		From:     from,
		To:       &to,
		Value:    big.NewInt(11),
		Data:     []byte("pq-transaction"),
	})

	signed, err := SignPQTx(tx, signer, privKey)
	if err != nil {
		t.Fatalf("SignPQTx error: %v", err)
	}

	pqtx, ok := signed.inner.(*PQTxData)
	if !ok {
		t.Fatal("signed transaction does not contain PQTxData")
	}
	if pqtx.From != from {
		t.Fatalf("unexpected sender field: have %s want %s", pqtx.From.Hex(), from.Hex())
	}
	if len(pqtx.PQSignature) != mldsa44.SignatureSize {
		t.Fatalf("unexpected signature size: have %d want %d", len(pqtx.PQSignature), mldsa44.SignatureSize)
	}

	hash := signer.Hash(signed)
	if !crypto.VerifyPQ(pubKey, hash[:], pqtx.PQSignature) {
		t.Fatal("stored PQ signature does not verify")
	}

	gotFrom, err := Sender(signer, signed)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if gotFrom != pqtx.From {
		t.Fatalf("unexpected sender: have %s want %s", gotFrom.Hex(), pqtx.From.Hex())
	}
}
