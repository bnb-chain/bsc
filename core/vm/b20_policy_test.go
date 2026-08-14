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
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func encodeUpdateList(sel [4]byte, id uint64, flag bool, addrs []common.Address) []byte {
	out := append([]byte{}, sel[:]...)
	out = append(out, u256hash(id).Bytes()...)   // id
	out = append(out, encBool(flag)...)          // add/remove
	out = append(out, u256hash(0x60).Bytes()...) // offset to array (3-word head)
	out = append(out, u256hash(uint64(len(addrs))).Bytes()...)
	for _, a := range addrs {
		out = append(out, addrKey(a).Bytes()...)
	}
	return out
}

// b20BlockContext is the block context every B20 test runs under: post-merge, so
// the fork rules resolve, with transfers stubbed out.
func b20BlockContext(time uint64) BlockContext {
	return BlockContext{
		Random:      &common.Hash{},
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        time,
	}
}

func newB20EVM(t *testing.T) (*state.StateDB, *EVM) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *b20TestChainConfig()
	bc := b20BlockContext(1)
	seedActivation(statedb, b20ActivationAdmin)
	return statedb, NewEVM(bc, statedb, &cfg, Config{})
}

// b20ActivationAdmin is the activation admin the test harness seeds.
var b20ActivationAdmin = common.HexToAddress("0x60feed")

// seedActivation puts the harness in the state a live network reaches once the
// fork has run and governance has opened everything. The fork part delegates to
// SeedB20Activation so the harness cannot drift from it; opening the features
// stays local, since the fork deliberately opens nothing (BEP-702 3.15).
func seedActivation(statedb *state.StateDB, admin common.Address) {
	SeedB20Activation(statedb, admin)
	reg := b20Storage{state: statedb, token: B20ActivationRegistryAddress}
	for _, f := range []common.Hash{featureB20Asset, featureB20Stablecoin, featurePolicyRegistry} {
		reg.setWord(mappingSlot(actSlot(actSlotFeatures), f), common.Hash{31: 1})
	}
}

func TestB20PolicyRegistry(t *testing.T) {
	_, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad4149")
	reg := B20PolicyRegistryAddress

	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, reg, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	authorized := func(id uint64, a common.Address) bool {
		ret, err := call(admin, b20Call(selIsAuthorized, u256hash(id), addrKey(a)))
		if err != nil {
			t.Fatalf("isAuthorized: %v", err)
		}
		return bytes.Equal(ret, encBool(true))
	}

	// create a blocklist policy.
	ret, err := call(admin, b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	block := new(uint256.Int).SetBytes(ret).Uint64()
	if byte(block>>56) != b20PolicyBlocklist {
		t.Fatalf("blocklist id %#x has wrong type byte", block)
	}
	if r, _ := call(admin, b20Call(selPolicyExists, u256hash(block))); !bytes.Equal(r, encBool(true)) {
		t.Fatal("policy should exist")
	}
	if r, _ := call(admin, b20Call(selPolicyAdmin, u256hash(block))); common.BytesToAddress(r) != admin {
		t.Fatal("policyAdmin mismatch")
	}

	// empty blocklist allows everyone; adding bob blocks him.
	if !authorized(block, b20Bob) {
		t.Fatal("empty blocklist should allow")
	}
	if _, err := call(admin, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{b20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if authorized(block, b20Bob) {
		t.Fatal("bob should be blocked")
	}
	if !authorized(block, b20Carol) {
		t.Fatal("carol should still be allowed")
	}

	// create an allowlist policy: empty blocks everyone; adding carol allows her.
	ret, _ = call(admin, b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyAllowlist)))
	allow := new(uint256.Int).SetBytes(ret).Uint64()
	if byte(allow>>56) != b20PolicyAllowlist {
		t.Fatalf("allowlist id %#x wrong type", allow)
	}
	if authorized(allow, b20Carol) {
		t.Fatal("empty allowlist should block")
	}
	if _, err := call(admin, encodeUpdateList(selUpdateAllowlist, allow, true, []common.Address{b20Carol})); err != nil {
		t.Fatalf("updateAllowlist: %v", err)
	}
	if !authorized(allow, b20Carol) {
		t.Fatal("carol should be allowed")
	}

	// type mismatch and authorization guards.
	if _, err := call(admin, encodeUpdateList(selUpdateAllowlist, block, true, []common.Address{b20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("updateAllowlist on blocklist should revert")
	}
	if _, err := call(b20Bob, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{b20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("non-admin update should revert")
	}

	// two-step admin transfer.
	newAdmin := common.HexToAddress("0x9ead")
	if _, err := call(admin, b20Call(selStageUpdateAdmin, u256hash(block), addrKey(newAdmin))); err != nil {
		t.Fatalf("stageUpdateAdmin: %v", err)
	}
	if _, err := call(b20Alice, b20Call(selFinalizeUpdateAdmin, u256hash(block))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("finalize by non-nominee should revert")
	}
	if _, err := call(newAdmin, b20Call(selFinalizeUpdateAdmin, u256hash(block))); err != nil {
		t.Fatalf("finalizeUpdateAdmin: %v", err)
	}
	if _, err := call(admin, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{b20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("old admin should no longer update")
	}

	// renounce freezes the policy; reads still work.
	if _, err := call(newAdmin, b20Call(selRenounceAdmin, u256hash(block))); err != nil {
		t.Fatalf("renounceAdmin: %v", err)
	}
	if _, err := call(newAdmin, encodeUpdateList(selUpdateBlocklist, block, false, []common.Address{b20Bob})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("frozen policy should reject updates")
	}
	if authorized(block, b20Bob) {
		t.Fatal("frozen policy still evaluates: bob stays blocked")
	}
}

// TestB20PolicyIntegration binds policies to a token's compliance scopes and
// checks they gate transfers and mints.
func TestB20PolicyIntegration(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	custody := common.HexToAddress("0xc45d1")
	salt := common.HexToHash("0x0c")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// token with creator as admin + minter, 1000 to alice.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// blocklist bob, bind to TRANSFER_RECEIVER.
	ret, _ = call(creator, B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist)))
	blk := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, B20PolicyRegistryAddress, encodeUpdateList(selUpdateBlocklist, blk, true, []common.Address{b20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token, b20Call(selUpdatePolicy, scopeTransferReceiver, u256hash(blk))); err != nil {
		t.Fatalf("updatePolicy(receiver): %v", err)
	}

	// transfer to bob (blocked receiver) reverts; to carol succeeds.
	if _, err := call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(10))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("transfer to blocked receiver err = %v, want revert", err)
	}
	if _, err := call(b20Alice, token, b20Call(selTransfer, addrKey(b20Carol), u256hash(10))); err != nil {
		t.Fatalf("transfer to allowed receiver: %v", err)
	}

	// allowlist for MINT_RECEIVER: only custody may receive newly minted supply.
	ret, _ = call(creator, B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyAllowlist)))
	al := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, B20PolicyRegistryAddress, encodeUpdateList(selUpdateAllowlist, al, true, []common.Address{custody})); err != nil {
		t.Fatalf("updateAllowlist: %v", err)
	}
	if _, err := call(creator, token, b20Call(selUpdatePolicy, scopeMintReceiver, u256hash(al))); err != nil {
		t.Fatalf("updatePolicy(mint receiver): %v", err)
	}
	if _, err := call(creator, token, b20Call(selMint, addrKey(b20Alice), u256hash(1))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("mint to non-listed err = %v, want revert", err)
	}
	if _, err := call(creator, token, b20Call(selMint, addrKey(custody), u256hash(500))); err != nil {
		t.Fatalf("mint to custody: %v", err)
	}
	view := newB20Storage(statedb, token)
	if view.balanceOf(custody).Uint64() != 500 {
		t.Fatalf("custody balance = %d, want 500", view.balanceOf(custody).Uint64())
	}
	// binding a never-created policy id is rejected.
	if _, err := call(creator, token, b20Call(selUpdatePolicy, scopeTransferSender, u256hash(0x99999))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("binding nonexistent policy should revert")
	}
}

func TestB20SeizeWithMemo(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x0d")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// token: creator is admin, MINT and SEIZE holder; 1000 minted to bob.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selGrantRole, roleSeize, addrKey(creator)),
		b20Call(selMint, addrKey(b20Bob), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newB20Storage(statedb, token)

	memo := common.HexToHash("0x5e12e")

	// seizing an un-frozen account fails (must freeze first).
	if _, err := call(creator, token, b20Call(selSeizeWithMemo, addrKey(b20Bob), addrKey(b20Alice), u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("seize before freeze err = %v, want revert (AccountNotSeizable)", err)
	}

	// step 1: blacklist bob on the SEIZE_HOLDER scope.
	ret, _ = call(creator, B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist)))
	blk := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, B20PolicyRegistryAddress, encodeUpdateList(selUpdateBlocklist, blk, true, []common.Address{b20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token, b20Call(selUpdatePolicy, scopeSeizeHolder, u256hash(blk))); err != nil {
		t.Fatalf("updatePolicy(seizeHolder): %v", err)
	}

	// non-role caller cannot seize.
	if _, err := call(b20Alice, token, b20Call(selSeizeWithMemo, addrKey(b20Bob), addrKey(b20Alice), u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("unauthorized seize err = %v, want revert", err)
	}

	// the zero address is never a valid destination.
	if _, err := call(creator, token, b20Call(selSeizeWithMemo, addrKey(b20Bob), common.Hash{}, u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("seize to zero err = %v, want revert (InvalidReceiver)", err)
	}

	// step 2: seize part of the frozen balance. Value moves; supply does not.
	if _, err := call(creator, token, b20Call(selSeizeWithMemo, addrKey(b20Bob), addrKey(b20Alice), u256hash(400), memo)); err != nil {
		t.Fatalf("seizeWithMemo: %v", err)
	}
	if got := view.balanceOf(b20Bob).Uint64(); got != 600 {
		t.Fatalf("seized account balance = %d, want 600", got)
	}
	if got := view.balanceOf(b20Alice).Uint64(); got != 400 {
		t.Fatalf("destination balance = %d, want 400", got)
	}
	if got := view.totalSupply().Uint64(); got != 1000 {
		t.Fatalf("totalSupply = %d, want 1000 (seizure moves value, it does not burn)", got)
	}
}

// TestB20PolicyStorageLayout pins the registry's storage against base-std's
// PolicyRegistryStorage: the namespaced root, the slot order, and the packed
// existence-and-admin word. These are consensus-visible, so the assertions are
// on raw slots rather than on what the ABI reports.
func TestB20PolicyStorageLayout(t *testing.T) {
	statedb, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad4149")

	// Slot order, mirroring base-std: policies, members, pendingAdmins, counter,
	// then a reserved slot for composite children.
	root := new(uint256.Int).SetBytes(erc7201Root("bsc.policy_registry").Bytes())
	for offset, want := range map[uint64]uint64{
		polSlotPolicies: 0, polSlotMembers: 1, polSlotPendingAdmins: 2, polSlotCounter: 3,
	} {
		if offset != want {
			t.Errorf("slot constant = %d, want %d", offset, want)
		}
		got := new(uint256.Int).SetBytes(polSlot(offset).Bytes())
		if exp := new(uint256.Int).AddUint64(root, offset); !got.Eq(exp) {
			t.Errorf("slot %d = %x, want root+%d = %x", offset, got, offset, exp)
		}
	}

	ret, _, err := evm.Call(admin, B20PolicyRegistryAddress,
		b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyAllowlist)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	id := new(uint256.Int).SetBytes(ret).Uint64()

	view := newB20Storage(statedb, B20PolicyRegistryAddress)
	word := view.getWord(mappingSlot(polSlot(polSlotPolicies), idKey(id)))
	if word[0]&0x80 == 0 {
		t.Errorf("policy word %x has no exists bit", word)
	}
	if got := common.BytesToAddress(word[12:]); got != admin {
		t.Errorf("packed admin = %s, want %s", got.Hex(), admin.Hex())
	}
	for _, b := range word[1:12] { // bits 254:160 are reserved and must stay zero
		if b != 0 {
			t.Errorf("policy word %x has dirty reserved bits", word)
			break
		}
	}
	// Two sentinels are seeded first, so the first caller id draws counter 2 and
	// the counter lands on 3 — the same value base-std's own layout test asserts.
	if got := new(uint256.Int).SetBytes(view.getWord(polSlot(polSlotCounter)).Bytes()).Uint64(); got != 3 {
		t.Errorf("counter = %d, want 3", got)
	}
	if id != uint64(b20PolicyAllowlist)<<56|2 {
		t.Errorf("first allowlist id = %#x, want type 1 counter 2", id)
	}
}

// TestB20PolicySentinels pins the sentinel semantics: they exist and answer
// authorization from their id alone, so they are correct before any policy has
// been created and no membership write can redefine them.
func TestB20PolicySentinels(t *testing.T) {
	_, evm := newB20EVM(t)
	caller := common.HexToAddress("0xad4149")
	ask := func(sel [4]byte, args ...common.Hash) []byte {
		ret, _, err := evm.Call(caller, B20PolicyRegistryAddress, b20Call(sel, args...),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return ret
	}

	// Before anything is created: both sentinels report as existing, and their
	// authorization is fixed. ALWAYS_ALLOW is the value every unset policy field
	// holds, so it has to be right at this point in particular.
	for _, id := range []uint64{b20PolicyAlwaysAllow, b20PolicyAlwaysBlock} {
		if !bytes.Equal(ask(selPolicyExists, u256hash(id)), encBool(true)) {
			t.Errorf("policyExists(%#x) = false, want true", id)
		}
		if got := common.BytesToAddress(ask(selPolicyAdmin, u256hash(id))); got != (common.Address{}) {
			t.Errorf("policyAdmin(%#x) = %s, want zero", id, got.Hex())
		}
		if got := common.BytesToAddress(ask(selPendingPolicyAdmin, u256hash(id))); got != (common.Address{}) {
			t.Errorf("pendingPolicyAdmin(%#x) = %s, want zero", id, got.Hex())
		}
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(b20PolicyAlwaysAllow), addrKey(b20Bob)), encBool(true)) {
		t.Error("ALWAYS_ALLOW must authorize")
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(b20PolicyAlwaysBlock), addrKey(b20Bob)), encBool(false)) {
		t.Error("ALWAYS_BLOCK must refuse")
	}

	// A malformed type byte is not a policy: it never exists, never authorizes,
	// and has no admin.
	bad := uint64(5) << 56
	if !bytes.Equal(ask(selPolicyExists, u256hash(bad)), encBool(false)) {
		t.Error("a malformed type byte must not exist")
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(bad), addrKey(b20Bob)), encBool(false)) {
		t.Error("a malformed type byte must not authorize")
	}
	if got := common.BytesToAddress(ask(selPolicyAdmin, u256hash(bad))); got != (common.Address{}) {
		t.Errorf("policyAdmin(malformed) = %s, want zero", got.Hex())
	}

	// Neither sentinel can be administered: both are seeded with a zero admin.
	for _, id := range []uint64{b20PolicyAlwaysAllow, b20PolicyAlwaysBlock} {
		_, _, err := evm.Call(caller, B20PolicyRegistryAddress,
			b20Call(selStageUpdateAdmin, u256hash(id), addrKey(caller)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("stageUpdateAdmin(%#x) err = %v, want revert", id, err)
		}
	}
}

// TestB20PolicyCheckOrder pins the order a membership update applies its checks.
// The order is observable through which error a caller receives, so base-std's
// canonical existence -> type -> admin -> batch sequence is part of the surface.
func TestB20PolicyCheckOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad4149")
	stranger := common.HexToAddress("0x57ra9e")
	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, B20PolicyRegistryAddress, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	revertsWith := func(what string, caller common.Address, input []byte, sel [4]byte) {
		t.Helper()
		ret, err := call(caller, input)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want revert", what, err)
			return
		}
		if len(ret) < 4 || !bytes.Equal(ret[:4], sel[:]) {
			t.Errorf("%s: revert data %x, want selector %x", what, ret, sel)
		}
	}

	// An id no createPolicy produced: existence is checked first, so this is
	// PolicyNotFound and not Unauthorized.
	ghost := uint64(b20PolicyAllowlist)<<56 | 999
	revertsWith("nonexistent policy", stranger,
		encodeUpdateList(selUpdateAllowlist, ghost, true, []common.Address{b20Bob}), errSelPolicyNotFound)

	ret, err := call(admin, b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	block := new(uint256.Int).SetBytes(ret).Uint64()

	// Type is checked before admin, so a stranger calling the wrong method on a
	// real policy sees IncompatiblePolicyType rather than Unauthorized.
	revertsWith("wrong type, wrong caller", stranger,
		encodeUpdateList(selUpdateAllowlist, block, true, []common.Address{b20Bob}), errSelIncompatibleType)
	// Right method, wrong caller: now admin is what fails.
	revertsWith("right type, wrong caller", stranger,
		encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{b20Bob}), errSelUnauthorized)
	// Admin passes, so an oversized batch is what fails, last.
	oversized := make([]common.Address, b20PolicyBatchMax+1)
	for i := range oversized {
		oversized[i] = common.BigToAddress(new(big.Int).SetUint64(uint64(i + 1)))
	}
	revertsWith("oversized batch", admin,
		encodeUpdateList(selUpdateBlocklist, block, true, oversized), errSelBatchTooLarge)

	// The same ordering applies to the admin-handover paths.
	revertsWith("stage on nonexistent", stranger,
		b20Call(selStageUpdateAdmin, u256hash(ghost), addrKey(stranger)), errSelPolicyNotFound)
	revertsWith("finalize on nonexistent", stranger,
		b20Call(selFinalizeUpdateAdmin, u256hash(ghost)), errSelPolicyNotFound)
	revertsWith("renounce on nonexistent", stranger,
		b20Call(selRenounceAdmin, u256hash(ghost)), errSelPolicyNotFound)
}

// TestB20PolicySentinelsIgnoreMembership pins why the sentinel fast-paths exist.
// Their emptiness alone would give the same answers, so a membership-derived
// implementation looks correct — until something writes membership under a
// sentinel id, which a counter that carried into the type byte could do.
// Answering from the id makes them constant by construction.
func TestB20PolicySentinelsIgnoreMembership(t *testing.T) {
	statedb, evm := newB20EVM(t)

	// Plant membership under both sentinels, bypassing the ABI entirely.
	view := policyReg{s: newB20Storage(statedb, B20PolicyRegistryAddress)}
	view.setMember(b20PolicyAlwaysAllow, b20Bob, true) // "block bob" on ALWAYS_ALLOW
	view.setMember(b20PolicyAlwaysBlock, b20Bob, true) // "allow bob" on ALWAYS_BLOCK

	ask := func(id uint64) []byte {
		ret, _, err := evm.Call(b20Alice, B20PolicyRegistryAddress,
			b20Call(selIsAuthorized, u256hash(id), addrKey(b20Bob)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("isAuthorized: %v", err)
		}
		return ret
	}
	if !bytes.Equal(ask(b20PolicyAlwaysAllow), encBool(true)) {
		t.Error("ALWAYS_ALLOW stopped authorizing after a membership write — it is not constant")
	}
	if !bytes.Equal(ask(b20PolicyAlwaysBlock), encBool(false)) {
		t.Error("ALWAYS_BLOCK started authorizing after a membership write — it is not constant")
	}
}

// TestB20PolicyCounterExhaustion pins the 56-bit counter bound. Reaching it
// takes more createPolicy calls than any chain will see, so the counter is
// driven there directly; what matters is that the boundary is refused rather
// than allowed to carry into the type byte, where an id would change type,
// collide with another type's policy, or land on a sentinel.
func TestB20PolicyCounterExhaustion(t *testing.T) {
	statedb, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad4149")
	view := policyReg{s: newB20Storage(statedb, B20PolicyRegistryAddress)}

	create := func() ([]byte, error) {
		ret, _, err := evm.Call(admin, B20PolicyRegistryAddress,
			b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyBlocklist)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// One short of the bound still works, and stays inside its own type space.
	view.setCounter(b20PolicyCounterMax - 1)
	ret, err := create()
	if err != nil {
		t.Fatalf("createPolicy at counter max-1: %v", err)
	}
	if id := new(uint256.Int).SetBytes(ret).Uint64(); polIDType(id) != b20PolicyBlocklist {
		t.Errorf("id %#x escaped its type byte", id)
	}

	// At the bound, creation is refused: Panic(0x11), the arithmetic-overflow code.
	view.setCounter(b20PolicyCounterMax)
	ret, err = create()
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createPolicy at counter max err = %v, want revert", err)
	}
	want := append(append([]byte{}, errSelPanic[:]...), wU8(0x11).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Errorf("revert data = %x, want Panic(0x11) = %x", ret, want)
	}
}

// TestB20PolicyEvents pins the registry's log surface: which events each write
// emits, in what order, and with what payload. The membership events mix a
// static bool with a dynamic address[], so their data is also checked against
// go-ethereum's own ABI packer — a head/tail mistake in that shape would
// otherwise be invisible to an expectation built with the same encoder.
func TestB20PolicyEvents(t *testing.T) {
	statedb, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad4149")
	heir := common.HexToAddress("0x8e14")

	txSeq := 0
	logsOf := func(caller common.Address, input []byte) []*types.Log {
		t.Helper()
		txSeq++
		hash := common.BigToHash(new(big.Int).SetUint64(uint64(txSeq)))
		statedb.SetTxContext(hash, txSeq)
		if _, _, err := evm.Call(caller, B20PolicyRegistryAddress, input,
			NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
			t.Fatalf("call: %v", err)
		}
		return statedb.GetLogs(hash, 1, common.Hash{}, 1)
	}
	wantTopics := func(what string, got *types.Log, expect ...common.Hash) {
		t.Helper()
		if len(got.Topics) != len(expect) {
			t.Errorf("%s: %d topics, want %d", what, len(got.Topics), len(expect))
			return
		}
		for i := range expect {
			if got.Topics[i] != expect[i] {
				t.Errorf("%s: topic %d = %s, want %s", what, i, got.Topics[i].Hex(), expect[i].Hex())
			}
		}
	}

	// createPolicy: PolicyCreated then the initial admin as a transition from
	// nobody, so creation lands in the same stream as every later handover.
	logs := logsOf(admin, b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyBlocklist)))
	if len(logs) != 2 {
		t.Fatalf("createPolicy emitted %d logs, want 2", len(logs))
	}
	block := uint64(b20PolicyBlocklist)<<56 | 2
	wantTopics("PolicyCreated", logs[0], b20TopicPolicyCreated, idKey(block), addrKey(admin))
	if !bytes.Equal(logs[0].Data, wU8(b20PolicyBlocklist).Bytes()) {
		t.Errorf("PolicyCreated data = %x, want the type byte", logs[0].Data)
	}
	wantTopics("PolicyAdminUpdated", logs[1],
		b20TopicPolicyAdminUpdated, idKey(block), addrKey(common.Address{}), addrKey(admin))
	if len(logs[1].Data) != 0 {
		t.Errorf("PolicyAdminUpdated data = %x, want empty", logs[1].Data)
	}

	// creator is the caller, not the nominated admin: a policy created on someone
	// else's behalf must name whoever sent the transaction. base-std's
	// create_policy_inner reads storage.caller() for this field.
	logs = logsOf(b20Alice, b20Call(selCreatePolicy, addrKey(heir), u256hash(b20PolicyBlocklist)))
	if len(logs) != 2 {
		t.Fatalf("createPolicy (third party) emitted %d logs, want 2", len(logs))
	}
	third := uint64(b20PolicyBlocklist)<<56 | 3
	wantTopics("PolicyCreated (third party)", logs[0],
		b20TopicPolicyCreated, idKey(third), addrKey(b20Alice))
	wantTopics("PolicyAdminUpdated (third party)", logs[1],
		b20TopicPolicyAdminUpdated, idKey(third), addrKey(common.Address{}), addrKey(heir))

	// updateBlocklist: one log under the blocklist event, carrying the flag and
	// the accounts.
	accounts := []common.Address{b20Bob, b20Carol}
	logs = logsOf(admin, encodeUpdateList(selUpdateBlocklist, block, true, accounts))
	if len(logs) != 1 {
		t.Fatalf("updateBlocklist emitted %d logs, want 1", len(logs))
	}
	wantTopics("BlocklistUpdated", logs[0], b20TopicBlocklistUpdated, idKey(block), addrKey(admin))

	boolType, err := abi.NewType("bool", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	addrsType, err := abi.NewType("address[]", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(true, accounts)
	if err != nil {
		t.Fatalf("pack membership payload: %v", err)
	}
	if !bytes.Equal(logs[0].Data, oracle) {
		t.Errorf("BlocklistUpdated data\n got = %x\nwant = %x", logs[0].Data, oracle)
	}

	// An allowlist reports under its own event, not the blocklist one.
	ret, _, err := evm.Call(admin, B20PolicyRegistryAddress,
		b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyAllowlist)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy allowlist: %v", err)
	}
	allow := new(uint256.Int).SetBytes(ret).Uint64()
	logs = logsOf(admin, encodeUpdateList(selUpdateAllowlist, allow, false, accounts))
	if len(logs) != 1 || logs[0].Topics[0] != b20TopicAllowlistUpdated {
		t.Fatalf("updateAllowlist logs = %v", logs)
	}
	oracle, _ = abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(false, accounts)
	if !bytes.Equal(logs[0].Data, oracle) {
		t.Errorf("AllowlistUpdated data\n got = %x\nwant = %x", logs[0].Data, oracle)
	}

	// createPolicyWithAccounts emits the seed membership event too — including
	// for an empty batch, since the call form is part of the record.
	logs = logsOf(admin, encodeCreatePolicyWithAccounts(admin, b20PolicyAllowlist, nil))
	if len(logs) != 3 {
		t.Fatalf("createPolicyWithAccounts emitted %d logs, want 3", len(logs))
	}
	if logs[2].Topics[0] != b20TopicAllowlistUpdated {
		t.Errorf("third log = %s, want AllowlistUpdated", logs[2].Topics[0].Hex())
	}
	oracle, _ = abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(true, []common.Address{})
	if !bytes.Equal(logs[2].Data, oracle) {
		t.Errorf("seed AllowlistUpdated data\n got = %x\nwant = %x", logs[2].Data, oracle)
	}

	// The handover pair, then renunciation. Staging names the incumbent as well
	// as the nominee, so a stage log is self-contained.
	logs = logsOf(admin, b20Call(selStageUpdateAdmin, u256hash(block), addrKey(heir)))
	if len(logs) != 1 {
		t.Fatalf("stageUpdateAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminStaged", logs[0],
		b20TopicPolicyAdminStaged, idKey(block), addrKey(admin), addrKey(heir))

	// Withdrawing a nomination is a governance action, not a silent one.
	logs = logsOf(admin, b20Call(selStageUpdateAdmin, u256hash(block), addrKey(common.Address{})))
	wantTopics("PolicyAdminStaged (cancel)", logs[0],
		b20TopicPolicyAdminStaged, idKey(block), addrKey(admin), addrKey(common.Address{}))

	// Re-nominate, then the nominee takes over.
	logsOf(admin, b20Call(selStageUpdateAdmin, u256hash(block), addrKey(heir)))
	logs = logsOf(heir, b20Call(selFinalizeUpdateAdmin, u256hash(block)))
	if len(logs) != 1 {
		t.Fatalf("finalizeUpdateAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminUpdated (finalize)", logs[0],
		b20TopicPolicyAdminUpdated, idKey(block), addrKey(admin), addrKey(heir))

	logs = logsOf(heir, b20Call(selRenounceAdmin, u256hash(block)))
	if len(logs) != 1 {
		t.Fatalf("renounceAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminUpdated (renounce)", logs[0],
		b20TopicPolicyAdminUpdated, idKey(block), addrKey(heir), addrKey(common.Address{}))
}

func encodeCreatePolicyWithAccounts(admin common.Address, ptype byte, accounts []common.Address) []byte {
	out := append([]byte{}, selCreatePolicyWithAccounts[:]...)
	out = append(out, addrKey(admin).Bytes()...)
	out = append(out, u256hash(uint64(ptype)).Bytes()...)
	out = append(out, u256hash(0x60).Bytes()...) // offset past the 3-word head
	out = append(out, u256hash(uint64(len(accounts))).Bytes()...)
	for _, a := range accounts {
		out = append(out, addrKey(a).Bytes()...)
	}
	return out
}
