package vote

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"

	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
)

const (
	maxCurVoteAmountPerBlock    = 21
	maxFutureVoteAmountPerBlock = 50

	voteBufferForPut            = 256
	lowerLimitOfVoteBlockNumber = 256
	upperLimitOfVoteBlockNumber = 11

	chainHeadChanSize = 10 // chainHeadChanSize is the size of channel listening to ChainHeadEvent.
)

var errInvalidVote = errors.New("invalid Vote")

var (
	localCurVotesGauge    = metrics.NewRegisteredGauge("curVotes/local", nil)
	localFutureVotesGauge = metrics.NewRegisteredGauge("futureVotes/local", nil)

	localReceivedVotesGauge = metrics.NewRegisteredGauge("receivedVotes/local", nil)

	localCurVotesPqGauge    = metrics.NewRegisteredGauge("curVotesPq/local", nil)
	localFutureVotesPqGauge = metrics.NewRegisteredGauge("futureVotesPq/local", nil)
)

type VoteBox struct {
	blockNumber  uint64
	voteMessages []*types.VoteEnvelope
}

type VotePool struct {
	chain       *core.BlockChain
	chainconfig *params.ChainConfig
	mu          sync.RWMutex

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

	engine consensus.Engine
}

type votesPriorityQueue []*types.VoteData

func NewVotePool(chainconfig *params.ChainConfig, chain *core.BlockChain, engine consensus.Engine) *VotePool {
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
		engine:        engine,
	}

	// Subscribe events from blockchain and start the main event loop.
	votePool.chainHeadSub = votePool.chain.SubscribeChainHeadEvent(votePool.chainHeadCh)
	if posa, ok := engine.(consensus.PoSA); ok {
		posa.SetVotePool(votePool)
	}

	go votePool.loop()
	return votePool
}

// loop is the vote pool's main even loop, waiting for and reacting to outside blockchain events and votes channel event.
func (pool *VotePool) loop() {
	for {
		select {
		// Handle ChainHeadEvent.
		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				latestBlockNumber := ev.Block.NumberU64()
				pool.prune(latestBlockNumber)
				pool.transferVotesFromFutureToCur(ev.Block.Header())
			}
		case <-pool.chainHeadSub.Err():
			return

		// Handle votes channel and put the vote into vote pool.
		case vote := <-pool.votesCh:
			pool.putIntoVotePool(vote)
		}
	}
}

func (pool *VotePool) PutVote(vote *types.VoteEnvelope) {
	pool.votesCh <- vote
}

func (pool *VotePool) putIntoVotePool(vote *types.VoteEnvelope) bool {
	voteBlockNumber := vote.Data.BlockNumber
	voteBlockHash := vote.Data.BlockHash
	header := pool.chain.CurrentBlock().Header()
	headNumber := header.Number.Uint64()

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
		// Verify if the vote comes from valid validators based on voteAddress (BLSPublicKey).
		if posa, ok := pool.engine.(consensus.PoSA); ok {
			if !posa.VerifyVote(pool.chain, vote) {
				return false
			}
		}
		// Verify bls signature.
		if err := VerifyVoteWithBLS(vote); err != nil {
			log.Error("Failed to verify voteMessage", "err", err)
			return false
		}
		votes = pool.curVotes
		votesPq = pool.curVotesPq
	}

	voteHash := vote.Hash()
	if ok := pool.basicVerify(vote, headNumber, votes, isFutureVote, voteHash); !ok {
		return false
	}

	pool.putVote(votes, votesPq, vote, voteData, voteHash)

	if !isFutureVote {
		// Send vote for handler usage of broadcasting to peers.
		voteEv := core.NewVoteEvent{vote}
		pool.votesFeed.Send(voteEv)
		localCurVotesGauge.Inc(1)
		localCurVotesPqGauge.Inc(1)
	} else {
		localFutureVotesGauge.Inc(1)
		localFutureVotesPqGauge.Inc(1)
	}

	votesPerBlockHashMetric(voteBlockHash).Inc(1)
	localReceivedVotesGauge.Inc(1)
	return true
}

func (pool *VotePool) SubscribeNewVoteEvent(ch chan<- core.NewVoteEvent) event.Subscription {
	return pool.scope.Track(pool.votesFeed.Subscribe(ch))
}

func (pool *VotePool) putVote(m map[common.Hash]*VoteBox, votesPq *votesPriorityQueue, vote *types.VoteEnvelope, voteData *types.VoteData, voteHash common.Hash) {
	voteBlockHash := vote.Data.BlockHash
	voteBlockNumber := vote.Data.BlockNumber

	log.Info("The vote info to put is:", "voteBlockNumber=", voteBlockNumber, "voteBlockHash=", voteBlockHash)

	pool.mu.Lock()
	defer pool.mu.Unlock()
	if _, ok := m[voteBlockHash]; !ok {
		// Push into votes priorityQueue if not exist in corresponding votes Map.
		// To be noted: will not put into priorityQueue if exists in map to avoid duplicate element with the same voteData.
		heap.Push(votesPq, voteData)
		voteBox := &VoteBox{
			blockNumber:  voteBlockNumber,
			voteMessages: make([]*types.VoteEnvelope, 0, maxCurVoteAmountPerBlock),
		}
		m[voteBlockHash] = voteBox
	}

	// Put into corresponding votes map.
	m[voteBlockHash].voteMessages = append(m[voteBlockHash].voteMessages, vote)
	// Add into received vote to avoid future duplicated vote comes.
	pool.receivedVotes.Add(voteHash)
	log.Info("VoteHash put into votepool is:", "voteHash=", voteHash)

}

func (pool *VotePool) transferVotesFromFutureToCur(latestBlockHeader *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	curPq, futurePq := pool.curVotesPq, pool.futureVotesPq
	curVotes, futureVotes := pool.curVotes, pool.futureVotes

	for futurePq.Len() > 0 && futurePq.Peek().BlockNumber <= latestBlockHeader.Number.Uint64() {
		blockHash := futurePq.Peek().BlockHash
		voteBox := futureVotes[blockHash]
		validVotes := make([]*types.VoteEnvelope, 0, len(voteBox.voteMessages))
		for _, vote := range voteBox.voteMessages {
			// Verify if the vote comes from valid validators based on voteAddress (BLSPublicKey).
			if posa, ok := pool.engine.(consensus.PoSA); ok {
				if !posa.VerifyVote(pool.chain, vote) {
					continue
				}
			}
			// Verify the vote from futureVotes.
			if err := VerifyVoteWithBLS(vote); err == nil {
				// In the process of transfer, send valid vote to votes channel for handler usage
				voteEv := core.NewVoteEvent{vote}
				pool.votesFeed.Send(voteEv)
				validVotes = append(validVotes, vote)
			}
		}

		voteData := heap.Pop(futurePq)
		if _, ok := curVotes[blockHash]; !ok {
			heap.Push(curPq, voteData)
			curVotes[blockHash] = &VoteBox{voteBox.blockNumber, validVotes}
			localCurVotesPqGauge.Inc(1)
		} else {
			curVotes[blockHash].voteMessages = append(curVotes[blockHash].voteMessages, validVotes...)
		}

		delete(futureVotes, blockHash)

		localCurVotesGauge.Inc(1)
		localFutureVotesGauge.Dec(1)
		localFutureVotesPqGauge.Dec(1)
	}
}

// Prune old data of duplicationSet, curVotePq and curVotesMap.
func (pool *VotePool) prune(latestBlockNumber uint64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	curVotes := pool.curVotes
	curVotesPq := pool.curVotesPq

	for curVotesPq.Len() > 0 && curVotesPq.Peek().BlockNumber+lowerLimitOfVoteBlockNumber-1 < latestBlockNumber {
		// Prune curPriorityQueue.
		blockHash := heap.Pop(curVotesPq).(*types.VoteData).BlockHash
		voteMessages := curVotes[blockHash].voteMessages
		// Prune duplicationSet.
		for _, voteMessage := range voteMessages {
			voteHash := voteMessage.Hash()
			pool.receivedVotes.Remove(voteHash)
		}
		// Prune curVotes Map.
		delete(curVotes, blockHash)

		localCurVotesGauge.Dec(1)
		localCurVotesPqGauge.Dec(1)
		localReceivedVotesGauge.Dec(1)
		votesPerBlockHashMetric(blockHash).Dec(1)
	}
}

// GetVotes as batch.
func (pool *VotePool) GetVotes() []*types.VoteEnvelope {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	votesRes := make([]*types.VoteEnvelope, 0)
	curVotes := pool.curVotes
	for _, voteBox := range curVotes {
		votesRes = append(votesRes, voteBox.voteMessages...)
	}
	return votesRes
}

func (pool *VotePool) FetchVoteByHash(blockHash common.Hash) []*types.VoteEnvelope {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if _, ok := pool.curVotes[blockHash]; ok {
		return pool.curVotes[blockHash].voteMessages
	}
	return nil
	//TODO: More strict condition is needed.
}

func (pool *VotePool) basicVerify(vote *types.VoteEnvelope, headNumber uint64, m map[common.Hash]*VoteBox, isFutureVote bool, voteHash common.Hash) bool {
	voteBlockNumber := vote.Data.BlockNumber
	voteBlockHash := vote.Data.BlockHash

	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// Check duplicate voteMessage firstly.
	if pool.receivedVotes.Contains(voteHash) {
		log.Debug("Vote pool already contained the same vote", "voteHash=", voteHash)
		return false
	}
	// Make sure in the range currentHeight-256~currentHeight+11.
	if voteBlockNumber+lowerLimitOfVoteBlockNumber-1 < headNumber || voteBlockNumber > headNumber+upperLimitOfVoteBlockNumber {
		log.Warn("BlockNumber of vote is outside the range of header-256~header+11")
		return false
	}
	// To prevent DOS attacks, make sure no more than 50 votes for the same blockHash if it's futureVotes
	// No more than 21 votes per blockHash if not futureVotes and no more than 50 votes per blockHash if futureVotes
	maxVoteAmountPerBlock := maxCurVoteAmountPerBlock
	if isFutureVote {
		maxVoteAmountPerBlock = maxFutureVoteAmountPerBlock
	}
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

func (pq *votesPriorityQueue) Peek() *types.VoteData {
	if pq.Len() == 0 {
		return nil
	}
	return (*pq)[0]
}

func votesPerBlockHashMetric(blockHash common.Hash) metrics.Gauge {
	return metrics.GetOrRegisterGauge(fmt.Sprintf("blockHash/%s", blockHash), nil)
}
