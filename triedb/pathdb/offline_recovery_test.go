// Copyright 2026 The go-ethereum Authors
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

package pathdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestOfflineRecoveryReplay(t *testing.T) {
	fixture := newTester(t, &testerConfig{layers: 12})
	disk := fixture.db.diskdb
	defer disk.Close()
	if err := fixture.db.Commit(fixture.roots[11], false); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	target := fixture.roots[3]
	before := rawdb.ReadPersistentStateID(disk)
	report, err := OfflineRecover(disk, target, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Transitions != 8 || report.Applied {
		t.Fatalf("bad report %+v", report)
	}
	if rawdb.ReadPersistentStateID(disk) != before || rawdb.ReadSnapSyncStatusFlag(disk) != rawdb.StateSyncRunning {
		t.Fatal("inspection modified state")
	}
	if _, err = OfflineRecover(disk, common.Hash{1}, 100, true); err == nil {
		t.Fatal("accepted unknown root")
	}
	if _, err = OfflineRecover(disk, target, 1, true); err == nil {
		t.Fatal("accepted excessive replay")
	}
	if rawdb.ReadPersistentStateID(disk) != before {
		t.Fatal("rejected request changed durable base")
	}
	report, err = OfflineRecover(disk, target, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || rawdb.ReadPersistentStateID(disk) != report.TargetID {
		t.Fatal("replay did not complete")
	}
	// The recovery entry intentionally leaves sync/chain markers untouched.
	if rawdb.ReadSnapSyncStatusFlag(disk) != rawdb.StateSyncRunning {
		t.Fatal("replay cleared sync flag prematurely")
	}
	rawdb.WriteSnapSyncStatusFlag(disk, rawdb.StateSyncFinished)
	cfg := *ReadOnly
	fixture.db = New(disk, &cfg, false)
	defer fixture.db.Close()
	if err = fixture.verifyState(target); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineRecoveryRejectsTerminalMismatch(t *testing.T) {
	fixture := newTester(t, &testerConfig{layers: 5})
	disk := fixture.db.diskdb
	defer disk.Close()
	if err := fixture.db.Commit(fixture.roots[4], false); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	// Corrupt the mapping rather than merely asking for an unknown root.
	rawdb.WriteStateID(disk, fixture.roots[1], 3)
	before := rawdb.ReadPersistentStateID(disk)
	if _, err := OfflineRecover(disk, fixture.roots[1], 100, true); err == nil {
		t.Fatal("accepted mismatched history")
	}
	if rawdb.ReadPersistentStateID(disk) != before {
		t.Fatal("preflight wrote state")
	}
}

func TestOfflineReplayCheckDoesNotWrite(t *testing.T) {
	fixture := newTester(t, &testerConfig{layers: 12})
	disk := fixture.db.diskdb
	defer disk.Close()
	if e := fixture.db.Commit(fixture.roots[11], false); e != nil {
		t.Fatal(e)
	}
	if e := fixture.db.Close(); e != nil {
		t.Fatal(e)
	}
	root := rawdb.ReadAccountTrieNode(disk, nil)
	id := rawdb.ReadPersistentStateID(disk)
	report, e := OfflineReplayCheck(disk, fixture.roots[3], 100)
	if e != nil {
		t.Fatal(e)
	}
	if report.Transitions != 8 || report.Applied {
		t.Fatal(report)
	}
	if string(root) != string(rawdb.ReadAccountTrieNode(disk, nil)) || id != rawdb.ReadPersistentStateID(disk) {
		t.Fatal("source changed")
	}
	rawdb.DeleteAccountTrieNode(disk, nil)
	report, e = OfflineReplayCheck(disk, fixture.roots[3], 100)
	if e != nil {
		t.Fatal(e)
	}
	if report.Transitions != 8 || rawdb.HasAccountTrieNode(disk, nil) || id != rawdb.ReadPersistentStateID(disk) {
		t.Fatal("source changed after reconstructed-root replay")
	}
}

func TestOfflineReconstructThenApply(t *testing.T) {
	fixture := newTester(t, &testerConfig{layers: 12})
	disk := fixture.db.diskdb
	defer disk.Close()
	if e := fixture.db.Commit(fixture.roots[11], false); e != nil {
		t.Fatal(e)
	}
	if e := fixture.db.Close(); e != nil {
		t.Fatal(e)
	}
	original := rawdb.ReadAccountTrieNode(disk, nil)
	rawdb.DeleteAccountTrieNode(disk, nil)
	blob, e := OfflineReconstructedRoot(disk)
	if e != nil {
		t.Fatal(e)
	}
	if string(blob) != string(original) {
		t.Fatal("reconstruction differs")
	}
	if _, e = OfflineReplayCheck(disk, fixture.roots[3], 100); e != nil {
		t.Fatal(e)
	}
	rawdb.WriteAccountTrieNode(disk, nil, blob)
	if _, e = OfflineRecover(disk, fixture.roots[3], 100, true); e != nil {
		t.Fatal(e)
	}
	cfg := *ReadOnly
	fixture.db = New(disk, &cfg, false)
	defer fixture.db.Close()
	if e = fixture.verifyState(fixture.roots[3]); e != nil {
		t.Fatal(e)
	}
}

func TestOfflineReconstructedRootRejectsMismatch(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	rawdb.WriteSnapshotRoot(db, common.Hash{1})
	rawdb.WriteAccountTrieNode(db, []byte{0}, make([]byte, 33))
	if _, e := OfflineReconstructedRoot(db); e == nil {
		t.Fatal("accepted an unverified root")
	}
	if rawdb.HasAccountTrieNode(db, nil) {
		t.Fatal("reconstruction wrote to source")
	}
}
