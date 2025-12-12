package bsc

import (
	"github.com/ethereum/go-ethereum/p2p"
)

// SendBscCap sends the bsc capability message to the peer asynchronously.
// This is for backward compatibility with old nodes that expect a handshake.
// We send the message but don't wait for a response.
func (p *Peer) SendBscCap() {
	// Send capability message asynchronously for backward compatibility.
	// Old nodes expect this message to complete their handshake.
	// New nodes will ignore it via handleBscCap in handler.go.
	go func() {
		if err := p2p.Send(p.rw, BscCapMsg, &BscCapPacket{
			ProtocolVersion: p.version,
			Extra:           defaultExtra,
		}); err != nil {
			p.Log().Debug("Failed to send bsc capability message", "err", err)
		}
	}()
}
