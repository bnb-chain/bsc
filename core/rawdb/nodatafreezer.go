package rawdb

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/prometheus/tsdb/fileutil"
)

// nodatafreezer is an empty freezer, only record 'frozen' , the next recycle block number form kvstore.
type nodatafreezer struct {
	db ethdb.KeyValueStore // Meta database
	// WARNING: The `frozen` field is accessed atomically. On 32 bit platforms, only
	// 64-bit aligned fields can be atomic. The struct is guaranteed to be so aligned,
	// so take advantage of that (https://golang.org/pkg/sync/atomic/#pkg-note-BUG).
	frozen    uint64 // Number of next frozen block
	threshold uint64 // Number of recent blocks not to freeze (params.FullImmutabilityThreshold apart from tests)

	instanceLock fileutil.Releaser // File-system lock to prevent double opens
	quit         chan struct{}
	closeOnce    sync.Once
}

// newNoDataFreezer creates a chain freezer that deletes data enough ‘old’.
func newNoDataFreezer(datadir string, db ethdb.KeyValueStore) (*nodatafreezer, error) {
	if info, err := os.Lstat(datadir); !os.IsNotExist(err) {
		if info.Mode()&os.ModeSymlink != 0 {
			log.Warn("Symbolic link ancient database is not supported", "path", datadir)
			return nil, errSymlinkDatadir
		}
	}

	lock, _, err := fileutil.Flock(filepath.Join(datadir, "../NODATA_ANCIENT_FLOCK"))
	if err != nil {
		return nil, err
	}

	freezer := &nodatafreezer{
		db:           db,
		threshold:    params.FullImmutabilityThreshold,
		instanceLock: lock,
		quit:         make(chan struct{}),
	}

	if err := freezer.repair(datadir); err != nil {
		return nil, err
	}

	log.Info("Opened ancientdb with nodata mode", "database", datadir, "frozen", freezer.frozen)
	return freezer, nil
}

// repair init frozen , compatible disk-ancientdb and pruner-block-tool.
func (f *nodatafreezer) repair(datadir string) error {
	// compatible prune-block-tool
	offset := ReadOffSetOfCurrentAncientFreezer(f.db)

	// compatible freezer
	min := uint64(math.MaxUint64)
	for name, disableSnappy := range FreezerNoSnappy {
		table, err := NewFreezerTable(datadir, name, disableSnappy)
		if err != nil {
			return err
		}
		items := atomic.LoadUint64(&table.items)
		if min > items {
			min = items
		}
		table.Close()
	}
	offset += min

	if frozen := ReadFrozenOfAncientFreezer(f.db); frozen > offset {
		offset = frozen
	}

	atomic.StoreUint64(&f.frozen, offset)
	if err := f.Sync(); err != nil {
		return nil
	}
	return nil
}

// Close terminates the chain nodatafreezer.
func (f *nodatafreezer) Close() error {
	var err error
	f.closeOnce.Do(func() {
		close(f.quit)
		f.Sync()
		err = f.instanceLock.Release()
	})
	return err
}

// HasAncient returns an indicator whether the specified ancient data exists, return nil.
func (f *nodatafreezer) HasAncient(kind string, number uint64) (bool, error) {
	return false, nil
}

// Ancient retrieves an ancient binary blob from nodatafreezer, return nil.
func (f *nodatafreezer) Ancient(kind string, number uint64) ([]byte, error) {
	if _, ok := FreezerNoSnappy[kind]; ok {
		if number >= atomic.LoadUint64(&f.frozen) {
			return nil, errOutOfBounds
		}
		return nil, nil
	}
	return nil, errUnknownTable
}

// Ancients returns the last of the frozen items.
func (f *nodatafreezer) Ancients() (uint64, error) {
	return atomic.LoadUint64(&f.frozen), nil
}

// ItemAmountInAncient returns the actual length of current ancientDB, return 0.
func (f *nodatafreezer) ItemAmountInAncient() (uint64, error) {
	return 0, nil
}

// AncientOffSet returns the offset of current ancientDB, offset == frozen.
func (f *nodatafreezer) AncientOffSet() uint64 {
	return atomic.LoadUint64(&f.frozen)
}

// AncientSize returns the ancient size of the specified category, return 0.
func (f *nodatafreezer) AncientSize(kind string) (uint64, error) {
	if _, ok := FreezerNoSnappy[kind]; ok {
		return 0, nil
	}
	return 0, errUnknownTable
}

// AppendAncient update frozen.
//
// Notably, this function is lock free but kind of thread-safe. All out-of-order
// injection will be rejected. But if two injections with same number happen at
// the same time, we can get into the trouble.
func (f *nodatafreezer) AppendAncient(number uint64, hash, header, body, receipts, td []byte) (err error) {
	if atomic.LoadUint64(&f.frozen) != number {
		return errOutOrderInsertion
	}
	atomic.AddUint64(&f.frozen, 1)
	return nil
}

// TruncateAncients discards any recent data above the provided threshold number, always success.
func (f *nodatafreezer) TruncateAncients(items uint64) error {
	if atomic.LoadUint64(&f.frozen) <= items {
		return nil
	}
	atomic.StoreUint64(&f.frozen, items)
	WriteFrozenOfAncientFreezer(f.db, atomic.LoadUint64(&f.frozen))
	return nil
}

// Sync flushes meta data tables to disk.
func (f *nodatafreezer) Sync() error {
	WriteFrozenOfAncientFreezer(f.db, atomic.LoadUint64(&f.frozen))
	return nil
}

// freeze is a background thread that periodically checks the blockchain for any
// import progress and moves ancient data from the fast database into the freezer.
//
// This functionality is deliberately broken off from block importing to avoid
// incurring additional data shuffling delays on block propagation.
func (f *nodatafreezer) freeze() {
	nfdb := &nofreezedb{KeyValueStore: f.db}

	var backoff bool
	for {
		select {
		case <-f.quit:
			log.Info("Freezer shutting down")
			return
		default:
		}
		if backoff {
			select {
			case <-time.NewTimer(freezerRecheckInterval).C:
			case <-f.quit:
				return
			}
		}
		// Retrieve the freezing threshold.
		hash := ReadHeadBlockHash(nfdb)
		if hash == (common.Hash{}) {
			log.Debug("Current full block hash unavailable") // new chain, empty database
			backoff = true
			continue
		}
		number := ReadHeaderNumber(nfdb, hash)
		threshold := atomic.LoadUint64(&f.threshold)

		switch {
		case number == nil:
			log.Error("Current full block number unavailable", "hash", hash)
			backoff = true
			continue

		case *number < threshold:
			log.Debug("Current full block not old enough", "number", *number, "hash", hash, "delay", threshold)
			backoff = true
			continue

		case *number-threshold <= f.frozen:
			log.Debug("Ancient blocks frozen already", "number", *number, "hash", hash, "frozen", f.frozen)
			backoff = true
			continue
		}
		head := ReadHeader(nfdb, hash, *number)
		if head == nil {
			log.Error("Current full block unavailable", "number", *number, "hash", hash)
			backoff = true
			continue
		}
		// Seems we have data ready to be frozen, process in usable batches
		limit := *number - threshold
		if limit-f.frozen > freezerBatchLimit {
			limit = f.frozen + freezerBatchLimit
		}
		var (
			start    = time.Now()
			first    = f.frozen
			ancients = make([]common.Hash, 0, limit-f.frozen)
		)
		for f.frozen <= limit {
			// Retrieves all the components of the canonical block
			hash := ReadCanonicalHash(nfdb, f.frozen)
			if hash == (common.Hash{}) {
				log.Error("Canonical hash missing, can't freeze", "number", f.frozen)
				break
			}
			log.Trace("Deep froze ancient block", "number", f.frozen, "hash", hash)
			// Inject all the components into the relevant data tables
			if err := f.AppendAncient(f.frozen, nil, nil, nil, nil, nil); err != nil {
				break
			}
			ancients = append(ancients, hash)
		}
		// Batch of blocks have been frozen, flush them before wiping from leveldb
		if err := f.Sync(); err != nil {
			log.Crit("Failed to flush frozen tables", "err", err)
		}
		// Batch of blocks have been frozen, flush them before wiping from leveldb
		backoff = f.frozen-first >= freezerBatchLimit
		gcKvStore(f.db, ancients, first, f.frozen, start)
	}
}
