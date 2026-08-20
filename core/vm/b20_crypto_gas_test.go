package vm

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Gas for the runtime cryptography: the keccaks EIP-712 needs, the one address
// derivation hashes, and the secp256k1 recovery permit performs.
//
// None of it was charged. The storage, log and account formulas were all
// mirrored carefully, but hashing outside b20Storage was invisible: mapSlot and
// the long-string root meter themselves, so anything hashing directly through
// crypto.Keccak256 slipped past. A permit recovered a signature for free where
// the ECRECOVER precompile bills 3000 gas flat, which is the plainest possible
// violation of BEP-702 3.14's never-cheaper-than-bytecode rule.

// TestB20DomainSeparatorChargesItsHashes pins the name keccak by the only term
// that varies with it: one more word of name costs exactly one more keccak word.
func TestB20DomainSeparatorChargesItsHashes(t *testing.T) {
	gasFor := func(name string) uint64 {
		t.Helper()
		_, evm := newB20EVM(t)
		creator := common.HexToAddress("0xc4ea70")
		params := b20AssetParams(name, "T", creator, 18)
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20WithParams(b20VariantAsset, common.HexToHash("0xd0"), params, nil),
			NewGasBudget(9_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("createB20(%d-byte name): %v", len(name), err)
		}
		token := common.BytesToAddress(ret)
		budget := NewGasBudget(5_000_000)
		if _, left, err := evm.Call(creator, token, b20Call(selDomainSeparator),
			budget, uint256.NewInt(0)); err != nil {
			t.Fatalf("DOMAIN_SEPARATOR(): %v", err)
		} else {
			return budget.RegularGas - left.RegularGas
		}
		return 0
	}

	// Two names in the same storage shape — both long strings occupying the same
	// number of 32-byte chunks — so the SLOADs are identical and only the keccak
	// word count differs.
	short := strings.Repeat("n", 33) // 2 chunks, keccak over 33 bytes = 2 words
	long := strings.Repeat("n", 63)  // 2 chunks, keccak over 63 bytes = 2 words
	if gasFor(short) != gasFor(long) {
		t.Errorf("names of 33 and 63 bytes differ in cost (%d vs %d) but hash to the "+
			"same word count and occupy the same chunks", gasFor(short), gasFor(long))
	}
	// One more keccak word, same chunk count is impossible past 64, so step both
	// together and subtract the known chunk cost.
	wider := strings.Repeat("n", 65) // 3 chunks, keccak over 65 bytes = 3 words
	delta := gasFor(wider) - gasFor(long)
	// The chunk is warm, not cold: the factory wrote the name in this same frame,
	// so its slots are already in the access list by the time the read happens.
	wantChunk := params.WarmStorageReadCostEIP2929
	wantHash := params.Keccak256WordGas // the third keccak word
	if delta != wantChunk+wantHash {
		t.Errorf("65-byte name costs %d more than 63-byte, want %d (%d for the extra "+
			"warm chunk, %d for the extra keccak word)",
			delta, wantChunk+wantHash, wantChunk, wantHash)
	}
}

// TestB20PermitChargesRecovery pins everything permit does after the deadline
// check. The expired path returns before any of it, so the difference between the
// two is exactly the hashing, the two cold reads and ECRECOVER's flat fee.
func TestB20PermitChargesRecovery(t *testing.T) {
	const name = "Test Token" // what encodeCreateB20 sets

	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0xd1"), creator, nil),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	cost := func(deadline uint64) uint64 {
		t.Helper()
		budget := NewGasBudget(5_000_000)
		_, left, err := evm.Call(creator, token,
			b20Call(selPermit, addrKey(b20Alice), addrKey(b20Bob), u256hash(1), u256hash(deadline),
				wU8(27), common.Hash{}, common.Hash{}),
			budget, uint256.NewInt(0))
		if err == nil {
			t.Fatal("a permit with a zero signature must not succeed")
		}
		return budget.RegularGas - left.RegularGas
	}

	keccak := func(n int) uint64 {
		return params.Keccak256Gas + params.Keccak256WordGas*uint64((n+31)/32)
	}
	want := keccak(64) + params.ColdSloadCostEIP2929 + // nonces[owner]: a mapping, so its
		// slot derivation is itself a 64-byte keccak, then a first-touch read
		params.WarmStorageReadCostEIP2929 + // the name slot, warmed when the factory wrote it
		keccak(len(name)) + keccak(len(b20EIP712Version)) + keccak(160) + // the domain
		keccak(192) + // the permit struct
		keccak(66) + // the 0x1901 digest
		params.EcrecoverGas

	// deadline 0 expires before the block time; a far-future one reaches recovery
	// and fails there instead.
	expired, recovered := cost(0), cost(1<<40)
	if got := recovered - expired; got != want {
		t.Errorf("reaching recovery costs %d more than expiring early, want %d "+
			"(2 cold reads, five keccaks, and ECRECOVER's %d)",
			got, want, params.EcrecoverGas)
	}
}

// TestB20AddressDerivationChargesItsHash pins the factory's one keccak. Both
// entry points hash 64 bytes of (creator, salt); neither charged for it.
func TestB20AddressDerivationChargesItsHash(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")

	// getB20Address does nothing but decode, hash and return, so its whole cost is
	// the calldata charge plus that hash.
	input := b20Call(selGetB20Address, u256hash(b20VariantAsset), addrKey(creator), common.HexToHash("0x1"))
	budget := NewGasBudget(5_000_000)
	if _, left, err := evm.Call(creator, B20FactoryAddress, input, budget, uint256.NewInt(0)); err != nil {
		t.Fatalf("getB20Address: %v", err)
	} else {
		words := (uint64(len(input)) + 31) / 32
		calldata := GasFastestStep + words*b20CalldataWordGas + words*words/params.QuadCoeffDiv
		wantHash := params.Keccak256Gas + params.Keccak256WordGas*2 // 64-byte preimage
		if got, want := budget.RegularGas-left.RegularGas, calldata+wantHash; got != want {
			t.Errorf("getB20Address cost %d, want %d (%d calldata + %d for the "+
				"64-byte keccak)", got, want, calldata, wantHash)
		}
	}
}
