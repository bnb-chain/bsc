package vdn

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"

	"github.com/ethereum/go-ethereum/log"
	"github.com/libp2p/go-libp2p"
	mplex "github.com/libp2p/go-libp2p-mplex"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	leakybucket "github.com/prysmaticlabs/prysm/v5/container/leaky-bucket"
)

// Server validator dedicated p2p server
type Server struct {
	started          bool
	ctx              context.Context
	cancel           context.CancelFunc
	cfg              *Config
	host             host.Host
	dht              *kaddht.IpfsDHT
	watcher          *PeerWatcher
	ipLimiter        *leakybucket.Collector
	peerID           peer.ID
	privKey          *ecdsa.PrivateKey
	pubsub           *pubsub.PubSub
	joinedTopics     map[string]*pubsub.Topic
	joinedTopicsLock sync.RWMutex

	bootPeerInfo   map[peer.ID]peer.AddrInfo
	staticPeerInfo map[peer.ID]peer.AddrInfo
}

func NewServer(cfg *Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // govet fix for lost cancel. Cancel is handled in service.Stop().

	if err := cfg.SanityCheck(); err != nil {
		return nil, err
	}
	peerID, priv, err := cfg.LoadPrivateKey()
	if err != nil {
		return nil, errors.Wrap(err, "LoadPrivateKey err")
	}

	ipLimiter := leakybucket.NewCollector(ipLimit, ipBurst, 30*time.Second, true /* deleteEmptyBuckets */)

	s := &Server{
		ctx:            ctx,
		cancel:         cancel,
		cfg:            cfg,
		ipLimiter:      ipLimiter,
		peerID:         peerID,
		privKey:        priv,
		joinedTopics:   make(map[string]*pubsub.Topic, 8),
		bootPeerInfo:   make(map[peer.ID]peer.AddrInfo, 8),
		staticPeerInfo: make(map[peer.ID]peer.AddrInfo, 8),
	}

	// setup libp2p instance
	opts, err := s.buildOptions()
	if err != nil {
		return nil, errors.Wrap(err, "buildOptions err")
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "create p2p host err")
	}
	s.host = h

	// setup Gossipsub immediately
	psOpts := s.pubsubOptions()
	// We have to unfortunately set this globally in order
	// to configure our message id time-cache rather than instantiating
	// it with a router instance.
	pubsub.TimeCacheDuration = 2 * oneEpochDuration()

	gs, err := pubsub.NewGossipSub(s.ctx, s.host, psOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create pubsub")
	}
	s.pubsub = gs

	// setup peer watcher
	watcher, err := NewPeerWatcher(&WatcherConfig{
		PeerLimit:            cfg.MaxPeers,
		BadRespThreshold:     defaultBadRespThreshold,
		BadRespDecayInterval: defaultBadRespDecayInterval,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create peer watcher")
	}
	s.watcher = watcher
	return s, nil
}

func (s *Server) Start() {
	if s.started {
		log.Error("VDN Server already started, skip...")
		return
	}

	// setup discovery
	bootPeers := s.connectPeersFromAddr(s.cfg.BootstrapPeers)
	for _, p := range bootPeers {
		s.host.ConnManager().Protect(p.ID, "bootnode")
		s.bootPeerInfo[p.ID] = p
	}

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.ProtocolPrefix("/bsc/validator/disc"),
	}

	var err error
	s.dht, err = dht.New(s.ctx, s.host, dhtOpts...)
	if err != nil {
		log.Error("Failed to create DHT", "err", err)
		return
	}

	// Now bootstrap the DHT, which also starts its background refresh loop.
	if err := s.dht.Bootstrap(s.ctx); err != nil {
		log.Error("DHT bootstrap failed", "err", err)
		return
	}

	// Set up the discovery interface
	routingDiscovery := drouting.NewRoutingDiscovery(s.dht)
	// Advertise ourselves under the Rendezvous string
	log.Info("Advertising Rendezvous", "topic", Rendezvous)
	dutil.Advertise(s.ctx, routingDiscovery, Rendezvous)

	// Also, find peers in the background
	go func() {
		log.Info("Searching for peers for rendezvous", "topic", Rendezvous)
		peerChan, err := routingDiscovery.FindPeers(s.ctx, Rendezvous)
		if err != nil {
			log.Error("Failed to find peers via discovery", "err", err)
			return
		}
		for {
			select {
			case <-s.ctx.Done():
				log.Info("Context cancelled, stopping peer discovery")
				return
			case discovered, ok := <-peerChan:
				if !ok {
					return
				}
				if discovered.ID == s.host.ID() {
					continue // skip self
				}

				log.Info("Discovered peer", "id", discovered.ID, "addrs", discovered.Addrs)
				// Optionally attempt a connection
				if err := s.host.Connect(s.ctx, discovered); err != nil {
					log.Warn("Failed to connect to discovered peer", "id", discovered.ID, "err", err)
				} else {
					log.Info("Connected to discovered peer", "id", discovered.ID)
				}
			}
		}
	}()

	log.Info("Rendezvous p2p server started", "peerID", s.host.ID(), "addrs", s.host.Addrs())

	s.started = true

	staticPeers := s.connectPeersFromAddr(s.cfg.StaticPeers)
	for _, p := range staticPeers {
		// TODO(galaio): set trust peer
		//s.watcher.SetTrustedPeers(p.ID)
		s.staticPeerInfo[p.ID] = p
	}

	// Periodic functions.
	// TODO(galaio): add more Periodic logic
	// 1. retry connect static peers
	// 2. prune peer in every 30 minutes
	// 3. metrics updates, report connect peers, inbound, outbound, tcp, quic
	go s.eventLoop()
	listenAddrs := s.host.Network().ListenAddresses()
	log.Info("VDN Server started at:", "addrs", listenAddrs)
}

func (s *Server) connectPeersFromAddr(addrs []string) []peer.AddrInfo {
	parsedAddrs, err := ParsePeersAddr(addrs)
	if err != nil {
		log.Error("fail to parse boot strap addr")
		return nil
	}

	var successed []peer.AddrInfo
	for _, addr := range parsedAddrs {
		err := s.host.Connect(context.Background(), addr)
		if err != nil {
			log.Warn("cannot connect the boot node", "addr", addr)
		}
		successed = append(successed, addr)
	}
	return successed
}

func (s *Server) Stop() {
	defer s.cancel()
	s.started = false
	if s.dht != nil {
		s.dht.Close()
	}
}

func (s *Server) buildOptions() ([]libp2p.Option, error) {
	ipAddr := net.ParseIP(s.cfg.HostAddress)
	var ipType string
	if ipAddr.To4() != nil {
		ipType = "ip4"
	} else if ipAddr.To16() != nil {
		ipType = "ip6"
	} else {
		return nil, errors.New("unsupported ip address")
	}

	// Example: /ip4/1.2.3.4./tcp/5678
	multiAddrTCP, err := multiaddr.NewMultiaddr(fmt.Sprintf("/%s/%s/tcp/%d", ipType, ipAddr, s.cfg.TCPPort))
	if err != nil {
		return nil, errors.Wrapf(err, "NewMultiaddr fail from %s:%d", ipAddr, s.cfg.TCPPort)
	}
	multiaddrs := []multiaddr.Multiaddr{multiAddrTCP}
	if s.cfg.EnableQuic {
		// Example: /ip4/1.2.3.4/udp/5678/quic-v1
		multiAddrQUIC, err := multiaddr.NewMultiaddr(fmt.Sprintf("/%s/%s/udp/%d/quic-v1", ipType, ipAddr, s.cfg.QUICPort))
		if err != nil {
			return nil, errors.Wrapf(err, "QUIC NewMultiaddr fail from %s:%d", ipAddr, s.cfg.QUICPort)
		}

		multiaddrs = append(multiaddrs, multiAddrQUIC)
	}
	log.Info("configure VDN Server", "peerID", s.peerID)
	options := []libp2p.Option{
		privKeyOption(s.privKey),
		libp2p.ListenAddrs(multiaddrs...),
		libp2p.UserAgent("bsc-vdb-p2p"),
		libp2p.ConnectionGater(s),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.DefaultMuxers,
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DisableRelay(), // Disable relay transport, just connect directly
	}

	if s.cfg.EnableQuic {
		options = append(options, libp2p.Transport(libp2pquic.NewTransport))
	}

	// TODO(galaio): confirm ResourceManager
	//if disableResourceManager {
	//	options = append(options, libp2p.ResourceManager(&network.NullResourceManager{}))
	//}

	return options, nil
}

// Started returns true if the p2p service has successfully started.
func (s *Server) Started() bool {
	return s.started
}

// PubSub returns the p2p pubsub framework.
func (s *Server) PubSub() *pubsub.PubSub {
	return s.pubsub
}

// Host returns the currently running libp2p
// host of the service.
func (s *Server) Host() host.Host {
	return s.host
}

// PeerID returns the Peer ID of the local peer.
func (s *Server) PeerID() peer.ID {
	return s.host.ID()
}

// Disconnect from a peer.
func (s *Server) Disconnect(pid peer.ID) error {
	return s.host.Network().ClosePeer(pid)
}

// Connect to a specific peer.
func (s *Server) Connect(pi peer.AddrInfo) error {
	return s.host.Connect(s.ctx, pi)
}

func (s *Server) eventLoop() {
	reportTicker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-reportTicker.C:
			peers := s.host.Network().Peers()
			var statics []peer.ID
			for id := range s.staticPeerInfo {
				if slices.Contains(peers, id) {
					statics = append(statics, id)
				}
			}
			var boots []peer.ID
			for id := range s.bootPeerInfo {
				if slices.Contains(peers, id) {
					boots = append(boots, id)
				}
			}
			log.Info("Count VDN peers", "peers", len(peers), "bootnodes", len(boots), "static", len(statics))
			// TODO(galaio): may remove below logs later.
			for _, id := range peers {
				log.Debug("VDN connect to peer", "peerID", id, "addr", s.host.Network().Peerstore().Addrs(id))
			}
		case <-s.ctx.Done():
			log.Debug("VDN stopped, exit the event loop")
			return
		}
	}
}

func privKeyOption(privkey *ecdsa.PrivateKey) libp2p.Option {
	return func(cfg *libp2p.Config) error {
		ifaceKey, err := ConvertToInterfacePrivkey(privkey)
		if err != nil {
			return err
		}
		return cfg.Apply(libp2p.Identity(ifaceKey))
	}
}
