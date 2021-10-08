package diff

import (
	"math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

const maxQueuedDiffLayers = 12

// Peer is a collection of relevant information we have about a `diff` peer.
type Peer struct {
	id               string              // Unique ID for the peer, cached
	diffSync         bool                // whether the peer can diff sync
	queuedDiffLayers chan []rlp.RawValue // Queue of diff layers to broadcast to the peer

	*p2p.Peer                   // The embedded P2P package peer
	rw        p2p.MsgReadWriter // Input/output streams for diff
	version   uint              // Protocol version negotiated
	logger    log.Logger        // Contextual logger with the peer id injected
	term      chan struct{}     // Termination channel to stop the broadcasters
}

// NewPeer create a wrapper for a network connection and negotiated  protocol
// version.
func NewPeer(version uint, p *p2p.Peer, rw p2p.MsgReadWriter) *Peer {
	id := p.ID().String()
	peer := &Peer{
		id:               id,
		Peer:             p,
		rw:               rw,
		diffSync:         false,
		version:          version,
		logger:           log.New("peer", id[:8]),
		queuedDiffLayers: make(chan []rlp.RawValue, maxQueuedDiffLayers),
		term:             make(chan struct{}),
	}
	go peer.broadcastDiffLayers()
	return peer
}

func (p *Peer) broadcastDiffLayers() {
	for {
		select {
		case prop := <-p.queuedDiffLayers:
			if err := p.SendDiffLayers(prop); err != nil {
				p.Log().Error("Failed to propagated diff layer", "err", err)
				return
			}
		case <-p.term:
			return
		}
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

func (p *Peer) DiffSync() bool {
	return p.diffSync
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

// RequestDiffLayers fetches a batch of diff layers corresponding to the hashes
// specified.
func (p *Peer) RequestDiffLayers(hashes []common.Hash) error {
	id := rand.Uint64()

	requestTracker.Track(p.id, p.version, GetDiffLayerMsg, FullDiffLayerMsg, id)
	return p2p.Send(p.rw, GetDiffLayerMsg, GetDiffLayersPacket{
		RequestId:   id,
		BlockHashes: hashes,
	})
}

func (p *Peer) SendDiffLayers(diffs []rlp.RawValue) error {
	return p2p.Send(p.rw, DiffLayerMsg, diffs)
}

func (p *Peer) AsyncSendDiffLayer(diffLayers []rlp.RawValue) {
	select {
	case p.queuedDiffLayers <- diffLayers:
	default:
		p.Log().Debug("Dropping diff layers propagation")
	}
}
