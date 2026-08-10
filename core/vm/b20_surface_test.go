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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Coverage for the parts of the dispatch surface that other tests reach only
// indirectly. A selector-by-selector audit found 21 of 87 never dispatched by
// any test; these are the ones whose absence could hide a real defect.

// TestB20ConstantGetters dispatches every pure constant getter and checks the
// value against the derivation the standard specifies, not against the variable
// the implementation happens to hold. A constant is the one thing no other test
// can catch being wrong: nothing downstream fails, callers just bind to a role
// or scope nobody grants.
func TestB20ConstantGetters(t *testing.T) {
	_, token, run := newTokenWithEVM(t, 1, nil)
	_ = token

	// Role ids are keccak of their canonical names, except DEFAULT_ADMIN which is
	// bytes32(0) so that an unset role admin means DEFAULT_ADMIN.
	roles := []struct {
		sel  [4]byte
		name string // "" means the zero hash
	}{
		{selDefaultAdminRole, ""},
		{selMintRole, "MINT_ROLE"},
		{selBurnRole, "BURN_ROLE"},
		{selSeizeRole, "SEIZE_ROLE"},
		{selPauseRole, "PAUSE_ROLE"},
		{selUnpauseRole, "UNPAUSE_ROLE"},
		{selMetadataRole, "METADATA_ROLE"},
	}
	for _, r := range roles {
		want := common.Hash{}
		if r.name != "" {
			want = crypto.Keccak256Hash([]byte(r.name))
		}
		got, err := run(b20Alice, b20Call(r.sel))
		if err != nil {
			t.Errorf("%s: %v", r.name, err)
			continue
		}
		if !bytes.Equal(got, want.Bytes()) {
			t.Errorf("role %q = %x, want keccak256(%q) = %x", r.name, got, r.name, want)
		}
	}

	// Policy scope ids are keccak of their canonical names too.
	scopes := []struct {
		sel  [4]byte
		name string
	}{
		{selTransferSenderScope, "TRANSFER_SENDER_POLICY"},
		{selTransferReceiverScope, "TRANSFER_RECEIVER_POLICY"},
		{selTransferExecutorScope, "TRANSFER_EXECUTOR_POLICY"},
		{selMintReceiverScope, "MINT_RECEIVER_POLICY"},
		{selSeizeHolderScope, "SEIZE_HOLDER_POLICY"},
		{selSeizeReceiverScope, "SEIZE_RECEIVER_POLICY"},
	}
	for _, s := range scopes {
		want := crypto.Keccak256Hash([]byte(s.name))
		got, err := run(b20Alice, b20Call(s.sel))
		if err != nil {
			t.Errorf("%s: %v", s.name, err)
			continue
		}
		if !bytes.Equal(got, want.Bytes()) {
			t.Errorf("scope %q = %x, want keccak256(%q) = %x", s.name, got, s.name, want)
		}
	}

	// OPERATOR_ROLE belongs to the Asset variant's own surface, so it is reached
	// through a real Asset token rather than the shared dispatch.
	_, evm := newAmsterdamEVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x0b"), creator, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	assetToken := common.BytesToAddress(ret)
	ret, _, err = evm.Call(b20Alice, assetToken, b20Call(selOperatorRole),
		NewGasBudget(1_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("OPERATOR_ROLE: %v", err)
	}
	if want := crypto.Keccak256Hash([]byte("OPERATOR_ROLE")); !bytes.Equal(ret, want.Bytes()) {
		t.Errorf("OPERATOR_ROLE() = %x, want %x", ret, want)
	}
	// It must not collide with any shared role either.
	if ret, _, err := evm.Call(b20Alice, assetToken, b20Call(selMintRole),
		NewGasBudget(1_000_000), uint256.NewInt(0)); err == nil &&
		bytes.Equal(ret, crypto.Keccak256Hash([]byte("OPERATOR_ROLE")).Bytes()) {
		t.Error("OPERATOR_ROLE collides with MINT_ROLE")
	}

	// Every id must be distinct: a collision would silently merge two roles or
	// two compliance scopes.
	seen := map[string]string{}
	for _, r := range roles {
		if r.name == "" {
			continue
		}
		k := crypto.Keccak256Hash([]byte(r.name)).Hex()
		if prev, dup := seen[k]; dup {
			t.Errorf("id collision: %q and %q", prev, r.name)
		}
		seen[k] = r.name
	}
	for _, s := range scopes {
		k := crypto.Keccak256Hash([]byte(s.name)).Hex()
		if prev, dup := seen[k]; dup {
			t.Errorf("id collision: %q and %q", prev, s.name)
		}
		seen[k] = s.name
	}
}

// TestB20MemoVariants covers the three *WithMemo methods no other test
// dispatches. Each is a primary operation followed by Memo, so what needs
// pinning is that the primary op still happens, that Memo lands after it, and
// that a failing primary op emits nothing at all.
func TestB20MemoVariants(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	memo := common.HexToHash("0xfeed")

	setup := func() (*b20Storage, func(common.Address, []byte) ([]byte, error), func() []common.Hash) {
		statedb, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
			s.setRole(roleDefaultAdmin, admin, true)
			s.setRole(roleMint, admin, true)
			s.setRole(roleBurn, admin, true)
			s.setAdminCount(uint256.NewInt(1))
			s.setSupplyCap(uint256.NewInt(1_000_000))
			s.setBalance(b20Alice, uint256.NewInt(1000))
		})
		txHash := common.HexToHash("0x11")
		statedb.SetTxContext(txHash, 0)
		topics := func() []common.Hash {
			var out []common.Hash
			for _, l := range statedb.GetLogs(txHash, 1, common.Hash{}, 1) {
				out = append(out, l.Topics[0])
			}
			return out
		}
		view := newB20Storage(statedb, b20Addr(b20VariantAsset, 1))
		return &view, run, topics
	}

	// mintWithMemo: Transfer(0 -> to) then Memo.
	view, run, topics := setup()
	if _, err := run(admin, b20Call(selMintWithMemo, addrKey(b20Bob), u256hash(50), memo)); err != nil {
		t.Fatalf("mintWithMemo: %v", err)
	}
	if got := view.balanceOf(b20Bob).Uint64(); got != 50 {
		t.Errorf("mintWithMemo credited %d, want 50", got)
	}
	if got := topics(); len(got) != 2 || got[0] != b20TopicTransfer || got[1] != b20TopicMemo {
		t.Errorf("mintWithMemo topics = %v, want [Transfer, Memo]", got)
	}

	// burnWithMemo burns from the caller.
	view, run, topics = setup()
	if _, err := run(admin, b20Call(selGrantRole, roleBurn, addrKey(b20Alice))); err != nil {
		t.Fatalf("grant BURN: %v", err)
	}
	if _, err := run(b20Alice, b20Call(selBurnWithMemo, u256hash(400), memo)); err != nil {
		t.Fatalf("burnWithMemo: %v", err)
	}
	if got := view.balanceOf(b20Alice).Uint64(); got != 600 {
		t.Errorf("burnWithMemo left %d, want 600", got)
	}
	last := topics()[len(topics())-1]
	if last != b20TopicMemo {
		t.Errorf("burnWithMemo last topic = %s, want Memo", last.Hex())
	}

	// transferFromWithMemo spends an allowance and reports the spender in Memo.
	view, run, topics = setup()
	if _, err := run(b20Alice, b20Call(selApprove, addrKey(b20Carol), u256hash(300))); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := run(b20Carol, b20Call(selTransferFromWithMemo, addrKey(b20Alice), addrKey(b20Bob), u256hash(120), memo)); err != nil {
		t.Fatalf("transferFromWithMemo: %v", err)
	}
	if got := view.balanceOf(b20Bob).Uint64(); got != 120 {
		t.Errorf("transferFromWithMemo moved %d, want 120", got)
	}
	if got := view.allowance(b20Alice, b20Carol).Uint64(); got != 180 {
		t.Errorf("allowance after transferFromWithMemo = %d, want 180", got)
	}

	// A failing primary operation emits nothing — not even the memo.
	_, run, topics = setup()
	before := len(topics())
	if _, err := run(b20Alice, b20Call(selMintWithMemo, addrKey(b20Bob), u256hash(1), memo)); err == nil {
		t.Error("mintWithMemo without MINT_ROLE should fail")
	}
	if got := len(topics()); got != before {
		t.Errorf("a failed mintWithMemo emitted %d logs, want none", got-before)
	}
}

// TestB20RoleAdminMachinery covers getRoleAdmin and setRoleAdmin, which nothing
// else dispatches. An unset role admin reads as DEFAULT_ADMIN, so the delegation
// this enables is the only way a role can be granted by someone other than the
// default admin.
func TestB20RoleAdminMachinery(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	delegate := common.HexToAddress("0xde1e9a7e")

	statedb, token, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(roleDefaultAdmin, admin, true)
		s.setAdminCount(uint256.NewInt(1))
	})
	view := newB20Storage(statedb, token)

	// Unset means DEFAULT_ADMIN, which is the zero hash.
	got, err := run(admin, b20Call(selGetRoleAdmin, roleMint))
	if err != nil {
		t.Fatalf("getRoleAdmin: %v", err)
	}
	if !bytes.Equal(got, common.Hash{}.Bytes()) {
		t.Errorf("unset role admin = %x, want the zero hash (DEFAULT_ADMIN)", got)
	}

	// Delegate MINT_ROLE's administration to a role the delegate holds.
	if _, err := run(admin, b20Call(selSetRoleAdmin, roleMint, roleMetadata)); err != nil {
		t.Fatalf("setRoleAdmin: %v", err)
	}
	got, err = run(admin, b20Call(selGetRoleAdmin, roleMint))
	if err != nil {
		t.Fatalf("getRoleAdmin: %v", err)
	}
	if !bytes.Equal(got, roleMetadata.Bytes()) {
		t.Errorf("role admin = %x, want METADATA_ROLE %x", got, roleMetadata)
	}

	// The delegate can now grant MINT_ROLE, and the default admin no longer can:
	// administration moved rather than being shared.
	if _, err := run(admin, b20Call(selGrantRole, roleMetadata, addrKey(delegate))); err != nil {
		t.Fatalf("grant METADATA to delegate: %v", err)
	}
	if _, err := run(delegate, b20Call(selGrantRole, roleMint, addrKey(b20Bob))); err != nil {
		t.Fatalf("delegate could not grant MINT: %v", err)
	}
	if !view.hasRole(roleMint, b20Bob) {
		t.Error("MINT_ROLE was not granted")
	}
	if _, err := run(admin, b20Call(selGrantRole, roleMint, addrKey(b20Carol))); err == nil {
		t.Error("the default admin still granted MINT after administration moved")
	}
}

// TestB20MiscViews covers the remaining views nothing else dispatches: symbol,
// isPaused and the factory's isB20.
func TestB20MiscViews(t *testing.T) {
	_, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setSymbol("TT")
		s.setPaused(uint256.NewInt(1 << b20PauseBurn))
	})

	ret, err := run(b20Alice, b20Call(selSymbol))
	if err != nil {
		t.Fatalf("symbol: %v", err)
	}
	if got := decodeString(t, ret); got != "TT" {
		t.Errorf("symbol() = %q, want TT", got)
	}

	for feature, want := range map[byte]bool{
		b20PauseTransfer: false, b20PauseMint: false,
		b20PauseBurn: true, b20PauseSeize: false,
	} {
		ret, err := run(b20Alice, b20Call(selIsPaused, wU8(feature)))
		if err != nil {
			t.Fatalf("isPaused(%d): %v", feature, err)
		}
		if got := bytes.Equal(ret, encBool(true)); got != want {
			t.Errorf("isPaused(%d) = %v, want %v", feature, got, want)
		}
	}

	// isB20 is the factory's syntactic check: prefix only, ignoring the variant
	// byte and saying nothing about whether a token exists there.
	_, evm := newAmsterdamEVM(t)
	askIsB20 := func(a common.Address) bool {
		ret, _, err := evm.Call(b20Alice, B20FactoryAddress, b20Call(selIsB20, addrKey(a)),
			NewGasBudget(1_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("isB20: %v", err)
		}
		return bytes.Equal(ret, encBool(true))
	}
	for _, tc := range []struct {
		name string
		addr common.Address
		want bool
	}{
		{"asset prefix", b20Addr(b20VariantAsset, 1), true},
		{"never created", b20Addr(b20VariantAsset, 0xfe), true},
		{"unrecognized variant still in space", b20Addr(0x7f, 1), true},
		{"the factory itself", B20FactoryAddress, false},
		{"outside the space", common.HexToAddress("0x1234"), false},
		{"zero address", common.Address{}, false},
	} {
		if got := askIsB20(tc.addr); got != tc.want {
			t.Errorf("isB20(%s) [%s] = %v, want %v", tc.addr.Hex(), tc.name, got, tc.want)
		}
	}
}
