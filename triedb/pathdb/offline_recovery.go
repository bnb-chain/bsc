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

// Offline recovery is an explicit administrative operation.
// It deliberately does not use New: New loads journals, repairs/truncates
// histories, and honours the snap-sync disable flag before recovery can run.
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type OfflineReport struct {
	PersistentRoot common.Hash `json:"persistent_root"`
	TargetRoot     common.Hash `json:"target_root"`
	PersistentID   uint64      `json:"persistent_id"`
	TargetID       uint64      `json:"target_id"`
	Transitions    uint64      `json:"transitions"`
	SnapStatus     uint8       `json:"snap_status"`
	Applied        bool        `json:"applied"`
}

// OfflineRecover restores a durable MPT base, ignoring in-memory journals and
// the snap disable flag ONLY in this isolated offline instance. The caller must
// enforce exclusive access and explicit authorization before apply=true.
// It never invokes blockchain SetHead or falls back to genesis. On error state
// may be partly reverted: do not start it or retry automatically.
func OfflineRecover(disk ethdb.Database, target common.Hash, maxSteps uint64, applyChanges bool) (report OfflineReport, err error) {
	report.TargetRoot = target
	report.SnapStatus = rawdb.ReadSnapSyncStatusFlag(disk)
	report.PersistentID = rawdb.ReadPersistentStateID(disk)
	blob := rawdb.ReadAccountTrieNode(disk, nil)
	if len(blob) == 0 || report.PersistentID == 0 {
		return report, errors.New("no non-genesis durable MPT base; refusing")
	}
	report.PersistentRoot, err = merkleNodeHasher(blob)
	if err != nil {
		return report, err
	}
	if target == (common.Hash{}) || target == types.EmptyRootHash {
		return report, errors.New("empty/genesis target forbidden")
	}
	id := rawdb.ReadStateID(disk, target)
	if id == nil {
		return report, errors.New("target root has no state ID")
	}
	report.TargetID = *id
	if *id > report.PersistentID {
		return report, errors.New("target is ahead of durable base; journal recovery is not supported")
	}
	report.Transitions = report.PersistentID - *id
	if report.Transitions > maxSteps {
		return report, errors.New("history exceeds explicit step limit")
	}
	// Index maintenance is intentionally unsupported rather than silently leaving
	// an index which points at histories truncated by Recover.
	if len(rawdb.ReadStateHistoryIndexMetadata(disk)) > 0 || len(rawdb.ReadTrienodeHistoryIndexMetadata(disk)) > 0 {
		return report, errors.New("indexed history is unsupported by offline recovery")
	}
	ancient, err := disk.AncientDatadir()
	if err != nil {
		return report, err
	}
	freezer, err := rawdb.NewStateFreezer(ancient, false, !applyChanges)
	if err != nil {
		return report, err
	}
	defer func() {
		if e := freezer.Close(); err == nil {
			err = e
		}
	}()
	// Keep optional trie-node histories aligned with the restored state ID too.
	var trieFreezer ethdb.ResettableAncientStore
	if _, e := os.Stat(filepath.Join(ancient, rawdb.MerkleTrienodeFreezerName)); e == nil {
		trieFreezer, err = rawdb.NewTrienodeFreezer(ancient, false, !applyChanges)
		if err != nil {
			return report, err
		}
		defer func() {
			if e := trieFreezer.Close(); err == nil {
				err = e
			}
		}()
	} else if !os.IsNotExist(e) {
		return report, e
	}
	head, err := freezer.Ancients()
	if err != nil {
		return report, err
	}
	if head < report.PersistentID {
		return report, errors.New("history head is below durable state ID")
	}
	// Decode ALL five tables before the first state mutation. Metadata-only
	// Recoverable is not enough. Check both ends, not only parent continuity.
	root := target
	for n := *id + 1; n <= report.PersistentID; n++ {
		h, e := readStateHistory(freezer, n)
		if e != nil {
			return report, fmt.Errorf("history %d: %w", n, e)
		}
		if h.meta.parent != root {
			return report, fmt.Errorf("history %d parent mismatch", n)
		}
		root = h.meta.root
	}
	if root != report.PersistentRoot {
		return report, errors.New("history terminal root does not match durable MPT root")
	}
	if !applyChanges {
		return report, nil
	}
	config := *Defaults
	config.SnapshotNoBuild = true
	config.NoAsyncFlush = true
	config.NoAsyncGeneration = true
	config.EnableStateIndexing = false
	db := &Database{config: &config, diskdb: disk, hasher: merkleNodeHasher, stateFreezer: freezer, trienodeFreezer: trieFreezer}
	db.tree = newLayerTree(newDiskLayer(report.PersistentRoot, report.PersistentID, db, nil, nil, newBuffer(config.WriteBufferSize, nil, nil, 0), nil))
	if target != report.PersistentRoot {
		// Original Recover/apply verifies every reconstructed storage/account root.
		if err = db.Recover(target); err != nil {
			return report, err
		}
	}
	got, e := merkleNodeHasher(rawdb.ReadAccountTrieNode(disk, nil))
	if e != nil {
		return report, e
	}
	if got != target || rawdb.ReadPersistentStateID(disk) != *id {
		return report, errors.New("post-replay durable root/state ID mismatch")
	}
	if err = disk.SyncKeyValue(); err != nil {
		return report, err
	}
	report.Applied = true
	return report, nil
}
