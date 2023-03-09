package bsc

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/p2p"
)

const (
	// handshakeTimeout is the maximum allowed time for the `bsc` handshake to
	// complete before dropping the connection as malicious.
	handshakeTimeout = 5 * time.Second
)

// Handshake executes the bsc protocol handshake,
func (p *Peer) Handshake() error {
	// Send out own handshake in a new thread
	errc := make(chan error, 2)

	var cap BscCapPacket // safe to read after two values have been received from errc

	gopool.Submit(func() {
		errc <- p2p.Send(p.rw, BscCapMsg, &BscCapPacket{
			ProtocolVersion: p.version,
			Extra:           defaultExtra,
		})
	})
	gopool.Submit(func() {
		errc <- p.readCap(&cap)
	})
	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}
		case <-timeout.C:
			return p2p.DiscReadTimeout
		}
	}
	return nil
}

// readCap reads the remote handshake message.
func (p *Peer) readCap(cap *BscCapPacket) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != BscCapMsg {
		return fmt.Errorf("%w: first msg has code %x (!= %x)", errNoBscCapMsg, msg.Code, BscCapMsg)
	}
	if msg.Size > maxMessageSize {
		return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	}
	// Decode the handshake and make sure everything matches
	if err := msg.Decode(cap); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	if cap.ProtocolVersion != p.version {
		return fmt.Errorf("%w: %d (!= %d)", errProtocolVersionMismatch, cap.ProtocolVersion, p.version)
	}
	return nil
}
