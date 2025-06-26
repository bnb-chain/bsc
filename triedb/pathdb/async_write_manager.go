package pathdb

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// IncrDataType represents the type of incremental data
type IncrDataType int

const (
	IncrChainData IncrDataType = iota
	IncrStateData
)

// AsyncWriteTask represents an asynchronous write task
type AsyncWriteTask struct {
	taskType   IncrDataType
	bottom     *diffLayer
	blockNum   uint64
	stateID    uint64
	resultChan chan error
}

// AsyncWriteManager manages asynchronous writes for incremental data
type AsyncWriteManager struct {
	taskQueue   chan *AsyncWriteTask
	db          *Database
	workerCount int
	stopChan    chan struct{}
	doneChan    chan struct{}

	// Statistics
	totalTasks     uint64
	completedTasks uint64
	failedTasks    uint64
	statsLock      sync.RWMutex
}

// NewAsyncWriteManager creates a new async write manager
func NewAsyncWriteManager(db *Database, workerCount int) *AsyncWriteManager {
	return &AsyncWriteManager{
		taskQueue:   make(chan *AsyncWriteTask, 100), // Buffer for 100 tasks
		db:          db,
		workerCount: workerCount,
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}
}

// Start starts the async write workers
func (awm *AsyncWriteManager) Start() {
	for i := 0; i < awm.workerCount; i++ {
		go awm.worker(i)
	}
	log.Info("Async write manager started", "workers", awm.workerCount)
}

// Stop stops the async write manager
func (awm *AsyncWriteManager) Stop() {
	close(awm.stopChan)
	<-awm.doneChan
	log.Info("Async write manager stopped")
}

// worker processes write tasks asynchronously
func (awm *AsyncWriteManager) worker(id int) {
	defer func() {
		if id == 0 { // Only the first worker signals completion
			close(awm.doneChan)
		}
	}()

	for {
		select {
		case task := <-awm.taskQueue:
			err := awm.processTask(task)

			// Update statistics
			awm.statsLock.Lock()
			if err != nil {
				awm.failedTasks++
				log.Error("Async write task failed", "type", task.taskType, "err", err)
			} else {
				awm.completedTasks++
			}
			awm.statsLock.Unlock()

			if task.resultChan != nil {
				task.resultChan <- err
			}
		case <-awm.stopChan:
			return
		}
	}
}

// processTask processes a single write task
func (awm *AsyncWriteManager) processTask(task *AsyncWriteTask) error {
	// Handle special sync task for WaitForCompletion
	if task.resultChan != nil && task.bottom == nil && task.blockNum == 0 && task.stateID == 0 {
		return nil // Just return to signal completion
	}

	switch task.taskType {
	case IncrStateData:
		if task.bottom == nil {
			return fmt.Errorf("missing bottom layer for state write task")
		}
		return writeIncrHistory(awm.db.incr.stateFreezer, task.bottom)
	case IncrChainData:
		env, _ := awm.db.incr.freezeEnv.Load().(*ethdb.FreezerEnv)
		return writeIncrBlockToFreezer(env, awm.db.diskdb.BlockStore(), awm.db.incr.chainFreezer, task.blockNum, task.stateID)
	default:
		return fmt.Errorf("unknown task type: %d", task.taskType)
	}
}

// SubmitStateWriteTask submits a state write task
func (awm *AsyncWriteManager) SubmitStateWriteTask(bottom *diffLayer, sync bool) error {
	task := &AsyncWriteTask{
		taskType: IncrStateData,
		bottom:   bottom,
	}

	if sync {
		task.resultChan = make(chan error, 1)
	}

	awm.statsLock.Lock()
	awm.totalTasks++
	awm.statsLock.Unlock()

	select {
	case awm.taskQueue <- task:
		if sync {
			return <-task.resultChan
		}
		return nil
	default:
		return fmt.Errorf("task queue is full")
	}
}

// SubmitBlockWriteTask submits a block write task
func (awm *AsyncWriteManager) SubmitBlockWriteTask(blockNum, stateID uint64, sync bool) error {
	task := &AsyncWriteTask{
		taskType: IncrChainData,
		blockNum: blockNum,
		stateID:  stateID,
	}

	if sync {
		task.resultChan = make(chan error, 1)
	}

	awm.statsLock.Lock()
	awm.totalTasks++
	awm.statsLock.Unlock()

	select {
	case awm.taskQueue <- task:
		if sync {
			return <-task.resultChan
		}
		return nil
	default:
		return fmt.Errorf("task queue is full")
	}
}

// WaitForCompletion waits for all pending tasks to complete
func (awm *AsyncWriteManager) WaitForCompletion() {
	// Send a sync task to ensure all previous tasks are completed
	task := &AsyncWriteTask{
		taskType:   IncrStateData, // Use any type, it won't be processed
		resultChan: make(chan error, 1),
	}

	select {
	case awm.taskQueue <- task:
		<-task.resultChan // Wait for completion
	default:
		// Queue is full, tasks are being processed
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
	log.Info("Async write manager stats", "total", total, "completed", completed, "failed", failed,
		"pending", queueLen, "success_rate", fmt.Sprintf("%.2f%%", float64(completed)/float64(total)*100))
}
