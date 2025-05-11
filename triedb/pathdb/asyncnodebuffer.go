package pathdb

import (
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/fastcache"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

var _ trienodebuffer = &asyncnodebuffer{}

// asyncnodebuffer implement trienodebuffer interface, and async the nodecache
// to disk.
type asyncnodebuffer struct {
	mux          sync.RWMutex
	current      *nodecache
	background   *nodecache
	isFlushing   atomic.Bool
	stopFlushing atomic.Bool
}

// newAsyncNodeBuffer initializes the async node buffer with the provided nodes and states.
func newAsyncNodeBuffer(limit int, nodes *nodeSet, states *stateSet, layers uint64) *asyncnodebuffer {
	return &asyncnodebuffer{
		current:    newNodeCache(limit, nodes, states, layers),
		background: newNodeCache(limit, nil, nil, 0),
	}
}

func (a *asyncnodebuffer) account(hash common.Hash) ([]byte, bool) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	node, found := a.current.account(hash)
	if !found {
		node, found = a.background.account(hash)
	}
	return node, found
}

func (a *asyncnodebuffer) storage(addrHash common.Hash, storageHash common.Hash) ([]byte, bool) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	node, found := a.current.storage(addrHash, storageHash)
	if !found {
		node, found = a.background.storage(addrHash, storageHash)
	}
	return node, found
}

// node retrieves the trie node with given node info.
func (a *asyncnodebuffer) node(owner common.Hash, path []byte) (*trienode.Node, bool) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	node, found := a.current.node(owner, path)
	if !found {
		node, found = a.background.node(owner, path)
	}
	return node, found
}

// commit merges the provided states and trie nodesinto the nodebuffer. This operation won't take
// the ownership of the nodes map which belongs to the bottom-most diff layer.
// It will just hold the node references from the given map which are safe to
// copy.
func (a *asyncnodebuffer) commit(nodes *nodeSet, states *stateSet) trienodebuffer {
	a.mux.Lock()
	defer a.mux.Unlock()

	err := a.current.commit(nodes, states)
	if err != nil {
		log.Crit("[BUG] Failed to commit nodes to asyncnodebuffer", "error", err)
	}
	return a
}

// revertTo is the reverse operation of commit. It also merges the provided nodes
// into the nodebuffer, the difference is that the provided node set should
// revert the changes made by the last state transition.
func (a *asyncnodebuffer) revertTo(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node, accounts map[common.Hash][]byte, storages map[common.Hash]map[common.Hash][]byte) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	var err error
	a.current, err = a.current.merge(a.background)
	if err != nil {
		log.Crit("[BUG] Failed to merge node cache under revert async node buffer", "error", err)
	}
	a.background.reset()
	return a.current.revertTo(db, nodes, accounts, storages)
}

// empty returns an indicator if nodebuffer contains any state transition inside.
func (a *asyncnodebuffer) empty() bool {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.empty() && a.background.empty()
}

// flush persists the in-memory dirty trie node into the disk if the configured
// memory threshold is reached. Note, all data must be written atomically.
func (a *asyncnodebuffer) flush(db ethdb.KeyValueStore, freezer ethdb.AncientWriter, clean *fastcache.Cache, id uint64, force bool) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	if a.stopFlushing.Load() {
		return nil
	}

	if force {
		for {
			if atomic.LoadUint64(&a.background.immutable) == 1 {
				time.Sleep(time.Duration(DefaultBackgroundFlushInterval) * time.Second)
				log.Info("Waiting background memory table flushed into disk for forcing flush node buffer")
				continue
			}
			atomic.StoreUint64(&a.current.immutable, 1)
			return a.current.flush(db, freezer, clean, id, true)
		}
	}

	if !a.current.full() {
		return nil
	}

	// background flush doing
	if atomic.LoadUint64(&a.background.immutable) == 1 {
		return nil
	}

	atomic.StoreUint64(&a.current.immutable, 1)
	a.current, a.background = a.background, a.current

	a.isFlushing.Store(true)
	go func(persistID uint64) {
		defer a.isFlushing.Store(false)
		for {
			err := a.background.flush(db, freezer, clean, persistID, true)
			if err == nil {
				log.Debug("Succeed to flush background nodecache to disk", "state_id", persistID)
				return
			}
			log.Error("Failed to flush background nodecache to disk", "state_id", persistID, "error", err)
		}
	}(id)
	return nil
}

func (a *asyncnodebuffer) waitAndStopFlushing() {
	a.stopFlushing.Store(true)
	for a.isFlushing.Load() {
		time.Sleep(time.Second)
		log.Warn("Waiting background memory table flushed into disk")
	}
}

func (a *asyncnodebuffer) getAllNodesAndStates() (*nodeSet, *stateSet) {
	a.mux.Lock()
	defer a.mux.Unlock()

	cached, err := a.current.merge(a.background)
	if err != nil {
		log.Crit("[BUG] Failed to merge node cache under revert async node buffer", "error", err)
	}
	return cached.nodes, cached.states
}

func (a *asyncnodebuffer) getStates() *stateSet {
	_, states := a.getAllNodesAndStates()
	return states
}

func (a *asyncnodebuffer) getLayers() uint64 {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.layers + a.background.layers
}

func (a *asyncnodebuffer) getSize() (uint64, uint64) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.size(), a.background.size()
}

type nodecache struct {
	*buffer
	immutable uint64 // The flag equal 1, flush nodes to disk background
}

func newNodeCache(limit int, nodes *nodeSet, states *stateSet, layers uint64) *nodecache {
	return &nodecache{
		buffer:    newBuffer(limit, nodes, states, layers),
		immutable: 0,
	}
}

func (nc *nodecache) commit(nodes *nodeSet, states *stateSet) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errWriteImmutable
	}
	nc.buffer.commit(nodes, states)
	return nil
}

func (nc *nodecache) revertTo(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node, accounts map[common.Hash][]byte, storages map[common.Hash]map[common.Hash][]byte) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errRevertImmutable
	}
	nc.buffer.revertTo(db, nodes, accounts, storages)
	return nil
}

func (nc *nodecache) reset() {
	atomic.StoreUint64(&nc.immutable, 0)
	nc.buffer.reset()
}

func (nc *nodecache) flush(db ethdb.KeyValueStore, freezer ethdb.AncientWriter, nodesCache *fastcache.Cache, id uint64, force bool) error {
	if atomic.LoadUint64(&nc.immutable) != 1 {
		return errFlushMutable
	}
	nc.buffer.flush(db, freezer, nodesCache, id, force)
	atomic.StoreUint64(&nc.immutable, 0)
	return nil
}

func (nc *nodecache) merge(nc1 *nodecache) (*nodecache, error) {
	if nc == nil && nc1 == nil {
		return nil, nil
	}
	if nc == nil || nc.empty() {
		res := copyNodeCache(nc1)
		atomic.StoreUint64(&res.immutable, 0)
		return res, nil
	}
	if nc1 == nil || nc1.empty() {
		res := copyNodeCache(nc)
		atomic.StoreUint64(&res.immutable, 0)
		return res, nil
	}
	if atomic.LoadUint64(&nc.immutable) == atomic.LoadUint64(&nc1.immutable) {
		return nil, errIncompatibleMerge
	}

	var (
		immutable *nodecache
		mutable   *nodecache
	)
	if atomic.LoadUint64(&nc.immutable) == 1 {
		immutable = nc
		mutable = nc1
	} else {
		immutable = nc1
		mutable = nc
	}
	res := copyNodeCache(immutable)
	atomic.StoreUint64(&res.immutable, 0)
	res.nodes.merge(mutable.nodes)
	res.states.merge(mutable.states)
	return res, nil
}

func copyNodeCache(n *nodecache) *nodecache {
	if n == nil || n.buffer == nil {
		return nil
	}
	nc := newNodeCache(int(n.limit), nil, nil, n.layers)
	nc.immutable = atomic.LoadUint64(&n.immutable)

	for acc, subTree := range n.nodes.nodes {
		nc.nodes.nodes[acc] = maps.Clone(subTree)
	}
	nc.nodes.size = n.nodes.size

	storageData := make(map[common.Hash]map[common.Hash][]byte, len(n.states.storageData))
	for accountHash, storage := range n.states.storageData {
		storageData[accountHash] = maps.Clone(storage)
	}
	nc.states = newStates(maps.Clone(n.states.accountData), storageData, n.states.rawStorageKey)
	return nc
}
