package ethapi

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/internal/ethapi/override"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// The token-level answers are covered where a token can be created, in core/vm;
// this covers the plumbing: block resolution, the fork gate, and the batch shape.
func TestCAS20GetTokenInfoPlumbing(t *testing.T) {
	t.Parallel()
	api := NewBlockChainAPI(newJennerBSCBackend(t))
	ctx := context.Background()
	stranger := common.HexToAddress("0x5714a9e7")

	if _, err := api.GetCAS20TokenInfo(ctx, stranger, nil); !errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("stranger: err = %v, want ErrNotCAS20Token", err)
	}
	if _, err := api.GetCAS20TokenInfo(ctx, vm.CAS20FactoryAddress, nil); !errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("factory: err = %v, want ErrNotCAS20Token", err)
	}

	got, err := api.GetCAS20TokenInfoBatch(ctx, []common.Address{stranger, vm.CAS20FactoryAddress}, nil)
	if err != nil || len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Errorf("batch of non-tokens = %v, %v; want two nulls", got, err)
	}
	if _, err := api.GetCAS20TokenInfoBatch(ctx, make([]common.Address, cas20BatchLimit+1), nil); err == nil {
		t.Error("a batch past the limit was accepted")
	}
	unknown := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1 << 40))
	if _, err := api.GetCAS20TokenInfo(ctx, stranger, &unknown); err == nil || errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("unknown block: err = %v, want a lookup error", err)
	}
}

func TestCAS20GetTokenInfoBeforeJenner(t *testing.T) {
	t.Parallel()
	acc := newTestAccount()
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{acc.addr: {Balance: big.NewInt(params.Ether)}},
	}
	api := NewBlockChainAPI(newTestBackend(t, 1, gspec, ethash.NewFaker(), func(i int, b *core.BlockGen) {}))
	if _, err := api.GetCAS20TokenInfo(context.Background(), common.HexToAddress("0x5714a9e7"), nil); err == nil || errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("before the fork: err = %v, want the not-active error", err)
	}
}

// eth_createAccessList applies the overrides before it builds its EVM, so a code
// override on a CAS20 address has to reach that EVM's precompile set too.
func TestCAS20CreateAccessListWithCodeOverride(t *testing.T) {
	t.Parallel()
	api := NewBlockChainAPI(newJennerBSCBackend(t))
	token := common.HexToAddress("0xca52000000000000000000000000000000000001")
	returns42 := hexutil.Bytes(common.FromHex("602a60005260206000f3"))
	overrides := &override.StateOverride{token: override.OverrideAccount{Code: &returns42}}
	from := common.HexToAddress("0xf00d")
	res, err := api.CreateAccessList(context.Background(), TransactionArgs{From: &from, To: &token}, nil, overrides)
	if err != nil {
		t.Fatalf("eth_createAccessList with a CAS20 code override: %v", err)
	}
	if res.Error != "" {
		t.Errorf("the overridden code did not run: %s", res.Error)
	}
}
