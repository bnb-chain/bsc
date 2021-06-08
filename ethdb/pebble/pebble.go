package pebble

import (
	"fmt"

	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

// Database is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace in
// binary-alphabetical order.
type Database struct {
	fn string     // filename for reporting
	db *pebble.DB // LevelDB instance

	compTimeMeter      metrics.Meter // Meter for measuring the total time spent in database compaction
	compReadMeter      metrics.Meter // Meter for measuring the data read during compaction
	compWriteMeter     metrics.Meter // Meter for measuring the data written during compaction
	writeDelayNMeter   metrics.Meter // Meter for measuring the write delay number due to database compaction
	writeDelayMeter    metrics.Meter // Meter for measuring the write delay duration due to database compaction
	diskSizeGauge      metrics.Gauge // Gauge for tracking the size of all the levels in the database
	diskReadMeter      metrics.Meter // Meter for measuring the effective amount of data read
	diskWriteMeter     metrics.Meter // Meter for measuring the effective amount of data written
	memCompGauge       metrics.Gauge // Gauge for tracking the number of memory compaction
	level0CompGauge    metrics.Gauge // Gauge for tracking the number of table compaction in level0
	nonlevel0CompGauge metrics.Gauge // Gauge for tracking the number of table compaction in non0 level
	seekCompGauge      metrics.Gauge // Gauge for tracking the number of table compaction caused by read opt

	quitLock sync.Mutex      // Mutex protecting the quit channel access
	quitChan chan chan error // Quit channel to stop the metrics collection before closing the database

	log log.Logger // Contextual logger tracking the database path
}

// New returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
func New(file string, cache int, handles int, namespace string, readonly bool) (*Database, error) {
	return NewCustom(file, namespace, func(options *pebble.Options) {
		// Ensure we have some minimal caching and file guarantees
		if readonly {
			options.ReadOnly = true
		}
	})
}

// NewCustom returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
// The customize function allows the caller to modify the leveldb options.
func NewCustom(file string, namespace string, customize func(options *pebble.Options)) (*Database, error) {
	options := configureOptions(customize)
	logger := log.New("database", file)

	// Open the db and recover any potential corruptions
	db, err := pebble.Open(file, options)
	if err != nil {
		return nil, err
	}

	// Assemble the wrapper with all the registered metrics
	ldb := &Database{
		fn:       file,
		db:       db,
		log:      logger,
		quitChan: make(chan chan error),
	}

	return ldb, nil
}

// configureOptions sets some default options, then runs the provided setter.
func configureOptions(customizeFn func(*pebble.Options)) *pebble.Options {
	// Set default options
	options := &pebble.Options{}
	// Allow caller to make custom modifications to the options
	if customizeFn != nil {
		customizeFn(options)
	}
	return options
}

// Close stops the metrics collection, flushes any pending data to disk and closes
// all io accesses to the underlying key-value store.
func (db *Database) Close() error {
	db.quitLock.Lock()
	defer db.quitLock.Unlock()

	if db.quitChan != nil {
		errc := make(chan error)
		db.quitChan <- errc
		if err := <-errc; err != nil {
			db.log.Error("Metrics collection failed", "err", err)
		}
		db.quitChan = nil
	}
	return db.db.Close()
}

// Has retrieves if a key is present in the key-value store.
func (db *Database) Has(key []byte) (bool, error) {
	dat, closer, err := db.db.Get(key)
	if err != nil {
		return false, err
	}
	err = closer.Close()
	if err != nil {
		return false, err
	}

	return dat != nil, nil
}

// Get retrieves the given key if it's present in the key-value store.
func (db *Database) Get(key []byte) ([]byte, error) {
	dat, closer, err := db.db.Get(key)
	if err != nil {
		return nil, err
	}
	err = closer.Close()
	if err != nil {
		return nil, err
	}
	return dat, nil
}

// Put inserts the given value into the key-value store.
func (db *Database) Put(key []byte, value []byte) error {
	return db.db.Set(key, value, nil)
}

// Delete removes the key from the key-value store.
func (db *Database) Delete(key []byte) error {
	return db.db.Delete(key, nil)
}

// NewBatch creates a write-only key-value store that buffers changes to its host
// database until a final write is called.
func (db *Database) NewBatch() ethdb.Batch {
	return &batch{
		db: db.db,
		b:  db.db.NewBatch(),
	}
}

// NewIterator creates a binary-alphabetical iterator over a subset
// of database content with a particular key prefix, starting at a particular
// initial key (or after, if it does not exist).
func (db *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return &pebbleIterator{db.db.NewIter(bytesPrefixIterOptions(prefix, start))}
}

func bytesPrefixIterOptions(prefix []byte, start []byte) *pebble.IterOptions {
	var limit []byte
	for i := len(prefix) - 1; i >= 0; i-- {
		c := prefix[i]
		if c < 0xff {
			limit = make([]byte, i+1)
			copy(limit, prefix)
			limit[i] = c + 1
			break
		}
	}

	return &pebble.IterOptions{
		LowerBound: append(prefix, start...),
		UpperBound: limit,
	}
}

type pebbleIterator struct {
	*pebble.Iterator
}

func (it *pebbleIterator) Release() {
	err := it.Close()
	if err != nil {
		panic(err)
	}
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	// TODO: did not find property in pebble
	return "", nil
}

// Compact flattens the underlying data store for the given key range. In essence,
// deleted and overwritten versions are discarded, and the data is rearranged to
// reduce the cost of operations needed to access them.
//
// A nil start is treated as a key before all keys in the data store; a nil limit
// is treated as a key after all keys in the data store. If both is nil then it
// will compact entire data store.
func (db *Database) Compact(start []byte, limit []byte) error {
	// TODO: should make sure this compact works like in the comments
	return db.db.Compact(start, limit)
}

// Path returns the path to the database directory.
func (db *Database) Path() string {
	return db.fn
}

// batch is a write-only leveldb batch that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type batch struct {
	db   *pebble.DB
	b    *pebble.Batch
	size int
}

// Put inserts the given value into the batch for later committing.
func (b *batch) Put(key, value []byte) error {
	err := b.b.Set(key, value, nil)
	if err != nil {
		// TODO: should we swallow this error?
		panic(err)
	}
	b.size += len(value)
	return nil
}

// Delete inserts the a key removal into the batch for later committing.
func (b *batch) Delete(key []byte) error {
	err := b.b.Delete(key, nil)
	if err != nil {
		// TODO: should we swallow this error?
		panic(err)
	}
	b.size += len(key)
	return nil
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int {
	return b.size
}

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	return b.db.Apply(b.b, nil)
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.b.Reset()
	b.size = 0
}

// Replay replays the batch contents.
func (b *batch) Replay(w ethdb.KeyValueWriter) error {
	batchReader := b.b.Reader()

	for {
		if len(batchReader) == 0 {
			return nil
		}

		kind, key, value, ok := batchReader.Next()
		if !ok {
			return fmt.Errorf("batch corrupted")
		}

		if kind == pebble.InternalKeyKindDelete {
			if err := w.Delete(key); err != nil {
				return err
			}
		}

		if kind == pebble.InternalKeyKindSet {
			if err := w.Put(key, value); err != nil {
				return err
			}
		}
	}
}
