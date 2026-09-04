package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// TestCAS20StringLengthWordIsNotTrusted covers a length word no write could have
// produced. Before the bound, both read paths crashed the node:
func TestCAS20StringLengthWordIsNotTrusted(t *testing.T) {
	word := func(build func(*common.Hash)) common.Hash {
		var h common.Hash
		build(&h)
		return h
	}
	for _, tc := range []struct {
		name string
		word common.Hash
	}{
		{"short string claiming 100 bytes", word(func(h *common.Hash) { h[31] = 200 })},
		{"short string claiming 127 bytes", word(func(h *common.Hash) { h[31] = 254 })},
		{"long string of almost 2^255 bytes", word(func(h *common.Hash) {
			for i := range h {
				h[i] = 0xff
			}
		})},
		{"long string one byte past the cap", common.Hash(
			uint256.NewInt(2*(cas20MaxStringLen+1) + 1).Bytes32())},
		// The long form encodes 32 bytes or more. A shorter length belongs to the
		// short form, so these words are as non-canonical as the oversized ones
		// above and must answer the same way — without them the short form's bound
		// is the only one under test, and dropping the long form's would go
		// unnoticed while the reader went off to the data root for content.
		{"long string claiming one byte", common.Hash(uint256.NewInt(3).Bytes32())},
		{"long string claiming 31 bytes", common.Hash(uint256.NewInt(2*31 + 1).Bytes32())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			if err != nil {
				t.Fatal(err)
			}
			s := newUnmeteredCAS20Storage(statedb, cas20Addr(cas20VariantAsset, 1))
			statedb.SetState(s.token, slotAt(cas20SlotName), tc.word)

			if got := strOf(s.name()); got != "" {
				t.Errorf("name() = %q, want the empty string", got)
			}
			// Fatal, not Errorf: the repair below feeds this count to setStringAt's
			// release loop, which on an unmetered view has no out-of-gas guard. An
			// unbounded count there does not fail the test, it hangs it.
			if got := s.stringChunks(slotAt(cas20SlotName)); got != 0 {
				t.Fatalf("stringChunks = %d, want 0 — setStringAt would release that "+
					"many slots, and nothing would stop it", got)
			}
			// A write over the corrupt word must still land, so a token whose state
			// arrived that way is repairable rather than bricked.
			s.setName("ok")
			if got := strOf(s.name()); got != "ok" {
				t.Errorf("name() after setName = %q, want %q", got, "ok")
			}
		})
	}

	// The bound must not touch a value the chain could actually hold. The longest
	// string these tests can afford is far shorter than the cap, so check the
	// boundary arithmetic directly instead.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	s := newUnmeteredCAS20Storage(statedb, cas20Addr(cas20VariantAsset, 2))
	atCap := common.Hash(uint256.NewInt(2*cas20MaxStringLen + 1).Bytes32())
	statedb.SetState(s.token, slotAt(cas20SlotName), atCap)
	if got := s.stringChunks(slotAt(cas20SlotName)); got != cas20MaxStringLen/32 {
		t.Errorf("a string exactly at the cap reports %d chunks, want %d — the bound is "+
			"off by one and rejects a length it should accept", got, cas20MaxStringLen/32)
	}
}
