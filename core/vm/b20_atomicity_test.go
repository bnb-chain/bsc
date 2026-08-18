package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20CreateIsAtomic pins that a createB20 whose bootstrap fails leaves
// nothing behind. createB20 takes no snapshot of its own and relies on the one
// EVM.Call holds; the failure lands partway through, so a rollback that missed
// any of it would leave a half-built token at a deterministic address that
// b20AddressOccupied would then refuse to create properly.
//
// Asserts on the state root, not hand-picked slots, which would miss whichever
// slot a regression happened to write.
func TestB20CreateIsAtomic(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	minter := common.HexToAddress("0x33333")
	salt := common.HexToHash("0xa70m1c")

	addr := b20DeriveAddress(b20VariantAsset, creator, salt)
	before := statedb.IntermediateRoot(false)

	// A bundle whose earlier entries mutate and whose last one fails. Granting a
	// role and minting both write; the trailing entry is an unknown selector, so
	// the token's dispatch refuses it and the whole announcement unwinds.
	bundle := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(minter)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
		{0xde, 0xad, 0xbe, 0xef},
	}
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, salt, creator, bundle),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err == nil {
		t.Fatalf("createB20 with a failing init call returned %x, want a revert", ret)
	}

	if after := statedb.IntermediateRoot(false); after != before {
		t.Errorf("state root changed across a reverted createB20: %s -> %s", before.Hex(), after.Hex())
	}
	if code := statedb.GetCode(addr); len(code) != 0 {
		t.Errorf("code left at %s: %x — the sentinel survived the revert", addr.Hex(), code)
	}
	if view := newUnmeteredB20Storage(statedb, addr); view.totalSupply().Sign() != 0 {
		t.Errorf("totalSupply left at %s: %s", addr.Hex(), view.totalSupply())
	}
	if view := newUnmeteredB20Storage(statedb, addr); view.hasRole(roleMint, minter) {
		t.Error("the granted MINT_ROLE survived the revert")
	}

	// And the address must be free again, or the real token could never be
	// created at the salt its issuer published.
	if _, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, salt, creator, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("re-creating at the same salt after a failed bootstrap: %v", err)
	}
	if code := statedb.GetCode(addr); len(code) != 1 || code[0] != 0xEF {
		t.Errorf("code after the successful creation = %x, want the sentinel", code)
	}
}
