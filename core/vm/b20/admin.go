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

package b20

import (
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// B20 RBAC roles, granular pausing, and mint/burn. Mirrors the shared IB20
// RoleManaged/Pausable/Mintable/Burnable traits. Compliance policies
// (MINT_RECEIVER etc.) and memos are layered on later.

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
)

// dispatchAdmin handles the RBAC / pause / mint-burn selectors. ok is false
// when sel is none of them, so the caller can continue matching.
func (t b20Token) dispatchAdmin(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	// role constants
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

	// role views
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

	// role mutations
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

	// pause
	case selIsPaused:
		f, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encBool(t.isPaused(uint(f.Uint64()))), nil, true
	case selPause:
		return nil, t.setPause(args, true), true
	case selUnpause:
		return nil, t.setPause(args, false), true

	// mint / burn
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

	// configurable
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

// policyIdByScope reads the policy bound to one of the six scopes.
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

// roleMutable reports whether admin mutations are still possible. Once the last
// DEFAULT_ADMIN is renounced (adminCount == 0) the token is permanently
// ungovernable.
//
// TODO: an ownerless token created with initialAdmin == 0 also starts at
// adminCount == 0 yet must accept role grants during the factory's privileged
// bootstrap window; reconcile with a dedicated "renounced" marker when the
// factory lands (verify against base-std roles.rs).
func (t b20Token) roleMutable() bool { return !t.s.adminCount().IsZero() }

// ensureAdminOf checks the caller holds the admin role governing role (skipped
// on the privileged bootstrap path).
func (t b20Token) ensureAdminOf(role common.Hash) bool {
	if t.privileged {
		return true
	}
	return t.s.hasRole(t.s.roleAdmin(role), t.ctx.Caller)
}

func (t b20Token) grantRole(role common.Hash, account common.Address) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
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
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
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
		return vm.ErrWriteProtection
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
		return vm.ErrWriteProtection
	}
	if !t.s.hasRole(roleDefaultAdmin, t.ctx.Caller) || !t.s.adminCount().Eq(uint256.NewInt(1)) {
		return revB20("NotSoleAdmin()", errSelNotSoleAdmin)
	}
	t.s.setRole(roleDefaultAdmin, t.ctx.Caller, false)
	t.s.setAdminCount(new(uint256.Int))
	t.ctx.AddLog([]common.Hash{b20TopicRoleRevoked, roleDefaultAdmin, addrKey(t.ctx.Caller), addrKey(t.ctx.Caller)}, nil)
	// TODO: LastAdminRenounced(address) — verify indexing against base-std.
	return nil
}

func (t b20Token) setRoleAdmin(role, newAdminRole common.Hash) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
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

// ensureRoleMutable applies the shared role-mutation gates: mutations are
// impossible once the last admin is gone, and otherwise require the caller to
// hold role's admin role.
func (t b20Token) ensureRoleMutable(role common.Hash) error {
	if !t.privileged && !t.roleMutable() {
		return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
			addrKey(t.ctx.Caller), t.s.roleAdmin(role))
	}
	if !t.ensureAdminOf(role) {
		return revB20("AccessControlUnauthorizedAccount(address,bytes32)", errSelACUnauthorized,
			addrKey(t.ctx.Caller), t.s.roleAdmin(role))
	}
	return nil
}

// --- Pausable ---------------------------------------------------------------

func (t b20Token) setPause(args []byte, on bool) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
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
	for _, f := range features {
		if uint(f) > b20PauseSeize {
			return revPanic(0x21) // invalid enum value
		}
		mask := new(uint256.Int).Lsh(uint256.NewInt(1), uint(f))
		if on {
			p.Or(p, mask)
		} else {
			p.And(p, mask.Not(mask))
		}
	}
	t.s.setPaused(p)
	// TODO: Paused/Unpaused events — verify signatures against base-std.
	return nil
}

// --- Mintable / Burnable ----------------------------------------------------

func (t b20Token) mint(to common.Address, amount *uint256.Int) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
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
	// MINT_RECEIVER compliance is enforced even during privileged bootstrap.
	if !t.policyAllows(t.s.mintReceiverPolicy(), to) {
		return revB20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
			scopeMintReceiver, wU64(t.s.mintReceiverPolicy()))
	}
	newSupply := new(uint256.Int).Add(t.s.totalSupply(), amount)
	if newSupply.Lt(t.s.totalSupply()) {
		return revPanic(0x11) // supply overflow
	}
	if newSupply.Gt(t.s.supplyCap()) {
		return revB20("SupplyCapExceeded(uint256,uint256)", errSelSupplyCapExceeded,
			wU256(t.s.supplyCap()), wU256(newSupply))
	}
	t.s.setBalance(to, new(uint256.Int).Add(t.s.balanceOf(to), amount))
	t.s.setTotalSupply(newSupply)
	t.emit(b20TopicTransfer, common.Address{}, to, amount)
	return nil
}

func (t b20Token) burn(from common.Address, amount *uint256.Int) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
	if t.isPaused(b20PauseBurn) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseBurn))
	}
	if err := t.ensureRole(roleBurn); err != nil {
		return err
	}
	bal := t.s.balanceOf(from)
	if bal.Lt(amount) {
		return revB20("InsufficientBalance(address,uint256,uint256)", errSelInsufficientBalance,
			addrKey(from), wU256(bal), wU256(amount))
	}
	t.s.setBalance(from, new(uint256.Int).Sub(bal, amount))
	t.s.setTotalSupply(new(uint256.Int).Sub(t.s.totalSupply(), amount))
	t.emit(b20TopicTransfer, from, common.Address{}, amount)
	return nil
}

// seizeWithMemo reassigns a frozen account's balance to to. It moves value
// rather than destroying it, so totalSupply is unchanged, and it skips the
// allowance and the transfer policies: the seize scopes gate it instead.
//
// SEIZE_HOLDER is checked inverted — from is seizable only while the policy
// does not authorize it — which is what makes freezing structurally prior to
// seizure. An unset scope reads as ALWAYS_ALLOW, so on an unconfigured token no
// account is seizable at all.
func (t b20Token) seizeWithMemo(from, to common.Address, amount *uint256.Int, memo common.Hash) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
	if t.isPaused(b20PauseSeize) {
		return revB20("ContractPaused(uint8)", errSelContractPaused, wU8(b20PauseSeize))
	}
	if err := t.ensureRole(roleSeize); err != nil {
		return err
	}
	if to == (common.Address{}) {
		return revB20("InvalidReceiver(address)", errSelInvalidReceiver, addrKey(to))
	}
	if t.policyAllows(t.s.seizeHolderPolicy(), from) {
		return revB20("AccountNotSeizable(address)", errSelAccountNotSeizable, addrKey(from))
	}
	if !t.policyAllows(t.s.seizeReceiverPolicy(), to) {
		return revB20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
			scopeSeizeReceiver, wU64(t.s.seizeReceiverPolicy()))
	}
	bal := t.s.balanceOf(from)
	if bal.Lt(amount) {
		return revB20("InsufficientBalance(address,uint256,uint256)", errSelInsufficientBalance,
			addrKey(from), wU256(bal), wU256(amount))
	}
	t.s.setBalance(from, new(uint256.Int).Sub(bal, amount))
	t.s.setBalance(to, new(uint256.Int).Add(t.s.balanceOf(to), amount))
	t.emit(b20TopicTransfer, from, to, amount)
	t.emitMemo(memo)
	ab := amount.Bytes32()
	t.ctx.AddLog([]common.Hash{b20TopicSeized, addrKey(t.ctx.Caller), addrKey(from), addrKey(to)}, ab[:])
	return nil
}

// --- Configurable (subset) --------------------------------------------------

func (t b20Token) updateSupplyCap(newCap *uint256.Int) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
	if err := t.ensureRole(roleDefaultAdmin); err != nil {
		return err
	}
	if newCap.Lt(t.s.totalSupply()) || newCap.Gt(b20NoSupplyCap) {
		return revB20("InvalidSupplyCap(uint256,uint256)", errSelInvalidSupplyCap,
			wU256(t.s.totalSupply()), wU256(newCap))
	}
	t.s.setSupplyCap(newCap)
	// TODO: SupplyCapUpdated event.
	return nil
}

// updatePolicy binds a policy id to one of the token's four compliance scopes.
// The id must reference an existing registry policy (or a sentinel); binding a
// never-created id is rejected so the read path's empty-set tolerance cannot be
// exploited.
func (t b20Token) updatePolicy(scope common.Hash, id uint64) error {
	if t.ctx.ReadOnly {
		return vm.ErrWriteProtection
	}
	if err := t.ensureRole(roleDefaultAdmin); err != nil {
		return err
	}
	if id != b20PolicyAlwaysAllow && id != b20PolicyAlwaysBlock && !newPolicyReg(t.ctx).exists(id) {
		return revB20("PolicyNotFound()", errSelPolicyNotFound)
	}
	switch scope {
	case scopeTransferSender:
		t.s.setTransferSenderPolicy(id)
	case scopeTransferReceiver:
		t.s.setTransferReceiverPolicy(id)
	case scopeTransferExecutor:
		t.s.setTransferExecutorPolicy(id)
	case scopeMintReceiver:
		t.s.setMintReceiverPolicy(id)
	case scopeSeizeHolder:
		t.s.setSeizeHolderPolicy(id)
	case scopeSeizeReceiver:
		t.s.setSeizeReceiverPolicy(id)
	default:
		return revB20("UnsupportedPolicyType(bytes32)", errSelUnsupportedScope, scope)
	}
	return nil
}

// --- ABI: dynamic uint8[] ---------------------------------------------------

func readUint8Array(args []byte) ([]uint8, error) {
	L := uint64(len(args))
	if L < 32 {
		return nil, vm.ErrExecutionReverted
	}
	off := new(uint256.Int).SetBytes(args[0:32]).Uint64()
	if off > L-32 {
		return nil, vm.ErrExecutionReverted
	}
	n := new(uint256.Int).SetBytes(args[off : off+32]).Uint64()
	dataPos := off + 32
	if n > (L-dataPos)/32 {
		return nil, vm.ErrExecutionReverted
	}
	out := make([]uint8, n)
	for i := uint64(0); i < n; i++ {
		out[i] = args[dataPos+i*32+31]
	}
	return out, nil
}
