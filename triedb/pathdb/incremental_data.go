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
	// These two freezers are used to store incremental block and state history, nil possible in tests
	incrDB    *rawdb.IncrDB
	freezeEnv atomic.Value

	// Async write manager for incremental data
	asyncWriteManager *AsyncWriteManager
}

func (in *incrStore) commit(db ethdb.BlockStore, bottom *diffLayer) error {
	if err := in.checkFreezerEnv(); err != nil {
		return err
	}

	// reset incremental state freezer table
	if err := rawdb.ResetEmptyIncrStateTable(in.incrDB.GetStateFreezer(), bottom.stateID()); err != nil {
		log.Error("Failed to reset empty incr state freezer", "err", err)
		return err
	}

	// async write state data
	if err := in.asyncWriteManager.SubmitStateWriteTask(bottom, false); err != nil {
		log.Error("Failed to submit async state write task", "err", err)
		return err
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

	if err := in.asyncWriteManager.SubmitBlockWriteTask(bottom.block, bottom.stateID(), false); err != nil {
		log.Error("Failed to submit async block write task", "err", err)
		return err
	}
	return nil
}

// writeIncrementalBlockData writes block data to incremental freezer
// This handles both empty blocks and blocks with state changes
// Scenarios:
// 1. First time startup with incremental flag from a snapshot base
// 2. Restart after graceful shutdown with incremental flag
// 3. Normal block processing during runtime
func (in *incrStore) writeIncrementalBlockData(db ethdb.BlockStore, blockNumber, stateID uint64) error {
	head, err := in.incrDB.GetChainFreezer().Ancients()
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

		env, _ := in.freezeEnv.Load().(*ethdb.FreezerEnv)
		if err = writeIncrBlockToFreezer(env, db.BlockStore(), in.incrDB.GetChainFreezer(), i, currentStateID); err != nil {
			log.Error("Failed to write block data to freezer", "block", i, "stateID", currentStateID, "err", err)
			return err
		}
	}

	log.Debug("Incremental block data processing completed",
		"startBlock", startBlock, "endBlock", blockNumber, "totalProcessed", blockNumber-startBlock+1)
	return nil
}

func (i *incrStore) checkFreezerEnv() error {
	_, exist := i.freezeEnv.Load().(*ethdb.FreezerEnv)
	if exist {
		return nil
	}
	return errors.New("missing freezer env error")
}

// This scheme can store incremental data, including state and block data.

// writeIncrHistory persists the incremental history
func writeIncrHistory(writer ethdb.AncientWriter, dl *diffLayer) error {
	// Short circuit if state set is not available.
	if dl.states == nil {
		return errors.New("state change set is not available")
	}

	var (
		start   = time.Now()
		nodes   = compressTrieNodes(dl.nodes.nodes)
		history = newHistory(dl.rootHash(), dl.parentLayer().rootHash(), dl.block, dl.states.accountOrigin, dl.states.storageOrigin, dl.states.rawStorageKey)
	)
	accountData, storageData, accountIndex, storageIndex := history.encode()
	nodesBytes, err := rlp.EncodeToBytes(nodes)
	if err != nil {
		log.Crit("Failed to encode trie nodes", "error", err)
	}

	err = rawdb.WriteIncrState(writer, dl.stateID(), history.meta.encode(), accountIndex, storageIndex,
		accountData, storageData, nodesBytes)
	if err != nil {
		return err
	}
	log.Debug("Stored incremental history", "id", dl.stateID(), "block", dl.block, "nodes size", dl.nodes.size,
		"elapsed", common.PrettyDuration(time.Since(start)))

	return nil
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
func writeIncrBlockToFreezer(env *ethdb.FreezerEnv, reader ethdb.Reader, writer ethdb.AncientWriter, blockNumber, stateID uint64) error {
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

	err := rawdb.WriteIncrBlockData(writer, blockNumber, stateID, blockHash[:], header, body, receipts, td, sidecars, isCancun(env, h.Number, h.Time))
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
