// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BidBlock permission management (BEP-675 Layer 2).

package miner

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// BidBlockRevokeReason classifies why a builder lost SendBidBlock permission.
type BidBlockRevokeReason string

const (
	RevokeReasonInsertChainFailed BidBlockRevokeReason = "insertchain_failed"
	RevokeReasonManual            BidBlockRevokeReason = "manual"
)

// BidBlockRevokeRecord holds one active revoke event.
type BidBlockRevokeRecord struct {
	RevokedAt time.Time
	Reason    BidBlockRevokeReason
	BlockHash common.Hash
	BlockNum  uint64
}

// BidBlockPermissionManager tracks per-builder SendBidBlock revokes.
// Revokes are in-memory and expire lazily at the next UTC day.
type BidBlockPermissionManager struct {
	mu      sync.RWMutex
	revoked map[common.Address]BidBlockRevokeRecord

	clock func() time.Time
}

// NewBidBlockPermissionManager returns a fresh manager with no builders revoked.
func NewBidBlockPermissionManager() *BidBlockPermissionManager {
	return &BidBlockPermissionManager{
		revoked: make(map[common.Address]BidBlockRevokeRecord),
		clock:   time.Now,
	}
}

// IsAllowed reports whether builder may currently use SendBidBlock.
func (m *BidBlockPermissionManager) IsAllowed(builder common.Address) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isAllowed(builder, m.clock())
}

func (m *BidBlockPermissionManager) isAllowed(builder common.Address, now time.Time) bool {
	rec, found := m.revoked[builder]
	return !found || !sameUTCDay(rec.RevokedAt, now)
}

// Revoke marks builder as denied for the remainder of the current UTC day.
func (m *BidBlockPermissionManager) Revoke(
	builder common.Address,
	reason BidBlockRevokeReason,
	blockHash common.Hash,
	blockNum uint64,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[builder] = BidBlockRevokeRecord{
		RevokedAt: m.clock(),
		Reason:    reason,
		BlockHash: blockHash,
		BlockNum:  blockNum,
	}
}

func (m *BidBlockPermissionManager) GetStatus(builder common.Address) types.BidBlockPermissionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.clock()
	status := types.BidBlockPermissionStatus{
		Allowed: true,
	}
	if m.isAllowed(builder, now) {
		return status
	}
	rec := m.revoked[builder]
	status.Allowed = false
	status.Reason = string(rec.Reason)
	status.BlockHash = rec.BlockHash
	status.BlockNum = rec.BlockNum
	status.RevokedAt = rec.RevokedAt
	status.ResetAt = nextUTCDay(now)
	return status
}

// ActiveRevokeCount returns the number of currently revoked builders.
func (m *BidBlockPermissionManager) ActiveRevokeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.clock()
	count := 0
	for _, rec := range m.revoked {
		if sameUTCDay(rec.RevokedAt, now) {
			count++
		}
	}
	return count
}

func (m *BidBlockPermissionManager) SetAllowed(builder common.Address, allowed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if allowed {
		delete(m.revoked, builder)
		return
	}
	m.revoked[builder] = BidBlockRevokeRecord{
		RevokedAt: m.clock(),
		Reason:    RevokeReasonManual,
	}
}

func sameUTCDay(t1, t2 time.Time) bool {
	y1, mo1, d1 := t1.UTC().Date()
	y2, mo2, d2 := t2.UTC().Date()
	return y1 == y2 && mo1 == mo2 && d1 == d2
}

func nextUTCDay(t time.Time) time.Time {
	y, mo, d := t.UTC().Date()
	return time.Date(y, mo, d+1, 0, 0, 0, 0, time.UTC)
}
