package vote

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
)

const naturallyJustifiedDist = 15 // The distance to naturally justify a block

// VoteManager will handle the vote produced by self.
type VoteManager struct {
	mux *event.TypeMux

	chain       *core.BlockChain
	chainconfig *params.ChainConfig

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	pool    *VotePool
	signer  *VoteSigner
	journal *VoteJournal

	engine consensus.PoSA
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain *core.BlockChain, pool *VotePool, journalPath, blsPasswordPath, blsWalletPath string, engine consensus.PoSA) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		pool:   pool,
		engine: engine,
	}

	// Create voteSigner.
	voteSigner, err := NewVoteSigner(blsPasswordPath, blsWalletPath)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteSigner successfully")
	voteManager.signer = voteSigner

	// Create voteJournal
	voteJournal, err := NewVoteJournal(journalPath)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteJournal successfully")
	voteManager.journal = voteJournal

	// Subscribe to chain head event.
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

	startVote := true
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
			// Check if cur validator is within the validatorSet at curHead
			if !voteManager.engine.IsActiveValidatorAt(voteManager.chain, curHead) {
				continue
			}

			// Vote for curBlockHeader block.
			vote := &types.VoteData{
				TargetNumber: curHead.Number.Uint64(),
				TargetHash:   curHead.Hash(),
			}
			voteMessage := &types.VoteEnvelope{
				Data: vote,
			}

			// Put Vote into journal and VotesPool if we are active validator and allow to sign it.
			if ok, sourceNumber, sourceHash := voteManager.UnderRules(curHead); ok {
				if sourceHash == (common.Hash{}) {
					continue
				}

				voteMessage.Data.SourceNumber = sourceNumber
				voteMessage.Data.SourceHash = sourceHash

				if err := voteManager.signer.SignVote(voteMessage); err != nil {
					log.Error("Failed to sign vote", "err", err)
					votesSigningErrorMetric(vote.TargetNumber, vote.TargetHash).Inc(1)
					continue
				}
				if err := voteManager.journal.WriteVote(voteMessage); err != nil {
					log.Error("Failed to write vote into journal", "err", err)
					voteJournalError.Inc(1)
					continue
				}

				log.Debug("vote manager produced vote", "votedBlockNumber", voteMessage.Data.TargetNumber, "votedBlockHash", voteMessage.Data.TargetHash, "voteMessageHash", voteMessage.Hash())
				voteManager.pool.PutVote(voteMessage)
				votesManagerMetric(vote.TargetNumber, vote.TargetHash).Inc(1)
			}
		case <-voteManager.chainHeadSub.Err():
			return
		}
	}
}

// UnderRules checks if the produced header under the following rules:
// A validator must not publish two distinct votes for the same height. (Rule 1)
// A validator must not vote within the span of its other votes . (Rule 2)
// Validators always vote for their canonical chain’s latest block. (Rule 3)
func (voteManager *VoteManager) UnderRules(header *types.Header) (bool, uint64, common.Hash) {
	justifiedHeader := voteManager.engine.GetJustifiedHeader(voteManager.chain, header)
	if justifiedHeader == nil {
		log.Error("highestJustifiedHeader at cur header is nil", "curHeader's BlockNumber", header.Number.Uint64(), "curHeader's BlockHash", header.Hash())
		return false, 0, common.Hash{}
	}

	sourceNumber := justifiedHeader.Number.Uint64()
	sourceHash := justifiedHeader.Hash()
	targetNumber := header.Number.Uint64()

	voteDataBuffer := voteManager.journal.voteDataBuffer
	//Rule 1:  A validator must not publish two distinct votes for the same height.
	if voteDataBuffer.Contains(header.Number.Uint64()) {
		return false, 0, common.Hash{}
	}

	//Rule 2: A validator must not vote within the span of its other votes.
	for blockNumber := sourceNumber + 1; blockNumber < targetNumber; blockNumber++ {
		if voteDataBuffer.Contains(blockNumber) {
			voteData, ok := voteDataBuffer.Get(blockNumber)
			if !ok {
				log.Error("Failed to get voteData info from LRU cache.")
				continue
			}
			if voteData.(*types.VoteData).SourceNumber > sourceNumber {
				return false, 0, common.Hash{}
			}
		}
	}
	for blockNumber := targetNumber; blockNumber <= targetNumber+naturallyJustifiedDist; blockNumber++ {
		if voteDataBuffer.Contains(blockNumber) {
			voteData, ok := voteDataBuffer.Get(blockNumber)
			if !ok {
				log.Error("Failed to get voteData info from LRU cache.")
				continue
			}
			if voteData.(*types.VoteData).SourceNumber < sourceNumber {
				return false, 0, common.Hash{}
			}
		}
	}

	// Rule 3: Validators always vote for their canonical chain’s latest block.
	// Since the header subscribed to is the canonical chain, so this rule is satisified by default.
	return true, sourceNumber, sourceHash
}

// Metrics to monitor if voteManager worked in the expetected logic.
func votesManagerMetric(blockNumber uint64, blockHash common.Hash) metrics.Gauge {
	return metrics.GetOrRegisterGauge(fmt.Sprintf("voteManager/blockNumber/%d/blockHash/%s", blockNumber, blockHash), nil)
}
