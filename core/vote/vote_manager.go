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
	"context"
	"io/ioutil"

	"github.com/prysmaticlabs/prysm/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
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
}

func NewVoteManager(mux *event.TypeMux, chainconfig *params.ChainConfig, chain *core.BlockChain, pool *VotePool, journalPath, bLSPassWordPath, bLSWalletPath string) (*VoteManager, error) {
	voteManager := &VoteManager{
		mux: mux,

		chain:       chain,
		chainconfig: chainconfig,
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),

		pool: pool,
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

	w, err := wallet.OpenWallet(context.Background(), &wallet.Config{
		WalletDir:      bLSWalletPath,
		WalletPassword: string(walletPassword),
	})
	if err != nil {
		log.Error("Open BLS wallet failed: %v.", err)
		return nil, err
	}

	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		log.Error("Initialize key manager failed: %v.", err)
		return nil, err
	}

	voteJournal, err := NewVoteJournal(journalPath)
	if err != nil {
		return nil, err
	}
	voteManager.journal = voteJournal

	voteSigner, err := NewVoteSigner(&km)
	if err != nil {
		return nil, err
	}
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
					if err := voteManager.journal.WriteVote(voteMessage); err != nil {
						log.Warn("Failed to write vote into journal", "err", err)
						continue
					}
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
	if header.Number.Uint64() > latestBlockNumber+maxForkLength {
		return true
	}

	curBlockHeader := header
	if curBlockHeader.Number.Uint64() <= latestBlockNumber {
		return false
	}

	for curBlockHeader != nil && curBlockHeader.Number.Uint64() >= latestBlockNumber {
		if curBlockHeader.Number.Uint64() == latestBlockNumber {
			return curBlockHeader.Hash() == latestBlockHash
		}
		curBlockHeader = voteManager.chain.GetHeader(curBlockHeader.ParentHash, curBlockHeader.Number.Uint64()-1)
	}

	return false
}
