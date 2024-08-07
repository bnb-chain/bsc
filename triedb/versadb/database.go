package versadb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
	"sync"
)

const (
	// maxDiffLayers is the maximum diff layers allowed in the layer tree.
	maxDiffLayers = 128
)

// Config contains the settings for database.
type Config struct {
	CleanCacheSize int // Maximum memory allowance (in bytes) for caching clean nodes
}

// Defaults is the default setting for database if it's not specified. ,
var Defaults = &Config{
	CleanCacheSize: 0,
}

type Database struct {
	// readOnly is the flag whether the mutation is allowed to be applied.
	// It will be set automatically when the database is journaled during
	// the shutdown to reject all following unexpected mutations.
	readOnly bool         // Flag if database is opened in read only mode
	config   *Config      // Configuration for database
	lock     sync.RWMutex // Lock to prevent mutations from happening at the same time
}

// New initializes the version state database.
func New(diskdb ethdb.Database, config *Config) *Database {
	if config == nil {
		config = Defaults
	}
	// TODO
	db := VersaDB.NewVersaDB(nil)
	return db
}

// Scheme returns the identifier of used storage scheme.
func (db *Database) Scheme() string {
	return rawdb.VersaScheme
}

// Initialized returns an indicator if the state data is already initialized
// according to the state scheme.
func (db *Database) Initialized(genesisRoot common.Hash) bool {
	// TODO
}

// Size returns the current storage size of the memory cache in front of the
// persistent database layer.
func (db *Database) Size() (common.StorageSize, common.StorageSize, common.StorageSize) {
	// TODO
}

// Update performs a state transition by committing dirty nodes contained
// in the given set in order to update state from the specified parent to
// the specified root.
//
// The passed in maps(nodes, states) will be retained to avoid copying
// everything. Therefore, these maps must not be changed afterwards.
func (db *Database) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// TODO
}

// Commit writes all relevant trie nodes belonging to the specified state
// to disk. Report specifies whether logs will be displayed in info level.
func (db *Database) Commit(root common.Hash, report bool) error {
	// TODO
}

// Close closes the trie database backend and releases all held resources.
func (db *Database) Close() error {
	// TODO
}
