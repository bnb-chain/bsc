package rawdb

import (
	"encoding/binary"
	"errors"
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

// AsyncWriteManager defines the interface for async write manager
type AsyncWriteManager interface {
	// ForceFlushStateBuffer forces all buffered incr state data to be flushed before directory switch
	ForceFlushStateBuffer() error
}

const (
	incrDirNameRegexPattern = `^incr-(\d+)-(\d+)$`
	incrDirNamePattern      = "incr-%d-%d"
)

// FirstStateID is used to record the first state id in each incr snapshot
var FirstStateID = []byte("firstStateID")

type IncrSnapDB struct {
	currSnapDB *snapDBWrapper
	info       incrSnapDBInfo
	baseDir    string
	currentDir string
	lastBlock  uint64 // last updated block number
	blockCount uint64 // record the stored number of blocks in current snap db
	lock       sync.RWMutex

	// directory switching control
	switching   bool
	switchCond  *sync.Cond
	switchMutex sync.Mutex
}

type snapDBWrapper struct {
	chainFreezer ethdb.ResettableAncientStore
	stateFreezer ethdb.ResettableAncientStore
	kvDB         ethdb.KeyValueStore
}

type incrSnapDBInfo struct {
	readonly      bool
	namespace     string
	offset        uint64
	maxTableSize  uint32
	chainTables   map[string]freezerTableConfig
	stateTables   map[string]freezerTableConfig
	blockInterval uint64 // write needs to set it; 0 is used in reading data from incr db
}

// IncrDirInfo holds information about an incremental directory
type IncrDirInfo struct {
	Name          string
	Path          string
	StartBlockNum uint64
	EndBlockNum   uint64
}

// NewIncrSnapDB creates a new incremental snap database
func NewIncrSnapDB(baseDir string, readonly bool, startBlock, blockInterval uint64) (*IncrSnapDB, error) {
	info := incrSnapDBInfo{
		readonly:      readonly,
		namespace:     "eth/db/incremental/",
		maxTableSize:  stateHistoryTableSize,
		chainTables:   incrChainFreezerTableConfigs,
		stateTables:   incrStateFreezerTableConfigs,
		blockInterval: blockInterval,
	}

	// Find the latest directory or create the first one
	currentDir, err := findLatestIncrDir(baseDir, startBlock, blockInterval)
	if err != nil {
		return nil, err
	}

	db, err := newSnapDBWrapper(currentDir, &info)
	if err != nil {
		return nil, err
	}

	incrDB := &IncrSnapDB{
		currSnapDB: db,
		info:       info,
		baseDir:    baseDir,
		currentDir: currentDir,
		switching:  false,
	}
	incrDB.switchCond = sync.NewCond(&incrDB.switchMutex)

	log.Info("New incr snap db", "baseDir", baseDir, "currentDir", currentDir, "blockInterval", blockInterval,
		"startBlock", startBlock)
	return incrDB, nil
}

func newSnapDBWrapper(incrDir string, info *incrSnapDBInfo) (*snapDBWrapper, error) {
	if incrDir == "" {
		return &snapDBWrapper{
			chainFreezer: NewMemoryFreezer(info.readonly, incrChainFreezerTableConfigs),
			stateFreezer: NewMemoryFreezer(info.readonly, incrStateFreezerTableConfigs),
			kvDB:         NewMemoryDatabase(),
		}, nil
	}

	chainPath := filepath.Join(incrDir, ChainFreezerName)
	chainNamespace := fmt.Sprintf("%s%s", info.namespace, "ChainFreezer")
	cFreezer, err := newResettableFreezer(chainPath, chainNamespace, info.readonly, info.maxTableSize,
		info.chainTables, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create incr chain freezer: %w", err)
	}

	statePath := filepath.Join(incrDir, MerkleStateFreezerName)
	stateNamespace := fmt.Sprintf("%s%s", info.namespace, "MerkleStateFreezer")
	sFreezer, err := newResettableFreezer(statePath, stateNamespace, info.readonly, info.maxTableSize,
		info.stateTables, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create incr state freezer: %w", err)
	}

	kvNamespace := fmt.Sprintf("%s%s", info.namespace, "kv")
	db, err := pebble.New(incrDir, 10, 10, kvNamespace, info.readonly)
	if err != nil {
		return nil, fmt.Errorf("failed to create incr KV database: %w", err)
	}

	return &snapDBWrapper{
		chainFreezer: cFreezer,
		stateFreezer: sFreezer,
		kvDB:         NewDatabase(db),
	}, nil
}

// SetBlockCount sets the block count
func (idb *IncrSnapDB) SetBlockCount(blockCount uint64) {
	idb.blockCount = blockCount
}

// waitForSwitchComplete waits until directory switching is complete
func (idb *IncrSnapDB) waitForSwitchComplete() {
	const (
		pollInterval = 100 * time.Millisecond
		timeout      = 60 * time.Second
	)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		idb.switchMutex.Lock()
		if !idb.switching {
			idb.switchMutex.Unlock()
			return
		}
		idb.switchMutex.Unlock()

		<-ticker.C
		log.Debug("Waiting for directory switch to complete")
	}

	// Timeout occurred
	log.Error("Timeout waiting for directory switch to complete")
}

// WriteIncrBlockData writes incremental block data and checks if directory switch is needed
func (idb *IncrSnapDB) WriteIncrBlockData(number, id uint64, hash, header, body, receipts, td, sidecars []byte,
	isEmptyBlock, isCancun bool) error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	err := WriteIncrBlockData(idb.currSnapDB.chainFreezer, number, id, hash, header, body, receipts, td, sidecars, isEmptyBlock, isCancun)
	if err != nil {
		return fmt.Errorf("failed to write block %d: %w", number, err)
	}
	idb.blockCount++
	log.Debug("Block written to IncrDB", "blockNum", number, "currentDir", idb.currentDir, "blockCount", idb.blockCount)

	return nil
}

// WriteIncrState writes incremental state data
func (idb *IncrSnapDB) WriteIncrTrieNodes(id uint64, meta, trieNodes []byte) error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	if err := WriteIncrTrieNodes(idb.currSnapDB.stateFreezer, id, meta, trieNodes); err != nil {
		return fmt.Errorf("failed to write incr trie nodes: %w", err)
	}
	return nil
}

// WriteIncrState writes incremental state data
func (idb *IncrSnapDB) WriteIncrState(id uint64, meta, states []byte) error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	if err := WriteIncrState(idb.currSnapDB.stateFreezer, id, meta, states); err != nil {
		return fmt.Errorf("failed to write incr state: %w", err)
	}
	return nil
}

// WriteIncrContractCodes writes contract codes
func (idb *IncrSnapDB) WriteIncrContractCodes(codes map[common.Address]ContractCode) error {
	idb.waitForSwitchComplete()

	idb.lock.Lock()
	defer idb.lock.Unlock()

	batch := idb.currSnapDB.kvDB.NewBatch()
	for _, code := range codes {
		WriteCode(batch, code.Hash, code.Blob)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write incr contract codes: %w", err)
	}
	return nil
}

// WriteParliaSnapshot stores parlia snapshot into pebble.
func (idb *IncrSnapDB) WriteParliaSnapshot(hash common.Hash, blob []byte) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	if err := idb.currSnapDB.kvDB.Put(append(ParliaSnapshotPrefix, hash[:]...), blob); err != nil {
		log.Crit("Failed to write parlia snapshot", "error", err)
	}
}

func (idb *IncrSnapDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	return idb.currSnapDB.kvDB.NewIterator(prefix, start)
}

func (idb *IncrSnapDB) WriteFirstStateID(id uint64) {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, id)
	if err := idb.currSnapDB.kvDB.Put(FirstStateID, enc); err != nil {
		log.Crit("Failed to write first state ID", "id", id, "error", err)
	}
}

// switchToNewDirectoryWithAsyncManager performs directory switch with async write manager coordination
func (idb *IncrSnapDB) switchToNewDirectoryWithAsyncManager(blockNum uint64, asyncManager AsyncWriteManager) error {
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

	log.Info("Force flushing all incr state data before directory switch")
	if err := asyncManager.ForceFlushStateBuffer(); err != nil {
		return err
	}

	idb.lock.Lock()
	defer idb.lock.Unlock()

	// Record the last block in old chain freezer before switching
	// It's used to detect and handle empty blocks that might be skipped
	if idb.currSnapDB != nil && idb.currSnapDB.chainFreezer != nil {
		ancients, err := idb.currSnapDB.chainFreezer.Ancients()
		if err != nil {
			log.Error("Failed to get ancients from old chain freezer", "err", err)
			return err
		}
		tail, err := idb.currSnapDB.chainFreezer.Tail()
		if err != nil {
			log.Error("Failed to get tail from old chain freezer", "err", err)
			return err
		}

		// Record the last block that was actually written to the old chain freezer
		// ancients represents the count of blocks, so the last written block is ancients - 1
		idb.lastBlock = ancients - 1
		log.Info("Recorded old chain freezer state", "lastBlock", idb.lastBlock, "ancients", ancients,
			"tail", tail, "switchTriggerBlock", blockNum)
	}

	if err := idb.closeCurrentDatabases(); err != nil {
		return err
	}

	newDir := filepath.Join(idb.baseDir, fmt.Sprintf(incrDirNamePattern, blockNum, blockNum+idb.info.blockInterval-1))
	db, err := newSnapDBWrapper(newDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create new snap db wrapper in directory %s: %v", newDir, err)
	}

	idb.currSnapDB = db
	idb.currentDir = newDir
	idb.blockCount = 0
	log.Info("Successfully completed coordinated directory switch", "newDir", newDir,
		"oldLastBlock", idb.lastBlock, "startBlock", blockNum)

	return nil
}

// closeCurrentDatabases safely closes all current databases
func (idb *IncrSnapDB) closeCurrentDatabases() error {
	if idb.currSnapDB == nil {
		return nil
	}

	var errs []error

	// Close chain freezer
	if idb.currSnapDB.chainFreezer != nil {
		if err := idb.currSnapDB.chainFreezer.Close(); err != nil {
			log.Error("Failed to close chain freezer", "err", err)
			errs = append(errs, fmt.Errorf("chain freezer: %v", err))
		}
	}

	// Close state freezer
	if idb.currSnapDB.stateFreezer != nil {
		if err := idb.currSnapDB.stateFreezer.Close(); err != nil {
			log.Error("Failed to close state freezer", "err", err)
			errs = append(errs, fmt.Errorf("state freezer: %v", err))
		}
	}

	// Close KV database
	if idb.currSnapDB.kvDB != nil {
		if err := idb.currSnapDB.kvDB.Close(); err != nil {
			log.Error("Failed to close kv db", "err", err)
			errs = append(errs, fmt.Errorf("kv database: %v", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	log.Info("All databases closed successfully")
	return nil
}

// GetChainFreezer returns the current chain freezer
func (idb *IncrSnapDB) GetChainFreezer() ethdb.ResettableAncientStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currSnapDB != nil {
		return idb.currSnapDB.chainFreezer
	}
	return nil
}

// GetStateFreezer returns the current state freezer
func (idb *IncrSnapDB) GetStateFreezer() ethdb.ResettableAncientStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currSnapDB != nil {
		return idb.currSnapDB.stateFreezer
	}
	return nil
}

// GetKVDB returns the current kv db
func (idb *IncrSnapDB) GetKVDB() ethdb.KeyValueStore {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if idb.currSnapDB != nil {
		return idb.currSnapDB.kvDB
	}
	return nil
}

// GetLastBlock returns the last block number
func (idb *IncrSnapDB) GetLastBlock() uint64 {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	return idb.lastBlock
}

// Full returns the last block number
func (idb *IncrSnapDB) Full() bool {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	return idb.info.blockInterval > 0 && idb.blockCount >= idb.info.blockInterval
}

// Close closes the IncrDB and all underlying databases
func (idb *IncrSnapDB) Close() error {
	idb.lock.Lock()
	defer idb.lock.Unlock()

	log.Info("Closing IncrDB", "currentDir", idb.currentDir)
	return idb.closeCurrentDatabases()
}

// IsSwitching returns true if directory switching is in progress
func (idb *IncrSnapDB) IsSwitching() bool {
	idb.switchMutex.Lock()
	defer idb.switchMutex.Unlock()

	return idb.switching
}

// CheckAndInitiateSwitch safely checks if directory switch is needed and initiates it
// Returns true if switch was initiated, false if not needed or already in progress
func (idb *IncrSnapDB) CheckAndInitiateSwitch(blockNum uint64, asyncManager AsyncWriteManager) (bool, error) {
	// First check without lock (fast path)
	if idb.IsSwitching() {
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
	limitReached := idb.info.blockInterval > 0 && idb.blockCount >= idb.info.blockInterval
	idb.lock.RUnlock()

	if !limitReached {
		idb.switchMutex.Unlock()
		return false, nil
	}

	// We need to switch and we're the first to acquire the switch lock
	idb.switchMutex.Unlock()

	log.Info("Initiating directory switch", "blockNum", blockNum)
	err := idb.switchToNewDirectoryWithAsyncManager(blockNum, asyncManager)
	return true, err
}

// Reset all incremental directories.
func (idb *IncrSnapDB) ResetAllIncr(block uint64) error {
	idb.lock.RLock()
	defer idb.lock.RUnlock()

	if err := idb.reset(block); err != nil {
		return err
	}
	return nil
}

// reset the specified directory and create a new snap db.
func (idb *IncrSnapDB) reset(block uint64) error {
	if err := idb.closeCurrentDatabases(); err != nil {
		return err
	}
	if err := os.RemoveAll(idb.currentDir); err != nil {
		return err
	}
	if err := os.MkdirAll(idb.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory %s: %v", idb.baseDir, err)
	}

	newDir := filepath.Join(idb.baseDir, fmt.Sprintf(incrDirNamePattern, block, block+idb.info.blockInterval-1))
	db, err := newSnapDBWrapper(newDir, &idb.info)
	if err != nil {
		return fmt.Errorf("failed to create new snap db wrapper in directory %s: %v", newDir, err)
	}

	idb.currSnapDB = db
	idb.currentDir = newDir
	idb.blockCount = 0
	return nil
}

func (idb *IncrSnapDB) ParseCurrDirBlockNumber() (uint64, uint64, error) {
	return parseDirBlockNumber(idb.currentDir)
}

// parseDirBlockNumber parses the start and end block number from directory path
func parseDirBlockNumber(dirPath string) (uint64, uint64, error) {
	path := filepath.Base(dirPath)
	pattern := regexp.MustCompile(incrDirNameRegexPattern)
	matches := pattern.FindStringSubmatch(path)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("invalid directory name format: %s", path)
	}

	startBlock, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse start block from directory name %s: %v", path, err)
	}
	endBlock, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse end block from directory name %s: %v", path, err)
	}

	return startBlock, endBlock, nil
}

// findLatestIncrDir finds the latest incremental directory or creates the first one
func findLatestIncrDir(baseDir string, startBlock, blockLimit uint64) (string, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create base directory %s: %v", baseDir, err)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to read base directory %s: %v", baseDir, err)
	}

	var incrDirs []IncrDirInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		start, end, err := parseDirBlockNumber(entry.Name())
		if err != nil {
			log.Warn("Invalid incremental directory name", "dir", entry.Name(), "err", err)
			continue
		}
		incrDirs = append(incrDirs, IncrDirInfo{
			Name:          entry.Name(),
			Path:          filepath.Join(baseDir, entry.Name()),
			StartBlockNum: start,
			EndBlockNum:   end,
		})
	}

	// If no existing directories found, create the first one
	if len(incrDirs) == 0 {
		firstDir := filepath.Join(baseDir, fmt.Sprintf(incrDirNamePattern, startBlock, startBlock+blockLimit-1))
		log.Info("No existing incremental directories found, creating first one", "dir", firstDir)
		return firstDir, nil
	}

	// Sort by block number and return the latest one
	sort.Slice(incrDirs, func(i, j int) bool {
		return incrDirs[i].StartBlockNum < incrDirs[j].StartBlockNum
	})

	latestDir := incrDirs[len(incrDirs)-1]
	log.Info("Found latest incremental directory", "dir", latestDir.Path, "startBlockNum", latestDir.StartBlockNum,
		"endBlockNum", latestDir.EndBlockNum)
	return latestDir.Path, nil
}

// GetAllIncrDirs returns all incremental directories sorted by block number
func GetAllIncrDirs(baseDir string) ([]IncrDirInfo, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read base directory %s: %v", baseDir, err)
	}

	var incrDirs []IncrDirInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		start, end, err := parseDirBlockNumber(entry.Name())
		if err != nil {
			log.Warn("Invalid incremental directory name", "dir", entry.Name(), "err", err)
			continue
		}
		incrDirs = append(incrDirs, IncrDirInfo{
			Name:          entry.Name(),
			Path:          filepath.Join(baseDir, entry.Name()),
			StartBlockNum: start,
			EndBlockNum:   end,
		})
	}

	// Sort by block number
	sort.Slice(incrDirs, func(i, j int) bool {
		return incrDirs[i].StartBlockNum < incrDirs[j].StartBlockNum
	})
	return incrDirs, nil
}
