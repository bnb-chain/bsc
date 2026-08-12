package miner

import (
	"math/big"
	"runtime"

	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// Lane resolution is the only makeEnv state read that can fail; it must happen before StartPrefetcher.
func TestMakeEnvLeavesNoPrefetcherWhenTheLaneFails(t *testing.T) {
	w, _, parent, header, _ := laneMinerChain(t, true)

	// One leaked prefetcher per failure would show up well above this threshold.
	const iterations = 20
	before := runtime.NumGoroutine()
	for i := 0; i < iterations; i++ {
		env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
		require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
		require.Nil(t, env)
	}
	runtime.Gosched()
	require.Less(t, runtime.NumGoroutine()-before, iterations/2,
		"makeEnv leaked prefetcher goroutines on the lane error path")
}

// Failed bid-path transactions must not book lane gas even if buyGas already ran.
func TestBidCommitTransactionBooksNothingForAFailedTransaction(t *testing.T) {
	w, config, parent, header, key := laneMinerChain(t, false)

	env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
	require.NoError(t, err)
	require.True(t, env.lane.On(), "the lane must bind, or the assertion below is vacuous")

	// 20,000 gas buys successfully but fails intrinsic gas.
	tx, err := types.SignTx(types.NewTransaction(0, common.Address{0xaa}, big.NewInt(1), 20_000, common.Big0, nil),
		types.LatestSigner(config), key)
	require.NoError(t, err)
	require.Equal(t, paymentlane.ClassPayment, env.lane.Classify(tx), "the fixture must be a payment transaction")

	r := &BidRuntime{env: env}
	require.ErrorIs(t, r.commitTransaction(w.chain, config, tx, false), core.ErrIntrinsicGas)
	require.NotZero(t, env.gasPool.Used(), "buyGas must have drawn from the pool, or the test proves nothing")
	require.Zero(t, env.lane.Budget.PaymentUsed, "a failed transaction must not book lane gas")
}
