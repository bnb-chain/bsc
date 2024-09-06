package eth

import (
	"container/heap"
	"sync"
	"time"
)

// Implements the heap.Interface for *BlackListPeer based on LastSeen
type PeerHeap []*BlackListPeer

func (h PeerHeap) Len() int           { return len(h) }
func (h PeerHeap) Less(i, j int) bool { return h[i].LastSeen.Before(h[j].LastSeen) }
func (h PeerHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }

func (h *PeerHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*BlackListPeer)
	item.index = n
	*h = append(*h, item)
}

func (h *PeerHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	item.index = -1 // for safety
	*h = old[0 : n-1]
	return item
}

// Peer represents the state of a peer in the network.
type BlackListPeer struct {
	ID             string    // Unique identifier for the peer
	HeadBlock      int64     // Current head block of the peer
	LastSeen       time.Time // Timestamp of the last head block update
	BlacklistCount int       // Counter for failed head block updates
	index          int       // Index of the peer in the heap
}

// blackList manages peers, both active and blacklisted.
type blackList struct {
	mu               sync.Mutex // To handle concurrent access
	peers            map[string]*BlackListPeer
	peerHeap         PeerHeap
	maxPeers         int
	expiryTime       time.Duration
	blacklistedCount int
}

// NewBlackList creates a new instance of blackList.
func NewBlackList(maxPeers int, expiryTime time.Duration, blacklistedCount int) *blackList {
	bl := &blackList{
		peers:            make(map[string]*BlackListPeer),
		peerHeap:         make(PeerHeap, 0, maxPeers),
		maxPeers:         maxPeers,
		expiryTime:       expiryTime,
		blacklistedCount: blacklistedCount,
	}
	heap.Init(&bl.peerHeap)
	return bl
}

// AddOrUpdatePeer adds or updates a peer in the map, rejecting invalid IDs.
func (bl *blackList) AddOrUpdatePeer(id string, headBlock int64) {
	if id == "" {
		return // Reject empty ID
	}

	bl.mu.Lock()
	defer bl.mu.Unlock()

	peer, exists := bl.peers[id]
	if exists {
		if peer.HeadBlock != headBlock {
			peer.HeadBlock = headBlock
			peer.LastSeen = time.Now()
			peer.BlacklistCount = 0
			heap.Fix(&bl.peerHeap, peer.index)
		}
	} else {
		if len(bl.peers) >= bl.maxPeers {
			oldest := heap.Pop(&bl.peerHeap).(*BlackListPeer)
			delete(bl.peers, oldest.ID) // Corrected to use ID
		}
		newPeer := &BlackListPeer{
			ID: id, HeadBlock: headBlock, LastSeen: time.Now(), BlacklistCount: 0,
		}
		bl.peers[id] = newPeer
		heap.Push(&bl.peerHeap, newPeer)
	}
}

// BlacklistStalePeers updates the blacklist count of stale peers.
func (bl *blackList) BlacklistStalePeers() {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	now := time.Now()
	for _, peer := range bl.peers {
		if now.Sub(peer.LastSeen) > bl.expiryTime {
			peer.BlacklistCount++
		}
	}
}

// IsBlacklisted checks if a peer is blacklisted.
func (bl *blackList) IsBlacklisted(id string) bool {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	peer, exists := bl.peers[id]
	return exists && peer.BlacklistCount >= bl.blacklistedCount
}
