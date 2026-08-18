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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Which accounts a token reads is about to stop being an implementation detail.
// EIP-7928 puts the block's access list in the header, and BSC schedules it at
// Amsterdam. B20 activates on its own fork rather than that one (see
// b20Enabled), but the two will coexist: once BAL is live, every account a token
// reads is consensus data, and a chain-wide singleton read by every transfer is
// also what serialises parallel execution.
//
// The PolicyRegistry is the only account a token reads besides its own. The tests
// below pin when that read happens and when it does not.
//
// They measure charged state gas rather than the transaction access list. The two
// disagree on any path that reverts: access-list entries are journaled and undone
// (see accessListAddSlotChange), while the read set EIP-7928 collects is not. A
// read on a reverting path is therefore invisible to the access list and visible
// to the BAL — which is exactly why revert paths must not read what they do not
// need.

// policyReadGas reports the state gas a single policy check charges, which is
// zero when the check answers without reaching the registry.
func policyReadGas(t *testing.T, seed func(policyReg), id uint64, account common.Address) uint64 {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(policyReg{s: newUnmeteredB20Storage(statedb, B20PolicyRegistryAddress)})
	}
	gas := NewGasBudget(5_000_000)
	ctx := &PrecompileContext{
		StateDB: statedb, Self: b20Addr(b20VariantAsset, 1),
		Caller: account, DirectCall: true, gas: &gas,
	}
	before := ctx.stateGasUsed()
	newB20Token(ctx, 18).policyAllows(id, account)
	return ctx.stateGasUsed() - before
}

// TestB20SentinelScopesReadNoRegistry pins that both sentinels answer from the id
// alone. That covers the two commonest configurations — an unconfigured token and
// a fully blocked one — and keeps their transfers inside a single account.
func TestB20SentinelScopesReadNoRegistry(t *testing.T) {
	if got := policyReadGas(t, nil, b20PolicyAlwaysAllow, b20Alice); got != 0 {
		t.Errorf("ALWAYS_ALLOW charged %d state gas, want 0 — it reached the registry", got)
	}
	if got := policyReadGas(t, nil, b20PolicyAlwaysBlock, b20Alice); got != 0 {
		t.Errorf("ALWAYS_BLOCK charged %d state gas, want 0 — the sentinel fast path is not short-circuiting", got)
	}
	// A malformed id is refused from its type byte, so it reads nothing either.
	if got := policyReadGas(t, nil, uint64(0x7f)<<56, b20Alice); got != 0 {
		t.Errorf("a malformed policy id charged %d state gas, want 0", got)
	}
}

// TestB20RealPolicyReadsRegistry is the other half: a scope bound to a created
// policy genuinely reads the shared registry. Without this the absence above
// could just mean the measurement cannot see a read.
func TestB20RealPolicyReadsRegistry(t *testing.T) {
	pid := uint64(b20PolicyBlocklist)<<56 | 2
	seed := func(p policyReg) {
		p.setPolicyAdmin(pid, common.HexToAddress("0xad4149"))
		p.setMember(pid, b20Bob, true)
	}
	got := policyReadGas(t, seed, pid, b20Alice)
	if got == 0 {
		t.Fatal("a real policy charged no state gas — it did not read the registry")
	}
	// One membership lookup: a nested mapping, so two 64-byte keccaks, then a
	// cold read of the derived slot.
	keccak64 := params.Keccak256Gas + 2*params.Keccak256WordGas
	if want := 2*keccak64 + params.ColdSloadCostEIP2929; got != want {
		t.Errorf("real policy charged %d state gas, want %d (2 mapping derivations + a cold read)", got, want)
	}
}

// TestB20TokenStorageIsPerToken pins that a token's own state lives under its own
// address. Two tokens sharing an account would contend under parallel execution
// no matter what their policies said, and would appear as one account in the
// block access list.
func TestB20TokenStorageIsPerToken(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	create := func(salt common.Hash) common.Address {
		init := [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
		}
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20(b20VariantAsset, salt, creator, init), NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("createB20: %v", err)
		}
		return common.BytesToAddress(ret)
	}
	a, b := create(common.HexToHash("0x1")), create(common.HexToHash("0x2"))
	if a == b {
		t.Fatal("two salts produced one address")
	}

	// Moving balance on one token must leave the other's balance slot untouched:
	// the same slot number under a different account.
	if _, _, err := evm.Call(b20Alice, a, b20Call(selTransfer, addrKey(b20Bob), u256hash(100)),
		NewGasBudget(1_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if got := newUnmeteredB20Storage(statedb, a).balanceOf(b20Bob).Uint64(); got != 100 {
		t.Errorf("token A credited %d, want 100", got)
	}
	if got := newUnmeteredB20Storage(statedb, b).balanceOf(b20Bob).Uint64(); got != 0 {
		t.Errorf("token B saw %d — the two tokens share storage", got)
	}
}
