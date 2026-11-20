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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestSyncV2CatchUpMissingBALCommitment(t *testing.T) {
	for _, missingTarget := range []bool{true, false} {
		name := "gap"
		if missingTarget {
			name = "target"
		}
		t.Run(name, func(t *testing.T) {
			const scheme = rawdb.PathScheme
			_, treeA, elemsA := makeAccountTrieNoStorage(20, scheme)
			_, treeB, elemsB := makeAccountTrieNoStorage(30, scheme)
			cancel := make(chan struct{})
			stop := sync.OnceFunc(func() { close(cancel) })
			defer stop()
			peer := newTestPeerV2("recovery", t, stop)
			peer.accountTrie, peer.accountValues = treeA.Copy(), elemsA
			s := setupSyncerV2(scheme, peer)
			defer s.db.Close()
			from := mkPivot(100, treeA.Hash())
			if err := s.Sync(from, cancel); err != nil {
				t.Fatal(err)
			}
			peer.trieLock.Lock()
			peer.accountTrie, peer.accountValues = treeB.Copy(), elemsB
			peer.trieLock.Unlock()
			target := mkPivot(101, treeB.Hash())
			target.ParentHash = from.Hash()
			if !missingTarget {
				gap := mkPivot(101, treeA.Hash())
				gap.ParentHash = from.Hash()
				rawdb.WriteHeader(s.db, gap)
				target.Number.SetUint64(102)
				target.ParentHash = gap.Hash()
				empty := types.EmptyBlockAccessListHash
				target.BlockAccessListHash = &empty
			}
			if err := s.Sync(target, cancel); err != nil {
				t.Fatal(err)
			}
			if peer.nAccessListRequests.Load() != 0 {
				t.Fatal("catch-up sent a BAL request without a commitment")
			}
			progress := decodeJournal(t, s.db)
			if progress.Phase != phaseComplete || progress.Pivot == nil || progress.Pivot.Hash() != target.Hash() {
				t.Fatal("state download did not complete at the target")
			}
			verifyTrie(scheme, s.db, target.Root, t)
		})
	}
}

func TestSyncV2RetriesAfterBALCancellation(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		t.Run(scheme, func(t *testing.T) {
			fix := seedCompletedSync(t, scheme)
			defer fix.db.Close()
			s := newSyncerV2(fix.db, scheme)
			cancel, retry := make(chan struct{}), make(chan struct{})
			stop := sync.OnceFunc(func() { close(cancel) })
			stopRetry := sync.OnceFunc(func() { close(retry) })
			var count atomic.Int32
			var oldID atomic.Uint64
			peer := newTestPeerV2("retry", t, stopRetry)
			peer.remote, peer.accessLists = s, fix.bals
			peer.accessListRequestHandler = func(p *testPeerV2, id uint64, hashes []common.Hash, max int) error {
				switch count.Add(1) {
				case 1:
					oldID.Store(id)
					stop()
					return nil
				case 2:
					if err := s.OnAccessLists(p, oldID.Load(), rlp.RawList[rlp.RawValue]{}); err != nil {
						t.Errorf("late reply returned %v", err)
					}
					s.lock.RLock()
					_, idle := s.accessListIdlers[p.ID()]
					s.lock.RUnlock()
					if idle {
						t.Error("late reply made the busy peer idle")
					}
				}
				return defaultAccessListRequestHandler(p, id, hashes, max)
			}
			if err := s.Register(peer); err != nil {
				t.Fatal(err)
			}
			firstDeadline := time.AfterFunc(5*time.Second, stop)
			defer firstDeadline.Stop()
			if err := s.Sync(fix.header102, cancel); err != ErrCancelled || oldID.Load() == 0 {
				t.Fatalf("first cycle did not cancel an in-flight BAL request: %v", err)
			}
			retryDeadline := time.AfterFunc(5*time.Second, stopRetry)
			defer retryDeadline.Stop()
			if err := s.Sync(fix.header102, retry); err != nil {
				t.Fatalf("retry with the same peer: %v", err)
			}
			verifyTrie(scheme, fix.db, fix.header102.Root, t)
		})
	}
}
