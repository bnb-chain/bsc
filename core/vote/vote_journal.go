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
	"encoding/json"
	"math"
	"sync"

	"github.com/tidwall/wal"

	"github.com/ethereum/go-ethereum/core/types"
)

const (
	BufferSizeForJournal = 256
)

type VoteJournal struct {
	mu sync.RWMutex

	journalBuffer []*types.VoteEnvelope // buffered journal in memory.

	config *VoteJournalConfig

	log *wal.Log
}

type VoteJournalConfig struct {
	filePathForJournal string // Disk journal for saving the vote

}

func NewVoteJournal(config *VoteJournalConfig) (*VoteJournal, error) {
	voteJournal := &VoteJournal{
		journalBuffer: make([]*types.VoteEnvelope, 0, BufferSizeForJournal),
		config:        config,
	}
	log, err := wal.Open(voteJournal.config.filePathForJournal, nil)
	if err != nil {
		return nil, err
	}
	voteJournal.log = log
	return voteJournal, nil
}

func (vj *VoteJournal) WriteVotesJournal(voteMessage *types.VoteEnvelope) error {
	vj.mu.Lock()
	defer vj.mu.Unlock()
	if len(vj.journalBuffer) == BufferSizeForJournal {
		vj.journalBuffer = vj.journalBuffer[1:]
	}
	vj.journalBuffer = append(vj.journalBuffer, voteMessage)

	log := vj.log

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

	firstIndex, err := log.FirstIndex()
	if err != nil {
		return err
	}
	if lastIndex-firstIndex+1 > BufferSizeForJournal {
		if err := log.TruncateFront(lastIndex - BufferSizeForJournal); err != nil {
			return err
		}
	}

	return nil
}

// LoadVotesJournal in case of node restart.
func (vj *VoteJournal) LoadVotesJournal() error {
	vj.mu.RLock()
	defer vj.mu.RUnlock()

	log := vj.log

	voteRes := make([]*types.VoteEnvelope, 0, BufferSizeForJournal)
	lastIndex, err := log.LastIndex()
	if err != nil {
		return err
	}

	for index := math.Max(1, float64(lastIndex-BufferSizeForJournal+1)); index <= float64(lastIndex); index++ {
		voteMessage, err := log.Read(uint64(index))
		if err != nil {
			return err
		}
		vote := types.VoteEnvelope{}
		if err := json.Unmarshal(voteMessage, &vote); err != nil {
			return err
		}
		voteRes = append(voteRes, &vote)
	}

	vj.journalBuffer = voteRes
	return nil
}
