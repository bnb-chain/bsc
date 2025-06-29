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
	writeQueue  chan *diffLayer
	workerCount int
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// Statistics
	stats     WriteStats
	statsLock sync.RWMutex

	// State
	started bool
	lock    sync.RWMutex
}

// NewIncrStore creates a new incremental store with async write capability
func NewIncrStore(diskDB ethdb.Database, incrDB *rawdb.IncrDB, workerCount int) *incrStore {
	store := &incrStore{
		diskDB:      diskDB,
		incrDB:      incrDB,
		writeQueue:  make(chan *diffLayer, 100), // Buffer for 100 tasks
		workerCount: workerCount,
		stopChan:    make(chan struct{}),
		started:     false,
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

	// for i := 0; i < in.workerCount; i++ {
	// 	in.wg.Add(1)
	// 	go in.worker(i)
	// }

	in.started = true
	log.Info("Incremental store async workers started", "workers", in.workerCount)
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
			"timeout", shutdownTimeout,
			"remaining_tasks", in.GetQueueLength())
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
	// Check if directory switch is in progress and wait for completion
	if in.incrDB.IsSwitching() {
		log.Info("Directory switch in progress, waiting for completion", "block", dl.block)
		for in.incrDB.IsSwitching() {
			time.Sleep(50 * time.Millisecond)
		}
		log.Info("Directory switch completed, resuming commit", "block", dl.block)
	}

	// Reset incremental state freezer table - fail fast on error
	if err := rawdb.ResetEmptyIncrStateTable(in.incrDB.GetStateFreezer(), dl.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	// Write state data - fail fast on error
	if err := in.writeStateData(dl); err != nil {
		log.Error("Failed to write state data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	// Get block information
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

	// Reset incremental chain freezer table - fail fast on error
	if err := rawdb.ResetEmptyIncrChainTable(in.incrDB.GetChainFreezer(), dl.block, isCancun(env, h.Number, h.Time)); err != nil {
		log.Error("Failed to reset empty incr chain freezer", "block", dl.block, "err", err)
		return err
	}

	// Write chain data - fail fast on error
	if err := in.writeChainData(dl.block, dl.stateID()); err != nil {
		log.Error("Failed to write chain data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
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

	log.Debug("Incremental block data processing completed",
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
		log.Debug("Write task submitted", "stateID", bottom.stateID(), "block", bottom.block)
		return nil // Fire-and-forget: return immediately

	case <-in.stopChan:
		atomic.AddInt32(&in.stats.queueLength, -1)
		return errors.New("incremental store is stopping")

	default:
		atomic.AddInt32(&in.stats.queueLength, -1)
		return errors.New("task queue is full")
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
		"success_rate", fmt.Sprintf("%.2f%%", successRate),
		"avg_process_time", time.Duration(avgProcessTime),
		"max_process_time", time.Duration(maxProcessTime),
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
Design Philosophy: Fire-and-Forget Async Processing

This incremental store now uses a true async approach:

**Benefits of Removing Callback Channels:**
1. **Reduced Memory Overhead**: No channel allocation per task
2. **Better Performance**: No blocking on async operations
3. **Simplified Code**: Less complexity in task handling
4. **True Async**: Doesn't defeat the purpose of async processing

**Error Handling Strategy:**
Instead of immediate error feedback, we use:
1. **Comprehensive Logging**: All errors are logged with context
2. **Statistics Monitoring**: Track success/failure rates
3. **Health Checks**: IsHealthy() method for system status
4. **Periodic Monitoring**: Check statistics regularly

**Usage Example:**

```go
// Submit tasks asynchronously
for _, diffLayer := range layers {
    if err := incrStore.commit(diffLayer); err != nil {
        // Only queue-related errors (full queue, stopping, etc.)
        log.Error("Failed to queue task", "err", err)
        break
    }
}

// Monitor health periodically
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        if !incrStore.IsHealthy() {
            log.Warn("Incremental store unhealthy")
            incrStore.LogStats() // Show detailed statistics

            // Take corrective action:
            // - Alert operators
            // - Pause processing
            // - Switch to backup storage
        }
    }
}()
```

**When to Use Each Approach:**
- **Fire-and-Forget**: High throughput scenarios, non-critical writes
- **Synchronous**: Critical operations where immediate error handling is essential
*/
