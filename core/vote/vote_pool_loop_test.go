package vote

import (
	"container/heap"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

const testDeadline = 10 * time.Second

func newTestChain(t *testing.T, autoStop bool) (*core.BlockChain, *VotePool) {
	t.Helper()
	genesis := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
	}
	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), genesis, ethash.NewFullFaker(), nil)
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	pool := NewVotePool(chain, &mockPOSA{})
	if autoStop {
		t.Cleanup(func() {
			pool.Stop()
			chain.Stop()
		})
	}
	return chain, pool
}

func insertBlocks(chain *core.BlockChain, n int) error {
	parent := chain.GetBlockByHash(chain.CurrentBlock().Hash())
	blocks, _ := core.GenerateChain(params.TestChainConfig, parent, ethash.NewFaker(), chain.TrieDB().Disk(), n, nil)
	_, err := chain.InsertChain(blocks)
	return err
}

// insertBlocksWithin fails the test if the import does not finish in time, which is
// what happens when the pool's head channel back-pressures writeBlockAndSetHead.
func insertBlocksWithin(t *testing.T, chain *core.BlockChain, n int, d time.Duration) {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- insertBlocks(chain, n) }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("insert chain: %v", err)
		}
	case <-time.After(d):
		t.Fatalf("block import of %d blocks blocked on the vote pool", n)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testDeadline)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitLoopsDone(t *testing.T, pool *VotePool) {
	t.Helper()
	done := make(chan struct{})
	go func() { pool.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(testDeadline):
		t.Fatal("vote pool loops did not exit")
	}
}

// injectFutureVote places a vote directly into the future set, the way the pool
// would after basicVerify, without needing a signed envelope.
func injectFutureVote(pool *VotePool, number uint64, hash common.Hash) *types.VoteEnvelope {
	vote := &types.VoteEnvelope{Data: &types.VoteData{TargetNumber: number, TargetHash: hash}}
	pool.mu.Lock()
	pool.futureVotes[hash] = &VoteBox{blockNumber: number, blockHash: hash, voteMessages: []*types.VoteEnvelope{vote}}
	heap.Push(pool.futureVotesPq, &types.VoteData{TargetNumber: number, TargetHash: hash})
	pool.mu.Unlock()
	return vote
}

// Block import publishes head events synchronously. With the main loop stuck, the
// events must still be absorbed and queued in arrival order.
func TestVotePoolHeadEventsNeverBlockImport(t *testing.T) {
	chain, pool := newTestChain(t, true)
	const n = 3 * highestVerifiedBlockChanSize

	// The loop pops the first head and then blocks on pool.mu inside prune; every
	// later head has to wait in pendingHeads.
	pool.mu.Lock()
	defer pool.mu.Unlock()
	insertBlocksWithin(t, chain, n, testDeadline)

	waitFor(t, "head events to be queued", func() bool {
		pool.headMu.Lock()
		defer pool.headMu.Unlock()
		heads := pool.pendingHeads
		return len(heads) == n-1 && heads[0].Number.Uint64() == 2 && heads[len(heads)-1].Number.Uint64() == n
	})
	pool.headMu.Lock()
	defer pool.headMu.Unlock()
	for i, h := range pool.pendingHeads {
		if want := uint64(i + 2); h.Number.Uint64() != want {
			t.Fatalf("pendingHeads[%d] = %d, want %d", i, h.Number.Uint64(), want)
		}
	}
}

// A subscriber that never reads must not hold the pool lock while transferred
// votes are announced: readers such as block import's FetchVotesByBlockHash
// have to get through, and the vote must already be in cur.
func TestVotePoolTransferNotifiesOutsideLock(t *testing.T) {
	chain, pool := newTestChain(t, true)

	stuck := make(chan core.NewVoteEvent) // unbuffered, never read until the end
	sub := pool.SubscribeNewVoteEvent(stuck)
	defer sub.Unsubscribe()

	targetHash := common.HexToHash("0x01")
	vote := injectFutureVote(pool, 1, targetHash)

	// Head 13 makes target 1 eligible for the unconditional transfer path (1+11 < 13).
	insertBlocksWithin(t, chain, upperLimitOfVoteBlockNumber+2, testDeadline)

	// The read lock must be obtainable while the announcement is blocked.
	waitFor(t, "transfer to release the pool lock", func() bool {
		if !pool.mu.TryRLock() {
			return false
		}
		defer pool.mu.RUnlock()
		return len(pool.futureVotes) == 0
	})

	fetched := make(chan int, 1)
	go func() { fetched <- len(pool.FetchVotesByBlockHash(targetHash, 0)) }()
	select {
	case n := <-fetched:
		if n != 1 {
			t.Fatalf("expected the transferred vote to be readable, got %d", n)
		}
	case <-time.After(testDeadline):
		t.Fatal("FetchVotesByBlockHash blocked behind a subscriber notification")
	}

	select {
	case ev := <-stuck:
		if ev.Vote != vote {
			t.Fatal("unexpected vote announced")
		}
	case <-time.After(testDeadline):
		t.Fatal("transferred vote was never announced")
	}
}

// Stop must release a loop that is blocked announcing to an unread subscriber,
// even with a head backlog queued and the chain already stopped.
func TestVotePoolStopReleasesBlockedNotification(t *testing.T) {
	chain, pool := newTestChain(t, false)

	stuck := make(chan core.NewVoteEvent) // never read
	sub := pool.SubscribeNewVoteEvent(stuck)

	targetHash := common.HexToHash("0x02")
	injectFutureVote(pool, 1, targetHash)
	insertBlocksWithin(t, chain, upperLimitOfVoteBlockNumber+2, testDeadline)
	waitFor(t, "transfer to start announcing", func() bool {
		if !pool.mu.TryRLock() {
			return false
		}
		defer pool.mu.RUnlock()
		return len(pool.futureVotes) == 0
	})

	// The loop is now parked in Feed.Send; heads must still be accepted.
	insertBlocksWithin(t, chain, 2*highestVerifiedBlockChanSize, testDeadline)

	// Chain first, pool second, as in eth.Ethereum.Stop.
	chain.Stop()
	pool.Stop()
	waitLoopsDone(t, pool)

	select {
	case <-sub.Err():
	default:
		t.Fatal("subscription not torn down by Stop")
	}
}
