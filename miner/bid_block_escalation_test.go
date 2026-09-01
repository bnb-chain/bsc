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

var escalationTestBuilder = common.HexToAddress("0x1")

func newEscalationTestManager() (*BidBlockPermissionManager, *time.Time) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	m := NewBidBlockPermissionManager("")
	setBidBlockPermissionClock(m, func() time.Time { return now })
	return m, &now
}

func requireEscalatedRevoke(t *testing.T, m *BidBlockPermissionManager, wantStrike int, wantDuration time.Duration) {
	t.Helper()
	duration, strike := m.RevokeEscalating(escalationTestBuilder, testInsertChainReason, common.Hash{}, uint64(wantStrike))
	if strike != wantStrike || duration != wantDuration {
		t.Fatalf("got strike=%d duration=%v, want %d/%v", strike, duration, wantStrike, wantDuration)
	}
}

func seedRevokeJournal(t *testing.T, path string, rec BidBlockRevokeRecord) {
	t.Helper()
	blob, err := json.Marshal(bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion,
		Revokes: map[common.Address]BidBlockRevokeRecord{escalationTestBuilder: rec},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForJournalStrikes(t *testing.T, path string, want int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		blob, err := os.ReadFile(path)
		var journal bidBlockRevokeJournal
		if err == nil && json.Unmarshal(blob, &journal) == nil && journal.Revokes[escalationTestBuilder].Strikes == want {
			if _, err := os.Stat(path + ".tmp"); os.IsNotExist(err) {
				return
			}
		}
	}
	t.Fatalf("timed out waiting for journal to record %d strikes", want)
}

func TestEscalatedRevokeDuration(t *testing.T) {
	for _, tc := range []struct {
		strike int
		want   time.Duration
	}{
		{0, 24 * time.Hour}, {1, 24 * time.Hour}, {2, 36 * time.Hour},
		{3, 48 * time.Hour}, {5, 72 * time.Hour}, {12, 156 * time.Hour},
		{13, bidBlockRevokeMaxDuration}, {50, bidBlockRevokeMaxDuration},
	} {
		if got := escalatedRevokeDuration(tc.strike); got != tc.want {
			t.Errorf("strike %d: got %v, want %v", tc.strike, got, tc.want)
		}
	}
}

func TestRevokeEscalationAndRetention(t *testing.T) {
	m, now := newEscalationTestManager()
	for strike, want := range []time.Duration{24 * time.Hour, 36 * time.Hour, 48 * time.Hour} {
		requireEscalatedRevoke(t, m, strike+1, want)
		*now = now.Add(want)
		if !m.IsAllowed(escalationTestBuilder) {
			t.Fatalf("strike %d did not expire", strike+1)
		}
	}

	for _, tc := range []struct {
		name       string
		offset     time.Duration
		wantStrike int
	}{
		{"retained", bidBlockStrikeRetention - time.Second, 2},
		{"reset", bidBlockStrikeRetention + time.Second, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm, testNow := newEscalationTestManager()
			requireEscalatedRevoke(t, tm, 1, 24*time.Hour)
			*testNow = testNow.Add(24*time.Hour + tc.offset)
			_, strike := tm.RevokeEscalating(escalationTestBuilder, testInsertChainReason, common.Hash{}, 2)
			if strike != tc.wantStrike {
				t.Fatalf("got strike %d, want %d", strike, tc.wantStrike)
			}
		})
	}
}

func TestNonEscalatingRevokesPreserveLadder(t *testing.T) {
	m, now := newEscalationTestManager()
	requireEscalatedRevoke(t, m, 1, 24*time.Hour)
	resetAt := now.Add(24 * time.Hour)

	m.RevokeFor(escalationTestBuilder, "gas price too low", common.Hash{}, 2, bidBlockGasPriceLowRevokeDuration)
	rec, _ := getBidBlockPermissionRecord(m, escalationTestBuilder)
	if rec.Strikes != 1 || !revokeResetAt(rec).Equal(resetAt) {
		t.Fatalf("flat revoke changed ladder: %+v", rec)
	}

	*now = resetAt
	requireEscalatedRevoke(t, m, 2, 36*time.Hour)
	resetAt = now.Add(36 * time.Hour)
	m.SetAllowed(escalationTestBuilder, false)
	rec, _ = getBidBlockPermissionRecord(m, escalationTestBuilder)
	if rec.Strikes != 2 || !revokeResetAt(rec).Equal(resetAt) {
		t.Fatalf("manual deny changed ladder: %+v", rec)
	}

	m.SetAllowed(escalationTestBuilder, true)
	if !m.IsAllowed(escalationTestBuilder) {
		t.Fatal("manual allow did not clear lockout")
	}
	requireEscalatedRevoke(t, m, 1, 24*time.Hour)
}

func TestStrikeJournalRetention(t *testing.T) {
	for _, tc := range []struct {
		name       string
		age        time.Duration
		oldStrikes int
		wantStrike int
	}{
		{"retained", 25 * time.Hour, 2, 3},
		{"stale", bidBlockRevokeDuration + bidBlockStrikeRetention + time.Hour, 5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testJournalPath(t)
			seedRevokeJournal(t, path, BidBlockRevokeRecord{
				RevokedAt: time.Now().Add(-tc.age), Duration: bidBlockRevokeDuration,
				Reason: testInsertChainReason, Strikes: tc.oldStrikes,
			})
			m := NewBidBlockPermissionManager(path)
			if !m.IsAllowed(escalationTestBuilder) {
				t.Fatal("expired lockout was restored")
			}
			duration, strike := m.RevokeEscalating(escalationTestBuilder, testInsertChainReason, common.Hash{}, 1)
			if strike != tc.wantStrike || duration != escalatedRevokeDuration(tc.wantStrike) {
				t.Fatalf("got strike=%d duration=%v", strike, duration)
			}
			waitForJournalStrikes(t, path, tc.wantStrike)
		})
	}
}
