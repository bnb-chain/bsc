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
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// ActivationRegistry: the per-feature governance switch (BEP-702 3.15). It gates
// token creation and PolicyRegistry writes only — deactivation never reaches an
// existing token, so it cannot freeze balances.

const b20ActivationNamespace = "bsc.activation_registry"

// Slots are append-only: never reorder them across forks.
const (
	actSlotFeatures = 0 // mapping(bytes32 feature => bool)
)

// b20FeatureKeyPrefix bounds the parameter key space. A key carrying it names a
// feature, and the namespace stays open inside the prefix so a later BEP
// introduces one by naming it; anything else is refused rather than hashed into
// a feature nothing gates. That matters more than it looks: GovHub catches a
// target's revert and reports it in an event, leaving the proposal itself
// successful, so a key this registry accepted by accident would read as a
// deliberate governance action.
const b20FeatureKeyPrefix = "bsc."

var b20ActivationRoot = erc7201Root(b20ActivationNamespace)

// Feature identifiers: keccak256 of the canonical feature name (BEP-702 3.15).
// The names are consensus-visible and independent of the ERC-7201 storage
// namespaces, which the first two deliberately spell differently
// ("bsc.b20.asset") and the third does not: featurePolicyRegistry and
// b20PolicyNamespace are the same string for two unrelated derivations. Renaming
// the namespace must not carry the feature with it, or every integrator's
// activate() call breaks.
var (
	featureB20Asset       = crypto.Keccak256Hash([]byte("bsc.b20_asset"))
	featureB20Stablecoin  = crypto.Keccak256Hash([]byte("bsc.b20_stablecoin"))
	featurePolicyRegistry = crypto.Keccak256Hash([]byte("bsc.policy_registry"))
)

var (
	selIsActivated    = selector("isActivated(bytes32)")
	selCheckActivated = selector("checkActivated(bytes32)")
	selUpdateParam    = selector("updateParam(string,bytes)")

	b20TopicFeatureActivated   = eventTopic("FeatureActivated(bytes32,address)")
	b20TopicFeatureDeactivated = eventTopic("FeatureDeactivated(bytes32,address)")
	b20TopicParamChange        = eventTopic("ParamChange(string,bytes)")
)

// activationReg is a gas-metered view over the registry's storage.
type activationReg struct{ s b20Storage }

func newActivationReg(ctx *PrecompileContext) activationReg {
	return activationReg{s: newMeteredB20StorageAt(ctx, B20ActivationRegistryAddress)}
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

// requireGov reverts unless the caller is GovHub. The authority is a constant
// rather than a storage slot, so there is nothing to seed, nothing to rotate and
// no way to configure a chain that can never open a feature.
func requireGov(ctx *PrecompileContext) error {
	if ctx.Caller != params.B20GovHubAddress {
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
	// writes (governance only)
	case selUpdateParam:
		return nil, updateParam(ctx, reg, args)
	}
	return nil, ErrExecutionReverted
}

// updateParam is the governance entry point, in the shape every BSC system
// contract uses: a canonical name and a raw value, with address values 20 bytes
// and everything else 32. One key is reserved; the rest of the space is the open
// feature namespace BEP-702 3.15 requires, so a later BEP introduces a feature by
// naming it. Anything outside both is refused rather than guessed at.
func updateParam(ctx *PrecompileContext, reg activationReg, args []byte) error {
	if ctx.ReadOnly {
		return ErrWriteProtection
	}
	// Decoded before the authorization check, as Solidity's external decoder does.
	key, err := readStringArg(args, 0)
	if err != nil {
		return err
	}
	value, err := readBytesArg(args, 1)
	if err != nil {
		return err
	}
	if err := requireGov(ctx); err != nil {
		return err
	}

	if !strings.HasPrefix(key, b20FeatureKeyPrefix) || len(key) == len(b20FeatureKeyPrefix) {
		return revB20StringBytes("UnknownParam(string,bytes)", errSelUnknownParam, key, value)
	}
	if err := setFeature(ctx, reg, key, value); err != nil {
		return err
	}
	// Every system contract logs the parameter change alongside its own event, so
	// one stream carries the exact key and raw value governance submitted.
	if !ctx.AddLog([]common.Hash{b20TopicParamChange},
		encodeTuple(abiString(key), abiBytes(value))) {
		return ErrOutOfGas
	}
	return nil
}

// setFeature flips one feature. A no-op is surfaced rather than accepted:
// activating an active feature fails, and deactivating an inactive one reuses
// FeatureNotActivated.
func setFeature(ctx *PrecompileContext, reg activationReg, key string, value []byte) error {
	if len(value) != 32 {
		return revB20StringBytes("InvalidValue(string,bytes)", errSelInvalidValue, key, value)
	}
	on := common.BytesToHash(value) != (common.Hash{})
	if !ctx.chargeKeccak(len(key)) {
		return ErrOutOfGas
	}
	feature := crypto.Keccak256Hash([]byte(key))
	switch active := reg.isActivated(feature); {
	case on && active:
		return revB20("AlreadyActivated(bytes32)", errSelAlreadyActivated, feature)
	case !on && !active:
		return revB20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	ctx.ensureSentinel()
	reg.setActivated(feature, on)
	topic := b20TopicFeatureDeactivated
	if on {
		topic = b20TopicFeatureActivated
	}
	if !ctx.AddLog([]common.Hash{topic, feature, addrKey(ctx.Caller)}, nil) {
		return ErrOutOfGas
	}
	return nil
}

// ensureFeatureActivated is the gate the factory and the PolicyRegistry apply to
// their creation paths. It is deliberately not applied on any path of a token
// that already exists (BEP-702 3.15).
func ensureFeatureActivated(ctx *PrecompileContext, feature common.Hash) error {
	if !newActivationReg(ctx).isActivated(feature) {
		return revB20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	return nil
}

func variantFeature(variant byte) (common.Hash, bool) {
	v, ok := b20Variants[variant]
	return v.feature, ok
}
