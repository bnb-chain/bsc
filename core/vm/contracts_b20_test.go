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
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

// b20Addr builds a token address in the reserved space with the given variant
// byte and a one-byte identity fingerprint.
func b20Addr(variant, id byte) common.Address {
	var a common.Address
	a[0] = b20MagicPrefix
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
		{"nonzero padding byte", common.HexToAddress("0xb2000000000100000000000000000000000000ff"), false},
		{"zero address", common.Address{}, false},
	}
	for _, tc := range cases {
		if got := IsB20Address(tc.addr); got != tc.want {
			t.Errorf("%s: IsB20Address(%s) = %v, want %v", tc.name, tc.addr.Hex(), got, tc.want)
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
		statedb.SetCode(a, []byte{0x01}, tracing.CodeChangeContractCreation)
	}

	// Factory resolves regardless of state.
	if p, ok := resolveB20(statedb, B20FactoryAddress); !ok {
		t.Fatal("factory address should resolve")
	} else if _, ok := p.(*b20FactoryPrecompile); !ok {
		t.Fatalf("factory resolved to %T, want *b20FactoryPrecompile", p)
	}

	// Initialized Asset / Stablecoin route to the right variant.
	if p, ok := resolveB20(statedb, asset); !ok {
		t.Fatal("initialized asset token should resolve")
	} else if _, ok := p.(*b20AssetPrecompile); !ok {
		t.Fatalf("asset resolved to %T, want *b20AssetPrecompile", p)
	}
	if p, ok := resolveB20(statedb, stable); !ok {
		t.Fatal("initialized stablecoin token should resolve")
	} else if _, ok := p.(*b20StablecoinPrecompile); !ok {
		t.Fatalf("stablecoin resolved to %T, want *b20StablecoinPrecompile", p)
	}

	// Unknown variant byte is not routed even when initialized.
	if _, ok := resolveB20(statedb, unknown); ok {
		t.Error("unknown variant should not resolve")
	}
	// Uninitialized address in the B20 space is not a live token.
	if _, ok := resolveB20(statedb, uninit); ok {
		t.Error("uninitialized token address should not resolve")
	}
	// A plain address outside the space is not a B20 precompile.
	if _, ok := resolveB20(statedb, common.HexToAddress("0x1234")); ok {
		t.Error("non-B20 address should not resolve")
	}
}

// TestB20DelegateCallGuard checks that every variant rejects non-direct calls
// before touching any state.
func TestB20DelegateCallGuard(t *testing.T) {
	ctx := &PrecompileContext{DirectCall: false}
	precompiles := []StatefulPrecompiledContract{
		&b20FactoryPrecompile{},
		&b20AssetPrecompile{token: b20Addr(b20VariantAsset, 1)},
		&b20StablecoinPrecompile{token: b20Addr(b20VariantStablecoin, 1)},
	}
	for _, p := range precompiles {
		if _, err := p.RunStateful(ctx, nil); !errors.Is(err, ErrB20DelegateCall) {
			t.Errorf("%T: err = %v, want ErrB20DelegateCall", p, err)
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
