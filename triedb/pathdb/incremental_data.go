package pathdb

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// IncrStoreConfig holds configuration for incremental store
type IncrStoreConfig struct {
	QueueSize         int           // Size of the task queue buffer
	WorkerCount       int           // Number of async write workers
	SwitchWaitTimeout time.Duration // Timeout for waiting directory switch
	HealthThreshold   float64       // Failure rate threshold for health check
}

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

// ResetStats resets all statistics
func (ws *WriteStats) ResetStats() {
	atomic.StoreUint64(&ws.totalTasks, 0)
	atomic.StoreUint64(&ws.completedTasks, 0)
	atomic.StoreUint64(&ws.failedTasks, 0)
	atomic.StoreInt32(&ws.queueLength, 0)
	atomic.StoreUint64(&ws.avgProcessTime, 0)
	atomic.StoreUint64(&ws.maxProcessTime, 0)
	atomic.StoreUint64(&ws.totalProcessTime, 0)
	ws.lastResetTime = time.Now()
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

type incrStore struct {
	// Core components
	diskDB    ethdb.Database
	incrDB    *rawdb.IncrDB
	freezeEnv atomic.Value

	// Async write control
	writeQueue chan *diffLayer
	stopChan   chan struct{}
	wg         sync.WaitGroup

	// Statistics
	stats     WriteStats
	statsLock sync.RWMutex

	// State
	started bool
	lock    sync.RWMutex
}

// NewIncrStore creates a new incremental store with async write capability
func NewIncrStore(diskDB ethdb.Database, incrDB *rawdb.IncrDB) *incrStore {
	// Use larger queue size to handle directory switching scenarios
	// During directory switch, worker may be blocked, so we need more buffer
	queueSize := 1000 // Increased from 100 to handle switch scenarios

	store := &incrStore{
		diskDB:     diskDB,
		incrDB:     incrDB,
		writeQueue: make(chan *diffLayer, queueSize),
		stopChan:   make(chan struct{}),
		started:    false,
	}

	return store
}

// Start starts the async write workers
func (in *incrStore) Start() {
	in.lock.Lock()
	defer in.lock.Unlock()

	if in.started {
		log.Warn("Incremental store already started")
		return
	}

	in.wg.Add(1)
	go in.worker()

	in.started = true
	log.Info("Incremental store async workers started")
}

// Stop stops the async write workers and waits for completion
// Statistics are preserved for debugging purposes
func (in *incrStore) Stop() {
	in.stopWithReset(false)
}

// StopWithReset stops the async write workers and resets all statistics
// Useful when restarting the store within the same process
func (in *incrStore) StopWithReset() {
	in.stopWithReset(true)
}

// stopWithReset is the internal implementation of stop with optional reset
func (in *incrStore) stopWithReset(resetStats bool) {
	in.lock.Lock()
	defer in.lock.Unlock()

	if !in.started {
		return
	}

	log.Info("Stopping incremental store", "pending", in.GetQueueLength(), "resetStats", resetStats)

	// Set a timeout for graceful shutdown
	shutdownTimeout := 30 * time.Second
	shutdownComplete := make(chan struct{})

	go func() {
		// Drain queue first
		in.drainQueue()

		// Stop workers
		close(in.stopChan)
		in.wg.Wait()

		close(shutdownComplete)
	}()

	// Wait for graceful shutdown or timeout
	select {
	case <-shutdownComplete:
		log.Info("Incremental store stopped gracefully")
	case <-time.After(shutdownTimeout):
		log.Warn("Incremental store shutdown timeout, forcing stop",
			"timeout", shutdownTimeout, "remaining_tasks", in.GetQueueLength())
	}

	// Final cleanup
	in.started = false
	in.LogStats() // Log final statistics before potential reset

	// Optionally reset statistics
	if resetStats {
		log.Debug("Resetting incremental store statistics")
		in.stats.ResetStats()
	}
}

// worker processes write tasks asynchronously
func (in *incrStore) worker() {
	defer in.wg.Done()

	for {
		select {
		case dl := <-in.writeQueue:
			// Update queue length
			atomic.AddInt32(&in.stats.queueLength, -1)

			// Record start time for performance monitoring
			startTime := time.Now()

			err := in.processWriteTask(dl)

			// Record processing time
			processingTime := time.Since(startTime)
			in.stats.UpdateProcessTime(processingTime)

			// Update statistics
			in.updateStats(err)

			if err != nil {
				log.Error("Async write task failed", "block", dl.block, "stateID", dl.stateID(),
					"processingTime", processingTime, "err", err)
			} else {
				log.Debug("Async write task completed", "block", dl.block,
					"stateID", dl.stateID(), "processingTime", processingTime)
			}

		case <-in.stopChan:
			log.Debug("Worker stopping")
			return
		}
	}
}

func (in *incrStore) processWriteTask(dl *diffLayer) error {
	// Check if directory switch is in progress
	if in.incrDB.IsSwitching() {
		log.Info("Directory switch in progress, waiting for completion", "block", dl.block)

		// Add timeout to prevent infinite waiting
		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for in.incrDB.IsSwitching() {
			select {
			case <-timeout:
				log.Error("Timeout waiting for directory switch", "block", dl.block)
				return fmt.Errorf("timeout waiting for directory switch for block %d", dl.block)
			case <-ticker.C:
				// Continue checking
			}
		}
		log.Info("Directory switch completed, resuming commit", "block", dl.block)
	}

	if dl.block%10000 == 0 {
		log.Info("Processing", "stateID", dl.stateID(), "block", dl.block)
	}

	// check and write block firstly
	blockHash := rawdb.ReadCanonicalHash(in.diskDB.BlockStore(), dl.block)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", dl.block)
	}
	h, _ := rawdb.ReadHeaderAndRaw(in.diskDB.BlockStore(), blockHash, dl.block)
	if h == nil {
		return fmt.Errorf("block header missing, can't freeze block %d", dl.block)
	}
	env := in.GetFreezerEnv()
	if env == nil {
		return errors.New("freezer env is not available")
	}
	if err := rawdb.ResetEmptyIncrChainTable(in.incrDB.GetChainFreezer(), dl.block, isCancun(env, h.Number, h.Time)); err != nil {
		log.Error("Failed to reset empty incr chain freezer", "block", dl.block, "err", err)
		return err
	}
	if err := in.writeChainData(dl.block, dl.stateID()); err != nil {
		log.Error("Failed to write chain data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	if err := rawdb.ResetEmptyIncrStateTable(in.incrDB.GetStateFreezer(), dl.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}
	if err := in.writeStateData(dl); err != nil {
		log.Error("Failed to write state data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	if dl.block%10000 == 0 {
		log.Info("Processed", "stateID", dl.stateID(), "block", dl.block)
	}

	return nil
}

// writeStateData writes state data to incremental database
func (in *incrStore) writeStateData(dl *diffLayer) error {
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

	err = in.incrDB.WriteIncrState(dl.stateID(), history.meta.encode(), accountIndex, storageIndex,
		accountData, storageData, nodesBytes)
	if err != nil {
		return err
	}

	log.Debug("Stored incremental history", "id", dl.stateID(), "block", dl.block,
		"nodes size", dl.nodes.size, "elapsed", common.PrettyDuration(time.Since(start)))

	return nil
}

// writeChainData writes incremental chain data
func (in *incrStore) writeChainData(blockNumber, stateID uint64) error {
	env := in.GetFreezerEnv()
	if env == nil {
		return errors.New("freezer env is not available")
	}

	// Check if directory switch is needed before writing
	if in.incrDB.IsBlockLimitReached() && !in.incrDB.IsSwitching() {
		log.Info("Block limit reached, initiating directory switch", "blockNumber", blockNumber)
		if err := in.incrDB.SwitchToNewDirectoryWithAsyncManager(blockNumber, in); err != nil {
			return fmt.Errorf("failed to switch directory: %v", err)
		}
	}

	head, err := in.incrDB.GetChainFreezer().Ancients()
	if err != nil {
		log.Error("Failed to get ancients from incr chain freezer", "err", err)
		return err
	}

	var startBlock uint64
	// Determine the scenario and calculate startBlock
	if blockNumber == head {
		startBlock = blockNumber
		log.Debug("Block number is equal to head", "blockNumber", blockNumber)
	} else if blockNumber > head {
		startBlock = head
		log.Debug("Block number is greater than head",
			"freezerHead", head, "blockNumber", blockNumber, "gapSize", blockNumber-head)
	} else {
		if blockNumber < head {
			log.Crit("Block number should be greater than or equal to head",
				"blockNumber", blockNumber, "head", head)
		}
	}

	for i := startBlock; i <= blockNumber; i++ {
		// Determine if this block has state changes
		currentStateID := uint64(0)
		if i == blockNumber {
			currentStateID = stateID
		}

		if err = writeIncrBlockToFreezer(env, in.diskDB.BlockStore(), in.incrDB, i, currentStateID); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", currentStateID, "err", err)
			return err
		}
	}

	log.Info("Incremental block data processing completed",
		"startBlock", startBlock, "endBlock", blockNumber, "totalProcessed", blockNumber-startBlock+1)
	return nil
}

// commit submits an async write task using fire-and-forget approach
func (in *incrStore) commit(bottom *diffLayer) error {
	if !in.started {
		return errors.New("incremental store not started")
	}

	// Update statistics
	atomic.AddUint64(&in.stats.totalTasks, 1)
	atomic.AddInt32(&in.stats.queueLength, 1)

	select {
	case in.writeQueue <- bottom:
		if bottom.block%10000 == 0 {
			log.Info("Write task submitted", "stateID", bottom.stateID(), "block", bottom.block)
		}
		return nil

	case <-in.stopChan:
		atomic.AddInt32(&in.stats.queueLength, -1)
		return errors.New("incremental store is stopping")

	default:
		atomic.AddInt32(&in.stats.queueLength, -1)

		// Enhanced error handling for queue full scenario
		queueLen := in.GetQueueLength()
		log.Warn("Task queue is full, checking if directory switch is in progress",
			"queueLength", queueLen, "block", bottom.block,
			"stateID", bottom.stateID(), "switching", in.incrDB.IsSwitching())

		// If directory switch is in progress, this is expected
		if in.incrDB.IsSwitching() {
			log.Info("Queue full during directory switch - this is expected",
				"block", bottom.block, "queueLength", queueLen)
			return fmt.Errorf("task queue is full during directory switch (block %d, queue length %d)",
				bottom.block, queueLen)
		}

		// If not switching, this indicates a performance issue
		log.Error("Task queue is full outside of directory switch",
			"queueLength", queueLen, "block", bottom.block)
		in.LogStats()

		return fmt.Errorf("task queue is full (length %d, block %d)", queueLen, bottom.block)
	}
}

// updateStats updates operation statistics
func (in *incrStore) updateStats(err error) {
	if err != nil {
		atomic.AddUint64(&in.stats.failedTasks, 1)
	} else {
		atomic.AddUint64(&in.stats.completedTasks, 1)
	}
}

// drainQueue waits for all pending tasks to be processed
func (in *incrStore) drainQueue() {
	for {
		queueLen := in.GetQueueLength()
		if queueLen == 0 {
			break
		}
		log.Debug("Waiting for queue to drain", "remaining", queueLen)
		time.Sleep(100 * time.Millisecond)
	}
}

// DrainQueue waits for all pending tasks to be processed (interface implementation)
func (in *incrStore) DrainQueue() {
	in.drainQueue()
}

// GetQueueLength returns the current number of pending tasks
func (in *incrStore) GetQueueLength() int {
	return int(atomic.LoadInt32(&in.stats.queueLength))
}

// GetQueueCapacity returns the maximum queue capacity
func (in *incrStore) GetQueueCapacity() int {
	return cap(in.writeQueue)
}

// GetQueueUsageRate returns the queue usage rate as a percentage
func (in *incrStore) GetQueueUsageRate() float64 {
	queueLen := in.GetQueueLength()
	capacity := in.GetQueueCapacity()
	if capacity == 0 {
		return 0
	}
	return float64(queueLen) / float64(capacity) * 100
}

// IsQueueNearFull returns true if queue usage is above 80%
func (in *incrStore) IsQueueNearFull() bool {
	return in.GetQueueUsageRate() > 80.0
}

// GetStats returns current statistics
func (in *incrStore) GetStats() (total, completed, failed uint64, queueLen int) {
	return atomic.LoadUint64(&in.stats.totalTasks),
		atomic.LoadUint64(&in.stats.completedTasks),
		atomic.LoadUint64(&in.stats.failedTasks),
		in.GetQueueLength()
}

// LogStats logs current statistics
func (in *incrStore) LogStats() {
	total := atomic.LoadUint64(&in.stats.totalTasks)
	completed := atomic.LoadUint64(&in.stats.completedTasks)
	failed := atomic.LoadUint64(&in.stats.failedTasks)
	queueLen := in.GetQueueLength()
	queueCapacity := in.GetQueueCapacity()
	queueUsage := in.GetQueueUsageRate()

	avgProcessTime := atomic.LoadUint64(&in.stats.avgProcessTime)
	maxProcessTime := atomic.LoadUint64(&in.stats.maxProcessTime)

	successRate := float64(0)
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}

	log.Info("Incremental store statistics",
		"total_tasks", total,
		"completed", completed,
		"failed", failed,
		"pending", queueLen,
		"queue_capacity", queueCapacity,
		"queue_usage", fmt.Sprintf("%.1f%%", queueUsage),
		"success_rate", fmt.Sprintf("%.2f%%", successRate),
		"avg_process_time", time.Duration(avgProcessTime),
		"max_process_time", time.Duration(maxProcessTime),
		"switching", in.incrDB.IsSwitching(),
		"uptime", time.Since(in.stats.lastResetTime).Round(time.Second))
}

// IsHealthy returns true if the incremental store is functioning properly
func (in *incrStore) IsHealthy() bool {
	total := atomic.LoadUint64(&in.stats.totalTasks)
	failed := atomic.LoadUint64(&in.stats.failedTasks)

	if total == 0 {
		return true
	}

	failureRate := float64(failed) / float64(total)
	return failureRate < 0.1
}

// GetIncrDB returns the IncrDB instance
func (in *incrStore) GetIncrDB() *rawdb.IncrDB {
	return in.incrDB
}

// GetDiskDB returns the disk database
func (in *incrStore) GetDiskDB() ethdb.Database {
	return in.diskDB
}

// GetFreezerEnv returns the freezer environment
func (in *incrStore) GetFreezerEnv() *ethdb.FreezerEnv {
	env, _ := in.freezeEnv.Load().(*ethdb.FreezerEnv)
	return env
}

// SetFreezerEnv sets the freezer environment
func (in *incrStore) SetFreezerEnv(env *ethdb.FreezerEnv) {
	in.freezeEnv.Store(env)
}

func (in *incrStore) checkFreezerEnv() error {
	_, exist := in.freezeEnv.Load().(*ethdb.FreezerEnv)
	if exist {
		return nil
	}
	return errors.New("missing freezer env error")
}

// readIncrData reads the incremental history and trie nodes
func readIncrData(reader ethdb.AncientReader, id uint64) (*history, map[common.Hash]map[string]*trienode.Node, error) {
	blob := rawdb.ReadStateHistoryMeta(reader, id)
	if len(blob) == 0 {
		return nil, nil, fmt.Errorf("state history not found %d", id)
	}
	var m meta
	if err := m.decode(blob); err != nil {
		return nil, nil, err
	}

	var (
		dec            = history{meta: &m}
		accountData    = rawdb.ReadStateAccountHistory(reader, id)
		storageData    = rawdb.ReadStateStorageHistory(reader, id)
		accountIndexes = rawdb.ReadStateAccountIndex(reader, id)
		storageIndexes = rawdb.ReadStateStorageIndex(reader, id)
	)
	if err := dec.decode(accountData, storageData, accountIndexes, storageIndexes); err != nil {
		return nil, nil, err
	}

	data, err := rawdb.ReadIncrStateTrieNodes(reader, id)
	if err != nil {
		log.Crit("Failed to read incremental trie nodes", "error", err)
	}
	var decodedTrieNodes []journalNodes
	if err = rlp.DecodeBytes(data, &decodedTrieNodes); err != nil {
		log.Crit("Failed to decode incremental trie nodes", "error", err)
	}

	return &dec, flattenTrieNodes(decodedTrieNodes), nil
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

// writeIncrBlockToFreezer writes incremental block into freezer
func writeIncrBlockToFreezer(env *ethdb.FreezerEnv, reader ethdb.Reader, incrDB *rawdb.IncrDB, blockNumber, stateID uint64) error {
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
	// blobs is nil before cancun fork
	var sidecars rlp.RawValue
	if isCancun(env, h.Number, h.Time) {
		sidecars = rawdb.ReadBlobSidecarsRLP(reader, blockHash, blockNumber)
		if len(sidecars) == 0 {
			return fmt.Errorf("block blobs missing, can't freeze block %d", blockNumber)
		}
	}

	err := incrDB.WriteIncrBlockData(blockNumber, stateID, blockHash[:], header, body, receipts, td, sidecars, isCancun(env, h.Number, h.Time))
	if err != nil {
		log.Error("Failed to write block data", "err", err)
		return err
	}

	log.Debug("Write one block data into incr chain freezer", "block", blockNumber, "hash", blockHash.Hex())
	return nil
}

func isCancun(env *ethdb.FreezerEnv, num *big.Int, time uint64) bool {
	if env == nil || env.ChainCfg == nil {
		return false
	}

	return env.ChainCfg.IsCancun(num, time)
}

/*
Directory Switch Queue Full Issue - Solutions Implemented:

**Problem:**
During directory switching, the worker thread blocks waiting for the switch to complete,
causing the task queue to fill up and reject new commits.

**Root Cause:**
1. Directory switch blocks the worker thread
2. New commit requests keep coming
3. Queue fills up (reaches capacity)
4. New commits are rejected with "task queue is full"

**Solutions Implemented:**

1. **Increased Queue Capacity**:
   - Raised from 100 to 1000 to provide more buffer during switches
   - Handles temporary spikes during directory operations

2. **Timeout Protection**:
   - Added 30-second timeout for directory switch waiting
   - Prevents infinite blocking if switch gets stuck

3. **Enhanced Error Messages**:
   - Distinguishes between queue full during switch vs normal operation
   - Provides context (block number, queue length, switch status)

4. **Queue Monitoring**:
   - GetQueueUsageRate() for monitoring queue pressure
   - IsQueueNearFull() for early warning (>80% usage)
   - Enhanced logging with queue statistics

5. **Operational Guidance**:
   - Queue full during switch: Expected, will resolve after switch
   - Queue full during normal operation: Performance issue, needs investigation

**Monitoring Commands:**
```go
// Check queue status
if incrStore.IsQueueNearFull() {
    log.Warn("Queue approaching capacity", "usage", incrStore.GetQueueUsageRate())
}

// Log detailed statistics
incrStore.LogStats()

// Check if directory switch is causing the issue
if incrStore.incrDB.IsSwitching() {
    log.Info("Directory switch in progress, queue pressure expected")
}
```

**Expected Behavior:**
- During directory switch: Queue may fill up temporarily (normal)
- After switch completion: Queue should drain quickly
- Normal operation: Queue usage should stay below 50%
*/
