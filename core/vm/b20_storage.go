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

// B20 core storage layout.
//
// A token's state lives in the account storage trie at the token's own
// address, using an ERC-7201 namespaced root and Solidity-identical slot math
// (fixed offsets, keccak-derived mapping slots, in-slot packing). This makes
// the layout byte-for-byte reproducible by a reference Solidity contract, so
// the golden tests can cross-check the native precompile against base-std.
//
// Layout mirrors B20CoreStorage (namespace "bsc.b20"):
//
//	slot 0   name (string)
//	slot 1   symbol (string)
//	slot 2   contractURI (string)
//	slot 3   totalSupply (uint256)
//	slot 4   balances    mapping(address => uint256)
//	slot 5   allowances  mapping(address => mapping(address => uint256))
//	slot 6   roles       mapping(bytes32 => mapping(address => bool))
//	slot 7   roleAdmins  mapping(bytes32 => bytes32)
//	slot 8   adminCount (uint256)
//	slot 9   packed: transferSender|transferReceiver|transferExecutor|reserved (4×u64)
//	slot 10  packed: mintReceiver|reserved (4×u64)
//	slot 11  paused (uint256 bitmask)
//	slot 12  supplyCap (uint256)
//	slot 13  nonces      mapping(address => uint256)
//	slot 14  packed: seizeHolder|seizeReceiver|reserved (4×u64)
const b20Namespace = "bsc.b20"

const (
	b20SlotName             = 0
	b20SlotSymbol           = 1
	b20SlotContractURI      = 2
	b20SlotTotalSupply      = 3
	b20SlotBalances         = 4
	b20SlotAllowances       = 5
	b20SlotRoles            = 6
	b20SlotRoleAdmins       = 7
	b20SlotAdminCount       = 8
	b20SlotTransferPolicies = 9
	b20SlotMintPolicy       = 10
	b20SlotPaused           = 11
	b20SlotSupplyCap        = 12
	b20SlotNonces           = 13
	b20SlotSeizePolicies    = 14
)

// Packed u64 byte offsets within the policy slots. The lanes a group leaves
// free are reserved for that group: the mint slot holds one id today and the
// rest of it belongs to future mint-side policy types, which is why the seize
// ids sit in a slot of their own rather than filling it. Seize is a cold path,
// so the extra load costs nothing that matters.
const (
	b20OffTransferSender   = 0
	b20OffTransferReceiver = 8
	b20OffTransferExecutor = 16
	b20OffMintReceiver     = 0
	b20OffSeizeHolder      = 0
	b20OffSeizeReceiver    = 8
)

// b20CoreRoot is the ERC-7201 root of the core storage namespace, computed once.
var b20CoreRoot = erc7201Root(b20Namespace)

// erc7201Root computes the ERC-7201 storage root of a namespace:
//
//	keccak256(abi.encode(uint256(keccak256(namespace)) - 1)) & ~bytes32(uint256(0xff))
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
	s := new(uint256.Int).SetBytes(b20CoreRoot.Bytes())
	s.AddUint64(s, offset)
	return common.Hash(s.Bytes32())
}

// offsetSlot returns the slot at root+offset, the fixed-field addressing every
// B20 namespace uses.
func offsetSlot(root common.Hash, offset uint64) common.Hash {
	x := new(uint256.Int).SetBytes(root.Bytes())
	x.AddUint64(x, offset)
	return common.Hash(x.Bytes32())
}

// mappingSlot returns the Solidity storage slot of mapping[key] where the
// mapping is declared at base: keccak256(pad32(key) ++ base).
func mappingSlot(base, key common.Hash) common.Hash {
	return crypto.Keccak256Hash(key.Bytes(), base.Bytes())
}

// addrKey left-pads an address to a 32-byte mapping key.
func addrKey(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

type b20Storage struct {
	state StateDB
	token common.Address
	ctx   *PrecompileContext
}

// mapSlot derives mapping[key] and meters the keccak, which hashes the 64-byte
// (key ++ base) preimage. Use it on any metered path; the bare mappingSlot
// helper stays available for tests and unmetered views.
func (s b20Storage) mapSlot(base, key common.Hash) common.Hash {
	if s.ctx != nil {
		s.ctx.chargeKeccak(64)
	}
	return mappingSlot(base, key)
}

// strMapSlot derives mapping[key] for a string-keyed mapping. Solidity hashes
// the key's raw bytes concatenated with the base slot rather than padding the
// key to a word, so the preimage is variable-length — and so is the charge,
// which is what keeps a long caller-supplied key from hashing for free.
func (s b20Storage) strMapSlot(base common.Hash, key string) common.Hash {
	if s.ctx != nil {
		s.ctx.chargeKeccak(len(key) + 32)
	}
	return crypto.Keccak256Hash([]byte(key), base.Bytes())
}

// newB20Storage returns an unmetered view (read-only queries, tests).
func newB20Storage(state StateDB, token common.Address) b20Storage {
	return b20Storage{state: state, token: token}
}

// newMeteredB20Storage returns a view bound to ctx.Self that charges gas for
// each slot access.
func newMeteredB20Storage(ctx *PrecompileContext) b20Storage {
	return b20Storage{state: ctx.StateDB, token: ctx.Self, ctx: ctx}
}

// chargeRead meters an SLOAD-equivalent at EIP-2929 prices: warm always, plus
// the cold surcharge on the first touch of the slot in this transaction.
func (s b20Storage) chargeRead(slot common.Hash) {
	if s.ctx == nil {
		return
	}
	if _, warm := s.state.SlotInAccessList(s.token, slot); warm {
		s.ctx.chargeStateGas(params.WarmStorageReadCostEIP2929)
		return
	}
	s.state.AddSlotToAccessList(s.token, slot)
	s.ctx.chargeStateGas(params.ColdSloadCostEIP2929)
}

func (s b20Storage) getWord(slot common.Hash) common.Hash {
	s.chargeRead(slot)
	return s.state.GetState(s.token, slot)
}

// setWord writes a slot after metering it under EIP-2200 net metering with
// EIP-3529 refunds (see chargeStorageWrite). A write refused by the reentrancy
// sentry does not happen at all; the context is marked out of gas and the
// dispatcher fails the call.
func (s b20Storage) setWord(slot, val common.Hash) {
	if !s.chargeStorageWrite(slot, val) {
		return
	}
	s.state.SetState(s.token, slot, val)
}

// --- fixed uint256 fields ---------------------------------------------------

func (s b20Storage) getU256(offset uint64) *uint256.Int {
	return new(uint256.Int).SetBytes(s.getWord(slotAt(offset)).Bytes())
}

func (s b20Storage) setU256(offset uint64, v *uint256.Int) {
	s.setWord(slotAt(offset), common.Hash(v.Bytes32()))
}

func (s b20Storage) totalSupply() *uint256.Int     { return s.getU256(b20SlotTotalSupply) }
func (s b20Storage) setTotalSupply(v *uint256.Int) { s.setU256(b20SlotTotalSupply, v) }
func (s b20Storage) supplyCap() *uint256.Int       { return s.getU256(b20SlotSupplyCap) }
func (s b20Storage) setSupplyCap(v *uint256.Int)   { s.setU256(b20SlotSupplyCap, v) }
func (s b20Storage) adminCount() *uint256.Int      { return s.getU256(b20SlotAdminCount) }
func (s b20Storage) setAdminCount(v *uint256.Int)  { s.setU256(b20SlotAdminCount, v) }
func (s b20Storage) paused() *uint256.Int          { return s.getU256(b20SlotPaused) }
func (s b20Storage) setPaused(v *uint256.Int)      { s.setU256(b20SlotPaused, v) }

// --- balances / allowances / nonces ----------------------------------------

// Deriving a mapping slot is a metered keccak, so a read-modify-write that goes
// through balanceOf then setBalance pays for the hash twice where a Solidity
// implementation computes it once. The slot-taking forms below let a caller
// derive once and reuse; the address-taking forms remain for single accesses,
// views and tests.

func (s b20Storage) balanceSlot(a common.Address) common.Hash {
	return s.mapSlot(slotAt(b20SlotBalances), addrKey(a))
}

func (s b20Storage) allowanceSlot(owner, spender common.Address) common.Hash {
	return s.mapSlot(s.mapSlot(slotAt(b20SlotAllowances), addrKey(owner)), addrKey(spender))
}

func (s b20Storage) getU256At(slot common.Hash) *uint256.Int {
	return new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
}

func (s b20Storage) setU256At(slot common.Hash, v *uint256.Int) {
	s.setWord(slot, common.Hash(v.Bytes32()))
}

func (s b20Storage) balanceOf(a common.Address) *uint256.Int {
	return s.getU256At(s.balanceSlot(a))
}

func (s b20Storage) setBalance(a common.Address, v *uint256.Int) {
	s.setU256At(s.balanceSlot(a), v)
}

func (s b20Storage) allowance(owner, spender common.Address) *uint256.Int {
	return s.getU256At(s.allowanceSlot(owner, spender))
}

func (s b20Storage) setAllowance(owner, spender common.Address, v *uint256.Int) {
	s.setU256At(s.allowanceSlot(owner, spender), v)
}

func (s b20Storage) nonce(owner common.Address) *uint256.Int {
	slot := s.mapSlot(slotAt(b20SlotNonces), addrKey(owner))
	return new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
}

func (s b20Storage) setNonce(owner common.Address, v *uint256.Int) {
	slot := s.mapSlot(slotAt(b20SlotNonces), addrKey(owner))
	s.setWord(slot, common.Hash(v.Bytes32()))
}

// --- roles ------------------------------------------------------------------

func (s b20Storage) hasRole(role common.Hash, a common.Address) bool {
	inner := s.mapSlot(slotAt(b20SlotRoles), role)
	slot := s.mapSlot(inner, addrKey(a))
	return s.getWord(slot) != (common.Hash{})
}

func (s b20Storage) setRole(role common.Hash, a common.Address, enabled bool) {
	inner := s.mapSlot(slotAt(b20SlotRoles), role)
	slot := s.mapSlot(inner, addrKey(a))
	var v common.Hash
	if enabled {
		v[31] = 1
	}
	s.setWord(slot, v)
}

func (s b20Storage) roleAdmin(role common.Hash) common.Hash {
	return s.getWord(s.mapSlot(slotAt(b20SlotRoleAdmins), role))
}

func (s b20Storage) setRoleAdmin(role, admin common.Hash) {
	s.setWord(s.mapSlot(slotAt(b20SlotRoleAdmins), role), admin)
}

// --- packed policy ids ------------------------------------------------------

func (s b20Storage) getPackedU64(offset uint64, byteOff uint) uint64 {
	word := new(uint256.Int).SetBytes(s.getWord(slotAt(offset)).Bytes())
	return word.Rsh(word, byteOff*8).Uint64()
}

// setPackedU64 writes v into the u64 lane at byteOff, preserving the other lanes.
func (s b20Storage) setPackedU64(offset uint64, byteOff uint, v uint64) {
	slot := slotAt(offset)
	word := new(uint256.Int).SetBytes(s.getWord(slot).Bytes())
	lane := new(uint256.Int).Lsh(uint256.NewInt(0xffffffffffffffff), byteOff*8)
	word.And(word, lane.Not(lane))
	word.Or(word, new(uint256.Int).Lsh(uint256.NewInt(v), byteOff*8))
	s.setWord(slot, common.Hash(word.Bytes32()))
}

// transferPolicies reads all three transfer-side ids with one storage access:
// they share a slot, so reading them separately pays for the same slot three
// times. seizePolicies does the same for its pair.
func (s b20Storage) transferPolicies() (sender, receiver, executor uint64) {
	w := s.getU256At(slotAt(b20SlotTransferPolicies))
	return packedLane(w, b20OffTransferSender), packedLane(w, b20OffTransferReceiver),
		packedLane(w, b20OffTransferExecutor)
}

func (s b20Storage) seizePolicies() (holder, receiver uint64) {
	w := s.getU256At(slotAt(b20SlotSeizePolicies))
	return packedLane(w, b20OffSeizeHolder), packedLane(w, b20OffSeizeReceiver)
}

// packedLane extracts the u64 lane at byteOff from an already-read slot value.
func packedLane(word *uint256.Int, byteOff uint) uint64 {
	return new(uint256.Int).Rsh(word, byteOff*8).Uint64()
}

func (s b20Storage) transferSenderPolicy() uint64 {
	return s.getPackedU64(b20SlotTransferPolicies, b20OffTransferSender)
}
func (s b20Storage) transferReceiverPolicy() uint64 {
	return s.getPackedU64(b20SlotTransferPolicies, b20OffTransferReceiver)
}
func (s b20Storage) transferExecutorPolicy() uint64 {
	return s.getPackedU64(b20SlotTransferPolicies, b20OffTransferExecutor)
}
func (s b20Storage) mintReceiverPolicy() uint64 {
	return s.getPackedU64(b20SlotMintPolicy, b20OffMintReceiver)
}
func (s b20Storage) seizeHolderPolicy() uint64 {
	return s.getPackedU64(b20SlotSeizePolicies, b20OffSeizeHolder)
}
func (s b20Storage) seizeReceiverPolicy() uint64 {
	return s.getPackedU64(b20SlotSeizePolicies, b20OffSeizeReceiver)
}
func (s b20Storage) setTransferSenderPolicy(id uint64) {
	s.setPackedU64(b20SlotTransferPolicies, b20OffTransferSender, id)
}
func (s b20Storage) setTransferReceiverPolicy(id uint64) {
	s.setPackedU64(b20SlotTransferPolicies, b20OffTransferReceiver, id)
}
func (s b20Storage) setTransferExecutorPolicy(id uint64) {
	s.setPackedU64(b20SlotTransferPolicies, b20OffTransferExecutor, id)
}
func (s b20Storage) setMintReceiverPolicy(id uint64) {
	s.setPackedU64(b20SlotMintPolicy, b20OffMintReceiver, id)
}
func (s b20Storage) setSeizeHolderPolicy(id uint64) {
	s.setPackedU64(b20SlotSeizePolicies, b20OffSeizeHolder, id)
}
func (s b20Storage) setSeizeReceiverPolicy(id uint64) {
	s.setPackedU64(b20SlotSeizePolicies, b20OffSeizeReceiver, id)
}

// --- strings (Solidity storage encoding) ------------------------------------

func (s b20Storage) getString(offset uint64) string { return s.getStringAt(slotAt(offset)) }
func (s b20Storage) setString(offset uint64, str string) {
	s.setStringAt(slotAt(offset), str)
}

// stringDataRoot derives the slot a long string's data begins at, keccak256 of
// the length slot, and meters the hash of that 32-byte preimage. Deriving a
// mapping slot is metered the same way (see mapSlot); a long string's data root
// is no less a runtime keccak just because the preimage is one word.
func (s b20Storage) stringDataRoot(slot common.Hash) *uint256.Int {
	if s.ctx != nil {
		s.ctx.chargeKeccak(32)
	}
	return new(uint256.Int).SetBytes(crypto.Keccak256(slot.Bytes()))
}

// getStringAt / setStringAt read and write a Solidity string at an arbitrary
// slot (used for fixed fields and for string-keyed mapping values).
func (s b20Storage) getStringAt(slot common.Hash) string {
	word := s.getWord(slot)
	if word[31]&1 == 0 {
		// short string: content in the high bytes, low byte holds 2*len.
		n := int(word[31]) / 2
		return string(word[:n])
	}
	// long string: slot holds 2*len+1; content starts at keccak256(slot).
	length := (new(uint256.Int).SetBytes(word.Bytes()).Uint64() - 1) / 2
	base := s.stringDataRoot(slot)
	out := make([]byte, 0, length)
	for i := uint64(0); i < length; i += 32 {
		chunkSlot := common.Hash(new(uint256.Int).AddUint64(base, i/32).Bytes32())
		chunk := s.getWord(chunkSlot)
		out = append(out, chunk[:]...)
	}
	return string(out[:length])
}

// setStringAt writes a string, releasing whatever the previous value held.
//
// Solidity's string assignment zeroes the tail slots the old value occupied and
// no longer needs. Skipping that would leave the state root diverging from a
// Solidity reference implementation and forfeit the clearing refunds — which
// only became reachable once name, symbol and contractURI turned mutable.
func (s b20Storage) setStringAt(slot common.Hash, str string) {
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
		s.setWord(slot, word)
	} else {
		s.setWord(slot, common.Hash(uint256.NewInt(uint64(len(b)*2+1)).Bytes32()))
	}
	if newChunks == 0 && oldChunks == 0 {
		return // wholly inline, before and after: no data region exists
	}
	// One keccak covers writing the new chunks and releasing the old ones, as
	// it would in Solidity — deriving the root twice would overcharge.
	base := s.stringDataRoot(slot)
	for i := uint64(0); i < newChunks; i++ {
		if s.ctx != nil && s.ctx.OutOfGas() {
			return
		}
		var chunk common.Hash
		copy(chunk[:], b[i*32:])
		s.setWord(common.Hash(new(uint256.Int).AddUint64(base, i).Bytes32()), chunk)
	}
	for i := newChunks; i < oldChunks; i++ {
		s.setWord(common.Hash(new(uint256.Int).AddUint64(base, i).Bytes32()), common.Hash{})
	}
}

// stringChunks reports how many tail slots the string currently at slot
// occupies. A short string is held inline and occupies none.
func (s b20Storage) stringChunks(slot common.Hash) uint64 {
	word := s.getWord(slot)
	if word[31]&1 == 0 {
		return 0
	}
	length := (new(uint256.Int).SetBytes(word.Bytes()).Uint64() - 1) / 2
	return (length + 31) / 32
}

func (s b20Storage) name() string            { return s.getString(b20SlotName) }
func (s b20Storage) setName(v string)        { s.setString(b20SlotName, v) }
func (s b20Storage) symbol() string          { return s.getString(b20SlotSymbol) }
func (s b20Storage) setSymbol(v string)      { s.setString(b20SlotSymbol, v) }
func (s b20Storage) contractURI() string     { return s.getString(b20SlotContractURI) }
func (s b20Storage) setContractURI(v string) { s.setString(b20SlotContractURI, v) }
