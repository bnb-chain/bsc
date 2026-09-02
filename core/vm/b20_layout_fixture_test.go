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
	"sort"
	"strings"
	"testing"
)

// TestB20LayoutFixtureFollowsTheCode keeps testdata/b20_layout.json in step with
// the constants, because BEP-702 3.17 is generated from that fixture and is what a
// second implementation builds from.
//
// It pins agreement, not values. The namespace strings are still undecided — a
// rename to n20 would move all five ERC-7201 roots and with them the absolute
// position of every field — so nothing here asserts what they are. It asserts that
// whatever the code says, the fixture says too: rename a namespace or renumber a
// slot and this fails, telling you to regenerate the fixture, which regenerates the
// spec section in the same commit.
func TestB20LayoutFixtureFollowsTheCode(t *testing.T) {
	// Derived from the code, never transcribed.
	live := map[string]map[string]uint64{
		b20Namespace: {
			"name": b20SlotName, "symbol": b20SlotSymbol, "contractURI": b20SlotContractURI,
			"totalSupply": b20SlotTotalSupply, "balances": b20SlotBalances,
			"allowances": b20SlotAllowances, "roles": b20SlotRoles,
			"roleAdmins": b20SlotRoleAdmins, "adminCount": b20SlotAdminCount,
			"transferPolicies": b20SlotTransferPolicies, "mintPolicy": b20SlotMintPolicy,
			"paused": b20SlotPaused, "supplyCap": b20SlotSupplyCap,
			"nonces": b20SlotNonces, "seizePolicies": b20SlotSeizePolicies,
		},
		b20AssetNamespace: {
			"decimals": b20AssetSlotDecimals, "multiplier": b20AssetSlotMultiplier,
			"announcements": b20AssetSlotAnnouncements, "extraMetadata": b20AssetSlotExtraMeta,
			"pendingMultiplier": b20AssetSlotPending,
		},
		b20StablecoinNamespace: {"currency": b20StablecoinSlotCurrency},
		b20PolicyNamespace: {
			"policies": polSlotPolicies, "members": polSlotMembers,
			"pendingAdmins": polSlotPendingAdmins, "counter": polSlotCounter,
			"children": polSlotChildren,
		},
		b20ActivationNamespace: {"features": actSlotFeatures},
	}

	var ref struct {
		Namespaces []struct {
			Name   string `json:"name"`
			Root   string `json:"root"`
			Fields []struct {
				Slot uint64 `json:"slot"`
				Name string `json:"name"`
			} `json:"fields"`
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

	const regen = "update the fixture, and BEP-702 3.17 with it"

	seenNS := map[string]bool{}
	for _, ns := range ref.Namespaces {
		fields, known := live[ns.Name]
		if !known {
			t.Errorf("the fixture names namespace %q, which no constant declares — %s",
				ns.Name, regen)
			continue
		}
		seenNS[ns.Name] = true
		// Recomputed from the code's own string: a rename moves every slot in the
		// namespace, so a stale root here would reach the spec unnoticed.
		if want := erc7201Root(ns.Name).Hex(); ns.Root != want {
			t.Errorf("namespace %q: fixture root %s, the code derives %s — %s",
				ns.Name, ns.Root, want, regen)
		}
		seen := map[string]bool{}
		for _, f := range ns.Fields {
			seen[f.Name] = true
			slot, ok := fields[f.Name]
			if !ok {
				t.Errorf("%s: the fixture names field %q, which no constant declares — %s",
					ns.Name, f.Name, regen)
			} else if slot != f.Slot {
				t.Errorf("%s.%s is slot %d in the fixture and %d in the code — %s",
					ns.Name, f.Name, f.Slot, slot, regen)
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
				"reimplementation cannot place — %s", ns.Name, strings.Join(missing, ", "), regen)
		}
	}
	for ns := range live {
		if !seenNS[ns] {
			t.Errorf("namespace %q is absent from the fixture, so BEP-702 3.17 will not "+
				"mention it — %s", ns, regen)
		}
	}
	if ref.Derivation.StringMaxLen != b20MaxStringLen {
		t.Errorf("the fixture caps a string at %d, the code at %d — %s",
			ref.Derivation.StringMaxLen, b20MaxStringLen, regen)
	}
}
