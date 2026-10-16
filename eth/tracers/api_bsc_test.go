package tracers

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/internal/ethapi/override"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

func TestTraceCallBSCBlockOverride(t *testing.T) {
	account := newAccounts(1)[0]
	config := *params.ParliaTestChainConfig
	config.ShanghaiTime = nil
	config.KeplerTime = nil
	config.FeynmanTime = nil
	config.FeynmanFixTime = nil
	config.CancunTime = nil
	config.BlobScheduleConfig = nil
	backend := newTestBackend(t, 1, &core.Genesis{
		Config: &config,
		Alloc:  types.GenesisAlloc{account.addr: {Balance: big.NewInt(params.Ether)}},
	}, func(i int, b *core.BlockGen) {})
	defer backend.teardown()

	bad := common.BigToHash(big.NewInt(1000))
	_, err := NewAPI(backend).TraceCall(context.Background(), ethapi.TransactionArgs{
		From: &account.addr,
		To:   &account.addr,
	}, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), &TraceCallConfig{
		BlockOverrides: &override.BlockOverrides{PrevRandao: &bad},
	})
	if err == nil {
		t.Fatal("BSC TraceCall accepted an invalid millisecond remainder")
	}
	if !strings.Contains(err.Error(), "prevRandao") {
		t.Fatalf("unexpected block override error: %v", err)
	}
}
