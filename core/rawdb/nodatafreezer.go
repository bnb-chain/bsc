package rawdb

import (
	"os"
	"math"
	"sync"
	"time"
	"sync/atomic"
	"path/filepath"
	
	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/tsdb/fileutil"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

type nodatafreezer struct {
	db 			ethdb.KeyValueStore 	// Meta database
	frozen    	uint64 					// Number of next frozen block
	threshold 	uint64					// Number of recent blocks not to freeze (params.FullImmutabilityThreshold apart from tests)

	instanceLock 	fileutil.Releaser        // File-system lock to prevent double opens
	quit 			chan struct{}
	closeOnce 		sync.Once
}

func newNoDataFreezer(datadir string, db ethdb.KeyValueStore) (*nodatafreezer, error) {
	if info, err := os.Lstat(datadir); !os.IsNotExist(err) {
		if info.Mode()&os.ModeSymlink != 0 {
			log.Warn("Symbolic link ancient database is not supported", "path", datadir)
			return nil, errSymlinkDatadir
		}
	}

	lock, _, err := fileutil.Flock(filepath.Join(datadir, "NODATA_ANCIENT_FLOCK"))
	if err != nil {
		return nil, err
	} 
	
	freezer := &nodatafreezer{
		db 			: db,
		threshold 	: params.FullImmutabilityThreshold,
		instanceLock: lock,
		quit 		: make(chan struct{}),
	}

	if err := freezer.repair(datadir); err != nil {
		return nil, err
	}
	
	log.Info("Opened ancientdb with nodata mode", "database", datadir, "frozen", freezer.frozen)
	return freezer, nil
}


func (f *nodatafreezer) repair(datadir string) error {
	if frozen := ReadFrozenOfAncientFreezer(f.db); frozen != 0 {
		atomic.StoreUint64(&f.frozen, frozen)
		return nil
	}

	
	currentOffset := ReadOffSetOfCurrentAncientFreezer(f.db)
	lastOffset := ReadOffSetOfLastAncientFreezer(f.db)

	var offset uint64
	if currentOffset > lastOffset {
		atomic.StoreUint64(&f.frozen, currentOffset)
		offset = currentOffset
	} else {
		offset = lastOffset
	}
	
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

	atomic.StoreUint64(&f.frozen, offset)
	if err := f.Sync(); err != nil {
		return nil
	}
	return nil
}

func (f *nodatafreezer) Close() error {
	var err error
	f.closeOnce.Do(func() {
		close(f.quit)
		f.Sync()
		err = f.instanceLock.Release()
	})
	return err
}

func (f *nodatafreezer) HasAncient(kind string, number uint64) (bool, error) {
	return false, nil
}

func (f *nodatafreezer) Ancient(kind string, number uint64) ([]byte, error) {
	if _, ok := FreezerNoSnappy[kind]; ok {
		if number >= atomic.LoadUint64(&f.frozen) {
			return nil, errOutOfBounds
		}
		return nil, nil
	}
	return nil, errUnknownTable
}

func (f *nodatafreezer) Ancients() (uint64, error) {
	return atomic.LoadUint64(&f.frozen), nil
}

func (f *nodatafreezer) ItemAmountInAncient() (uint64, error) {
	return 0, nil
}

func (f *nodatafreezer) AncientOffSet() uint64 {
	return atomic.LoadUint64(&f.frozen)
}

func (f *nodatafreezer) AncientSize(kind string) (uint64, error) {
	if _, ok := FreezerNoSnappy[kind]; ok {
		return 0, nil
	}
	return 0, errUnknownTable
}

func (f *nodatafreezer) AppendAncient(number uint64, hash, header, body, receipts, td []byte) (err error) {
	if atomic.LoadUint64(&f.frozen) != number {
		return errOutOrderInsertion
	}
	atomic.AddUint64(&f.frozen, 1)
	return nil
}

func (f *nodatafreezer) TruncateAncients(items uint64) error {
	return nil
}

func (f *nodatafreezer) Sync() error {
	WriteFrozenOfAncientFreezer(f.db, atomic.LoadUint64(&f.frozen))
	return nil
}

func (f *nodatafreezer) freeze() {
	nfdb := &nofreezedb{KeyValueStore: f.db}

	var (
		backoff   bool
	)
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
				backoff = false
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
		backoff = gcKvStore(f.db, ancients, first, f.frozen, start)
	}
}