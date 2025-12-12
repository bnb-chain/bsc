package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
)

// MergeIncrSnapshot merges the incremental snapshot into local data.
func MergeIncrSnapshot(chainDB ethdb.Database, trieDB *triedb.Database, incrPath string) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	if err := updateGenesisMeta(chainDB); err != nil {
		return err
	}

	// merge incremental state data
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := trieDB.MergeIncrState(incrPath); err != nil {
			log.Error("Failed to merge incremental state data", "path", incrPath, "err", err)
			errChan <- fmt.Errorf("failed to merge incremental state data: %v", err)
		}
	}()

	// merge incremental block data
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := mergeIncrBlock(incrPath, chainDB); err != nil {
			log.Error("Failed to merge incremental block data", "path", incrPath, "err", err)
			errChan <- fmt.Errorf("failed to merge incremental block data: %v", err)
		}
	}()

	// merge contract codes
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := mergeIncrKV(incrPath, chainDB); err != nil {
			log.Error("Failed to merge incremental contract codes", "path", incrPath, "err", err)
			errChan <- fmt.Errorf("failed to merge incremental contract codes: %v", err)
		}
	}()

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var mergeErrors []error
	for err := range errChan {
		mergeErrors = append(mergeErrors, err)
	}
	if len(mergeErrors) > 0 {
		errs := errors.Join(mergeErrors...)
		log.Error("Parallel merge operations failed", "total_errors", len(mergeErrors), "path", incrPath)
		return errs
	}

	log.Info("All merge operations completed successfully", "path", incrPath)
	return nil
}

func mergeIncrBlock(incrDir string, chainDB ethdb.Database) error {
	incrChainFreezer, err := rawdb.OpenIncrChainFreezer(incrDir, true)
	if err != nil {
		log.Error("Failed to open incremental chain freezer", "err", err)
		return err
	}
	defer incrChainFreezer.Close()

	// Get chain config from database
	chainConfig, err := rawdb.GetChainConfig(chainDB)
	if err != nil {
		log.Error("Failed to get chain config", "err", err)
		return err
	}

	incrAncients, _ := incrChainFreezer.Ancients()
	tail, _ := incrChainFreezer.Tail()

	// delete old block data in pebble
	if err = chainDB.CleanBlock(chainDB, tail); err != nil {
		log.Error("Failed to force freeze to ancients", "err", err)
		return err
	}

	baseHead, _ := chainDB.Ancients()
	if tail == baseHead && baseHead <= incrAncients {
		for number := tail; number < incrAncients-1; number++ {
			hashBytes, header, body, receipts, td, err := rawdb.ReadIncrBlock(incrChainFreezer, number)
			if err != nil {
				log.Error("Failed to read incremental block", "block", number, "err", err)
				return err
			}

			var h types.Header
			if err = rlp.DecodeBytes(header, &h); err != nil {
				log.Error("Failed to decode header", "block", number, "err", err)
				return err
			}
			// Check if Cancun hardfork is active for this block
			isCancunActive := chainConfig.IsCancun(h.Number, h.Time)
			var sidecars rlp.RawValue
			if isCancunActive {
				sidecars, err = rawdb.ReadIncrChainBlobSideCars(incrChainFreezer, number)
				if err != nil {
					log.Error("Failed to read increment chain blob side car", "block", number, "err", err)
					return err
				}
			}

			blockBatch := chainDB.NewBatch()
			hash := common.BytesToHash(hashBytes)
			rawdb.WriteCanonicalHash(blockBatch, hash, number)
			rawdb.WriteTdRLP(blockBatch, hash, number, td)
			rawdb.WriteBodyRLP(blockBatch, hash, number, body)
			rawdb.WriteHeaderRLP(blockBatch, hash, number, header)
			rawdb.WriteRawReceipts(blockBatch, hash, number, receipts)
			if isCancunActive {
				rawdb.WriteBlobSidecarsRLP(blockBatch, hash, number, sidecars)
			}
			if err = blockBatch.Write(); err != nil {
				log.Error("Failed to batch commit block data", "err", err)
				return err
			}
		}
	} else {
		log.Crit("There are block data gap", "tail", tail, "baseHead", baseHead)
	}

	if err = rawdb.FinalizeIncrementalMerge(chainDB, incrChainFreezer, chainConfig, incrAncients-1); err != nil {
		log.Error("Failed to finalize incremental data merge", "err", err)
		return err
	}

	log.Info("Finished merging incremental block data", "merged_number", incrAncients-baseHead)
	return nil
}

// mergeIncrKV merges incr kv: contract codes, parlia snapshot, chain config and genesis state spec..
func mergeIncrKV(incrDir string, chainDB ethdb.Database) error {
	newDB, err := pebble.New(incrDir, 10, 10, "incremental", true)
	if err != nil {
		log.Error("Failed to open pebble to read incremental data", "err", err)
		return err
	}
	defer newDB.Close()

	it := newDB.NewIterator(rawdb.CodePrefix, nil)
	defer it.Release()

	codeCount := 0
	for it.Next() {
		key := it.Key()
		value := it.Value()

		isCode, hashBytes := rawdb.IsCodeKey(key)
		if !isCode {
			log.Warn("Invalid code key found", "key", fmt.Sprintf("%x", key))
			continue
		}

		codeHash := common.BytesToHash(hashBytes)
		if rawdb.HasCodeWithPrefix(chainDB, codeHash) {
			log.Debug("Code already exists, skipping", "hash", codeHash.Hex())
			continue
		}
		rawdb.WriteCode(chainDB, codeHash, value)
		codeCount++
	}

	if err = it.Error(); err != nil {
		log.Error("Iterator error while reading contract codes", "err", err)
		return err
	}

	// Merge Parlia snapshots from incremental snapshot to local data
	if err = mergeParliaSnapshots(chainDB, newDB); err != nil {
		log.Error("Failed to merge Parlia snapshots", "err", err)
		return err
	}

	if err = updateGenesisMeta(chainDB); err != nil {
		log.Error("Failed to merge genesis meta data", "err", err)
		return err
	}

	log.Info("Complete merging contract codes", "total", codeCount)
	return nil
}

// mergeParliaSnapshots merges Parlia consensus snapshots from incremental snapshot into local data
func mergeParliaSnapshots(chainDB ethdb.Database, incrKV *pebble.Database) error {
	log.Info("Starting Parlia snapshots import from incremental snapshot")
	iter := incrKV.NewIterator(rawdb.ParliaSnapshotPrefix, nil)
	defer iter.Release()

	count := 0
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		if err := chainDB.Put(key, value); err != nil {
			log.Error("Failed to store Parlia snapshot in main DB", "key", common.Bytes2Hex(key), "err", err)
			return err
		}
		count++
	}

	if iter.Error() != nil {
		log.Error("Failed to iterate Parlia snapshot", "error", iter.Error())
		return iter.Error()
	}

	log.Info("Completed Parlia snapshots merging", "total_snapshots", count)
	return nil
}

// updateGenesisMeta updates base snapshot chain config
func updateGenesisMeta(chainDB ethdb.Database) error {
	stored := rawdb.ReadCanonicalHash(chainDB, 0)
	if (stored == common.Hash{}) {
		return fmt.Errorf("invalid genesis hash in database: %x", stored)
	}

	builtInConf := params.GetBuiltInChainConfig(stored)
	rawdb.WriteChainConfig(chainDB, stored, builtInConf)
	return nil
}
