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
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// A failed catch-up must remain usable by the next state-sync cycle. Returning
// an error and preserving a journal alone does not prove that retry can finish.
func TestSyncV2RetriesAfterBALUnavailable(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		t.Run(scheme, func(t *testing.T) {
			fix := seedCompletedSync(t, scheme)
			defer fix.db.Close()
			s := newSyncerV2(fix.db, scheme)
			var available atomic.Bool
			peer := newTestPeerV2("retry", t, func() {})
			peer.accessLists = fix.bals
			peer.accessListRequestHandler = func(p *testPeerV2, id uint64, hashes []common.Hash, max int) error {
				if available.Load() {
					return defaultAccessListRequestHandler(p, id, hashes, max)
				}
				var values rlp.RawList[rlp.RawValue]
				for range hashes {
					values.AppendRaw(rlp.EmptyString)
				}
				return p.remote.OnAccessLists(p, id, values)
			}
			peer.remote = s
			if err := s.Register(peer); err != nil {
				t.Fatal(err)
			}
			cancel := make(chan struct{})
			err := s.Sync(fix.header102, cancel)
			close(cancel) // The downloader ends the failed state-sync cycle.
			if err != errAccessListUnavailable {
				t.Fatalf("first cycle: got %v, want unavailable BAL", err)
			}
			progress := decodeJournal(t, fix.db)
			if progress.Pivot == nil || progress.Pivot.Hash() != fix.pivotA.Hash() || progress.Phase != phaseComplete {
				t.Fatal("failed catch-up lost the completed checkpoint")
			}
			available.Store(true)
			cancel = make(chan struct{})
			defer close(cancel)
			if err := s.Sync(fix.header102, cancel); err != nil {
				t.Fatalf("retry after BAL becomes available: %v", err)
			}
			if peer.nAccountRequests.Load() != 0 {
				t.Fatal("retry downloaded accounts instead of resuming catch-up")
			}
			progress = decodeJournal(t, fix.db)
			if progress.Pivot == nil || progress.Pivot.Hash() != fix.header102.Hash() || progress.Phase != phaseComplete {
				t.Fatal("retry did not persist completion at the target")
			}
			assertAccountBalance(t, fix.db, fix.hashY, 2000)
			verifyTrie(scheme, fix.db, fix.header102.Root, t)
		})
	}
}

// CLI overrides are applied after package initialization. Both the downloader
// and state syncer must use the current value, rather than a captured copy.
func TestCatchUpRetentionPolicy(t *testing.T) {
	previous := params.FullImmutabilityThreshold
	params.FullImmutabilityThreshold = 7
	defer func() { params.FullImmutabilityThreshold = previous }()
	from := mkPivot(100, common.Hash{})
	for _, tc := range []struct {
		to      uint64
		expired bool
	}{{99, false}, {100, false}, {107, false}, {108, true}} {
		if got := CatchUpExceedsRetention(from, mkPivot(tc.to, common.Hash{})); got != tc.expired {
			t.Fatalf("target %d: expired=%v, want %v", tc.to, got, tc.expired)
		}
	}
}

func TestSyncV2CatchUpRecoveryPolicy(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		for _, reason := range []string{"unavailable-bal", "expired-pivot", "missing-target-bal", "missing-gap-bal", "cancelled-missing-bal"} {
			t.Run(scheme+"/"+reason, func(t *testing.T) {
				_, treeA, elemsA := makeAccountTrieNoStorage(20, scheme)
				_, treeB, elemsB := makeAccountTrieNoStorage(30, scheme)
				var once sync.Once
				cancel := make(chan struct{})
				term := func() { once.Do(func() { close(cancel) }) }
				defer term()
				peer := newTestPeerV2("recovery", t, term)
				peer.accountTrie = treeA.Copy()
				peer.accountValues = elemsA
				s := setupSyncerV2(scheme, peer)
				defer s.db.Close()
				from := mkPivot(100, treeA.Hash())
				if err := s.Sync(from, cancel); err != nil {
					t.Fatal(err)
				}
				before := peer.nAccountRequests.Load()
				peer.trieLock.Lock()
				peer.accountTrie = treeB.Copy()
				peer.accountValues = elemsB
				peer.trieLock.Unlock()
				target := mkPivot(101, treeB.Hash())
				target.ParentHash = from.Hash()
				empty := types.EmptyBlockAccessListHash
				target.BlockAccessListHash = &empty
				peer.accessListRequestHandler = func(p *testPeerV2, id uint64, hashes []common.Hash, _ int) error {
					var values rlp.RawList[rlp.RawValue]
					for range hashes {
						values.AppendRaw(rlp.EmptyString)
					}
					return p.remote.OnAccessLists(p, id, values)
				}
				switch reason {
				case "expired-pivot":
					target.Number = new(big.Int).SetUint64(from.Number.Uint64() + params.FullImmutabilityThreshold + 1)
				case "missing-target-bal", "cancelled-missing-bal":
					target.BlockAccessListHash = nil
				case "missing-gap-bal":
					gap := mkPivot(101, treeA.Hash())
					gap.ParentHash = from.Hash()
					rawdb.WriteHeader(s.db, gap)
					target.Number = big.NewInt(102)
					target.ParentHash = gap.Hash()
				}
				if reason == "cancelled-missing-bal" {
					term()
				}
				err := s.Sync(target, cancel)
				if reason == "cancelled-missing-bal" {
					if err != ErrCancelled {
						t.Fatalf("error = %v, want cancelled", err)
					}
					progress := decodeJournal(t, s.db)
					if progress.Pivot == nil || progress.Pivot.Hash() != from.Hash() || progress.Phase != phaseComplete {
						t.Fatal("cancelled catch-up discarded the previous checkpoint")
					}
					return
				}
				if reason == "unavailable-bal" {
					if err != errAccessListUnavailable {
						t.Fatalf("error = %v, want unavailable BAL", err)
					}
					if peer.nAccessListRequests.Load() == 0 || peer.nAccountRequests.Load() != before {
						t.Fatal("unavailable BAL must return an error without restarting state download")
					}
					loader := newSyncerV2(s.db, scheme)
					loader.loadSyncStatus()
					if loader.pivot == nil || loader.pivot.Hash() != from.Hash() || loader.getPhase() != phaseComplete {
						t.Fatal("unavailable BAL discarded the previous checkpoint")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if peer.nAccountRequests.Load() <= before {
					t.Fatal("did not restart the account download")
				}
				if peer.nAccessListRequests.Load() != 0 {
					t.Fatal("unusable checkpoint requested BALs")
				}
				if s.getPhase() != phaseComplete || s.pivot.Hash() != target.Hash() {
					t.Fatal("recovery did not complete at the target pivot")
				}
				verifyTrie(scheme, s.db, target.Root, t)
				loader := newSyncerV2(s.db, scheme)
				loader.loadSyncStatus()
				if loader.getPhase() != phaseComplete || loader.pivot.Hash() != target.Hash() {
					t.Fatal("recovered state and checkpoint disagree")
				}
			})
		}
	}
}

func TestSyncV2RetriesAfterBALCancellation(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		t.Run(scheme, func(t *testing.T) {
			fix := seedCompletedSync(t, scheme)
			defer fix.db.Close()
			s := newSyncerV2(fix.db, scheme)
			peer := newTestPeerV2("retry", t, func() {})
			peer.remote, peer.accessLists = s, fix.bals
			requests := make(chan uint64, 2)
			release := make(chan struct{})
			var count atomic.Int32
			peer.accessListRequestHandler = func(p *testPeerV2, id uint64, hashes []common.Hash, max int) error {
				switch count.Add(1) {
				case 1:
					requests <- id
					return nil
				case 2:
					requests <- id
					<-release
				}
				return defaultAccessListRequestHandler(p, id, hashes, max)
			}
			if err := s.Register(peer); err != nil {
				t.Fatal(err)
			}
			start := func() (func(), <-chan error) {
				cancel := make(chan struct{})
				done := make(chan error, 1)
				go func() {
					defer close(done)
					done <- s.Sync(fix.header102, cancel)
				}()
				return sync.OnceFunc(func() { close(cancel) }), done
			}
			request := func() uint64 {
				t.Helper()
				select {
				case id := <-requests:
					return id
				case <-time.After(5 * time.Second):
					t.Fatal("BAL request was not sent")
					return 0
				}
			}
			cancel, done := start()
			defer func() { cancel(); <-done }()
			oldID := request()
			cancel()
			if err := <-done; err != ErrCancelled {
				t.Fatalf("first cycle: %v, want cancelled", err)
			}
			cancelRetry, retryDone := start()
			unblock := sync.OnceFunc(func() { close(release) })
			defer func() { unblock(); cancelRetry(); <-retryDone }()
			request()
			if err := s.OnAccessLists(peer, oldID, rlp.RawList[rlp.RawValue]{}); err != nil {
				t.Fatal(err)
			}
			s.lock.RLock()
			_, idle := s.accessListIdlers[peer.ID()]
			s.lock.RUnlock()
			if idle {
				t.Fatal("late reply made the busy peer idle")
			}
			unblock()
			select {
			case err := <-retryDone:
				if err != nil {
					t.Fatalf("retry with the same peer: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("retry did not complete")
			}
			verifyTrie(scheme, fix.db, fix.header102.Root, t)
		})
	}
}
