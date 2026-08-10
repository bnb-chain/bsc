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
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// B20 Asset (RWA) variant extensions: the net-asset-value multiplier (rebase),
// decimals held in extension storage, batch minting and the OPERATOR role.
// announce/extraMetadata are follow-ups.
//
// TODO: verify the extension namespace and decimals param against base-std;
// decimals defaults to 18 until createB20 carries the param.

const b20AssetNamespace = "bsc.b20.asset"

const (
	b20AssetSlotDecimals      = 0
	b20AssetSlotMultiplier    = 1
	b20AssetSlotAnnouncements = 2 // mapping(bytes32 id => bool used)
	b20AssetSlotExtraMeta     = 3 // mapping(string key => string value)
)

var (
	b20AssetRoot = erc7201Root(b20AssetNamespace)
	// b20WAD is the multiplier fixed-point base (1e18 = 1.0x).
	b20WAD = uint256.NewInt(1_000_000_000_000_000_000)

	roleOperator = crypto.Keccak256Hash([]byte("OPERATOR_ROLE"))

	selMultiplier       = selector("multiplier()")
	selWadPrecision     = selector("WAD_PRECISION()")
	selScaledBalanceOf  = selector("scaledBalanceOf(address)")
	selToScaledBalance  = selector("toScaledBalance(uint256)")
	selToRawBalance     = selector("toRawBalance(uint256)")
	selUpdateMultiplier = selector("updateMultiplier(uint256)")
	selOperatorRole     = selector("OPERATOR_ROLE()")
	selBatchMint        = selector("batchMint(address[],uint256[])")

	selAnnounce             = selector("announce(bytes[],uint256,string,string)")
	selIsAnnouncementIdUsed = selector("isAnnouncementIdUsed(uint256)")

	selExtraMetadata       = selector("extraMetadata(string)")
	selUpdateExtraMetadata = selector("updateExtraMetadata(string,string)")

	b20TopicMultiplierUpdated    = eventTopic("MultiplierUpdated(uint256)")
	b20TopicAnnouncement         = eventTopic("Announcement(address,uint256,string,string)")
	b20TopicEndAnnouncement      = eventTopic("EndAnnouncement(uint256)")
	b20TopicExtraMetadataUpdated = eventTopic("ExtraMetadataUpdated(string,string)")
)

// assetExt is a gas-metered view over the Asset extension storage.
type assetExt struct{ s b20Storage }

func newAssetExt(ctx *PrecompileContext) assetExt { return assetExt{s: newMeteredB20Storage(ctx)} }

func assetSlot(offset uint64) common.Hash { return offsetSlot(b20AssetRoot, offset) }

func (e assetExt) decimals() uint8 {
	return uint8(new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotDecimals)).Bytes()).Uint64())
}
func (e assetExt) setDecimals(d uint8) {
	e.s.setWord(assetSlot(b20AssetSlotDecimals), common.Hash(uint256.NewInt(uint64(d)).Bytes32()))
}
func (e assetExt) multiplier() *uint256.Int {
	return new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotMultiplier)).Bytes())
}
func (e assetExt) setMultiplier(m *uint256.Int) {
	e.s.setWord(assetSlot(b20AssetSlotMultiplier), common.Hash(m.Bytes32()))
}
func (e assetExt) announcementUsed(id common.Hash) bool {
	return e.s.getWord(e.s.mapSlot(assetSlot(b20AssetSlotAnnouncements), id)) != (common.Hash{})
}
func (e assetExt) markAnnouncement(id common.Hash) {
	var one common.Hash
	one[31] = 1
	e.s.setWord(e.s.mapSlot(assetSlot(b20AssetSlotAnnouncements), id), one)
}

// extraMetaSlot is the Solidity mapping(string=>string) value slot for key:
// keccak256(keyBytes ++ baseSlot).
func extraMetaSlot(key string) common.Hash {
	return crypto.Keccak256Hash([]byte(key), assetSlot(b20AssetSlotExtraMeta).Bytes())
}
func (e assetExt) extraMetadata(key string) string {
	return e.s.getStringAt(extraMetaSlot(key))
}
func (e assetExt) setExtraMetadata(key, value string) {
	e.s.setStringAt(extraMetaSlot(key), value)
}

// initAssetExtension seeds a new Asset token's extension storage. decimals is
// fixed at creation and never changes (BEP-702 section 4.10).
func initAssetExtension(ctx *PrecompileContext, decimals byte) {
	e := newAssetExt(ctx)
	e.setDecimals(decimals)
	e.setMultiplier(b20WAD)
}

func applyMultiplier(raw, mul *uint256.Int) (*uint256.Int, error) {
	p, overflow := new(uint256.Int).MulOverflow(raw, mul)
	if overflow {
		return nil, revPanic(0x11)
	}
	return p.Div(p, b20WAD), nil
}

func removeMultiplier(scaled, mul *uint256.Int) (*uint256.Int, error) {
	if mul.IsZero() {
		return new(uint256.Int), nil
	}
	p, overflow := new(uint256.Int).MulOverflow(scaled, b20WAD)
	if overflow {
		return nil, revPanic(0x11)
	}
	return p.Div(p, mul), nil
}

// assetDispatch routes an Asset call: extension selectors first, then the
// shared IB20 dispatch. Used by the precompile and by announce's internal
// calls (so a disclosure can bundle multiplier/batchMint updates).
func assetDispatch(tok b20Token, ext assetExt, input []byte) ([]byte, error) {
	if ret, err, ok := dispatchAsset(tok, ext, input); ok {
		return ret, err
	}
	return tok.dispatch(input)
}

// dispatchAsset handles the Asset-variant selectors, returning ok=false so the
// caller falls back to the shared IB20 dispatch.
func dispatchAsset(tok b20Token, ext assetExt, input []byte) (ret []byte, err error, ok bool) {
	if len(input) < 4 {
		return nil, nil, false
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selDecimals:
		return encU256(uint256.NewInt(uint64(ext.decimals()))), nil, true
	case selMultiplier:
		return encU256(ext.multiplier()), nil, true
	case selWadPrecision:
		return encU256(b20WAD), nil, true
	case selOperatorRole:
		return roleOperator.Bytes(), nil, true
	case selScaledBalanceOf:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := applyMultiplier(tok.s.balanceOf(a), ext.multiplier())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selToScaledBalance:
		raw, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := applyMultiplier(raw, ext.multiplier())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selToRawBalance:
		scaled, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := removeMultiplier(scaled, ext.multiplier())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selUpdateMultiplier:
		m, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, updateMultiplier(tok, ext, m), true
	case selBatchMint:
		return nil, batchMint(tok, args), true
	case selIsAnnouncementIdUsed:
		id, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encBool(ext.announcementUsed(id)), nil, true
	case selAnnounce:
		return nil, announce(tok, ext, args), true
	case selExtraMetadata:
		key, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encString(ext.extraMetadata(key)), nil, true
	case selUpdateExtraMetadata:
		key, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		value, err := readStringArg(args, 1)
		if err != nil {
			return nil, err, true
		}
		return nil, updateExtraMetadata(tok, ext, key, value), true
	}
	return nil, nil, false
}

// updateExtraMetadata writes/clears (value == "") a custom metadata entry.
func updateExtraMetadata(tok b20Token, ext assetExt, key, value string) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleMetadata); err != nil {
		return err
	}
	if len(key) == 0 {
		return revB20("InvalidMetadataKey()", errSelInvalidMetadataKey)
	}
	ext.setExtraMetadata(key, value)
	// TODO: carry key/value in ExtraMetadataUpdated event data (base-std align).
	tok.ctx.AddLog([]common.Hash{b20TopicExtraMetadataUpdated}, nil)
	return nil
}

// readStringArg decodes an ABI string argument at head word argIndex.
func readStringArg(args []byte, argIndex int) (string, error) {
	L := uint64(len(args))
	off, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || off > L || L-off < 32 {
		return "", ErrExecutionReverted
	}
	n, ok2 := wordU64(args, off)
	if !ok2 {
		return "", ErrExecutionReverted // malformed length word
	}
	dataPos := off + 32
	if n > L-dataPos {
		return "", ErrExecutionReverted
	}
	return string(args[dataPos : dataPos+n]), nil
}

// announce publishes a disclosure and atomically runs a bundle of internal
// calls against this token, preserving the caller's identity (role checks
// still apply). Any failure — malformed call, re-entrant announce, or a
// reverting internal call — rolls back the whole disclosure.
func announce(tok b20Token, ext assetExt, args []byte) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if tok.inAnnounce {
		return revB20("AnnouncementInProgress()", errSelAnnounceInProgress)
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	calls, err := readBytesArray(args, 0)
	if err != nil {
		return err
	}
	id, err := readWord(args, 1)
	if err != nil {
		return err
	}
	// args 2,3 (description, uri strings) are carried only by the event.
	if ext.announcementUsed(id) {
		return revB20("AnnouncementIdAlreadyUsed(uint256)", errSelAnnounceIdUsed, id)
	}
	ext.markAnnouncement(id) // marked before execution
	// TODO: carry description/uri in the Announcement event data (base-std align).
	tok.ctx.AddLog([]common.Hash{b20TopicAnnouncement, addrKey(tok.ctx.Caller), id}, nil)

	tok.inAnnounce = true // threaded into the internal calls below by value
	for _, c := range calls {
		if len(c) < 4 {
			return revB20Bytes("InternalCallMalformed(bytes)", errSelInternalMalformed, c)
		}
		if _, err := assetDispatch(tok, ext, c); err != nil {
			return revB20Bytes("InternalCallFailed(bytes)", errSelInternalFailed, c)
		}
	}
	tok.ctx.AddLog([]common.Hash{b20TopicEndAnnouncement, id}, nil)
	return nil
}

func updateMultiplier(tok b20Token, ext assetExt, newMul *uint256.Int) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	if newMul.IsZero() || newMul.Gt(b20NoSupplyCap) { // (0, type(uint128).max]
		return revB20("InvalidMultiplier()", errSelInvalidMultiplier)
	}
	ext.setMultiplier(newMul)
	mb := newMul.Bytes32()
	tok.ctx.AddLog([]common.Hash{b20TopicMultiplierUpdated}, mb[:])
	return nil
}

func batchMint(tok b20Token, args []byte) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if tok.isPaused(b20PauseMint) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseMint))
	}
	if err := tok.ensureRole(roleMint); err != nil {
		return err
	}
	recipients, err := readWordArray(args, 0)
	if err != nil {
		return err
	}
	amounts, err := readWordArray(args, 1)
	if err != nil {
		return err
	}
	if len(recipients) != len(amounts) {
		return revB20("LengthMismatch(uint256,uint256)", errSelLengthMismatch,
			wU64(uint64(len(recipients))), wU64(uint64(len(amounts))))
	}
	if len(recipients) == 0 {
		return revB20("EmptyBatch()", errSelEmptyBatch)
	}
	for i := range recipients {
		to := common.BytesToAddress(recipients[i].Bytes())
		amount := new(uint256.Int).SetBytes(amounts[i].Bytes())
		if err := tok.mintCore(to, amount); err != nil {
			return err
		}
	}
	return nil
}

// readWordArray decodes an ABI dynamic array of 32-byte words (address[] /
// uint256[]) at head word argIndex.
func readWordArray(args []byte, argIndex int) ([]common.Hash, error) {
	L := uint64(len(args))
	base, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || base > L || L-base < 32 {
		return nil, ErrExecutionReverted
	}
	n, ok2 := wordU64(args, base)
	if !ok2 {
		return nil, ErrExecutionReverted // malformed length word
	}
	dataPos := base + 32
	if n > (L-dataPos)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([]common.Hash, n)
	for i := uint64(0); i < n; i++ {
		out[i] = common.BytesToHash(args[dataPos+i*32 : dataPos+i*32+32])
	}
	return out, nil
}
