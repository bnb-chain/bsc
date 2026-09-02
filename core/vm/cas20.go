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

// CAS20 native token family. The factory, the two variants and the registries are
// native precompiles; a token deploys no executable bytecode, only the one-byte
// sentinel. It is identified by its address, routed by the address prefix, and
// isolated by the state stored under that address (BEP-702 §3.3).
var cas20MarkerPrefix = [2]byte{0xca, 0x50}

const (
	cas20VariantAsset      = 0x00
	cas20VariantStablecoin = 0x01

	// cas20VariantMax is the highest ordinal the variant enum defines. A word above
	// it is outside the enum, which is a different failure from a word inside it
	// that no handler claims — see createCAS20.
	cas20VariantMax = cas20VariantStablecoin
)

// The three singletons (BEP-702 §3.1). The factory's second byte is 0x5F where
// the token space pins 0x50, so IsCAS20Address matches no singleton.
// CAS20PolicyRegistryAddress is declared in cas20_policy.go.
var (
	CAS20FactoryAddress            = common.HexToAddress("0xCA5F000000000000000000000000000000000000")
	CAS20ActivationRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000001")
)

var (
	// ErrCAS20DelegateCall is returned when a token precompile is reached via a
	// non-direct call (DELEGATECALL/CALLCODE), where Self could not be trusted
	// as the storage root.
	ErrCAS20DelegateCall = errors.New("cas20: delegate call not allowed")

	// ErrCAS20StatelessDispatch guards the plain Run path: CAS20 precompiles must be
	// reached through runPrecompile's stateful route. cas20Precompile makes this
	// unreachable; it stays as a backstop.
	ErrCAS20StatelessDispatch = errors.New("cas20: stateful precompile invoked without state")
)

// IsCAS20Address reports whether addr falls in the reserved CAS20 token space:
// byte[0:2] == 0xCA50 and byte[2:10] all zero. The variant byte is deliberately
// not inspected, so a future variant's addresses still read as CAS20, matching the
// spec's isCAS20 rule.
func IsCAS20Address(addr common.Address) bool {
	if addr[0] != cas20MarkerPrefix[0] || addr[1] != cas20MarkerPrefix[1] {
		return false
	}
	for i := 2; i < 10; i++ {
		if addr[i] != 0 {
			return false
		}
	}
	return true
}

// cas20MarkerCodeHash is keccak256(0xEF), the code hash of the factory's sentinel.
// EIP-3541 makes 0xEF-prefixed code undeployable, so nothing an attacker installs
// can hash to it (BEP-702 3.3).
var cas20MarkerCodeHash = crypto.Keccak256Hash(CAS20MarkerCode)

// cas20InitializedMetered reports whether a CAS20 token exists at addr, charged as an
// account access. Exact-hash, not non-empty: foreign code is not a token.
func cas20InitializedMetered(ctx *PrecompileContext, addr common.Address) bool {
	if !ctx.chargeAccountAccess(addr) {
		return false
	}
	return ctx.StateDB.GetCodeHash(addr) == cas20MarkerCodeHash
}

// cas20AddressOccupied reports whether a derived address is already taken, by a
// token or by anything else. createCAS20 must reject on any code, not only on
// the sentinel: overwriting foreign code would destroy it (BEP-702 3.4).
func cas20AddressOccupied(ctx *PrecompileContext, addr common.Address) bool {
	if !ctx.chargeAccountAccess(addr) {
		// Reported occupied: the caller refuses on true, which is the safe answer
		// when the frame can no longer pay to find out.
		return true
	}
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
	if !ctx.chargeAccountAccess(ctx.Self) {
		return
	}
	if !hadNoCode(ctx.StateDB, ctx.Self) {
		return
	}
	if !ctx.chargeCodeWrite(ctx.Self, CAS20MarkerCode) {
		return
	}
	ctx.StateDB.SetCode(ctx.Self, CAS20MarkerCode, tracing.CodeChangeContractCreation)
}

// cas20EnterCall applies the guards every CAS20 entry point shares: direct calls
// only, nonpayable, and calldata charged once (BEP-702 3.14). The value check
// precedes the charge, so a value-bearing call consumes no gas.
func cas20EnterCall(ctx *PrecompileContext, input []byte) error {
	if !ctx.DirectCall {
		return ErrCAS20DelegateCall
	}
	if ctx.Value != nil && !ctx.Value.IsZero() {
		return revCAS20("NonPayable()", errSelNonPayable)
	}
	// Fail here rather than at the exit. RequiredGas is zero, so the interpreter
	// charges nothing before the handler runs, and a caller that cannot even
	// afford the calldata charge would otherwise still get the whole handler's
	// native work — decoding, trie reads, keccak, permit's ecrecover — for the
	// cost of a warm CALL, repeatable in a loop against already-expanded memory.
	// Every later charge is bounded by what this one proves the caller can pay.
	if !ctx.chargeCalldata(input) {
		return ErrOutOfGas
	}
	return nil
}

// cas20Precompile is both host interfaces at once. They are independent, and only
// PrecompiledContract is enforced by flowing through evm.precompile: runPrecompile
// reaches RunStateful by runtime type assertion, so a precompile missing it would
// build and then answer every call with ErrCAS20StatelessDispatch. The resolvers
// return this type so the compiler catches that instead.
type cas20Precompile interface {
	PrecompiledContract
	StatefulPrecompiledContract
}

// resolveCAS20Token routes a recognized variant without checking existence, so a
// value-bearing call to an empty CAS20 address is refused rather than stranded. An
// unrecognized variant is not routed at all (BEP-702 3.3).
func resolveCAS20Token(addr common.Address) (cas20Precompile, bool) {
	v, ok := cas20Variants[addr[10]]
	return v.precompile, ok
}

// cas20Variants is the variant table. One entry per ordinal, so the handler an
// address routes to and the feature that gates it cannot disagree — they used to
// be three separate switches with a test holding two of them together.
var cas20Variants = map[byte]struct {
	precompile cas20Precompile
	feature    common.Hash
}{
	cas20VariantAsset:      {cas20Asset, featureCAS20Asset},
	cas20VariantStablecoin: {cas20Stablecoin, featureCAS20Stablecoin},
}

// resolveCAS20 decides whether an address is a CAS20 precompile — a singleton or a
// dynamic token — and returns the bound instance. Fork gating is the caller's.
func resolveCAS20(addr common.Address) (cas20Precompile, bool) {
	switch addr {
	case CAS20FactoryAddress:
		return cas20Factory, true
	case CAS20PolicyRegistryAddress:
		return cas20Policy, true
	case CAS20ActivationRegistryAddress:
		return cas20Activation, true
	}
	if IsCAS20Address(addr) {
		return resolveCAS20Token(addr)
	}
	return nil, false
}

var (
	cas20Factory    = &cas20FactoryPrecompile{}
	cas20Policy     = &cas20PolicyPrecompile{}
	cas20Activation = &cas20ActivationPrecompile{}
	cas20Asset      = &cas20AssetPrecompile{}
	cas20Stablecoin = &cas20StablecoinPrecompile{}
)

type cas20StatefulBase struct{}

func (cas20StatefulBase) Run([]byte) ([]byte, error) { return nil, ErrCAS20StatelessDispatch }

// RequiredGas is zero for every CAS20 precompile: the cost depends on state not yet
// read — whether a slot is cold, whether a write creates or rewrites — so all
// metering happens inside RunStateful as the work is performed (BEP-702 3.14, see
// cas20_gas.go).
func (cas20StatefulBase) RequiredGas([]byte) uint64 { return 0 }

type cas20FactoryPrecompile struct{ cas20StatefulBase }

func (p *cas20FactoryPrecompile) Name() string { return "CAS20Factory" }

func (p *cas20FactoryPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	ret, err := runCAS20Factory(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}

// cas20AssetPrecompile is the Asset (RWA) variant. Stateless: the token it acts on
// comes from ctx.Self, so one value serves every Asset address.
type cas20AssetPrecompile struct{ cas20StatefulBase }

func (p *cas20AssetPrecompile) Name() string { return "CAS20Asset" }

func (p *cas20AssetPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runCAS20Token(ctx, input, bindAsset)
}

// bindAsset binds the Asset variant's token and extension. The extension
// intercepts decimals, so the shared token's field is unused here.
func bindAsset(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return assetDispatch(newCAS20Token(ctx, 0), newAssetExt(ctx), input)
}

// cas20StablecoinPrecompile is the Stablecoin variant, stateless for the same reason.
type cas20StablecoinPrecompile struct{ cas20StatefulBase }

func (p *cas20StablecoinPrecompile) Name() string { return "CAS20Stablecoin" }

func (p *cas20StablecoinPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runCAS20Token(ctx, input, bindStablecoin)
}

// bindStablecoin binds the Stablecoin variant's token and extension. Its decimals
// are fixed at 6 (BEP-702 3.13).
func bindStablecoin(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return stablecoinDispatch(newCAS20Token(ctx, 6), newStablecoinExt(ctx), input)
}

// runCAS20Token is the sequence both variants share: entry guards, existence check,
// then the variant's binding behind the metered exit. The order is observable
// through the error a caller gets, so it lives in one place. bind is a top-level
// function value, not a closure, so the split allocates nothing.
func runCAS20Token(ctx *PrecompileContext, input []byte,
	bind func(*PrecompileContext, []byte) ([]byte, error),
) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	if !cas20InitializedMetered(ctx, ctx.Self) {
		// Metered exit: if the check's own charge exhausted the budget, this is
		// out of gas rather than a revert.
		return finishCAS20Metered(ctx, nil, ErrExecutionReverted)
	}
	ret, err := bind(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}
