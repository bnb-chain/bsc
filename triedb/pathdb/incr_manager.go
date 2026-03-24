package pathdb

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

const (
	// The default kept blocks in incremental chain freezer: 1024.
	DefaultKeptBlocks = 1024

	// Number of blocks after which to save the parlia snapshot to the database
	parliaSnapCheckpointInterval = 1024

	// The default number of blocks and state history stored in incr freezer db
	DefaultBlockInterval = 100000

	// The default memory allowance for incremental state buffer: 6GB
	DefaultIncrStateBufferSize = 6 * 1024 * 1024 * 1024

	// The maximum size of the batch to be flushed into the ancient db: 2GB
	defaultFlushBatchSize = 2 * 1024 * 1024 * 1024
)

// writeStats tracks write operation statistics
type writeStats struct {
	totalTasks       uint64
	completedTasks   uint64
	failedTasks      uint64
	queueLength      int32
	avgProcessTime   uint64    // Average processing time in nanoseconds
	maxProcessTime   uint64    // Maximum processing time in nanoseconds
	totalProcessTime uint64    // Total processing time for calculating average
	lastResetTime    time.Time // Last time stats were reset
}

// UpdateProcessTime updates processing time statistics
func (ws *writeStats) UpdateProcessTime(duration time.Duration) {
	durationNs := uint64(duration.Nanoseconds())

	// Update max processing time
	for {
		current := atomic.LoadUint64(&ws.maxProcessTime)
		if durationNs <= current || atomic.CompareAndSwapUint64(&ws.maxProcessTime, current, durationNs) {
			break
		}
	}

	// Update total processing time for average calculation
	atomic.AddUint64(&ws.totalProcessTime, durationNs)

	// Calculate and update average
	completed := atomic.LoadUint64(&ws.completedTasks)
	if completed > 0 {
		avg := atomic.LoadUint64(&ws.totalProcessTime) / completed
		atomic.StoreUint64(&ws.avgProcessTime, avg)
	}
}

// incrManager manages incremental state storage with async write capability
type incrManager struct {
	db          *Database // Reference to parent Database for accessing diskdb
	incrDB      *rawdb.IncrSnapDB
	chainConfig *params.ChainConfig

	// used to skip duplicate blocks until end block
	duplicateEndBlock uint64

	// Async write control
	writeQueue chan *diffLayer
	stopChan   chan struct{}
	wg         sync.WaitGroup

	stats   writeStats
	started bool
	lock    sync.RWMutex

	// Async incremental state buffer
	asyncBuffer *asyncIncrStateBuffer
	bufferLimit uint64 // Memory limit for buffer
}

// NewIncrManager creates a new incremental manager with async write capability
func NewIncrManager(db *Database, incrDB *rawdb.IncrSnapDB) *incrManager {
	im := &incrManager{
		db:          db,
		incrDB:      incrDB,
		writeQueue:  make(chan *diffLayer, 100),
		stopChan:    make(chan struct{}),
		started:     false,
		bufferLimit: db.config.IncrStateBuffer,
	}

	chainConfig, err := rawdb.GetChainConfig(db.diskdb)
	if err != nil {
		log.Crit("Failed to get chain config", "error", err)
	}
	im.chainConfig = chainConfig

	// Initialize async incremental state buffer
	im.asyncBuffer = newAsyncIncrStateBuffer(im.bufferLimit, defaultFlushBatchSize)
	return im
}

// Start starts the async write workers and directory switch checker
func (im *incrManager) Start() {
	im.lock.Lock()
	defer im.lock.Unlock()

	if im.started {
		log.Warn("Incremental store already started")
		return
	}

	im.wg.Add(1)
	go im.worker()

	im.wg.Add(1)
	go im.listenTruncateSignal()

	im.started = true
	log.Info("Incremental store async worker started")
}

// Stop stops the async write workers and directory switch checker
func (im *incrManager) Stop() {
	im.lock.Lock()
	defer im.lock.Unlock()

	if !im.started {
		return
	}

	log.Info("Stopping incremental store", "pending_tasks", im.GetQueueLength())

	// Set a timeout for graceful shutdown
	shutdownTimeout := 30 * time.Second
	shutdownComplete := make(chan struct{})

	go func() {
		im.drainQueue()

		// Stop workers
		close(im.stopChan)
		im.wg.Wait()

		close(shutdownComplete)
	}()

	// Wait for graceful shutdown or timeout
	select {
	case <-shutdownComplete:
		log.Info("Incremental store stopped gracefully")
	case <-time.After(shutdownTimeout):
		log.Warn("Incremental store shutdown timeout, forcing stop",
			"timeout", shutdownTimeout, "remaining_tasks", im.GetQueueLength())
	}

	if im.asyncBuffer != nil {
		if err := im.ForceFlushStateBuffer(); err != nil {
			log.Crit("Failed to force flush data", "error", err)
		}
	}

	ancients, _ := im.incrDB.GetChainFreezer().Ancients()
	_ = im.truncateExtraBlock(ancients - 1)

	im.started = false
	im.LogStats()
}

// listenTruncateSignal listens truncate state freezer and incr chain freezer signal.
func (im *incrManager) listenTruncateSignal() {
	truncateTicker := time.NewTicker(time.Second * 3)
	defer truncateTicker.Stop()
	defer im.wg.Done()

	for {
		select {
		case <-truncateTicker.C:
			ancients, err := im.incrDB.GetChainFreezer().Ancients()
			if err != nil {
				log.Error("Failed to get ancients in truncating", "error", err)
			}
			if ancients == 0 {
				continue
			}
			if err = im.truncateExtraBlock(ancients - 1); err != nil {
				continue
			}

		case stateID := <-im.asyncBuffer.getTruncateSignal():
			if err := im.truncateStateFreezer(stateID); err != nil {
				continue
			}

		case <-im.stopChan:
			log.Debug("Truncate signal listener stopped")
			return
		}
	}
}

// commit submits an async write task
func (im *incrManager) commit(bottom *diffLayer) error {
	if !im.started {
		return errors.New("incremental store not started")
	}

	atomic.AddUint64(&im.stats.totalTasks, 1)
	atomic.AddInt32(&im.stats.queueLength, 1)

	if im.incrDB.IsSwitching() {
		log.Info("Directory switching in progress, waiting for completion", "block", bottom.block, "stateID", bottom.stateID())
		for im.incrDB.IsSwitching() {
			time.Sleep(100 * time.Millisecond)
		}
	}

	select {
	case im.writeQueue <- bottom:
		return nil

	case <-im.stopChan:
		atomic.AddInt32(&im.stats.queueLength, -1)
		return errors.New("incremental store is stopping")

	default:
		atomic.AddInt32(&im.stats.queueLength, -1)
		log.Error("Task queue is full", "queueLength", im.GetQueueLength(), "block", bottom.block)
		im.LogStats()
		return fmt.Errorf("task queue is full (length %d, block %d)", im.GetQueueLength(), bottom.block)
	}
}

// worker processes write tasks asynchronously
func (im *incrManager) worker() {
	defer im.wg.Done()

	for {
		select {
		case dl := <-im.writeQueue:
			if dl == nil {
				log.Crit("Diff layer is nil")
				return
			}
			atomic.AddInt32(&im.stats.queueLength, -1)

			log.Debug("Worker received task", "block", dl.block, "stateID", dl.stateID(), "queueLength", im.GetQueueLength())

			startTime := time.Now()
			err := im.processWriteTask(dl)
			processingTime := time.Since(startTime)
			im.stats.UpdateProcessTime(processingTime)
			if err != nil {
				log.Error("Async write task failed", "block", dl.block, "stateID", dl.stateID(),
					"processingTime", processingTime, "error", err)
				incrProcessErrorMeter.Mark(1)
				return
			} else {
				log.Debug("Task processed successfully", "block", dl.block, "stateID", dl.stateID(), "processingTime", processingTime)
			}

			im.updateStats(err)
		case <-im.stopChan:
			log.Debug("Worker stopping")
			return
		}
	}
}

func (im *incrManager) processWriteTask(dl *diffLayer) error {
	// skip already written incremental data
	if dl.block <= im.duplicateEndBlock {
		return nil
	}

	// Write incremental data
	if err := im.resetIncrChainFreezer(im.db.diskdb, dl.block); err != nil {
		return err
	}
	if err := im.writeIncrData(dl); err != nil {
		return err
	}

	return nil
}

// writeIncrData writes incremental data: chain and state
func (im *incrManager) writeIncrData(dl *diffLayer) error {
	head, err := im.incrDB.GetChainFreezer().Ancients()
	if err != nil {
		log.Error("Failed to get ancients from incr chain freezer", "error", err)
		return err
	}

	var startBlock uint64
	if dl.block == head {
		startBlock = dl.block
	} else if dl.block > head {
		startBlock = head
	} else {
		if dl.block < head {
			log.Crit("Block number should be greater than or equal to head", "blockNumber", dl.block,
				"head", head)
		}
	}

	for i := startBlock; i <= dl.block; i++ {
		// check if this block has state changes
		isEmptyBlock := true
		currStateID := dl.stateID() - 1
		if i == dl.block {
			isEmptyBlock = false
			currStateID = dl.stateID()
		}

		if im.incrDB.Full() {
			if err = im.truncateExtraBlock(i - 1); err != nil {
				log.Error("Failed to truncate incr chain freezer", "blockNumber", i-1, "error", err)
				return err
			}
			switched, err := im.incrDB.CheckAndInitiateSwitch(i, im)
			if err != nil {
				log.Error("Failed to check and switch incremental db", "error", err)
				return err
			}

			if switched {
				im.asyncBuffer = newAsyncIncrStateBuffer(im.bufferLimit, defaultFlushBatchSize)
				// record the first state id in pebble
				im.incrDB.WriteFirstStateID(dl.stateID() - 1)
				log.Info("Directory switch completed", "blockNumber", i, "stateID", dl.stateID())
			}

			if err = im.resetIncrChainFreezer(im.db.diskdb, i); err != nil {
				log.Error("Failed to reset incr chain freezer", "blockNumber", i, "error", err)
				return err
			}
		}

		if err = im.writeIncrBlock(im.db.diskdb, i, currStateID, isEmptyBlock); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", dl.stateID(), "error", err)
			return err
		}
		if !isEmptyBlock {
			if err = im.writeIncrStateData(dl); err != nil {
				log.Error("Failed to write incr state data", "block", dl.block, "stateID", dl.stateID(), "error", err)
				return err
			}
		}
	}

	log.Debug("Incremental block data processing completed", "startBlock", startBlock, "endBlock", dl.block,
		"totalProcessed", dl.block-startBlock+1)
	return nil
}

func (im *incrManager) resetIncrChainFreezer(reader ethdb.Reader, blockNumber uint64) error {
	blockHash := rawdb.ReadCanonicalHash(reader, blockNumber)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", blockNumber)
	}
	h, _ := rawdb.ReadHeaderAndRaw(reader, blockHash, blockNumber)
	if h == nil {
		return fmt.Errorf("block header missing, can't freeze block %d", blockNumber)
	}
	isCancun := im.chainConfig.IsCancun(h.Number, h.Time)
	if err := rawdb.ResetEmptyIncrChainTable(im.incrDB.GetChainFreezer(), blockNumber, isCancun); err != nil {
		log.Error("Failed to reset empty incr chain freezer", "block", blockNumber, "error", err)
		return err
	}
	return nil
}

// writeIncrStateData writes incr state data using async incremental state buffer
func (im *incrManager) writeIncrStateData(dl *diffLayer) error {
	// Short circuit if states is not available
	if dl.states == nil {
		return errors.New("state change set is not available")
	}

	start := time.Now()
	// Commit to async buffer instead of direct write
	im.asyncBuffer.commit(dl.root, dl.nodes.nodeSet, dl.states.stateSet, dl.stateID(), dl.block)
	if err := im.asyncBuffer.flush(im.incrDB, false); err != nil {
		return fmt.Errorf("failed to flush async incremental state buffer: %v", err)
	}
	log.Debug("Committed to incremental state buffer", "id", dl.stateID(), "block", dl.block,
		"nodes_size", dl.nodes.size, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

func (im *incrManager) truncateExtraBlock(blockNumber uint64) error {
	// always reload the incr chain freezer to
	incrChainFreezer := im.incrDB.GetChainFreezer()
	tail, err := incrChainFreezer.Tail()
	if err != nil {
		log.Error("Failed to get incr chain freezer tail", "error", err)
		return err
	}
	if tail == 0 {
		return nil
	}

	// Only truncate if we have more blocks than the limit and there are actual blocks to truncate
	if blockNumber-tail >= im.db.config.IncrKeptBlocks {
		targetTail := blockNumber - im.db.config.IncrKeptBlocks + 1
		pruned, err := truncateIncrChainFreezerFromTail(incrChainFreezer, targetTail)
		if err != nil {
			log.Error("Failed to truncate chain freezer", "target_tail", targetTail, "current_tail", tail,
				"blockNumber", blockNumber, "error", err)
			return err
		}

		if err = incrChainFreezer.SyncAncient(); err != nil {
			log.Error("Failed to sync after incr chain freezer truncation", "error", err)
		} else {
			log.Debug("Successfully synced incr chain freezer after truncation")
		}
		log.Debug("Pruned incr chain history", "items", pruned, "target_tail", targetTail, "old_tail", tail)
	}
	return nil
}

// updateStats updates operation statistics
func (im *incrManager) updateStats(err error) {
	if err != nil {
		atomic.AddUint64(&im.stats.failedTasks, 1)
	} else {
		atomic.AddUint64(&im.stats.completedTasks, 1)
	}
}

// drainQueue waits for all pending tasks to be processed
func (im *incrManager) drainQueue() {
	for {
		queueLen := im.GetQueueLength()
		if queueLen == 0 {
			break
		}
		log.Debug("Waiting for queue to drain", "remaining", queueLen)
		time.Sleep(100 * time.Millisecond)
	}
}

// GetQueueLength returns the current number of pending tasks
func (im *incrManager) GetQueueLength() int {
	return int(atomic.LoadInt32(&im.stats.queueLength))
}

// GetQueueCapacity returns the maximum queue capacity
func (im *incrManager) GetQueueCapacity() int {
	return cap(im.writeQueue)
}

// GetQueueUsageRate returns the queue usage rate as a percentage
func (im *incrManager) GetQueueUsageRate() float64 {
	queueLen := im.GetQueueLength()
	capacity := im.GetQueueCapacity()
	if capacity == 0 {
		return 0
	}
	return float64(queueLen) / float64(capacity) * 100
}

// GetStats returns current statistics
func (im *incrManager) GetStats() (total, completed, failed uint64, queueLen int) {
	return atomic.LoadUint64(&im.stats.totalTasks),
		atomic.LoadUint64(&im.stats.completedTasks),
		atomic.LoadUint64(&im.stats.failedTasks),
		im.GetQueueLength()
}

// LogStats logs current statistics
func (im *incrManager) LogStats() {
	total := atomic.LoadUint64(&im.stats.totalTasks)
	completed := atomic.LoadUint64(&im.stats.completedTasks)
	failed := atomic.LoadUint64(&im.stats.failedTasks)
	queueLen := im.GetQueueLength()
	queueCapacity := im.GetQueueCapacity()
	queueUsage := im.GetQueueUsageRate()

	avgProcessTime := atomic.LoadUint64(&im.stats.avgProcessTime)
	maxProcessTime := atomic.LoadUint64(&im.stats.maxProcessTime)

	successRate := float64(0)
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}

	log.Info("Incremental store statistics", "total_tasks", total, "completed", completed,
		"failed", failed, "pending", queueLen, "queue_capacity", queueCapacity, "queue_usage", fmt.Sprintf("%.1f%%", queueUsage),
		"success_rate", fmt.Sprintf("%.2f%%", successRate), "avg_process_time", time.Duration(avgProcessTime),
		"max_process_time", time.Duration(maxProcessTime), "switching", im.incrDB.IsSwitching(),
		"uptime", time.Since(im.stats.lastResetTime).Round(time.Second))
}

// writeIncrBlock writes incremental block
func (im *incrManager) writeIncrBlock(reader ethdb.Reader, blockNumber, stateID uint64, isEmptyBlock bool) error {
	blockHash := rawdb.ReadCanonicalHash(reader, blockNumber)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", blockNumber)
	}
	h, header := rawdb.ReadHeaderAndRaw(reader, blockHash, blockNumber)
	if len(header) == 0 {
		return fmt.Errorf("block header missing, can't freeze block %d", blockNumber)
	}
	body := rawdb.ReadBodyRLP(reader, blockHash, blockNumber)
	if len(body) == 0 {
		return fmt.Errorf("block body missing, can't freeze block %d", blockNumber)
	}
	receipts := rawdb.ReadReceiptsRLP(reader, blockHash, blockNumber)
	if len(receipts) == 0 {
		return fmt.Errorf("block receipts missing, can't freeze block %d", blockNumber)
	}
	td := rawdb.ReadTdRLP(reader, blockHash, blockNumber)
	if len(td) == 0 {
		return fmt.Errorf("total difficulty not found for block %d (hash: %s)", blockNumber, blockHash.Hex())
	}

	chainConfig, err := rawdb.GetChainConfig(reader)
	if err != nil {
		log.Error("Failed to get chain config", "error", err)
		return err
	}
	// blobs is nil before cancun fork
	var sidecars rlp.RawValue
	isCancun := chainConfig.IsCancun(h.Number, h.Time)
	if isCancun {
		sidecars = rawdb.ReadBlobSidecarsRLP(reader, blockHash, blockNumber)
		if len(sidecars) == 0 {
			return fmt.Errorf("block blobs missing, can't freeze block %d", blockNumber)
		}
	}

	err = im.incrDB.WriteIncrBlockData(blockNumber, stateID, blockHash[:], header, body, receipts, td, sidecars, isEmptyBlock, isCancun)
	if err != nil {
		log.Error("Failed to write block data", "error", err)
		return err
	}

	if blockNumber%parliaSnapCheckpointInterval == 0 {
		blob, err := reader.Get(append(rawdb.ParliaSnapshotPrefix, blockHash[:]...))
		if err != nil {
			log.Error("Failed to get parlia snapshot", "error", err)
			return err
		}
		im.incrDB.WriteParliaSnapshot(blockHash, blob)
		log.Debug("Writing parlia snapshot into incremental", "blockNumber", blockNumber)
	}

	log.Debug("Write one block data into incr chain freezer", "block", blockNumber, "hash", blockHash.Hex())
	return nil
}

// ForceFlushStateBuffer forces all buffered data in asyncIncrStateBuffer to be flushed.
// This is called before directory switch to ensure data integrity
func (im *incrManager) ForceFlushStateBuffer() error {
	if im.asyncBuffer == nil {
		return nil
	}

	// Check if there's any data to flush
	if im.asyncBuffer.empty() {
		log.Info("No buffered data to flush")
		return nil
	}

	// Force flush all data
	if err := im.asyncBuffer.flush(im.incrDB, true); err != nil {
		return fmt.Errorf("failed to force flush all buffered data: %v", err)
	}

	im.asyncBuffer.waitAndStopFlushing()

	// Get the last stateID from the buffer
	stateID := im.asyncBuffer.getFlushedStateID()
	if stateID > 0 {
		if err := im.truncateStateFreezer(stateID); err != nil {
			log.Error("Failed to truncate state freezer", "stateID", stateID, "error", err)
			return err
		}
	}

	return nil
}

// truncateStateFreezer truncate state history by flushed stateID.
func (im *incrManager) truncateStateFreezer(stateID uint64) error {
	tail, err := im.db.stateFreezer.Tail()
	if err != nil {
		return nil
	}
	limit := im.db.config.StateHistory
	if limit == 0 || stateID-tail <= limit {
		log.Info("No truncation needed", "stateID", stateID, "tail", tail, "limit", limit)
		return nil
	}

	pruned, err := truncateFromTail(im.db.stateFreezer, typeStateHistory, stateID-limit)
	if err != nil {
		log.Error("Failed to truncate from tail", "error", err, "target", stateID)
		return err
	}
	log.Info("Successfully truncated state history", "pruned_items", pruned, "target_tail", stateID)
	return nil
}

func readIncrTrieNodes(reader ethdb.AncientReader, id uint64) (*nodeSet, error) {
	data, err := rawdb.ReadIncrStateTrieNodes(reader, id)
	if err != nil {
		log.Error("Failed to read incremental trie nodes", "id", id, "error", err)
		return nil, err
	}

	var decodedTrieNodes []journalNodes
	if err = rlp.DecodeBytes(data, &decodedTrieNodes); err != nil {
		log.Error("Failed to decode incremental trie nodes", "id", id, "error", err)
		return nil, err
	}

	return newNodeSet(flattenTrieNodes(decodedTrieNodes)), nil
}

func readIncrStatesData(reader ethdb.AncientReader, id uint64) (*stateSet, error) {
	data, err := rawdb.ReadIncrStatesData(reader, id)
	if err != nil {
		log.Error("Failed to read incr states data", "id", id, "error", err)
		return nil, err
	}

	var s statesData
	if err = rlp.DecodeBytes(data, &s); err != nil {
		log.Error("Failed to decode incr states data", "id", id, "error", err)
		return nil, err
	}

	accountSet := make(map[common.Hash][]byte)
	for i := 0; i < len(s.Acc.AddrHashes); i++ {
		accountSet[s.Acc.AddrHashes[i]] = s.Acc.Accounts[i]
	}

	storageSet := make(map[common.Hash]map[common.Hash][]byte)
	for _, entry := range s.Storages {
		storageSet[entry.AddrHash] = make(map[common.Hash][]byte, len(entry.Keys))
		for i := 0; i < len(entry.Keys); i++ {
			storageSet[entry.AddrHash][entry.Keys[i]] = entry.Vals[i]
		}
	}

	return newStates(accountSet, storageSet, s.RawStorageKey), nil
}

// flattenTrieNodes returns a two-dimensional map for internal nodes.
func flattenTrieNodes(jn []journalNodes) map[common.Hash]map[string]*trienode.Node {
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	for _, entry := range jn {
		subset := make(map[string]*trienode.Node)
		for _, n := range entry.Nodes {
			if len(n.Blob) > 0 {
				subset[string(n.Path)] = trienode.New(crypto.Keccak256Hash(n.Blob), n.Blob)
			} else {
				subset[string(n.Path)] = trienode.NewDeleted()
			}
		}
		nodes[entry.Owner] = subset
	}
	return nodes
}
