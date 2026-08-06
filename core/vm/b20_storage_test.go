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
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// TestERC7201Root anchors the whole slot-math layer: every derived slot hangs
// off this root, so a change to it silently relocates all B20 storage.
//
// The constant is a regression anchor, not an external reference. While the
// namespace was "base.b20" it could be checked against the value base-std
// publishes; "bsc.b20" has no published counterpart, so the derivation is also
// recomputed here from ERC-7201's definition over an independent big.Int path.
func TestERC7201Root(t *testing.T) {
	const want = "0xd7d17b10507583ccbb27e6049e378ddb3a23890fde1bf3d25a473c9817975c00"
	if got := b20CoreRoot.Hex(); got != want {
		t.Fatalf("erc7201Root(%q) = %s, want %s", b20Namespace, got, want)
	}

	// ERC-7201: keccak256(keccak256(namespace) - 1), low byte cleared.
	inner := new(big.Int).SetBytes(crypto.Keccak256([]byte(b20Namespace)))
	inner.Sub(inner, big.NewInt(1))
	var buf [32]byte
	inner.FillBytes(buf[:])
	exp := crypto.Keccak256Hash(buf[:])
	exp[31] = 0
	if b20CoreRoot != exp {
		t.Fatalf("root does not follow ERC-7201: got %s, want %s", b20CoreRoot.Hex(), exp.Hex())
	}
}

func newTestStorage(t *testing.T) b20Storage {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	return newB20Storage(statedb, b20Addr(b20VariantAsset, 1))
}

func TestB20StorageScalars(t *testing.T) {
	s := newTestStorage(t)
	v := uint256.NewInt(123456789)
	s.setTotalSupply(v)
	s.setSupplyCap(uint256.NewInt(1_000_000))
	s.setAdminCount(uint256.NewInt(2))
	s.setPaused(uint256.NewInt(0b101))

	if got := s.totalSupply(); got.Cmp(v) != 0 {
		t.Errorf("totalSupply = %v, want %v", got, v)
	}
	if got := s.supplyCap().Uint64(); got != 1_000_000 {
		t.Errorf("supplyCap = %d", got)
	}
	if got := s.adminCount().Uint64(); got != 2 {
		t.Errorf("adminCount = %d", got)
	}
	if got := s.paused().Uint64(); got != 0b101 {
		t.Errorf("paused = %b", got)
	}
}

func TestB20StorageMappings(t *testing.T) {
	s := newTestStorage(t)
	alice := common.HexToAddress("0xa11ce")
	bob := common.HexToAddress("0xb0b")

	s.setBalance(alice, uint256.NewInt(500))
	s.setAllowance(alice, bob, uint256.NewInt(42))
	s.setNonce(alice, uint256.NewInt(7))

	if got := s.balanceOf(alice).Uint64(); got != 500 {
		t.Errorf("balanceOf = %d", got)
	}
	if got := s.balanceOf(bob).Uint64(); got != 0 {
		t.Errorf("unset balance = %d, want 0", got)
	}
	if got := s.allowance(alice, bob).Uint64(); got != 42 {
		t.Errorf("allowance = %d", got)
	}
	if got := s.allowance(bob, alice).Uint64(); got != 0 {
		t.Errorf("reversed allowance = %d, want 0", got)
	}
	if got := s.nonce(alice).Uint64(); got != 7 {
		t.Errorf("nonce = %d", got)
	}
}

func TestB20StorageRoles(t *testing.T) {
	s := newTestStorage(t)
	mintRole := crypto.Keccak256Hash([]byte("MINT_ROLE"))
	alice := common.HexToAddress("0xa11ce")

	if s.hasRole(mintRole, alice) {
		t.Fatal("role should be unset initially")
	}
	s.setRole(mintRole, alice, true)
	if !s.hasRole(mintRole, alice) {
		t.Error("role should be set")
	}
	s.setRole(mintRole, alice, false)
	if s.hasRole(mintRole, alice) {
		t.Error("role should be cleared")
	}

	s.setRoleAdmin(mintRole, common.Hash{}) // DEFAULT_ADMIN
	if got := s.roleAdmin(mintRole); got != (common.Hash{}) {
		t.Errorf("roleAdmin = %s", got.Hex())
	}
}

// TestB20StoragePackedPolicies verifies the four policy ids share their packed
// slots without clobbering each other.
func TestB20StoragePackedPolicies(t *testing.T) {
	s := newTestStorage(t)
	s.setTransferSenderPolicy(0x1111111111111111)
	s.setTransferReceiverPolicy(0x2222222222222222)
	s.setTransferExecutorPolicy(0x3333333333333333)
	s.setMintReceiverPolicy(0x4444444444444444)

	if got := s.transferSenderPolicy(); got != 0x1111111111111111 {
		t.Errorf("sender = %#x", got)
	}
	if got := s.transferReceiverPolicy(); got != 0x2222222222222222 {
		t.Errorf("receiver = %#x", got)
	}
	if got := s.transferExecutorPolicy(); got != 0x3333333333333333 {
		t.Errorf("executor = %#x", got)
	}
	if got := s.mintReceiverPolicy(); got != 0x4444444444444444 {
		t.Errorf("mintReceiver = %#x", got)
	}

	// The three transfer lanes must live in a single slot (slot 9), packed at
	// byte offsets 0/8/16.
	word := s.getWord(slotAt(b20SlotTransferPolicies))
	// byte offsets 24(reserved,0) | 16(executor) | 8(receiver) | 0(sender)
	wantWord := "0x" + "0000000000000000" + "3333333333333333" + "2222222222222222" + "1111111111111111"
	if word.Hex() != wantWord {
		t.Errorf("packed slot 9 = %s, want %s", word.Hex(), wantWord)
	}

	// Overwriting one lane must not disturb the others.
	s.setTransferReceiverPolicy(0xdeadbeefdeadbeef)
	if got := s.transferSenderPolicy(); got != 0x1111111111111111 {
		t.Errorf("sender disturbed: %#x", got)
	}
	if got := s.transferExecutorPolicy(); got != 0x3333333333333333 {
		t.Errorf("executor disturbed: %#x", got)
	}
	if got := s.transferReceiverPolicy(); got != 0xdeadbeefdeadbeef {
		t.Errorf("receiver = %#x", got)
	}
}

// TestB20StorageGas checks the v0 storage gas schedule: cold/warm reads and
// SSTORE set/reset/no-op writes, plus out-of-gas propagation.
func TestB20StorageGas(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)
	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
	s := newMeteredB20Storage(ctx)

	alice := common.HexToAddress("0xa11ce")
	charged := func(fn func()) uint64 {
		before := gas.RegularGas
		fn()
		return before - gas.RegularGas
	}

	// cold write, zero -> non-zero: cold surcharge + set.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(500)) }); c != b20GasColdSlot+b20GasSstoreSet {
		t.Errorf("cold set write charged %d, want %d", c, b20GasColdSlot+b20GasSstoreSet)
	}
	// warm read of the same slot.
	if c := charged(func() { _ = s.balanceOf(alice) }); c != b20GasWarmSlot {
		t.Errorf("warm read charged %d, want %d", c, b20GasWarmSlot)
	}
	// warm write, non-zero -> other: reset.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(600)) }); c != b20GasSstoreReset {
		t.Errorf("warm reset write charged %d, want %d", c, b20GasSstoreReset)
	}
	// warm write, same value: no-op.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(600)) }); c != b20GasWarmSlot {
		t.Errorf("warm no-op write charged %d, want %d", c, b20GasWarmSlot)
	}

	if got := ctx.StateGasUsed(); got != b20GasColdSlot+b20GasSstoreSet+b20GasWarmSlot+b20GasSstoreReset+b20GasWarmSlot {
		t.Errorf("stateGasUsed = %d", got)
	}
	if ctx.OutOfGas() {
		t.Error("should not be out of gas")
	}

	// Unmetered view never charges.
	free := newB20Storage(statedb, token)
	if c := charged(func() { _ = free.balanceOf(alice) }); c != 0 {
		t.Errorf("unmetered read charged %d, want 0", c)
	}
}

func TestB20StorageGasOutOfGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	token := b20Addr(b20VariantAsset, 1)
	gas := NewGasBudget(100) // less than one cold access
	ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
	s := newMeteredB20Storage(ctx)

	_ = s.getWord(slotAt(b20SlotTotalSupply)) // cold read needs 2100 > 100
	if !ctx.OutOfGas() {
		t.Fatal("expected out of gas")
	}
	if gas.RegularGas != 0 {
		t.Errorf("budget should be exhausted, got %d", gas.RegularGas)
	}
}

func TestB20StorageStrings(t *testing.T) {
	s := newTestStorage(t)

	// short string (< 32 bytes): stored in-slot, low byte = 2*len.
	s.setName("USD Coin")
	if got := s.name(); got != "USD Coin" {
		t.Errorf("name = %q", got)
	}
	if w := s.getWord(slotAt(b20SlotName)); w[31] != byte(len("USD Coin")*2) {
		t.Errorf("short string length byte = %d, want %d", w[31], len("USD Coin")*2)
	}

	// exactly 31 bytes stays short; 32+ goes long.
	short31 := strings.Repeat("a", 31)
	s.setSymbol(short31)
	if got := s.symbol(); got != short31 {
		t.Errorf("31-byte string round-trip failed")
	}

	long := strings.Repeat("x", 100)
	s.setContractURI(long)
	if got := s.contractURI(); got != long {
		t.Errorf("long string round-trip failed: len %d", len(got))
	}
	// long-string marker: low bit of the length slot is set.
	if w := s.getWord(slotAt(b20SlotContractURI)); w[31]&1 != 1 {
		t.Error("long string should set the low bit of the length slot")
	}
}
