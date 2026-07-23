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
)

// B20 native token family (Base B-20 equivalent). Tokens deploy no bytecode:
// the factory, the two variants and the registries are native precompiles.
// A token is identified by its address, routed to variant code by its address
// prefix, and isolated by the state stored under that address. See the B20
// spec, section "B-20 地址编码".
//
// Address layout (20 bytes):
//
//	byte[0]        0xb2               magic prefix
//	byte[1..9]     0x00 (9 bytes)     namespace padding
//	byte[10]       variant            0x00 = Asset, 0x01 = Stablecoin
//	byte[11..19]   keccak256(creator,salt)[:9]  identity fingerprint
const (
	b20MagicPrefix       = 0xb2
	b20VariantAsset      = 0x00
	b20VariantStablecoin = 0x01
)

// B20FactoryAddress is the singleton token-creation entry point. It sits just
// outside the 0xb2·00…00 token space (byte[1] = 0x0f), so it never collides
// with a token address and is not matched by IsB20Address.
var B20FactoryAddress = common.HexToAddress("0xB20f000000000000000000000000000000000000")

var (
	// ErrB20AddressReserved is returned when CREATE/CREATE2 targets the reserved
	// B20 address space once the fork is active. Reserving the prefix at the
	// protocol level is what stops anyone from squatting or forging a token
	// address (cf. Tempo TIP-1047).
	ErrB20AddressReserved = errors.New("b20: reserved address space")

	// ErrB20DelegateCall is returned when a token precompile is reached via a
	// non-direct call (DELEGATECALL/CALLCODE), where Self could not be trusted
	// as the storage root.
	ErrB20DelegateCall = errors.New("b20: delegate call not allowed")

	// ErrB20NotImplemented is a placeholder for the not-yet-ported dispatch/logic.
	ErrB20NotImplemented = errors.New("b20: not implemented")

	// ErrB20StatelessDispatch guards the plain Run path: B20 precompiles are
	// stateful and must be reached through runPrecompile's stateful route. This
	// is a defensive backstop and should never fire in practice.
	ErrB20StatelessDispatch = errors.New("b20: stateful precompile invoked without state")
)

// IsB20Address reports whether addr falls in the reserved B20 token space:
// byte[0] == 0xb2 and byte[1..9] all zero. It intentionally does not inspect
// the variant byte, so addresses of future variants are still recognised as
// B20 (matching the spec's isB20 rule).
func IsB20Address(addr common.Address) bool {
	if addr[0] != b20MagicPrefix {
		return false
	}
	for i := 1; i < 10; i++ {
		if addr[i] != 0 {
			return false
		}
	}
	return true
}

// b20Initialized reports whether a token has been created at addr. The factory
// writes a marker code hash on creation; an address in the B20 space with no
// marker is not a live token and does not resolve as a precompile.
func b20Initialized(state StateDB, addr common.Address) bool {
	ch := state.GetCodeHash(addr)
	return ch != (common.Hash{}) && ch != types.EmptyCodeHash
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

// b20StatefulBase provides the defensive Run backstop shared by every B20
// precompile so they satisfy PrecompiledContract; real execution always goes
// through RunStateful.
type b20StatefulBase struct{}

func (b20StatefulBase) Run([]byte) ([]byte, error) { return nil, ErrB20StatelessDispatch }

// b20FactoryPrecompile is the singleton createB20 entry point.
type b20FactoryPrecompile struct{ b20StatefulBase }

func (p *b20FactoryPrecompile) Name() string                    { return "B20Factory" }
func (p *b20FactoryPrecompile) RequiredGas(input []byte) uint64 { return 0 } // TODO: gas schedule

func (p *b20FactoryPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if !ctx.DirectCall {
		return nil, ErrB20DelegateCall
	}
	// TODO: decode createB20 / getB20Address / isB20 / isB20Initialized;
	// derive address, check ActivationRegistry, write initial storage + marker,
	// run initCalls in the privileged bootstrap window, emit B20Created.
	return nil, ErrB20NotImplemented
}

// b20AssetPrecompile is the Asset (RWA) variant bound to a token address.
type b20AssetPrecompile struct {
	b20StatefulBase
	token common.Address
}

func (p *b20AssetPrecompile) Name() string                    { return "B20Asset" }
func (p *b20AssetPrecompile) RequiredGas(input []byte) uint64 { return 0 } // TODO: gas schedule

func (p *b20AssetPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if !ctx.DirectCall {
		return nil, ErrB20DelegateCall
	}
	// TODO: Asset decimals live in the extension storage (namespace not yet
	// ported); 18 is a placeholder until that layer lands.
	// TODO: Asset extension selectors (multiplier/announce/batchMint/extraMetadata)
	// before falling back to the shared IB20 dispatch.
	ret, err := newB20Token(ctx, 18).dispatch(input)
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return ret, err
}

// b20StablecoinPrecompile is the Stablecoin variant bound to a token address.
type b20StablecoinPrecompile struct {
	b20StatefulBase
	token common.Address
}

func (p *b20StablecoinPrecompile) Name() string                    { return "B20Stablecoin" }
func (p *b20StablecoinPrecompile) RequiredGas(input []byte) uint64 { return 0 } // TODO: gas schedule

func (p *b20StablecoinPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if !ctx.DirectCall {
		return nil, ErrB20DelegateCall
	}
	// Stablecoin decimals are fixed at 6.
	// TODO: Stablecoin currency() selector before the shared IB20 dispatch.
	ret, err := newB20Token(ctx, 6).dispatch(input)
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return ret, err
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
