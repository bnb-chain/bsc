// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package diff

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/p2p"
)

const (
	// handshakeTimeout is the maximum allowed time for the `diff` handshake to
	// complete before dropping the connection as malicious.
	handshakeTimeout = 5 * time.Second
)

// Handshake executes the diff protocol handshake,
func (p *Peer) Handshake(diffSync bool) error {
	// Send out own handshake in a new thread
	errc := make(chan error, 2)

	var cap DiffCapPacket // safe to read after two values have been received from errc

	gopool.Submit(func() {
		errc <- p2p.Send(p.rw, DiffCapMsg, &DiffCapPacket{
			DiffSync: diffSync,
			Extra:    defaultExtra,
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
	p.diffSync = cap.DiffSync
	return nil
}

// readStatus reads the remote handshake message.
func (p *Peer) readCap(cap *DiffCapPacket) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != DiffCapMsg {
		return fmt.Errorf("%w: first msg has code %x (!= %x)", errNoCapMsg, msg.Code, DiffCapMsg)
	}
	if msg.Size > maxMessageSize {
		return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	}
	// Decode the handshake and make sure everything matches
	if err := msg.Decode(cap); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	return nil
}
