package pathdb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	taskQueueFullError            = errors.New("task queue is full")
	asyncWriteManagerStoppedError = errors.New("async write manager is stopping")
)

// IncrTaskType represents the task type of incremental data
type IncrTaskType int

const (
	IncrChainTask IncrTaskType = iota
	IncrStateTask
)

// AsyncWriteTask represents an asynchronous write task
type AsyncWriteTask struct {
	taskType IncrTaskType
	bottom   *diffLayer
}

// IncrStoreInterface defines the interface for incremental store operations
type IncrStoreInterface interface {
	GetIncrDB() *rawdb.IncrDB
	GetDiskDB() ethdb.Database
	GetFreezerEnv() *ethdb.FreezerEnv
}

// AsyncWriteManager manages asynchronous writes for incremental data
type AsyncWriteManager struct {
	taskQueue   chan *AsyncWriteTask
	incrStore   IncrStoreInterface
	workerCount int
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// Statistics
	totalTasks     uint64
	completedTasks uint64
	failedTasks    uint64
	statsLock      sync.RWMutex
}

// Ensure AsyncWriteManager implements AsyncWriteManagerInterface
var _ rawdb.AsyncWriteManagerInterface = (*AsyncWriteManager)(nil)

// NewAsyncWriteManager creates a new async write manager
func NewAsyncWriteManager(incrStore IncrStoreInterface, workerCount int) *AsyncWriteManager {
	return &AsyncWriteManager{
		taskQueue:   make(chan *AsyncWriteTask, 100),
		incrStore:   incrStore,
		workerCount: workerCount,
		stopChan:    make(chan struct{}),
	}
}

// Start starts the async write workers
func (awm *AsyncWriteManager) Start() {
	for i := 0; i < awm.workerCount; i++ {
		awm.wg.Add(1)
		go awm.worker(i)
	}
	log.Info("Async write manager started", "workers", awm.workerCount)
}

// Stop stops the async write manager and waits for all workers to finish
func (awm *AsyncWriteManager) Stop() {
	close(awm.stopChan)
	awm.wg.Wait()
	log.Info("Async write manager stopped")
}

// worker processes write tasks asynchronously
func (awm *AsyncWriteManager) worker(id int) {
	defer awm.wg.Done()

	for {
		select {
		case task := <-awm.taskQueue:
			err := awm.processTask(task)

			// Update statistics
			awm.statsLock.Lock()
			if err != nil {
				awm.failedTasks++
				log.Error("Async write task failed", "worker", id, "type", task.taskType, "err", err)
			} else {
				awm.completedTasks++
				log.Debug("Async write task completed", "worker", id, "type", task.taskType)
			}
			awm.statsLock.Unlock()

		case <-awm.stopChan:
			log.Debug("Worker stopping", "id", id)
			return
		}
	}
}

// processTask processes a single write task
func (awm *AsyncWriteManager) processTask(task *AsyncWriteTask) error {
	switch task.taskType {
	case IncrStateTask:
		if task.bottom == nil {
			return fmt.Errorf("missing bottom layer for state write task")
		}
		return awm.writeStateData(task.bottom)
	case IncrChainTask:
		return awm.writeChainData(task.bottom.block, task.bottom.stateID())
	default:
		return fmt.Errorf("unknown task type: %d", task.taskType)
	}
}

// writeStateData writes state data to incremental database
func (awm *AsyncWriteManager) writeStateData(dl *diffLayer) error {
	incrDB := awm.incrStore.GetIncrDB()
	if incrDB == nil {
		return errors.New("incremental database not available")
	}

	// Short circuit if state set is not available.
	if dl.states == nil {
		return errors.New("state change set is not available")
	}

	var (
		start   = time.Now()
		nodes   = compressTrieNodes(dl.nodes.nodes)
		history = newHistory(dl.rootHash(), dl.parentLayer().rootHash(), dl.block, dl.states.accountOrigin,
			dl.states.storageOrigin, dl.states.rawStorageKey)
	)
	accountData, storageData, accountIndex, storageIndex := history.encode()
	nodesBytes, err := rlp.EncodeToBytes(nodes)
	if err != nil {
		log.Crit("Failed to encode trie nodes", "error", err)
	}

	err = incrDB.WriteIncrState(dl.stateID(), history.meta.encode(), accountIndex, storageIndex, accountData, storageData, nodesBytes)
	if err != nil {
		return err
	}
	log.Debug("Stored incremental history", "id", dl.stateID(), "block", dl.block, "nodes size", dl.nodes.size,
		"elapsed", common.PrettyDuration(time.Since(start)))

	return nil
}

// writeChainData writes incremental chain data into db
func (awm *AsyncWriteManager) writeChainData(blockNumber, stateID uint64) error {
	incrDB := awm.incrStore.GetIncrDB()
	if incrDB == nil {
		return errors.New("incremental db is not available")
	}

	diskDB := awm.incrStore.GetDiskDB()
	if diskDB == nil {
		return errors.New("disk db is not available")
	}

	env := awm.incrStore.GetFreezerEnv()
	if env == nil {
		return errors.New("freezer env is not available")
	}

	// Check if directory switch is needed before writing
	if incrDB.IsBlockLimitReached() && !incrDB.IsSwitching() {
		log.Info("Block limit reached, initiating directory switch", "blockNumber", blockNumber)
		if err := incrDB.SwitchToNewDirectoryWithAsyncManager(blockNumber, awm); err != nil {
			return fmt.Errorf("failed to switch directory: %v", err)
		}
	}

	head, err := incrDB.GetChainFreezer().Ancients()
	if err != nil {
		log.Error("Failed to get ancients from incr chain freezer", "err", err)
		return err
	}

	var startBlock uint64
	// Determine the scenario and calculate startBlock
	if blockNumber == head {
		// Scenario 1: First time startup with incremental flag
		startBlock = blockNumber
		log.Debug("Block number is equal to head", "blockNumber", blockNumber)
	} else if blockNumber > head {
		// There's a gap between freezer head and current block
		// Need to fill all missing blocks (including empty ones)
		startBlock = head
		log.Debug("Block number is greater than head",
			"freezerHead", head, "blockNumber", blockNumber, "gapSize", blockNumber-head)
	} else {
		if blockNumber < head {
			log.Crit("Block number should be greater than or equal to head", "blockNumber", blockNumber, "head", head)
		}
	}

	for i := startBlock; i <= blockNumber; i++ {
		// Determine if this block has state changes
		currentStateID := uint64(0)
		if i == blockNumber {
			currentStateID = stateID
		}

		if err = writeIncrBlockToFreezer(env, diskDB.BlockStore(), incrDB, i, currentStateID); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", currentStateID, "err", err)
			return err
		}
	}

	log.Debug("Incremental block data processing completed",
		"startBlock", startBlock, "endBlock", blockNumber, "totalProcessed", blockNumber-startBlock+1)
	return nil
}

// SubmitIncrWriteTask submits an incremental write task
func (awm *AsyncWriteManager) SubmitIncrWriteTask(taskType IncrTaskType, bottom *diffLayer) error {
	if bottom == nil {
		return fmt.Errorf("bottom layer cannot be nil")
	}

	task := &AsyncWriteTask{
		taskType,
		bottom,
	}

	awm.statsLock.Lock()
	awm.totalTasks++
	awm.statsLock.Unlock()

	select {
	case awm.taskQueue <- task:
		log.Debug("Write task is submitted", "stateID", bottom.stateID(), "block", bottom.block,
			"taskType", taskType)
		return nil
	case <-awm.stopChan:
		return asyncWriteManagerStoppedError
	default:
		return taskQueueFullError
	}
}

// DrainQueue waits for all pending tasks to be processed
func (awm *AsyncWriteManager) DrainQueue() {
	for {
		queueLen := awm.GetQueueLength()
		if queueLen == 0 {
			break
		}
		log.Debug("Waiting for queue to drain", "remaining", queueLen)
		time.Sleep(100 * time.Millisecond)
	}
}

// GetQueueLength returns the current number of pending tasks
func (awm *AsyncWriteManager) GetQueueLength() int {
	return len(awm.taskQueue)
}

// GetStats returns current statistics
func (awm *AsyncWriteManager) GetStats() (total, completed, failed uint64, queueLen int) {
	awm.statsLock.RLock()
	defer awm.statsLock.RUnlock()
	return awm.totalTasks, awm.completedTasks, awm.failedTasks, len(awm.taskQueue)
}

// LogStats logs current statistics
func (awm *AsyncWriteManager) LogStats() {
	total, completed, failed, queueLen := awm.GetStats()
	successRate := float64(0)
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}

	log.Info("Async write manager stats",
		"total", total,
		"completed", completed,
		"failed", failed,
		"pending", queueLen,
		"success_rate", fmt.Sprintf("%.2f%%", successRate))
}

// IsHealthy returns true if the async write manager is functioning properly
func (awm *AsyncWriteManager) IsHealthy() bool {
	awm.statsLock.RLock()
	defer awm.statsLock.RUnlock()

	// Consider healthy if failure rate is less than 10%
	if awm.totalTasks == 0 {
		return true
	}

	failureRate := float64(awm.failedTasks) / float64(awm.totalTasks)
	return failureRate < 0.1
}

// WaitForDirectorySwitchComplete waits for any ongoing directory switch to complete
func (awm *AsyncWriteManager) WaitForDirectorySwitchComplete() {
	incrDB := awm.incrStore.GetIncrDB()
	if incrDB == nil {
		return
	}

	for incrDB.IsSwitching() {
		log.Debug("Waiting for directory switch to complete")
		time.Sleep(50 * time.Millisecond)
	}
}
