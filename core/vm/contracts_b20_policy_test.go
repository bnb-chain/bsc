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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// encodeUpdateList ABI-encodes updateAllowlist/updateBlocklist(uint256,bool,address[]).
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

func newPasteurEVM(t *testing.T) (*state.StateDB, *EVM) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *params.TestChainConfig
	zero := uint64(0)
	cfg.PasteurTime = &zero
	bc := BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
	}
	return statedb, NewEVM(bc, statedb, &cfg, Config{})
}

func TestB20PolicyRegistry(t *testing.T) {
	_, evm := newPasteurEVM(t)
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
	statedb, evm := newPasteurEVM(t)
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
	if _, err := call(creator, token, b20Call(selUpdatePolicy, u256hash(1), u256hash(blk))); err != nil {
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
	if _, err := call(creator, token, b20Call(selUpdatePolicy, u256hash(3), u256hash(al))); err != nil {
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
	if _, err := call(creator, token, b20Call(selUpdatePolicy, u256hash(0), u256hash(0x99999))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("binding nonexistent policy should revert")
	}
}
