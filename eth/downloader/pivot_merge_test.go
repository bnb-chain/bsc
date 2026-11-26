package downloader

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/rlp"
)

// Only state synchronization is exercised; peer packet methods are unused.
type pivotMergeSyncer struct {
	snap.Syncer
	run    func(*types.Header, chan struct{}) error
	frozen atomic.Pointer[types.Header]
}

func (s *pivotMergeSyncer) Sync(pivot *types.Header, cancel chan struct{}) error {
	return s.run(pivot, cancel)
}

func (s *pivotMergeSyncer) FrozenPivot() *types.Header {
	return s.frozen.Load()
}

func (*pivotMergeSyncer) Version() uint { return snap.SNAP2 }

func newPivotMergeDownloader(t *testing.T, pivot *types.Header, syncer snap.Syncer) *Downloader {
	t.Helper()
	d := &Downloader{
		queue:          newQueue(16, 16),
		stateDB:        rawdb.NewMemoryDatabase(),
		headerCache:    lru.NewCache[common.Hash, *types.Header](1024),
		pivotHeader:    pivot,
		snapSyncer:     syncer,
		stateSyncStart: make(chan *stateSync),
		cancelCh:       make(chan struct{}),
		quitCh:         make(chan struct{}),
		syncLogTime:    time.Now(), // No progress/database reporting in these tests.
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		d.stateFetcher()
	}()
	t.Cleanup(func() {
		close(d.cancelCh)
		close(d.quitCh)
		d.queue.Close()
		<-stopped
		d.stateDB.Close()
	})
	return d
}

func checkPivotMergeError(t *testing.T, d *Downloader, want error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- d.processSnapSyncContent() }()
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("processSnapSyncContent returned %v, want %v", err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processSnapSyncContent did not finish")
	}
}

func TestSnapContentEmptyQueuePropagatesStateError(t *testing.T) {
	want := errors.New("state download failed")
	syncer := &pivotMergeSyncer{run: func(*types.Header, chan struct{}) error { return want }}
	d := newPivotMergeDownloader(t, &types.Header{Number: big.NewInt(1)}, syncer)
	// closeOnErr must wake Results and return the state error instead of
	// indexing an empty result batch or blaming the block-serving peer.
	checkPivotMergeError(t, d, want)
}

func TestSnapContentRetargetsStateSync(t *testing.T) {
	for _, publish := range []bool{false, true} {
		name := "downloaded-header"
		if publish {
			name = "published-pivot"
		}
		t.Run(name, func(t *testing.T) {
			old := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), Extra: []byte("old")}
			target := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), Extra: []byte("new")}
			want := errors.New("new pivot state sync started")
			var d *Downloader
			syncer := &pivotMergeSyncer{run: func(header *types.Header, cancel chan struct{}) error {
				if header.Hash() == target.Hash() {
					return want
				}
				if publish {
					d.pivotLock.Lock()
					d.pivotHeader = target
					d.pivotLock.Unlock()
				}
				// Deliver the new pivot while the old state download is still
				// running. The processor must cancel it and start the new one.
				d.queue.resultCache.lock.Lock()
				d.queue.resultCache.items[0] = &fetchResult{Header: target}
				d.queue.resultCache.lock.Unlock()
				d.queue.Close()
				<-cancel
				return snap.ErrCancelled
			}}
			d = newPivotMergeDownloader(t, old, syncer)
			d.queue.resultCache.Prepare(1)
			checkPivotMergeError(t, d, want)
		})
	}
}

type pivotCommitChain struct {
	BlockChain
	importBlocks func(types.Blocks)
}

func (pivotCommitChain) InsertReceiptChain(types.Blocks, []rlp.RawValue, uint64) (int, error) {
	return 0, nil
}

func (pivotCommitChain) SnapSyncComplete(common.Hash, bool) error { return nil }

func (c pivotCommitChain) InsertChain(blocks types.Blocks) (int, error) {
	c.importBlocks(blocks)
	return len(blocks), nil
}

func TestSnapContentFrozenPivotBackpressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pivot := &types.Header{Number: big.NewInt(1), Difficulty: common.Big0}
		tail := &types.Header{Number: big.NewInt(2), Difficulty: common.Big0, ParentHash: pivot.Hash()}
		finish := make(chan struct{})
		syncer := &pivotMergeSyncer{run: func(*types.Header, chan struct{}) error {
			<-finish
			return nil
		}}
		syncer.frozen.Store(pivot)
		d := newPivotMergeDownloader(t, pivot, syncer)
		d.moder = new(syncModer)
		cache := d.queue.resultCache
		cache.Prepare(1)
		cache.items[0] = &fetchResult{Header: pivot}
		var imported bool
		d.blockchain = pivotCommitChain{importBlocks: func(blocks types.Blocks) {
			if !d.committed.Load() || len(blocks) != 1 || blocks[0].Hash() != tail.Hash() {
				t.Error("tail must be imported after the pivot commits")
			}
			imported = true
		}}

		result := make(chan error, 1)
		go func() { result <- d.processSnapSyncContent() }()
		synctest.Wait()
		cache.lock.Lock()
		cache.items[0] = &fetchResult{Header: tail}
		cache.lock.Unlock()
		d.queue.Close()
		time.Sleep(2 * time.Second) // Advance past the staleness timer.
		synctest.Wait()
		if offset := cache.Offset(); offset != tail.Number.Uint64() {
			t.Errorf("tail was dequeued before the pivot committed: offset %d", offset)
		}
		close(finish)
		if err := <-result; err != nil || !imported {
			t.Fatalf("tail import did not complete: %v", err)
		}
	})
}
