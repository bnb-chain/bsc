// Copyright 2022 The go-ethereum Authors
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
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

const (
	StateSyncUnknown  = uint8(0) // flags the state snap sync is unknown
	StateSyncRunning  = uint8(1) // flags the state snap sync is not completed yet
	StateSyncFinished = uint8(2) // flags the state snap sync is completed
)

// ReadSnapSyncStatusFlag retrieves the state snap sync status flag.
func ReadSnapSyncStatusFlag(db ethdb.KeyValueReader) uint8 {
	blob, err := db.Get(snapSyncStatusFlagKey)
	if err != nil || len(blob) != 1 {
		return StateSyncUnknown
	}
	return blob[0]
}

// WriteSnapSyncStatusFlag stores the state snap sync status flag into database.
func WriteSnapSyncStatusFlag(db ethdb.KeyValueWriter, flag uint8) {
	if err := db.Put(snapSyncStatusFlagKey, []byte{flag}); err != nil {
		log.Crit("Failed to store sync status flag", "err", err)
	}
}
