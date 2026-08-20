package vm

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestB20DeriveAddressMatchesBaseStd pins the fingerprint against a vector from
// the reference implementation rather than from our own code. Every other
// derivation test calls b20DeriveAddress for its expectation and so stays green
// under any preimage, including a wrong one.
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
