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
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CAS20 core storage layout.
const cas20Namespace = "bsc.cas20"

const (
	cas20SlotName             = 0
	cas20SlotSymbol           = 1
	cas20SlotContractURI      = 2
	cas20SlotTotalSupply      = 3
	cas20SlotBalances         = 4
	cas20SlotAllowances       = 5
	cas20SlotRoles            = 6
	cas20SlotRoleAdmins       = 7
	cas20SlotAdminCount       = 8
	cas20SlotTransferPolicies = 9
	cas20SlotMintPolicy       = 10
	cas20SlotPaused           = 11
	cas20SlotSupplyCap        = 12
	cas20SlotNonces           = 13
	cas20SlotSeizePolicies    = 14
)

// Packed u64 byte offsets within the policy slots. The lanes a group leaves
// free are reserved for that group: the mint slot holds one id today and the
// rest of it belongs to future mint-side policy types, which is why the seize
// ids sit in a slot of their own rather than filling it. Seize is a cold path,
// so the extra load costs nothing that matters.
const (
	cas20OffTransferSender   = 0
	cas20OffTransferReceiver = 8
	cas20OffTransferExecutor = 16
	cas20OffMintReceiver     = 0
	cas20OffSeizeHolder      = 0
	cas20OffSeizeReceiver    = 8
)

// cas20CoreRoot is the ERC-7201 root of the core storage namespace, computed once.
var cas20CoreRoot = erc7201Root(cas20Namespace)

// erc7201Root computes the ERC-7201 storage root of a namespace:
func erc7201Root(namespace string) common.Hash {
	inner := new(uint256.Int).SetBytes(crypto.Keccak256([]byte(namespace)))
	inner.SubUint64(inner, 1)
	buf := inner.Bytes32()
	root := crypto.Keccak256Hash(buf[:])
	root[31] = 0 // clear the low byte
	return root
}

// slotAt returns the absolute storage slot of a fixed field at the given
// offset from the core root.
func slotAt(offset uint64) common.Hash {
	s := new(uint256.Int).SetBytes(cas20CoreRoot.Bytes())
	s.AddUint64(s, offset)
	return s.Bytes32()
}

// offsetSlot returns the slot at root+offset, the fixed-field addressing every
// CAS20 namespace uses.
func offsetSlot(root common.Hash, offset uint64) common.Hash {
	x := new(uint256.Int).SetBytes(root.Bytes())
	x.AddUint64(x, offset)
	return x.Bytes32()
}

// mappingSlot returns the Solidity storage slot of mapping[key] where the
// mapping is declared at base: keccak256(pad32(key) ++ base).
func mappingSlot(base, key common.Hash) common.Hash {
	return crypto.Keccak256Hash(key.Bytes(), base.Bytes())
}

// addrKey left-pads an address to a 32-byte mapping key.
func addrKey(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

type cas20Storage struct {
	state StateDB
	token common.Address
	ctx   *PrecompileContext
}

// mapSlot derives mapping[key] and meters the keccak, which hashes the 64-byte
// (key ++ base) preimage. Use it on any metered path; the bare mappingSlot helper
// stays available for tests and unmetered views.
//
// The three derivations below return the zero slot when their charge is refused.
// That slot is meaningless, not dangerous: the frame is out of gas by then, so
// every read or write through it is refused too. Skipping the hash matters most
// for strMapSlot, whose preimage is caller-sized.
func (s cas20Storage) mapSlot(base, key common.Hash) common.Hash {
	if s.ctx != nil && !s.ctx.chargeKeccak(64) {
		return common.Hash{}
	}
	return mappingSlot(base, key)
}

// strMapSlot derives mapping[key] for a string-keyed mapping. Solidity hashes
// the key's raw bytes concatenated with the base slot rather than padding the
// key to a word, so the preimage is variable-length — and so is the charge,
// which is what keeps a long caller-supplied key from hashing for free.
func (s cas20Storage) strMapSlot(base common.Hash, key string) common.Hash {
	if s.ctx != nil && !s.ctx.chargeKeccak(len(key)+32) {
		return common.Hash{}
	}
	return crypto.Keccak256Hash([]byte(key), base.Bytes())
}

// newUnmeteredCAS20Storage returns a view that charges no gas. Only for callers
// that have no frame to charge — the fork seeding hook and the state queries
// behind it — and for tests. Everything reached from a precompile call must use
// newMeteredCAS20Storage, or its state access is free.
func newUnmeteredCAS20Storage(state StateDB, token common.Address) cas20Storage {
	return cas20Storage{state: state, token: token}
}

// newMeteredCAS20Storage returns a view bound to ctx.Self that charges gas for
// each slot access.
func newMeteredCAS20Storage(ctx *PrecompileContext) cas20Storage {
	return cas20Storage{state: ctx.StateDB, token: ctx.Self, ctx: ctx}
}

// newMeteredCAS20StorageAt is newMeteredCAS20Storage for a fixed address instead of
// ctx.Self, which a token needs when it consults a registry for its own gating.
// Charged as storage with no account-access surcharge, as if the slot were its
// own (BEP-702 3.14).
func newMeteredCAS20StorageAt(ctx *PrecompileContext, token common.Address) cas20Storage {
	return cas20Storage{state: ctx.StateDB, token: token, ctx: ctx}
}

// chargeRead meters an SLOAD-equivalent at EIP-2929 prices: warm always, plus
// the cold surcharge on the first touch of the slot in this transaction.
func (s cas20Storage) chargeRead(slot common.Hash) bool {
	if s.ctx == nil {
		return true
	}
	if _, warm := s.state.SlotInAccessList(s.token, slot); warm {
		return s.ctx.chargeGas(params.WarmStorageReadCostEIP2929)
	}
	s.state.AddSlotToAccessList(s.token, slot)
	return s.ctx.chargeGas(params.ColdSloadCostEIP2929)
}

// getWord reads a slot after charging for it. The second result is false when the
// charge could not be covered, in which case nothing was read and the caller must
// stop — the value is meaningless, and acting on a zero is how a frame out of gas
// took a fail-open branch.
// getWordChecked is getWord with the charge result, for reads whose value decides
// a branch. getWord's zero is safe only where the next charge stops the caller
// anyway; where a zero means "absent" and absent means "proceed", the caller has
// to know the read never happened.
func (s cas20Storage) getWordChecked(slot common.Hash) (common.Hash, bool) {
	if !s.chargeRead(slot) {
		return common.Hash{}, false
	}
	return s.state.GetState(s.token, slot), true
}

func (s cas20Storage) getWord(slot common.Hash) common.Hash {
	if !s.chargeRead(slot) {
		return common.Hash{}
	}
	return s.state.GetState(s.token, slot)
}

// setWord writes a slot after metering it (see chargeStorageWrite) and reports
// whether the write happened. False covers an unaffordable charge, the reentrancy
// sentry, and a read-only frame.
//
// Honour the result before doing work proportional to what the caller sent. The
// fixed-size wrappers below drop it deliberately: the out-of-gas flag is sticky
// and the exit reports it either way, so what dropping it costs is the bound on
// work, not correctness.
func (s cas20Storage) setWord(slot, val common.Hash) bool {
	if !s.chargeStorageWrite(slot, val) {
		return false
	}
	s.state.SetState(s.token, slot, val)
	return true
}

// --- fixed uint256 fields ---------------------------------------------------

func (s cas20Storage) getU256(offset uint64) *uint256.Int {
	return new(uint256.Int).SetBytes(s.getWord(slotAt(offset)).Bytes())
}

func (s cas20Storage) setU256(offset uint64, v *uint256.Int) {
	s.setWord(slotAt(offset), v.Bytes32())
}

func (s cas20Storage) totalSupply() *uint256.Int     { return s.getU256(cas20SlotTotalSupply) }
func (s cas20Storage) setTotalSupply(v *uint256.Int) { s.setU256(cas20SlotTotalSupply, v) }
func (s cas20Storage) supplyCap() *uint256.Int       { return s.getU256(cas20SlotSupplyCap) }
func (s cas20Storage) setSupplyCap(v *uint256.Int)   { s.setU256(cas20SlotSupplyCap, v) }
func (s cas20Storage) adminCount() *uint256.Int      { return s.getU256(cas20SlotAdminCount) }
func (s cas20Storage) setAdminCount(v *uint256.Int)  { s.setU256(cas20SlotAdminCount, v) }

// pausedChecked is paused with the charge result, for the write path, whose work
// after the read is proportional to the caller's feature array.
func (s cas20Storage) pausedChecked() (*uint256.Int, bool) {
	w, ok := s.getWordChecked(slotAt(cas20SlotPaused))
	if !ok {
		return new(uint256.Int), false
	}
	return new(uint256.Int).SetBytes(w.Bytes()), true
}

func (s cas20Storage) paused() *uint256.Int     { return s.getU256(cas20SlotPaused) }
func (s cas20Storage) setPaused(v *uint256.Int) { s.setU256(cas20SlotPaused, v) }

// --- balances / allowances / nonces ----------------------------------------

// Deriving a mapping slot is a metered keccak, so a read-modify-write that goes
// through balanceOf then setBalance pays for the hash twice where a Solidity
// implementation computes it once. The slot-taking forms below let a caller
// derive once and reuse; the address-taking forms remain for single accesses,
// views and tests.

func (s cas20Storage) balanceSlot(a common.Address) common.Hash {
	return s.mapSlot(slotAt(cas20SlotBalances), addrKey(a))
}

func (s cas20Storage) allowanceSlot(owner, spender common.Address) common.Hash {
	return s.mapSlot(s.mapSlot(slotAt(cas20SlotAllowances), addrKey(owner)), addrKey(spender))
}

func (s cas20Storage) getU256At(slot common.Hash) *uint256.Int {
	return new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
}

func (s cas20Storage) setU256At(slot common.Hash, v *uint256.Int) {
	s.setWord(slot, v.Bytes32())
}

func (s cas20Storage) balanceOf(a common.Address) *uint256.Int {
	return s.getU256At(s.balanceSlot(a))
}

func (s cas20Storage) setBalance(a common.Address, v *uint256.Int) {
	s.setU256At(s.balanceSlot(a), v)
}

func (s cas20Storage) allowance(owner, spender common.Address) *uint256.Int {
	return s.getU256At(s.allowanceSlot(owner, spender))
}

func (s cas20Storage) setAllowance(owner, spender common.Address, v *uint256.Int) {
	s.setU256At(s.allowanceSlot(owner, spender), v)
}

func (s cas20Storage) nonce(owner common.Address) *uint256.Int {
	slot := s.mapSlot(slotAt(cas20SlotNonces), addrKey(owner))
	return new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
}

func (s cas20Storage) setNonce(owner common.Address, v *uint256.Int) {
	slot := s.mapSlot(slotAt(cas20SlotNonces), addrKey(owner))
	s.setWord(slot, v.Bytes32())
}

// --- roles ------------------------------------------------------------------

func (s cas20Storage) hasRole(role common.Hash, a common.Address) bool {
	inner := s.mapSlot(slotAt(cas20SlotRoles), role)
	slot := s.mapSlot(inner, addrKey(a))
	return s.getWord(slot) != (common.Hash{})
}

func (s cas20Storage) setRole(role common.Hash, a common.Address, enabled bool) {
	inner := s.mapSlot(slotAt(cas20SlotRoles), role)
	slot := s.mapSlot(inner, addrKey(a))
	var v common.Hash
	if enabled {
		v[31] = 1
	}
	s.setWord(slot, v)
}

func (s cas20Storage) roleAdmin(role common.Hash) common.Hash {
	return s.getWord(s.mapSlot(slotAt(cas20SlotRoleAdmins), role))
}

func (s cas20Storage) setRoleAdmin(role, admin common.Hash) {
	s.setWord(s.mapSlot(slotAt(cas20SlotRoleAdmins), role), admin)
}

// --- packed policy ids ------------------------------------------------------

func (s cas20Storage) getPackedU64(offset uint64, byteOff uint) uint64 {
	word := new(uint256.Int).SetBytes(s.getWord(slotAt(offset)).Bytes())
	return word.Rsh(word, byteOff*8).Uint64()
}

// setPackedU64 writes v into the u64 lane at byteOff, preserving the other lanes.
func (s cas20Storage) setPackedU64(offset uint64, byteOff uint, v uint64) {
	slot := slotAt(offset)
	word := new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
	lane := new(uint256.Int).Lsh(uint256.NewInt(0xffffffffffffffff), byteOff*8)
	word.And(word, lane.Not(lane))
	word.Or(word, new(uint256.Int).Lsh(uint256.NewInt(v), byteOff*8))
	s.setWord(slot, word.Bytes32())
}

// transferPolicies reads all three transfer-side ids with one storage access:
// they share a slot, so reading them separately pays for the same slot three
// times. seizePolicies does the same for its pair.
func (s cas20Storage) transferPolicies() (sender, receiver, executor uint64) {
	w := s.getU256At(slotAt(cas20SlotTransferPolicies))
	return packedLane(w, cas20OffTransferSender), packedLane(w, cas20OffTransferReceiver),
		packedLane(w, cas20OffTransferExecutor)
}

func (s cas20Storage) seizePolicies() (holder, receiver uint64) {
	w := s.getU256At(slotAt(cas20SlotSeizePolicies))
	return packedLane(w, cas20OffSeizeHolder), packedLane(w, cas20OffSeizeReceiver)
}

// packedLane extracts the u64 lane at byteOff from an already-read slot value.
func packedLane(word *uint256.Int, byteOff uint) uint64 {
	return new(uint256.Int).Rsh(word, byteOff*8).Uint64()
}

func (s cas20Storage) transferSenderPolicy() uint64 {
	return s.getPackedU64(cas20SlotTransferPolicies, cas20OffTransferSender)
}
func (s cas20Storage) transferReceiverPolicy() uint64 {
	return s.getPackedU64(cas20SlotTransferPolicies, cas20OffTransferReceiver)
}
func (s cas20Storage) transferExecutorPolicy() uint64 {
	return s.getPackedU64(cas20SlotTransferPolicies, cas20OffTransferExecutor)
}
func (s cas20Storage) mintReceiverPolicy() uint64 {
	return s.getPackedU64(cas20SlotMintPolicy, cas20OffMintReceiver)
}
func (s cas20Storage) setTransferSenderPolicy(id uint64) {
	s.setPackedU64(cas20SlotTransferPolicies, cas20OffTransferSender, id)
}
func (s cas20Storage) setTransferReceiverPolicy(id uint64) {
	s.setPackedU64(cas20SlotTransferPolicies, cas20OffTransferReceiver, id)
}
func (s cas20Storage) setTransferExecutorPolicy(id uint64) {
	s.setPackedU64(cas20SlotTransferPolicies, cas20OffTransferExecutor, id)
}
func (s cas20Storage) setMintReceiverPolicy(id uint64) {
	s.setPackedU64(cas20SlotMintPolicy, cas20OffMintReceiver, id)
}
func (s cas20Storage) setSeizeHolderPolicy(id uint64) {
	s.setPackedU64(cas20SlotSeizePolicies, cas20OffSeizeHolder, id)
}
func (s cas20Storage) setSeizeReceiverPolicy(id uint64) {
	s.setPackedU64(cas20SlotSeizePolicies, cas20OffSeizeReceiver, id)
}

// --- strings (Solidity storage encoding) ------------------------------------

func (s cas20Storage) getString(offset uint64) (string, bool) { return s.getStringAt(slotAt(offset)) }
func (s cas20Storage) setString(offset uint64, str string) bool {
	return s.setStringAt(slotAt(offset), str)
}

// stringDataRoot derives the slot a long string's data begins at, keccak256 of
// the length slot, and meters the hash of that 32-byte preimage. Deriving a
// mapping slot is metered the same way (see mapSlot); a long string's data root
// is no less a runtime keccak just because the preimage is one word.
func (s cas20Storage) stringDataRoot(slot common.Hash) *uint256.Int {
	if s.ctx != nil && !s.ctx.chargeKeccak(32) {
		return new(uint256.Int)
	}
	return new(uint256.Int).SetBytes(crypto.Keccak256(slot.Bytes()))
}

// cas20MaxStringLen bounds a string read against a malformed length word.
const cas20MaxStringLen = 1 << 24

// getStringAt / setStringAt read and write a Solidity string at an arbitrary
// slot (used for fixed fields and for string-keyed mapping values).
func (s cas20Storage) getStringAt(slot common.Hash) (string, bool) {
	word, ok := s.getWordChecked(slot)
	if !ok {
		return "", false
	}
	if word[31]&1 == 0 {
		// short string: content in the high bytes, low byte holds 2*len.
		n := int(word[31]) / 2
		if n > 31 {
			return "", true
		}
		return string(word[:n]), true
	}
	// long string: slot holds 2*len+1; content starts at keccak256(slot).
	encoded := new(uint256.Int).SetBytes(word.Bytes())
	if encoded.Gt(uint256.NewInt(2*cas20MaxStringLen + 1)) {
		return "", true
	}
	length := (encoded.Uint64() - 1) / 2
	base := s.stringDataRoot(slot)
	// Before the allocation, not after: the loop below checks each iteration, but
	// make() runs once with the stored length whatever the budget says.
	if s.ctx != nil && s.ctx.OutOfGas() {
		return "", false
	}
	out := make([]byte, 0, length)
	for i := uint64(0); i < length; i += 32 {
		chunkSlot := new(uint256.Int).AddUint64(base, i/32).Bytes32()
		chunk, ok := s.getWordChecked(chunkSlot)
		if !ok {
			return "", false
		}
		out = append(out, chunk[:]...)
	}
	return string(out[:length]), true
}

// setStringAt writes a string, releasing whatever the previous value held.
func (s cas20Storage) setStringAt(slot common.Hash, str string) bool {
	b := []byte(str)
	oldChunks := s.stringChunks(slot)
	newChunks := uint64(0)
	if len(b) >= 32 {
		newChunks = uint64((len(b) + 31) / 32)
	}

	if len(b) < 32 {
		var word common.Hash
		copy(word[:], b)
		word[31] = byte(len(b) * 2)
		if !s.setWord(slot, word) {
			return false
		}
	} else if !s.setWord(slot, uint256.NewInt(uint64(len(b)*2+1)).Bytes32()) {
		return false
	}
	if newChunks == 0 && oldChunks == 0 {
		return true // wholly inline, before and after: no data region exists
	}
	// One keccak covers writing the new chunks and releasing the old ones, as
	// it would in Solidity — deriving the root twice would overcharge.
	base := s.stringDataRoot(slot)
	for i := uint64(0); i < newChunks; i++ {
		if s.ctx != nil && s.ctx.OutOfGas() {
			return false
		}
		var chunk common.Hash
		copy(chunk[:], b[i*32:])
		s.setWord(new(uint256.Int).AddUint64(base, i).Bytes32(), chunk)
	}
	// The release loop needs its own guard, and for a different reason than the
	// write loop above: oldChunks comes from state, not from this call's calldata.
	// Replacing a long stored string with a short one does work proportional to the
	// *old* length, which the current caller never paid for — and since an
	// exhausted frame reverts, the long string survives for the next attempt. A
	// 60,000-byte name costs 41.9M gas to store once, after which updateName("x")
	// on 30,000 gas cleared 1875 slots in 126us and could do so again forever.
	for i := newChunks; i < oldChunks; i++ {
		if s.ctx != nil && s.ctx.OutOfGas() {
			return false
		}
		s.setWord(new(uint256.Int).AddUint64(base, i).Bytes32(), common.Hash{})
	}
	return true
}

// stringChunks reports how many tail slots the string currently at slot
// occupies. A short string is held inline and occupies none.
func (s cas20Storage) stringChunks(slot common.Hash) uint64 {
	word := s.getWord(slot)
	if word[31]&1 == 0 {
		return 0
	}
	// Same untrusted word as getStringAt, so the same bound: without it the
	// release loop in setStringAt would be handed a chunk count of any size, and
	// an unmetered view has no out-of-gas guard to stop it.
	encoded := new(uint256.Int).SetBytes(word.Bytes())
	if encoded.Gt(uint256.NewInt(2*cas20MaxStringLen + 1)) {
		return 0
	}
	length := (encoded.Uint64() - 1) / 2
	return (length + 31) / 32
}

func (s cas20Storage) name() (string, bool)         { return s.getString(cas20SlotName) }
func (s cas20Storage) setName(v string) bool        { return s.setString(cas20SlotName, v) }
func (s cas20Storage) symbol() (string, bool)       { return s.getString(cas20SlotSymbol) }
func (s cas20Storage) setSymbol(v string) bool      { return s.setString(cas20SlotSymbol, v) }
func (s cas20Storage) contractURI() (string, bool)  { return s.getString(cas20SlotContractURI) }
func (s cas20Storage) setContractURI(v string) bool { return s.setString(cas20SlotContractURI, v) }
