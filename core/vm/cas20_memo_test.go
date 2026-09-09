package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/holiman/uint256"
)

// selector ++ static words ++ one trailing uint64[] (offset, length, elements).
func withU64Array(sel [4]byte, static []common.Hash, ids []uint64) []byte {
	out := append([]byte{}, sel[:]...)
	for _, w := range static {
		out = append(out, w.Bytes()...)
	}
	out = append(out, u256hash(uint64(32*(len(static)+1))).Bytes()...)
	out = append(out, u256hash(uint64(len(ids))).Bytes()...)
	for _, id := range ids {
		out = append(out, u256hash(id).Bytes()...)
	}
	return out
}

// createMemoFormat(uint8 category, (uint16,uint16,uint8,bool)[] fields).
func encodeCreateMemoFormat(category uint64, fields []memoFieldSpec) []byte {
	out := append([]byte{}, selCreateMemoFormat[:]...)
	out = append(out, u256hash(category).Bytes()...)
	out = append(out, u256hash(0x40).Bytes()...)
	out = append(out, u256hash(uint64(len(fields))).Bytes()...)
	for _, f := range fields {
		out = append(out, u256hash(uint64(f.offset)).Bytes()...)
		out = append(out, u256hash(uint64(f.length)).Bytes()...)
		out = append(out, u256hash(uint64(f.kind)).Bytes()...)
		out = append(out, encBool(f.required)...)
	}
	return out
}

// The Travel Rule layout from the proposal: four fields, 164 bytes.
var travelRuleFields = []memoFieldSpec{
	{0, 70, 0, true}, {70, 20, 1, true}, {90, 70, 0, true}, {160, 4, 3, false},
}

func TestCAS20MemoFormatRegistry(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	reg := CAS20MemoFormatRegistryAddress
	creator := common.HexToAddress("0xc0ffee")
	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, reg, input, NewGasBudget(2_000_000), uint256.NewInt(0))
		return ret, err
	}
	sel := func(input []byte, want [4]byte) bool { return len(input) >= 4 && [4]byte(input[:4]) == want }

	// UNSTRUCTURED exists before anything is created, and reads as an empty layout.
	if ret, _ := call(creator, cas20Call(selFormatExists, u256hash(0))); !bytes.Equal(ret, encBool(true)) {
		t.Fatal("format 0 must exist from the start")
	}
	if ret, err := call(creator, cas20Call(selGetMemoFormat, u256hash(0))); err != nil ||
		!bytes.Equal(ret, encodeTuple(abiPart{dynamic: true, tail: u256hash(0).Bytes()}, abiWord(common.Hash{}), abiWord(common.Hash{}))) {
		t.Fatalf("getMemoFormat(0) = %x, %v; want an empty layout", ret, err)
	}
	if ret, _ := call(creator, cas20Call(selFormatExists, u256hash(1))); !bytes.Equal(ret, encBool(false)) {
		t.Fatal("INTERNAL#1 must not exist before creation")
	}

	// Creation mints TRAVEL_RULE#1 and records the layout.
	statedb.SetTxContext(common.HexToHash("0x7a"), 0)
	ret, err := call(creator, encodeCreateMemoFormat(memoCategoryTravelRule, travelRuleFields))
	if err != nil {
		t.Fatalf("createMemoFormat: %v", err)
	}
	want := uint64(memoCategoryTravelRule)<<56 | 1
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != want {
		t.Fatalf("formatId = %x, want %x", got, want)
	}
	logs := statedb.GetLogs(common.HexToHash("0x7a"), 1, common.Hash{}, 1)
	if len(logs) != 1 || logs[0].Topics[0] != cas20TopicMemoFormatCreated || logs[0].Topics[1] != wU64(want) ||
		logs[0].Topics[2] != addrKey(creator) || !bytes.Equal(logs[0].Data, wU8(memoCategoryTravelRule).Bytes()) {
		t.Errorf("MemoFormatCreated log = %+v", logs)
	}
	if ret, _ := call(creator, cas20Call(selFormatCount, u256hash(memoCategoryTravelRule))); new(uint256.Int).SetBytes(ret).Uint64() != 1 {
		t.Errorf("formatCount(TRAVEL_RULE) = %x, want 1", ret)
	}
	ret, err = call(creator, cas20Call(selGetMemoFormat, u256hash(want)))
	if err != nil {
		t.Fatalf("getMemoFormat: %v", err)
	}
	// (fields, totalLen, creator): head offset, totalLen 164, creator; tail 4 fields.
	if got := new(uint256.Int).SetBytes(ret[32:64]).Uint64(); got != 164 {
		t.Errorf("totalLen = %d, want 164", got)
	}
	if got := common.BytesToAddress(ret[64:96]); got != creator {
		t.Errorf("creator = %s, want %s", got, creator)
	}
	if n := new(uint256.Int).SetBytes(ret[96:128]).Uint64(); n != 4 {
		t.Fatalf("field count = %d, want 4", n)
	}
	for i, f := range travelRuleFields {
		e := ret[128+i*128:]
		if new(uint256.Int).SetBytes(e[:32]).Uint64() != uint64(f.offset) || new(uint256.Int).SetBytes(e[32:64]).Uint64() != uint64(f.length) ||
			e[95] != f.kind || (e[127] == 1) != f.required {
			t.Errorf("field %d = %x, want %+v", i, e[:128], f)
		}
	}

	// Ids are sequential within a category, and INTERNAL starts at 1 because 0 is taken.
	ret, _ = call(creator, encodeCreateMemoFormat(memoCategoryTravelRule, travelRuleFields[:1]))
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != want+1 {
		t.Errorf("second TRAVEL_RULE id = %x, want %x", got, want+1)
	}
	ret, _ = call(creator, encodeCreateMemoFormat(memoCategoryInternal, travelRuleFields[:1]))
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != 1 {
		t.Errorf("first INTERNAL id = %x, want 1", got)
	}

	// Errors, in check order: category, then the fields.
	for _, tc := range []struct {
		name  string
		input []byte
		want  [4]byte
	}{
		{"category past GOV", encodeCreateMemoFormat(4, travelRuleFields), errSelInvalidCategory},
		{"no fields", encodeCreateMemoFormat(memoCategoryISO, nil), errSelInvalidFieldCount},
		{"zero-length field", encodeCreateMemoFormat(memoCategoryISO, []memoFieldSpec{{0, 0, 0, true}}), errSelInvalidFieldSpec},
		{"field past 65535", encodeCreateMemoFormat(memoCategoryISO, []memoFieldSpec{{0xfff0, 0x20, 0, true}}), errSelInvalidFieldSpec},
		{"kind past BYTES", encodeCreateMemoFormat(memoCategoryISO, []memoFieldSpec{{0, 1, 5, true}}), errSelInvalidFieldSpec},
		{"unknown id", cas20Call(selGetMemoFormat, u256hash(1<<56|9)), errSelFormatNotFound},
	} {
		ret, err := call(creator, tc.input)
		if !errors.Is(err, ErrExecutionReverted) || !sel(ret, tc.want) {
			t.Errorf("%s: ret %x err %v, want %x", tc.name, ret, err, tc.want)
		}
	}
	// A dirty bool word is a decode failure: empty returndata.
	dirty := encodeCreateMemoFormat(memoCategoryISO, travelRuleFields[:1])
	dirty[len(dirty)-1] = 2
	if ret, err := call(creator, dirty); !errors.Is(err, ErrExecutionReverted) || len(ret) != 0 {
		t.Errorf("dirty required word: ret %x err %v, want an empty revert", ret, err)
	}
	// A static frame is refused before decoding.
	if ret, _, err := evm.StaticCall(creator, reg, encodeCreateMemoFormat(memoCategoryISO, travelRuleFields), NewGasBudget(2_000_000)); !sel(ret, errSelStaticCallDenied) {
		t.Errorf("STATICCALL createMemoFormat: ret %x err %v, want StaticCallNotAllowed", ret, err)
	}
	// And the feature gate precedes decoding too.
	act := cas20Storage{state: statedb, token: CAS20ActivationRegistryAddress}
	act.setWord(mappingSlot(actSlot(actSlotFeatures), featureMemoRegistry), common.Hash{})
	if ret, err := call(creator, encodeCreateMemoFormat(memoCategoryISO, nil)); !sel(ret, errSelFeatureNotActive) {
		t.Errorf("closed feature: ret %x err %v, want FeatureNotActivated before InvalidFieldCount", ret, err)
	}
}

func TestCAS20TransferWithMemoFormats(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(3_000_000), uint256.NewInt(0))
		return ret, err
	}
	sel := func(ret []byte, want [4]byte) bool { return len(ret) >= 4 && [4]byte(ret[:4]) == want }

	ret, err := call(creator, CAS20MemoFormatRegistryAddress, encodeCreateMemoFormat(memoCategoryISO, travelRuleFields[:2]))
	if err != nil {
		t.Fatal(err)
	}
	iso := new(uint256.Int).SetBytes(ret).Uint64()
	ret, err = call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantStablecoin, common.HexToHash("0x3e0"), creator, [][]byte{
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selGrantRole, rolePause, addrKey(creator)),
		cas20Call(selGrantRole, roleUnpause, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Alice), u256hash(1000)),
	}))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newUnmeteredCAS20Storage(statedb, token)
	memo := common.HexToHash("0x1234")

	// The declared transfer: Transfer, Memo, MemoFormatsDeclared, in that order.
	statedb.SetTxContext(common.HexToHash("0x7b"), 0)
	if ret, err := call(cas20Alice, token, withU64Array(selTransferWithMemoFormats, []common.Hash{addrKey(cas20Bob), u256hash(40), memo}, []uint64{0, iso})); err != nil || !bytes.Equal(ret, encBool(true)) {
		t.Fatalf("transferWithMemoFormats: ret %x err %v", ret, err)
	}
	if view.balanceOf(cas20Bob).Uint64() != 40 {
		t.Errorf("bob = %d, want 40", view.balanceOf(cas20Bob).Uint64())
	}
	logs := statedb.GetLogs(common.HexToHash("0x7b"), 1, common.Hash{}, 1)
	if len(logs) != 3 || logs[0].Topics[0] != cas20TopicTransfer || logs[1].Topics[0] != cas20TopicMemo || logs[2].Topics[0] != cas20TopicMemoFormatsDeclared {
		t.Fatalf("logs = %+v, want Transfer, Memo, MemoFormatsDeclared", logs)
	}
	if logs[2].Topics[1] != memo || !bytes.Equal(logs[2].Data, encodeTuple(abiWordArray(u64Words([]uint64{0, iso})))) {
		t.Errorf("MemoFormatsDeclared = topics %v data %x", logs[2].Topics, logs[2].Data)
	}

	// The claim is checked for existence and nothing else, before the transfer's own checks.
	for _, tc := range []struct {
		name string
		ids  []uint64
		want [4]byte
	}{
		{"unknown format", []uint64{iso + 7}, errSelFormatNotFound},
		{"no formats", nil, errSelEmptyBatch},
	} {
		ret, err := call(cas20Alice, token, withU64Array(selTransferWithMemoFormats, []common.Hash{addrKey(cas20Bob), u256hash(1), memo}, tc.ids))
		if !errors.Is(err, ErrExecutionReverted) || !sel(ret, tc.want) {
			t.Errorf("%s: ret %x err %v, want %x", tc.name, ret, err, tc.want)
		}
	}
	if _, err := call(creator, token, cas20CallU8Array(selPause, byte(cas20PauseTransfer))); err != nil {
		t.Fatal(err)
	}
	if ret, _ := call(cas20Alice, token, withU64Array(selTransferWithMemoFormats, []common.Hash{addrKey(cas20Bob), u256hash(1), memo}, []uint64{iso + 7})); !sel(ret, errSelFormatNotFound) {
		t.Errorf("paused + unknown format: ret %x, want FormatNotFound (the claim is checked first)", ret)
	}
	if ret, _ := call(cas20Alice, token, withU64Array(selTransferWithMemoFormats, []common.Hash{addrKey(cas20Bob), u256hash(1), memo}, []uint64{iso})); !sel(ret, errSelContractPaused) {
		t.Errorf("paused + known format: ret %x, want ContractPaused", ret)
	}
	if _, err := call(creator, token, cas20CallU8Array(selUnpause, byte(cas20PauseTransfer))); err != nil {
		t.Fatal(err)
	}

	// transferFrom spends the allowance like its memo counterpart.
	if _, err := call(cas20Alice, token, cas20Call(selApprove, addrKey(cas20Carol), u256hash(10))); err != nil {
		t.Fatal(err)
	}
	if _, err := call(cas20Carol, token, withU64Array(selTransferFromWithMemoFormats, []common.Hash{addrKey(cas20Alice), addrKey(cas20Bob), u256hash(6), memo}, []uint64{iso})); err != nil {
		t.Fatalf("transferFromWithMemoFormats: %v", err)
	}
	if view.allowance(cas20Alice, cas20Carol).Uint64() != 4 || view.balanceOf(cas20Bob).Uint64() != 46 {
		t.Errorf("allowance %d bob %d, want 4 and 46", view.allowance(cas20Alice, cas20Carol).Uint64(), view.balanceOf(cas20Bob).Uint64())
	}

	// The issuer's advisory list: admin-gated, existence-checked, shrink clears the tail.
	if ret, _ := call(cas20Alice, token, withU64Array(selUpdateExpectedMemoFormats, nil, []uint64{iso})); !sel(ret, errSelACUnauthorized) {
		t.Errorf("stranger updateExpectedMemoFormats: %x, want AccessControlUnauthorizedAccount", ret)
	}
	if ret, _ := call(creator, token, withU64Array(selUpdateExpectedMemoFormats, nil, []uint64{iso + 7})); !sel(ret, errSelFormatNotFound) {
		t.Errorf("unknown expected format: %x, want FormatNotFound", ret)
	}
	statedb.SetTxContext(common.HexToHash("0x7c"), 0)
	five := []uint64{iso, 0, iso, 0, iso}
	if _, err := call(creator, token, withU64Array(selUpdateExpectedMemoFormats, nil, five)); err != nil {
		t.Fatal(err)
	}
	logs = statedb.GetLogs(common.HexToHash("0x7c"), 1, common.Hash{}, 1)
	if len(logs) != 1 || logs[0].Topics[0] != cas20TopicExpectedMemoFormatsUpdated || logs[0].Topics[1] != addrKey(creator) {
		t.Errorf("ExpectedMemoFormatsUpdated log = %+v", logs)
	}
	if ret, _ := call(cas20Alice, token, cas20Call(selExpectedMemoFormats)); !bytes.Equal(ret, encodeTuple(abiWordArray(u64Words(five)))) {
		t.Errorf("expectedMemoFormats = %x, want the five", ret)
	}
	if _, err := call(creator, token, withU64Array(selUpdateExpectedMemoFormats, nil, []uint64{iso})); err != nil {
		t.Fatal(err)
	}
	slot := slotAt(cas20SlotExpectedMemoFormats)
	second := new(uint256.Int).AddUint64(view.stringDataRoot(slot), 1).Bytes32()
	if got := view.u64ArrayAt(slot, cas20MemoMaxFields); len(got) != 1 || got[0] != iso {
		t.Errorf("after shrink = %v, want [iso]", got)
	}
	if statedb.GetState(token, second) != (common.Hash{}) {
		t.Error("the second data word survived the shrink")
	}
	info, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, token, evm.Context.Time)
	if err != nil || len(info.ExpectedMemoFormats) != 1 || uint64(info.ExpectedMemoFormats[0]) != iso {
		t.Errorf("token info expectedMemoFormats = %v, %v", info.ExpectedMemoFormats, err)
	}
}

// The fork hook plants the third sentinel beside the other two.
func TestCAS20MemoRegistrySentinelPlanted(t *testing.T) {
	statedb, _ := state.New(common.Hash{}, state.NewDatabaseForTesting())
	SeedCAS20Activation(statedb)
	if statedb.GetCodeHash(CAS20MemoFormatRegistryAddress) != cas20MarkerCodeHash {
		t.Fatal("the MemoFormatRegistry has no sentinel after seeding")
	}
}
