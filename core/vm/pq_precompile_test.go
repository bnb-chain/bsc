package vm

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
)

func TestPQRecoverValid(t *testing.T) {
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	hash := crypto.Keccak256([]byte("pq-precompile-valid"))
	sig, err := crypto.SignPQ(hash, privKey)
	if err != nil {
		t.Fatalf("SignPQ error: %v", err)
	}

	input := append(append(append([]byte{}, hash...), sig...), pubKey...)
	got, err := (pqRecover{}).Run(input)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	want := make([]byte, 32)
	addr := crypto.PQPubkeyToAddress(pubKey)
	copy(want[12:], addr[:])
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected output: have %x want %x", got, want)
	}
}

func TestPQRecoverInvalid(t *testing.T) {
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	hash := crypto.Keccak256([]byte("pq-precompile-invalid"))
	sig, err := crypto.SignPQ(hash, privKey)
	if err != nil {
		t.Fatalf("SignPQ error: %v", err)
	}
	sig[0] ^= 0xff

	input := append(append(append([]byte{}, hash...), sig...), pubKey...)
	got, err := (pqRecover{}).Run(input)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !bytes.Equal(got, make([]byte, 32)) {
		t.Fatalf("expected zero output for invalid signature, got %x", got)
	}
}
