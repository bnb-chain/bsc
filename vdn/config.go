package vdn

import (
	"github.com/ethereum/go-ethereum/log"
)

const (
	defaultPubsubQueueSize = 600
	defaultMaxPeers        = 11
)

type Config struct {
	StaticPeers      []string // using multi address format
	BootStrapAddrs   []string
	HostAddress      string // it indicates listen IP addr, it better a external IP for public service
	PrivateKeyPath   string // the path saves the key and cached nodes
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
		log.Warn("Invalid pubsub queue size of %d, set %d", cfg.QueueSize, defaultPubsubQueueSize)
		cfg.QueueSize = defaultPubsubQueueSize
	}
	if cfg.MaxPeers == 0 {
		log.Warn("Invalid MaxPeers size of %d, set %d", cfg.QueueSize, defaultMaxPeers)
		cfg.MaxPeers = defaultMaxPeers
	}
	if len(cfg.BootStrapAddrs) == 0 && cfg.EnableDiscovery {
		log.Warn("Invalid BootStrapAddrs, disable discovery")
		cfg.EnableDiscovery = false
	}
	return nil
}
