// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.

package vote

import (
	"sync"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

// Per-block limits chosen to match the BLS VotePool semantics (21 validators on BSC).
const (
	maxPQCurVotesPerBlock = 21
)

var (
	localPQCurVotesCounter    = metrics.NewRegisteredCounter("curPQVotes/local", nil)
	localPQReceivedVotesGauge = metrics.NewRegisteredGauge("receivedPQVotes/local", nil)
)

// PQVoteBox groups PQ vote envelopes targeting the same block hash.
type PQVoteBox struct {
	blockNumber  uint64
	blockHash    common.Hash
	voteMessages []*types.PQVoteEnvelope
}

// PQVotePool stores post-quantum (ML-DSA-44) vote envelopes produced by validators.
// Compared to the BLS VotePool this implementation is intentionally minimal:
//   - no future/current split (votes are stored for their target hash directly),
//   - no priority queue (we prune via a full scan on chain head events, which is
//     fine for the expected O(256) capacity),
//   - no engine.VerifyVote call (quorum + committee verification happens inside
//     parlia.pqVerifyVoteAttestation during header validation).
//
// The pool emits core.NewPQVoteEvent whenever a new vote is accepted so the
// eth protocol handler can broadcast it to peers.
type PQVotePool struct {
	chain *core.BlockChain

	mu sync.RWMutex

	votesFeed event.Feed
	scope     event.SubscriptionScope

	// Dedup set keyed by PQVoteEnvelope.Hash().
	receivedVotes mapset.Set[common.Hash]

	// Vote messages grouped by target block hash.
	curVotes map[common.Hash]*PQVoteBox

	highestVerifiedBlockCh  chan core.HighestVerifiedBlockEvent
	highestVerifiedBlockSub event.Subscription

	votesCh chan *types.PQVoteEnvelope

	quitCh chan struct{}
}

// NewPQVotePool creates a new PQVotePool subscribed to chain head events for pruning.
func NewPQVotePool(chain *core.BlockChain) *PQVotePool {
	pool := &PQVotePool{
		chain:                  chain,
		receivedVotes:          mapset.NewSet[common.Hash](),
		curVotes:               make(map[common.Hash]*PQVoteBox),
		highestVerifiedBlockCh: make(chan core.HighestVerifiedBlockEvent, highestVerifiedBlockChanSize),
		votesCh:                make(chan *types.PQVoteEnvelope, voteBufferForPut),
		quitCh:                 make(chan struct{}),
	}

	if chain != nil {
		pool.highestVerifiedBlockSub = chain.SubscribeHighestVerifiedHeaderEvent(pool.highestVerifiedBlockCh)
	}

	go pool.loop()
	return pool
}

// Stop releases resources. Safe to call multiple times.
func (pool *PQVotePool) Stop() {
	select {
	case <-pool.quitCh:
		return
	default:
		close(pool.quitCh)
	}
	pool.scope.Close()
}

func (pool *PQVotePool) loop() {
	if pool.highestVerifiedBlockSub != nil {
		defer pool.highestVerifiedBlockSub.Unsubscribe()
	}
	var subErrCh <-chan error
	if pool.highestVerifiedBlockSub != nil {
		subErrCh = pool.highestVerifiedBlockSub.Err()
	}

	for {
		select {
		case ev := <-pool.highestVerifiedBlockCh:
			if ev.Header != nil {
				pool.prune(ev.Header.Number.Uint64())
			}
		case vote := <-pool.votesCh:
			pool.putIntoVotePool(vote)
		case <-subErrCh:
			return
		case <-pool.quitCh:
			return
		}
	}
}

// PutVote enqueues a PQ vote for asynchronous insertion into the pool.
func (pool *PQVotePool) PutVote(vote *types.PQVoteEnvelope) {
	if vote == nil || vote.Data == nil {
		return
	}
	select {
	case pool.votesCh <- vote:
	default:
		log.Warn("PQ vote pool channel full, dropping vote", "target", vote.Data.TargetNumber)
	}
}

// SubscribeNewPQVoteEvent lets the protocol handler listen for newly accepted PQ votes.
func (pool *PQVotePool) SubscribeNewPQVoteEvent(ch chan<- core.NewPQVoteEvent) event.Subscription {
	return pool.scope.Track(pool.votesFeed.Subscribe(ch))
}

func (pool *PQVotePool) putIntoVotePool(vote *types.PQVoteEnvelope) bool {
	targetNumber := vote.Data.TargetNumber
	targetHash := vote.Data.TargetHash

	var headNumber uint64
	if pool.chain != nil {
		if head := pool.chain.CurrentBlock(); head != nil {
			headNumber = head.Number.Uint64()
		}
	}

	// Range check: (head-256, head+11].
	if headNumber > 0 {
		if targetNumber+lowerLimitOfVoteBlockNumber-1 < headNumber ||
			targetNumber > headNumber+upperLimitOfVoteBlockNumber {
			log.Debug("PQ vote outside accepted window, discarding",
				"target", targetNumber, "head", headNumber)
			return false
		}
	}

	voteHash := vote.Hash()

	pool.mu.Lock()
	if pool.receivedVotes.Contains(voteHash) {
		pool.mu.Unlock()
		return false
	}
	if box, ok := pool.curVotes[targetHash]; ok && len(box.voteMessages) >= maxPQCurVotesPerBlock {
		pool.mu.Unlock()
		log.Debug("PQ vote pool box full", "target", targetNumber)
		return false
	}
	pool.mu.Unlock()

	// Signature verification is expensive (ML-DSA-44 ~3ms); do it outside the lock.
	if err := vote.Verify(); err != nil {
		log.Warn("Failed to verify PQ vote", "err", err, "target", targetNumber)
		return false
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Re-check dedup after verify.
	if pool.receivedVotes.Contains(voteHash) {
		return false
	}

	box, ok := pool.curVotes[targetHash]
	if !ok {
		box = &PQVoteBox{
			blockNumber:  targetNumber,
			blockHash:    targetHash,
			voteMessages: make([]*types.PQVoteEnvelope, 0, maxPQCurVotesPerBlock),
		}
		pool.curVotes[targetHash] = box
	}
	box.voteMessages = append(box.voteMessages, vote)
	pool.receivedVotes.Add(voteHash)

	localPQCurVotesCounter.Inc(1)
	localPQReceivedVotesGauge.Update(int64(pool.receivedVotes.Cardinality()))

	// Broadcast to subscribers (protocol handler).
	pool.votesFeed.Send(core.NewPQVoteEvent{Vote: vote})

	log.Debug("PQ vote accepted", "target", targetNumber, "hash", voteHash)
	return true
}

// prune removes votes that are too old relative to the latest verified block.
func (pool *PQVotePool) prune(latestBlockNumber uint64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	for hash, box := range pool.curVotes {
		if box.blockNumber+lowerLimitOfVoteBlockNumber-1 < latestBlockNumber {
			for _, v := range box.voteMessages {
				pool.receivedVotes.Remove(v.Hash())
			}
			localPQCurVotesCounter.Dec(int64(len(box.voteMessages)))
			delete(pool.curVotes, hash)
		}
	}
	localPQReceivedVotesGauge.Update(int64(pool.receivedVotes.Cardinality()))
}

// GetVotes returns a snapshot of all currently pooled PQ votes.
func (pool *PQVotePool) GetVotes() []*types.PQVoteEnvelope {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	res := make([]*types.PQVoteEnvelope, 0)
	for _, box := range pool.curVotes {
		res = append(res, box.voteMessages...)
	}
	return res
}

// FetchVotesByBlockHash returns every vote for targetBlockHash whose
// SourceNumber matches the supplied sourceBlockNum. Mirrors VotePool.FetchVotesByBlockHash.
func (pool *PQVotePool) FetchVotesByBlockHash(targetBlockHash common.Hash, sourceBlockNum uint64) []*types.PQVoteEnvelope {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	box, ok := pool.curVotes[targetBlockHash]
	if !ok {
		return nil
	}
	var res []*types.PQVoteEnvelope
	for _, v := range box.voteMessages {
		if v.Data.SourceNumber == sourceBlockNum {
			res = append(res, v)
		}
	}
	return res
}
