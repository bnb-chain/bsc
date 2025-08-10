package rawdb

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

type TrieDB struct {
	shardingdb.Database
	ancientDir string
}

func (db *TrieDB) MultiDB() bool {
	return false
}

func (db *TrieDB) ChainDB() ethdb.Database {
	panic("not supported")
}

func (db *TrieDB) IndexDB() ethdb.Database {
	panic("not supported")
}

func (db *TrieDB) SnapDB() ethdb.Database {
	panic("not supported")
}

func (db *TrieDB) TrieDB() ethdb.Database {
	panic("not supported")
}

func (db *TrieDB) SetupFreezerEnv(env *ethdb.FreezerEnv, blockHistory uint64) error {
	panic("not supported")
}

func (db *TrieDB) HasAncient(kind string, number uint64) (bool, error) {
	panic("not supported")
}

func (db *TrieDB) Ancient(kind string, number uint64) ([]byte, error) {
	panic("not supported")
}

func (db *TrieDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	panic("not supported")
}

func (db *TrieDB) Ancients() (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) Tail() (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) AncientSize(kind string) (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) ItemAmountInAncient() (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) AncientOffSet() uint64 {
	panic("not supported")
}

func (db *TrieDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	panic("not supported")
}

func (db *TrieDB) ModifyAncients(f func(ethdb.AncientWriteOp) error) (int64, error) {
	panic("not supported")
}

func (db *TrieDB) SyncAncient() error {
	panic("not supported")
}

func (db *TrieDB) TruncateHead(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) TruncateTail(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) TruncateTableTail(kind string, tail uint64) (uint64, error) {
	panic("not supported")
}

func (db *TrieDB) ResetTable(kind string, startAt uint64, onlyEmpty bool) error {
	panic("not supported")
}

func NewTrieDB(cfg *TrieDBConfig, cache int, handles int, readonly, disableFreeze bool) (*TrieDB, error) {
	db, err := shardingdb.New(&cfg.Config, cache, handles, readonly)
	if err != nil {
		return nil, err
	}
	ancientDir := cfg.AncientDir
	if disableFreeze {
		ancientDir = ""
	}
	return &TrieDB{Database: *db, ancientDir: ancientDir}, nil
}

func (db *TrieDB) Close() error {
	return db.Database.Close()
}

// ShardIndex returns the shard index of the given key
// it accepts account trie key, storage trie key, and state root key
func (db *TrieDB) ShardIndex(key []byte) int {
	// TrieNodeAccountPrefix + hexPath -> trie node
	if bytes.HasPrefix(key, TrieNodeAccountPrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]>>4) % db.ShardNum()
	}
	// TrieNodeStoragePrefix + accountHash + hexPath -> trie node
	if bytes.HasPrefix(key, TrieNodeStoragePrefix) {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]>>4) % db.ShardNum()
	}
	// CodePrefix + code hash -> account code
	if bytes.HasPrefix(key, CodePrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]>>4) % db.ShardNum()
	}
	// some metadata, journal save in shard0
	// such as persistentStateIDKey, trieJournalKey, stateIDPrefix, etc.
	return 0
}

func (db *TrieDB) AncientDatadir() (string, error) {
	if db.ancientDir == "" {
		return "", errors.New("disableFreeze in trieDB")
	}
	return db.ancientDir, nil
}
