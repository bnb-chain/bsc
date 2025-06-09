package rawdb

import (
	"encoding/binary"
	"errors"

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

// WriteIncrState writes the provided state data into the database.
// Compute the position of state history in freezer by minus one since the id of first state
// history starts from one(zero for initial state).
func WriteIncrState(db ethdb.AncientWriter, number, id uint64, meta, accountIndex, storageIndex, accounts, storages, trieNodes []byte) error {
	_, err := db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw(stateHistoryMeta, id-1, meta); err != nil {
			return err
		}
		if err := op.AppendRaw(stateHistoryAccountIndex, id-1, accountIndex); err != nil {
			return err
		}
		if err := op.AppendRaw(stateHistoryStorageIndex, id-1, storageIndex); err != nil {
			return err
		}
		if err := op.AppendRaw(stateHistoryAccountData, id-1, accounts); err != nil {
			return err
		}
		if err := op.AppendRaw(stateHistoryStorageData, id-1, storages); err != nil {
			return err
		}
		if err := op.AppendRaw(incrStateHistoryTrieNodesData, id-1, trieNodes); err != nil {
			return err
		}
		if err := op.AppendRaw(IncrBlockStateIDMappingTable, id-1, encodeBlockNumber(number)); err != nil {
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

// ReadIncrStateBlockNumber retrieves the block number corresponding to the specified
// state id. Compute the position of state history in freezer by minus one
// since the id of first state history starts from one(zero for initial state).
func ReadIncrStateBlockNumber(db ethdb.AncientReaderOp, id uint64) (uint64, error) {
	blob, err := db.Ancient(IncrBlockStateIDMappingTable, id-1)
	if err != nil {
		return 0, err
	}
	number := binary.BigEndian.Uint64(blob)
	return number, nil
}

// WriteBlockData writes the provided block data to the database.
func WriteBlockData(db ethdb.AncientWriter, number uint64, hash, header, body, receipts, td, sidecars []byte, isCancun bool) error {
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
		if isCancun {
			if err := op.AppendRaw(ChainFreezerBlobSidecarTable, number, sidecars); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// WriteIncrBlockData writes the provided block data to the database.
func WriteIncrBlockData(db ethdb.AncientWriter, number, stateID uint64, hash, header, body, receipts, td, sidecars []byte, isCancun bool) error {
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
		log.Error("Failed to read increment chain hash", "err", err)
		return nil, nil, nil, nil, nil, err
	}
	header, err := ReadIncrChainHeader(db, number)
	if err != nil {
		log.Error("Failed to read increment chain header", "err", err)
		return nil, nil, nil, nil, nil, err
	}
	body, err := ReadIncrChainBodies(db, number)
	if err != nil {
		log.Error("Failed to read increment chain bodies", "err", err)
		return nil, nil, nil, nil, nil, err
	}
	receipts, err := ReadIncrChainReceipts(db, number)
	if err != nil {
		log.Error("Failed to read increment chain receipts", "err", err)
		return nil, nil, nil, nil, nil, err
	}
	td, err := ReadIncrChainDifficulty(db, number)
	if err != nil {
		log.Error("Failed to read increment chain difficulty", "err", err)
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
		log.Error("Failed to read incremental block", "block", number, "err", err)
		return err
	}
	hash := common.BytesToHash(hashBytes)

	var h types.Header
	if err = rlp.DecodeBytes(header, &h); err != nil {
		log.Error("Failed to decode header", "block", number, "err", err)
	}
	isCancunActive := chainConfig.IsCancun(h.Number, h.Time)

	var sidecars rlp.RawValue
	if isCancunActive {
		sidecars, err = ReadIncrChainBlobSideCars(incrChainFreezer, number)
		if err != nil {
			log.Error("Failed to read increment chain blob side car", "block", number, "err", err)
			return err
		}
	}

	blockBatch := db.NewBatch()

	// write block data
	WriteTdRLP(blockBatch, hash, number, td)
	WriteBodyRLP(blockBatch, hash, number, body)
	WriteHeaderRLP(blockBatch, hash, number, header)
	WriteReceiptsRLP(blockBatch, hash, number, receipts)
	if isCancunActive {
		WriteBlobSidecarsRLP(blockBatch, hash, number, sidecars)
	}

	// update blockchain metadata
	WriteCanonicalHash(blockBatch, hash, number)
	WriteHeadBlockHash(blockBatch, hash)
	WriteHeadHeaderHash(blockBatch, hash)
	WriteHeaderNumber(blockBatch, hash, number)
	WriteHeadFastBlockHash(blockBatch, hash)
	WriteFinalizedBlockHash(blockBatch, hash)
	if err = blockBatch.Write(); err != nil {
		log.Error("Failed to update block metadata into disk", "err", err)
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

// ReadIncrChainReceipt retrieves the incremental receipts history from the database with the provided block number.
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

// WriteIncrFirstBlockNumber writes the first block number to the database
func WriteIncrFirstBlockNumber(db ethdb.KeyValueWriter, firstBlockNumber uint64) error {
	if err := db.Put(incrFirstBlockKey, encodeBlockNumber(firstBlockNumber)); err != nil {
		log.Crit("Failed to store the first block number", "err", err)
	}
	return nil
}

// ReadIncrFirstBlockNumber reads the first block number from the database
func ReadIncrFirstBlockNumber(db ethdb.KeyValueReader) uint64 {
	data, err := db.Get(incrFirstBlockKey)
	if err != nil {
		log.Crit("Failed to read the first block number", "err", err)
	}
	if len(data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

// WriteIncrFirstStateID writes the first state id to the database
func WriteIncrFirstStateID(db ethdb.KeyValueWriter, firstStateID uint64) error {
	if err := db.Put(incrFirstBlockKey, encodeBlockNumber(firstStateID)); err != nil {
		log.Crit("Failed to store the first state id", "err", err)
	}
	return nil
}

// ReadIncrFirstStateID reads the first state id from the database
func ReadIncrFirstStateID(db ethdb.KeyValueReader) uint64 {
	data, err := db.Get(incrFirstStateIDKey)
	if err != nil {
		log.Crit("Failed to read the first state id", "err", err)
	}
	if len(data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

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
	if isCancun {
		if err := db.ResetTable(ChainFreezerBlobSidecarTable, next, true); err != nil {
			return err
		}
	}
	return nil
}

func ResetEmptyIncrStateTable(db ethdb.AncientWriter, next uint64) error {
	if err := db.ResetTable(stateHistoryMeta, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(stateHistoryAccountIndex, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(stateHistoryStorageIndex, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(stateHistoryAccountData, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(stateHistoryStorageData, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(incrStateHistoryTrieNodesData, next-1, true); err != nil {
		return err
	}
	if err := db.ResetTable(IncrBlockStateIDMappingTable, next-1, true); err != nil {
		return err
	}
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
