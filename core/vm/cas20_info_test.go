package vm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/holiman/uint256"
)

// The facade must answer exactly what the selectors answer at the same block.
func TestCAS20TokenInfoMatchesTheSelectors(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	now := evm.Context.Time
	future := now + 3600
	call := func(to common.Address, input []byte) []byte {
		t.Helper()
		ret, _, err := evm.Call(creator, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("call %x: %v", input[:4], err)
		}
		return ret
	}
	word := func(to common.Address, input []byte) uint64 {
		return new(uint256.Int).SetBytes(call(to, input)).Uint64()
	}
	big := func(v *hexutil.Big) uint64 { return v.ToInt().Uint64() }

	ret := call(CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x1f0"), creator, [][]byte{
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selGrantRole, roleOperator, addrKey(creator)),
		cas20Call(selGrantRole, rolePause, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Alice), u256hash(1000)),
		cas20CallU8Array(selPause, byte(cas20PauseBurn)),
		cas20Call(selUpdateUIMultiplier, u256hash(2e18), u256hash(future)),
		cas20Call(selUpdatePolicy, scopeTransferReceiver, u256hash(cas20PolicyAlwaysBlock)),
	}))
	token := common.BytesToAddress(ret)

	info, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, token, now)
	if err != nil {
		t.Fatalf("CAS20TokenInfoAt: %v", err)
	}
	if info.Variant != "asset" || info.Address != token {
		t.Errorf("variant %q address %x, want asset %x", info.Variant, info.Address, token)
	}
	if got := call(token, cas20Call(selName)); info.Name != "Test Token" || !containsString(got, info.Name) {
		t.Errorf("name = %q, want Test Token", info.Name)
	}
	if info.Symbol != "TT" || info.ContractURI != "" {
		t.Errorf("symbol %q uri %q, want TT and empty", info.Symbol, info.ContractURI)
	}
	for _, tc := range []struct {
		name string
		sel  [4]byte
		got  uint64
	}{
		{"decimals", selDecimals, uint64(info.Decimals)},
		{"totalSupply", selTotalSupply, big(info.TotalSupply)},
		{"supplyCap", selSupplyCap, big(info.SupplyCap)},
		{"multiplier", selMultiplier, big(info.Multiplier)},
		{"totalSupplyUI", selTotalSupplyUI, big(info.TotalSupplyUI)},
		{"newUIMultiplier", selNewUIMultiplier, big(info.PendingMultiplier.Value)},
		{"effectiveAt", selEffectiveAt, uint64(info.PendingMultiplier.EffectiveAt)},
	} {
		if want := word(token, cas20Call(tc.sel)); tc.got != want {
			t.Errorf("%s = %d, selector %d", tc.name, tc.got, want)
		}
	}
	if info.Decimals != 18 || big(info.TotalSupply) != 1000 || big(info.Multiplier) != 1e18 || big(info.TotalSupplyUI) != 1000 {
		t.Errorf("literals: decimals %d supply %d multiplier %d ui %d", info.Decimals, big(info.TotalSupply), big(info.Multiplier), big(info.TotalSupplyUI))
	}
	if len(info.PausedFeatures) != 1 || info.PausedFeatures[0] != cas20PauseBurn {
		t.Errorf("pausedFeatures = %v, want [BURN]", info.PausedFeatures)
	}
	for _, tc := range []struct {
		scope common.Hash
		got   hexutil.Uint64
	}{
		{scopeTransferSender, info.Policies.TransferSender},
		{scopeTransferReceiver, info.Policies.TransferReceiver},
		{scopeTransferExecutor, info.Policies.TransferExecutor},
		{scopeMintReceiver, info.Policies.MintReceiver},
		{scopeSeizeHolder, info.Policies.SeizeHolder},
		{scopeSeizeReceiver, info.Policies.SeizeReceiver},
	} {
		if want := word(token, cas20Call(selPolicyId, tc.scope)); uint64(tc.got) != want {
			t.Errorf("policy %x = %d, selector %d", tc.scope[:4], tc.got, want)
		}
	}
	if uint64(info.Policies.TransferReceiver) != cas20PolicyAlwaysBlock {
		t.Errorf("transferReceiver = %d, want the bound ALWAYS_BLOCK", info.Policies.TransferReceiver)
	}
	if got := common.BytesToHash(call(token, cas20Call(selDomainSeparator))); got != info.DomainSeparator {
		t.Errorf("domainSeparator = %x, selector %x", info.DomainSeparator, got)
	}
	if info.Currency != "" {
		t.Errorf("an Asset reports currency %q", info.Currency)
	}

	// Past the schedule, with no transaction in between: the pending value is
	// the multiplier and nothing is pending, for the selectors and the facade alike.
	evm.Context.Time = future
	matured, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, token, future)
	if err != nil {
		t.Fatal(err)
	}
	if got := word(token, cas20Call(selMultiplier)); got != big(matured.Multiplier) || got != 2e18 {
		t.Errorf("matured multiplier = %d, selector %d, want 2e18", big(matured.Multiplier), got)
	}
	if got := word(token, cas20Call(selNewUIMultiplier)); got != big(matured.Multiplier) || matured.PendingMultiplier != nil {
		t.Errorf("matured: newUIMultiplier %d pending %+v, want the multiplier and nil", got, matured.PendingMultiplier)
	}
	if got := word(token, cas20Call(selTotalSupplyUI)); got != big(matured.TotalSupplyUI) || got != 2000 {
		t.Errorf("matured totalSupplyUI = %d, selector %d, want 2000", big(matured.TotalSupplyUI), got)
	}
	evm.Context.Time = now

	// Stablecoin: fixed decimals, a currency, no multiplier surface.
	ret = call(CAS20FactoryAddress, encodeCreateCAS20(cas20VariantStablecoin, common.HexToHash("0x1f1"), creator, nil))
	stable, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, common.BytesToAddress(ret), now)
	if err != nil {
		t.Fatal(err)
	}
	if stable.Variant != "stablecoin" || stable.Decimals != 6 || stable.Currency != "USD" {
		t.Errorf("stablecoin: variant %q decimals %d currency %q", stable.Variant, stable.Decimals, stable.Currency)
	}
	if stable.Multiplier != nil || stable.PendingMultiplier != nil || stable.TotalSupplyUI != nil {
		t.Error("a Stablecoin reports a multiplier surface")
	}

	// The JSON shape: hex quantities, an empty list rather than null, and the
	// other variant's fields absent.
	assetJSON, _ := json.Marshal(info)
	for _, want := range []string{`"decimals":"0x12"`, `"totalSupply":"0x3e8"`, `"pausedFeatures":["0x2"]`, `"pendingMultiplier":{"value":"0x1bc16d674ec80000","effectiveAt":"0x`} {
		if !strings.Contains(string(assetJSON), want) {
			t.Errorf("asset JSON lacks %s: %s", want, assetJSON)
		}
	}
	if strings.Contains(string(assetJSON), "currency") {
		t.Errorf("asset JSON carries currency: %s", assetJSON)
	}
	stableJSON, _ := json.Marshal(stable)
	if !strings.Contains(string(stableJSON), `"currency":"USD"`) || !strings.Contains(string(stableJSON), `"pausedFeatures":[]`) {
		t.Errorf("stablecoin JSON: %s", stableJSON)
	}
	for _, absent := range []string{"multiplier", "totalSupplyUI", "pendingMultiplier"} {
		if strings.Contains(string(stableJSON), absent) {
			t.Errorf("stablecoin JSON carries %s: %s", absent, stableJSON)
		}
	}

	// Not tokens: outside the space, the factory, an address never created.
	for _, addr := range []common.Address{creator, CAS20FactoryAddress, cas20Addr(cas20VariantAsset, 0xee)} {
		if _, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, addr, now); !errors.Is(err, ErrNotCAS20Token) {
			t.Errorf("%x: err = %v, want ErrNotCAS20Token", addr, err)
		}
	}

	// A length word no transaction could have paid for is refused, not walked.
	view := newUnmeteredCAS20Storage(statedb, token)
	view.setWord(slotAt(cas20SlotContractURI), uint256.NewInt(2*(cas20RPCMaxStringLen+32)+1).Bytes32())
	if _, err := CAS20TokenInfoAt(statedb, evm.chainConfig.ChainID, token, now); !errors.Is(err, ErrCAS20StringTooLong) {
		t.Errorf("oversized contractURI: err = %v, want ErrCAS20StringTooLong", err)
	}
}

func containsString(abiRet []byte, s string) bool {
	if len(abiRet) < 64 {
		return false
	}
	n := new(uint256.Int).SetBytes(abiRet[32:64]).Uint64()
	return uint64(len(abiRet)) >= 64+n && string(abiRet[64:64+n]) == s
}
