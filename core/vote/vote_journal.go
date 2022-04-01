package vote

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/wal"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

const (
	maxSizeOfRecentEntry = 512
)

type VoteJournal struct {
	journalPath string // file path of disk journal for saving the vote.

	walLog *wal.Log

	latestVote *types.VoteEnvelope // Maintain a variable to record the most recent vote of the local node.
}

func NewVoteJournal(filePath string) (*VoteJournal, error) {
	walLog, err := wal.Open(filePath, &wal.Options{
		LogFormat:        wal.JSON,
		SegmentCacheSize: maxSizeOfRecentEntry,
	})
	if err != nil {
		log.Error("Failed to open vote journal", "err", err)
		return nil, err
	}

	voteJournal := &VoteJournal{
		journalPath: filePath,
		walLog:      walLog,
	}

	lastIndex, err := walLog.LastIndex()
	if err != nil {
		log.Error("Failed to get lastIndex of vote journal", "err", err)
		return nil, err
	}
	vote, err := voteJournal.ReadVote(lastIndex)
	if err != nil {
		return nil, err
	}
	voteJournal.latestVote = vote

	return voteJournal, nil
}

func (journal *VoteJournal) WriteVote(voteMessage *types.VoteEnvelope) error {
	walLog := journal.walLog

	vote, err := json.Marshal(voteMessage)
	if err != nil {
		log.Error("Failed to unmarshal vote", "err", err)
		return err
	}

	lastIndex, err := walLog.LastIndex()
	if err != nil {
		log.Error("Failed to get lastIndex of vote journal", "err", err)
		return err
	}

	lastIndex += 1
	if err = walLog.Write(lastIndex, vote); err != nil {
		log.Error("Failed to write vote journal", "err", err)
		return err
	}

	firstIndex, err := walLog.FirstIndex()
	if err != nil {
		log.Warn("Failed to get first index of votes journal", "err", err)
	}

	if lastIndex-firstIndex+1 > maxSizeOfRecentEntry {
		if err := walLog.TruncateFront(lastIndex - maxSizeOfRecentEntry + 1); err != nil {
			log.Warn("Failed to truncate votes journal", "err", err)
		}
	}
	journal.latestVote = voteMessage

	return nil
}

func (journal *VoteJournal) ReadVote(index uint64) (*types.VoteEnvelope, error) {
	voteMessage, err := journal.walLog.Read(index)
	if err != nil && err != wal.ErrNotFound {
		log.Error("Failed to read votes journal", "err", err)
		return nil, err
	}

	var vote *types.VoteEnvelope
	if voteMessage != nil {
		vote = &types.VoteEnvelope{}
		if err := json.Unmarshal(voteMessage, vote); err != nil {
			log.Error("Failed to unmarshal vote in the proecss for intializing journal object", "err", err)
			return nil, err
		}
	}

	return vote, nil
}

// Metrics to monitor if there's any error for writing vote journal.
func votesJournalErrorMetric(blockNumber uint64, blockHash common.Hash) metrics.Gauge {
	return metrics.GetOrRegisterGauge(fmt.Sprintf("voteJournal/blockNumber/%d/blockHash/%s", blockNumber, blockHash), nil)
}
