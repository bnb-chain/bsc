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
	"container/heap"
	"errors"
	"sync"

	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	maxVoteAmountPerBlock       = 21
	voteBufferForPut            = 256
	lowerLimitOfVoteBlockNumber = 256
	upperLimitOfVoteBlockNumber = 11
	voteMaxSize                 = 128 * 1024 // Set the maxSize of vote as 128KB.

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10
)

var invalidVote = errors.New("invalid Vote")

type blockChain interface {
	CurrentBlock() *types.Block
	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
	GetHeader(hash common.Hash, number uint64) *types.Header
}

type VoteBox struct {
	blockNumber  uint64
	voteMessages []*types.VoteEnvelope
}

type VotePool struct {
	chain       blockChain
	chainconfig *params.ChainConfig
	mu          sync.RWMutex

	voteManager *VoteManager

	votesFeed event.Feed
	scope     event.SubscriptionScope

	receivedVotes mapset.Set

	curVotes    map[common.Hash]*VoteBox
	futureVotes map[common.Hash]*VoteBox

	curVotesPq    *votesPriorityQueue
	futureVotesPq *votesPriorityQueue

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	votesCh chan *types.VoteEnvelope
	voteSub event.Subscription
}

type votesPriorityQueue []*types.VoteData

func NewVotePool(chainconfig *params.ChainConfig, chain blockChain) *VotePool {

	votePool := &VotePool{
		chain:         chain,
		chainconfig:   chainconfig,
		receivedVotes: mapset.NewSet(),
		curVotes:      make(map[common.Hash]*VoteBox),
		futureVotes:   make(map[common.Hash]*VoteBox),
		curVotesPq:    &votesPriorityQueue{},
		futureVotesPq: &votesPriorityQueue{},
		chainHeadCh:   make(chan core.ChainHeadEvent, chainHeadChanSize),
		votesCh:       make(chan *types.VoteEnvelope, voteBufferForPut),
	}

	// Subscribe events from blockchain and start the main event loop.
	votePool.chainHeadSub = votePool.chain.SubscribeChainHeadEvent(votePool.chainHeadCh)

	// Subscribe event from voteManager feed and put the votes to vote pool.
	votePool.voteSub = votePool.voteManager.SubscribeNewVotesForPut(votePool.votesCh)

	go votePool.loop()

	return votePool
}

// loop is the vote pool's main even loop, waiting for and reacting to outside blockchain events.
func (pool *VotePool) loop() {

	for {
		select {
		// Handle ChainHeadEvent
		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				latestBlockNumber := ev.Block.NumberU64()
				pool.prune(latestBlockNumber)
				pool.transferVotesFromFutureToCur(latestBlockNumber)
			}
		case <-pool.chainHeadSub.Err():
			return
		case vote := <-pool.votesCh:
			pool.PutVote(vote)
		case <-pool.voteSub.Err():
			return
		}
	}

}

func (pool *VotePool) PutVote(vote *types.VoteEnvelope) bool {

	voteBlockNumber := vote.Data.BlockNumber
	voteBlockHash := vote.Data.BlockHash
	headNumber := pool.chain.CurrentBlock().NumberU64()

	voteData := &types.VoteData{
		BlockNumber: voteBlockNumber,
		BlockHash:   voteBlockHash,
	}

	var votes map[common.Hash]*VoteBox
	var votesPq *votesPriorityQueue
	isFutureVote := false

	if voteBlockNumber > headNumber {
		votes = pool.futureVotes
		votesPq = pool.futureVotesPq
		isFutureVote = true
	} else {
		if err := VerifyVoteWithBLS(vote); err != nil {
			log.Error("Failed to verify voteMessage", "err", err)
			return false
		}
		votes = pool.curVotes
		votesPq = pool.curVotesPq
	}

	if ok := pool.basicVerify(vote, headNumber, votes, isFutureVote); !ok {
		log.Error("voteMessage is invalid", "err", invalidVote)
		return false
	}

	pool.putVote(votes, vote)
	heap.Push(votesPq, voteData)

	if !isFutureVote {
		pool.votesFeed.Send(vote)
	}

	return true
}

func (pool *VotePool) SubscribeNewVotesEvent(ch chan<- []*types.VoteEnvelope) event.Subscription {
	return pool.scope.Track(pool.votesFeed.Subscribe(ch))
}

func (pool *VotePool) putVote(m map[common.Hash]*VoteBox, vote *types.VoteEnvelope) {
	voteBlockHash := vote.Data.BlockHash
	voteBlockNumber := vote.Data.BlockNumber

	if _, ok := m[voteBlockHash]; !ok {
		voteBox := &VoteBox{
			blockNumber:  voteBlockNumber,
			voteMessages: make([]*types.VoteEnvelope, 0, maxVoteAmountPerBlock),
		}
		m[voteBlockHash] = voteBox
	}

	m[voteBlockHash].voteMessages = append(m[voteBlockHash].voteMessages, vote)

}

func (pool *VotePool) transferVotesFromFutureToCur(latestBlockNumber uint64) {
	curPq, futurePq := pool.curVotesPq, pool.futureVotesPq
	curVotes, futureVotes := pool.curVotes, pool.futureVotes

	for futurePq.Len() > 0 && (*futurePq)[0].BlockNumber <= latestBlockNumber {
		blockHash := (*futurePq)[0].BlockHash
		voteBox := futureVotes[blockHash]
		validVotes := make([]*types.VoteEnvelope, 0, len(voteBox.voteMessages))
		for _, vote := range voteBox.voteMessages {
			// Verify the vote from futureVotes.
			if err := VerifyVoteWithBLS(vote); err == nil {
				// Send valid vote to votes channel in the process of transfer.
				pool.votesFeed.Send(vote)
				validVotes = append(validVotes, vote)
			}
		}

		curVotes[blockHash] = &VoteBox{voteBox.blockNumber, validVotes}

		voteData := heap.Pop(futurePq)
		heap.Push(curPq, voteData)

		delete(futureVotes, blockHash)
	}
}

// Prune duplicationSet, priorityQueue and Map of curVotes.
func (pool *VotePool) prune(lastestBlockNumber uint64) {

	pool.mu.Lock()

	curVotes := pool.curVotes
	curBlockPq := pool.curVotesPq

	for curBlockPq.Len() > 0 && (*curBlockPq)[0].BlockNumber < lastestBlockNumber-lowerLimitOfVoteBlockNumber {
		blockHash := (*curBlockPq)[0].BlockHash

		// Prune curPriorityQueue.
		curBlockPq.Pop()
		voteMessages := curVotes[blockHash].voteMessages

		// Prune duplicationSet.
		for _, voteMessage := range voteMessages {
			voteHash := voteMessage.Hash()
			pool.receivedVotes.Remove(voteHash)
		}

		// Prune curVotes Map.
		delete(curVotes, blockHash)

	}
	pool.mu.Unlock()

}

// Get votes as batch.
func (pool *VotePool) GetVotes() []*types.VoteEnvelope {
	pool.mu.RLock()
	defer pool.mu.Unlock()

	votesRes := make([]*types.VoteEnvelope, 0)

	curVotes := pool.curVotes
	for _, voteBox := range curVotes {
		votesRes = append(votesRes, voteBox.voteMessages...)
	}

	return votesRes
}

func (pool *VotePool) FetchAvailableVotes(blockHash common.Hash) (*VoteBox, bool) {
	pool.mu.RLock()
	defer pool.mu.Unlock()
	if vote, ok := pool.curVotes[blockHash]; ok && len(vote.voteMessages) >= maxVoteAmountPerBlock*3/4 {
		return vote, true
	}
	return nil, false
}

func (pool *VotePool) basicVerify(vote *types.VoteEnvelope, headNumber uint64, m map[common.Hash]*VoteBox, isFutureVote bool) bool {

	voteHash := vote.Hash()
	voteBlockNumber := vote.Data.BlockNumber
	voteBlockHash := vote.Data.BlockHash

	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Check duplicate voteMessage firstly.
	if !pool.receivedVotes.Add(voteHash) {
		return false
	}

	// Make sure in the range currentHeight-256~currentHeight+11.
	if voteBlockNumber < headNumber-lowerLimitOfVoteBlockNumber || voteBlockNumber > headNumber+upperLimitOfVoteBlockNumber {
		return false
	}

	// Reject votes over defined size to prevent DOS attacks.
	if isFutureVote {
		return vote.Size() <= voteMaxSize
	}

	// No more than 21 votes for the same blockHash if not futureVotes.
	if voteBox, ok := m[voteBlockHash]; ok {
		return len(voteBox.voteMessages) <= maxVoteAmountPerBlock
	}

	return true
}

func (pq votesPriorityQueue) Less(i, j int) bool {
	return pq[i].BlockNumber < pq[j].BlockNumber
}

func (pq votesPriorityQueue) Len() int {
	return len(pq)
}

func (pq votesPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *votesPriorityQueue) Push(vote interface{}) {
	curVote := vote.(*types.VoteData)
	*pq = append(*pq, curVote)
}

func (pq *votesPriorityQueue) Pop() interface{} {
	tmp := *pq
	l := len(tmp)
	var res interface{} = tmp[l-1]
	*pq = tmp[:l-1]
	return res
}
