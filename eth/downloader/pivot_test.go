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

package downloader

import (
	"encoding/json"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

type pivotHeaderPeer struct {
	Peer
	headers   map[common.Hash]*types.Header
	requests  atomic.Int32
	requested chan struct{}
	stall     bool
	malformed bool
}

func (p *pivotHeaderPeer) RequestHeadersByHash(hash common.Hash, amount, skip int, reverse bool, sink chan *eth.Response) (*eth.Request, error) {
	p.requests.Add(1)
	if p.requested != nil {
		close(p.requested)
	}
	req := &eth.Request{Peer: "pivot"}
	if p.stall {
		return req, nil
	}
	var headers []*types.Header
	for len(headers) < amount {
		h := p.headers[hash]
		if h == nil {
			break
		}
		headers = append(headers, h)
		hash = h.ParentHash
	}
	if p.malformed && len(headers) > 1 {
		headers[1] = headers[0]
	}
	hashes := make([]common.Hash, len(headers))
	for i, h := range headers {
		hashes[i] = h.Hash()
	}
	response := &eth.Response{Req: req, Res: (*eth.BlockHeadersRequest)(&headers), Meta: hashes, Done: make(chan error, 1)}
	go func() { sink <- response }()
	return req, nil
}

func newPivotHeaderDownloader(t *testing.T) (*Downloader, *pivotHeaderPeer, []*types.Header) {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { db.Close() })
	p := &pivotHeaderPeer{headers: make(map[common.Hash]*types.Header)}
	headers := make([]*types.Header, 6)
	for i := range headers {
		h := &types.Header{Number: big.NewInt(int64(100 + i)), Difficulty: big.NewInt(1)}
		if i > 0 {
			h.ParentHash = headers[i-1].Hash()
		}
		headers[i] = h
		p.headers[h.Hash()] = h
	}
	d := &Downloader{stateDB: db, peers: newPeerSet(), cancelPeer: "pivot", cancelCh: make(chan struct{}), headerCache: lru.NewCache[common.Hash, *types.Header](1024)}
	if err := d.peers.Register(newPeerConnection("pivot", eth.ETH70, p, log.New())); err != nil {
		t.Fatal(err)
	}
	return d, p, headers
}

func TestResumeFrozenPivot(t *testing.T) {
	for _, name := range []string{"same-chain", "reorg", "lagging-peer", "missing-header", "probe-limit", "expired-pivot"} {
		t.Run(name, func(t *testing.T) {
			d, p, headers := newPivotHeaderDownloader(t)
			frozen, head, pivot := headers[1], headers[5], headers[4]
			switch name {
			case "reorg":
				frozen = types.CopyHeader(frozen)
				frozen.Extra = []byte("orphan")
			case "lagging-peer":
				head = headers[0]
			case "expired-pivot":
				head = types.CopyHeader(head)
				head.Number = new(big.Int).SetUint64(frozen.Number.Uint64() + params.FullImmutabilityThreshold + uint64(fsMinFullBlocks) + 1)
				pivot = types.CopyHeader(pivot)
				pivot.Number = new(big.Int).Sub(head.Number, big.NewInt(int64(fsMinFullBlocks)))
			case "probe-limit":
				head = types.CopyHeader(head)
				head.Number = new(big.Int).SetUint64(frozen.Number.Uint64() + params.FullImmutabilityThreshold + uint64(fsMinFullBlocks) + 1)
			case "missing-header":
				delete(p.headers, headers[2].Hash())
			}
			// Restore a real V2 syncer's frozen phase from an on-disk journal. The
			// local canonical index intentionally has not reached this pivot.
			progress, err := json.Marshal(struct {
				Pivot *types.Header
				Phase uint8
			}{frozen, 1})
			if err != nil {
				t.Fatal(err)
			}
			journal := append([]byte{2}, progress...)
			rawdb.WriteSnapshotSyncStatus(d.stateDB, journal)
			d.snapSyncer = snap.NewV2Syncer(d.stateDB, rawdb.HashScheme, d.queryHeader)
			got, err := d.resumeFrozenPivot(head, pivot)
			if name == "lagging-peer" || name == "missing-header" || name == "probe-limit" {
				if err == nil {
					t.Fatal("expected unavailable ancestry to abort the attempt")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := pivot
				if name == "same-chain" {
					want = frozen
				}
				if got.Hash() != want.Hash() {
					t.Fatalf("pivot = %s, want %s", got.Hash(), want.Hash())
				}
			}
			if string(rawdb.ReadSnapshotSyncStatus(d.stateDB)) != string(journal) {
				t.Fatal("ancestry check changed persisted progress")
			}
		})
	}
}

func TestSyncHeaderBatchAndValidation(t *testing.T) {
	t.Run("batch-cache", func(t *testing.T) {
		d, p, headers := newPivotHeaderDownloader(t)
		for i := len(headers) - 1; i >= 0; i-- {
			got, err := d.queryHeader(headers[i].Hash(), headers[i].Number.Uint64(), nil)
			if err != nil || got.Hash() != headers[i].Hash() {
				t.Fatalf("header %d: %v", i, err)
			}
			if rawdb.ReadHeader(d.stateDB, got.Hash(), got.Number.Uint64()) != nil {
				t.Fatal("peer ancestry must not be written to the chain database")
			}
		}
		if p.requests.Load() != 1 {
			t.Fatalf("requests = %d, want one reverse batch", p.requests.Load())
		}
	})
	t.Run("invalid-header", func(t *testing.T) {
		d, p, headers := newPivotHeaderDownloader(t)
		header := types.CopyHeader(headers[0])
		header.BaseFee = new(big.Int).Lsh(big.NewInt(1), 256)
		p.headers[header.Hash()] = header
		if _, err := d.queryHeader(header.Hash(), header.Number.Uint64(), nil); !errors.Is(err, errBadPeer) {
			t.Fatalf("error = %v, want bad peer", err)
		}
		if d.headerCache.Len() != 0 {
			t.Fatal("invalid header polluted ancestry cache")
		}
	})
	t.Run("malformed-chain", func(t *testing.T) {
		d, p, headers := newPivotHeaderDownloader(t)
		p.malformed = true
		_, err := d.queryHeader(headers[5].Hash(), headers[5].Number.Uint64(), nil)
		if !errors.Is(err, errBadPeer) {
			t.Fatalf("error = %v, want bad peer", err)
		}
		if d.headerCache.Len() != 0 {
			t.Fatal("malformed batch polluted ancestry cache")
		}
	})
}

func TestSyncHeaderCancellation(t *testing.T) {
	d, p, headers := newPivotHeaderDownloader(t)
	p.stall = true
	p.requested = make(chan struct{})
	result := make(chan error, 1)
	go func() { _, err := d.queryHeader(headers[5].Hash(), headers[5].Number.Uint64(), nil); result <- err }()
	<-p.requested
	close(d.cancelCh)
	select {
	case err := <-result:
		if !errors.Is(err, errCanceled) {
			t.Fatalf("error = %v, want cancelled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("header request did not observe cancellation")
	}
}

func TestMovePivotGuards(t *testing.T) {
	for _, name := range []string{"movable", "frozen", "superseded", "committed"} {
		t.Run(name, func(t *testing.T) {
			d, _, headers := newPivotHeaderDownloader(t)
			from, to := headers[1], headers[2]
			syncer := new(pivotMergeSyncer)
			d.snapSyncer = syncer
			d.pivotHeader = from
			switch name {
			case "frozen":
				syncer.frozen.Store(from)
			case "superseded":
				d.pivotHeader = headers[3]
			case "committed":
				d.committed.Store(true)
			}
			before := d.pivotHeader
			moved := d.movePivot(from, to)
			if moved != (name == "movable") {
				t.Fatalf("moved = %v", moved)
			}
			if !moved && d.pivotHeader.Hash() != before.Hash() {
				t.Fatal("rejected move changed pivot")
			}
			if moved {
				if d.pivotHeader.Hash() != to.Hash() {
					t.Fatal("move did not publish the target")
				}
				if number := rawdb.ReadLastPivotNumber(d.stateDB); number == nil || *number != to.Number.Uint64() {
					t.Fatal("move did not persist the rollback boundary")
				}
			} else if rawdb.ReadLastPivotNumber(d.stateDB) != nil {
				t.Fatal("rejected move changed persisted pivot")
			}
		})
	}
}

// Retargeting cancels only the state cycle. It must release an ancestry request
// without shutting down the downloader or discarding the previous checkpoint.
func TestStateCycleHeaderCancellation(t *testing.T) {
	d, p, headers := newPivotHeaderDownloader(t)
	p.stall = true
	p.requested = make(chan struct{})
	progress, err := json.Marshal(struct {
		Pivot *types.Header
		Phase uint8
	}{headers[0], 2}) // completed checkpoint; the next cycle must validate ancestry
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteSnapshotSyncStatus(d.stateDB, append([]byte{2}, progress...))
	d.snapSyncer = snap.NewV2Syncer(d.stateDB, rawdb.HashScheme, d.queryHeader)
	s := newStateSync(d, headers[5])
	go s.run()
	defer func() {
		close(d.cancelCh)
		s.Cancel()
	}()
	select {
	case <-p.requested:
	case <-time.After(3 * time.Second):
		t.Fatal("ancestry request did not start")
	}
	result := make(chan error, 1)
	go func() { result <- s.Cancel() }()
	select {
	case err := <-result:
		if err != snap.ErrCancelled {
			t.Fatalf("error = %v, want state cycle cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("state cycle cancellation did not interrupt the header request")
	}
	select {
	case <-d.cancelCh:
		t.Fatal("state cycle cancellation shut down the downloader")
	default:
	}
	var checkpoint struct {
		Pivot *types.Header
		Phase uint8
	}
	journal := rawdb.ReadSnapshotSyncStatus(d.stateDB)
	if len(journal) == 0 {
		t.Fatal("cancelled cycle discarded the checkpoint")
	}
	if err := json.Unmarshal(journal[1:], &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Pivot == nil || checkpoint.Pivot.Hash() != headers[0].Hash() || checkpoint.Phase != 2 {
		t.Fatal("cancelled ancestry lookup changed checkpoint pivot or phase")
	}
	// The same downloader must remain usable for the next cycle's ancestry.
	p.stall = false
	p.requested = nil
	got, err := d.queryHeader(headers[5].Hash(), headers[5].Number.Uint64(), make(chan struct{}))
	if err != nil || got == nil || got.Hash() != headers[5].Hash() {
		t.Fatalf("next cycle header request failed: %v", err)
	}
	cancel := make(chan struct{})
	close(cancel)
	if _, err := d.queryHeader(headers[5].Hash(), headers[5].Number.Uint64(), cancel); err != snap.ErrCancelled {
		t.Fatalf("cached header ignored cancellation: %v", err)
	}
	if p.requests.Load() != 2 {
		t.Fatalf("requests = %d, want two cycles", p.requests.Load())
	}
}
