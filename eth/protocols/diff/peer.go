package diff

import (
	"math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
)

// Peer is a collection of relevant information we have about a `diff` peer.
type Peer struct {
	id        string // Unique ID for the peer, cached
	lightSync bool   // whether the peer can light sync

	*p2p.Peer                   // The embedded P2P package peer
	rw        p2p.MsgReadWriter // Input/output streams for diff
	version   uint              // Protocol version negotiated
	logger    log.Logger        // Contextual logger with the peer id injected
}

// newPeer create a wrapper for a network connection and negotiated  protocol
// version.
func newPeer(version uint, p *p2p.Peer, rw p2p.MsgReadWriter) *Peer {
	id := p.ID().String()
	return &Peer{
		id:        id,
		Peer:      p,
		rw:        rw,
		lightSync: false,
		version:   version,
		logger:    log.New("peer", id[:8]),
	}
}

// ID retrieves the peer's unique identifier.
func (p *Peer) ID() string {
	return p.id
}

// Version retrieves the peer's negoatiated `diff` protocol version.
func (p *Peer) Version() uint {
	return p.version
}

func (p *Peer) LightSync() bool {
	return p.lightSync
}

// Log overrides the P2P logget with the higher level one containing only the id.
func (p *Peer) Log() log.Logger {
	return p.logger
}

// RequestDiffLayers fetches a batch of diff layers corresponding to the hashes
// specified.
func (p *Peer) RequestDiffLayers(hashes []common.Hash) error {
	id := rand.Uint64()

	requestTracker.Track(p.id, p.version, GetDiffLayerMsg, DiffLayerMsg, id)
	return p2p.Send(p.rw, GetDiffLayerMsg, GetDiffLayersPacket{
		RequestId:   id,
		BlockHashes: hashes,
	})
}
