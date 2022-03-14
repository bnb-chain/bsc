// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
package vote

import (
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	voteWhiteList         = 11
	blockLimitForTraverse = 11
)

type Rules interface {
	UnderRules(header *types.Header) bool
}

type votePool interface {
	PutVote(vote *types.VoteEnvelope)
}

// VoteManager will handle the vote produced by self.
type VoteManager struct {
	mux *event.TypeMux

	chain       *core.BlockChain
	chainconfig *params.ChainConfig

	pool votePool

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	signer  *VoteSigner
	journal *VoteJournal

	rules Rules
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain *core.BlockChain, journal *VoteJournal, signer *VoteSigner) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		signer:  signer,
		journal: journal,
	}

	voteManager.chainHeadSub = voteManager.chain.SubscribeChainHeadEvent(voteManager.chainHeadCh)

	go voteManager.loop()

	return voteManager, nil
}

func (voteManager *VoteManager) loop() {
	events := voteManager.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	dlEventCh := events.Chan()

	startVote := false
	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				startVote = false
			case downloader.FailedEvent:
				startVote = false
			case downloader.DoneEvent:
				startVote = true
			}
		case cHead := <-voteManager.chainHeadCh:
			if !startVote || cHead.Block == nil {
				continue
			}
			curHead := cHead.Block.Header()

			var lastLatestVoteNumber uint64
			lastLatestVote := voteManager.journal.latestVote
			if lastLatestVote == nil {
				lastLatestVoteNumber = 0
			} else {
				lastLatestVoteNumber = lastLatestVote.Data.BlockNumber
			}

			var newChainStack []*types.Header
			for i := 0; i < blockLimitForTraverse; i++ {
				if curHead == nil || curHead.Number.Uint64() <= lastLatestVoteNumber {
					break
				}
				newChainStack = append(newChainStack, curHead)
				curHead = voteManager.chain.GetHeader(curHead.ParentHash, curHead.Number.Uint64()-1)
			}

			for i := len(newChainStack) - 1; i >= 0; i-- {
				curBlockHeader := newChainStack[i]
				// Vote for curBlockHeader block.
				vote := &types.VoteData{
					BlockNumber: curBlockHeader.Number.Uint64(),
					BlockHash:   curBlockHeader.Hash(),
				}
				voteMessage := &types.VoteEnvelope{
					Data: vote,
				}
				// Put Vote into journal and VotesPool if we are active validator and allow to sign it.
				if ok := voteManager.UnderRules(curBlockHeader); ok {
					if err := voteManager.signer.SignVote(voteMessage); err != nil {
						log.Warn("Failed to sign vote", "err", err)
						continue
					}
					voteManager.journal.WriteVote(voteMessage)
					voteManager.pool.PutVote(voteMessage)
				}
			}
		}
	}
}

// Check if the produced header under the Rule1: Validators always vote once and only once on one height,
// Rule2: Validators always vote for the child of its previous vote within a predefined n blocks to avoid vote on two different
// forks of chain.
func (voteManager *VoteManager) UnderRules(header *types.Header) bool {

	latestVote := voteManager.journal.latestVote
	if latestVote == nil {
		return true
	}

	latestBlockNumber := latestVote.Data.BlockNumber
	latestBlockHash := latestVote.Data.BlockHash

	// Check for Rules.
	if header.Number.Uint64() > latestBlockNumber+voteWhiteList {
		return true
	}

	curBlockHeader := header
	if curBlockHeader.Number.Uint64() <= latestBlockNumber {
		return false
	}
	for curBlockHeader != nil && curBlockHeader.Number.Uint64() >= latestBlockNumber {
		if curBlockHeader.Number.Uint64() == latestBlockNumber {
			if curBlockHeader.Hash() == latestBlockHash {
				return true
			}
			break
		}
		curBlockHeader = voteManager.chain.GetHeader(curBlockHeader.ParentHash, curBlockHeader.Number.Uint64()-1)
	}

	return false
}
