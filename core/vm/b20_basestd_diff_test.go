package vm

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestB20SurfaceMatchesBaseStd diffs our whole registered ABI against base-std's,
// the B20 reference implementation's published interfaces.
//
// This exists because TestB20ABIBaseline cannot do it. That check is exhaustive
// in both directions and still could not have caught a single one of the
// divergences found on 2026-08-17 — a uint256 announcement id where base-std
// uses a string, and four events missing the actor and previous value — because
// the three things it compares (the Go code, b20std/B20Std.sol, BEP-702) are all
// ours. A passing consistency check reads like ABI verification and is not.
//
// Alignment was previously done by point query: probing a selector on Base
// mainnet, or reading its docs. That can only verify a signature someone already
// suspected, which is why the PolicyRegistry's events were corrected while the
// identical omission on IB20's went untouched. base-std is public and MIT, so the
// check can be a set difference instead of a hunch.
//
// The fixture is generated — see scripts/b20-basestd-surface.py — and carries the
// commit it came from. Selectors are not stored in it: hashing there would derive
// the same values from the same strings. TestB20PublishedValuesMatchBaseStd anchors
// the hashing separately, against literals base-std publishes itself.
func TestB20SurfaceMatchesBaseStd(t *testing.T) {
	raw, err := os.ReadFile("testdata/basestd_surface.json")
	if err != nil {
		t.Fatalf("read the base-std surface: %v", err)
	}
	var ref struct {
		Source    string     `json:"source"`
		Commit    string     `json:"commit"`
		Functions []refEntry `json:"functions"`
		Events    []refEntry `json:"events"`
		Errors    []refEntry `json:"errors"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatalf("parse the base-std surface: %v", err)
	}
	if ref.Commit == "" {
		t.Fatal("the fixture records no base-std commit; regenerate it")
	}

	ours := ourRegisteredSurface()
	if len(ours["function"]) == 0 {
		t.Fatal("no function signatures registered; the selector registry is empty")
	}

	for _, group := range []struct {
		kind    string
		entries []refEntry
		ours    map[string]bool
	}{
		{"function", ref.Functions, ours["function"]},
		{"event", ref.Events, ours["event"]},
		{"error", ref.Errors, ours["error"]},
	} {
		var berylGaps, cobaltGaps []string
		refHas := map[string]bool{}
		for _, e := range group.entries {
			refHas[e.Sig] = true
			if group.ours[e.Sig] {
				continue
			}
			if e.Fork == "cobalt" {
				cobaltGaps = append(cobaltGaps, e.Sig)
			} else {
				berylGaps = append(berylGaps, e.Sig)
			}
		}

		// A Beryl signature base-std has and we do not is live divergence: the
		// reference implementation ships it on Base today.
		for _, sig := range berylGaps {
			if reason := b20IntentionalOmission[sig]; reason != "" {
				continue
			}
			t.Errorf("%s %s is on Base today and we do not have it.\n"+
				"    Implement it, or record why not in b20IntentionalOmission.", group.kind, sig)
		}

		// A Cobalt one is a tracked gap. Failing on each would make every commit
		// wait on a fork we have not decided to follow; logging alone would rot,
		// since nobody reads a passing test's output. So the count is asserted:
		// implementing one, or a pin bump that adds one, has to come here.
		sort.Strings(cobaltGaps)
		if want := b20CobaltGaps[group.kind]; len(cobaltGaps) != want {
			t.Errorf("%d %s(s) from Cobalt are unimplemented, expected %d. Update "+
				"b20CobaltGaps if this is deliberate:\n  %s",
				len(cobaltGaps), group.kind, want, strings.Join(cobaltGaps, "\n  "))
		} else if len(cobaltGaps) > 0 {
			t.Logf("%d %s(s) deferred from Cobalt (additive per base-std's changelogs):\n  %s",
				len(cobaltGaps), group.kind, strings.Join(cobaltGaps, "\n  "))
		}

		// And the other direction: anything we register that base-std does not
		// have is a divergence someone has to own.
		var extra []string
		for sig := range group.ours {
			if !refHas[sig] && b20IntentionalAddition[sig] == "" {
				extra = append(extra, sig)
			}
		}
		sort.Strings(extra)
		for _, sig := range extra {
			t.Errorf("%s %s is ours alone and base-std has no counterpart.\n"+
				"    Remove it, or record why it exists in b20IntentionalAddition.", group.kind, sig)
		}
	}

	t.Logf("diffed against %s @ %s", ref.Source, ref.Commit[:12])
}

// b20CobaltGaps is how much of the Cobalt fork we have deliberately not
// implemented: the ERC-8056 scheduled multiplier and composite policies. Cobalt's
// third addition, transfer-based seize, we do implement — it replaces the
// burn-based path base-std deprecated and we never carried.
//
// Asserted rather than logged so that adopting a piece of Cobalt, or bumping the
// base-std pin onto a fork that adds more, has to be acknowledged here.
//
// What deferring costs, so the decision is made with it stated. Selectors and
// storage are safe: every Beryl selector and topic0 survives at Cobalt, and both
// additions take slot 4 of a namespace whose slots 0-3 are already in use, so no
// populated slot moves. Three existing behaviours do change, and the middle one is
// the reason this is not simply "add it whenever":
//
//   - updateMultiplier(uint256) keeps its selector but starts emitting
//     UIMultiplierUpdated as well, and UIMultiplierUpdateCancelled when it clears
//     a pending schedule. The log count per call changes, so the gas does too.
//   - multiplier() — an existing read — becomes time-dependent. Once any operator
//     schedules an update, "once block.timestamp >= effectiveAt, uiMultiplier() /
//     multiplier() flip on read. No event fires at maturation."
//     (base-std changelog/02_Cobalt_B20Asset_multiplier.md). An indexer rebuilding
//     state from the event stream alone silently diverges, and no signature diff
//     can show this.
//   - createPolicy and createPolicyWithAccounts change the revert for policyType
//     2 or 3 from Panic(0x21) to IncompatiblePolicyType(). Only reachable with
//     input that is invalid today, so it affects error decoding rather than any
//     working call.
//
// isAuthorized and the policy event streams also begin carrying composite ids, so
// a consumer asserting policyType is 0 or 1 needs widening.
// Only composite policies remain: createCompositePolicy, updateComposite,
// compositePolicyChildIds and the two child-count bounds, CompositePolicyUpdated,
// InvalidChildPolicy and ChildPoliciesOutsideOfRange. The ERC-8056 scheduled
// multiplier is implemented as of the Cobalt adoption.
var b20CobaltGaps = map[string]int{"function": 5, "event": 1, "error": 2}

type refEntry struct {
	Sig  string `json:"sig"`
	Fork string `json:"fork"`
}

// b20IntentionalAddition records every signature we register that base-std does
// not, with the reason. A bare list would become a dumping ground; requiring a
// sentence makes adding one a decision.
var b20IntentionalAddition = map[string]string{
	"setAdmin(address)": "BSC rotates the activation admin; Base returns a hardcoded constant " +
		"from admin() and stores none, so it needs no setter (BEP-702 3.15).",
	"AdminChanged(address,address,address)": "emitted by setAdmin, which Base does not have.",
	"ZeroAdminAddress()":                    "setAdmin rejects the zero address; Base has no setter to reject it in.",
	"variantOf(address)": "a convenience read derived from the address alone. Base's callers use " +
		"B20FactoryLib off-chain instead of a precompile call.",
	"Panic(uint256)": "Solidity's built-in, not an interface declaration — base-std does not " +
		"redeclare it either. Registered here because the precompile raises it directly.",
}

// b20IntentionalOmission records Beryl signatures we deliberately do not
// implement. Same rule: a reason, not a checkbox.
var b20IntentionalOmission = map[string]string{
	"burnBlocked(address,uint256)": "the legacy burn-based freeze-and-seize. base-std removed it " +
		"from IB20 and keeps the selector only for back-compat, recommending seizeWithMemo then " +
		"burn — which we implement. A chain launching after the deprecation carries no callers.",
	"BURN_BLOCKED_ROLE()":                    "gates burnBlocked, which we do not implement.",
	"BurnedBlocked(address,address,uint256)": "emitted by burnBlocked.",
	"AccountNotBlocked(address)":             "burnBlocked's revert.",
	"InvalidAmount()": "declared in IB20 as \"an amount argument was zero where a non-zero " +
		"value is required, not used for ERC-20 amount arguments\", but no method's natspec, " +
		"changelog entry or smoke journey in base-std says which operation raises it. Nothing " +
		"here needs it; revisit if a trigger is found in the reference implementation.",
}

// ourRegisteredSurface reads the live registries selector/eventTopic/b20ErrorSel
// populate, so the diff sees what the code actually dispatches on rather than
// what a second artifact says it does.
func ourRegisteredSurface() map[string]map[string]bool {
	out := map[string]map[string]bool{
		"function": {},
		"event":    {},
		"error":    {},
	}
	for sig := range b20FnSigs {
		out["function"][sig] = true
	}
	for sig := range b20EventSigs {
		out["event"][sig] = true
	}
	for sig := range b20ErrSigs {
		out["error"][sig] = true
	}
	return out
}
