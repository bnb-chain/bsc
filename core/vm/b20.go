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
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// B20 native token family. The factory, the two variants and the registries are
// native precompiles; a token deploys no executable bytecode, only the one-byte
// sentinel. It is identified by its address, routed by the address prefix, and
// isolated by the state stored under that address (BEP-702 §3.3).
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

	// b20VariantMax is the highest ordinal the variant enum defines. A word above
	// it is outside the enum, which is a different failure from a word inside it
	// that no handler claims — see createB20.
	b20VariantMax = b20VariantStablecoin
)

// The three singletons (BEP-702 §3.1). The factory's second byte is 0xBF where
// the token space pins 0xB0, so IsB20Address matches no singleton.
// B20PolicyRegistryAddress is declared in b20_policy.go.
var (
	B20FactoryAddress            = common.HexToAddress("0x20BF000000000000000000000000000000000000")
	B20ActivationRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000001")
)

var (
	// ErrB20DelegateCall is returned when a token precompile is reached via a
	// non-direct call (DELEGATECALL/CALLCODE), where Self could not be trusted
	// as the storage root.
	ErrB20DelegateCall = errors.New("b20: delegate call not allowed")

	// ErrB20StatelessDispatch guards the plain Run path: B20 precompiles must be
	// reached through runPrecompile's stateful route. b20Precompile makes this
	// unreachable; it stays as a backstop.
	ErrB20StatelessDispatch = errors.New("b20: stateful precompile invoked without state")
)

// IsB20Address reports whether addr falls in the reserved B20 token space:
// byte[0:2] == 0x20B0 and byte[2:10] all zero. The variant byte is deliberately
// not inspected, so a future variant's addresses still read as B20, matching the
// spec's isB20 rule.
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

// b20MarkerCodeHash is keccak256(0xEF), the code hash of the factory's sentinel.
// EIP-3541 makes 0xEF-prefixed code undeployable, so nothing an attacker installs
// can hash to it (BEP-702 3.3).
var b20MarkerCodeHash = crypto.Keccak256Hash(b20MarkerCode)

// b20InitializedMetered reports whether a B20 token exists at addr, charged as an
// account access. Exact-hash, not non-empty: foreign code is not a token.
//
// Not the factory's occupancy check — that asks whether the address is free,
// which any code denies. See b20AddressOccupied.
func b20InitializedMetered(ctx *PrecompileContext, addr common.Address) bool {
	ctx.chargeAccountAccess(addr)
	return ctx.StateDB.GetCodeHash(addr) == b20MarkerCodeHash
}

// b20AddressOccupied reports whether a derived address is already taken, by a
// token or by anything else. createB20 must reject on any code, not only on
// the sentinel: overwriting foreign code would destroy it (BEP-702 3.4).
func b20AddressOccupied(ctx *PrecompileContext, addr common.Address) bool {
	ctx.chargeAccountAccess(addr)
	return !hadNoCode(ctx.StateDB, addr)
}

// hadNoCode reports whether addr carries no code at all — the condition under
// which writing code owes the account-creation cost.
func hadNoCode(state StateDB, addr common.Address) bool {
	ch := state.GetCodeHash(addr)
	return ch == (common.Hash{}) || ch == types.EmptyCodeHash
}

// ensureSentinel keeps a registry's storage out of reach of EIP-161 clearing. It
// plants the marker only on an account with no code at all (BEP-702 3.16).
func (ctx *PrecompileContext) ensureSentinel() {
	ctx.chargeAccountAccess(ctx.Self)
	if !hadNoCode(ctx.StateDB, ctx.Self) {
		return
	}
	ctx.chargeCodeWrite(ctx.Self, b20MarkerCode)
	ctx.StateDB.SetCode(ctx.Self, b20MarkerCode, tracing.CodeChangeContractCreation)
}

// b20EnterCall applies the guards every B20 entry point shares: direct calls
// only, nonpayable, and calldata charged once (BEP-702 3.14). The value check
// precedes the charge, as in base-std, so a value-bearing call consumes no gas.
func b20EnterCall(ctx *PrecompileContext, input []byte) error {
	if !ctx.DirectCall {
		return ErrB20DelegateCall
	}
	if ctx.Value != nil && !ctx.Value.IsZero() {
		return revB20("NonPayable()", errSelNonPayable)
	}
	ctx.chargeCalldata(input)
	// Fail here rather than at the exit. RequiredGas is zero, so the interpreter
	// charges nothing before the handler runs, and a caller that cannot even
	// afford the calldata charge would otherwise still get the whole handler's
	// native work — decoding, trie reads, keccak, permit's ecrecover — for the
	// cost of a warm CALL, repeatable in a loop against already-expanded memory.
	// Every later charge is bounded by what this one proves the caller can pay.
	if ctx.OutOfGas() {
		return ErrOutOfGas
	}
	return nil
}

// b20Precompile is both host interfaces at once. They are independent, and only
// PrecompiledContract is enforced by flowing through evm.precompile: runPrecompile
// reaches RunStateful by runtime type assertion, so a precompile missing it would
// build and then answer every call with ErrB20StatelessDispatch. The resolvers
// return this type so the compiler catches that instead.
type b20Precompile interface {
	PrecompiledContract
	StatefulPrecompiledContract
}

// resolveB20Token routes a recognized variant without checking existence, so a
// value-bearing call to an empty B20 address is refused rather than stranded. An
// unrecognized variant is not routed at all (BEP-702 3.3).
func resolveB20Token(addr common.Address) (b20Precompile, bool) {
	switch addr[10] {
	case b20VariantAsset:
		return b20Asset, true
	case b20VariantStablecoin:
		return b20Stablecoin, true
	default:
		return nil, false
	}
}

// resolveB20 decides whether an address is a B20 precompile — a singleton or a
// dynamic token — and returns the bound instance. Fork gating is the caller's.
func resolveB20(addr common.Address) (b20Precompile, bool) {
	switch addr {
	case B20FactoryAddress:
		return b20Factory, true
	case B20PolicyRegistryAddress:
		return b20Policy, true
	case B20ActivationRegistryAddress:
		return b20Activation, true
	}
	if IsB20Address(addr) {
		return resolveB20Token(addr)
	}
	return nil, false
}

var (
	b20Factory    = &b20FactoryPrecompile{}
	b20Policy     = &b20PolicyPrecompile{}
	b20Activation = &b20ActivationPrecompile{}
	b20Asset      = &b20AssetPrecompile{}
	b20Stablecoin = &b20StablecoinPrecompile{}
)

type b20StatefulBase struct{}

func (b20StatefulBase) Run([]byte) ([]byte, error) { return nil, ErrB20StatelessDispatch }

// RequiredGas is zero for every B20 precompile: the cost depends on state not yet
// read — whether a slot is cold, whether a write creates or rewrites — so all
// metering happens inside RunStateful as the work is performed (BEP-702 3.14, see
// b20_gas.go).
func (b20StatefulBase) RequiredGas([]byte) uint64 { return 0 }

type b20FactoryPrecompile struct{ b20StatefulBase }

func (p *b20FactoryPrecompile) Name() string { return "B20Factory" }

func (p *b20FactoryPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	ret, err := runB20Factory(ctx, input)
	return finishB20Metered(ctx, ret, err)
}

// b20AssetPrecompile is the Asset (RWA) variant. Stateless: the token it acts on
// comes from ctx.Self, so one value serves every Asset address.
type b20AssetPrecompile struct{ b20StatefulBase }

func (p *b20AssetPrecompile) Name() string { return "B20Asset" }

func (p *b20AssetPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runB20Token(ctx, input, bindAsset)
}

// bindAsset binds the Asset variant's token and extension. The extension
// intercepts decimals, so the shared token's field is unused here.
func bindAsset(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return assetDispatch(newB20Token(ctx, 0), newAssetExt(ctx), input)
}

// b20StablecoinPrecompile is the Stablecoin variant, stateless for the same reason.
type b20StablecoinPrecompile struct{ b20StatefulBase }

func (p *b20StablecoinPrecompile) Name() string { return "B20Stablecoin" }

func (p *b20StablecoinPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runB20Token(ctx, input, bindStablecoin)
}

// bindStablecoin binds the Stablecoin variant's token and extension. Its decimals
// are fixed at 6 (BEP-702 3.13).
func bindStablecoin(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return stablecoinDispatch(newB20Token(ctx, 6), newStablecoinExt(ctx), input)
}

// runB20Token is the sequence both variants share: entry guards, existence check,
// then the variant's binding behind the metered exit. The order is observable
// through the error a caller gets, so it lives in one place. bind is a top-level
// function value, not a closure, so the split allocates nothing.
func runB20Token(ctx *PrecompileContext, input []byte,
	bind func(*PrecompileContext, []byte) ([]byte, error),
) ([]byte, error) {
	if err := b20EnterCall(ctx, input); err != nil {
		return finishB20(nil, err)
	}
	if !b20InitializedMetered(ctx, ctx.Self) {
		// Metered exit: if the check's own charge exhausted the budget, this is
		// out of gas rather than a revert.
		return finishB20Metered(ctx, nil, ErrExecutionReverted)
	}
	ret, err := bind(ctx, input)
	return finishB20Metered(ctx, ret, err)
}
