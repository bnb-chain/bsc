package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// encodeSetAdmin builds the governance call that appoints the activation admin.
func encodeSetAdmin(a common.Address) []byte {
	return encodeUpdateParamRaw(cas20ParamAdmin, a.Bytes())
}

// encodeUpdateParamRaw builds updateParam(string,bytes) with the value passed
// through verbatim, so a test can submit a wrong-length one.
func encodeUpdateParamRaw(key string, value []byte) []byte {
	out := append([]byte{}, selUpdateParam[:]...)
	out = append(out, u256hash(0x40).Bytes()...)
	out = append(out, u256hash(0x40+32+32*wordsOf(len(key))).Bytes()...)
	out = append(out, u256hash(uint64(len(key))).Bytes()...)
	out = append(out, padRight32([]byte(key))...)
	out = append(out, u256hash(uint64(len(value))).Bytes()...)
	out = append(out, padRight32(value)...)
	return out
}

func wordsOf(n int) uint64 {
	if n == 0 {
		return 0
	}
	return uint64((n + 31) / 32)
}

func padRight32(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, 32*((len(b)+31)/32))
	copy(out, b)
	return out
}

// featureNameAsset and featureNameStablecoin are the canonical names the feature
// identifiers hash from, spelled here so a rename has to change the tests too.
const (
	featureNameAsset      = "bsc.cas20_asset"
	featureNameStablecoin = "bsc.cas20_stablecoin"
	featureNamePolicy     = "bsc.policy_registry"
)

// TestCAS20ActivationRegistry exercises the two-step authority: governance
// appoints the activation admin, the admin works the switch, and reads never
// revert. Neither half can do the other's job.
func TestCAS20ActivationRegistry(t *testing.T) {
	_, evm := newCAS20EVM(t)
	reg := CAS20ActivationRegistryAddress
	gov := params.CAS20GovHubAddress
	admin := common.HexToAddress("0xad4149")
	stranger := common.HexToAddress("0x5747a9e")

	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, reg, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return ret, err
	}
	feature := common.HexToHash("0xf1") // not seeded, so it starts inactive

	// Reads never revert, whatever the flag.
	if ret, err := call(stranger, cas20Call(selIsActivated, feature)); err != nil {
		t.Fatalf("isActivated: %v", err)
	} else if !bytes.Equal(ret, encBool(false)) {
		t.Errorf("isActivated = %x, want false", ret)
	}
	if _, err := call(stranger, cas20Call(selCheckActivated, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("checkActivated on an inactive feature: err = %v, want a revert", err)
	}

	// Only governance appoints, and only the appointed admin may flip a feature.
	if _, err := call(stranger, encodeSetAdmin(admin)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("updateParam from a stranger: err = %v, want a revert", err)
	}
	if _, err := call(gov, encodeSetAdmin(admin)); err != nil {
		t.Fatalf("governance appointing the admin: %v", err)
	}
	if ret, err := call(stranger, cas20Call(selActivationAdm)); err != nil || !bytes.Equal(ret, addrKey(admin).Bytes()) {
		t.Fatalf("admin() = %x err %v, want %s", ret, err, admin.Hex())
	}
	if _, err := call(gov, cas20Call(selActivate, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("governance holds the appointment, not the switch; activate from GovHub must fail")
	}
	if _, err := call(stranger, cas20Call(selActivate, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("activate from a stranger must fail")
	}
	if _, err := call(admin, cas20Call(selActivate, feature)); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// No-ops are surfaced in both directions.
	if _, err := call(admin, cas20Call(selActivate, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("activating an active feature should report AlreadyActivated")
	}
	if _, err := call(admin, cas20Call(selDeactivate, feature)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := call(admin, cas20Call(selDeactivate, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("deactivating an inactive feature should report FeatureNotActivated")
	}

	// Governance can rotate, and the previous admin loses the switch at once —
	// which is what makes a compromised key recoverable without a fork.
	next := common.HexToAddress("0xad4150")
	if _, err := call(gov, encodeSetAdmin(next)); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := call(admin, cas20Call(selActivate, feature)); !errors.Is(err, ErrExecutionReverted) {
		t.Error("the replaced admin must no longer hold the switch")
	}
	if _, err := call(next, cas20Call(selActivate, feature)); err != nil {
		t.Errorf("the new admin cannot use the switch: %v", err)
	}
}

// policies, and nothing on a token that already exists (BEP-702 3.15).
func TestCAS20ActivationGates(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// A token created while the feature is open.
	initCalls := [][]byte{
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Alice), u256hash(1000)),
	}
	ret, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xa1"), creator, initCalls))
	if err != nil {
		t.Fatalf("createCAS20 while activated: %v", err)
	}
	token := common.BytesToAddress(ret)

	// Deactivate the Asset variant.
	if _, err := call(cas20TestCaller, CAS20ActivationRegistryAddress, cas20Call(selDeactivate, featureCAS20Asset)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Creation stops.
	if _, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xa2"), creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createCAS20 while deactivated err = %v, want FeatureNotActivated", err)
	}
	// The other variant is unaffected — the switch is per feature.
	if _, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantStablecoin, common.HexToHash("0xa3"), creator, nil)); err != nil {
		t.Fatalf("stablecoin creation must be unaffected: %v", err)
	}

	// The existing token keeps working: transfers, reads, everything.
	if _, err := call(cas20Alice, token, cas20Call(selTransfer, addrKey(cas20Bob), u256hash(10))); err != nil {
		t.Fatalf("transfer on a live token must not be gated: %v", err)
	}
	view := newUnmeteredCAS20Storage(statedb, token)
	if got := view.balanceOf(cas20Bob).Uint64(); got != 10 {
		t.Fatalf("bob balance = %d, want 10", got)
	}

	// PolicyRegistry: reads stay open, writes stop.
	if _, err := call(cas20TestCaller, CAS20ActivationRegistryAddress, cas20Call(selDeactivate, featurePolicyRegistry)); err != nil {
		t.Fatalf("deactivate policy registry: %v", err)
	}
	if _, err := call(creator, CAS20PolicyRegistryAddress, cas20Call(selIsAuthorized, u256hash(0), addrKey(cas20Alice))); err != nil {
		t.Fatalf("isAuthorized must never be gated: %v", err)
	}
	if _, err := call(creator, CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyBlocklist))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createPolicy while deactivated err = %v, want FeatureNotActivated", err)
	}
}

// TestCAS20RegistrySentinel pins the fix for the reaping hazard: a registry has
// no factory to create it, so its first write must plant the account sentinel.
// Storage alone leaves the account EIP-161-empty, and a clearing pass would
// take every flag and policy with it (BEP-702 3.16).
func TestCAS20RegistrySentinel(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc0ffee")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Strip the sentinel the fork planted, so the registry is bare going in. This
	// is deliberately a state the fork never leaves behind — the point is that the
	// guard does not depend on the fork having run, which is what makes it a
	// backstop rather than a duplicate of the seeding.
	statedb.SetCode(CAS20PolicyRegistryAddress, nil, tracing.CodeChangeContractCreation)
	if got := statedb.GetCodeHash(CAS20PolicyRegistryAddress); got == cas20MarkerCodeHash {
		t.Fatal("precondition: the policy registry should start without a sentinel")
	}
	if _, err := call(creator, CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyBlocklist))); err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	if got := statedb.GetCodeHash(CAS20PolicyRegistryAddress); got != cas20MarkerCodeHash {
		t.Fatalf("policy registry code hash = %x, want the sentinel %x", got, cas20MarkerCodeHash)
	}

	// The account must now survive a clearing pass with its storage intact.
	statedb.Finalise(true)
	if got := statedb.GetCodeHash(CAS20PolicyRegistryAddress); got != cas20MarkerCodeHash {
		t.Fatal("the policy registry was reaped despite the sentinel")
	}

	// The activation registry is the same story. Idempotence in gas terms is
	// asserted precisely by TestCAS20EnsureSentinelIdempotent; here the point is
	// only that a later write leaves the planted sentinel alone.
	const laterName = "bsc.something_else"
	if _, err := call(cas20TestCaller, CAS20ActivationRegistryAddress,
		cas20Call(selActivate, crypto.Keccak256Hash([]byte(laterName)))); err != nil {
		t.Fatalf("activate: %v", err)
	}
	planted := statedb.GetCodeHash(CAS20ActivationRegistryAddress)
	if planted != cas20MarkerCodeHash {
		t.Fatalf("activation registry code hash = %x, want the sentinel %x", planted, cas20MarkerCodeHash)
	}
	if _, err := call(cas20TestCaller, CAS20ActivationRegistryAddress,
		cas20Call(selDeactivate, crypto.Keccak256Hash([]byte(laterName)))); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if after := statedb.GetCodeHash(CAS20ActivationRegistryAddress); after != planted {
		t.Fatalf("the sentinel changed on a subsequent write: %x, want %x", after, planted)
	}
}

// TestCAS20EnsureSentinelIdempotent asserts the write-once property exactly, at
// the unit level. A loose "cheaper than CreateGas" bound is not enough: with
// the guard removed the account already has code, so CreateGas is not charged
// again and only the deposit and hash costs leak — a few hundred gas that a
// coarse bound cannot see. The second call must therefore charge the warm
// account access and nothing else.
func TestCAS20EnsureSentinelIdempotent(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	addr := CAS20PolicyRegistryAddress
	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{StateDB: statedb, Self: addr, gas: &gas}

	charged := func(fn func()) uint64 {
		before := gas.RegularGas
		fn()
		return before - gas.RegularGas
	}

	codeWrite := params.CreateGas +
		params.CreateDataGas*uint64(len(CAS20MarkerCode)) +
		params.Keccak256Gas + params.Keccak256WordGas
	first := charged(ctx.ensureSentinel)
	if first < codeWrite {
		t.Fatalf("first ensureSentinel charged %d, want at least the code write %d", first, codeWrite)
	}
	if statedb.GetCodeHash(addr) != cas20MarkerCodeHash {
		t.Fatal("first ensureSentinel did not plant the sentinel")
	}

	second := charged(ctx.ensureSentinel)
	if second != params.WarmStorageReadCostEIP2929 {
		t.Fatalf("second ensureSentinel charged %d, want %d — the warm account access alone; "+
			"anything more means the code write ran again",
			second, params.WarmStorageReadCostEIP2929)
	}
}

// TestCAS20NeverOverwritesForeignCode pins BEP-702 3.4 and 3.16: an address that
// already carries code is occupied, whoever put it there. createCAS20 must refuse
// it rather than plant the sentinel over it, and a registry must not either.
func TestCAS20NeverOverwritesForeignCode(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc0de")
	foreign := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // a plausible runtime stub

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// A token address that is occupied by something that is not a CAS20 token.
	salt := common.HexToHash("0xf01e")
	predicted := cas20DeriveAddress(cas20VariantAsset, creator, salt)
	statedb.SetCode(predicted, foreign, tracing.CodeChangeContractCreation)
	foreignHash := statedb.GetCodeHash(predicted)

	if _, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, salt, creator, nil)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createCAS20 at an occupied address err = %v, want TokenAlreadyExists", err)
	}
	if got := statedb.GetCodeHash(predicted); got != foreignHash {
		t.Fatalf("foreign code was overwritten: hash %x, want %x", got, foreignHash)
	}

	// A registry that somehow carries foreign code: the first write must not
	// plant over it. The account is already non-empty, so nothing is at risk.
	reg := CAS20PolicyRegistryAddress
	statedb.SetCode(reg, foreign, tracing.CodeChangeContractCreation)
	if _, err := call(creator, reg, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyBlocklist))); err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	if got := statedb.GetCodeHash(reg); got != foreignHash {
		t.Fatalf("registry foreign code was overwritten: hash %x, want %x", got, foreignHash)
	}
}

// TestCAS20ParamKeySpaceIsClosed pins the parameter surface. The registry takes
// exactly one key and refuses everything else, as BSC's system contracts do —
// GovHub reports a target's revert in an event and leaves the proposal
// successful, so a key accepted by accident would read as a deliberate
// governance action that in fact changed nothing.
func TestCAS20ParamKeySpaceIsClosed(t *testing.T) {
	_, evm := newCAS20EVM(t)
	gov := params.CAS20GovHubAddress
	call := func(input []byte) error {
		_, _, err := evm.Call(gov, CAS20ActivationRegistryAddress, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return err
	}
	addr := common.HexToAddress("0xad4151").Bytes()

	for _, tc := range []struct {
		name   string
		key    string
		value  []byte
		accept bool
	}{
		{"the one key", cas20ParamAdmin, addr, true},
		{"miscased", "Admin", addr, false},
		{"an unknown key", "bsc.something_later", addr, false},
		{"an empty key", "", addr, false},
		{"a padded address", cas20ParamAdmin, common.HexToHash("0xad4151").Bytes(), false},
		{"a short value", cas20ParamAdmin, addr[:19], false},
		{"the zero address", cas20ParamAdmin, make([]byte, 20), false},
	} {
		err := call(encodeUpdateParamRaw(tc.key, tc.value))
		if tc.accept && err != nil {
			t.Errorf("%s: err = %v, want accepted", tc.name, err)
		}
		if !tc.accept && !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want a revert", tc.name, err)
		}
	}
}

// TestCAS20ActivationAdminMustBeSet covers the clause that stops an empty admin
// slot from being matchable. The harness seeds an admin, so this builds a
// registry without one — the state a network is in between the fork and
// governance's first appointment. Equality alone would let the zero address hold
// the switch there.
func TestCAS20ActivationAdminMustBeSet(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *cas20TestChainConfig()
	evm := NewEVM(cas20BlockContext(1), statedb, &cfg, Config{})
	if _, ok := evm.precompile(CAS20ActivationRegistryAddress); !ok {
		t.Fatal("the activation registry does not resolve; the fork gate is in the way")
	}
	reg := cas20Storage{state: statedb, token: CAS20ActivationRegistryAddress}
	if got := reg.getWord(actSlot(actSlotAdmin)); got != (common.Hash{}) {
		t.Fatalf("the admin slot holds %x, want empty for this test to mean anything", got)
	}

	feature := common.HexToHash("0xf3")
	for _, caller := range []common.Address{{}, common.HexToAddress("0x57ra9e")} {
		_, _, err := evm.Call(caller, CAS20ActivationRegistryAddress,
			cas20Call(selActivate, feature), NewGasBudget(5_000_000), uint256.NewInt(0))
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("activate from %s against an empty admin slot: err = %v, want a revert",
				caller.Hex(), err)
		}
	}
	if got := reg.getWord(mappingSlot(actSlot(actSlotFeatures), feature)); got != (common.Hash{}) {
		t.Errorf("the feature reads %x, want off", got)
	}
}

// TestCAS20FeatureNamesArePinned holds the three canonical feature names against
// the identifiers derived from them. The names are consensus-visible: a feature
// id is keccak256 of its name, so changing one character breaks every
// integrator's activate() call and silently returns an activated feature to
// inactive. Nothing else in the suite reads the strings — every other test uses
// the Go variables, which move together with any rename.
func TestCAS20FeatureNamesArePinned(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   common.Hash
	}{
		{featureNameAsset, featureCAS20Asset},
		{featureNameStablecoin, featureCAS20Stablecoin},
		{featureNamePolicy, featurePolicyRegistry},
	} {
		if got := crypto.Keccak256Hash([]byte(tc.name)); got != tc.id {
			t.Errorf("keccak256(%q) = %x, want %x", tc.name, got, tc.id)
		}
	}

	// The policy registry's feature name and its ERC-7201 storage namespace are
	// the same string for two unrelated derivations, while the other two features
	// spell themselves differently from their namespaces. A rename must not carry
	// one into the other.
	if featureNamePolicy != cas20PolicyNamespace {
		t.Errorf("the policy feature name %q and namespace %q have diverged; if that is "+
			"deliberate, both values changed and every integrator's activate() call with it",
			featureNamePolicy, cas20PolicyNamespace)
	}
	for _, tc := range []struct{ feature, namespace string }{
		{featureNameAsset, cas20AssetNamespace},
		{featureNameStablecoin, cas20StablecoinNamespace},
	} {
		if tc.feature == tc.namespace {
			t.Errorf("feature %q now equals its storage namespace; they are independent "+
				"derivations and a namespace rename must not move the feature id", tc.feature)
		}
	}
}
