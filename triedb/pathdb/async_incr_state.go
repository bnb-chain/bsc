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
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// asyncIncrStateBuffer writes the incremental state trie nodes into incr state db.
type asyncIncrStateBuffer struct {
	mux        sync.RWMutex
	current    *incrNodeBuffer
	background *incrNodeBuffer

	truncateChan   chan uint64
	flushedStateID atomic.Uint64

	isFlushing   atomic.Bool
	stopFlushing atomic.Bool
	done         chan struct{}
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
func (a *asyncIncrStateBuffer) commit(root common.Hash, nodes *nodeSet, states *stateSet, stateID, blockNumber uint64) *asyncIncrStateBuffer {
	a.mux.Lock()
	defer a.mux.Unlock()

	err := a.current.commit(root, nodes, states, stateID, blockNumber)
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

func (a *asyncIncrStateBuffer) getFlushedStateID() uint64 {
	old := a.flushedStateID.Load()
	a.flushedStateID.Store(0)
	return old
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
				log.Info("Waiting background incr state buffer flushed to disk for forcing flush")
				time.Sleep(3 * time.Second)
				continue
			}
			atomic.StoreUint64(&a.current.immutable, 1)
			a.flushedStateID.Store(a.current.stateIDArray[1])
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
				a.forwardTruncateSignal(a.background)
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

// forwardTruncateSignal forwards truncate signal from buffer to truncate channel
func (a *asyncIncrStateBuffer) forwardTruncateSignal(buffer *incrNodeBuffer) {
	select {
	case lastStateID := <-buffer.getTruncateSignal():
		select {
		case a.truncateChan <- lastStateID:
			log.Info("Forwarded truncate signal", "stateID", lastStateID)
		default:
			log.Debug("Truncate channel full, skipping signal")
		}
	default:
	}
}

// incrNodeBuffer is a specialized buffer for incremental trie nodes
type incrNodeBuffer struct {
	*buffer
	root             common.Hash
	batchSize        uint64 // Maximum flush batch size
	stateIDArray     [2]uint64
	blockNumberArray [2]uint64
	immutable        uint64 // atomic flag: 1 = immutable, 0 = mutable
	truncateSignal   chan uint64
}

var emptyArray = [2]uint64{0, 0}

// newIncrNodeBuffer creates a new incremental node buffer
func newIncrNodeBuffer(limit, batchSize uint64, nodes *nodeSet, states *stateSet, layers uint64) *incrNodeBuffer {
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
func (c *incrNodeBuffer) commit(root common.Hash, nodes *nodeSet, states *stateSet, stateID, blockNumber uint64) error {
	if atomic.LoadUint64(&c.immutable) == 1 {
		return fmt.Errorf("cannot commit to immutable cache")
	}

	c.buffer.commit(nodes, states)
	c.root = root
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

	if err := c.flushTrieNodes(incrDB); err != nil {
		return err
	}
	if err := c.flushStates(incrDB); err != nil {
		return err
	}

	c.resetIncrBuffer()
	return nil
}

func (c *incrNodeBuffer) flushStates(incrDB *rawdb.IncrSnapDB) error {
	var acc accounts
	var storages []storage
	currentSize := uint64(0)

	// Helper function to write current batch and reset
	writeBatch := func() error {
		if len(acc.AddrHashes) == 0 && len(storages) == 0 {
			return nil
		}

		s := statesData{
			RawStorageKey: c.states.rawStorageKey,
			Acc:           acc,
			Storages:      storages,
		}

		if err := c.writeStatesToAncientDB(incrDB, s); err != nil {
			return err
		}

		// Reset for next batch
		acc = accounts{}
		storages = make([]storage, 0)
		currentSize = 0
		return nil
	}

	// Process account data
	for addrHash, blob := range c.states.accountData {
		accountSize := uint64(len(addrHash[:]) + len(blob))

		if currentSize+accountSize > c.batchSize && (len(acc.AddrHashes) > 0 || len(storages) > 0) {
			if err := writeBatch(); err != nil {
				return err
			}
		}

		acc.AddrHashes = append(acc.AddrHashes, addrHash)
		acc.Accounts = append(acc.Accounts, blob)
		currentSize += accountSize
	}

	// Process storage data
	for addrHash, slots := range c.states.storageData {
		keys := make([]common.Hash, 0, len(slots))
		vals := make([][]byte, 0, len(slots))
		storageSize := uint64(len(addrHash[:]))

		for key, val := range slots {
			slotSize := uint64(len(key[:]) + len(val))

			if currentSize+storageSize+slotSize > c.batchSize && (len(acc.AddrHashes) > 0 || len(storages) > 0 || len(keys) > 0) {
				// Finish current storage if we have keys
				if len(keys) > 0 {
					storages = append(storages, storage{
						AddrHash: addrHash,
						Keys:     keys,
						Vals:     vals,
					})
				}

				if err := writeBatch(); err != nil {
					return err
				}

				// Start new storage
				keys = make([]common.Hash, 0, len(slots))
				vals = make([][]byte, 0, len(slots))
				storageSize = uint64(len(addrHash[:]))
			}

			keys = append(keys, key)
			vals = append(vals, val)
			storageSize += slotSize
		}

		if len(keys) > 0 {
			storages = append(storages, storage{
				AddrHash: addrHash,
				Keys:     keys,
				Vals:     vals,
			})
			currentSize += storageSize
		}
	}

	// Write remaining data
	return writeBatch()
}

func (c *incrNodeBuffer) flushTrieNodes(incrDB *rawdb.IncrSnapDB) error {
	jn := make([]journalNodes, 0, len(c.nodes.storageNodes)+1)
	totalSize := uint64(0)

	processNodes := func(owner common.Hash, nodes map[string]*trienode.Node) error {
		entry := journalNodes{Owner: owner}
		ownerSize := uint64(len(owner[:]))
		nodesListSize := uint64(0)

		for path, node := range nodes {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})

			nodeSize := uint64(len([]byte(path)) + len(node.Blob))
			nodesListSize += nodeSize
			currentEntrySize := ownerSize + nodesListSize
			newTotalSize := totalSize + currentEntrySize

			if newTotalSize >= c.batchSize {
				log.Info("Batch size limit reached during node iteration, flushing nodes to ancient db",
					"newTotalSize", common.StorageSize(newTotalSize), "entryCount", len(jn)+1)
				if err := c.writeTrieNodesToAncientDB(incrDB, append(jn, entry)); err != nil {
					return err
				}

				jn = make([]journalNodes, 0, len(c.nodes.storageNodes)+1)
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
			log.Info("Batch size limit reached after adding entry, flushing nodes to ancient db",
				"totalSize", common.StorageSize(totalSize), "entryCount", len(jn))
			if err := c.writeTrieNodesToAncientDB(incrDB, jn); err != nil {
				return err
			}
			jn = make([]journalNodes, 0, len(c.nodes.storageNodes)+1)
			totalSize = 0
		}
		return nil
	}

	if len(c.nodes.accountNodes) > 0 {
		if err := processNodes(common.Hash{}, c.nodes.accountNodes); err != nil {
			return err
		}
	}
	for owner, subset := range c.nodes.storageNodes {
		if err := processNodes(owner, subset); err != nil {
			return err
		}
	}

	// Flush remaining nodes and states
	if len(jn) > 0 {
		log.Info("Flushing remaining trie nodes to ancient db", "entryCount", len(jn))
		if err := c.writeTrieNodesToAncientDB(incrDB, jn); err != nil {
			return err
		}
	}

	log.Info("Flushed incremental state buffer to ancient db", "size", common.StorageSize(c.nodes.size))
	return nil
}

// writeTrieNodesToAncientDB writes a batch of trie nodes to the incremental state db.
func (c *incrNodeBuffer) writeTrieNodesToAncientDB(incrDB *rawdb.IncrSnapDB, jn []journalNodes) error {
	if len(jn) == 0 {
		return nil
	}

	ancients, _ := incrDB.GetStateFreezer().Ancients()
	incrementalID := ancients + 1

	encodedBatch, err := rlp.EncodeToBytes(jn)
	if err != nil {
		return fmt.Errorf("failed to RLP encode trie node batch: %v", err)
	}
	m := rawdb.IncrStateMetadata{
		Root:             c.root,
		HasStates:        false,
		NodeCount:        uint64(len(jn)),
		Layers:           c.layers,
		StateIDArray:     c.stateIDArray,
		BlockNumberArray: c.blockNumberArray,
	}
	metaBytes, err := rlp.EncodeToBytes(m)
	if err != nil {
		return fmt.Errorf("failed to RLP encode metadata: %v", err)
	}

	if err = incrDB.WriteIncrTrieNodes(incrementalID, metaBytes, encodedBatch); err != nil {
		log.Error("Failed to write incr trie nodes", "error", err, "ancients", ancients,
			"incrementalID", incrementalID)
		return err
	}

	select {
	case c.truncateSignal <- c.stateIDArray[1]:
		log.Debug("Sent truncate signal after WriteIncrState", "incrementalID", incrementalID)
	default:
		log.Debug("Truncate signal channel full, skipping signal")
	}

	log.Info("Wrote incr trie nodes to ancient db", "incrementalID", incrementalID, "nodeCount", len(jn),
		"layers", c.layers, "nodesSize", common.StorageSize(len(encodedBatch)), "stateIDArray", c.stateIDArray,
		"blockNumberArray", c.blockNumberArray)
	return nil
}

// writeStatesToAncientDB writes a batch of states to the incremental state db.
func (c *incrNodeBuffer) writeStatesToAncientDB(incrDB *rawdb.IncrSnapDB, s statesData) error {
	ancients, _ := incrDB.GetStateFreezer().Ancients()
	incrementalID := ancients + 1

	encodedBatch, err := rlp.EncodeToBytes(s)
	if err != nil {
		return fmt.Errorf("failed to RLP encode trie node batch: %v", err)
	}
	m := rawdb.IncrStateMetadata{
		Root:             c.root,
		HasStates:        true,
		NodeCount:        0,
		Layers:           c.layers,
		StateIDArray:     c.stateIDArray,
		BlockNumberArray: c.blockNumberArray,
	}
	metaBytes, err := rlp.EncodeToBytes(m)
	if err != nil {
		return fmt.Errorf("failed to RLP encode metadata: %v", err)
	}

	if err = incrDB.WriteIncrState(incrementalID, metaBytes, encodedBatch); err != nil {
		log.Error("Failed to write incr state", "error", err, "ancients", ancients,
			"incrementalID", incrementalID)
		return err
	}

	log.Info("Wrote incr state batch to ancient db", "incrementalID", incrementalID,
		"statesSize", common.StorageSize(len(encodedBatch)))
	return nil
}

// resetIncrBuffer resets the incr buffer
func (c *incrNodeBuffer) resetIncrBuffer() {
	atomic.StoreUint64(&c.immutable, 0)
	c.reset()
	c.root = common.Hash{}
	c.stateIDArray = emptyArray
	c.blockNumberArray = emptyArray
}

// getTruncateSignal returns the truncate signal channel
func (c *incrNodeBuffer) getTruncateSignal() <-chan uint64 {
	return c.truncateSignal
}
