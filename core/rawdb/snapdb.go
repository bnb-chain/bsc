package rawdb

import (
	"bytes"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

type SnapDB struct {
	shardingdb.Database
}

func NewSnapDB(cfg *shardingdb.Config, cache int, handles int, readonly bool) (*SnapDB, error) {
	db, err := shardingdb.New(cfg, cache, handles, readonly)
	if err != nil {
		return nil, err
	}
	return &SnapDB{Database: *db}, nil
}

func (db *SnapDB) Close() error {
	return db.Database.Close()
}

// ShardIndex returns the shard index of the given key
// it accepts account snapshot key, storage snapshot key, and state root key
func (db *SnapDB) ShardIndex(key []byte) int {
	// SnapshotAccountPrefix + account hash -> account trie value
	if bytes.HasPrefix(key, SnapshotAccountPrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]>>4) % db.ShardNum()
	}
	// SnapshotStoragePrefix + account hash + storage hash -> storage trie value
	if bytes.HasPrefix(key, SnapshotStoragePrefix) {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]>>4) % db.ShardNum()
	}
	// some metadata, journal save in shard0
	// such as snapshotDisabledKey, SnapshotRootKey, snapshotGeneratorKey, snapshotJournalKey, etc.
	return 0
}
