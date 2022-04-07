package remotedb

import (
    "time"
    "sync"
    "context"
    "errors"
    "math/rand"

    rocks "github.com/go-redis/redis/v8"
    "github.com/ethereum/go-ethereum/ethdb"
    "github.com/ethereum/go-ethereum/log"
)

var (
    // max batch count
    REMOTE_BATCH_COUNT int64 = 100
)

// not support iterator, for archive-compute node
type noIterator struct {
}

// newNoIterator creates a empty iterator
func (db *RocksDB) newNoIterator() ethdb.Iterator {
    return &noIterator{}
}

// Next moves the iterator to the next key/value pair. It returns whether the
// iterator is exhausted.
func (it *noIterator) Next() bool {
    return false
}

// Error returns any accumulated error. Exhausting all the key/value pairs
// is not considered to be an error.
func (it *noIterator) Error() error {
    return errors.New("noIterator not supported")
}

// Key returns the key of the current key/value pair, or nil if done. The caller
// should not modify the contents of the returned slice, and its contents may
// change on the next call to Next.
func (it *noIterator) Key() []byte {
    return nil
}

// Value returns the value of the current key/value pair, or nil if done. The
// caller should not modify the contents of the returned slice, and its contents
// may change on the next call to Next.
func (it *noIterator) Value() []byte {
    return nil
}

// Release releases associated resources. Release should always succeed and can
// be called multiple times without causing error.
func (it *noIterator) Release() {
    return 
}


// NewIterator creates a binary-alphabetical iterator over a subset
// of database content with a particular key prefix, starting at a particular
// initial key (or after, if it does not exist).
func (db *RocksDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
    if db.persistCache == nil {
        return db.newNoIterator()
    }
    return db.newRemoteIterator(prefix, start)
}

// remoteIterator scan remotedb key/value pair 
// store local cache(leveldb or memorydb) for iterator
type remoteIterator struct {
    db      *RocksDB
    cache   ethdb.KeyValueStore
    cacheIt ethdb.Iterator
    prefix  []byte
    start   []byte
    err     error
}

// newRemoteIterator creates a binary-alphabetical iterator by local cache
func (db *RocksDB) newRemoteIterator(prefix []byte, start []byte) ethdb.Iterator {
    it := &remoteIterator {
        db:      db,
        cache:   db.persistCache,
        prefix:  prefix,
        start:   start,
    }

    shards, err := db.client.ClusterSlots(context.Background()).Result() 
    if err != nil {
        log.Error("remotedb iterator cluster slots error", "err", err)
        it.err = err
        return it
    }

    var wg sync.WaitGroup
    for _, shard := range shards {
        r := rand.New(rand.NewSource(time.Now().UnixNano()))
        node := shard.Nodes[r.Intn(len(shard.Nodes))]
        go func(addr string) {
            wg.Add(1)
            defer wg.Done()
            var cursor uint64
            ctx := context.Background()
            prefix := string(append(append(prefix, start...), []byte("*")...))
            nodeClient := rocks.NewClient(db.config.GetIteratorOption(addr))

            for {
                keys, cursor, err := nodeClient.Scan(ctx, cursor, prefix, REMOTE_BATCH_COUNT).Result()
                if err != nil {
                    log.Error("remotedb iterator scan error", "node", addr, "err", err)
                    it.err = err
                    return 
                }
 
                go func (addr string, keys []string) {
                    wg.Add(1)
                    defer wg.Done()
                    nodeClient := rocks.NewClient(db.config.GetIteratorOption(addr))
                    vals, err := nodeClient.MGet(context.Background(), keys[:]...).Result()
                    if err != nil {
                        log.Error("remotedb iterator mget error", "node", addr, "err", err)
                        it.err = err
                        return 
                    }
                    batch := db.persistCache.NewBatch()
                    for idx, key := range keys {
                        batch.Put([]byte(key), []byte(vals[idx].(string)))
                    }
                    if err := batch.Write(); err != nil {
                        log.Error("remotedb iterator batch write persist cache error", "err", err)
                        it.err = err
                        return
                    }
                }(addr, keys)

                if cursor == 0 {
                    break
                }
            }
        }(node.Addr)
    }

    wg.Wait()
    if it.err == nil {
        it.cacheIt = db.persistCache.NewIterator(prefix, start)
    }
    return it
}

// Next moves the iterator to the next key/value pair. It returns whether the
// iterator is exhausted.
func (it *remoteIterator) Next() bool {
    if it.cacheIt == nil {
        return false
    }
    return it.cacheIt.Next()
}

// Error returns any accumulated error. Exhausting all the key/value pairs
// is not considered to be an error.
func (it *remoteIterator) Error() error {
    if it.err != nil {
        return it.err
    }
    if it.cacheIt == nil {
        return errors.New("persist cache iterator is nil")
    }
    return it.cacheIt.Error()
}

// Key returns the key of the current key/value pair, or nil if done. The caller
// should not modify the contents of the returned slice, and its contents may
// change on the next call to Next.
func (it *remoteIterator) Key() []byte {
    if it.cacheIt == nil {
        return nil
    }
    return it.cacheIt.Key()
}

// Value returns the value of the current key/value pair, or nil if done. The
// caller should not modify the contents of the returned slice, and its contents
// may change on the next call to Next.
func (it *remoteIterator) Value() []byte {
     if it.cacheIt == nil {
        return nil
    }
    return it.cacheIt.Value()
}

// Release releases associated resources. Release should always succeed and can
// be called multiple times without causing error.
func (it *remoteIterator) Release() {
    it.prefix = it.prefix[:0]
    it.start = it.start[:0]
    it.cacheIt.Release()
}