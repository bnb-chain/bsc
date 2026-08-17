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

// Built-in role ids. DEFAULT_ADMIN is bytes32(0); the rest are keccak of their
// canonical names. An unset role admin (zero) therefore means DEFAULT_ADMIN.
var (
	roleDefaultAdmin = common.Hash{}
	roleMint         = crypto.Keccak256Hash([]byte("MINT_ROLE"))
	roleBurn         = crypto.Keccak256Hash([]byte("BURN_ROLE"))
	roleSeize        = crypto.Keccak256Hash([]byte("SEIZE_ROLE"))
	rolePause        = crypto.Keccak256Hash([]byte("PAUSE_ROLE"))
	roleUnpause      = crypto.Keccak256Hash([]byte("UNPAUSE_ROLE"))
	roleMetadata     = crypto.Keccak256Hash([]byte("METADATA_ROLE"))
)

var (
	selHasRole           = selector("hasRole(bytes32,address)")
	selGetRoleAdmin      = selector("getRoleAdmin(bytes32)")
	selGrantRole         = selector("grantRole(bytes32,address)")
	selRevokeRole        = selector("revokeRole(bytes32,address)")
	selRenounceRole      = selector("renounceRole(bytes32,address)")
	selSetRoleAdmin      = selector("setRoleAdmin(bytes32,bytes32)")
	selRenounceLastAdmin = selector("renounceLastAdmin()")

	selDefaultAdminRole = selector("DEFAULT_ADMIN_ROLE()")
	selMintRole         = selector("MINT_ROLE()")
	selBurnRole         = selector("BURN_ROLE()")
	selSeizeRole        = selector("SEIZE_ROLE()")
	selPauseRole        = selector("PAUSE_ROLE()")
	selUnpauseRole      = selector("UNPAUSE_ROLE()")
	selMetadataRole     = selector("METADATA_ROLE()")

	// Policy scope identifiers: keccak256 of the canonical scope names
	// (BEP-702 section 3.8).
	scopeTransferSender   = crypto.Keccak256Hash([]byte("TRANSFER_SENDER_POLICY"))
	scopeTransferReceiver = crypto.Keccak256Hash([]byte("TRANSFER_RECEIVER_POLICY"))
	scopeTransferExecutor = crypto.Keccak256Hash([]byte("TRANSFER_EXECUTOR_POLICY"))
	scopeMintReceiver     = crypto.Keccak256Hash([]byte("MINT_RECEIVER_POLICY"))
	scopeSeizeHolder      = crypto.Keccak256Hash([]byte("SEIZE_HOLDER_POLICY"))
	scopeSeizeReceiver    = crypto.Keccak256Hash([]byte("SEIZE_RECEIVER_POLICY"))

	selTransferSenderScope   = selector("TRANSFER_SENDER_POLICY()")
	selTransferReceiverScope = selector("TRANSFER_RECEIVER_POLICY()")
	selTransferExecutorScope = selector("TRANSFER_EXECUTOR_POLICY()")
	selMintReceiverScope     = selector("MINT_RECEIVER_POLICY()")
	selSeizeHolderScope      = selector("SEIZE_HOLDER_POLICY()")
	selSeizeReceiverScope    = selector("SEIZE_RECEIVER_POLICY()")
	selPolicyId              = selector("policyId(bytes32)")

	selIsPaused        = selector("isPaused(uint8)")
	selPause           = selector("pause(uint8[])")
	selUnpause         = selector("unpause(uint8[])")
	selMint            = selector("mint(address,uint256)")
	selBurn            = selector("burn(uint256)")
	selSeizeWithMemo   = selector("seizeWithMemo(address,address,uint256,bytes32)")
	selUpdateSupplyCap = selector("updateSupplyCap(uint256)")
	selUpdatePolicy    = selector("updatePolicy(bytes32,uint64)")

	b20TopicRoleGranted      = eventTopic("RoleGranted(bytes32,address,address)")
	b20TopicRoleRevoked      = eventTopic("RoleRevoked(bytes32,address,address)")
	b20TopicRoleAdminChanged = eventTopic("RoleAdminChanged(bytes32,bytes32,bytes32)")
	b20TopicSeized           = eventTopic("Seized(address,address,address,uint256)")

	b20TopicLastAdminRenounced = eventTopic("LastAdminRenounced(address)")
	b20TopicPolicyUpdated      = eventTopic("PolicyUpdated(bytes32,uint64,uint64)")
	b20TopicPaused             = eventTopic("Paused(address,uint8[])")
	b20TopicUnpaused           = eventTopic("Unpaused(address,uint8[])")
	b20TopicSupplyCapUpdated   = eventTopic("SupplyCapUpdated(address,uint256,uint256)")
)

// dispatchAdmin handles the RBAC / pause / mint-burn selectors. ok is false
// when sel is none of them, so the caller can continue matching.
func (t b20Token) dispatchAdmin(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	case selDefaultAdminRole:
		return roleDefaultAdmin.Bytes(), nil, true
	case selMintRole:
		return roleMint.Bytes(), nil, true
	case selBurnRole:
		return roleBurn.Bytes(), nil, true
	case selSeizeRole:
		return roleSeize.Bytes(), nil, true
	case selPauseRole:
		return rolePause.Bytes(), nil, true
	case selUnpauseRole:
		return roleUnpause.Bytes(), nil, true
	case selMetadataRole:
		return roleMetadata.Bytes(), nil, true

	case selHasRole:
		role, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		acct, err := readAddress(args, 1)
		if err != nil {
			return nil, err, true
		}
		return encBool(t.s.hasRole(role, acct)), nil, true
	case selGetRoleAdmin:
		role, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		admin := t.s.roleAdmin(role)
		return admin.Bytes(), nil, true

	case selGrantRole:
		role, acct, err := readRoleAccount(args)
		if err != nil {
			return nil, err, true
		}
		return nil, t.grantRole(role, acct), true
	case selRevokeRole:
		role, acct, err := readRoleAccount(args)
		if err != nil {
			return nil, err, true
		}
		return nil, t.revokeRole(role, acct), true
	case selRenounceRole:
		role, confirm, err := readRoleAccount(args)
		if err != nil {
			return nil, err, true
		}
		return nil, t.renounceRole(role, confirm), true
	case selSetRoleAdmin:
		role, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		newAdmin, err := readWord(args, 1)
		if err != nil {
			return nil, err, true
		}
		return nil, t.setRoleAdmin(role, newAdmin), true
	case selRenounceLastAdmin:
		return nil, t.renounceLastAdmin(), true

	case selIsPaused:
		// Decoded exactly as pause()/unpause() decode their elements: a word with
		// dirty high bytes is a malformed encoding, and a well-formed value
		// outside the enum is Panic(0x21).
		w, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		if !isEnumWord(w, 0xff) {
			return nil, ErrExecutionReverted, true
		}
		if uint(w[31]) > b20PauseSeize {
			return nil, revPanic(0x21), true
		}
		return encBool(t.isPaused(uint(w[31]))), nil, true
	case selPause:
		return nil, t.setPause(args, true), true
	case selUnpause:
		return nil, t.setPause(args, false), true

	case selMint:
		to, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err, true
		}
		return nil, t.mint(to, amount), true
	case selBurn:
		amount, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.burn(t.ctx.Caller, amount), true
	case selSeizeWithMemo:
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
		if err := t.seizeWithMemo(from, to, amount, memo); err != nil {
			return nil, err, true
		}
		return encBool(true), nil, true

	case selUpdateSupplyCap:
		cap, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateSupplyCap(cap), true
	case selUpdatePolicy:
		scope, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		id, err := readU64(args, 1)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updatePolicy(scope, id), true
	case selTransferSenderScope:
		return scopeTransferSender.Bytes(), nil, true
	case selTransferReceiverScope:
		return scopeTransferReceiver.Bytes(), nil, true
	case selTransferExecutorScope:
		return scopeTransferExecutor.Bytes(), nil, true
	case selMintReceiverScope:
		return scopeMintReceiver.Bytes(), nil, true
	case selSeizeHolderScope:
		return scopeSeizeHolder.Bytes(), nil, true
	case selSeizeReceiverScope:
		return scopeSeizeReceiver.Bytes(), nil, true
	case selPolicyId:
		scope, err := readWord(args, 0)
		if err != nil {
			return nil, err, true
		}
		id, ok := t.policyIdByScope(scope)
		if !ok {
			return nil, revB20("UnsupportedPolicyType(bytes32)", errSelUnsupportedScope, scope), true
		}
		return encU256(uint256.NewInt(id)), nil, true
	}
	return nil, nil, false
}

func (t b20Token) policyIdByScope(scope common.Hash) (uint64, bool) {
	switch scope {
	case scopeTransferSender:
		return t.s.transferSenderPolicy(), true
	case scopeTransferReceiver:
		return t.s.transferReceiverPolicy(), true
	case scopeTransferExecutor:
		return t.s.transferExecutorPolicy(), true
	case scopeMintReceiver:
		return t.s.mintReceiverPolicy(), true
	case scopeSeizeHolder:
		return t.s.seizeHolderPolicy(), true
	case scopeSeizeReceiver:
		return t.s.seizeReceiverPolicy(), true
	}
	return 0, false
}

func readRoleAccount(args []byte) (common.Hash, common.Address, error) {
	role, err := readWord(args, 0)
	if err != nil {
		return common.Hash{}, common.Address{}, err
	}
	acct, err := readAddress(args, 1)
	if err != nil {
		return common.Hash{}, common.Address{}, err
	}
	return role, acct, nil
}

// --- RoleManaged ------------------------------------------------------------

// roleMutable reports whether role mutations are still possible: adminCount == 0
// freezes them, except inside the factory's privileged bootstrap.
func (t b20Token) grantRole(role common.Hash, account common.Address) error {
	if err := t.ensureRoleMutable(role); err != nil {
		return err
	}
	if !t.s.hasRole(role, account) {
		t.s.setRole(role, account, true)
		if role == roleDefaultAdmin {
			t.s.setAdminCount(new(uint256.Int).AddUint64(t.s.adminCount(), 1))
		}
		t.ctx.AddLog([]common.Hash{b20TopicRoleGranted, role, addrKey(account), addrKey(t.ctx.Caller)}, nil)
	}
	return nil
}

func (t b20Token) revokeRole(role common.Hash, account common.Address) error {
	if err := t.ensureRoleMutable(role); err != nil {
		return err
	}
	// The last DEFAULT_ADMIN cannot be removed via revoke; use renounceLastAdmin.
	if role == roleDefaultAdmin && t.s.hasRole(role, account) && t.s.adminCount().Eq(uint256.NewInt(1)) {
		return revB20("LastAdminCannotRenounce()", errSelLastAdminRenounce)
	}
	t.removeRole(role, account)
	return nil
}

func (t b20Token) renounceRole(role common.Hash, confirmation common.Address) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	// Confirmation must equal the caller (OZ 5.x anti-misuse guard).
	if confirmation != t.ctx.Caller {
		return revB20("AccessControlBadConfirmation()", errSelACBadConfirmation)
	}
	// The sole DEFAULT_ADMIN must use renounceLastAdmin, not this path.
	if role == roleDefaultAdmin && t.s.hasRole(role, t.ctx.Caller) && t.s.adminCount().Eq(uint256.NewInt(1)) {
		return revB20("LastAdminCannotRenounce()", errSelLastAdminRenounce)
	}
	t.removeRole(role, t.ctx.Caller)
	return nil
}

func (t b20Token) renounceLastAdmin() error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	// Two checks, not one condition: base-std reports a caller who holds no
	// admin role as unauthorized and reserves NotSoleAdmin for one who does hold
	// it but is not the last. Collapsing them told a stranger they were "not the
	// sole admin", which is true but says the wrong thing.
	//
	// hasRole directly rather than ensureRole, because this guard is not
	// skippable inside the privileged bootstrap: it is the anti-resurrection
	// check BEP-702 3.4 says is never skipped.
	if !t.s.hasRole(roleDefaultAdmin, t.ctx.Caller) {
		return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
			addrKey(t.ctx.Caller), roleDefaultAdmin)
	}
	if !t.s.adminCount().Eq(uint256.NewInt(1)) {
		return revB20("NotSoleAdmin()", errSelNotSoleAdmin)
	}
	t.s.setRole(roleDefaultAdmin, t.ctx.Caller, false)
	t.s.setAdminCount(new(uint256.Int))
	t.ctx.AddLog([]common.Hash{b20TopicRoleRevoked, roleDefaultAdmin, addrKey(t.ctx.Caller), addrKey(t.ctx.Caller)}, nil)
	// The dedicated event marks the transition an indexer cannot infer from
	// RoleRevoked alone: the token is now permanently ungovernable.
	t.ctx.AddLog([]common.Hash{b20TopicLastAdminRenounced, addrKey(t.ctx.Caller)}, nil)
	return nil
}

func (t b20Token) setRoleAdmin(role, newAdminRole common.Hash) error {
	if err := t.ensureRoleMutable(role); err != nil {
		return err
	}
	prev := t.s.roleAdmin(role)
	t.s.setRoleAdmin(role, newAdminRole)
	t.ctx.AddLog([]common.Hash{b20TopicRoleAdminChanged, role, prev, newAdminRole}, nil)
	return nil
}

// removeRole clears role from account (if held), maintaining adminCount, and
// emits RoleRevoked. Renounce silently succeeds when the role is not held.
func (t b20Token) removeRole(role common.Hash, account common.Address) {
	if !t.s.hasRole(role, account) {
		return
	}
	t.s.setRole(role, account, false)
	if role == roleDefaultAdmin {
		t.s.setAdminCount(new(uint256.Int).SubUint64(t.s.adminCount(), 1))
	}
	t.ctx.AddLog([]common.Hash{b20TopicRoleRevoked, role, addrKey(account), addrKey(t.ctx.Caller)}, nil)
}

// ensureRole reverts unless the caller holds role (skipped when privileged).
func (t b20Token) ensureRole(role common.Hash) error {
	if t.privileged || t.s.hasRole(role, t.ctx.Caller) {
		return nil
	}
	return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
		addrKey(t.ctx.Caller), role)
}

func (t b20Token) ensureRoleMutable(role common.Hash) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if !t.privileged && t.s.adminCount().IsZero() {
		return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
			addrKey(t.ctx.Caller), t.s.roleAdmin(role))
	}
	if !t.privileged && !t.s.hasRole(t.s.roleAdmin(role), t.ctx.Caller) {
		return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
			addrKey(t.ctx.Caller), t.s.roleAdmin(role))
	}
	return nil
}

// --- Pausable ---------------------------------------------------------------

func (t b20Token) setPause(args []byte, on bool) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	role := rolePause
	if !on {
		role = roleUnpause
	}
	if err := t.ensureRole(role); err != nil {
		return err
	}
	features, err := readUint8Array(args)
	if err != nil {
		return err
	}
	if len(features) == 0 {
		return revB20("EmptyFeatureSet()", errSelEmptyFeatureSet)
	}
	p := t.s.paused()
	words := make([]common.Hash, len(features))
	for i, f := range features {
		if uint(f) > b20PauseSeize {
			return revPanic(0x21) // invalid enum value
		}
		words[i] = wU8(f)
		mask := new(uint256.Int).Lsh(uint256.NewInt(1), uint(f))
		if on {
			p.Or(p, mask)
		} else {
			p.And(p, mask.Not(mask))
		}
	}
	t.s.setPaused(p)
	topic := b20TopicPaused
	if !on {
		topic = b20TopicUnpaused
	}
	// The event carries the requested feature list, not the resulting mask: it
	// records the action taken, so re-pausing an already-paused feature is
	// visible rather than indistinguishable from a no-op.
	t.ctx.AddLog([]common.Hash{topic, addrKey(t.ctx.Caller)}, encodeTuple(abiWordArray(words)))
	return nil
}

// --- Mintable / Burnable ----------------------------------------------------

func (t b20Token) mint(to common.Address, amount *uint256.Int) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if t.isPaused(b20PauseMint) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseMint))
	}
	if err := t.ensureRole(roleMint); err != nil {
		return err
	}
	return t.mintCore(to, amount)
}

// mintCore performs the mint accounting (supply cap + credit + Transfer) after
// the caller has checked pause and role. Used by mint and batchMint.
func (t b20Token) mintCore(to common.Address, amount *uint256.Int) error {
	if to == (common.Address{}) {
		return revB20("InvalidReceiver(address)", errSelInvalidReceiver, addrKey(to))
	}
	// MINT_RECEIVER compliance is enforced even during privileged bootstrap. Each
	// value below is read once and reused, the revert payloads included.
	mintReceiver := t.s.mintReceiverPolicy()
	if !t.policyAllows(mintReceiver, to) {
		return revB20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
			scopeMintReceiver, wU64(mintReceiver))
	}
	supply := t.s.totalSupply()
	newSupply := new(uint256.Int).Add(supply, amount)
	if newSupply.Lt(supply) {
		return revPanic(0x11) // supply overflow
	}
	if cap := t.s.supplyCap(); newSupply.Gt(cap) {
		return revB20("SupplyCapExceeded(uint256,uint256)", errSelSupplyCapExceeded,
			wU256(cap), wU256(newSupply))
	}
	toSlot := t.s.balanceSlot(to)
	t.s.setU256At(toSlot, new(uint256.Int).Add(t.s.getU256At(toSlot), amount))
	t.s.setTotalSupply(newSupply)
	t.emit(b20TopicTransfer, common.Address{}, to, amount)
	return nil
}

func (t b20Token) burn(from common.Address, amount *uint256.Int) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if t.isPaused(b20PauseBurn) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseBurn))
	}
	if err := t.ensureRole(roleBurn); err != nil {
		return err
	}
	fromSlot := t.s.balanceSlot(from)
	bal := t.s.getU256At(fromSlot)
	if bal.Lt(amount) {
		return revB20("InsufficientBalance(address,uint256,uint256)", errSelInsufficientBalance,
			addrKey(from), wU256(bal), wU256(amount))
	}
	t.s.setU256At(fromSlot, new(uint256.Int).Sub(bal, amount))
	t.s.setTotalSupply(new(uint256.Int).Sub(t.s.totalSupply(), amount))
	t.emit(b20TopicTransfer, from, common.Address{}, amount)
	return nil
}

// seizeWithMemo reassigns a frozen account's balance. SEIZE_HOLDER is inverted:
// only a disallowed holder is seizable, so an ALWAYS_ALLOW scope makes every
// account non-seizable.
func (t b20Token) seizeWithMemo(from, to common.Address, amount *uint256.Int, memo common.Hash) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if t.isPaused(b20PauseSeize) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseSeize))
	}
	if err := t.ensureRole(roleSeize); err != nil {
		return err
	}
	// A seizure is a reassignment, so it needs a real source and a different
	// destination: self-seizing would emit Seized over a no-op, and a zero source
	// would report InsufficientBalance for what is really a malformed argument.
	if to == (common.Address{}) || from == to {
		return revB20("InvalidReceiver(address)", errSelInvalidReceiver, addrKey(to))
	}
	if from == (common.Address{}) {
		return revB20("InvalidSender(address)", errSelInvalidSender, addrKey(from))
	}
	// Both seize ids share a slot, so they are read together.
	seizeHolder, seizeReceiver := t.s.seizePolicies()
	if t.policyAllows(seizeHolder, from) {
		return revB20("AccountNotSeizable(address)", errSelAccountNotSeizable, addrKey(from))
	}
	if !t.policyAllows(seizeReceiver, to) {
		return revB20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
			scopeSeizeReceiver, wU64(seizeReceiver))
	}
	fromSlot := t.s.balanceSlot(from)
	bal := t.s.getU256At(fromSlot)
	if bal.Lt(amount) {
		return revB20("InsufficientBalance(address,uint256,uint256)", errSelInsufficientBalance,
			addrKey(from), wU256(bal), wU256(amount))
	}
	t.s.setU256At(fromSlot, new(uint256.Int).Sub(bal, amount))
	toSlot := t.s.balanceSlot(to)
	t.s.setU256At(toSlot, new(uint256.Int).Add(t.s.getU256At(toSlot), amount))
	t.emit(b20TopicTransfer, from, to, amount)
	t.emitMemo(memo)
	ab := amount.Bytes32()
	t.ctx.AddLog([]common.Hash{b20TopicSeized, addrKey(t.ctx.Caller), addrKey(from), addrKey(to)}, ab[:])
	return nil
}

// --- Configurable (subset) --------------------------------------------------

func (t b20Token) updateSupplyCap(newCap *uint256.Int) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := t.ensureRole(roleDefaultAdmin); err != nil {
		return err
	}
	if supply := t.s.totalSupply(); newCap.Lt(supply) || newCap.Gt(b20NoSupplyCap) {
		return revB20("InvalidSupplyCap(uint256,uint256)", errSelInvalidSupplyCap,
			wU256(supply), wU256(newCap))
	}
	previous := t.s.supplyCap()
	t.s.setSupplyCap(newCap)
	t.ctx.AddLog([]common.Hash{b20TopicSupplyCapUpdated, addrKey(t.ctx.Caller)},
		append(wU256(previous).Bytes(), wU256(newCap).Bytes()...))
	return nil
}

// updatePolicy binds a policy id to one of the token's four compliance scopes.
// The id must reference an existing registry policy (or a sentinel); binding a
// never-created id is rejected so the read path's empty-set tolerance cannot be
// exploited.
func (t b20Token) updatePolicy(scope common.Hash, id uint64) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := t.ensureRole(roleDefaultAdmin); err != nil {
		return err
	}
	// The scope is validated before the id, matching base-std: an unrecognized
	// scope is reported as such whatever id accompanies it. Resolving it to its
	// accessors first is what puts that check ahead of the registry lookup.
	var read func() uint64
	var write func(uint64)
	switch scope {
	case scopeTransferSender:
		read, write = t.s.transferSenderPolicy, t.s.setTransferSenderPolicy
	case scopeTransferReceiver:
		read, write = t.s.transferReceiverPolicy, t.s.setTransferReceiverPolicy
	case scopeTransferExecutor:
		read, write = t.s.transferExecutorPolicy, t.s.setTransferExecutorPolicy
	case scopeMintReceiver:
		read, write = t.s.mintReceiverPolicy, t.s.setMintReceiverPolicy
	case scopeSeizeHolder:
		read, write = t.s.seizeHolderPolicy, t.s.setSeizeHolderPolicy
	case scopeSeizeReceiver:
		read, write = t.s.seizeReceiverPolicy, t.s.setSeizeReceiverPolicy
	default:
		return revB20("UnsupportedPolicyType(bytes32)", errSelUnsupportedScope, scope)
	}
	// policyExists answers for the sentinels itself, so binding one needs no
	// special case here (BEP-702 3.8).
	if !newPolicyReg(t.ctx).policyExists(id) {
		return revB20("PolicyNotFound(uint64)", errSelPolicyNotFoundID, wU64(id))
	}
	// The event carries the id being replaced, so the previous binding is read
	// before the write — an SLOAD a Solidity implementation emitting the same
	// event would also pay.
	previous := read()
	write(id)
	t.ctx.AddLog([]common.Hash{b20TopicPolicyUpdated, scope},
		append(wU64(previous).Bytes(), wU64(id).Bytes()...))
	return nil
}

// --- ABI: dynamic uint8[] ---------------------------------------------------

// readUint8Array decodes a dynamic uint8[] argument. Offsets, the length and
// every element are read strictly: a word with dirty high bits is a malformed
// encoding, not a value to be truncated into something plausible. Truncating an
// element would be the worst of the three — 0x0100 would silently become
// feature 0 and pause a feature the caller never named.
func readUint8Array(args []byte) ([]uint8, error) {
	L := uint64(len(args))
	off, ok := wordU64(args, 0)
	if !ok || off > L || L-off < 32 {
		return nil, ErrExecutionReverted
	}
	n, ok := wordU64(args, off)
	if !ok {
		return nil, ErrExecutionReverted
	}
	dataPos := off + 32
	if n > (L-dataPos)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([]uint8, n)
	for i := uint64(0); i < n; i++ {
		// Byte-addressed, not word-indexed: the head offset is caller-supplied
		// and need not be 32-aligned.
		v, ok := wordU64(args, dataPos+i*32)
		if !ok || v > 0xff {
			return nil, ErrExecutionReverted
		}
		out[i] = byte(v)
	}
	return out, nil
}
