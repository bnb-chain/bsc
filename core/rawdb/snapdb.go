package rawdb

import (
	"bytes"

	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

type SnapDB struct {
	shardingdb.Database
}

func (db *SnapDB) MultiDB() bool {
	return false
}

func (db *SnapDB) ChainDB() ethdb.Database {
	panic("not supported")
}

func (db *SnapDB) IndexDB() ethdb.Database {
	panic("not supported")
}

func (db *SnapDB) SnapDB() ethdb.Database {
	panic("not supported")
}

func (db *SnapDB) TrieDB() ethdb.Database {
	panic("not supported")
}

func (db *SnapDB) SetupFreezerEnv(env *ethdb.FreezerEnv, blockHistory uint64) error {
	panic("not supported")
}

func (db *SnapDB) HasAncient(kind string, number uint64) (bool, error) {
	panic("not supported")
}

func (db *SnapDB) Ancient(kind string, number uint64) ([]byte, error) {
	panic("not supported")
}

func (db *SnapDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	panic("not supported")
}

func (db *SnapDB) Ancients() (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) Tail() (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) AncientSize(kind string) (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) ItemAmountInAncient() (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) AncientOffSet() uint64 {
	panic("not supported")
}

func (db *SnapDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	panic("not supported")
}

func (db *SnapDB) ModifyAncients(f func(ethdb.AncientWriteOp) error) (int64, error) {
	panic("not supported")
}

func (db *SnapDB) SyncAncient() error {
	panic("not supported")
}

func (db *SnapDB) TruncateHead(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) TruncateTail(n uint64) (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) TruncateTableTail(kind string, tail uint64) (uint64, error) {
	panic("not supported")
}

func (db *SnapDB) ResetTable(kind string, startAt uint64, onlyEmpty bool) error {
	panic("not supported")
}

func (db *SnapDB) AncientDatadir() (string, error) {
	panic("not supported")
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
