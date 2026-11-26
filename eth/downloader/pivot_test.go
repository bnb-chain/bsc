package downloader

import (
	"encoding/json"
	"math/big"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/log"
)

type pivotHeaderPeer struct {
	Peer
	header *types.Header
}

func (p pivotHeaderPeer) RequestHeadersByHash(_ common.Hash, _, _ int, _ bool, sink chan *eth.Response) (*eth.Request, error) {
	req := new(eth.Request)
	if p.header != nil {
		headers := eth.BlockHeadersRequest{p.header}
		resp := &eth.Response{Req: req, Res: &headers, Meta: []common.Hash{p.header.Hash()}, Done: make(chan error, 1)}
		go func() { sink <- resp }()
	}
	return req, nil
}

func newPivotTestDownloader(t *testing.T, peer pivotHeaderPeer) *Downloader {
	t.Helper()
	d := newPivotMergeDownloader(t, nil, nil)
	d.peers, d.cancelPeer = newPeerSet(), "pivot"
	if err := d.peers.Register(newPeerConnection(d.cancelPeer, eth.ETH70, peer, log.New())); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestResumeFrozenPivot(t *testing.T) {
	frozen := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(1)}
	head := &types.Header{Number: big.NewInt(3), ParentHash: frozen.Hash()}
	lower := &types.Header{Number: big.NewInt(1)}
	d := newPivotTestDownloader(t, pivotHeaderPeer{header: frozen})
	syncer := new(pivotMergeSyncer)
	syncer.frozen.Store(frozen)
	d.snapSyncer = syncer
	if got, err := d.resumeFrozenPivot(head, head); err != nil || got != frozen {
		t.Fatalf("peer ancestry should preserve the frozen pivot: got %v, err %v", got, err)
	}
	if got, err := d.resumeFrozenPivot(lower, lower); err != nil || got != lower {
		t.Fatalf("lower peer head should use the candidate: got %v, err %v", got, err)
	}
}

func TestStateCycleHeaderCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newPivotTestDownloader(t, pivotHeaderPeer{})
		checkpoint := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1)}
		target := &types.Header{Number: big.NewInt(3), ParentHash: common.Hash{1}}
		journal, err := json.Marshal(map[string]any{"Pivot": checkpoint, "Phase": 2}) // phaseComplete
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteSnapshotSyncStatus(d.stateDB, append([]byte{2}, journal...))
		d.snapSyncer = snap.NewV2Syncer(d.stateDB, rawdb.HashScheme, d.queryHeader)
		s := d.syncState(target)
		synctest.Wait() // The peer withholds the ancestry response.
		start := time.Now()
		if err := s.Cancel(); err != snap.ErrCancelled || time.Since(start) != 0 {
			t.Fatalf("header request cancellation took %v, got %v", time.Since(start), err)
		}
	})
}
