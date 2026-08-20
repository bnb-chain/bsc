package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// TestB20AssetEventPayloads pins the two Asset events that carried empty data.
func TestB20AssetEventPayloads(t *testing.T) {
	mustType := func(s string) abi.Type {
		t.Helper()
		ty, err := abi.NewType(s, "", nil)
		if err != nil {
			t.Fatalf("NewType(%s): %v", s, err)
		}
		return ty
	}
	twoStrings := abi.Arguments{{Type: mustType("string")}, {Type: mustType("string")}}

	// A short pair; one whose first member spans several words, since a long
	// first string moves the second's tail and a fixed-offset assumption
	// survives the short case; and an empty value, which is how an entry is
	// deleted and still emits. The key may not be empty — updateExtraMetadata
	// rejects that — so an empty first member has no case here.
	for _, tc := range []struct{ a, b string }{
		{"category", "rwa"},
		{strings.Repeat("k", 100), strings.Repeat("v", 3)},
		{"category", ""},
	} {
		want, err := twoStrings.Pack(tc.a, tc.b)
		if err != nil {
			t.Fatalf("Pack: %v", err)
		}

		statedb, evm := newB20EVM(t)
		creator := common.HexToAddress("0xc4ea70")
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20(b20VariantAsset, common.HexToHash("0xe1"), creator,
				[][]byte{b20Call(selGrantRole, roleMetadata, addrKey(creator))}),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("createB20: %v", err)
		}
		token := common.BytesToAddress(ret)

		if _, _, err := evm.Call(creator, token,
			encodeStringCall(selUpdateExtraMetadata, tc.a, tc.b),
			NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
			t.Fatalf("updateExtraMetadata(%q, %q): %v", tc.a, tc.b, err)
		}
		logs := statedb.Logs()
		last := logs[len(logs)-1]
		if last.Topics[0] != b20TopicExtraMetadataUpdated {
			t.Fatalf("last log topic0 = %s, want ExtraMetadataUpdated", last.Topics[0].Hex())
		}
		if !bytes.Equal(last.Data, want) {
			t.Errorf("ExtraMetadataUpdated data for (%q, %q):\n got %x\nwant %x", tc.a, tc.b, last.Data, want)
		}
	}
}

// TestB20AnnouncementPayload pins the other half, and the price the data carries.
func TestB20AnnouncementPayload(t *testing.T) {
	mustType := func(s string) abi.Type {
		t.Helper()
		ty, err := abi.NewType(s, "", nil)
		if err != nil {
			t.Fatalf("NewType(%s): %v", s, err)
		}
		return ty
	}
	str := mustType("string")
	threeStrings := abi.Arguments{{Type: str}, {Type: str}, {Type: str}}

	newToken := func(t *testing.T) (*state.StateDB, *EVM, common.Address, common.Address) {
		t.Helper()
		statedb, evm := newB20EVM(t)
		creator := common.HexToAddress("0xc4ea70")
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20(b20VariantAsset, common.HexToHash("0xa11"), creator,
				[][]byte{b20Call(selGrantRole, roleOperator, addrKey(creator))}),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("createB20: %v", err)
		}
		return statedb, evm, common.BytesToAddress(ret), creator
	}

	const (
		announceID = "2026-Q1-NAV"
		desc       = "quarterly NAV update"
		uri        = "ipfs://QmExample"
	)

	statedb, evm, token, operator := newToken(t)
	if _, _, err := evm.Call(operator, token, encodeAnnounceWith(nil, announceID, desc, uri),
		NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("announce: %v", err)
	}
	want, err := threeStrings.Pack(announceID, desc, uri)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	var found *types.Log
	for _, l := range statedb.Logs() {
		if l.Topics[0] == b20TopicAnnouncement {
			found = l
		}
	}
	if found == nil {
		t.Fatal("no Announcement log")
	}
	if !bytes.Equal(found.Data, want) {
		t.Errorf("Announcement data:\n got %x\nwant %x", found.Data, want)
	}
	if len(found.Topics) != 2 || found.Topics[1] != addrKey(operator) {
		t.Errorf("Announcement topics = %v, want [sig, caller] — the id is data, not a topic", found.Topics)
	}

	// EndAnnouncement closes the bracket and carries the id the same way, with
	// no indexed argument at all. Indexers pair the two by that id, so it has to
	// be readable from the data rather than hashed into a topic.
	var end *types.Log
	for _, l := range statedb.Logs() {
		if l.Topics[0] == b20TopicEndAnnouncement {
			end = l
		}
	}
	if end == nil {
		t.Fatal("no EndAnnouncement log")
	}
	oneString := abi.Arguments{{Type: str}}
	wantEnd, err := oneString.Pack(announceID)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if !bytes.Equal(end.Data, wantEnd) {
		t.Errorf("EndAnnouncement data:\n got %x\nwant %x", end.Data, wantEnd)
	}
	if len(end.Topics) != 1 {
		t.Errorf("EndAnnouncement topics = %v, want the signature alone", end.Topics)
	}

	// The added bytes are charged at the LOG data rate, no more and no less. The
	// longer arguments also lengthen the calldata, so that charge is subtracted
	// out — both terms derived from the schedule rather than from another B20 call,
	// which would hold under any per-byte rate.
	gasFor := func(description, u string) (uint64, int) {
		_, evm, token, operator := newToken(t)
		input := encodeAnnounceWith(nil, announceID, description, u)
		budget := NewGasBudget(5_000_000)
		_, left, err := evm.Call(operator, token, input, budget, uint256.NewInt(0))
		if err != nil {
			t.Fatalf("announce(%q, %q): %v", description, u, err)
		}
		return budget.RegularGas - left.RegularGas, len(input)
	}
	calldataGas := func(n int) uint64 {
		words := (uint64(n) + 31) / 32
		if words == 0 {
			return 0
		}
		return GasFastestStep + words*b20CalldataWordGas + words*words/params.QuadCoeffDiv
	}

	emptyGas, emptyLen := gasFor("", "")
	fullGas, fullLen := gasFor(desc, uri)
	emptyData, _ := threeStrings.Pack(announceID, "", "")

	wantLog := params.LogDataGas * uint64(len(want)-len(emptyData))
	wantCalldata := calldataGas(fullLen) - calldataGas(emptyLen)
	if got, wantTotal := fullGas-emptyGas, wantLog+wantCalldata; got != wantTotal {
		t.Errorf("payload gas delta = %d, want %d (%d log-data bytes at %d, plus %d for the longer calldata)",
			got, wantTotal, len(want)-len(emptyData), params.LogDataGas, wantCalldata)
	}
}
