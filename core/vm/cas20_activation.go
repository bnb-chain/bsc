package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// ActivationRegistry: the per-feature governance switch (BEP-702 3.15). It gates
// token creation and PolicyRegistry writes only — deactivation never reaches an
// existing token, so it cannot freeze balances.

const cas20ActivationNamespace = "bsc.activation_registry"

// Slots are append-only: never reorder them across forks.
const (
	actSlotFeatures = 0 // mapping(bytes32 feature => bool)
	actSlotAdmin    = 1 // address, zero means no admin exists
)

// cas20ParamAdmin is the only parameter this registry takes. Governance appoints
// the activation admin and the admin operates the switch, so opening or closing
// a feature does not wait out a voting period while the authority behind it
// still belongs to governance. Any other key is refused: GovHub reports a
// target's revert in an event and leaves the proposal successful, so a key
// accepted by accident would read as a deliberate governance action.
const cas20ParamAdmin = "admin"

var cas20ActivationRoot = erc7201Root(cas20ActivationNamespace)

// Feature identifiers: keccak256 of the canonical feature name (BEP-702 3.15).
// The names are consensus-visible and independent of the ERC-7201 storage
// namespaces, which the first two deliberately spell differently
// ("bsc.cas20.asset") and the third does not: featurePolicyRegistry and
// cas20PolicyNamespace are the same string for two unrelated derivations. Renaming
// the namespace must not carry the feature with it, or every integrator's
// activate() call breaks.
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
	// Deactivating clears the slot rather than writing false, so the storage
	// refund applies exactly as it would for a Solidity `delete`.
	r.s.setWord(r.s.mapSlot(actSlot(actSlotFeatures), feature), v)
}

// admin returns the activation admin. Zero means none is set, so nothing can be
// activated yet — a state governance can always leave, unlike the seeded slot
// this replaced.
func (r activationReg) admin() common.Address {
	return common.BytesToAddress(r.s.getWord(actSlot(actSlotAdmin)).Bytes())
}

func (r activationReg) setAdmin(a common.Address) { r.s.setWord(actSlot(actSlotAdmin), addrKey(a)) }

// requireAdmin reverts unless the caller is the activation admin. The stored
// address must be non-zero: an empty slot means no admin exists, and equality
// alone would let a caller with no address hold the switch.
func (r activationReg) requireAdmin(ctx *PrecompileContext) error {
	if a := r.admin(); a == (common.Address{}) || ctx.Caller != a {
		return revCAS20("Unauthorized(address)", errSelUnauthorizedAddr, addrKey(ctx.Caller))
	}
	return nil
}

// requireGov reverts unless the caller is GovHub. Appointment authority is a
// constant rather than configuration, so there is nothing to seed at the fork and
// no way to ship a chain whose switch can never be thrown: an appointment is an
// ordinary parameter-change proposal, and a lost admin key is replaced by another
// one rather than by a fork.
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

// updateParam is the governance entry point, in the shape every BSC system
// contract uses: a canonical name and a raw value, with address values 20 bytes
// wide. It takes exactly one key. Governance appoints the admin here; the admin
// then works the switch through activate and deactivate, so opening or closing a
// feature does not wait out a voting period while the authority behind it still
// belongs to governance.
func updateParam(ctx *PrecompileContext, reg activationReg, args []byte) error {
	// Refused before the decode. The metering layer would refuse the write anyway
	// and the exit reports the same error either way, so this bounds the work a
	// read-only frame can ask for rather than deciding the outcome.
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
	// Twenty raw bytes, as every address-valued system parameter takes, and
	// non-zero because an admin nobody holds cannot open anything.
	if len(value) != 20 {
		return revCAS20StringBytes("InvalidValue(string,bytes)", errSelInvalidValue, key, value)
	}
	next := common.BytesToAddress(value)
	if next == (common.Address{}) {
		return revCAS20StringBytes("InvalidValue(string,bytes)", errSelInvalidValue, key, value)
	}
	previous := reg.admin()
	ctx.ensureSentinel()
	reg.setAdmin(next)
	if !ctx.AddLog([]common.Hash{cas20TopicAdminChanged, addrKey(previous), addrKey(next), addrKey(ctx.Caller)}, nil) {
		return ErrOutOfGas
	}
	// Every system contract logs the parameter change alongside its own event, so
	// one stream carries the exact key and raw value governance submitted.
	if !ctx.AddLog([]common.Hash{cas20TopicParamChange},
		encodeTuple(abiString(key), abiBytes(value))) {
		return ErrOutOfGas
	}
	return nil
}

// setFeature flips one feature, for the admin governance appointed. A no-op is
// surfaced rather than accepted: activating an active feature fails, and
// deactivating an inactive one reuses FeatureNotActivated.
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
	if on {
		ctx.ensureSentinel() // only the activating write creates state worth keeping
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

// ensureFeatureActivated is the gate the factory and the PolicyRegistry apply to
// their creation paths. It is deliberately not applied on any path of a token
// that already exists (BEP-702 3.15).
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
