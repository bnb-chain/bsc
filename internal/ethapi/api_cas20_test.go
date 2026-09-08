package ethapi

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// The token-level answers are covered where a token can be created, in core/vm;
// this covers the plumbing: block resolution, the fork gate, and the batch shape.
func TestCAS20GetTokenInfoPlumbing(t *testing.T) {
	t.Parallel()
	api := NewCAS20API(newJennerBSCBackend(t))
	ctx := context.Background()
	stranger := common.HexToAddress("0x5714a9e7")

	if _, err := api.GetTokenInfo(ctx, stranger, nil); !errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("stranger: err = %v, want ErrNotCAS20Token", err)
	}
	if _, err := api.GetTokenInfo(ctx, vm.CAS20FactoryAddress, nil); !errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("factory: err = %v, want ErrNotCAS20Token", err)
	}

	got, err := api.GetTokenInfoBatch(ctx, []common.Address{stranger, vm.CAS20FactoryAddress}, nil)
	if err != nil || len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Errorf("batch of non-tokens = %v, %v; want two nulls", got, err)
	}
	if _, err := api.GetTokenInfoBatch(ctx, make([]common.Address, cas20BatchLimit+1), nil); err == nil {
		t.Error("a batch past the limit was accepted")
	}
	unknown := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1 << 40))
	if _, err := api.GetTokenInfo(ctx, stranger, &unknown); err == nil || errors.Is(err, vm.ErrNotCAS20Token) {
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
	api := NewCAS20API(newTestBackend(t, 1, gspec, ethash.NewFaker(), func(i int, b *core.BlockGen) {}))
	if _, err := api.GetTokenInfo(context.Background(), common.HexToAddress("0x5714a9e7"), nil); err == nil || errors.Is(err, vm.ErrNotCAS20Token) {
		t.Errorf("before the fork: err = %v, want the not-active error", err)
	}
}
