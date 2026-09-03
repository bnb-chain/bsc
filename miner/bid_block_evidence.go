// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BEP-675 bad-BidBlock evidence tracking. Local revoke only fires after a builder
// has burned this node's own slot; counting the blocks it broke for other
// validators lets a node revoke it before its turn comes.

package miner

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// badBidBlockEvidenceWindow keeps incidents months apart from accumulating.
const badBidBlockEvidenceWindow = 24 * time.Hour

func majorityThreshold(validators int) int { return validators/2 + 1 }

// isTrue reports whether an optional config flag is set and enabled.
func isTrue(flag *bool) bool { return flag != nil && *flag }

// badBidBlockSighting records one validator's failing block for a builder.
type badBidBlockSighting struct {
	at      time.Time
	cabinet bool
}

// badBidBlockTracker counts distinct validators that sealed a failing BidBlock
// per builder. In memory only: losing it on restart just delays a revoke.
type badBidBlockTracker struct {
	mu    sync.Mutex
	seen  map[common.Address]map[common.Address]badBidBlockSighting
	clock func() time.Time
}

func newBadBidBlockTracker() *badBidBlockTracker {
	return &badBidBlockTracker{
		seen:  make(map[common.Address]map[common.Address]badBidBlockSighting),
		clock: time.Now,
	}
}

// add records a sighting and reports the cabinet and total votes still inside the
// evidence window. Repeat sightings from one sealer refresh it without voting again.
func (t *badBidBlockTracker) add(builder, sealer common.Address, cabinet bool) (cabinetVotes, totalVotes int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock()
	sightings := t.seen[builder]
	if sightings == nil {
		sightings = make(map[common.Address]badBidBlockSighting)
		t.seen[builder] = sightings
	}
	// cabinet is sticky, so a later sighting of a demoted sealer cannot lower the count.
	previous := sightings[sealer]
	sightings[sealer] = badBidBlockSighting{
		at:      now,
		cabinet: previous.cabinet || cabinet,
	}

	for addr, s := range sightings {
		if now.Sub(s.at) >= badBidBlockEvidenceWindow {
			delete(sightings, addr)
			continue
		}
		totalVotes++
		if s.cabinet {
			cabinetVotes++
		}
	}
	return cabinetVotes, totalVotes
}

// clear drops a revoked builder's evidence so the next lockout starts fresh.
func (t *badBidBlockTracker) clear(builder common.Address) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, builder)
}
