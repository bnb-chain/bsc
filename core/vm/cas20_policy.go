package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// PolicyRegistry: a chain-shared allow/block-list registry. A policy is a set
// of addresses plus a type, referenced by tokens via a self-describing uint64
// id (high byte = type, low 56 bits = global counter). Reads never revert (they
// sit on every transfer's hot path); writes are admin-gated.

// CAS20PolicyRegistryAddress is the singleton registry precompile (BEP-702 §3.1).
var CAS20PolicyRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000002")

const cas20PolicyNamespace = "bsc.policy_registry"

const (
	cas20PolicyBlocklist = 0
	cas20PolicyAllowlist = 1
	cas20PolicyUnion     = 2 // authorized by ANY child (Cobalt)
	cas20PolicyIntersect = 3 // authorized by EVERY child (Cobalt)
	cas20PolicyBatchMax  = 64

	// A composite references between two and four simple policies, inclusive.
	cas20CompositeMinChildren = 2
	cas20CompositeMaxChildren = 4
	cas20PolicyFirstID        = 2 // counters 0 and 1 belong to the two sentinels

	// Sentinel policy ids, seeded at initialization and always valid to bind.
	cas20PolicyAlwaysAllow = 0                 // blocklist type, empty -> allow all
	cas20PolicyAlwaysBlock = uint64(1)<<56 | 1 // allowlist type, empty -> block all

	// cas20PolicyCounterMax bounds the 56-bit counter space. Creation is refused at
	// the boundary rather than allowed to carry into the type byte, where it
	// would collide ids across types and could reach a sentinel.
	cas20PolicyCounterMax = uint64(1)<<56 - 1
)

// Storage layout (BEP-702 3.17). Slots are append-only: never reorder them
// across forks.
const (
	polSlotPolicies      = 0 // mapping(uint64 => packed word)
	polSlotMembers       = 1 // mapping(uint64 => mapping(address => bool))
	polSlotPendingAdmins = 2 // mapping(uint64 => address)
	polSlotCounter       = 3 // uint64
	polSlotChildren      = 4 // mapping(uint64 => uint64[]) (Cobalt)
)

// A policy's existence and admin share one word: bit 255 exists, bits 159:0 the
// admin. That keeps a policy whose admin was renounced to zero distinct from an
// unwritten slot.
var polExistsBit = new(uint256.Int).Lsh(uint256.NewInt(1), 255)

func packPolicy(admin common.Address) common.Hash {
	w := new(uint256.Int).SetBytes(admin.Bytes())
	return w.Or(w, polExistsBit).Bytes32()
}

func polWordExists(w common.Hash) bool          { return w[0]&0x80 != 0 }
func polWordAdmin(w common.Hash) common.Address { return common.BytesToAddress(w[12:]) }

func polIDType(id uint64) byte { return byte(id >> 56) }

// polIDWellFormed reports whether an id's type byte names a real policy type.
func polIDWellFormed(id uint64) bool { return polIDType(id) <= cas20PolicyIntersect }

func isSentinelPolicy(id uint64) bool {
	return id == cas20PolicyAlwaysAllow || id == cas20PolicyAlwaysBlock
}

var cas20PolicyRoot = erc7201Root(cas20PolicyNamespace)

var (
	selCreatePolicy             = selector("createPolicy(address,uint8)")
	selCreatePolicyWithAccounts = selector("createPolicyWithAccounts(address,uint8,address[])")
	selCreateComposite          = selector("createCompositePolicy(address,uint8,uint64[])")
	selUpdateComposite          = selector("updateComposite(uint64,uint64[])")
	selCompositeChildIds        = selector("compositePolicyChildIds(uint64)")
	selMinCompositeChildren     = selector("MIN_COMPOSITE_CHILD_POLICIES()")
	selMaxCompositeChildren     = selector("MAX_COMPOSITE_CHILD_POLICIES()")
	selUpdateAllowlist          = selector("updateAllowlist(uint64,bool,address[])")
	selUpdateBlocklist          = selector("updateBlocklist(uint64,bool,address[])")
	selStageUpdateAdmin         = selector("stageUpdateAdmin(uint64,address)")
	selFinalizeUpdateAdmin      = selector("finalizeUpdateAdmin(uint64)")
	selRenounceAdmin            = selector("renounceAdmin(uint64)")
	selIsAuthorized             = selector("isAuthorized(uint64,address)")
	selPolicyExists             = selector("policyExists(uint64)")
	selPolicyAdmin              = selector("policyAdmin(uint64)")
	selPendingPolicyAdmin       = selector("pendingPolicyAdmin(uint64)")

	cas20TopicPolicyCreated      = eventTopic("PolicyCreated(uint64,address,uint8)")
	cas20TopicPolicyAdminStaged  = eventTopic("PolicyAdminStaged(uint64,address,address)")
	cas20TopicPolicyAdminUpdated = eventTopic("PolicyAdminUpdated(uint64,address,address)")
	cas20TopicCompositeUpdated   = eventTopic("CompositePolicyUpdated(uint64,address,uint64[])")
	cas20TopicAllowlistUpdated   = eventTopic("AllowlistUpdated(uint64,address,bool,address[])")
	cas20TopicBlocklistUpdated   = eventTopic("BlocklistUpdated(uint64,address,bool,address[])")
)

// emitPolicyAdminUpdated reports creation, handover and renunciation through one
// event, so a policy's whole admin history is a single filter.
func emitPolicyAdminUpdated(ctx *PrecompileContext, id uint64, previous, next common.Address) bool {
	return ctx.AddLog([]common.Hash{
		cas20TopicPolicyAdminUpdated, idKey(id), addrKey(previous), addrKey(next),
	}, nil)
}

// emitMembersUpdated reports under the event belonging to the policy's own type,
// so a consumer can subscribe to just the list it cares about.
func emitMembersUpdated(ctx *PrecompileContext, ptype byte, id uint64, updater common.Address, included bool, accounts []common.Hash) bool {
	topic := cas20TopicBlocklistUpdated
	if ptype == cas20PolicyAllowlist {
		topic = cas20TopicAllowlistUpdated
	}
	return ctx.AddLog(
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
type policyReg struct{ s cas20Storage }

func newPolicyReg(ctx *PrecompileContext) policyReg {
	return policyReg{s: newMeteredCAS20StorageAt(ctx, CAS20PolicyRegistryAddress)}
}

func polSlot(offset uint64) common.Hash { return offsetSlot(cas20PolicyRoot, offset) }

func idKey(id uint64) common.Hash { return uint256.NewInt(id).Bytes32() }

// isEnumWord reports whether an ABI word strictly encodes an enum/bool value
// in [0, max]: every byte above the last must be zero.
func isEnumWord(w common.Hash, max byte) bool {
	return wordFitsIn(w, 1) && w[31] <= max
}

func (p policyReg) counter() uint64 {
	return new(uint256.Int).SetBytes(p.s.getWord(polSlot(polSlotCounter)).Bytes()).Uint64()
}
func (p policyReg) setCounter(v uint64) {
	p.s.setWord(polSlot(polSlotCounter), uint256.NewInt(v).Bytes32())
}

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

// isAuthorized never reverts: it sits on every transfer's path. A malformed or
// absent policy takes empty-set semantics — which authorizes everyone for a
// BLOCKLIST or an INTERSECT and no one for an ALLOWLIST or a UNION — and the
// sentinel ids answer before the registry is initialized.
func (p policyReg) isAuthorized(id uint64, account common.Address) bool {
	if !polIDWellFormed(id) {
		return false
	}
	switch id {
	case cas20PolicyAlwaysAllow:
		return true
	case cas20PolicyAlwaysBlock:
		return false
	}
	// A composite has no members of its own: it asks its children, every time, so
	// mutating a child's membership changes the composite's verdict with no call on
	// the composite itself.
	switch polIDType(id) {
	case cas20PolicyUnion:
		for _, child := range p.children(id) {
			if p.isAuthorized(child, account) {
				return true
			}
		}
		return false
	case cas20PolicyIntersect:
		// An AND over no children is vacuously true, so a well-formed INTERSECT id
		// that names no policy authorizes everyone — the same tolerance a
		// never-created BLOCKLIST already gets, and for the same reason: binding
		// checks existence, so no token can reference one (BEP-702 3.8).
		for _, child := range p.children(id) {
			if !p.isAuthorized(child, account) {
				return false
			}
		}
		return true
	}
	member := p.member(id, account)
	if polIDType(id) == cas20PolicyAllowlist {
		return member
	}
	return !member
}

// polIsComposite reports whether an id names a UNION or INTERSECT policy.
func polIsComposite(id uint64) bool {
	t := polIDType(id)
	return t == cas20PolicyUnion || t == cas20PolicyIntersect
}

// children reads a composite's child set. Solidity stores a uint64[] as a length
// word at the mapping slot with the elements packed four per word from
// keccak256(slot), which is what the factory and a reference contract must agree
// on byte for byte.
func (p policyReg) childrenSlot(id uint64) common.Hash {
	return p.s.mapSlot(polSlot(polSlotChildren), idKey(id))
}

func (p policyReg) children(id uint64) []uint64 {
	slot := p.childrenSlot(id)
	n := new(uint256.Int).SetBytes(p.s.getWord(slot).Bytes()).Uint64()
	if n == 0 || n > cas20CompositeMaxChildren {
		return nil
	}
	base := p.s.stringDataRoot(slot)
	out := make([]uint64, 0, n)
	for i := uint64(0); i < n; i++ {
		if p.s.ctx != nil && p.s.ctx.OutOfGas() {
			return nil
		}
		w := p.s.getWord(new(uint256.Int).AddUint64(base, i/4).Bytes32())
		// Four uint64 lanes per word, LSB-first as Solidity packs them.
		lane := uint((i % 4) * 8)
		out = append(out, new(uint256.Int).Rsh(new(uint256.Int).SetBytes(w.Bytes()), lane*8).Uint64())
	}
	return out
}

func (p policyReg) setChildren(id uint64, kids []uint64) {
	slot := p.childrenSlot(id)
	p.s.setWord(slot, uint256.NewInt(uint64(len(kids))).Bytes32())
	base := p.s.stringDataRoot(slot)
	// Each word is built from scratch and written whole, so lanes past the new
	// length are zeroed rather than left behind. The loop then runs to the word
	// count the *maximum* set would need, not the new one, which clears any tail a
	// shrink orphaned — Solidity's array assignment does the same, and without it
	// the state root would diverge from a reference contract even though every read
	// agreed. Today the cap of four is exactly one word so the tail is empty; this
	// is what keeps that from being load-bearing.
	maxWords := (cas20CompositeMaxChildren + 3) / 4
	for w := 0; w < maxWords; w++ {
		packed := new(uint256.Int)
		for lane := 0; lane < 4 && w*4+lane < len(kids); lane++ {
			packed.Or(packed, new(uint256.Int).Lsh(uint256.NewInt(kids[w*4+lane]), uint(lane)*64))
		}
		slotW := new(uint256.Int).AddUint64(base, uint64(w)).Bytes32()
		if packed.IsZero() && w*4 >= len(kids) {
			// Nothing to store here; only write if something is already there, so a
			// fresh composite does not pay for clearing empty slots.
			if p.s.getWord(slotW) == (common.Hash{}) {
				continue
			}
		}
		p.s.setWord(slotW, packed.Bytes32())
	}
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
// the first id available to callers. It gates on the counter, not on the sentinel
// words, so a harness that pre-warms the account's bytecode cannot cause the
// seeding to be skipped.
func (p policyReg) ensureInitialized() uint64 {
	c := p.counter()
	if c >= cas20PolicyFirstID {
		return c
	}
	// Both sentinels are born renounced: they exist, and nobody administers them.
	p.setPolicyAdmin(cas20PolicyAlwaysAllow, common.Address{})
	p.setPolicyAdmin(cas20PolicyAlwaysBlock, common.Address{})
	p.setCounter(cas20PolicyFirstID)
	return cas20PolicyFirstID
}

// cas20PolicyPrecompile is the singleton registry precompile.
type cas20PolicyPrecompile struct{ cas20StatefulBase }

func (p *cas20PolicyPrecompile) Name() string { return "CAS20PolicyRegistry" }

func (p *cas20PolicyPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	ret, err := runCAS20Policy(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}

func runCAS20Policy(ctx *PrecompileContext, input []byte) ([]byte, error) {
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
	case selMinCompositeChildren:
		return encU256(uint256.NewInt(cas20CompositeMinChildren)), nil
	case selMaxCompositeChildren:
		return encU256(uint256.NewInt(cas20CompositeMaxChildren)), nil
	case selCompositeChildIds:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		kids := reg.children(id)
		words := make([]common.Hash, len(kids))
		for i, k := range kids {
			words[i] = wU64(k)
		}
		return encodeTuple(abiWordArray(words)), nil
	}

	// Writes: unknown selector, then static frame, then inactive feature, before
	// decoding arguments. The order is consensus-visible (BEP-702 3.15).
	//
	// This is the only place a write path checks ReadOnly. The handlers below used
	// to repeat it, which the reference implementation does not — it enforces the
	// call-frame invariants in the frame machinery, not per method — and repeating
	// it here bought nothing: none of them is reachable except through this switch.
	switch sel {
	case selCreatePolicy, selCreatePolicyWithAccounts, selUpdateAllowlist,
		selUpdateBlocklist, selStageUpdateAdmin, selFinalizeUpdateAdmin, selRenounceAdmin,
		selCreateComposite, selUpdateComposite:
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
	case selCreateComposite:
		return createCompositePolicy(ctx, reg, args)
	case selUpdateComposite:
		return nil, updateComposite(ctx, reg, args)
	case selUpdateAllowlist:
		return nil, updateMembers(ctx, reg, args, cas20PolicyAllowlist)
	case selUpdateBlocklist:
		return nil, updateMembers(ctx, reg, args, cas20PolicyBlocklist)
	case selStageUpdateAdmin:
		return nil, stageUpdateAdmin(ctx, reg, args)
	case selFinalizeUpdateAdmin:
		return nil, finalizeUpdateAdmin(ctx, reg, args)
	case selRenounceAdmin:
		return nil, renounceAdmin(ctx, reg, args)
	}
	return nil, ErrExecutionReverted // unreachable: the gate above is exhaustive
}

// validateChildren checks a proposed child set: the count, then that every entry
// exists, then that every entry is eligible — neither a composite nor a sentinel.
// The no-nesting rule keeps evaluation one level deep so isAuthorized cannot
// recurse without bound; a sentinel is refused because it stores no membership
// for a composite to consult.
func validateChildren(reg policyReg, kids []common.Hash) ([]uint64, error) {
	if len(kids) < cas20CompositeMinChildren || len(kids) > cas20CompositeMaxChildren {
		return nil, revCAS20("ChildPoliciesOutsideOfRange()", errSelChildrenOutOfRange)
	}
	out := make([]uint64, 0, len(kids))
	for _, w := range kids {
		// Strictly decoded, as Solidity's external decoder does: taking the low
		// eight bytes of a 32-byte word would let a caller name one policy in the
		// bytes that matter and anything at all in the rest, and we would act on
		// the former where a revert is owed.
		id, ok := u64FromWord(w)
		if !ok {
			return nil, ErrExecutionReverted
		}
		out = append(out, id)
	}
	// Two passes over the whole set, not one interleaved pass per child: a set
	// holding both a missing child and an ineligible one owes PolicyNotFound,
	// whichever comes first in the array. The order decides which error the
	// caller receives, so it is consensus (BEP-702 3.9).
	for _, id := range out {
		if !reg.policyExists(id) {
			return nil, revCAS20("PolicyNotFound()", errSelPolicyNotFound)
		}
	}
	// A sentinel is refused alongside a composite: only a simple policy the
	// registry actually minted can be a child.
	for _, id := range out {
		if isSentinelPolicy(id) || polIsComposite(id) {
			return nil, revCAS20("InvalidChildPolicy(uint64)", errSelInvalidChildPolicy, wU64(id))
		}
	}
	return out, nil
}

// emitCompositeUpdated logs the complete post-update child set, on creation and
// on every replacement.
func emitCompositeUpdated(ctx *PrecompileContext, id uint64, admin common.Address, kids []uint64) bool {
	words := make([]common.Hash, len(kids))
	for i, k := range kids {
		words[i] = wU64(k)
	}
	return ctx.AddLog([]common.Hash{cas20TopicCompositeUpdated, idKey(id), addrKey(admin)},
		encodeTuple(abiWordArray(words)))
}

// createCompositePolicy mints a UNION or INTERSECT over existing simple policies.
// The zero-admin guard comes first, as it does in the simple constructors: all
// three agree on the order, and it is observable through which error a caller
// receives (BEP-702 3.9).
func createCompositePolicy(ctx *PrecompileContext, reg policyReg, args []byte) ([]byte, error) {
	admin, err := readAddress(args, 0)
	if err != nil {
		return nil, err
	}
	ptypeWord, err := readWord(args, 1)
	if err != nil {
		return nil, err
	}
	if !isEnumWord(ptypeWord, cas20PolicyIntersect) {
		return nil, ErrExecutionReverted
	}
	ptype := ptypeWord[31]
	if admin == (common.Address{}) {
		return nil, revCAS20("ZeroAddress()", errSelZeroAddress)
	}
	if !polIsComposite(uint64(ptype) << 56) {
		return nil, revCAS20("IncompatiblePolicyType()", errSelIncompatibleType)
	}
	rawKids, err := readWordArray(args, 2)
	if err != nil {
		return nil, err
	}
	kids, err := validateChildren(reg, rawKids)
	if err != nil {
		return nil, err
	}

	c := reg.ensureInitialized()
	if c >= cas20PolicyCounterMax {
		return nil, revPanic(0x11)
	}
	id := uint64(ptype)<<56 | c
	reg.setCounter(c + 1)
	reg.setPolicyAdmin(id, admin)
	reg.setChildren(id, kids)
	if !ctx.AddLog([]common.Hash{cas20TopicPolicyCreated, idKey(id), addrKey(ctx.Caller)}, wU8(ptype).Bytes()) {
		return nil, ErrOutOfGas
	}
	if !emitPolicyAdminUpdated(ctx, id, common.Address{}, admin) {
		return nil, ErrOutOfGas
	}
	if !emitCompositeUpdated(ctx, id, admin, kids) {
		return nil, ErrOutOfGas
	}
	return wU64(id).Bytes(), nil
}

// updateComposite replaces a composite's child set in full. There is no partial
// update and no way to empty the list, since the count bound forbids it.
func updateComposite(ctx *PrecompileContext, reg policyReg, args []byte) error {
	id, err := readU64(args, 0)
	if err != nil {
		return err
	}
	if !reg.policyExists(id) {
		return revCAS20("PolicyNotFound()", errSelPolicyNotFound)
	}
	if !polIsComposite(id) {
		return revCAS20("IncompatiblePolicyType()", errSelIncompatibleType)
	}
	if admin := reg.admin(id); admin == (common.Address{}) || admin != ctx.Caller {
		return revCAS20("Unauthorized()", errSelUnauthorized)
	}
	rawKids, err := readWordArray(args, 1)
	if err != nil {
		return err
	}
	kids, err := validateChildren(reg, rawKids)
	if err != nil {
		return err
	}
	reg.setChildren(id, kids)
	if !emitCompositeUpdated(ctx, id, ctx.Caller, kids) {
		return ErrOutOfGas
	}
	return nil
}

func createPolicy(ctx *PrecompileContext, reg policyReg, args []byte, withAccounts bool) ([]byte, error) {
	admin, err := readAddress(args, 0)
	if err != nil {
		return nil, err
	}
	ptypeWord, err := readWord(args, 1)
	if err != nil {
		return nil, err
	}
	ptype := ptypeWord[31]
	// The enum widened to four values at Cobalt, so 2 and 3 now decode. They are
	// refused by the logic instead, after the zero-admin check and before the batch
	// bound — a composite is minted only through createCompositePolicy.
	if !isEnumWord(ptypeWord, cas20PolicyIntersect) {
		return nil, ErrExecutionReverted
	}
	if admin == (common.Address{}) {
		return nil, revCAS20("ZeroAddress()", errSelZeroAddress)
	}
	if polIsComposite(uint64(ptype) << 56) {
		return nil, revCAS20("IncompatiblePolicyType()", errSelIncompatibleType)
	}

	// The batch is decoded and bounded before any state is written. An enclosing
	// revert would discard premature writes anyway, but it would not give back the
	// gas they were metered at.
	var accounts []common.Hash
	if withAccounts {
		if accounts, err = readWordArray(args, 2); err != nil {
			return nil, err
		}
		if len(accounts) > cas20PolicyBatchMax {
			return nil, revCAS20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(cas20PolicyBatchMax))
		}
	}

	c := reg.ensureInitialized()
	// The counter shares its 56 bits across both types, so exhausting it must be
	// refused rather than allowed to carry into the type byte.
	if c >= cas20PolicyCounterMax {
		return nil, revPanic(0x11)
	}
	id := uint64(ptype)<<56 | c
	reg.setCounter(c + 1)
	reg.setPolicyAdmin(id, admin)
	if !ctx.AddLog([]common.Hash{cas20TopicPolicyCreated, idKey(id), addrKey(ctx.Caller)}, wU8(ptype).Bytes()) {
		return nil, ErrOutOfGas
	}
	// The initial admin is reported as a transition from nobody, so it lands in
	// the same event stream as every later handover.
	if !emitPolicyAdminUpdated(ctx, id, common.Address{}, admin) {
		return nil, ErrOutOfGas
	}

	if withAccounts {
		for _, a := range accounts {
			// Same strictness as a single address argument: readAddress refuses a
			// dirty word, and an element of an array must not be laxer.
			addr, ok := addressFromWord(a)
			if !ok {
				return nil, ErrExecutionReverted
			}
			reg.setMember(id, addr, true)
		}
		// Emitted even for an empty batch: the call form is part of the record.
		if !emitMembersUpdated(ctx, ptype, id, ctx.Caller, true, accounts) {
			return nil, ErrOutOfGas
		}
	}
	return encU256(uint256.NewInt(id)), nil
}

func updateMembers(ctx *PrecompileContext, reg policyReg, args []byte, wantType byte) error {
	pid, err := readU64(args, 0)
	if err != nil {
		return err
	}
	inWord, err := readWord(args, 1)
	if err != nil {
		return err
	}
	if !isEnumWord(inWord, 1) { // strict ABI bool
		return ErrExecutionReverted
	}
	accounts, err := readWordArray(args, 2)
	if err != nil {
		return err
	}
	// Order matters: it is observable through which error the caller receives —
	// existence, then type, then admin, then batch.
	if err := requirePolicyExists(reg, pid); err != nil {
		return err
	}
	if polIDType(pid) != wantType {
		return revCAS20("IncompatiblePolicyType()", errSelIncompatibleType)
	}
	if err := requirePolicyAdmin(reg, pid, ctx.Caller); err != nil {
		return err
	}
	if len(accounts) > cas20PolicyBatchMax {
		return revCAS20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(cas20PolicyBatchMax))
	}
	// Both strictly decoded, as Solidity's external decoder does. bool accepts
	// only 0 or 1, and an address element must not drop its high padding — this
	// path is the routine one, so a dirty word would add or remove a different
	// account than the encoding names.
	if !isEnumWord(inWord, 1) {
		return ErrExecutionReverted
	}
	in := inWord[31] == 1
	for _, a := range accounts {
		addr, ok := addressFromWord(a)
		if !ok {
			return ErrExecutionReverted
		}
		reg.setMember(pid, addr, in)
	}
	if !emitMembersUpdated(ctx, wantType, pid, ctx.Caller, in, accounts) {
		return ErrOutOfGas
	}
	return nil
}

func stageUpdateAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
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
	if !ctx.AddLog([]common.Hash{
		cas20TopicPolicyAdminStaged, idKey(id), addrKey(ctx.Caller), addrKey(newAdmin),
	}, nil) {
		return ErrOutOfGas
	}
	return nil
}

func finalizeUpdateAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
	pid, err := readU64(args, 0)
	if err != nil {
		return err
	}
	if err := requirePolicyExists(reg, pid); err != nil {
		return err
	}
	pending := reg.pending(pid)
	if pending == (common.Address{}) {
		return revCAS20("NoPendingAdmin()", errSelNoPendingAdmin)
	}
	if pending != ctx.Caller || ctx.Caller == (common.Address{}) {
		return revCAS20("Unauthorized()", errSelUnauthorized)
	}
	previous := reg.admin(pid)
	reg.setPolicyAdmin(pid, ctx.Caller)
	reg.setPending(pid, common.Address{})
	if !emitPolicyAdminUpdated(ctx, pid, previous, ctx.Caller) {
		return ErrOutOfGas
	}
	return nil
}

func renounceAdmin(ctx *PrecompileContext, reg policyReg, args []byte) error {
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
	if !emitPolicyAdminUpdated(ctx, pid, ctx.Caller, common.Address{}) {
		return ErrOutOfGas
	}
	return nil
}

// requirePolicyExists reverts PolicyNotFound unless the policy exists. It asks
// policyExists rather than the raw bit, because the sentinels are seeded lazily
// and always exist whether or not their word has been written; it is their zero
// admin that keeps them un-administrable.
func requirePolicyExists(reg policyReg, id uint64) error {
	if !reg.policyExists(id) {
		return revCAS20("PolicyNotFound()", errSelPolicyNotFound)
	}
	return nil
}

// requirePolicyAdmin reverts unless the policy exists and caller is its admin.
func requirePolicyAdmin(reg policyReg, id uint64, caller common.Address) error {
	if err := requirePolicyExists(reg, id); err != nil {
		return err
	}
	if admin := reg.admin(id); admin == (common.Address{}) || admin != caller {
		return revCAS20("Unauthorized()", errSelUnauthorized)
	}
	return nil
}
