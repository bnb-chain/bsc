package vm

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// params.TestChainConfig has no Parlia, so a harness built on it would exercise
// CAS20 on a chain where production never enables it.
func cas20TestChainConfig() *params.ChainConfig {
	cfg := *params.TestChainConfig
	zero := uint64(0)
	cfg.JennerTime = &zero
	cfg.Parlia = &params.ParliaConfig{}
	return &cfg
}

func cas20Addr(variant, id byte) common.Address {
	var a common.Address
	a[0], a[1] = cas20MarkerPrefix[0], cas20MarkerPrefix[1]
	a[10] = variant
	a[19] = id
	return a
}

func TestIsCAS20Address(t *testing.T) {
	cases := []struct {
		name string
		addr common.Address
		want bool
	}{
		{"asset token", cas20Addr(cas20VariantAsset, 1), true},
		{"stablecoin token", cas20Addr(cas20VariantStablecoin, 1), true},
		{"unknown variant still in space", cas20Addr(0x7f, 1), true},
		{"factory is outside token space", CAS20FactoryAddress, false},
		{"wrong magic prefix", common.HexToAddress("0xb3000000000000000000ab0000000000000000ff"), false},
		{"nonzero padding byte", common.HexToAddress("0xca520000000001000000000000000000000000ff"), false},
		{"the reference implementation's prefix", common.HexToAddress("0xb2000000000000000000000000000000000000ff"), false},
		{"zero address", common.Address{}, false},
	}
	for _, tc := range cases {
		if got := IsCAS20Address(tc.addr); got != tc.want {
			t.Errorf("%s: IsCAS20Address(%s) = %v, want %v", tc.name, tc.addr.Hex(), got, tc.want)
		}
	}
}

func TestResolveCAS20(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	asset := cas20Addr(cas20VariantAsset, 1)
	stable := cas20Addr(cas20VariantStablecoin, 1)
	unknown := cas20Addr(0x02, 1)
	uninit := cas20Addr(cas20VariantAsset, 2)

	for _, a := range []common.Address{asset, stable, unknown} {
		statedb.SetCode(a, CAS20MarkerCode, tracing.CodeChangeContractCreation)
	}

	if p, ok := resolveCAS20(CAS20FactoryAddress); !ok {
		t.Fatal("factory address should resolve")
	} else if _, ok := p.(*cas20FactoryPrecompile); !ok {
		t.Fatalf("factory resolved to %T, want *cas20FactoryPrecompile", p)
	}

	if p, ok := resolveCAS20(asset); !ok {
		t.Fatal("initialized asset token should resolve")
	} else if _, ok := p.(*cas20AssetPrecompile); !ok {
		t.Fatalf("asset resolved to %T, want *cas20AssetPrecompile", p)
	}
	if p, ok := resolveCAS20(stable); !ok {
		t.Fatal("initialized stablecoin token should resolve")
	} else if _, ok := p.(*cas20StablecoinPrecompile); !ok {
		t.Fatalf("stablecoin resolved to %T, want *cas20StablecoinPrecompile", p)
	}

	if _, ok := resolveCAS20(unknown); ok {
		t.Error("unknown variant should not resolve")
	}
	// Uninitialized but routed: existence is the handler's check, so a value-bearing
	// call is refused rather than stranded (BEP-702 3.3).
	if p, ok := resolveCAS20(uninit); !ok {
		t.Error("uninitialized recognized-variant address should still route")
	} else if _, ok := p.(*cas20AssetPrecompile); !ok {
		t.Errorf("uninitialized asset address resolved to %T, want *cas20AssetPrecompile", p)
	}
	if _, ok := resolveCAS20(common.HexToAddress("0x1234")); ok {
		t.Error("non-CAS20 address should not resolve")
	}
}

func TestCAS20VariantOf(t *testing.T) {
	_, evm := newCAS20EVM(t)
	call := func(to common.Address) ([]byte, error) {
		ret, _, err := evm.Call(cas20Alice, CAS20FactoryAddress,
			cas20Call(selVariantOf, addrKey(to)), NewGasBudget(1_000_000), uint256.NewInt(0))
		return ret, err
	}

	for _, tc := range []struct {
		name string
		addr common.Address
		want byte
	}{
		{"asset", cas20Addr(cas20VariantAsset, 1), cas20VariantAsset},
		{"stablecoin", cas20Addr(cas20VariantStablecoin, 1), cas20VariantStablecoin},
	} {
		ret, err := call(tc.addr)
		if err != nil {
			t.Fatalf("%s: variantOf err %v", tc.name, err)
		}
		if !bytes.Equal(ret, wU8(tc.want).Bytes()) {
			t.Errorf("%s: variantOf = %x, want %x", tc.name, ret, wU8(tc.want).Bytes())
		}
	}

	// Derived from the address alone, so an address never created still reports its variant.
	if _, err := call(cas20Addr(cas20VariantAsset, 0xfe)); err != nil {
		t.Errorf("variantOf on an uncreated address err = %v, want success", err)
	}

	for _, tc := range []struct {
		name string
		addr common.Address
	}{
		{"unrecognized variant", cas20Addr(0x7f, 1)},
		{"outside the token space", common.HexToAddress("0x1234")},
		{"the factory itself", CAS20FactoryAddress},
		{"zero address", common.Address{}},
	} {
		ret, err := call(tc.addr)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: variantOf err = %v, want revert", tc.name, err)
		}
		if !bytes.Equal(ret, errSelInvalidVariant[:]) {
			t.Errorf("%s: revert data = %x, want InvalidVariant()", tc.name, ret)
		}
	}
}

// BEP-702 3.2: DELEGATECALL and CALLCODE revert rather than halt, so the caller
// keeps its gas and can decode the reason.
func TestCAS20DelegateCallGuard(t *testing.T) {
	want := revCAS20("DelegateCallNotAllowed()", errSelDelegateCallDenied)
	wantData, _ := finishCAS20(nil, want)
	if len(wantData) != 4 {
		t.Fatalf("the expected payload is %d bytes, want a bare selector", len(wantData))
	}
	precompiles := []StatefulPrecompiledContract{cas20Factory, cas20Asset, cas20Stablecoin, cas20Policy, cas20Activation}
	for _, p := range precompiles {
		ret, err := p.RunStateful(&PrecompileContext{DirectCall: false}, nil)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%T: err = %v, want a revert", p, err)
		}
		if !bytes.Equal(ret, wantData) {
			t.Errorf("%T: returndata = %x, want DelegateCallNotAllowed() = %x", p, ret, wantData)
		}
	}
}

// The plain Run path is never reached in practice, but must not silently no-op.
func TestCAS20StatelessDispatchGuard(t *testing.T) {
	var p PrecompiledContract = &cas20AssetPrecompile{}
	if _, err := p.Run(nil); !errors.Is(err, ErrCAS20StatelessDispatch) {
		t.Errorf("Run err = %v, want ErrCAS20StatelessDispatch", err)
	}
}

// BEP-702 3.3: a reserved address holding no token is routed, so value sent to it
// is refused rather than stranded.
func TestCAS20UninitializedAddressBehavior(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := common.HexToAddress("0xca11e5")
	empty := cas20Addr(cas20VariantAsset, 0x77)

	ret, _, err := evm.Call(caller, empty, cas20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("call to uninitialized token err = %v, want ErrExecutionReverted", err)
	}
	if len(ret) != 0 {
		t.Fatalf("revert data = %x, want empty", ret)
	}

	ret, _, err = evm.Call(caller, empty, cas20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(5))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("value-bearing call err = %v, want ErrExecutionReverted", err)
	}
	if !bytes.Equal(ret, errSelNonPayable[:]) {
		t.Fatalf("revert data = %x, want NonPayable() = %x", ret, errSelNonPayable)
	}

	// An unrecognized variant is not routed, so the ordinary account path applies.
	future := cas20Addr(0x02, 0x77)
	ret, _, err = evm.Call(caller, future, cas20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("call to a future-variant address err = %v, want success", err)
	}
	if len(ret) != 0 {
		t.Fatalf("future-variant call returned %x, want empty", ret)
	}
}

func TestCAS20GateIsBSCOnly(t *testing.T) {
	newEVM := func(cfg *params.ChainConfig) *EVM {
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		bc := BlockContext{
			Random:      &common.Hash{}, // post-merge rules, matching a live BSC chain
			CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
			Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
			BlockNumber: big.NewInt(1),
			Time:        1,
		}
		return NewEVM(bc, statedb, cfg, Config{})
	}

	bsc := newEVM(cas20TestChainConfig())
	if !bsc.chainRules.IsJenner || !bsc.chainRules.IsInBSC {
		t.Fatal("the BSC harness must have both the CAS20 fork flag and IsInBSC")
	}
	if !bsc.cas20Enabled() {
		t.Error("CAS20 must be enabled on a BSC chain past the fork")
	}

	// Same fork time, no Parlia: the flag itself must be false, not just the routing.
	nonBSCCfg := *cas20TestChainConfig()
	nonBSCCfg.Parlia = nil
	nonBSC := newEVM(&nonBSCCfg)
	if nonBSC.chainRules.IsInBSC {
		t.Fatal("a config without Parlia must not report IsInBSC")
	}
	if nonBSC.chainRules.IsJenner {
		t.Error("IsJenner must embed the IsInBSC gate, so a non-BSC config with jennerTime " +
			"set does not reach the fork at all")
	}
	if nonBSC.cas20Enabled() {
		t.Error("CAS20 must not be enabled off BSC, even past the fork")
	}
	for _, addr := range []common.Address{
		CAS20FactoryAddress, CAS20PolicyRegistryAddress, CAS20ActivationRegistryAddress,
		cas20Addr(cas20VariantAsset, 1),
	} {
		if _, ok := nonBSC.precompile(addr); ok {
			t.Errorf("%s resolved to a precompile off BSC", addr.Hex())
		}
	}
}

// The existence check charges an account access; when that charge is what
// exhausted the budget, the exit is out of gas, not a revert.
func TestCAS20UninitializedExitReportsOutOfGas(t *testing.T) {
	call := func(budget uint64) (*PrecompileContext, error) {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		gas := NewGasBudget(budget)
		ctx := &PrecompileContext{
			StateDB: statedb, Self: cas20Addr(cas20VariantAsset, 9),
			Caller: cas20Alice, DirectCall: true, gas: &gas,
		}
		_, err := cas20Asset.RunStateful(ctx, cas20Call(selBalanceOf, addrKey(cas20Alice)))
		return ctx, err
	}

	// Enough for the calldata charge but not the cold account access.
	ctx, err := call(params.ColdAccountAccessCostEIP2929 - 1)
	if !ctx.OutOfGas() {
		t.Fatal("expected the account access to exhaust this budget")
	}
	if !errors.Is(err, ErrOutOfGas) {
		t.Errorf("err = %v, want ErrOutOfGas — an unaffordable charge is not a revert", err)
	}

	ctx, err = call(1_000_000)
	if ctx.OutOfGas() {
		t.Fatal("did not expect exhaustion with a generous budget")
	}
	if !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("err = %v, want ErrExecutionReverted for an uninitialized token", err)
	}
}

// The reserved space is routed exactly when Jenner is active, and behaves as it
// did before CAS20 existed otherwise.
func TestCAS20RoutingFollowsTheFork(t *testing.T) {
	token := cas20Addr(cas20VariantAsset, 1)
	for _, tc := range []struct {
		name   string
		jenner *uint64
		want   bool
	}{
		{"Jenner active", new(uint64), true},
		{"Jenner unscheduled", nil, false},
	} {
		cfg := *cas20TestChainConfig()
		cfg.JennerTime = tc.jenner
		evm := NewEVM(cas20BlockContext(1), nil, &cfg, Config{})

		if got := evm.cas20Enabled(); got != tc.want {
			t.Errorf("%s: cas20Enabled = %v, want %v", tc.name, got, tc.want)
		}
		for _, addr := range []common.Address{token, CAS20FactoryAddress,
			CAS20PolicyRegistryAddress, CAS20ActivationRegistryAddress} {
			_, ok := evm.precompile(addr)
			if ok != tc.want {
				t.Errorf("%s: %s resolves to a precompile = %v, want %v", tc.name, addr.Hex(), ok, tc.want)
			}
		}
	}
}
