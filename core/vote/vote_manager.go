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
	log.Debug("vote manager routine loop started")
	events := voteManager.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		log.Debug("vote manager loop defer func occur")
		if !events.Closed() {
			log.Debug("event not closed, unsubscribed by vote manager loop")
			events.Unsubscribe()
		}
	}()

	dlEventCh := events.Chan()

	startVote := true
	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				log.Debug("dlEvent is nil, continue")
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				log.Debug("downloader is in startEvent mode, will not startVote")
				startVote = false
			case downloader.FailedEvent:
				log.Debug("downloader is in FailedEvent mode, set startVote flag as true")
				startVote = true
			case downloader.DoneEvent:
				log.Debug("downloader is in DoneEvent mode, set the startVote flag to true")
				startVote = true
			}
		case cHead := <-voteManager.chainHeadCh:
			if !startVote {
				log.Debug("startVote flag is false, continue")
				continue
			}

			if cHead.Block == nil {
				log.Debug("cHead.Block is nil, continue")
				continue
			}

			curHead := cHead.Block.Header()
			// Check if cur validator is within the validatorSet at curHead
			if !voteManager.engine.IsActiveValidatorAt(voteManager.chain, curHead) {
				log.Debug("cur validator is not within the validatorSet at curHead")
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
				log.Debug("curHead is underRules for voting")
				if sourceHash == (common.Hash{}) {
					log.Debug("sourceHash is empty")
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
			log.Debug("voteManager subscribed chainHead failed")
			return
		}
	}
}

// UnderRules checks if the produced header under the following rules:
// A validator must not publish two distinct votes for the same height. (Rule 1)
// A validator must not vote within the span of its other votes . (Rule 2)
// Validators always vote for their canonical chain’s latest block. (Rule 3)
func (voteManager *VoteManager) UnderRules(header *types.Header) (bool, uint64, common.Hash) {
	sourceNumber, sourceHash, err := voteManager.engine.GetJustifiedNumberAndHash(voteManager.chain, header)
	if err != nil {
		log.Error("failed to get the highest justified number and hash at cur header", "curHeader's BlockNumber", header.Number, "curHeader's BlockHash", header.Hash())
		return false, 0, common.Hash{}
	}

	targetNumber := header.Number.Uint64()

	voteDataBuffer := voteManager.journal.voteDataBuffer
	//Rule 1:  A validator must not publish two distinct votes for the same height.
	if voteDataBuffer.Contains(targetNumber) {
		log.Debug("err: A validator must not publish two distinct votes for the same height.")
		return false, 0, common.Hash{}
	}

	//Rule 2: A validator must not vote within the span of its other votes.
	blockNumber := sourceNumber + 1
	if blockNumber+maliciousVoteSlashScope < targetNumber {
		blockNumber = targetNumber - maliciousVoteSlashScope
	}
	for ; blockNumber < targetNumber; blockNumber++ {
		if voteDataBuffer.Contains(blockNumber) {
			voteData, ok := voteDataBuffer.Get(blockNumber)
			if !ok {
				log.Error("Failed to get voteData info from LRU cache.")
				continue
			}
			if voteData.(*types.VoteData).SourceNumber > sourceNumber {
				log.Debug(fmt.Sprintf("error: cur vote %d-->%d is within the span of other votes %d-->%d",
					sourceNumber, targetNumber, voteData.(*types.VoteData).SourceNumber, voteData.(*types.VoteData).TargetNumber))
				return false, 0, common.Hash{}
			}
		}
	}
	for blockNumber := targetNumber + 1; blockNumber <= targetNumber+upperLimitOfVoteBlockNumber; blockNumber++ {
		if voteDataBuffer.Contains(blockNumber) {
			voteData, ok := voteDataBuffer.Get(blockNumber)
			if !ok {
				log.Error("Failed to get voteData info from LRU cache.")
				continue
			}
			if voteData.(*types.VoteData).SourceNumber < sourceNumber {
				log.Debug("error: other votes are within span of cur vote")
				return false, 0, common.Hash{}
			}
		}
	}

	// Rule 3: Validators always vote for their canonical chain’s latest block.
	// Since the header subscribed to is the canonical chain, so this rule is satisified by default.
	log.Debug("All three rules check passed")
	return true, sourceNumber, sourceHash
}

// Metrics to monitor if voteManager worked in the expetected logic.
func votesManagerMetric(blockNumber uint64, blockHash common.Hash) metrics.Gauge {
	return metrics.GetOrRegisterGauge(fmt.Sprintf("voteManager/blockNumber/%d/blockHash/%s", blockNumber, blockHash), nil)
}
