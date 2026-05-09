// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/miner/builderclient"
)

func TestBidBlockPermission_DefaultActive(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")
	if !m.IsAllowed(builder) {
		t.Fatal("default state should be Active for any builder")
	}
	if _, ok := m.GetRecord(builder); ok {
		t.Fatal("no record expected for fresh builder")
	}
}

func TestBidBlockPermission_RevokeBlocks(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")
	hash := common.HexToHash("0xabc")

	m.Revoke(builder, RevokeReasonInsertChainFailed, hash, 100)
	if m.IsAllowed(builder) {
		t.Fatal("revoked builder should not be allowed in same UTC day")
	}

	rec, ok := m.GetRecord(builder)
	if !ok {
		t.Fatal("record expected after Revoke")
	}
	if rec.Reason != RevokeReasonInsertChainFailed {
		t.Fatalf("reason: got %s, want %s", rec.Reason, RevokeReasonInsertChainFailed)
	}
	if rec.BlockHash != hash {
		t.Fatalf("blockHash: got %s, want %s", rec.BlockHash.Hex(), hash.Hex())
	}
	if rec.BlockNum != 100 {
		t.Fatalf("blockNum: got %d, want 100", rec.BlockNum)
	}
}

func TestBidBlockPermission_BuildersIndependent(t *testing.T) {
	m := NewBidBlockPermissionManager()
	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")

	m.Revoke(a, RevokeReasonGasFeeOverClaim, common.Hash{}, 1)
	if m.IsAllowed(a) {
		t.Fatal("a should be revoked")
	}
	if !m.IsAllowed(b) {
		t.Fatal("b should remain active")
	}
}

func TestBidBlockPermission_RevokeOverwritesSameDay(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	m.Revoke(builder, RevokeReasonInsertChainFailed, common.HexToHash("0x1"), 1)
	m.Revoke(builder, RevokeReasonGasFeeOverClaim, common.HexToHash("0x2"), 2)

	rec, ok := m.GetRecord(builder)
	if !ok {
		t.Fatal("record expected")
	}
	if rec.Reason != RevokeReasonGasFeeOverClaim {
		t.Fatalf("most recent reason should win: got %s", rec.Reason)
	}
	if rec.BlockNum != 2 {
		t.Fatalf("most recent blockNum should win: got %d", rec.BlockNum)
	}
}

func TestBidBlockPermission_LazyResetCrossDay(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	day1 := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	m.setClock(func() time.Time { return day1 })
	m.Revoke(builder, RevokeReasonInsertChainFailed, common.Hash{}, 1)
	if m.IsAllowed(builder) {
		t.Fatal("revoked on day1 should be blocked on day1")
	}

	m.setClock(func() time.Time { return day2 })
	if !m.IsAllowed(builder) {
		t.Fatal("cross-day record should be treated as Active (lazy reset)")
	}
	if _, ok := m.GetRecord(builder); ok {
		t.Fatal("getRecord should report expired on cross-day")
	}
}

func TestBidBlockPermission_SameDayBoundary(t *testing.T) {
	m := NewBidBlockPermissionManager()
	builder := common.HexToAddress("0x1")

	near := time.Date(2026, 5, 8, 23, 59, 59, 0, time.UTC)
	m.setClock(func() time.Time { return near })
	m.Revoke(builder, RevokeReasonInsertChainFailed, common.Hash{}, 1)

	justAfter := time.Date(2026, 5, 9, 0, 0, 1, 0, time.UTC)
	m.setClock(func() time.Time { return justAfter })
	if !m.IsAllowed(builder) {
		t.Fatal("revoke at 23:59:59 should not survive past 00:00 UTC")
	}
}

func TestBidBlockPermission_SameUTCDay(t *testing.T) {
	cases := []struct {
		name string
		t1   time.Time
		t2   time.Time
		want bool
	}{
		{
			name: "midnight to end of day same UTC",
			t1:   time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
			t2:   time.Date(2026, 5, 8, 23, 59, 59, 0, time.UTC),
			want: true,
		},
		{
			name: "across UTC midnight",
			t1:   time.Date(2026, 5, 8, 23, 59, 59, 0, time.UTC),
			t2:   time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "different zones, same UTC day",
			t1:   time.Date(2026, 5, 7, 23, 0, 0, 0, time.FixedZone("EST", -5*3600)),
			t2:   time.Date(2026, 5, 8, 4, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "different zones, different UTC days",
			t1:   time.Date(2026, 5, 8, 22, 0, 0, 0, time.FixedZone("EST", -5*3600)),
			t2:   time.Date(2026, 5, 8, 22, 0, 0, 0, time.UTC),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameUTCDay(c.t1, c.t2); got != c.want {
				t.Fatalf("sameUTCDay: got %v, want %v", got, c.want)
			}
		})
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
		go func() { defer wg.Done(); m.Revoke(b, RevokeReasonInsertChainFailed, common.Hash{}, 1) }()
		go func() { defer wg.Done(); m.GetRecord(b) }()
	}
	wg.Wait()
}

func TestBidBlockPermission_ActiveRevokeCount(t *testing.T) {
	m := NewBidBlockPermissionManager()

	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("empty manager: got %d, want 0", got)
	}

	day1 := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	m.setClock(func() time.Time { return day1 })

	a := common.HexToAddress("0xa")
	b := common.HexToAddress("0xb")
	m.Revoke(a, RevokeReasonInsertChainFailed, common.Hash{}, 1)
	m.Revoke(b, RevokeReasonGasFeeOverClaim, common.Hash{}, 2)

	if got := m.ActiveRevokeCount(); got != 2 {
		t.Fatalf("two revoked: got %d, want 2", got)
	}

	m.setClock(func() time.Time { return day2 })
	if got := m.ActiveRevokeCount(); got != 0 {
		t.Fatalf("after cross-day: got %d, want 0 (entries are stale, not active)", got)
	}
}

func TestBidBlockAdmission_RevokedDoesNotConsumeQuota(t *testing.T) {
	b := &bidSimulator{
		builders:          make(map[common.Address]*builderclient.Client),
		pending:           make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		permMgr:           NewBidBlockPermissionManager(),
		maxBidsPerBuilder: 2,
	}

	builder := common.HexToAddress("0x1")
	const blockNum uint64 = 100

	b.builders[builder] = nil
	b.permMgr.Revoke(builder, RevokeReasonInsertChainFailed, common.Hash{}, blockNum-1)

	if !b.ExistBuilder(builder) {
		t.Fatal("registered builder must pass ExistBuilder")
	}
	if b.IsBidBlockAllowed(builder) {
		t.Fatal("revoked builder must fail IsBidBlockAllowed")
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

func TestBidBlockPermission_SharedManager(t *testing.T) {
	m := NewBidBlockPermissionManager()
	w := &worker{permMgr: m}
	b := &bidSimulator{permMgr: m}
	builder := common.HexToAddress("0x1")

	w.permMgr.Revoke(builder, RevokeReasonGasFeeOverClaim, common.Hash{}, 1)
	if b.IsBidBlockAllowed(builder) {
		t.Fatal("bidSimulator should observe worker revoke")
	}
}
