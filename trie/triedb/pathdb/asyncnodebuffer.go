package pathdb

import (
	"errors"
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
	if nodes == nil {
		nodes = make(map[common.Hash]map[string]*trienode.Node)
	}
	var size uint64
	for _, subset := range nodes {
		for path, n := range subset {
			size += uint64(len(n.Blob) + len(path))
		}
	}

	return &asyncnodebuffer{
		current:    newNodeCache(uint64(limit), size, nodes, layers),
		background: newNodeCache(uint64(limit), 0, make(map[common.Hash]map[string]*trienode.Node), 0),
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

// setSize sets the buffer size to the provided number, and invokes a flush
// operation if the current memory usage exceeds the new limit.
//func (b *nodebuffer) setSize(size int, db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
//	b.limit = uint64(size)
//	return b.flush(db, clean, id, false)
//}

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

	if a.current.size < a.current.limit {
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
	return cached.nodes
}

func (a *asyncnodebuffer) getLayers() uint64 {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.layers + a.background.layers
}

func (a *asyncnodebuffer) getSize() (uint64, uint64) {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.size, a.background.size
}

type nodecache struct {
	layers    uint64                                    // The number of diff layers aggregated inside
	size      uint64                                    // The size of aggregated writes
	limit     uint64                                    // The maximum memory allowance in bytes
	nodes     map[common.Hash]map[string]*trienode.Node // The dirty node set, mapped by owner and path
	immutable uint64                                    // The flag equal 1, flush nodes to disk background
}

func newNodeCache(limit, size uint64, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *nodecache {
	return &nodecache{
		layers:    layers,
		size:      size,
		limit:     limit,
		nodes:     nodes,
		immutable: 0,
	}
}

func (nc *nodecache) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	subset, ok := nc.nodes[owner]
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

func (nc *nodecache) commit(nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errWriteImmutable
	}
	var (
		delta         int64
		overwrite     int64
		overwriteSize int64
	)
	for owner, subset := range nodes {
		current, exist := nc.nodes[owner]
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
			nc.nodes[owner] = current
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
		nc.nodes[owner] = current
	}
	nc.updateSize(delta)
	nc.layers++
	gcNodesMeter.Mark(overwrite)
	gcBytesMeter.Mark(overwriteSize)
	return nil
}

func (nc *nodecache) updateSize(delta int64) {
	size := int64(nc.size) + delta
	if size >= 0 {
		nc.size = uint64(size)
		return
	}
	s := nc.size
	nc.size = 0
	log.Error("Invalid pathdb buffer size", "prev", common.StorageSize(s), "delta", common.StorageSize(delta))
}

func (nc *nodecache) reset() {
	atomic.StoreUint64(&nc.immutable, 0)
	nc.layers = 0
	nc.size = 0
	nc.nodes = make(map[common.Hash]map[string]*trienode.Node)
}

func (nc *nodecache) empty() bool {
	return nc.layers == 0
}

func (nc *nodecache) flush(db ethdb.KeyValueStore, clean *fastcache.Cache, id uint64) error {
	if atomic.LoadUint64(&nc.immutable) != 1 {
		return errFlushMutable
	}

	// Ensure the target state id is aligned with the internal counter.
	head := rawdb.ReadPersistentStateID(db)
	if head+nc.layers != id {
		return fmt.Errorf("buffer layers (%d) cannot be applied on top of persisted state id (%d) to reach requested state id (%d)", nc.layers, head, id)
	}
	var (
		start = time.Now()
		batch = db.NewBatchWithSize(int(float64(nc.size) * DefaultBatchRedundancyRate))
	)
	nodes := writeNodes(batch, nc.nodes, clean)
	rawdb.WritePersistentStateID(batch, id)

	// Flush all mutations in a single batch
	size := batch.ValueSize()
	if err := batch.Write(); err != nil {
		return err
	}
	commitBytesMeter.Mark(int64(size))
	commitNodesMeter.Mark(int64(nodes))
	commitTimeTimer.UpdateSince(start)
	log.Debug("Persisted pathdb nodes", "nodes", len(nc.nodes), "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	nc.reset()
	return nil
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

func (nc *nodecache) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
	if atomic.LoadUint64(&nc.immutable) == 1 {
		return errRevertImmutable
	}

	// Short circuit if no embedded state transition to revert.
	if nc.layers == 0 {
		return errStateUnrecoverable
	}
	nc.layers--

	// Reset the entire buffer if only a single transition left.
	if nc.layers == 0 {
		nc.reset()
		return nil
	}
	var delta int64
	for owner, subset := range nodes {
		current, ok := nc.nodes[owner]
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
	nc.updateSize(delta)
	return nil
}

func copyNodeCache(n *nodecache) *nodecache {
	if n == nil {
		return nil
	}
	nc := &nodecache{
		layers:    n.layers,
		size:      n.size,
		limit:     n.limit,
		immutable: atomic.LoadUint64(&n.immutable),
		nodes:     make(map[common.Hash]map[string]*trienode.Node),
	}
	for acc, subTree := range n.nodes {
		if _, ok := nc.nodes[acc]; !ok {
			nc.nodes[acc] = make(map[string]*trienode.Node)
		}
		for path, node := range subTree {
			nc.nodes[acc][path] = node
		}
	}
	return nc
}
