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
//
// Both emitted only topics: ExtraMetadataUpdated dropped its key and value,
// Announcement its description and uri. base-std declares all four as non-indexed
// arguments, so an indexer following that ABI decoded nothing. Filling them in also
// changes the gas — log data is charged per byte — so this test asserts the bytes
// and the next one the price.
//
// The expectation comes from go-ethereum's own ABI packer rather than from
// encodeTuple, which is the encoder under test.
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

	// A short pair and one whose first member spans several words, since a long
	// first string moves the second's tail — a fixed-offset assumption survives
	// the short case.
	for _, tc := range []struct{ a, b string }{
		{"category", "rwa"},
		{strings.Repeat("k", 100), strings.Repeat("v", 3)},
		{"", ""},
	} {
		want, err := twoStrings.Pack(tc.a, tc.b)
		if err != nil {
			t.Fatalf("Pack: %v", err)
		}

		// updateExtraMetadata rejects an empty key, so the empty case only
		// exercises the announcement.
		if tc.a != "" {
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
}

// TestB20AnnouncementPayload pins the other half, and the price the data carries.
//
// The gas delta is derived from params.LogDataGas rather than measured against
// another B20 call: comparing two B20 shapes would hold under any per-byte rate,
// which is how the calldata charge went wrong earlier in this work.
func TestB20AnnouncementPayload(t *testing.T) {
	mustType := func(s string) abi.Type {
		t.Helper()
		ty, err := abi.NewType(s, "", nil)
		if err != nil {
			t.Fatalf("NewType(%s): %v", s, err)
		}
		return ty
	}
	twoStrings := abi.Arguments{{Type: mustType("string")}, {Type: mustType("string")}}

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

	const desc = "quarterly NAV update"
	const uri = "ipfs://QmExample"

	statedb, evm, token, operator := newToken(t)
	if _, _, err := evm.Call(operator, token, encodeAnnounceWith(nil, u256hash(1), desc, uri),
		NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("announce: %v", err)
	}
	want, err := twoStrings.Pack(desc, uri)
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
	if len(found.Topics) != 3 || found.Topics[2] != u256hash(1) {
		t.Errorf("Announcement topics = %v, want [sig, caller, id]", found.Topics)
	}

	// The added bytes are charged at the LOG data rate, no more and no less. The
	// longer arguments also lengthen the calldata, so that charge is subtracted
	// out — both terms derived from the schedule rather than from another B20 call,
	// which would hold under any per-byte rate.
	gasFor := func(description, u string) (uint64, int) {
		_, evm, token, operator := newToken(t)
		input := encodeAnnounceWith(nil, u256hash(2), description, u)
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
	emptyData, _ := twoStrings.Pack("", "")

	wantLog := params.LogDataGas * uint64(len(want)-len(emptyData))
	wantCalldata := calldataGas(fullLen) - calldataGas(emptyLen)
	if got, wantTotal := fullGas-emptyGas, wantLog+wantCalldata; got != wantTotal {
		t.Errorf("payload gas delta = %d, want %d (%d log-data bytes at %d, plus %d for the longer calldata)",
			got, wantTotal, len(want)-len(emptyData), params.LogDataGas, wantCalldata)
	}
}
