package vm

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/holiman/uint256"
)

// ErrNotCAS20Token is returned for an address outside the CAS20 space or one
// no createCAS20 has initialized.
var ErrNotCAS20Token = errors.New("not an initialized CAS20 token")

// ErrCAS20StringTooLong bounds what one unmetered read will walk. No single
// transaction can store a string this long, so it is only ever met on state
// that did not come from the precompile.
var ErrCAS20StringTooLong = errors.New("CAS20 string exceeds the RPC read bound")

const cas20RPCMaxStringLen = 256 << 10

// CAS20TokenInfo is a token's whole configuration read from one state, the way a
// sequence of eth_calls at one block would read it. It is the cas20_getTokenInfo
// RPC result, off the consensus path. Variant-specific fields are omitted for the
// other variant, and PendingMultiplier is omitted unless a schedule is live at
// the block; Multiplier is the effective value, a matured schedule folded in.
type CAS20TokenInfo struct {
	Address         common.Address      `json:"address"`
	Variant         string              `json:"variant"`
	Name            string              `json:"name"`
	Symbol          string              `json:"symbol"`
	Decimals        hexutil.Uint        `json:"decimals"`
	ContractURI     string              `json:"contractURI"`
	TotalSupply     *hexutil.Big        `json:"totalSupply"`
	SupplyCap       *hexutil.Big        `json:"supplyCap"`
	PausedFeatures  []hexutil.Uint      `json:"pausedFeatures"`
	Policies        CAS20PolicyBindings `json:"policies"`
	DomainSeparator common.Hash         `json:"domainSeparator"`

	// Asset only.
	Multiplier        *hexutil.Big            `json:"multiplier,omitempty"`
	PendingMultiplier *CAS20PendingMultiplier `json:"pendingMultiplier,omitempty"`
	TotalSupplyUI     *hexutil.Big            `json:"totalSupplyUI,omitempty"`

	// Stablecoin only.
	Currency string `json:"currency,omitempty"`
}

type CAS20PolicyBindings struct {
	TransferSender   hexutil.Uint64 `json:"transferSender"`
	TransferReceiver hexutil.Uint64 `json:"transferReceiver"`
	TransferExecutor hexutil.Uint64 `json:"transferExecutor"`
	MintReceiver     hexutil.Uint64 `json:"mintReceiver"`
	SeizeHolder      hexutil.Uint64 `json:"seizeHolder"`
	SeizeReceiver    hexutil.Uint64 `json:"seizeReceiver"`
}

type CAS20PendingMultiplier struct {
	Value       *hexutil.Big   `json:"value"`
	EffectiveAt hexutil.Uint64 `json:"effectiveAt"`
}

var cas20VariantNames = map[byte]string{
	cas20VariantAsset:      "asset",
	cas20VariantStablecoin: "stablecoin",
}

// CAS20TokenInfoAt reads addr's configuration from state as of a block with the
// given time. Unmetered: it is for RPC, never for a precompile frame.
func CAS20TokenInfoAt(state StateDB, chainID *big.Int, addr common.Address, blockTime uint64) (*CAS20TokenInfo, error) {
	variant, ok := cas20VariantNames[addr[10]]
	if !ok || !IsCAS20Address(addr) || state.GetCodeHash(addr) != cas20MarkerCodeHash {
		return nil, ErrNotCAS20Token
	}
	if chainID == nil {
		return nil, errors.New("chain config has no chain id")
	}
	s := newUnmeteredCAS20Storage(state, addr)
	strings := []common.Hash{slotAt(cas20SlotName), slotAt(cas20SlotSymbol), slotAt(cas20SlotContractURI)}
	if addr[10] == cas20VariantStablecoin {
		strings = append(strings, stablecoinSlot(cas20StablecoinSlotCurrency))
	}
	for _, slot := range strings {
		if s.stringChunks(slot) > cas20RPCMaxStringLen/32 {
			return nil, ErrCAS20StringTooLong
		}
	}

	info := &CAS20TokenInfo{
		Address:        addr,
		Variant:        variant,
		TotalSupply:    u256Big(s.totalSupply()),
		SupplyCap:      u256Big(s.supplyCap()),
		PausedFeatures: []hexutil.Uint{},
	}
	info.Name, _ = s.name()
	info.Symbol, _ = s.symbol()
	info.ContractURI, _ = s.contractURI()

	paused := s.paused()
	for f := uint(0); f <= cas20PauseSeize; f++ {
		if new(uint256.Int).Rsh(paused, f).Uint64()&1 == 1 {
			info.PausedFeatures = append(info.PausedFeatures, hexutil.Uint(f))
		}
	}
	sender, receiver, executor := s.transferPolicies()
	holder, seizeTo := s.seizePolicies()
	info.Policies = CAS20PolicyBindings{
		TransferSender:   hexutil.Uint64(sender),
		TransferReceiver: hexutil.Uint64(receiver),
		TransferExecutor: hexutil.Uint64(executor),
		MintReceiver:     hexutil.Uint64(s.mintReceiverPolicy()),
		SeizeHolder:      hexutil.Uint64(holder),
		SeizeReceiver:    hexutil.Uint64(seizeTo),
	}
	id, _ := uint256.FromBig(chainID)
	info.DomainSeparator = cas20DomainSeparator(info.Name, id, addr)

	switch addr[10] {
	case cas20VariantAsset:
		ext := assetExt{s: s}
		info.Decimals = hexutil.Uint(ext.decimals())
		mul := ext.effectiveMultiplier(blockTime)
		info.Multiplier = u256Big(mul)
		if pending, at := ext.pending(); at > blockTime {
			info.PendingMultiplier = &CAS20PendingMultiplier{Value: u256Big(pending), EffectiveAt: hexutil.Uint64(at)}
		}
		// Both factors are bounded by type(uint128).max, so this cannot overflow.
		ui, _ := applyMultiplier(s.totalSupply(), mul)
		info.TotalSupplyUI = u256Big(ui)
	case cas20VariantStablecoin:
		info.Decimals = 6
		info.Currency, _ = stablecoinExt{s: s}.currency()
	}
	return info, nil
}

func u256Big(v *uint256.Int) *hexutil.Big { return (*hexutil.Big)(v.ToBig()) }
