package pathdb

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// asyncIncrStateBuffer writes the incremental state trie nodes into incr state db.
type asyncIncrStateBuffer struct {
	mux          sync.RWMutex
	current      *incrNodeBuffer
	background   *incrNodeBuffer
	isFlushing   atomic.Bool
	stopFlushing atomic.Bool
	done         chan struct{}
	truncateChan chan uint64
}

// newAsyncIncrStateBuffer initializes the async incremental state buffer.
func newAsyncIncrStateBuffer(limit, batchSize uint64) *asyncIncrStateBuffer {
	b := &asyncIncrStateBuffer{
		current:      newIncrNodeBuffer(limit, batchSize, nil, nil, 0),
		background:   newIncrNodeBuffer(limit, batchSize, nil, nil, 0),
		done:         make(chan struct{}),
		truncateChan: make(chan uint64, 1),
	}

	// Start monitoring goroutine
	go b.monitorStateBuffer()

	return b
}

// monitorStateBuffer monitors the state buffer every 5 minutes
func (a *asyncIncrStateBuffer) monitorStateBuffer() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.printBufferInfo()
		case <-a.done:
			log.Debug("Monitor buffer stopped due to done signal")
			return
		}
	}
}

// printBufferInfo prints detailed info about both current and background buffer
func (a *asyncIncrStateBuffer) printBufferInfo() {
	a.mux.RLock()
	defer a.mux.RUnlock()

	log.Info("Current buffer Status", "empty", a.current.empty(), "full", a.current.full(),
		"totalSize", common.StorageSize(a.current.size()), "nodesSize", common.StorageSize(a.current.nodes.size),
		"statesSize", common.StorageSize(a.current.states.size), "layers", a.current.layers,
		"immutable", atomic.LoadUint64(&a.current.immutable) == 1,
		"stateIDRange", fmt.Sprintf("%d-%d", a.current.stateIDArray[0], a.current.stateIDArray[1]),
		"blockNumberRange", fmt.Sprintf("%d-%d", a.current.blockNumberArray[0], a.current.blockNumberArray[1]),
		"limit", common.StorageSize(a.current.limit), "batchSize", common.StorageSize(a.current.batchSize))

	log.Info("Background buffer Status", "empty", a.background.empty(), "full", a.background.full(),
		"totalSize", common.StorageSize(a.background.size()), "nodesSize", common.StorageSize(a.background.nodes.size),
		"statesSize", common.StorageSize(a.background.states.size), "layers", a.background.layers,
		"immutable", atomic.LoadUint64(&a.background.immutable) == 1,
		"stateIDRange", fmt.Sprintf("%d-%d", a.background.stateIDArray[0], a.background.stateIDArray[1]),
		"blockNumberRange", fmt.Sprintf("%d-%d", a.background.blockNumberArray[0], a.background.blockNumberArray[1]),
		"limit", common.StorageSize(a.current.limit), "batchSize", common.StorageSize(a.current.batchSize))
}

// commit merges the provided states and trie nodes into the buffer.
func (a *asyncIncrStateBuffer) commit(nodes *nodeSet, states *stateSet, stateID, blockNumber uint64) *asyncIncrStateBuffer {
	a.mux.Lock()
	defer a.mux.Unlock()

	err := a.current.commit(nodes, states, stateID, blockNumber)
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
			// TODO: whether need to get truncate signal here?
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

				select {
				case lastStateID := <-a.background.getTruncateSignal():
					select {
					case a.truncateChan <- lastStateID:
						log.Info("Forwarded truncate signal after background flush", "stateID", lastStateID)
					default:
						log.Debug("Truncate channel full, skipping signal")
					}
				default:
				}
				return
			}
			log.Error("Failed to flush incremental state buffer to ancient db", "error", err)
		}
	}()

	return nil
}

// waitAndStopFlushing waits for ongoing flush operations to complete and stops flushing
func (a *asyncIncrStateBuffer) waitAndStopFlushing() {
	// Stop monitoring goroutine immediately using done channel
	close(a.done)

	// Stop flushing operations
	a.stopFlushing.Store(true)

	// Wait for flush operations to complete
	for a.isFlushing.Load() {
		time.Sleep(time.Second)
		log.Warn("Waiting for incremental state buffer flush to complete")
	}
}

// getTruncateSignal returns the truncate signal channel
func (a *asyncIncrStateBuffer) getTruncateSignal() <-chan uint64 {
	return a.truncateChan
}

// incrStateMetadata represents metadata for incremental state storage
type incrStateMetadata struct {
	NodeCount        uint64
	StateCount       uint64
	Layers           uint64
	StateIDArray     [2]uint64
	BlockNumberArray [2]uint64
}

// incrNodeBuffer is a specialized buffer for incremental trie nodes
type incrNodeBuffer struct {
	*buffer
	batchSize        uint64 // Maximum flush batch size
	stateIDArray     [2]uint64
	blockNumberArray [2]uint64
	immutable        uint64 // atomic flag: 1 = immutable, 0 = mutable
	truncateSignal   chan uint64
}

var emptyArray = [2]uint64{0, 0}

// newIncrNodeBuffer creates a new incremental node buffer
func newIncrNodeBuffer(limit, batchSize uint64, nodes *nodeSet, states *stateSet, layers uint64) *incrNodeBuffer {
	if nodes == nil {
		nodes = newNodeSet(nil)
	}

	return &incrNodeBuffer{
		buffer:           newBuffer(int(limit), nodes, states, layers),
		batchSize:        batchSize,
		stateIDArray:     emptyArray,
		blockNumberArray: emptyArray,
		immutable:        0,
		truncateSignal:   make(chan uint64, 1),
	}
}

// commit adds nodes and states to the buffer
func (c *incrNodeBuffer) commit(nodes *nodeSet, states *stateSet, stateID, blockNumber uint64) error {
	if atomic.LoadUint64(&c.immutable) == 1 {
		return fmt.Errorf("cannot commit to immutable cache")
	}

	c.buffer.commit(nodes, states)
	if c.stateIDArray[0] == 0 && c.stateIDArray[1] == 0 {
		c.stateIDArray[0] = stateID
		c.stateIDArray[1] = stateID
		c.blockNumberArray[0] = blockNumber
		c.blockNumberArray[1] = blockNumber
	} else {
		c.stateIDArray[1] = stateID
		c.blockNumberArray[1] = blockNumber
	}

	return nil
}

// flush writes the immutable buffer to the incremental state db.
func (c *incrNodeBuffer) flush(incrDB *rawdb.IncrSnapDB) error {
	if atomic.LoadUint64(&c.immutable) != 1 {
		return fmt.Errorf("cannot flush mutable cache")
	}

	if err := c.flushToAncientDB(incrDB); err != nil {
		return err
	}
	atomic.StoreUint64(&c.immutable, 0)
	return nil
}

func (c *incrNodeBuffer) flushStates() statesData {
	var acc accounts
	for addrHash, blob := range c.states.accountData {
		acc.AddrHashes = append(acc.AddrHashes, addrHash)
		acc.Accounts = append(acc.Accounts, blob)
	}

	storages := make([]storage, 0, len(c.states.storageData))
	for addrHash, slots := range c.states.storageData {
		keys := make([]common.Hash, 0, len(slots))
		vals := make([][]byte, 0, len(slots))
		for key, val := range slots {
			keys = append(keys, key)
			vals = append(vals, val)
		}
		storages = append(storages, storage{
			AddrHash: addrHash,
			Keys:     keys,
			Vals:     vals,
		})
	}

	s := statesData{
		RawStorageKey: c.states.rawStorageKey,
		Acc:           acc,
		Storages:      storages,
	}
	return s
}

func (c *incrNodeBuffer) flushToAncientDB(incrDB *rawdb.IncrSnapDB) error {
	jn := make([]journalNodes, 0, len(c.nodes.nodes))
	totalSize := uint64(0)

	for owner, subset := range c.nodes.nodes {
		entry := journalNodes{Owner: owner}
		ownerSize := uint64(len(owner[:]))
		nodesListSize := uint64(0)

		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})

			nodeSize := uint64(len([]byte(path)) + len(node.Blob))
			nodesListSize += nodeSize
			currentEntrySize := ownerSize + nodesListSize
			newTotalSize := totalSize + currentEntrySize

			if newTotalSize >= c.batchSize {
				log.Info("Batch size limit reached during node iteration, flushing to ancient db",
					"newTotalSize", common.StorageSize(newTotalSize), "entryCount", len(jn)+1)
				if err := c.writeBatchToAncientDB(incrDB, append(jn, entry)); err != nil {
					return err
				}

				jn = make([]journalNodes, 0, len(c.nodes.nodes))
				totalSize = 0
				entry = journalNodes{Owner: owner} // Reset entry for remaining nodes
				ownerSize = uint64(len(owner[:]))  // Reset owner size
				nodesListSize = 0
			}
		}

		if len(entry.Nodes) > 0 {
			jn = append(jn, entry)
			entrySize := ownerSize + nodesListSize
			totalSize += entrySize
		}

		if totalSize >= c.batchSize {
			log.Info("Batch size limit reached after adding entry, flushing to ancient db",
				"totalSize", common.StorageSize(totalSize), "entryCount", len(jn))
			if err := c.writeBatchToAncientDB(incrDB, jn); err != nil {
				return err
			}
			jn = make([]journalNodes, 0, len(c.nodes.nodes))
			totalSize = 0
		}
	}

	if len(jn) > 0 {
		log.Info("Flushing remaining incremental state buffer to ancient db", "entryCount", len(jn))
		if err := c.writeBatchToAncientDB(incrDB, jn); err != nil {
			return err
		}
	}
	log.Info("Flushed incremental state buffer to ancient db", "size", common.StorageSize(c.nodes.size))
	c.reset()
	return nil
}

// writeBatchToAncientDB writes a batch of trie nodes to the incremental state db.
func (c *incrNodeBuffer) writeBatchToAncientDB(incrDB *rawdb.IncrSnapDB, jn []journalNodes) error {
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

	if err = incrDB.WriteIncrState(incrementalID, metaBytes, encodedBatch, nil); err != nil {
		log.Error("Failed to write incremental state", "error", err, "ancients", ancients,
			"incrementalID", incrementalID)
		return err
	}

	select {
	case c.truncateSignal <- c.stateIDArray[1]:
		log.Debug("Sent truncate signal after WriteIncrState", "incrementalID", incrementalID)
	default:
		log.Debug("Truncate signal channel full, skipping signal")
	}

	log.Info("Wrote incremental state batch to ancient db", "incrementalID", incrementalID, "nodeCount", len(jn),
		"layers", c.layers, "nodesSize", common.StorageSize(len(encodedBatch)), "stateIDArray", c.stateIDArray,
		"blockNumberArray", c.blockNumberArray)
	return nil
}

// empty returns true if the cache is empty
func (c *incrNodeBuffer) empty() bool {
	return c.layers == 0
}

// TODO: whether need to add states size here?
// full returns true if the cache exceeds the memory limit
func (c *incrNodeBuffer) full() bool {
	return c.size() > c.limit
}

// TODO: whether need to add states size here?
// size returns the approximate memory size of the cache
func (c *incrNodeBuffer) size() uint64 {
	return c.nodes.size
}

// reset clears the cache
func (c *incrNodeBuffer) reset() {
	atomic.StoreUint64(&c.immutable, 0)
	c.nodes.reset()
	c.states.reset()
	c.layers = 0
	c.stateIDArray = emptyArray
	c.blockNumberArray = emptyArray
}

// getTruncateSignal returns the truncate signal channel
func (c *incrNodeBuffer) getTruncateSignal() <-chan uint64 {
	return c.truncateSignal
}
