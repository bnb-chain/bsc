package eth

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/bsc"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// bscHandler implements the bsc.Backend interface to handle the various network
// packets that are sent as broadcasts.
type bscHandler handler

func (h *bscHandler) Chain() *core.BlockChain { return h.chain }

// RunPeer is invoked when a peer joins on the `bsc` protocol.
func (h *bscHandler) RunPeer(peer *bsc.Peer, hand bsc.Handler) error {
	if err := peer.Handshake(); err != nil {
		// ensure that waitBscExtension receives the exit signal normally
		// otherwise, can't graceful shutdown
		ps := h.peers
		id := peer.ID()

		// Ensure nobody can double connect
		ps.lock.Lock()
		if wait, ok := ps.bscWait[id]; ok {
			delete(ps.bscWait, id)
			peer.Log().Error("Bsc extension Handshake failed", "err", err)
			wait <- nil
		}
		ps.lock.Unlock()
		return err
	}
	return (*handler)(h).runBscExtension(peer, hand)
}

// PeerInfo retrieves all known `bsc` information about a peer.
func (h *bscHandler) PeerInfo(id enode.ID) interface{} {
	if p := h.peers.peer(id.String()); p != nil && p.bscExt != nil {
		return p.bscExt.info()
	}
	return nil
}

// Handle is invoked from a peer's message handler when it receives a new remote
// message that the handler couldn't consume and serve itself.
func (h *bscHandler) Handle(peer *bsc.Peer, packet bsc.Packet) error {
	// DeliverSnapPacket is invoked from a peer's message handler when it transmits a
	// data packet for the local node to consume.
	switch packet := packet.(type) {
	case *bsc.VotesPacket:
		return h.handleVotesBroadcast(peer, packet.Votes)

	default:
		return fmt.Errorf("unexpected bsc packet type: %T", packet)
	}
}

// handleVotesBroadcast is invoked from a peer's message handler when it transmits a
// votes broadcast for the local node to process.
func (h *bscHandler) handleVotesBroadcast(peer *bsc.Peer, votes []*types.VoteEnvelope) error {
	// Try to put votes into votepool
	for _, vote := range votes {
		h.votepool.PutVote(vote)
	}
	return nil
}
