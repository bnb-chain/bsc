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

	// IncrBlockStateIDMappingTable indicates the mapping table between block numbers and state IDs.
	IncrBlockStateIDMappingTable = "mapping"

	// IncrEmptyBlockTable indicates the block has a state transition.
	IncrEmptyBlockTable = "empty"
)

// chainFreezerTableConfigs configures the settings for tables in the chain freezer.
// Compression is disabled for hashes as they don't compress well.
var chainFreezerTableConfigs = map[string]freezerTableConfig{
	ChainFreezerHeaderTable:      {noSnappy: false, prunable: true},
	ChainFreezerHashTable:        {noSnappy: true, prunable: true},
	ChainFreezerBodiesTable:      {noSnappy: false, prunable: true},
	ChainFreezerReceiptTable:     {noSnappy: false, prunable: true},
	ChainFreezerDifficultyTable:  {noSnappy: true, prunable: true},
	ChainFreezerBlobSidecarTable: {noSnappy: false, prunable: true},
}
var additionTables = []string{ChainFreezerBlobSidecarTable}

// incrChainFreezerTableConfigs configures the settings for tables in the incr chain freezer.
// Hashes and difficulties don't compress well.
var incrChainFreezerTableConfigs = map[string]freezerTableConfig{
	ChainFreezerHeaderTable:      {noSnappy: false, prunable: true},
	ChainFreezerHashTable:        {noSnappy: true, prunable: true},
	ChainFreezerBodiesTable:      {noSnappy: false, prunable: true},
	ChainFreezerReceiptTable:     {noSnappy: false, prunable: true},
	ChainFreezerDifficultyTable:  {noSnappy: true, prunable: true},
	ChainFreezerBlobSidecarTable: {noSnappy: false, prunable: true},
	IncrBlockStateIDMappingTable: {noSnappy: false, prunable: true}, // block number -> state id
	IncrEmptyBlockTable:          {noSnappy: false, prunable: true},
}

// freezerTableConfig contains the settings for a freezer table.
type freezerTableConfig struct {
	noSnappy bool // disables item compression
	prunable bool // true for tables that can be pruned by TruncateTail
}

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
	incrStateHistoryMeta          = "incrhistory.meta"
	incrStateHistoryTrieNodesData = "trienodes.data"
	incrStateHistoryStatesData    = "states.data"
)

// stateFreezerTableConfigs configures the settings for tables in the state freezer.
var stateFreezerTableConfigs = map[string]freezerTableConfig{
	stateHistoryMeta:         {noSnappy: true, prunable: true},
	stateHistoryAccountIndex: {noSnappy: false, prunable: true},
	stateHistoryStorageIndex: {noSnappy: false, prunable: true},
	stateHistoryAccountData:  {noSnappy: false, prunable: true},
	stateHistoryStorageData:  {noSnappy: false, prunable: true},
}

var additionIncrTables = []string{ChainFreezerHeaderTable, ChainFreezerHashTable, ChainFreezerBodiesTable, ChainFreezerReceiptTable,
	ChainFreezerDifficultyTable, IncrBlockStateIDMappingTable, IncrEmptyBlockTable}

// incrStateFreezerTableConfigs configures the settings for tables in the incr state freezer.
var incrStateFreezerTableConfigs = map[string]freezerTableConfig{
	incrStateHistoryMeta:          {noSnappy: true, prunable: true},
	incrStateHistoryTrieNodesData: {noSnappy: false, prunable: true},
	incrStateHistoryStatesData:    {noSnappy: false, prunable: true},
}

const (
	trienodeHistoryHeaderTable       = "trienode.header"
	trienodeHistoryKeySectionTable   = "trienode.key"
	trienodeHistoryValueSectionTable = "trienode.value"
)

// trienodeFreezerTableConfigs configures the settings for tables in the trienode freezer.
var trienodeFreezerTableConfigs = map[string]freezerTableConfig{
	trienodeHistoryHeaderTable: {noSnappy: false, prunable: true},

	// Disable snappy compression to allow efficient partial read.
	trienodeHistoryKeySectionTable: {noSnappy: true, prunable: true},

	// Disable snappy compression to allow efficient partial read.
	trienodeHistoryValueSectionTable: {noSnappy: true, prunable: true},
}

// The list of identifiers of ancient stores.
var (
	ChainFreezerName          = "chain"           // the folder name of chain segment ancient store.
	MerkleStateFreezerName    = "state"           // the folder name of state history ancient store.
	VerkleStateFreezerName    = "state_verkle"    // the folder name of state history ancient store.
	MerkleTrienodeFreezerName = "trienode"        // the folder name of trienode history ancient store.
	VerkleTrienodeFreezerName = "trienode_verkle" // the folder name of trienode history ancient store.

	IncrementalPath = "incremental" // the folder name of incremental ancient store
)

// freezers the collections of all builtin freezers.
var freezers = []string{
	ChainFreezerName,
	MerkleStateFreezerName, VerkleStateFreezerName,
	MerkleTrienodeFreezerName, VerkleTrienodeFreezerName,
}

// NewStateFreezer initializes the ancient store for state history.
//
//   - if the empty directory is given, initializes the pure in-memory
//     state freezer (e.g. dev mode).
//   - if non-empty directory is given, initializes the regular file-based
//     state freezer.
func NewStateFreezer(ancientDir string, verkle bool, readOnly bool) (ethdb.ResettableAncientStore, error) {
	if ancientDir == "" {
		return NewMemoryFreezer(readOnly, stateFreezerTableConfigs), nil
	}
	var name string
	if verkle {
		name = filepath.Join(ancientDir, VerkleStateFreezerName)
	} else {
		name = filepath.Join(ancientDir, MerkleStateFreezerName)
	}
	return newResettableFreezer(name, "eth/db/state", readOnly, stateHistoryTableSize, stateFreezerTableConfigs, false)
}

// OpenIncrStateFreezer opens the incremental state freezer.
func OpenIncrStateFreezer(incrStateDir string, readOnly bool) (ethdb.ResettableAncientStore, error) {
	if incrStateDir == "" {
		return nil, errors.New("empty incr state directory")
	}

	name := filepath.Join(incrStateDir, MerkleStateFreezerName)
	return newResettableFreezer(name, "eth/db/incr/state", readOnly, stateHistoryTableSize, incrStateFreezerTableConfigs, true)
}

// OpenIncrChainFreezer opens the incremental chain freezer.
func OpenIncrChainFreezer(incrChainDir string, readOnly bool) (ethdb.ResettableAncientStore, error) {
	if incrChainDir == "" {
		return nil, errors.New("empty incr chain directory")
	}

	name := filepath.Join(incrChainDir, ChainFreezerName)
	return newResettableFreezer(name, "eth/db/incr/chain", readOnly, stateHistoryTableSize, incrChainFreezerTableConfigs, true)
}

// NewTrienodeFreezer initializes the ancient store for trienode history.
//
//   - if the empty directory is given, initializes the pure in-memory
//     trienode freezer (e.g. dev mode).
//   - if non-empty directory is given, initializes the regular file-based
//     trienode freezer.
func NewTrienodeFreezer(ancientDir string, verkle bool, readOnly bool) (ethdb.ResettableAncientStore, error) {
	if ancientDir == "" {
		return NewMemoryFreezer(readOnly, trienodeFreezerTableConfigs), nil
	}
	var name string
	if verkle {
		name = filepath.Join(ancientDir, VerkleTrienodeFreezerName)
	} else {
		name = filepath.Join(ancientDir, MerkleTrienodeFreezerName)
	}
	return newResettableFreezer(name, "eth/db/trienode", readOnly, stateHistoryTableSize, trienodeFreezerTableConfigs, true)
}
