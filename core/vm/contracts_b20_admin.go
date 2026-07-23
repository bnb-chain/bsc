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

// B20 RBAC roles, granular pausing, and mint/burn. Mirrors the shared IB20
// RoleManaged/Pausable/Mintable/Burnable traits. Compliance policies
// (MINT_RECEIVER etc.), memos and burnBlocked are layered on later.

// Built-in role ids. DEFAULT_ADMIN is bytes32(0); the rest are keccak of their
// canonical names. An unset role admin (zero) therefore means DEFAULT_ADMIN.
var (
	roleDefaultAdmin = common.Hash{}
	roleMint         = crypto.Keccak256Hash([]byte("MINT_ROLE"))
	roleBurn         = crypto.Keccak256Hash([]byte("BURN_ROLE"))
	roleBurnBlocked  = crypto.Keccak256Hash([]byte("BURN_BLOCKED_ROLE"))
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
	selBurnBlockedRole  = selector("BURN_BLOCKED_ROLE()")
	selPauseRole        = selector("PAUSE_ROLE()")
	selUnpauseRole      = selector("UNPAUSE_ROLE()")
	selMetadataRole     = selector("METADATA_ROLE()")

	selIsPaused        = selector("isPaused(uint8)")
	selPause           = selector("pause(uint8[])")
	selUnpause         = selector("unpause(uint8[])")
	selMint            = selector("mint(address,uint256)")
	selBurn            = selector("burn(uint256)")
	selUpdateSupplyCap = selector("updateSupplyCap(uint256)")

	b20TopicRoleGranted      = crypto.Keccak256Hash([]byte("RoleGranted(bytes32,address,address)"))
	b20TopicRoleRevoked      = crypto.Keccak256Hash([]byte("RoleRevoked(bytes32,address,address)"))
	b20TopicRoleAdminChanged = crypto.Keccak256Hash([]byte("RoleAdminChanged(bytes32,bytes32,bytes32)"))
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
	case selBurnBlockedRole:
		return roleBurnBlocked.Bytes(), nil, true
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

	// configurable
	case selUpdateSupplyCap:
		cap, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateSupplyCap(cap), true
	}
	return nil, nil, false
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
		return ErrWriteProtection
	}
	if !t.privileged && !t.roleMutable() {
		return ErrExecutionReverted
	}
	if !t.ensureAdminOf(role) {
		return ErrExecutionReverted
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
		return ErrWriteProtection
	}
	if !t.privileged && !t.roleMutable() {
		return ErrExecutionReverted
	}
	if !t.ensureAdminOf(role) {
		return ErrExecutionReverted
	}
	// The last DEFAULT_ADMIN cannot be removed via revoke; use renounceLastAdmin.
	if role == roleDefaultAdmin && t.s.hasRole(role, account) && t.s.adminCount().Eq(uint256.NewInt(1)) {
		return ErrExecutionReverted
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
		return ErrExecutionReverted
	}
	// The sole DEFAULT_ADMIN must use renounceLastAdmin, not this path.
	if role == roleDefaultAdmin && t.s.hasRole(role, t.ctx.Caller) && t.s.adminCount().Eq(uint256.NewInt(1)) {
		return ErrExecutionReverted
	}
	t.removeRole(role, t.ctx.Caller)
	return nil
}

func (t b20Token) renounceLastAdmin() error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if !t.s.hasRole(roleDefaultAdmin, t.ctx.Caller) || !t.s.adminCount().Eq(uint256.NewInt(1)) {
		return ErrExecutionReverted
	}
	t.s.setRole(roleDefaultAdmin, t.ctx.Caller, false)
	t.s.setAdminCount(new(uint256.Int))
	t.ctx.AddLog([]common.Hash{b20TopicRoleRevoked, roleDefaultAdmin, addrKey(t.ctx.Caller), addrKey(t.ctx.Caller)}, nil)
	// TODO: LastAdminRenounced(address) — verify indexing against base-std.
	return nil
}

func (t b20Token) setRoleAdmin(role, newAdminRole common.Hash) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if !t.privileged && !t.roleMutable() {
		return ErrExecutionReverted
	}
	if !t.ensureAdminOf(role) {
		return ErrExecutionReverted
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
	return ErrExecutionReverted
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
		return ErrExecutionReverted // EmptyFeatureSet
	}
	p := t.s.paused()
	for _, f := range features {
		if uint(f) > b20PauseBurn {
			return ErrExecutionReverted
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
		return ErrWriteProtection
	}
	if t.isPaused(b20PauseMint) {
		return ErrExecutionReverted
	}
	if err := t.ensureRole(roleMint); err != nil {
		return err
	}
	return t.mintCore(to, amount)
}

// mintCore performs the mint accounting (supply cap + credit + Transfer) after
// the caller has checked pause and role. Used by mint and batchMint.
func (t b20Token) mintCore(to common.Address, amount *uint256.Int) error {
	// TODO: MINT_RECEIVER policy (enforced even when privileged) — PolicyRegistry.
	newSupply := new(uint256.Int).Add(t.s.totalSupply(), amount)
	if newSupply.Lt(t.s.totalSupply()) || newSupply.Gt(t.s.supplyCap()) {
		return ErrExecutionReverted // overflow or SupplyCapExceeded
	}
	t.s.setBalance(to, new(uint256.Int).Add(t.s.balanceOf(to), amount))
	t.s.setTotalSupply(newSupply)
	t.emit(b20TopicTransfer, common.Address{}, to, amount)
	return nil
}

func (t b20Token) burn(from common.Address, amount *uint256.Int) error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if t.isPaused(b20PauseBurn) {
		return ErrExecutionReverted
	}
	if err := t.ensureRole(roleBurn); err != nil {
		return err
	}
	bal := t.s.balanceOf(from)
	if bal.Lt(amount) {
		return ErrExecutionReverted
	}
	t.s.setBalance(from, new(uint256.Int).Sub(bal, amount))
	t.s.setTotalSupply(new(uint256.Int).Sub(t.s.totalSupply(), amount))
	t.emit(b20TopicTransfer, from, common.Address{}, amount)
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
	if newCap.Lt(t.s.totalSupply()) {
		return ErrExecutionReverted // cannot drop below current supply
	}
	t.s.setSupplyCap(newCap)
	// TODO: SupplyCapUpdated event.
	return nil
}

// --- ABI: dynamic uint8[] ---------------------------------------------------

func readUint8Array(args []byte) ([]uint8, error) {
	L := uint64(len(args))
	if L < 32 {
		return nil, ErrExecutionReverted
	}
	off := new(uint256.Int).SetBytes(args[0:32]).Uint64()
	if off > L-32 {
		return nil, ErrExecutionReverted
	}
	n := new(uint256.Int).SetBytes(args[off : off+32]).Uint64()
	dataPos := off + 32
	if n > (L-dataPos)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([]uint8, n)
	for i := uint64(0); i < n; i++ {
		out[i] = args[dataPos+i*32+31]
	}
	return out, nil
}
