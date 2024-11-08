package bboltdb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"go.etcd.io/bbolt"
)

// Database is a persistent key-value store based on the bbolt storage engine.
// Apart from basic data storage functionality it also supports batch writes and
// iterating over the keyspace in binary-alphabetical order.
type Database struct {
	fn                  string        // Filename for reporting
	db                  *bbolt.DB     // Underlying bbolt storage engine
	mu                  sync.RWMutex  // Mutex to ensure atomic write operations
	compTimeMeter       metrics.Meter // Meter for measuring the total time spent in database compaction
	compReadMeter       metrics.Meter // Meter for measuring the data read during compaction
	compWriteMeter      metrics.Meter // Meter for measuring the data written during compaction
	writeDelayNMeter    metrics.Meter // Meter for measuring the write delay number due to database compaction
	writeDelayMeter     metrics.Meter // Meter for measuring the write delay duration due to database compaction
	diskSizeGauge       metrics.Gauge // Gauge for tracking the size of all the levels in the database
	diskReadMeter       metrics.Meter // Meter for measuring the effective amount of data read
	diskWriteMeter      metrics.Meter // Meter for measuring the effective amount of data written
	memCompGauge        metrics.Gauge // Gauge for tracking the number of memory compaction
	level0CompGauge     metrics.Gauge // Gauge for tracking the number of table compaction in level0
	nonlevel0CompGauge  metrics.Gauge // Gauge for tracking the number of table compaction in non0 level
	seekCompGauge       metrics.Gauge // Gauge for tracking the number of table compaction caused by read opt
	manualMemAllocGauge metrics.Gauge // Gauge for tracking amount of non-managed memory currently allocated

	levelsGauge []metrics.Gauge // Gauge for tracking the number of tables in levels

	//quitLock sync.RWMutex    // Mutex protecting the quit channel and the closed flag
	//quitChan chan chan error // Quit channel to stop the metrics collection before closing the database
	closed bool // keep track of whether we're Closed

	log log.Logger // Contextual logger tracking the database path

	activeComp          int           // Current number of active compactions
	compStartTime       time.Time     // The start time of the earliest currently-active compaction
	compTime            atomic.Int64  // Total time spent in compaction in ns
	level0Comp          atomic.Uint32 // Total number of level-zero compactions
	nonLevel0Comp       atomic.Uint32 // Total number of non level-zero compactions
	writeDelayStartTime time.Time     // The start time of the latest write stall
	writeDelayCount     atomic.Int64  // Total number of write stall counts
	writeDelayTime      atomic.Int64  // Total time spent in write stalls

	writeOptions *pebble.WriteOptions
}

// dumpGoroutines dumps the stack trace of all goroutines.
// dumpGoroutines dumps the stack trace of all goroutines.
func dumpGoroutines() {
	fmt.Println("=== Starting goroutine stack dump ===")
	buf := make([]byte, 1<<22) // 4 MB buffer to store stack traces
	stackLen := runtime.Stack(buf, true)
	fmt.Printf("=== Goroutine stack dump ===\n%s\n", buf[:stackLen])
	fmt.Println("=== End of goroutine stack dump ===")

	time.Sleep(30 * time.Second)
	buf2 := make([]byte, 1<<22) // 4 MB buffer to store stack traces
	stackLen = runtime.Stack(buf2, true)

	fmt.Printf("=== Goroutine stack dump agagin===\n%s\n", buf2[:stackLen])
	fmt.Println("=== End of goroutine stack dump ===")

	time.Sleep(30 * time.Second)
	buf3 := make([]byte, 1<<22) // 4 MB buffer to store stack traces
	stackLen = runtime.Stack(buf2, true)

	fmt.Printf("=== Goroutine stack dump agagin===\n%s\n", buf3[:stackLen])
	fmt.Println("=== End of goroutine stack dump ===")
}

func (d *Database) onCompactionBegin(info pebble.CompactionInfo) {
	if d.activeComp == 0 {
		d.compStartTime = time.Now()
	}
	l0 := info.Input[0]
	if l0.Level == 0 {
		d.level0Comp.Add(1)
	} else {
		d.nonLevel0Comp.Add(1)
	}
	d.activeComp++
}

func (d *Database) onCompactionEnd(info pebble.CompactionInfo) {
	if d.activeComp == 1 {
		d.compTime.Add(int64(time.Since(d.compStartTime)))
	} else if d.activeComp == 0 {
		panic("should not happen")
	}
	d.activeComp--
}

func (d *Database) onWriteStallBegin(b pebble.WriteStallBeginInfo) {
	d.writeDelayStartTime = time.Now()
}

func (d *Database) onWriteStallEnd() {
	d.writeDelayTime.Add(int64(time.Since(d.writeDelayStartTime)))
}

// New creates a new instance of Database.
func New(file string, cache int, handles int, namespace string, readonly bool, ephemeral bool) (*Database, error) {
	// Open the bbolt database file

	options := &bbolt.Options{Timeout: 0,
		ReadOnly: readonly,
		NoSync:   ephemeral,
	}

	fullpath := filepath.Join(file, "bbolt.db")
	dir := filepath.Dir(fullpath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %v", err)
	}
	innerDB, err := bbolt.Open(fullpath, 0600, options)
	if err != nil {
		panic("open db err" + err.Error())
		return nil, fmt.Errorf("failed to open bbolt database: %v", err)
	}

	// Create the default bucket if it does not exist
	err = innerDB.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("ethdb"))
		if err != nil {
			panic("fail to create bucket")
		}
		return err
	})
	if err != nil {
		innerDB.Close()
		return nil, fmt.Errorf("failed to create default bucket: %v", err)
	}

	db := &Database{
		fn: file,
		db: innerDB,
	}

	db.db = innerDB

	return db, nil
}

// Put adds the given value under the specified key to the database.
func (d *Database) Put(key []byte, value []byte) error {
	/*
		d.mu.Lock()
		defer d.mu.Unlock()
		log.Info("db write begin")
		start := time.Now()
		defer func() {
			log.Info("db write cost time", "time", time.Since(start).Milliseconds())
		}()

	*/
	return d.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			panic("put db bucket is nil")
			return fmt.Errorf("bucket does not exist")
		}
		err := bucket.Put(key, value)
		if err != nil {
			panic("put db err" + err.Error())
		}
		log.Info("db write txn finish")
		return err
	})
}

// Get retrieves the value corresponding to the specified key from the database.
func (d *Database) Get(key []byte) ([]byte, error) {
	//	d.mu.RLock()
	//	defer d.mu.RUnlock()

	var result []byte
	if err := d.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			return fmt.Errorf("bucket does not exist")
		}
		log.Info("read key", "key", string(key))
		result = bucket.Get(key)
		return nil
	}); err != nil {
		if err != nil {
			panic("get  db err" + err.Error())
		}
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("key not found")
	}
	return result, nil
}

// Delete removes the specified key from the database.
func (d *Database) Delete(key []byte) error {
	start := time.Now()
	defer func() {
		log.Info("db delete cost time", "time", time.Since(start).Milliseconds())
	}()
	//	d.mu.Lock()
	//	defer d.mu.Unlock()
	return d.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			return fmt.Errorf("bucket does not exist")
		}
		err := bucket.Delete(key)
		if err != nil {
			panic("delete db err" + err.Error())
		}
		log.Info("db delete txn finish")
		return err
	})
}

// Close closes the database file.
func (d *Database) Close() error {
	//	d.mu.Lock()
	//	defer d.mu.Unlock()
	fmt.Println("close db1")

	if d.closed {
		return nil
	}

	fmt.Println("close db2")

	d.closed = true
	err := d.db.Close()
	if err != nil {
		log.Info("close db fail", "err", err.Error())
	}
	fmt.Println("finish close")
	return nil
}

// Has checks if the given key exists in the database.
func (d *Database) Has(key []byte) (bool, error) {
	//	d.mu.RLock()
	//	defer d.mu.RUnlock()

	var has bool
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("ethdb"))
		if b == nil {
			has = false
		} else {
			v := b.Get(key)
			log.Info("has read key", "key", string(key))
			has = v != nil
		}
		return nil
	})
	return has, err
}

// Stat returns a particular internal stat of the database.
func (d *Database) Stat(property string) (string, error) {
	if property == "" {
		property = "bbolt.stats"
	} else if !strings.HasPrefix(property, "bbolt.") {
		property = "bbolt." + property
	}
	stats := d.db.Stats()

	return fmt.Sprintf("%v", stats), nil
}

// DeleteRange deletes all of the keys (and values) in the range [start, end)
// (inclusive on start, exclusive on end).
func (d *Database) DeleteRange(start, end []byte) error {
	//	d.mu.Lock()
	//	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("database is closed")
	}
	start1 := time.Now()
	defer func() {
		log.Info("db delete range cost time", "time", time.Since(start1).Milliseconds())
	}()

	return d.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			return fmt.Errorf("bucket no exixt")
		}
		cursor := bucket.Cursor()
		for k, _ := cursor.Seek(start); k != nil && string(k) < string(end); k, _ = cursor.Next() {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
		log.Info("db delete range txn finish")
		return nil
	})
}

func (d *Database) Compact(start []byte, limit []byte) error {
	return nil
}

// BBoltIterator is an iterator for the bbolt database.
type BBoltIterator struct {
	//	tx       *bbolt.Tx
	//	cursor   *bbolt.Cursor
	db       *bbolt.DB
	prefix   []byte
	start    []byte
	key      []byte
	value    []byte
	firstKey bool
	emptyDB  bool
}

func (d *Database) NewSeekIterator(prefix, key []byte) ethdb.Iterator {
	// Start a read-write transaction to create the bucket if it does not exist.
	tx, _ := d.db.Begin(false) // Begin a read-write transaction
	bucket := tx.Bucket([]byte("ethdb"))

	if bucket == nil {
		panic("bucket is nil in iterator")
	}

	cursor := bucket.Cursor()
	cursor.Seek(prefix)

	return &BBoltIterator{prefix: prefix, start: key}
}

// NewIterator returns a new iterator for traversing the keys in the database.
func (d *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	var k, v []byte
	err := d.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			tx.Rollback()
			panic("bucket is nil")
		}
		cursor := bucket.Cursor()

		if len(prefix) > 0 && len(start) > 0 {
			k, v = cursor.Seek(append(prefix, start...))

			if k != nil && !bytes.HasPrefix(k, prefix) {
				k, v = nil, nil
			}
		} else if len(prefix) > 0 {
			k, v = cursor.Seek(prefix)
			if k != nil && !bytes.HasPrefix(k, prefix) {
				k, v = nil, nil
			}
		} else if len(start) > 0 {
			k, v = cursor.Seek(start)
		} else {
			k, v = cursor.First()
		}
		return nil
	})
	if err != nil {
		panic("err next:" + err.Error())
	}

	log.Info("iterator begin")

	return &BBoltIterator{
		//	tx:       tx,
		//	cursor:   cursor,
		db:       d.db,
		prefix:   prefix,
		start:    start,
		key:      k,
		value:    v,
		firstKey: true,
	}
}

// Next moves the iterator to the next key/value pair.
func (it *BBoltIterator) Next() bool {
	//log.Info("iterator next ")
	var k, v []byte
	if it.firstKey {
		k, v = it.key, it.value
		it.firstKey = false
		if k == nil {
			fmt.Println("key is nil")
		}
	} else {
		err := it.db.View(func(tx *bbolt.Tx) error {
			cursor := tx.Bucket([]byte("ethdb")).Cursor()

			cursor.Seek(it.key)
			k, v = cursor.Next()
			return nil
		})
		if err != nil {
			panic("err next:" + err.Error())
		}
	}

	if k != nil && len(it.prefix) > 0 && !bytes.HasPrefix(k, it.prefix) {
		k = nil
	}

	if k == nil {
		return false
	}

	it.key = k
	it.value = v
	return true
}

// Seek moves the iterator to the given key or the closest following key.
// Returns true if the iterator is pointing at a valid entry and false otherwise.
func (it *BBoltIterator) Seek(key []byte) bool {
	//	it.key, it.value = it.cursor.Seek(key
	err := it.db.View(func(tx *bbolt.Tx) error {
		cursor := tx.Bucket([]byte("ethdb")).Cursor()
		it.key, it.value = cursor.Seek(key)

		if it.key != nil && string(it.key) >= string(key) {
			it.key, it.value = cursor.Prev()
		}
		return nil
	})
	if err != nil {
		panic("err next:" + err.Error())
	}

	return it.key != nil
}

// Error returns any accumulated error.
func (it *BBoltIterator) Error() error {
	// BBolt iterator does not return accumulated errors
	return nil
}

// Key returns the key of the current key/value pair, or nil if done.
func (it *BBoltIterator) Key() []byte {
	if it.key == nil {
		return nil
	}
	return it.key
}

// Value returns the value of the current key/value pair, or nil if done.
func (it *BBoltIterator) Value() []byte {
	if it.value == nil {
		return nil
	}
	return it.value
}

// Release releases associated resources.
func (it *BBoltIterator) Release() {
	fmt.Println("iterator release1")
	/*
		if it.tx != nil {
			_ = it.tx.Rollback()

			log.Info("iterator release2")
			it.tx = nil
		}
		it.cursor = nil

	*/
	it.db = nil
	it.key = nil
	it.value = nil
}

// Batch is a write-only batch that commits changes to its host database when Write is called.
type batch struct {
	db *Database
	//	ops        []func(*bbolt.Tx) error
	size       int
	operations []operation
}

type operation struct {
	key   []byte
	value []byte
	del   bool
}

// NewBatch creates a new batch for batching database operations.
func (d *Database) NewBatch() ethdb.Batch {
	return &batch{
		db: d,
		//	ops:        make([]func(*bbolt.Tx) error, 0),
		operations: make([]operation, 0),
	}
}

// NewBatchWithSize creates a write-only database batch with pre-allocated buffer.
func (d *Database) NewBatchWithSize(size int) ethdb.Batch {
	return &batch{
		db:         d,
		operations: make([]operation, 0, size),
	}
}

// Put inserts the given value into the batch for later committing.
func (b *batch) Put(key, value []byte) error {
	b.operations = append(b.operations, operation{
		key:   key,
		value: value,
		del:   false,
	})

	b.size += len(key) + len(value)
	return nil
}

// Delete inserts a key removal into the batch for later committing.
func (b *batch) Delete(key []byte) error {
	b.operations = append(b.operations, operation{
		key: key,
		del: true,
	})

	b.size += len(key)
	return nil
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int {
	return b.size
}

// Write flushes any accumulated data to disk.
/*
func (b *batch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	log.Info("batch write begin")
	start := time.Now()
	defer func() {
		log.Info("batch txn write cost time", "time", time.Since(start).Milliseconds())
	}()
	if len(b.operations) == 0 {
		log.Info("batch write empty")
		return nil
	}

	return b.db.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		for _, op := range b.operations {
			log.Info("batch write op", "msg", string(op.key))
			if op.del {
				if err := bucket.Delete(op.key); err != nil {
					log.Info("batch write err" + err.Error())
					return err
				}
			} else {
				if err := bucket.Put(op.key, op.value); err != nil {
					log.Info("batch write err" + err.Error())
					return err
				}
			}
		}
		log.Info("batch write txn finish")
		return nil
	})
}

*/
func (b *batch) Write() error {
	//	b.db.mu.Lock()
	//	defer b.db.mu.Unlock()
	log.Info("batch write begin")
	start := time.Now()
	defer func() {
		log.Info("batch txn write cost time", "time", time.Since(start).Milliseconds())
	}()

	if len(b.operations) == 0 {
		log.Info("batch write empty")
		return nil
	}

	return b.db.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		for _, op := range b.operations {
			log.Info("batch write op", "msg", string(op.key))
			if op.del {
				if err := bucket.Delete(op.key); err != nil {
					log.Info("batch write err" + err.Error())
					return err
				}
			} else {
				if err := bucket.Put(op.key, op.value); err != nil {
					log.Info("batch write err" + err.Error())
					return err
				}
			}
		}
		log.Info("batch write txn finish")
		return nil
	})
}

func (b *batch) DeleteRange(start, end []byte) error {
	b.db.DeleteRange(start, end)
	b.size += len(start)
	b.size += len(end)
	return nil
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	//b.ops = nil
	b.size = 0
	b.operations = b.operations[:0]
}

// Replay replays the batch contents.
func (b *batch) Replay(w ethdb.KeyValueWriter) error {
	//	b.db.mu.Lock()
	//	defer b.db.mu.Unlock()
	for _, op := range b.operations {
		if op.del {
			if err := w.Delete(op.key); err != nil {
				fmt.Println("replay delete err")
				return err
			}
		} else {
			if err := w.Put(op.key, op.value); err != nil {
				fmt.Println("replay put err")
				return err
			}
		}
	}
	return nil
}

/*
// snapshot wraps a bbolt transaction for implementing the Snapshot interface.
type snapshot struct {
	tx *bbolt.Tx
}

// NewSnapshot creates a database snapshot based on the current state.
// The created snapshot will not be affected by all following mutations
// happened on the database.
func (d *Database) NewSnapshot() (ethdb.Snapshot, error) {
	// Start a read-only transaction that will be used as the snapshot
	tx, err := d.db.Begin(false)
	if err != nil {
		return nil, err
	}
	return &snapshot{
		tx: tx,
	}, nil
}

// Has retrieves if a key is present in the snapshot backing by a key-value
// data store.
func (snap *snapshot) Has(key []byte) (bool, error) {
	bucket := snap.tx.Bucket([]byte("ethdb"))
	if bucket == nil {
		return false, nil
	}

	value := bucket.Get(key)
	return value != nil, nil
}

// Get retrieves the given key if it's present in the snapshot backing by
// key-value data store.
func (snap *snapshot) Get(key []byte) ([]byte, error) {
	bucket := snap.tx.Bucket([]byte("ethdb"))
	if bucket == nil {
		return nil, errors.New("bucket not found")
	}

	value := bucket.Get(key)
	if value == nil {
		return nil, errors.New("not found")
	}

	ret := make([]byte, len(value))
	copy(ret, value)
	return ret, nil
}

// Release releases associated resources. Release should always succeed and can
// be called multiple times without causing error.
func (snap *snapshot) Release() {
	if snap.tx != nil {
		snap.tx.Rollback()
		snap.tx = nil
	}
}

*/

// snapshot wraps a database snapshot for implementing the Snapshot interface.
type snapshot struct {
	snapshotDB *bbolt.DB // db snapshot
	path       string    // file path
}

// NewSnapshot creates a database snapshot based on the current state.
func (d *Database) NewSnapshot() (ethdb.Snapshot, error) {
	originalPath := d.db.Path()
	dir := filepath.Dir(originalPath)
	timestamp := time.Now().UnixNano()
	snapPath := filepath.Join(dir, fmt.Sprintf("%v.%d.snapshot", filepath.Base(originalPath), timestamp))

	tx, err := d.db.Begin(false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 复制数据库文件
	if err := func() error {
		sourceFile, err := os.Open(originalPath)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destFile, err := os.Create(snapPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, sourceFile)
		return err
	}(); err != nil {
		return nil, fmt.Errorf("failed to copy database file: %v", err)
	}

	snapDB, err := bbolt.Open(snapPath, 0600, &bbolt.Options{
		ReadOnly: true,
	})
	if err != nil {
		os.Remove(snapPath)
		return nil, fmt.Errorf("failed to open snapshot database: %v", err)
	}

	return &snapshot{
		snapshotDB: snapDB,
		path:       snapPath,
	}, nil
}

// Has retrieves if a key is present in the snapshot backing by a key-value
// data store.
func (snap *snapshot) Has(key []byte) (bool, error) {
	if snap.snapshotDB == nil {
		return false, errors.New("snapshot released")
	}

	var exists bool
	err := snap.snapshotDB.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			return nil
		}
		exists = bucket.Get(key) != nil
		return nil
	})
	return exists, err
}

// Get retrieves the given key if it's present in the snapshot backing by
// key-value data store.
func (snap *snapshot) Get(key []byte) ([]byte, error) {
	if snap.snapshotDB == nil {
		return nil, errors.New("snapshot released")
	}

	var value []byte
	err := snap.snapshotDB.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("ethdb"))
		if bucket == nil {
			return errors.New("bucket not found")
		}
		v := bucket.Get(key)
		if v == nil {
			return errors.New("not found")
		}
		value = make([]byte, len(v))
		copy(value, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

// Release releases associated resources. Release should always succeed and can
// be called multiple times without causing error.
func (snap *snapshot) Release() {
	if snap.snapshotDB != nil {
		snap.snapshotDB.Close()
		snap.snapshotDB = nil
	}
	if snap.path != "" {
		os.Remove(snap.path)
		snap.path = ""
	}
}
