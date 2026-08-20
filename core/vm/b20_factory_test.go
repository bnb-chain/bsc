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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

func rightPad32(b []byte) []byte {
	out := make([]byte, (len(b)+31)/32*32)
	copy(out, b)
	return out
}

func b20AssetParams(name, symbol string, admin common.Address, decimals byte) []byte {
	return abiEncodeStruct(
		abiWord(wU8(b20ParamsVersion)),
		abiString(name),
		abiString(symbol),
		abiWord(addrKey(admin)),
		abiWord(wU8(decimals)),
	)
}

func b20StablecoinParams(name, symbol string, admin common.Address, currency string) []byte {
	return abiEncodeStruct(
		abiWord(wU8(b20ParamsVersion)),
		abiString(name),
		abiString(symbol),
		abiWord(addrKey(admin)),
		abiString(currency),
	)
}

// encodeCreateB20 ABI-encodes createB20(uint8,bytes32,bytes,bytes[]) with the
// variant's default params, which is what most tests want.
func encodeCreateB20(variant byte, salt common.Hash, admin common.Address, calls [][]byte) []byte {
	params := b20AssetParams("Test Token", "TT", admin, 18)
	if variant == b20VariantStablecoin {
		params = b20StablecoinParams("Test Stable", "TS", admin, "USD")
	}
	return encodeCreateB20WithParams(variant, salt, params, calls)
}

// encodeCreateB20WithParams is encodeCreateB20 with an explicit params blob,
// for tests that exercise the validation paths.
func encodeCreateB20WithParams(variant byte, salt common.Hash, params []byte, calls [][]byte) []byte {
	elems := make([][]byte, len(calls))
	for i, c := range calls {
		elems[i] = append(u256hash(uint64(len(c))).Bytes(), rightPad32(c)...)
	}
	arr := append([]byte{}, u256hash(uint64(len(calls))).Bytes()...)
	cur := uint64(len(calls) * 32) // element offsets are relative to just after the length word
	for _, e := range elems {
		arr = append(arr, u256hash(cur).Bytes()...)
		cur += uint64(len(e))
	}
	for _, e := range elems {
		arr = append(arr, e...)
	}

	out := append([]byte{}, selCreateB20[:]...)
	return append(out, encodeTuple(
		abiWord(u256hash(uint64(variant))),
		abiWord(salt),
		abiBytes(params),
		abiPart{dynamic: true, tail: arr},
	)...)
}

func TestB20Factory(t *testing.T) {
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
	salt := common.HexToHash("0x01")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// predict address.
	predicted, err := call(creator, B20FactoryAddress, b20Call(selGetB20Address, u256hash(b20VariantAsset), addrKey(creator), salt))
	if err != nil {
		t.Fatalf("getB20Address: %v", err)
	}
	want := b20DeriveAddress(b20VariantAsset, creator, salt)
	if common.BytesToAddress(predicted) != want {
		t.Fatalf("getB20Address = %s, want %s", common.BytesToAddress(predicted).Hex(), want.Hex())
	}

	// create the token with bootstrap initCalls: grant MINT to minter, mint 1000 to alice.
	initCalls := [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(minter)),
		b20Call(selMint, addrKey(b20Alice), u256hash(1000)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	if token != want {
		t.Fatalf("createB20 returned %s, want %s", token.Hex(), want.Hex())
	}

	// isB20Initialized(token) == true.
	if r, _ := call(creator, B20FactoryAddress, b20Call(selIsB20Initialized, addrKey(token))); !bytes.Equal(r, encBool(true)) {
		t.Fatal("token should be initialized")
	}

	// the created token is live: bootstrap state applied.
	view := newUnmeteredB20Storage(statedb, token)
	if view.totalSupply().Uint64() != 1000 || view.balanceOf(b20Alice).Uint64() != 1000 {
		t.Fatalf("supply %d aliceBal %d, want 1000/1000", view.totalSupply().Uint64(), view.balanceOf(b20Alice).Uint64())
	}
	if !view.hasRole(roleDefaultAdmin, creator) || view.adminCount().Uint64() != 1 {
		t.Fatal("creator should be sole DEFAULT_ADMIN")
	}
	if !view.hasRole(roleMint, minter) {
		t.Fatal("minter should hold MINT_ROLE")
	}

	// and it behaves like a token through the EVM: alice transfers to bob.
	if r, err := call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(400))); err != nil || !bytes.Equal(r, encBool(true)) {
		t.Fatalf("transfer via created token: ret %x err %v", r, err)
	}
	if view.balanceOf(b20Bob).Uint64() != 400 {
		t.Fatalf("bob balance %d, want 400", view.balanceOf(b20Bob).Uint64())
	}

	// re-creating at the same salt collides.
	if _, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("duplicate createB20 err = %v, want revert", err)
	}
}

// TestB20FactoryOwnerless creates a token with initialAdmin == 0: roles are set
// up during the privileged bootstrap and the token is then ungovernable.
func TestB20FactoryOwnerless(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	cfg := *b20TestChainConfig()
	bc := b20BlockContext(1)
	seedActivation(statedb, b20ActivationAdmin)
	evm := NewEVM(bc, statedb, &cfg, Config{})
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x02")

	// initCalls grant MINT to creator despite no admin (privileged bootstrap).
	initCalls := [][]byte{b20Call(selGrantRole, roleMint, addrKey(creator))}
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantStablecoin, salt, common.Address{}, initCalls),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20 ownerless: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newUnmeteredB20Storage(statedb, token)
	if !view.adminCount().IsZero() {
		t.Fatalf("adminCount = %d, want 0 (ownerless)", view.adminCount().Uint64())
	}
	if !view.hasRole(roleMint, creator) {
		t.Fatal("bootstrap should have granted MINT despite ownerless")
	}
	// post-creation, role mutations are impossible (no admin).
	if _, _, err := evm.Call(creator, token, b20Call(selGrantRole, roleBurn, addrKey(creator)),
		NewGasBudget(1_000_000), uint256.NewInt(0)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("grant on ownerless token err = %v, want revert", err)
	}
}

// TestB20CreateParams exercises the create-params blob: the version gate that
// precedes every field check, the per-variant validation, and the metadata the
// token ends up carrying.
func TestB20CreateParams(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	call := func(input []byte) ([]byte, error) {
		ret, _, err := evm.Call(creator, B20FactoryAddress, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	salt := func(n uint64) common.Hash { return u256hash(n) }

	// An unsupported version is reported before any field is looked at, so a
	// blob that is also invalid downstream still fails on the version.
	bad := abiEncodeStruct(abiWord(wU8(2)), abiString("N"), abiString("S"), abiWord(addrKey(creator)), abiWord(wU8(3)))
	ret, err := call(encodeCreateB20WithParams(b20VariantAsset, salt(1), bad, nil))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("bad version err = %v, want revert", err)
	}
	want := append(append([]byte{}, errSelUnsupportedVersion[:]...), wU8(2).Bytes()...)
	want = append(want, wU8(b20VariantAsset).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want UnsupportedVersion(2, ASSET) = %x", ret, want)
	}

	// Asset decimals are bounded.
	for _, d := range []byte{5, 19} {
		p := b20AssetParams("N", "S", creator, d)
		ret, err := call(encodeCreateB20WithParams(b20VariantAsset, salt(uint64(d)), p, nil))
		if !errors.Is(err, ErrExecutionReverted) {
			t.Fatalf("decimals %d err = %v, want revert", d, err)
		}
		want := append(append([]byte{}, errSelInvalidDecimals[:]...), wU8(d).Bytes()...)
		if !bytes.Equal(ret, want) {
			t.Fatalf("decimals %d revert data = %x, want InvalidDecimals", d, ret)
		}
	}

	// Stablecoin currency must be present and uppercase A-Z.
	if _, err := call(encodeCreateB20WithParams(b20VariantStablecoin, salt(20), b20StablecoinParams("N", "S", creator, ""), nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("empty currency err = %v, want MissingRequiredField", err)
	}
	if _, err := call(encodeCreateB20WithParams(b20VariantStablecoin, salt(21), b20StablecoinParams("N", "S", creator, "usd"), nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("lowercase currency err = %v, want InvalidCurrency", err)
	}

	// A valid Asset carries its metadata and its chosen decimals.
	ret, err = call(encodeCreateB20WithParams(b20VariantAsset, salt(30), b20AssetParams("Gold Fund", "GLD", creator, 8), nil))
	if err != nil {
		t.Fatalf("createB20 asset: %v", err)
	}
	asset := common.BytesToAddress(ret)
	view := newUnmeteredB20Storage(statedb, asset)
	if got := view.name(); got != "Gold Fund" {
		t.Fatalf("name = %q, want Gold Fund", got)
	}
	if got := view.symbol(); got != "GLD" {
		t.Fatalf("symbol = %q, want GLD", got)
	}
	dec, _, err := evm.Call(creator, asset, b20Call(selDecimals), NewGasBudget(200_000), uint256.NewInt(0))
	if err != nil || !bytes.Equal(dec, u256hash(8).Bytes()) {
		t.Fatalf("decimals() = %x (err %v), want 8", dec, err)
	}

	// A valid Stablecoin exposes its immutable currency and fixed 6 decimals.
	ret, err = call(encodeCreateB20WithParams(b20VariantStablecoin, salt(31), b20StablecoinParams("Euro Coin", "EURC", creator, "EUR"), nil))
	if err != nil {
		t.Fatalf("createB20 stablecoin: %v", err)
	}
	stable := common.BytesToAddress(ret)
	cur, _, err := evm.Call(creator, stable, b20Call(selCurrency), NewGasBudget(200_000), uint256.NewInt(0))
	if err != nil || !bytes.Equal(cur, encString("EUR")) {
		t.Fatalf("currency() = %x (err %v), want EUR", cur, err)
	}
	dec, _, err = evm.Call(creator, stable, b20Call(selDecimals), NewGasBudget(200_000), uint256.NewInt(0))
	if err != nil || !bytes.Equal(dec, u256hash(6).Bytes()) {
		t.Fatalf("stablecoin decimals() = %x (err %v), want 6", dec, err)
	}
}

// TestB20CreatedEvent pins the creation event's topics and payload: an indexer
// must be able to build a token index from the factory address alone.
func TestB20CreatedEvent(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xe7e17")
	statedb.SetTxContext(common.HexToHash("0xbeef"), 0)

	params := b20StablecoinParams("Dollar Coin", "USDX", creator, "USD")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20WithParams(b20VariantStablecoin, common.HexToHash("0x77"), params, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	var created *types.Log
	for _, l := range statedb.Logs() {
		if len(l.Topics) > 0 && l.Topics[0] == b20TopicB20Created {
			created = l
		}
	}
	if created == nil {
		t.Fatal("no B20Created log emitted")
	}
	if created.Address != B20FactoryAddress {
		t.Fatalf("emitted by %x, want the factory %x", created.Address, B20FactoryAddress)
	}
	if len(created.Topics) != 3 {
		t.Fatalf("topics = %d, want 3 (signature, token, variant)", len(created.Topics))
	}
	if created.Topics[1] != addrKey(token) {
		t.Fatalf("indexed token = %x, want %x", created.Topics[1], addrKey(token))
	}
	if created.Topics[2] != wU8(b20VariantStablecoin) {
		t.Fatalf("indexed variant = %x, want STABLECOIN", created.Topics[2])
	}

	// Data is (name, symbol, decimals, variantEventParams); the last carries
	// the versioned currency struct for a Stablecoin.
	wantParams := abiEncodeStruct(abiWord(wU8(b20ParamsVersion)), abiString("USD"))
	wantData := encodeTuple(
		abiString("Dollar Coin"), abiString("USDX"), abiWord(wU8(6)), abiBytes(wantParams),
	)
	if !bytes.Equal(created.Data, wantData) {
		t.Fatalf("event data = %x\nwant                = %x", created.Data, wantData)
	}
}

// TestB20CreateParamsCanonicalEncoding decodes a params blob written out by
// hand, byte for byte, as `abi.encode(B20AssetCreateParams{...})` would produce
// it. The other tests build their input with the same helper the expected
// output uses, so a shared mistake in that helper would pass unnoticed; this
// vector is independent of it.
func TestB20CreateParamsCanonicalEncoding(t *testing.T) {
	statedb, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad3111")

	word := func(v uint64) []byte { return u256hash(v).Bytes() }
	var blob []byte
	blob = append(blob, word(0x20)...)             // w0 outer offset
	blob = append(blob, word(1)...)                // w1 version
	blob = append(blob, word(0xa0)...)             // w2 name offset
	blob = append(blob, word(0xe0)...)             // w3 symbol offset
	blob = append(blob, addrKey(admin).Bytes()...) // w4 initialAdmin
	blob = append(blob, word(18)...)               // w5 decimals
	blob = append(blob, word(1)...)                // w6 name length
	blob = append(blob, rightPad32([]byte("A"))...)
	blob = append(blob, word(1)...) // w8 symbol length
	blob = append(blob, rightPad32([]byte("B"))...)

	// The helper must agree with the hand-written vector; if it does not, every
	// other params test is measuring the helper against itself.
	if got := b20AssetParams("A", "B", admin, 18); !bytes.Equal(got, blob) {
		t.Fatalf("b20AssetParams disagrees with the canonical encoding:\n got %x\nwant %x", got, blob)
	}

	ret, _, err := evm.Call(admin, B20FactoryAddress,
		encodeCreateB20WithParams(b20VariantAsset, common.HexToHash("0xcafe"), blob, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20 with a canonically encoded blob: %v", err)
	}
	view := newUnmeteredB20Storage(statedb, common.BytesToAddress(ret))
	if view.name() != "A" || view.symbol() != "B" {
		t.Fatalf("name/symbol = %q/%q, want A/B", view.name(), view.symbol())
	}
}

// TestB20CreateParamsRejectsMalformed covers the decoder's strictness: dirty
// high bits in a uint8 field or an out-of-range offset are malformed
// encodings, reported as a bare revert rather than decoded into something
// plausible.
func TestB20CreateParamsRejectsMalformed(t *testing.T) {
	_, evm := newB20EVM(t)
	admin := common.HexToAddress("0xad3111")
	call := func(salt common.Hash, blob []byte) error {
		_, _, err := evm.Call(admin, B20FactoryAddress,
			encodeCreateB20WithParams(b20VariantAsset, salt, blob, nil),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return err
	}

	// A version word carrying dirty high bits.
	dirty := b20AssetParams("A", "B", admin, 18)
	dirty[32] = 0xff // first byte of the version word, inside the struct
	if err := call(common.HexToHash("0x1"), dirty); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("dirty version word err = %v, want revert", err)
	}

	// An outer offset pointing past the end of the blob.
	bad := b20AssetParams("A", "B", admin, 18)
	copy(bad[:32], u256hash(uint64(len(bad)+32)).Bytes())
	if err := call(common.HexToHash("0x2"), bad); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("out-of-range offset err = %v, want revert", err)
	}

	// A dirty length word on a dynamic field. Truncating it to uint64 would
	// silently read a zero-length name; it must fail the decode instead.
	// The name length sits at the start of the first string tail: outer offset
	// (1 word) + 5 struct head words = word 6.
	dirtyLen := b20AssetParams("A", "B", admin, 18)
	dirtyLen[6*32] = 0x01 // high byte of the length word
	ret, _, err := evm.Call(admin, B20FactoryAddress,
		encodeCreateB20WithParams(b20VariantAsset, common.HexToHash("0x3"), dirtyLen, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("dirty length word err = %v, want revert", err)
	}
	// A malformed encoding reverts bare, the way an ABI decode failure does —
	// it is not a business-rule error and carries no typed payload.
	if len(ret) != 0 {
		t.Fatalf("malformed encoding revert data = %x, want empty", ret)
	}
}

// TestB20FieldValidationPrecedesOccupancy pins base-std's precedence: an invalid
// currency is reported as such even when the salt in the same call is already
// taken, because every field is validated before the address is derived
// (IB20Factory.createB20's documented order puts MissingRequiredField and
// InvalidCurrency ahead of TokenAlreadyExists).
func TestB20FieldValidationPrecedesOccupancy(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdup)")
	call := func(salt common.Hash, currency string) ([]byte, error) {
		params := b20StablecoinParams("N", "S", creator, currency)
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20WithParams(b20VariantStablecoin, salt, params, nil),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	salt := common.HexToHash("0x5a17")

	if _, err := call(salt, "USD"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Same salt AND an invalid currency: the currency is what is reported.
	ret, err := call(salt, "usd")
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("duplicate salt with a bad currency: err = %v, want revert", err)
	}
	if len(ret) < 4 || [4]byte(ret[:4]) != errSelInvalidCurrency {
		t.Fatalf("revert selector = %x, want InvalidCurrency %x", ret[:min(4, len(ret))], errSelInvalidCurrency)
	}
	// An empty one likewise outranks the duplicate salt.
	ret, err = call(salt, "")
	if len(ret) < 4 || [4]byte(ret[:4]) != errSelMissingField {
		t.Fatalf("empty currency: selector = %x, want MissingRequiredField %x",
			ret[:min(4, len(ret))], errSelMissingField)
	}
	// And with a valid currency the duplicate salt is what binds, so the cases
	// above had two failing conditions rather than one.
	ret, err = call(salt, "EUR")
	if len(ret) < 4 || [4]byte(ret[:4]) != errSelTokenExists {
		t.Fatalf("valid currency, duplicate salt: selector = %x, want TokenAlreadyExists %x",
			ret[:min(4, len(ret))], errSelTokenExists)
	}
}

// TestB20OutOfEnumVariantPanics covers the variant word no enum member claims.
func TestB20OutOfEnumVariantPanics(t *testing.T) {
	_, evm := newB20EVM(t)
	caller := common.HexToAddress("0xca11e4")
	wantData, _ := finishB20(nil, revPanic(0x21))

	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"createB20", encodeCreateB20WithParams(0x02, common.HexToHash("0xbv"),
			b20AssetParams("T", "T", caller, 18), nil)},
		{"getB20Address", b20Call(selGetB20Address, u256hash(2), addrKey(caller), common.Hash{})},
	} {
		ret, _, err := evm.Call(caller, B20FactoryAddress, tc.input,
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s with variant 2: err = %v, want a revert", tc.name, err)
		}
		if !bytes.Equal(ret, wantData) {
			t.Errorf("%s with variant 2: returndata = %x, want Panic(0x21) = %x. Both entry "+
				"points must decode the variant identically", tc.name, ret, wantData)
		}
	}

	// And a word inside the enum still routes, so the bound is not simply refusing
	// everything.
	ret, _, err := evm.Call(caller, B20FactoryAddress,
		b20Call(selGetB20Address, u256hash(uint64(b20VariantStablecoin)), addrKey(caller), common.Hash{}),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("getB20Address for the stablecoin variant: %v", err)
	}
	if want := addrKey(b20DeriveAddress(b20VariantStablecoin, caller, common.Hash{})); !bytes.Equal(ret, want.Bytes()) {
		t.Errorf("getB20Address = %x, want %x", ret, want)
	}
}
