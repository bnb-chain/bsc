package eth

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/protocols/trust"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// trustHandler implements the trust.Backend interface to handle the various network
// packets that are sent as replies or broadcasts.
type trustHandler handler

func (h *trustHandler) Chain() *core.BlockChain { return h.chain }

// RunPeer is invoked when a peer joins on the `snap` protocol.
func (h *trustHandler) RunPeer(peer *trust.Peer, hand trust.Handler) error {
	return (*handler)(h).runTrustExtension(peer, hand)
}

// PeerInfo retrieves all known `trust` information about a peer.
func (h *trustHandler) PeerInfo(id enode.ID) interface{} {
	if p := h.peers.peer(id.String()); p != nil {
		if p.trustExt != nil {
			return p.trustExt.info()
		}
	}
	return nil
}

// Handle is invoked from a peer's message handler when it receives a new remote
// message that the handler couldn't consume and serve itself.
func (h *trustHandler) Handle(peer *trust.Peer, packet trust.Packet) error {
	switch packet := packet.(type) {
	case *trust.RootResponsePacket:
		verifyResult := &core.VerifyResult{
			Status:      packet.Status,
			BlockNumber: packet.BlockNumber,
			BlockHash:   packet.BlockHash,
			Root:        packet.Root,
		}
		if vm := h.Chain().Validator().RemoteVerifyManager(); vm != nil {
			vm.HandleRootResponse(verifyResult, peer.ID())
			return nil
		}
		return fmt.Errorf("verify manager is nil which is unexpected")

	default:
		return fmt.Errorf("unexpected trust packet type: %T", packet)
	}
}
