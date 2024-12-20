package vote

import (
	"bytes"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

const blocksNumberSinceMining = 5 // the number of blocks need to wait before voting, counting from the validator begin to mine

var diffInTurn = big.NewInt(2) // Block difficulty for in-turn signatures
var votesManagerCounter = metrics.NewRegisteredCounter("votesManager/local", nil)
var notJustified = metrics.NewRegisteredCounter("votesManager/notJustified", nil)
var inTurnJustified = metrics.NewRegisteredCounter("votesManager/inTurnJustified", nil)
var notInTurnJustified = metrics.NewRegisteredCounter("votesManager/notInTurnJustified", nil)
var continuousJustified = metrics.NewRegisteredCounter("votesManager/continuousJustified", nil)
var notContinuousJustified = metrics.NewRegisteredCounter("votesManager/notContinuousJustified", nil)

// Backend wraps all methods required for voting.
type Backend interface {
	IsMining() bool
	EventMux() *event.TypeMux
}

// VoteManager will handle the vote produced by self.
type VoteManager struct {
	eth Backend

	chain *core.BlockChain

	highestVerifiedBlockCh  chan core.HighestVerifiedBlockEvent
	highestVerifiedBlockSub event.Subscription

	// used for backup validators to sync votes from corresponding mining validator
	syncVoteCh  chan core.NewVoteEvent
	syncVoteSub event.Subscription

	pool    *VotePool
	signer  *VoteSigner
	journal *VoteJournal

	engine consensus.PoSA
}

func NewVoteManager(eth Backend, chain *core.BlockChain, pool *VotePool, journalPath, blsPasswordPath, blsWalletPath string, engine consensus.PoSA) (*VoteManager, error) {
	voteManager := &VoteManager{
		eth:                    eth,
		chain:                  chain,
		highestVerifiedBlockCh: make(chan core.HighestVerifiedBlockEvent, highestVerifiedBlockChanSize),
		syncVoteCh:             make(chan core.NewVoteEvent, voteBufferForPut),
		pool:                   pool,
		engine:                 engine,
	}

	// Create voteSigner.
	voteSigner, err := NewVoteSigner(blsPasswordPath, blsWalletPath)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteSigner successfully")
	voteManager.signer = voteSigner
	metrics.GetOrRegisterLabel("miner-info", nil).Mark(map[string]interface{}{"VoteKey": common.Bytes2Hex(voteManager.signer.PubKey[:])})

	// Create voteJournal
	voteJournal, err := NewVoteJournal(journalPath)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteJournal successfully")
	voteManager.journal = voteJournal

	// Subscribe to chain head event.
	voteManager.highestVerifiedBlockSub = voteManager.chain.SubscribeHighestVerifiedHeaderEvent(voteManager.highestVerifiedBlockCh)
	voteManager.syncVoteSub = voteManager.pool.SubscribeNewVoteEvent(voteManager.syncVoteCh)

	go voteManager.loop()

	return voteManager, nil
}

func (voteManager *VoteManager) loop() {
	log.Debug("vote manager routine loop started")
	defer voteManager.highestVerifiedBlockSub.Unsubscribe()
	defer voteManager.syncVoteSub.Unsubscribe()

	events := voteManager.eth.EventMux().Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		log.Debug("vote manager loop defer func occur")
		if !events.Closed() {
			log.Debug("event not closed, unsubscribed by vote manager loop")
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
		case cHead := <-voteManager.highestVerifiedBlockCh:
			if !startVote {
				log.Debug("startVote flag is false, continue")
				continue
			}
			if !voteManager.eth.IsMining() {
				blockCountSinceMining = 0
				log.Debug("skip voting because mining is disabled, continue")
				continue
			}
			blockCountSinceMining++
			if blockCountSinceMining <= blocksNumberSinceMining {
				log.Debug("skip voting", "blockCountSinceMining", blockCountSinceMining, "blocksNumberSinceMining", blocksNumberSinceMining)
				continue
			}

			if cHead.Header == nil {
				log.Debug("cHead.Header is nil, continue")
				continue
			}

			curHead := cHead.Header
			if p, ok := voteManager.engine.(*parlia.Parlia); ok {
				nextBlockMinedTime := time.Unix(int64((curHead.Time + p.Period())), 0)
				timeForBroadcast := 50 * time.Millisecond // enough to broadcast a vote
				if time.Now().Add(timeForBroadcast).After(nextBlockMinedTime) {
					log.Warn("too late to vote", "Head.Time(Second)", curHead.Time, "Now(Millisecond)", time.Now().UnixMilli())
					continue
				}
			}

			// Check if cur validator is within the validatorSet at curHead
			if !voteManager.engine.IsActiveValidatorAt(voteManager.chain, curHead,
				func(bLSPublicKey *types.BLSPublicKey) bool {
					return bytes.Equal(voteManager.signer.PubKey[:], bLSPublicKey[:])
				}) {
				log.Debug("local validator with voteKey is not within the validatorSet at curHead")
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
					log.Error("Failed to sign vote", "err", err, "votedBlockNumber", voteMessage.Data.TargetNumber, "votedBlockHash", voteMessage.Data.TargetHash, "voteMessageHash", voteMessage.Hash())
					votesSigningErrorCounter.Inc(1)
					continue
				}
				if err := voteManager.journal.WriteVote(voteMessage); err != nil {
					log.Error("Failed to write vote into journal", "err", err)
					voteJournalErrorCounter.Inc(1)
					continue
				}

				log.Debug("vote manager produced vote", "votedBlockNumber", voteMessage.Data.TargetNumber, "votedBlockHash", voteMessage.Data.TargetHash, "voteMessageHash", voteMessage.Hash())
				voteManager.pool.PutVote(voteMessage)
				votesManagerCounter.Inc(1)
			}

			// check the latest justified block, which indicating the stability of the network
			curJustifiedNumber, _, err := voteManager.engine.GetJustifiedNumberAndHash(voteManager.chain, []*types.Header{curHead})
			if err == nil && curJustifiedNumber != 0 {
				if curJustifiedNumber+1 != curHead.Number.Uint64() {
					log.Debug("not justified", "blockNumber", curHead.Number.Uint64()-1)
					notJustified.Inc(1)
				} else {
					parent := voteManager.chain.GetHeaderByHash(curHead.ParentHash)
					if parent != nil {
						if parent.Difficulty.Cmp(diffInTurn) == 0 {
							inTurnJustified.Inc(1)
						} else {
							log.Debug("not in turn block justified", "blockNumber", parent.Number.Int64(), "blockHash", parent.Hash())
							notInTurnJustified.Inc(1)
						}

						lastJustifiedNumber, _, err := voteManager.engine.GetJustifiedNumberAndHash(voteManager.chain, []*types.Header{parent})
						if err == nil {
							if lastJustifiedNumber == 0 || lastJustifiedNumber+1 == curJustifiedNumber {
								continuousJustified.Inc(1)
							} else {
								log.Debug("not continuous block justified", "lastJustified", lastJustifiedNumber, "curJustified", curJustifiedNumber)
								notContinuousJustified.Inc(1)
							}
						}
					}
				}
			}

		case event := <-voteManager.syncVoteCh:
			voteMessage := event.Vote
			if voteManager.eth.IsMining() || !bytes.Equal(voteManager.signer.PubKey[:], voteMessage.VoteAddress[:]) {
				continue
			}
			if err := voteManager.journal.WriteVote(voteMessage); err != nil {
				log.Error("Failed to write vote into journal", "err", err)
				voteJournalErrorCounter.Inc(1)
				continue
			}
			log.Debug("vote manager synced vote", "votedBlockNumber", voteMessage.Data.TargetNumber, "votedBlockHash", voteMessage.Data.TargetHash, "voteMessageHash", voteMessage.Hash())
			votesManagerCounter.Inc(1)
		case <-voteManager.syncVoteSub.Err():
			log.Debug("voteManager subscribed votes failed")
			return
		case <-voteManager.highestVerifiedBlockSub.Err():
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
	sourceNumber, sourceHash, err := voteManager.engine.GetJustifiedNumberAndHash(voteManager.chain, []*types.Header{header})
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
				log.Debug(fmt.Sprintf("error: cur vote %d-->%d is across the span of other votes %d-->%d",
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
				log.Debug(fmt.Sprintf("error: cur vote %d-->%d is within the span of other votes %d-->%d",
					sourceNumber, targetNumber, voteData.(*types.VoteData).SourceNumber, voteData.(*types.VoteData).TargetNumber))
				return false, 0, common.Hash{}
			}
		}
	}

	// Rule 3: Validators always vote for their canonical chain’s latest block.
	// Since the header subscribed to is the canonical chain, so this rule is satisfied by default.
	log.Debug("All three rules check passed")
	return true, sourceNumber, sourceHash
}
