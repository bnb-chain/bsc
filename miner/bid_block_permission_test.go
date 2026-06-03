// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")
	if !m.IsAllowed(builder) {
		t.Fatal("default state should be Active for any builder")
	}
	if _, ok := getBidBlockPermissionRecord(m, builder); ok {
		t.Fatal("no record expected for fresh builder")
	}
}

func TestBidBlockPermission_RevokeBlocks(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")
	hash := common.HexToHash("0xabc")

	m.Revoke(builder, testInsertChainReason, hash, 100)
	if m.IsAllowed(builder) {
		t.Fatal("revoked builder should not be allowed within 24h of revoke")
	}

	rec, ok := getBidBlockPermissionRecord(m, builder)
	if !ok {
		t.Fatal("record expected after Revoke")
	}
	if rec.Reason != testInsertChainReason {
		t.Fatalf("reason: got %s, want %s", rec.Reason, testInsertChainReason)
	}
	if rec.BlockHash != hash {
		t.Fatalf("blockHash: got %s, want %s", rec.BlockHash.Hex(), hash.Hex())
	}
	if rec.BlockNum != 100 {
		t.Fatalf("blockNum: got %d, want 100", rec.BlockNum)
	}
	if rec.Duration != bidBlockRevokeDuration {
		t.Fatalf("duration: got %s, want %s", rec.Duration, bidBlockRevokeDuration)
	}
}

func TestBidBlockPermission_BuildersIndependent(t *testing.T) {
	m := NewBidBlockPermissionManager()
	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")

	m.Revoke(a, testInsertChainReason, common.Hash{}, 1)
	if m.IsAllowed(a) {
		t.Fatal("a should be revoked")
	}
	if !m.IsAllowed(b) {
		t.Fatal("b should remain active")
	}
}

func TestBidBlockPermission_RevokeForCustomDuration(t *testing.T) {
	m := NewBidBlockPermissionManager()
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

func TestBidBlockPermission_RevokeOverwrites(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	m.Revoke(builder, testInsertChainReason, common.HexToHash("0x1"), 1)
	m.Revoke(builder, RevokeReasonManual, common.HexToHash("0x2"), 2)

	rec, ok := getBidBlockPermissionRecord(m, builder)
	if !ok {
		t.Fatal("record expected")
	}
	if rec.Reason != RevokeReasonManual {
		t.Fatalf("most recent reason should win: got %s", rec.Reason)
	}
	if rec.BlockNum != 2 {
		t.Fatalf("most recent blockNum should win: got %d", rec.BlockNum)
	}
}

func TestBidBlockPermission_ExpiresAt24h(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	revokeTime := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	exactly24hLater := revokeTime.Add(24 * time.Hour)

	setBidBlockPermissionClock(m, func() time.Time { return revokeTime })
	m.Revoke(builder, testInsertChainReason, common.Hash{}, 1)
	if m.IsAllowed(builder) {
		t.Fatal("freshly revoked builder should be blocked")
	}

	setBidBlockPermissionClock(m, func() time.Time { return exactly24hLater })
	if !m.IsAllowed(builder) {
		t.Fatal("record at exactly revokedAt + 24h should be expired")
	}
	if _, ok := getBidBlockPermissionRecord(m, builder); ok {
		t.Fatal("getRecord should report expired at revokedAt + 24h")
	}
}

func TestBidBlockPermission_StillRevokedWithin24h(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	// UTC midnight should not reset the revoke; only elapsed time matters.
	// This covers the old day-boundary bypass.
	revokeTime := time.Date(2026, 5, 8, 23, 59, 59, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return revokeTime })
	m.Revoke(builder, testInsertChainReason, common.Hash{}, 1)

	justAfterUTCMidnight := time.Date(2026, 5, 9, 0, 0, 1, 0, time.UTC)
	setBidBlockPermissionClock(m, func() time.Time { return justAfterUTCMidnight })
	if m.IsAllowed(builder) {
		t.Fatal("revoke must not expire just because UTC day rolled over (only 2s elapsed)")
	}

	justBefore24h := revokeTime.Add(24*time.Hour - time.Second)
	setBidBlockPermissionClock(m, func() time.Time { return justBefore24h })
	if m.IsAllowed(builder) {
		t.Fatal("revoke must still be active 1s before the 24h boundary")
	}
}

// Builders revoked at different times should expire independently.
func TestBidBlockPermission_IndependentResetAt(t *testing.T) {
	m := NewBidBlockPermissionManager()
	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")

	t0 := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	setBidBlockPermissionClock(m, func() time.Time { return t0 })
	m.Revoke(a, testInsertChainReason, common.Hash{}, 1)

	setBidBlockPermissionClock(m, func() time.Time { return t0.Add(5 * time.Hour) })
	m.Revoke(b, testInsertChainReason, common.Hash{}, 2)

	// Both revoked at t0 + 6h; resetAt fields must be independent.
	setBidBlockPermissionClock(m, func() time.Time { return t0.Add(6 * time.Hour) })
	statusA := m.GetStatus(a)
	statusB := m.GetStatus(b)
	if statusA.Allowed || statusB.Allowed {
		t.Fatal("both builders should be revoked at t0 + 6h")
	}
	if want := t0.Add(24 * time.Hour); !statusA.ResetAt.Equal(want) {
		t.Fatalf("A resetAt: got %s, want %s", statusA.ResetAt, want)
	}
	if want := t0.Add(29 * time.Hour); !statusB.ResetAt.Equal(want) {
		t.Fatalf("B resetAt: got %s, want %s", statusB.ResetAt, want)
	}

	// At A.RevokedAt + 24h, A's lockout expires but B still has 5h left.
	setBidBlockPermissionClock(m, func() time.Time { return t0.Add(24 * time.Hour) })
	if !m.IsAllowed(a) {
		t.Fatal("A should be allowed at its own RevokedAt + 24h")
	}
	if m.IsAllowed(b) {
		t.Fatal("B should still be revoked (only 19h elapsed since its own RevokedAt)")
	}

	// At B.RevokedAt + 24h, B's lockout also expires.
	setBidBlockPermissionClock(m, func() time.Time { return t0.Add(29 * time.Hour) })
	if !m.IsAllowed(b) {
		t.Fatal("B should be allowed at its own RevokedAt + 24h")
	}
}

func TestBidBlockPermission_ConcurrentAccess(t *testing.T) {
	m := NewBidBlockPermissionManager()
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
		go func() { defer wg.Done(); m.Revoke(b, testInsertChainReason, common.Hash{}, 1) }()
		go func() { defer wg.Done(); getBidBlockPermissionRecord(m, b) }()
	}
	wg.Wait()
}

func TestBidBlockPermission_ActiveRevokeCount(t *testing.T) {
	m := NewBidBlockPermissionManager()

	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("empty manager: got %d, want 0", got)
	}

	revokeTime := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	after24h := revokeTime.Add(24 * time.Hour)
	setBidBlockPermissionClock(m, func() time.Time { return revokeTime })

	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")
	m.Revoke(a, testInsertChainReason, common.Hash{}, 1)
	m.Revoke(b, RevokeReasonManual, common.Hash{}, 2)

	if got := m.ActiveRevokeCount(); got != 2 {
		t.Fatalf("two revoked: got %d, want 2", got)
	}

	setBidBlockPermissionClock(m, func() time.Time { return after24h })
	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("after revokedAt + 24h: got %d, want 0 (entries are stale, not active)", got)
	}
}

func TestBidBlockPermission_GetStatus(t *testing.T) {
	m := NewBidBlockPermissionManager()
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
	m.Revoke(builder, testInsertChainReason, hash, 100)
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
	permMgr := NewBidBlockPermissionManager()
	b := &bidSimulator{
		builders:          make(map[common.Address]*builderclient.Client),
		pending:           make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		maxBidsPerBuilder: 2,
	}

	builder := common.HexToAddress("0x1")
	const blockNum uint64 = 100

	b.builders[builder] = nil
	permMgr.Revoke(builder, testInsertChainReason, common.Hash{}, blockNum-1)

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
	if err := b.CheckPending(blockNum, other, otherHash); err != nil {
		t.Fatalf("active builder should pass CheckPending: %v", err)
	}
	b.AddPending(blockNum, other, otherHash)

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

			_, err := miner.SendBidBlock(context.Background(), &types.BidBlockArgs{})
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
	m := NewBidBlockPermissionManager()
	miner := &Miner{worker: &worker{permMgr: m}}
	builder := common.HexToAddress("0x1")

	m.Revoke(builder, testInsertChainReason, common.Hash{}, 1)
	if miner.GetBidBlockPermission(builder).Allowed {
		t.Fatal("miner should report worker revoke")
	}
}

func TestBidBlockPermission_SetAllowed_Deny(t *testing.T) {
	m := NewBidBlockPermissionManager()
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
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	m.Revoke(builder, testInsertChainReason, common.HexToHash("0xabc"), 100)
	m.SetAllowed(builder, true)
	if !m.IsAllowed(builder) {
		t.Fatal("manual SetAllowed(true) should override revoke")
	}
	if _, ok := getBidBlockPermissionRecord(m, builder); ok {
		t.Fatal("record should be cleared")
	}
}
