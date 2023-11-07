package pathdb

import (
	"errors"
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

// asyncnodebuffer implement trienodebuffer interface, and aysnc the nodecache
// to disk.
type asyncnodebuffer struct {
	mux        sync.RWMutex
	current    *nodecache
	background *nodecache
}

// newAsyncNodeBuffer initializes the async node buffer with the provided nodes.
func newAsyncNodeBuffer(limit int, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *asyncnodebuffer {
	return &asyncnodebuffer{
		current:    newNodeCache(limit, nodes, layers),
		background: newNodeCache(limit, nil, 0),
	}
}

// node retrieves the trie node with given node info.
func (a *asyncnodebuffer) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	node, err := a.current.node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return a.background.node(owner, path, hash)
	}
	return node, nil
}

// commit merges the dirty nodes into the nodebuffer. This operation won't take
// the ownership of the nodes map which belongs to the bottom-most diff layer.
// It will just hold the node references from the given map which are safe to
// copy.
func (a *asyncnodebuffer) commit(nodes map[common.Hash]map[string]*trienode.Node) trienodebuffer {
	a.mux.Lock()
	defer a.mux.Unlock()

	err := a.current.commit(nodes)
	if err != nil {
		log.Crit("[BUG] failed to commit nodes to asyncnodebuffer", "error", err)
	}
	return a
}

// revert is the reverse operation of commit. It also merges the provided nodes
// into the nodebuffer, the difference is that the provided node set should
// revert the changes made by the last state transition.
func (a *asyncnodebuffer) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	var err error
	a.current, err = a.current.merge(a.background)
	if err != nil {
		log.Crit("[BUG] failed to merge node cache under revert async node buffer", "error", err)
	}
	a.background.reset()
	return a.current.revert(db, nodes)
}

// setSize is unsupported in asyncnodebuffer, due to the double buffer, blocking will occur.
func (a *asyncnodebuffer) setSize(size int, db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
	return errors.New("not supported")
}

// reset cleans up the disk cache.
func (a *asyncnodebuffer) reset() {
	a.mux.Lock()
	defer a.mux.Unlock()

	a.current.reset()
	a.background.reset()
}

// empty returns an indicator if nodebuffer contains any state transition inside.
func (a *asyncnodebuffer) empty() bool {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.empty() && a.background.empty()
}

// flush persists the in-memory dirty trie node into the disk if the configured
// memory threshold is reached. Note, all data must be written atomically.
func (a *asyncnodebuffer) flush(db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64, force bool) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	if force {
		for {
			if atomic.LoadUint64(&a.background.immutable) == 1 {
				time.Sleep(time.Duration(DefaultBackgroundFlushInterval) * time.Second)
				log.Info("waiting background memory table flush to disk for force flush node buffer")
				continue
			}
			atomic.StoreUint64(&a.current.immutable, 1)
			return a.current.flush(db, clean, id)
		}
	}

	if a.current.nb.size < a.current.nb.limit {
		return nil
	}

	// background flush doing
	if atomic.LoadUint64(&a.background.immutable) == 1 {
		return nil
	}

	atomic.StoreUint64(&a.current.immutable, 1)
	a.current, a.background = a.background, a.current

	go func(persistId uint64) {
		for {
			err := a.background.flush(db, clean, persistId)
			if err == nil {
				log.Debug("succeed to flush background nodecahce to disk", "state_id", persistId)
				return
			}
			log.Error("failed to flush background nodecahce to disk", "state_id", persistId, "error", err)
		}
	}(id)
	return nil
}

func (a *asyncnodebuffer) getAllNodes() map[common.Hash]map[string]*trienode.Node {
	a.mux.Lock()
	defer a.mux.Unlock()

	cached, err := a.current.merge(a.background)
	if err != nil {
		log.Crit("[BUG] failed to merge nodecache under revert asyncnodebuffer", "error", err)
	}
	return cached.nb.nodes
}

func (a *asyncnodebuffer) getLayers() uint64 {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.nb.layers + a.background.nb.layers
}

func (a *asyncnodebuffer) getSize() (uint64, uint64) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.nb.size, a.background.nb.size
}

type nodecache struct {
	nb        *nodebuffer // Hold the cache node and layers metadata.
	immutable uint64      // The flag equal 1, flush nodes to disk background
}

func newNodeCache(limit int, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *nodecache {
	return &nodecache{
		nb:        newNodeBuffer(limit, nodes, layers),
		immutable: 0,
	}
}

func (nc *nodecache) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	return nc.nb.node(owner, path, hash)
}

func (nc *nodecache) commit(nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errWriteImmutable
	}
	_ = nc.nb.commit(nodes)
	return nil
}

func (nc *nodecache) reset() {
	atomic.StoreUint64(&nc.immutable, 0)
	nc.nb.reset()
}

func (nc *nodecache) empty() bool {
	return nc.nb.layers == 0
}

func (nc *nodecache) flush(db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
	if atomic.LoadUint64(&nc.immutable) != 1 {
		return errFlushMutable
	}
	return nc.nb.flush(db, clean, id, true)
}

func (nc *nodecache) merge(nc1 *nodecache) (*nodecache, error) {
	if nc == nil && nc1 == nil {
		return nil, nil
	}
	if nc == nil || nc.empty() {
		res := copyNodeCache(nc1)
		atomic.StoreUint64(&res.immutable, 0)
		return nc1, nil
	}
	if nc1 == nil || nc1.empty() {
		res := copyNodeCache(nc)
		atomic.StoreUint64(&res.immutable, 0)
		return nc, nil
	}
	if atomic.LoadUint64(&nc.immutable) == atomic.LoadUint64(&nc1.immutable) {
		return nil, errIncompatibleMerge
	}

	var (
		immutable *nodecache
		mutable   *nodecache
		res       = &nodecache{}
	)
	if atomic.LoadUint64(&nc.immutable) == 1 {
		immutable = nc
		mutable = nc1
	} else {
		immutable = nc1
		mutable = nc
	}
	res.nb.size = immutable.nb.size + mutable.nb.size
	res.nb.layers = immutable.nb.layers + mutable.nb.layers
	res.nb.limit = immutable.nb.size
	res.nb.nodes = make(map[common.Hash]map[string]*trienode.Node)
	for acc, subTree := range immutable.nb.nodes {
		if _, ok := res.nb.nodes[acc]; !ok {
			res.nb.nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			res.nb.nodes[acc][path] = node
		}
	}

	for acc, subTree := range mutable.nb.nodes {
		if _, ok := res.nb.nodes[acc]; !ok {
			res.nb.nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			res.nb.nodes[acc][path] = node
		}
	}
	return res, nil
}

func (nc *nodecache) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errRevertImmutable
	}
	return nc.nb.revert(db, nodes)
}

func copyNodeCache(n *nodecache) *nodecache {
	if n == nil {
		return nil
	}
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	for acc, subTree := range n.nb.nodes {
		if _, ok := nodes[acc]; !ok {
			nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			nodes[acc][path] = node
		}
	}

	nb := newNodeBuffer(int(n.nb.limit), nodes, n.nb.layers)
	nc := &nodecache{
		nb:        nb,
		immutable: atomic.LoadUint64(&n.immutable),
	}
	return nc
}
