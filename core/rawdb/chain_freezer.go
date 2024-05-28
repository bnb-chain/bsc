// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// freezerRecheckInterval is the frequency to check the key-value database for
	// chain progression that might permit new blocks to be frozen into immutable
	// storage.
	freezerRecheckInterval = time.Minute

	// freezerBatchLimit is the maximum number of blocks to freeze in one batch
	// before doing an fsync and deleting it from the key-value store.
	freezerBatchLimit = 30000
)

var (
	missFreezerEnvErr = errors.New("missing freezer env error")
)

// chainFreezer is a wrapper of freezer with additional chain freezing feature.
// The background thread will keep moving ancient chain segments from key-value
// database to flat files for saving space on live database.
type chainFreezer struct {
	threshold atomic.Uint64 // Number of recent blocks not to freeze (params.FullImmutabilityThreshold apart from tests)

	*Freezer
	quit    chan struct{}
	wg      sync.WaitGroup
	trigger chan chan struct{} // Manual blocking freeze trigger, test determinism

	freezeEnv atomic.Value

	multiDatabase bool
}

// newChainFreezer initializes the freezer for ancient chain data.
func newChainFreezer(datadir string, namespace string, readonly bool, offset uint64, multiDatabase bool) (*chainFreezer, error) {
	freezer, err := NewChainFreezer(datadir, namespace, readonly, offset)
	if err != nil {
		return nil, err
	}
	cf := chainFreezer{
		Freezer: freezer,
		quit:    make(chan struct{}),
		trigger: make(chan chan struct{}),
	}
	cf.threshold.Store(params.FullImmutabilityThreshold)
	return &cf, nil
}

// Close closes the chain freezer instance and terminates the background thread.
func (f *chainFreezer) Close() error {
	select {
	case <-f.quit:
	default:
		close(f.quit)
	}
	f.wg.Wait()
	return f.Freezer.Close()
}

// readHeadNumber returns the number of chain head block. 0 is returned if the
// block is unknown or not available yet.
func (f *chainFreezer) readHeadNumber(db ethdb.KeyValueReader) uint64 {
	hash := ReadHeadBlockHash(db)
	if hash == (common.Hash{}) {
		log.Error("Head block is not reachable")
		return 0
	}
	number := ReadHeaderNumber(db, hash)
	if number == nil {
		log.Error("Number of head block is missing")
		return 0
	}
	return *number
}

// readFinalizedNumber returns the number of finalized block. 0 is returned
// if the block is unknown or not available yet.
func (f *chainFreezer) readFinalizedNumber(db ethdb.KeyValueReader) uint64 {
	hash := ReadFinalizedBlockHash(db)
	if hash == (common.Hash{}) {
		return 0
	}
	number := ReadHeaderNumber(db, hash)
	if number == nil {
		log.Error("Number of finalized block is missing")
		return 0
	}
	return *number
}

// freezeThreshold returns the threshold for chain freezing. It's determined
// by formula: max(finality, HEAD-params.FullImmutabilityThreshold).
func (f *chainFreezer) freezeThreshold(db ethdb.KeyValueReader) (uint64, error) {
	var (
		head      = f.readHeadNumber(db)
		final     = f.readFinalizedNumber(db)
		headLimit uint64
	)
	if head > params.FullImmutabilityThreshold {
		headLimit = head - params.FullImmutabilityThreshold
	}
	if final == 0 && headLimit == 0 {
		return 0, errors.New("freezing threshold is not available")
	}
	if final > headLimit {
		return final, nil
	}
	return headLimit, nil
}

// freeze is a background thread that periodically checks the blockchain for any
// import progress and moves ancient data from the fast database into the freezer.
//
// This functionality is deliberately broken off from block importing to avoid
// incurring additional data shuffling delays on block propagation.
func (f *chainFreezer) freeze(db ethdb.KeyValueStore) {
	var (
		backoff   bool
		triggered chan struct{} // Used in tests
		nfdb      = &nofreezedb{KeyValueStore: db}
	)
	timer := time.NewTimer(freezerRecheckInterval)
	defer timer.Stop()

	for {
		select {
		case <-f.quit:
			log.Info("Freezer shutting down")
			return
		default:
		}
		if backoff {
			// If we were doing a manual trigger, notify it
			if triggered != nil {
				triggered <- struct{}{}
				triggered = nil
			}
			select {
			case <-timer.C:
				backoff = false
				timer.Reset(freezerRecheckInterval)
			case triggered = <-f.trigger:
				backoff = false
			case <-f.quit:
				return
			}
		}

		// check freezer env first, it must wait a while when the env is necessary
		err := f.checkFreezerEnv()
		if err == missFreezerEnvErr {
			log.Warn("Freezer need related env, may wait for a while", "err", err)
			backoff = true
			continue
		}
		if err != nil {
			log.Error("Freezer check FreezerEnv err", "err", err)
			backoff = true
			continue
		}

		var (
			frozen    uint64
			threshold uint64
			first     uint64 // the first block to freeze
			last      uint64 // the last block to freeze

			hash   common.Hash
			number *uint64
			head   *types.Header
		)

		// use finalized block as the chain freeze indicator was used for multiDatabase feature, if multiDatabase is false, keep 9W blocks in db
		if f.multiDatabase {
			threshold, err = f.freezeThreshold(nfdb)
			if err != nil {
				backoff = true
				log.Debug("Current full block not old enough to freeze", "err", err)
				continue
			}
			frozen = f.frozen.Load()

			// Short circuit if the blocks below threshold are already frozen.
			if frozen != 0 && frozen-1 >= threshold {
				backoff = true
				log.Debug("Ancient blocks frozen already", "threshold", threshold, "frozen", frozen)
				continue
			}

			hash = ReadHeadBlockHash(nfdb)
			if hash == (common.Hash{}) {
				log.Debug("Current full block hash unavailable") // new chain, empty database
				backoff = true
				continue
			}
			number = ReadHeaderNumber(nfdb, hash)
			if number == nil {
				log.Error("Current full block number unavailable", "hash", hash)
				backoff = true
				continue
			}
			head = ReadHeader(nfdb, hash, *number)
			if head == nil {
				log.Error("Current full block unavailable", "number", *number, "hash", hash)
				backoff = true
				continue
			}

			first = frozen
			last = threshold
			if last-first+1 > freezerBatchLimit {
				last = freezerBatchLimit + first - 1
			}
		} else {
			// Retrieve the freezing threshold.
			hash = ReadHeadBlockHash(nfdb)
			if hash == (common.Hash{}) {
				log.Debug("Current full block hash unavailable") // new chain, empty database
				backoff = true
				continue
			}
			number = ReadHeaderNumber(nfdb, hash)
			threshold = f.threshold.Load()
			frozen = f.frozen.Load()
			switch {
			case number == nil:
				log.Error("Current full block number unavailable", "hash", hash)
				backoff = true
				continue

			case *number < threshold:
				log.Debug("Current full block not old enough to freeze", "number", *number, "hash", hash, "delay", threshold)
				backoff = true
				continue

			case *number-threshold <= frozen:
				log.Debug("Ancient blocks frozen already", "number", *number, "hash", hash, "frozen", frozen)
				backoff = true
				continue
			}
			head = ReadHeader(nfdb, hash, *number)
			if head == nil {
				log.Error("Current full block unavailable", "number", *number, "hash", hash)
				backoff = true
				continue
			}
			first, _ = f.Ancients()
			last = *number - threshold
			if last-first > freezerBatchLimit {
				last = first + freezerBatchLimit
			}
		}
		// Seems we have data ready to be frozen, process in usable batches
		var (
			start = time.Now()
		)

		ancients, err := f.freezeRangeWithBlobs(nfdb, first, last)
		if err != nil {
			log.Error("Error in block freeze operation", "err", err)
			backoff = true
			continue
		}
		// Batch of blocks have been frozen, flush them before wiping from leveldb
		if err := f.Sync(); err != nil {
			log.Crit("Failed to flush frozen tables", "err", err)
		}
		// Wipe out all data from the active database
		batch := db.NewBatch()
		for i := 0; i < len(ancients); i++ {
			// Always keep the genesis block in active database
			if first+uint64(i) != 0 {
				DeleteBlockWithoutNumber(batch, ancients[i], first+uint64(i))
				DeleteCanonicalHash(batch, first+uint64(i))
			}
		}
		if err := batch.Write(); err != nil {
			log.Crit("Failed to delete frozen canonical blocks", "err", err)
		}
		batch.Reset()

		// Wipe out side chains also and track dangling side chains
		var dangling []common.Hash
		frozen = f.frozen.Load() // Needs reload after during freezeRange
		for number := first; number < frozen; number++ {
			// Always keep the genesis block in active database
			if number != 0 {
				dangling = ReadAllHashes(db, number)
				for _, hash := range dangling {
					log.Trace("Deleting side chain", "number", number, "hash", hash)
					DeleteBlock(batch, hash, number)
				}
			}
		}
		if err := batch.Write(); err != nil {
			log.Crit("Failed to delete frozen side blocks", "err", err)
		}
		batch.Reset()

		// Step into the future and delete any dangling side chains
		if frozen > 0 {
			tip := frozen
			for len(dangling) > 0 {
				drop := make(map[common.Hash]struct{})
				for _, hash := range dangling {
					log.Debug("Dangling parent from Freezer", "number", tip-1, "hash", hash)
					drop[hash] = struct{}{}
				}
				children := ReadAllHashes(db, tip)
				for i := 0; i < len(children); i++ {
					// Dig up the child and ensure it's dangling
					child := ReadHeader(nfdb, children[i], tip)
					if child == nil {
						log.Error("Missing dangling header", "number", tip, "hash", children[i])
						continue
					}
					if _, ok := drop[child.ParentHash]; !ok {
						children = append(children[:i], children[i+1:]...)
						i--
						continue
					}
					// Delete all block data associated with the child
					log.Debug("Deleting dangling block", "number", tip, "hash", children[i], "parent", child.ParentHash)
					DeleteBlock(batch, children[i], tip)
				}
				dangling = children
				tip++
			}
			if err := batch.Write(); err != nil {
				log.Crit("Failed to delete dangling side blocks", "err", err)
			}
		}

		// Log something friendly for the user
		context := []interface{}{
			"blocks", frozen - first, "elapsed", common.PrettyDuration(time.Since(start)), "number", frozen - 1,
		}
		if n := len(ancients); n > 0 {
			context = append(context, []interface{}{"hash", ancients[n-1]}...)
		}
		log.Debug("Deep froze chain segment", context...)

		env, _ := f.freezeEnv.Load().(*ethdb.FreezerEnv)
		// try prune blob data after cancun fork
		if isCancun(env, head.Number, head.Time) {
			f.tryPruneBlobAncientTable(env, *number)
		}

		// Avoid database thrashing with tiny writes
		if frozen-first < freezerBatchLimit {
			backoff = true
		}
	}
}

func (f *chainFreezer) tryPruneBlobAncientTable(env *ethdb.FreezerEnv, num uint64) {
	extraReserve := getBlobExtraReserveFromEnv(env)
	// It means that there is no need for pruning
	if extraReserve == 0 {
		return
	}
	reserveThreshold := params.MinBlocksForBlobRequests + extraReserve
	if num <= reserveThreshold {
		return
	}
	expectTail := num - reserveThreshold
	start := time.Now()
	if _, err := f.TruncateTableTail(ChainFreezerBlobSidecarTable, expectTail); err != nil {
		log.Error("Cannot prune blob ancient", "block", num, "expectTail", expectTail, "err", err)
		return
	}
	log.Debug("Chain freezer prune useless blobs, now ancient data is", "from", expectTail, "to", num, "cost", common.PrettyDuration(time.Since(start)))
}

func getBlobExtraReserveFromEnv(env *ethdb.FreezerEnv) uint64 {
	if env == nil {
		return params.DefaultExtraReserveForBlobRequests
	}
	return env.BlobExtraReserve
}

func (f *chainFreezer) freezeRangeWithBlobs(nfdb *nofreezedb, number, limit uint64) (hashes []common.Hash, err error) {
	defer func() {
		log.Debug("freezeRangeWithBlobs", "from", number, "to", limit, "err", err)
	}()
	lastHash := ReadCanonicalHash(nfdb, limit)
	if lastHash == (common.Hash{}) {
		return nil, fmt.Errorf("canonical hash missing, can't freeze block %d", limit)
	}
	last, _ := ReadHeaderAndRaw(nfdb, lastHash, limit)
	if last == nil {
		return nil, fmt.Errorf("block header missing, can't freeze block %d", limit)
	}
	env, _ := f.freezeEnv.Load().(*ethdb.FreezerEnv)
	if !isCancun(env, last.Number, last.Time) {
		return f.freezeRange(nfdb, number, limit)
	}

	var (
		cancunNumber uint64
		preHashes    []common.Hash
	)
	for i := number; i <= limit; i++ {
		hash := ReadCanonicalHash(nfdb, i)
		if hash == (common.Hash{}) {
			return nil, fmt.Errorf("canonical hash missing, can't freeze block %d", i)
		}
		h, header := ReadHeaderAndRaw(nfdb, hash, i)
		if len(header) == 0 {
			return nil, fmt.Errorf("block header missing, can't freeze block %d", i)
		}
		if isCancun(env, h.Number, h.Time) {
			cancunNumber = i
			break
		}
	}

	// freeze pre cancun
	preHashes, err = f.freezeRange(nfdb, number, cancunNumber-1)
	if err != nil {
		return preHashes, err
	}

	if err = ResetEmptyBlobAncientTable(f, cancunNumber); err != nil {
		return preHashes, err
	}
	// freeze post cancun
	postHashes, err := f.freezeRange(nfdb, cancunNumber, limit)
	hashes = append(preHashes, postHashes...)
	return hashes, err
}

// freezeRange moves a batch of chain segments from the fast database to the freezer.
// The parameters (number, limit) specify the relevant block range, both of which
// are included.
func (f *chainFreezer) freezeRange(nfdb *nofreezedb, number, limit uint64) (hashes []common.Hash, err error) {
	if number > limit {
		return nil, nil
	}

	env, _ := f.freezeEnv.Load().(*ethdb.FreezerEnv)
	hashes = make([]common.Hash, 0, limit-number+1)
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for ; number <= limit; number++ {
			// Retrieve all the components of the canonical block.
			hash := ReadCanonicalHash(nfdb, number)
			if hash == (common.Hash{}) {
				return fmt.Errorf("canonical hash missing, can't freeze block %d", number)
			}
			h, header := ReadHeaderAndRaw(nfdb, hash, number)
			if len(header) == 0 {
				return fmt.Errorf("block header missing, can't freeze block %d", number)
			}
			body := ReadBodyRLP(nfdb, hash, number)
			if len(body) == 0 {
				return fmt.Errorf("block body missing, can't freeze block %d", number)
			}
			receipts := ReadReceiptsRLP(nfdb, hash, number)
			if len(receipts) == 0 {
				return fmt.Errorf("block receipts missing, can't freeze block %d", number)
			}
			td := ReadTdRLP(nfdb, hash, number)
			if len(td) == 0 {
				return fmt.Errorf("total difficulty missing, can't freeze block %d", number)
			}
			// blobs is nil before cancun fork
			var sidecars rlp.RawValue
			if isCancun(env, h.Number, h.Time) {
				sidecars = ReadBlobSidecarsRLP(nfdb, hash, number)
				if len(sidecars) == 0 {
					return fmt.Errorf("block blobs missing, can't freeze block %d", number)
				}
			}

			// Write to the batch.
			if err := op.AppendRaw(ChainFreezerHashTable, number, hash[:]); err != nil {
				return fmt.Errorf("can't write hash to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerHeaderTable, number, header); err != nil {
				return fmt.Errorf("can't write header to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerBodiesTable, number, body); err != nil {
				return fmt.Errorf("can't write body to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerReceiptTable, number, receipts); err != nil {
				return fmt.Errorf("can't write receipts to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerDifficultyTable, number, td); err != nil {
				return fmt.Errorf("can't write td to Freezer: %v", err)
			}
			if isCancun(env, h.Number, h.Time) {
				if err := op.AppendRaw(ChainFreezerBlobSidecarTable, number, sidecars); err != nil {
					return fmt.Errorf("can't write blobs to Freezer: %v", err)
				}
			}

			hashes = append(hashes, hash)
		}
		return nil
	})

	return hashes, err
}

func (f *chainFreezer) SetupFreezerEnv(env *ethdb.FreezerEnv) error {
	f.freezeEnv.Store(env)
	return nil
}

func (f *chainFreezer) checkFreezerEnv() error {
	_, exist := f.freezeEnv.Load().(*ethdb.FreezerEnv)
	if exist {
		return nil
	}
	blobFrozen, err := f.TableAncients(ChainFreezerBlobSidecarTable)
	if err != nil {
		return err
	}
	if blobFrozen > 0 {
		return missFreezerEnvErr
	}
	return nil
}

func isCancun(env *ethdb.FreezerEnv, num *big.Int, time uint64) bool {
	if env == nil || env.ChainCfg == nil {
		return false
	}

	return env.ChainCfg.IsCancun(num, time)
}

func ResetEmptyBlobAncientTable(db ethdb.AncientWriter, next uint64) error {
	return db.ResetTable(ChainFreezerBlobSidecarTable, next, true)
}
