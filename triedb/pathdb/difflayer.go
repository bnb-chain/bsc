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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type RefTrieNode struct {
	refCount uint32
	node     *trienode.Node
}

type HashNodeCache struct {
	lock  sync.RWMutex
	cache map[common.Hash]*RefTrieNode
}

func (h *HashNodeCache) length() int {
	if h == nil {
		return 0
	}
	h.lock.RLock()
	defer h.lock.RUnlock()
	return len(h.cache)
}

func (h *HashNodeCache) set(hash common.Hash, node *trienode.Node) {
	if h == nil {
		return
	}
	h.lock.Lock()
	defer h.lock.Unlock()
	if n, ok := h.cache[hash]; ok {
		n.refCount++
	} else {
		h.cache[hash] = &RefTrieNode{1, node}
	}
}

func (h *HashNodeCache) Get(hash common.Hash) *trienode.Node {
	if h == nil {
		return nil
	}
	h.lock.RLock()
	defer h.lock.RUnlock()
	if n, ok := h.cache[hash]; ok {
		return n.node
	}
	return nil
}

func (h *HashNodeCache) del(hash common.Hash) {
	if h == nil {
		return
	}
	h.lock.Lock()
	defer h.lock.Unlock()
	n, ok := h.cache[hash]
	if !ok {
		return
	}
	if n.refCount > 0 {
		n.refCount--
	}
	if n.refCount == 0 {
		delete(h.cache, hash)
	}
}

func (h *HashNodeCache) Add(ly layer) {
	if h == nil {
		return
	}
	dl, ok := ly.(*diffLayer)
	if !ok {
		return
	}
	beforeAdd := h.length()
	for _, subset := range dl.nodes.nodes {
		for _, node := range subset {
			h.set(node.Hash, node)
		}
	}
	diffHashCacheLengthGauge.Update(int64(h.length()))
	log.Debug("Add difflayer to hash map", "root", ly.rootHash(), "block_number", dl.block, "map_len", h.length(), "add_delta", h.length()-beforeAdd)
}

func (h *HashNodeCache) Remove(ly layer) {
	if h == nil {
		return
	}
	dl, ok := ly.(*diffLayer)
	if !ok {
		return
	}
	go func() {
		beforeDel := h.length()
		for _, subset := range dl.nodes.nodes {
			for _, node := range subset {
				h.del(node.Hash)
			}
		}
		diffHashCacheLengthGauge.Update(int64(h.length()))
		log.Debug("Remove difflayer from hash map", "root", ly.rootHash(), "block_number", dl.block, "map_len", h.length(), "del_delta", beforeDel-h.length())
	}()
}

// diffLayer represents a collection of modifications made to the in-memory tries
// along with associated state changes after running a block on top.
//
// The purpose of a diff layer is to serve as a journal, recording recent state modifications
// that have not yet been committed to a more stable or semi-permanent state.
type diffLayer struct {
	// Immutables
	root   common.Hash         // Root hash to which this layer diff belongs to
	id     uint64              // Corresponding state id
	block  uint64              // Associated block number
	nodes  *nodeSet            // Cached trie nodes indexed by owner and path
	states *StateSetWithOrigin // Associated state changes along with origin value
	cache  *HashNodeCache      // trienode cache by hash key. cache is immutable, but cache's item can be add/del.

	// mutables
	origin *diskLayer   // The current difflayer corresponds to the underlying disklayer and is updated during cap.
	parent layer        // Parent layer modified by this one, never nil, **can be changed**
	lock   sync.RWMutex // Lock used to protect parent
}

// newDiffLayer creates a new diff layer on top of an existing layer.
func newDiffLayer(parent layer, root common.Hash, id uint64, block uint64, nodes *nodeSet, states *StateSetWithOrigin) *diffLayer {
	dl := &diffLayer{
		root:   root,
		id:     id,
		block:  block,
		parent: parent,
		nodes:  nodes,
		states: states,
	}

	switch l := parent.(type) {
	case *diskLayer:
		dl.origin = l
		dl.cache = &HashNodeCache{
			cache: make(map[common.Hash]*RefTrieNode),
		}
	case *diffLayer:
		dl.origin = l.originDiskLayer()
		dl.cache = l.cache
	default:
		panic("unknown parent type")
	}

	dirtyNodeWriteMeter.Mark(int64(nodes.size))
	dirtyStateWriteMeter.Mark(int64(states.size))
	log.Debug("Created new diff layer", "id", id, "block", block, "nodesize", common.StorageSize(nodes.size), "statesize", common.StorageSize(states.size))
	return dl
}

func (dl *diffLayer) originDiskLayer() *diskLayer {
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	return dl.origin
}

func (dl *diffLayer) updateOriginDiskLayer(persistLayer *diskLayer) {
	dl.lock.Lock()
	defer dl.lock.Unlock()
	dl.origin = persistLayer
}

// rootHash implements the layer interface, returning the root hash of
// corresponding state.
func (dl *diffLayer) rootHash() common.Hash {
	return dl.root
}

// stateID implements the layer interface, returning the state id of the layer.
func (dl *diffLayer) stateID() uint64 {
	return dl.id
}

// parentLayer implements the layer interface, returning the subsequent
// layer of the diff layer.
func (dl *diffLayer) parentLayer() layer {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	return dl.parent
}

// node implements the layer interface, retrieving the trie node blob with the
// provided node information. No error will be returned if the node is not found.
// The hash parameter can access the cache to speed up access.
func (dl *diffLayer) node(owner common.Hash, path []byte, hash common.Hash, depth int) ([]byte, common.Hash, *nodeLoc, error) {
	if hash != (common.Hash{}) {
		if n := dl.cache.Get(hash); n != nil {
			// The query from the hash map is fastpath,
			// avoiding recursive query of 128 difflayers.
			diffHashCacheHitMeter.Mark(1)
			diffHashCacheReadMeter.Mark(int64(len(n.Blob)))
			return n.Blob, n.Hash, &nodeLoc{loc: locDiffLayer, depth: depth}, nil
		}
	}

	diffHashCacheMissMeter.Mark(1)
	persistLayer := dl.originDiskLayer()
	if hash != (common.Hash{}) && persistLayer != nil {
		blob, rhash, nloc, err := persistLayer.node(owner, path, hash, depth+1)
		if err != nil || rhash != hash {
			// This is a bad case with a very low probability.
			// r/w the difflayer cache and r/w the disklayer are not in the same lock,
			// so in extreme cases, both reading the difflayer cache and reading the disklayer may fail, eg, disklayer is stale.
			// In this case, fallback to the original 128-layer recursive difflayer query path.
			diffHashCacheSlowPathMeter.Mark(1)
			log.Debug("Retry difflayer due to query origin failed",
				"owner", owner, "path", path, "query_hash", hash.String(), "return_hash", rhash.String(), "error", err)
			return dl.intervalNode(owner, path, hash, 0)
		} else { // This is the fastpath.
			return blob, rhash, nloc, nil
		}
	}
	diffHashCacheSlowPathMeter.Mark(1)
	log.Debug("Retry difflayer due to origin is nil or hash is empty",
		"owner", owner, "path", path, "query_hash", hash.String(), "disk_layer_is_empty", persistLayer == nil)
	return dl.intervalNode(owner, path, hash, 0)
}

func (dl *diffLayer) intervalNode(owner common.Hash, path []byte, hash common.Hash, depth int) ([]byte, common.Hash, *nodeLoc, error) {
	// Hold the lock, ensure the parent won't be changed during the
	// state accessing.
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	// If the trie node is known locally, return it
	n, ok := dl.nodes.node(owner, path)
	if ok {
		dirtyNodeHitMeter.Mark(1)
		dirtyNodeHitDepthHist.Update(int64(depth))
		dirtyNodeReadMeter.Mark(int64(len(n.Blob)))
		return n.Blob, n.Hash, &nodeLoc{loc: locDiffLayer, depth: depth}, nil
	}
	// Trie node unknown to this layer, resolve from parent
	if diff, ok := dl.parent.(*diffLayer); ok {
		return diff.intervalNode(owner, path, hash, depth+1)
	}
	// Failed to resolve through diff layers, fallback to disk layer
	return dl.parent.node(owner, path, hash, depth+1)
}

// account directly retrieves the account RLP associated with a particular
// hash in the slim data format.
//
// Note the returned account is not a copy, please don't modify it.
func (dl *diffLayer) account(hash common.Hash, depth int) ([]byte, error) {
	// Hold the lock, ensure the parent won't be changed during the
	// state accessing.
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if blob, found := dl.states.account(hash); found {
		dirtyStateHitMeter.Mark(1)
		dirtyStateHitDepthHist.Update(int64(depth))
		dirtyStateReadMeter.Mark(int64(len(blob)))

		if len(blob) == 0 {
			stateAccountInexMeter.Mark(1)
		} else {
			stateAccountExistMeter.Mark(1)
		}
		return blob, nil
	}
	// Account is unknown to this layer, resolve from parent
	return dl.parent.account(hash, depth+1)
}

// storage directly retrieves the storage data associated with a particular hash,
// within a particular account.
//
// Note the returned storage slot is not a copy, please don't modify it.
func (dl *diffLayer) storage(accountHash, storageHash common.Hash, depth int) ([]byte, error) {
	// Hold the lock, ensure the parent won't be changed during the
	// state accessing.
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if blob, found := dl.states.storage(accountHash, storageHash); found {
		dirtyStateHitMeter.Mark(1)
		dirtyStateHitDepthHist.Update(int64(depth))
		dirtyStateReadMeter.Mark(int64(len(blob)))

		if len(blob) == 0 {
			stateStorageInexMeter.Mark(1)
		} else {
			stateStorageExistMeter.Mark(1)
		}
		return blob, nil
	}
	// storage slot is unknown to this layer, resolve from parent
	return dl.parent.storage(accountHash, storageHash, depth+1)
}

// update implements the layer interface, creating a new layer on top of the
// existing layer tree with the specified data items.
func (dl *diffLayer) update(root common.Hash, id uint64, block uint64, nodes *nodeSet, states *StateSetWithOrigin) *diffLayer {
	return newDiffLayer(dl, root, id, block, nodes, states)
}

// persist flushes the diff layer and all its parent layers to disk layer.
func (dl *diffLayer) persist(force bool) (layer, error) {
	if parent, ok := dl.parentLayer().(*diffLayer); ok {
		// Hold the lock to prevent any read operation until the new
		// parent is linked correctly.
		dl.lock.Lock()

		// The merging of diff layers starts at the bottom-most layer,
		// therefore we recurse down here, flattening on the way up
		// (diffToDisk).
		result, err := parent.persist(force)
		if err != nil {
			dl.lock.Unlock()
			return nil, err
		}
		dl.parent = result
		dl.lock.Unlock()
	}
	return diffToDisk(dl, force)
}

// size returns the approximate memory size occupied by this diff layer.
func (dl *diffLayer) size() uint64 {
	return dl.nodes.size + dl.states.size
}

// diffToDisk merges a bottom-most diff into the persistent disk layer underneath
// it. The method will panic if called onto a non-bottom-most diff layer.
func diffToDisk(layer *diffLayer, force bool) (layer, error) {
	disk, ok := layer.parentLayer().(*diskLayer)
	if !ok {
		panic(fmt.Sprintf("unknown layer type: %T", layer.parentLayer()))
	}
	return disk.commit(layer, force)
}
