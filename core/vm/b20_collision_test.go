package vm

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20SlotsNeverCollide walks the whole slot space B20 addresses and asserts
// no two distinct locations land on the same slot.
//
// Every field of every namespace, and every mapping entry, is derived by
// arithmetic or keccak over a shared root. A collision would alias two pieces of
// state — a balance over a role, one namespace over another — with no symptom
// except wrong answers, and the per-field tests cannot see it because each checks
// its own location in isolation.
//
// The five roots are 32 bytes apart at most by luck; what makes them safe is
// ERC-7201's low-byte clearing, which leaves 255 free slots under each. Fixed
// fields use only the low numbers, so the check that matters is that no fixed
// field of one namespace reaches into another's, and that no mapping entry lands
// on a fixed field.
func TestB20SlotsNeverCollide(t *testing.T) {
	seen := map[common.Hash]string{}
	claim := func(slot common.Hash, what string) {
		if prev, ok := seen[slot]; ok {
			t.Errorf("slot %s is both %s and %s", slot.Hex(), prev, what)
			return
		}
		seen[slot] = what
	}

	// Fixed fields of every namespace, well past the numbers in use, so an
	// off-by-many renumbering still shows up as a collision rather than silently
	// working.
	roots := []struct {
		name string
		root common.Hash
	}{
		{"core", b20CoreRoot},
		{"asset", b20AssetRoot},
		{"stablecoin", b20StablecoinRoot},
		{"activation", b20ActivationRoot},
		{"policy", b20PolicyRoot},
	}
	for _, r := range roots {
		for off := uint64(0); off < 64; off++ {
			claim(offsetSlot(r.root, off), fmt.Sprintf("%s field %d", r.name, off))
		}
	}

	// Mapping entries: the same keys under every mapping-bearing field of every
	// namespace. Keys are chosen to include the boundary values a packing bug
	// would confuse — zero, one, the low and high halves set.
	keys := []common.Hash{
		{},
		{31: 1},
		{0: 1},
		addrKey(common.HexToAddress("0x0")),
		addrKey(common.HexToAddress("0x1")),
		addrKey(b20Alice),
		addrKey(b20Bob),
		u256hash(0), u256hash(1), u256hash(2),
		idKey(0), idKey(1), idKey(1<<56 | 1),
		common.Hash(uint256.NewInt(0).Not(uint256.NewInt(0)).Bytes32()),
		featureB20Asset, featureB20Stablecoin, featurePolicyRegistry,
		roleDefaultAdmin, roleMint, roleOperator,
	}
	// Several of the constructions above name the same word — bytes32(0) is the
	// zero address, u256(0), policy id 0 and DEFAULT_ADMIN_ROLE alike — so dedupe
	// first. Without this the check reports its own duplicates as collisions.
	seenKey := map[common.Hash]bool{}
	uniq := keys[:0:0]
	for _, k := range keys {
		if !seenKey[k] {
			seenKey[k] = true
			uniq = append(uniq, k)
		}
	}
	keys = uniq

	for _, r := range roots {
		for off := uint64(0); off < 8; off++ {
			base := offsetSlot(r.root, off)
			for ki, k := range keys {
				claim(mappingSlot(base, k), fmt.Sprintf("%s field %d mapping key #%d", r.name, off, ki))
			}
		}
	}

	// Nested mappings, where the inner base is itself a derived slot: allowances
	// and roles on the token, members on the registry.
	for _, outer := range []struct {
		name string
		base common.Hash
	}{
		{"core allowances", slotAt(b20SlotAllowances)},
		{"core roles", slotAt(b20SlotRoles)},
		{"policy members", offsetSlot(b20PolicyRoot, polSlotMembers)},
	} {
		for i, a := range keys[:8] {
			inner := mappingSlot(outer.base, a)
			for j, b := range keys[:8] {
				claim(mappingSlot(inner, b), fmt.Sprintf("%s[%d][%d]", outer.name, i, j))
			}
		}
	}

	// Long-string data regions, which hang off a length slot by another keccak.
	view := newUnmeteredB20Storage(nil, common.Address{})
	for _, r := range roots {
		for off := uint64(0); off < 4; off++ {
			root := common.Hash(view.stringDataRoot(offsetSlot(r.root, off)).Bytes32())
			for chunk := uint64(0); chunk < 4; chunk++ {
				claim(offsetSlot(root, chunk),
					fmt.Sprintf("%s field %d string chunk %d", r.name, off, chunk))
			}
		}
	}

	t.Logf("%d distinct slots, no collisions", len(seen))
}
