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

package rpc

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

// TestRpcServingTimerRecordsWithoutMetrics is a regression test for #3859:
// eth_health derives its verdict from RpcServingTimer percentiles, so the
// timer must record serving latency even when metrics collection is disabled.
// Before the fix the gated sample never recorded anything without --metrics
// and eth_health always reported healthy, even under multi-second load.
func TestRpcServingTimerRecordsWithoutMetrics(t *testing.T) {
	if metrics.Enabled() {
		t.Skip("metrics enabled; regression targets the disabled configuration")
	}
	RpcServingTimer.Update(7 * time.Second)
	snap := RpcServingTimer.Snapshot()
	if snap.Count() == 0 {
		t.Fatal("RpcServingTimer recorded nothing with metrics disabled")
	}
	if p := snap.Percentile(0.75); p < float64(5*time.Second) {
		t.Fatalf("75th percentile = %v ns, want >= 5s after a 7s request", p)
	}
}
