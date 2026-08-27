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

package bsc

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

func newRateLimitTestPeer() *Peer {
	p := &Peer{
		id:              "rate-limit-test-peer",
		blockServeBegin: time.Now(),
	}
	p.logger = log.New("peer", p.id)
	return p
}

// TestBlockServeRateLimit verifies the per-peer GetBlocksByRange serving budget:
// a fresh peer is under limit, charging beyond the rolling budget trips it, and
// the period rolls over after secondsPerPeriod.
func TestBlockServeRateLimit(t *testing.T) {
	p := newRateLimitTestPeer()
	budget := blockServeBudget()

	if p.IsOverBlockServeLimit() {
		t.Fatalf("a fresh peer must be under the serve limit")
	}

	// Serving up to the budget stays under the limit.
	p.ChargeBlockServe(budget)
	if p.IsOverBlockServeLimit() {
		t.Fatalf("serving exactly the budget must not trip the limit")
	}

	// One more byte crosses it.
	p.ChargeBlockServe(1)
	if !p.IsOverBlockServeLimit() {
		t.Fatalf("serving beyond the budget must trip the limit")
	}

	// Simulate the rolling window elapsing: the next check resets the period
	// and the peer is served again.
	p.blockServeBegin = time.Now().Add(-time.Duration(secondsPerPeriod+1) * time.Second)
	if p.IsOverBlockServeLimit() {
		t.Fatalf("after the period rolls over the peer must be under the limit again")
	}
	if p.blockServeCounter != 0 {
		t.Fatalf("period rollover must reset the counter, got %d", p.blockServeCounter)
	}
}

// TestBlockServeRateLimit_ChargeRollover verifies ChargeBlockServe also rolls
// the period over, so a burst that spans a period boundary is not double-counted.
func TestBlockServeRateLimit_ChargeRollover(t *testing.T) {
	p := newRateLimitTestPeer()
	p.ChargeBlockServe(blockServeBudget())

	// Move the window start into the past, then charge again: the stale counter
	// must be reset before the new charge is applied.
	p.blockServeBegin = time.Now().Add(-time.Duration(secondsPerPeriod+1) * time.Second)
	p.ChargeBlockServe(1)
	if p.blockServeCounter != 1 {
		t.Fatalf("charge after rollover must start from a reset counter, got %d", p.blockServeCounter)
	}
	if p.IsOverBlockServeLimit() {
		t.Fatalf("a single byte in a fresh period must be under the limit")
	}
}
