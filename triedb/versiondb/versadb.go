package versiondb

import (
	versa "github.com/bnb-chain/versioned-state-database"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
)

type Config struct {
	Path              string
	FlushInterval     int64
	MaxStatesInMem    int
	EnableHashChecker bool
}

type VersionDB struct {
	db versa.Database
}

func New(config *Config) *VersionDB {
	cfg := &versa.VersaDBConfig{
		FlushInterval:     config.FlushInterval,
		MaxStatesInMem:    config.MaxStatesInMem,
		EnableHashChecker: config.EnableHashChecker,
	}
	db, err := versa.NewVersaDB(config.Path, cfg)
	if err != nil {
		log.Crit("failed to new version db", "error", err)
	}
	v := &VersionDB{
		db: db,
	}
	return v
}

// Scheme returns the identifier of used storage scheme.
func (v *VersionDB) Scheme() string {
	return rawdb.VersionScheme
}

// Initialized returns an indicator if the state data is already initialized
// according to the state scheme.
func (v *VersionDB) Initialized(genesisRoot common.Hash) bool {
	return v.db == nil
}

// Size returns the current storage size of the memory cache in front of the
// persistent database layer.
func (v *VersionDB) Size() (common.StorageSize, common.StorageSize, common.StorageSize) {
	// TODO:: waiting versa db supported
	return 0, 0, 0
}

// Update performs a state transition by committing dirty nodes contained
// in the given set in order to update state from the specified parent to
// the specified root.
//
// The passed in maps(nodes, states) will be retained to avoid copying
// everything. Therefore, these maps must not be changed afterwards.
func (v *VersionDB) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	panic("version db not supported")
}

// Commit writes all relevant trie nodes belonging to the specified state
// to disk. Report specifies whether logs will be displayed in info level.
func (v *VersionDB) Commit(root common.Hash, report bool) error {
	panic("version db not supported")
}

// Close closes the trie database backend and releases all held resources.
func (v *VersionDB) Close() error {
	return v.db.Close()
}

func (v *VersionDB) VersaDB() versa.Database {
	return v.db
}
