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

func u256hash(n uint64) common.Hash { return common.Hash(uint256.NewInt(n).Bytes32()) }

func b20Call(sel [4]byte, args ...common.Hash) []byte {
	out := append([]byte{}, sel[:]...)
	for _, a := range args {
		out = append(out, a.Bytes()...)
	}
	return out
}

var (
	b20Alice = common.HexToAddress("0xa11ce")
	b20Bob   = common.HexToAddress("0xb0b")
	b20Carol = common.HexToAddress("0xca401")
)

func TestB20TokenDispatch(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)

	// Seed initial state through the unmetered view.
	view := newUnmeteredB20Storage(statedb, token)
	view.setName("Test Token")
	view.setSymbol("TT")
	view.setTotalSupply(uint256.NewInt(1000))
	view.setBalance(b20Alice, uint256.NewInt(1000))

	run := func(caller common.Address, ro bool, input []byte) ([]byte, error) {
		gas := NewGasBudget(1_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: token, Caller: caller, DirectCall: true, ReadOnly: ro, gas: &gas}
		return newB20Token(ctx, 18).dispatch(input)
	}
	readU := func(input []byte) uint64 {
		t.Helper()
		ret, err := run(b20Alice, true, input)
		if err != nil {
			t.Fatalf("view call err: %v", err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64()
	}
	wantU := func(label string, got, want uint64) {
		t.Helper()
		if got != want {
			t.Fatalf("%s = %d, want %d", label, got, want)
		}
	}

	// views
	if ret, err := run(b20Alice, true, b20Call(selName)); err != nil || string(bytes.TrimRight(ret[64:], "\x00")) != "Test Token" {
		t.Fatalf("name() = %q, err %v", ret, err)
	}
	wantU("decimals", readU(b20Call(selDecimals)), 18)
	wantU("totalSupply", readU(b20Call(selTotalSupply)), 1000)
	wantU("balanceOf(alice)", readU(b20Call(selBalanceOf, addrKey(b20Alice))), 1000)

	// transfer alice -> bob 100
	if ret, err := run(b20Alice, false, b20Call(selTransfer, addrKey(b20Bob), u256hash(100))); err != nil || !bytes.Equal(ret, encBool(true)) {
		t.Fatalf("transfer ret %x err %v", ret, err)
	}
	wantU("balanceOf(alice)", readU(b20Call(selBalanceOf, addrKey(b20Alice))), 900)
	wantU("balanceOf(bob)", readU(b20Call(selBalanceOf, addrKey(b20Bob))), 100)

	// approve alice -> carol 50, then carol transferFrom alice -> bob 30
	if _, err := run(b20Alice, false, b20Call(selApprove, addrKey(b20Carol), u256hash(50))); err != nil {
		t.Fatalf("approve err %v", err)
	}
	wantU("allowance", readU(b20Call(selAllowance, addrKey(b20Alice), addrKey(b20Carol))), 50)
	if _, err := run(b20Carol, false, b20Call(selTransferFrom, addrKey(b20Alice), addrKey(b20Bob), u256hash(30))); err != nil {
		t.Fatalf("transferFrom err %v", err)
	}
	wantU("balanceOf(alice)", readU(b20Call(selBalanceOf, addrKey(b20Alice))), 870)
	wantU("balanceOf(bob)", readU(b20Call(selBalanceOf, addrKey(b20Bob))), 130)
	wantU("allowance", readU(b20Call(selAllowance, addrKey(b20Alice), addrKey(b20Carol))), 20)

	// transferFrom beyond allowance reverts.
	if _, err := run(b20Carol, false, b20Call(selTransferFrom, addrKey(b20Alice), addrKey(b20Bob), u256hash(100))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("over-allowance err = %v, want revert", err)
	}
	// transfer beyond balance reverts.
	if _, err := run(b20Bob, false, b20Call(selTransfer, addrKey(b20Alice), u256hash(1e9))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("over-balance err = %v, want revert", err)
	}
	// state-mutating call in a read-only frame throws.
	if _, err := run(b20Alice, true, b20Call(selTransfer, addrKey(b20Bob), u256hash(1))); !errors.Is(err, ErrWriteProtection) {
		t.Fatalf("readonly transfer err = %v, want write protection", err)
	}
	// unknown selector reverts.
	if _, err := run(b20Alice, true, []byte{0xde, 0xad, 0xbe, 0xef}); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("unknown selector err = %v, want revert", err)
	}
}

func TestB20TokenPauseBlocksTransfer(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	token := b20Addr(b20VariantAsset, 1)
	view := newUnmeteredB20Storage(statedb, token)
	view.setBalance(b20Alice, uint256.NewInt(100))
	view.setPaused(uint256.NewInt(1 << b20PauseTransfer))

	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: token, Caller: b20Alice, DirectCall: true, gas: &gas}
	if _, err := newB20Token(ctx, 18).dispatch(b20Call(selTransfer, addrKey(b20Bob), u256hash(1))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("paused transfer err = %v, want revert", err)
	}
}

// TestB20EndToEndTransfer drives a transfer through the full EVM Call path:
// address resolution, the stateful precompile host, dispatch, state mutation
// and the Transfer log.
func TestB20EndToEndTransfer(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)
	// Simulate factory creation: initialization marker + seed balance.
	statedb.SetCode(token, B20MarkerCode, 0)
	newUnmeteredB20Storage(statedb, token).setBalance(b20Alice, uint256.NewInt(1000))

	cfg := *b20TestChainConfig()

	txHash := common.HexToHash("0x1234")
	statedb.SetTxContext(txHash, 0)

	bc := b20BlockContext(1)
	evm := NewEVM(bc, statedb, &cfg, Config{})
	if !evm.b20Enabled() {
		t.Fatal("B20 must be enabled for the precompile to resolve")
	}

	input := b20Call(selTransfer, addrKey(b20Bob), u256hash(250))
	ret, _, err := evm.Call(b20Alice, token, input, NewGasBudget(1_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("evm.Call transfer err: %v", err)
	}
	if !bytes.Equal(ret, encBool(true)) {
		t.Fatalf("transfer returned %x, want true", ret)
	}

	view := newUnmeteredB20Storage(statedb, token)
	if got := view.balanceOf(b20Alice).Uint64(); got != 750 {
		t.Errorf("alice balance = %d, want 750", got)
	}
	if got := view.balanceOf(b20Bob).Uint64(); got != 250 {
		t.Errorf("bob balance = %d, want 250", got)
	}

	logs := statedb.GetLogs(txHash, 1, common.Hash{}, 1)
	if len(logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(logs))
	}
	log := logs[0]
	if log.Address != token || log.Topics[0] != b20TopicTransfer {
		t.Errorf("unexpected log: addr %s topic %s", log.Address.Hex(), log.Topics[0].Hex())
	}
	if log.Topics[1] != addrKey(b20Alice) || log.Topics[2] != addrKey(b20Bob) {
		t.Errorf("Transfer topics from/to mismatch")
	}
}

// TestB20CalldataStrictnessProfile pins which malformed encodings are refused and
// which are not, so the profile is a decision rather than an accident.
func TestB20CalldataStrictnessProfile(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x57"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	call := func(caller common.Address, input []byte) ([]byte, error) {
		r, _, e := evm.Call(caller, token, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return r, e
	}

	// Refused: one byte short of the second argument.
	short := b20Call(selTransfer, addrKey(b20Bob), u256hash(1))
	if _, err := call(b20Alice, short[:len(short)-1]); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("truncated transfer args: err = %v, want a revert", err)
	}
	// Refused: an address word with bits above the low 20 bytes.
	dirty := addrKey(b20Bob)
	dirty[0] = 0x01
	if _, err := call(b20Alice, b20Call(selTransfer, dirty, u256hash(1))); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("dirty address high bits: err = %v, want a revert", err)
	}

	// Accepted: a whole extra word after a complete argument list, as Solidity
	// accepts it. The transfer must go through, not merely fail to revert.
	before := newUnmeteredB20Storage(statedb, token).balanceOf(b20Bob).Uint64()
	trailing := append(b20Call(selTransfer, addrKey(b20Bob), u256hash(7)), u256hash(0xdead).Bytes()...)
	if r, err := call(b20Alice, trailing); err != nil || !bytes.Equal(r, encBool(true)) {
		t.Fatalf("transfer with trailing data: ret %x err %v, want success — Solidity ignores "+
			"extra calldata, so refusing it would diverge from the contract this replaces", r, err)
	}
	if got := newUnmeteredB20Storage(statedb, token).balanceOf(b20Bob).Uint64(); got != before+7 {
		t.Errorf("bob's balance = %d, want %d; the trailing word changed how the args decoded",
			got, before+7)
	}
}

// TestB20TransferFromSelfSpendsAllowance covers the owner moving their own balance
// through transferFrom: the allowance is spent, the executor policy is not
// consulted.
func TestB20TransferFromSelfSpendsAllowance(t *testing.T) {
	statedb, evm := newB20EVM(t)
	admin := b20TestCaller

	// A policy authorizing nobody, bound as the executor scope, so the shortcut is
	// observable: without it the self transfer would be refused too.
	ret, _, err := evm.Call(admin, B20PolicyRegistryAddress,
		b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyAllowlist)),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	empty := new(uint256.Int).SetBytes(ret).Uint64()

	ret, _, err = evm.Call(admin, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x5e1f"), admin, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(admin)),
			b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
			b20Call(selUpdatePolicy, scopeTransferExecutor, wU64(empty)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newUnmeteredB20Storage(statedb, token)
	send := func(caller common.Address, input []byte) ([]byte, error) {
		r, _, e := evm.Call(caller, token, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return r, e
	}

	// No self-approval yet: the allowance is consumed unconditionally, so this is
	// InsufficientAllowance and not a free pass.
	out, err := send(b20Alice, b20Call(selTransferFrom, addrKey(b20Alice), addrKey(b20Bob), u256hash(40)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("self transferFrom without an approval: err = %v, want a revert", err)
	}
	if len(out) < 4 || [4]byte(out[:4]) != errSelInsufficientAllow {
		t.Errorf("revert = %x, want InsufficientAllowance", out[:min(4, len(out))])
	}

	// With one, it goes through and the allowance decrements — the executor policy
	// authorizes nobody, so reaching the transfer at all is the self shortcut.
	if _, err := send(b20Alice, b20Call(selApprove, addrKey(b20Alice), u256hash(100))); err != nil {
		t.Fatalf("self approve: %v", err)
	}
	if out, err := send(b20Alice, b20Call(selTransferFrom, addrKey(b20Alice), addrKey(b20Bob), u256hash(40))); err != nil ||
		!bytes.Equal(out, encBool(true)) {
		t.Fatalf("self transferFrom with an approval: ret %x err %v", out, err)
	}
	if got := view.allowance(b20Alice, b20Alice).Uint64(); got != 60 {
		t.Errorf("self-allowance = %d, want 60 — it must be spent like any other", got)
	}
	if got := view.balanceOf(b20Bob).Uint64(); got != 40 {
		t.Errorf("bob's balance = %d, want 40", got)
	}

	// And the contrast: a third party with a full allowance is still refused by the
	// executor policy, so the shortcut above is the executor check and nothing else.
	if _, err := send(admin, b20Call(selApprove, addrKey(b20Carol), u256hash(100))); err != nil {
		t.Fatalf("approve carol: %v", err)
	}
	if _, err := send(b20Carol, b20Call(selTransferFrom, addrKey(admin), addrKey(b20Bob), u256hash(1))); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("a delegated transfer under a deny-all executor policy err = %v, want a revert", err)
	}
}
