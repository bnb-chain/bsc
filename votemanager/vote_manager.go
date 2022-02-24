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
package votemanager

import (
	"encoding/json"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/tidwall/wal"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	BufferSizeForJournal = 256
)

type VoteManager struct {
	mu            sync.RWMutex
	mux           *event.TypeMux
	chain         blockChain
	chainconfig   *params.ChainConfig
	chainHeadCh   chan core.ChainHeadEvent
	chainHeadSub  event.Subscription
	vp            *VotesPool
	journalBuffer []*types.VoteRecord
	voteSet       mapset.Set
	isReady       chan bool
	wg            sync.WaitGroup // tracks loop
	voteConfig    *VoteManagerConfig
	bls           BLS
}

type VoteManagerConfig struct {
	VoteJournal string        // Disk journal for saving the vote
	Rejournal   time.Duration // Time interval to regenerate the local votes journal
}

var DefaultVoteConfig = VoteManagerConfig{
	Rejournal: 1 * time.Hour,
}

// sanitize checks the provided user configurations and changes anything that's
// unreasonable or unworkable.
func (config *VoteManagerConfig) sanitize() VoteManagerConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		log.Warn("Sanitizing invalid votePool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	return conf

}

func NewVoteManager(config *VoteManagerConfig, mux *event.TypeMux, chainconfig *params.ChainConfig, chain blockChain) *VoteManager {
	voteManager := &VoteManager{
		mux:           mux,
		chain:         chain,
		chainconfig:   chainconfig,
		chainHeadCh:   make(chan core.ChainHeadEvent, chainHeadChanSize),
		journalBuffer: make([]*types.VoteRecord, 0, BufferSizeForJournal),
		voteSet:       mapset.NewSet(),
		isReady:       make(chan bool, 1),
		voteConfig:    config,
	}

	if config.VoteJournal != "" {
		if err := voteManager.LoadVotesJournal(); err != nil {
			log.Warn("Failed to load votes journal", "err", err)
		}
	}

	voteManager.chainHeadSub = voteManager.chain.SubscribeChainHeadEvent(voteManager.chainHeadCh)

	voteManager.wg.Add(1)
	go voteManager.loop()

	voteManager.wg.Add(1)
	go voteManager.produceVote()

	return voteManager
}

func (vm *VoteManager) loop() {
	defer vm.wg.Done()

	events := vm.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	dlEventCh := events.Chan()

	// cleanTicker is the ticker used to trigger the prune for journal in disk.
	var cleanTicker = time.NewTicker(vm.voteConfig.Rejournal)
	pruning := func() {
		walLog, err := wal.Open(vm.voteConfig.VoteJournal, nil)
		if err != nil {
			log.Debug("Failed to prune journal in disk", "err", err)
			return
		}
		defer walLog.Close()
		lastIndex, _ := walLog.LastIndex()
		walLog.TruncateFront(lastIndex - BufferSizeForJournal)
	}

	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				// Unsubscription done, stop listening
				dlEventCh = nil
				break
			}
			switch ev.Data.(type) {
			case downloader.DoneEvent:
				vm.isReady <- true
				events.Unsubscribe()
			}
		case <-cleanTicker.C:
			pruning()
		}
	}

}

func (vm *VoteManager) produceVote() {
	defer vm.wg.Done()

	if <-vm.isReady {
		for {
			// Handle ChainHeadEvent
			ev := <-vm.chainHeadCh
			if ev.Block != nil {
				curBlock := ev.Block

				// Vote for curBlock.
				vote := &types.VoteData{
					BlockNumber: curBlock.NumberU64(),
					BlockHash:   curBlock.Hash(),
				}
				voteMessage := &types.VoteRecord{
					Data: *vote,
				}
				// Put Vote into journal and VotesPool if verified by BLS.
				if ok, err := vm.IsUnderRules(curBlock.Header()); err == nil {
					if ok && vm.bls.Sign(voteMessage) && vm.bls.Verify(voteMessage) {
						vm.WriteVotesJournal(voteMessage)
						vm.vp.PutVote(voteMessage)
					}
				} else {
					log.Error("is under rules error", "err", err)
				}

			}
		}
	}
}

func (vm *VoteManager) WriteVotesJournal(voteMessage *types.VoteRecord) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if len(vm.journalBuffer) == BufferSizeForJournal {
		vm.journalBuffer = vm.journalBuffer[1:]
	}
	vm.journalBuffer = append(vm.journalBuffer, voteMessage)

	log, err := wal.Open(vm.voteConfig.VoteJournal, nil)
	if err != nil {
		return err
	}
	defer log.Close()
	// Write vote message
	vote, err := json.Marshal(voteMessage)
	if err != nil {
		return err
	}
	lastIndex, err := log.LastIndex()
	if err != nil {
		return err
	}
	if err = log.Write(lastIndex+1, vote); err != nil {
		return err
	}
	return nil
}

// LoadVotesJournal in case of node restart.
func (vm *VoteManager) LoadVotesJournal() error {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	log, err := wal.Open(vm.voteConfig.VoteJournal, nil)
	voteRes := make([]*types.VoteRecord, 0, 256)
	if err != nil {
		return err
	}
	defer log.Close()

	lastIndex, err := log.LastIndex()
	if err != nil {
		return err
	}
	if lastIndex <= BufferSizeForJournal {
		for index := 1; index <= int(lastIndex); index++ {
			voteMessage, err := log.Read(uint64(index))
			if err != nil {
				return err
			}
			vote := types.VoteRecord{}
			if err := json.Unmarshal(voteMessage, &vote); err != nil {
				return err
			}
			voteRes = append(voteRes, &vote)
		}
	} else {
		for index := lastIndex - BufferSizeForJournal + 1; index <= lastIndex; index++ {
			voteMessage, err := log.Read(uint64(index))
			if err != nil {
				return err
			}
			vote := types.VoteRecord{}
			if err := json.Unmarshal(voteMessage, &vote); err != nil {
				return err
			}
			voteRes = append(voteRes, &vote)
		}
	}
	vm.journalBuffer = voteRes
	return nil
}

func (vm *VoteManager) Truncate(index uint64) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	log, err := wal.Open(vm.voteConfig.VoteJournal, nil)
	if err != nil {
		return err
	}
	defer log.Close()
	if err := log.TruncateFront(index); err != nil {
		return err
	}
	return nil
}

// Check if the produced header under the Rule1: Validators always vote once and only once on one height,
// Rule2: Validators always vote for the child of its previous vote within a predefined n blocks to avoid vote on two different
// forks of chain.
func (vm *VoteManager) IsUnderRules(header *types.Header) (bool, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	journalBuffer := vm.journalBuffer
	// Check for Rule1.
	for index := len(journalBuffer) - 1; index >= 0; index-- {
		vote := journalBuffer[index]
		if vote.Data.BlockNumber == header.Number.Uint64() {
			return false, nil
		} else if vote.Data.BlockNumber < header.Number.Uint64() {
			break
		}
	}
	isValid := false

	// Check for Rule2.
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
	return isValid, nil
}
