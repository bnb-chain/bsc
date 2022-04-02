package vote

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/prysmaticlabs/prysm/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"

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

const (
	maxForkLength = 11
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

	engine consensus.Engine
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain *core.BlockChain, pool *VotePool, journalPath, bLSPassWordPath, bLSWalletPath string, engine consensus.Engine) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		pool:   pool,
		engine: engine,
	}

	dirExists, err := wallet.Exists(bLSWalletPath)
	if err != nil {
		log.Error("Check BLS wallet exists error: %v.", err)
	}
	if !dirExists {
		log.Error("BLS wallet did not exists.")
	}

	walletPassword, err := ioutil.ReadFile(bLSPassWordPath)
	if err != nil {
		log.Error("Read BLS wallet password error: %v.", err)
		return nil, err
	}
	log.Info("Read BLS wallet password successfully")

	w, err := wallet.OpenWallet(context.Background(), &wallet.Config{
		WalletDir:      bLSWalletPath,
		WalletPassword: string(walletPassword),
	})
	if err != nil {
		log.Error("Open BLS wallet failed: %v.", err)
		return nil, err
	}
	log.Info("Open BLS wallet successfully")

	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		log.Error("Initialize key manager failed: %v.", err)
		return nil, err
	}
	log.Info("Initialized keymanager successfully")

	voteJournal, err := NewVoteJournal(journalPath)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteJournal successfully")
	voteManager.journal = voteJournal

	voteSigner, err := NewVoteSigner(&km)
	if err != nil {
		return nil, err
	}
	log.Info("Create voteSigner successfully")
	voteManager.signer = voteSigner

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

			var lastLatestVoteNumber uint64
			lastLatestVote := voteManager.journal.latestVote
			if lastLatestVote == nil {
				lastLatestVoteNumber = 0
			} else {
				lastLatestVoteNumber = lastLatestVote.Data.TargetNumber
			}

			var newChainStack []*types.Header
			for i := 0; i < maxForkLength; i++ {
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
					TargetNumber: curBlockHeader.Number.Uint64(),
					TargetHash:   curBlockHeader.Hash(),
				}
				voteMessage := &types.VoteEnvelope{
					Data: vote,
				}
				// Put Vote into journal and VotesPool if we are active validator and allow to sign it.
				if ok, sourceNumber, sourceHash := voteManager.UnderRules(curBlockHeader); ok {
					if sourceHash != (common.Hash{}) {
						voteMessage.Data.SourceNumber = sourceNumber
						voteMessage.Data.SourceHash = sourceHash
					}

					if err := voteManager.signer.SignVote(voteMessage); err != nil {
						log.Debug("Failed to sign vote", "err", err)
						votesSigningErrorMetric(vote.TargetNumber, vote.TargetHash).Inc(1)
						continue
					}
					if err := voteManager.journal.WriteVote(voteMessage); err != nil {
						log.Warn("Failed to write vote into journal", "err", err)
						votesJournalErrorMetric(vote.TargetNumber, vote.TargetHash).Inc(1)
						continue
					}

					log.Info("vote manager produced vote", "voteHash=", voteMessage.Hash())
					voteManager.pool.PutVote(voteMessage)
					votesManagerMetric(vote.TargetNumber, vote.TargetHash).Inc(1)
				}
			}
		}
	}
}

// UnderRules checks if the produced header under the following rules:
// A validator must not publish two distinct votes for the same height. (Rule 1)
// A validator must not vote within the span of its other votes . (Rule 2)
// Validators always vote for their canonical chain’s latest block. (Rule 3)
func (voteManager *VoteManager) UnderRules(header *types.Header) (bool, uint64, common.Hash) {
	posa, ok := voteManager.engine.(consensus.PoSA)
	if !ok {
		return true, 0, common.Hash{}
	}

	curHighestJustifiedHeader := posa.GetHighestJustifiedHeader(voteManager.chain, header)
	if curHighestJustifiedHeader == nil {
		//return true, 0, common.Hash{}
		//For Integration Test only!:
		return true, header.Number.Uint64() - 1, header.ParentHash
	}

	sourceBlockNumber := curHighestJustifiedHeader.Number.Uint64()
	sourceBlockHash := curHighestJustifiedHeader.Hash()
	targetBlockNumber := header.Number.Uint64()

	journal := voteManager.journal
	journalLatestVote := journal.latestVote
	if journalLatestVote == nil {
		return true, sourceBlockNumber, sourceBlockHash
	}

	walLog := journal.walLog
	firstIndex, err := walLog.FirstIndex()
	if err != nil {
		log.Error("Failed to get firstIndex of vote journal", "err", err)
		return false, 0, common.Hash{}
	}

	journalFirstVote, err := journal.ReadVote(firstIndex)
	if err != nil {
		return false, 0, common.Hash{}
	}

	lastIndex, err := walLog.LastIndex()
	if err != nil {
		log.Error("Failed to get lastIndex of vote journal", "err", err)
		return false, 0, common.Hash{}
	}

	if sourceBlockNumber >= journalLatestVote.Data.TargetNumber || targetBlockNumber <= journalFirstVote.Data.SourceNumber {
		return true, sourceBlockNumber, sourceBlockHash
	}

	for index := lastIndex; index >= firstIndex; index-- {
		vote, err := journal.ReadVote(index)
		if err != nil {
			return false, 0, common.Hash{}
		}
		if vote == nil {
			log.Error("vote is nil")
			return false, 0, common.Hash{}
		}

		if sourceBlockNumber >= vote.Data.TargetNumber {
			// No need to iterate later, since the blockNumber in journal is in ascending order,
			// so there'll never be any overlap for all the later iterations
			break
		}

		if vote.Data.SourceNumber > sourceBlockNumber && vote.Data.TargetNumber < targetBlockNumber {
			log.Warn("curHeader's vote source and target are within its other votes")
			return false, 0, common.Hash{}
		}
		if vote.Data.SourceNumber < sourceBlockNumber && vote.Data.TargetNumber > targetBlockNumber {
			log.Warn("Other votes source and target are within curHeader's")
			return false, 0, common.Hash{}
		}
	}

	return true, sourceBlockNumber, sourceBlockHash
}

// Metrics to monitor if voteManager worked in the expetected logic.
func votesManagerMetric(blockNumber uint64, blockHash common.Hash) metrics.Gauge {
	return metrics.GetOrRegisterGauge(fmt.Sprintf("voteManager/blockNumber/%d/blockHash/%s", blockNumber, blockHash), nil)
}
