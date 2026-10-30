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
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestOfflineResetMarkersFromZeroExecutionHead(t *testing.T) {
	db, err := Open(NewMemoryDatabase(), OpenOptions{Ancient: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var blocks []*types.Block
	for i := 0; i < 4; i++ {
		h := &types.Header{Number: big.NewInt(int64(i)), Root: common.Hash{byte(i + 1)}}
		if i > 0 {
			h.ParentHash = blocks[i-1].Hash()
		}
		b := types.NewBlockWithHeader(h)
		blocks = append(blocks, b)
		WriteBlock(db, b)
		WriteCanonicalHash(db, b.Hash(), uint64(i))
	}
	WriteHeadBlockHash(db, blocks[0].Hash())
	WriteHeadHeaderHash(db, blocks[3].Hash())
	WriteHeadFastBlockHash(db, blocks[3].Hash())
	WriteFinalizedBlockHash(db, blocks[2].Hash())
	WriteLastPivotNumber(db, 99)
	WriteSnapSyncStatusFlag(db, StateSyncRunning)
	db.Put([]byte("fm-test"), []byte{1})
	if err = OfflineResetMarkers(db, 1, blocks[1].Hash()); err != nil {
		t.Fatal(err)
	}
	if ReadHeadBlockHash(db) != blocks[1].Hash() || ReadHeadHeaderHash(db) != blocks[1].Hash() || ReadHeadFastBlockHash(db) != blocks[1].Hash() {
		t.Fatal("heads not aligned")
	}
	if ReadFinalizedBlockHash(db) != (common.Hash{}) || ReadLastPivotNumber(db) != nil || ReadSnapSyncStatusFlag(db) != StateSyncFinished {
		t.Fatal("stale finality/sync markers")
	}
	if ReadCanonicalHash(db, 2) != (common.Hash{}) {
		t.Fatal("abandoned canonical mapping retained")
	}
	if ReadBlock(db, blocks[2].Hash(), 2) == nil {
		t.Fatal("historical block data deleted")
	}
	if present, _ := db.Has([]byte("fm-test")); present {
		t.Fatal("stale log index retained")
	}
}

func TestOfflineResetMarkersRejectsWrongTarget(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()
	before := common.Hash{9}
	WriteHeadBlockHash(db, before)
	if err := OfflineResetMarkers(db, 1, common.Hash{1}); err == nil {
		t.Fatal("accepted absent target")
	}
	if ReadHeadBlockHash(db) != before {
		t.Fatal("changed head on rejected target")
	}
}
