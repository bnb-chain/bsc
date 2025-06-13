package rawdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/ethdb"
)

// incrFreezer is a wrapper of multiple freezers which automatically
// switches to a new freezer when the current one exceeds the size limit.
type incrFreezer struct {
	readOnly       bool
	currentFreezer *resettableFreezer
	baseDir        string
	kind           string
	namespace      string
	offset         uint64
	maxTableSize   uint32
	tables         map[string]bool
	blockLimit     uint64
	lock           sync.RWMutex
}

// newIncrFreezer creates a new incremental freezer that automatically
// switches to a new freezer when the current one exceeds the item limit.
func newIncrFreezer(baseDir, namespace string, readonly bool, offset uint64, maxTableSize uint32,
	tables map[string]bool, blockLimit uint64) (*incrFreezer, error) {
	if blockLimit == 0 {
		return nil, errors.New("block limit must be greater than 0")
	}

	if err := cleanup(baseDir); err != nil {
		return nil, err
	}

	// Find the latest data directory
	// latestDir := baseDir
	// entries, err := os.ReadDir(baseDir)
	// if err != nil && !os.IsNotExist(err) {
	// 	return nil, err
	// }

	// If there are subdirectories, find the one with the highest block number
	// if len(entries) > 0 {
	// 	var maxBlock uint64
	// 	for _, entry := range entries {
	// 		if !entry.IsDir() {
	// 			continue
	// 		}
	// 		// Try to parse directory name as block number
	// 		if blockNum, err := strconv.ParseUint(entry.Name(), 10, 64); err == nil {
	// 			if blockNum > maxBlock {
	// 				maxBlock = blockNum
	// 				latestDir = filepath.Join(baseDir, entry.Name())
	// 			}
	// 		}
	// 	}
	// } else {
	// 	latestDir = filepath.Join(fmt.Sprintf("%s/%d/%s", baseDir, 0, kind))
	// 	log.Info("Creating new incremental base directory", "dir", latestDir)
	// }

	// Create initial freezer
	freezer, err := newResettableFreezer(baseDir, namespace, readonly, offset, maxTableSize, tables)
	if err != nil {
		return nil, err
	}

	return &incrFreezer{
		readOnly:       readonly,
		currentFreezer: freezer,
		baseDir:        baseDir,
		namespace:      namespace,
		offset:         offset,
		maxTableSize:   maxTableSize,
		tables:         tables,
		blockLimit:     blockLimit,
	}, nil
}

// switchToNewFreezer creates a new freezer with the given block number as suffix
// and switches to it.
func (f *incrFreezer) switchToNewFreezer(blockNumber uint64) error {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.readOnly {
		return errReadOnly
	}

	// Create new directory with block number as suffix
	newDir := filepath.Join(fmt.Sprintf("%s/%d/%s", f.baseDir, blockNumber, f.kind))
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return err
	}

	// Create new freezer
	newFreezer, err := newResettableFreezer(newDir, f.namespace, f.readOnly, f.offset, f.maxTableSize, f.tables)
	if err != nil {
		os.RemoveAll(newDir)
		return err
	}

	// Close old freezer
	if err = f.currentFreezer.Close(); err != nil {
		newFreezer.Close()
		os.RemoveAll(newDir)
		return err
	}

	// Switch to new freezer
	f.currentFreezer = newFreezer
	return nil
}

// checkAndSwitch checks if the current freezer exceeds the item limit and
// switches to a new one if necessary.
func (f *incrFreezer) checkAndSwitch() error {
	// TODO: confirm this amount is correct?
	items, err := f.currentFreezer.ItemAmountInAncient()
	if err != nil {
		return err
	}

	if f.blockLimit != 0 && items > f.blockLimit {
		head, err := f.currentFreezer.Ancients()
		if err != nil {
			return err
		}
		return f.switchToNewFreezer(head)
	}
	return nil
}

// ModifyAncients implements the ethdb.AncientWriteOp interface.
func (f *incrFreezer) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	// Check if we need to switch to a new freezer before writing
	// if err := f.checkAndSwitch(); err != nil {
	// 	return 0, err
	// }

	return f.currentFreezer.ModifyAncients(fn)
}

// Ancient implements the ethdb.AncientReader interface.
func (f *incrFreezer) Ancient(kind string, number uint64) ([]byte, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.Ancient(kind, number)
}

// Ancients implements the ethdb.AncientReader interface.
func (f *incrFreezer) Ancients() (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.Ancients()
}

// Tail implements the ethdb.AncientReader interface.
func (f *incrFreezer) Tail() (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.Tail()
}

// Close implements the ethdb.AncientStore interface.
func (f *incrFreezer) Close() error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.Close()
}

// HasAncient returns an indicator whether the specified ancient data exists
// in the freezer
func (f *incrFreezer) HasAncient(kind string, number uint64) (bool, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.HasAncient(kind, number)
}

// AncientRange retrieves multiple items in sequence, starting from the index 'start'.
// It will return
//   - at most 'count' items,
//   - if maxBytes is specified: at least 1 item (even if exceeding the maxByteSize),
//     but will otherwise return as many items as fit into maxByteSize.
//   - if maxBytes is not specified, 'count' items will be returned if they are present.
func (f *incrFreezer) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.AncientRange(kind, start, count, maxBytes)
}

// AncientDatadir implements the ethdb.AncientStore interface.
func (f *incrFreezer) AncientDatadir() (string, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.baseDir, nil
}

// ItemAmountInAncient implements the ethdb.AncientStore interface.
func (f *incrFreezer) ItemAmountInAncient() (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.ItemAmountInAncient()
}

// AncientSize returns the ancient size of the specified category.
func (f *incrFreezer) AncientSize(kind string) (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.AncientSize(kind)
}

// AncientOffSet returns the offset of current ancientDB.
func (f *incrFreezer) AncientOffSet() uint64 {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.AncientOffSet()
}

// SyncAncient flushes all data tables to disk.
func (f *incrFreezer) SyncAncient() error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.SyncAncient()
}

// ReadAncients runs the given read operation while ensuring that no writes take place
// on the underlying freezer.
func (f *incrFreezer) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.ReadAncients(fn)
}

// TruncateHead discards any recent data above the provided threshold number.
// It returns the previous head number.
func (f *incrFreezer) TruncateHead(items uint64) (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.TruncateHead(items)
}

// TruncateTail discards any recent data below the provided threshold number.
// It returns the previous value
func (f *incrFreezer) TruncateTail(tail uint64) (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.TruncateTail(tail)
}

// Reset deletes the file directory exclusively occupied by the freezer and
// recreate the freezer from scratch. The atomicity of directory deletion
// is guaranteed by the rename operation, the leftover directory will be
// cleaned up in next startup in case crash happens after rename.
func (f *incrFreezer) Reset() error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.Reset()
}

// TruncateTableTail will truncate certain table to new tail
func (f *incrFreezer) TruncateTableTail(kind string, tail uint64) (uint64, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.TruncateTableTail(kind, tail)
}

// ResetTable will reset certain table with new start point
func (f *incrFreezer) ResetTable(kind string, startAt uint64, onlyEmpty bool) error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.currentFreezer.ResetTable(kind, startAt, onlyEmpty)
}
