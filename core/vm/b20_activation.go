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
)

// ActivationRegistry: the per-feature governance switch (BEP-702 3.15). It gates
// token creation and PolicyRegistry writes only — deactivation never reaches an
// existing token, so it cannot freeze balances.

const b20ActivationNamespace = "bsc.activation_registry"

const (
	actSlotFeatures = 0 // mapping(bytes32 feature => bool)
	actSlotAdmin    = 1 // address
)

var b20ActivationRoot = erc7201Root(b20ActivationNamespace)

// Feature identifiers: keccak256 of the canonical feature name.
var (
	featureB20Asset       = crypto.Keccak256Hash([]byte("bsc.b20_asset"))
	featureB20Stablecoin  = crypto.Keccak256Hash([]byte("bsc.b20_stablecoin"))
	featurePolicyRegistry = crypto.Keccak256Hash([]byte("bsc.policy_registry"))
)

var (
	selIsActivated    = selector("isActivated(bytes32)")
	selCheckActivated = selector("checkActivated(bytes32)")
	selActivationAdm  = selector("admin()")
	selSetAdmin       = selector("setAdmin(address)")
	selActivate       = selector("activate(bytes32)")
	selDeactivate     = selector("deactivate(bytes32)")

	b20TopicFeatureActivated   = eventTopic("FeatureActivated(bytes32,address)")
	b20TopicFeatureDeactivated = eventTopic("FeatureDeactivated(bytes32,address)")
	b20TopicAdminChanged       = eventTopic("AdminChanged(address,address,address)")
)

// activationReg is a gas-metered view over the registry's storage.
type activationReg struct{ s b20Storage }

func newActivationReg(ctx *PrecompileContext) activationReg {
	return activationReg{s: b20Storage{state: ctx.StateDB, token: B20ActivationRegistryAddress, ctx: ctx}}
}

func actSlot(offset uint64) common.Hash {
	return offsetSlot(b20ActivationRoot, offset)
}

func (r activationReg) isActivated(feature common.Hash) bool {
	return r.s.getWord(r.s.mapSlot(actSlot(actSlotFeatures), feature)) != (common.Hash{})
}

func (r activationReg) setActivated(feature common.Hash, on bool) {
	var v common.Hash
	if on {
		v[31] = 1
	}
	// Deactivating clears the slot rather than writing false, so the storage
	// refund applies exactly as it would for a Solidity `delete`.
	r.s.setWord(r.s.mapSlot(actSlot(actSlotFeatures), feature), v)
}

// admin returns the activation admin. The zero address means no admin is set
// and therefore that nothing can be activated on this network; it is never a
// valid caller.
func (r activationReg) admin() common.Address {
	return common.BytesToAddress(r.s.getWord(actSlot(actSlotAdmin)).Bytes())
}

func (r activationReg) setAdmin(a common.Address) {
	r.s.setWord(actSlot(actSlotAdmin), addrKey(a))
}

// requireAdmin reverts unless the caller is the (non-zero) activation admin.
func (r activationReg) requireAdmin(ctx *PrecompileContext) error {
	admin := r.admin()
	if admin == (common.Address{}) || ctx.Caller == (common.Address{}) || ctx.Caller != admin {
		return revB20("Unauthorized(address)", errSelUnauthorizedAddr, addrKey(ctx.Caller))
	}
	return nil
}

type b20ActivationPrecompile struct{ b20StatefulBase }

func (p *b20ActivationPrecompile) Name() string { return "B20ActivationRegistry" }

func (p *b20ActivationPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	ret, err := runB20Activation(ctx, input)
	return finishB20Metered(ctx, ret, err)
}

func runB20Activation(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]
	reg := newActivationReg(ctx)

	switch sel {
	// reads (permitted in read-only frames)
	case selIsActivated:
		feature, err := readWord(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(reg.isActivated(feature)), nil
	case selCheckActivated:
		feature, err := readWord(args, 0)
		if err != nil {
			return nil, err
		}
		if !reg.isActivated(feature) {
			return nil, revB20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
		}
		return nil, nil
	case selActivationAdm:
		return addrKey(reg.admin()).Bytes(), nil

	// writes (activation admin only)
	case selActivate:
		return nil, setFeature(ctx, reg, args, true)
	case selDeactivate:
		return nil, setFeature(ctx, reg, args, false)
	case selSetAdmin:
		return nil, updateActivationAdmin(ctx, reg, args)
	}
	return nil, ErrExecutionReverted
}

// setFeature flips a feature flag. A no-op is surfaced rather than accepted:
// activating an active feature fails, and deactivating an inactive one reuses
// FeatureNotActivated.
func setFeature(ctx *PrecompileContext, reg activationReg, args []byte, on bool) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	feature, err := readWord(args, 0)
	if err != nil {
		return err
	}
	if err := reg.requireAdmin(ctx); err != nil {
		return err
	}
	switch active := reg.isActivated(feature); {
	case on && active:
		return revB20("AlreadyActivated(bytes32)", errSelAlreadyActivated, feature)
	case !on && !active:
		return revB20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	if on {
		ctx.ensureSentinel() // only the activating write creates state worth keeping
	}
	reg.setActivated(feature, on)
	topic := b20TopicFeatureDeactivated
	if on {
		topic = b20TopicFeatureActivated
	}
	ctx.AddLog([]common.Hash{topic, feature, addrKey(ctx.Caller)}, nil)
	return nil
}

// updateActivationAdmin rotates the activation admin, so a compromised key
// needs no hard fork to replace. The zero address is rejected: it is the
// "no admin" sentinel, and accepting it would permanently disable activation.
func updateActivationAdmin(ctx *PrecompileContext, reg activationReg, args []byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	newAdmin, err := readAddress(args, 0)
	if err != nil {
		return err
	}
	// The zero check precedes the authorization check, matching base-std's
	// order. A non-admin passing the zero address therefore sees
	// ZeroAdminAddress rather than Unauthorized; the argument is caller-supplied
	// so the earlier error leaks nothing about registry state.
	if newAdmin == (common.Address{}) {
		return revB20("ZeroAdminAddress()", errSelZeroAdminAddress)
	}
	if err := reg.requireAdmin(ctx); err != nil {
		return err
	}
	previous := reg.admin()
	ctx.ensureSentinel()
	reg.setAdmin(newAdmin)
	ctx.AddLog([]common.Hash{b20TopicAdminChanged, addrKey(previous), addrKey(newAdmin), addrKey(ctx.Caller)}, nil)
	return nil
}

// ensureFeatureActivated is the gate the factory and the PolicyRegistry apply
// to their creation paths. It is deliberately not applied on any path of a
// token that already exists (BEP-702 section 3.15).
func ensureFeatureActivated(ctx *PrecompileContext, feature common.Hash) error {
	if !newActivationReg(ctx).isActivated(feature) {
		return revB20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	return nil
}

func variantFeature(variant byte) (common.Hash, bool) {
	switch variant {
	case b20VariantAsset:
		return featureB20Asset, true
	case b20VariantStablecoin:
		return featureB20Stablecoin, true
	}
	return common.Hash{}, false
}
