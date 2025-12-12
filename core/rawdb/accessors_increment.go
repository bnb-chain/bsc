package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// ContractCode represents a contract code with associated metadata.
type ContractCode struct {
	Hash common.Hash // hash is the cryptographic hash of the contract code.
	Blob []byte      // blob is the binary representation of the contract code.
}

// IncrStateMetadata represents metadata for incremental state data.
type IncrStateMetadata struct {
	Root             common.Hash
	HasStates        bool
	NodeCount        uint64
	StateCount       uint64
	Layers           uint64
	StateIDArray     [2]uint64
	BlockNumberArray [2]uint64
}

// WriteIncrState writes the provided state data into the database.
// Compute the position of state history in freezer by minus one since the id of first state
// history starts from one(zero for initial state).
func WriteIncrTrieNodes(db ethdb.AncientWriter, id uint64, meta, trieNodes []byte) error {
	_, err := db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw(incrStateHistoryMeta, id-1, meta); err != nil {
			return err
		}
		if err := op.AppendRaw(incrStateHistoryTrieNodesData, id-1, trieNodes); err != nil {
			return err
		}
		if err := op.AppendRaw(incrStateHistoryStatesData, id-1, []byte{}); err != nil {
			return err
		}
		return nil
	})
	return err
}

// WriteIncrState writes the provided state data into the database.
// Compute the position of state history in freezer by minus one since the id of first state
// history starts from one(zero for initial state).
func WriteIncrState(db ethdb.AncientWriter, id uint64, meta, states []byte) error {
	_, err := db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw(incrStateHistoryMeta, id-1, meta); err != nil {
			return err
		}
		if err := op.AppendRaw(incrStateHistoryTrieNodesData, id-1, []byte{}); err != nil {
			return err
		}
		if err := op.AppendRaw(incrStateHistoryStatesData, id-1, states); err != nil {
			return err
		}
		return nil
	})
	return err
}

// ReadIncrStateTrieNodes retrieves the trie nodes corresponding to the specified
// state history. Compute the position of state history in freezer by minus one
// since the id of first state history starts from one(zero for initial state).
func ReadIncrStateTrieNodes(db ethdb.AncientReaderOp, id uint64) ([]byte, error) {
	blob, err := db.Ancient(incrStateHistoryTrieNodesData, id-1)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrStatesData retrieves the states corresponding to the specified
// state history. Compute the position of state history in freezer by minus one
// since the id of first state history starts from one(zero for initial state).
func ReadIncrStatesData(db ethdb.AncientReaderOp, id uint64) ([]byte, error) {
	blob, err := db.Ancient(incrStateHistoryStatesData, id-1)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// WriteIncrBlockData writes the provided block data to the database.
func WriteIncrBlockData(db ethdb.AncientWriter, number, stateID uint64, hash, header, body, receipts, td, sidecars []byte,
	isEmptyBlock, isCancun bool) error {
	_, err := db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw(ChainFreezerHashTable, number, hash); err != nil {
			return err
		}
		if err := op.AppendRaw(ChainFreezerHeaderTable, number, header); err != nil {
			return err
		}
		if err := op.AppendRaw(ChainFreezerBodiesTable, number, body); err != nil {
			return err
		}
		if err := op.AppendRaw(ChainFreezerReceiptTable, number, receipts); err != nil {
			return err
		}
		if err := op.AppendRaw(ChainFreezerDifficultyTable, number, td); err != nil {
			return err
		}
		if err := op.AppendRaw(IncrBlockStateIDMappingTable, number, encodeBlockNumber(stateID)); err != nil {
			return err
		}
		if err := op.AppendRaw(IncrEmptyBlockTable, number, boolToBytes(isEmptyBlock)); err != nil {
			return err
		}
		if isCancun {
			if err := op.AppendRaw(ChainFreezerBlobSidecarTable, number, sidecars); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// ReadIncrBlock read the block data with the provided block number.
func ReadIncrBlock(db ethdb.AncientReaderOp, number uint64) ([]byte, []byte, []byte, []byte, []byte, error) {
	hashBytes, err := ReadIncrChainHash(db, number)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	header, err := ReadIncrChainHeader(db, number)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	body, err := ReadIncrChainBodies(db, number)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	receipts, err := ReadIncrChainReceipts(db, number)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	td, err := ReadIncrChainDifficulty(db, number)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return hashBytes, header, body, receipts, td, nil
}

// FinalizeIncrementalMerge is ued to write last block data from incremental db into blockchain db.
// Blockchain metadata: head block, head hash, canonical hash, etc.
func FinalizeIncrementalMerge(db ethdb.Database, incrChainFreezer ethdb.AncientReaderOp, chainConfig *params.ChainConfig,
	number uint64) error {
	hashBytes, header, body, receipts, td, err := ReadIncrBlock(incrChainFreezer, number)
	if err != nil {
		log.Error("Failed to read incremental block", "block", number, "error", err)
		return err
	}
	hash := common.BytesToHash(hashBytes)

	var h types.Header
	if err = rlp.DecodeBytes(header, &h); err != nil {
		log.Error("Failed to decode header", "block", number, "error", err)
		return err
	}
	isCancunActive := chainConfig.IsCancun(h.Number, h.Time)

	var sidecars rlp.RawValue
	if isCancunActive {
		sidecars, err = ReadIncrChainBlobSideCars(incrChainFreezer, number)
		if err != nil {
			log.Error("Failed to read increment chain blob side car", "block", number, "error", err)
			return err
		}
	}

	blockBatch := db.NewBatch()

	// write block data
	WriteTdRLP(blockBatch, hash, number, td)
	WriteBodyRLP(blockBatch, hash, number, body)
	WriteHeaderRLP(blockBatch, hash, number, header)
	WriteRawReceipts(blockBatch, hash, number, receipts)
	if isCancunActive {
		WriteBlobSidecarsRLP(blockBatch, hash, number, sidecars)
	}

	// update blockchain metadata
	WriteCanonicalHash(blockBatch, hash, number)
	WriteHeadBlockHash(blockBatch, hash)
	WriteHeadHeaderHash(blockBatch, hash)
	WriteHeadFastBlockHash(blockBatch, hash)
	WriteFinalizedBlockHash(blockBatch, hash)
	if err = blockBatch.Write(); err != nil {
		log.Error("Failed to update block metadata into disk", "error", err)
		return err
	}

	return nil
}

// ReadIncrChainHash retrieves the incremental hash history from the database with the provided block number.
func ReadIncrChainHash(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blob, err := db.Ancient(ChainFreezerHashTable, number)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrChainHeader retrieves the incremental header history from the database with the provided block number.
func ReadIncrChainHeader(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blob, err := db.Ancient(ChainFreezerHeaderTable, number)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrChainBodies retrieves the incremental bodies history from the database with the provided block number.
func ReadIncrChainBodies(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blob, err := db.Ancient(ChainFreezerBodiesTable, number)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrChainReceipts retrieves the incremental receipts history from the database with the provided block number.
func ReadIncrChainReceipts(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blob, err := db.Ancient(ChainFreezerReceiptTable, number)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrChainDifficulty retrieves the incremental difficulty history from the database with the provided block number.
func ReadIncrChainDifficulty(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blob, err := db.Ancient(ChainFreezerDifficultyTable, number)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// ReadIncrChainBlobSideCars retrieves the incremental blob history from the database with the provided block number.
func ReadIncrChainBlobSideCars(db ethdb.AncientReaderOp, number uint64) ([]byte, error) {
	blobs, err := db.Ancient(ChainFreezerBlobSidecarTable, number)
	if err != nil {
		return nil, err
	}
	return blobs, nil
}

// ReadIncrChainMapping retrieves the state id from the incremental database with the provided block number.
func ReadIncrChainMapping(db ethdb.AncientReaderOp, number uint64) (uint64, error) {
	blob, err := db.Ancient(IncrBlockStateIDMappingTable, number)
	if err != nil {
		return 0, err
	}
	id := binary.BigEndian.Uint64(blob)
	return id, nil
}

// ReadIncrStateHistoryMeta retrieves the incremental metadata corresponding to the
// specified state history. Compute the position of state history in freezer by minus
// one since the id of first state history starts from one(zero for initial state).
func ReadIncrStateHistoryMeta(db ethdb.AncientReaderOp, id uint64) *IncrStateMetadata {
	blob, err := db.Ancient(incrStateHistoryMeta, id-1)
	if err != nil {
		return nil
	}
	m := new(IncrStateMetadata)
	if err = rlp.DecodeBytes(blob, m); err != nil {
		log.Error("Failed to decode incr state history", "error", err)
		return nil
	}
	return m
}

// ResetEmptyIncrChainTable resets the empty incremental chain table to the new start point.
func ResetEmptyIncrChainTable(db ethdb.AncientWriter, next uint64, isCancun bool) error {
	if err := db.ResetTable(ChainFreezerHeaderTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(ChainFreezerHashTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(ChainFreezerBodiesTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(ChainFreezerReceiptTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(ChainFreezerDifficultyTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(IncrBlockStateIDMappingTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTable(IncrEmptyBlockTable, next, true); err != nil {
		return err
	}
	if isCancun {
		if err := db.ResetTable(ChainFreezerBlobSidecarTable, next, true); err != nil {
			return err
		}
	}
	return nil
}

// ResetChainTable resets the chain table to the new start point.
// It's used in merging the incremental snapshot case.
func ResetChainTable(db ethdb.AncientWriter, next uint64, isCancun bool) error {
	if err := db.ResetTableForIncr(ChainFreezerHeaderTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(ChainFreezerHashTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(ChainFreezerBodiesTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(ChainFreezerReceiptTable, next, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(ChainFreezerDifficultyTable, next, true); err != nil {
		return err
	}
	if isCancun {
		if err := db.ResetTableForIncr(ChainFreezerBlobSidecarTable, next, true); err != nil {
			return err
		}
	}
	return nil
}

// ResetStateTableToNewStartPoint resets the entire state tables and sets a new start point for an empty state freezer.
func ResetStateTableToNewStartPoint(db ethdb.ResettableAncientStore, startPoint uint64) error {
	if err := db.Reset(); err != nil {
		log.Error("Failed to reset state freezer", "error", err)
		return err
	}

	if err := db.ResetTableForIncr(stateHistoryMeta, startPoint, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(stateHistoryAccountIndex, startPoint, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(stateHistoryStorageIndex, startPoint, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(stateHistoryAccountData, startPoint, true); err != nil {
		return err
	}
	if err := db.ResetTableForIncr(stateHistoryStorageData, startPoint, true); err != nil {
		return err
	}

	log.Info("Successfully set state freezer start point", "startPoint", startPoint)
	return nil
}

// GetChainConfig reads chain config from db.
func GetChainConfig(db ethdb.Reader) (*params.ChainConfig, error) {
	genesisHash := ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return nil, errors.New("genesis hash not found")
	}

	chainConfig := ReadChainConfig(db, genesisHash)
	if chainConfig == nil {
		return nil, errors.New("chain config not found")
	}

	return chainConfig, nil
}

// CheckIncrSnapshotComplete check the incr snapshot is complete for force kill or graceful kill
// True is graceful kill, false is force kill.
func CheckIncrSnapshotComplete(incrDir string) (bool, error) {
	cf, err := OpenIncrChainFreezer(incrDir, true)
	if err != nil {
		if strings.Contains(err.Error(), "garbage data bytes") {
			return false, nil
		}
		return false, err
	}
	defer cf.Close()
	sf, err := OpenIncrStateFreezer(incrDir, true)
	if err != nil {
		if strings.Contains(err.Error(), "garbage data bytes") {
			return false, nil
		}
		return false, err
	}
	defer sf.Close()

	chainAncients, err := cf.Ancients()
	if err != nil {
		return false, err
	}
	stateAncients, err := sf.Ancients()
	if err != nil {
		return false, err
	}
	if chainAncients == 0 || stateAncients == 0 {
		return false, nil
	}

	// Read last state metadata
	m := ReadIncrStateHistoryMeta(sf, stateAncients)
	if m == nil {
		return false, fmt.Errorf("last incr state history not found: %d", stateAncients)
	}

	if chainAncients-1 != m.BlockNumberArray[1] {
		return false, nil
	}
	return true, nil
}

func boolToBytes(b bool) []byte {
	buf := make([]byte, 1)
	if b {
		buf[0] = 1
	}
	return buf
}
