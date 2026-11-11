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

package snap

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestForwardAccountTaskCompletionFlags(t *testing.T) {
	hash := func(n byte) common.Hash { return common.BytesToHash([]byte{n}) }
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		for _, tc := range []struct {
			name    string
			hashes  []common.Hash
			cont    bool
			pending int // Index of an account still waiting for code, or -1.
			want    []common.Hash
			next    common.Hash
			done    bool
		}{
			{
				name: "continued", hashes: []common.Hash{hash(20), hash(40)}, cont: true, pending: -1,
				want: []common.Hash{hash(50)}, next: hash(41),
			},
			{
				name: "partial", hashes: []common.Hash{hash(20), hash(40)}, pending: 1,
				want: []common.Hash{hash(30), hash(40), hash(50)}, next: hash(21),
			},
			{
				name: "blocked", hashes: []common.Hash{hash(20), hash(40)}, pending: 0,
				want: []common.Hash{hash(10), hash(20), hash(30), hash(40), hash(50)}, next: hash(10),
			},
			{
				name: "completed", hashes: []common.Hash{hash(20), hash(40)}, pending: -1,
				next: hash(41), done: true,
			},
			{
				name: "empty", pending: -1, next: hash(10), done: true,
			},
			{
				name: "maximum_hash", hashes: []common.Hash{common.MaxHash}, pending: -1, done: true,
			},
		} {
			t.Run(scheme+"/"+tc.name, func(t *testing.T) {
				db := rawdb.NewMemoryDatabase()
				defer db.Close()
				syncer := newSyncer(db, scheme)
				defer syncer.syncRunner.barrier()
				syncer.loadSyncStatus()
				task := syncer.tasks[0]
				task.Next, task.Last = hash(10), common.MaxHash
				for _, n := range []byte{10, 20, 30, 40, 50} {
					task.stateCompleted[hash(n)] = struct{}{}
				}
				task.res = &accountResponse{hashes: tc.hashes, cont: tc.cont}
				for range tc.hashes {
					task.res.accounts = append(task.res.accounts, &types.StateAccount{
						Balance: uint256.NewInt(1), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash.Bytes(),
					})
				}
				task.needCode = make([]bool, len(tc.hashes))
				task.needState = make([]bool, len(tc.hashes))
				task.needHeal = make([]bool, len(tc.hashes))
				if tc.pending >= 0 {
					task.needCode[tc.pending] = true
				}
				syncer.forwardAccountTask(task)
				if task.Next != tc.next || task.done != tc.done {
					t.Fatalf("task progress: next %x, done %t; want next %x, done %t", task.Next, task.done, tc.next, tc.done)
				}
				checkFlags := func(task *accountTask) {
					t.Helper()
					if len(task.stateCompleted) != len(tc.want) {
						t.Fatalf("completion flags: have %v, want %v", task.stateCompleted, tc.want)
					}
					for _, hash := range tc.want {
						if _, ok := task.stateCompleted[hash]; !ok {
							t.Fatalf("missing completion flag for %x", hash)
						}
					}
				}
				checkFlags(task)
				if !task.done {
					syncer.saveSyncStatus()
					resumed := newSyncer(db, scheme)
					resumed.loadSyncStatus()
					checkFlags(resumed.tasks[0])
				}
			})
		}
	}
}

// TestSyncCompletionFlagsAfterRestart exercises real request scheduling, proof
// verification, cancellation, and reopening a persistent database at a new root.
func TestSyncCompletionFlagsAfterRestart(t *testing.T) {
	// Keep all accounts in one task so pending bytecode holds the account cursor
	// while the storage responses finish. Like TestSyncAccountPerformance, this
	// test must not run in parallel because it changes accountConcurrency.
	defer func(old int) { accountConcurrency = old }(accountConcurrency)
	accountConcurrency = 1
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		for _, scenario := range []string{"middle_removed", "tail_removed", "unchanged"} {
			t.Run(scheme+"/"+scenario, func(t *testing.T) {
				oldTrie, accounts, storages, slots := makeAccountTrieWithStorage(rawdb.HashScheme, 39, 1, true, false, false)
				remaining := accounts
				switch scenario {
				case "middle_removed":
					remaining = []*kv{accounts[0], accounts[38]}
				case "tail_removed":
					remaining = accounts[:2]
				}
				// Build the expected root independently of the destination database.
				sourceDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), newDbConfig(rawdb.HashScheme))
				defer sourceDB.Close()
				newTrie := trie.NewEmpty(sourceDB)
				for _, account := range remaining {
					newTrie.MustUpdate(account.k, account.v)
				}
				newRoot, nodes := newTrie.Commit(false)
				if err := sourceDB.Update(newRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), triedb.NewStateSet()); err != nil {
					t.Fatal(err)
				}
				newTrie, err := trie.New(trie.StateTrieID(newRoot), sourceDB)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				openDB := func() ethdb.Database {
					t.Helper()
					db, err := pebble.New(dir, 16, 16, "", false)
					if err != nil {
						t.Fatal(err)
					}
					return rawdb.NewDatabase(db)
				}
				db := openDB()
				defer func() { db.Close() }()
				run := func(accountTrie *trie.Trie, values []*kv, interrupt bool) {
					t.Helper()
					cancel := make(chan struct{})
					var once sync.Once
					stop := func() { once.Do(func() { close(cancel) }) }
					defer stop()
					timeout := time.AfterFunc(10*time.Second, stop)
					defer timeout.Stop()
					syncer := newSyncer(db, scheme)
					peer := newTestPeer("source", t, stop)
					peer.accountTrie = accountTrie.Copy()
					peer.accountValues = values
					peer.setStorageTries(storages)
					peer.storageValues = slots
					if interrupt {
						// Withhold bytecode so the completed storage flags must be
						// persisted, rather than removed by normal account forwarding.
						peer.codeRequestHandler = func(*testPeer, uint64, []common.Hash, int) error { return nil }
						var delivered atomic.Int64
						peer.storageRequestHandler = func(p *testPeer, id uint64, root common.Hash, owners []common.Hash, origin, limit []byte, max int) error {
							err := defaultStorageRequestHandler(p, id, root, owners, origin, limit, max)
							if delivered.Add(int64(len(owners))) == int64(len(accounts)) {
								stop()
							}
							return err
						}
					}
					peer.remote = syncer
					if err := syncer.Register(peer); err != nil {
						t.Fatal(err)
					}
					err := syncer.Sync(accountTrie.Hash(), cancel)
					if interrupt {
						if !errors.Is(err, ErrCancelled) {
							t.Fatalf("interrupted sync: %v", err)
						}
					} else {
						if err != nil {
							t.Fatalf("resumed sync: %v", err)
						}
						if n := peer.nStorageRequests.Load(); n != 0 {
							t.Fatalf("completed storage was downloaded again: %d requests", n)
						}
					}
				}
				run(oldTrie, accounts, true)
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				db = openDB()
				var progress syncProgress
				if err := json.Unmarshal(rawdb.ReadSnapshotSyncStatus(db), &progress); err != nil {
					t.Fatal(err)
				}
				if len(progress.Tasks) != 1 || len(progress.Tasks[0].StorageCompleted) != 39 {
					t.Fatalf("expected one persisted task with 39 completed storages: %+v", progress.Tasks)
				}
				run(newTrie, remaining, false)
				verifyTrie(scheme, db, newRoot, t)
				for _, account := range remaining {
					owner := common.BytesToHash(account.k)
					full, err := types.FullAccountRLP(rawdb.ReadAccountSnapshot(db, owner))
					if err != nil || !bytes.Equal(full, account.v) {
						t.Fatalf("account mismatch for %x: %v", owner, err)
					}
					for _, slot := range slots[owner] {
						if got := rawdb.ReadStorageSnapshot(db, owner, common.BytesToHash(slot.k)); !bytes.Equal(got, slot.v) {
							t.Fatalf("storage mismatch for %x/%x", owner, slot.k)
						}
					}
				}
				t.Logf("reopened Pebble, resumed at %x, verified %d accounts and reused their storage", newRoot, len(remaining))
			})
		}
	}
}

// TestSyncResumedCompletionFlags checks that storage completed before a restart
// does not prevent an account range from finishing if those accounts disappear
// from the next state root.
func TestSyncResumedCompletionFlags(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		t.Run(scheme, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			defer db.Close()
			syncer := newSyncer(db, scheme)
			syncer.loadSyncStatus()
			syncer.tasks = syncer.tasks[:1]
			task := syncer.tasks[0]
			task.Last = common.MaxHash

			key, slot := common.HexToHash("0x01"), []byte{0x01}
			storage := trie.NewStackTrie(nil)
			if err := storage.Update(key[:], slot); err != nil {
				t.Fatal(err)
			}
			root := storage.Hash()
			hashes := make([]common.Hash, 39)
			accounts := make([]*types.StateAccount, len(hashes))
			for i := range hashes {
				hashes[i] = common.BytesToHash([]byte{byte(i + 1)})
				accounts[i] = &types.StateAccount{
					Balance: uint256.NewInt(1), Root: root, CodeHash: types.EmptyCodeHash.Bytes(),
				}
			}
			deliver := func(task *accountTask, owners []common.Hash) {
				t.Helper()
				res := &storageResponse{
					mainTask: task, accounts: owners,
					roots:  make([]common.Hash, len(owners)),
					hashes: make([][]common.Hash, len(owners)),
					slots:  make([][][]byte, len(owners)),
				}
				for i := range owners {
					res.roots[i] = root
					res.hashes[i] = []common.Hash{key}
					res.slots[i] = [][]byte{slot}
				}
				syncer.processStorageResponse(res)
			}
			syncer.processAccountResponse(&accountResponse{task: task, hashes: hashes, accounts: accounts})
			// The first account remains pending, so none of the 37 completed
			// accounts can be passed by the account cursor before shutdown.
			deliver(task, hashes[1:38])
			syncer.forwardAccountTask(task)
			syncer.saveSyncStatus()

			syncer = newSyncer(db, scheme)
			syncer.loadSyncStatus()
			task = syncer.tasks[0]
			if len(task.stateCompleted) != 37 {
				t.Fatalf("restored completion flags: have %d, want 37", len(task.stateCompleted))
			}
			// Only the first and last accounts exist in the resumed root.
			remaining := []common.Hash{hashes[0], hashes[38]}
			syncer.processAccountResponse(&accountResponse{
				task: task, hashes: remaining, accounts: []*types.StateAccount{accounts[0], accounts[38]},
			})
			deliver(task, remaining)
			syncer.syncRunner.barrier()
			if !task.done || len(task.stateCompleted) != 0 {
				t.Fatalf("unfinished account task: done %t, completion flags %d", task.done, len(task.stateCompleted))
			}
			// The surviving accounts and their storage must still form a valid trie.
			accountTrie := trie.NewStackTrie(nil)
			for _, hash := range remaining {
				full, err := types.FullAccountRLP(rawdb.ReadAccountSnapshot(db, hash))
				if err != nil {
					t.Fatal(err)
				}
				if err := accountTrie.Update(hash[:], full); err != nil {
					t.Fatal(err)
				}
			}
			verifyTrie(scheme, db, accountTrie.Hash(), t)
		})
	}
}
