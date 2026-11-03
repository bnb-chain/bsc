// Copyright 2026 The go-ethereum Authors
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

package pathdb

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
)

// Source must be opened read-only. Every replay write is retained in memory.
type replayOverlay struct {
	ethdb.Database
	mu      sync.RWMutex
	entries map[string][]byte
}

func (m *replayOverlay) Get(k []byte) ([]byte, error) {
	m.mu.RLock()
	v, ok := m.entries[string(k)]
	m.mu.RUnlock()
	if ok {
		if v == nil {
			return nil, errors.New("overlay: deleted")
		}
		return common.CopyBytes(v), nil
	}
	return m.Database.Get(k)
}
func (m *replayOverlay) Has(k []byte) (bool, error) {
	m.mu.RLock()
	v, ok := m.entries[string(k)]
	m.mu.RUnlock()
	if ok {
		return v != nil, nil
	}
	return m.Database.Has(k)
}
func (m *replayOverlay) Put(k, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[string(k)] = append([]byte{}, v...)
	return nil
}
func (m *replayOverlay) Delete(k []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[string(k)] = nil
	return nil
}
func (m *replayOverlay) DeleteRange(a, b []byte) error {
	return errors.New("range delete forbidden in replay check")
}
func (m *replayOverlay) SyncKeyValue() error { return nil }

type replayBatch struct {
	ethdb.Batch
	target *replayOverlay
}

func (b *replayBatch) Write() error                         { return b.Batch.Replay(b.target) }
func (m *replayOverlay) NewBatch() ethdb.Batch              { return &replayBatch{memorydb.New().NewBatch(), m} }
func (m *replayOverlay) NewBatchWithSize(n int) ethdb.Batch { return m.NewBatch() }

// OfflineReplayCheck reconstructs only a missing full-branch account root,
// requires its hash to equal the saved snapshot root, and verifies every reverse
// transition using Geth's original apply path. No history is truncated.
func OfflineReplayCheck(source ethdb.Database, target common.Hash, maxSteps uint64) (OfflineReport, error) {
	m := &replayOverlay{Database: source, entries: make(map[string][]byte)}
	if !rawdb.HasAccountTrieNode(source, nil) {
		b, e := OfflineReconstructedRoot(source)
		if e != nil {
			return OfflineReport{}, e
		}
		rawdb.WriteAccountTrieNode(m, nil, b)
	}
	report, e := OfflineRecover(m, target, maxSteps, false)
	if e != nil {
		return report, e
	}
	ancient, e := source.AncientDatadir()
	if e != nil {
		return report, e
	}
	f, e := rawdb.NewStateFreezer(ancient, false, true)
	if e != nil {
		return report, e
	}
	defer f.Close()
	cfg := *Defaults
	cfg.SnapshotNoBuild = true
	cfg.NoAsyncFlush = true
	cfg.NoAsyncGeneration = true
	cfg.EnableStateIndexing = false
	db := &Database{config: &cfg, diskdb: m, hasher: merkleNodeHasher, stateFreezer: f}
	dl := newDiskLayer(report.PersistentRoot, report.PersistentID, db, nil, nil, newBuffer(cfg.WriteBufferSize, nil, nil, 0), nil)
	db.tree = newLayerTree(dl)
	for dl.rootHash() != target {
		h, e := readStateHistory(f, dl.stateID())
		if e != nil {
			return report, e
		}
		id := dl.stateID()
		dl, e = dl.revert(h)
		if e != nil {
			return report, fmt.Errorf("replay state ID %d: %w", id, e)
		}
		db.tree.init(dl)
	}
	if crypto.Keccak256Hash(rawdb.ReadAccountTrieNode(m, nil)) != target || rawdb.ReadPersistentStateID(m) != report.TargetID {
		return report, errors.New("replayed state mismatch")
	}
	return report, nil
}

// OfflineReconstructedRoot returns a verified root blob without writing it.
func OfflineReconstructedRoot(source ethdb.KeyValueReader) ([]byte, error) {

	children := make([][]byte, 17)
	for i := 0; i < 16; i++ {
		b := rawdb.ReadAccountTrieNode(source, []byte{byte(i)})
		if len(b) == 0 {
			continue
		}
		if len(b) < 32 {
			return nil, fmt.Errorf("cannot reconstruct child %x", i)
		}
		children[i] = crypto.Keccak256(b)
	}
	b, e := rlp.EncodeToBytes(children)
	if e != nil {
		return nil, e
	}
	if crypto.Keccak256Hash(b) != rawdb.ReadSnapshotRoot(source) {
		return nil, errors.New("reconstructed root does not match snapshot")
	}
	return b, nil
}
