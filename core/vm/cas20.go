package vm

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// cas20MarkerPrefix opens every CAS20 token address.
var cas20MarkerPrefix = [2]byte{0xca, 0x52}

const (
	cas20VariantAsset      = 0x00
	cas20VariantStablecoin = 0x01
	cas20VariantMax        = cas20VariantStablecoin
)

var (
	CAS20FactoryAddress            = common.HexToAddress("0xCA5F000000000000000000000000000000000000")
	CAS20ActivationRegistryAddress = common.HexToAddress("0x7020000000000000000000000000000000000001")
)

var (
	ErrCAS20DelegateCall      = errors.New("cas20: delegate call not allowed")
	ErrCAS20StatelessDispatch = errors.New("cas20: stateful precompile invoked without state")
)

// IsCAS20Address: byte[0:2] == 0xCA52 and byte[2:10] all zero.
// IsCAS20Routed reports whether dispatch resolves addr to native CAS20 code once
// Jenner is active: the token space and the three singletons.
func IsCAS20Routed(addr common.Address) bool {
	return IsCAS20Address(addr) || addr == CAS20FactoryAddress ||
		addr == CAS20PolicyRegistryAddress || addr == CAS20ActivationRegistryAddress
}

// DisabledPrecompile, placed in an EVM's precompile map, makes dispatch treat the
// address as an ordinary account. A state override that replaces a CAS20
// account's code puts it there, since the address would otherwise be routed to
// native code whatever the override said.
var DisabledPrecompile PrecompiledContract = disabledPrecompile{}

type disabledPrecompile struct{}

func (disabledPrecompile) RequiredGas([]byte) uint64  { return 0 }
func (disabledPrecompile) Run([]byte) ([]byte, error) { return nil, ErrExecutionReverted }
func (disabledPrecompile) Name() string               { return "disabled" }

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

var cas20MarkerCodeHash = crypto.Keccak256Hash(CAS20MarkerCode)

// Exact-hash, not non-empty: foreign code is not a token.
func cas20InitializedMetered(ctx *PrecompileContext, addr common.Address) bool {
	if !ctx.chargeAccountAccess(addr) {
		return false
	}
	return ctx.StateDB.GetCodeHash(addr) == cas20MarkerCodeHash
}

// Any code counts, not just the sentinel: overwriting foreign code would destroy it.
func cas20AddressOccupied(ctx *PrecompileContext, addr common.Address) bool {
	if !ctx.chargeAccountAccess(addr) {
		// True is the safe answer when the frame can no longer pay to find out.
		return true
	}
	return !hadNoCode(ctx.StateDB, addr)
}

// No code at all is the condition under which writing code owes the
// account-creation cost.
func hadNoCode(state StateDB, addr common.Address) bool {
	ch := state.GetCodeHash(addr)
	return ch == (common.Hash{}) || ch == types.EmptyCodeHash
}

// cas20EnterCall is the prologue every CAS20 entry point runs.
func cas20EnterCall(ctx *PrecompileContext, input []byte) error {
	if !ctx.DirectCall {
		return ErrCAS20DelegateCall
	}
	if ctx.Value != nil && !ctx.Value.IsZero() {
		return revCAS20("NonPayable()", errSelNonPayable)
	}
	if !ctx.chargeCalldata(input) {
		return ErrOutOfGas
	}
	return nil
}

type cas20Precompile interface {
	PrecompiledContract
	StatefulPrecompiledContract
}

func resolveCAS20Token(addr common.Address) (cas20Precompile, bool) {
	v, ok := cas20Variants[addr[10]]
	return v.precompile, ok
}

// One entry per ordinal, so routing and feature gating cannot disagree.
var cas20Variants = map[byte]struct {
	precompile cas20Precompile
	feature    common.Hash
}{
	cas20VariantAsset:      {cas20Asset, featureCAS20Asset},
	cas20VariantStablecoin: {cas20Stablecoin, featureCAS20Stablecoin},
}

// resolveCAS20 leaves fork gating to the caller.
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

// RequiredGas : so all metering happens inside RunStateful.
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

// Stateless: the token is ctx.Self, so one value serves every Asset address.
type cas20AssetPrecompile struct{ cas20StatefulBase }

func (p *cas20AssetPrecompile) Name() string { return "CAS20Asset" }

func (p *cas20AssetPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runCAS20Token(ctx, input, bindAsset)
}

func bindAsset(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return assetDispatch(newCAS20Token(ctx, 0), newAssetExt(ctx), input)
}

type cas20StablecoinPrecompile struct{ cas20StatefulBase }

func (p *cas20StablecoinPrecompile) Name() string { return "CAS20Stablecoin" }

func (p *cas20StablecoinPrecompile) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return runCAS20Token(ctx, input, bindStablecoin)
}

func bindStablecoin(ctx *PrecompileContext, input []byte) ([]byte, error) {
	return stablecoinDispatch(newCAS20Token(ctx, 6), newStablecoinExt(ctx), input)
}

func runCAS20Token(ctx *PrecompileContext, input []byte,
	bind func(*PrecompileContext, []byte) ([]byte, error),
) ([]byte, error) {
	if err := cas20EnterCall(ctx, input); err != nil {
		return finishCAS20(nil, err)
	}
	if !cas20InitializedMetered(ctx, ctx.Self) {
		return finishCAS20Metered(ctx, nil, ErrExecutionReverted)
	}
	ret, err := bind(ctx, input)
	return finishCAS20Metered(ctx, ret, err)
}
