package rawdb

import (
	"bytes"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

type SnapShardingDB struct {
	shardingdb.Database
}

func NewSnapShardingDB(cfg *shardingdb.Config, cache int, handles int, readonly bool) (*SnapShardingDB, error) {
	db, err := shardingdb.New(cfg, cache, handles, readonly, ShardIndexInSnapDB)
	if err != nil {
		return nil, err
	}
	return &SnapShardingDB{Database: *db}, nil
}

func (db *SnapShardingDB) Close() error {
	return db.Database.Close()
}

// ShardIndex returns the shard index of the given key
// it accepts account snapshot key, storage snapshot key, and state root key
func ShardIndexInSnapDB(key []byte, shardNum int) int {
	// SnapshotAccountPrefix + account hash -> account trie value
	if bytes.HasPrefix(key, SnapshotAccountPrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]) % shardNum
	}
	// SnapshotStoragePrefix + account hash + storage hash -> storage trie value
	if bytes.HasPrefix(key, SnapshotStoragePrefix) {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]) % shardNum
	}
	// some metadata, journal save in shard0
	// such as snapshotDisabledKey, SnapshotRootKey, snapshotGeneratorKey, snapshotJournalKey, etc.
	return 0
}
