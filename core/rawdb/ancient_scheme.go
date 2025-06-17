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
	"errors"
	"path/filepath"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// The list of table names of chain freezer.
const (
	// ChainFreezerHeaderTable indicates the name of the freezer header table.
	ChainFreezerHeaderTable = "headers"

	// ChainFreezerHashTable indicates the name of the freezer canonical hash table.
	ChainFreezerHashTable = "hashes"

	// ChainFreezerBodiesTable indicates the name of the freezer block body table.
	ChainFreezerBodiesTable = "bodies"

	// ChainFreezerReceiptTable indicates the name of the freezer receipts table.
	ChainFreezerReceiptTable = "receipts"

	// ChainFreezerDifficultyTable indicates the name of the freezer total difficulty table.
	ChainFreezerDifficultyTable = "diffs"

	// ChainFreezerBlobSidecarTable indicates the name of the freezer total blob table.
	ChainFreezerBlobSidecarTable = "blobs"
)

// chainFreezerNoSnappy configures whether compression is disabled for the ancient-tables.
// Hashes and difficulties don't compress well.
var chainFreezerNoSnappy = map[string]bool{
	ChainFreezerHeaderTable:      false,
	ChainFreezerHashTable:        true,
	ChainFreezerBodiesTable:      false,
	ChainFreezerReceiptTable:     false,
	ChainFreezerDifficultyTable:  true,
	ChainFreezerBlobSidecarTable: false,
}

var additionTables = []string{ChainFreezerBlobSidecarTable}

const (
	// stateHistoryTableSize defines the maximum size of freezer data files.
	stateHistoryTableSize = 2 * 1000 * 1000 * 1000

	// stateHistoryAccountIndex indicates the name of the freezer state history table.
	stateHistoryMeta         = "history.meta"
	stateHistoryAccountIndex = "account.index"
	stateHistoryStorageIndex = "storage.index"
	stateHistoryAccountData  = "account.data"
	stateHistoryStorageData  = "storage.data"

	// indicates the name of the freezer incremental state history table.
	incrStateHistoryTrieNodesData = "trienodes.data"
)

// stateFreezerNoSnappy configures whether compression is disabled for the state freezer.
var stateFreezerNoSnappy = map[string]bool{
	stateHistoryMeta:         true,
	stateHistoryAccountIndex: false,
	stateHistoryStorageIndex: false,
	stateHistoryAccountData:  false,
	stateHistoryStorageData:  false,
}

var additionIncrTables = []string{ChainFreezerHeaderTable, ChainFreezerHashTable, ChainFreezerBodiesTable, ChainFreezerReceiptTable,
	ChainFreezerDifficultyTable, stateHistoryMeta, stateHistoryAccountIndex, stateHistoryStorageIndex,
	stateHistoryAccountData, stateHistoryStorageData, incrStateHistoryTrieNodesData}

// incrStateFreezerNoSnappy configures whether compression is disabled for the incremental state freezer.
var incrStateFreezerNoSnappy = map[string]bool{
	stateHistoryMeta:              true,
	stateHistoryAccountIndex:      false,
	stateHistoryStorageIndex:      false,
	stateHistoryAccountData:       false,
	stateHistoryStorageData:       false,
	incrStateHistoryTrieNodesData: false,
}

// The list of identifiers of ancient stores.
var (
	ChainFreezerName       = "chain"        // the folder name of chain segment ancient store.
	MerkleStateFreezerName = "state"        // the folder name of state history ancient store.
	VerkleStateFreezerName = "state_verkle" // the folder name of state history ancient store.

	IncrementalPath = "incremental_snapshot" // the folder name of incremental ancient store
)

// freezers the collections of all builtin freezers.
var freezers = []string{ChainFreezerName, MerkleStateFreezerName, VerkleStateFreezerName}

// NewStateFreezer initializes the ancient store for state history.
//
//   - if the empty directory is given, initializes the pure in-memory
//     state freezer (e.g. dev mode).
//   - if non-empty directory is given, initializes the regular file-based
//     state freezer.
func NewStateFreezer(ancientDir string, verkle bool, readOnly bool, offset uint64) (ethdb.ResettableAncientStore, error) {
	if ancientDir == "" {
		return NewMemoryFreezer(readOnly, stateFreezerNoSnappy), nil
	}
	var name string
	if verkle {
		name = filepath.Join(ancientDir, VerkleStateFreezerName)
	} else {
		name = filepath.Join(ancientDir, MerkleStateFreezerName)
	}
	return newResettableFreezer(name, "eth/db/state", readOnly, offset, stateHistoryTableSize, stateFreezerNoSnappy, false)
}

// NewIncrStateFreezer initializes the ancient store for incremental state history.
//
//   - if the empty directory is given, initializes the pure in-memory
//     incremental state freezer (e.g. dev mode).
//   - if non-empty directory is given, initializes the regular file-based
//     incremental state freezer.
func NewIncrStateFreezer(ancientDir string, readOnly bool, offset, blockLimit uint64) (ethdb.ResettableAncientStore, error) {
	if ancientDir == "" {
		return NewMemoryFreezer(readOnly, incrStateFreezerNoSnappy), nil
	}

	name := filepath.Join(ancientDir, IncrementalPath, MerkleStateFreezerName)
	return newResettableFreezer(name, "eth/db/state", readOnly, offset, stateHistoryTableSize, incrStateFreezerNoSnappy, true)
	// return newIncrFreezer(name, "eth/db/incremental/state", readOnly, offset, stateHistoryTableSize,
	// 	incrStateFreezerNoSnappy, blockLimit)
}

func OpenIncrStateFreezer(incrStateDir string, readOnly bool) (ethdb.ResettableAncientStore, error) {
	if incrStateDir == "" {
		log.Error("Incremental state directory is empty")
		return nil, errors.New("empty incr state directory")
	}
	return newIncrFreezer(incrStateDir, "eth/db/incremental/state", readOnly, 0, stateHistoryTableSize,
		incrStateFreezerNoSnappy, 1)
}

// NewIncrChainFreezer initializes the ancient store for incremental block history.
func NewIncrChainFreezer(ancientDir string, readOnly bool, offset, blockLimit uint64) (ethdb.ResettableAncientStore, error) {
	if ancientDir == "" {
		return NewMemoryFreezer(readOnly, incrStateFreezerNoSnappy), nil
	}

	name := filepath.Join(ancientDir, IncrementalPath, ChainFreezerName)
	return newResettableFreezer(name, "eth/db/chain", readOnly, offset, stateHistoryTableSize, chainFreezerNoSnappy, true)
	// return newIncrFreezer(name, "eth/db/incremental/chain", readOnly, offset, stateHistoryTableSize,
	// 	chainFreezerNoSnappy, blockLimit)
}
