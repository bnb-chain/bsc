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

package rawdb

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

// OfflineResetMarkers is for offline recovery AFTER root-verified state replay.
// The caller must hold the exclusive datadir lock and guard interrupted recovery. Block/header/receipt data
// is retained. Only canonical mappings above target and derived indices reset.
func OfflineResetMarkers(db ethdb.Database, height uint64, hash common.Hash) error {
	block := ReadBlock(db, hash, height)
	if block == nil || ReadCanonicalHash(db, height) != hash {
		return fmt.Errorf("target block/canonical mapping missing")
	}
	head, ok := ReadHeaderNumber(db, ReadHeadHeaderHash(db))
	if !ok || head < height {
		return fmt.Errorf("invalid header head")
	}
	frozen, err := db.Ancients()
	if err != nil {
		return err
	}
	if height+1 < frozen {
		return fmt.Errorf("target crosses chain freezer; unsupported")
	}
	if head-height > 100000 {
		return fmt.Errorf("canonical rewind exceeds 100000 blocks")
	}
	batch := db.NewBatch()
	for n := height + 1; n <= head; n++ {
		h := ReadCanonicalHash(db, n)
		if b := ReadBlock(db, h, n); b != nil {
			for _, tx := range b.Transactions() {
				if num := ReadTxLookupEntry(db, tx.Hash()); num != nil && *num == n {
					DeleteTxLookupEntry(batch, tx.Hash())
				}
			}
		}
		DeleteCanonicalHash(batch, n)
	}
	// Discard restart journals, old sync work and snapshot progress. The MPT is
	// authoritative; regenerate flat snapshots instead of trusting snap partials.
	for _, key := range [][]byte{lastPivotKey, fastTrieProgressKey, snapshotDisabledKey, SnapshotRootKey, snapshotJournalKey, snapshotGeneratorKey, snapshotRecoveryKey, snapshotSyncStatusKey, skeletonSyncStatusKey, trieJournalKey, headFinalizedBlockKey} {
		if err := batch.Delete(key); err != nil {
			return err
		}
	}
	WriteHeadBlockHash(batch, hash)
	WriteHeadHeaderHash(batch, hash)
	WriteHeadFastBlockHash(batch, hash)
	WriteSnapSyncStatusFlag(batch, StateSyncFinished)
	if err := batch.Write(); err != nil {
		return err
	}
	// Filter maps contain references to abandoned canonical blocks. Rebuild the
	// derived index on restart rather than advertise stale indexed logs.
	it := db.NewIterator([]byte(filterMapsPrefix), nil)
	defer it.Release()
	batch.Reset()
	for it.Next() {
		if err := batch.Delete(common.CopyBytes(it.Key())); err != nil {
			return err
		}
		if batch.ValueSize() > 1024*1024 {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return err
	}
	return db.SyncKeyValue()
}
