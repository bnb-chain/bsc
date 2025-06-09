package pathdb

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// WriteStats tracks write operation statistics
type WriteStats struct {
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
func (ws *WriteStats) UpdateProcessTime(duration time.Duration) {
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

type incrManager struct {
	// Core components
	db          *Database // Reference to parent Database for accessing diskdb
	incrDB      *rawdb.IncrDB
	chainConfig *params.ChainConfig

	count      uint64
	skipCount  uint64
	endStateID uint64

	// Async write control
	writeQueue chan *diffLayer
	stopChan   chan struct{}
	wg         sync.WaitGroup

	// Statistics
	stats WriteStats

	// State
	started bool
	lock    sync.RWMutex
}

// NewIncrManager creates a new incremental manager with async write capability
func NewIncrManager(db *Database, incrDB *rawdb.IncrDB) *incrManager {
	im := &incrManager{
		db:         db,
		incrDB:     incrDB,
		writeQueue: make(chan *diffLayer, 100),
		stopChan:   make(chan struct{}),
		started:    false,
	}

	chainConfig, err := rawdb.GetChainConfig(db.diskdb.BlockStore())
	if err != nil {
		log.Crit("Failed to get chain config", "err", err)
	}
	im.chainConfig = chainConfig

	return im
}

// Start starts the async write workers
func (im *incrManager) Start() {
	im.lock.Lock()
	defer im.lock.Unlock()

	if im.started {
		log.Warn("Incremental store already started")
		return
	}

	im.wg.Add(1)
	go im.worker()

	im.started = true
	log.Info("Incremental store async workers started")
}

// Stop stops the async write workers and waits for completion
// Statistics are preserved for debugging purposes
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
		// Drain queue first
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

	im.started = false
	im.LogStats()
}

// commit submits an async write task.
func (im *incrManager) commit(bottom *diffLayer) error {
	if !im.started {
		return errors.New("incremental store not started")
	}

	// Check if directory switch is needed before committing
	switched, err := im.incrDB.CheckAndInitiateSwitch(bottom.block, im)
	if err != nil {
		return fmt.Errorf("failed to switch directory: %v", err)
	}

	// If directory switched, check and fill empty blocks
	if switched {
		log.Info("Directory switch completed, checking for empty blocks", "blockNumber", bottom.block)
		if err = im.checkAndFillEmptyBlocks(bottom.block); err != nil {
			log.Error("Failed to fill empty blocks after directory switch", "block", bottom.block, "err", err)
			return err
		}
	}

	atomic.AddUint64(&im.stats.totalTasks, 1)
	atomic.AddInt32(&im.stats.queueLength, 1)

	select {
	case im.writeQueue <- bottom:
		return nil

	case <-im.stopChan:
		atomic.AddInt32(&im.stats.queueLength, -1)
		return errors.New("incremental store is stopping")

	default:
		atomic.AddInt32(&im.stats.queueLength, -1)
		queueLen := im.GetQueueLength()
		log.Warn("Task queue is full, checking if directory switch is in progress", "queueLength", queueLen,
			"block", bottom.block, "stateID", bottom.stateID(), "switching", im.incrDB.IsSwitching())

		if im.incrDB.IsSwitching() {
			log.Info("Queue full during directory switch - this is expected",
				"block", bottom.block, "queueLength", queueLen)
			return nil
		}

		log.Error("Task queue is full outside of directory switch", "queueLength", queueLen, "block", bottom.block)
		im.LogStats()
		return fmt.Errorf("task queue is full (length %d, block %d)", queueLen, bottom.block)
	}
}

// worker processes write tasks asynchronously
func (im *incrManager) worker() {
	defer im.wg.Done()

	for {
		select {
		case dl := <-im.writeQueue:
			atomic.AddInt32(&im.stats.queueLength, -1)

			startTime := time.Now()
			err := im.processWriteTask(dl)
			processingTime := time.Since(startTime)
			im.stats.UpdateProcessTime(processingTime)
			if err != nil {
				log.Error("Async write task failed", "block", dl.block, "stateID", dl.stateID(),
					"processingTime", processingTime, "err", err)
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
	if dl.stateID() <= im.endStateID {
		im.count++
		if im.count%10000 == 0 {
			log.Info("Print skipped info", "im.count", im.count, "dl.stateID()", dl.stateID(), "im.endStateID", im.endStateID)
		}
		return nil
	}

	// check and write block firstly
	blockHash := rawdb.ReadCanonicalHash(im.db.diskdb.BlockStore(), dl.block)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", dl.block)
	}
	h, _ := rawdb.ReadHeaderAndRaw(im.db.diskdb.BlockStore(), blockHash, dl.block)
	if h == nil {
		return fmt.Errorf("block header missing, can't freeze block %d", dl.block)
	}
	isCancun := im.chainConfig.IsCancun(h.Number, h.Time)
	if err := rawdb.ResetEmptyIncrChainTable(im.incrDB.GetChainFreezer(), dl.block, isCancun); err != nil {
		log.Error("Failed to reset empty incr chain freezer", "block", dl.block, "err", err)
		return err
	}
	// Write chain data first
	if err := im.writeChainData(dl.block, dl.stateID()); err != nil {
		log.Error("Failed to write chain data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	// Write state data
	if err := rawdb.ResetEmptyIncrStateTable(im.incrDB.GetStateFreezer(), dl.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}
	if err := im.writeStateData(dl); err != nil {
		log.Error("Failed to write state data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	if im.skipCount != im.count {
		log.Error("different number of skipped blocks", "im.skipCount", im.skipCount, "im.count", im.count)
	}

	return nil
}

// writeStateData writes state data to incremental database
func (im *incrManager) writeStateData(dl *diffLayer) error {
	// Short circuit if state set is not available
	if dl.states == nil {
		return errors.New("state change set is not available")
	}

	var (
		start   = time.Now()
		nodes   = compressTrieNodes(dl.nodes.nodes)
		history = newHistory(dl.rootHash(), dl.parentLayer().rootHash(), dl.block,
			dl.states.accountOrigin, dl.states.storageOrigin, dl.states.rawStorageKey)
	)

	accountData, storageData, accountIndex, storageIndex := history.encode()
	nodesBytes, err := rlp.EncodeToBytes(nodes)
	if err != nil {
		log.Crit("Failed to encode trie nodes", "error", err)
	}

	err = im.incrDB.WriteIncrState(dl.block, dl.stateID(), history.meta.encode(), accountIndex, storageIndex,
		accountData, storageData, nodesBytes)
	if err != nil {
		return err
	}

	log.Debug("Stored incremental history", "id", dl.stateID(), "block", dl.block,
		"nodes size", dl.nodes.size, "elapsed", common.PrettyDuration(time.Since(start)))

	return nil
}

// writeChainData writes incremental chain data
func (im *incrManager) writeChainData(blockNumber, stateID uint64) error {
	// Get freezer reference directly without waiting for switch completion
	// This avoids deadlock while still maintaining data consistency
	head, err := im.incrDB.GetChainFreezer().Ancients()
	if err != nil {
		log.Error("Failed to get ancients from incr chain freezer", "err", err)
		return err
	}

	var startBlock uint64
	if blockNumber == head {
		startBlock = blockNumber
		log.Debug("Block number is equal to head", "blockNumber", blockNumber)
	} else if blockNumber > head {
		startBlock = head
		log.Debug("Block number is greater than head", "freezerHead", head, "blockNumber", blockNumber,
			"gapSize", blockNumber-head)
	} else {
		if blockNumber < head {
			log.Crit("Block number should be greater than or equal to head", "blockNumber", blockNumber,
				"head", head)
		}
	}

	for i := startBlock; i <= blockNumber; i++ {
		// check if this block has state changes
		currentStateID := uint64(0)
		if i == blockNumber {
			currentStateID = stateID
		}

		if err = im.writeIncrBlock(im.db.diskdb.BlockStore(), i, currentStateID); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", currentStateID, "err", err)
			return err
		}
	}

	log.Debug("Incremental block data processing completed", "startBlock", startBlock, "endBlock", blockNumber,
		"totalProcessed", blockNumber-startBlock+1)
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

// DrainQueue waits for all pending tasks to be processed
func (im *incrManager) DrainQueue() {
	im.drainQueue()
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

// IsQueueNearFull returns true if queue usage is above 80%
func (im *incrManager) IsQueueNearFull() bool {
	return im.GetQueueUsageRate() > 80.0
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

// IsHealthy returns true if the incremental store is functioning properly
func (im *incrManager) IsHealthy() bool {
	total := atomic.LoadUint64(&im.stats.totalTasks)
	failed := atomic.LoadUint64(&im.stats.failedTasks)

	if total == 0 {
		return true
	}

	failureRate := float64(failed) / float64(total)
	return failureRate < 0.1
}

// checkAndFillEmptyBlocks checks for gaps and fills empty blocks after directory switch
func (im *incrManager) checkAndFillEmptyBlocks(currentBlock uint64) error {
	lastBlock := im.incrDB.GetLastBlock()

	log.Info("Checking for empty blocks after directory switch",
		"currentBlock", currentBlock, "freezerLastBlock", lastBlock)

	// If there's a gap between freezer and current block, fill it
	if currentBlock > lastBlock+1 {
		startBlock := lastBlock + 1
		endBlock := currentBlock - 1

		log.Info("Found empty block gap, filling",
			"startBlock", startBlock, "endBlock", endBlock, "count", endBlock-startBlock+1)

		// Fill the gap using existing writeChainData logic
		for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
			if err := im.fillSingleEmptyBlock(blockNum); err != nil {
				log.Error("Failed to fill empty block", "block", blockNum, "err", err)
				return fmt.Errorf("failed to fill empty block %d: %v", blockNum, err)
			}
			log.Info("Filled empty block", "block", blockNum)
		}

		log.Info("Successfully filled all empty blocks",
			"startBlock", startBlock, "endBlock", endBlock, "count", endBlock-startBlock+1)
	} else {
		log.Info("No empty blocks to fill", "currentBlock", currentBlock, "freezerLastBlock", lastBlock)
	}

	return nil
}

// fillSingleEmptyBlock fills a single empty block, reusing existing logic
func (im *incrManager) fillSingleEmptyBlock(blockNum uint64) error {
	blockStore := im.db.diskdb.BlockStore()
	if blockStore == nil {
		return errors.New("block store not available")
	}

	// Check if block exists
	blockHash := rawdb.ReadCanonicalHash(blockStore, blockNum)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", blockNum)
	}

	h, _ := rawdb.ReadHeaderAndRaw(blockStore, blockHash, blockNum)
	if h == nil {
		return fmt.Errorf("block header not found for block %d", blockNum)
	}
	isCancun := im.chainConfig.IsCancun(h.Number, h.Time)

	// Reset chain table for this empty block
	chainFreezer := im.incrDB.GetChainFreezer()
	if err := rawdb.ResetEmptyIncrChainTable(chainFreezer, blockNum, isCancun); err != nil {
		return fmt.Errorf("failed to reset chain table: %v", err)
	}

	// Write the empty block (stateID = 0 for empty blocks)
	if err := im.writeIncrBlock(blockStore, blockNum, 0); err != nil {
		return fmt.Errorf("failed to write block to freezer: %v", err)
	}

	return nil
}

// writeIncrBlock writes incremental block
func (im *incrManager) writeIncrBlock(reader ethdb.Reader, blockNumber, stateID uint64) error {
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
		log.Error("Failed to get chain config", "err", err)
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

	err = im.incrDB.WriteIncrBlockData(blockNumber, stateID, blockHash[:], header, body, receipts, td, sidecars, isCancun)
	if err != nil {
		log.Error("Failed to write block data", "err", err)
		return err
	}

	log.Debug("Write one block data into incr chain freezer", "block", blockNumber, "hash", blockHash.Hex())
	return nil
}

// readIncrHistory reads incremental history
func readIncrHistory(reader ethdb.AncientReader, id uint64) (*history, error) {
	blob := rawdb.ReadStateHistoryMeta(reader, id)
	if len(blob) == 0 {
		return nil, fmt.Errorf("state history not found %d", id)
	}
	var m meta
	if err := m.decode(blob); err != nil {
		return nil, err
	}

	var (
		dec            = history{meta: &m}
		accountData    = rawdb.ReadStateAccountHistory(reader, id)
		storageData    = rawdb.ReadStateStorageHistory(reader, id)
		accountIndexes = rawdb.ReadStateAccountIndex(reader, id)
		storageIndexes = rawdb.ReadStateStorageIndex(reader, id)
	)
	if err := dec.decode(accountData, storageData, accountIndexes, storageIndexes); err != nil {
		return nil, err
	}
	return &dec, nil
}

func readIncrTrieNodes(reader ethdb.AncientReader, id uint64) (map[common.Hash]map[string]*trienode.Node, error) {
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

	return flattenTrieNodes(decodedTrieNodes), nil
}
