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

// TestMakeEnvLeavesNoPrefetcherWhenTheLaneFails pins the ordering that keeps makeEnv's error
// paths leak-free: all of them must precede StartPrefetcher, because a nil environment has no
// discard() to stop the prefetcher goroutine that would otherwise be running. Lane resolution is
// the only step in makeEnv that reads state and can fail, and prepareWork calls makeEnv once per
// bid, so the leak is unbounded.
func TestMakeEnvLeavesNoPrefetcherWhenTheLaneFails(t *testing.T) {
	w, _, parent, header, _ := laneMinerChain(t, true)

	// One failed call leaks exactly one subfetcher goroutine, so count them: a threshold below
	// the iteration count cannot be met by an implementation that starts the prefetcher first.
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

// TestBidCommitTransactionBooksNothingForAFailedTransaction pins the bid path's accounting
// against the one input that separates it from the other three sites: core.ApplyTransaction can
// fail AFTER buyGas has drawn tx.Gas() from the pool, and the bid path - unlike
// worker.applyTransaction - does not revert the pool. Booking before the error check would credit
// the lane with the whole declared gas of a transaction that never ran.
func TestBidCommitTransactionBooksNothingForAFailedTransaction(t *testing.T) {
	w, config, parent, header, key := laneMinerChain(t, false)

	env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
	require.NoError(t, err)
	require.True(t, env.lane.On(), "the lane must bind, or the assertion below is vacuous")

	// A bare transfer to a codeless account is payment class, and 20,000 gas is below the
	// intrinsic floor, so it is bought and then rejected.
	tx, err := types.SignTx(types.NewTransaction(0, common.Address{0xaa}, big.NewInt(1), 20_000, common.Big0, nil),
		types.LatestSigner(config), key)
	require.NoError(t, err)
	class, err := env.lane.Classify(tx)
	require.NoError(t, err)
	require.Equal(t, paymentlane.ClassPayment, class, "the fixture must be a payment transaction")

	r := &BidRuntime{env: env}
	require.ErrorIs(t, r.commitTransaction(w.chain, config, tx, false), core.ErrIntrinsicGas)
	require.NotZero(t, env.gasPool.Used(), "buyGas must have drawn from the pool, or the test proves nothing")
	require.Zero(t, env.lane.Budget.PaymentUsed, "a failed transaction must not book lane gas")
}
