// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/miner/builderclient"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
)

// testInsertChainReason is a placeholder used by tests where the specific
// InsertChain error text doesn't matter — production passes
// "InsertChain err: <err.Error()>" here.
const testInsertChainReason = "InsertChain err: test"

func getBidBlockPermissionRecord(m *BidBlockPermissionManager, builder common.Address) (BidBlockRevokeRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeRecord(builder, m.clock())
}

func setBidBlockPermissionClock(m *BidBlockPermissionManager, f func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clock = f
}

func TestBidBlockPermission_DefaultActive(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")
	if !m.IsAllowed(builder) {
		t.Fatal("default state should be Active for any builder")
	}
	if _, ok := getBidBlockPermissionRecord(m, builder); ok {
		t.Fatal("no record expected for fresh builder")
	}
}

func TestBidBlockPermission_RevokeForCustomDuration(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	setBidBlockPermissionClock(m, func() time.Time { return now })
	m.RevokeFor(builder, errBidBlockAverageGasPriceTooLow.Error(), common.Hash{}, 1, bidBlockGasPriceLowRevokeDuration)

	status := m.GetStatus(builder)
	if status.Allowed {
		t.Fatal("builder should be revoked")
	}
	if want := now.Add(bidBlockGasPriceLowRevokeDuration); !status.ResetAt.Equal(want) {
		t.Fatalf("resetAt: got %s, want %s", status.ResetAt, want)
	}

	setBidBlockPermissionClock(m, func() time.Time { return now.Add(bidBlockGasPriceLowRevokeDuration) })
	if !m.IsAllowed(builder) {
		t.Fatal("gas price low revoke should expire after the custom duration")
	}
}

func TestBidBlockPermission_ConcurrentAccess(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builders := []common.Address{
		common.HexToAddress("0xa"),
		common.HexToAddress("0xb"),
		common.HexToAddress("0xc"),
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		b := builders[i%len(builders)]
		go func() { defer wg.Done(); m.IsAllowed(b) }()
		go func() {
			defer wg.Done()
			m.RevokeFor(b, testInsertChainReason, common.Hash{}, 1, bidBlockRevokeDuration)
		}()
		go func() { defer wg.Done(); getBidBlockPermissionRecord(m, b) }()
	}
	wg.Wait()
}

func TestBidBlockPermission_ActiveRevokeCount(t *testing.T) {
	m := NewBidBlockPermissionManager("")

	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("empty manager: got %d, want 0", got)
	}

	revokeTime := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	after24h := revokeTime.Add(24 * time.Hour)
	setBidBlockPermissionClock(m, func() time.Time { return revokeTime })

	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")
	m.RevokeFor(a, testInsertChainReason, common.Hash{}, 1, bidBlockRevokeDuration)
	m.RevokeFor(b, RevokeReasonManual, common.Hash{}, 2, bidBlockRevokeDuration)

	if got := m.ActiveRevokeCount(); got != 2 {
		t.Fatalf("two revoked: got %d, want 2", got)
	}

	setBidBlockPermissionClock(m, func() time.Time { return after24h })
	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("after revokedAt + 24h: got %d, want 0 (entries are stale, not active)", got)
	}
}

func TestBidBlockPermission_GetStatus(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")
	now := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	resetAt := now.Add(24 * time.Hour)
	setBidBlockPermissionClock(m, func() time.Time { return now })

	status := m.GetStatus(builder)
	if !status.Allowed {
		t.Fatal("fresh builder should be allowed")
	}
	if !status.ResetAt.IsZero() {
		t.Fatalf("allowed status should not set resetAt: got %s", status.ResetAt)
	}

	hash := common.HexToHash("0xabc")
	m.RevokeFor(builder, testInsertChainReason, hash, 100, bidBlockRevokeDuration)
	status = m.GetStatus(builder)
	if status.Allowed {
		t.Fatal("revoked builder should not be allowed")
	}
	if status.Reason != testInsertChainReason {
		t.Fatalf("reason: got %s", status.Reason)
	}
	if status.BlockHash != hash || status.BlockNum != 100 || !status.RevokedAt.Equal(now) || !status.ResetAt.Equal(resetAt) {
		t.Fatalf("status mismatch: %#v", status)
	}
}

func TestBidBlockAdmission_RevokedDoesNotConsumeQuota(t *testing.T) {
	permMgr := NewBidBlockPermissionManager("")
	b := &bidSimulator{
		builders:          make(map[common.Address]*builderclient.Client),
		pending:           make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		maxBidsPerBuilder: 2,
	}

	builder := common.HexToAddress("0x1")
	const blockNum uint64 = 100

	b.builders[builder] = nil
	permMgr.RevokeFor(builder, testInsertChainReason, common.Hash{}, blockNum-1, bidBlockRevokeDuration)

	if !b.ExistBuilder(builder) {
		t.Fatal("registered builder must pass ExistBuilder")
	}
	if permMgr.IsAllowed(builder) {
		t.Fatal("revoked builder must fail permission check")
	}

	b.pendingMu.RLock()
	pendingForBlock := b.pending[blockNum]
	b.pendingMu.RUnlock()
	if len(pendingForBlock) != 0 {
		t.Fatalf("revoked admission must not touch pending map; got %d entries", len(pendingForBlock))
	}

	other := common.HexToAddress("0x2")
	otherHash := common.HexToHash("0xbeef")
	if err := b.ReservePending(blockNum, other, otherHash); err != nil {
		t.Fatalf("active builder should pass ReservePending: %v", err)
	}

	b.pendingMu.RLock()
	otherCount := len(b.pending[blockNum][other])
	revokedCount := len(b.pending[blockNum][builder])
	b.pendingMu.RUnlock()
	if otherCount != 1 {
		t.Fatalf("active builder should have 1 pending entry; got %d", otherCount)
	}
	if revokedCount != 0 {
		t.Fatalf("revoked builder should have 0 pending entries; got %d", revokedCount)
	}
}

func TestBidBlockAdmission_DisabledDoesNotConsumeQuota(t *testing.T) {
	for _, tc := range []struct {
		name            string
		mevEnabled      bool
		bidBlockEnabled bool
	}{
		{name: "BidBlock disabled", mevEnabled: true, bidBlockEnabled: false},
		{name: "MEV disabled", mevEnabled: false, bidBlockEnabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner := &Miner{
				worker: &worker{config: &minerconfig.Config{
					Mev: minerconfig.MevConfig{
						Enabled:         &tc.mevEnabled,
						BidBlockEnabled: &tc.bidBlockEnabled,
					},
				}},
				bidSimulator: &bidSimulator{
					pending: make(map[uint64]map[common.Address]map[common.Hash]struct{}),
				},
			}

			_, err := miner.SendBidBlock(context.Background(), &buildertypes.BidBlockArgs{})
			if err == nil || !strings.Contains(err.Error(), "BidBlock disabled") {
				t.Fatalf("expected BidBlock disabled error, got %v", err)
			}
			if len(miner.bidSimulator.pending) != 0 {
				t.Fatalf("disabled SendBidBlock must not touch pending map; got %d entries", len(miner.bidSimulator.pending))
			}
		})
	}
}

func TestMinerBidBlockPermission_UsesWorkerManager(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	miner := &Miner{worker: &worker{permMgr: m}}
	builder := common.HexToAddress("0x1")

	m.RevokeFor(builder, testInsertChainReason, common.Hash{}, 1, bidBlockRevokeDuration)
	if miner.GetBidBlockPermission(builder).Allowed {
		t.Fatal("miner should report worker revoke")
	}
}

func TestBidBlockPermission_SetAllowed_Deny(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	m.SetAllowed(builder, false)
	if m.IsAllowed(builder) {
		t.Fatal("builder should be denied after SetAllowed(false)")
	}
	rec, ok := getBidBlockPermissionRecord(m, builder)
	if !ok {
		t.Fatal("record expected after SetAllowed(false)")
	}
	if rec.Reason != RevokeReasonManual {
		t.Fatalf("reason: got %q, want %q", rec.Reason, RevokeReasonManual)
	}
}

func TestBidBlockPermission_SetAllowed_Clear(t *testing.T) {
	m := NewBidBlockPermissionManager("")
	builder := common.HexToAddress("0x1")

	m.RevokeFor(builder, testInsertChainReason, common.HexToHash("0xabc"), 100, bidBlockRevokeDuration)
	m.SetAllowed(builder, true)
	if !m.IsAllowed(builder) {
		t.Fatal("manual SetAllowed(true) should override revoke")
	}
	if _, ok := getBidBlockPermissionRecord(m, builder); ok {
		t.Fatal("record should be cleared")
	}
}

// testJournalPath returns a journal file path inside a fresh temp dir.
func testJournalPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "bidblockrevokes.json")
}

// waitForBidBlockPersist polls until the asynchronous persistence has written a
// non-empty journal file, or fails the test after a short deadline.
func waitForBidBlockPersist(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if blob, _ := os.ReadFile(path); len(blob) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for BidBlock revokes to persist")
}

// TestBidBlockPermission_SurvivesRestart is the regression for the reported
// gap: a revoke persisted by one manager must still be in effect (with the same
// resetAt) after a fresh manager is constructed over the same journal, as
// happens when the validator process restarts within the lockout window.
func TestBidBlockPermission_SurvivesRestart(t *testing.T) {
	path := testJournalPath(t)
	builder := common.HexToAddress("0x1")
	hash := common.HexToHash("0xabc")

	m1 := NewBidBlockPermissionManager(path)
	m1.RevokeFor(builder, testInsertChainReason, hash, 100, bidBlockRevokeDuration)
	waitForBidBlockPersist(t, path)
	want := m1.GetStatus(builder)

	// Simulate a restart: a brand-new manager over the same journal.
	m2 := NewBidBlockPermissionManager(path)
	if m2.IsAllowed(builder) {
		t.Fatal("revoke must survive a restart within its window")
	}
	got := m2.GetStatus(builder)
	if !got.ResetAt.Equal(want.ResetAt) {
		t.Fatalf("resetAt changed across restart: got %v, want %v", got.ResetAt, want.ResetAt)
	}
	if !got.RevokedAt.Equal(want.RevokedAt) || got.Reason != want.Reason || got.BlockHash != want.BlockHash || got.BlockNum != want.BlockNum {
		t.Fatalf("restored record differs from persisted one: got %+v, want %+v", got, want)
	}
}

// TestBidBlockPermission_ExpiredNotRestored verifies that a revoke whose window
// elapsed while the process was down is dropped on load, while a still-active
// one is restored. The journal is seeded directly so the elapsed time is
// deterministic and does not depend on wall-clock waits.
func TestBidBlockPermission_ExpiredNotRestored(t *testing.T) {
	path := testJournalPath(t)
	active := common.HexToAddress("0x1")
	expired := common.HexToAddress("0x2")

	now := time.Now()
	seed := bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion,
		Revokes: map[common.Address]BidBlockRevokeRecord{
			active:  {RevokedAt: now.Add(-1 * time.Hour), Duration: bidBlockRevokeDuration, Reason: "active"},
			expired: {RevokedAt: now.Add(-25 * time.Hour), Duration: bidBlockRevokeDuration, Reason: "expired"},
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
	if m.IsAllowed(active) {
		t.Fatal("still-active revoke must be restored")
	}
	if !m.IsAllowed(expired) {
		t.Fatal("revoke expired during downtime must not be restored")
	}
}

// TestBidBlockPermission_LoadTolERatesBadState verifies that a missing or
// corrupt journal leaves the manager empty instead of panicking.
func TestBidBlockPermission_LoadToleratesBadState(t *testing.T) {
	builder := common.HexToAddress("0x1")

	// Corrupt journal: start empty, no panic.
	path := testJournalPath(t)
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	m := NewBidBlockPermissionManager(path)
	if !m.IsAllowed(builder) {
		t.Fatal("a corrupt journal must yield an empty manager")
	}

	// Missing file (fresh datadir): start empty, no panic.
	m2 := NewBidBlockPermissionManager(testJournalPath(t))
	if !m2.IsAllowed(builder) {
		t.Fatal("a missing journal must yield an empty manager")
	}
}

// TestBidBlockPermission_RejectsUnknownVersion pins the version envelope: a
// journal written by a future build must be refused outright rather than
// decoded with this build's interpretation of the fields. Starting empty only
// costs the remaining lockout windows, which self-expire; misreading a changed
// field could revoke or release the wrong builders.
func TestBidBlockPermission_RejectsUnknownVersion(t *testing.T) {
	builder := common.HexToAddress("0x1")
	rec := BidBlockRevokeRecord{
		RevokedAt: time.Now(),
		Duration:  bidBlockRevokeDuration,
		Reason:    testInsertChainReason,
	}

	// A future version carrying an otherwise-valid record: must NOT be applied.
	future := testJournalPath(t)
	blob, err := json.Marshal(bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion + 1,
		Revokes: map[common.Address]BidBlockRevokeRecord{builder: rec},
	})
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(future, blob, 0o600); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	if !NewBidBlockPermissionManager(future).IsAllowed(builder) {
		t.Fatal("a journal from an unknown version must not be applied")
	}

	// The pre-envelope format (a bare map) decodes into the envelope with
	// version 0, so it lands on the same refusal path rather than being
	// silently half-read.
	legacy := testJournalPath(t)
	blob, err = json.Marshal(map[common.Address]BidBlockRevokeRecord{builder: rec})
	if err != nil {
		t.Fatalf("marshal legacy seed: %v", err)
	}
	if err := os.WriteFile(legacy, blob, 0o600); err != nil {
		t.Fatalf("seed legacy journal: %v", err)
	}
	if !NewBidBlockPermissionManager(legacy).IsAllowed(builder) {
		t.Fatal("a pre-envelope journal must not be applied")
	}
}

// TestBidBlockPermission_JournalKeysAreStable pins the on-disk key names. They
// are the wire format: renaming a Go field is free, but changing a json tag is
// a format change and must come with a version bump, or old journals decode
// their renamed fields to zero — a zero Duration reads as already expired.
func TestBidBlockPermission_JournalKeysAreStable(t *testing.T) {
	path := testJournalPath(t)
	m := NewBidBlockPermissionManager(path)
	m.RevokeForViolation(common.HexToAddress("0x1"), testInsertChainReason, common.HexToHash("0xabc"), 100)
	waitForBidBlockPersist(t, path)

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	for _, key := range []string{
		`"version"`, `"revokes"`,
		`"revokedAt"`, `"duration"`, `"reason"`, `"blockHash"`, `"blockNum"`, `"violationCount"`,
	} {
		if !strings.Contains(string(blob), key) {
			t.Errorf("journal is missing the stable key %s; a tag change needs a version bump\ngot: %s", key, blob)
		}
	}
}

// TestBidBlockPermission_FailedNewerBlocksOlder locks the ordering guarantee:
// once a newer snapshot has entered the writer — even if its write fails — an
// older snapshot must never overwrite it and resurrect stale state. persistAsync
// is called directly to control ordering deterministically; the write failure
// is injected by occupying the journal path with a directory, which makes the
// atomic rename fail.
func TestBidBlockPermission_FailedNewerBlocksOlder(t *testing.T) {
	path := testJournalPath(t)
	m := NewBidBlockPermissionManager(path)

	builder := common.HexToAddress("0x1")
	revoked := map[common.Address]BidBlockRevokeRecord{
		builder: {RevokedAt: time.Now(), Duration: bidBlockRevokeDuration, Reason: "revoke"},
	}
	cleared := map[common.Address]BidBlockRevokeRecord{} // SetAllowed(true) result

	// The newer snapshot (seq 2, cleared) is attempted first but its write
	// fails: the journal path is occupied by a directory.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("occupy journal path: %v", err)
	}
	m.persistAsync(2, cleared)
	if m.persistedSeq != 2 {
		t.Fatalf("persistedSeq must advance to 2 even when the write fails, got %d", m.persistedSeq)
	}

	// The older snapshot (seq 1, revoked) is now handled with writes working
	// again — it must be dropped, not overwrite the newer intent.
	if err := os.Remove(path); err != nil {
		t.Fatalf("free journal path: %v", err)
	}
	m.persistAsync(1, revoked)
	if blob, _ := os.ReadFile(path); len(blob) != 0 {
		t.Fatalf("older snapshot must not overwrite a newer (failed) one; journal should be absent, got %s", blob)
	}
}

// TestBidBlockPermission_SetAllowedPersists verifies that a manual clear is also
// mirrored to disk, so it is not resurrected on restart.
func TestBidBlockPermission_SetAllowedPersists(t *testing.T) {
	path := testJournalPath(t)
	builder := common.HexToAddress("0x1")

	m1 := NewBidBlockPermissionManager(path)
	m1.RevokeFor(builder, testInsertChainReason, common.HexToHash("0xabc"), 100, bidBlockRevokeDuration)
	waitForBidBlockPersist(t, path)
	m1.SetAllowed(builder, true) // manual clear must persist too
	// Wait until the cleared (empty-map) journal has been written.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		blob, _ := os.ReadFile(path)
		var stored bidBlockRevokeJournal
		if json.Unmarshal(blob, &stored) == nil {
			if _, ok := stored.Revokes[builder]; !ok {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
	}

	m2 := NewBidBlockPermissionManager(path)
	if !m2.IsAllowed(builder) {
		t.Fatal("a manually cleared builder must not be resurrected on restart")
	}
}
