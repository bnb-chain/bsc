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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// B20Factory: the singleton createB20 entry point plus the address-prediction
// and identity views. Token creation writes the initial core storage and an
// initialization marker, then runs initCalls in a privileged bootstrap window
// (role and transfer-side policy gates skipped) so a token can be fully
// configured — roles granted, initial supply minted — in one transaction.
//
// TODO: verify the address preimage against base-std before golden testing.

// b20ParamsVersion is the encoding version every create-params struct carries
// as its leading field. A struct that does not match is rejected before any
// field is looked at, so a version error always takes precedence.
const b20ParamsVersion = 1

// Asset decimals bounds (BEP-702 section 4.10).
const (
	b20MinDecimals = 6
	b20MaxDecimals = 18
)

var (
	b20TopicB20Created = eventTopic("B20Created(address,uint8,string,string,uint8,bytes)")

	selCreateB20        = selector("createB20(uint8,bytes32,bytes,bytes[])")
	selGetB20Address    = selector("getB20Address(uint8,address,bytes32)")
	selIsB20            = selector("isB20(address)")
	selIsB20Initialized = selector("isB20Initialized(address)")
)

// b20MarkerCode is the account sentinel written to a token address on creation
// (BEP-702 §3.16). It is never executed — the precompile takes precedence in
// Call — and serves two purposes: it marks the account as an initialized B20
// token, and it keeps the account non-empty so EIP-161 end-of-block state
// clearing cannot reap it together with every balance it holds.
//
// 0xEF is the EIP-3541 reserved prefix, which no CREATE/CREATE2 deployment can
// produce, so the marker stays unforgeable even if the reserved-space guard
// were weakened.
var b20MarkerCode = []byte{0xEF}

// b20NoSupplyCap is the "unlimited" sentinel: type(uint128).max.
var b20NoSupplyCap = new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))

// b20DeriveAddress computes a token's deterministic address:
// 0x20B0 ++ 8×0x00 ++ variant ++ keccak256(creator ++ salt)[:9].
//
// TODO: verify the preimage (encoding/ordering of creator and salt) against
// base-std; routing does not depend on it, but cross-client determinism does.
func b20DeriveAddress(variant byte, creator common.Address, salt common.Hash) common.Address {
	h := crypto.Keccak256(creator.Bytes(), salt.Bytes())
	var a common.Address
	a[0], a[1] = b20MarkerPrefix[0], b20MarkerPrefix[1]
	a[10] = variant
	copy(a[11:20], h[:9])
	return a
}

func runB20Factory(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selGetB20Address:
		variant, err := readWord(args, 0)
		if err != nil {
			return nil, err
		}
		sender, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		salt, err := readWord(args, 2)
		if err != nil {
			return nil, err
		}
		addr := b20DeriveAddress(variant[31], sender, salt)
		return addrKey(addr).Bytes(), nil
	case selIsB20:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsB20Address(a)), nil
	case selIsB20Initialized:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsB20Address(a) && b20InitializedMetered(ctx, a)), nil
	case selCreateB20:
		return createB20(ctx, args)
	}
	return nil, ErrExecutionReverted
}

func createB20(ctx *PrecompileContext, args []byte) ([]byte, error) {
	if ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	variantWord, err := readWord(args, 0)
	if err != nil {
		return nil, err
	}
	salt, err := readWord(args, 1)
	if err != nil {
		return nil, err
	}
	params, err := readBytesArg(args, 2)
	if err != nil {
		return nil, err
	}
	initCalls, err := readBytesArray(args, 3)
	if err != nil {
		return nil, err
	}

	// Order follows BEP-702 section 3.4, and base-std: the variant is resolved
	// and its feature gate applied before the variant-specific params blob is
	// decoded, so a closed feature is reported as such whatever the payload.
	if !isEnumWord(variantWord, b20VariantStablecoin) {
		return nil, revB20("InvalidVariant()", errSelInvalidVariant)
	}
	variant := variantWord[31]
	feature, ok := variantFeature(variant)
	if !ok {
		return nil, revB20("InvalidVariant()", errSelInvalidVariant)
	}
	if err := ensureFeatureActivated(ctx, feature); err != nil {
		return nil, err
	}
	create, err := decodeCreateParams(variant, params)
	if err != nil {
		return nil, err
	}
	creator := ctx.Caller
	addr := b20DeriveAddress(variant, creator, salt)

	if b20AddressOccupied(ctx, addr) {
		return nil, revB20("TokenAlreadyExists(address)", errSelTokenExists, addrKey(addr))
	}
	ctx.chargeCodeWrite(addr, b20MarkerCode)
	ctx.StateDB.SetCode(addr, b20MarkerCode, tracing.CodeChangeContractCreation)

	// Bootstrap context/token bound to the new address, privileged so initCalls
	// can grant roles and mint before any role holder exists.
	decimals := create.decimals
	tokenCtx := ctx.spawnBootstrap(addr, creator)
	tok := newB20TokenBootstrap(tokenCtx, decimals)

	// Initial state: metadata, no supply cap, and the variant's own storage.
	tok.s.setName(create.name)
	tok.s.setSymbol(create.symbol)
	tok.s.setSupplyCap(b20NoSupplyCap)
	if variant == b20VariantAsset {
		initAssetExtension(tokenCtx, create.decimals)
	} else {
		if err := validateCurrency(create.currency); err != nil {
			return nil, err
		}
		newStablecoinExt(tokenCtx).setCurrency(create.currency)
	}
	initialAdmin := create.initialAdmin
	if initialAdmin != (common.Address{}) {
		tok.s.setRole(roleDefaultAdmin, initialAdmin, true)
		tok.s.setAdminCount(uint256.NewInt(1))
		tokenCtx.AddLog([]common.Hash{b20TopicRoleGranted, roleDefaultAdmin, addrKey(initialAdmin), addrKey(creator)}, nil)
	}

	// Privileged bootstrap: any initCall failure reverts the whole creation.
	for i, call := range initCalls {
		if len(call) < 4 {
			return nil, revB20Bytes("InternalCallMalformed(bytes)", errSelInternalMalformed, call)
		}
		if _, err := tok.dispatch(call); err != nil {
			if _, isRev := err.(*b20RevertError); !isRev && err != ErrExecutionReverted {
				return nil, err // out-of-gas / write-protection propagate as-is
			}
			return nil, revB20("InitCallFailed(uint256)", errSelInitCallFailed, wU64(uint64(i)))
		}
	}
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	// B20Created is emitted by the factory, not the token, so an indexer can
	// follow creation from one address. variantEventParams carries the
	// variant's immutable identity data: empty for Asset, the versioned
	// currency struct for Stablecoin.
	ctx.AddLog(
		[]common.Hash{b20TopicB20Created, addrKey(addr), wU8(variant)},
		encodeB20CreatedData(create),
	)
	return addrKey(addr).Bytes(), nil
}

// b20CreateParams is the decoded, validated content of a createB20 params blob.
// decimals is resolved for both variants: Stablecoin's is fixed at 6 rather
// than carried on the wire.
type b20CreateParams struct {
	variant      byte
	name         string
	symbol       string
	initialAdmin common.Address
	decimals     byte
	currency     string // Stablecoin only
}

// decodeCreateParams decodes and validates the variant's create-params struct.
// The version check precedes every field check, so an unsupported encoding is
// always reported as such (base-std does the same).
func decodeCreateParams(variant byte, params []byte) (b20CreateParams, error) {
	out := b20CreateParams{variant: variant}

	// abi.encode of a single dynamic struct wraps it in a one-element tuple, so
	// the blob opens with an offset to the struct's own encoding rather than
	// with its first field. Read through it before touching any field.
	off, ok := wordU64(params, 0)
	if !ok || off > uint64(len(params)) {
		return out, ErrExecutionReverted // malformed encoding
	}
	body := params[off:]

	// A uint8 field with dirty high bits is a malformed encoding, which the
	// decode reports as such — Panic(0x21) is for an out-of-range enum, and
	// version and decimals are plain integers.
	version, err := readStrictUint8(body, 0)
	if err != nil {
		return out, err
	}
	if version != b20ParamsVersion {
		return out, revB20("UnsupportedVersion(uint8,uint8)", errSelUnsupportedVersion,
			wU8(version), wU8(variant))
	}
	if out.name, err = readStringArg(body, 1); err != nil {
		return out, err
	}
	if out.symbol, err = readStringArg(body, 2); err != nil {
		return out, err
	}
	if out.initialAdmin, err = readAddress(body, 3); err != nil {
		return out, err
	}

	if variant == b20VariantAsset {
		if out.decimals, err = readStrictUint8(body, 4); err != nil {
			return out, err
		}
		if out.decimals < b20MinDecimals || out.decimals > b20MaxDecimals {
			return out, revB20("InvalidDecimals(uint8)", errSelInvalidDecimals, wU8(out.decimals))
		}
		return out, nil
	}

	// Stablecoin: decimals are fixed and not carried on the wire. The currency
	// is only decoded here; its content is checked at initialization, after the
	// occupancy check, matching where base-std validates it.
	out.decimals = 6
	if out.currency, err = readStringArg(body, 4); err != nil {
		return out, err
	}
	return out, nil
}

// validateCurrency is the Stablecoin content check, applied at initialization
// so that a duplicate salt reports TokenAlreadyExists ahead of it, as on Base.
func validateCurrency(code string) error {
	if code == "" {
		return revB20Bytes("MissingRequiredField(string)", errSelMissingField, []byte("currency"))
	}
	if !validCurrency(code) {
		return revB20Bytes("InvalidCurrency(string)", errSelInvalidCurrency, []byte(code))
	}
	return nil
}

// readStrictUint8 decodes a uint8 field: every byte above the last must be
// zero, or the encoding is malformed.
func readStrictUint8(args []byte, i int) (byte, error) {
	w, err := readWord(args, i)
	if err != nil {
		return 0, err
	}
	for _, b := range w[:31] {
		if b != 0 {
			return 0, ErrExecutionReverted // malformed encoding
		}
	}
	return w[31], nil
}

// encodeB20CreatedData ABI-encodes the non-indexed fields of B20Created:
// (string name, string symbol, uint8 decimals, bytes variantEventParams).
func encodeB20CreatedData(c b20CreateParams) []byte {
	var variantParams []byte
	if c.variant == b20VariantStablecoin {
		// abi.encode(B20StablecoinEventParams{version, currency}) — a single
		// dynamic struct, so it carries the same outer offset wrapper.
		variantParams = abiEncodeStruct(
			abiWord(wU8(b20ParamsVersion)),
			abiString(c.currency),
		)
	}
	return encodeTuple(
		abiString(c.name),
		abiString(c.symbol),
		abiWord(wU8(c.decimals)),
		abiBytes(variantParams),
	)
}

// readBytesArray decodes an ABI `bytes[]` argument at head word argIndex.
func readBytesArray(args []byte, argIndex int) ([][]byte, error) {
	L := uint64(len(args))
	base, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || base > L || L-base < 32 {
		return nil, ErrExecutionReverted
	}
	n, ok2 := wordU64(args, base)
	if !ok2 {
		return nil, ErrExecutionReverted // malformed length word
	}
	arrData := base + 32
	if n > (L-arrData)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([][]byte, n)
	for i := uint64(0); i < n; i++ {
		elemOff, ok2 := wordU64(args, arrData+i*32)
		if !ok2 {
			return nil, ErrExecutionReverted // malformed element offset
		}
		pos := arrData + elemOff
		if elemOff > L-arrData || pos > L || L-pos < 32 {
			return nil, ErrExecutionReverted
		}
		elemLen, ok3 := wordU64(args, pos)
		if !ok3 {
			return nil, ErrExecutionReverted // malformed element length
		}
		start := pos + 32
		if elemLen > L-start {
			return nil, ErrExecutionReverted
		}
		out[i] = args[start : start+elemLen]
	}
	return out, nil
}

// wordU64 reads the 32-byte word at byte position pos as a uint64. It reports
// false when the word is out of range or does not fit a uint64: an offset or
// length with dirty high bits is a malformed encoding, not a large number to
// be truncated into something plausible.
func wordU64(args []byte, pos uint64) (uint64, bool) {
	if pos > uint64(len(args)) || uint64(len(args))-pos < 32 {
		return 0, false
	}
	for _, b := range args[pos : pos+24] {
		if b != 0 {
			return 0, false
		}
	}
	return new(uint256.Int).SetBytes(args[pos+24 : pos+32]).Uint64(), true
}
