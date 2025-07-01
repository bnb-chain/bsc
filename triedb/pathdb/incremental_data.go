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

type incrStore struct {
	// Core components
	db        *Database // Reference to parent Database for accessing diskdb
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

	lastBlock atomic.Uint64
}

// NewIncrStore creates a new incremental store with async write capability
func NewIncrStore(db *Database, incrDB *rawdb.IncrDB) *incrStore {
	store := &incrStore{
		db:         db,
		incrDB:     incrDB,
		writeQueue: make(chan *diffLayer, 100),
		stopChan:   make(chan struct{}),
		started:    false,
		lastBlock:  atomic.Uint64{},
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
	in.lock.Lock()
	defer in.lock.Unlock()

	if !in.started {
		return
	}

	log.Info("Stopping incremental store", "pending_tasks", in.GetQueueLength())

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

	in.started = false
	in.LogStats()
}

// commit submits an async write task.
func (in *incrStore) commit(bottom *diffLayer) error {
	if !in.started {
		return errors.New("incremental store not started")
	}

	// Check if directory switch is needed before committing
	switched, err := in.incrDB.CheckAndInitiateSwitch(bottom.block, in)
	if err != nil {
		return fmt.Errorf("failed to switch directory: %v", err)
	}

	// If directory switched, check and fill empty blocks
	if switched {
		log.Info("Directory switch completed, checking for empty blocks", "blockNumber", bottom.block)
		if err = in.checkAndFillEmptyBlocks(bottom.block); err != nil {
			log.Error("Failed to fill empty blocks after directory switch", "block", bottom.block, "err", err)
			return err
		}
	}

	atomic.AddUint64(&in.stats.totalTasks, 1)
	atomic.AddInt32(&in.stats.queueLength, 1)

	select {
	case in.writeQueue <- bottom:
		return nil

	case <-in.stopChan:
		atomic.AddInt32(&in.stats.queueLength, -1)
		return errors.New("incremental store is stopping")

	default:
		atomic.AddInt32(&in.stats.queueLength, -1)
		queueLen := in.GetQueueLength()
		log.Warn("Task queue is full, checking if directory switch is in progress", "queueLength", queueLen,
			"block", bottom.block, "stateID", bottom.stateID(), "switching", in.incrDB.IsSwitching())

		if in.incrDB.IsSwitching() {
			log.Info("Queue full during directory switch - this is expected",
				"block", bottom.block, "queueLength", queueLen)
			return nil
		}

		log.Error("Task queue is full outside of directory switch", "queueLength", queueLen, "block", bottom.block)
		in.LogStats()
		return fmt.Errorf("task queue is full (length %d, block %d)", queueLen, bottom.block)
	}
}

// worker processes write tasks asynchronously
func (in *incrStore) worker() {
	defer in.wg.Done()

	for {
		select {
		case dl := <-in.writeQueue:
			atomic.AddInt32(&in.stats.queueLength, -1)

			startTime := time.Now()
			err := in.processWriteTask(dl)
			processingTime := time.Since(startTime)
			in.stats.UpdateProcessTime(processingTime)
			if err != nil {
				log.Error("Async write task failed", "block", dl.block, "stateID", dl.stateID(),
					"processingTime", processingTime, "err", err)
			}

			in.updateStats(err)

		case <-in.stopChan:
			log.Debug("Worker stopping")
			return
		}
	}
}

func (in *incrStore) processWriteTask(dl *diffLayer) error {
	// check and write block firstly
	blockHash := rawdb.ReadCanonicalHash(in.db.diskdb.BlockStore(), dl.block)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", dl.block)
	}
	h, _ := rawdb.ReadHeaderAndRaw(in.db.diskdb.BlockStore(), blockHash, dl.block)
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
	// Write chain data first
	if err := in.writeChainData(dl.block, dl.stateID()); err != nil {
		log.Error("Failed to write chain data", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}

	// Write state data
	if err := rawdb.ResetEmptyIncrStateTable(in.incrDB.GetStateFreezer(), dl.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "block", dl.block, "stateID", dl.stateID(), "err", err)
		return err
	}
	if err := in.writeStateData(dl); err != nil {
		log.Error("Failed to write state data", "block", dl.block, "stateID", dl.stateID(), "err", err)
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

	// Get freezer reference directly without waiting for switch completion
	// This avoids deadlock while still maintaining data consistency
	head, err := in.incrDB.GetChainFreezer().Ancients()
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
		log.Debug("Block number is greater than head",
			"freezerHead", head, "blockNumber", blockNumber, "gapSize", blockNumber-head)
	} else {
		if blockNumber < head {
			log.Crit("Block number should be greater than or equal to head",
				"blockNumber", blockNumber, "head", head)
		}
	}

	for i := startBlock; i <= blockNumber; i++ {
		// check if this block has state changes
		currentStateID := uint64(0)
		if i == blockNumber {
			currentStateID = stateID
		}

		if err = writeIncrBlockToFreezer(env, in.db.diskdb.BlockStore(), in.incrDB, i, currentStateID); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", currentStateID, "err", err)
			return err
		}
	}

	log.Debug("Incremental block data processing completed", "startBlock", startBlock, "endBlock", blockNumber,
		"totalProcessed", blockNumber-startBlock+1)
	return nil
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

// DrainQueue waits for all pending tasks to be processed
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

	log.Info("Incremental store statistics", "total_tasks", total, "completed", completed,
		"failed", failed, "pending", queueLen, "queue_capacity", queueCapacity, "queue_usage", fmt.Sprintf("%.1f%%", queueUsage),
		"success_rate", fmt.Sprintf("%.2f%%", successRate), "avg_process_time", time.Duration(avgProcessTime),
		"max_process_time", time.Duration(maxProcessTime), "switching", in.incrDB.IsSwitching(),
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
	return in.db.diskdb
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

// checkAndFillEmptyBlocks checks for gaps and fills empty blocks after directory switch
func (in *incrStore) checkAndFillEmptyBlocks(currentBlock uint64) error {
	lastBlock := in.incrDB.GetLastBlock()

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
			if err := in.fillSingleEmptyBlock(blockNum); err != nil {
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
func (in *incrStore) fillSingleEmptyBlock(blockNum uint64) error {
	// Get freezer environment
	env := in.GetFreezerEnv()
	if env == nil {
		return errors.New("freezer env not available")
	}

	// Get block store
	blockStore := in.db.diskdb.BlockStore()
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

	// Reset chain table for this empty block
	chainFreezer := in.incrDB.GetChainFreezer()
	if err := rawdb.ResetEmptyIncrChainTable(chainFreezer, blockNum, isCancun(env, h.Number, h.Time)); err != nil {
		return fmt.Errorf("failed to reset chain table: %v", err)
	}

	// Write the empty block (stateID = 0 for empty blocks)
	if err := writeIncrBlockToFreezer(env, blockStore, in.incrDB, blockNum, 0); err != nil {
		return fmt.Errorf("failed to write block to freezer: %v", err)
	}

	return nil
}
