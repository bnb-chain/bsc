// Copyright 2020 The go-ethereum Authors
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

package eth

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
)

const (
	// handshakeTimeout is the maximum allowed time for the `eth` handshake to
	// complete before dropping the connection.= as malicious.
	handshakeTimeout = 5 * time.Second
)

// Handshake executes the eth protocol handshake, negotiating version number,
// network IDs, difficulties, head and genesis blocks.
func (p *Peer) Handshake(network uint64, td *big.Int, head common.Hash, genesis common.Hash, forkID forkid.ID, forkFilter forkid.Filter, extension *UpgradeStatusExtension) error {
	// Send out own handshake in a new thread
	errc := make(chan error, 2)

	var status StatusPacket // safe to read after two values have been received from errc

	gopool.Submit(func() {
		errc <- p2p.Send(p.rw, StatusMsg, &StatusPacket{
			ProtocolVersion: uint32(p.version),
			NetworkID:       network,
			TD:              td,
			Head:            head,
			Genesis:         genesis,
			ForkID:          forkID,
		})
	})
	gopool.Submit(func() {
		errc <- p.readStatus(network, &status, genesis, forkFilter)
	})
	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				markError(p, err)
				return err
			}
		case <-timeout.C:
			markError(p, p2p.DiscReadTimeout)
			return p2p.DiscReadTimeout
		}
	}
	p.td, p.head = status.TD, status.Head

	if p.version >= ETH67 {
		var upgradeStatus UpgradeStatusPacket // safe to read after two values have been received from errc
		if extension == nil {
			extension = &UpgradeStatusExtension{}
		}
		extensionRaw, err := extension.Encode()
		if err != nil {
			return err
		}

		gopool.Submit(func() {
			errc <- p2p.Send(p.rw, UpgradeStatusMsg, &UpgradeStatusPacket{
				Extension: extensionRaw,
			})
		})
		gopool.Submit(func() {
			errc <- p.readUpgradeStatus(&upgradeStatus)
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

		extension, err := upgradeStatus.GetExtension()
		if err != nil {
			return err
		}
		p.statusExtension = extension

		if p.statusExtension.DisablePeerTxBroadcast {
			p.Log().Debug("peer does not need broadcast txs, closing broadcast routines")
			p.CloseTxBroadcast()
		}
	}

	// TD at mainnet block #7753254 is 76 bits. If it becomes 100 million times
	// larger, it will still fit within 100 bits
	if tdlen := p.td.BitLen(); tdlen > 100 {
		return fmt.Errorf("too large total difficulty: bitlen %d", tdlen)
	}
	return nil
}

// readStatus reads the remote handshake message.
func (p *Peer) readStatus(network uint64, status *StatusPacket, genesis common.Hash, forkFilter forkid.Filter) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return fmt.Errorf("%w: first msg has code %x (!= %x)", errNoStatusMsg, msg.Code, StatusMsg)
	}
	if msg.Size > maxMessageSize {
		return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	}
	// Decode the handshake and make sure everything matches
	if err := msg.Decode(&status); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	if status.NetworkID != network {
		return fmt.Errorf("%w: %d (!= %d)", errNetworkIDMismatch, status.NetworkID, network)
	}
	if uint(status.ProtocolVersion) != p.version {
		return fmt.Errorf("%w: %d (!= %d)", errProtocolVersionMismatch, status.ProtocolVersion, p.version)
	}
	if status.Genesis != genesis {
		return fmt.Errorf("%w: %x (!= %x)", errGenesisMismatch, status.Genesis, genesis)
	}
	if err := forkFilter(status.ForkID); err != nil {
		return fmt.Errorf("%w: %v", errForkIDRejected, err)
	}
	return nil
}

func (p *Peer) readUpgradeStatus(status *UpgradeStatusPacket) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != UpgradeStatusMsg {
		return fmt.Errorf("%w: upgrade status msg has code %x (!= %x)", errNoStatusMsg, msg.Code, UpgradeStatusMsg)
	}
	if msg.Size > maxMessageSize {
		return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	}
	if err := msg.Decode(&status); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	return nil
}

// markError registers the error with the corresponding metric.
func markError(p *Peer, err error) {
	if !metrics.Enabled {
		return
	}
	m := meters.get(p.Inbound())
	switch errors.Unwrap(err) {
	case errNetworkIDMismatch:
		m.networkIDMismatch.Mark(1)
	case errProtocolVersionMismatch:
		m.protocolVersionMismatch.Mark(1)
	case errGenesisMismatch:
		m.genesisMismatch.Mark(1)
	case errForkIDRejected:
		m.forkidRejected.Mark(1)
	case p2p.DiscReadTimeout:
		m.timeoutError.Mark(1)
	default:
		m.peerError.Mark(1)
	}
}
