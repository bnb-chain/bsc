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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// encodeUpdateParam builds the governance call: a canonical feature name and a
// 32-byte value, non-zero to activate.
func encodeUpdateParam(key string, on bool) []byte {
	var v common.Hash
	if on {
		v[31] = 1
	}
	out := append([]byte{}, selUpdateParam[:]...)
	out = append(out, u256hash(0x40).Bytes()...)                         // offset: key
	out = append(out, u256hash(0x40+32+32*wordsOf(len(key))).Bytes()...) // offset: value
	out = append(out, u256hash(uint64(len(key))).Bytes()...)
	out = append(out, padRight32([]byte(key))...)
	out = append(out, u256hash(32).Bytes()...)
	out = append(out, v.Bytes()...)
	return out
}

// encodeUpdateParamRaw builds updateParam(string,bytes) with the value passed
// through verbatim, so a test can submit a wrong-length one.
func encodeUpdateParamRaw(key string, value []byte) []byte {
	out := append([]byte{}, selUpdateParam[:]...)
	out = append(out, u256hash(0x40).Bytes()...)
	out = append(out, u256hash(0x40+32+32*wordsOf(len(key))).Bytes()...)
	out = append(out, u256hash(uint64(len(key))).Bytes()...)
	out = append(out, padRight32([]byte(key))...)
	out = append(out, u256hash(uint64(len(value))).Bytes()...)
	out = append(out, padRight32(value)...)
	return out
}

func wordsOf(n int) uint64 {
	if n == 0 {
		return 0
	}
	return uint64((n + 31) / 32)
}

func padRight32(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, 32*((len(b)+31)/32))
	copy(out, b)
	return out
}

// featureNameAsset and featureNameStablecoin are the canonical names the feature
// identifiers hash from, spelled here so a rename has to change the tests too.
const (
	featureNameAsset      = "bsc.b20_asset"
	featureNameStablecoin = "bsc.b20_stablecoin"
	featureNamePolicy     = "bsc.policy_registry"
)

// TestB20ActivationRegistry exercises the switch itself: reads never revert,
// writes are governance-only, and a no-op is surfaced rather than accepted.
func TestB20ActivationRegistry(t *testing.T) {
	_, evm := newB20EVM(t)
	reg := B20ActivationRegistryAddress
	gov := params.B20GovHubAddress
	stranger := common.HexToAddress("0x5747a9e")

	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, reg, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return ret, err
	}
	// A feature the harness did not seed, so it starts inactive.
	const name = "bsc.something_later"
	feature := crypto.Keccak256Hash([]byte(name))

	// Reads never revert, whatever the flag.
	if ret, err := call(stranger, b20Call(selIsActivated, feature)); err != nil {
		t.Fatalf("isActivated: %v", err)
	} else if !bytes.Equal(ret, encBool(false)) {
		t.Errorf("isActivated = %x, want false", ret)
	}
	if _, err := call(stranger, b20Call(selCheckActivated, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("checkActivated on an inactive feature: err = %v, want a revert", err)
	}

	// Only GovHub may write.
	if _, err := call(stranger, encodeUpdateParam(name, true)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("updateParam from a stranger: err = %v, want a revert", err)
	}
	if _, err := call(gov, encodeUpdateParam(name, true)); err != nil {
		t.Fatalf("updateParam from GovHub: %v", err)
	}
	if ret, _ := call(stranger, b20Call(selIsActivated, feature)); !bytes.Equal(ret, encBool(true)) {
		t.Errorf("isActivated after activation = %x, want true", ret)
	}

	// No-ops are surfaced in both directions.
	if _, err := call(gov, encodeUpdateParam(name, true)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("activating an active feature: err = %v, want AlreadyActivated", err)
	}
	if _, err := call(gov, encodeUpdateParam(name, false)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := call(gov, encodeUpdateParam(name, false)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("deactivating an inactive feature: err = %v, want FeatureNotActivated", err)
	}

	// An empty name and a mis-sized value are both rejected.
	if _, err := call(gov, encodeUpdateParam("", true)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("empty key: err = %v, want InvalidValue", err)
	}
}

// policies, and nothing on a token that already exists (BEP-702 3.15).
func TestB20ActivationGates(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// A token created while the feature is open.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0xa1"), creator, initCalls))
	if err != nil {
		t.Fatalf("createB20 while activated: %v", err)
	}
	token := common.BytesToAddress(ret)

	// Deactivate the Asset variant.
	if _, err := call(params.B20GovHubAddress, B20ActivationRegistryAddress, encodeUpdateParam(featureNameAsset, false)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Creation stops.
	if _, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0xa2"), creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createB20 while deactivated err = %v, want FeatureNotActivated", err)
	}
	// The other variant is unaffected — the switch is per feature.
	if _, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantStablecoin, common.HexToHash("0xa3"), creator, nil)); err != nil {
		t.Fatalf("stablecoin creation must be unaffected: %v", err)
	}

	// The existing token keeps working: transfers, reads, everything.
	if _, err := call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(10))); err != nil {
		t.Fatalf("transfer on a live token must not be gated: %v", err)
	}
	view := newUnmeteredB20Storage(statedb, token)
	if got := view.balanceOf(b20Bob).Uint64(); got != 10 {
		t.Fatalf("bob balance = %d, want 10", got)
	}

	// PolicyRegistry: reads stay open, writes stop.
	if _, err := call(params.B20GovHubAddress, B20ActivationRegistryAddress, encodeUpdateParam(featureNamePolicy, false)); err != nil {
		t.Fatalf("deactivate policy registry: %v", err)
	}
	if _, err := call(creator, B20PolicyRegistryAddress, b20Call(selIsAuthorized, u256hash(0), addrKey(b20Alice))); err != nil {
		t.Fatalf("isAuthorized must never be gated: %v", err)
	}
	if _, err := call(creator, B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createPolicy while deactivated err = %v, want FeatureNotActivated", err)
	}
}

// TestB20RegistrySentinel pins the fix for the reaping hazard: a registry has
// no factory to create it, so its first write must plant the account sentinel.
// Storage alone leaves the account EIP-161-empty, and a clearing pass would
// take every flag and policy with it (BEP-702 3.16).
func TestB20RegistrySentinel(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc0ffee")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Strip the sentinel the fork planted, so the registry is bare going in. This
	// is deliberately a state the fork never leaves behind — the point is that the
	// guard does not depend on the fork having run, which is what makes it a
	// backstop rather than a duplicate of the seeding.
	statedb.SetCode(B20PolicyRegistryAddress, nil, tracing.CodeChangeContractCreation)
	if got := statedb.GetCodeHash(B20PolicyRegistryAddress); got == b20MarkerCodeHash {
		t.Fatal("precondition: the policy registry should start without a sentinel")
	}
	if _, err := call(creator, B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist))); err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	if got := statedb.GetCodeHash(B20PolicyRegistryAddress); got != b20MarkerCodeHash {
		t.Fatalf("policy registry code hash = %x, want the sentinel %x", got, b20MarkerCodeHash)
	}

	// The account must now survive a clearing pass with its storage intact.
	statedb.Finalise(true)
	if got := statedb.GetCodeHash(B20PolicyRegistryAddress); got != b20MarkerCodeHash {
		t.Fatal("the policy registry was reaped despite the sentinel")
	}

	// The activation registry is the same story. Idempotence in gas terms is
	// asserted precisely by TestB20EnsureSentinelIdempotent; here the point is
	// only that a later write leaves the planted sentinel alone.
	const laterName = "bsc.something_else"
	if _, err := call(params.B20GovHubAddress, B20ActivationRegistryAddress,
		encodeUpdateParam(laterName, true)); err != nil {
		t.Fatalf("activate: %v", err)
	}
	planted := statedb.GetCodeHash(B20ActivationRegistryAddress)
	if planted != b20MarkerCodeHash {
		t.Fatalf("activation registry code hash = %x, want the sentinel %x", planted, b20MarkerCodeHash)
	}
	if _, err := call(params.B20GovHubAddress, B20ActivationRegistryAddress,
		encodeUpdateParam(laterName, false)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if after := statedb.GetCodeHash(B20ActivationRegistryAddress); after != planted {
		t.Fatalf("the sentinel changed on a subsequent write: %x, want %x", after, planted)
	}
}

// TestB20EnsureSentinelIdempotent asserts the write-once property exactly, at
// the unit level. A loose "cheaper than CreateGas" bound is not enough: with
// the guard removed the account already has code, so CreateGas is not charged
// again and only the deposit and hash costs leak — a few hundred gas that a
// coarse bound cannot see. The second call must therefore charge the warm
// account access and nothing else.
func TestB20EnsureSentinelIdempotent(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	addr := B20PolicyRegistryAddress
	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: addr, gas: &gas}

	charged := func(fn func()) uint64 {
		before := gas.RegularGas
		fn()
		return before - gas.RegularGas
	}

	codeWrite := params.CreateGas +
		params.CreateDataGas*uint64(len(B20MarkerCode)) +
		params.Keccak256Gas + params.Keccak256WordGas
	first := charged(ctx.ensureSentinel)
	if first < codeWrite {
		t.Fatalf("first ensureSentinel charged %d, want at least the code write %d", first, codeWrite)
	}
	if statedb.GetCodeHash(addr) != b20MarkerCodeHash {
		t.Fatal("first ensureSentinel did not plant the sentinel")
	}

	second := charged(ctx.ensureSentinel)
	if second != params.WarmStorageReadCostEIP2929 {
		t.Fatalf("second ensureSentinel charged %d, want %d — the warm account access alone; "+
			"anything more means the code write ran again",
			second, params.WarmStorageReadCostEIP2929)
	}
}

// TestB20NeverOverwritesForeignCode pins BEP-702 3.4 and 3.16: an address that
// already carries code is occupied, whoever put it there. createB20 must refuse
// it rather than plant the sentinel over it, and a registry must not either.
func TestB20NeverOverwritesForeignCode(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc0de")
	foreign := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // a plausible runtime stub

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// A token address that is occupied by something that is not a B20 token.
	salt := common.HexToHash("0xf01e")
	predicted := b20DeriveAddress(b20VariantAsset, creator, salt)
	statedb.SetCode(predicted, foreign, tracing.CodeChangeContractCreation)
	foreignHash := statedb.GetCodeHash(predicted)

	if _, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createB20 at an occupied address err = %v, want TokenAlreadyExists", err)
	}
	if got := statedb.GetCodeHash(predicted); got != foreignHash {
		t.Fatalf("foreign code was overwritten: hash %x, want %x", got, foreignHash)
	}

	// A registry that somehow carries foreign code: the first write must not
	// plant over it. The account is already non-empty, so nothing is at risk.
	reg := B20PolicyRegistryAddress
	statedb.SetCode(reg, foreign, tracing.CodeChangeContractCreation)
	if _, err := call(creator, reg, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist))); err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	if got := statedb.GetCodeHash(reg); got != foreignHash {
		t.Fatalf("registry foreign code was overwritten: hash %x, want %x", got, foreignHash)
	}
}

// TestB20ParamKeySpaceIsBounded pins the parameter key space. The namespace is
// open inside the feature prefix so a later BEP can add one by naming it, and
// closed outside it — GovHub reports a target's revert in an event and leaves the
// proposal successful, so a key accepted by accident would read as a deliberate
// governance action that in fact gated nothing.
func TestB20ParamKeySpaceIsBounded(t *testing.T) {
	_, evm := newB20EVM(t)
	gov := params.B20GovHubAddress
	call := func(input []byte) error {
		_, _, err := evm.Call(gov, B20ActivationRegistryAddress, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return err
	}
	word := func(n byte) []byte { var h common.Hash; h[31] = n; return h.Bytes() }

	for _, tc := range []struct {
		name   string
		key    string
		value  []byte
		accept bool
	}{
		{"a feature under the prefix", "bsc.something_later", word(1), true},
		{"an unprefixed key", "something_later", word(1), false},
		{"the bare prefix", "bsc.", word(1), false},
		{"an empty key", "", word(1), false},
		{"a wrong-length value", "bsc.another", make([]byte, 20), false},
	} {
		err := call(encodeUpdateParamRaw(tc.key, tc.value))
		if tc.accept && err != nil {
			t.Errorf("%s: err = %v, want accepted", tc.name, err)
		}
		if !tc.accept && !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want a revert", tc.name, err)
		}
	}
}
