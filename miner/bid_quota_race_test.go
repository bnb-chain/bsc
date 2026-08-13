// Copyright 2024 The go-ethereum Authors
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

package miner

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestReservePendingEnforcesQuotaUnderConcurrency verifies that the atomic
// check-and-insert in ReservePending prevents concurrent submissions from the
// same builder from exceeding maxBidsPerBuilder. Each goroutine uses a distinct
// bid hash (so the duplicate check does not interfere) and all start together
// to maximize the check/insert race window that the split CheckPending/AddPending
// flow was vulnerable to.
func TestReservePendingEnforcesQuotaUnderConcurrency(t *testing.T) {
	const (
		maxBids     = 2
		blockNumber = uint64(100)
		goroutines  = 64
	)
	b := &bidSimulator{
		pending:           make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		maxBidsPerBuilder: maxBids,
	}
	builder := common.HexToAddress("0x1")

	var (
		start    = make(chan struct{})
		wg       sync.WaitGroup
		accepted atomic.Int64
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var bidHash common.Hash // distinct per goroutine, no shared state
			bidHash[0] = byte(i)
			bidHash[1] = byte(i >> 8)
			<-start
			if err := b.ReservePending(blockNumber, builder, bidHash); err == nil {
				accepted.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := accepted.Load(); got != maxBids {
		t.Fatalf("ReservePending admitted %d bids concurrently, want exactly %d (quota bypassed)", got, maxBids)
	}
	b.pendingMu.RLock()
	pendingCount := len(b.pending[blockNumber][builder])
	b.pendingMu.RUnlock()
	if pendingCount != maxBids {
		t.Fatalf("pending map holds %d entries, want %d", pendingCount, maxBids)
	}
}
