// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"errors"
	"runtime"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/snap"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// NodeReader warps all the necessary functions for accessing trie node.
type NodeReader interface {
	// GetReader returns a reader for accessing all trie nodes with provided
	// state root. Nil is returned in case the state is not available.
	GetReader(root common.Hash) Reader
}

// Config defines all necessary options for database.
type Config struct {
	Cache     int          // Memory allowance (MB) to use for caching trie nodes in memory
	Journal   string       // Journal of clean cache to survive node restarts
	Preimages bool         // Flag whether the preimage of trie key is recorded
	NoTries   bool
	Snap      *snap.Config // Configs for experimental path-based scheme, not used yet.
}

// nodeBackend defines the methods needed to access/update trie nodes in
// different state scheme. It's implemented by hashDatabase and snapDatabase.
type nodeBackend interface {
	// Commit writes all relevant trie nodes belonging to the specified state to disk.
	// Report specifies whether logs will be displayed in info level.
	Commit(root common.Hash, report bool) error

	// Initialized returns an indicator if the state data is already initialized
	// according to the state scheme.
	Initialized(genesisRoot common.Hash) bool

	// Size returns the current storage size of the memory cache in front of the
	// persistent database layer.
	Size() common.StorageSize

	// Scheme returns the node scheme used in the database.
	Scheme() string

	// Close closes the trie database backend and releases all held resources.
	Close() error
}

// Database is the wrapper of the underlying nodeBackend which is shared by
// different types of nodeBackends as an entrypoint. It's responsible for all
// interactions relevant with trie nodes and the node preimages.
type Database struct {
	config    *Config          // Configuration for trie database.
	diskdb    ethdb.Database   // Persistent database to store the snapshot
	cleans    *fastcache.Cache // Megabytes permitted using for read caches
	preimages *preimageStore   // The store for caching preimages
	backend   nodeBackend      // The backend for managing trie nodes
}

// prepare initializes the database with provided configs, but the
// database backend is still left as nil.
func prepare(diskdb ethdb.Database, config *Config) *Database {
	var cleans *fastcache.Cache
	if config != nil && config.Cache > 0 {
		if config.Journal == "" {
			cleans = fastcache.New(config.Cache * 1024 * 1024)
		} else {
			cleans = fastcache.LoadFromFileOrNew(config.Journal, config.Cache*1024*1024)
		}
	}
	var preimages *preimageStore
	if config != nil && config.Preimages {
		preimages = newPreimageStore(diskdb)
	}
	return &Database{
		config:    config,
		diskdb:    diskdb,
		cleans:    cleans,
		preimages: preimages,
	}
}

// NewHashDatabase initializes the trie database with legacy hash-based scheme.
func NewHashDatabase(diskdb ethdb.Database) *Database {
	return NewDatabase(diskdb, nil)
}

// NewDatabase initializes the trie database with provided configs.
func NewDatabase(diskdb ethdb.Database, config *Config) *Database {
	db := prepare(diskdb, config)
	if config != nil && config.Snap != nil {
		db.backend = snap.New(diskdb, db.cleans, config.Snap)
	} else {
		db.backend = openHashDatabase(diskdb, db.cleans)
	}
	return db
}

// GetReader returns a reader for accessing all trie nodes with provided
// state root. Nil is returned in case the state is not available.
func (db *Database) GetReader(blockRoot common.Hash) Reader {
	if snap, ok := db.backend.(*snap.Database); ok {
		return snap.GetReader(blockRoot)
	}
	return db.backend.(*hashDatabase).GetReader(blockRoot)
}

// Update performs a state transition by committing dirty nodes contained
// in the given set in order to update state from the specified parent to
// the specified root. The held pre-images accumulated up to this point
// will be flushed in case the size exceeds the threshold.
func (db *Database) Update(root common.Hash, parent common.Hash, nodes *MergedNodeSet) error {
	if db.preimages != nil {
		db.preimages.commit(false)
	}
	if snap, ok := db.backend.(*snap.Database); ok {
		sets := make(map[common.Hash]map[string]*trienode.WithPrev)
		for owner, set := range nodes.sets {
			sets[owner] = set.nodes
		}
		return snap.Update(root, parent, sets)
	}
	return db.backend.(*hashDatabase).Update(root, parent, nodes)
}

// Commit iterates over all the children of a particular node, writes them out
// to disk. As a side effect, all pre-images accumulated up to this point are
// also written.
func (db *Database) Commit(root common.Hash, report bool) error {
	if db.preimages != nil {
		db.preimages.commit(true)
	}
	return db.backend.Commit(root, report)
}

// Size returns the storage size of dirty trie nodes in front of the persistent
// database and the size of cached preimages.
func (db *Database) Size() (common.StorageSize, common.StorageSize) {
	var (
		storages  common.StorageSize
		preimages common.StorageSize
	)
	storages = db.backend.Size()
	if db.preimages != nil {
		preimages = db.preimages.size()
	}
	return storages, preimages
}

// Initialized returns an indicator if the state data is already initialized
// according to the state scheme.
func (db *Database) Initialized(genesisRoot common.Hash) bool {
	return db.backend.Initialized(genesisRoot)
}

// Scheme returns the node scheme used in the database.
func (db *Database) Scheme() string {
	return db.backend.Scheme()
}

// Close flushes the dangling preimages to disk and closes the trie database.
// It is meant to be called when closing the blockchain object, so that all
// resources held can be released correctly.
func (db *Database) Close() error {
	if db.preimages != nil {
		db.preimages.commit(true)
	}
	return db.backend.Close()
}

// Recover rollbacks the database to a specified historical point. The state is
// supported as the rollback destination only if it's canonical state and the
// corresponding trie histories are existent. It's only supported by snap database
// and will return an error for others.
func (db *Database) Recover(target common.Hash) error {
	snapDB, ok := db.backend.(*snap.Database)
	if !ok {
		return errors.New("not supported")
	}
	return snapDB.Recover(target)
}

// Recoverable returns the indicator if the specified state is enabled to be
// recovered. It's only supported by snap database and will return an error
// for others.
func (db *Database) Recoverable(root common.Hash) (bool, error) {
	snapDB, ok := db.backend.(*snap.Database)
	if !ok {
		return false, errors.New("not supported")
	}
	return snapDB.Recoverable(root), nil
}

// Reset wipes all available journal from the persistent database and discard
// all caches and diff layers. Using the given root to create a new disk layer.
// It's only supported by path-based database and will return an error for others.
func (db *Database) Reset(root common.Hash) error {
	snapDB, ok := db.backend.(*snap.Database)
	if !ok {
		return errors.New("not supported")
	}
	return snapDB.Reset(root)
}

// Journal commits an entire diff hierarchy to disk into a single journal entry.
// This is meant to be used during shutdown to persist the snapshot without
// flattening everything down (bad for reorgs). It's only supported by path-based
// database and will return an error for others.
func (db *Database) Journal(root common.Hash) error {
	snapDB, ok := db.backend.(*snap.Database)
	if !ok {
		return errors.New("not supported")
	}
	return snapDB.Journal(root)
}

// SetCacheSize sets the dirty cache size to the provided value(in mega-bytes).
// It's only supported by path-based database and will return an error for others.
func (db *Database) SetCacheSize(size int) error {
	snapDB, ok := db.backend.(*snap.Database)
	if !ok {
		return errors.New("not supported")
	}
	return snapDB.SetCacheSize(size)
}

// saveCache saves clean state cache to given directory path
// using specified CPU cores.
func (db *Database) saveCache(dir string, threads int) error {
	if db.cleans == nil {
		return nil
	}
	log.Info("Writing clean trie cache to disk", "path", dir, "threads", threads)

	start := time.Now()
	err := db.cleans.SaveToFileConcurrent(dir, threads)
	if err != nil {
		log.Error("Failed to persist clean trie cache", "error", err)
		return err
	}
	log.Info("Persisted the clean trie cache", "path", dir, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// SaveCache atomically saves fast cache data to the given dir using all
// available CPU cores.
func (db *Database) SaveCache(dir string) error {
	return db.saveCache(dir, runtime.GOMAXPROCS(0))
}

// SaveCachePeriodically atomically saves fast cache data to the given dir with
// the specified interval. All dump operation will only use a single CPU core.
func (db *Database) SaveCachePeriodically(dir string, interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			db.saveCache(dir, 1)
		case <-stopCh:
			return
		}
	}
}

// DiskDB retrieves the persistent storage backing the trie database.
func (db *Database) DiskDB() ethdb.KeyValueStore {
        return db.diskdb
}
