package vm

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// encodeU64Array encodes a uint64[] argument at the given head position, alone.
func encodeU64Array(sel [4]byte, head []common.Hash, ids ...uint64) []byte {
	out := append([]byte{}, sel[:]...)
	for _, h := range head {
		out = append(out, h.Bytes()...)
	}
	out = append(out, u256hash(uint64(len(head)+1)*32).Bytes()...) // offset past the head
	out = append(out, u256hash(uint64(len(ids))).Bytes()...)
	for _, id := range ids {
		out = append(out, wU64(id).Bytes()...)
	}
	return out
}

// newCompositeFixture opens the registry feature and creates two simple policies:
// an allowlist holding alice and dave, and one holding bob and dave.
func newCompositeFixture(t *testing.T) (*EVM, common.Address, uint64, uint64) {
	t.Helper()
	_, evm := newB20EVM(t)
	admin := b20ActivationAdmin
	// newB20EVM seeds the registries with every feature already activated.
	call := func(to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(admin, to, input, NewGasBudget(9_000_000), uint256.NewInt(0))
		return ret, err
	}
	mk := func(members ...common.Address) uint64 {
		t.Helper()
		ret, err := call(B20PolicyRegistryAddress,
			b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyAllowlist)))
		if err != nil {
			t.Fatalf("createPolicy: %v", err)
		}
		id := new(uint256.Int).SetBytes(ret).Uint64()
		if _, err := call(B20PolicyRegistryAddress,
			encodeUpdateList(selUpdateAllowlist, id, true, members)); err != nil {
			t.Fatalf("updateAllowlist: %v", err)
		}
		return id
	}
	return evm, admin, mk(b20Alice, b20Carol), mk(b20Bob, b20Carol)
}

// TestB20CompositeEvaluatesChildrenLive is the property that makes a composite
// worth having: it stores no membership of its own, so changing a child changes
// the composite's verdict with no call on the composite.
func TestB20CompositeEvaluatesChildrenLive(t *testing.T) {
	evm, admin, childA, childB := newCompositeFixture(t)
	call := func(input []byte) ([]byte, error) {
		ret, _, err := evm.Call(admin, B20PolicyRegistryAddress, input, NewGasBudget(9_000_000), uint256.NewInt(0))
		return ret, err
	}
	mkComposite := func(ptype uint64) uint64 {
		t.Helper()
		ret, err := call(encodeU64Array(selCreateComposite,
			[]common.Hash{addrKey(admin), u256hash(ptype)}, childA, childB))
		if err != nil {
			t.Fatalf("createCompositePolicy(type %d): %v", ptype, err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64()
	}
	authorized := func(id uint64, who common.Address) bool {
		t.Helper()
		ret, err := call(b20Call(selIsAuthorized, wU64(id), addrKey(who)))
		if err != nil {
			t.Fatalf("isAuthorized: %v", err)
		}
		return bytes.Equal(ret, encBool(true))
	}

	union, intersect := mkComposite(b20PolicyUnion), mkComposite(b20PolicyIntersect)

	// childA allows {alice, carol}; childB allows {bob, carol}.
	for _, tc := range []struct {
		who          common.Address
		name         string
		wantU, wantI bool
	}{
		{b20Alice, "in childA only", true, false},
		{b20Bob, "in childB only", true, false},
		{b20Carol, "in both", true, true},
		{common.HexToAddress("0xdead"), "in neither", false, false},
	} {
		if got := authorized(union, tc.who); got != tc.wantU {
			t.Errorf("UNION authorizes %s (%s) = %v, want %v", tc.who.Hex(), tc.name, got, tc.wantU)
		}
		if got := authorized(intersect, tc.who); got != tc.wantI {
			t.Errorf("INTERSECT authorizes %s (%s) = %v, want %v", tc.who.Hex(), tc.name, got, tc.wantI)
		}
	}

	// Mutate a CHILD, not the composite, and both verdicts move.
	if _, err := call(encodeUpdateList(selUpdateAllowlist, childB, true, []common.Address{b20Alice})); err != nil {
		t.Fatalf("adding alice to childB: %v", err)
	}
	if !authorized(intersect, b20Alice) {
		t.Error("alice is now in both children but INTERSECT still refuses her — the " +
			"composite is not reading its children live")
	}
	if _, err := call(encodeUpdateList(selUpdateAllowlist, childA, false, []common.Address{b20Carol})); err != nil {
		t.Fatalf("removing carol from childA: %v", err)
	}
	if authorized(intersect, b20Carol) {
		t.Error("carol was removed from a child but INTERSECT still authorizes her")
	}
	if !authorized(union, b20Carol) {
		t.Error("carol is still in childB, so UNION should authorize her")
	}

	// The child set reads back in order.
	ret, err := call(b20Call(selCompositeChildIds, wU64(union)))
	if err != nil {
		t.Fatalf("compositePolicyChildIds: %v", err)
	}
	want := encodeTuple(abiWordArray([]common.Hash{wU64(childA), wU64(childB)}))
	if !bytes.Equal(ret, want) {
		t.Errorf("child ids = %x, want %x", ret, want)
	}
}

// TestB20CompositeRevertOrder walks base-std's documented orders, which differ
// between the two constructors: createCompositePolicy checks the type first and
// then the admin, while the simple ones check the admin first and then refuse a
// composite type.
func TestB20CompositeRevertOrder(t *testing.T) {
	evm, admin, childA, childB := newCompositeFixture(t)
	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, B20PolicyRegistryAddress, input, NewGasBudget(9_000_000), uint256.NewInt(0))
		return ret, err
	}

	// createCompositePolicy: a simple type with a zero admin reports the type.
	ret, err := call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{{}, u256hash(b20PolicyAllowlist)}, childA, childB))
	wantRevert(t, ret, err, errSelIncompatibleType, "simple type and a zero admin")

	// A zero admin with a valid composite type reports the admin.
	ret, err = call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{{}, u256hash(b20PolicyUnion)}, childA, childB))
	wantRevert(t, ret, err, errSelZeroAddress, "zero admin with a composite type")

	// Too few children outranks a nonexistent one.
	ret, err = call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{addrKey(admin), u256hash(b20PolicyUnion)}, 0xdead))
	wantRevert(t, ret, err, errSelChildrenOutOfRange, "one child, and it does not exist")

	// Then a nonexistent child, ahead of the no-nesting rule.
	ret, err = call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{addrKey(admin), u256hash(b20PolicyUnion)}, childA, 0xdead))
	wantRevert(t, ret, err, errSelPolicyNotFoundID, "a child that does not exist")

	// A composite may not nest, which is what keeps evaluation one level deep.
	ret, err = call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{addrKey(admin), u256hash(b20PolicyUnion)}, childA, childB))
	if err != nil {
		t.Fatalf("a valid composite should be creatable: %v", err)
	}
	nested := new(uint256.Int).SetBytes(ret).Uint64()
	ret, err = call(admin, encodeU64Array(selCreateComposite,
		[]common.Hash{addrKey(admin), u256hash(b20PolicyIntersect)}, childA, nested))
	wantRevert(t, ret, err, errSelInvalidChildPolicy, "a composite as a child")

	// The simple constructors refuse a composite type, after the admin check.
	ret, err = call(admin, b20Call(selCreatePolicy, common.Hash{}, u256hash(b20PolicyUnion)))
	wantRevert(t, ret, err, errSelZeroAddress, "createPolicy with a zero admin and a composite type")
	ret, err = call(admin, b20Call(selCreatePolicy, addrKey(admin), u256hash(b20PolicyUnion)))
	wantRevert(t, ret, err, errSelIncompatibleType, "createPolicy with a composite type")

	// updateComposite: existence, then type, then authorization.
	ret, err = call(admin, encodeU64Array(selUpdateComposite, []common.Hash{wU64(0xdead)}, childA, childB))
	wantRevert(t, ret, err, errSelPolicyNotFoundID, "updating a policy that does not exist")
	ret, err = call(admin, encodeU64Array(selUpdateComposite, []common.Hash{wU64(childA)}, childA, childB))
	wantRevert(t, ret, err, errSelIncompatibleType, "updating a simple policy as a composite")
	ret, err = call(b20Bob, encodeU64Array(selUpdateComposite, []common.Hash{wU64(nested)}, childA, childB))
	wantRevert(t, ret, err, errSelUnauthorized, "a stranger replacing a composite's children")

	// And the admin can, which shows the reverts above were not blanket refusals.
	if _, err := call(admin, encodeU64Array(selUpdateComposite,
		[]common.Hash{wU64(nested)}, childB, childA)); err != nil {
		t.Fatalf("the admin replacing the child set: %v", err)
	}
}

// TestB20CompositeChildStorageLayout pins the child array's encoding, which is
// the port's correctness argument: a Solidity reference contract must produce the
// same slots byte for byte, so the state roots agree and not merely the reads.
func TestB20CompositeChildStorageLayout(t *testing.T) {
	statedb, _ := newB20EVM(t)
	reg := policyReg{s: b20Storage{state: statedb, token: B20PolicyRegistryAddress}}
	const id = uint64(7)

	slot := reg.childrenSlot(id)
	// The data region hangs off the mapping slot by one keccak, as Solidity does.
	if got, want := reg.s.stringDataRoot(slot), new(uint256.Int).SetBytes(
		crypto.Keccak256(slot.Bytes())); got.Cmp(want) != 0 {
		t.Errorf("data root = %s, want keccak256(slot) = %s", got, want)
	}
	word := func(i uint64) common.Hash {
		return statedb.GetState(B20PolicyRegistryAddress,
			common.Hash(new(uint256.Int).AddUint64(reg.s.stringDataRoot(slot), i).Bytes32()))
	}

	reg.setChildren(id, []uint64{0x11, 0x22, 0x33, 0x44})
	if got := new(uint256.Int).SetBytes(statedb.GetState(B20PolicyRegistryAddress, slot).Bytes()).Uint64(); got != 4 {
		t.Errorf("length word = %d, want 4", got)
	}
	// Four lanes LSB-first: the first element occupies the low bytes.
	wantPacked := "0x0000000000000044000000000000003300000000000000220000000000000011"
	if got := word(0).Hex(); got != wantPacked {
		t.Errorf("packed word = %s, want %s (four uint64 lanes, LSB-first)", got, wantPacked)
	}

	// Shrinking must zero the lanes it abandons, not merely shorten the length.
	reg.setChildren(id, []uint64{0xaa, 0xbb})
	wantShrunk := "0x0000000000000000000000000000000000000000000000bb00000000000000aa"
	if got := word(0).Hex(); got != wantShrunk {
		t.Errorf("after shrinking to 2, word = %s, want %s — the abandoned lanes are "+
			"still set, so the state root diverges from a reference contract", got, wantShrunk)
	}
	if got := reg.children(id); len(got) != 2 || got[0] != 0xaa || got[1] != 0xbb {
		t.Errorf("children = %v, want [aa bb]", got)
	}

	// And the cap keeps the array inside one word today, which is why the tail
	// clearing above has nothing to do. If the cap rises, it starts mattering.
	if words := (b20CompositeMaxChildren + 3) / 4; words != 1 {
		t.Errorf("a maximal child set spans %d words; the shrink path now has a real "+
			"tail to clear and needs a test that exercises more than one word", words)
	}
}
