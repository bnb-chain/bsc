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

package aggpathdb

import (
	"errors"
	"fmt"
	"sync"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
	"golang.org/x/crypto/sha3"
)

// diskLayer is a low level persistent layer built on top of a key-value store.
type diskLayer struct {
	root   common.Hash      // Immutable, root hash to which this layer was made for
	id     uint64           // Immutable, corresponding state id
	db     *Database        // Path-based trie database
	cleans *fastcache.Cache // GC friendly memory cache of clean node RLPs
	buffer *nodebuffer      // Node buffer to aggregate writes
	stale  bool             // Signals that the layer became stale (state progressed)
	lock   sync.RWMutex     // Lock used to protect stale flag
}

// newDiskLayer creates a new disk layer based on the passing arguments.
func newDiskLayer(root common.Hash, id uint64, db *Database, cleans *fastcache.Cache, buffer *nodebuffer) *diskLayer {
	// Initialize a clean cache if the memory allowance is not zero
	// or reuse the provided cache if it is not nil (inherited from
	// the original disk layer).
	if cleans == nil && db.config.CleanCacheSize != 0 {
		cleans = fastcache.New(db.config.CleanCacheSize)
	}
	return &diskLayer{
		root:   root,
		id:     id,
		db:     db,
		cleans: cleans,
		buffer: buffer,
	}
}

// root implements the layer interface, returning root hash of corresponding state.
func (dl *diskLayer) rootHash() common.Hash {
	return dl.root
}

// stateID implements the layer interface, returning the state id of disk layer.
func (dl *diskLayer) stateID() uint64 {
	return dl.id
}

// parent implements the layer interface, returning nil as there's no layer
// below the disk.
func (dl *diskLayer) parentLayer() layer {
	return nil
}

// isStale return whether this layer has become stale (was flattened across) or if
// it's still live.
func (dl *diskLayer) isStale() bool {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	return dl.stale
}

// markStale sets the stale flag as true.
func (dl *diskLayer) markStale() {
	dl.lock.Lock()
	defer dl.lock.Unlock()

	if dl.stale {
		panic("triedb disk layer is stale") // we've committed into the same base from two children, boom
	}
	dl.stale = true
}

// Node implements the layer interface, retrieving the trie node with the
// provided node info. No error will be returned if the node is not found.
func (dl *diskLayer) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return nil, errSnapshotStale
	}
	// Try to retrieve the trie node from the not-yet-written
	// node buffer first. Note the buffer is lock free since
	// it's impossible to mutate the buffer before tagging the
	// layer as stale.
	n, err := dl.buffer.node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if n != nil {
		dirtyHitMeter.Mark(1)
		dirtyReadMeter.Mark(int64(len(n.Blob)))
		return n.Blob, nil
	}
	dirtyMissMeter.Mark(1)

	aggPath := getAggNodePath(path)
	// try to retrieve the trie aggNode from the clean memory cache
	aggNode, err := getAggNodeFromCache(dl.cleans, owner, aggPath)
	if err != nil {
		return nil, err
	}

	// try to get aggNode from the database
	if aggNode == nil {
		aggNode, err = loadAggNodeFromDatabase(dl.db.diskdb, owner, aggPath)
		if err != nil {
			return nil, err
		}
	}

	// aggNode not found
	if aggNode == nil {
		return []byte{}, nil
	}

	rawNode := aggNode.Node(path)
	if rawNode == nil {
		// not found
		return []byte{}, nil
	}

	h := newHasher()
	defer h.release()

	got := h.hash(rawNode)
	if got == hash {
		return rawNode, nil
	}
	if dl.cleans != nil {
		dl.cleans.Set(cacheKey(owner, path), aggNode.encodeTo())
	}

	return []byte{}, nil
}

// update implements the layer interface, returning a new diff layer on top
// with the given state set.
func (dl *diskLayer) update(root common.Hash, id uint64, block uint64, nodes map[common.Hash]map[string]*trienode.Node, states *triestate.Set) *diffLayer {
	return newDiffLayer(dl, root, id, block, nodes, states)
}

// commit merges the given bottom-most diff layer into the node buffer
// and returns a newly constructed disk layer. Note the current disk
// layer must be tagged as stale first to prevent re-access.
func (dl *diskLayer) commit(bottom *diffLayer, force bool) (*diskLayer, error) {
	dl.lock.Lock()
	defer dl.lock.Unlock()

	// Construct and store the state history first. If crash happens
	// after storing the state history but without flushing the
	// corresponding states(journal), the stored state history will
	// be truncated in the next restart.
	if dl.db.freezer != nil {
		err := writeHistory(dl.db.diskdb, dl.db.freezer, bottom, dl.db.config.StateHistory)
		if err != nil {
			return nil, err
		}
	}
	// Mark the diskLayer as stale before applying any mutations on top.
	//dl.stale = true

	// Store the root->id lookup afterwards. All stored lookups are
	// identified by the **unique** state root. It's impossible that
	// in the same chain blocks are not adjacent but have the same
	// root.
	if dl.id == 0 {
		rawdb.WriteStateID(dl.db.diskdb, dl.root, 0)
	}
	rawdb.WriteStateID(dl.db.diskdb, bottom.rootHash(), bottom.stateID())

	// Construct a new disk layer by merging the nodes from the provided
	// diff layer, and flush the content in disk layer if there are too
	// many nodes cached. The clean cache is inherited from the original
	// disk layer for reusing.
	ndl := newDiskLayer(bottom.root, bottom.stateID(), dl.db, dl.cleans, dl.buffer.commit(bottom.nodes))
	err := ndl.buffer.flush(ndl.db.diskdb, ndl.cleans, ndl.id, force)
	if err != nil {
		return nil, err
	}
	return ndl, nil
}

// revert applies the given state history and return a reverted disk layer.
func (dl *diskLayer) revert(h *history, loader triestate.TrieLoader) (*diskLayer, error) {
	if h.meta.root != dl.rootHash() {
		return nil, errUnexpectedHistory
	}
	// Reject if the provided state history is incomplete. It's due to
	// a large construct SELF-DESTRUCT which can't be handled because
	// of memory limitation.
	if len(h.meta.incomplete) > 0 {
		return nil, errors.New("incomplete state history")
	}
	if dl.id == 0 {
		return nil, fmt.Errorf("%w: zero state id", errStateUnrecoverable)
	}
	// Apply the reverse state changes upon the current state. This must
	// be done before holding the lock in order to access state in "this"
	// layer.
	nodes, err := triestate.Apply(h.meta.parent, h.meta.root, h.accounts, h.storages, loader)
	if err != nil {
		return nil, err
	}
	// Mark the diskLayer as stale before applying any mutations on top.
	dl.lock.Lock()
	defer dl.lock.Unlock()

	dl.stale = true

	// State change may be applied to node buffer, or the persistent
	// state, depends on if node buffer is empty or not. If the node
	// buffer is not empty, it means that the state transition that
	// needs to be reverted is not yet flushed and cached in node
	// buffer, otherwise, manipulate persistent state directly.
	if !dl.buffer.empty() {
		err := dl.buffer.revert(dl.db.diskdb, nodes)
		if err != nil {
			return nil, err
		}
	} else {
		batch := dl.db.diskdb.NewBatch()
		aggregateAndWriteNodes(dl.db.diskdb, dl.cleans, batch, nodes)
		rawdb.WritePersistentStateID(batch, dl.id-1)
		if err := batch.Write(); err != nil {
			log.Crit("Failed to write states", "err", err)
		}
	}
	return newDiskLayer(h.meta.parent, dl.id-1, dl.db, dl.cleans, dl.buffer), nil
}

// setBufferSize sets the node buffer size to the provided value.
func (dl *diskLayer) setBufferSize(size int) error {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return errSnapshotStale
	}
	return dl.buffer.setSize(size, dl.db.diskdb, dl.cleans, dl.id)
}

// size returns the approximate size of cached nodes in the disk layer.
func (dl *diskLayer) size() common.StorageSize {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return 0
	}
	return common.StorageSize(dl.buffer.size)
}

// resetCache releases the memory held by clean cache to prevent memory leak.
func (dl *diskLayer) resetCache() {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// Stale disk layer loses the ownership of clean cache.
	if dl.stale {
		return
	}
	if dl.cleans != nil {
		dl.cleans.Reset()
	}
}

// aggregateAndWriteNodes will aggregate the trienode into trie aggnode and persist into the database
// Note this function will inject all the clean aggNode into the cleanCache
func aggregateAndWriteNodes(reader ethdb.KeyValueReader, clean *fastcache.Cache, batch ethdb.Batch, nodes map[common.Hash]map[string]*trienode.Node) (total int) {
	// pre-aggregate the node
	// Note: temporary impl. When a node writes to a diskLayer, it should be aggregated to aggNode.
	changeSets := make(map[common.Hash]map[string]map[string]*trienode.Node)
	for owner, subset := range nodes {
		current, exist := changeSets[owner]
		if !exist {
			current = make(map[string]map[string]*trienode.Node)
			changeSets[owner] = current
		}
		for path, n := range subset {
			aggPath := getAggNodePath([]byte(path))
			aggChangeSet, exist := changeSets[owner][string(aggPath)]
			if !exist {
				aggChangeSet = make(map[string]*trienode.Node)
			}
			aggChangeSet[path] = n
			changeSets[owner][string(aggPath)] = aggChangeSet
		}
	}

	for owner, subset := range changeSets {
		for aggPath, cs := range subset {
			aggNode := getOrNewAggNode(reader, clean, owner, []byte(aggPath))
			for path, n := range cs {
				if n.IsDeleted() {
					aggNode.Delete([]byte(path))
				} else {
					aggNode.Update([]byte(path), n.Blob)
				}
			}
			aggNodeBytes := EncodeAggNode(aggNode)
			writeAggNode(batch, owner, []byte(aggPath), aggNodeBytes)
			if clean != nil {
				clean.Set(cacheKey(owner, []byte(aggPath)), aggNodeBytes)
			}
			total++
		}
	}
	return total
}

// hasher is used to compute the sha256 hash of the provided data.
type hasher struct{ sha crypto.KeccakState }

var hasherPool = sync.Pool{
	New: func() interface{} { return &hasher{sha: sha3.NewLegacyKeccak256().(crypto.KeccakState)} },
}

func newHasher() *hasher {
	return hasherPool.Get().(*hasher)
}

func (h *hasher) hash(data []byte) common.Hash {
	return crypto.HashData(h.sha, data)
}

func (h *hasher) release() {
	hasherPool.Put(h)
}
