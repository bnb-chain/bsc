package vdn

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/log"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
)

const (
	defaultPubsubQueueSize = 600
	defaultMaxPeers        = 11
)

type Config struct {
	StaticPeers      []string // using multi address format
	BootstrapPeers   []string
	HostAddress      string // it indicates listen IP addr, it better a external IP for public service
	NodeKeyPath      string // the path saves the key and cached nodes
	QUICPort         int
	TCPPort          int
	MaxPeers         int
	QueueSize        int
	EnableQuic       bool
	EnableDiscovery  bool
	MinimumSyncPeers int
}

func (cfg *Config) SanityCheck() error {
	if cfg.QueueSize == 0 {
		log.Warn("Invalid pubsub queue size", "input", cfg.QueueSize, "default", defaultPubsubQueueSize)
		cfg.QueueSize = defaultPubsubQueueSize
	}
	if cfg.MaxPeers == 0 {
		log.Warn("Invalid MaxPeers size", "input", cfg.MaxPeers, "default", defaultMaxPeers)
		cfg.MaxPeers = defaultMaxPeers
	}
	if len(cfg.BootstrapPeers) == 0 && cfg.EnableDiscovery {
		log.Warn("Invalid BootstrapPeers, disable discovery")
		cfg.EnableDiscovery = false
	}
	return nil
}

func (cfg *Config) LoadPrivateKey() (peer.ID, *ecdsa.PrivateKey, error) {
	priv, err := LoadPrivateKey(cfg.NodeKeyPath)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to load node key")
	}
	ipriv, err := ConvertToInterfacePrivkey(priv)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to convert node key")
	}
	peerId, err := peer.IDFromPrivateKey(ipriv)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to parse peer id")
	}
	return peerId, priv, nil
}
