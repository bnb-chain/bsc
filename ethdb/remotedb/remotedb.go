package remotedb

import (
	"errors"
	"time"
	"bytes"
	"context"

	rocks "github.com/go-redis/redis/v8"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log")

var (
	// reWriteKeyPrefix tracks keys to write remotedb again, before write failed
	reWriteKeyPrefix = []byte("ReWriteKeyPrefix")
	// reDeleteKeyPrefix tracks keys to delete remotedb again, before delete failed
	reDeleteKeyPrefix = []byte("ReDeleteKeyPrefix")
	// reDeleteKeyContent recoeds val to delete remotedb again, before delete failed
	reDeleteKeyContent = []byte("ReDeleteKeyContent")
	// handleExceptionKeyInterval is a timer that handler operate remotedb fail request
	handleExceptionKeyInterval = time.Minute

	// headBlockKey tracks the latest known full block's hash.
	headBlockKey = []byte("LastBlock")
	// headHeaderKey tracks the latest known header's hash.
	headHeaderKey = []byte("LastHeader")
	// headFastBlockKey tracks the latest known incomplete block's hash during fast sync.
	headFastBlockKey = []byte("LastFast")
	// remoteDbWriteMarker flag if has eth node write remotedb
	remoteDbWriteMarker = []byte("remotedbwritermarker") 
	// remoteKeys is collection for get remotedb
	remoteKeys = [][]byte{headBlockKey, headHeaderKey, headFastBlockKey, remoteDbWriteMarker}
)

// reWriteKey return key-prefix that rewrite to remotedb from local persist cache
func reWriteKey(key []byte) []byte {
	return append(reWriteKeyPrefix, key[:]...)
}

// reDeleteKey return key-prefix that redelete to remotedb from local persist cache
func reDeleteKey(key []byte) []byte {
	return append(reDeleteKeyPrefix, key[:]...)
}

// RocksDB is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace in
// binary-alphabetical order.
type RocksDB struct {
	config        *Config
	client        *rocks.ClusterClient
	persistCache  ethdb.KeyValueStore
	readonly      bool
	quitChan      chan struct{}
}

// NewRocksDB returns a wrapped RemoteDB（compatible redis） object.
func NewRocksDB(cfg *Config, cache ethdb.KeyValueStore, readonly bool) (*RocksDB, error) {
	db := &RocksDB {
		config:       cfg,
		persistCache: cache,
		readonly:     readonly,
		quitChan:     make(chan struct{}),
	}
	db.client = rocks.NewClusterClient(cfg.GetClusterOption())
	go db.handleExceptionKey()
	return db, nil
}

// Close closes all io accesses to the underlying key-value store.
func (db *RocksDB) Close() error {
	close(db.quitChan)
	if db.persistCache != nil {
		if err := db.persistCache.Close(); err != nil {
			return err
		}
	}
	if err := db.client.Close(); err != nil {
		return err
	}
	return nil
}

// Stat returns a particular internal stat of the database.
func (db *RocksDB) Stat(property string) (string, error) {
	return "", nil
}

// Compact flattens the underlying data store for the given key range.
// remotedb no manual compact
func (db *RocksDB) Compact(start []byte, limit []byte) error {
	return nil
}

// Has retrieves if a key is present in the key-value store.
func (db *RocksDB) Has(key []byte) (bool, error) {
	if db.persistCache != nil && !excludeKeys(key) {
		if exist, _ := db.persistCache.Has(key); exist {
			return true, nil
		}
		if exist, _ := db.persistCache.Has(reWriteKey(key)); exist {
			return true, nil
		}
	}

	exist, err := db.client.Exists(context.Background(), string(key)).Result()
	if err != nil {
		return false, err
	}
	return int64(exist) == 1, nil
}

// excludeKeys helper func , Get whether omit persist cache
func excludeKeys(key []byte) bool {
	for _, rkey :=range remoteKeys {
		if bytes.Equal(key, rkey) {
			return true
		}
	}
	return false
}

// Get retrieves the given key if it's present in the key-value store.
func (db *RocksDB) Get(key []byte) ([]byte, error) {
	if db.persistCache != nil && !excludeKeys(key) {
		if data, _ := db.persistCache.Get(key); len(data) != 0 {
			return data, nil
		}
		if data, _ := db.persistCache.Get(reWriteKey(key)); len(data) != 0 {
			return data, nil
		}
	}

	data, err := db.client.Get(context.Background(), string(key)).Result()
	if err != nil {
		return nil, err
	}
	if db.persistCache != nil {
		db.persistCache.Put(key, []byte(data))
	}
	return []byte(data), nil
}

// Put inserts the given value into the key-value store.
func (db *RocksDB) Put(key []byte, value []byte) error {
	if db.readonly {
		return errors.New("remotdb is readonly, not support Put")
	}
	err := db.client.Set(context.Background(), string(key), string(value), 0).Err()
	if db.persistCache != nil {
		db.persistCache.Put(key, value)
		if err != nil {
			if suberr := db.persistCache.Put(reWriteKey(key), value); suberr != nil {
				log.Error("remotedb backup rewrite key failed", "error", suberr, "key", key)
				return suberr
			} else {
				return nil
			}
		}
	}
	return err
}

// Delete removes the key from the key-value store.
func (db *RocksDB) Delete(key []byte) error {
	if db.readonly {
		return errors.New("remotdb is readonly, not support Delete")
	}
	if db.persistCache != nil {
		db.persistCache.Delete(key)
	}
	err := db.client.Del(context.Background(), string(key)).Err()
	if err != nil && db.persistCache != nil {
		if suberr := db.persistCache.Put(reDeleteKey(key), reDeleteKeyContent); suberr != nil {
			log.Error("remotedb backup redetele key failed", "error", suberr, "key", key)
			return suberr
		} else {
			return nil
		}
	}
	return err
}

// handleExceptionKey rewirte exception key to remotedb
func (db *RocksDB) handleExceptionKey() {
	if db.persistCache == nil || db.readonly {
		return 
	}

	gcExceptionTimer := time.NewTicker(handleExceptionKeyInterval)
	for {
		select {
		case <-db.quitChan:
			return 

		case <-gcExceptionTimer.C:
			ctx := context.Background()
			it := db.persistCache.NewIterator(reWriteKeyPrefix, nil)
			for it.Next() {
				exceptionKey := it.Key()
				key := exceptionKey[len(reWriteKeyPrefix):]
				val := it.Value()
				if err := db.client.Set(ctx, string(key), string(val), 0).Err(); err != nil {
					log.Debug("remotedb rewrite exception failed", "err", err)
					continue
				}
				db.persistCache.Delete(exceptionKey)
			}
			it.Release()

			it = db.persistCache.NewIterator(reDeleteKeyPrefix, nil)
			for it.Next() {
				exceptionKey := it.Key()
				key := exceptionKey[len(reDeleteKeyPrefix):]
				if err := db.client.Del(context.Background(), string(key)).Err(); err != nil {
					log.Debug("remotedb redelete exception failed", "err", err)
					continue
				}
				db.persistCache.Delete(exceptionKey)
			}
			it.Release()
		}
	}
}

// batch is a write-only that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type batch struct {
	db           *RocksDB
	ctx          context.Context
	pipe         rocks.Pipeliner
	size         int
	op           []string
	args         [][][]byte
	cacheBatch   ethdb.Batch
}

func (db *RocksDB) NewBatch() ethdb.Batch {
	b := &batch{
		db:   db,
		ctx:  context.Background(),
		pipe: db.client.Pipeline(),
	}
	if db.persistCache != nil {
		b.cacheBatch = db.persistCache.NewBatch()
	}
	return b
}

// Put inserts the given value into the batch for later committing.
func (b *batch) Put(key, value []byte) error {
	if b.db.readonly {
		return errors.New("remotdb is readonly, not support Batch Put")
	}
	if b.cacheBatch != nil {
		b.cacheBatch.Put(key, value)
	}
	b.pipe.Set(b.ctx, string(key), string(value), 0)
	b.size += len(value)
	b.op = append(b.op, "SET")
	b.args = append(b.args, [][]byte{key, value})
	return nil
}

// Delete inserts the a key removal into the batch for later committing.
func (b *batch) Delete(key []byte) error {
	if b.db.readonly {
		return errors.New("remotdb is readonly, not support Batch Delete")
	}
	if b.cacheBatch != nil {
		b.cacheBatch.Delete(key)
	}
	b.pipe.Del(b.ctx, string(key))
	b.size += len(key)
	b.op = append(b.op, "DEL")
	b.args = append(b.args, [][]byte{key})
	return nil
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int {
	return b.size
}

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	if b.db.readonly {
		return errors.New("remotdb is readonly, not support Batch Write")
	}
	if b.cacheBatch != nil {
		b.cacheBatch.Write()
	}
	_, err := b.pipe.Exec(b.ctx)
	if err != nil && b.db.persistCache != nil {
		for idx, op := range b.op {
			switch op {
			case "SET":
				err := b.db.persistCache.Put(reWriteKey(b.args[idx][0]), b.args[idx][1])
				if err != nil {
					break
				}
			case "DEL":
				err := b.db.persistCache.Put(reDeleteKey(b.args[idx][0]), reDeleteKeyContent)
				if err != nil {
					break
				}
			}
		}
	}
	return err
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.pipe.Discard()
	b.op = b.op[:0]
	b.args = b.args[:0]
	b.size = 0
}

// Replay replays the batch contents.
func (b *batch) Replay(w ethdb.KeyValueWriter) error {
	if b.db.readonly {
		return errors.New("remotdb is readonly, not support Batch Replay")
	}
	replay := & replayer {
		writer : w,
	}
	for idx, op := range b.op {
		if replay.failure != nil {
			return replay.failure
		}
		switch op {
		case "SET":
			replay.Put(b.args[idx][0], b.args[idx][1])
		case "DEL":
			replay.Delete(b.args[idx][0])
		}
	}
	return replay.failure
}

// replayer is a small wrapper to implement the correct replay methods.
type replayer struct {
	writer  ethdb.KeyValueWriter
	failure error
}

// Put inserts the given value into the key-value data store.
func (r *replayer) Put(key, value []byte) {
	// If the replay already failed, stop executing ops
	if r.failure != nil {
		return
	}
	r.failure = r.writer.Put(key, value)
}

// Delete removes the key from the key-value data store.
func (r *replayer) Delete(key []byte) {
	// If the replay already failed, stop executing ops
	if r.failure != nil {
		return
	}
	r.failure = r.writer.Delete(key)
}
