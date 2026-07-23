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
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// encodeBatchMint ABI-encodes batchMint(address[],uint256[]).
func encodeBatchMint(recips []common.Address, amounts []uint64) []byte {
	out := append([]byte{}, selBatchMint[:]...)
	out = append(out, u256hash(0x40).Bytes()...)                            // offset arr1
	out = append(out, u256hash(uint64(0x40+(1+len(recips))*32)).Bytes()...) // offset arr2
	out = append(out, u256hash(uint64(len(recips))).Bytes()...)
	for _, r := range recips {
		out = append(out, addrKey(r).Bytes()...)
	}
	out = append(out, u256hash(uint64(len(amounts))).Bytes()...)
	for _, a := range amounts {
		out = append(out, u256hash(a).Bytes()...)
	}
	return out
}

func TestB20AssetExtension(t *testing.T) {
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
	evm := NewEVM(bc, statedb, &cfg, Config{})

	creator := common.HexToAddress("0xc4ea70")
	minter := common.HexToAddress("0x33333")
	operator := common.HexToAddress("0x09e4a704")
	salt := common.HexToHash("0x0a")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	u := func(ret []byte, err error) uint64 {
		t.Helper()
		if err != nil {
			t.Fatalf("call err: %v", err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64()
	}

	// create an Asset token: minter=MINT, operator=OPERATOR, mint 1000 to alice.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(minter)),
		b20Call(selGrantRole, roleOperator, addrKey(operator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// defaults from extension storage.
	if got := u(call(creator, token, b20Call(selDecimals))); got != 18 {
		t.Errorf("decimals = %d, want 18", got)
	}
	if got := u(call(creator, token, b20Call(selMultiplier))); got != 1e18 {
		t.Errorf("multiplier = %d, want 1e18", got)
	}
	if got := u(call(creator, token, b20Call(selWadPrecision))); got != 1e18 {
		t.Errorf("WAD_PRECISION = %d, want 1e18", got)
	}
	if got := u(call(creator, token, b20Call(selScaledBalanceOf, addrKey(b20Alice)))); got != 1000 {
		t.Errorf("scaledBalanceOf(alice) = %d, want 1000", got)
	}

	// non-operator updateMultiplier reverts.
	if _, err := call(minter, token, b20Call(selUpdateMultiplier, u256hash(2e18))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("non-operator updateMultiplier err = %v, want revert", err)
	}
	// operator sets multiplier to 1.5x.
	if _, err := call(operator, token, b20Call(selUpdateMultiplier, u256hash(1_500_000_000_000_000_000))); err != nil {
		t.Fatalf("updateMultiplier: %v", err)
	}
	if got := u(call(creator, token, b20Call(selMultiplier))); got != 1_500_000_000_000_000_000 {
		t.Errorf("multiplier = %d, want 1.5e18", got)
	}

	// scaled views reflect the multiplier; raw balance is unchanged.
	if got := u(call(creator, token, b20Call(selScaledBalanceOf, addrKey(b20Alice)))); got != 1500 {
		t.Errorf("scaledBalanceOf(alice) = %d, want 1500", got)
	}
	if got := u(call(creator, token, b20Call(selToScaledBalance, u256hash(1000)))); got != 1500 {
		t.Errorf("toScaledBalance(1000) = %d, want 1500", got)
	}
	if got := u(call(creator, token, b20Call(selToRawBalance, u256hash(1500)))); got != 1000 {
		t.Errorf("toRawBalance(1500) = %d, want 1000", got)
	}
	if got := u(call(creator, token, b20Call(selBalanceOf, addrKey(b20Alice)))); got != 1000 {
		t.Errorf("balanceOf(alice) = %d, want 1000 (raw unchanged)", got)
	}
	// updateMultiplier to 0 reverts.
	if _, err := call(operator, token, b20Call(selUpdateMultiplier, u256hash(0))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("zero multiplier err = %v, want revert", err)
	}

	// batchMint to bob and carol.
	if _, err := call(minter, token, encodeBatchMint([]common.Address{b20Bob, b20Carol}, []uint64{10, 20})); err != nil {
		t.Fatalf("batchMint: %v", err)
	}
	view := newB20Storage(statedb, token)
	if view.balanceOf(b20Bob).Uint64() != 10 || view.balanceOf(b20Carol).Uint64() != 20 {
		t.Errorf("batchMint balances bob %d carol %d, want 10/20", view.balanceOf(b20Bob).Uint64(), view.balanceOf(b20Carol).Uint64())
	}
	if view.totalSupply().Uint64() != 1030 {
		t.Errorf("supply = %d, want 1030", view.totalSupply().Uint64())
	}
	// mismatched array lengths revert.
	if _, err := call(minter, token, encodeBatchMint([]common.Address{b20Bob}, []uint64{1, 2})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("length-mismatch batchMint err = %v, want revert", err)
	}
}
