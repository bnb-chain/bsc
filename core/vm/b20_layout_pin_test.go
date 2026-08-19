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
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestB20LayoutMatchesTheFixture holds testdata/b20_layout.json against the Go
// constants, in both directions.
//
// The layout is consensus: the precompile writes real storage under the token's
// account, so a slot number decides the account's storage root and hence the
// block hash. Two implementations that disagree about slot 13 diverge on the
// first permit. BEP-702 leans on this already — 3.14 requires the metered slots
// to be "the token's real storage keys" and 4.6 requires the storage layout to be
// fuzzed — without defining it anywhere, so the fixture is where it is defined
// and BEP-702's section is generated from it.
//
// The table below names each constant, so a renumbering changes the Go side and
// the literal in the fixture stops matching. Roots are recomputed from the
// namespace strings rather than copied, so renaming a namespace fails too.
func TestB20LayoutMatchesTheFixture(t *testing.T) {
	type field struct {
		Slot uint64 `json:"slot"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var ref struct {
		Namespaces []struct {
			Name   string  `json:"name"`
			Root   string  `json:"root"`
			Fields []field `json:"fields"`
		} `json:"namespaces"`
		Derivation struct {
			StringMaxLen uint64 `json:"string_max_len"`
		} `json:"derivation"`
	}
	raw, err := os.ReadFile("testdata/b20_layout.json")
	if err != nil {
		t.Fatalf("read the layout fixture: %v", err)
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatalf("parse the layout fixture: %v", err)
	}

	// Every slot constant the implementation has, by the name the fixture uses.
	live := map[string]map[string]uint64{
		b20Namespace: {
			"name":             b20SlotName,
			"symbol":           b20SlotSymbol,
			"contractURI":      b20SlotContractURI,
			"totalSupply":      b20SlotTotalSupply,
			"balances":         b20SlotBalances,
			"allowances":       b20SlotAllowances,
			"roles":            b20SlotRoles,
			"roleAdmins":       b20SlotRoleAdmins,
			"adminCount":       b20SlotAdminCount,
			"transferPolicies": b20SlotTransferPolicies,
			"mintPolicy":       b20SlotMintPolicy,
			"paused":           b20SlotPaused,
			"supplyCap":        b20SlotSupplyCap,
			"nonces":           b20SlotNonces,
			"seizePolicies":    b20SlotSeizePolicies,
		},
		b20AssetNamespace: {
			"decimals":          b20AssetSlotDecimals,
			"multiplier":        b20AssetSlotMultiplier,
			"announcements":     b20AssetSlotAnnouncements,
			"extraMetadata":     b20AssetSlotExtraMeta,
			"pendingMultiplier": b20AssetSlotPending,
		},
		b20StablecoinNamespace: {
			"currency": b20StablecoinSlotCurrency,
		},
		b20PolicyNamespace: {
			"policies":      polSlotPolicies,
			"members":       polSlotMembers,
			"pendingAdmins": polSlotPendingAdmins,
			"counter":       polSlotCounter,
			"children":      polSlotChildren,
		},
		b20ActivationNamespace: {
			"features": actSlotFeatures,
			"admin":    actSlotAdmin,
		},
	}

	documented := map[string]bool{}
	for _, ns := range ref.Namespaces {
		documented[ns.Name] = true
		fields, known := live[ns.Name]
		if !known {
			t.Errorf("the fixture documents namespace %q, which the implementation does not "+
				"have. Remove it, or add the namespace here", ns.Name)
			continue
		}
		// The root is derived, not transcribed: a renamed namespace moves every
		// slot in it, which is a state-root change and must not pass silently.
		if want := erc7201Root(ns.Name).Hex(); ns.Root != want {
			t.Errorf("namespace %q: fixture root %s, erc7201Root gives %s", ns.Name, ns.Root, want)
		}
		seen := map[string]bool{}
		for _, f := range ns.Fields {
			seen[f.Name] = true
			slot, ok := fields[f.Name]
			if !ok {
				t.Errorf("%s: the fixture documents field %q, which has no constant", ns.Name, f.Name)
				continue
			}
			if slot != f.Slot {
				t.Errorf("%s.%s is slot %d in the fixture and %d in the code. Renumbering a "+
					"populated slot is a state-root change: it needs a fork, not an edit",
					ns.Name, f.Name, f.Slot, slot)
			}
			if f.Type == "" {
				t.Errorf("%s.%s has no type; the generated BEP-702 table needs one", ns.Name, f.Name)
			}
		}
		var missing []string
		for name := range fields {
			if !seen[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: the fixture does not document %s. An undocumented slot is one a "+
				"reimplementation cannot place", ns.Name, strings.Join(missing, ", "))
		}
	}
	for ns := range live {
		if !documented[ns] {
			t.Errorf("namespace %q is not in the fixture at all, so BEP-702 will not mention it", ns)
		}
	}

	if ref.Derivation.StringMaxLen != b20MaxStringLen {
		t.Errorf("the fixture caps a string at %d, the code at %d",
			ref.Derivation.StringMaxLen, b20MaxStringLen)
	}

	// The roots must stay distinct, and none may collide with the low slots a
	// naive layout would use — that is the whole point of ERC-7201.
	roots := map[common.Hash]string{}
	for ns := range live {
		root := erc7201Root(ns)
		if other, dup := roots[root]; dup {
			t.Errorf("namespaces %q and %q derive the same root %s", ns, other, root.Hex())
		}
		roots[root] = ns
		if root[31] != 0 {
			t.Errorf("namespace %q root %s does not end in a zero byte; ERC-7201 masks it so "+
				"that a namespace has 256 consecutive slots", ns, root.Hex())
		}
	}
}

// TestB20LayoutDocIsInStep checks that BEP-702's generated storage section still
// matches the fixture, when a checkout of the BEPs repository is reachable.
//
// The spec lives in another repository, so this cannot be a hard gate: it skips
// unless B20_BEPS_DIR names a checkout, or one sits beside this one. Skipping is
// the honest failure mode — the alternative is a test that passes because it
// looked nowhere. Set B20_BEPS_DIR in CI to make it binding.
func TestB20LayoutDocIsInStep(t *testing.T) {
	dir := os.Getenv("B20_BEPS_DIR")
	if dir == "" {
		dir = "../../../BEPs"
	}
	doc := filepath.Join(dir, "BEPs", "BEP-702.md")
	if _, err := os.Stat(doc); err != nil {
		t.Skipf("no BEP-702 to check against (%s); set B20_BEPS_DIR to a BEPs checkout", doc)
	}
	abs, err := filepath.Abs(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Run from the repository root: the script's default fixture path is relative
	// to it, as its usage line shows.
	cmd := exec.Command("python3", "scripts/b20-layout-doc.py", "--check", abs)
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("BEP-702's storage layout no longer matches testdata/b20_layout.json.\n"+
			"Regenerate it:\n    python3 scripts/b20-layout-doc.py --write %s\n%s",
			doc, out)
	}
}
