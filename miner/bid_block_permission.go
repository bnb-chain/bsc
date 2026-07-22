// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BidBlock permission management (BEP-675 Layer 2).

package miner

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
)

// RevokeReasonManual is the Reason value used when an operator manually revokes
// a builder via SetAllowed. Automatic revokes carry the underlying error or
// policy message as Reason directly.
const RevokeReasonManual = "manual"

const (
	// bidBlockRevokeDuration is the default lockout window for invalid BidBlocks.
	bidBlockRevokeDuration = 24 * time.Hour
	// bidBlockGasPriceLowRevokeDuration is one epoch for gas-price policy revokes.
	bidBlockGasPriceLowRevokeDuration = 450 * time.Second
)

// BidBlockRevokeRecord holds one active revoke event.
type BidBlockRevokeRecord struct {
	RevokedAt time.Time
	Duration  time.Duration
	Reason    string // err detail for auto revokes (InsertChain failure), or RevokeReasonManual
	BlockHash common.Hash
	BlockNum  uint64
}

// BidBlockPermissionManager tracks per-builder SendBidBlock revokes.
// Revokes are kept in memory and expire lazily after their lockout window.
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
	_, found := m.activeRecord(builder, m.clock())
	return !found
}

// Revoke denies builder and records the reason exposed by the permission RPC.
func (m *BidBlockPermissionManager) Revoke(
	builder common.Address,
	reason string,
	blockHash common.Hash,
	blockNum uint64,
) {
	m.RevokeFor(builder, reason, blockHash, blockNum, bidBlockRevokeDuration)
}

// RevokeFor denies builder for the supplied duration and records the reason
// exposed by the permission RPC.
func (m *BidBlockPermissionManager) RevokeFor(
	builder common.Address,
	reason string,
	blockHash common.Hash,
	blockNum uint64,
	duration time.Duration,
) {
	if duration <= 0 {
		duration = bidBlockRevokeDuration
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[builder] = BidBlockRevokeRecord{
		RevokedAt: m.clock(),
		Duration:  duration,
		Reason:    reason,
		BlockHash: blockHash,
		BlockNum:  blockNum,
	}
}

func (m *BidBlockPermissionManager) GetStatus(builder common.Address) buildertypes.BidBlockPermissionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := buildertypes.BidBlockPermissionStatus{
		Allowed: true,
	}
	rec, found := m.activeRecord(builder, m.clock())
	if !found {
		return status
	}
	status.Allowed = false
	status.Reason = rec.Reason
	status.BlockHash = rec.BlockHash
	status.BlockNum = rec.BlockNum
	status.RevokedAt = rec.RevokedAt
	status.ResetAt = rec.RevokedAt.Add(rec.Duration)
	return status
}

// ActiveRevokeCount returns the number of currently revoked builders.
func (m *BidBlockPermissionManager) ActiveRevokeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.clock()
	count := 0
	for _, rec := range m.revoked {
		if isRevokeActive(rec, now) {
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
		Duration:  bidBlockRevokeDuration,
		Reason:    RevokeReasonManual,
	}
}

// isRevokeActive reports whether now is before the revoke reset time.
func isRevokeActive(rec BidBlockRevokeRecord, now time.Time) bool {
	return now.Before(rec.RevokedAt.Add(rec.Duration))
}

func (m *BidBlockPermissionManager) activeRecord(builder common.Address, now time.Time) (BidBlockRevokeRecord, bool) {
	rec, found := m.revoked[builder]
	if !found || !isRevokeActive(rec, now) {
		return BidBlockRevokeRecord{}, false
	}
	return rec, true
}
