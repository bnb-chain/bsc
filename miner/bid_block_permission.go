// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BidBlock permission management (BEP-675 Layer 2).

package miner

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/log"
)

// RevokeReasonManual is the Reason value used when an operator manually revokes
// a builder via SetAllowed. Automatic revokes carry the underlying error or
// policy message as Reason directly.
const RevokeReasonManual = "manual"

const (
	// bidBlockRevokeDuration is the default lockout window for invalid BidBlocks,
	// and the first step of the escalating ladder below.
	bidBlockRevokeDuration = 24 * time.Hour
	// bidBlockGasPriceLowRevokeDuration is one epoch for gas-price policy revokes.
	bidBlockGasPriceLowRevokeDuration = 450 * time.Second

	// Repeats cost more, so a broken or hostile builder cannot burn one slot per
	// validator per day at a flat price:
	//
	//	lockout(n) = min(24h + n*12h, 7d)   n = prior violations still on record
	//
	// Capped at the 13th violation, ~52 days of relentless attacking away.
	bidBlockRevokeEscalationStep = 12 * time.Hour
	bidBlockRevokeMaxDuration    = 7 * 24 * time.Hour

	// bidBlockViolationRetention is how long a violation outlives its lockout.
	// Staying clean this long clears the count, which an attacker cannot exploit:
	// idling for a week to earn a 24h lockout is slower than the 7d cap it escapes.
	bidBlockViolationRetention = 7 * 24 * time.Hour
)

// escalatedRevokeDuration returns the lockout for a builder's violation count
// (1-based), following the ladder documented above.
func escalatedRevokeDuration(violationCount int) time.Duration {
	if violationCount < 1 {
		violationCount = 1
	}
	d := bidBlockRevokeDuration + time.Duration(violationCount-1)*bidBlockRevokeEscalationStep
	if d > bidBlockRevokeMaxDuration {
		return bidBlockRevokeMaxDuration
	}
	return d
}

// The revoke map is persisted as a single JSON file under the node's datadir
// (a whole-map snapshot, atomically replaced on every change) so that lockouts
// survive a process restart within their window. It deliberately lives OUTSIDE
// chaindata: revokes are validator-local MEV policy, not chain data — they must
// not be wiped by a chaindata resync nor carried along when chaindata is cloned.
// This mirrors how other node-local state is stored (vote journal, txpool
// journal).
//
// The file is JSON-encoded rather than RLP. RLP is geth's codec for
// consensus-critical and large/frequent data, but it supports neither maps nor
// time.Time, so it would require flattening the map and converting timestamps
// through a separate wire struct. This state is tiny, written rarely (only on
// bad bids), never on-chain, and benefits from JSON's forgiving schema
// evolution and readability.

// bidBlockRevokeJournalVersion is the on-disk format version. Bump it only for a
// change JSON cannot absorb on its own: adding a field is already compatible in
// both directions (an old file decodes with the field zeroed, a new file read by
// an older binary ignores the unknown key), so a bump is for a change in the
// MEANING of an existing field, a removal, or a rename — the cases that would
// otherwise be applied silently and wrongly to old data.
const bidBlockRevokeJournalVersion = 1

// bidBlockRevokeJournal is the envelope actually written to disk. The version
// lives beside the payload so an unrecognised format is refused explicitly
// rather than being half-decoded into whatever the current struct happens to
// look like.
type bidBlockRevokeJournal struct {
	Version int                                     `json:"version"`
	Revokes map[common.Address]BidBlockRevokeRecord `json:"revokes"`
}

// BidBlockRevokeRecord holds one active revoke event.
//
// The json tags are explicit and MUST stay stable: without them the wire format
// would be the Go field names, so a routine rename would silently change the
// on-disk keys, and every renamed field would decode to its zero value on the
// next start — a zero Duration reads as "already expired", i.e. a lockout that
// quietly disappears. Renaming a Go field here is free; changing a tag is a
// format change and needs a version bump.
type BidBlockRevokeRecord struct {
	RevokedAt time.Time     `json:"revokedAt"`
	Duration  time.Duration `json:"duration"`
	Reason    string        `json:"reason"` // err detail for auto revokes (InsertChain failure), or RevokeReasonManual
	BlockHash common.Hash   `json:"blockHash"`
	BlockNum  uint64        `json:"blockNum"`
	// ViolationCount tracks retained on-chain violations used to escalate future revokes.
	ViolationCount int `json:"violationCount,omitempty"`
}

// BidBlockPermissionManager tracks per-builder SendBidBlock revokes.
//
// Revokes are kept in memory and expire lazily after their lockout window.
// They are also mirrored to a journal file under the datadir (best-effort,
// asynchronously) so that a lockout is not silently cleared when the validator
// restarts within its window — the RPC advertises resetAt = revokedAt +
// duration as a wall-clock promise, which must hold across restarts.
type BidBlockPermissionManager struct {
	mu      sync.RWMutex
	revoked map[common.Address]BidBlockRevokeRecord

	clock func() time.Time

	// journalPath is the optional persistence backend. When empty the manager
	// is purely in-memory (used by tests and any caller that does not wire a
	// journal), preserving the original behaviour.
	journalPath string

	// Persistence is asynchronous and best-effort: mutations only snapshot the
	// map under mu and hand it to a background writer, never blocking the
	// mining path on disk I/O. Losing the newest write on a crash is
	// acceptable (at worst one lockout is not restored); what must never happen
	// is a disk hiccup stalling permission updates.
	persistMu    sync.Mutex // serialises background writers so they don't interleave
	persistSeq   uint64     // bumped under mu on every mutation; identifies the latest snapshot
	persistedSeq uint64     // highest seq handled by a writer (guarded by persistMu); advanced before the write, so a failed newer write still blocks older ones
}

// NewBidBlockPermissionManager returns a manager journalling to journalPath
// (may be empty for a purely in-memory manager). Any revokes persisted by a
// previous process that are still within their lockout window are restored;
// expired ones are dropped.
func NewBidBlockPermissionManager(journalPath string) *BidBlockPermissionManager {
	m := &BidBlockPermissionManager{
		revoked:     make(map[common.Address]BidBlockRevokeRecord),
		clock:       time.Now,
		journalPath: journalPath,
	}
	m.load()
	return m
}

// load restores persisted revokes at startup, dropping any whose window has
// already elapsed (including time spent offline). It runs once, synchronously,
// off the hot path. Any error leaves the manager empty — same as a fresh node.
func (m *BidBlockPermissionManager) load() {
	if m.journalPath == "" {
		return
	}
	blob, err := os.ReadFile(m.journalPath)
	if err != nil || len(blob) == 0 {
		return // no journal yet (or unreadable): start empty
	}
	var journal bidBlockRevokeJournal
	if err := json.Unmarshal(blob, &journal); err != nil {
		log.Warn("Failed to decode persisted BidBlock revokes, starting empty", "path", m.journalPath, "err", err)
		return
	}
	// Refuse a version this build does not know rather than applying the current
	// struct's meaning to it. Starting empty costs at most the remaining lockout
	// windows, which expire on their own within bidBlockRevokeDuration anyway;
	// misreading a future format could resurrect or drop the wrong builders.
	if journal.Version != bidBlockRevokeJournalVersion {
		log.Warn("Unsupported BidBlock revoke journal version, starting empty",
			"path", m.journalPath, "version", journal.Version, "supported", bidBlockRevokeJournalVersion)
		return
	}
	// Retain expired revokes while their violation count is still retained.
	now := m.clock()
	active := 0
	for builder, rec := range journal.Revokes {
		if !isViolationRetained(rec, now) {
			continue
		}
		m.revoked[builder] = rec
		if isRevokeActive(rec, now) {
			active++
		}
	}
	if len(m.revoked) > 0 {
		log.Info("Restored BidBlock revokes from journal",
			"path", m.journalPath, "active", active, "retainedViolations", len(m.revoked)-active)
	}
}

// markDirtyLocked snapshots the current map and kicks off an asynchronous write.
// It MUST be called with m.mu held. The snapshot is taken here (under the lock)
// so the background writer never touches the live map; the caller returns
// immediately without waiting for the disk write.
func (m *BidBlockPermissionManager) markDirtyLocked() {
	if m.journalPath == "" {
		return
	}
	m.persistSeq++
	seq := m.persistSeq
	snapshot := make(map[common.Address]BidBlockRevokeRecord, len(m.revoked))
	for k, v := range m.revoked {
		snapshot[k] = v
	}
	// Fire-and-forget: mutations are rare (only on bad bids), so spawning a
	// goroutine per change is cheap. persistAsync serialises writers and drops
	// stale snapshots via the seq guard.
	go m.persistAsync(seq, snapshot)
}

// persistAsync writes snapshot to the journal file unless a newer snapshot has
// already been handled. Errors are logged and swallowed: persistence is
// best-effort and must not affect the caller.
//
// The write is atomic (temp file + rename), so a crash mid-write leaves the
// previous journal intact rather than a truncated one.
//
// persistedSeq is advanced BEFORE the write and regardless of its outcome. This
// makes the seq that actually reaches the disk strictly monotonic: once a newer
// snapshot has entered here, every older one is dropped even if the newer
// write failed. Otherwise a newer write failing could let an older snapshot
// win the disk afterwards and resurrect stale state (e.g. a revoke overwriting
// a later manual clear). Losing the newest write on failure is acceptable
// (best-effort); an older snapshot overwriting a newer one is not.
func (m *BidBlockPermissionManager) persistAsync(seq uint64, snapshot map[common.Address]BidBlockRevokeRecord) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	if seq <= m.persistedSeq {
		return // an equal-or-newer snapshot has already been handled; this one is stale
	}
	m.persistedSeq = seq
	blob, err := json.Marshal(bidBlockRevokeJournal{
		Version: bidBlockRevokeJournalVersion,
		Revokes: snapshot,
	})
	if err != nil {
		log.Warn("Failed to encode BidBlock revokes for persistence", "err", err)
		return
	}
	tmp := m.journalPath + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		log.Warn("Failed to write BidBlock revoke journal", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, m.journalPath); err != nil {
		log.Warn("Failed to replace BidBlock revoke journal", "path", m.journalPath, "err", err)
		return
	}
}

// IsAllowed reports whether builder may currently use SendBidBlock.
func (m *BidBlockPermissionManager) IsAllowed(builder common.Address) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, found := m.activeRecord(builder, m.clock())
	return !found
}

// RevokeForViolation records a violation and applies the corresponding escalating lockout.
func (m *BidBlockPermissionManager) RevokeForViolation(
	builder common.Address,
	reason string,
	blockHash common.Hash,
	blockNum uint64,
) (time.Duration, int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.clock()
	violationCount := m.carriedViolationCount(builder, now) + 1
	duration := escalatedRevokeDuration(violationCount)
	m.applyRevokeLocked(builder, reason, blockHash, blockNum, duration, violationCount, now)
	return duration, violationCount
}

// RevokeFor denies builder for the supplied duration and records the reason
// exposed by the permission RPC. The violation count is carried over untouched,
// so a short policy lockout neither escalates nor clears the ladder.
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

	now := m.clock()
	m.applyRevokeLocked(builder, reason, blockHash, blockNum, duration, m.carriedViolationCount(builder, now), now)
}

// applyRevokeLocked stores one revoke and schedules persistence. Must be called
// with m.mu held.
//
// An active lockout is never shortened, so a 450s gas-price revoke landing on a
// builder serving a multi-day one only refreshes its reason and violation count.
func (m *BidBlockPermissionManager) applyRevokeLocked(
	builder common.Address,
	reason string,
	blockHash common.Hash,
	blockNum uint64,
	duration time.Duration,
	violationCount int,
	now time.Time,
) {
	m.pruneLocked(now)
	if rec, found := m.revoked[builder]; found && isRevokeActive(rec, now) {
		if remaining := revokeResetAt(rec).Sub(now); remaining > duration {
			duration = remaining
		}
	}
	m.revoked[builder] = BidBlockRevokeRecord{
		RevokedAt:      now,
		Duration:       duration,
		Reason:         reason,
		BlockHash:      blockHash,
		BlockNum:       blockNum,
		ViolationCount: violationCount,
	}
	m.markDirtyLocked()
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

// SetAllowed is the operator override. Allowing a builder clears its violation
// count as well as its lockout: a manual release is a judgement that the
// builder is clean, so the next automatic revoke starts at the base duration.
// Manual denial does not escalate and does not add a violation.
func (m *BidBlockPermissionManager) SetAllowed(builder common.Address, allowed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if allowed {
		delete(m.revoked, builder)
		m.markDirtyLocked()
		return
	}
	// Via applyRevokeLocked so denying an already-denied builder — a natural
	// operator action — cannot cut short a longer escalated lockout.
	now := m.clock()
	m.applyRevokeLocked(builder, RevokeReasonManual, common.Hash{}, 0, bidBlockRevokeDuration, m.carriedViolationCount(builder, now), now)
}

// isRevokeActive reports whether now is before the revoke reset time.
func isRevokeActive(rec BidBlockRevokeRecord, now time.Time) bool {
	return now.Before(revokeResetAt(rec))
}

// isViolationRetained reports whether rec still carries a usable violation
// count. It outlives the lockout itself by bidBlockViolationRetention, which
// makes the escalation survive between attacks instead of resetting on unban.
func isViolationRetained(rec BidBlockRevokeRecord, now time.Time) bool {
	return now.Before(revokeResetAt(rec).Add(bidBlockViolationRetention))
}

func revokeResetAt(rec BidBlockRevokeRecord) time.Time {
	return rec.RevokedAt.Add(rec.Duration)
}

// carriedViolationCount returns the violation count a new revoke for builder
// inherits, or zero once the previous record's retention window has passed.
// Must be called with m.mu held.
func (m *BidBlockPermissionManager) carriedViolationCount(builder common.Address, now time.Time) int {
	rec, found := m.revoked[builder]
	if !found || !isViolationRetained(rec, now) {
		return 0
	}
	return rec.ViolationCount
}

// pruneLocked drops records past their violation retention, so the map (and the
// journal snapshot taken from it) does not grow without bound. Must be called
// with m.mu held.
func (m *BidBlockPermissionManager) pruneLocked(now time.Time) {
	for builder, rec := range m.revoked {
		if !isViolationRetained(rec, now) {
			delete(m.revoked, builder)
		}
	}
}

func (m *BidBlockPermissionManager) activeRecord(builder common.Address, now time.Time) (BidBlockRevokeRecord, bool) {
	rec, found := m.revoked[builder]
	if !found || !isRevokeActive(rec, now) {
		return BidBlockRevokeRecord{}, false
	}
	return rec, true
}
