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
package votejournal

import (
	"encoding/json"

	"github.com/tidwall/wal"
)

//VoteMessage to broadcast
type voteMsg struct {
	validatorIndex uint64
	//TODO: blsSignature,
	blockHeight uint64
	blockHash   string
}

func WriteVotesJournal(voteMessage voteMsg) error {
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
func LoadVotesJournal() ([]voteMsg, error) {
	log, err := wal.Open("voteslog", nil)
	voteRes := make([]voteMsg, 0, 256)
	if err != nil {
		return nil, err
	}
	defer log.Close()

	lastIndex, err := log.LastIndex()
	if err != nil {
		return nil, err
	}
	if lastIndex <= 255 {
		for index := 0; index <= int(lastIndex); index++ {
			voteMessage, err := log.Read(uint64(index))
			if err != nil {
				return nil, err
			}
			vote := voteMsg{}
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
			vote := voteMsg{}
			if err := json.Unmarshal(voteMessage, &vote); err != nil {
				return nil, err
			}
			voteRes = append(voteRes, vote)
		}
	}
	return voteRes, nil
}
