package pathdb

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// asyncIncrStateBuffer writes the incremental state trie nodes into incr state db.
type asyncIncrStateBuffer struct {
	mux          sync.RWMutex
	current      *incrNodeCache
	background   *incrNodeCache
	isFlushing   atomic.Bool
	stopFlushing atomic.Bool
}

// newAsyncIncrStateBuffer initializes the async incremental state buffer.
func newAsyncIncrStateBuffer(limit, batchSize uint64) *asyncIncrStateBuffer {
	b := &asyncIncrStateBuffer{
		current:    newIncrNodeCache(limit, batchSize, nil, 0),
		background: newIncrNodeCache(limit, batchSize, nil, 0),
	}
	return b
}

// commit merges the provided states and trie nodes into the buffer.
func (a *asyncIncrStateBuffer) commit(nodes *nodeSet, stateID, blockNumber uint64) *asyncIncrStateBuffer {
	a.mux.Lock()
	defer a.mux.Unlock()

	err := a.current.commit(nodes, stateID, blockNumber)
	if err != nil {
		log.Crit("Failed to commit nodes to async incremental state buffer", "error", err)
	}
	return a
}

// empty returns an indicator if buffer contains any state transition inside.
func (a *asyncIncrStateBuffer) empty() bool {
	a.mux.RLock()
	defer a.mux.RUnlock()

	return a.current.empty() && a.background.empty()
}

// flush persists the in-memory trie nodes to ancient db if the memory threshold is reached.
func (a *asyncIncrStateBuffer) flush(incrDB *rawdb.IncrSnapDB, force bool) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	if a.stopFlushing.Load() {
		return nil
	}

	if force {
		for {
			if atomic.LoadUint64(&a.background.immutable) == 1 {
				log.Info("Waiting background incremental state buffer flushed to disk for forcing flush")
				time.Sleep(time.Duration(DefaultBackgroundFlushInterval) * time.Second)
				continue
			}
			atomic.StoreUint64(&a.current.immutable, 1)
			return a.current.flush(incrDB)
		}
	}

	if !a.current.full() {
		return nil
	}

	if atomic.LoadUint64(&a.background.immutable) == 1 {
		return nil
	}

	atomic.StoreUint64(&a.current.immutable, 1)
	a.current, a.background = a.background, a.current

	a.isFlushing.Store(true)
	go func() {
		defer a.isFlushing.Store(false)
		for {
			err := a.background.flush(incrDB)
			if err == nil {
				log.Info("Successfully flushed incremental state buffer to ancient db")
				return
			}
			log.Error("Failed to flush incremental state buffer to ancient db", "error", err)
		}
	}()

	return nil
}

// waitAndStopFlushing waits for ongoing flush operations to complete and stops flushing
func (a *asyncIncrStateBuffer) waitAndStopFlushing() {
	a.stopFlushing.Store(true)
	for a.isFlushing.Load() {
		time.Sleep(time.Second)
		log.Warn("Waiting for incremental state buffer flush to complete")
	}
}

// incrStateMetadata represents metadata for incremental state storage
type incrStateMetadata struct {
	NodeCount        uint64
	Layers           uint64
	StateIDArray     [2]uint64
	BlockNumberArray [2]uint64
}

// incrNodeCache is a specialized cache for incremental trie nodes
type incrNodeCache struct {
	nodes            *nodeSet
	layers           uint64
	limit            uint64 // Memory limit in bytes
	batchSize        uint64
	stateIDArray     [2]uint64
	blockNumberArray [2]uint64
	immutable        uint64 // atomic flag: 1 = immutable, 0 = mutable
}

var emptyArray = [2]uint64{0, 0}

// newIncrNodeCache creates a new incremental node cache
func newIncrNodeCache(limit, batchSize uint64, nodes *nodeSet, layers uint64) *incrNodeCache {
	if nodes == nil {
		nodes = newNodeSet(nil)
	}

	return &incrNodeCache{
		nodes:            nodes,
		layers:           layers,
		limit:            limit,
		batchSize:        batchSize,
		stateIDArray:     emptyArray,
		blockNumberArray: emptyArray,
		immutable:        0,
	}
}

// commit adds nodes and states to the cache
func (c *incrNodeCache) commit(nodes *nodeSet, stateID, blockNumber uint64) error {
	if atomic.LoadUint64(&c.immutable) == 1 {
		return fmt.Errorf("cannot commit to immutable cache")
	}

	if nodes != nil {
		c.nodes.merge(nodes)
	}
	if c.stateIDArray[0] == 0 && c.stateIDArray[1] == 0 {
		c.stateIDArray[0] = stateID
		c.stateIDArray[1] = stateID
		c.blockNumberArray[0] = blockNumber
		c.blockNumberArray[1] = blockNumber
	} else {
		c.stateIDArray[1] = stateID
		c.blockNumberArray[1] = blockNumber
	}
	c.layers++

	return nil
}

// flush writes the immutable cache to the incremental state db.
func (c *incrNodeCache) flush(incrDB *rawdb.IncrSnapDB) error {
	if atomic.LoadUint64(&c.immutable) != 1 {
		return fmt.Errorf("cannot flush mutable cache")
	}

	err := c.flushToAncientDB(incrDB)
	if err != nil {
		return err
	}

	atomic.StoreUint64(&c.immutable, 0)
	return nil
}

// flushToAncientDB writes the trie nodes to the incremental state db.
func (c *incrNodeCache) flushToAncientDB(incrDB *rawdb.IncrSnapDB) error {
	nodeCount := 0
	jn := make([]journalNodes, 0, len(c.nodes.nodes))

	for owner, subset := range c.nodes.nodes {
		entry := journalNodes{Owner: owner}
		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})
		}
		jn = append(jn, entry)
		nodeCount++

		if len(jn) >= int(c.batchSize) {
			if err := c.writeBatchToAncientDB(incrDB, jn); err != nil {
				return err
			}
			jn = jn[:0]
		}
	}

	if len(jn) > 0 {
		if err := c.writeBatchToAncientDB(incrDB, jn); err != nil {
			return err
		}
	}
	
	log.Info("Flushed incremental state buffer to ancient db", "total_nodes", nodeCount, "size", c.nodes.size)
	c.reset()
	return nil
}

// writeBatchToAncientDB writes a batch of trie nodes to the incremental state db.
func (c *incrNodeCache) writeBatchToAncientDB(incrDB *rawdb.IncrSnapDB, jn []journalNodes) error {
	if len(jn) == 0 {
		return nil
	}

	ancients, _ := incrDB.GetStateFreezer().Ancients()
	incrementalID := ancients + 1

	encodedBatch, err := rlp.EncodeToBytes(jn)
	if err != nil {
		return fmt.Errorf("failed to RLP encode trie node batch: %v", err)
	}
	m := incrStateMetadata{
		NodeCount:        uint64(len(jn)),
		Layers:           c.layers,
		StateIDArray:     c.stateIDArray,
		BlockNumberArray: c.blockNumberArray,
	}
	metaBytes, err := rlp.EncodeToBytes(m)
	if err != nil {
		return fmt.Errorf("failed to RLP encode metadata: %v", err)
	}

	if err = incrDB.WriteIncrState(incrementalID, metaBytes, encodedBatch); err != nil {
		log.Error("Failed to write incremental state", "error", err, "ancients", ancients,
			"incrementalID", incrementalID)
		return err
	}

	log.Info("Wrote incremental state batch to ancient db", "incrementalID", incrementalID,
		"nodeCount", len(jn), "layers", c.layers, "stateIDArray", c.stateIDArray, "blockNumberArray", c.blockNumberArray)

	return nil
}

// empty returns true if the cache is empty
func (c *incrNodeCache) empty() bool {
	return c.layers == 0
}

// full returns true if the cache exceeds the memory limit
func (c *incrNodeCache) full() bool {
	return c.size() > c.limit
}

// size returns the approximate memory size of the cache
func (c *incrNodeCache) size() uint64 {
	return c.nodes.size
}

// reset clears the cache
func (c *incrNodeCache) reset() {
	atomic.StoreUint64(&c.immutable, 0)
	c.nodes.reset()
	c.layers = 0
	c.stateIDArray = emptyArray
	c.blockNumberArray = emptyArray
}
