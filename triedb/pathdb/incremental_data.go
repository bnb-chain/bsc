package pathdb

import (
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type incrStore struct {
	diskDB    ethdb.Database
	incrDB    *rawdb.IncrDB
	freezeEnv atomic.Value

	// Async write manager for incremental data
	asyncWriteManager *AsyncWriteManager
}

// NewIncrStore creates a new incremental store with async write manager
func NewIncrStore(diskDB ethdb.Database, incrDB *rawdb.IncrDB, workerCount int) *incrStore {
	store := &incrStore{
		diskDB: diskDB,
		incrDB: incrDB,
	}
	store.asyncWriteManager = NewAsyncWriteManager(store, workerCount)

	return store
}

// GetIncrDB returns the IncrDB instance (implements IncrStoreInterface)
func (in *incrStore) GetIncrDB() *rawdb.IncrDB {
	return in.incrDB
}

// GetDiskDB returns the disk db.
func (in *incrStore) GetDiskDB() ethdb.Database {
	return in.diskDB
}

// GetFreezerEnv returns the freezer env which is used to check Cancun hardfork.
func (in *incrStore) GetFreezerEnv() *ethdb.FreezerEnv {
	env, _ := in.freezeEnv.Load().(*ethdb.FreezerEnv)
	return env
}

func (in *incrStore) commit(db ethdb.BlockStore, bottom *diffLayer) error {
	if err := in.checkFreezerEnv(); err != nil {
		return err
	}

	// Check if directory switch is in progress and wait for completion
	if in.incrDB.IsSwitching() {
		log.Info("Directory switch in progress, waiting for completion", "block", bottom.block)
		// Wait for switch to complete before proceeding
		for in.incrDB.IsSwitching() {
			time.Sleep(50 * time.Millisecond)
		}
		log.Info("Directory switch completed, resuming commit", "block", bottom.block)
	}

	// reset incremental state freezer table
	if err := rawdb.ResetEmptyIncrStateTable(in.incrDB.GetStateFreezer(), bottom.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "err", err)
		return err
	}

	// async write state data
	if err := in.asyncWriteManager.SubmitIncrWriteTask(IncrStateTask, bottom); err != nil {
		log.Error("Failed to submit async state write task", "err", err)
		// If async submission fails, try to wait for directory switch completion and retry
		if in.incrDB.IsSwitching() {
			log.Info("Async write failed during directory switch, waiting and retrying", "block", bottom.block)
			for in.incrDB.IsSwitching() {
				time.Sleep(50 * time.Millisecond)
			}
			// Retry once after directory switch
			if retryErr := in.asyncWriteManager.SubmitIncrWriteTask(IncrStateTask, bottom); retryErr != nil {
				log.Error("Failed to submit async state write task after retry", "err", retryErr)
				return retryErr
			}
		} else {
			return err
		}
	}

	blockHash := rawdb.ReadCanonicalHash(db.BlockStore(), bottom.block)
	if blockHash == (common.Hash{}) {
		return fmt.Errorf("canonical hash not found for block %d", bottom.block)
	}
	h, _ := rawdb.ReadHeaderAndRaw(db.BlockStore(), blockHash, bottom.block)
	if h == nil {
		return fmt.Errorf("block header missing, can't freeze block %d", bottom.block)
	}
	env, _ := in.freezeEnv.Load().(*ethdb.FreezerEnv)

	// reset incremental chain freezer table
	if err := rawdb.ResetEmptyIncrChainTable(in.incrDB.GetChainFreezer(), bottom.block, isCancun(env, h.Number, h.Time)); err != nil {
		log.Error("Failed to reset empty incr chain freezer", "err", err)
		return err
	}

	if err := in.asyncWriteManager.SubmitIncrWriteTask(IncrChainTask, bottom); err != nil {
		log.Error("Failed to submit async block write task", "err", err)
		// If async submission fails, try to wait for directory switch completion and retry
		if in.incrDB.IsSwitching() {
			log.Info("Async block write failed during directory switch, waiting and retrying", "block", bottom.block)
			for in.incrDB.IsSwitching() {
				time.Sleep(50 * time.Millisecond)
			}
			// Retry once after directory switch
			if retryErr := in.asyncWriteManager.SubmitIncrWriteTask(IncrChainTask, bottom); retryErr != nil {
				log.Error("Failed to submit async block write task after retry", "err", retryErr)
				return retryErr
			}
		} else {
			return err
		}
	}
	return nil
}

func (in *incrStore) checkFreezerEnv() error {
	_, exist := in.freezeEnv.Load().(*ethdb.FreezerEnv)
	if exist {
		return nil
	}
	return errors.New("missing freezer env error")
}

// readIncrData reads the incremental history and tre nodes
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

// readIncrHistory reads incremental trie nodes
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

// checkIncrDataEmpty is a generic function to check if incremental data is empty
// and initialize it with the first record if needed
// func (dl *diskLayer) checkIncrDataEmpty(dataType IncrDataType, value uint64) error {
// 	var (
// 		freezer    ethdb.ResettableAncientStore
// 		dataName   string
// 		logMessage string
// 		writeFunc  func(ethdb.KeyValueWriter, uint64) error
// 	)
//
// 	switch dataType {
// 	case IncrChainData:
// 		freezer = dl.db.incr.chainFreezer
// 		dataName = "incrChainFreezer"
// 		logMessage = "Put first block number in pebble"
// 		writeFunc = rawdb.WriteIncrFirstBlockNumber
// 	case IncrStateData:
// 		freezer = dl.db.incr.stateFreezer
// 		dataName = "incrStateFreezer"
// 		logMessage = "Put first state id in pebble"
// 		writeFunc = rawdb.WriteIncrFirstStateID
// 	default:
// 		return fmt.Errorf("unknown incremental data type: %d", dataType)
// 	}
//
// 	item, err := freezer.Ancients()
// 	if err != nil {
// 		log.Error(fmt.Sprintf("Failed to get %s ancients", dataName), "err", err)
// 		return err
// 	}
//
// 	if item == 0 {
// 		pebblePath := filepath.Join(dl.db.config.IncrHistoryPath, rawdb.IncrementalPath)
// 		db, err := pebble.New(pebblePath, 10, 10, "incremental", false)
// 		if err != nil {
// 			log.Error("Failed to create pebble to store incremental data", "path", pebblePath, "err", err)
// 			return err
// 		}
// 		defer db.Close()
//
// 		if err = writeFunc(db, value); err != nil {
// 			log.Error("Failed to write first record into db", "type", dataName, "value", value, "err", err)
// 			return err
// 		}
// 		log.Info(logMessage, "type", dataName, "value", value)
// 	}
//
// 	return nil
// }
//
// func (dl *diskLayer) checkIncrChainEmpty(block uint64) error {
// 	return dl.checkIncrDataEmpty(IncrChainData, block)
// }
//
// func (dl *diskLayer) checkIncrStateEmpty(stateID uint64) error {
// 	return dl.checkIncrDataEmpty(IncrStateData, stateID)
// }
