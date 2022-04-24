package vote

import (
	"container/heap"
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
	upperLimitOfVoteBlockNumber = 13

	chainHeadChanSize = 10 // chainHeadChanSize is the size of channel listening to ChainHeadEvent.
)

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

	engine consensus.PoSA
}

type votesPriorityQueue []*types.VoteData

func NewVotePool(chainconfig *params.ChainConfig, chain *core.BlockChain, engine consensus.PoSA) *VotePool {
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
	voteBlockNumber := vote.Data.TargetNumber
	voteBlockHash := vote.Data.TargetHash
	header := pool.chain.CurrentBlock().Header()
	headNumber := header.Number.Uint64()

	voteData := &types.VoteData{
		TargetNumber: voteBlockNumber,
		TargetHash:   voteBlockHash,
	}

	var votes map[common.Hash]*VoteBox
	var votesPq *votesPriorityQueue
	isFutureVote := false

	voteBlock := pool.chain.GetHeaderByHash(voteBlockHash)
	if voteBlock == nil {
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

	pool.putVote(votes, votesPq, vote, voteData, voteHash, isFutureVote)

	if !isFutureVote {
		// Send vote for handler usage of broadcasting to peers.
		voteEv := core.NewVoteEvent{Vote: vote}
		pool.votesFeed.Send(voteEv)
	}

	return true
}

func (pool *VotePool) SubscribeNewVoteEvent(ch chan<- core.NewVoteEvent) event.Subscription {
	return pool.scope.Track(pool.votesFeed.Subscribe(ch))
}

func (pool *VotePool) putVote(m map[common.Hash]*VoteBox, votesPq *votesPriorityQueue, vote *types.VoteEnvelope, voteData *types.VoteData, voteHash common.Hash, isFutureVote bool) {
	voteBlockHash := vote.Data.TargetHash
	voteBlockNumber := vote.Data.TargetNumber

	log.Info("The vote info to put is:", "voteBlockNumber", voteBlockNumber, "voteBlockHash", voteBlockHash)

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

		if isFutureVote {
			localFutureVotesPqGauge.Inc(1)
		} else {
			localCurVotesPqGauge.Inc(1)
		}
	}

	// Put into corresponding votes map.
	m[voteBlockHash].voteMessages = append(m[voteBlockHash].voteMessages, vote)
	// Add into received vote to avoid future duplicated vote comes.
	pool.receivedVotes.Add(voteHash)
	log.Info("VoteHash put into votepool is:", "voteHash", voteHash)

	if isFutureVote {
		localFutureVotesGauge.Inc(1)
	} else {
		localCurVotesGauge.Inc(1)
	}
	votesPerBlockHashMetric(voteBlockHash).Inc(1)
	localReceivedVotesGauge.Inc(1)
}

func (pool *VotePool) transferVotesFromFutureToCur(latestBlockHeader *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	futurePq := pool.futureVotesPq
	latestBlockNumber := latestBlockHeader.Number.Uint64()

	// For vote before latestBlockHeader-13, transfer to cur if valid.
	for futurePq.Len() > 0 && futurePq.Peek().TargetNumber+upperLimitOfVoteBlockNumber < latestBlockNumber {
		blockHash := futurePq.Peek().TargetHash
		pool.transfer(blockHash)
	}

	// For vote within latestBlockHeader-13 ~ latestBlockHeader, only transfer the the vote inside the local fork.
	futurePqBuffer := make([]*types.VoteData, 0)
	for futurePq.Len() > 0 && futurePq.Peek().TargetNumber <= latestBlockNumber {
		blockHash := futurePq.Peek().TargetHash
		header := pool.chain.GetHeaderByHash(blockHash)
		if header == nil {
			// Put into pq buffer used for later put again into futurePq
			futurePqBuffer = append(futurePqBuffer, heap.Pop(futurePq).(*types.VoteData))
			continue
		}
		pool.transfer(blockHash)
	}

	for _, voteData := range futurePqBuffer {
		heap.Push(futurePq, voteData)
	}
}

func (pool *VotePool) transfer(blockHash common.Hash) {
	curPq, futurePq := pool.curVotesPq, pool.futureVotesPq
	curVotes, futureVotes := pool.curVotes, pool.futureVotes

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
			voteEv := core.NewVoteEvent{Vote: vote}
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

	localCurVotesGauge.Inc(int64(len(validVotes)))
	localFutureVotesGauge.Dec(int64(len(voteBox.voteMessages)))
	localFutureVotesPqGauge.Dec(1)
}

// Prune old data of duplicationSet, curVotePq and curVotesMap.
func (pool *VotePool) prune(latestBlockNumber uint64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	curVotes := pool.curVotes
	curVotesPq := pool.curVotesPq

	for curVotesPq.Len() > 0 && curVotesPq.Peek().TargetNumber+lowerLimitOfVoteBlockNumber-1 < latestBlockNumber {
		// Prune curPriorityQueue.
		blockHash := heap.Pop(curVotesPq).(*types.VoteData).TargetHash
		voteMessages := curVotes[blockHash].voteMessages
		// Prune duplicationSet.
		for _, voteMessage := range voteMessages {
			voteHash := voteMessage.Hash()
			pool.receivedVotes.Remove(voteHash)
		}
		// Prune curVotes Map.
		delete(curVotes, blockHash)

		localCurVotesGauge.Dec(int64(len(voteMessages)))
		localCurVotesPqGauge.Dec(1)
		localReceivedVotesGauge.Dec(int64(len(voteMessages)))
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
	voteBlockNumber := vote.Data.TargetNumber
	voteBlockHash := vote.Data.TargetHash

	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// Check duplicate voteMessage firstly.
	if pool.receivedVotes.Contains(voteHash) {
		log.Debug("Vote pool already contained the same vote", "voteHash", voteHash)
		return false
	}
	// Make sure in the range currentHeight-256~currentHeight+13.
	if voteBlockNumber+lowerLimitOfVoteBlockNumber-1 < headNumber || voteBlockNumber > headNumber+upperLimitOfVoteBlockNumber {
		log.Warn("BlockNumber of vote is outside the range of header-256~header+13")
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
	return pq[i].TargetNumber < pq[j].TargetNumber
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
