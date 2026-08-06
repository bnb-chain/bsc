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

package b20

import (
	"github.com/ethereum/go-ethereum/core/vm"

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

func rightPad32(b []byte) []byte {
	out := make([]byte, (len(b)+31)/32*32)
	copy(out, b)
	return out
}

// encodeCreateB20 ABI-encodes createB20(uint8,bytes32,address,bytes[]).
func encodeCreateB20(variant byte, salt common.Hash, admin common.Address, calls [][]byte) []byte {
	out := append([]byte{}, selCreateB20[:]...)
	out = append(out, u256hash(uint64(variant)).Bytes()...) // w0 variant
	out = append(out, salt.Bytes()...)                      // w1 salt
	out = append(out, addrKey(admin).Bytes()...)            // w2 initialAdmin
	out = append(out, u256hash(0x80).Bytes()...)            // w3 offset to bytes[]

	// array region: length, offsets table, elements.
	elems := make([][]byte, len(calls))
	for i, c := range calls {
		elems[i] = append(u256hash(uint64(len(c))).Bytes(), rightPad32(c)...)
	}
	out = append(out, u256hash(uint64(len(calls))).Bytes()...)
	cur := uint64(len(calls) * 32) // element offsets are relative to just after the length word
	for _, e := range elems {
		out = append(out, u256hash(cur).Bytes()...)
		cur += uint64(len(e))
	}
	for _, e := range elems {
		out = append(out, e...)
	}
	return out
}

func TestB20Factory(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *params.TestChainConfig
	zero := uint64(0)
	cfg.AmsterdamTime = &zero
	bc := vm.BlockContext{
		Random:      &common.Hash{}, // post-merge rules, so IsAmsterdam resolves
		CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
	}
	evm := vm.NewEVM(bc, statedb, &cfg, vm.Config{})

	creator := common.HexToAddress("0xc4ea70")
	minter := common.HexToAddress("0x33333")
	salt := common.HexToHash("0x01")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, vm.NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// predict address.
	predicted, err := call(creator, B20FactoryAddress, b20Call(selGetB20Address, u256hash(b20VariantAsset), addrKey(creator), salt))
	if err != nil {
		t.Fatalf("getB20Address: %v", err)
	}
	want := b20DeriveAddress(b20VariantAsset, creator, salt)
	if common.BytesToAddress(predicted) != want {
		t.Fatalf("getB20Address = %s, want %s", common.BytesToAddress(predicted).Hex(), want.Hex())
	}

	// create the token with bootstrap initCalls: grant MINT to minter, mint 1000 to alice.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(minter)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	if token != want {
		t.Fatalf("createB20 returned %s, want %s", token.Hex(), want.Hex())
	}

	// isB20Initialized(token) == true.
	if r, _ := call(creator, B20FactoryAddress, b20Call(selIsB20Initialized, addrKey(token))); !bytes.Equal(r, encBool(true)) {
		t.Fatal("token should be initialized")
	}

	// the created token is live: bootstrap state applied.
	view := newB20Storage(statedb, token)
	if view.totalSupply().Uint64() != 1000 || view.balanceOf(b20Alice).Uint64() != 1000 {
		t.Fatalf("supply %d aliceBal %d, want 1000/1000", view.totalSupply().Uint64(), view.balanceOf(b20Alice).Uint64())
	}
	if !view.hasRole(roleDefaultAdmin, creator) || view.adminCount().Uint64() != 1 {
		t.Fatal("creator should be sole DEFAULT_ADMIN")
	}
	if !view.hasRole(roleMint, minter) {
		t.Fatal("minter should hold MINT_ROLE")
	}

	// and it behaves like a token through the vm.EVM: alice transfers to bob.
	if r, err := call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(400))); err != nil || !bytes.Equal(r, encBool(true)) {
		t.Fatalf("transfer via created token: ret %x err %v", r, err)
	}
	if view.balanceOf(b20Bob).Uint64() != 400 {
		t.Fatalf("bob balance %d, want 400", view.balanceOf(b20Bob).Uint64())
	}

	// re-creating at the same salt collides.
	if _, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, nil)); !errors.Is(err, vm.ErrExecutionReverted) {
		t.Fatalf("duplicate createB20 err = %v, want revert", err)
	}
}

// TestB20FactoryOwnerless creates a token with initialAdmin == 0: roles are set
// up during the privileged bootstrap and the token is then ungovernable.
func TestB20FactoryOwnerless(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	cfg := *params.TestChainConfig
	zero := uint64(0)
	cfg.AmsterdamTime = &zero
	bc := vm.BlockContext{
		Random:      &common.Hash{}, // post-merge rules, so IsAmsterdam resolves
		CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
	}
	evm := vm.NewEVM(bc, statedb, &cfg, vm.Config{})
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x02")

	// initCalls grant MINT to creator despite no admin (privileged bootstrap).
	initCalls := [][]byte{b20Call(selGrantRole, roleMint, addrKey(creator))}
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantStablecoin, salt, common.Address{}, initCalls),
		vm.NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20 ownerless: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newB20Storage(statedb, token)
	if !view.adminCount().IsZero() {
		t.Fatalf("adminCount = %d, want 0 (ownerless)", view.adminCount().Uint64())
	}
	if !view.hasRole(roleMint, creator) {
		t.Fatal("bootstrap should have granted MINT despite ownerless")
	}
	// post-creation, role mutations are impossible (no admin).
	if _, _, err := evm.Call(creator, token, b20Call(selGrantRole, roleBurn, addrKey(creator)),
		vm.NewGasBudget(1_000_000), uint256.NewInt(0)); !errors.Is(err, vm.ErrExecutionReverted) {
		t.Fatalf("grant on ownerless token err = %v, want revert", err)
	}
}
