// Copyright 2019 The go-ethereum Authors
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
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/gofrs/flock"
)

var (
	// errReadOnly is returned if the freezer is opened in read only mode. All the
	// mutations are disallowed.
	errReadOnly = errors.New("read only")

	// errUnknownTable is returned if the user attempts to read from a table that is
	// not tracked by the freezer.
	errUnknownTable = errors.New("unknown table")

	// errOutOrderInsertion is returned if the user attempts to inject out-of-order
	// binary blobs into the freezer.
	errOutOrderInsertion = errors.New("the append operation is out-order")

	// errSymlinkDatadir is returned if the ancient directory specified by user
	// is a symbolic link.
	errSymlinkDatadir = errors.New("symbolic link datadir is not supported")
)

// freezerTableSize defines the maximum size of freezer data files.
const freezerTableSize = 2 * 1000 * 1000 * 1000

// Freezer is an append-only database to store immutable ordered data into
// flat files:
//
// - The append-only nature ensures that disk writes are minimized.
// - The in-order data ensures that disk reads are always optimized.
type Freezer struct {
	datadir string
	frozen  atomic.Uint64 // Number of items already frozen
	tail    atomic.Uint64 // Number of the first stored item in the freezer

	// This lock synchronizes writers and the truncate operation, as well as
	// the "atomic" (batched) read operations.
	writeLock  sync.RWMutex
	writeBatch *freezerBatch

	readonly     bool
	tables       map[string]*freezerTable // Data tables for storing everything
	instanceLock *flock.Flock             // File-system lock to prevent double opens
	closeOnce    sync.Once
	offset       uint64 // Starting BlockNumber in current freezer
}

// NewFreezer creates a freezer instance for maintaining immutable ordered
// data according to the given parameters.
//
// The 'tables' argument defines the data tables. If the value of a map
// entry is true, snappy compression is disabled for the table.
// additionTables indicates the new add tables for freezerDB, it has some special rules.
func NewFreezer(datadir string, namespace string, readonly bool, offset uint64, maxTableSize uint32, tables map[string]bool) (*Freezer, error) {
	// Create the initial freezer object
	var (
		readMeter  = metrics.NewRegisteredMeter(namespace+"ancient/read", nil)
		writeMeter = metrics.NewRegisteredMeter(namespace+"ancient/write", nil)
		sizeGauge  = metrics.NewRegisteredGauge(namespace+"ancient/size", nil)
	)
	// Ensure the datadir is not a symbolic link if it exists.
	if info, err := os.Lstat(datadir); !os.IsNotExist(err) {
		if info == nil {
			log.Warn("Could not Lstat the database", "path", datadir)
			return nil, errors.New("lstat failed")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			log.Warn("Symbolic link ancient database is not supported", "path", datadir)
			return nil, errSymlinkDatadir
		}
	}
	flockFile := filepath.Join(datadir, "FLOCK")
	if err := os.MkdirAll(filepath.Dir(flockFile), 0755); err != nil {
		return nil, err
	}
	// Leveldb uses LOCK as the filelock filename. To prevent the
	// name collision, we use FLOCK as the lock name.
	lock := flock.New(flockFile)
	tryLock := lock.TryLock
	if readonly {
		tryLock = lock.TryRLock
	}
	if locked, err := tryLock(); err != nil {
		return nil, err
	} else if !locked {
		return nil, errors.New("locking failed")
	}
	// Open all the supported data tables
	freezer := &Freezer{
		datadir:      datadir,
		readonly:     readonly,
		tables:       make(map[string]*freezerTable),
		instanceLock: lock,
		offset:       offset,
	}

	// Create the tables.
	for name, disableSnappy := range tables {
		var (
			table *freezerTable
			err   error
		)
		if slices.Contains(additionTables, name) {
			table, err = openAdditionTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, disableSnappy, readonly)
		} else {
			table, err = newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, disableSnappy, readonly)
		}
		if err != nil {
			for _, table := range freezer.tables {
				table.Close()
			}
			lock.Unlock()
			return nil, err
		}
		freezer.tables[name] = table
	}
	var err error
	if freezer.readonly {
		// In readonly mode only validate, don't truncate.
		// validate also sets `freezer.frozen`.
		err = freezer.validate()
	} else {
		// Truncate all tables to common length.
		err = freezer.repair()
	}
	if err != nil {
		for _, table := range freezer.tables {
			table.Close()
		}
		lock.Unlock()
		return nil, err
	}

	// Some blocks in ancientDB may have already been frozen and been pruned, so adding the offset to
	// represent the absolute number of blocks already frozen.
	freezer.frozen.Add(offset)
	freezer.tail.Add(offset)

	// Create the write batch.
	freezer.writeBatch = newFreezerBatch(freezer)

	log.Info("Opened ancient database", "database", datadir, "readonly", readonly, "frozen", freezer.frozen.Load())
	return freezer, nil
}

// openAdditionTable create table, it will auto create new files when it was first initialized
func openAdditionTable(datadir, name string, readMeter, writeMeter *metrics.Meter, sizeGauge *metrics.Gauge, maxTableSize uint32, disableSnappy, readonly bool) (*freezerTable, error) {
	if readonly {
		f, err := newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, disableSnappy, false)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	}
	return newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, disableSnappy, readonly)
}

// Close terminates the chain freezer, closing all the data files.
func (f *Freezer) Close() error {
	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	var errs []error
	f.closeOnce.Do(func() {
		for _, table := range f.tables {
			if err := table.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if err := f.instanceLock.Unlock(); err != nil {
			errs = append(errs, err)
		}
	})
	if errs != nil {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// AncientDatadir returns the path of the ancient store.
func (f *Freezer) AncientDatadir() (string, error) {
	return f.datadir, nil
}

// HasAncient returns an indicator whether the specified ancient data exists
// in the freezer.
func (f *Freezer) HasAncient(kind string, number uint64) (bool, error) {
	if table := f.tables[kind]; table != nil {
		return table.has(number - f.offset), nil
	}
	return false, nil
}

// Ancient retrieves an ancient binary blob from the append-only immutable files.
func (f *Freezer) Ancient(kind string, number uint64) ([]byte, error) {
	if table := f.tables[kind]; table != nil {
		return table.Retrieve(number - f.offset)
	}
	return nil, errUnknownTable
}

// AncientRange retrieves multiple items in sequence, starting from the index 'start'.
// It will return
//   - at most 'count' items,
//   - if maxBytes is specified: at least 1 item (even if exceeding the maxByteSize),
//     but will otherwise return as many items as fit into maxByteSize.
//   - if maxBytes is not specified, 'count' items will be returned if they are present.
func (f *Freezer) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	if table := f.tables[kind]; table != nil {
		return table.RetrieveItems(start-f.offset, count, maxBytes)
	}
	return nil, errUnknownTable
}

// Ancients returns the length of the frozen items.
func (f *Freezer) Ancients() (uint64, error) {
	return f.frozen.Load(), nil
}

func (f *Freezer) TableAncients(kind string) (uint64, error) {
	f.writeLock.RLock()
	defer f.writeLock.RUnlock()
	return f.tables[kind].items.Load() + f.offset, nil
}

// ItemAmountInAncient returns the actual length of current ancientDB.
func (f *Freezer) ItemAmountInAncient() (uint64, error) {
	return f.frozen.Load() - atomic.LoadUint64(&f.offset), nil
}

// AncientOffSet returns the offset of current ancientDB.
func (f *Freezer) AncientOffSet() uint64 {
	return atomic.LoadUint64(&f.offset)
}

// Tail returns the number of first stored item in the freezer.
func (f *Freezer) Tail() (uint64, error) {
	return f.tail.Load(), nil
}

// AncientSize returns the ancient size of the specified category.
func (f *Freezer) AncientSize(kind string) (uint64, error) {
	// This needs the write lock to avoid data races on table fields.
	// Speed doesn't matter here, AncientSize is for debugging.
	f.writeLock.RLock()
	defer f.writeLock.RUnlock()

	if table := f.tables[kind]; table != nil {
		return table.size()
	}
	return 0, errUnknownTable
}

// ReadAncients runs the given read operation while ensuring that no writes take place
// on the underlying freezer.
func (f *Freezer) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	f.writeLock.RLock()
	defer f.writeLock.RUnlock()

	return fn(f)
}

// ModifyAncients runs the given write operation.
func (f *Freezer) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (writeSize int64, err error) {
	if f.readonly {
		return 0, errReadOnly
	}
	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	// Roll back all tables to the starting position in case of error.
	prevItem := f.frozen.Load()
	defer func() {
		if err != nil {
			// The write operation has failed. Go back to the previous item position.
			for name, table := range f.tables {
				err := table.truncateHead(prevItem)
				if err != nil {
					log.Error("Freezer table roll-back failed", "table", name, "index", prevItem, "err", err)
				}
			}
		}
	}()

	f.writeBatch.reset()
	if err := fn(f.writeBatch); err != nil {
		return 0, err
	}
	item, writeSize, err := f.writeBatch.commit()
	if err != nil {
		return 0, err
	}
	f.frozen.Store(item)
	return writeSize, nil
}

// TruncateHead discards any recent data above the provided threshold number.
// It returns the previous head number.
func (f *Freezer) TruncateHead(items uint64) (uint64, error) {
	if f.readonly {
		return 0, errReadOnly
	}
	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	oitems := f.frozen.Load()
	if oitems <= items {
		return oitems, nil
	}
	for kind, table := range f.tables {
		err := table.truncateHead(items - f.offset)
		if err == errTruncationBelowTail {
			// This often happens in chain rewinds, but the blob table is special.
			// It has the same head, but a different tail from other tables (like bodies, receipts).
			// So if the chain is rewound to head below the blob's tail, it needs to reset again.
			if kind != ChainFreezerBlobSidecarTable {
				return 0, err
			}
			nt, err := table.resetItems(items - f.offset)
			if err != nil {
				return 0, err
			}
			f.tables[kind] = nt
			continue
		}
		if err != nil {
			return 0, err
		}
	}
	f.frozen.Store(items)
	return oitems, nil
}

// TruncateTail discards any recent data below the provided threshold number.
func (f *Freezer) TruncateTail(tail uint64) (uint64, error) {
	if f.readonly {
		return 0, errReadOnly
	}
	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	old := f.tail.Load()
	if old >= tail {
		return old, nil
	}
	for _, table := range f.tables {
		if err := table.truncateTail(tail - f.offset); err != nil {
			return 0, err
		}
	}
	f.tail.Store(tail)
	return old, nil
}

// Sync flushes all data tables to disk.
func (f *Freezer) Sync() error {
	var errs []error
	for _, table := range f.tables {
		if err := table.Sync(); err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// validate checks that every table has the same boundary.
// Used instead of `repair` in readonly mode.
func (f *Freezer) validate() error {
	if len(f.tables) == 0 {
		return nil
	}
	var (
		head uint64
		tail uint64
		name string
	)
	// Hack to get boundary of any table
	for kind, table := range f.tables {
		// addition tables is special cases
		if slices.Contains(additionTables, kind) {
			continue
		}
		head = table.items.Load()
		tail = table.itemHidden.Load()
		name = kind
		break
	}
	// Now check every table against those boundaries.
	for kind, table := range f.tables {
		// check addition tables, try to align with exist tables
		if slices.Contains(additionTables, kind) {
			// if the table is empty, just skip
			if EmptyTable(table) {
				continue
			}
			// otherwise, just align head
			if head != table.items.Load() {
				return fmt.Errorf("freezer tables %s and %s have differing head: %d != %d", kind, name, table.items.Load(), head)
			}
			if tail > table.itemHidden.Load() {
				return fmt.Errorf("freezer tables %s and %s have differing tail: %d != %d", kind, name, table.itemHidden.Load(), tail)
			}
			continue
		}
		if head != table.items.Load() {
			return fmt.Errorf("freezer tables %s and %s have differing head: %d != %d", kind, name, table.items.Load(), head)
		}
		if tail != table.itemHidden.Load() {
			return fmt.Errorf("freezer tables %s and %s have differing tail: %d != %d", kind, name, table.itemHidden.Load(), tail)
		}
	}
	f.frozen.Store(head)
	f.tail.Store(tail)
	return nil
}

// repair truncates all data tables to the same length.
func (f *Freezer) repair() error {
	var (
		head = uint64(math.MaxUint64)
		tail = uint64(0)
	)
	for kind, table := range f.tables {
		// addition tables only align head
		if slices.Contains(additionTables, kind) {
			if EmptyTable(table) {
				continue
			}
			items := table.items.Load()
			if head > items {
				head = items
			}
			continue
		}
		items := table.items.Load()
		if head > items {
			head = items
		}
		hidden := table.itemHidden.Load()
		if hidden > tail {
			tail = hidden
		}
	}
	for kind, table := range f.tables {
		//  try to align with exist tables, skip empty table
		if slices.Contains(additionTables, kind) && EmptyTable(table) {
			continue
		}
		err := table.truncateHead(head)
		if err == errTruncationBelowTail {
			// This often happens in chain rewinds, but the blob table is special.
			// It has the same head, but a different tail from other tables (like bodies, receipts).
			// So if the chain is rewound to head below the blob's tail, it needs to reset again.
			if kind != ChainFreezerBlobSidecarTable {
				return err
			}
			nt, err := table.resetItems(head)
			if err != nil {
				return err
			}
			f.tables[kind] = nt
			continue
		}
		if err != nil {
			return err
		}
		if err := table.truncateTail(tail); err != nil {
			return err
		}
	}
	f.frozen.Store(head)
	f.tail.Store(tail)
	return nil
}

// delete leveldb data that save to ancientdb, split from func freeze
func gcKvStore(db ethdb.KeyValueStore, ancients []common.Hash, first uint64, frozen uint64, start time.Time) {
	// Wipe out all data from the active database
	batch := db.NewBatch()
	for i := 0; i < len(ancients); i++ {
		// Always keep the genesis block in active database
		if blockNumber := first + uint64(i); blockNumber != 0 {
			DeleteBlockWithoutNumber(batch, ancients[i], blockNumber)
			DeleteCanonicalHash(batch, blockNumber)
		}
	}
	if err := batch.Write(); err != nil {
		log.Crit("Failed to delete frozen canonical blocks", "err", err)
	}
	batch.Reset()

	// Wipe out side chains also and track dangling side chians
	var dangling []common.Hash
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

	// Log something friendly for the user
	context := []interface{}{
		"blocks", frozen - first, "elapsed", common.PrettyDuration(time.Since(start)), "number", frozen - 1,
	}
	if n := len(ancients); n > 0 {
		context = append(context, []interface{}{"hash", ancients[n-1]}...)
	}
	log.Info("Deep froze chain segment", context...)
}

// TruncateTableTail will truncate certain table to new tail
func (f *Freezer) TruncateTableTail(kind string, tail uint64) (uint64, error) {
	if f.readonly {
		return 0, errReadOnly
	}

	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	if !slices.Contains(additionTables, kind) {
		return 0, errors.New("only new added table could be truncated independently")
	}
	if tail < f.offset {
		return 0, errors.New("the input tail&head is less than offset")
	}
	t, exist := f.tables[kind]
	if !exist {
		return 0, errors.New("you reset a non-exist table")
	}

	old := t.itemHidden.Load() + f.offset
	if err := t.truncateTail(tail - f.offset); err != nil {
		return 0, err
	}
	return old, nil
}

// ResetTable will reset certain table with new start point
// only used for ChainFreezerBlobSidecarTable now
func (f *Freezer) ResetTable(kind string, startAt uint64, onlyEmpty bool) error {
	if f.readonly {
		return errReadOnly
	}

	f.writeLock.Lock()
	defer f.writeLock.Unlock()

	t, exist := f.tables[kind]
	if !exist {
		return errors.New("you reset a non-exist table")
	}

	// if you reset a non empty table just skip
	if onlyEmpty && !EmptyTable(t) {
		return nil
	}

	if err := f.Sync(); err != nil {
		return err
	}
	nt, err := t.resetItems(startAt - f.offset)
	if err != nil {
		return err
	}
	f.tables[kind] = nt

	// repair all tables with same tail & head
	if err := f.repair(); err != nil {
		for _, table := range f.tables {
			table.Close()
		}
		return err
	}

	f.frozen.Add(f.offset)
	f.tail.Add(f.offset)
	f.writeBatch = newFreezerBatch(f)
	log.Debug("Reset Table", "kind", kind, "tail", f.tables[kind].itemHidden.Load(), "frozen", f.tables[kind].items.Load())
	return nil
}

func EmptyTable(t *freezerTable) bool {
	return t.items.Load() == 0
}
