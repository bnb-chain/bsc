package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// ActivationRegistry: the per-feature governance switch. It gates token creation
// and PolicyRegistry writes only — deactivation never reaches an
// existing token, so it cannot freeze balances.

const cas20ActivationNamespace = "bsc.activation_registry"

const (
	actSlotFeatures = 0 // mapping(bytes32 feature => bool)
	actSlotAdmin    = 1 // address, zero means no admin exists
)

// cas20ParamAdmin is the only key this registry takes.
const cas20ParamAdmin = "admin"

var cas20ActivationRoot = erc7201Root(cas20ActivationNamespace)

var (
	featureCAS20Asset      = crypto.Keccak256Hash([]byte("bsc.cas20_asset"))
	featureCAS20Stablecoin = crypto.Keccak256Hash([]byte("bsc.cas20_stablecoin"))
	featurePolicyRegistry  = crypto.Keccak256Hash([]byte("bsc.policy_registry"))
)

var (
	selIsActivated    = selector("isActivated(bytes32)")
	selCheckActivated = selector("checkActivated(bytes32)")
	selActivationAdm  = selector("admin()")
	selActivate       = selector("activate(bytes32)")
	selDeactivate     = selector("deactivate(bytes32)")
	selUpdateParam    = selector("updateParam(string,bytes)")

	cas20TopicFeatureActivated   = eventTopic("FeatureActivated(bytes32,address)")
	cas20TopicFeatureDeactivated = eventTopic("FeatureDeactivated(bytes32,address)")
	cas20TopicAdminChanged       = eventTopic("AdminChanged(address,address,address)")
	cas20TopicParamChange        = eventTopic("ParamChange(string,bytes)")
)

// activationReg is a gas-metered view over the registry's storage.
type activationReg struct{ s cas20Storage }

func newActivationReg(ctx *PrecompileContext) activationReg {
	return activationReg{s: newMeteredCAS20StorageAt(ctx, CAS20ActivationRegistryAddress)}
}

func actSlot(offset uint64) common.Hash {
	return offsetSlot(cas20ActivationRoot, offset)
}

func (r activationReg) isActivated(feature common.Hash) bool {
	return r.s.getWord(r.s.mapSlot(actSlot(actSlotFeatures), feature)) != (common.Hash{})
}

func (r activationReg) setActivated(feature common.Hash, on bool) {
	var v common.Hash
	if on {
		v[31] = 1
	}
	// Cleared, not written false, so the refund matches a Solidity `delete`.
	r.s.setWord(r.s.mapSlot(actSlot(actSlotFeatures), feature), v)
}

func (r activationReg) admin() common.Address {
	return common.BytesToAddress(r.s.getWord(actSlot(actSlotAdmin)).Bytes())
}

func (r activationReg) setAdmin(a common.Address) { r.s.setWord(actSlot(actSlotAdmin), addrKey(a)) }

func (r activationReg) requireAdmin(ctx *PrecompileContext) error {
	if a := r.admin(); a == (common.Address{}) || ctx.Caller != a {
		return revCAS20("Unauthorized(address)", errSelUnauthorizedAddr, addrKey(ctx.Caller))
	}
	return nil
}

func requireGov(ctx *PrecompileContext) error {
	if ctx.Caller != params.CAS20GovHubAddress {
		return revCAS20("Unauthorized(address)", errSelUnauthorizedAddr, addrKey(ctx.Caller))
	}
	return nil
}

type cas20ActivationPrecompile struct{ cas20StatefulBase }

func (p *cas20ActivationPrecompile) Name() string { return "CAS20ActivationRegistry" }

func (p *cas20ActivationPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	ret, err := runCAS20Activation(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}

func runCAS20Activation(ctx *PrecompileContext, input []byte) ([]byte, error) {
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
			return nil, revCAS20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
		}
		return nil, nil
	case selActivationAdm:
		return addrKey(reg.admin()).Bytes(), nil

	// writes: governance appoints the admin, the admin works the switch
	case selUpdateParam:
		return nil, updateParam(ctx, reg, args)
	case selActivate:
		return nil, setFeature(ctx, reg, args, true)
	case selDeactivate:
		return nil, setFeature(ctx, reg, args, false)
	}
	return nil, ErrExecutionReverted
}

// updateParam is the governance contract entry point, in the shape every BSC system
// contract uses. Governance appoints the admin here and the admin works the
// switch, so a feature opens without a voting period while authority stays with governance.
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

	if key != cas20ParamAdmin {
		return revCAS20StringBytes("UnknownParam(string,bytes)", errSelUnknownParam, key, value)
	}
	if len(value) != 20 {
		return revCAS20StringBytes("InvalidValue(string,bytes)", errSelInvalidValue, key, value)
	}
	next := common.BytesToAddress(value)
	if next == (common.Address{}) {
		return revCAS20StringBytes("InvalidValue(string,bytes)", errSelInvalidValue, key, value)
	}
	previous := reg.admin()
	reg.setAdmin(next)
	if !ctx.AddLog([]common.Hash{cas20TopicAdminChanged, addrKey(previous), addrKey(next), addrKey(ctx.Caller)}, nil) {
		return ErrOutOfGas
	}
	// Logged alongside the registry's own event, as every system contract does.
	if !ctx.AddLog([]common.Hash{cas20TopicParamChange},
		encodeTuple(abiString(key), abiBytes(value))) {
		return ErrOutOfGas
	}
	return nil
}

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
		return revCAS20("AlreadyActivated(bytes32)", errSelAlreadyActivated, feature)
	case !on && !active:
		return revCAS20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	reg.setActivated(feature, on)
	topic := cas20TopicFeatureDeactivated
	if on {
		topic = cas20TopicFeatureActivated
	}
	if !ctx.AddLog([]common.Hash{topic, feature, addrKey(ctx.Caller)}, nil) {
		return ErrOutOfGas
	}
	return nil
}

func ensureFeatureActivated(ctx *PrecompileContext, feature common.Hash) error {
	if !newActivationReg(ctx).isActivated(feature) {
		return revCAS20("FeatureNotActivated(bytes32)", errSelFeatureNotActive, feature)
	}
	return nil
}

func variantFeature(variant byte) (common.Hash, bool) {
	v, ok := cas20Variants[variant]
	return v.feature, ok
}
