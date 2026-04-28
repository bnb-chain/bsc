package mldsa

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pubKey, privKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	digest := keccak256([]byte("phase-0-mldsa"))
	sig, err := Sign(privKey, digest)
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if !Verify(pubKey, digest, sig) {
		t.Fatal("Verify returned false for valid signature")
	}

	tampered := append([]byte(nil), digest...)
	tampered[0] ^= 0xff
	if Verify(pubKey, tampered, sig) {
		t.Fatal("Verify accepted a tampered digest")
	}
}

func TestPubKeyToAddress(t *testing.T) {
	pubKey, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	want := common.BytesToAddress(keccak256(pubKey)[12:])
	if got := PubKeyToAddress(pubKey); got != want {
		t.Fatalf("unexpected address: have %s want %s", got.Hex(), want.Hex())
	}
}
