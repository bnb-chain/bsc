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
	"context"
	"encoding/json"

	"github.com/tidwall/wal"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

type VoteManager struct {
	mux    *event.TypeMux
	client *ethclient.Client // Client connection to the Ethereum chain

}

//VoteMessage to broadcast
type VoteMsg struct {
	validatorIndex uint64
	//TODO: blsSignature,
	blockHeight uint64
	blockHash   common.Hash
}

func NewVoteManager(mux *event.TypeMux) *VoteManager {
	voteManager := &VoteManager{
		mux: mux,
	}
	isReady := make(chan bool, 1)
	go voteManager.update(isReady)
	go voteManager.produceVote(isReady)

	return voteManager
}
func (voteMgr *VoteManager) produceVote(isReady chan bool) {
	if <-isReady {
		go voteMgr.loop()
	}
}

func (voteMgr *VoteManager) loop() {
	heads := make(chan *types.Header, 11)
	sub, err := voteMgr.client.SubscribeNewHead(context.Background(), heads)
	if err != nil {
		log.Crit("Failed to subscribe to head events", "err", err)
	}
	defer sub.Unsubscribe()

	for {
		head, ok := <-heads
		if !ok {
			break
		}
		// Vote for head.
		voteMessage := &VoteMsg{
			blockHeight: head.Number.Uint64(),
			blockHash:   head.Hash(),
		}
		// Put Vote into journal and VotesPool.
		WriteVotesJournal(*voteMessage)
	}

}

func (voteMgr *VoteManager) update(isReady chan bool) {
	events := voteMgr.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()
	dlEventCh := events.Chan()

	for {
		ev := <-dlEventCh
		if ev == nil {
			// Unsubscription done, stop listening
			dlEventCh = nil
			break
		}
		switch ev.Data.(type) {
		case downloader.DoneEvent:
			isReady <- true
			events.Unsubscribe()
		}
	}

}

func WriteVotesJournal(voteMessage VoteMsg) error {
	//TODO: file path
	log, err := wal.Open("voteslog", nil)
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
func LoadVotesJournal() ([]VoteMsg, error) {
	log, err := wal.Open("voteslog", nil)
	voteRes := make([]VoteMsg, 0, 256)
	if err != nil {
		return nil, err
	}
	defer log.Close()

	lastIndex, err := log.LastIndex()
	if err != nil {
		return nil, err
	}
	if lastIndex <= 256 {
		for index := 1; index <= int(lastIndex); index++ {
			voteMessage, err := log.Read(uint64(index))
			if err != nil {
				return nil, err
			}
			vote := VoteMsg{}
			if err := json.Unmarshal(voteMessage, &vote); err != nil {
				return nil, err
			}
			voteRes = append(voteRes, vote)
		}
	} else {
		for index := lastIndex - 255; index <= lastIndex; index++ {
			voteMessage, err := log.Read(uint64(index))
			if err != nil {
				return nil, err
			}
			vote := VoteMsg{}
			if err := json.Unmarshal(voteMessage, &vote); err != nil {
				return nil, err
			}
			voteRes = append(voteRes, vote)
		}
	}
	return voteRes, nil
}

// Check if the produced header under the Rule1: Validators always vote once and only once on one height,
// Rule2: Validators always vote for the child of its previous vote within a predefined n blocks to avoid vote on two different
// forks of chain.
func isUnderRules(header *types.Header) (bool, error) {
	//TODO: file path
	log, err := wal.Open("voteslog", nil)
	if err != nil {
		return false, err
	}
	defer log.Close()

	lastIndex, err := log.LastIndex()
	if err != nil {
		return false, err
	}

	// Check for Rule
	for index := lastIndex; index >= 1; index-- {
		voteMessage, err := log.Read(uint64(index))
		if err != nil {
			return false, err
		}
		vote := VoteMsg{}
		if err := json.Unmarshal(voteMessage, &vote); err != nil {
			return false, err
		}
		if vote.blockHeight == header.Number.Uint64() {
			return false, nil
		} else if vote.blockHeight < header.Number.Uint64() {
			break
		}
	}

	// Check for Rule2
	voteMessage, err := log.Read(uint64(lastIndex))
	if err != nil {
		return false, err
	}
	vote := VoteMsg{}
	if err := json.Unmarshal(voteMessage, &vote); err != nil {
		return false, err
	}

	if vote.blockHash != header.ParentHash {
		return false, nil
	}
	return true, nil
}
