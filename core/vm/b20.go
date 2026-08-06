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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// B20 native token family. The factory, the two variants and the registries are
// native precompiles; a token deploys no *executable* bytecode, only the
// one-byte sentinel below. A token is identified by its address, routed to
// variant code by its address prefix, and isolated by the state stored under
// that address. See BEP-702 §3.3.
//
// Address layout (20 bytes):
//
//	byte[0:2]      0x20B0             marker
//	byte[2:10]     0x00 (8 bytes)     namespace padding
//	byte[10]       variant            0x00 = Asset, 0x01 = Stablecoin
//	byte[11:20]    keccak256(creator,salt)[:9]  identity fingerprint
var b20MarkerPrefix = [2]byte{0x20, 0xb0}

const (
	b20VariantAsset      = 0x00
	b20VariantStablecoin = 0x01
)

// The three singletons (BEP-702 §3.1). The factory shares the 0x20B nibble
// prefix with the token space and is separated from it by the second byte,
// which the token space pins to 0xB0 — so no singleton is matched by
// IsB20Address or can collide with a token.
// B20PolicyRegistryAddress is declared in contracts_b20_policy.go.
var (
	B20FactoryAddress            = common.HexToAddress("0x20BF000000000000000000000000000000000000")
	B20ActivationRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000002")
)

var (
	// ErrB20DelegateCall is returned when a token precompile is reached via a
	// non-direct call (DELEGATECALL/CALLCODE), where Self could not be trusted
	// as the storage root.
	ErrB20DelegateCall = errors.New("b20: delegate call not allowed")

	// ErrB20StatelessDispatch guards the plain Run path: B20 precompiles are
	// stateful and must be reached through runPrecompile's stateful route. This
	// is a defensive backstop and should never fire in practice.
	ErrB20StatelessDispatch = errors.New("b20: stateful precompile invoked without state")
)

// IsB20Address reports whether addr falls in the reserved B20 token space:
// byte[0:2] == 0x20B0 and byte[2:10] all zero. It intentionally does not inspect
// the variant byte, so addresses of future variants are still recognised as
// B20 (matching the spec's isB20 rule).
func IsB20Address(addr common.Address) bool {
	if addr[0] != b20MarkerPrefix[0] || addr[1] != b20MarkerPrefix[1] {
		return false
	}
	for i := 2; i < 10; i++ {
		if addr[i] != 0 {
			return false
		}
	}
	return true
}

// b20MarkerCodeHash is keccak256(0xEF): the code hash the factory's sentinel
// produces. Existence is an exact comparison against it, not a non-empty test
// (BEP-702 section 3.3) — EIP-3541 makes 0xEF-prefixed code undeployable, so
// nothing an attacker installs can hash to it.
var b20MarkerCodeHash = crypto.Keccak256Hash(b20MarkerCode)

// b20Initialized reports whether a token has been created at addr: true exactly
// when the account's code hash equals the sentinel's. Unmetered; used by
// dispatch resolution, which runs before a frame's gas context exists.
func b20Initialized(state StateDB, addr common.Address) bool {
	return state.GetCodeHash(addr) == b20MarkerCodeHash
}

// b20InitializedMetered is b20Initialized charged as an account access, for
// the in-frame paths (isB20Initialized, createB20's occupancy check).
func b20InitializedMetered(ctx *PrecompileContext, addr common.Address) bool {
	ctx.chargeAccountAccess(addr)
	return b20Initialized(ctx.StateDB, addr)
}

// hadNoCode reports whether addr carries no code at all — the condition under
// which writing code owes the account-creation cost.
func hadNoCode(state StateDB, addr common.Address) bool {
	ch := state.GetCodeHash(addr)
	return ch == (common.Hash{}) || ch == types.EmptyCodeHash
}

// b20EnterCall applies the guards shared by every B20 entry point: only direct
// calls (CALL/STATICCALL) are dispatched, every entry point is nonpayable, and
// the input is charged once (BEP-702 section 3.14). The value check precedes
// the charge so a value-bearing call is refused before it can consume gas,
// matching the order base-std's dispatch uses.
func b20EnterCall(ctx *PrecompileContext, input []byte) error {
	if !ctx.DirectCall {
		return ErrB20DelegateCall
	}
	if ctx.Value != nil && !ctx.Value.IsZero() {
		return revB20("NonPayable()", errSelNonPayable)
	}
	ctx.chargeCalldata(input)
	return nil
}

// resolveB20Token synthesizes the variant precompile bound to a token address.
// Same variant code serves every token of that variant; the bound address is
// the storage root. Returns false for uninitialized addresses and unknown
// variant bytes (the latter are simply not routed).
func resolveB20Token(state StateDB, addr common.Address) (PrecompiledContract, bool) {
	if !b20Initialized(state, addr) {
		return nil, false
	}
	switch addr[10] {
	case b20VariantAsset:
		return &b20AssetPrecompile{token: addr}, true
	case b20VariantStablecoin:
		return &b20StablecoinPrecompile{token: addr}, true
	default:
		return nil, false
	}
}

// resolveB20 is the "BerylLookup" equivalent: given an address, decide whether
// it is a B20 precompile (fixed factory or a dynamic token) and return the
// bound instance. Fork gating is the caller's responsibility.
func resolveB20(state StateDB, addr common.Address) (PrecompiledContract, bool) {
	if addr == B20FactoryAddress {
		return &b20FactoryPrecompile{}, true
	}
	if addr == B20PolicyRegistryAddress {
		return &b20PolicyPrecompile{}, true
	}
	if IsB20Address(addr) {
		return resolveB20Token(state, addr)
	}
	return nil, false
}

// --- skeleton precompiles ---------------------------------------------------
//
// These implement the StatefulPrecompiledContract host contract and the shared
// guards (direct-call, read-only). The ABI dispatch and the IB20 trait logic
// (transfer/approve/mint/burn/roles/pause/permit/memo, plus variant
// extensions) are ported on top of these in the P1 business layer.

// Every B20 precompile reports RequiredGas zero. A stateful precompile cannot
// be priced up front — the cost depends on state it has not read when the call
// begins, such as whether a slot is cold or whether a write creates or rewrites
// — so all metering happens inside RunStateful as the work is performed
// (BEP-702 section 3.14, see b20_gas.go).

// b20StatefulBase provides the defensive Run backstop shared by every B20
// precompile so they satisfy PrecompiledContract; real execution always goes
// through RunStateful.
type b20StatefulBase struct{}

func (b20StatefulBase) Run([]byte) ([]byte, error) { return nil, ErrB20StatelessDispatch }

// b20FactoryPrecompile is the singleton createB20 entry point.
type b20FactoryPrecompile struct{ b20StatefulBase }

func (p *b20FactoryPrecompile) Name() string                    { return "B20Factory" }
func (p *b20FactoryPrecompile) RequiredGas(input []byte) uint64 { return 0 } // priced inside RunStateful

func (p *b20FactoryPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	ret, err := runB20Factory(ctx, input)
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return finishB20(ret, err)
}

// b20AssetPrecompile is the Asset (RWA) variant bound to a token address.
type b20AssetPrecompile struct {
	b20StatefulBase
	token common.Address
}

func (p *b20AssetPrecompile) Name() string                    { return "B20Asset" }
func (p *b20AssetPrecompile) RequiredGas(input []byte) uint64 { return 0 } // priced inside RunStateful

func (p *b20AssetPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	// Decimals is intercepted by the Asset extension (read from extension
	// storage), so the shared token's decimals field is unused here.
	ret, err := assetDispatch(newB20Token(ctx, 0), newAssetExt(ctx), input)
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return finishB20(ret, err)
}

// b20StablecoinPrecompile is the Stablecoin variant bound to a token address.
type b20StablecoinPrecompile struct {
	b20StatefulBase
	token common.Address
}

func (p *b20StablecoinPrecompile) Name() string                    { return "B20Stablecoin" }
func (p *b20StablecoinPrecompile) RequiredGas(input []byte) uint64 { return 0 } // priced inside RunStateful

func (p *b20StablecoinPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	// Stablecoin decimals are fixed at 6.
	// TODO: Stablecoin currency() selector before the shared IB20 dispatch.
	ret, err := newB20Token(ctx, 6).dispatch(input)
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return finishB20(ret, err)
}

// compile-time checks that the skeletons satisfy both the plain precompile
// contract (so they flow through evm.precompile) and the stateful host contract.
var (
	_ PrecompiledContract         = (*b20FactoryPrecompile)(nil)
	_ PrecompiledContract         = (*b20AssetPrecompile)(nil)
	_ PrecompiledContract         = (*b20StablecoinPrecompile)(nil)
	_ StatefulPrecompiledContract = (*b20FactoryPrecompile)(nil)
	_ StatefulPrecompiledContract = (*b20AssetPrecompile)(nil)
	_ StatefulPrecompiledContract = (*b20StablecoinPrecompile)(nil)
)
