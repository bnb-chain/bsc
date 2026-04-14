// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.

package vote

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

var pqVotesManagerCounter = metrics.NewRegisteredCounter("pqVotesManager/local", nil)

// PQVoteManager signs blocks with ML-DSA-44 and inserts the PQ vote envelopes
// into the PQVotePool. It is the post-quantum counterpart of VoteManager.
//
// Activation is gated on chainConfig.IsPQFork — before the fork timestamp the
// manager stays idle so the legacy BLS VoteManager remains the only voter.
type PQVoteManager struct {
	eth   Backend
	chain *core.BlockChain

	highestVerifiedBlockCh  chan core.HighestVerifiedBlockEvent
	highestVerifiedBlockSub event.Subscription

	pool   *PQVotePool
	signer *PQVoteSigner

	// Reused only to get fork state, block interval, and active-validator check.
	engine *parlia.Parlia
}

// NewPQVoteManager wires up the PQ vote manager. signer and pool are required;
// the manager starts its loop goroutine immediately.
func NewPQVoteManager(eth Backend, chain *core.BlockChain, pool *PQVotePool, signer *PQVoteSigner, engine *parlia.Parlia) (*PQVoteManager, error) {
	m := &PQVoteManager{
		eth:                    eth,
		chain:                  chain,
		highestVerifiedBlockCh: make(chan core.HighestVerifiedBlockEvent, highestVerifiedBlockChanSize),
		pool:                   pool,
		signer:                 signer,
		engine:                 engine,
	}
	metrics.GetOrRegisterLabel("miner-info", nil).Mark(map[string]interface{}{
		"PQVoteKey": common.Bytes2Hex(signer.PubKey[:]),
	})

	m.highestVerifiedBlockSub = chain.SubscribeHighestVerifiedHeaderEvent(m.highestVerifiedBlockCh)

	go m.loop()
	return m, nil
}

func (m *PQVoteManager) loop() {
	log.Debug("PQ vote manager loop started")
	defer m.highestVerifiedBlockSub.Unsubscribe()

	events := m.eth.EventMux().Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	dlEventCh := events.Chan()

	startVote := true
	blockCountSinceMining := 0
	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				startVote = false
			case downloader.FailedEvent, downloader.DoneEvent:
				startVote = true
			}

		case cHead := <-m.highestVerifiedBlockCh:
			if !startVote || cHead.Header == nil {
				continue
			}
			if !m.eth.IsMining() {
				blockCountSinceMining = 0
				continue
			}
			blockCountSinceMining++
			if blockCountSinceMining <= blocksNumberSinceMining {
				continue
			}

			curHead := cHead.Header

			// Fork gate — do nothing until the PQ fork is active for this block.
			cfg := m.chain.Config()
			if !cfg.IsPQFork(curHead.Number, curHead.Time) {
				continue
			}

			// Must be a known active validator AND our ML-DSA pubkey must match the one on chain.
			if !m.engine.IsActivePQValidatorAt(m.chain, curHead,
				func(pqPubKey *types.PQPublicKey) bool {
					return bytes.Equal(m.signer.PubKey[:], pqPubKey[:])
				}) {
				log.Debug("local PQ vote key is not an active validator at curHead",
					"number", curHead.Number)
				continue
			}

			sourceNumber, sourceHash, err := m.engine.GetJustifiedNumberAndHash(m.chain, []*types.Header{curHead})
			if err != nil {
				log.Debug("PQ vote: failed to get justified source", "err", err)
				continue
			}
			if sourceHash == (common.Hash{}) {
				continue
			}

			voteMessage := &types.PQVoteEnvelope{
				Data: &types.VoteData{
					SourceNumber: sourceNumber,
					SourceHash:   sourceHash,
					TargetNumber: curHead.Number.Uint64(),
					TargetHash:   curHead.Hash(),
				},
			}

			if err := m.signer.SignVote(voteMessage); err != nil {
				log.Error("Failed to sign PQ vote", "err", err,
					"target", voteMessage.Data.TargetNumber)
				continue
			}

			log.Info("PQ vote produced",
				"target", voteMessage.Data.TargetNumber,
				"source", voteMessage.Data.SourceNumber,
				"hash", voteMessage.Hash())
			m.pool.PutVote(voteMessage)
			pqVotesManagerCounter.Inc(1)

		case <-m.highestVerifiedBlockSub.Err():
			log.Debug("PQ vote manager: chainHead subscription closed")
			return
		}
	}
}
