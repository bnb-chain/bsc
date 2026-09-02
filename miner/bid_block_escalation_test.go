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

func requireRevokeForViolation(t *testing.T, m *BidBlockPermissionManager, wantCount int, wantDuration time.Duration) {
	t.Helper()
	duration, violationCount := m.RevokeForViolation(escalationTestBuilder, testInsertChainReason, common.Hash{}, uint64(wantCount))
	if violationCount != wantCount || duration != wantDuration {
		t.Fatalf("got violationCount=%d duration=%v, want %d/%v", violationCount, duration, wantCount, wantDuration)
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

func waitForJournalViolations(t *testing.T, path string, want int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		blob, err := os.ReadFile(path)
		var journal bidBlockRevokeJournal
		if err == nil && json.Unmarshal(blob, &journal) == nil && journal.Revokes[escalationTestBuilder].ViolationCount == want {
			if _, err := os.Stat(path + ".tmp"); os.IsNotExist(err) {
				return
			}
		}
	}
	t.Fatalf("timed out waiting for journal to record %d violations", want)
}

func TestEscalatedRevokeDuration(t *testing.T) {
	for _, tc := range []struct {
		violationCount int
		want           time.Duration
	}{
		{0, 24 * time.Hour}, {1, 24 * time.Hour}, {2, 36 * time.Hour},
		{3, 48 * time.Hour}, {5, 72 * time.Hour}, {12, 156 * time.Hour},
		{13, bidBlockRevokeMaxDuration}, {50, bidBlockRevokeMaxDuration},
	} {
		if got := escalatedRevokeDuration(tc.violationCount); got != tc.want {
			t.Errorf("violation count %d: got %v, want %v", tc.violationCount, got, tc.want)
		}
	}
}

func TestRevokeEscalationAndRetention(t *testing.T) {
	m, now := newEscalationTestManager()
	for violation, want := range []time.Duration{24 * time.Hour, 36 * time.Hour, 48 * time.Hour} {
		requireRevokeForViolation(t, m, violation+1, want)
		*now = now.Add(want)
		if !m.IsAllowed(escalationTestBuilder) {
			t.Fatalf("violation %d did not expire", violation+1)
		}
	}

	for _, tc := range []struct {
		name      string
		offset    time.Duration
		wantCount int
	}{
		{"retained", bidBlockViolationRetention - time.Second, 2},
		{"reset", bidBlockViolationRetention + time.Second, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm, testNow := newEscalationTestManager()
			requireRevokeForViolation(t, tm, 1, 24*time.Hour)
			*testNow = testNow.Add(24*time.Hour + tc.offset)
			_, violationCount := tm.RevokeForViolation(escalationTestBuilder, testInsertChainReason, common.Hash{}, 2)
			if violationCount != tc.wantCount {
				t.Fatalf("got violation count %d, want %d", violationCount, tc.wantCount)
			}
		})
	}
}

func TestViolationJournalRetention(t *testing.T) {
	for _, tc := range []struct {
		name      string
		age       time.Duration
		oldCount  int
		wantCount int
	}{
		{"retained", 25 * time.Hour, 2, 3},
		{"stale", bidBlockRevokeDuration + bidBlockViolationRetention + time.Hour, 5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testJournalPath(t)
			seedRevokeJournal(t, path, BidBlockRevokeRecord{
				RevokedAt: time.Now().Add(-tc.age), Duration: bidBlockRevokeDuration,
				Reason: testInsertChainReason, ViolationCount: tc.oldCount,
			})
			m := NewBidBlockPermissionManager(path)
			if !m.IsAllowed(escalationTestBuilder) {
				t.Fatal("expired lockout was restored")
			}
			duration, violationCount := m.RevokeForViolation(escalationTestBuilder, testInsertChainReason, common.Hash{}, 1)
			if violationCount != tc.wantCount || duration != escalatedRevokeDuration(tc.wantCount) {
				t.Fatalf("got violationCount=%d duration=%v", violationCount, duration)
			}
			waitForJournalViolations(t, path, tc.wantCount)
		})
	}
}
