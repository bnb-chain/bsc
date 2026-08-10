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

// newUnseededAmsterdamEVM builds an Amsterdam EVM whose registries have NOT been
// seeded, which is the state a node is in between shipping the code and running
// the fork's seeding.
func newUnseededAmsterdamEVM(t *testing.T) (*state.StateDB, *EVM) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *b20TestChainConfig()
	bc := BlockContext{
		Random:      &common.Hash{},
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
	}
	return statedb, NewEVM(bc, statedb, &cfg, Config{})
}

// TestSeedB20Activation covers what the fork installs, and why each piece has to
// be installed by the fork rather than by a later call.
func TestSeedB20Activation(t *testing.T) {
	statedb, evm := newUnseededAmsterdamEVM(t)
	timelock := common.HexToAddress("0x0000000000000000000000000000000000002006")
	creator := common.HexToAddress("0xdec0de")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Before seeding there is no admin, so nothing can be activated by anyone —
	// the precompiles are present but permanently inert.
	if got := B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Fatalf("unseeded admin = %s, want zero", got.Hex())
	}
	if _, err := call(timelock, B20ActivationRegistryAddress,
		b20Call(selActivate, featureB20Asset)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("activate before seeding err = %v, want revert", err)
	}

	SeedB20Activation(statedb, timelock)

	// The admin is installed, and both registries carry the sentinel that keeps
	// them out of reach of EIP-161 clearing.
	if got := B20ActivationAdmin(statedb); got != timelock {
		t.Errorf("admin = %s, want the timelock %s", got.Hex(), timelock.Hex())
	}
	for _, addr := range []common.Address{B20ActivationRegistryAddress, B20PolicyRegistryAddress} {
		if !bytes.Equal(statedb.GetCode(addr), b20MarkerCode) {
			t.Errorf("%s carries no sentinel after seeding", addr.Hex())
		}
	}

	// Seeding activates nothing: createB20 is still refused until governance
	// opens the feature.
	if _, err := call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x1"), creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("createB20 succeeded straight after seeding — the fork must open nothing")
	}

	// Only the seeded admin can open one, and once open creation works. This is
	// the end-to-end proof that the seeding makes B20 reachable at all.
	if _, err := call(creator, B20ActivationRegistryAddress, b20Call(selActivate, featureB20Asset)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("a non-admin activated a feature")
	}
	if _, err := call(timelock, B20ActivationRegistryAddress, b20Call(selActivate, featureB20Asset)); err != nil {
		t.Fatalf("timelock activate: %v", err)
	}
	ret, err := call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x1"), creator, nil))
	if err != nil {
		t.Fatalf("createB20 after activation: %v", err)
	}
	if token := common.BytesToAddress(ret); !IsB20Address(token) {
		t.Errorf("createB20 returned %s, not a B20 address", token.Hex())
	}
}

// TestSeedB20ActivationIdempotent pins that re-running the fork's seeding cannot
// undo governance. Replaying the fork block must not reinstate the configured
// admin over one that setAdmin has since replaced.
func TestSeedB20ActivationIdempotent(t *testing.T) {
	statedb, evm := newUnseededAmsterdamEVM(t)
	timelock := common.HexToAddress("0x0000000000000000000000000000000000002006")
	rotated := common.HexToAddress("0x0ead11")

	SeedB20Activation(statedb, timelock)
	if _, _, err := evm.Call(timelock, B20ActivationRegistryAddress,
		b20Call(selSetAdmin, addrKey(rotated)), NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("setAdmin: %v", err)
	}
	if got := B20ActivationAdmin(statedb); got != rotated {
		t.Fatalf("admin after rotation = %s, want %s", got.Hex(), rotated.Hex())
	}

	SeedB20Activation(statedb, timelock) // fork block replayed
	if got := B20ActivationAdmin(statedb); got != rotated {
		t.Errorf("re-seeding reverted the admin to %s — governance was overwritten", got.Hex())
	}
}

// TestSeedB20ActivationZeroAdmin covers the deliberate configuration where a
// network ships the code with the switch welded shut. The sentinels still go on,
// so the accounts survive state clearing and a later fork can install an admin.
func TestSeedB20ActivationZeroAdmin(t *testing.T) {
	statedb, _ := newUnseededAmsterdamEVM(t)

	SeedB20Activation(statedb, common.Address{})

	if got := B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Errorf("admin = %s, want zero", got.Hex())
	}
	for _, addr := range []common.Address{B20ActivationRegistryAddress, B20PolicyRegistryAddress} {
		if !bytes.Equal(statedb.GetCode(addr), b20MarkerCode) {
			t.Errorf("%s carries no sentinel", addr.Hex())
		}
	}
}
