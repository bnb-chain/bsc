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

// RevokeReasonManual is the Reason value used when an operator manually revokes
// a builder via SetAllowed. Automatic revokes always come from InsertChain
// failures and carry the underlying error message as Reason directly — see the
// auto-revoke call site in handleBidBlockResult for the conditions that trigger.
const RevokeReasonManual = "manual"

// BidBlockRevokeRecord holds one active revoke event.
type BidBlockRevokeRecord struct {
	RevokedAt time.Time
	Reason    string // err detail for auto revokes (InsertChain failure), or RevokeReasonManual
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
	now := m.clock()
	rec, found := m.revoked[builder]
	return !found || !sameUTCDay(rec.RevokedAt, now)
}

// Revoke marks builder as denied for the remainder of the current UTC day.
// reason is surfaced via GetBidBlockPermission RPC so builders can see specifics
// (the InsertChain error text for auto revokes, or RevokeReasonManual for admin).
func (m *BidBlockPermissionManager) Revoke(
	builder common.Address,
	reason string,
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
	rec, found := m.revoked[builder]
	if !found || !sameUTCDay(rec.RevokedAt, now) {
		return status
	}
	status.Allowed = false
	status.Reason = rec.Reason
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
