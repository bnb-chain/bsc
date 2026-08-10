package vm

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestB20DeriveAddressMatchesBaseStd pins the address fingerprint against a
// vector taken from the reference implementation rather than from our own code.
//
// This is the whole point of the test. Every other test that touches derivation
// calls b20DeriveAddress to compute its own expectation, so all of them stay
// green under any change to the preimage — including a wrong one. The preimage
// was in fact wrong: it hashed packed bytes (20+32), while base-std hashes
// abi.encode (32+32).
//
// The vector below is the value getB20Address(ASSET, creator, salt) returns on
// Base mainnet (chain 8453) and Base Sepolia (84532), which agree. Only the
// nine fingerprint bytes are portable: Base's reserved prefix is 0xB200… and its
// variant byte numbering is its own, so the surrounding bytes are deliberately
// not compared.
func TestB20DeriveAddressMatchesBaseStd(t *testing.T) {
	var (
		creator = common.HexToAddress("0x04d63aBCd2b9b1baa327f2Dda0f873F197ccd186")
		salt    = common.HexToHash("0xb0b0")
		// From Base: getB20Address -> 0xB2000000000000000000007227619A766f6ac0E1
		wantFingerprint = common.FromHex("7227619a766f6ac0e1")
	)

	got := b20DeriveAddress(b20VariantAsset, creator, salt)
	if fp := got[11:20]; !bytes.Equal(fp, wantFingerprint) {
		t.Errorf("fingerprint = %x, want %x (from base-std on Base mainnet/Sepolia)", fp, wantFingerprint)
	}

	// Spell the two candidate preimages out, so a future change that reverts to
	// packed concatenation fails with the reason rather than just a mismatch.
	packed := crypto.Keccak256(creator.Bytes(), salt.Bytes())
	if bytes.Equal(got[11:20], packed[:9]) {
		t.Error("the preimage is packed (20+32); base-std uses abi.encode (32+32)")
	}

	// And the prefix and variant placement are ours to hold: 0x20B0, eight zero
	// bytes, then the variant in the eleventh.
	if got[0] != 0x20 || got[1] != 0xb0 {
		t.Errorf("marker = %x%x, want 20b0", got[0], got[1])
	}
	for i := 2; i < 10; i++ {
		if got[i] != 0 {
			t.Errorf("padding byte %d = %#x, want zero", i, got[i])
		}
	}
	if got[10] != b20VariantAsset {
		t.Errorf("variant byte = %#x, want %#x", got[10], b20VariantAsset)
	}
}
