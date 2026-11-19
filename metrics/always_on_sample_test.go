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

package metrics

import "testing"

// TestAlwaysOnExpDecaySampleRecordsWhenDisabled checks that the always-on
// sample records values while the global metrics-enabled flag is off, whereas
// the regular gated sample does not. This is the property eth_health relies on
// to work without --metrics (#3859).
func TestAlwaysOnExpDecaySampleRecordsWhenDisabled(t *testing.T) {
	if Enabled() {
		t.Skip("metrics enabled; test targets the disabled configuration")
	}
	gated := NewExpDecaySample(1028, 0.015)
	gated.Update(42)
	if gated.Snapshot().Count() != 0 {
		t.Fatal("gated sample recorded a value while metrics disabled")
	}
	always := NewAlwaysOnExpDecaySample(1028, 0.015)
	always.Update(42)
	snap := always.Snapshot()
	if snap.Count() != 1 {
		t.Fatalf("always-on sample count = %d, want 1", snap.Count())
	}
	if got := snap.Percentile(0.5); got != 42 {
		t.Fatalf("always-on sample median = %v, want 42", got)
	}
}
