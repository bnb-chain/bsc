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
	"sync"

	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// VoteManager will handle the vote produced by self.
type VoteManager struct {
	mu  sync.RWMutex
	mux *event.TypeMux

	chain       blockChain
	chainconfig *params.ChainConfig

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	vp *VotePool // Pointer to VotesPool component

	vj *VoteJournal // Pointer to VoteJournal component

	voteSet mapset.Set
	isReady chan bool

	bls BLS
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain blockChain, vp *VotePool, vj *VoteJournal) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		vp: vp,
		vj: vj,

		voteSet: mapset.NewSet(),
		isReady: make(chan bool, 1),
	}

	if err := voteManager.vj.LoadVotesJournal(); err != nil {
		log.Warn("Failed to load votes journal", "err", err)
		return voteManager, err
	}

	voteManager.chainHeadSub = voteManager.chain.SubscribeChainHeadEvent(voteManager.chainHeadCh)

	go voteManager.loop()

	return voteManager, nil
}

func (vm *VoteManager) loop() {

	events := vm.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
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
		case cHead := <-vm.chainHeadCh:
			if startVote {
				if cHead.Block == nil {
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
				if ok := vm.IsUnderRules(curBlock.Header()); ok {
					hash := voteMessage.CalcVoteHash()
					signature := vm.bls.Sign(hash)
					voteMessage.Signature = signature

					vm.vj.WriteVotesJournal(voteMessage)
					vm.vp.PutVote(voteMessage)

				}

			}
		}

	}

}

// Check if the produced header under the Rule1: Validators always vote once and only once on one height,
// Rule2: Validators always vote for the child of its previous vote within a predefined n blocks to avoid vote on two different
// forks of chain.
func (vm *VoteManager) IsUnderRules(header *types.Header) bool {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	journalBuffer := vm.vj.journalBuffer
	// Check for Rule.
	isValid := false

	if len(journalBuffer) == 0 {
		isValid = true
	} else {
		vote := journalBuffer[len(journalBuffer)-1]
		blockNumber := vote.Data.BlockNumber
		blockHash := vote.Data.BlockHash
		curBlockHeader := header

		for curBlockHeader.Number.Uint64() >= blockNumber {
			if curBlockHeader.Number.Uint64() == blockNumber {
				if curBlockHeader.Hash() == blockHash {
					isValid = true
				}
				break
			}
			curBlockHeader = vm.chain.GetHeader(curBlockHeader.ParentHash, curBlockHeader.Number.Uint64()-1)
		}

	}
	return isValid
}
