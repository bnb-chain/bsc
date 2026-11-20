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
	"bytes"
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

func loadPivotJournal(t *testing.T, d *Downloader, pivot *types.Header, phase uint8) {
	t.Helper()
	progress, err := json.Marshal(struct {
		Pivot *types.Header
		Phase uint8
	}{pivot, phase})
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteSnapshotSyncStatus(d.stateDB, append([]byte{2}, progress...))
	d.snapSyncer = snap.NewV2Syncer(d.stateDB, rawdb.HashScheme, d.queryHeader)
}

func TestResumeFrozenPivot(t *testing.T) {
	for _, name := range []string{"same-chain", "reorg", "missing-header", "expired-pivot"} {
		t.Run(name, func(t *testing.T) {
			d, p, headers := newPivotHeaderDownloader(t)
			frozen, head, candidate := headers[1], headers[5], headers[4]
			switch name {
			case "reorg":
				frozen = types.CopyHeader(frozen)
				frozen.Extra = []byte("orphan")
			case "missing-header":
				delete(p.headers, headers[2].Hash())
			case "expired-pivot":
				candidate = types.CopyHeader(candidate)
				candidate.Number.SetUint64(frozen.Number.Uint64() + params.FullImmutabilityThreshold + 1)
				head = types.CopyHeader(head)
				head.Number.SetUint64(candidate.Number.Uint64() + uint64(fsMinFullBlocks))
			}
			loadPivotJournal(t, d, frozen, 1) // phaseGenerate
			journal := rawdb.ReadSnapshotSyncStatus(d.stateDB)
			got, err := d.resumeFrozenPivot(head, candidate)
			if name == "missing-header" {
				if err == nil || !bytes.Equal(journal, rawdb.ReadSnapshotSyncStatus(d.stateDB)) {
					t.Fatal("missing ancestry must abort and preserve the checkpoint")
				}
				return
			}
			want := candidate
			if name == "same-chain" {
				want = frozen
				if p.requests.Load() != 1 {
					t.Fatal("expected one reverse header batch")
				}
			}
			if err != nil || got == nil || got.Hash() != want.Hash() {
				t.Fatalf("pivot = %v, error = %v, want %s", got, err, want.Hash())
			}
		})
	}
}

func TestQueryHeaderRejectsInvalidHeader(t *testing.T) {
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
}

func TestStateCycleHeaderCancellation(t *testing.T) {
	d, p, headers := newPivotHeaderDownloader(t)
	p.stall = true
	p.requested = make(chan struct{})
	loadPivotJournal(t, d, headers[0], 2) // phaseComplete
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
}
