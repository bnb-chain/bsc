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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// TestB20StringLengthWordIsNotTrusted covers a length word no write could have
// produced. Before the bound, both read paths crashed the node:
//
//	short: 2*len of 200 sliced the 32-byte word at [:100]
//	       — "slice bounds out of range [:100] with length 32"
//	long:  a length near 2^64 reached make([]byte, 0, length)
//	       — "makeslice: cap out of range"
//
// Nothing writes such a word today, which is the whole reason to test it: the
// slot is reachable from genesis and from fork hooks as well as from setStringAt,
// and "only our own writer touches it" is an invariant that holds until someone
// adds a field. A panic inside a precompile is a node crash, not a revert, so it
// cannot be left to reachability.
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

			if got := s.name(); got != "" {
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
			if got := s.name(); got != "ok" {
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

// TestB20StorageViewsAreConstructed keeps b20Storage literals out of production
// code, so that whether a view meters is a decision with a name.
//
// The struct's ctx field is what makes a view charge gas, and it is optional —
// b20Storage{state: x, token: y} compiles and silently reads state for free.
// Nothing marks that at the call site, and the two registry views did it while a
// named unmetered constructor sat with no production caller at all. Go cannot
// forbid an in-package literal, so this is the check.
func TestB20StorageViewsAreConstructed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "b20_storage.go" {
			continue // the constructors themselves live here
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "b20Storage{") {
				t.Errorf("%s:%d builds a b20Storage literal. Use newMeteredB20Storage, "+
					"newMeteredB20StorageAt or newUnmeteredB20Storage, so that a view "+
					"reading state for free says so:\n    %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
	if len(files) < 5 {
		t.Fatalf("globbed %d files; the scan is not looking at the package", len(files))
	}
}
