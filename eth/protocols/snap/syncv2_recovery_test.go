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
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestSyncV2CatchUpMissingBALCommitment(t *testing.T) {
	const scheme = rawdb.PathScheme
	fix := seedCompletedSync(t, scheme)
	defer fix.db.Close()
	gap := mkPivot(101, fix.pivotA.Root)
	gap.ParentHash = fix.pivotA.Hash()
	rawdb.WriteHeader(fix.db, gap)
	target := types.CopyHeader(fix.header102)
	target.ParentHash = gap.Hash()

	s := newSyncerV2(fix.db, scheme)
	cancel := make(chan struct{})
	peer := newTestPeerV2("recovery", t, sync.OnceFunc(func() { close(cancel) }))
	peer.remote, peer.accountTrie, peer.accountValues = s, fix.trie102.Copy(), fix.elems102
	if err := s.Register(peer); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(target, cancel); err != nil {
		t.Fatal(err)
	}
	if peer.nAccessListRequests.Load() != 0 {
		t.Fatal("catch-up sent a BAL request without a commitment")
	}
	verifyTrie(scheme, fix.db, target.Root, t)
}

func TestSyncV2RetriesAfterBALCancellation(t *testing.T) {
	const scheme = rawdb.PathScheme
	fix := seedCompletedSync(t, scheme)
	defer fix.db.Close()
	s := newSyncerV2(fix.db, scheme)
	cancel, retry := make(chan struct{}), make(chan struct{})
	stop := sync.OnceFunc(func() { close(cancel) })
	stopRetry := sync.OnceFunc(func() { close(retry) })
	deadline := time.AfterFunc(5*time.Second, func() {
		stop()
		stopRetry()
	})
	defer deadline.Stop()
	peer := newTestPeerV2("retry", t, stopRetry)
	peer.remote, peer.accessLists = s, fix.bals
	peer.accessListRequestHandler = func(p *testPeerV2, id uint64, hashes []common.Hash, max int) error {
		if p.nAccessListRequests.Load() == 1 {
			stop()
			return nil
		}
		return defaultAccessListRequestHandler(p, id, hashes, max)
	}
	if err := s.Register(peer); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(fix.header102, cancel); err != ErrCancelled || peer.nAccessListRequests.Load() == 0 {
		t.Fatalf("first cycle did not cancel an in-flight BAL request: %v", err)
	}
	if err := s.Sync(fix.header102, retry); err != nil {
		t.Fatalf("retry with the same peer: %v", err)
	}
	verifyTrie(scheme, fix.db, fix.header102.Root, t)
}
