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
	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	preDefinedBlocks = 15
)

type Rules interface {
	UnderRules(header *types.Header) bool
}

// VoteManager will handle the vote produced by self.
type VoteManager struct {
	mux *event.TypeMux

	chain       blockChain
	chainconfig *params.ChainConfig

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	signer  *VoteSigner
	journal *VoteJournal

	voteSet mapset.Set

	votesFeed event.Feed
	scope     event.SubscriptionScope

	rules Rules
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain blockChain, journal *VoteJournal, signer *VoteSigner) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		signer:  signer,
		journal: journal,

		voteSet: mapset.NewSet(),
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

			curBlock := cHead.Block
			// Vote for curBlock.
			vote := &types.VoteData{
				BlockNumber: curBlock.NumberU64(),
				BlockHash:   curBlock.Hash(),
			}
			voteMessage := &types.VoteEnvelope{
				Data: vote,
			}
			// Put Vote into journal and VotesPool if we are active validator and allow to sign it.
			if ok := voteManager.rules.UnderRules(curBlock.Header()); ok {
				if err := voteManager.signer.SignVote(voteMessage); err != nil {
					log.Warn("Failed to sign vote", "err", err)
					continue
				}
				voteManager.journal.WriteVote(voteMessage)
				voteManager.votesFeed.Send(voteMessage)
			}

		}

	}

}

func (voteManager *VoteManager) SubscribeNewVotesForPut(ch chan<- *types.VoteEnvelope) event.Subscription {
	return voteManager.scope.Track(voteManager.votesFeed.Subscribe(ch))
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
	if header.Number.Uint64()-latestBlockNumber > preDefinedBlocks {
		return true
	}

	curBlockHeader := header
	if curBlockHeader.Number.Uint64() <= latestBlockNumber {
		return false
	}
	for curBlockHeader.Number.Uint64() >= latestBlockNumber {
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
