package vdn

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/exp/maps"
)

const (
	// BadRespThreshold is the maximum number of bad responses from a peer before we stop talking to it.
	defaultBadRespThreshold     = 5
	defaultBadRespDecayInterval = time.Hour

	// InboundRatio is the proportion of our connected peer limit at which we will allow inbound peers.
	InboundRatio = float64(0.8)
)

type WatcherConfig struct {
	// PeerLimit specifies maximum amount of concurrent peers.
	PeerLimit int
	// Threshold specifies number of bad responses tolerated, before peer is banned.
	BadRespThreshold int
	// DecayInterval specifies how often bad response stats should be decayed.
	BadRespDecayInterval time.Duration
}

type PeerWatcher struct {
	cfg          *WatcherConfig
	peerAddrMap  map[peer.ID]string // id -> addr
	peerScoreMap map[peer.ID]int    // id -> addr
}

func (w *PeerWatcher) IsBad(pid peer.ID) error {
	// TODO(galaio): check whether it from trust peers/ static peers
	// check whether it from bad ip
	// check whether it from bad peer in score
	return nil
}

func (w *PeerWatcher) InboundLimit() int {
	return int(float64(w.cfg.PeerLimit) * InboundRatio)
}

// TODO(galaio): refactory
func (w *PeerWatcher) InboundConnected() []peer.ID {
	return maps.Keys(w.peerAddrMap)
}

// TODO(galaio): refactory
func (w *PeerWatcher) Active() []peer.ID {
	return maps.Keys(w.peerAddrMap)
}

func NewPeerWatcher(cfg *WatcherConfig) (*PeerWatcher, error) {
	return &PeerWatcher{
		cfg:          cfg,
		peerAddrMap:  make(map[peer.ID]string),
		peerScoreMap: make(map[peer.ID]int),
	}, nil
}
