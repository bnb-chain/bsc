package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// The MemoFormatRegistry is the third singleton: a permissionless catalogue of
// memo layouts, so a transfer can say which format(s) the payload behind its
// 32-byte memo hash follows. The chain records the claim and checks only that
// the formats exist; it never sees the payload, so it never verifies the claim.

var CAS20MemoFormatRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000003")

const cas20MemoNamespace = "bsc.memo_registry"

var cas20MemoRoot = erc7201Root(cas20MemoNamespace)

var featureMemoRegistry = crypto.Keccak256Hash([]byte(cas20MemoNamespace))

const (
	memoSlotFormats  = 0 // mapping(uint64 => packed word)
	memoSlotFields   = 1 // mapping(uint64 => uint64[]), one packed FieldSpec per lane
	memoSlotCounters = 2 // mapping(uint8 category => uint64)
)

// A formatId is self-describing: the high byte is the category, the low 56 bits a
// counter within it. Format 0, UNSTRUCTURED, is the plain memo of today.
const (
	memoCategoryInternal   = 0
	memoCategoryISO        = 1
	memoCategoryTravelRule = 2
	memoCategoryGov        = 3
	memoCategoryMax        = memoCategoryGov

	memoFormatUnstructured = uint64(0)
	memoCounterMax         = uint64(1)<<56 - 1

	memoKindBytes = 4 // ASCII_UPPER, ASCII_ALNUM, NUMERIC, FIXED_ENUM, BYTES

	// cas20MemoMaxFields bounds a format's field list, as the policy batch is bounded.
	cas20MemoMaxFields = 64
)

// Packed format word: bit 255 exists, creator in bits 0..160, category 160..168,
// field count 168..184, total length 184..200.
var memoExistsBit = new(uint256.Int).Lsh(uint256.NewInt(1), 255)

// A FieldSpec packs into one uint64 lane: offset 0..16, length 16..32, kind 32..40,
// required 40..48.
type memoFieldSpec struct {
	offset, length uint16
	kind           byte
	required       bool
}

func (f memoFieldSpec) pack() uint64 {
	v := uint64(f.offset) | uint64(f.length)<<16 | uint64(f.kind)<<32
	if f.required {
		v |= 1 << 40
	}
	return v
}

func unpackMemoField(v uint64) memoFieldSpec {
	return memoFieldSpec{offset: uint16(v), length: uint16(v >> 16), kind: byte(v >> 32), required: v>>40&1 == 1}
}

var (
	selCreateMemoFormat = selector("createMemoFormat(uint8,(uint16,uint16,uint8,bool)[])")
	selFormatExists     = selector("formatExists(uint64)")
	selGetMemoFormat    = selector("getMemoFormat(uint64)")
	selFormatCount      = selector("formatCount(uint8)")

	cas20TopicMemoFormatCreated = eventTopic("MemoFormatCreated(uint64,address,uint8)")

	// Token side.
	selTransferWithMemoFormats     = selector("transferWithMemoFormats(address,uint256,bytes32,uint64[])")
	selTransferFromWithMemoFormats = selector("transferFromWithMemoFormats(address,address,uint256,bytes32,uint64[])")
	selUpdateExpectedMemoFormats   = selector("updateExpectedMemoFormats(uint64[])")
	selExpectedMemoFormats         = selector("expectedMemoFormats()")

	cas20TopicMemoFormatsDeclared        = eventTopic("MemoFormatsDeclared(bytes32,uint64[])")
	cas20TopicExpectedMemoFormatsUpdated = eventTopic("ExpectedMemoFormatsUpdated(address,uint64[])")
)

type memoReg struct{ s cas20Storage }

func newMemoReg(ctx *PrecompileContext) memoReg {
	return memoReg{s: newMeteredCAS20StorageAt(ctx, CAS20MemoFormatRegistryAddress)}
}

func memoSlot(offset uint64) common.Hash { return offsetSlot(cas20MemoRoot, offset) }

func memoCategory(id uint64) byte { return byte(id >> 56) }

func (r memoReg) formatWord(id uint64) *uint256.Int {
	return new(uint256.Int).SetBytes(r.s.getWord(r.s.mapSlot(memoSlot(memoSlotFormats), idKey(id))).Bytes())
}

// formatExists answers for UNSTRUCTURED from the id alone, so it holds before any
// format has been created.
func (r memoReg) formatExists(id uint64) bool {
	if id == memoFormatUnstructured {
		return true
	}
	if memoCategory(id) > memoCategoryMax {
		return false
	}
	return new(uint256.Int).And(r.formatWord(id), memoExistsBit).Sign() != 0
}

func (r memoReg) setFormat(id uint64, creator common.Address, category byte, fieldCount, totalLen uint16) {
	w := new(uint256.Int).SetBytes(addrKey(creator).Bytes())
	w.Or(w, new(uint256.Int).Lsh(uint256.NewInt(uint64(category)), 160))
	w.Or(w, new(uint256.Int).Lsh(uint256.NewInt(uint64(fieldCount)), 168))
	w.Or(w, new(uint256.Int).Lsh(uint256.NewInt(uint64(totalLen)), 184))
	w.Or(w, memoExistsBit)
	r.s.setWord(r.s.mapSlot(memoSlot(memoSlotFormats), idKey(id)), w.Bytes32())
}

func (r memoReg) counter(category byte) uint64 {
	return new(uint256.Int).SetBytes(r.s.getWord(r.s.mapSlot(memoSlot(memoSlotCounters), wU8(category))).Bytes()).Uint64()
}

func (r memoReg) setCounter(category byte, n uint64) {
	r.s.setWord(r.s.mapSlot(memoSlot(memoSlotCounters), wU8(category)), wU64(n))
}

func (r memoReg) fieldsSlot(id uint64) common.Hash {
	return r.s.mapSlot(memoSlot(memoSlotFields), idKey(id))
}

type cas20MemoPrecompile struct{ cas20StatefulBase }

func (p *cas20MemoPrecompile) Name() string { return "CAS20MemoFormatRegistry" }

func (p *cas20MemoPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	ret, err := runCAS20Memo(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}

func runCAS20Memo(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]
	reg := newMemoReg(ctx)

	switch sel {
	case selFormatExists:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(reg.formatExists(id)), nil
	case selFormatCount:
		w, err := readWord(args, 0)
		if err != nil {
			return nil, err
		}
		if !isEnumWord(w, memoCategoryMax) {
			return nil, ErrExecutionReverted
		}
		return encU256(uint256.NewInt(reg.counter(w[31]))), nil
	case selGetMemoFormat:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return getMemoFormat(reg, id)
	case selCreateMemoFormat:
		// Static frame, then the feature gate, before decoding: the registries' order.
		if ctx.ReadOnly {
			return nil, ErrWriteProtection
		}
		if err := ensureFeatureActivated(ctx, featureMemoRegistry); err != nil {
			return nil, err
		}
		return createMemoFormat(ctx, reg, args)
	}
	return nil, ErrExecutionReverted
}

// getMemoFormat returns (FieldSpec[] fields, uint16 totalLen, address creator).
func getMemoFormat(reg memoReg, id uint64) ([]byte, error) {
	if !reg.formatExists(id) {
		return nil, revCAS20("FormatNotFound(uint64)", errSelFormatNotFound, wU64(id))
	}
	var fields []memoFieldSpec
	var totalLen uint64
	var creator common.Hash
	if id != memoFormatUnstructured {
		w := reg.formatWord(id)
		creator = common.BytesToHash(common.BytesToAddress(w.Bytes()).Bytes())
		count := new(uint256.Int).Rsh(w, 168).Uint64() & 0xffff
		totalLen = new(uint256.Int).Rsh(w, 184).Uint64() & 0xffff
		for _, lane := range reg.s.u64ArrayAt(reg.fieldsSlot(id), count) {
			fields = append(fields, unpackMemoField(lane))
		}
	}
	tail := make([]byte, 0, 32*(1+4*len(fields)))
	tail = append(tail, wU64(uint64(len(fields))).Bytes()...)
	for _, f := range fields {
		tail = append(tail, wU64(uint64(f.offset)).Bytes()...)
		tail = append(tail, wU64(uint64(f.length)).Bytes()...)
		tail = append(tail, wU8(f.kind).Bytes()...)
		tail = append(tail, encBool(f.required)...)
	}
	return encodeTuple(abiPart{dynamic: true, tail: tail}, abiWord(wU64(totalLen)), abiWord(creator)), nil
}

// createMemoFormat(uint8 category, FieldSpec[] fields) -> uint64 formatId.
// Order: category, then each field in array order, then the counter bound.
func createMemoFormat(ctx *PrecompileContext, reg memoReg, args []byte) ([]byte, error) {
	catWord, err := readWord(args, 0)
	if err != nil {
		return nil, err
	}
	if !isEnumWord(catWord, 0xff) {
		return nil, ErrExecutionReverted
	}
	fields, err := readFieldSpecs(args, 1)
	if err != nil {
		return nil, err
	}
	category := catWord[31]
	if category > memoCategoryMax {
		return nil, revCAS20("InvalidCategory()", errSelInvalidCategory)
	}
	if len(fields) == 0 || len(fields) > cas20MemoMaxFields {
		return nil, revCAS20("InvalidFieldCount(uint256)", errSelInvalidFieldCount, wU64(uint64(len(fields))))
	}
	var totalLen uint64
	for i, f := range fields {
		end := uint64(f.offset) + uint64(f.length)
		if f.length == 0 || end > 0xffff || f.kind > memoKindBytes {
			return nil, revCAS20("InvalidFieldSpec(uint256)", errSelInvalidFieldSpec, wU64(uint64(i)))
		}
		if end > totalLen {
			totalLen = end
		}
	}

	c := reg.counter(category) + 1
	if c > memoCounterMax {
		return nil, revPanic(0x11)
	}
	id := uint64(category)<<56 | c
	reg.setCounter(category, c)
	lanes := make([]uint64, len(fields))
	for i, f := range fields {
		lanes[i] = f.pack()
	}
	reg.s.setU64ArrayAt(reg.fieldsSlot(id), lanes)
	reg.setFormat(id, ctx.Caller, category, uint16(len(fields)), uint16(totalLen))
	if !ctx.AddLog([]common.Hash{cas20TopicMemoFormatCreated, wU64(id), addrKey(ctx.Caller)}, wU8(category).Bytes()) {
		return nil, ErrOutOfGas
	}
	return encU256(uint256.NewInt(id)), nil
}

// readFieldSpecs decodes a (uint16,uint16,uint8,bool)[]: a dynamic array of
// static four-word tuples, every narrow member decoded strictly.
func readFieldSpecs(args []byte, argIndex int) ([]memoFieldSpec, error) {
	L := uint64(len(args))
	base, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || base > L || L-base < 32 {
		return nil, ErrExecutionReverted
	}
	n, ok := wordU64(args, base)
	if !ok {
		return nil, ErrExecutionReverted
	}
	dataPos := base + 32
	if n > (L-dataPos)/(4*32) {
		return nil, ErrExecutionReverted
	}
	out := make([]memoFieldSpec, n)
	for i := uint64(0); i < n; i++ {
		elem := args[dataPos+i*128 : dataPos+(i+1)*128]
		var w [4]common.Hash
		for j := range w {
			w[j] = common.BytesToHash(elem[j*32 : (j+1)*32])
		}
		if !wordFitsIn(w[0], 2) || !wordFitsIn(w[1], 2) || !isEnumWord(w[2], 0xff) || !isEnumWord(w[3], 1) {
			return nil, ErrExecutionReverted
		}
		out[i] = memoFieldSpec{
			offset:   uint16(new(uint256.Int).SetBytes(w[0].Bytes()).Uint64()),
			length:   uint16(new(uint256.Int).SetBytes(w[1].Bytes()).Uint64()),
			kind:     w[2][31],
			required: w[3][31] == 1,
		}
	}
	return out, nil
}

// --- Token side ------------------------------------------------------------

// dispatchMemoFormats handles the format-declaring transfers and the issuer's
// advisory list. ok is false when sel is none of them.
func (t cas20Token) dispatchMemoFormats(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	case selExpectedMemoFormats:
		ids := t.s.u64ArrayAt(slotAt(cas20SlotExpectedMemoFormats), cas20MemoMaxFields)
		return encodeTuple(abiWordArray(u64Words(ids))), nil, true
	case selUpdateExpectedMemoFormats:
		ids, err := readU64Array(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateExpectedMemoFormats(ids), true
	case selTransferWithMemoFormats:
		to, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err, true
		}
		memo, err := readWord(args, 2)
		if err != nil {
			return nil, err, true
		}
		ids, err := readU64Array(args, 3)
		if err != nil {
			return nil, err, true
		}
		ret, err := t.transferWithMemoFormats(t.ctx.Caller, t.ctx.Caller, to, amount, memo, ids)
		return ret, err, true
	case selTransferFromWithMemoFormats:
		from, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		to, err := readAddress(args, 1)
		if err != nil {
			return nil, err, true
		}
		amount, err := readU256(args, 2)
		if err != nil {
			return nil, err, true
		}
		memo, err := readWord(args, 3)
		if err != nil {
			return nil, err, true
		}
		ids, err := readU64Array(args, 4)
		if err != nil {
			return nil, err, true
		}
		ret, err := t.transferWithMemoFormats(t.ctx.Caller, from, to, amount, memo, ids)
		return ret, err, true
	}
	return nil, nil, false
}

// transferWithMemoFormats is the memo transfer plus a self-declared claim. Order:
// static frame, the feature gate, every id's existence, then the transfer's own
// sequence; the claim is emitted after Memo.
func (t cas20Token) transferWithMemoFormats(spender, from, to common.Address, amount *uint256.Int, memo common.Hash, ids []uint64) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if err := t.ensureMemoFormats(ids); err != nil {
		return nil, err
	}
	var ret []byte
	var err error
	if spender == from {
		ret, err = t.transfer(from, to, amount)
	} else {
		ret, err = t.transferFrom(spender, from, to, amount)
	}
	if err != nil {
		return nil, err
	}
	if !t.emitMemo(memo) || !t.emitMemoFormats(memo, ids) {
		return nil, ErrOutOfGas
	}
	return ret, nil
}

// ensureMemoFormats is the whole of what the chain checks about a claim: the
// feature is open and every id names a format that exists.
func (t cas20Token) ensureMemoFormats(ids []uint64) error {
	if err := ensureFeatureActivated(t.ctx, featureMemoRegistry); err != nil {
		return err
	}
	if len(ids) == 0 {
		return revCAS20("EmptyBatch()", errSelEmptyBatch)
	}
	reg := newMemoReg(t.ctx)
	for _, id := range ids {
		if t.ctx.OutOfGas() {
			return ErrOutOfGas
		}
		if !reg.formatExists(id) {
			return revCAS20("FormatNotFound(uint64)", errSelFormatNotFound, wU64(id))
		}
	}
	return nil
}

// updateExpectedMemoFormats publishes the formats an issuer expects transfers to
// declare. Advisory: a transfer omitting one still succeeds.
func (t cas20Token) updateExpectedMemoFormats(ids []uint64) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := t.ensureRole(roleDefaultAdmin); err != nil {
		return err
	}
	if err := ensureFeatureActivated(t.ctx, featureMemoRegistry); err != nil {
		return err
	}
	if len(ids) > cas20MemoMaxFields {
		return revCAS20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(cas20MemoMaxFields))
	}
	reg := newMemoReg(t.ctx)
	for _, id := range ids {
		if !reg.formatExists(id) {
			return revCAS20("FormatNotFound(uint64)", errSelFormatNotFound, wU64(id))
		}
	}
	t.s.setU64ArrayAt(slotAt(cas20SlotExpectedMemoFormats), ids)
	if !t.ctx.AddLog([]common.Hash{cas20TopicExpectedMemoFormatsUpdated, addrKey(t.ctx.Caller)},
		encodeTuple(abiWordArray(u64Words(ids)))) {
		return ErrOutOfGas
	}
	return nil
}

func (t cas20Token) emitMemoFormats(memo common.Hash, ids []uint64) bool {
	return t.ctx.AddLog([]common.Hash{cas20TopicMemoFormatsDeclared, memo}, encodeTuple(abiWordArray(u64Words(ids))))
}

func u64Words(ids []uint64) []common.Hash {
	words := make([]common.Hash, len(ids))
	for i, id := range ids {
		words[i] = wU64(id)
	}
	return words
}

// readU64Array decodes a uint64[] strictly: every element's high 24 bytes must be zero.
func readU64Array(args []byte, argIndex int) ([]uint64, error) {
	words, err := readWordArray(args, argIndex)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, len(words))
	for i, w := range words {
		v, ok := u64FromWord(w)
		if !ok {
			return nil, ErrExecutionReverted
		}
		out[i] = v
	}
	return out, nil
}
