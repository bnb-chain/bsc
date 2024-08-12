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
	var (
		cfg  *versa.VersaDBConfig
		path = "./version_db" // TODO:: debug code
	)

	if config != nil {
		path = config.Path
		cfg = &versa.VersaDBConfig{
			FlushInterval:     config.FlushInterval,
			MaxStatesInMem:    config.MaxStatesInMem,
			EnableHashChecker: config.EnableHashChecker,
		}
	}
	db, err := versa.NewVersaDB(path, cfg)
	if err != nil {
		log.Crit("failed to new version db", "error", err)
	}
	v := &VersionDB{
		db: db,
	}
	log.Info("success to init version mode triedb")
	return v
}

func (v *VersionDB) Scheme() string {
	return rawdb.VersionScheme
}

func (v *VersionDB) Initialized(genesisRoot common.Hash) bool {
	return v.db == nil
}

func (v *VersionDB) Size() (common.StorageSize, common.StorageSize, common.StorageSize) {
	// TODO:: waiting versa db supported
	return 0, 0, 0
}

func (v *VersionDB) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set) error {
	// TODO:: debug code, will change to return error.
	panic("version db not supported")
}

func (v *VersionDB) Commit(root common.Hash, report bool) error {
	// TODO:: debug code, will change to return error.
	panic("version db not supported")
}

func (v *VersionDB) Close() error {
	return v.db.Close()
}

func (v *VersionDB) VersaDB() versa.Database {
	return v.db
}
