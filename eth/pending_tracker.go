package eth

import (
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const pendingTrackerLimit = 200000

// pendingTxRecord keeps the first seen info of a pending transaction.
type pendingTxRecord struct {
	FirstSeen   int64  // millisecond timestamp when we first accepted the pending tx
	PeerID      string // peer id delivered the tx first
	PeerAddress string // remote address delivered the tx first
}

type pendingPeerStat struct {
	PeerAddress string
	Count       uint64
}

// pendingTxTracker tracks the first time we saw pending transactions and
// accumulates per-peer stats for the first deliveries.
type pendingTxTracker struct {
	mu         sync.RWMutex
	records    map[common.Hash]pendingTxRecord
	order      []common.Hash // insertion order to evict oldest records
	peerCounts map[string]uint64
	limit      int
}

func newPendingTxTracker(limit int) *pendingTxTracker {
	return &pendingTxTracker{
		records:    make(map[common.Hash]pendingTxRecord),
		order:      make([]common.Hash, 0, limit),
		peerCounts: make(map[string]uint64),
		limit:      limit,
	}
}

// Record saves the first seen time of a pending transaction. If the transaction
// is already tracked it is ignored. Set countPeer to false for local
// submissions so we don't skew peer stats.
func (t *pendingTxTracker) Record(hash common.Hash, peerID, peerAddr string, countPeer bool) {
	t.mu.Lock()
	if _, ok := t.records[hash]; ok {
		t.mu.Unlock()
		return
	}

	t.records[hash] = pendingTxRecord{
		FirstSeen:   time.Now().UnixMilli(),
		PeerID:      peerID,
		PeerAddress: peerAddr,
	}
	t.order = append(t.order, hash)
	for len(t.order) > t.limit {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.records, oldest)
	}

	if countPeer && peerAddr != "" {
		t.peerCounts[peerAddr]++
	}
	t.mu.Unlock()
}

// FirstSeen returns the first seen record if any.
func (t *pendingTxTracker) FirstSeen(hash common.Hash) (pendingTxRecord, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rec, ok := t.records[hash]
	return rec, ok
}

// TopPeers returns the top N peers sorted by the number of first seen pending
// transactions delivered.
func (t *pendingTxTracker) TopPeers(n int) []pendingPeerStat {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := make([]pendingPeerStat, 0, len(t.peerCounts))
	for peer, count := range t.peerCounts {
		stats = append(stats, pendingPeerStat{PeerAddress: peer, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count == stats[j].Count {
			return stats[i].PeerAddress < stats[j].PeerAddress
		}
		return stats[i].Count > stats[j].Count
	})
	if n > 0 && len(stats) > n {
		stats = stats[:n]
	}
	return stats
}
