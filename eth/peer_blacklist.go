package eth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

type peerBlacklistConfig struct {
	Enabled          bool
	SuccessThreshold float64
	MinimumSamples   uint64
	Path             string
}

type blacklistEntry struct {
	AddedAt     time.Time `json:"addedAt"`
	SuccessRate float64   `json:"successRate"`
}

type blacklistFile struct {
	Entries map[string]blacklistEntry `json:"entries"`
}

type peerTxStats struct {
	Total   uint64
	Success uint64
}

type txPeerBlacklist struct {
	cfg     peerBlacklistConfig
	mu      sync.Mutex
	entries map[string]blacklistEntry
	stats   map[string]*peerTxStats
}

func newTxPeerBlacklist(cfg peerBlacklistConfig) (*txPeerBlacklist, error) {
	if !cfg.Enabled || cfg.Path == "" {
		return nil, nil
	}
	bl := &txPeerBlacklist{
		cfg:     cfg,
		entries: make(map[string]blacklistEntry),
		stats:   make(map[string]*peerTxStats),
	}
	if err := bl.load(); err != nil {
		return nil, err
	}
	return bl, nil
}

func (bl *txPeerBlacklist) load() error {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	data, err := os.ReadFile(bl.cfg.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file blacklistFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	if file.Entries != nil {
		bl.entries = file.Entries
	}
	return nil
}

func (bl *txPeerBlacklist) persistLocked() {
	if !bl.cfg.Enabled || bl.cfg.Path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(bl.cfg.Path), 0o755); err != nil {
		log.Warn("Failed to create peer blacklist directory", "path", bl.cfg.Path, "err", err)
		return
	}
	payload := blacklistFile{Entries: bl.entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Warn("Failed to marshal peer blacklist", "err", err)
		return
	}
	if err := os.WriteFile(bl.cfg.Path, data, 0o644); err != nil {
		log.Warn("Failed to persist peer blacklist", "path", bl.cfg.Path, "err", err)
	}
}

func (bl *txPeerBlacklist) isBlacklisted(id string) bool {
	if bl == nil {
		return false
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	_, ok := bl.entries[id]
	return ok
}

// record updates the statistics for a peer and returns true if the peer has been newly blacklisted.
func (bl *txPeerBlacklist) record(id string, success, total int) bool {
	if bl == nil || !bl.cfg.Enabled || id == "" || total <= 0 {
		return false
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()

	if _, blocked := bl.entries[id]; blocked {
		return false
	}

	stat := bl.stats[id]
	if stat == nil {
		stat = &peerTxStats{}
		bl.stats[id] = stat
	}
	stat.Total += uint64(total)
	stat.Success += uint64(success)

	if stat.Total == 0 {
		return false
	}
	if stat.Total < bl.cfg.MinimumSamples {
		return false
	}
	rate := float64(stat.Success) / float64(stat.Total)
	if rate >= bl.cfg.SuccessThreshold {
		// keep collecting but avoid overflow by trimming counts gradually
		stat.Total = stat.Total / 2
		stat.Success = stat.Success / 2
		return false
	}
	entry := blacklistEntry{
		AddedAt:     time.Now().UTC(),
		SuccessRate: rate,
	}
	bl.entries[id] = entry
	delete(bl.stats, id)
	log.Warn("Blacklisting peer due to low transaction success rate", "peer", id, "successRate", rate, "threshold", bl.cfg.SuccessThreshold, "samples", stat.Total)
	bl.persistLocked()
	return true
}

func (bl *txPeerBlacklist) list() map[string]blacklistEntry {
	if bl == nil {
		return nil
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	result := make(map[string]blacklistEntry, len(bl.entries))
	for id, entry := range bl.entries {
		result[id] = entry
	}
	return result
}
