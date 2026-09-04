package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// cas20ParamsVersion is the encoding version every create-params struct carries
// as its leading field. A struct that does not match is rejected before any
// field is looked at, so a version error always takes precedence.
const cas20ParamsVersion = 1

// Asset decimals bounds (BEP-702 section 4.10).
const (
	cas20MinDecimals = 6
	cas20MaxDecimals = 18
)

var (
	cas20TopicCAS20Created = eventTopic("CAS20Created(address,uint8,string,string,uint8,bytes)")

	selCreateCAS20        = selector("createCAS20(uint8,bytes32,bytes,bytes[])")
	selGetCAS20Address    = selector("getCAS20Address(uint8,address,bytes32)")
	selIsCAS20            = selector("isCAS20(address)")
	selVariantOf          = selector("variantOf(address)")
	selIsCAS20Initialized = selector("isCAS20Initialized(address)")
)

// cas20VariantRecognized reports whether this variant reaches a handler.
func cas20VariantRecognized(variant byte) bool {
	_, ok := cas20Variants[variant]
	return ok
}

// CAS20MarkerCode marks an initialized token, keeps the account clear of EIP-161
// reaping, and uses EIP-3541's reserved 0xEF prefix so no deployment can forge
// it (BEP-702 3.16). It is never executed. Exported so a genesis that starts
// after the fork can pre-deploy it on the registries, the way the other system
// accounts are.
var CAS20MarkerCode = []byte{0xEF}

// cas20NoSupplyCap is the "unlimited" sentinel: type(uint128).max.
var cas20NoSupplyCap = new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))

// cas20DeriveAddress computes a token's deterministic address:
// 0xCA50 ++ 8×0x00 ++ variant ++ keccak256(abi.encode(creator, salt))[:9].
func cas20DeriveAddress(variant byte, creator common.Address, salt common.Hash) common.Address {
	h := crypto.Keccak256(common.LeftPadBytes(creator.Bytes(), 32), salt.Bytes())
	var a common.Address
	a[0], a[1] = cas20MarkerPrefix[0], cas20MarkerPrefix[1]
	a[10] = variant
	copy(a[11:20], h[:9])
	return a
}

func runCAS20Factory(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selGetCAS20Address:
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
		// Decoded exactly as createCAS20 decodes the same argument. Prediction is
		// only meaningful if the two agree on what the input means: truncating
		// variant[31] answered for encodings creation rejects, and named
		// addresses in unroutable variant spaces.
		if !isEnumWord(variant, cas20VariantMax) {
			return nil, ErrExecutionReverted
		}
		if !ctx.chargeKeccak(64) {
			return nil, ErrOutOfGas
		}
		addr := cas20DeriveAddress(variant[31], sender, salt)
		return addrKey(addr).Bytes(), nil
	case selIsCAS20:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsCAS20Address(a)), nil
	case selVariantOf:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		// Unlike isCAS20, this one validates the variant byte: the return type is
		// an enum, so naming an unrecognized variant would hand the caller a
		// value its own ABI decoder rejects.
		if !IsCAS20Address(a) || !cas20VariantRecognized(a[10]) {
			return nil, revCAS20("InvalidVariant()", errSelInvalidVariant)
		}
		return wU8(a[10]).Bytes(), nil
	case selIsCAS20Initialized:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsCAS20Address(a) && cas20InitializedMetered(ctx, a)), nil
	case selCreateCAS20:
		return createCAS20(ctx, args)
	}
	return nil, ErrExecutionReverted
}

func createCAS20(ctx *PrecompileContext, args []byte) ([]byte, error) {
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

	// Order follows BEP-702 3.4: the variant is resolved and its
	// feature gate applied before the variant-specific params blob is decoded, so
	// a closed feature is reported as such whatever the payload.
	if !isEnumWord(variantWord, cas20VariantMax) {
		return nil, ErrExecutionReverted
	}
	variant := variantWord[31]
	feature, ok := variantFeature(variant)
	if !ok {
		return nil, revCAS20("InvalidVariant()", errSelInvalidVariant)
	}
	if err := ensureFeatureActivated(ctx, feature); err != nil {
		return nil, err
	}
	create, err := decodeCreateParams(variant, params)
	if err != nil {
		return nil, err
	}
	// Every field is validated before the address is derived, so a malformed
	// currency is reported as such even when the salt is also taken.
	if variant == cas20VariantStablecoin {
		if err := validateCurrency(create.currency); err != nil {
			return nil, err
		}
	}
	creator := ctx.Caller
	if !ctx.chargeKeccak(64) {
		return nil, ErrOutOfGas
	}
	addr := cas20DeriveAddress(variant, creator, salt)

	if cas20AddressOccupied(ctx, addr) {
		return nil, revCAS20("TokenAlreadyExists(address)", errSelTokenExists, addrKey(addr))
	}
	if !ctx.chargeCodeWrite(addr, CAS20MarkerCode) {
		return nil, ErrOutOfGas
	}
	ctx.StateDB.SetCode(addr, CAS20MarkerCode, tracing.CodeChangeContractCreation)

	// Bootstrap context/token bound to the new address, privileged so initCalls
	// can grant roles and mint before any role holder exists.
	decimals := create.decimals
	tokenCtx := ctx.spawnBootstrap(addr, creator)
	tok := newCAS20TokenBootstrap(tokenCtx, decimals)

	// Initial state: metadata, no supply cap, and the variant's own storage.
	if !tok.s.setName(create.name) || !tok.s.setSymbol(create.symbol) {
		return nil, ErrOutOfGas
	}
	tok.s.setSupplyCap(cas20NoSupplyCap)
	if variant == cas20VariantAsset {
		initAssetExtension(tokenCtx, create.decimals)
	} else if !newStablecoinExt(tokenCtx).setCurrency(create.currency) {
		return nil, ErrOutOfGas
	}
	initialAdmin := create.initialAdmin
	if initialAdmin != (common.Address{}) {
		tok.s.setRole(roleDefaultAdmin, initialAdmin, true)
		tok.s.setAdminCount(uint256.NewInt(1))
		if !tokenCtx.AddLog([]common.Hash{cas20TopicRoleGranted, roleDefaultAdmin, addrKey(initialAdmin), addrKey(creator)}, nil) {
			return nil, ErrOutOfGas
		}
	}

	// Each entry runs on the variant's full dispatcher, not the shared half: an
	// Asset token has to be able to set its multiplier or batch its first
	// distribution at creation. The bootstrap dispatched tok.dispatch directly
	// from the commit that introduced it, when no variant layer existed yet, and
	// was not revisited when one did.
	dispatch := func(call []byte) ([]byte, error) { return stablecoinDispatch(tok, newStablecoinExt(tokenCtx), call) }
	if variant == cas20VariantAsset {
		dispatch = func(call []byte) ([]byte, error) { return assetDispatch(tok, newAssetExt(tokenCtx), call) }
	}

	// Privileged bootstrap: any initCall failure reverts the whole creation.
	for i, call := range initCalls {
		// Same shape as announce's bundle: each entry dispatches a full token
		// call, so without this the whole array runs on an exhausted budget.
		if ctx.OutOfGas() {
			return nil, ErrOutOfGas
		}
		if len(call) < 4 {
			return nil, revCAS20Bytes("InternalCallMalformed(bytes)", errSelInternalMalformed, call)
		}
		if _, err := dispatch(call); err != nil {
			if _, isRev := err.(*cas20RevertError); !isRev && err != ErrExecutionReverted {
				return nil, err // out-of-gas / write-protection propagate as-is
			}
			return nil, revCAS20("InitCallFailed(uint256)", errSelInitCallFailed, wU64(uint64(i)))
		}
	}
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	// CAS20Created is emitted by the factory, not the token, so an indexer can
	// follow creation from one address. variantEventParams carries the
	// variant's immutable identity data: empty for Asset, the versioned
	// currency struct for Stablecoin.
	if !ctx.AddLog(
		[]common.Hash{cas20TopicCAS20Created, addrKey(addr), wU8(variant)},
		encodeCAS20CreatedData(create),
	) {
		return nil, ErrOutOfGas
	}
	return addrKey(addr).Bytes(), nil
}

type cas20CreateParams struct {
	variant      byte
	name         string
	symbol       string
	initialAdmin common.Address
	decimals     byte
	currency     string // Stablecoin only
}

// decodeCreateParams decodes and validates the variant's create-params struct.
// The version check precedes every field check, so an unsupported encoding is
// always reported as such.
func decodeCreateParams(variant byte, params []byte) (cas20CreateParams, error) {
	out := cas20CreateParams{variant: variant}

	// abi.encode of a single dynamic struct wraps it in a one-element tuple, so
	// the blob opens with an offset to the struct's own encoding rather than
	// with its first field. Read through it before touching any field.
	off, ok := wordU64(params, 0)
	if !ok || off > uint64(len(params)) {
		return out, ErrExecutionReverted // malformed encoding
	}
	body := params[off:]

	// A uint8 field with dirty high bits is a malformed encoding, which the
	// decode reports as such: version and decimals are plain integers, so their
	// range is a field check rather than a decode failure.
	version, err := readStrictUint8(body, 0)
	if err != nil {
		return out, err
	}
	if version != cas20ParamsVersion {
		return out, revCAS20("UnsupportedVersion(uint8,uint8)", errSelUnsupportedVersion,
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

	if variant == cas20VariantAsset {
		if out.decimals, err = readStrictUint8(body, 4); err != nil {
			return out, err
		}
		if out.decimals < cas20MinDecimals || out.decimals > cas20MaxDecimals {
			return out, revCAS20("InvalidDecimals(uint8)", errSelInvalidDecimals, wU8(out.decimals))
		}
		return out, nil
	}

	// Stablecoin: decimals are fixed and not carried on the wire. The currency's
	// content is checked by the caller before the address is derived, so a
	// malformed one is reported ahead of TokenAlreadyExists.
	out.decimals = 6
	if out.currency, err = readStringArg(body, 4); err != nil {
		return out, err
	}
	return out, nil
}

// validateCurrency is the Stablecoin content check. Its caller runs it before
// deriving the address, so it reports ahead of TokenAlreadyExists.
func validateCurrency(code string) error {
	if code == "" {
		return revCAS20Bytes("MissingRequiredField(string)", errSelMissingField, []byte("currency"))
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return revCAS20Bytes("InvalidCurrency(string)", errSelInvalidCurrency, []byte(code))
		}
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
	if !isEnumWord(w, 0xff) {
		return 0, ErrExecutionReverted // malformed encoding
	}
	return w[31], nil
}

// encodeCAS20CreatedData ABI-encodes the non-indexed fields of CAS20Created:
// (string name, string symbol, uint8 decimals, bytes variantEventParams).
func encodeCAS20CreatedData(c cas20CreateParams) []byte {
	var variantParams []byte
	if c.variant == cas20VariantStablecoin {
		// abi.encode(CAS20StablecoinEventParams{version, currency}) — a single
		// dynamic struct, so it carries the same outer offset wrapper.
		variantParams = abiEncodeStruct(
			abiWord(wU8(cas20ParamsVersion)),
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
