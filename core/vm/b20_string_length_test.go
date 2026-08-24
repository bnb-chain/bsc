// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// TestB20StringLengthWordIsNotTrusted covers a length word no write could have
// produced. Before the bound, both read paths crashed the node:
func TestB20StringLengthWordIsNotTrusted(t *testing.T) {
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
			uint256.NewInt(2*(b20MaxStringLen+1) + 1).Bytes32())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			if err != nil {
				t.Fatal(err)
			}
			s := newUnmeteredB20Storage(statedb, b20Addr(b20VariantAsset, 1))
			statedb.SetState(s.token, slotAt(b20SlotName), tc.word)

			if got := strOf(s.name()); got != "" {
				t.Errorf("name() = %q, want the empty string", got)
			}
			// Fatal, not Errorf: the repair below feeds this count to setStringAt's
			// release loop, which on an unmetered view has no out-of-gas guard. An
			// unbounded count there does not fail the test, it hangs it.
			if got := s.stringChunks(slotAt(b20SlotName)); got != 0 {
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
	s := newUnmeteredB20Storage(statedb, b20Addr(b20VariantAsset, 2))
	atCap := common.Hash(uint256.NewInt(2*b20MaxStringLen + 1).Bytes32())
	statedb.SetState(s.token, slotAt(b20SlotName), atCap)
	if got := s.stringChunks(slotAt(b20SlotName)); got != b20MaxStringLen/32 {
		t.Errorf("a string exactly at the cap reports %d chunks, want %d — the bound is "+
			"off by one and rejects a length it should accept", got, b20MaxStringLen/32)
	}
}
