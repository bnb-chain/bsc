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

// TestB20ExtraMetadataSlot pins mapping(string => string): a dynamic key is
// hashed unpadded, keccak256(bytes(key) . slot).
func TestB20ExtraMetadataSlot(t *testing.T) {
	const key = "category"
	want := crypto.Keccak256Hash([]byte(key), solSlot(b20AssetRoot, b20AssetSlotExtraMeta).Bytes())
	if got := extraMetaSlot(key); got != want {
		t.Errorf("extraMetaSlot(%q) = %s, want keccak256(key . slot) = %s", key, got.Hex(), want.Hex())
	}

	// And the reversed preimage must differ, or the assertion above is vacuous.
	reversed := crypto.Keccak256Hash(solSlot(b20AssetRoot, b20AssetSlotExtraMeta).Bytes(), []byte(key))
	if got := extraMetaSlot(key); got == reversed {
		t.Error("slot . key and key . slot agree; the concatenation order is not pinned")
	}
}

// TestB20ComplianceLanes pins the three lanes packed into core slot 10. Slot 9's
// transfer lanes are covered elsewhere; swapping slot 10's holder and receiver
// offsets would apply the wrong policy to a seizure.
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

	// One raw word, read straight from the slot, with each lane at its own byte
	// offset from the low end.
	word := s.getU256At(slotAt(b20SlotMintPolicy)).Bytes32()
	for _, tc := range []struct {
		name string
		off  int
		want uint64
	}{
		{"mintReceiver", 0, mintRecv},
		{"seizeHolder", 8, seizeHold},
		{"seizeReceiver", 16, seizeRecv},
	} {
		// bytes are big-endian in the word, so lane n occupies [31-off-7 : 31-off]
		lo := 32 - tc.off - 8
		var got uint64
		for _, b := range word[lo : lo+8] {
			got = got<<8 | uint64(b)
		}
		if got != tc.want {
			t.Errorf("%s lane at byte offset %d = %#x, want %#x (raw word %s)", tc.name, tc.off, got, tc.want, common.Hash(word).Hex())
		}
	}

	// Distinct values, so a swapped pair cannot pass by coincidence.
	if mintRecv == seizeHold || seizeHold == seizeRecv {
		t.Fatal("the lane values must differ for the swap to be detectable")
	}
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
		{"seizeHolder", b20OffSeizeHolder, 8},
		{"seizeReceiver", b20OffSeizeReceiver, 16},
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
