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

	"github.com/tidwall/wal"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	maxSizeOfRecentEntry = 256
)

type VoteJournal struct {
	journalPath string // Disk journal for saving the vote.

	walLog *wal.Log

	latestVote *types.VoteEnvelope // Maintain a variable to record the most recent vote of the local node.
}

func NewVoteJournal(filePath string) (*VoteJournal, error) {

	walLog, err := wal.Open(filePath, &wal.Options{
		LogFormat:        wal.JSON,
		SegmentCacheSize: maxSizeOfRecentEntry,
	})
	if err != nil {
		return nil, err
	}

	voteJournal := &VoteJournal{
		journalPath: filePath,
		walLog:      walLog,
	}

	if err := voteJournal.LoadVotes(); err != nil {
		log.Warn("Failed to load votes journal", "err", err)
		return nil, err
	}

	return voteJournal, nil
}

func (journal *VoteJournal) WriteVote(voteMessage *types.VoteEnvelope) error {
	walLog := journal.walLog

	vote, err := json.Marshal(voteMessage)
	if err != nil {
		return err
	}

	lastIndex, err := walLog.LastIndex()
	if err != nil {
		return err
	}

	lastIndex += 1
	if err = walLog.Write(lastIndex, vote); err != nil {
		return err
	}

	firstIndex, err := walLog.FirstIndex()
	if err != nil {
		log.Warn("Failed to get first index of votes journal", "err", err)
	}

	if lastIndex-firstIndex+1 > maxSizeOfRecentEntry {
		if err := walLog.TruncateFront(lastIndex - maxSizeOfRecentEntry); err != nil {
			log.Warn("Failed to truncate votes journal", "err", err)
		}
	}
	journal.latestVote = voteMessage

	return nil
}

// LoadVotesJournal in case of node restart.
func (journal *VoteJournal) LoadVotes() error {
	walLog := journal.walLog

	lastIndex, err := walLog.LastIndex()
	if err != nil {
		return err
	}

	var startIndex uint64 = 1
	if index := lastIndex - maxSizeOfRecentEntry + 1; index > 1 {
		startIndex = index
	}

	for index := startIndex; index <= lastIndex; index++ {
		voteMessage, err := walLog.Read(index)
		if err != nil {
			log.Warn("Failed to get the entry of votes journal", "err", err)
		}

		vote := types.VoteEnvelope{}
		if err := json.Unmarshal(voteMessage, &vote); err != nil {
			return err
		}

		if index == lastIndex {
			journal.latestVote = &vote
		}
	}

	return nil
}
