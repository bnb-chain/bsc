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
	"bytes"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20RevertData verifies typed revert payloads travel end to end: a
// business-rule failure inside a B20 precompile surfaces through evm.Call as
// (ABI-encoded error, ErrExecutionReverted), exactly like a Solidity
// `revert CustomError(...)`.
func TestB20RevertData(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		b20Call(selGrantRole, rolePause, addrKey(creator)),
		b20Call(selGrantRole, roleUnpause, addrKey(creator)),
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(100)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0x5e"), creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// ContractPaused(TRANSFER): selector ++ uint8 word.
	if _, err := call(creator, token, b20CallU8Array(selPause, byte(b20PauseTransfer))); err != nil {
		t.Fatalf("pause: %v", err)
	}
	ret, err = call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("paused transfer err = %v, want ErrExecutionReverted", err)
	}
	want := append(append([]byte{}, errSelContractPaused[:]...), wU8(b20PauseTransfer).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want ContractPaused(TRANSFER) = %x", ret, want)
	}

	// InsufficientBalance(sender, balance, needed) carries the observed values.
	if _, err := call(creator, token, b20CallU8Array(selUnpause, byte(b20PauseTransfer))); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	ret, err = call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1000)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("over-balance transfer err = %v, want ErrExecutionReverted", err)
	}
	want = append([]byte{}, errSelInsufficientBalance[:]...)
	want = append(want, addrKey(b20Alice).Bytes()...)
	want = append(want, u256hash(100).Bytes()...)
	want = append(want, u256hash(1000).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want InsufficientBalance(alice,100,1000) = %x", ret, want)
	}

	// NonPayable(): a value-bearing call is refused across the routed space.
	ret, _, err = evm.Call(creator, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)), NewGasBudget(5_000_000), uint256.NewInt(7))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("value-bearing call err = %v, want ErrExecutionReverted", err)
	}
	if !bytes.Equal(ret, errSelNonPayable[:]) {
		t.Fatalf("revert data = %x, want NonPayable() = %x", ret, errSelNonPayable)
	}

	// The role and policy-scope ids themselves, which base-std publishes as full
	// hashes next to those selectors. A getter can return the right shape and the
	// wrong constant; these are what a token's storage is actually keyed on.
	for _, tc := range []struct {
		name string
		got  common.Hash
		want string
	}{
		{"SEIZE_ROLE", roleSeize, "0x3469b8b0d89e9604f8510ed143f74a8336d22955d4f83e23bf53d9414e27f432"},
		{"SEIZE_HOLDER_POLICY", scopeSeizeHolder, "0x1497ab2b67ebb0a75dd9cdd6aec9f0e64620e6b87e911af7a088ac12e58d9ef2"},
		{"SEIZE_RECEIVER_POLICY", scopeSeizeReceiver, "0xbf15b19caf5c77422c038bc25f26b8b815c3a14f6d04c6616076b81bcfe07b3d"},
	} {
		if got := tc.got.Hex(); got != tc.want {
			t.Errorf("%s = %s, want %s (base-std's published value)", tc.name, got, tc.want)
		}
	}

	// And the one event topic0 it publishes in full.
	const wantSeized = "0xa9aec5d8b86e2fa2fd6ac3af62f2622e3dfdab1967d4cbbb56a5df7d74cb887c"
	if got := b20TopicSeized.Hex(); got != wantSeized {
		t.Errorf("Seized topic0 = %s, want %s (base-std's published value)", got, wantSeized)
	}
	const wantComposite = "0x4ff6adaab31b0df87aa7b8b7320c52b8b3b5eede3bf28a6baaaa8b8b7e1d6363"
	if got := b20TopicCompositeUpdated.Hex(); got != wantComposite {
		t.Errorf("CompositePolicyUpdated topic0 = %s, want %s (base-std's published value)", got, wantComposite)
	}
}

// TestB20UndecodableCalldataRevertsEmpty pins BEP-702 3.2's second failure kind:
// calldata that cannot be decoded reverts with no returndata at all, across
// every entry point.
func TestB20UndecodableCalldataRevertsEmpty(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(creator, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Open the Asset feature and create a token so the token path is live too.
	if _, err := call(B20ActivationRegistryAddress, encodeUpdateParam(featureNameAsset, true)); err != nil {
		// The harness seeds every feature already; activating again is fine to skip.
		_ = err
	}
	ret, err := call(B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0xd0"), creator, nil))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	targets := map[string]common.Address{
		"factory":             B20FactoryAddress,
		"policy registry":     B20PolicyRegistryAddress,
		"activation registry": B20ActivationRegistryAddress,
		"token":               token,
	}
	inputs := map[string][]byte{
		"unknown selector":      {0xde, 0xad, 0xbe, 0xef},
		"empty calldata":        {},
		"shorter than selector": {0x01, 0x02, 0x03},
		"selector, no args":     append([]byte{}, selBalanceOf[:]...),
	}
	for tname, to := range targets {
		for iname, input := range inputs {
			got, err := call(to, input)
			if !errors.Is(err, ErrExecutionReverted) {
				t.Errorf("%s / %s: err = %v, want revert", tname, iname, err)
				continue
			}
			if len(got) != 0 {
				t.Errorf("%s / %s: returndata = %x, want empty", tname, iname, got)
			}
		}
	}
}

// TestB20PublishedValuesMatchBaseStd pins every selector, role id and topic0
// base-std states as a literal in its own changelog tables, so the values can be
// checked by eye against the reference rather than trusted.
func TestB20PublishedValuesMatchBaseStd(t *testing.T) {
	for _, tc := range []struct {
		sig  string
		got  [4]byte
		want string
	}{
		{"announce(bytes[],string,string,string)", selAnnounce, "595135dd"},
		{"multiplier()", selMultiplier, "1b3ed722"},
		{"toScaledBalance(uint256)", selToScaledBalance, "04f04c99"},
		{"toRawBalance(uint256)", selToRawBalance, "0ca06c44"},
		{"scaledBalanceOf(address)", selScaledBalanceOf, "1da24f3e"},
		{"updateMultiplier(uint256)", selUpdateMultiplier, "5ffe6146"},
		{"OPERATOR_ROLE()", selOperatorRole, "f5b541a6"},
		{"WAD_PRECISION()", selWadPrecision, "664808a8"},
		// The seize surface, published in base-std's Cobalt seize note. It is the
		// one Cobalt group we implement — it replaces the burn-based freeze-and
		// seize that base-std deprecated and we never carried.
		{"seizeWithMemo(address,address,uint256,bytes32)", selSeizeWithMemo, "f916d81b"},
		{"SEIZE_ROLE()", selSeizeRole, "3c7e9ba5"},
		{"SEIZE_HOLDER_POLICY()", selSeizeHolderScope, "b279d311"},
		{"SEIZE_RECEIVER_POLICY()", selSeizeReceiverScope, "b31da27f"},
		// The scheduled multiplier and composite policies, the two Cobalt features
		// adopted after seize.
		{"uiMultiplier()", selUIMultiplier, "a60bf13d"},
		{"toUIAmount(uint256)", selToUIAmount, "3248d4ff"},
		{"fromUIAmount(uint256)", selFromUIAmount, "65cd9b3c"},
		{"balanceOfUI(address)", selBalanceOfUI, "437a9958"},
		{"totalSupplyUI()", selTotalSupplyUI, "9bea6429"},
		{"newUIMultiplier()", selNewUIMultiplier, "dc767007"},
		{"effectiveAt()", selEffectiveAt, "97a4064f"},
		{"updateUIMultiplier(uint256,uint256)", selUpdateUIMultiplier, "628e600f"},
		{"cancelUIMultiplierUpdate()", selCancelUIMultiplier, "2c97a0f0"},
		{"MAX_UI_MULTIPLIER()", selMaxUIMultiplier, "785c0cf0"},
		{"supportsInterface(bytes4)", selSupportsInterface, "01ffc9a7"},
		{"createCompositePolicy(address,uint8,uint64[])", selCreateComposite, "6fdd1491"},
		{"updateComposite(uint64,uint64[])", selUpdateComposite, "bfe142c0"},
		{"compositePolicyChildIds(uint64)", selCompositeChildIds, "7c40df74"},
		{"MIN_COMPOSITE_CHILD_POLICIES()", selMinCompositeChildren, "b3ae29f7"},
		{"MAX_COMPOSITE_CHILD_POLICIES()", selMaxCompositeChildren, "54309870"},
	} {
		if got := common.Bytes2Hex(tc.got[:]); got != tc.want {
			t.Errorf("%s = 0x%s, want 0x%s (base-std's published selector)", tc.sig, got, tc.want)
		}
		if got := selector(tc.sig); got != tc.got {
			t.Errorf("%s does not hash to the registered selector: %x vs %x", tc.sig, got, tc.got)
		}
	}
}

// TestB20ConstantsMatchBaseStd pins the numeric constants base-std publishes in
// B20Constants.sol and its policy-id codec.
func TestB20ConstantsMatchBaseStd(t *testing.T) {
	// B20Constants.sol
	if b20MinDecimals != 6 || b20MaxDecimals != 18 {
		t.Errorf("asset decimals bounds = [%d, %d], want [6, 18]", b20MinDecimals, b20MaxDecimals)
	}
	wantCap := new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))
	if !b20NoSupplyCap.Eq(wantCap) {
		t.Errorf("MAX_SUPPLY_CAP = %s, want type(uint128).max", b20NoSupplyCap)
	}
	// ALL_FEATURES_PAUSED is 15 at Cobalt, which is the four features 0..3 —
	// TRANSFER, MINT, BURN and the SEIZE ordinal Cobalt appended.
	if b20PauseSeize != 3 {
		t.Errorf("SEIZE pause ordinal = %d, want 3; PausableFeature is append-only", b20PauseSeize)
	}
	if mask := 1<<(b20PauseSeize+1) - 1; mask != 15 {
		t.Errorf("all-features mask = %d, want 15", mask)
	}

	// PolicyType, and the id codec: top byte is the type, low 56 bits the counter.
	if b20PolicyBlocklist != 0 || b20PolicyAllowlist != 1 ||
		b20PolicyUnion != 2 || b20PolicyIntersect != 3 {
		t.Errorf("PolicyType = {%d, %d, %d, %d}, want {0, 1, 2, 3} — the enum is "+
			"append-only, so an existing ordinal moving repoints every stored id",
			b20PolicyBlocklist, b20PolicyAllowlist, b20PolicyUnion, b20PolicyIntersect)
	}
	if b20CompositeMinChildren != 2 || b20CompositeMaxChildren != 4 {
		t.Errorf("composite child bounds = [%d, %d], want [2, 4]",
			b20CompositeMinChildren, b20CompositeMaxChildren)
	}
	if got := polIDType(uint64(b20PolicyAllowlist)<<56 | 42); got != b20PolicyAllowlist {
		t.Errorf("polIDType of a packed allowlist id = %d, want %d", got, b20PolicyAllowlist)
	}
	// The two built-ins take counters 0 and 1 under their own types.
	if b20PolicyAlwaysAllow != 0 {
		t.Errorf("ALWAYS_ALLOW = %d, want 0 (BLOCKLIST type, counter 0)", b20PolicyAlwaysAllow)
	}
	if want := uint64(b20PolicyAllowlist)<<56 | 1; b20PolicyAlwaysBlock != want {
		t.Errorf("ALWAYS_BLOCK = %#x, want %#x (ALLOWLIST type, counter 1)", b20PolicyAlwaysBlock, want)
	}

	// The membership batch limit is ours: base-std raises BatchSizeTooLarge for
	// "the registry limit" without publishing the number in any interface. 64 comes
	// from BEP-702 3.8, so this pins the spec rather than the reference.
	if b20PolicyBatchMax != 64 {
		t.Errorf("membership batch limit = %d, want 64 (BEP-702 3.8)", b20PolicyBatchMax)
	}
}

// TestB20ErrorOverloadsAreDeliberate is what remains of a constraint solc used to
// enforce. Solidity forbids two errors of one name in one interface, and the
// deleted .sol mirror failed to compile until a duplicate PolicyNotFound was
// split — the only time anything checked this.
//
// The check cannot be rebuilt from the specification: BEP-702 declares errors
// inside an interface only for IActivationRegistry, four of the fifty-five, so
// there is no attribution to test against and inventing one would pin this
// implementation to its own guess. What is checkable without inventing anything
// is that the overloaded names stay a closed, named set. A third form appearing
// beside an existing name is the accidental case — the one the mirror caught —
// and it fails here.
func TestB20ErrorOverloadsAreDeliberate(t *testing.T) {
	// Each entry is legitimate only because the two forms live in different
	// interfaces, which BEP-702 states in prose rather than in a declaration.
	allowed := map[string]string{
		"PolicyNotFound": "IPolicyRegistry answers about a policy the caller named, so " +
			"the argument-less form suffices; IN20 names the id it could not find (3.8)",
		"Unauthorized": "IPolicyRegistry rejects a caller who is not the policy admin; " +
			"IActivationRegistry names the caller (3.8, 3.15)",
	}

	forms := map[string][]string{}
	for sig := range b20ErrSigs {
		name := sig[:strings.IndexByte(sig, '(')]
		forms[name] = append(forms[name], sig)
	}
	for name, sigs := range forms {
		if len(sigs) == 1 {
			continue
		}
		sort.Strings(sigs)
		if _, ok := allowed[name]; !ok {
			t.Errorf("%s is registered in %d forms (%s) and is not a declared overload. "+
				"Two errors of one name are illegal in one Solidity interface, so either "+
				"they belong to different ones — say which, here — or one of them is a "+
				"mistake", name, len(sigs), strings.Join(sigs, ", "))
		}
		if len(sigs) > 2 {
			t.Errorf("%s has %d forms (%s); the exemption covers a pair in two interfaces, "+
				"not a family", name, len(sigs), strings.Join(sigs, ", "))
		}
	}
	// And the exemptions must stay earned: an entry whose second form was removed
	// is a stale licence for the next duplicate to slip through.
	for name := range allowed {
		if len(forms[name]) < 2 {
			t.Errorf("%s is exempted as an overload but has %d form(s); drop the exemption",
				name, len(forms[name]))
		}
	}
}
