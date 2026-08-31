// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// The escalating ladder exists so a builder cannot burn one slot per validator
// per day at a flat 24h price. These tests pin the ladder itself, the retention
// that carries strikes between attacks, and the paths that must not feed it.

// waitForJournalStrikes waits until the asynchronous write has landed with the
// expected strike count and its temp file is gone. Polling for a non-empty file
// is not enough here: these tests seed the journal first, so it is already
// non-empty before the write under test even starts.
func waitForJournalStrikes(t *testing.T, path string, builder common.Address, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if blob, err := os.ReadFile(path); err == nil && len(blob) > 0 {
			var journal bidBlockRevokeJournal
			if json.Unmarshal(blob, &journal) == nil {
				if rec, ok := journal.Revokes[builder]; ok && rec.Strikes == want {
					if _, err := os.Stat(path + ".tmp"); os.IsNotExist(err) {
						return
					}
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for journal to record %d strikes", want)
}

func TestEscalatedRevokeDurationLadder(t *testing.T) {
	for _, tc := range []struct {
		strike int
		want   time.Duration
	}{
		{strike: 1, want: 24 * time.Hour},
		{strike: 2, want: 36 * time.Hour},
		{strike: 3, want: 48 * time.Hour},
		{strike: 5, want: 72 * time.Hour},
		{strike: 12, want: 156 * time.Hour},
		{strike: 13, want: bidBlockRevokeMaxDuration},
		{strike: 50, want: bidBlockRevokeMaxDuration},
		// Defensive: a zero or negative strike must still price as the first.
		{strike: 0, want: 24 * time.Hour},
	} {
		if got := escalatedRevokeDuration(tc.strike); got != tc.want {
			t.Errorf("strike %d: got %v, want %v", tc.strike, got, tc.want)
		}
	}
}

func TestEscalatedRevokeCapIsReachedAtStrike13(t *testing.T) {
	if escalatedRevokeDuration(12) >= bidBlockRevokeMaxDuration {
		t.Fatal("strike 12 must still be below the cap")
	}
	if escalatedRevokeDuration(13) != bidBlockRevokeMaxDuration {
		t.Fatal("strike 13 must be the first at the cap")
	}
}

func TestRevokeEscalatingGrowsAcrossRepeats(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	duration, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)
	if strike != 1 || duration != 24*time.Hour {
		t.Fatalf("first strike: got strike=%d duration=%v, want 1/24h", strike, duration)
	}

	// Attack again as soon as the lockout ends: still inside retention, so the
	// next lockout is one step longer rather than another flat 24h.
	now = now.Add(duration)
	if !m.IsAllowed(builder) {
		t.Fatal("builder should be released once the lockout elapses")
	}
	duration, strike = m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 2)
	if strike != 2 || duration != 36*time.Hour {
		t.Fatalf("second strike: got strike=%d duration=%v, want 2/36h", strike, duration)
	}

	now = now.Add(duration)
	duration, strike = m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 3)
	if strike != 3 || duration != 48*time.Hour {
		t.Fatalf("third strike: got strike=%d duration=%v, want 3/48h", strike, duration)
	}
}

func TestRevokeEscalatingResetsAfterCleanRetention(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	duration, _ := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)

	// One tick past lockout + retention: the builder stayed clean long enough to
	// be treated as fixed.
	now = now.Add(duration + bidBlockStrikeRetention + time.Second)
	gotDuration, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 2)
	if strike != 1 || gotDuration != 24*time.Hour {
		t.Fatalf("after clean retention: got strike=%d duration=%v, want 1/24h", strike, gotDuration)
	}
}

func TestRevokeEscalatingKeepsStrikeJustInsideRetention(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	duration, _ := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)

	now = now.Add(duration + bidBlockStrikeRetention - time.Second)
	_, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 2)
	if strike != 2 {
		t.Fatalf("one second before retention elapses the strike must still count, got %d", strike)
	}
}

// A cheap flat revoke must not be usable to wipe an accumulated ladder.
func TestFlatRevokeCarriesStrikesWithoutEscalating(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	duration, _ := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)
	now = now.Add(duration)

	// Flat gas-price style revoke: neither adds a strike nor clears one.
	m.RevokeFor(builder, "gas price too low", common.Hash{}, 2, bidBlockGasPriceLowRevokeDuration)
	rec, ok := getBidBlockPermissionRecord(m, builder)
	if !ok {
		t.Fatal("flat revoke should be active")
	}
	if rec.Strikes != 1 {
		t.Fatalf("flat revoke must carry the strike count, got %d", rec.Strikes)
	}

	now = now.Add(bidBlockGasPriceLowRevokeDuration)
	_, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 3)
	if strike != 2 {
		t.Fatalf("ladder must continue after a flat revoke, got strike=%d", strike)
	}
}

// A short flat revoke landing on a long active lockout must not release it.
func TestFlatRevokeDoesNotShortenActiveLockout(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)
	m.RevokeFor(builder, "gas price too low", common.Hash{}, 2, bidBlockGasPriceLowRevokeDuration)

	now = now.Add(bidBlockGasPriceLowRevokeDuration + time.Second)
	if m.IsAllowed(builder) {
		t.Fatal("the longer lockout must survive a shorter revoke landing on top of it")
	}

	now = now.Add(24 * time.Hour)
	if !m.IsAllowed(builder) {
		t.Fatal("builder should be released once the original lockout elapses")
	}
}

// Denying an already-denied builder is a natural operator action; it must not
// release one early by resetting a long escalated lockout to the base duration.
func TestManualDenyDoesNotShortenEscalatedLockout(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	// Climb to a lockout well beyond the 24h base.
	var duration time.Duration
	for i := 0; i < 5; i++ {
		duration, _ = m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, uint64(i+1))
		if i < 4 {
			now = now.Add(duration)
		}
	}
	if duration != 72*time.Hour { // 24h + 4*12h
		t.Fatalf("expected the 5th strike to be 72h, got %v", duration)
	}
	expectedReset := now.Add(duration)

	m.SetAllowed(builder, false)

	rec, ok := getBidBlockPermissionRecord(m, builder)
	if !ok {
		t.Fatal("builder must stay revoked after a manual deny")
	}
	if got := revokeResetAt(rec); !got.Equal(expectedReset) {
		t.Fatalf("manual deny moved resetAt from %v to %v", expectedReset, got)
	}

	// Just before the original reset the builder is still blocked, and the strike
	// count survived the manual deny.
	now = expectedReset.Add(-time.Second)
	if m.IsAllowed(builder) {
		t.Fatal("manual deny must not release the escalated lockout early")
	}
	now = expectedReset
	if !m.IsAllowed(builder) {
		t.Fatal("builder should be released at the original reset time")
	}
	if _, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 9); strike != 6 {
		t.Fatalf("manual deny must not add or clear a strike, got strike=%d", strike)
	}
}

func TestSetAllowedClearsStrikes(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)
	m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 2)

	// Operator judges the builder clean; the ladder starts over.
	m.SetAllowed(builder, true)
	if !m.IsAllowed(builder) {
		t.Fatal("manual release must lift the lockout")
	}
	duration, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 3)
	if strike != 1 || duration != 24*time.Hour {
		t.Fatalf("after manual release: got strike=%d duration=%v, want 1/24h", strike, duration)
	}
}

// Strikes must outlive a restart even when the lockout itself has expired,
// otherwise restarting a validator resets the ladder.
func TestStrikesSurviveRestartAfterLockoutExpiry(t *testing.T) {
	path := testJournalPath(t)
	builder := common.HexToAddress("0x1")

	now := time.Now()
	seed := bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion,
		Revokes: map[common.Address]BidBlockRevokeRecord{
			// Lockout ended an hour ago, still well inside strike retention.
			builder: {
				RevokedAt: now.Add(-25 * time.Hour),
				Duration:  bidBlockRevokeDuration,
				Reason:    testInsertChainReason,
				Strikes:   2,
			},
		},
	}
	blob, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	m := NewBidBlockPermissionManager(path)
	if !m.IsAllowed(builder) {
		t.Fatal("an expired lockout must not block after restart")
	}
	duration, strike := m.RevokeEscalating(builder, testInsertChainReason, common.Hash{}, 1)
	if strike != 3 || duration != 48*time.Hour {
		t.Fatalf("ladder must resume after restart: got strike=%d duration=%v, want 3/48h", strike, duration)
	}
	waitForJournalStrikes(t, path, builder, 3)
}

// Records past their retention are dropped rather than accumulating forever.
func TestStaleRecordsPrunedAndNotRestored(t *testing.T) {
	path := testJournalPath(t)
	stale := common.HexToAddress("0x1")

	now := time.Now()
	seed := bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion,
		Revokes: map[common.Address]BidBlockRevokeRecord{
			stale: {
				RevokedAt: now.Add(-bidBlockRevokeDuration - bidBlockStrikeRetention - time.Hour),
				Duration:  bidBlockRevokeDuration,
				Reason:    testInsertChainReason,
				Strikes:   5,
			},
		},
	}
	blob, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	m := NewBidBlockPermissionManager(path)
	if _, ok := getBidBlockPermissionRecord(m, stale); ok {
		t.Fatal("record past retention must not be active")
	}
	_, strike := m.RevokeEscalating(stale, testInsertChainReason, common.Hash{}, 1)
	if strike != 1 {
		t.Fatalf("strikes past retention must not be inherited, got %d", strike)
	}
	// The revoke above kicked off an asynchronous write; let it land before the
	// temp dir is torn down.
	waitForJournalStrikes(t, path, stale, 1)
}
