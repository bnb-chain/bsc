// Copyright 2021 The go-ethereum Authors
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
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb/ancienttest"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

var (
	freezerTestTableDef = map[string]freezerTableConfig{"test": {noSnappy: true}}
	o1o2TableDef        = map[string]freezerTableConfig{"o1": {noSnappy: true, prunable: true}, "o2": {noSnappy: true, prunable: true}}
	o1o2a1TableDef      = map[string]freezerTableConfig{"o1": {noSnappy: true, prunable: true}, "o2": {noSnappy: true, prunable: true}, "a1": {noSnappy: true, prunable: true}}
)

func TestFreezerModify(t *testing.T) {
	t.Parallel()

	// Create test data.
	var valuesRaw [][]byte
	var valuesRLP []*big.Int
	for x := 0; x < 100; x++ {
		v := getChunk(256, x)
		valuesRaw = append(valuesRaw, v)
		iv := big.NewInt(int64(x))
		iv = iv.Exp(iv, iv, nil)
		valuesRLP = append(valuesRLP, iv)
	}

	tables := map[string]freezerTableConfig{"raw": {noSnappy: true}, "rlp": {noSnappy: false}}
	f, _ := newFreezerForTesting(t, tables)
	defer f.Close()

	// Commit test data.
	_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for i := range valuesRaw {
			if err := op.AppendRaw("raw", uint64(i), valuesRaw[i]); err != nil {
				return err
			}
			if err := op.Append("rlp", uint64(i), valuesRLP[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal("ModifyAncients failed:", err)
	}

	// Dump indexes.
	for _, table := range f.tables {
		t.Log(table.name, "index:", table.dumpIndexString(0, int64(len(valuesRaw))))
	}

	// Read back test data.
	checkAncientCount(t, f, "raw", uint64(len(valuesRaw)))
	checkAncientCount(t, f, "rlp", uint64(len(valuesRLP)))
	for i := range valuesRaw {
		v, _ := f.Ancient("raw", uint64(i))
		if !bytes.Equal(v, valuesRaw[i]) {
			t.Fatalf("wrong raw value at %d: %x", i, v)
		}
		ivEnc, _ := f.Ancient("rlp", uint64(i))
		want, _ := rlp.EncodeToBytes(valuesRLP[i])
		if !bytes.Equal(ivEnc, want) {
			t.Fatalf("wrong RLP value at %d: %x", i, ivEnc)
		}
	}
}

// This checks that ModifyAncients rolls back freezer updates
// when the function passed to it returns an error.
func TestFreezerModifyRollback(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, freezerTestTableDef)

	theError := errors.New("oops")
	_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		// Append three items. This creates two files immediately,
		// because the table size limit of the test freezer is 2048.
		require.NoError(t, op.AppendRaw("test", 0, make([]byte, 2048)))
		require.NoError(t, op.AppendRaw("test", 1, make([]byte, 2048)))
		require.NoError(t, op.AppendRaw("test", 2, make([]byte, 2048)))
		return theError
	})
	if err != theError {
		t.Errorf("ModifyAncients returned wrong error %q", err)
	}
	checkAncientCount(t, f, "test", 0)
	f.Close()

	// Reopen and check that the rolled-back data doesn't reappear.
	tables := map[string]freezerTableConfig{"test": {noSnappy: true}}
	f2, err := NewFreezer(dir, "", false, 2049, tables, false)
	if err != nil {
		t.Fatalf("can't reopen freezer after failed ModifyAncients: %v", err)
	}
	defer f2.Close()
	checkAncientCount(t, f2, "test", 0)
}

// This test runs ModifyAncients and Ancient concurrently with each other.
func TestFreezerConcurrentModifyRetrieve(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, freezerTestTableDef)
	defer f.Close()

	var (
		numReaders     = 5
		writeBatchSize = uint64(50)
		written        = make(chan uint64, numReaders*6)
		wg             sync.WaitGroup
	)
	wg.Add(numReaders + 1)

	// Launch the writer. It appends 10000 items in batches.
	go func() {
		defer wg.Done()
		defer close(written)
		for item := uint64(0); item < 10000; item += writeBatchSize {
			_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
				for i := uint64(0); i < writeBatchSize; i++ {
					item := item + i
					value := getChunk(32, int(item))
					if err := op.AppendRaw("test", item, value); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				panic(err)
			}
			for i := 0; i < numReaders; i++ {
				written <- item + writeBatchSize
			}
		}
	}()

	// Launch the readers. They read random items from the freezer up to the
	// current frozen item count.
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for frozen := range written {
				for rc := 0; rc < 80; rc++ {
					num := uint64(rand.Intn(int(frozen)))
					value, err := f.Ancient("test", num)
					if err != nil {
						panic(fmt.Errorf("error reading %d (frozen %d): %v", num, frozen, err))
					}
					if !bytes.Equal(value, getChunk(32, int(num))) {
						panic(fmt.Errorf("wrong value at %d", num))
					}
				}
			}
		}()
	}

	wg.Wait()
}

// This test runs ModifyAncients and TruncateHead concurrently with each other.
func TestFreezerConcurrentModifyTruncate(t *testing.T) {
	f, _ := newFreezerForTesting(t, freezerTestTableDef)
	defer f.Close()

	var item = make([]byte, 256)

	for i := 0; i < 10; i++ {
		// First reset and write 100 items.
		if _, err := f.TruncateHead(0); err != nil {
			t.Fatal("truncate failed:", err)
		}
		_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
			for i := uint64(0); i < 100; i++ {
				if err := op.AppendRaw("test", i, item); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal("modify failed:", err)
		}
		checkAncientCount(t, f, "test", 100)

		// Now append 100 more items and truncate concurrently.
		var (
			wg          sync.WaitGroup
			truncateErr error
			modifyErr   error
		)
		wg.Add(3)
		go func() {
			_, modifyErr = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
				for i := uint64(100); i < 200; i++ {
					if err := op.AppendRaw("test", i, item); err != nil {
						return err
					}
				}
				return nil
			})
			wg.Done()
		}()
		go func() {
			_, truncateErr = f.TruncateHead(10)
			wg.Done()
		}()
		go func() {
			f.AncientSize("test")
			wg.Done()
		}()
		wg.Wait()

		// Now check the outcome. If the truncate operation went through first, the append
		// fails, otherwise it succeeds. In either case, the freezer should be positioned
		// at 10 after both operations are done.
		if truncateErr != nil {
			t.Fatal("concurrent truncate failed:", truncateErr)
		}
		if !(errors.Is(modifyErr, nil) || errors.Is(modifyErr, errOutOrderInsertion)) {
			t.Fatal("wrong error from concurrent modify:", modifyErr)
		}
		checkAncientCount(t, f, "test", 10)
	}
}

func TestFreezerReadonlyValidate(t *testing.T) {
	tables := map[string]freezerTableConfig{"a": {noSnappy: true}, "b": {noSnappy: true}}
	dir := t.TempDir()
	// Open non-readonly freezer and fill individual tables
	// with different amount of data.
	f, err := NewFreezer(dir, "", false, 2049, tables, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}
	var item = make([]byte, 1024)
	aBatch := f.tables["a"].newBatch()
	require.NoError(t, aBatch.AppendRaw(0, item))
	require.NoError(t, aBatch.AppendRaw(1, item))
	require.NoError(t, aBatch.AppendRaw(2, item))
	require.NoError(t, aBatch.commit())
	bBatch := f.tables["b"].newBatch()
	require.NoError(t, bBatch.AppendRaw(0, item))
	require.NoError(t, bBatch.commit())
	if f.tables["a"].items.Load() != 3 {
		t.Fatalf("unexpected number of items in table")
	}
	if f.tables["b"].items.Load() != 1 {
		t.Fatalf("unexpected number of items in table")
	}
	require.NoError(t, f.Close())

	// Re-opening as readonly should fail when validating
	// table lengths.
	_, err = NewFreezer(dir, "", true, 2049, tables, false)
	if err == nil {
		t.Fatal("readonly freezer should fail with differing table lengths")
	}
}

func TestFreezerConcurrentReadonly(t *testing.T) {
	t.Parallel()

	tables := map[string]freezerTableConfig{"a": {noSnappy: true}}
	dir := t.TempDir()

	f, err := NewFreezer(dir, "", false, 2049, tables, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}
	var item = make([]byte, 1024)
	batch := f.tables["a"].newBatch()
	items := uint64(10)
	for i := uint64(0); i < items; i++ {
		require.NoError(t, batch.AppendRaw(i, item))
	}
	require.NoError(t, batch.commit())
	if loaded := f.tables["a"].items.Load(); loaded != items {
		t.Fatalf("unexpected number of items in table, want: %d, have: %d", items, loaded)
	}
	require.NoError(t, f.Close())

	var (
		wg   sync.WaitGroup
		fs   = make([]*Freezer, 5)
		errs = make([]error, 5)
	)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			f, err := NewFreezer(dir, "", true, 2049, tables, false)
			if err == nil {
				fs[i] = f
			} else {
				errs[i] = err
			}
		}(i)
	}

	wg.Wait()

	for i := range fs {
		if err := errs[i]; err != nil {
			t.Fatal("failed to open freezer", err)
		}
		require.NoError(t, fs[i].Close())
	}
}

func TestFreezer_AdditionTables(t *testing.T) {
	dir := t.TempDir()
	// Open non-readonly freezer and fill individual tables
	// with different amount of data.
	f, err := NewFreezer(dir, "", false, 2049, o1o2TableDef, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}

	var item = make([]byte, 1024)
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw("o1", 0, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o1", 1, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", 0, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", 1, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// check read only
	additionTables = []string{"a1"}
	f, err = NewFreezer(dir, "", true, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = NewFreezer(dir, "", false, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	frozen, _ := f.Ancients()
	require.NoError(t, f.ResetTable("a1", frozen, true))
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 2, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 3, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 4, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// check additional table boundary
	_, err = f.Ancient("a1", 1)
	require.Error(t, err)
	actual, err := f.Ancient("a1", 2)
	require.NoError(t, err)
	require.Equal(t, item, actual)

	// truncate additional table, and check boundary
	_, err = f.TruncateTableTail("o1", 3)
	require.Error(t, err)
	_, err = f.TruncateTableTail("a1", 3)
	require.NoError(t, err)
	_, err = f.Ancient("a1", 2)
	require.Error(t, err)
	actual, err = f.Ancient("a1", 3)
	require.NoError(t, err)
	require.Equal(t, item, actual)

	// check additional table head
	ancients, err := f.TableAncients("a1")
	require.NoError(t, err)
	require.Equal(t, uint64(5), ancients)
	require.NoError(t, f.Close())

	// reopen and read
	f, err = NewFreezer(dir, "", true, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)

	// recheck additional table boundary
	_, err = f.Ancient("a1", 2)
	require.Error(t, err)
	actual, err = f.Ancient("a1", 3)
	require.NoError(t, err)
	require.Equal(t, item, actual)
	ancients, err = f.TableAncients("a1")
	require.NoError(t, err)
	require.Equal(t, uint64(5), ancients)
	require.NoError(t, f.Close())
}

func TestFreezer_ResetTailMeta_WithAdditionTable(t *testing.T) {
	dir := t.TempDir()
	f, err := NewFreezer(dir, "", false, 2049, o1o2TableDef, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}

	var item = make([]byte, 1024)
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw("o1", 0, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o1", 1, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", 0, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", 1, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	additionTables = []string{"a1"}
	f, err = NewFreezer(dir, "", false, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	frozen, _ := f.Ancients()
	require.NoError(t, f.ResetTable("a1", frozen, true))
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 2, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 3, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, 4, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, f.SyncAncient())

	var offset uint64 = 10000
	require.NoError(t, f.resetTailMeta(offset))
	f.Close()

	// check items
	f, err = NewFreezer(dir, "", false, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	_, err = f.Ancient("o1", 0)
	require.Error(t, err)
	actual, err := f.Ancient("o1", offset)
	require.NoError(t, err)
	require.Equal(t, item, actual)
	_, err = f.Ancient("a1", offset+1)
	require.Error(t, err)
	actual, err = f.Ancient("a1", offset+2)
	require.NoError(t, err)
	require.Equal(t, item, actual)

	// truncate tail
	_, err = f.TruncateTail(offset + 2)
	require.NoError(t, err)
	actual, err = f.Ancient("o1", offset+2)
	require.NoError(t, err)
	require.Equal(t, item, actual)
	actual, err = f.Ancient("a1", offset+2)
	require.NoError(t, err)
	require.Equal(t, item, actual)
}

func TestFreezer_ResetTailMeta_EmptyTable(t *testing.T) {
	dir := t.TempDir()
	f, err := NewFreezer(dir, "", false, 2049, o1o2TableDef, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}
	var offset uint64 = 10000
	require.NoError(t, f.resetTailMeta(offset))
	f.Close()

	// try to append the ancient
	additionTables = []string{"a1"}
	f, err = NewFreezer(dir, "", false, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	var item = make([]byte, 1024)
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw("o1", offset, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o1", offset+1, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", offset, item); err != nil {
			return err
		}
		if err := op.AppendRaw("o2", offset+1, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = NewFreezer(dir, "", false, 2049, o1o2a1TableDef, false)
	require.NoError(t, err)
	frozen, _ := f.Ancients()
	require.NoError(t, f.ResetTable("a1", frozen, true))
	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, offset+2, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, offset+3, item); err != nil {
			return err
		}
		if err := appendSameItem(op, []string{"o1", "o2", "a1"}, offset+4, item); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, f.SyncAncient())

	// truncate tail
	_, err = f.TruncateTail(offset + 2)
	require.NoError(t, err)
	actual, err := f.Ancient("o1", offset+2)
	require.NoError(t, err)
	require.Equal(t, item, actual)
	actual, err = f.Ancient("a1", offset+2)
	require.NoError(t, err)
	require.Equal(t, item, actual)
}

func appendSameItem(op ethdb.AncientWriteOp, tables []string, i uint64, item []byte) error {
	for _, t := range tables {
		if err := op.AppendRaw(t, i, item); err != nil {
			return err
		}
	}
	return nil
}

func newFreezerForTesting(t *testing.T, tables map[string]freezerTableConfig) (*Freezer, string) {
	t.Helper()

	dir := t.TempDir()
	// note: using low max table size here to ensure the tests actually
	// switch between multiple files.
	f, err := NewFreezer(dir, "", false, 2049, tables, false)
	if err != nil {
		t.Fatal("can't open freezer", err)
	}
	return f, dir
}

// checkAncientCount verifies that the freezer contains n items.
func checkAncientCount(t *testing.T, f *Freezer, kind string, n uint64) {
	t.Helper()

	if frozen, _ := f.Ancients(); frozen != n {
		t.Fatalf("Ancients() returned %d, want %d", frozen, n)
	}

	// Check at index n-1.
	if n > 0 {
		index := n - 1
		if _, err := f.Ancient(kind, index); err != nil {
			t.Errorf("Ancient(%q, %d) returned unexpected error %q", kind, index, err)
		}
	}

	// Check at index n.
	index := n
	if _, err := f.Ancient(kind, index); err == nil {
		t.Errorf("Ancient(%q, %d) didn't return expected error", kind, index)
	} else if err != errOutOfBounds {
		t.Errorf("Ancient(%q, %d) returned unexpected error %q", kind, index, err)
	}
}

func TestFreezerCloseSync(t *testing.T) {
	t.Parallel()
	f, _ := newFreezerForTesting(t, map[string]freezerTableConfig{"a": {noSnappy: true}, "b": {noSnappy: true}})
	defer f.Close()

	// Now, close and sync. This mimics the behaviour if the node is shut down,
	// just as the chain freezer is writing.
	// 1: thread-1: chain treezer writes, via freezeRange (holds lock)
	// 2: thread-2: Close called, waits for write to finish
	// 3: thread-1: finishes writing, releases lock
	// 4: thread-2: obtains lock, completes Close()
	// 5: thread-1: calls f.Sync()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.SyncAncient(); err == nil {
		t.Fatalf("want error, have nil")
	} else if have, want := err.Error(), "[closed closed]"; have != want {
		t.Fatalf("want %v, have %v", have, want)
	}
}

func TestFreezerSuite(t *testing.T) {
	ancienttest.TestAncientSuite(t, func(kinds []string) ethdb.AncientStore {
		tables := make(map[string]freezerTableConfig)
		for _, kind := range kinds {
			tables[kind] = freezerTableConfig{
				noSnappy: true,
				prunable: true,
			}
		}
		f, _ := newFreezerForTesting(t, tables)
		return f
	})
	ancienttest.TestResettableAncientSuite(t, func(kinds []string) ethdb.ResettableAncientStore {
		tables := make(map[string]freezerTableConfig)
		for _, kind := range kinds {
			tables[kind] = freezerTableConfig{
				noSnappy: true,
				prunable: true,
			}
		}
		f, _ := newResettableFreezer(t.TempDir(), "", false, 2048, tables, false)
		return f
	})
}
