package downloader

import (
	"errors"
	"math/big"
	"sync/atomic"

	"testing"
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
	run           func(*types.Header, chan struct{}) error
	frozen        atomic.Pointer[types.Header]
	onFrozenCheck func()
}

func (s *pivotMergeSyncer) Sync(pivot *types.Header, cancel chan struct{}) error {
	return s.run(pivot, cancel)
}

func (s *pivotMergeSyncer) FrozenPivot() *types.Header {
	header := s.frozen.Load()
	if s.onFrozenCheck != nil {
		s.onFrozenCheck()
	}
	return header
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

// Generation can start between the staleness decision and cancellation. The
// final checkpoint must win over an ordinary move to a newer, same-chain pivot.
func TestSnapContentFreezesDuringRetarget(t *testing.T) {
	old := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}
	target := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(0), ParentHash: old.Hash()}
	want := errors.New("resumed generation at frozen pivot")
	var d *Downloader
	var runs atomic.Int32
	syncer := new(pivotMergeSyncer)
	syncer.run = func(header *types.Header, cancel chan struct{}) error {
		if runs.Add(1) == 2 {
			if header.Hash() != old.Hash() {
				return errors.New("generation was moved to the new pivot")
			}
			return want
		}
		d.pivotLock.Lock()
		d.pivotHeader = target
		d.pivotLock.Unlock()
		d.queue.resultCache.lock.Lock()
		d.queue.resultCache.items[0] = &fetchResult{Header: old}
		d.queue.resultCache.items[1] = &fetchResult{Header: target}
		d.queue.resultCache.lock.Unlock()
		d.queue.Close()
		<-cancel
		syncer.frozen.Store(old)
		return snap.ErrCancelled
	}
	d = newPivotMergeDownloader(t, old, syncer)
	rawdb.WriteHeader(d.stateDB, old)
	d.queue.resultCache.Prepare(1)
	checkPivotMergeError(t, d, want)
	if d.pivotHeader.Hash() != old.Hash() {
		t.Fatal("downloader did not restore the frozen pivot")
	}
	if number := rawdb.ReadLastPivotNumber(d.stateDB); number == nil || *number != old.Number.Uint64() {
		t.Fatal("rollback boundary did not follow the restored pivot")
	}
}

type pivotCommitChain struct {
	BlockChain
	complete func(common.Hash) error
	insert   func(types.Blocks) error
}

func (c pivotCommitChain) InsertReceiptChain(types.Blocks, []rlp.RawValue, uint64) (int, error) {
	return 0, nil
}

func (c pivotCommitChain) SnapSyncComplete(hash common.Hash, _ bool) error {
	return c.complete(hash)
}

func (c pivotCommitChain) InsertChain(blocks types.Blocks) (int, error) {
	return 0, c.insert(blocks)
}

func TestSnapContentFrozenPivotBackpressure(t *testing.T) {
	pivot := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}
	tail := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(0), ParentHash: pivot.Hash()}
	finish := make(chan struct{})
	syncer := new(pivotMergeSyncer)
	syncer.frozen.Store(pivot)
	syncer.run = func(_ *types.Header, cancel chan struct{}) error {
		select {
		case <-finish:
			syncer.frozen.Store(nil)
			return nil
		case <-cancel:
			return snap.ErrCancelled
		}
	}
	var checks int
	syncer.onFrozenCheck = func() {
		checks++
		// The first call checks pivot movement. Release generation when the
		// processor next consults the freeze, which must precede another dequeue.
		if checks == 2 {
			close(finish)
		}
	}

	d := newPivotMergeDownloader(t, pivot, syncer)
	d.moder = new(syncModer)
	d.queue.resultCache.Prepare(1)
	d.queue.resultCache.items[0] = &fetchResult{Header: pivot}
	d.chainInsertHook = func(results []*fetchResult, _ chan struct{}) {
		if len(results) == 1 && results[0].Header.Hash() == pivot.Hash() {
			d.queue.resultCache.lock.Lock()
			d.queue.resultCache.items[0] = &fetchResult{Header: tail}
			d.queue.resultCache.lock.Unlock()
			d.queue.Close()
		}
	}
	var imported bool
	d.blockchain = pivotCommitChain{
		complete: func(hash common.Hash) error {
			if hash != pivot.Hash() || d.queue.resultCache.Offset() != tail.Number.Uint64() {
				return errors.New("pivot must commit before dequeuing its tail")
			}
			return nil
		},
		insert: func(blocks types.Blocks) error {
			if !d.committed.Load() || len(blocks) != 1 || blocks[0].Hash() != tail.Hash() {
				return errors.New("tail must be imported after the pivot commits")
			}
			imported = true
			return nil
		},
	}
	checkPivotMergeError(t, d, nil)
	if !imported {
		t.Fatal("queued tail was not imported")
	}
}
