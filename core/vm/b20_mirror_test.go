package vm

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// b20std/B20Std.sol is a published artifact — integrators compile against it —
// that nothing in this tree compiles or generates. Both bugs found on 2026-08-18
// lived there: a PolicyType two members short of the precompile's, and the two
// registry addresses swapped. Neither could be caught by the ABI diff, since an
// enum is uint8 on the wire and a constant is not in any signature.
//
// The checks here are the ones a compiler would have made, run without needing
// one in the build. TestB20SolEnumsMatchGo and TestB20SingletonAddresses cover
// the two specific bugs; these cover the class.

// TestB20MirrorIsWellFormedSolidity catches the declarations solc rejects.
//
// The mirror did not compile at all: `error PolicyNotFound()` and
// `error PolicyNotFound(uint64)` were both declared in IB20, and errors cannot be
// overloaded ("Identifier already declared"). base-std puts the no-argument form
// in IPolicyRegistry, which is a different scope. The second failure was an
// address literal with a bad EIP-55 checksum, which Solidity rejects outright
// rather than accepting as a number.
func TestB20MirrorIsWellFormedSolidity(t *testing.T) {
	src := b20MirrorSource(t)

	// Errors are function-like but cannot be overloaded, so the name alone must
	// be unique within a scope. Events and functions can be, so those compare by
	// full signature.
	var scope string
	seen := map[string]string{} // "scope.identifier" -> where it was first seen
	decl := regexp.MustCompile(`^\s*(error|event|function)\s+(\w+)\s*\(([^)]*)\)`)
	for i, line := range strings.Split(src, "\n") {
		if m := regexp.MustCompile(`^(?:interface|library|contract)\s+(\w+)`).FindStringSubmatch(line); m != nil {
			scope = m[1]
			continue
		}
		m := decl.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kind, name, args := m[1], m[2], m[3]
		key := scope + "." + name
		if kind != "error" {
			key += "(" + args + ")"
		}
		if first, dup := seen[key]; dup {
			t.Errorf("%s:%d: %s %s is already declared in %s (%s). Solidity permits no "+
				"overload for errors — check which scope base-std declares each form in",
				"b20std/B20Std.sol", i+1, kind, name, scope, first)
			continue
		}
		seen[key] = "line " + strconv.Itoa(i+1)
	}

	// EIP-55: solc rejects a mixed-case address literal whose checksum is wrong,
	// so a hand-edited constant fails to compile rather than pointing somewhere
	// unintended. common.Address.Hex() emits the checksummed form.
	for _, m := range regexp.MustCompile(`(0x[0-9a-fA-F]{40})`).FindAllStringSubmatch(src, -1) {
		if want := common.HexToAddress(m[1]).Hex(); m[1] != want {
			t.Errorf("address literal %s is not EIP-55 checksummed; solc wants %s", m[1], want)
		}
	}
}

// TestB20MirrorMutabilityMatchesBaseStd diffs `view` against `pure` per function.
//
// Neither is in a selector, so the surface diff is blind to both, and the
// precompile has no notion of either. It still matters to a caller: a `pure`
// function may only call a `pure` one, so where our mirror said `pure` and
// base-std says `view` — nineteen constant getters, every role and policy scope
// among them — a wrapper compiling here would not compile against base-std.
// base-std declares every one of them `view`, including the compile-time
// constants, so this pins its convention rather than what the precompile does.
func TestB20MirrorMutabilityMatchesBaseStd(t *testing.T) {
	raw, err := os.ReadFile("testdata/basestd_surface.json")
	if err != nil {
		t.Fatalf("read the base-std surface: %v", err)
	}
	var ref struct {
		Functions []struct {
			Sig string `json:"sig"`
			Mut string `json:"mut"`
		} `json:"functions"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatalf("parse the base-std surface: %v", err)
	}
	want := map[string]string{}
	for _, f := range ref.Functions {
		if f.Mut == "" {
			t.Fatalf("the fixture records no mutability for %s; regenerate it", f.Sig)
		}
		want[f.Sig] = f.Mut
	}

	ours := b20MirrorMutability(b20MirrorSource(t))
	if len(ours) == 0 {
		t.Fatal("parsed no functions out of the mirror")
	}
	var mismatched []string
	for sig, got := range ours {
		if w, ok := want[sig]; ok && w != got && b20MutabilityDivergence[sig] == "" {
			mismatched = append(mismatched, sig+": ours "+got+", base-std "+w)
		}
	}
	sort.Strings(mismatched)
	for _, m := range mismatched {
		t.Errorf("state mutability differs from base-std — %s", m)
	}
}

// b20MutabilityDivergence records where we deliberately declare a different
// mutability from base-std, with the reason — the same rule as
// b20IntentionalAddition.
var b20MutabilityDivergence = map[string]string{
	"createB20(uint8,bytes32,bytes,bytes[])": "base-std declares it payable while its own " +
		"natspec says \"Reverts with NonPayable when ETH is attached to the call\", and names " +
		"the selector nonpayable in the error's own doc comment. Runtime behaviour is the same " +
		"either way — b20EnterCall refuses any value before charging — so the only difference " +
		"is that payable lets a caller compile `createB20{value: x}(...)`, which then always " +
		"reverts. Declaring it nonpayable moves that to compile time.",
}

func b20MirrorSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("b20std/B20Std.sol")
	if err != nil {
		t.Fatalf("read the interface mirror: %v", err)
	}
	// Comments hold signatures in prose; strip them so they are not parsed.
	out := regexp.MustCompile(`(?m)//[^\n]*`).ReplaceAll(src, nil)
	return string(regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAll(out, nil))
}

// b20MirrorMutability returns {signature: view|pure|payable|nonpayable}, with
// argument types lowered the way the ABI encodes them.
func b20MirrorMutability(src string) map[string]string {
	flat := strings.Join(strings.Fields(src), " ")
	out := map[string]string{}
	re := regexp.MustCompile(`function (\w+) ?\(([^)]*)\) (?:external|public|internal|private)? ?(view|pure|payable)?`)
	for _, m := range re.FindAllStringSubmatch(flat, -1) {
		mut := m[3]
		if mut == "" {
			mut = "nonpayable"
		}
		out[m[1]+"("+strings.Join(b20MirrorArgTypes(m[2]), ",")+")"] = mut
	}
	return out
}

func b20MirrorArgTypes(raw string) []string {
	var types []string
	strip := regexp.MustCompile(`\b(calldata|memory|storage|indexed|payable)\b`)
	for _, arg := range strings.Split(raw, ",") {
		arg = strings.TrimSpace(strip.ReplaceAllString(arg, ""))
		if arg == "" {
			continue
		}
		typ := strings.Fields(arg)[0]
		// The enums lower to their encoding, as the generator does for base-std.
		for _, e := range []string{"Variant", "PolicyType", "PausableFeature"} {
			if typ == e || strings.HasPrefix(typ, e+"[") {
				typ = "uint8" + strings.TrimPrefix(typ, e)
			}
		}
		types = append(types, typ)
	}
	return types
}
