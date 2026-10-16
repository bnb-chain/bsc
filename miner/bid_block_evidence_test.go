// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func testBuilder() common.Address { return common.HexToAddress("0xb0") }

func sealerN(n int) common.Address {
	return common.BigToAddress(big.NewInt(int64(n)))
}

func newTestTracker(now *time.Time) *badBidBlockTracker {
	t := newBadBidBlockTracker()
	t.clock = func() time.Time { return *now }
	return t
}

func TestBadBidBlockTracker(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := newTestTracker(&now)
	builder := testBuilder()

	tracker.add(builder, sealerN(1), true)
	tracker.add(builder, sealerN(1), true)
	cabinet, total := tracker.add(builder, sealerN(2), false)
	if cabinet != 1 || total != 2 {
		t.Fatalf("got cabinet=%d total=%d, want 1/2", cabinet, total)
	}

	now = now.Add(badBidBlockEvidenceWindow + time.Second)
	cabinet, total = tracker.add(builder, sealerN(3), false)
	if cabinet != 0 || total != 1 {
		t.Fatalf("stale sightings must be dropped: got cabinet=%d total=%d, want 0/1", cabinet, total)
	}
}

func TestMajorityThreshold(t *testing.T) {
	tests := []struct {
		validators int
		want       int
	}{
		{validators: 21, want: 11},
		{validators: 45, want: 23},
		{validators: 3, want: 2},
		{validators: 4, want: 3},
	}
	for _, test := range tests {
		if got := majorityThreshold(test.validators); got != test.want {
			t.Errorf("majorityThreshold(%d) = %d, want %d", test.validators, got, test.want)
		}
	}
}
