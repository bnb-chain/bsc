package bsc

import (
	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
)

const (
	// maxKnownVotes is the maximum vote hashes to keep in the known list
	// before starting to randomly evict them.
	maxKnownVotes = 5376
)

// max is a helper function which returns the larger of the two given integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Peer is a collection of relevant information we have about a `bsc` peer.
type Peer struct {
	id            string                     // Unique ID for the peer, cached
	knownVotes    *knownCache                // Set of vote hashes known to be known by this peer
	voteBroadcast chan []*types.VoteEnvelope // Channel used to queue votes propagation requests

	*p2p.Peer                   // The embedded P2P package peer
	rw        p2p.MsgReadWriter // Input/output streams for bsc
	version   uint              // Protocol version negotiated
	logger    log.Logger        // Contextual logger with the peer id injected
	term      chan struct{}     // Termination channel to stop the broadcasters
}

// NewPeer create a wrapper for a network connection and negotiated protocol
// version.
func NewPeer(version uint, p *p2p.Peer, rw p2p.MsgReadWriter) *Peer {
	id := p.ID().String()
	peer := &Peer{
		id:            id,
		knownVotes:    newKnownCache(maxKnownVotes),
		voteBroadcast: make(chan []*types.VoteEnvelope),
		Peer:          p,
		rw:            rw,
		version:       version,
		logger:        log.New("peer", id[:8]),
		term:          make(chan struct{}),
	}
	go peer.broadcastVotes()
	return peer
}

// ID retrieves the peer's unique identifier.
func (p *Peer) ID() string {
	return p.id
}

// Version retrieves the peer's negotiated `bsc` protocol version.
func (p *Peer) Version() uint {
	return p.version
}

// Log overrides the P2P logget with the higher level one containing only the id.
func (p *Peer) Log() log.Logger {
	return p.logger
}

// Close signals the broadcast goroutine to terminate. Only ever call this if
// you created the peer yourself via NewPeer. Otherwise let whoever created it
// clean it up!
func (p *Peer) Close() {
	close(p.term)
}

// KnownVote returns whether peer is known to already have a vote.
func (p *Peer) KnownVote(hash common.Hash) bool {
	return p.knownVotes.contains(hash)
}

// markVotes marks votes as known for the peer, ensuring that they
// will never be repropagated to this particular peer.
func (p *Peer) markVotes(votes []*types.VoteEnvelope) {
	for _, vote := range votes {
		if !p.knownVotes.contains(vote.Hash()) {
			// If we reached the memory allowance, drop a previously known vote hash
			p.knownVotes.add(vote.Hash())
		}
	}
}

// sendVotes propagates a batch of votes to the remote peer.
func (p *Peer) sendVotes(votes []*types.VoteEnvelope) error {
	// Mark all the votes as known, but ensure we don't overflow our limits
	p.markVotes(votes)
	return p2p.Send(p.rw, VotesMsg, &VotesPacket{votes})
}

// AsyncSendVotes queues a batch of vote hashes for propagation to a remote peer. If
// the peer's broadcast queue is full, the event is silently dropped.
func (p *Peer) AsyncSendVotes(votes []*types.VoteEnvelope) {
	select {
	case p.voteBroadcast <- votes:
	case <-p.term:
		p.Log().Debug("Dropping vote propagation", "count", len(votes))
	}
}

// broadcastVotes is a write loop that schedules votes broadcasts
// to the remote peer. The goal is to have an async writer that does not lock up
// node internals and at the same time rate limits queued data.
func (p *Peer) broadcastVotes() {
	for {
		select {
		case votes := <-p.voteBroadcast:
			if err := p.sendVotes(votes); err != nil {
				return
			}
			p.Log().Trace("Sent votes", "count", len(votes))

		case <-p.term:
			return
		}
	}
}

// knownCache is a cache for known hashes.
type knownCache struct {
	hashes mapset.Set
	max    int
}

// newKnownCache creates a new knownCache with a max capacity.
func newKnownCache(max int) *knownCache {
	return &knownCache{
		max:    max,
		hashes: mapset.NewSet(),
	}
}

// add adds a list of elements to the set.
func (k *knownCache) add(hashes ...common.Hash) {
	for k.hashes.Cardinality() > max(0, k.max-len(hashes)) {
		k.hashes.Pop()
	}
	for _, hash := range hashes {
		k.hashes.Add(hash)
	}
}

// contains returns whether the given item is in the set.
func (k *knownCache) contains(hash common.Hash) bool {
	return k.hashes.Contains(hash)
}
