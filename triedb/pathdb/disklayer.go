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
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
	"golang.org/x/crypto/sha3"
)

// trienodebuffer is a collection of modified trie nodes to aggregate the disk
// write. The content of the trienodebuffer must be checked before diving into
// disk (since it basically is not-yet-written data).
type trienodebuffer interface {
	// node retrieves the trie node with given node info.
	node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error)

	// commit merges the dirty nodes into the trienodebuffer. This operation won't take
	// the ownership of the nodes map which belongs to the bottom-most diff layer.
	// It will just hold the node references from the given map which are safe to
	// copy.
	commit(nodes map[common.Hash]map[string]*trienode.Node, set *triestate.Set) trienodebuffer

	// revert is the reverse operation of commit. It also merges the provided nodes
	// into the trienodebuffer, the difference is that the provided node set should
	// revert the changes made by the last state transition.
	revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node, accounts map[common.Hash][]byte, storages map[common.Hash]map[common.Hash][]byte) error

	// flush persists the in-memory dirty trie node into the disk if the configured
	// memory threshold is reached. Note, all data must be written atomically.
	flush(db ethdb.KeyValueStore, clean *cleanCache, id uint64, force bool) error

	// setSize sets the buffer size to the provided number, and invokes a flush
	// operation if the current memory usage exceeds the new limit.
	setSize(size int, db ethdb.KeyValueStore, clean *cleanCache, id uint64) error

	// reset cleans up the disk cache.
	reset()

	// empty returns an indicator if trienodebuffer contains any state transition inside.
	empty() bool

	// getSize return the trienodebuffer used size.
	getSize() (uint64, uint64)

	// getAllNodes return all the trie nodes are cached in trienodebuffer.
	getAllNodes() map[common.Hash]map[string]*trienode.Node

	// getLatestStates return all the latest states(account, storage, destructSet) are cached in trienodebuffer.
	getLatestStates() *triestate.Set

	// getLayers return the size of cached difflayers.
	getLayers() uint64

	// account return the value of the specify account
	account(hash common.Hash) ([]byte, bool)

	// storage return the value of the specify account and storage key
	storage(accountHash, storageHash common.Hash) ([]byte, bool)

	// waitAndStopFlushing will block unit writing the trie nodes of trienodebuffer to disk.
	waitAndStopFlushing()
}

type cleanCache struct {
	nodes       *fastcache.Cache
	plainStates *fastcache.Cache
}

func NewTrieNodeBuffer(sync bool, limit int,
	nodes map[common.Hash]map[string]*trienode.Node,
	latestAccounts map[common.Hash][]byte,
	latestStorages map[common.Hash]map[common.Hash][]byte,
	destructSet map[common.Hash]struct{}, layers uint64) trienodebuffer {
	if sync {
		log.Info("New sync node buffer", "limit", common.StorageSize(limit), "layers", layers)
		return newNodeBuffer(limit, nodes, layers)
	}
	log.Info("new async node buffer", "limit", common.StorageSize(limit), "layers", layers)
	return newAsyncNodeBuffer(limit, nodes, latestAccounts, latestStorages, destructSet, layers)
}

// diskLayer is a low level persistent layer built on top of a key-value store.
type diskLayer struct {
	root   common.Hash    // Immutable, root hash to which this layer was made for
	id     uint64         // Immutable, corresponding state id
	db     *Database      // Path-based trie database
	cleans *cleanCache    // GC friendly memory cache of clean node RLPs
	buffer trienodebuffer // Node buffer to aggregate writes
	stale  bool           // Signals that the layer became stale (state progressed)
	lock   sync.RWMutex   // Lock used to protect stale flag
}

// newDiskLayer creates a new disk layer based on the passing arguments.
func newDiskLayer(root common.Hash, id uint64, db *Database, cleans *cleanCache, buffer trienodebuffer) *diskLayer {
	// Initialize a clean cache if the memory allowance is not zero
	// or reuse the provided cache if it is not nil (inherited from
	// the original disk layer).
	if cleans == nil && db.config.CleanCacheSize != 0 {
		cleans = &cleanCache{}
		nodeCacheSize := db.config.CleanCacheSize * 43 / 100
		plainStatesCacheSize := db.config.CleanCacheSize * 57 / 100
		cleans.nodes = fastcache.New(nodeCacheSize)
		cleans.plainStates = fastcache.New(plainStatesCacheSize)
		log.Info("Allocate clean cache in disklayer", "nodes", common.StorageSize(nodeCacheSize),
			"plainStates", common.StorageSize(plainStatesCacheSize))
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

func (dl *diskLayer) Account(hash common.Hash) ([]byte, error) {
	// Hold the lock, ensure the parent won't be changed during the
	// state accessing.
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	if dl.stale {
		return nil, errSnapshotStale
	}

	trieDiffLayerAccountMissMeter.Mark(1)
	if data, exist := dl.buffer.account(hash); exist {
		trieDirtyAccountHitMeter.Mark(1)
		trieDiskLayerAccountHitMeter.Mark(1)
		return data, nil
	}
	trieDirtyAccountMissMeter.Mark(1)
	if data, ok := dl.cleans.plainStates.HasGet(nil, hash.Bytes()); ok {
		trieCleanAccountHitMeter.Mark(1)
		trieDiskLayerAccountHitMeter.Mark(1)
		return types.MustFullAccountRLP(data), nil
	}

	trieCleanAccountMissMeter.Mark(1)
	blob := dl.readAccountTrie(hash)
	dl.cleans.plainStates.Set(hash.Bytes(), types.FullToSlimAccountRLP(blob))
	return blob, nil
}

func (dl *diskLayer) Storage(accountHash, storageHash common.Hash) ([]byte, error) {
	// Hold the lock, ensure the parent won't be changed during the
	// state accessing.
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return nil, errSnapshotStale
	}

	trieDiffLayerStorageMissMeter.Mark(1)
	if data, exist := dl.buffer.storage(accountHash, storageHash); exist {
		trieDiskLayerStorageHitMeter.Mark(1)
		trieDirtyStorageHitMeter.Mark(1)
		return data, nil
	}

	trieDirtyStorageMissMeter.Mark(1)
	if data, ok := dl.cleans.plainStates.HasGet(nil, append(accountHash.Bytes(), storageHash.Bytes()...)); ok {
		trieDiskLayerStorageHitMeter.Mark(1)
		trieCleanStorageHitMeter.Mark(1)
		return data, nil
	}

	trieCleanStorageMissMeter.Mark(1)
	blob := dl.readStorageTrie(accountHash, storageHash)
	dl.cleans.plainStates.Set(append(accountHash.Bytes(), storageHash.Bytes()...), blob)
	return blob, nil
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
	bufferNodeStart := time.Now()
	n, err := dl.buffer.node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if n != nil {
		dirtyHitMeter.Mark(1)
		dirtyReadMeter.Mark(int64(len(n.Blob)))
		diskBufferNodeTimer.UpdateSince(bufferNodeStart)
		return n.Blob, nil
	}
	dirtyMissMeter.Mark(1)

	// Try to retrieve the trie node from the clean memory cache
	key := cacheKey(owner, path)
	if dl.cleans != nil {
		cleanNodeStart := time.Now()
		if blob := dl.cleans.nodes.Get(nil, key); len(blob) > 0 {
			h := newHasher()
			defer h.release()

			got := h.hash(blob)

			diskCleanNodeTimer.UpdateSince(cleanNodeStart)

			if got == hash {
				cleanHitMeter.Mark(1)
				cleanReadMeter.Mark(int64(len(blob)))
				return blob, nil
			}
			cleanFalseMeter.Mark(1)
			log.Error("Unexpected trie node in clean cache", "owner", owner, "path", path, "expect", hash, "got", got)
		}
		cleanMissMeter.Mark(1)
	}
	// Try to retrieve the trie node from the disk.
	var (
		nBlob         []byte
		nHash         common.Hash
		diskNodeStart = time.Now()
	)
	if owner == (common.Hash{}) {
		nBlob, nHash = rawdb.ReadAccountTrieNode(dl.db.diskdb, path)
	} else {
		nBlob, nHash = rawdb.ReadStorageTrieNode(dl.db.diskdb, owner, path)
	}
	diskDBNodeTimer.UpdateSince(diskNodeStart)
	if nHash != hash {
		diskFalseMeter.Mark(1)
		log.Error("Unexpected trie node in disk", "owner", owner, "path", path, "expect", hash, "got", nHash)
		return nil, newUnexpectedNodeError("disk", hash, nHash, owner, path, nBlob)
	}
	if dl.cleans != nil && len(nBlob) > 0 {
		dl.cleans.nodes.Set(key, nBlob)
		cleanWriteMeter.Mark(int64(len(nBlob)))
	}
	return nBlob, nil
}

// readAccountTrie return value of the account leaf node directly from the db
func (dl *diskLayer) readAccountTrie(hash common.Hash) []byte {
	start := time.Now()
	defer func() {
		readAndDecodeAccountTimer.UpdateSince(start)
	}()
	nBlob, path, nHash := rawdb.ReadAccountFromTrieDirectly(dl.db.diskdb, hash.Bytes())
	diskReadAccountTimer.UpdateSince(start)
	if nBlob == nil {
		return nil
	}
	dl.cleans.nodes.Set(cacheKey(common.Hash{}, path[:]), nBlob)
	diskTotalAccountCounter.Inc(1)
	val, key := trie.DecodeLeafNode(nHash.Bytes(), path, nBlob)
	if bytes.Compare(key, hash.Bytes()) == 0 {
		return val
	} else {
		log.Debug("account short node info ", "account hash", hash.String(), "gotten key", hex.EncodeToString(key), "path", common.Bytes2Hex(path))
	}
	diskTotalMissAccoutCounter.Inc(1)
	return nil
}

// readStorageTrie return value of the storage leaf node directly from the db
func (dl *diskLayer) readStorageTrie(accountHash, storageHash common.Hash) []byte {
	start := time.Now()
	defer func() {
		readAndDecodeStorageTimer.UpdateSince(start)
	}()
	key := storageHash.Bytes()
	nBlob, path, nHash := rawdb.ReadStorageFromTrieDirectly(dl.db.diskdb, accountHash, key)
	diskReadStorageTimer.UpdateSince(start)
	if nBlob == nil {
		return nil
	}
	dl.cleans.nodes.Set(cacheKey(accountHash, path[common.HashLength:]), nBlob)

	diskTotalStorageCounter.Inc(1)
	val, key := trie.DecodeLeafNode(nHash.Bytes(), path[common.HashLength:], nBlob)
	if bytes.Compare(storageHash.Bytes(), key) == 0 {
		return val
	}
	diskTotalMissStorageCounter.Inc(1)
	return nil
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

	// Construct and store the state history first. If crash happens after storing
	// the state history but without flushing the corresponding states(journal),
	// the stored state history will be truncated from head in the next restart.
	var (
		overflow bool
		oldest   uint64
	)
	if dl.db.freezer != nil {
		err := writeHistory(dl.db.freezer, bottom)
		if err != nil {
			return nil, err
		}
		// Determine if the persisted history object has exceeded the configured
		// limitation, set the overflow as true if so.
		tail, err := dl.db.freezer.Tail()
		if err != nil {
			return nil, err
		}
		limit := dl.db.config.StateHistory
		if limit != 0 && bottom.stateID()-tail > limit {
			overflow = true
			oldest = bottom.stateID() - limit + 1 // track the id of history **after truncation**
		}
	}
	// Mark the diskLayer as stale before applying any mutations on top.
	dl.stale = true

	// Store the root->id lookup afterwards. All stored lookups are identified
	// by the **unique** state root. It's impossible that in the same chain
	// blocks are not adjacent but have the same root.
	if dl.id == 0 {
		rawdb.WriteStateID(dl.db.diskdb, dl.root, 0)
	}
	rawdb.WriteStateID(dl.db.diskdb, bottom.rootHash(), bottom.stateID())

	// Construct a new disk layer by merging the nodes from the provided diff
	// layer, and flush the content in disk layer if there are too many nodes
	// cached. The clean cache is inherited from the original disk layer.
	ndl := newDiskLayer(bottom.root, bottom.stateID(), dl.db, dl.cleans, dl.buffer.commit(bottom.nodes, bottom.states))

	// In a unique scenario where the ID of the oldest history object (after tail
	// truncation) surpasses the persisted state ID, we take the necessary action
	// of forcibly committing the cached dirty nodes to ensure that the persisted
	// state ID remains higher.
	if !force && rawdb.ReadPersistentStateID(dl.db.diskdb) < oldest {
		force = true
	}
	if err := ndl.buffer.flush(ndl.db.diskdb, ndl.cleans, ndl.id, force); err != nil {
		return nil, err
	}
	// To remove outdated history objects from the end, we set the 'tail' parameter
	// to 'oldest-1' due to the offset between the freezer index and the history ID.
	if overflow {
		pruned, err := truncateFromTail(ndl.db.diskdb, ndl.db.freezer, oldest-1)
		if err != nil {
			return nil, err
		}
		log.Debug("Pruned state history", "items", pruned, "tailid", oldest)
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
	nodes, latestAccounts, latestStorages, err := triestate.Apply(h.meta.parent, h.meta.root, h.accounts, h.storages, loader)
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
		err := dl.buffer.revert(dl.db.diskdb, nodes, latestAccounts, latestStorages)
		if err != nil {
			return nil, err
		}
	} else {
		batch := dl.db.diskdb.NewBatch()
		writeNodes(batch, nodes, dl.cleans)
		rawdb.WritePersistentStateID(batch, dl.id-1)
		if err := batch.Write(); err != nil {
			log.Crit("Failed to write states", "err", err)
		}
	}
	return newDiskLayer(h.meta.parent, dl.id-1, dl.db, dl.cleans, dl.buffer), nil
}

// setBufferSize sets the trie node buffer size to the provided value.
func (dl *diskLayer) setBufferSize(size int) error {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return errSnapshotStale
	}
	return dl.buffer.setSize(size, dl.db.diskdb, dl.cleans, dl.id)
}

// size returns the approximate size of cached nodes in the disk layer.
func (dl *diskLayer) size() (common.StorageSize, common.StorageSize) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return 0, 0
	}
	dirtyNodes, dirtyimmutableNodes := dl.buffer.getSize()
	return common.StorageSize(dirtyNodes), common.StorageSize(dirtyimmutableNodes)
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
		dl.cleans.nodes.Reset()
	}
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
