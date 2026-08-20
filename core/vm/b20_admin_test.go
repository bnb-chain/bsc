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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

func b20CallU8Array(sel [4]byte, vals ...byte) []byte {
	out := append([]byte{}, sel[:]...)
	out = append(out, u256hash(0x20).Bytes()...)              // head offset
	out = append(out, u256hash(uint64(len(vals))).Bytes()...) // length
	for _, v := range vals {
		out = append(out, u256hash(uint64(v)).Bytes()...)
	}
	return out
}

// TestB20PauseRejectsMalformedArray pins strict uint8[] decoding. A truncating
// decoder is worse than a permissive one here: 0x0100 would silently become
// feature 0 and pause a feature the caller never named.
func TestB20PauseRejectsMalformedArray(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	_, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(rolePause, admin, true)
	})

	word := func(v uint64) []byte { return u256hash(v).Bytes() }
	dirty := func(hi byte) []byte { // a word with a nonzero byte above the low one
		var w common.Hash
		w[0], w[31] = hi, 0
		return w.Bytes()
	}

	cases := []struct {
		name string
		args []byte
	}{
		{"element above uint8 range",
			append(append(word(0x20), word(1)...), word(0x100)...)},
		{"element with dirty high bytes",
			append(append(word(0x20), word(1)...), dirty(1)...)},
		{"length word with dirty high bytes",
			append(append(word(0x20), dirty(1)...), word(0)...)},
		{"head offset with dirty high bytes",
			append(append(dirty(1), word(1)...), word(0)...)},
		{"head offset past the end",
			append(append(word(0x4000), word(1)...), word(0)...)},
	}
	for _, tc := range cases {
		input := append(append([]byte{}, selPause[:]...), tc.args...)
		if _, err := run(admin, input); !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want revert", tc.name, err)
		}
	}

	// The well-formed equivalent still works, so the checks are not just
	// rejecting everything.
	if _, err := run(admin, b20CallU8Array(selPause, byte(b20PauseTransfer))); err != nil {
		t.Errorf("well-formed pause: %v", err)
	}
}

func TestB20AdminLifecycle(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)
	admin := common.HexToAddress("0xad4149")
	minter := common.HexToAddress("0x33333")

	// Seed: admin holds DEFAULT_ADMIN, adminCount 1, generous cap.
	view := newUnmeteredB20Storage(statedb, token)
	view.setRole(roleDefaultAdmin, admin, true)
	view.setAdminCount(uint256.NewInt(1))
	view.setSupplyCap(uint256.NewInt(1_000_000))

	run := func(caller common.Address, input []byte) ([]byte, error) {
		gas := NewGasBudget(2_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: token, Caller: caller, DirectCall: true, gas: &gas}
		return newB20Token(ctx, 18).dispatch(input)
	}
	mustOK := func(what string, caller common.Address, input []byte) {
		t.Helper()
		if _, err := run(caller, input); err != nil {
			t.Fatalf("%s: unexpected err %v", what, err)
		}
	}
	mustRevert := func(what string, caller common.Address, input []byte) {
		t.Helper()
		if _, err := run(caller, input); !errors.Is(err, ErrExecutionReverted) {
			t.Fatalf("%s: err = %v, want revert", what, err)
		}
	}
	bal := func(a common.Address) uint64 { return view.balanceOf(a).Uint64() }

	// role constant getter.
	if ret, err := run(admin, b20Call(selMintRole)); err != nil || !bytes.Equal(ret, roleMint.Bytes()) {
		t.Fatalf("MINT_ROLE() = %x err %v", ret, err)
	}

	// admin grants MINT / BURN / PAUSE / UNPAUSE.
	mustOK("grant MINT", admin, b20Call(selGrantRole, roleMint, addrKey(minter)))
	mustOK("grant BURN", admin, b20Call(selGrantRole, roleBurn, addrKey(minter)))
	mustOK("grant PAUSE", admin, b20Call(selGrantRole, rolePause, addrKey(admin)))
	mustOK("grant UNPAUSE", admin, b20Call(selGrantRole, roleUnpause, addrKey(admin)))
	if ret, _ := run(admin, b20Call(selHasRole, roleMint, addrKey(minter))); !bytes.Equal(ret, encBool(true)) {
		t.Fatal("minter should hold MINT_ROLE")
	}

	// non-minter mint reverts; minter mint succeeds.
	mustRevert("unauthorized mint", admin, b20Call(selMint, addrKey(b20Alice), u256hash(100)))
	mustOK("mint", minter, b20Call(selMint, addrKey(b20Alice), u256hash(500)))
	if bal(b20Alice) != 500 || view.totalSupply().Uint64() != 500 {
		t.Fatalf("after mint: bal %d supply %d", bal(b20Alice), view.totalSupply().Uint64())
	}

	// mint beyond supply cap reverts.
	mustRevert("cap exceeded", minter, b20Call(selMint, addrKey(b20Alice), u256hash(1_000_000)))

	// pause TRANSFER -> transfer reverts; unpause -> ok.
	mustOK("pause TRANSFER", admin, b20CallU8Array(selPause, b20PauseTransfer))
	mustRevert("transfer while paused", b20Alice, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)))
	mustOK("unpause TRANSFER", admin, b20CallU8Array(selUnpause, b20PauseTransfer))
	mustOK("transfer after unpause", b20Alice, b20Call(selTransfer, addrKey(b20Bob), u256hash(100)))
	if bal(b20Bob) != 100 {
		t.Fatalf("bob balance %d", bal(b20Bob))
	}

	// pause MINT -> mint reverts.
	mustOK("pause MINT", admin, b20CallU8Array(selPause, b20PauseMint))
	mustRevert("mint while paused", minter, b20Call(selMint, addrKey(b20Alice), u256hash(1)))
	mustOK("unpause MINT", admin, b20CallU8Array(selUnpause, b20PauseMint))

	// burn by holder-with-role.
	supplyBefore := view.totalSupply().Uint64()
	mustOK("burn", minter, b20Call(selBurn, u256hash(0))) // minter has 0, burning 0 ok
	mustOK("mint to minter", minter, b20Call(selMint, addrKey(minter), u256hash(50)))
	mustOK("burn 50", minter, b20Call(selBurn, u256hash(50)))
	if view.totalSupply().Uint64() != supplyBefore {
		t.Fatalf("supply after mint+burn = %d, want %d", view.totalSupply().Uint64(), supplyBefore)
	}

	// empty feature set reverts.
	mustRevert("empty pause set", admin, b20CallU8Array(selPause))
}

func TestB20AdminLastAdminProtection(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	token := b20Addr(b20VariantStablecoin, 1)
	admin := common.HexToAddress("0xad4149")

	view := newUnmeteredB20Storage(statedb, token)
	view.setRole(roleDefaultAdmin, admin, true)
	view.setAdminCount(uint256.NewInt(1))

	run := func(caller common.Address, input []byte) ([]byte, error) {
		gas := NewGasBudget(2_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: token, Caller: caller, DirectCall: true, gas: &gas}
		return newB20Token(ctx, 6).dispatch(input)
	}

	// revoking the sole DEFAULT_ADMIN via revokeRole is blocked.
	if _, err := run(admin, b20Call(selRevokeRole, roleDefaultAdmin, addrKey(admin))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("revoking last admin should revert")
	}
	// renounceRole with mismatched confirmation reverts.
	if _, err := run(admin, b20Call(selRenounceRole, roleDefaultAdmin, addrKey(b20Bob))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("bad confirmation should revert")
	}
	// second admin can be added, then original can renounce normally.
	if _, err := run(admin, b20Call(selGrantRole, roleDefaultAdmin, addrKey(b20Bob))); err != nil {
		t.Fatalf("grant second admin: %v", err)
	}
	if view.adminCount().Uint64() != 2 {
		t.Fatalf("adminCount = %d, want 2", view.adminCount().Uint64())
	}

	// now the sole-admin path: renounceLastAdmin only works when count == 1.
	if _, err := run(admin, b20Call(selRenounceLastAdmin)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("renounceLastAdmin with 2 admins should revert")
	}
	// bob renounces via normal path (not last).
	if _, err := run(b20Bob, b20Call(selRenounceRole, roleDefaultAdmin, addrKey(b20Bob))); err != nil {
		t.Fatalf("bob renounce: %v", err)
	}
	if view.adminCount().Uint64() != 1 {
		t.Fatalf("adminCount = %d, want 1", view.adminCount().Uint64())
	}

	// sole admin renounces permanently; token becomes ungovernable.
	statedb.SetTxContext(common.HexToHash("0x1a57"), 0)
	if _, err := run(admin, b20Call(selRenounceLastAdmin)); err != nil {
		t.Fatalf("renounceLastAdmin: %v", err)
	}
	if !view.adminCount().IsZero() {
		t.Fatalf("adminCount = %d, want 0", view.adminCount().Uint64())
	}
	// RoleRevoked alone cannot express that no admin can ever exist again, so a
	// dedicated event names the departing admin.
	logs := statedb.GetLogs(common.HexToHash("0x1a57"), 1, common.Hash{}, 1)
	if len(logs) != 2 {
		t.Fatalf("renounceLastAdmin emitted %d logs, want 2 (RoleRevoked, LastAdminRenounced)", len(logs))
	}
	last := logs[1]
	if len(last.Topics) != 2 || last.Topics[0] != b20TopicLastAdminRenounced || last.Topics[1] != addrKey(admin) {
		t.Errorf("LastAdminRenounced topics = %v, want [LastAdminRenounced, admin]", last.Topics)
	}
	if len(last.Data) != 0 {
		t.Errorf("LastAdminRenounced data = %x, want empty", last.Data)
	}
	// no further role mutations are possible.
	if _, err := run(admin, b20Call(selGrantRole, roleMint, addrKey(admin))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("grant after renounceLastAdmin should revert (ungovernable)")
	}
}

// TestB20BurnChecksSupplyUnderflow covers the subtraction the balance check only
// bounds while the balances sum to totalSupply.
func TestB20BurnChecksSupplyUnderflow(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0xbb"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selGrantRole, roleBurn, addrKey(creator)),
			b20Call(selMint, addrKey(b20Alice), u256hash(100)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newUnmeteredB20Storage(statedb, token)

	if _, _, err := evm.Call(creator, token, b20Call(selGrantRole, roleBurn, addrKey(b20Alice)),
		NewGasBudget(1_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("granting alice BURN_ROLE: %v", err)
	}
	// Break the invariant the way a bad state import or a future field could:
	// alice holds more than the token has ever issued.
	view.setBalance(b20Alice, uint256.NewInt(500))
	if view.totalSupply().Uint64() != 100 {
		t.Fatalf("totalSupply = %d, want 100 — the fixture is not set up", view.totalSupply().Uint64())
	}

	_, _, err = evm.Call(b20Alice, token, b20Call(selBurn, u256hash(300)),
		NewGasBudget(1_000_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("burning past totalSupply: err = %v, want a revert", err)
	}
	if got := view.totalSupply().Uint64(); got != 100 {
		t.Errorf("totalSupply = %d after the refused burn, want 100. An unchecked Sub would "+
			"have wrapped it to near 2^256", got)
	}
}

// TestB20PauseDecodesBeforeAuthorizing pins the order Solidity's dispatcher has.
func TestB20PauseDecodesBeforeAuthorizing(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0xba"), creator, nil),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// b20Bob holds no role, so both checks would refuse him. The malformed array
	// has to be the one reported: a Solidity implementation decodes in the external
	// dispatcher, before any modifier runs, so it answers the same way.
	good := b20CallU8Array(selPause, byte(b20PauseTransfer))
	out, _, err := evm.Call(b20Bob, token, good[:len(good)-1], NewGasBudget(1_000_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("truncated pause args: err = %v, want a revert", err)
	}
	if len(out) != 0 {
		t.Errorf("returndata = %x, want empty. Malformed calldata reverts with no reason "+
			"(BEP-702 3.2); a role error here means authorization ran first", out)
	}

	// And a well-formed array from the same caller still reports the role.
	out, _, err = evm.Call(b20Bob, token, good, NewGasBudget(1_000_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("unauthorized pause: err = %v, want a revert", err)
	}
	if len(out) < 4 || [4]byte(out[:4]) != errSelACUnauthorized {
		t.Errorf("returndata = %x, want AccessControlUnauthorizedAccount", out)
	}
}
