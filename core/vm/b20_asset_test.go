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
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

func encodeBatchMint(recips []common.Address, amounts []uint64) []byte {
	out := append([]byte{}, selBatchMint[:]...)
	out = append(out, u256hash(0x40).Bytes()...)                            // offset arr1
	out = append(out, u256hash(uint64(0x40+(1+len(recips))*32)).Bytes()...) // offset arr2
	out = append(out, u256hash(uint64(len(recips))).Bytes()...)
	for _, r := range recips {
		out = append(out, addrKey(r).Bytes()...)
	}
	out = append(out, u256hash(uint64(len(amounts))).Bytes()...)
	for _, a := range amounts {
		out = append(out, u256hash(a).Bytes()...)
	}
	return out
}

func TestB20AssetExtension(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *b20TestChainConfig()
	bc := b20BlockContext(1)
	seedActivation(statedb, b20ActivationAdmin)
	evm := NewEVM(bc, statedb, &cfg, Config{})

	creator := common.HexToAddress("0xc4ea70")
	minter := common.HexToAddress("0x33333")
	operator := common.HexToAddress("0x09e4a704")
	salt := common.HexToHash("0x0a")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	u := func(ret []byte, err error) uint64 {
		t.Helper()
		if err != nil {
			t.Fatalf("call err: %v", err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64()
	}

	// create an Asset token: minter=MINT, operator=OPERATOR, mint 1000 to alice.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(minter)),
		b20Call(selGrantRole, roleOperator, addrKey(operator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// defaults from extension storage.
	if got := u(call(creator, token, b20Call(selDecimals))); got != 18 {
		t.Errorf("decimals = %d, want 18", got)
	}
	if got := u(call(creator, token, b20Call(selMultiplier))); got != 1e18 {
		t.Errorf("multiplier = %d, want 1e18", got)
	}
	if got := u(call(creator, token, b20Call(selWadPrecision))); got != 1e18 {
		t.Errorf("WAD_PRECISION = %d, want 1e18", got)
	}
	if got := u(call(creator, token, b20Call(selScaledBalanceOf, addrKey(b20Alice)))); got != 1000 {
		t.Errorf("scaledBalanceOf(alice) = %d, want 1000", got)
	}

	// non-operator updateMultiplier reverts.
	if _, err := call(minter, token, b20Call(selUpdateMultiplier, u256hash(2e18))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("non-operator updateMultiplier err = %v, want revert", err)
	}
	// operator sets multiplier to 1.5x.
	if _, err := call(operator, token, b20Call(selUpdateMultiplier, u256hash(1_500_000_000_000_000_000))); err != nil {
		t.Fatalf("updateMultiplier: %v", err)
	}
	if got := u(call(creator, token, b20Call(selMultiplier))); got != 1_500_000_000_000_000_000 {
		t.Errorf("multiplier = %d, want 1.5e18", got)
	}

	// scaled views reflect the multiplier; raw balance is unchanged.
	if got := u(call(creator, token, b20Call(selScaledBalanceOf, addrKey(b20Alice)))); got != 1500 {
		t.Errorf("scaledBalanceOf(alice) = %d, want 1500", got)
	}
	if got := u(call(creator, token, b20Call(selToScaledBalance, u256hash(1000)))); got != 1500 {
		t.Errorf("toScaledBalance(1000) = %d, want 1500", got)
	}
	if got := u(call(creator, token, b20Call(selToRawBalance, u256hash(1500)))); got != 1000 {
		t.Errorf("toRawBalance(1500) = %d, want 1000", got)
	}
	if got := u(call(creator, token, b20Call(selBalanceOf, addrKey(b20Alice)))); got != 1000 {
		t.Errorf("balanceOf(alice) = %d, want 1000 (raw unchanged)", got)
	}
	// updateMultiplier to 0 reverts.
	if _, err := call(operator, token, b20Call(selUpdateMultiplier, u256hash(0))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("zero multiplier err = %v, want revert", err)
	}

	// batchMint to bob and carol.
	if _, err := call(minter, token, encodeBatchMint([]common.Address{b20Bob, b20Carol}, []uint64{10, 20})); err != nil {
		t.Fatalf("batchMint: %v", err)
	}
	view := newB20Storage(statedb, token)
	if view.balanceOf(b20Bob).Uint64() != 10 || view.balanceOf(b20Carol).Uint64() != 20 {
		t.Errorf("batchMint balances bob %d carol %d, want 10/20", view.balanceOf(b20Bob).Uint64(), view.balanceOf(b20Carol).Uint64())
	}
	if view.totalSupply().Uint64() != 1030 {
		t.Errorf("supply = %d, want 1030", view.totalSupply().Uint64())
	}
	// mismatched array lengths revert.
	if _, err := call(minter, token, encodeBatchMint([]common.Address{b20Bob}, []uint64{1, 2})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("length-mismatch batchMint err = %v, want revert", err)
	}
}

// abiStr encodes a string's tail: its length followed by the bytes padded up to
// a word. An empty string is one zero word and nothing else.
func abiStr(s string) []byte {
	return append(u256hash(uint64(len(s))).Bytes(), rightPad32([]byte(s))...)
}

// encodeAnnounce ABI-encodes announce(bytes[],string,string,string) with the
// bytes[] placed right after the head and empty description/uri strings. All
// four arguments are dynamic, so the head is four offsets.
func encodeAnnounce(calls [][]byte, id string) []byte {
	return encodeAnnounceWith(calls, id, "", "")
}

func encodeAnnounceWith(calls [][]byte, id, description, uri string) []byte {
	elems := make([][]byte, len(calls))
	for i, c := range calls {
		elems[i] = append(u256hash(uint64(len(c))).Bytes(), rightPad32(c)...)
	}
	arr := append([]byte{}, u256hash(uint64(len(calls))).Bytes()...)
	cur := uint64(len(calls) * 32)
	for _, e := range elems {
		arr = append(arr, u256hash(cur).Bytes()...)
		cur += uint64(len(e))
	}
	for _, e := range elems {
		arr = append(arr, e...)
	}
	idEnc, descEnc, uriEnc := abiStr(id), abiStr(description), abiStr(uri)
	idOff := uint64(0x80 + len(arr))
	descOff := idOff + uint64(len(idEnc))
	uriOff := descOff + uint64(len(descEnc))

	out := append([]byte{}, selAnnounce[:]...)
	out = append(out, u256hash(0x80).Bytes()...)    // w0 offset -> bytes[]
	out = append(out, u256hash(idOff).Bytes()...)   // w1 offset -> id
	out = append(out, u256hash(descOff).Bytes()...) // w2 offset -> description
	out = append(out, u256hash(uriOff).Bytes()...)  // w3 offset -> uri
	out = append(out, arr...)
	out = append(out, idEnc...)
	out = append(out, descEnc...)
	out = append(out, uriEnc...)
	return out
}

func TestB20Announce(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	operator := common.HexToAddress("0x09e4a704")
	salt := common.HexToHash("0x0e")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		b20Call(selGrantRole, roleOperator, addrKey(operator)),
		b20Call(selGrantRole, roleMint, addrKey(operator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	mul := func() uint64 {
		r, err := call(creator, token, b20Call(selMultiplier))
		if err != nil {
			t.Fatalf("multiplier(): %v", err)
		}
		return new(uint256.Int).SetBytes(r).Uint64()
	}

	const id1 = "2026-Q1-NAV"

	// happy path: announce bundling an updateMultiplier runs atomically.
	inner := [][]byte{b20Call(selUpdateMultiplier, u256hash(1_200_000_000_000_000_000))}
	if _, err := call(operator, token, encodeAnnounce(inner, id1)); err != nil {
		t.Fatalf("announce: %v", err)
	}
	if got := mul(); got != 1_200_000_000_000_000_000 {
		t.Fatalf("multiplier after announce = %d, want 1.2e18", got)
	}
	if r, _ := call(creator, token, encodeStringCall(selIsAnnouncementIdUsed, id1)); !bytes.Equal(r, encBool(true)) {
		t.Fatal("id1 should be marked used")
	}

	// reusing the id reverts.
	if _, err := call(operator, token, encodeAnnounce(nil, id1)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("reused id err = %v, want revert", err)
	}
	// non-operator reverts.
	if _, err := call(creator, token, encodeAnnounce(nil, "2026-Q2-NAV")); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("non-operator announce err = %v, want revert", err)
	}
	// nesting announce inside announce reverts (and rolls back, id unused).
	const nestedID = "2026-Q3-NAV"
	nested := [][]byte{encodeAnnounce(nil, "2026-Q4-NAV")}
	if _, err := call(operator, token, encodeAnnounce(nested, nestedID)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("nested announce err = %v, want revert", err)
	}
	if r, _ := call(creator, token, encodeStringCall(selIsAnnouncementIdUsed, nestedID)); !bytes.Equal(r, encBool(false)) {
		t.Fatal("failed announce must not mark its id (atomic rollback)")
	}

	// a failing internal call rolls the whole announce back.
	const badID = "2027-Q1-NAV"
	bad := [][]byte{b20Call(selUpdateMultiplier, u256hash(0))} // zero multiplier reverts
	if _, err := call(operator, token, encodeAnnounce(bad, badID)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("failing internal call err = %v, want revert", err)
	}
	if got := mul(); got != 1_200_000_000_000_000_000 {
		t.Fatalf("multiplier changed despite rollback = %d", got)
	}
	if r, _ := call(creator, token, encodeStringCall(selIsAnnouncementIdUsed, badID)); !bytes.Equal(r, encBool(false)) {
		t.Fatal("badID must not be marked (rollback)")
	}
}

func encodeStringCall(sel [4]byte, strs ...string) []byte {
	out := append([]byte{}, sel[:]...)
	bodies := make([][]byte, len(strs))
	cur := uint64(len(strs) * 32)
	offs := make([]uint64, len(strs))
	for i, s := range strs {
		offs[i] = cur
		bodies[i] = append(u256hash(uint64(len(s))).Bytes(), rightPad32([]byte(s))...)
		cur += uint64(len(bodies[i]))
	}
	for _, o := range offs {
		out = append(out, u256hash(o).Bytes()...)
	}
	for _, b := range bodies {
		out = append(out, b...)
	}
	return out
}

func TestB20ExtraMetadata(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x0f")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	readStr := func(to common.Address, input []byte) string {
		t.Helper()
		ret, err := call(creator, to, input)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// decode ABI string: [offset][len][data]
		n := new(uint256.Int).SetBytes(ret[32:64]).Uint64()
		return string(ret[64 : 64+n])
	}

	initCalls := [][]byte{b20Call(selGrantRole, roleMetadata, addrKey(creator))}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// unset key returns empty.
	if got := readStr(token, encodeStringCall(selExtraMetadata, "category")); got != "" {
		t.Fatalf("unset extraMetadata = %q, want empty", got)
	}
	// set + read a short value.
	if _, err := call(creator, token, encodeStringCall(selUpdateExtraMetadata, "category", "fund")); err != nil {
		t.Fatalf("updateExtraMetadata: %v", err)
	}
	if got := readStr(token, encodeStringCall(selExtraMetadata, "category")); got != "fund" {
		t.Fatalf("extraMetadata(category) = %q, want fund", got)
	}
	// long value (> 32 bytes) exercises the long-string path at a mapping slot.
	long := "an-international-securities-identification-number-XS1234567890"
	if _, err := call(creator, token, encodeStringCall(selUpdateExtraMetadata, "isin", long)); err != nil {
		t.Fatalf("updateExtraMetadata long: %v", err)
	}
	if got := readStr(token, encodeStringCall(selExtraMetadata, "isin")); got != long {
		t.Fatalf("extraMetadata(isin) = %q, want %q", got, long)
	}
	// empty value deletes.
	if _, err := call(creator, token, encodeStringCall(selUpdateExtraMetadata, "category", "")); err != nil {
		t.Fatalf("delete extraMetadata: %v", err)
	}
	if got := readStr(token, encodeStringCall(selExtraMetadata, "category")); got != "" {
		t.Fatalf("deleted extraMetadata = %q, want empty", got)
	}
	// empty key reverts.
	if _, err := call(creator, token, encodeStringCall(selUpdateExtraMetadata, "", "x")); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("empty key err = %v, want revert", err)
	}
	// non-METADATA caller reverts.
	if _, err := call(b20Alice, token, encodeStringCall(selUpdateExtraMetadata, "k", "v")); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("unauthorized err = %v, want revert", err)
	}
}

// TestB20AnnounceEncoderIsCanonical checks the hand-rolled encoder above against
// go-ethereum's ABI packer.
//
// encodeAnnounceWith feeds the decoder under test (readBytesArray, readStringArg).
// A mistake shared by both — a head offset counted from the wrong base, a tail
// padded the wrong way — leaves every announce test passing while the precompile
// disagrees with every real ABI encoder in existence. Nothing else in the suite
// distinguishes those two worlds.
//
// The cases exercise the shapes where the two encoders could part company: an
// all-empty call, a bytes[] element that is an exact multiple of 32 (so its tail
// needs no padding), an empty element inside a non-empty array, and a long id
// that pushes the later tails past one word.
func TestB20AnnounceEncoderIsCanonical(t *testing.T) {
	mustType := func(s string) abi.Type {
		t.Helper()
		ty, err := abi.NewType(s, "", nil)
		if err != nil {
			t.Fatalf("NewType(%s): %v", s, err)
		}
		return ty
	}
	args := abi.Arguments{
		{Type: mustType("bytes[]")},
		{Type: mustType("string")},
		{Type: mustType("string")},
		{Type: mustType("string")},
	}

	for _, tc := range []struct {
		name          string
		calls         [][]byte
		id, desc, uri string
	}{
		{name: "all empty"},
		{name: "no calls", id: "2026-Q1-NAV", desc: "d", uri: "u"},
		{name: "one call", calls: [][]byte{{1, 2, 3, 4}}, id: "id", desc: "desc", uri: "uri"},
		{
			name:  "element of exactly one word",
			calls: [][]byte{bytes.Repeat([]byte{9}, 32)},
			id:    "id", desc: "d", uri: "u",
		},
		{
			name:  "empty element among non-empty",
			calls: [][]byte{{}, {1, 2, 3, 4}, {}},
			id:    "id", desc: "d", uri: "u",
		},
		{
			name:  "multi-word id and description",
			calls: [][]byte{{1, 2, 3, 4}, bytes.Repeat([]byte{9}, 40)},
			id:    strings.Repeat("L", 70), desc: strings.Repeat("d", 33), uri: "u",
		},
		// Either side of the word boundary, where a string's tail either fits
		// exactly or spills into a second word and moves every later offset.
		{name: "id of 31 bytes", id: strings.Repeat("L", 31), desc: "d", uri: "u"},
		{name: "id of 32 bytes", id: strings.Repeat("L", 32), desc: "d", uri: "u"},
		{name: "id of 33 bytes", id: strings.Repeat("L", 33), desc: "d", uri: "u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := args.Pack(tc.calls, tc.id, tc.desc, tc.uri)
			if err != nil {
				t.Fatalf("Pack: %v", err)
			}
			if got := encodeAnnounceWith(tc.calls, tc.id, tc.desc, tc.uri)[4:]; !bytes.Equal(got, want) {
				t.Errorf("encoder disagrees with abi.Pack:\n got  %x\n want %x", got, want)
			}
		})
	}
}

// TestB20AnnouncementIDShapes covers the id lengths the fixed test ids do not.
//
// Announcement ids became strings, so their slot comes from keccak over the raw
// key bytes rather than a padded word. Every other announce test uses one
// 11-character id, which cannot tell that rule apart from a mistaken one for
// keys at or past a word boundary. The prefix pair is the point of the last
// check: keccak(key . base) must not let "x" and "xz" reach the same slot.
func TestB20AnnouncementIDShapes(t *testing.T) {
	for _, id := range []string{
		"",
		"x",
		strings.Repeat("L", 31),
		strings.Repeat("L", 32),
		strings.Repeat("L", 200),
	} {
		_, evm := newB20EVM(t)
		creator := common.HexToAddress("0xc4ea70")
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20(b20VariantAsset, common.HexToHash("0xbb"), creator,
				[][]byte{b20Call(selGrantRole, roleOperator, addrKey(creator))}),
			NewGasBudget(9_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("createB20: %v", err)
		}
		token := common.BytesToAddress(ret)

		call := func(input []byte) ([]byte, error) {
			out, _, err := evm.Call(creator, token, input, NewGasBudget(9_000_000), uint256.NewInt(0))
			return out, err
		}
		used := func(id string) bool {
			t.Helper()
			out, err := call(encodeStringCall(selIsAnnouncementIdUsed, id))
			if err != nil {
				t.Fatalf("isAnnouncementIdUsed(len %d): %v", len(id), err)
			}
			return bytes.Equal(out, encBool(true))
		}

		if used(id) {
			t.Fatalf("len %d: reported used before any announcement", len(id))
		}
		if _, err := call(encodeAnnounce(nil, id)); err != nil {
			t.Fatalf("announce(len %d): %v", len(id), err)
		}
		if !used(id) {
			t.Errorf("len %d: not marked used after announcing", len(id))
		}
		if _, err := call(encodeAnnounce(nil, id)); !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("len %d: reuse err = %v, want a revert", len(id), err)
		}
		if used(id + "z") {
			t.Errorf("len %d: a one-character-longer id aliases onto it", len(id))
		}
	}
}
