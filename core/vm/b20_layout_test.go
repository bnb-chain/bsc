package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Storage-layout pins. Expectations are spelled out from Solidity's rules, not
// obtained from the accessor under test: a test that asks mapSlot where a slot
// lives agrees with mapSlot whatever mapSlot does, which is why each case below
// could be changed in reader and writer together without failing anything.

// solMap is Solidity's mapping slot: keccak256(h(key) . slot), with the key
// left-padded to a word for value types.
func solMap(slot common.Hash, key common.Hash) common.Hash {
	return crypto.Keccak256Hash(key.Bytes(), slot.Bytes())
}

// solSlot is base + offset, as Solidity numbers consecutive fields.
func solSlot(base common.Hash, offset uint64) common.Hash {
	s := new(uint256.Int).SetBytes(base.Bytes())
	s.AddUint64(s, offset)
	return common.Hash(s.Bytes32())
}

// TestB20PolicyMemberSlot pins members[id][account] at
// keccak256(account . keccak256(id . slot)) — id first. The token's own nested
// mappings are covered against this; the registry's was not.
func TestB20PolicyMemberSlot(t *testing.T) {
	s := newTestStorage(t)
	reg := policyReg{s: s}

	const id = uint64(0x0100000000002a) // allowlist type, counter 42
	account := common.HexToAddress("0xbeef")

	reg.setMember(id, account, true)

	inner := solMap(solSlot(b20PolicyRoot, polSlotMembers), idKey(id))
	want := solMap(inner, addrKey(account))
	if got := s.getWord(want); got == (common.Hash{}) {
		t.Errorf("members[%#x][%s] is not at %s", id, account.Hex(), want.Hex())
	}

	// The swapped order must be empty, which is what makes this an assertion
	// about ordering rather than about reachability.
	swappedInner := solMap(solSlot(b20PolicyRoot, polSlotMembers), addrKey(account))
	if got := s.getWord(solMap(swappedInner, idKey(id))); got != (common.Hash{}) {
		t.Error("the account-then-id ordering also holds a value; the keys are interchangeable")
	}
}

// TestB20StringKeyedSlots pins both string-keyed mappings: a dynamic key is
// hashed unpadded, keccak256(bytes(key) . slot). Padding it to a word instead —
// the rule for value-typed keys — would put every entry somewhere else.
func TestB20StringKeyedSlots(t *testing.T) {
	ext := assetExt{s: newUnmeteredB20Storage(nil, common.Address{})}
	for _, tc := range []struct {
		name string
		slot uint64
		got  func(string) common.Hash
	}{
		{"extraMetadata", b20AssetSlotExtraMeta, ext.extraMetaSlot},
		{"announcements", b20AssetSlotAnnouncements, ext.announcementSlot},
	} {
		const key = "category"
		base := solSlot(b20AssetRoot, tc.slot)
		if want, got := crypto.Keccak256Hash([]byte(key), base.Bytes()), tc.got(key); got != want {
			t.Errorf("%s slot for %q = %s, want keccak256(key . slot) = %s",
				tc.name, key, got.Hex(), want.Hex())
		}
		// And the reversed preimage must differ, or the assertion above is vacuous.
		if reversed := crypto.Keccak256Hash(base.Bytes(), []byte(key)); tc.got(key) == reversed {
			t.Errorf("%s: slot . key and key . slot agree; the order is not pinned", tc.name)
		}
	}

	// The two mappings must not answer the same slot for the same key, which is
	// what keeps a metadata key from marking an announcement id used.
	if ext.extraMetaSlot("x") == ext.announcementSlot("x") {
		t.Error("extraMetadata and announcements collide on a shared key")
	}
}

// TestB20ComplianceLanes pins which slot each compliance id lands in and where
// inside it. Swapping the seize holder and receiver offsets would apply the
// wrong policy to a seizure; putting a seize id back in the mint slot would
// take a lane reserved for future mint-side policy types.
func TestB20ComplianceLanes(t *testing.T) {
	s := newTestStorage(t)
	const (
		mintRecv  = uint64(0x11)
		seizeHold = uint64(0x22)
		seizeRecv = uint64(0x33)
	)
	s.setMintReceiverPolicy(mintRecv)
	s.setSeizeHolderPolicy(seizeHold)
	s.setSeizeReceiverPolicy(seizeRecv)

	// Distinct values, so a swapped pair cannot pass by coincidence.
	if mintRecv == seizeHold || seizeHold == seizeRecv || mintRecv == seizeRecv {
		t.Fatal("the lane values must differ for a swap to be detectable")
	}

	// Raw words, read straight from their slots, with each lane at its own byte
	// offset from the low end.
	lane := func(slot uint64, off int) uint64 {
		word := s.getU256At(slotAt(slot)).Bytes32()
		// bytes are big-endian in the word, so lane n occupies [31-off-7 : 31-off]
		var got uint64
		for _, b := range word[32-off-8 : 32-off] {
			got = got<<8 | uint64(b)
		}
		return got
	}
	for _, tc := range []struct {
		name string
		slot uint64
		off  int
		want uint64
	}{
		{"mintReceiver", b20SlotMintPolicy, 0, mintRecv},
		{"seizeHolder", b20SlotSeizePolicies, 0, seizeHold},
		{"seizeReceiver", b20SlotSeizePolicies, 8, seizeRecv},
	} {
		if got := lane(tc.slot, tc.off); got != tc.want {
			t.Errorf("%s at slot %d byte offset %d = %#x, want %#x",
				tc.name, tc.slot, tc.off, got, tc.want)
		}
	}

	// And the mint slot holds nothing but its one id: the rest of that word is
	// reserved for mint-side policy types a later revision adds, so a seize id
	// straying back into it is a divergence even though both still read back.
	if word := s.getU256At(slotAt(b20SlotMintPolicy)).Bytes32(); word != mintOnly(mintRecv) {
		t.Errorf("mint slot = %s, want only the receiver id in the low lane",
			common.Hash(word).Hex())
	}
}

// mintOnly is the mint slot's expected word: one id in the low lane, the rest
// zero.
func mintOnly(id uint64) [32]byte {
	var w [32]byte
	for i := 0; i < 8; i++ {
		w[31-i] = byte(id >> (8 * i))
	}
	return w
}

// TestB20SlotNumbers pins every field's slot number as a literal. The accessors
// agree with themselves whatever the numbers say, so without this a renumbering
// is invisible. Slots are consensus constants and append-only.
func TestB20SlotNumbers(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uint64
		want uint64
	}{
		{"core.name", b20SlotName, 0},
		{"core.symbol", b20SlotSymbol, 1},
		{"core.contractURI", b20SlotContractURI, 2},
		{"core.totalSupply", b20SlotTotalSupply, 3},
		{"core.balances", b20SlotBalances, 4},
		{"core.allowances", b20SlotAllowances, 5},
		{"core.roles", b20SlotRoles, 6},
		{"core.roleAdmins", b20SlotRoleAdmins, 7},
		{"core.adminCount", b20SlotAdminCount, 8},
		{"core.transferPolicies", b20SlotTransferPolicies, 9},
		{"core.mintPolicy", b20SlotMintPolicy, 10},
		{"core.paused", b20SlotPaused, 11},
		{"core.supplyCap", b20SlotSupplyCap, 12},
		{"core.nonces", b20SlotNonces, 13},
		{"core.seizePolicies", b20SlotSeizePolicies, 14},

		{"activation.features", actSlotFeatures, 0},
		{"activation.admin", actSlotAdmin, 1},

		{"policy.policies", polSlotPolicies, 0},
		{"policy.members", polSlotMembers, 1},
		{"policy.pendingAdmins", polSlotPendingAdmins, 2},
		{"policy.counter", polSlotCounter, 3},

		{"asset.decimals", b20AssetSlotDecimals, 0},
		{"asset.multiplier", b20AssetSlotMultiplier, 1},
		{"asset.announcements", b20AssetSlotAnnouncements, 2},
		{"asset.extraMeta", b20AssetSlotExtraMeta, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	// The packed lane offsets too, since a swap there is invisible to the
	// accessors.
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"transferSender", b20OffTransferSender, 0},
		{"transferReceiver", b20OffTransferReceiver, 8},
		{"transferExecutor", b20OffTransferExecutor, 16},
		{"mintReceiver", b20OffMintReceiver, 0},
		{"seizeHolder", b20OffSeizeHolder, 0},
		{"seizeReceiver", b20OffSeizeReceiver, 8},
	} {
		if tc.got != tc.want {
			t.Errorf("offset %s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestB20PolicyWordReservedBits pins that byte 0 is exactly 0x80. Bits 254:248
// were unchecked, and they are where a later revision would add a flag.
func TestB20PolicyWordReservedBits(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	w := packPolicy(admin)

	if w[0] != 0x80 {
		t.Errorf("byte 0 = %#02x, want exactly 0x80: bit 255 set, bits 254:248 clear", w[0])
	}
	for i := 1; i < 12; i++ {
		if w[i] != 0 {
			t.Errorf("byte %d = %#02x, want zero (bits 247:160 are reserved)", i, w[i])
		}
	}
	if got := polWordAdmin(w); got != admin {
		t.Errorf("admin round-trip = %s, want %s", got.Hex(), admin.Hex())
	}
	if !polWordExists(w) {
		t.Error("packPolicy must set the existence bit")
	}
}

// TestB20PolicyLanePositions checks every entry of b20PolicyLanes against the byte
// the id actually lands on, for all six scopes.
func TestB20PolicyLanePositions(t *testing.T) {
	if len(b20PolicyLanes) != 6 {
		t.Fatalf("b20PolicyLanes has %d entries, want the six scopes", len(b20PolicyLanes))
	}
	tok := b20Token{s: newTestStorage(t)}

	// A distinct id per scope, so no pair can pass by coincidence.
	ids := map[common.Hash]uint64{
		scopeTransferSender:   0x11,
		scopeTransferReceiver: 0x22,
		scopeTransferExecutor: 0x33,
		scopeMintReceiver:     0x44,
		scopeSeizeHolder:      0x55,
		scopeSeizeReceiver:    0x66,
	}
	if len(ids) != len(b20PolicyLanes) {
		t.Fatalf("this test names %d scopes, the table has %d", len(ids), len(b20PolicyLanes))
	}
	seen := map[uint64]common.Hash{}
	for scope, lane := range b20PolicyLanes {
		id, named := ids[scope]
		if !named {
			t.Fatalf("the table holds a scope this test does not name: %s", scope.Hex())
		}
		tok.s.setPackedU64(lane.slot, lane.byteOff, id)
		// Two scopes on the same byte of the same slot would overwrite each other.
		key := lane.slot<<8 | uint64(lane.byteOff)
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s share slot %d byte %d", scope.Hex(), other.Hex(),
				lane.slot, lane.byteOff)
		}
		seen[key] = scope
	}

	// Read back through the dispatcher's own path, then straight from the word, so
	// a table entry that is wrong in a self-consistent way still fails.
	for scope, want := range ids {
		got, ok := tok.policyIdByScope(scope)
		if !ok {
			t.Errorf("policyIdByScope(%s) reports unknown", scope.Hex())
			continue
		}
		if got != want {
			t.Errorf("policyIdByScope(%s) = %#x, want %#x", scope.Hex(), got, want)
		}
		lane := b20PolicyLanes[scope]
		word := tok.s.getU256At(slotAt(lane.slot)).Bytes32()
		var raw uint64
		for _, b := range word[32-int(lane.byteOff)-8 : 32-int(lane.byteOff)] {
			raw = raw<<8 | uint64(b)
		}
		if raw != want {
			t.Errorf("%s: slot %d byte %d holds %#x, want %#x", scope.Hex(),
				lane.slot, lane.byteOff, raw, want)
		}
	}
}
