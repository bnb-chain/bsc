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
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
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

	// Raw-slot checks, spelling out Solidity's derivation independently of the
	// helpers. Round-tripping through the accessors cannot catch a wrong layout:
	// a nested mapping whose two keys are swapped is self-consistent, so reads
	// and writes still agree with each other while diverging from every
	// reference implementation. Only the slot the value actually lands in tells
	// them apart.
	pad := func(b []byte) []byte { return common.BytesToHash(b).Bytes() }
	solMap := func(key, base []byte) []byte { return crypto.Keccak256(pad(key), pad(base)) }

	balSlot := solMap(alice.Bytes(), slotAt(b20SlotBalances).Bytes())
	if got := s.getWord(common.BytesToHash(balSlot)); new(uint256.Int).SetBytes(got.Bytes()).Uint64() != 500 {
		t.Errorf("balance is not at keccak256(alice ++ balancesBase)")
	}

	// allowances[owner][spender]: owner is the OUTER key, spender the inner one.
	inner := solMap(alice.Bytes(), slotAt(b20SlotAllowances).Bytes())
	allowSlot := solMap(bob.Bytes(), inner)
	if got := s.getWord(common.BytesToHash(allowSlot)); new(uint256.Int).SetBytes(got.Bytes()).Uint64() != 42 {
		t.Error("allowance is not at keccak256(spender ++ keccak256(owner ++ base)) — nesting order is wrong")
	}
	// The swapped nesting must be empty, which is what distinguishes the two.
	swappedInner := solMap(bob.Bytes(), slotAt(b20SlotAllowances).Bytes())
	if got := s.getWord(common.BytesToHash(solMap(alice.Bytes(), swappedInner))); got != (common.Hash{}) {
		t.Error("allowance also landed under the swapped nesting order")
	}

	// roles[role][account] nests the same way: role outer, account inner.
	s.setRole(roleMint, alice, true)
	roleInner := solMap(roleMint.Bytes(), slotAt(b20SlotRoles).Bytes())
	if got := s.getWord(common.BytesToHash(solMap(alice.Bytes(), roleInner))); got == (common.Hash{}) {
		t.Error("role is not at keccak256(account ++ keccak256(role ++ base))")
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

	// A balance slot is mapping-derived, so every access also pays the keccak
	// over the 64-byte (key ++ base) preimage.
	const keccak64 = params.Keccak256Gas + 2*params.Keccak256WordGas
	var (
		cold  = params.ColdSloadCostEIP2929
		warm  = params.WarmStorageReadCostEIP2929
		set   = params.SstoreSetGasEIP2200
		reset = params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929
	)

	// cold write, zero -> non-zero: cold surcharge + set.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(500)) }); c != keccak64+cold+set {
		t.Errorf("cold set write charged %d, want %d", c, keccak64+cold+set)
	}
	// warm read of the same slot.
	if c := charged(func() { _ = s.balanceOf(alice) }); c != keccak64+warm {
		t.Errorf("warm read charged %d, want %d", c, keccak64+warm)
	}
	// warm write, non-zero -> other. The slot was zero at the start of the
	// transaction, so under EIP-2200 net metering this is a dirty update
	// charged at the warm price, not a reset — the 20000 was already paid by
	// the first write. `reset` is exercised by TestB20StorageRefunds, where the
	// slot starts committed non-zero.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(600)) }); c != keccak64+warm {
		t.Errorf("dirty update charged %d, want %d", c, keccak64+warm)
	}
	// warm write, same value: no-op.
	if c := charged(func() { s.setBalance(alice, uint256.NewInt(600)) }); c != keccak64+warm {
		t.Errorf("warm no-op write charged %d, want %d", c, keccak64+warm)
	}
	_ = reset

	if got, want := ctx.StateGasUsed(), 4*keccak64+cold+set+warm+warm+warm; got != want {
		t.Errorf("stateGasUsed = %d, want %d", got, want)
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

// TestB20StorageStringShrink pins that rewriting a string releases the tail
// slots it no longer needs, as a Solidity assignment would. Reads are
// length-bounded and would look correct either way, so the check is on the raw
// slots: a leftover word diverges the state root from a Solidity reference.
func TestB20StorageStringShrink(t *testing.T) {
	s := newTestStorage(t)
	tailSlot := func(i uint64) common.Hash {
		base := new(uint256.Int).SetBytes(crypto.Keccak256(slotAt(b20SlotName).Bytes()))
		return common.Hash(base.AddUint64(base, i).Bytes32())
	}

	// 100 bytes spans four tail slots.
	s.setName(strings.Repeat("x", 100))
	for i := uint64(0); i < 4; i++ {
		if s.getWord(tailSlot(i)) == (common.Hash{}) {
			t.Fatalf("tail slot %d empty after writing a 100-byte name", i)
		}
	}

	// Shrink to a long-but-shorter value: slots 2 and 3 must be released.
	s.setName(strings.Repeat("y", 40))
	if got := s.name(); got != strings.Repeat("y", 40) {
		t.Errorf("name = %q, want 40 y's", got)
	}
	for i := uint64(2); i < 4; i++ {
		if got := s.getWord(tailSlot(i)); got != (common.Hash{}) {
			t.Errorf("tail slot %d = %x after shrink, want cleared", i, got)
		}
	}

	// Shrink to an inline short string: every tail slot must be released.
	s.setName("USD")
	if got := s.name(); got != "USD" {
		t.Errorf("name = %q, want USD", got)
	}
	for i := uint64(0); i < 4; i++ {
		if got := s.getWord(tailSlot(i)); got != (common.Hash{}) {
			t.Errorf("tail slot %d = %x after shrink to short, want cleared", i, got)
		}
	}
}

// TestB20StringBoundaryMatrix walks every directed transition between the
// lengths where the storage encoding changes shape: empty, inline, the 31/32
// inline-to-long boundary, and the 32-byte chunk boundaries. For each it checks
// the value round-trips, the length slot carries the right short/long marker,
// and the data region holds exactly the chunks the new value needs — no stale
// slot left behind by the old one.
func TestB20StringBoundaryMatrix(t *testing.T) {
	lengths := []int{0, 1, 31, 32, 33, 64, 65}
	// A generous scan bound: the largest case needs 3 chunks, so 8 proves that
	// nothing lingers past the end as well.
	const scan = 8

	for _, from := range lengths {
		for _, to := range lengths {
			s := newTestStorage(t)
			slot := slotAt(b20SlotName)
			dataSlot := func(i uint64) common.Hash {
				base := new(uint256.Int).SetBytes(crypto.Keccak256(slot.Bytes()))
				return common.Hash(base.AddUint64(base, i).Bytes32())
			}
			// Distinct fill bytes so a stale chunk cannot masquerade as fresh.
			before, after := strings.Repeat("a", from), strings.Repeat("b", to)

			s.setName(before)
			s.setName(after)

			if got := s.name(); got != after {
				t.Errorf("%d->%d: name = %q (len %d), want len %d", from, to, got, len(got), to)
			}
			word := s.getWord(slot)
			wantLong := to >= 32
			if isLong := word[31]&1 == 1; isLong != wantLong {
				t.Errorf("%d->%d: length slot long-marker = %v, want %v", from, to, isLong, wantLong)
			}
			if !wantLong && int(word[31]) != to*2 {
				t.Errorf("%d->%d: inline length byte = %d, want %d", from, to, word[31], to*2)
			}
			wantChunks := 0
			if wantLong {
				wantChunks = (to + 31) / 32
			}
			for i := uint64(0); i < scan; i++ {
				occupied := s.getWord(dataSlot(i)) != (common.Hash{})
				if want := i < uint64(wantChunks); occupied != want {
					t.Errorf("%d->%d: data slot %d occupied = %v, want %v (stale tail not released)",
						from, to, i, occupied, want)
				}
			}
		}
	}
}

// TestB20LongStringGas pins the cost of a long string's keccak-derived data
// region. Deriving that root is a runtime keccak exactly as a mapping slot is,
// and leaving it unmetered would donate the computation on every long name,
// symbol or contractURI access.
func TestB20LongStringGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	token := b20Addr(b20VariantAsset, 1)
	gas := NewGasBudget(10_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
	s := newMeteredB20Storage(ctx)

	charged := func(fn func()) uint64 {
		before := gas.RegularGas
		fn()
		return before - gas.RegularGas
	}
	const (
		keccak32 = params.Keccak256Gas + params.Keccak256WordGas // 32-byte preimage
		cold     = params.ColdSloadCostEIP2929
		warm     = params.WarmStorageReadCostEIP2929
		set      = params.SstoreSetGasEIP2200
	)

	// A short string lives inline: one slot, no data region, no keccak.
	if c := charged(func() { s.setName("USD Coin") }); c != cold+set {
		t.Errorf("short-string write charged %d, want %d", c, cold+set)
	}
	if c := charged(func() { _ = s.name() }); c != warm {
		t.Errorf("short-string read charged %d, want %d", c, warm)
	}

	// 100 bytes spans four data slots. The write pays: reading the old length to
	// see what has to be released, rewriting the length slot, one keccak for the
	// data root, and four cold sets. Both length-slot touches are warm dirty
	// updates — the slot was written above in the same transaction.
	long := strings.Repeat("x", 100)
	if c := charged(func() { s.setName(long) }); c != 2*warm+keccak32+4*(cold+set) {
		t.Errorf("long-string write charged %d, want %d", c, 2*warm+keccak32+4*(cold+set))
	}
	// The read pays the length slot, the same single keccak, and four warm reads.
	if c := charged(func() { _ = s.name() }); c != warm+keccak32+4*warm {
		t.Errorf("long-string read charged %d, want %d", c, warm+keccak32+4*warm)
	}

	// Shrinking back to inline releases the four data slots, and must derive the
	// data root only once for the whole operation — a second derivation would
	// show up here as an extra keccak32.
	if c := charged(func() { s.setName("USD") }); c != 2*warm+keccak32+4*warm {
		t.Errorf("shrink-to-short charged %d, want %d", c, 2*warm+keccak32+4*warm)
	}
	if got := s.name(); got != "USD" {
		t.Errorf("name = %q, want USD", got)
	}
}

// TestB20SpawnedContextPropagatesOutOfGas pins that exhausting a spawned
// context's budget is visible to the context it was spawned from. The budget is
// shared by pointer, so only the flag can go missing — and it is the spawner's
// dispatcher, never the child's, that checks it before reporting success.
func TestB20SpawnedContextPropagatesOutOfGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	gas := NewGasBudget(100)
	parent := &PrecompileContext{
		StateDB: statedb, Self: b20Addr(b20VariantAsset, 1),
		Caller: b20Alice, DirectCall: true, gas: &gas,
	}
	child := parent.spawnBootstrap(b20Addr(b20VariantAsset, 2), b20Alice)

	if parent.OutOfGas() || child.OutOfGas() {
		t.Fatal("fresh contexts must not report out of gas")
	}
	child.chargeStateGas(1_000_000) // more than the shared budget holds

	if !child.OutOfGas() {
		t.Error("child does not report out of gas after an unaffordable charge")
	}
	if !parent.OutOfGas() {
		t.Error("spawner does not see the child's exhaustion — it would report success over an empty budget")
	}
	if got := parent.GasLeft(); got != 0 {
		t.Errorf("shared budget = %d, want 0", got)
	}

	// The flag is one shared cell, not a copy kept in step, so it travels the
	// other way too: a context spawned after exhaustion starts exhausted.
	if later := parent.spawnBootstrap(b20Addr(b20VariantAsset, 3), b20Alice); !later.OutOfGas() {
		t.Error("a context spawned from an exhausted frame does not start exhausted")
	}
	// And a charge failing in the parent is visible to a child spawned earlier.
	gas2 := NewGasBudget(100)
	p2 := &PrecompileContext{StateDB: statedb, Self: b20Addr(b20VariantAsset, 4), gas: &gas2}
	c2 := p2.spawnBootstrap(b20Addr(b20VariantAsset, 5), b20Alice)
	p2.chargeStateGas(1_000_000)
	if !c2.OutOfGas() {
		t.Error("child spawned before the spawner's exhaustion does not observe it")
	}
}

// TestB20SpawnedContextSharesStateGasTally pins that state gas charged inside a
// bootstrap child counts toward the frame's total. A bootstrap is the same EVM
// frame with a different Self — it shares the enforced budget — so a tally that
// dropped its charges would under-report the frame once the StateGas reservoir
// is enforced rather than merely recorded.
func TestB20SpawnedContextSharesStateGasTally(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	gas := NewGasBudget(10_000_000)
	parent := &PrecompileContext{
		StateDB: statedb, Self: b20Addr(b20VariantAsset, 1),
		Caller: b20Alice, DirectCall: true, gas: &gas,
	}
	child := parent.spawnBootstrap(b20Addr(b20VariantAsset, 2), b20Alice)

	parent.chargeStateGas(700)
	child.chargeStateGas(300)

	if got := parent.StateGasUsed(); got != 1000 {
		t.Errorf("spawner StateGasUsed = %d, want 1000 — the child's charges are missing", got)
	}
	if got := child.StateGasUsed(); got != 1000 {
		t.Errorf("child StateGasUsed = %d, want 1000 — the tally is not frame-wide", got)
	}
}

// TestB20SpawnedContextPropagatesSentryRefusal covers the other way a spawned
// frame stops being able to write: the EIP-2200 reentrancy sentry refuses an
// SSTORE while gas remains. Nothing drains the budget here, so unlike an
// unaffordable charge this cannot be caught downstream by a later failing
// charge — the spawner would hold gas in hand and report success over a write
// that never landed.
func TestB20SpawnedContextPropagatesSentryRefusal(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	// Above zero but at or below the 2300 stipend, so the sentry refuses.
	gas := NewGasBudget(params.SstoreSentryGasEIP2200)
	parent := &PrecompileContext{
		StateDB: statedb, Self: b20Addr(b20VariantAsset, 1),
		Caller: b20Alice, DirectCall: true, gas: &gas,
	}
	token := b20Addr(b20VariantAsset, 2)
	child := parent.spawnBootstrap(token, b20Alice)

	b20Storage{state: statedb, token: token, ctx: child}.
		setWord(common.Hash{31: 7}, common.Hash{31: 1})

	if !child.OutOfGas() {
		t.Error("child does not report out of gas after a sentry-refused write")
	}
	if !parent.OutOfGas() {
		t.Error("spawner does not see the sentry refusal — it would report success over a skipped write")
	}
	if got := statedb.GetState(token, common.Hash{31: 7}); got != (common.Hash{}) {
		t.Errorf("refused write landed anyway: slot = %x", got)
	}
	if parent.GasLeft() == 0 {
		t.Error("sentry refusal should not itself drain the budget; the test would prove nothing")
	}
}

// TestB20StorageRefunds pins the EIP-3529 refund arms of the net-metered
// write path against the interpreter's own makeGasSStoreFunc.
func TestB20StorageRefunds(t *testing.T) {
	clearing := params.SstoreClearsScheduleRefundEIP3529

	newCtx := func() (*state.StateDB, b20Storage, *PrecompileContext) {
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		token := b20Addr(b20VariantAsset, 1)
		// Without the sentinel the token is an EIP-161 empty account and
		// Finalise below would reap it, storage included (BEP-702 3.16).
		statedb.SetCode(token, b20MarkerCode, tracing.CodeChangeContractCreation)
		gas := NewGasBudget(10_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
		return statedb, newMeteredB20Storage(ctx), ctx
	}
	slot := slotAt(b20SlotTotalSupply)
	one := common.Hash{31: 1}
	two := common.Hash{31: 2}

	// Clearing a slot that existed at the start of the transaction refunds.
	statedb, s, _ := newCtx()
	s.state.SetState(s.token, slot, one)
	statedb.Finalise(true) // commit, so `original` is non-zero
	s.setWord(slot, common.Hash{})
	if got := statedb.GetRefund(); got != clearing {
		t.Errorf("clear refund = %d, want %d", got, clearing)
	}

	// Re-creating it in the same transaction takes the refund back.
	s.setWord(slot, two)
	if got := statedb.GetRefund(); got != 0 {
		t.Errorf("refund after recreate = %d, want 0", got)
	}

	// Restoring a dirty slot to its committed value refunds the difference
	// between the reset price and a warm read.
	statedb, s, _ = newCtx()
	s.state.SetState(s.token, slot, one)
	statedb.Finalise(true)
	s.setWord(slot, two)
	s.setWord(slot, one)
	want := (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929
	if got := statedb.GetRefund(); got != want {
		t.Errorf("reset-to-original refund = %d, want %d", got, want)
	}
}

// TestB20SstoreSentry verifies the EIP-2200 reentrancy guard: a write is
// refused whenever remaining gas is at or below the 2300 call stipend, however
// cheap the write itself would be, and no state change happens.
func TestB20SstoreSentry(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)
	slot := slotAt(b20SlotTotalSupply)

	// Warm the slot and dirty it so the write itself would be cheap (~100 gas),
	// then leave exactly the stipend in the budget.
	gas := NewGasBudget(params.SstoreSentryGasEIP2200)
	ctx := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas}
	s := newMeteredB20Storage(ctx)
	statedb.AddSlotToAccessList(token, slot)

	s.setWord(slot, common.Hash{31: 7})
	if !ctx.OutOfGas() {
		t.Fatal("write at the stipend boundary must trip the sentry")
	}
	if got := statedb.GetState(token, slot); got != (common.Hash{}) {
		t.Fatalf("refused write still mutated state: %x", got)
	}

	// One gas above the stipend, the same write succeeds.
	gas2 := NewGasBudget(params.SstoreSentryGasEIP2200 + 1 + params.SstoreSetGasEIP2200)
	ctx2 := &PrecompileContext{StateDB: statedb, Self: token, gas: &gas2}
	newMeteredB20Storage(ctx2).setWord(slot, common.Hash{31: 7})
	if ctx2.OutOfGas() {
		t.Fatal("write above the stipend must be allowed")
	}
	if got := statedb.GetState(token, slot); got != (common.Hash{31: 7}) {
		t.Fatalf("state = %x, want 7", got)
	}
}

// TestB20GasNeverCheaperThanBytecode pins BEP-702 3.14's central rule: a B20
// operation must never cost less than the same state accesses performed
// through bytecode. It compares a transfer's charge against the floor an
// equivalent BEP-20 implementation pays for the same accesses — two cold slot
// reads and two cold writes for a first-time recipient — and additionally
// requires the derivation and dispatch work B20 does on top to be accounted.
func TestB20GasNeverCheaperThanBytecode(t *testing.T) {
	_, evm := newAmsterdamEVM(t)
	creator := common.HexToAddress("0xfee")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0x9a"), creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	const budget = 1_000_000
	measure := func(to common.Address, amount uint64) uint64 {
		t.Helper()
		in := b20Call(selTransfer, addrKey(to), u256hash(amount))
		// Warm the slots first, then measure the second call.
		if _, _, err := evm.Call(b20Alice, token, in, NewGasBudget(budget), uint256.NewInt(0)); err != nil {
			t.Fatalf("warming transfer: %v", err)
		}
		_, left, err := evm.Call(b20Alice, token, in, NewGasBudget(budget), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("measured transfer: %v", err)
		}
		return uint64(budget) - left.RegularGas
	}

	// Warm-path floor: both balance slots are warm and dirty by now, so bytecode
	// would pay 2 * SLOAD_warm + 2 * SSTORE_dirty, plus a 64-byte keccak per
	// balance slot to derive it.
	keccak64 := params.Keccak256Gas + 2*params.Keccak256WordGas
	floor := 4*params.WarmStorageReadCostEIP2929 + 2*keccak64

	ordinary := measure(b20Bob, 10)
	if ordinary < floor {
		t.Fatalf("warm transfer charged %d, below the bytecode floor %d", ordinary, floor)
	}

	// The degenerate shapes must cost exactly what an ordinary transfer costs.
	// A self-transfer and a zero-value transfer look like free wins — no balance
	// ends up different — but bytecode performs both assignments regardless, so
	// skipping them would make a native token cheaper than the contract it
	// replaces, which BEP-702 3.14 forbids outright.
	//
	// The floor above is far too loose to catch that on its own: the balance
	// accesses are a few hundred gas out of a few thousand, so dropping two
	// writes still clears it. Equality with the ordinary shape is what actually
	// pins it, and this is precisely the "optimisation" a later reader would
	// reach for.
	for _, tc := range []struct {
		what   string
		to     common.Address
		amount uint64
	}{
		{"self-transfer", b20Alice, 10},
		{"zero-value transfer", b20Bob, 0},
		{"zero-value self-transfer", b20Alice, 0},
	} {
		if charged := measure(tc.to, tc.amount); charged != ordinary {
			t.Errorf("%s charged %d, want %d — the same accesses bytecode would perform",
				tc.what, charged, ordinary)
		}
	}
}
