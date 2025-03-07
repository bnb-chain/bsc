package vdn

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	leakybucket "github.com/prysmaticlabs/prysm/v5/container/leaky-bucket"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/pkg/errors"
)

var Rendezvous = "bsc-validator-rendezvous"

type BootstrapServer struct {
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	cfg     *Config
	privKey *ecdsa.PrivateKey

	host      host.Host
	dht       *dht.IpfsDHT
	ipLimiter *leakybucket.Collector
}

// CreateBootstrapServer builds a Server with the Rendezvous approach
// but does NOT automatically start/advertise. Call `Start()` after.
func CreateBootstrapServer(cfg *Config) (*BootstrapServer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Load your ECDSA key (or however you handle it)
	priv, err := LoadPrivateKey(cfg.NodeKeyPath)
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "LoadPrivateKey")
	}

	ipLimiter := leakybucket.NewCollector(ipLimit, ipBurst, 30*time.Second, true /* deleteEmptyBuckets */)

	s := &BootstrapServer{
		ctx:       ctx,
		cancel:    cancel,
		cfg:       cfg,
		privKey:   priv,
		ipLimiter: ipLimiter,
	}

	opts, err := s.buildOptions()
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "buildOptions")
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "create p2p host")
	}

	s.host = h
	return s, nil
}

// Start boots the DHT, connects to bootstrap peers,
// advertises the rendezvous, and begins discovering peers.
func (s *BootstrapServer) Start() {
	if s.started {
		log.Warn("Server already started; skipping Start()")
		return
	}
	s.started = true
	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.ProtocolPrefix("/bsc/validator/disc"),
	}
	s.dht, _ = dht.New(s.ctx, s.host, dhtOpts...)

	// Now bootstrap the DHT, which also starts its background refresh loop.
	if err := s.dht.Bootstrap(s.ctx); err != nil {
		log.Error("DHT bootstrap failed", "err", err)
	}

	// Set up the discovery interface
	routingDiscovery := drouting.NewRoutingDiscovery(s.dht)
	// Advertise ourselves under the Rendezvous string
	log.Info("Advertising Rendezvous", "topic", Rendezvous)
	dutil.Advertise(s.ctx, routingDiscovery, Rendezvous)
	log.Info("Rendezvous p2p server started", "peerID", s.host.ID(), "addrs", s.host.Addrs())
}

// Stop shuts down the server (cancels context, closes DHT).
func (s *BootstrapServer) Stop() {
	if !s.started {
		return
	}
	s.started = false

	log.Info("Stopping Rendezvous server...", "peerID", s.host.ID())

	// Cancel the context so all background tasks (FindPeers loop, etc.) will end.
	s.cancel()

	if s.dht != nil {
		_ = s.dht.Close()
	}
}

// buildOptions sets up the basic libp2p options: addresses, key, etc.
func (s *BootstrapServer) buildOptions() ([]libp2p.Option, error) {
	ipAddr := net.ParseIP(s.cfg.HostAddress)
	if ipAddr == nil {
		return nil, fmt.Errorf("invalid HostAddress: %s", s.cfg.HostAddress)
	}

	var ipType string
	if ipAddr.To4() != nil {
		ipType = "ip4"
	} else if ipAddr.To16() != nil {
		ipType = "ip6"
	} else {
		return nil, fmt.Errorf("unsupported IP address: %s", s.cfg.HostAddress)
	}

	// Example: /ip4/0.0.0.0/tcp/3000
	multiAddrTCP, err := multiaddr.NewMultiaddr(
		fmt.Sprintf("/%s/%s/tcp/%d", ipType, ipAddr, s.cfg.TCPPort),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "error creating TCP multiaddr")
	}
	multiaddrs := []multiaddr.Multiaddr{multiAddrTCP}

	// Convert ECDSA to libp2p's crypto interface
	ifaceKey, err := ConvertToInterfacePrivkey(s.privKey)
	if err != nil {
		return nil, errors.Wrapf(err, "ConvertToInterfacePrivkey fail")
	}

	id, err := peer.IDFromPrivateKey(ifaceKey)
	if err != nil {
		return nil, errors.Wrapf(err, "IDFromPrivateKey fail")
	}

	log.Info("Setting up libp2p host", "peerID", id)

	// Build base set of libp2p options
	opts := []libp2p.Option{
		libp2p.Identity(ifaceKey),
		libp2p.ListenAddrs(multiaddrs...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DisableRelay(),
	}
	//if multiaddr is local we disable conn gater
	if manet.IsPublicAddr(multiAddrTCP) {
		opts = append(opts, libp2p.ConnectionGater(s))
	}
	return opts, nil
}

// InterceptPeerDial tests whether we're permitted to Dial the specified peer.
func (*BootstrapServer) InterceptPeerDial(_ peer.ID) (allow bool) {
	return true
}

// InterceptAddrDial tests whether we're permitted to dial the specified
// multiaddr for the given peer.
func (s *BootstrapServer) InterceptAddrDial(_ peer.ID, _ multiaddr.Multiaddr) (allow bool) {
	return true
}

// InterceptAccept checks whether the incidental inbound connection is allowed.
func (s *BootstrapServer) InterceptAccept(n network.ConnMultiaddrs) (allow bool) {
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
func (*BootstrapServer) InterceptSecured(_ network.Direction, _ peer.ID, _ network.ConnMultiaddrs) (allow bool) {
	return true
}

// InterceptUpgraded tests whether a fully capable connection is allowed.
func (*BootstrapServer) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

func (s *BootstrapServer) validateDial(addr multiaddr.Multiaddr) bool {
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
func (s *BootstrapServer) isPeerAtLimit(_ bool) bool {
	numOfConns := len(s.host.Network().Peers())
	maxPeers := s.cfg.MaxPeers
	return numOfConns >= maxPeers
}
