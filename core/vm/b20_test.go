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

// b20TestChainConfig returns a chain config B20 is actually active under:
// Pasteur scheduled, and Parlia set so IsInBSC holds. The BSC gate matters —
// params.TestChainConfig alone has no Parlia, so a harness built on it would
// exercise B20 on a chain where production never enables it.
func b20TestChainConfig() *params.ChainConfig {
	cfg := *params.TestChainConfig
	zero := uint64(0)
	cfg.PasteurTime = &zero
	cfg.Parlia = &params.ParliaConfig{}
	// A usable admin is half the B20 gate (ChainConfig.B20Scheduled): the built-in
	// networks still name the placeholder, so they route nothing, and a harness
	// that left this unset would too.
	admin := b20ActivationAdmin
	cfg.B20ActivationAdmin = &admin
	return &cfg
}

// b20Addr builds a token address in the reserved space with the given variant
// byte and a one-byte identity fingerprint.
func b20Addr(variant, id byte) common.Address {
	var a common.Address
	a[0], a[1] = b20MarkerPrefix[0], b20MarkerPrefix[1]
	a[10] = variant
	a[19] = id
	return a
}

func TestIsB20Address(t *testing.T) {
	cases := []struct {
		name string
		addr common.Address
		want bool
	}{
		{"asset token", b20Addr(b20VariantAsset, 1), true},
		{"stablecoin token", b20Addr(b20VariantStablecoin, 1), true},
		{"unknown variant still in space", b20Addr(0x7f, 1), true},
		{"factory is outside token space", B20FactoryAddress, false},
		{"wrong magic prefix", common.HexToAddress("0xb3000000000000000000ab0000000000000000ff"), false},
		{"nonzero padding byte", common.HexToAddress("0x20b00000000001000000000000000000000000ff"), false},
		{"old 0xb2 prefix", common.HexToAddress("0xb2000000000000000000000000000000000000ff"), false},
		{"zero address", common.Address{}, false},
	}
	for _, tc := range cases {
		if got := IsB20Address(tc.addr); got != tc.want {
			t.Errorf("%s: IsB20Address(%s) = %v, want %v", tc.name, tc.addr.Hex(), got, tc.want)
		}
	}
}

// TestB20VariantSetsAgree pins b20VariantRecognized to the set resolveB20Token
// actually routes. They are separate switches, so a variant added to one and
// not the other would let variantOf name a variant that reaches no handler.
func TestB20VariantSetsAgree(t *testing.T) {
	for v := 0; v < 256; v++ {
		variant := byte(v)
		_, routed := resolveB20Token(b20Addr(variant, 1))
		if got := b20VariantRecognized(variant); got != routed {
			t.Errorf("variant 0x%02x: b20VariantRecognized = %v, routed = %v", variant, got, routed)
		}
	}
}

func TestResolveB20(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	asset := b20Addr(b20VariantAsset, 1)
	stable := b20Addr(b20VariantStablecoin, 1)
	unknown := b20Addr(0x02, 1)
	uninit := b20Addr(b20VariantAsset, 2)

	// Mark three tokens as initialized (factory writes a marker code).
	for _, a := range []common.Address{asset, stable, unknown} {
		statedb.SetCode(a, b20MarkerCode, tracing.CodeChangeContractCreation)
	}

	// Factory resolves regardless of state.
	if p, ok := resolveB20(B20FactoryAddress); !ok {
		t.Fatal("factory address should resolve")
	} else if _, ok := p.(*b20FactoryPrecompile); !ok {
		t.Fatalf("factory resolved to %T, want *b20FactoryPrecompile", p)
	}

	// Initialized Asset / Stablecoin route to the right variant.
	if p, ok := resolveB20(asset); !ok {
		t.Fatal("initialized asset token should resolve")
	} else if _, ok := p.(*b20AssetPrecompile); !ok {
		t.Fatalf("asset resolved to %T, want *b20AssetPrecompile", p)
	}
	if p, ok := resolveB20(stable); !ok {
		t.Fatal("initialized stablecoin token should resolve")
	} else if _, ok := p.(*b20StablecoinPrecompile); !ok {
		t.Fatalf("stablecoin resolved to %T, want *b20StablecoinPrecompile", p)
	}

	// Unknown variant byte is not routed even when initialized.
	if _, ok := resolveB20(unknown); ok {
		t.Error("unknown variant should not resolve")
	}
	// An uninitialized address with a recognized variant is still routed:
	// existence is checked inside the handler, not by dispatch, so a
	// value-bearing call to it is refused rather than stranded (BEP-702 3.3).
	if p, ok := resolveB20(uninit); !ok {
		t.Error("uninitialized recognized-variant address should still route")
	} else if _, ok := p.(*b20AssetPrecompile); !ok {
		t.Errorf("uninitialized asset address resolved to %T, want *b20AssetPrecompile", p)
	}
	// A plain address outside the space is not a B20 precompile.
	if _, ok := resolveB20(common.HexToAddress("0x1234")); ok {
		t.Error("non-B20 address should not resolve")
	}
}

// TestB20VariantOf covers the factory view. It validates the variant byte,
// unlike isB20: the return is an enum, so an unrecognized variant must revert
// rather than hand back a value the caller's own decoder would reject.
func TestB20VariantOf(t *testing.T) {
	_, evm := newB20EVM(t)
	call := func(to common.Address) ([]byte, error) {
		ret, _, err := evm.Call(b20Alice, B20FactoryAddress,
			b20Call(selVariantOf, addrKey(to)), NewGasBudget(1_000_000), uint256.NewInt(0))
		return ret, err
	}

	for _, tc := range []struct {
		name string
		addr common.Address
		want byte
	}{
		{"asset", b20Addr(b20VariantAsset, 1), b20VariantAsset},
		{"stablecoin", b20Addr(b20VariantStablecoin, 1), b20VariantStablecoin},
	} {
		ret, err := call(tc.addr)
		if err != nil {
			t.Fatalf("%s: variantOf err %v", tc.name, err)
		}
		if !bytes.Equal(ret, wU8(tc.want).Bytes()) {
			t.Errorf("%s: variantOf = %x, want %x", tc.name, ret, wU8(tc.want).Bytes())
		}
	}

	// Existence is irrelevant: the answer is derived from the address alone, so
	// an address no createB20 has produced still reports its variant.
	if _, err := call(b20Addr(b20VariantAsset, 0xfe)); err != nil {
		t.Errorf("variantOf on an uncreated address err = %v, want success", err)
	}

	for _, tc := range []struct {
		name string
		addr common.Address
	}{
		{"unrecognized variant", b20Addr(0x7f, 1)},
		{"outside the token space", common.HexToAddress("0x1234")},
		{"the factory itself", B20FactoryAddress},
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

// TestB20DelegateCallGuard checks that every variant rejects non-direct calls
// before touching any state, and reverts rather than halting: BEP-702 3.2 says
// DELEGATECALL and CALLCODE MUST revert, so the caller keeps its gas and can
// decode the reason. Returning the bare sentinel drained the whole budget and
// returned nothing.
func TestB20DelegateCallGuard(t *testing.T) {
	want := revB20("DelegateCallNotAllowed()", errSelDelegateCallDenied)
	wantData, _ := finishB20(nil, want)
	if len(wantData) != 4 {
		t.Fatalf("the expected payload is %d bytes, want a bare selector", len(wantData))
	}
	precompiles := []StatefulPrecompiledContract{b20Factory, b20Asset, b20Stablecoin, b20Policy, b20Activation}
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

// TestB20StatelessDispatchGuard checks the defensive backstop on the plain Run
// path (should never be reached in practice, but must not silently no-op).
func TestB20StatelessDispatchGuard(t *testing.T) {
	var p PrecompiledContract = &b20AssetPrecompile{}
	if _, err := p.Run(nil); !errors.Is(err, ErrB20StatelessDispatch) {
		t.Errorf("Run err = %v, want ErrB20StatelessDispatch", err)
	}
}

// TestB20UninitializedAddressBehavior pins BEP-702 3.3's dispatch table for a
// reserved address that holds no token. It is routed to the handler, not left
// to the ordinary account path, so a value-bearing call is refused instead of
// being accepted and stranded at an address with no way to withdraw from.
func TestB20UninitializedAddressBehavior(t *testing.T) {
	_, evm := newB20EVM(t)
	caller := common.HexToAddress("0xca11e5")
	// A well-formed Asset address nobody has created.
	empty := b20Addr(b20VariantAsset, 0x77)

	// Zero-value call: reaches the handler, which reverts with empty
	// returndata because no token exists there.
	ret, _, err := evm.Call(caller, empty, b20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("call to uninitialized token err = %v, want ErrExecutionReverted", err)
	}
	if len(ret) != 0 {
		t.Fatalf("revert data = %x, want empty", ret)
	}

	// Value-bearing call: refused with NonPayable before anything else, so the
	// value never lands.
	ret, _, err = evm.Call(caller, empty, b20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(5))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("value-bearing call err = %v, want ErrExecutionReverted", err)
	}
	if !bytes.Equal(ret, errSelNonPayable[:]) {
		t.Fatalf("revert data = %x, want NonPayable() = %x", ret, errSelNonPayable)
	}

	// An unrecognized variant is the other case: not routed at all, so the
	// ordinary account path applies and the call succeeds trivially.
	future := b20Addr(0x02, 0x77)
	ret, _, err = evm.Call(caller, future, b20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("call to a future-variant address err = %v, want success", err)
	}
	if len(ret) != 0 {
		t.Fatalf("future-variant call returned %x, want empty", ret)
	}
}

// TestB20GateIsBSCOnly pins that the B20 address space is routed only on BSC.
// The fork flag alone is not enough: IsPasteur asks only for London, so any
// non-BSC config that set pasteurTime would otherwise hijack the reserved space
// — and its registries would never be seeded, since that runs from a BSC-gated
// fork hook. Reserved addresses would stop behaving like ordinary accounts on a
// chain where no token can ever be created.
func TestB20GateIsBSCOnly(t *testing.T) {
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

	bsc := newEVM(b20TestChainConfig())
	if !bsc.chainRules.IsPasteur || !bsc.chainRules.IsInBSC {
		t.Fatal("the BSC harness must have both the B20 fork flag and IsInBSC")
	}
	if !bsc.b20Enabled() {
		t.Error("B20 must be enabled on a BSC chain past the fork")
	}

	// Same fork time, no Parlia: the fork flag still resolves, so this is exactly
	// the configuration the gate has to exclude.
	nonBSCCfg := *b20TestChainConfig()
	nonBSCCfg.Parlia = nil
	nonBSC := newEVM(&nonBSCCfg)
	if !nonBSC.chainRules.IsPasteur {
		t.Fatal("expected the fork flag to still hold without Parlia")
	}
	if nonBSC.chainRules.IsInBSC {
		t.Fatal("a config without Parlia must not report IsInBSC")
	}
	if nonBSC.b20Enabled() {
		t.Error("B20 must not be enabled off BSC, even past the fork")
	}
	// And the reserved space must resolve to nothing there.
	for _, addr := range []common.Address{
		B20FactoryAddress, B20PolicyRegistryAddress, B20ActivationRegistryAddress,
		b20Addr(b20VariantAsset, 1),
	} {
		if _, ok := nonBSC.precompile(addr); ok {
			t.Errorf("%s resolved to a precompile off BSC", addr.Hex())
		}
	}
}

// TestB20UninitializedExitReportsOutOfGas covers the exit taken when a routed
// address holds no token. That exit reverts with empty returndata, but the
// existence check it just performed charged an account access — so when that
// charge is what exhausted the budget, the call is out of gas and not a revert.
// The two differ in what the enclosing frame is told and what a tracer records.
func TestB20UninitializedExitReportsOutOfGas(t *testing.T) {
	call := func(budget uint64) (*PrecompileContext, error) {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		gas := NewGasBudget(budget)
		ctx := &PrecompileContext{
			StateDB: statedb, Self: b20Addr(b20VariantAsset, 9),
			Caller: b20Alice, DirectCall: true, gas: &gas,
		}
		_, err := b20Asset.RunStateful(ctx, b20Call(selBalanceOf, addrKey(b20Alice)))
		return ctx, err
	}

	// Enough for the calldata charge but not the cold account access: the
	// existence check cannot be paid for.
	ctx, err := call(params.ColdAccountAccessCostEIP2929 - 1)
	if !ctx.OutOfGas() {
		t.Fatal("expected the account access to exhaust this budget")
	}
	if !errors.Is(err, ErrOutOfGas) {
		t.Errorf("err = %v, want ErrOutOfGas — an unaffordable charge is not a revert", err)
	}

	// With room to pay for it, the same exit is an ordinary empty revert.
	ctx, err = call(1_000_000)
	if ctx.OutOfGas() {
		t.Fatal("did not expect exhaustion with a generous budget")
	}
	if !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("err = %v, want ErrExecutionReverted for an uninitialized token", err)
	}
}

// TestB20RoutingNeedsAUsableAdmin pins the second half of the B20 gate.
//
// The fork alone is not enough: routing also requires ChainConfig.B20Scheduled,
// so a network whose admin is still the placeholder answers at a B20 address the
// way it did before the feature existed. Without this, Chapel — whose PasteurTime
// is already in the past — would route the whole reserved space to handlers whose
// registry nobody can ever open, and a fresh sync would write state that nodes
// running the released client never wrote.
func TestB20RoutingNeedsAUsableAdmin(t *testing.T) {
	token := b20Addr(b20VariantAsset, 1)
	for _, tc := range []struct {
		name  string
		admin *common.Address
		want  bool
	}{
		{"a real admin", &b20ActivationAdmin, true},
		{"no admin", nil, false},
		{"the zero address", &common.Address{}, false},
		{"the placeholder", &params.B20ActivationAdminPlaceholder, false},
	} {
		cfg := *b20TestChainConfig()
		cfg.B20ActivationAdmin = tc.admin
		evm := NewEVM(b20BlockContext(1), nil, &cfg, Config{})

		if got := evm.b20Enabled(); got != tc.want {
			t.Errorf("%s: b20Enabled = %v, want %v", tc.name, got, tc.want)
		}
		for _, addr := range []common.Address{token, B20FactoryAddress,
			B20PolicyRegistryAddress, B20ActivationRegistryAddress} {
			_, ok := evm.precompile(addr)
			if ok != tc.want {
				t.Errorf("%s: %s resolves to a precompile = %v, want %v. An unusable admin "+
					"must leave the reserved space behaving as it did before B20 existed",
					tc.name, addr.Hex(), ok, tc.want)
			}
		}
	}
}
