// Copyright 2019 The go-ethereum Authors
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

package discover

import (
	"crypto/ecdsa"
	"fmt"
	"net"

	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/p2p/netutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// UDPConn is a network connection on which discovery can operate.
type UDPConn interface {
	ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

type NodeFilterFunc func(*enr.Record) bool

func ParseEthFilter(chain string) (NodeFilterFunc, error) {
	var filter forkid.Filter
	switch chain {
	case "bsc":
		filter = forkid.NewStaticFilter(params.BSCChainConfig, params.BSCGenesisHash)
	case "chapel":
		filter = forkid.NewStaticFilter(params.ChapelChainConfig, params.ChapelGenesisHash)
	case "rialto":
		filter = forkid.NewStaticFilter(params.RialtoChainConfig, params.RialtoGenesisHash)
	default:
		return nil, fmt.Errorf("unknown network %q", chain)
	}

	f := func(r *enr.Record) bool {
		var eth struct {
			ForkID forkid.ID
			Tail   []rlp.RawValue `rlp:"tail"`
		}
		if r.Load(enr.WithEntry("eth", &eth)) != nil {
			return false
		}
		return filter(eth.ForkID) == nil
	}
	return f, nil
}

// Config holds settings for the discovery listener.
type Config struct {
	// These settings are required and configure the UDP listener:
	PrivateKey *ecdsa.PrivateKey

	// These settings are optional:
	NetRestrict    *netutil.Netlist   // list of allowed IP networks
	Bootnodes      []*enode.Node      // list of bootstrap nodes
	Unhandled      chan<- ReadPacket  // unhandled packets are sent on this channel
	Log            log.Logger         // if set, log messages go here
	ValidSchemes   enr.IdentityScheme // allowed identity schemes
	Clock          mclock.Clock
	FilterFunction NodeFilterFunc // function for filtering ENR entries
}

func (cfg Config) withDefaults() Config {
	if cfg.Log == nil {
		cfg.Log = log.Root()
	}
	if cfg.ValidSchemes == nil {
		cfg.ValidSchemes = enode.ValidSchemes
	}
	if cfg.Clock == nil {
		cfg.Clock = mclock.System{}
	}
	return cfg
}

// ListenUDP starts listening for discovery packets on the given UDP socket.
func ListenUDP(c UDPConn, ln *enode.LocalNode, cfg Config) (*UDPv4, error) {
	return ListenV4(c, ln, cfg)
}

// ReadPacket is a packet that couldn't be handled. Those packets are sent to the unhandled
// channel if configured.
type ReadPacket struct {
	Data []byte
	Addr *net.UDPAddr
}

func min(x, y int) int {
	if x > y {
		return y
	}
	return x
}
