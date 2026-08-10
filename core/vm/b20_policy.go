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
	"github.com/holiman/uint256"
)

// PolicyRegistry: a chain-shared allow/block-list registry. A policy is a set
// of addresses plus a type, referenced by tokens via a self-describing uint64
// id (high byte = type, low 56 bits = global counter). Reads never revert (they
// sit on every transfer's hot path); writes are admin-gated.

// B20PolicyRegistryAddress is the singleton registry precompile (BEP-702 §3.1).
var B20PolicyRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000001")

const b20PolicyNamespace = "bsc.policy_registry"

const (
	b20PolicyBlocklist = 0
	b20PolicyAllowlist = 1
	b20PolicyBatchMax  = 64
	b20PolicyFirstID   = 2 // counters 0 and 1 belong to the two sentinels

	// Sentinel policy ids, seeded at initialization and always valid to bind.
	b20PolicyAlwaysAllow = 0                 // blocklist type, empty -> allow all
	b20PolicyAlwaysBlock = uint64(1)<<56 | 1 // allowlist type, empty -> block all

	// b20PolicyCounterMax bounds the 56-bit counter space. Creation is refused at
	// the boundary rather than allowed to carry into the type byte, where it
	// would collide ids across types and could reach a sentinel.
	b20PolicyCounterMax = uint64(1)<<56 - 1
)

// Storage layout, mirroring base-std's PolicyRegistryStorage. Slots are
// append-only: never reorder them across forks.
const (
	polSlotPolicies      = 0 // mapping(uint64 => packed word)
	polSlotMembers       = 1 // mapping(uint64 => mapping(address => bool))
	polSlotPendingAdmins = 2 // mapping(uint64 => address)
	polSlotCounter       = 3 // uint64
	// Slot 4 is reserved for composite-policy children, which base-std adds in a
	// later logic version. Nothing may reuse it.
)

// A policy's existence and its admin share one storage word:
//
//	bit 255      exists
//	bits 254:160 reserved, zero
//	bits 159:0   admin
//
// The exists bit is set on every write, which is what lets one word carry both:
// the zero word is an unambiguous "never written" even for a policy whose admin
// has been renounced to the zero address.
var polExistsBit = new(uint256.Int).Lsh(uint256.NewInt(1), 255)

func packPolicy(admin common.Address) common.Hash {
	w := new(uint256.Int).SetBytes(admin.Bytes())
	return common.Hash(w.Or(w, polExistsBit).Bytes32())
}

func polWordExists(w common.Hash) bool          { return w[0]&0x80 != 0 }
func polWordAdmin(w common.Hash) common.Address { return common.BytesToAddress(w[12:]) }

// polIDType returns the type byte a policy id encodes.
func polIDType(id uint64) byte { return byte(id >> 56) }

// polIDWellFormed reports whether an id's type byte names a real policy type.
func polIDWellFormed(id uint64) bool { return polIDType(id) <= b20PolicyAllowlist }

func isSentinelPolicy(id uint64) bool {
	return id == b20PolicyAlwaysAllow || id == b20PolicyAlwaysBlock
}

var b20PolicyRoot = erc7201Root(b20PolicyNamespace)

var (
	selCreatePolicy             = selector("createPolicy(address,uint8)")
	selCreatePolicyWithAccounts = selector("createPolicyWithAccounts(address,uint8,address[])")
	selUpdateAllowlist          = selector("updateAllowlist(uint64,bool,address[])")
	selUpdateBlocklist          = selector("updateBlocklist(uint64,bool,address[])")
	selStageUpdateAdmin         = selector("stageUpdateAdmin(uint64,address)")
	selFinalizeUpdateAdmin      = selector("finalizeUpdateAdmin(uint64)")
	selRenounceAdmin            = selector("renounceAdmin(uint64)")
	selIsAuthorized             = selector("isAuthorized(uint64,address)")
	selPolicyExists             = selector("policyExists(uint64)")
	selPolicyAdmin              = selector("policyAdmin(uint64)")
	selPendingPolicyAdmin       = selector("pendingPolicyAdmin(uint64)")

	b20TopicPolicyCreated      = eventTopic("PolicyCreated(uint64,address,uint8)")
	b20TopicPolicyAdminStaged  = eventTopic("PolicyAdminStaged(uint64,address,address)")
	b20TopicPolicyAdminUpdated = eventTopic("PolicyAdminUpdated(uint64,address,address)")
	b20TopicAllowlistUpdated   = eventTopic("AllowlistUpdated(uint64,address,bool,address[])")
	b20TopicBlocklistUpdated   = eventTopic("BlocklistUpdated(uint64,address,bool,address[])")
)

// emitPolicyAdminUpdated logs an admin transition. Creation, handover and
// renunciation all report through this one event, so a policy's whole admin
// history is one filter: creation is (0 -> initial admin) and renunciation is
// (admin -> 0).
func emitPolicyAdminUpdated(ctx *PrecompileContext, id uint64, previous, next common.Address) {
	ctx.AddLog([]common.Hash{
		b20TopicPolicyAdminUpdated, idKey(id), addrKey(previous), addrKey(next),
	}, nil)
}

// emitMembersUpdated logs a membership change under the event belonging to the
// policy's own type. base-std reports allowlists and blocklists separately
// rather than through one merged event, so a consumer can subscribe to just the
// list it cares about.
func emitMembersUpdated(ctx *PrecompileContext, ptype byte, id uint64, updater common.Address, included bool, accounts []common.Hash) {
	topic := b20TopicBlocklistUpdated
	if ptype == b20PolicyAllowlist {
		topic = b20TopicAllowlistUpdated
	}
	ctx.AddLog(
		[]common.Hash{topic, idKey(id), addrKey(updater)},
		encodeTuple(abiWord(boolWord(included)), abiWordArray(accounts)),
	)
}

func boolWord(b bool) common.Hash {
	var w common.Hash
	if b {
		w[31] = 1
	}
	return w
}

// policyReg is a gas-metered view over the registry's storage.
type policyReg struct{ s b20Storage }

func newPolicyReg(ctx *PrecompileContext) policyReg {
	return policyReg{s: b20Storage{state: ctx.StateDB, token: B20PolicyRegistryAddress, ctx: ctx}}
}

func polSlot(offset uint64) common.Hash { return offsetSlot(b20PolicyRoot, offset) }

func idKey(id uint64) common.Hash { return common.Hash(uint256.NewInt(id).Bytes32()) }

// isEnumWord reports whether an ABI word strictly encodes an enum/bool value
// in [0, max]: every byte above the last must be zero.
func isEnumWord(w common.Hash, max byte) bool {
	for _, b := range w[:31] {
		if b != 0 {
			return false
		}
	}
	return w[31] <= max
}

func (p policyReg) counter() uint64 {
	return new(uint256.Int).SetBytes(p.s.getWord(polSlot(polSlotCounter)).Bytes()).Uint64()
}
func (p policyReg) setCounter(v uint64) {
	p.s.setWord(polSlot(polSlotCounter), common.Hash(uint256.NewInt(v).Bytes32()))
}

// policyWord reads a policy's packed existence-and-admin word.
func (p policyReg) policyWord(id uint64) common.Hash {
	return p.s.getWord(p.s.mapSlot(polSlot(polSlotPolicies), idKey(id)))
}

// setPolicyAdmin writes the packed word, which marks the policy as existing.
// Renouncing goes through here too, with the zero address: the exists bit stays
// set, so a renounced policy remains distinguishable from one never created.
func (p policyReg) setPolicyAdmin(id uint64, a common.Address) {
	p.s.setWord(p.s.mapSlot(polSlot(polSlotPolicies), idKey(id)), packPolicy(a))
}

func (p policyReg) exists(id uint64) bool          { return polWordExists(p.policyWord(id)) }
func (p policyReg) admin(id uint64) common.Address { return polWordAdmin(p.policyWord(id)) }

func (p policyReg) pending(id uint64) common.Address {
	return common.BytesToAddress(p.s.getWord(p.s.mapSlot(polSlot(polSlotPendingAdmins), idKey(id))).Bytes())
}
func (p policyReg) setPending(id uint64, a common.Address) {
	p.s.setWord(p.s.mapSlot(polSlot(polSlotPendingAdmins), idKey(id)), addrKey(a))
}
func (p policyReg) member(id uint64, account common.Address) bool {
	inner := p.s.mapSlot(polSlot(polSlotMembers), idKey(id))
	return p.s.getWord(p.s.mapSlot(inner, addrKey(account))) != (common.Hash{})
}
func (p policyReg) setMember(id uint64, account common.Address, in bool) {
	inner := p.s.mapSlot(polSlot(polSlotMembers), idKey(id))
	var v common.Hash
	if in {
		v[31] = 1
	}
	p.s.setWord(p.s.mapSlot(inner, addrKey(account)), v)
}

// isAuthorized answers whether account may be operated under policy id. It
// never reverts: a malformed or never-created id collapses to empty-set
// semantics (blocklist → allow all, allowlist → block all).
//
// The two sentinels answer from their id alone rather than from membership.
// Their emptiness would give the same answer, but ALWAYS_ALLOW is the value
// every unset policy field holds, so it has to be right before initialization
// has run — and answering constantly also means no membership write can ever
// change what a sentinel means.
func (p policyReg) isAuthorized(id uint64, account common.Address) bool {
	if !polIDWellFormed(id) {
		return false
	}
	switch id {
	case b20PolicyAlwaysAllow:
		return true
	case b20PolicyAlwaysBlock:
		return false
	}
	member := p.member(id, account)
	if polIDType(id) == b20PolicyAllowlist {
		return member
	}
	return !member
}

// policyExists is the ABI view. A malformed id never exists; the sentinels
// always do, before initialization included.
func (p policyReg) policyExists(id uint64) bool {
	if !polIDWellFormed(id) {
		return false
	}
	if isSentinelPolicy(id) {
		return true
	}
	return p.exists(id)
}

// policyAdminOf is the ABI view: zero for a malformed id and for any policy that
// does not exist, so a caller cannot mistake an unwritten slot for an admin.
func (p policyReg) policyAdminOf(id uint64) common.Address {
	if !polIDWellFormed(id) {
		return common.Address{}
	}
	w := p.policyWord(id)
	if !polWordExists(w) {
		return common.Address{}
	}
	return polWordAdmin(w)
}

// pendingPolicyAdminOf is the ABI view. The sentinels can never have a pending
// admin, so their slot is never even read.
func (p policyReg) pendingPolicyAdminOf(id uint64) common.Address {
	if !polIDWellFormed(id) || isSentinelPolicy(id) {
		return common.Address{}
	}
	return p.pending(id)
}

// ensureInitialized seeds the two sentinel policies and leaves the counter on
// the first id available to callers. Like base-std it gates on the counter, not
// on the sentinel words, so a harness that pre-warms the account's bytecode
// cannot cause the seeding to be skipped.
func (p policyReg) ensureInitialized() uint64 {
	c := p.counter()
	if c >= b20PolicyFirstID {
		return c
	}
	// Both sentinels are born renounced: they exist, and nobody administers them.
	p.setPolicyAdmin(b20PolicyAlwaysAllow, common.Address{})
	p.setPolicyAdmin(b20PolicyAlwaysBlock, common.Address{})
	p.setCounter(b20PolicyFirstID)
	return b20PolicyFirstID
}

// b20PolicyPrecompile is the singleton registry precompile.
type b20PolicyPrecompile struct{ b20StatefulBase }

func (p *b20PolicyPrecompile) Name() string                    { return "B20PolicyRegistry" }
func (p *b20PolicyPrecompile) RequiredGas(input []byte) uint64 { return 0 } // priced inside RunStateful

func (p *b20PolicyPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	ret, err := runB20Policy(ctx, input)
	return finishB20Metered(ctx, ret, err)
}

var _ StatefulPrecompiledContract = (*b20PolicyPrecompile)(nil)

func runB20Policy(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]
	reg := newPolicyReg(ctx)

	switch sel {
	// reads (allowed in read-only frames, never revert on lookup)
	case selIsAuthorized:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		acct, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		return encBool(reg.isAuthorized(id, acct)), nil
	case selPolicyExists:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(reg.policyExists(id)), nil
	case selPolicyAdmin:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return addrKey(reg.policyAdminOf(id)).Bytes(), nil
	case selPendingPolicyAdmin:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return addrKey(reg.pendingPolicyAdminOf(id)).Bytes(), nil

	}

	// Writes. Recognize the selector first, so an unknown one still reverts as
	// an unknown selector rather than as an inactive feature, then apply the
	// activation gate: every non-view method sits behind it, so a deactivation
	// freezes membership and admin changes on policies that already exist
	// (BEP-702 section 3.15).
	//
	// Two orderings here differ from base-std, both deliberately. Write
	// protection is checked before anything else, where base-std leaves it to the
	// storage layer: a caller in a static frame is refused for that reason alone,
	// rather than being told its feature is closed or its arguments are bad. And
	// the activation gate precedes argument decoding, so a closed feature reports
	// itself whatever the payload — base-std validates the encoding first, which
	// on a closed feature surfaces a decode failure instead. Ours reports the
	// condition the caller can act on, and reverts on the same set of inputs.
	switch sel {
	case selCreatePolicy, selCreatePolicyWithAccounts, selUpdateAllowlist,
		selUpdateBlocklist, selStageUpdateAdmin, selFinalizeUpdateAdmin, selRenounceAdmin:
		if ctx.ReadOnly {
			return nil, ErrWriteProtection
		}
		if err := ensureFeatureActivated(ctx, featurePolicyRegistry); err != nil {
			return nil, err
		}
		ctx.ensureSentinel()
	default:
		return nil, ErrExecutionReverted // unknown selector
	}

	switch sel {
	case selCreatePolicy:
		return createPolicy(ctx, reg, args, false)
	case selCreatePolicyWithAccounts:
		return createPolicy(ctx, reg, args, true)
	case selUpdateAllowlist:
		return nil, updateMembers(ctx, reg, args, b20PolicyAllowlist)
	case selUpdateBlocklist:
		return nil, updateMembers(ctx, reg, args, b20PolicyBlocklist)
	case selStageUpdateAdmin:
		return nil, stageUpdateAdmin(ctx, reg, args)
	case selFinalizeUpdateAdmin:
		return nil, finalizeUpdateAdmin(ctx, reg, args)
	case selRenounceAdmin:
		return nil, renounceAdmin(ctx, reg, args)
	}
	return nil, ErrExecutionReverted // unreachable: the gate above is exhaustive
}

func createPolicy(ctx *PrecompileContext, reg policyReg, args []byte, withAccounts bool) ([]byte, error) {
	if ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	admin, err := readAddress(args, 0)
	if err != nil {
		return nil, err
	}
	ptypeWord, err := readWord(args, 1)
	if err != nil {
		return nil, err
	}
	ptype := ptypeWord[31]
	if !isEnumWord(ptypeWord, b20PolicyAllowlist) {
		return nil, revPanic(0x21)
	}
	if admin == (common.Address{}) {
		return nil, revB20("ZeroAddress()", errSelZeroAddress)
	}

	// The batch is decoded and bounded before any state is written, matching
	// base-std. An enclosing revert would discard premature writes anyway, but it
	// would not give back the gas they were metered at.
	var accounts []common.Hash
	if withAccounts {
		if accounts, err = readWordArray(args, 2); err != nil {
			return nil, err
		}
		if len(accounts) > b20PolicyBatchMax {
			return nil, revB20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(b20PolicyBatchMax))
		}
	}

	c := reg.ensureInitialized()
	// The counter shares its 56 bits across both types, so exhausting it must be
	// refused rather than allowed to carry into the type byte.
	if c >= b20PolicyCounterMax {
		return nil, revPanic(0x11)
	}
	id := uint64(ptype)<<56 | c
	reg.setCounter(c + 1)
	reg.setPolicyAdmin(id, admin)
	ctx.AddLog([]common.Hash{b20TopicPolicyCreated, idKey(id), addrKey(ctx.Caller)}, wU8(ptype).Bytes())
	// The initial admin is reported as a transition from nobody, so it lands in
	// the same event stream as every later handover.
	emitPolicyAdminUpdated(ctx, id, common.Address{}, admin)

	if withAccounts {
		for _, a := range accounts {
			reg.setMember(id, common.BytesToAddress(a.Bytes()), true)
		}
		// Emitted even for an empty batch: the call form is part of the record.
		emitMembersUpdated(ctx, ptype, id, ctx.Caller, true, accounts)
	}
	return encU256(uint256.NewInt(id)), nil
}

func updateMembers(ctx *PrecompileContext, reg policyReg, args []byte, wantType byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	pid, err := readU64(args, 0)
	if err != nil {
		return err
	}
	inWord, err := readWord(args, 1)
	if err != nil {
		return err
	}
	if !isEnumWord(inWord, 1) { // strict ABI bool
		return revPanic(0x21)
	}
	accounts, err := readWordArray(args, 2)
	if err != nil {
		return err
	}
	// Order matters: it is observable through which error the caller receives, so
	// it follows base-std's canonical existence -> type -> admin -> batch.
	if err := requireCustomPolicy(reg, pid); err != nil {
		return err
	}
	if polIDType(pid) != wantType {
		return revB20("IncompatiblePolicyType()", errSelIncompatibleType)
	}
	if err := requirePolicyAdmin(reg, pid, ctx.Caller); err != nil {
		return err
	}
	if len(accounts) > b20PolicyBatchMax {
		return revB20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(b20PolicyBatchMax))
	}
	in := inWord[31] != 0
	for _, a := range accounts {
		reg.setMember(pid, common.BytesToAddress(a.Bytes()), in)
	}
	emitMembersUpdated(ctx, wantType, pid, ctx.Caller, in, accounts)
	return nil
}

func stageUpdateAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	id, err := readU64(args, 0)
	if err != nil {
		return err
	}
	newAdmin, err := readAddress(args, 1)
	if err != nil {
		return err
	}
	if err := requirePolicyAdmin(reg, id, ctx.Caller); err != nil {
		return err
	}
	reg.setPending(id, newAdmin)
	// Emitted for a cancellation too, where newAdmin is zero: withdrawing a
	// nomination is a governance action and should not be a silent one.
	ctx.AddLog([]common.Hash{
		b20TopicPolicyAdminStaged, idKey(id), addrKey(ctx.Caller), addrKey(newAdmin),
	}, nil)
	return nil
}

func finalizeUpdateAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	pid, err := readU64(args, 0)
	if err != nil {
		return err
	}
	if err := requireCustomPolicy(reg, pid); err != nil {
		return err
	}
	pending := reg.pending(pid)
	if pending == (common.Address{}) {
		return revB20("NoPendingAdmin()", errSelNoPendingAdmin)
	}
	if pending != ctx.Caller || ctx.Caller == (common.Address{}) {
		return revB20("Unauthorized()", errSelUnauthorized)
	}
	previous := reg.admin(pid)
	reg.setPolicyAdmin(pid, ctx.Caller)
	reg.setPending(pid, common.Address{})
	emitPolicyAdminUpdated(ctx, pid, previous, ctx.Caller)
	return nil
}

func renounceAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	pid, err := readU64(args, 0)
	if err != nil {
		return err
	}
	if err := requirePolicyAdmin(reg, pid, ctx.Caller); err != nil {
		return err
	}
	// Frozen, not deleted: the packed word keeps its exists bit, so the policy
	// stays distinguishable from one never created and its membership keeps
	// answering reads.
	reg.setPolicyAdmin(pid, common.Address{})
	reg.setPending(pid, common.Address{})
	emitPolicyAdminUpdated(ctx, pid, ctx.Caller, common.Address{})
	return nil
}

// requireCustomPolicy reverts PolicyNotFound unless the policy has been written.
// The sentinels pass: they are seeded with the exists bit set, and it is their
// zero admin that keeps them un-administrable.
func requireCustomPolicy(reg policyReg, id uint64) error {
	if !polIDWellFormed(id) || !reg.exists(id) {
		return revB20("PolicyNotFound()", errSelPolicyNotFound)
	}
	return nil
}

// requirePolicyAdmin reverts unless the policy exists and caller is its admin.
//
// The zero-admin guard is stricter than base-std, which relies on the caller
// never being the zero address to keep a renounced policy frozen. Both refuse
// every reachable call; this way the freeze does not depend on that assumption.
func requirePolicyAdmin(reg policyReg, id uint64, caller common.Address) error {
	if err := requireCustomPolicy(reg, id); err != nil {
		return err
	}
	if admin := reg.admin(id); admin == (common.Address{}) || admin != caller {
		return revB20("Unauthorized()", errSelUnauthorized)
	}
	return nil
}
