package downloader

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

// Only state synchronization is exercised; peer packet methods are unused.
type pivotMergeSyncer struct {
	snap.Syncer
	run func(*types.Header, chan struct{}) error
}

func (s *pivotMergeSyncer) Sync(pivot *types.Header, cancel chan struct{}) error {
	return s.run(pivot, cancel)
}

func (*pivotMergeSyncer) FrozenPivot() *types.Header { return nil }
func (*pivotMergeSyncer) Version() uint              { return snap.SNAP1 }

func newPivotMergeDownloader(t *testing.T, pivot *types.Header, syncer snap.Syncer) *Downloader {
	t.Helper()
	d := &Downloader{
		queue:          newQueue(16, 16),
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

type pivotCursorSyncer struct {
	snap.Syncer
	run              func(*types.Header, chan struct{}) error
	previous, frozen *types.Header
}

func (s *pivotCursorSyncer) Version() uint                               { return snap.SNAP2 }
func (s *pivotCursorSyncer) Pivot() *types.Header                        { return s.previous }
func (s *pivotCursorSyncer) FrozenPivot() *types.Header                  { return s.frozen }
func (s *pivotCursorSyncer) Sync(h *types.Header, c chan struct{}) error { return s.run(h, c) }

func TestSnapContentResumesPreviousPivot(t *testing.T) {
	previous := &types.Header{Number: big.NewInt(1)}
	target := &types.Header{Number: big.NewInt(3)}
	want := errors.New("observed initial state sync")
	got := make(chan common.Hash, 1)
	s := &pivotCursorSyncer{previous: previous, run: func(h *types.Header, c chan struct{}) error { got <- h.Hash(); return want }}
	d := newPivotMergeDownloader(t, target, s)
	checkPivotMergeError(t, d, want)
	if hash := <-got; hash != previous.Hash() {
		t.Fatal("recovery started at a new target before its gap headers are available")
	}
}

// prefixImportChain models prefix persistence; consensus validation is not under test.
type prefixImportChain struct {
	BlockChain
	db                       ethdb.Database
	oldCanceled, newObserved chan struct{}
	afterImport              func()
}

func (c *prefixImportChain) InsertReceiptChain(blocks types.Blocks, _ []rlp.RawValue, _ uint64) (int, error) {
	// If retargeting cancelled the old sync first, let the new task inspect the header before import.
	select {
	case <-c.oldCanceled:
		<-c.newObserved
	default:
	}
	for _, b := range blocks {
		rawdb.WriteHeader(c.db, b.Header())
	}
	if c.afterImport != nil {
		c.afterImport()
	}
	return 0, nil
}

func TestSnapContentImportsGapBeforeRetarget(t *testing.T) {
	a := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}
	b := &types.Header{Number: big.NewInt(2), ParentHash: a.Hash(), Difficulty: big.NewInt(0)}
	target := &types.Header{Number: big.NewInt(3), ParentHash: b.Hash(), Difficulty: big.NewInt(0)}
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	want := errors.New("new cursor observed")
	oldCanceled, newObserved := make(chan struct{}), make(chan struct{})
	present := make(chan bool, 1)
	var d *Downloader
	s := &pivotCursorSyncer{run: func(h *types.Header, c chan struct{}) error {
		if h.Hash() == target.Hash() {
			present <- rawdb.ReadHeader(db, b.Hash(), 2) != nil
			close(newObserved)
			return want
		}
		d.pivotLock.Lock()
		d.pivotHeader = target
		d.pivotLock.Unlock()
		d.queue.resultCache.lock.Lock()
		for i, h := range []*types.Header{a, b, target} {
			d.queue.resultCache.items[i] = &fetchResult{Header: h}
		}
		d.queue.resultCache.lock.Unlock()
		d.queue.Close()
		<-c
		close(oldCanceled)
		return snap.ErrCancelled
	}}
	d = newPivotMergeDownloader(t, a, s)
	d.queue.resultCache.Prepare(1)
	d.blockchain = &prefixImportChain{db: db, oldCanceled: oldCanceled, newObserved: newObserved}
	checkPivotMergeError(t, d, want)
	if !<-present {
		t.Fatal("new snap2 sync entered before intermediate headers were imported")
	}
}

type boundedHeaderPeer struct {
	*downloadTesterPeer
	head    uint64
	onPivot func()
}

func (p *boundedHeaderPeer) RequestHeadersByNumber(origin uint64, n, skip int, reverse bool, sink chan *eth.Response) (*eth.Request, error) {
	if origin > p.head {
		return nil, errCanceled
	}
	if n == 2 && p.onPivot != nil {
		p.onPivot()
	}
	return p.downloadTesterPeer.RequestHeadersByNumber(origin, n, skip, reverse, sink)
}
func TestHeaderFetcherKeepsFrozenPivot(t *testing.T) {
	dl := newTesterWithSnap(t, SnapSync, nil, true)
	defer dl.terminate()
	chain := testChainBase.shorten(800)
	peer := dl.newPeer("frozen", eth.ETH71, chain.blocks[1:])
	d := dl.downloader
	frozen := chain.blocks[64].Header()
	d.pivotHeader = frozen
	d.snapSyncer = &pivotCursorSyncer{Syncer: d.snapSyncer, frozen: frozen}
	d.cancelCh = make(chan struct{})
	d.headerProcCh = make(chan *headerTask, 64)
	conn := d.peers.Peer("frozen")
	conn.peer = &boundedHeaderPeer{downloadTesterPeer: peer, head: 799}
	if err := d.fetchHeaders(conn, 1, 799); !errors.Is(err, errCanceled) {
		t.Fatalf("header loop: %v", err)
	}
	if d.pivotHeader.Hash() != frozen.Hash() {
		t.Fatalf("header fetcher moved frozen pivot from 64 to %d", d.pivotHeader.Number.Uint64())
	}
}

func TestHeaderFetcherFreezesInFlightPivotRequest(t *testing.T) {
	dl := newTesterWithSnap(t, SnapSync, nil, true)
	defer dl.terminate()
	chain := testChainBase.shorten(800)
	peer := dl.newPeer("inflight", eth.ETH71, chain.blocks[1:])
	d := dl.downloader
	old := chain.blocks[64].Header()
	d.pivotHeader = old
	s := &pivotCursorSyncer{Syncer: d.snapSyncer}
	d.snapSyncer = s
	d.cancelCh = make(chan struct{})
	d.headerProcCh = make(chan *headerTask, 64)
	requested := false
	conn := d.peers.Peer("inflight")
	conn.peer = &boundedHeaderPeer{downloadTesterPeer: peer, head: 799, onPivot: func() { requested = true; s.frozen = old }}
	if err := d.fetchHeaders(conn, 1, 799); !errors.Is(err, errCanceled) {
		t.Fatal(err)
	}
	if !requested {
		t.Fatal("did not exercise an in-flight pivot request")
	}
	if d.pivotHeader.Hash() != old.Hash() {
		t.Fatal("response moved pivot after generation froze it")
	}
}

func TestSnapContentWaitsForTarget(t *testing.T) {
	a := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}
	b := &types.Header{Number: big.NewInt(2), ParentHash: a.Hash(), Difficulty: big.NewInt(0)}
	target := &types.Header{Number: big.NewInt(3), ParentHash: b.Hash(), Difficulty: big.NewInt(0)}
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	want := errors.New("target observed after batches")
	oldCanceled, newObserved := make(chan struct{}), make(chan struct{})
	present := make(chan bool, 1)
	var d *Downloader
	s := &pivotCursorSyncer{run: func(h *types.Header, c chan struct{}) error {
		if h.Hash() == target.Hash() {
			present <- rawdb.ReadHeader(db, b.Hash(), 2) != nil
			close(newObserved)
			return want
		}
		d.pivotLock.Lock()
		d.pivotHeader = target
		d.pivotLock.Unlock()
		d.queue.resultCache.lock.Lock()
		d.queue.resultCache.items[0] = &fetchResult{Header: a}
		d.queue.resultCache.lock.Unlock()
		d.queue.lock.Lock()
		d.queue.active.Broadcast()
		d.queue.lock.Unlock()
		<-c
		close(oldCanceled)
		return snap.ErrCancelled
	}}
	d = newPivotMergeDownloader(t, a, s)
	d.queue.resultCache.Prepare(1)
	delivered := false
	d.blockchain = &prefixImportChain{db: db, oldCanceled: oldCanceled, newObserved: newObserved, afterImport: func() {
		if delivered {
			return
		}
		delivered = true
		d.queue.resultCache.lock.Lock()
		d.queue.resultCache.items[0] = &fetchResult{Header: b}
		d.queue.resultCache.items[1] = &fetchResult{Header: target}
		d.queue.resultCache.lock.Unlock()
		d.queue.Close()
	}}
	checkPivotMergeError(t, d, want)
	if !<-present {
		t.Fatal("snap2 retargeted while the target block/gap headers were still unavailable")
	}
}

func TestSnapContentCommittedKeepsTarget(t *testing.T) {
	previous := &types.Header{Number: big.NewInt(100)}
	target := &types.Header{Number: big.NewInt(0)}
	want := errors.New("short-chain cursor observed")
	got := make(chan common.Hash, 1)
	s := &pivotCursorSyncer{previous: previous, run: func(h *types.Header, c chan struct{}) error { got <- h.Hash(); return want }}
	d := newPivotMergeDownloader(t, target, s)
	d.stateDB = rawdb.NewMemoryDatabase()
	defer d.stateDB.Close()
	d.committed.Store(true)
	d.queue.Close()
	checkPivotMergeError(t, d, want)
	if hash := <-got; hash != target.Hash() {
		t.Fatal("short/committed chain resumed unrelated old state")
	}
}
