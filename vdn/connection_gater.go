package vdn

import (
	"runtime"

	"github.com/ethereum/go-ethereum/log"

	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	// Limit for rate limiter when processing new inbound dials.
	ipLimit = 4

	// Burst limit for inbound dials.
	ipBurst = 8

	// High watermark buffer signifies the buffer till which
	// we will handle inbound requests.
	highWatermarkBuffer = 20
)

// InterceptPeerDial tests whether we're permitted to Dial the specified peer.
func (*Server) InterceptPeerDial(_ peer.ID) (allow bool) {
	return true
}

// InterceptAddrDial tests whether we're permitted to dial the specified
// multiaddr for the given peer.
func (s *Server) InterceptAddrDial(pid peer.ID, m multiaddr.Multiaddr) (allow bool) {
	// Disallow bad peers from dialing in.
	if s.watcher.IsBad(pid) != nil {
		return false
	}
	return true
}

// InterceptAccept checks whether the incidental inbound connection is allowed.
func (s *Server) InterceptAccept(n network.ConnMultiaddrs) (allow bool) {
	// Deny all incoming connections before we are ready
	if !s.started {
		return false
	}
	if !s.validateDial(n.RemoteMultiaddr()) {
		// Allow other go-routines to run in the event
		// we receive a large amount of junk connections.
		runtime.Gosched()
		log.Debug("peer exceeded dial limit, Not accepting inbound dial from ip address", "peer", n.RemoteMultiaddr())
		return false
	}
	if s.isPeerAtLimit(true /* inbound */) {
		log.Debug("peer at peer limit, Not accepting inbound dial", "peer", n.RemoteMultiaddr())
		return false
	}
	return true
}

// InterceptSecured tests whether a given connection, now authenticated,
// is allowed.
func (*Server) InterceptSecured(_ network.Direction, _ peer.ID, _ network.ConnMultiaddrs) (allow bool) {
	return true
}

// InterceptUpgraded tests whether a fully capable connection is allowed.
func (*Server) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

func (s *Server) validateDial(addr multiaddr.Multiaddr) bool {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return false
	}
	remaining := s.ipLimiter.Remaining(ip.String())
	if remaining <= 0 {
		return false
	}
	s.ipLimiter.Add(ip.String(), 1)
	return true
}

// This checks our set max peers in our config, and
// determines whether our currently connected and
// active peers are above our set max peer limit.
func (s *Server) isPeerAtLimit(inbound bool) bool {
	numOfConns := len(s.host.Network().Peers())
	maxPeers := s.cfg.MaxPeers
	// If we are measuring the limit for inbound peers
	// we apply the high watermark buffer.
	if inbound {
		maxPeers += highWatermarkBuffer
		maxInbound := s.watcher.InboundLimit() + highWatermarkBuffer
		currInbound := len(s.watcher.InboundConnected())
		// Exit early if we are at the inbound limit.
		if currInbound >= maxInbound {
			return true
		}
	}
	activePeers := len(s.watcher.Active())
	return activePeers >= maxPeers || numOfConns >= maxPeers
}
