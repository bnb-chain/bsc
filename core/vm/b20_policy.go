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
//
// TODO: align the storage layout / ABI / events with base-std.

// B20PolicyRegistryAddress is the singleton registry precompile (BEP-702 §3.1).
var B20PolicyRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000001")

const b20PolicyNamespace = "bsc.policyregistry"

const (
	b20PolicyBlocklist = 0
	b20PolicyAllowlist = 1
	b20PolicyBatchMax  = 64
	b20PolicyFirstID   = 2 // ids 0/1's counter slots are reserved for the sentinels

	// Sentinel policy ids (never created; valid to bind).
	b20PolicyAlwaysAllow = 0                 // blocklist type, empty -> allow all
	b20PolicyAlwaysBlock = uint64(1)<<56 | 1 // allowlist type, empty -> block all
)

const (
	polSlotCounter = 0 // global counter (uint256)
	polSlotExists  = 1 // mapping(uint64 => bool)
	polSlotAdmins  = 2 // mapping(uint64 => address)
	polSlotPending = 3 // mapping(uint64 => address)
	polSlotMembers = 4 // mapping(uint64 => mapping(address => bool))
)

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
)

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
func (p policyReg) exists(id uint64) bool {
	return p.s.getWord(p.s.mapSlot(polSlot(polSlotExists), idKey(id))) != (common.Hash{})
}
func (p policyReg) setExists(id uint64) {
	var one common.Hash
	one[31] = 1
	p.s.setWord(p.s.mapSlot(polSlot(polSlotExists), idKey(id)), one)
}
func (p policyReg) admin(id uint64) common.Address {
	return common.BytesToAddress(p.s.getWord(p.s.mapSlot(polSlot(polSlotAdmins), idKey(id))).Bytes())
}
func (p policyReg) setAdmin(id uint64, a common.Address) {
	p.s.setWord(p.s.mapSlot(polSlot(polSlotAdmins), idKey(id)), addrKey(a))
}
func (p policyReg) pending(id uint64) common.Address {
	return common.BytesToAddress(p.s.getWord(p.s.mapSlot(polSlot(polSlotPending), idKey(id))).Bytes())
}
func (p policyReg) setPending(id uint64, a common.Address) {
	p.s.setWord(p.s.mapSlot(polSlot(polSlotPending), idKey(id)), addrKey(a))
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
// semantics (blocklist → allow all, allowlist → block all). ALWAYS_ALLOW (0)
// and ALWAYS_BLOCK (allowlist, empty) fall out of this naturally.
func (p policyReg) isAuthorized(id uint64, account common.Address) bool {
	typeByte := byte(id >> 56)
	if typeByte > b20PolicyAllowlist {
		return false
	}
	member := p.member(id, account)
	if typeByte == b20PolicyAllowlist {
		return member
	}
	return !member
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
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return finishB20(ret, err)
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
		return encBool(reg.exists(id)), nil
	case selPolicyAdmin:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return addrKey(reg.admin(id)).Bytes(), nil
	case selPendingPolicyAdmin:
		id, err := readU64(args, 0)
		if err != nil {
			return nil, err
		}
		return addrKey(reg.pending(id)).Bytes(), nil

	}

	// Writes. Recognize the selector first, so an unknown one still reverts as
	// an unknown selector rather than as an inactive feature, then apply the
	// activation gate: every non-view method sits behind it, so a deactivation
	// freezes membership and admin changes on policies that already exist
	// (BEP-702 section 3.15).
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
	c := reg.counter()
	if c < b20PolicyFirstID {
		c = b20PolicyFirstID
	}
	id := uint64(ptype)<<56 | c
	reg.setCounter(c + 1)
	reg.setExists(id)
	reg.setAdmin(id, admin)

	if withAccounts {
		accounts, err := readWordArray(args, 2)
		if err != nil {
			return nil, err
		}
		if len(accounts) > b20PolicyBatchMax {
			return nil, revB20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(b20PolicyBatchMax))
		}
		for _, a := range accounts {
			reg.setMember(id, common.BytesToAddress(a.Bytes()), true)
		}
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
	if err := requirePolicyAdmin(reg, pid, ctx.Caller); err != nil {
		return err
	}
	if byte(pid>>56) != wantType {
		return revB20("IncompatiblePolicyType()", errSelIncompatibleType)
	}
	if len(accounts) > b20PolicyBatchMax {
		return revB20("BatchSizeTooLarge(uint256)", errSelBatchTooLarge, wU64(b20PolicyBatchMax))
	}
	in := inWord[31] != 0
	for _, a := range accounts {
		reg.setMember(pid, common.BytesToAddress(a.Bytes()), in)
	}
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
	if reg.pending(pid) == (common.Address{}) {
		return revB20("NoPendingAdmin()", errSelNoPendingAdmin)
	}
	if reg.pending(pid) != ctx.Caller || ctx.Caller == (common.Address{}) {
		return revB20("Unauthorized()", errSelUnauthorized)
	}
	reg.setAdmin(pid, ctx.Caller)
	reg.setPending(pid, common.Address{})
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
	reg.setAdmin(pid, common.Address{}) // frozen; policy still exists
	reg.setPending(pid, common.Address{})
	return nil
}

// requirePolicyAdmin reverts unless caller is the (non-zero) admin of the policy.
func requirePolicyAdmin(reg policyReg, id uint64, caller common.Address) error {
	admin := reg.admin(id)
	if admin == (common.Address{}) || admin != caller {
		return revB20("Unauthorized()", errSelUnauthorized)
	}
	return nil
}
