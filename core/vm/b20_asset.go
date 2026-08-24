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

// The Asset variant keeps its state in its own ERC-7201 namespace, disjoint
// from the core one, so the extension composes without touching the shared
// layout.
const b20AssetNamespace = "bsc.b20.asset"

const (
	b20AssetSlotDecimals      = 0
	b20AssetSlotMultiplier    = 1
	b20AssetSlotAnnouncements = 2 // mapping(string id => bool used)
	b20AssetSlotExtraMeta     = 3 // mapping(string key => string value)
	b20AssetSlotPending       = 4 // packed: multiplier (u128) | effectiveAt (u64)
)

// Packed lane offsets in the pending slot, LSB-first as Solidity packs a struct
// of {uint128 multiplier; uint64 effectiveAt}.
const (
	b20PendingMulBits  = 0
	b20PendingWhenBits = 128
)

var (
	b20AssetRoot = erc7201Root(b20AssetNamespace)
	// b20WAD is the multiplier fixed-point base (1e18 = 1.0x).
	b20WAD = uint256.NewInt(1_000_000_000_000_000_000)
	// b20U128Mask isolates the pending slot's low lane, and is also the ceiling
	// every multiplier is bounded by (type(uint128).max).
	b20U64Max   = new(uint256.Int).SetUint64(^uint64(0))
	b20U128Mask = new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))

	roleOperator = crypto.Keccak256Hash([]byte("OPERATOR_ROLE"))

	selMultiplier       = selector("multiplier()")
	selWadPrecision     = selector("WAD_PRECISION()")
	selScaledBalanceOf  = selector("scaledBalanceOf(address)")
	selToScaledBalance  = selector("toScaledBalance(uint256)")
	selToRawBalance     = selector("toRawBalance(uint256)")
	selUpdateMultiplier = selector("updateMultiplier(uint256)")
	selOperatorRole     = selector("OPERATOR_ROLE()")
	selBatchMint        = selector("batchMint(address[],uint256[])")

	selAnnounce             = selector("announce(bytes[],string,string,string)")
	selIsAnnouncementIdUsed = selector("isAnnouncementIdUsed(string)")

	selExtraMetadata       = selector("extraMetadata(string)")
	selUpdateExtraMetadata = selector("updateExtraMetadata(string,string)")

	// ERC-8056 (Cobalt). The canonical names alias the Beryl ones, which stay
	// dialable; updateUIMultiplier is the scheduled setter and is not a rename of
	// updateMultiplier, which remains as the instant failsafe.
	selUIMultiplier       = selector("uiMultiplier()")
	selToUIAmount         = selector("toUIAmount(uint256)")
	selFromUIAmount       = selector("fromUIAmount(uint256)")
	selBalanceOfUI        = selector("balanceOfUI(address)")
	selTotalSupplyUI      = selector("totalSupplyUI()")
	selNewUIMultiplier    = selector("newUIMultiplier()")
	selEffectiveAt        = selector("effectiveAt()")
	selUpdateUIMultiplier = selector("updateUIMultiplier(uint256,uint256)")
	selCancelUIMultiplier = selector("cancelUIMultiplierUpdate()")
	selMaxUIMultiplier    = selector("MAX_UI_MULTIPLIER()")
	selSupportsInterface  = selector("supportsInterface(bytes4)")

	b20TopicMultiplierUpdated    = eventTopic("MultiplierUpdated(uint256)")
	b20TopicAnnouncement         = eventTopic("Announcement(address,string,string,string)")
	b20TopicEndAnnouncement      = eventTopic("EndAnnouncement(string)")
	b20TopicExtraMetadataUpdated = eventTopic("ExtraMetadataUpdated(string,string)")

	b20TopicUIMultiplierUpdated   = eventTopic("UIMultiplierUpdated(uint256,uint256,uint256)")
	b20TopicUIMultiplierCancelled = eventTopic("UIMultiplierUpdateCancelled(uint256,uint256)")
)

// The ERC-165 ids the Asset variant advertises: ERC-165 itself and all four
// ERC-8056 interfaces, Conversion included — toUIAmount and fromUIAmount are its
// canonical converters and both are implemented. The advertised set is observable
// surface, so it has to match what is implemented rather than merely be a subset
// of it.
var b20AssetInterfaceIDs = map[[4]byte]bool{
	{0x01, 0xff, 0xc9, 0xa7}: true, // IERC165
	{0xa6, 0x0b, 0xf1, 0x3d}: true, // IScaledUIAmount
	{0x4b, 0xd2, 0x76, 0x48}: true, // IScaledUIAmountNewUIMultiplier
	{0xd8, 0x90, 0xfd, 0x71}: true, // IScaledUIAmountBalances
	{0x57, 0x85, 0x4f, 0xc3}: true, // IScaledUIAmountConversion
}

// assetExt is a gas-metered view over the Asset extension storage.
type assetExt struct{ s b20Storage }

func newAssetExt(ctx *PrecompileContext) assetExt { return assetExt{s: newMeteredB20Storage(ctx)} }

func assetSlot(offset uint64) common.Hash { return offsetSlot(b20AssetRoot, offset) }

func (e assetExt) decimals() uint8 {
	return uint8(new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotDecimals)).Bytes()).Uint64())
}
func (e assetExt) setDecimals(d uint8) {
	e.s.setWord(assetSlot(b20AssetSlotDecimals), uint256.NewInt(uint64(d)).Bytes32())
}
func (e assetExt) multiplier() *uint256.Int {
	return new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotMultiplier)).Bytes())
}
func (e assetExt) setMultiplier(m *uint256.Int) {
	e.s.setWord(assetSlot(b20AssetSlotMultiplier), m.Bytes32())
}

// --- ERC-8056 scheduled multiplier (Cobalt) ---------------------------------
//
// Two values decide what a holder's balance is worth: the multiplier at slot 1,
// set by the instant setter, and a pending schedule at slot 4. The *effective*
// multiplier is the pending one once its timestamp has passed, and the stored one
// otherwise — so multiplier() changes value at a timestamp, with no transaction
// and no event at the flip. That is why adopting ERC-8056 is not a pure
// addition: an indexer rebuilding state from logs alone will diverge
// (BEP-702 3.12).

func (e assetExt) pendingSlot() common.Hash { return assetSlot(b20AssetSlotPending) }

// pending reads the scheduled update. A zero effectiveAt means none is recorded;
// a non-zero one may be live or already matured.
func (e assetExt) pending() (mul *uint256.Int, effectiveAt uint64) {
	w := new(uint256.Int).SetBytes(e.s.getWord(e.pendingSlot()).Bytes())
	lane := new(uint256.Int).Rsh(w, b20PendingWhenBits)
	return new(uint256.Int).And(w, b20U128Mask), lane.Uint64()
}

func (e assetExt) setPending(mul *uint256.Int, effectiveAt uint64) {
	packed := new(uint256.Int).And(mul, b20U128Mask)
	packed.Or(packed, new(uint256.Int).Lsh(uint256.NewInt(effectiveAt), b20PendingWhenBits))
	e.s.setWord(e.pendingSlot(), packed.Bytes32())
}

func (e assetExt) clearPending() { e.s.setWord(e.pendingSlot(), common.Hash{}) }

// settleMatured folds a matured schedule into the stored multiplier. Reads compute
// the effective value and cannot write, so the fold has to happen on the next write
// that reuses the pending slot — otherwise replacing a matured schedule would
// silently revert the token to the value from before it matured.
func (e assetExt) settleMatured(now uint64) {
	if mul, at := e.pending(); at != 0 && now >= at {
		e.setMultiplier(mul)
	}
}

// effectiveMultiplier is what every conversion and balance view uses. It is the
// scheduled value once its timestamp has arrived, and the stored one until then.
func (e assetExt) effectiveMultiplier(now uint64) *uint256.Int {
	if mul, at := e.pending(); at != 0 && now >= at {
		return mul
	}
	return e.multiplier()
}

func (e assetExt) announcementSlot(id string) common.Hash {
	return e.s.strMapSlot(assetSlot(b20AssetSlotAnnouncements), id)
}

// announcementUsed reports whether the id has been announced, and whether the
// answer is real. False/false means the read could not be paid for: announce must
// stop there rather than treat an unpaid read as "unused" and go on to re-hash the
// caller-sized id and encode all three strings into two events.
func (e assetExt) announcementUsed(id string) (bool, bool) {
	w, ok := e.s.getWordChecked(e.announcementSlot(id))
	return w != (common.Hash{}), ok
}
func (e assetExt) markAnnouncement(id string) bool {
	var one common.Hash
	one[31] = 1
	return e.s.setWord(e.announcementSlot(id), one)
}

func (e assetExt) extraMetaSlot(key string) common.Hash {
	return e.s.strMapSlot(assetSlot(b20AssetSlotExtraMeta), key)
}
func (e assetExt) extraMetadata(key string) (string, bool) {
	return e.s.getStringAt(e.extraMetaSlot(key))
}
func (e assetExt) setExtraMetadata(key, value string) bool {
	return e.s.setStringAt(e.extraMetaSlot(key), value)
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

// removeMultiplier answers zero for a zero multiplier without a guard of its own:
// uint256 division by zero yields zero. updateMultiplier rejects zero anyway.
func removeMultiplier(scaled, mul *uint256.Int) (*uint256.Int, error) {
	p, overflow := new(uint256.Int).MulOverflow(scaled, b20WAD)
	if overflow {
		return nil, revPanic(0x11)
	}
	return p.Div(p, mul), nil
}

// assetDispatch tries the extension selectors first, then the shared surface.
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

	// Every conversion and balance view reads the effective multiplier, which is
	// the scheduled one once its timestamp has passed (ERC-8056).
	effective := func() *uint256.Int { return ext.effectiveMultiplier(tok.ctx.BlockTime()) }

	switch sel {
	case selDecimals:
		return encU256(uint256.NewInt(uint64(ext.decimals()))), nil, true
	case selMultiplier, selUIMultiplier:
		return encU256(effective()), nil, true
	case selWadPrecision:
		return encU256(b20WAD), nil, true
	case selMaxUIMultiplier:
		return encU256(b20U128Mask), nil, true
	case selOperatorRole:
		return roleOperator.Bytes(), nil, true
	case selNewUIMultiplier:
		mul, _ := ext.pending()
		return encU256(mul), nil, true
	case selEffectiveAt:
		_, at := ext.pending()
		return encU256(uint256.NewInt(at)), nil, true
	case selTotalSupplyUI:
		v, err := applyMultiplier(tok.s.totalSupply(), effective())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selSupportsInterface:
		id, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		// bytes4 occupies the high four bytes of its word; anything in the
		// remaining 28 is not a valid bytes4 and cannot be advertised.
		var want [4]byte
		copy(want[:], id[:4])
		for _, b := range id[4:] {
			if b != 0 {
				return encBool(false), nil, true
			}
		}
		return encBool(b20AssetInterfaceIDs[want]), nil, true
	case selCancelUIMultiplier:
		return nil, cancelUIMultiplier(tok, ext), true
	case selUpdateUIMultiplier:
		mul, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		at, err := readU256(args, 1)
		if err != nil {
			return nil, err, true
		}
		return nil, updateUIMultiplier(tok, ext, mul, at), true
	case selScaledBalanceOf, selBalanceOfUI:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := applyMultiplier(tok.s.balanceOf(a), effective())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selToScaledBalance, selToUIAmount:
		raw, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := applyMultiplier(raw, effective())
		if err != nil {
			return nil, err, true
		}
		return encU256(v), nil, true
	case selToRawBalance, selFromUIAmount:
		scaled, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, err := removeMultiplier(scaled, effective())
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
		id, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		used, ok := ext.announcementUsed(id)
		if !ok {
			return nil, ErrOutOfGas, true
		}
		return encBool(used), nil, true
	case selAnnounce:
		return nil, announce(tok, ext, args), true
	case selExtraMetadata:
		key, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		v, ok := ext.extraMetadata(key)
		if !ok {
			return nil, ErrOutOfGas, true
		}
		return encString(v), nil, true
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
	if !ext.setExtraMetadata(key, value) {
		return ErrOutOfGas
	}
	if !tok.ctx.AddLog([]common.Hash{b20TopicExtraMetadataUpdated},
		encodeTuple(abiString(key), abiString(value))) {
		return ErrOutOfGas
	}
	return nil
}

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
	// All three strings are decoded before the id is looked up, so a malformed
	// payload is reported as malformed rather than as AnnouncementIdAlreadyUsed.
	// The order is observable through which error the caller receives.
	id, err := readStringArg(args, 1)
	if err != nil {
		return err
	}
	description, err := readStringArg(args, 2)
	if err != nil {
		return err
	}
	uri, err := readStringArg(args, 3)
	if err != nil {
		return err
	}
	used, ok := ext.announcementUsed(id)
	if !ok {
		return ErrOutOfGas
	}
	if used {
		return revB20Bytes("AnnouncementIdAlreadyUsed(string)", errSelAnnounceIdUsed, []byte(id))
	}
	if !ext.markAnnouncement(id) { // marked before execution
		return ErrOutOfGas
	}
	if !tok.ctx.AddLog([]common.Hash{b20TopicAnnouncement, addrKey(tok.ctx.Caller)},
		encodeTuple(abiString(id), abiString(description), abiString(uri))) {
		return ErrOutOfGas
	}

	tok.inAnnounce = true // threaded into the internal calls below by value
	for _, c := range calls {
		if tok.ctx.OutOfGas() {
			return ErrOutOfGas
		}
		if len(c) < 4 {
			return revB20Bytes("InternalCallMalformed(bytes)", errSelInternalMalformed, c)
		}
		if _, err := assetDispatch(tok, ext, c); err != nil {
			return revB20Bytes("InternalCallFailed(bytes)", errSelInternalFailed, c)
		}
	}
	if !tok.ctx.AddLog([]common.Hash{b20TopicEndAnnouncement}, encodeTuple(abiString(id))) {
		return ErrOutOfGas
	}
	return nil
}

// updateUIMultiplier schedules a multiplier change for a future timestamp. Check
// order: role, then the value, then the two timestamp bounds, then whether a live
// schedule already exists.
func updateUIMultiplier(tok b20Token, ext assetExt, newMul, at *uint256.Int) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	if newMul.IsZero() || newMul.Gt(b20U128Mask) { // (0, type(uint128).max]
		return revB20("InvalidMultiplier()", errSelInvalidMultiplier)
	}
	now := tok.ctx.BlockTime()
	if !at.GtUint64(now) {
		return revB20("EffectiveAtInPast(uint256)", errSelEffectiveAtInPast, wU256(at))
	}
	if at.Gt(b20U64Max) {
		return revB20("EffectiveAtTooFar(uint256)", errSelEffectiveAtTooFar, wU256(at))
	}
	// Only a *live* schedule blocks a new one. A matured record is stale state,
	// not a commitment, so it is silently replaced.
	if _, existing := ext.pending(); existing > now {
		return revB20("UIMultiplierUpdateExists(uint256)", errSelUIMulExists, wU64(existing))
	}
	// The outgoing schedule may already be in force. Persist it before the slot is
	// reused, or the token drops back to its pre-maturity multiplier until the new
	// schedule arrives — a silent revaluation of every holder's balance.
	ext.settleMatured(now)
	previous := ext.multiplier()
	ext.setPending(newMul, at.Uint64())
	// The third argument is when the value takes effect, which for a schedule is
	// the future timestamp rather than now.
	if !tok.ctx.AddLog([]common.Hash{b20TopicUIMultiplierUpdated},
		append(append(wU256(previous).Bytes(), wU256(newMul).Bytes()...), wU256(at).Bytes()...)) {
		return ErrOutOfGas
	}
	return nil
}

// cancelUIMultiplier drops a live schedule. A matured one is not cancellable —
// its value is already in force, so there is nothing pending to withdraw.
func cancelUIMultiplier(tok b20Token, ext assetExt) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	mul, at := ext.pending()
	if at <= tok.ctx.BlockTime() {
		return revB20("UIMultiplierUpdateDoesNotExist()", errSelUIMulMissing)
	}
	ext.clearPending()
	if !tok.ctx.AddLog([]common.Hash{b20TopicUIMultiplierCancelled},
		append(wU256(mul).Bytes(), wU64(at).Bytes()...)) {
		return ErrOutOfGas
	}
	return nil
}

func updateMultiplier(tok b20Token, ext assetExt, newMul *uint256.Int) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	if newMul.IsZero() || newMul.Gt(b20U128Mask) { // (0, type(uint128).max]
		return revB20("InvalidMultiplier()", errSelInvalidMultiplier)
	}
	// The instant setter is the failsafe, so it overrides any schedule. A live one
	// is withdrawn loudly; a matured one is stale state and goes quietly, since its
	// value was already in force and is now being replaced.
	now := tok.ctx.BlockTime()
	previous := ext.effectiveMultiplier(now)
	if pendingMul, at := ext.pending(); at != 0 {
		ext.clearPending()
		if at > now {
			if !tok.ctx.AddLog([]common.Hash{b20TopicUIMultiplierCancelled},
				append(wU256(pendingMul).Bytes(), wU64(at).Bytes()...)) {
				return ErrOutOfGas
			}
		}
	}
	ext.setMultiplier(newMul)
	mb := newMul.Bytes32()
	if !tok.ctx.AddLog([]common.Hash{b20TopicMultiplierUpdated}, mb[:]) {
		return ErrOutOfGas
	}
	// ERC-8056's canonical event, emitted by both setters so one stream carries
	// every change.
	if !tok.ctx.AddLog([]common.Hash{b20TopicUIMultiplierUpdated},
		append(append(wU256(previous).Bytes(), wU256(newMul).Bytes()...), wU256(uint256.NewInt(now)).Bytes()...)) {
		return ErrOutOfGas
	}
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
		// chargeGas marks the frame out of gas and returns, so without this
		// the loop would run to completion on an exhausted budget — the state is
		// discarded either way, but the node has already done the work. A batch
		// long enough to exhaust its gas on the first recipient measured the same
		// wall-clock as one that paid for every one of them.
		if tok.ctx.OutOfGas() {
			return ErrOutOfGas
		}
		to, ok := addressFromWord(recipients[i])
		if !ok {
			return ErrExecutionReverted
		}
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
