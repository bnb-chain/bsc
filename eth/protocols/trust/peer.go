package trust

import (
	"math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
)

// Peer is a collection of relevant information we have about a `trust` peer.
type Peer struct {
	id string // Unique ID for the peer, cached

	*p2p.Peer                   // The embedded P2P package peer
	rw        p2p.MsgReadWriter // Input/output streams for diff
	version   uint              // Protocol version negotiated
	logger    log.Logger        // Contextual logger with the peer id injected
}

// NewPeer create a wrapper for a network connection and negotiated  protocol
// version.
func NewPeer(version uint, p *p2p.Peer, rw p2p.MsgReadWriter) *Peer {
	id := p.ID().String()
	peer := &Peer{
		id:      id,
		Peer:    p,
		rw:      rw,
		version: version,
		logger:  log.New("peer", id[:8]),
	}
	return peer
}

// ID retrieves the peer's unique identifier.
func (p *Peer) ID() string {
	return p.id
}

// Version retrieves the peer's negoatiated `diff` protocol version.
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
}

func (p *Peer) RequestRoot(blockNumber uint64, blockHash common.Hash, diffHash common.Hash) error {
	id := rand.Uint64()

	requestTracker.Track(p.id, p.version, RequestRootMsg, RespondRootMsg, id)
	return p2p.Send(p.rw, RequestRootMsg, RootRequestPacket{
		RequestId:   id,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		DiffHash:    diffHash,
	})
}
