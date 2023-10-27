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

package pathdb

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type memtable struct {
	layers    uint64                                    // The number of diff layers aggregated inside
	size      uint64                                    // The size of aggregated writes
	limit     uint64                                    // The maximum memory allowance in bytes
	nodes     map[common.Hash]map[string]*trienode.Node // The dirty node set, mapped by owner and path
	immutable uint64                                    // The flag equal 1, flush nodes to disk background
	resetMux  sync.RWMutex
}

func newMemTable(limit, size uint64, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *memtable {
	return &memtable{
		layers:    layers,
		size:      size,
		limit:     limit,
		nodes:     nodes,
		immutable: 0,
	}
}

func (m *memtable) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	subset, ok := m.nodes[owner]
	if !ok {
		return nil, nil
	}
	n, ok := subset[string(path)]
	if !ok {
		return nil, nil
	}
	if n.Hash != hash {
		dirtyFalseMeter.Mark(1)
		log.Error("Unexpected trie node in node buffer", "owner", owner, "path", path, "expect", hash, "got", n.Hash)
		return nil, newUnexpectedNodeError("dirty", hash, n.Hash, owner, path, n.Blob)
	}
	return n, nil
}

func (m *memtable) commit(nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&m.immutable) == 1 {
		return errWriteImmutable
	}
	var (
		delta         int64
		overwrite     int64
		overwriteSize int64
	)
	for owner, subset := range nodes {
		current, exist := m.nodes[owner]
		if !exist {
			// Allocate a new map for the subset instead of claiming it directly
			// from the passed map to avoid potential concurrent map read/write.
			// The nodes belong to original diff layer are still accessible even
			// after merging, thus the ownership of nodes map should still belong
			// to original layer and any mutation on it should be prevented.
			current = make(map[string]*trienode.Node)
			for path, n := range subset {
				current[path] = n
				delta += int64(len(n.Blob) + len(path))
			}
			m.nodes[owner] = current
			continue
		}
		for path, n := range subset {
			if orig, exist := current[path]; !exist {
				delta += int64(len(n.Blob) + len(path))
			} else {
				delta += int64(len(n.Blob) - len(orig.Blob))
				overwrite++
				overwriteSize += int64(len(orig.Blob) + len(path))
			}
			current[path] = n
		}
		m.nodes[owner] = current
	}
	m.updateSize(delta)
	m.layers++
	gcNodesMeter.Mark(overwrite)
	gcBytesMeter.Mark(overwriteSize)
	return nil
}

// updateSize updates the total cache size by the given delta.
func (m *memtable) updateSize(delta int64) {
	size := int64(m.size) + delta
	if size >= 0 {
		m.size = uint64(size)
		return
	}
	s := m.size
	m.size = 0
	log.Error("Invalid pathdb buffer size", "prev", common.StorageSize(s), "delta", common.StorageSize(delta))
}

func (m *memtable) reset() {
	m.layers = 0
	m.size = 0
	m.nodes = make(map[common.Hash]map[string]*trienode.Node)
	atomic.StoreUint64(&m.immutable, 0)
}

func (m *memtable) empty() bool {
	return m.layers == 0
}

func (m *memtable) flush(db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
	if atomic.LoadUint64(&m.immutable) != 1 {
		return errFlushImmutable
	}

	// Ensure the target state id is aligned with the internal counter.
	head := rawdb.ReadPersistentStateID(db)
	if head+m.layers != id {
		return fmt.Errorf("buffer layers (%d) cannot be applied on top of persisted state id (%d) to reach requested state id (%d)", m.layers, head, id)
	}
	var (
		start = time.Now()
		batch = db.NewBatchWithSize(int(m.size))
	)
	nodes := writeNodes(batch, m.nodes, clean)
	rawdb.WritePersistentStateID(batch, id)

	// Flush all mutations in a single batch
	size := batch.ValueSize()
	if err := batch.Write(); err != nil {
		return err
	}
	commitBytesMeter.Mark(int64(size))
	commitNodesMeter.Mark(int64(nodes))
	commitTimeTimer.UpdateSince(start)
	log.Debug("Persisted pathdb nodes", "nodes", len(m.nodes), "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	m.reset()
	return nil
}

func (m *memtable) merge(m1 *memtable) (*memtable, error) {
	if m == nil && m1 == nil {
		return nil, nil
	}
	if m == nil || m.empty() {
		atomic.StoreUint64(&m1.immutable, 0)
		return m1, nil
	}
	if m1 == nil || m1.empty() {
		atomic.StoreUint64(&m.immutable, 0)
		return m, nil
	}
	if atomic.LoadUint64(&m.immutable) == atomic.LoadUint64(&m1.immutable) {
		return nil, errIncompatibleMerge
	}

	var (
		immutable *memtable
		mutable   *memtable
		res       = &memtable{}
	)
	if atomic.LoadUint64(&m.immutable) == 1 {
		immutable = m
		mutable = m1
	} else {
		immutable = m1
		mutable = m
	}
	res.size = immutable.size + mutable.size
	res.layers = immutable.layers + mutable.layers
	res.limit = immutable.size
	res.nodes = make(map[common.Hash]map[string]*trienode.Node)
	for acc, subTree := range immutable.nodes {
		if _, ok := res.nodes[acc]; !ok {
			res.nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			res.nodes[acc][path] = node
		}
	}

	for acc, subTree := range mutable.nodes {
		if _, ok := res.nodes[acc]; !ok {
			res.nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			res.nodes[acc][path] = node
		}
	}
	return res, nil
}

func (m *memtable) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&m.immutable) != 1 {
		return errRevertImmutable
	}

	// Short circuit if no embedded state transition to revert.
	if m.layers == 0 {
		return errStateUnrecoverable
	}
	m.layers--

	// Reset the entire buffer if only a single transition left.
	if m.layers == 0 {
		m.reset()
		return nil
	}
	var delta int64
	for owner, subset := range nodes {
		current, ok := m.nodes[owner]
		if !ok {
			panic(fmt.Sprintf("non-existent subset (%x)", owner))
		}
		for path, n := range subset {
			orig, ok := current[path]
			if !ok {
				// There is a special case in MPT that one child is removed from
				// a fullNode which only has two children, and then a new child
				// with different position is immediately inserted into the fullNode.
				// In this case, the clean child of the fullNode will also be
				// marked as dirty because of node collapse and expansion.
				//
				// In case of database rollback, don't panic if this "clean"
				// node occurs which is not present in buffer.
				var nhash common.Hash
				if owner == (common.Hash{}) {
					_, nhash = rawdb.ReadAccountTrieNode(db, []byte(path))
				} else {
					_, nhash = rawdb.ReadStorageTrieNode(db, owner, []byte(path))
				}
				// Ignore the clean node in the case described above.
				if nhash == n.Hash {
					continue
				}
				panic(fmt.Sprintf("non-existent node (%x %v) blob: %v", owner, path, crypto.Keccak256Hash(n.Blob).Hex()))
			}
			current[path] = n
			delta += int64(len(n.Blob)) - int64(len(orig.Blob))
		}
	}
	m.updateSize(delta)
	return nil
}

// nodebuffer is a collection of modified trie nodes to aggregate the disk
// write. The content of the nodebuffer must be checked before diving into
// disk (since it basically is not-yet-written data).
type nodebuffer struct {
	mux     sync.RWMutex
	current *memtable
	backup  *memtable

	flushMux sync.RWMutex
	canFlush bool
}

// newNodeBuffer initializes the node buffer with the provided nodes.
func newNodeBuffer(limit int, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *nodebuffer {
	if nodes == nil {
		nodes = make(map[common.Hash]map[string]*trienode.Node)
	}
	var size uint64
	for _, subset := range nodes {
		for path, n := range subset {
			size += uint64(len(n.Blob) + len(path))
		}
	}

	return &nodebuffer{
		current: newMemTable(uint64(limit), size, nodes, layers),
		backup:  newMemTable(uint64(limit), 0, make(map[common.Hash]map[string]*trienode.Node), 0),
	}
}

// node retrieves the trie node with given node info.
func (b *nodebuffer) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	node, err := b.current.node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return b.backup.node(owner, path, hash)
	}
	return node, nil
}

// commit merges the dirty nodes into the nodebuffer. This operation won't take
// the ownership of the nodes map which belongs to the bottom-most diff layer.
// It will just hold the node references from the given map which are safe to
// copy.
func (b *nodebuffer) commit(nodes map[common.Hash]map[string]*trienode.Node) *nodebuffer {
	b.mux.Lock()
	defer b.mux.Unlock()

	err := b.current.commit(nodes)
	if err != nil {
		log.Crit("[BUG] failed to commit nodes to nodebuffer", "error", err)
	}
	return b
}

// revert is the reverse operation of commit. It also merges the provided nodes
// into the nodebuffer, the difference is that the provided node set should
// revert the changes made by the last state transition.
func (b *nodebuffer) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
	b.mux.Lock()
	defer b.mux.Unlock()
	var err error
	b.current, err = b.current.merge(b.backup)
	if err != nil {
		log.Crit("[BUG] failed to merge memory table under revert nodebuffer", "error", err)
	}
	b.backup.reset()
	return b.current.revert(db, nodes)
}

// reset cleans up the disk cache.
func (b *nodebuffer) reset() {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.current.reset()
	b.backup.reset()
}

// empty returns an indicator if nodebuffer contains any state transition inside.
func (b *nodebuffer) empty() bool {
	b.mux.RLock()
	defer b.mux.RUnlock()
	return b.current.empty() && b.backup.empty()
}

func (b *nodebuffer) safeFlush() bool {
	b.flushMux.RLock()
	defer b.flushMux.RUnlock()
	return b.canFlush
}

func (b *nodebuffer) enableFlush() {
	b.flushMux.Lock()
	defer b.flushMux.Unlock()
	b.canFlush = true
}

func (b *nodebuffer) disableFlush() {
	b.flushMux.Lock()
	defer b.flushMux.Unlock()
	b.canFlush = false
}

// setSize sets the buffer size to the provided number, and invokes a flush
// operation if the current memory usage exceeds the new limit.
//func (b *nodebuffer) setSize(size int, db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
//	b.limit = uint64(size)
//	return b.flush(db, clean, id, false)
//}

// flush persists the in-memory dirty trie node into the disk if the configured
// memory threshold is reached. Note, all data must be written atomically.
func (b *nodebuffer) flush(db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64, force bool) error {
	b.mux.Lock()
	defer b.mux.Unlock()

	if force {
		for {
			if atomic.LoadUint64(&b.backup.immutable) == 1 {
				time.Sleep(time.Duration(DefaultBackgroundFlushInterval) * time.Second)
				log.Info("waiting background memory table flush to disk for force flush node buffer")
				continue
			}
			atomic.StoreUint64(&b.current.immutable, 1)
			return b.current.flush(db, clean, id)
		}
	}

	if b.current.size < b.current.limit {
		return nil
	}

	// background flush doing
	if atomic.LoadUint64(&b.backup.immutable) == 1 {
		return nil
	}

	atomic.StoreUint64(&b.current.immutable, 1)
	tmp := b.backup
	b.backup = b.current
	b.current = tmp

	persistId := id - b.current.layers
	go func() {
		for {
			//if !b.safeFlush() {
			//	time.Sleep(time.Duration(DefaultBackgroundFlushInterval) * time.Second)
			//	log.Warn("waiting background memory table flush to disk", "state_id", persistId)
			//	continue
			//}
			err := b.backup.flush(db, clean, persistId)
			if err == nil {
				return
			}
			log.Error("failed to flush background memory table to disk", "state_id", persistId, "error", err)
		}
	}()
	return nil
}

func (b *nodebuffer) nodes() map[common.Hash]map[string]*trienode.Node {
	b.mux.Lock()
	b.mux.Unlock()

	table, err := b.current.merge(b.backup)
	if err != nil {
		log.Crit("[BUG] failed to merge memory table under revert nodebuffer", "error", err)
	}
	return table.nodes
}

func (b *nodebuffer) layers() uint64 {
	b.mux.RLock()
	b.mux.RUnlock()
	return b.current.layers + b.backup.layers
}

// writeNodes writes the trie nodes into the provided database batch.
// Note this function will also inject all the newly written nodes
// into clean cache.
func writeNodes(batch ethdb.Batch, nodes map[common.Hash]map[string]*trienode.Node, clean *fastcache.Cache) (total int) {
	for owner, subset := range nodes {
		for path, n := range subset {
			if n.IsDeleted() {
				if owner == (common.Hash{}) {
					rawdb.DeleteAccountTrieNode(batch, []byte(path))
				} else {
					rawdb.DeleteStorageTrieNode(batch, owner, []byte(path))
				}
				if clean != nil {
					clean.Del(cacheKey(owner, []byte(path)))
				}
			} else {
				if owner == (common.Hash{}) {
					rawdb.WriteAccountTrieNode(batch, []byte(path), n.Blob)
				} else {
					rawdb.WriteStorageTrieNode(batch, owner, []byte(path), n.Blob)
				}
				if clean != nil {
					clean.Set(cacheKey(owner, []byte(path)), n.Blob)
				}
			}
		}
		total += len(subset)
	}
	return total
}

// cacheKey constructs the unique key of clean cache.
func cacheKey(owner common.Hash, path []byte) []byte {
	if owner == (common.Hash{}) {
		return path
	}
	return append(owner.Bytes(), path...)
}
