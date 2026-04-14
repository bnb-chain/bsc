package crypto_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
)

func TestSignPQ(t *testing.T) {
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	digest := crypto.Keccak256([]byte("phase-1-sign-pq"))
	sig, err := crypto.SignPQ(digest, privKey)
	if err != nil {
		t.Fatalf("SignPQ error: %v", err)
	}
	if !crypto.VerifyPQ(pubKey, digest, sig) {
		t.Fatal("VerifyPQ returned false for a valid signature")
	}
}

func TestPQPubkeyToAddress(t *testing.T) {
	pubKey, _, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	want := common.BytesToAddress(crypto.Keccak256(pubKey)[12:])
	got1 := crypto.PQPubkeyToAddress(pubKey)
	got2 := crypto.PQPubkeyToAddress(pubKey)
	if got1 != want {
		t.Fatalf("unexpected address: have %s want %s", got1.Hex(), want.Hex())
	}
	if got1 != got2 {
		t.Fatalf("expected deterministic address output: first %s second %s", got1.Hex(), got2.Hex())
	}
}
