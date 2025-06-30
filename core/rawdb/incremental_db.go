package rawdb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
)

type IncrDB struct {
	currDB     *dbWrapper
	info       incrDBInfo
	baseDir    string
	currentDir string
	blockCount uint64
	lock       sync.RWMutex

	// Directory switching control
	switching   bool
	switchCond  *sync.Cond
	switchMutex sync.Mutex
}

type dbWrapper struct {
	chainFreezer ethdb.ResettableAncientStore
	stateFreezer ethdb.ResettableAncientStore
	kvDB         ethdb.KeyValueStore
}

type incrDBInfo struct {
	readonly     bool
	namespace    string
	offset       uint64
	maxTableSize uint32
	chainTables  map[string]bool
	stateTables  map[string]bool
	blockLimit   uint64 // write needs to set it; 0 is used in reading data from incr db
}

// IncrDirInfo holds information about an incremental directory
type IncrDirInfo struct {
	Name     string
	Path     string
	BlockNum uint64
}

// NewIncrDB creates a new incremental database
func NewIncrDB(baseDir string, readonly bool, offset uint64, blockLimit uint64) (*IncrDB, error) {
	info := incrDBInfo{
		readonly:     readonly,
		namespace:    "eth/db/incremental/",
		offset:       offset,
		maxTableSize: stateHistoryTableSize,
		chainTables:  incrChainFreezerNoSnappy,
		stateTables:  incrStateFreezerNoSnappy,
		blockLimit:   blockLimit,
	}

	incrBaseDir := filepath.Join(baseDir, IncrementalPath)
	// Find the latest directory or create the first one
	currentDir, err := findLatestIncrDir(incrBaseDir, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to find latest incremental directory: %v", err)
	}

	db, err := newDBWrapper(currentDir, &info)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial database wrapper: %v", err)
	}

	incrDB := &IncrDB{
		currDB:     db,
		info:       info,
		baseDir:    incrBaseDir,
		currentDir: currentDir,
		blockCount: 0,
		switching:  false,
	}
	// incrDB.blockCount.Store(0)
	incrDB.switchCond = sync.NewCond(&incrDB.switchMutex)

	log.Info("IncrDB created", "baseDir", baseDir, "currentDir", currentDir, "blockLimit", blockLimit)
	return incrDB, nil
}

func newDBWrapper(incrDir string, info *incrDBInfo) (*dbWrapper, error) {
	if incrDir == "" {
		return &dbWrapper{
			chainFreezer: NewMemoryFreezer(info.readonly, incrChainFreezerNoSnappy),
			stateFreezer: NewMemoryFreezer(info.readonly, incrStateFreezerNoSnappy),
			kvDB:         NewMemoryDatabase(),
		}, nil
	}

	chainPath := filepath.Join(incrDir, ChainFreezerName)
	chainNamespace := fmt.Sprintf("%s%s", info.namespace, "ChainFreezerName")
	cFreezer, err := newResettableFreezer(chainPath, chainNamespace, info.readonly, info.offset, info.maxTableSize,
		info.chainTables, true)
	if err != nil {
		log.Error("Failed to create incremental chain freezer", "err", err)
		return nil, err
	}

	item, _ := cFreezer.Ancients()
	a, _ := cFreezer.Tail()
	b, _ := cFreezer.ItemAmountInAncient()
	log.Info("Print incr chain ancient info", "item", item, "a", a, "b", b)

	statePath := filepath.Join(incrDir, MerkleStateFreezerName)
	stateNamespace := fmt.Sprintf("%s%s", info.namespace, "MerkleStateFreezerName")
	sFreezer, err := newResettableFreezer(statePath, stateNamespace, info.readonly, info.offset, info.maxTableSize,
		info.stateTables, true)
	if err != nil {
		log.Error("Failed to create incremental state freezer", "err", err)
		return nil, err
	}

	item, _ = sFreezer.Ancients()
	a, _ = sFreezer.Tail()
	b, _ = sFreezer.ItemAmountInAncient()
	log.Info("Print incr state ancient info", "item", item, "a", a, "b", b)

	kvNamespace := fmt.Sprintf("%s%s", info.namespace, "kv")
	db, err := pebble.New(incrDir, 10, 10, kvNamespace, info.readonly)
	if err != nil {
		log.Error("Failed to create incremental kv db", "err", err)
		return nil, err
	}

	return &dbWrapper{
		chainFreezer: cFreezer,
		stateFreezer: sFreezer,
		kvDB:         NewDatabase(db),
	}, nil
}

// waitForSwitchComplete waits until directory switching is complete
func (idb *IncrDB) waitForSwitchComplete() {
	idb.switchMutex.Lock()
	defer idb.switchMutex.Unlock()

	for idb.switching {
		log.Debug("Waiting for directory switch to complete")
		idb.switchCond.Wait()
	}
}

// WaitForSwitchComplete waits until directory switching is complete (public version)
func (idb *IncrDB) WaitForSwitchComplete() {
	idb.waitForSwitchComplete()
}

// WriteIncrBlockData writes incremental block data and checks if directory switch is needed
func (idb *IncrDB) WriteIncrBlockData(number, id uint64, hash, header, body, receipts, td, sidecars []byte, isCancun bool) error {
	// Wait for any ongoing directory switch to complete
	idb.waitForSwitchComplete()

	idb.lock.Lock()
	defer idb.lock.Unlock()

	if err := WriteIncrBlockData(idb.currDB.chainFreezer, number, id, hash, header, body, receipts, td, sidecars, isCancun); err != nil {
		log.Error("Failed to write incremental block data", "err", err)
		return err
	}
	idb.blockCount++
	log.Debug("Block written to IncrDB", "blockNum", number, "currentDir", idb.currentDir, "blockCount", idb.blockCount)

	return nil
}

// ReadIncrBlockData reads the related block data by the provided table name and block number
func (idb *IncrDB) ReadIncrBlockData(table string, number uint64) ([]byte, error) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currDB.chainFreezer.Ancient(table, number)
}

// WriteIncrState writes incremental state data
func (idb *IncrDB) WriteIncrState(id uint64, meta, accountIndex, storageIndex, accounts, storages, trieNodes []byte) error {
	// Wait for any ongoing directory switch to complete
	idb.waitForSwitchComplete()

	idb.lock.Lock()
	defer idb.lock.Unlock()

	return WriteIncrState(idb.currDB.stateFreezer, id, meta, accountIndex, storageIndex, accounts, storages, trieNodes)
}

func (idb *IncrDB) ReadStateData(table string, id uint64) ([]byte, error) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currDB.stateFreezer.Ancient(table, id-1)
}

// WriteIncrContractCodes writes contract codes
func (idb *IncrDB) WriteIncrContractCodes(codes map[common.Address]ContractCode) error {
	// Wait for any ongoing directory switch to complete
	idb.waitForSwitchComplete()

	idb.lock.Lock()
	defer idb.lock.Unlock()

	batch := idb.currDB.kvDB.NewBatch()
	for _, code := range codes {
		WriteCode(batch, code.Hash, code.Blob)
	}
	if err := batch.Write(); err != nil {
		return err
	}

	return nil
}

func (idb *IncrDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currDB.kvDB.NewIterator(prefix, start)
}

// switchToNewDirectoryBlocking performs directory switch and blocks all writes during the process
func (idb *IncrDB) switchToNewDirectoryBlocking(blockNum uint64) error {
	log.Info("Starting directory switch", "currentBlocks", idb.blockCount, "blockLimit", idb.info.blockLimit, "newStartBlock", blockNum)

	// Set switching flag to block new writes
	idb.switchMutex.Lock()
	idb.switching = true
	idb.switchMutex.Unlock()

	defer func() {
		// Clear switching flag and notify waiting writers
		idb.switchMutex.Lock()
		idb.switching = false
		idb.switchCond.Broadcast()
		idb.switchMutex.Unlock()
	}()

	// Close current databases safely
	if err := idb.closeCurrentDatabases(); err != nil {
		return fmt.Errorf("failed to close current databases: %v", err)
	}

	// Create new directory name based on block number
	newDir := filepath.Join(idb.baseDir, fmt.Sprintf("incr_%d", blockNum))

	// Create new database wrapper
	db, err := newDBWrapper(newDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create new database wrapper in directory %s: %v", newDir, err)
	}

	// Update current database and directory
	idb.currDB = db
	idb.currentDir = newDir
	// idb.blockCount.Store(0)
	idb.blockCount = 0

	log.Info("Successfully switched to new incremental directory", "newDir", newDir)
	return nil
}

// SwitchToNewDirectoryWithAsyncManager performs directory switch with async write manager coordination
func (idb *IncrDB) SwitchToNewDirectoryWithAsyncManager(blockNum uint64, asyncManager AsyncWriteManagerInterface) error {
	log.Info("Starting coordinated directory switch", "blockNum", blockNum)

	// Set switching flag to block new writes
	idb.switchMutex.Lock()
	idb.switching = true
	idb.switchMutex.Unlock()

	defer func() {
		// Clear switching flag and notify waiting writers
		idb.switchMutex.Lock()
		idb.switching = false
		idb.switchCond.Broadcast()
		idb.switchMutex.Unlock()
	}()

	// Wait for all pending async writes to complete
	log.Info("Waiting for async writes to complete", "pending", asyncManager.GetQueueLength())
	start := time.Now()
	asyncManager.DrainQueue()
	log.Info("Async writes completed", "duration", time.Since(start))

	idb.lock.Lock()
	defer idb.lock.Unlock()

	// Close current databases safely
	if err := idb.closeCurrentDatabases(); err != nil {
		return fmt.Errorf("failed to close current databases: %v", err)
	}

	// Create new directory name based on block number
	newDir := filepath.Join(idb.baseDir, fmt.Sprintf("incr_%d", blockNum))

	// Create new database wrapper
	db, err := newDBWrapper(newDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create new database wrapper in directory %s: %v", newDir, err)
	}

	// Update current database and directory
	idb.currDB = db
	idb.currentDir = newDir
	// idb.blockCount.Store(0)
	idb.blockCount = 0

	log.Info("Successfully completed coordinated directory switch", "newDir", newDir)
	return nil
}

// closeCurrentDatabases safely closes all current databases
func (idb *IncrDB) closeCurrentDatabases() error {
	if idb.currDB == nil {
		return nil
	}

	var errors []error

	// Close chain freezer
	if idb.currDB.chainFreezer != nil {
		if err := idb.currDB.chainFreezer.Close(); err != nil {
			log.Error("Failed to close chain freezer", "err", err)
			errors = append(errors, fmt.Errorf("chain freezer: %v", err))
		}
	}

	// Close state freezer
	if idb.currDB.stateFreezer != nil {
		if err := idb.currDB.stateFreezer.Close(); err != nil {
			log.Error("Failed to close state freezer", "err", err)
			errors = append(errors, fmt.Errorf("state freezer: %v", err))
		}
	}

	// Close KV database
	if idb.currDB.kvDB != nil {
		if err := idb.currDB.kvDB.Close(); err != nil {
			log.Error("Failed to close kv db", "err", err)
			errors = append(errors, fmt.Errorf("kv database: %v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple close errors: %v", errors)
	}

	log.Info("All databases closed successfully")
	return nil
}

// GetChainFreezer returns the current chain freezer
func (idb *IncrDB) GetChainFreezer() ethdb.ResettableAncientStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currDB != nil {
		return idb.currDB.chainFreezer
	}
	return nil
}

// GetStateFreezer returns the current state freezer
func (idb *IncrDB) GetStateFreezer() ethdb.ResettableAncientStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currDB != nil {
		return idb.currDB.stateFreezer
	}
	return nil
}

// GetKVDB returns the current KV database
func (idb *IncrDB) GetKVDB() ethdb.KeyValueStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currDB != nil {
		return idb.currDB.kvDB
	}
	return nil
}

// Close closes the IncrDB and all underlying databases
func (idb *IncrDB) Close() error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	log.Info("Closing IncrDB", "currentDir", idb.currentDir, "blockCount", idb.blockCount)
	return idb.closeCurrentDatabases()
}

// GetCurrentStats returns current statistics
func (idb *IncrDB) GetCurrentStats() (currentDir string, blockCount, blockLimit uint64) {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	return idb.currentDir, idb.blockCount, idb.info.blockLimit
}

// findLatestIncrDir finds the latest incremental directory or creates the first one
func findLatestIncrDir(baseDir string, offset uint64) (string, error) {
	// Ensure base directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create base directory %s: %v", baseDir, err)
	}

	// Scan for existing incr_* directories
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to read base directory %s: %v", baseDir, err)
	}

	// Regular expression to match incr_<number> pattern
	incrDirPattern := regexp.MustCompile(`^incr_(\d+)$`)
	var incrDirs []IncrDirInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := incrDirPattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}

		blockNum, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil {
			log.Warn("Invalid incremental directory name", "dir", entry.Name(), "err", err)
			continue
		}

		incrDirs = append(incrDirs, IncrDirInfo{
			Name:     entry.Name(),
			Path:     filepath.Join(baseDir, entry.Name()),
			BlockNum: blockNum,
		})
	}

	// If no existing directories found, create the first one
	if len(incrDirs) == 0 {
		firstDir := filepath.Join(baseDir, fmt.Sprintf("incr_%d", offset))
		log.Info("No existing incremental directories found, creating first one", "dir", firstDir)
		return firstDir, nil
	}

	// Sort by block number and return the latest one
	sort.Slice(incrDirs, func(i, j int) bool {
		return incrDirs[i].BlockNum < incrDirs[j].BlockNum
	})

	latestDir := incrDirs[len(incrDirs)-1]
	log.Info("Found latest incremental directory", "dir", latestDir.Path, "blockNum", latestDir.BlockNum)
	return latestDir.Path, nil
}

// GetAllIncrDirs returns all incremental directories sorted by block number
func GetAllIncrDirs(baseDir string) ([]IncrDirInfo, error) {
	dir := filepath.Join(baseDir, IncrementalPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read base directory %s: %v", dir, err)
	}

	incrDirPattern := regexp.MustCompile(`^incr_(\d+)$`)
	var incrDirs []IncrDirInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := incrDirPattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}

		blockNum, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil {
			continue
		}

		incrDirs = append(incrDirs, IncrDirInfo{
			Name:     entry.Name(),
			Path:     filepath.Join(dir, entry.Name()),
			BlockNum: blockNum,
		})
	}

	// Sort by block number
	sort.Slice(incrDirs, func(i, j int) bool {
		return incrDirs[i].BlockNum < incrDirs[j].BlockNum
	})

	return incrDirs, nil
}

// RecoverFromDirectory recovers the IncrDB to use a specific directory
func (idb *IncrDB) RecoverFromDirectory(targetDir string) error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	// Validate target directory exists
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("target directory does not exist: %s", targetDir)
	}

	// Close current databases
	if err := idb.closeCurrentDatabases(); err != nil {
		return fmt.Errorf("failed to close current databases: %v", err)
	}

	// Create new database wrapper for target directory
	db, err := newDBWrapper(targetDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create database wrapper for directory %s: %v", targetDir, err)
	}

	// Update current database and directory
	idb.currDB = db
	idb.currentDir = targetDir
	// idb.blockCount.Store(0)
	idb.blockCount = 0

	log.Info("Successfully recovered to directory", "dir", targetDir)
	return nil
}

// GetDirectoryBlockRange returns the block range for a specific directory
func (idb *IncrDB) GetDirectoryBlockRange(dirPath string) (startBlock uint64, endBlock uint64, err error) {
	// Extract start block from directory name
	dirName := filepath.Base(dirPath)
	incrDirPattern := regexp.MustCompile(`^incr_(\d+)$`)
	matches := incrDirPattern.FindStringSubmatch(dirName)

	if len(matches) != 2 {
		return 0, 0, fmt.Errorf("invalid directory name format: %s", dirName)
	}

	startBlock, err = strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse start block from directory name: %v", err)
	}

	// For end block, we would need to check the actual data in the directory
	// This is a placeholder - actual implementation would query the databases
	endBlock = startBlock + idb.info.blockLimit - 1

	return startBlock, endBlock, nil
}

// IsBlockLimitReached checks if the current directory has reached its block limit
func (idb *IncrDB) IsBlockLimitReached() bool {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.info.blockLimit == 0 {
		return false
	}

	// Use actual freezer count instead of internal blockCount to avoid race conditions
	if idb.currDB != nil && idb.currDB.chainFreezer != nil {
		ancients, err := idb.currDB.chainFreezer.Ancients()
		if err != nil {
			log.Crit("Failed to get ancients count for limit check", "err", err)
		}
		tail, err := idb.currDB.chainFreezer.Tail()
		if err != nil {
			log.Crit("Failed to get tail for limit check", "err", err)
		}
		return ancients-tail >= idb.info.blockLimit
	}
	return false
}

// IsSwitching returns true if directory switching is in progress
func (idb *IncrDB) IsSwitching() bool {
	idb.switchMutex.Lock()
	defer idb.switchMutex.Unlock()

	return idb.switching
}

// CheckAndInitiateSwitch safely checks if directory switch is needed and initiates it
// Returns true if switch was initiated, false if not needed or already in progress
func (idb *IncrDB) CheckAndInitiateSwitch(blockNum uint64, asyncManager AsyncWriteManagerInterface) (bool, error) {
	// First check without lock (fast path)
	if !idb.IsBlockLimitReached() || idb.IsSwitching() {
		return false, nil
	}

	// Double-checked locking to prevent race conditions
	idb.switchMutex.Lock()
	if idb.switching {
		// Another goroutine already initiated the switch
		idb.switchMutex.Unlock()
		return false, nil
	}

	// Check limit again with proper lock to ensure consistency
	idb.lock.RLock()
	limitReached := idb.info.blockLimit > 0
	if limitReached && idb.currDB != nil && idb.currDB.chainFreezer != nil {
		ancients, err := idb.currDB.chainFreezer.Ancients()
		if err != nil {
			log.Error("Failed to get ancients count for switch check", "err", err)
		}
		tail, err := idb.currDB.chainFreezer.Tail()
		if err != nil {
			log.Crit("Failed to get tail for limit check", "err", err)
		}
		limitReached = ancients-tail >= idb.info.blockLimit
	}
	idb.lock.RUnlock()

	if !limitReached {
		idb.switchMutex.Unlock()
		return false, nil
	}

	// We need to switch and we're the first to acquire the switch lock
	idb.switchMutex.Unlock()

	log.Info("Initiating directory switch", "blockNum", blockNum)
	err := idb.SwitchToNewDirectoryWithAsyncManager(blockNum, asyncManager)
	return true, err
}

// AsyncWriteManagerInterface defines the interface for async write manager
type AsyncWriteManagerInterface interface {
	DrainQueue()
	GetQueueLength() int
	GetStats() (total, completed, failed uint64, queueLen int)
	IsHealthy() bool
}
