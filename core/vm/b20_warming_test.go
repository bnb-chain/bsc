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
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// EIP-2929 warm/cold accounting for the B20 family. TestB20StorageGas covers the
// per-access prices; these cover which things are warm to begin with, who shares
// warmth with whom, and what a revert undoes — the parts a reimplementation has
// to match and that no price table states.

const b20Keccak64 = params.Keccak256Gas + 2*params.Keccak256WordGas

// warmed reports whether an (address, slot) pair is in the access list.
func warmed(db *state.StateDB, addr common.Address, slot common.Hash) bool {
	_, ok := db.SlotInAccessList(addr, slot)
	return ok
}

// TestB20AddressesAreNotPrewarmed pins the policy choice: a B20 address is warm
// only once something touches it, exactly like an ordinary account.
//
// Every classic precompile is warm from the first instruction of a transaction,
// because state.Prepare seeds the access list with ActivePrecompiles. B20 is
// dispatched from evm.precompile's dynamic fallback rather than from the address
// map, so none of its addresses appear in that list, and a first CALL to a token,
// the factory or a registry pays the 2600-gas cold surcharge.
//
// The token space could not be pre-warmed even in principle — it is a prefix, not
// an enumerable set — so listing only the three singletons would make the family
// inconsistent with itself for no benefit a transaction access list cannot buy.
func TestB20AddressesAreNotPrewarmed(t *testing.T) {
	rules := b20TestChainConfig().Rules(common.Big1, false, 1)
	if !rules.IsPasteur || !rules.IsInBSC {
		t.Fatal("the harness must have B20 active, or this proves nothing")
	}
	active := ActivePrecompiles(rules)

	inList := map[common.Address]bool{}
	for _, a := range active {
		inList[a] = true
	}
	// The control: ECRECOVER is warm, so the absence below is about B20 and not
	// about a misread of how Prepare works.
	if !inList[common.BytesToAddress([]byte{0x01})] {
		t.Fatal("ECRECOVER is not in ActivePrecompiles; the list is not what this test assumes")
	}
	for _, tc := range []struct {
		name string
		addr common.Address
	}{
		{"the factory", B20FactoryAddress},
		{"the ActivationRegistry", B20ActivationRegistryAddress},
		{"the PolicyRegistry", B20PolicyRegistryAddress},
		{"a token", b20Addr(b20VariantAsset, 1)},
	} {
		if inList[tc.addr] {
			t.Errorf("%s (%s) is in ActivePrecompiles, so every transaction would find it "+
				"warm. That is a consensus change: update BEP-702 3.14 and every "+
				"reimplementation, do not let it arrive as a side effect", tc.name, tc.addr.Hex())
		}
	}

	// And the consequence, through Prepare rather than by reasoning about it.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	sender, token := common.HexToAddress("0x5e0d"), b20Addr(b20VariantAsset, 1)
	other := b20Addr(b20VariantAsset, 2)
	statedb.Prepare(rules, sender, common.Address{}, &token, active, nil)
	if !statedb.AddressInAccessList(token) {
		t.Error("the transaction's own destination must be warm (Prepare adds dst)")
	}
	if statedb.AddressInAccessList(other) {
		t.Error("a token the transaction did not name is warm; nothing should have added it")
	}
	if statedb.AddressInAccessList(B20PolicyRegistryAddress) {
		t.Error("the PolicyRegistry is warm before anything touched it")
	}
}

// TestB20HonoursTransactionAccessList checks that an EIP-2930 access list buys
// the discount it is supposed to buy.
//
// The slots the precompile meters are the token's real storage keys, so a caller
// can name them in a transaction access list and pre-pay for the cold access.
// Nothing would break if the precompile ignored the list — it would just
// overcharge — which is why this needs a test rather than an argument.
func TestB20HonoursTransactionAccessList(t *testing.T) {
	rules := b20TestChainConfig().Rules(common.Big1, false, 1)
	token := b20Addr(b20VariantAsset, 1)

	// The same read, charged twice: once with the slot named in the access list,
	// once without.
	read := func(list types.AccessList) uint64 {
		t.Helper()
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		statedb.Prepare(rules, common.HexToAddress("0x5e0d"), common.Address{}, &token,
			ActivePrecompiles(rules), list)
		gas := NewGasBudget(1_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
		s := newMeteredB20Storage(ctx)
		before := gas.RegularGas
		_ = s.balanceOf(b20Alice)
		return before - gas.RegularGas
	}

	slot := b20Storage{token: token}.balanceSlot(b20Alice)
	cold := read(nil)
	warm := read(types.AccessList{{Address: token, StorageKeys: []common.Hash{slot}}})

	if want := b20Keccak64 + params.ColdSloadCostEIP2929; cold != want {
		t.Errorf("unlisted read charged %d, want %d (keccak + cold)", cold, want)
	}
	if want := b20Keccak64 + params.WarmStorageReadCostEIP2929; warm != want {
		t.Errorf("read of a slot named in the access list charged %d, want %d (keccak + warm). "+
			"The precompile is ignoring the transaction's access list, so a caller who "+
			"pre-paid for the slot pays the cold surcharge twice", warm, want)
	}
	// The keccak is charged either way, so the whole difference is the surcharge.
	if got, want := cold-warm, params.ColdSloadCostEIP2929-params.WarmStorageReadCostEIP2929; got != want {
		t.Errorf("the access list saved %d gas, want %d", got, want)
	}
}

// TestB20ForeignSlotReadWarmsTheForeignAddress records a side effect that has no
// counterpart in bytecode, so no reimplementation can infer it.
//
// A token consults the registries for its own gating (BEP-702 3.14: a foreign
// storage read is charged as storage, with no account-access surcharge). Warming
// a slot warms its address too — StateDB.AddSlotToAccessList adds both, and its
// comment calls the address branch unreachable "since there is no way to enter
// the scope of 'address' without having the 'address' become already added". A
// B20 token is that way: it reads the registry's storage without ever entering
// the registry's frame.
//
// So the registry address is warm afterwards, and a later CALL to it costs 100
// rather than 2600. That is a real discount reachable by ordering calls, and it
// is load-bearing for consensus: it must be identical in every implementation.
func TestB20ForeignSlotReadWarmsTheForeignAddress(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token, slot := b20Addr(b20VariantAsset, 1), common.Hash{31: 7}
	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
	reg := b20Storage{state: statedb, token: B20PolicyRegistryAddress, ctx: ctx}

	if statedb.AddressInAccessList(B20PolicyRegistryAddress) {
		t.Fatal("the registry is warm before the read; the fixture is not clean")
	}
	before := gas.RegularGas
	_ = reg.getWord(slot)
	charged := before - gas.RegularGas

	// Storage price only: no account-access surcharge for the foreign account.
	if want := params.ColdSloadCostEIP2929; charged != want {
		t.Errorf("a cold foreign slot read charged %d, want %d — the storage cost alone. "+
			"An account-access surcharge here would make a token's own gating cost more "+
			"than the same slot in its own account (BEP-702 3.14)", charged, want)
	}
	if !warmed(statedb, B20PolicyRegistryAddress, slot) {
		t.Error("the slot is not warm after being read, so the next read pays cold again")
	}
	if !statedb.AddressInAccessList(B20PolicyRegistryAddress) {
		t.Error("the registry address is cold after its slot was read. That is defensible, " +
			"but it is not what AddSlotToAccessList does — if this is now deliberate, the " +
			"discount described above disappeared and BEP-702 3.14 has to say so")
	}
}

// TestB20WarmthIsSharedAcrossTokens is the observable of a shared registry: two
// tokens gating on one policy pay the cold surcharge once between them.
func TestB20WarmthIsSharedAcrossTokens(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	slot := common.Hash{31: 9}
	read := func(from common.Address) uint64 {
		gas := NewGasBudget(1_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: from, gas: &gas}
		before := gas.RegularGas
		_ = b20Storage{state: statedb, token: B20PolicyRegistryAddress, ctx: ctx}.getWord(slot)
		return before - gas.RegularGas
	}
	first := read(b20Addr(b20VariantAsset, 1))
	second := read(b20Addr(b20VariantAsset, 2))

	if first != params.ColdSloadCostEIP2929 {
		t.Errorf("the first token's read charged %d, want %d", first, params.ColdSloadCostEIP2929)
	}
	if second != params.WarmStorageReadCostEIP2929 {
		t.Errorf("a second token reading the same registry slot charged %d, want %d — "+
			"warmth is keyed on (registry, slot), not on which token asked",
			second, params.WarmStorageReadCostEIP2929)
	}
}

// TestB20WarmingRevertsWithTheFrame pins that a reverted call leaves nothing warm.
//
// EIP-2929 reverts accessed_addresses and accessed_storage_keys along with the
// rest of the state. A B20 token warms slots through StateDB rather than by
// executing SLOAD, so it inherits that only because the journal records each
// addition — worth an assertion, since a cache that outlived the revert would
// make the next call cheaper than the same call in a fresh transaction.
func TestB20WarmingRevertsWithTheFrame(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x5e"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selUpdateSupplyCap, u256hash(1000)),
			b20Call(selMint, addrKey(b20Alice), u256hash(100)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	slot := b20Storage{token: token}.balanceSlot(b20Alice)
	if !warmed(statedb, token, slot) {
		t.Fatal("the mint should have left alice's balance slot warm")
	}
	// A slot no one has touched, which the reverting call below credits before it
	// fails: batchMint pays bob, then blows the supply cap on carol.
	victim := b20Storage{token: token}.balanceSlot(b20Bob)
	if warmed(statedb, token, victim) {
		t.Fatal("bob's slot is warm before anything touched it")
	}

	// Run the reverting call without evm.Call's automatic snapshot, so the warming
	// can be observed before the revert undoes it. Asserting only the end state
	// would pass just as well if the call had never warmed the slot at all.
	p, ok := resolveB20(token)
	if !ok {
		t.Fatal("the token address does not resolve to a precompile")
	}
	input := encodeBatchMint([]common.Address{b20Bob, b20Carol}, []uint64{10, 100_000})
	snap := statedb.Snapshot()
	_, _, err = runStatefulPrecompiledContract(evm, p.(StatefulPrecompiledContract),
		creator, token, input, NewGasBudget(1_000_000), false, true, uint256.NewInt(0))
	if err == nil {
		t.Fatal("a batchMint past the supply cap should revert")
	}
	if !warmed(statedb, token, victim) {
		t.Fatal("the reverting call never warmed bob's slot, so what follows would hold " +
			"whether or not warming is journalled. Pick an operation that warms a fresh " +
			"slot before it fails")
	}
	statedb.RevertToSnapshot(snap)

	if warmed(statedb, token, victim) {
		t.Error("a slot warmed inside a reverted call stayed warm. The next attempt would " +
			"pay the warm price for an access the chain never accepted, so gas would " +
			"depend on failed history (EIP-2929: the access sets revert with the frame)")
	}
	// The revert undoes only what the frame added: what was warm before it stays warm.
	if !warmed(statedb, token, slot) {
		t.Error("the revert un-warmed alice's slot, which was warmed before the call")
	}
}
