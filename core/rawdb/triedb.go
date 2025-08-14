package rawdb

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

type TrieShardingDB struct {
	shardingdb.Database
	ancientDir string
}

func (db *TrieShardingDB) SetStateStore(state ethdb.Database) {
	panic("not supported")
}

func (db *TrieShardingDB) GetStateStore() ethdb.Database {
	panic("not supported")
}

func (db *TrieShardingDB) HasSeparateStateStore() bool {
	return false
}

func (db *TrieShardingDB) StateStoreReader() ethdb.Reader {
	panic("not supported")
}

func (db *TrieShardingDB) SetSnapStore(state ethdb.KeyValueStore) {
	panic("not supported")
}

func (db *TrieShardingDB) GetSnapStore() ethdb.KeyValueStore {
	panic("not supported")
}

func (db *TrieShardingDB) HasSeparateSnapStore() bool {
	return false
}

func (db *TrieShardingDB) SetTxIndexStore(state ethdb.KeyValueStore) {
	panic("not supported")
}

func (db *TrieShardingDB) GetTxIndexStore() ethdb.KeyValueStore {
	panic("not supported")
}

func (db *TrieShardingDB) HasSeparateTxIndexStore() bool {
	panic("not supported")
}

func (db *TrieShardingDB) IndexStoreReader() ethdb.KeyValueReader {
	panic("not supported")
}

func (db *TrieShardingDB) SetupFreezerEnv(env *ethdb.FreezerEnv, blockHistory uint64) error {
	panic("not supported")
}

func (db *TrieShardingDB) HasAncient(kind string, number uint64) (bool, error) {
	panic("not supported")
}

func (db *TrieShardingDB) Ancient(kind string, number uint64) ([]byte, error) {
	panic("not supported")
}

func (db *TrieShardingDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	panic("not supported")
}

func (db *TrieShardingDB) Ancients() (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) Tail() (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) AncientSize(kind string) (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) ItemAmountInAncient() (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) AncientOffSet() uint64 {
	panic("not supported")
}

func (db *TrieShardingDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	panic("not supported")
}

func (db *TrieShardingDB) ModifyAncients(f func(ethdb.AncientWriteOp) error) (int64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) SyncAncient() error {
	panic("not supported")
}

func (db *TrieShardingDB) TruncateHead(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) TruncateTail(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) TruncateTableTail(kind string, tail uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieShardingDB) ResetTable(kind string, startAt uint64, onlyEmpty bool) error {
	panic("not supported")
}

func NewTrieShardingDB(cfg *TrieDBConfig, cache int, handles int, readonly, disableFreeze bool) (*TrieShardingDB, error) {
	db, err := shardingdb.New(&cfg.Config, cache, handles, readonly, ShardIndexInTrieDB)
	if err != nil {
		return nil, err
	}
	ancientDir := cfg.AncientDir
	if disableFreeze {
		ancientDir = ""
	}
	return &TrieShardingDB{Database: *db, ancientDir: ancientDir}, nil
}

func (db *TrieShardingDB) Close() error {
	return db.Database.Close()
}

func (db *TrieShardingDB) AncientDatadir() (string, error) {
	if db.ancientDir == "" {
		return "", errors.New("disableFreeze in trieDB")
	}
	return db.ancientDir, nil
}

// ShardIndexInTrieDB returns the shard index of the given key
// it accepts account trie key, storage trie key, and state root key
func ShardIndexInTrieDB(key []byte, shardNum int) int {
	// TrieNodeAccountPrefix + hexPath -> trie node
	if bytes.HasPrefix(key, TrieNodeAccountPrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]) % shardNum
	}
	// TrieNodeStoragePrefix + accountHash + hexPath -> trie node
	if bytes.HasPrefix(key, TrieNodeStoragePrefix) {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]) % shardNum
	}
	// CodePrefix + code hash -> account code
	if bytes.HasPrefix(key, CodePrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]) % shardNum
	}
	// some metadata, journal save in shard0
	// such as persistentStateIDKey, trieJournalKey, stateIDPrefix, etc.
	return 0
}
