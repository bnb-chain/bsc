package mlkem

import (
	"bytes"
	"testing"
)

func TestEncapsulateDecapsulateRoundTrip(t *testing.T) {
	encapKey, decapKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	ciphertext, sharedSecret, err := Encapsulate(encapKey)
	if err != nil {
		t.Fatalf("Encapsulate error: %v", err)
	}

	recovered, err := Decapsulate(decapKey, ciphertext)
	if err != nil {
		t.Fatalf("Decapsulate error: %v", err)
	}
	if !bytes.Equal(sharedSecret, recovered) {
		t.Fatal("shared secrets do not match")
	}
}
