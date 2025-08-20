package shardingdb

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"

	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/ethereum/go-ethereum/ethdb/leveldb"
)

const (
	DBTypePebble  = "pebble"
	DBTypeLeveldb = "leveldb"
	DBTypeMemory  = "memorydb"

	// minShardCache is the minimum amount of memory in megabytes to allocate to each shard
	minShardCache = 16
	// minShardHandles is the minimum number of file handles to allocate to each shard
	minShardHandles = 16
	// maxAtomicCommitRetries is the maximum number of retry attempts for atomic commit
	maxAtomicCommitRetries = 3
	// atomicCommitRetryDelay is the base delay duration between retry attempts
	atomicCommitRetryDelay = 10 * time.Millisecond
)

var (
	// shardBatchRetryMeter measures the number of retry attempts in atomic commits
	shardBatchRetryMeter = metrics.NewRegisteredMeter("ethdb/shardingdb/batch/retry", nil)
)

// shardCommitResult contains the result of a shard commit operation
type shardCommitResult struct {
	batch ethdb.Batch
	err   error
}

type ShardIndexFunc func(key []byte, shardNum int) int

// Config is the configuration of the sharding db
type Config struct {
	CacheRatio     int    // the cache ratio of the db, it will be evenly distributed to all shards
	Namespace      string // it's the default Namespace of the shards
	EnableSharding bool   // whether to enable sharding
	DBType         string // the type of db, such as "leveldb" or "pebble"
	DBPath         string // the path of db, it's the default root path of the shards
	ShardNum       int    // the number of shards
	Shards         []ShardConfig
}

func (c *Config) SanityCheck() error {
	if c.DBPath == "" || c.Namespace == "" || c.DBType == "" {
		return fmt.Errorf("dbpath, namespace and dbtype must be set")
	}
	if c.EnableSharding {
		if c.ShardNum <= 0 {
			return fmt.Errorf("shardnum must be greater than 0")
		}
		if len(c.Shards) == 0 || len(c.Shards) > c.ShardNum {
			return fmt.Errorf("shards must be set and less than or equal to shardnum")
		}
		for _, shard := range c.Shards {
			if shard.Indexes == "" {
				return fmt.Errorf("indexes in shards must be set")
			}
		}
	} else {
		if c.ShardNum != 0 || len(c.Shards) > 0 {
			return fmt.Errorf("shards must be empty when enable sharding is false")
		}
	}
	return nil
}

// parseShards parses the shards from the config
func (c *Config) parseShards() ([]ShardConfig, error) {
	if !c.EnableSharding {
		return []ShardConfig{
			{
				DBPath: c.DBPath,
			},
		}, nil
	}
	shards := make(map[int]ShardConfig)
	for _, shard := range c.Shards {
		indexes, err := parseShardIndexes(shard.Indexes, c.ShardNum)
		if err != nil {
			return nil, err
		}
		for _, index := range indexes {
			if _, ok := shards[index]; ok {
				return nil, fmt.Errorf("shard index %d conflict in %v", index, shard)
			}
			path := shard.DBPath
			if path == "" {
				path = c.DBPath
			}
			// the index is useless later, so ignore it
			shards[index] = ShardConfig{
				DBPath: filepath.Join(path, fmt.Sprintf("shard%04d", index)),
			}
		}
	}
	if len(shards) != c.ShardNum {
		return nil, fmt.Errorf("shard num not match, expect %d, got %d", c.ShardNum, len(shards))
	}
	ret := make([]ShardConfig, 0, len(shards))
	for i := 0; i < c.ShardNum; i++ {
		if _, ok := shards[i]; !ok {
			return nil, fmt.Errorf("shard index %d not found", i)
		}
		ret = append(ret, shards[i])
	}
	return ret, nil
}

// parseShardIndexes parses the shard indexes from the string
func parseShardIndexes(src string, shardNum int) ([]int, error) {
	items := strings.Split(src, ",")
	ret := make([]int, 0, len(items))
	for _, item := range items {
		abbrs := strings.Split(item, "-")
		if len(abbrs) > 2 {
			return nil, fmt.Errorf("invalid shard format: %s", item)
		}
		if len(abbrs) == 1 {
			index, err := strconv.Atoi(abbrs[0])
			if err != nil {
				return nil, fmt.Errorf("invalid shard format: %s", abbrs[0])
			}
			if index < 0 || index >= shardNum {
				return nil, fmt.Errorf("shard index %d out of range, expect [0, %d)", index, shardNum)
			}
			ret = append(ret, index)
			continue
		}
		from, err := strconv.Atoi(abbrs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid shard format: %s", abbrs[0])
		}
		to, err := strconv.Atoi(abbrs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid shard format: %s", abbrs[1])
		}
		if from < 0 || from >= shardNum {
			return nil, fmt.Errorf("shard index %d out of range, expect [0, %d)", from, shardNum)
		}
		if to < 0 || to >= shardNum {
			return nil, fmt.Errorf("shard index %d out of range, expect [0, %d)", to, shardNum)
		}
		if from >= to {
			return nil, fmt.Errorf("invalid shard format: %s", item)
		}
		for ; from <= to; from++ {
			ret = append(ret, from)
		}
	}
	return ret, nil
}

// ShardConfig is the configuration of a shard
type ShardConfig struct {
	// the specific root path of the shard
	// the shard path will be {DBPath}/shard0000, {DBPath}/shard0001, ...
	DBPath string
	// Supports multiple shard index formats to simplify configuration,
	// such as "0-7" or "0-1,6-7" or "2,3,4,5"
	Indexes string
}

// Database is the sharding database
type Database struct {
	cfg            *Config
	shardCfgs      []ShardConfig // the final config of each shard
	shards         []ethdb.KeyValueStore
	shardIndexFunc ShardIndexFunc
}

// New creates a new sharding database
// TODO(galaio): Correct the config and print the log using the number of stored shards
func New(cfg *Config, cache int, handles int, readonly bool, f ShardIndexFunc) (*Database, error) {
	if f == nil {
		return nil, fmt.Errorf("shardIndexFunc is not set")
	}
	if err := cfg.SanityCheck(); err != nil {
		return nil, err
	}

	shardCfgs, err := cfg.parseShards()
	if err != nil {
		return nil, err
	}

	shardCache := cache / len(shardCfgs)
	shardHandles := handles / len(shardCfgs)

	// Ensure each shard gets at least the minimum cache and handles
	if shardCache < minShardCache {
		shardCache = minShardCache
	}
	if shardHandles < minShardHandles {
		shardHandles = minShardHandles
	}
	shards := make([]ethdb.KeyValueStore, len(shardCfgs))
	for i, shardCfg := range shardCfgs {
		switch cfg.DBType {
		case DBTypePebble:
			db, err := pebble.New(shardCfg.DBPath, shardCache, shardHandles, cfg.Namespace, readonly)
			if err != nil {
				return nil, err
			}
			shards[i] = db
		case DBTypeLeveldb:
			db, err := leveldb.New(shardCfg.DBPath, shardCache, shardHandles, cfg.Namespace, readonly)
			if err != nil {
				return nil, err
			}
			shards[i] = db
		case DBTypeMemory:
			shards[i] = memorydb.New()
		default:
			return nil, fmt.Errorf("unsupported db type: %s", cfg.DBType)
		}
	}

	return &Database{
		cfg:            cfg,
		shardCfgs:      shardCfgs,
		shards:         shards,
		shardIndexFunc: f,
	}, nil
}

// Close closes the sharding database
func (db *Database) Close() error {
	for _, shard := range db.shards {
		shard.Close()
	}
	return nil
}

// Shard returns the shard for a given key
func (db *Database) Shard(key []byte) ethdb.KeyValueStore {
	return db.shards[db.shardIndexFunc(key, db.ShardNum())]
}

// ShardNum returns the number of shards
func (db *Database) ShardNum() int {
	return len(db.shards)
}

// Has returns true if the key is present in the database
func (db *Database) Has(key []byte) (bool, error) {
	return db.Shard(key).Has(key)
}

// Get retrieves the value for a given key
func (db *Database) Get(key []byte) ([]byte, error) {
	return db.Shard(key).Get(key)
}

// Put inserts the value for a given key
func (db *Database) Put(key []byte, value []byte) error {
	return db.Shard(key).Put(key, value)
}

// Delete removes the value for a given key
func (db *Database) Delete(key []byte) error {
	return db.Shard(key).Delete(key)
}

// DeleteRange deletes the range of keys
func (db *Database) DeleteRange(start, end []byte) error {
	for _, shard := range db.shards {
		if err := shard.DeleteRange(start, end); err != nil {
			return err
		}
	}
	return nil
}

// Stat returns the statistic data of the database
func (db *Database) Stat() (string, error) {
	var b strings.Builder
	for i, shard := range db.shards {
		s, err := shard.Stat()
		if err != nil {
			return "", err
		}
		if len(s) > 0 && s[len(s)-1] != '\n' {
			s += "\n"
		}
		fmt.Fprintf(&b, "[shard %d]\n%s", i, s)
	}
	return b.String(), nil
}

// SyncKeyValue syncs the database
func (db *Database) SyncKeyValue() error {
	for _, shard := range db.shards {
		if err := shard.SyncKeyValue(); err != nil {
			return err
		}
	}
	return nil
}

// Compact compacts the database
func (db *Database) Compact(start []byte, limit []byte) error {
	for _, shard := range db.shards {
		if err := shard.Compact(start, limit); err != nil {
			return err
		}
	}
	return nil
}

// shardingBatch implements ethdb.Batch
type shardingBatch struct {
	parent  *Database
	batches []ethdb.Batch
	size    int
}

// ensure ensures the batch for the shard
func (b *shardingBatch) ensure(idx int) ethdb.Batch {
	if b.batches[idx] == nil {
		b.batches[idx] = b.parent.shards[idx].NewBatch()
	}
	return b.batches[idx]
}

// Put inserts the value for a given key
func (b *shardingBatch) Put(key []byte, value []byte) error {
	idx := b.parent.shardIndexFunc(key, b.parent.ShardNum())
	if err := b.ensure(idx).Put(key, value); err != nil {
		return err
	}
	b.size += len(key) + len(value)
	return nil
}

// Delete removes the value for a given key
func (b *shardingBatch) Delete(key []byte) error {
	idx := b.parent.shardIndexFunc(key, b.parent.ShardNum())
	if err := b.ensure(idx).Delete(key); err != nil {
		return err
	}
	b.size += len(key)
	return nil
}

func (b *shardingBatch) ValueSize() int { return b.size }

// Write writes all the batches using atomic commit with retry logic
// It returns the first error if any
func (b *shardingBatch) Write() error {
	return b.atomicCommit()
}

// atomicCommit executes writes to all shards atomically with retry logic
// If any shard fails, it retries with delay and eventually panic
func (b *shardingBatch) atomicCommit() error {
	pendingBatches := b.batches
	attempts := 0

	for len(pendingBatches) > 0 && attempts < maxAtomicCommitRetries {
		// Add delay before retry (except first attempt)
		if attempts > 0 {
			shardBatchRetryMeter.Mark(1)
			delay := time.Duration(attempts) * atomicCommitRetryDelay
			time.Sleep(delay)
		}

		// Execute all pending writes in parallel
		var wg sync.WaitGroup
		resultChan := make(chan shardCommitResult, len(pendingBatches))

		for _, batch := range pendingBatches {
			if batch != nil {
				wg.Add(1)
				go func(b ethdb.Batch) {
					defer wg.Done()
					resultChan <- shardCommitResult{
						batch: b,
						err:   b.Write(),
					}
				}(batch)
			}
		}

		wg.Wait()
		close(resultChan)

		// Collect failed batches for next retry
		var nextBatches []ethdb.Batch
		for result := range resultChan {
			if result.err != nil {
				log.Warn("Shard batch write failed", "error", result.err)
				nextBatches = append(nextBatches, result.batch)
			}
		}

		if len(nextBatches) == 0 {
			return nil // All succeeded
		}

		pendingBatches = nextBatches
		attempts++
	}

	// If we reach here, all retries failed
	if len(pendingBatches) > 0 {
		log.Crit("atomicCommit failed after all retries", "failedShards", len(pendingBatches))
	}

	return nil
}

// Reset resets all the batches
func (b *shardingBatch) Reset() {
	for i, batch := range b.batches {
		if batch != nil {
			batch.Reset()
			b.batches[i] = nil
		}
	}
	b.size = 0
}

// Replay replays all the batches in parallel
// It returns the first error if any
func (b *shardingBatch) Replay(w ethdb.KeyValueWriter) error {
	for _, batch := range b.batches {
		if batch != nil {
			if err := batch.Replay(w); err != nil {
				return err
			}
		}
	}
	return nil
}

// NewBatch creates a new batch
func (db *Database) NewBatch() ethdb.Batch {
	return &shardingBatch{parent: db, batches: make([]ethdb.Batch, len(db.shards))}
}

// NewBatchWithSize creates a new batch with a given size
func (db *Database) NewBatchWithSize(size int) ethdb.Batch {
	// lazily allocate; underlying batch sizes will be decided by engines
	return db.NewBatch()
}

// shardingIterator implements ethdb.Iterator
// It caches one item per shard for performance
type shardingIterator struct {
	iters     []ethdb.Iterator
	cache     iterItem
	iterIndex int
}

// iterItem is a key-value pair
type iterItem struct {
	key []byte
	val []byte
}

func newIterItem(key []byte, val []byte) iterItem {
	c := iterItem{
		key: make([]byte, len(key)),
		val: make([]byte, len(val)),
	}
	copy(c.key, key)
	copy(c.val, val)
	return c
}

// Next advances the iterator to the next key/value pair.
// It returns false if the iterator is exhausted.
func (m *shardingIterator) Next() bool {
	if m.iterIndex >= len(m.iters) {
		return false
	}

	iter := m.iters[m.iterIndex]
	if iter.Next() {
		m.cache = newIterItem(iter.Key(), iter.Value())
		return true
	}
	m.iterIndex++
	return m.Next()
}

// Error returns the first error if any
func (m *shardingIterator) Error() error {
	for _, it := range m.iters {
		if it == nil {
			continue
		}
		if err := it.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Key returns the current key
func (m *shardingIterator) Key() []byte {
	return m.cache.key
}

// Value returns the current value
func (m *shardingIterator) Value() []byte {
	return m.cache.val
}

// Release releases the iterator
func (m *shardingIterator) Release() {
	for _, it := range m.iters {
		if it != nil {
			it.Release()
		}
	}
}

// NewIterator creates a new iterator
// Note: the iterator is not thread-safe, so it's not safe to use it in multiple goroutines
// Note: the sharding iterator will not return ordered key-value pairs
func (db *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	iters := make([]ethdb.Iterator, len(db.shards))
	for i, shard := range db.shards {
		iters[i] = shard.NewIterator(prefix, start)
	}
	return &shardingIterator{
		iters:     iters,
		cache:     iterItem{},
		iterIndex: 0,
	}
}
