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
	"slices"
	"sync"
	"sync/atomic"

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
}

// NewFreezer creates a freezer instance for maintaining immutable ordered
// data according to the given parameters.
//
// The 'tables' argument defines the data tables. If the value of a map
// entry is true, snappy compression is disabled for the table.
// additionTables indicates the new add tables for freezerDB, it has some special rules.
func NewFreezer(datadir string, namespace string, readonly bool, maxTableSize uint32, tables map[string]freezerTableConfig) (*Freezer, error) {
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
	}

	// Create the tables.
	for name, config := range tables {
		var (
			table *freezerTable
			err   error
		)
		if slices.Contains(additionTables, name) {
			table, err = openAdditionTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, config, readonly)
		} else {
			table, err = newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, config, readonly)
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

	// Create the write batch.
	freezer.writeBatch = newFreezerBatch(freezer)

	log.Info("Opened ancient database", "database", datadir, "readonly", readonly, "tail", freezer.tail.Load(), "frozen", freezer.frozen.Load())
	return freezer, nil
}

// openAdditionTable create table, it will auto create new files when it was first initialized
func openAdditionTable(datadir, name string, readMeter, writeMeter *metrics.Meter, sizeGauge *metrics.Gauge, maxTableSize uint32, config freezerTableConfig, readonly bool) (*freezerTable, error) {
	if readonly {
		f, err := newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, config, false)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	}
	return newTable(datadir, name, readMeter, writeMeter, sizeGauge, maxTableSize, config, readonly)
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
	return errors.Join(errs...)
}

// AncientDatadir returns the path of the ancient store.
func (f *Freezer) AncientDatadir() (string, error) {
	return f.datadir, nil
}

// Ancient retrieves an ancient binary blob from the append-only immutable files.
func (f *Freezer) Ancient(kind string, number uint64) ([]byte, error) {
	if table := f.tables[kind]; table != nil {
		return table.Retrieve(number)
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
		return table.RetrieveItems(start, count, maxBytes)
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
	return f.tables[kind].items.Load(), nil
}

// ItemAmountInAncient returns the actual length of current ancientDB.
func (f *Freezer) ItemAmountInAncient() (uint64, error) {
	return f.frozen.Load(), nil
}

// AncientOffSet returns the offset of current ancientDB.
func (f *Freezer) AncientOffSet() uint64 {
	return f.tail.Load()
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
		err := table.truncateHead(items)
		if err == errTruncationBelowTail {
			// This often happens in chain rewinds, but the blob table is special.
			// It has the same head, but a different tail from other tables (like bodies, receipts).
			// So if the chain is rewound to head below the blob's tail, it needs to reset again.
			if kind != ChainFreezerBlobSidecarTable {
				return 0, err
			}
			nt, err := table.resetItems(items)
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

// TruncateTail discards all data below the specified threshold. Note that only
// 'prunable' tables will be truncated.
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
	for kind, table := range f.tables {
		if slices.Contains(additionTables, kind) && EmptyTable(table) {
			continue
		}
		if table.config.prunable {
			if err := table.truncateTail(tail); err != nil {
				return 0, err
			}
		}
	}
	f.tail.Store(tail)
	return old, nil
}

// SyncAncient flushes all data tables to disk.
func (f *Freezer) SyncAncient() error {
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
		head       uint64
		prunedTail *uint64
	)
	// Hack to get boundary of any table
	for kind, table := range f.tables {
		// addition tables is special cases
		if slices.Contains(additionTables, kind) {
			continue
		}
		head = table.items.Load()
		break
	}
	for kind, table := range f.tables {
		// check addition tables, try to align with exist tables
		if slices.Contains(additionTables, kind) {
			// if the table is empty, just skip
			if EmptyTable(table) {
				continue
			}
			// all tables have to have the same head
			if head != table.items.Load() {
				return fmt.Errorf("freezer table %s has a differing head: %d != %d", kind, table.items.Load(), head)
			}
			// TODO(Jacksen): This error might be unexpected.
			if prunedTail != nil && *prunedTail > table.itemHidden.Load() {
				return fmt.Errorf("freezer table %s has differing tail: %d != %d", kind, table.itemHidden.Load(), *prunedTail)
			}
			continue
		}
		// all tables have to have the same head
		if head != table.items.Load() {
			return fmt.Errorf("freezer table %s has a differing head: %d != %d", kind, table.items.Load(), head)
		}
		if !table.config.prunable {
			// TODO(Nathan): In BSC's prune feature, `table.itemHidden.Load() != 0` may return true.
			//
			// non-prunable tables have to start at 0
			// if table.itemHidden.Load() != 0 {
			// 	return fmt.Errorf("non-prunable freezer table '%s' has a non-zero tail: %d", kind, table.itemHidden.Load())
			// }
		} else {
			// prunable tables have to have the same length
			if prunedTail == nil {
				tmp := table.itemHidden.Load()
				prunedTail = &tmp
			}
			if *prunedTail != table.itemHidden.Load() {
				return fmt.Errorf("freezer table %s has differing tail: %d != %d", kind, table.itemHidden.Load(), *prunedTail)
			}
		}
	}

	if prunedTail == nil {
		tmp := uint64(0)
		prunedTail = &tmp
	}

	f.frozen.Store(head)
	f.tail.Store(*prunedTail)
	return nil
}

// repair truncates all data tables to the same length.
func (f *Freezer) repair() error {
	var (
		head       = uint64(math.MaxUint64)
		prunedTail = uint64(0)
	)
	for kind, table := range f.tables {
		// addition tables only align head
		if slices.Contains(additionTables, kind) {
			if EmptyTable(table) {
				continue
			}
			head = min(head, table.items.Load())
			continue
		}
		head = min(head, table.items.Load())
		prunedTail = max(prunedTail, table.itemHidden.Load())
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
		if !table.config.prunable {
			// TODO(Nathan): In BSC's prune feature, `table.itemHidden.Load() != 0` may return true.
			//
			// non-prunable tables have to start at 0
			// if table.itemHidden.Load() != 0 {
			// 	panic(fmt.Sprintf("non-prunable freezer table %s has non-zero tail: %v", kind, table.itemHidden.Load()))
			// }
		} else {
			// prunable tables have to have the same length
			if err := table.truncateTail(prunedTail); err != nil {
				return err
			}
		}
	}

	f.frozen.Store(head)
	f.tail.Store(prunedTail)
	return nil
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
	t, exist := f.tables[kind]
	if !exist {
		return 0, errors.New("you reset a non-exist table")
	}

	old := t.itemHidden.Load()
	if err := t.truncateTail(tail); err != nil {
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

	if err := f.SyncAncient(); err != nil {
		return err
	}
	nt, err := t.resetItems(startAt)
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
	f.writeBatch = newFreezerBatch(f)
	log.Debug("Reset Table", "kind", kind, "tail", f.tables[kind].itemHidden.Load(), "frozen", f.tables[kind].items.Load())
	return nil
}

// resetTailMeta will reset tail meta with legacyOffset
// Caution: the freezer cannot be used anymore, it will sync/close all data files
func (f *Freezer) resetTailMeta(legacyOffset uint64) error {
	if f.readonly {
		return errReadOnly
	}

	// if the tail is already reset, just skip
	if f.tail.Load() == legacyOffset {
		return nil
	}

	if f.tail.Load() > 0 {
		return errors.New("the freezer's tail > 0, cannot reset again")
	}
	f.writeLock.Lock()
	defer f.writeLock.Unlock()
	for _, t := range f.tables {
		if err := t.resetTailMeta(legacyOffset); err != nil {
			return err
		}
	}
	return nil
}

func EmptyTable(t *freezerTable) bool {
	return t.items.Load() == 0
}
