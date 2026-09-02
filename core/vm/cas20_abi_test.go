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

// TestCAS20RevertData verifies typed revert payloads travel end to end: a
// business-rule failure inside a CAS20 precompile surfaces through evm.Call as
// (ABI-encoded error, ErrExecutionReverted), exactly like a Solidity
// `revert CustomError(...)`.
func TestCAS20RevertData(t *testing.T) {
	_, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		cas20Call(selGrantRole, rolePause, addrKey(creator)),
		cas20Call(selGrantRole, roleUnpause, addrKey(creator)),
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Alice), u256hash(100)),
	}
	ret, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x5e"), creator, initCalls))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// ContractPaused(TRANSFER): selector ++ uint8 word.
	if _, err := call(creator, token, cas20CallU8Array(selPause, byte(cas20PauseTransfer))); err != nil {
		t.Fatalf("pause: %v", err)
	}
	ret, err = call(cas20Alice, token, cas20Call(selTransfer, addrKey(cas20Bob), u256hash(1)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("paused transfer err = %v, want ErrExecutionReverted", err)
	}
	want := append(append([]byte{}, errSelContractPaused[:]...), wU8(cas20PauseTransfer).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want ContractPaused(TRANSFER) = %x", ret, want)
	}

	// InsufficientBalance(sender, balance, needed) carries the observed values.
	if _, err := call(creator, token, cas20CallU8Array(selUnpause, byte(cas20PauseTransfer))); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	ret, err = call(cas20Alice, token, cas20Call(selTransfer, addrKey(cas20Bob), u256hash(1000)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("over-balance transfer err = %v, want ErrExecutionReverted", err)
	}
	want = append([]byte{}, errSelInsufficientBalance[:]...)
	want = append(want, addrKey(cas20Alice).Bytes()...)
	want = append(want, u256hash(100).Bytes()...)
	want = append(want, u256hash(1000).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want InsufficientBalance(alice,100,1000) = %x", ret, want)
	}

	// NonPayable(): a value-bearing call is refused across the routed space.
	ret, _, err = evm.Call(creator, token, cas20Call(selTransfer, addrKey(cas20Bob), u256hash(1)), NewGasBudget(5_000_000), uint256.NewInt(7))
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
	if got := cas20TopicSeized.Hex(); got != wantSeized {
		t.Errorf("Seized topic0 = %s, want %s (base-std's published value)", got, wantSeized)
	}
	const wantComposite = "0x4ff6adaab31b0df87aa7b8b7320c52b8b3b5eede3bf28a6baaaa8b8b7e1d6363"
	if got := cas20TopicCompositeUpdated.Hex(); got != wantComposite {
		t.Errorf("CompositePolicyUpdated topic0 = %s, want %s (base-std's published value)", got, wantComposite)
	}
}

// TestCAS20UndecodableCalldataRevertsEmpty pins BEP-702 3.2's second failure kind:
// calldata that cannot be decoded reverts with no returndata at all, across
// every entry point.
func TestCAS20UndecodableCalldataRevertsEmpty(t *testing.T) {
	_, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(creator, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Open the Asset feature and create a token so the token path is live too.
	if _, err := call(CAS20ActivationRegistryAddress, cas20Call(selActivate, featureCAS20Asset)); err != nil {
		// The harness seeds every feature already; activating again is fine to skip.
		_ = err
	}
	ret, err := call(CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xd0"), creator, nil))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	targets := map[string]common.Address{
		"factory":             CAS20FactoryAddress,
		"policy registry":     CAS20PolicyRegistryAddress,
		"activation registry": CAS20ActivationRegistryAddress,
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

// TestCAS20ErrorOverloadsAreDeliberate is what remains of a constraint solc used to
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
func TestCAS20ErrorOverloadsAreDeliberate(t *testing.T) {
	// Each entry is legitimate only because the two forms live in different
	// interfaces, which BEP-702 states in prose rather than in a declaration.
	allowed := map[string]string{
		"PolicyNotFound": "IPolicyRegistry answers about a policy the caller named, so " +
			"the argument-less form suffices; a token names the id it could not find (3.8)",
		"Unauthorized": "IPolicyRegistry rejects a caller who is not the policy admin; " +
			"IActivationRegistry names the caller (3.8, 3.15)",
	}

	forms := map[string][]string{}
	for sig := range cas20ErrSigs {
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
