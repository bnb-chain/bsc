package vm

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
