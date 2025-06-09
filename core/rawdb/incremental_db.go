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
	lastBlock  uint64
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
func NewIncrDB(baseDir string, readonly bool, startBlock, blockLimit uint64) (*IncrDB, error) {
	info := incrDBInfo{
		readonly:     readonly,
		namespace:    "eth/db/incremental/",
		maxTableSize: stateHistoryTableSize,
		chainTables:  incrChainFreezerNoSnappy,
		stateTables:  incrStateFreezerNoSnappy,
		blockLimit:   blockLimit,
	}

	// Find the latest directory or create the first one
	currentDir, err := findLatestIncrDir(baseDir, startBlock)
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
		baseDir:    baseDir,
		currentDir: currentDir,
		switching:  false,
	}
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
	cFreezer, err := newResettableFreezer(chainPath, chainNamespace, info.readonly, info.maxTableSize,
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
	sFreezer, err := newResettableFreezer(statePath, stateNamespace, info.readonly, info.maxTableSize,
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
	log.Debug("Block written to IncrDB", "blockNum", number, "currentDir", idb.currentDir)

	return nil
}

// ReadIncrBlockData reads the related block data by the provided table name and block number
func (idb *IncrDB) ReadIncrBlockData(table string, number uint64) ([]byte, error) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currDB.chainFreezer.Ancient(table, number)
}

// WriteIncrState writes incremental state data
func (idb *IncrDB) WriteIncrState(number, id uint64, meta, accountIndex, storageIndex, accounts, storages, trieNodes []byte) error {
	idb.waitForSwitchComplete()

	idb.lock.Lock()
	defer idb.lock.Unlock()

	return WriteIncrState(idb.currDB.stateFreezer, number, id, meta, accountIndex, storageIndex, accounts, storages, trieNodes)
}

func (idb *IncrDB) ReadStateData(table string, id uint64) ([]byte, error) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currDB.stateFreezer.Ancient(table, id-1)
}

// WriteIncrContractCodes writes contract codes
func (idb *IncrDB) WriteIncrContractCodes(codes map[common.Address]ContractCode) error {
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

	// Critical: Record the last block in old chain freezer before switching
	// This is needed to detect and handle empty blocks that might be skipped
	if idb.currDB != nil && idb.currDB.chainFreezer != nil {
		ancients, err := idb.currDB.chainFreezer.Ancients()
		if err != nil {
			log.Error("Failed to get ancients from old chain freezer", "err", err)
			return err
		}
		tail, err := idb.currDB.chainFreezer.Tail()
		if err != nil {
			log.Error("Failed to get tail from old chain freezer", "err", err)
			return err
		}

		idb.lastBlock = ancients - 1
		log.Info("Recorded old chain freezer state",
			"lastBlock", idb.lastBlock, "ancients", ancients, "tail", tail)
	}

	if err := idb.closeCurrentDatabases(); err != nil {
		return err
	}

	newDir := filepath.Join(idb.baseDir, fmt.Sprintf("incr_%d", blockNum))
	db, err := newDBWrapper(newDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create new database wrapper in directory %s: %v", newDir, err)
	}

	idb.currDB = db
	idb.currentDir = newDir
	log.Info("Successfully completed coordinated directory switch", "newDir", newDir,
		"oldLastBlock", idb.lastBlock, "newStartBlock", blockNum)

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

// GetLastBlock returns the last block number
func (idb *IncrDB) GetLastBlock() uint64 {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	return idb.lastBlock
}

// Close closes the IncrDB and all underlying databases
func (idb *IncrDB) Close() error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	log.Info("Closing IncrDB", "currentDir", idb.currentDir)
	return idb.closeCurrentDatabases()
}

// GetCurrentStats returns current statistics
func (idb *IncrDB) GetCurrentStats() (string, uint64) {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	return idb.currentDir, idb.info.blockLimit
}

// findLatestIncrDir finds the latest incremental directory or creates the first one
func findLatestIncrDir(baseDir string, startBlock uint64) (string, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create base directory %s: %v", baseDir, err)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to read base directory %s: %v", baseDir, err)
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
		firstDir := filepath.Join(baseDir, fmt.Sprintf("incr_%d", startBlock))
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
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read base directory %s: %v", baseDir, err)
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
			Path:     filepath.Join(baseDir, entry.Name()),
			BlockNum: blockNum,
		})
	}

	// Sort by block number
	sort.Slice(incrDirs, func(i, j int) bool {
		return incrDirs[i].BlockNum < incrDirs[j].BlockNum
	})

	return incrDirs, nil
}

// IsBlockLimitReached checks if the current directory has reached its block limit
func (idb *IncrDB) IsBlockLimitReached() bool {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.info.blockLimit == 0 {
		return false
	}

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
			log.Crit("Failed to get ancients count for switch check", "err", err)
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
