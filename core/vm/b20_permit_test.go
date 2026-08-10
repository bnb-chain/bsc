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
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// newTokenWithEVM builds a token bound to a real EVM so ChainID()/BlockTime()
// resolve (permit needs both). Any caller can submit; the seed callback runs
// against the unmetered view.
func newTokenWithEVM(t *testing.T, now uint64, seed func(b20Storage)) (*state.StateDB, common.Address, func(caller common.Address, input []byte) ([]byte, error)) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	token := b20Addr(b20VariantAsset, 1)
	if seed != nil {
		seed(newB20Storage(statedb, token))
	}
	bc := BlockContext{
		Random:      &common.Hash{}, // post-merge rules, so IsAmsterdam resolves
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        now,
	}
	evm := NewEVM(bc, statedb, b20TestChainConfig(), Config{})
	run := func(caller common.Address, input []byte) ([]byte, error) {
		gas := NewGasBudget(2_000_000)
		ctx := &PrecompileContext{evm: evm, StateDB: statedb, Self: token, Caller: caller, DirectCall: true, gas: &gas}
		return newB20Token(ctx, 18).dispatch(input)
	}
	return statedb, token, run
}

func TestB20Permit(t *testing.T) {
	const now = 100
	statedb, token, run := newTokenWithEVM(t, now, func(s b20Storage) {
		s.setName("Test Token")
	})
	view := newB20Storage(statedb, token)

	// Build the token used only to compute the domain separator (same EVM cfg).
	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: now}, statedb, b20TestChainConfig(), Config{})
	gas := NewGasBudget(1)
	domTok := newB20Token(&PrecompileContext{evm: evm, StateDB: statedb, Self: token, gas: &gas}, 18)

	sign := func(key *ecdsa.PrivateKey, owner, spender common.Address, value, deadline, nonce uint64) (byte, common.Hash, common.Hash) {
		sh := append([]byte{}, b20PermitTypehash.Bytes()...)
		sh = append(sh, addrKey(owner).Bytes()...)
		sh = append(sh, addrKey(spender).Bytes()...)
		sh = append(sh, u256hash(value).Bytes()...)
		sh = append(sh, u256hash(nonce).Bytes()...)
		sh = append(sh, u256hash(deadline).Bytes()...)
		digest := crypto.Keccak256([]byte{0x19, 0x01}, domTok.domainSeparator().Bytes(), crypto.Keccak256(sh))
		sig, err := crypto.Sign(digest, key)
		if err != nil {
			t.Fatal(err)
		}
		return sig[64] + 27, common.BytesToHash(sig[0:32]), common.BytesToHash(sig[32:64])
	}
	permitCall := func(owner, spender common.Address, value, deadline uint64, v byte, r, s common.Hash) []byte {
		return b20Call(selPermit, addrKey(owner), addrKey(spender), u256hash(value), u256hash(deadline), u256hash(uint64(v)), r, s)
	}

	key, _ := crypto.GenerateKey()
	owner := crypto.PubkeyToAddress(key.PublicKey)
	spender := b20Carol
	relayer := b20Bob // anyone may submit

	// happy path: valid signature sets the allowance and bumps the nonce.
	v, r, s := sign(key, owner, spender, 777, 200, 0)
	if ret, err := run(relayer, permitCall(owner, spender, 777, 200, v, r, s)); err != nil || !bytes.Equal(ret, encBool(true)) {
		t.Fatalf("permit ret %x err %v", ret, err)
	}
	if got := view.allowance(owner, spender).Uint64(); got != 777 {
		t.Fatalf("allowance = %d, want 777", got)
	}
	if got := view.nonce(owner).Uint64(); got != 1 {
		t.Fatalf("nonce = %d, want 1", got)
	}

	// replay of the same signature now fails (nonce consumed).
	if _, err := run(relayer, permitCall(owner, spender, 777, 200, v, r, s)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("replay err = %v, want revert", err)
	}

	// expired deadline reverts.
	v, r, s = sign(key, owner, spender, 1, 50, 1) // deadline 50 < now 100
	if _, err := run(relayer, permitCall(owner, spender, 1, 50, v, r, s)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("expired err = %v, want revert", err)
	}

	// signature by a different key (claiming owner) reverts.
	other, _ := crypto.GenerateKey()
	v, r, s = sign(other, owner, spender, 5, 200, 1)
	if _, err := run(relayer, permitCall(owner, spender, 5, 200, v, r, s)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("wrong-signer err = %v, want revert", err)
	}

	// DOMAIN_SEPARATOR() and nonces() views.
	if ret, err := run(relayer, b20Call(selDomainSeparator)); err != nil || !bytes.Equal(ret, domTok.domainSeparator().Bytes()) {
		t.Fatalf("DOMAIN_SEPARATOR mismatch: %x err %v", ret, err)
	}
	if ret, _ := run(relayer, b20Call(selNonces, addrKey(owner))); new(uint256.Int).SetBytes(ret).Uint64() != 1 {
		t.Fatalf("nonces(owner) = %x, want 1", ret)
	}
}

func TestB20Memo(t *testing.T) {
	statedb, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setBalance(b20Alice, uint256.NewInt(1000))
	})
	txHash := common.HexToHash("0xabc")
	statedb.SetTxContext(txHash, 0)

	memo := common.HexToHash("0xdeadbeef")
	ret, err := run(b20Alice, b20Call(selTransferWithMemo, addrKey(b20Bob), u256hash(100), memo))
	if err != nil || !bytes.Equal(ret, encBool(true)) {
		t.Fatalf("transferWithMemo ret %x err %v", ret, err)
	}

	logs := statedb.GetLogs(txHash, 1, common.Hash{}, 1)
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2 (Transfer + Memo)", len(logs))
	}
	if logs[0].Topics[0] != b20TopicTransfer {
		t.Errorf("first log should be Transfer, got %s", logs[0].Topics[0].Hex())
	}
	if logs[1].Topics[0] != b20TopicMemo || logs[1].Topics[1] != addrKey(b20Alice) || logs[1].Topics[2] != memo {
		t.Errorf("second log should be Memo(alice, memo)")
	}
}
