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

// bidBlockRevokeDuration is the fixed lockout window for BidBlock revokes.
const bidBlockRevokeDuration = 24 * time.Hour

// BidBlockRevokeRecord holds one active revoke event.
type BidBlockRevokeRecord struct {
	RevokedAt time.Time
	Reason    string // err detail for auto revokes (InsertChain failure), or RevokeReasonManual
	BlockHash common.Hash
	BlockNum  uint64
}

// BidBlockPermissionManager tracks per-builder SendBidBlock revokes.
// Revokes are kept in memory and expire lazily after the fixed lockout window.
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
	rec, found := m.revoked[builder]
	return !found || !isRevokeActive(rec.RevokedAt, m.clock())
}

// Revoke denies builder and records the reason exposed by the permission RPC.
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
	status := types.BidBlockPermissionStatus{
		Allowed: true,
	}
	rec, found := m.revoked[builder]
	if !found || !isRevokeActive(rec.RevokedAt, m.clock()) {
		return status
	}
	status.Allowed = false
	status.Reason = rec.Reason
	status.BlockHash = rec.BlockHash
	status.BlockNum = rec.BlockNum
	status.RevokedAt = rec.RevokedAt
	status.ResetAt = rec.RevokedAt.Add(bidBlockRevokeDuration)
	return status
}

// ActiveRevokeCount returns the number of currently revoked builders.
func (m *BidBlockPermissionManager) ActiveRevokeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.clock()
	count := 0
	for _, rec := range m.revoked {
		if isRevokeActive(rec.RevokedAt, now) {
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

// isRevokeActive reports whether now is before the revoke reset time.
func isRevokeActive(revokedAt, now time.Time) bool {
	return now.Before(revokedAt.Add(bidBlockRevokeDuration))
}
