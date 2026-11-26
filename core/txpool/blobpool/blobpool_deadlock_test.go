// Copyright 2026 The go-ethereum Authors
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

package blobpool

import (
	"crypto/ecdsa"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// newDeadlockTestPool creates an empty blob pool whose accounts are funded and
// whose fees make every test transaction immediately announcable.
func newDeadlockTestPool(t *testing.T, keys []*ecdsa.PrivateKey) *BlobPool {
	t.Helper()

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	for _, key := range keys {
		statedb.AddBalance(crypto.PubkeyToAddress(key.PublicKey), uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeUnspecified)
	}
	statedb.Commit(0, true, false)

	chain := &testBlockChain{
		config:  params.MainnetChainConfig,
		basefee: uint256.NewInt(1),
		blobfee: uint256.NewInt(1),
		statedb: statedb,
	}
	pool := New(Config{Datadir: t.TempDir()}, chain, nil)
	if err := pool.Init(1, chain.CurrentBlock(), newReserver()); err != nil {
		t.Fatalf("failed to create blob pool: %v", err)
	}
	return pool
}

// TestAddNotifiesAfterUnlock reproduces the node-wide deadlock of go-ethereum#36106
// (bnb-chain/bsc#3871) deterministically.
//
// The cycle on a live node: BlobPool.add holds p.lock while Feed.Send blocks on the
// eth handler's full txsCh; txBroadcastLoop is stuck handing hashes to a peer whose
// announcer is inside BlobPool.GetMetadata, waiting for p.lock.RLock. Here the
// subscriber channel is unbuffered and nobody drains it until the "announcer" has
// called GetMetadata, which is exactly that ordering.
func TestAddNotifiesAfterUnlock(t *testing.T) {
	key, _ := crypto.GenerateKey()
	pool := newDeadlockTestPool(t, []*ecdsa.PrivateKey{key})
	defer pool.Close()

	ch := make(chan core.NewTxsEvent) // the eth handler's txsCh, already full
	sub := pool.SubscribeTransactions(ch, false)
	defer sub.Unsubscribe()

	tx := makeTx(0, 1, 1000, 100, key)
	added := make(chan []error, 1)
	go func() { added <- pool.Add([]*types.Transaction{tx}, true) }()

	// Act as the announcer and look the transaction up until the pool reports it.
	// Nobody drains ch yet, so the adder is parked in Feed.Send. With the fix the
	// lock is released before that, and GetMetadata eventually sees the tx; with
	// the bug the adder still holds p.lock and GetMetadata blocks. No sleeps are
	// needed to line up the two goroutines.
	deadline := time.Now().Add(10 * time.Second)
	for {
		found := make(chan bool, 1)
		go func() { found <- pool.GetMetadata(tx.Hash()) != nil }()

		var seen bool
		select {
		case seen = <-found:
		case <-time.After(5 * time.Second):
			<-ch // unblock the adder so the test can shut down instead of hanging
			<-found
			<-added
			t.Fatal("GetMetadata blocked while Add was delivering its tx event: p.lock is held across Feed.Send (go-ethereum#36106)")
		}
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transaction never became visible in the pool")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case ev := <-ch:
		if len(ev.Txs) != 1 || ev.Txs[0].Hash() != tx.Hash() {
			t.Fatalf("unexpected event: %d txs", len(ev.Txs))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no transaction event delivered")
	}
	select {
	case errs := <-added:
		if errs[0] != nil {
			t.Fatalf("failed to add transaction: %v", errs[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Add did not return after its event was consumed")
	}
}

// TestConcurrentAddWithLookingAnnouncer runs the live shape: several peers deliver
// blob transactions concurrently while a single announcer looks every hash up in the
// pool before it accepts the next event. Without the fix the pool deadlocks.
func TestConcurrentAddWithLookingAnnouncer(t *testing.T) {
	const (
		accounts = 8
		perAcct  = 4
	)
	keys := make([]*ecdsa.PrivateKey, accounts)
	for i := range keys {
		keys[i], _ = crypto.GenerateKey()
	}
	pool := newDeadlockTestPool(t, keys)
	defer pool.Close()

	ch := make(chan core.NewTxsEvent)
	sub := pool.SubscribeTransactions(ch, false)
	defer sub.Unsubscribe()

	// Announcer: look up the previous batch in the pool, then accept the next event.
	stop := make(chan struct{})
	announced := make(chan int, accounts*perAcct)
	go func() {
		var last []*types.Transaction
		for {
			for _, tx := range last {
				pool.GetMetadata(tx.Hash())
			}
			select {
			case ev := <-ch:
				last = ev.Txs
				announced <- len(ev.Txs)
			case <-stop:
				return
			}
		}
	}()

	// Build all transactions up front: makeTx draws from the package-level rnd,
	// which is not safe for concurrent use.
	txs := make([][]*types.Transaction, len(keys))
	for i, key := range keys {
		for nonce := uint64(0); nonce < perAcct; nonce++ {
			txs[i] = append(txs[i], makeTx(nonce, 1, 1000, 100, key))
		}
	}
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func(batch []*types.Transaction) {
			defer wg.Done()
			for _, tx := range batch {
				if errs := pool.Add([]*types.Transaction{tx}, true); errs[0] != nil {
					t.Errorf("failed to add transaction: %v", errs[0])
				}
			}
		}(txs[i])
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		close(stop)
		go func() { // drain so the parked adders can finish and the pool can close
			for {
				select {
				case <-ch:
				case <-done:
					return
				}
			}
		}()
		<-done
		t.Fatal("concurrent blob adds deadlocked against a pool-reading announcer (go-ethereum#36106)")
	}
	// Every Send has been received; the announcer may still be recording the last one.
	total := 0
	for deadline := time.After(5 * time.Second); total < accounts*perAcct; {
		select {
		case n := <-announced:
			total += n
		case <-deadline:
			close(stop)
			t.Fatalf("announced %d transactions, want %d", total, accounts*perAcct)
		}
	}
	close(stop)
}
