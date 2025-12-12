package monitor

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

// follow define in core/vote
const (
	maxSizeOfRecentEntry        = 512
	maliciousVoteSlashScope     = 256
	upperLimitOfVoteBlockNumber = 11
)

var (
	violateRule1Counter = metrics.NewRegisteredCounter("monitor/maliciousVote/violateRule1", nil)
	violateRule2Counter = metrics.NewRegisteredCounter("monitor/maliciousVote/violateRule2", nil)
)

// two purposes
// 1. monitor whether there are bugs in the voting mechanism, so add metrics to observe it.
// 2. do malicious vote slashing. TODO
type MaliciousVoteMonitor struct {
	curVotes map[types.BLSPublicKey]*lru.Cache[uint64, *types.VoteEnvelope]
}

func NewMaliciousVoteMonitor() *MaliciousVoteMonitor {
	return &MaliciousVoteMonitor{
		curVotes: make(map[types.BLSPublicKey]*lru.Cache[uint64, *types.VoteEnvelope], 21), // mainnet config
	}
}

func (m *MaliciousVoteMonitor) ConflictDetect(newVote *types.VoteEnvelope, pendingBlockNumber uint64) bool {
	// get votes for specified VoteAddress
	if _, ok := m.curVotes[newVote.VoteAddress]; !ok {
		m.curVotes[newVote.VoteAddress] = lru.NewCache[uint64, *types.VoteEnvelope](maxSizeOfRecentEntry)
	}
	voteDataBuffer := m.curVotes[newVote.VoteAddress]
	sourceNumber, targetNumber := newVote.Data.SourceNumber, newVote.Data.TargetNumber

	//Basic check
	// refer to https://github.com/bnb-chain/bsc-genesis-contract/blob/master/contracts/SlashIndicator.sol#LL207C4-L207C4
	if !(targetNumber+maliciousVoteSlashScope > pendingBlockNumber) {
		return false
	}

	// UnderRules check
	blockNumber := sourceNumber + 1
	if !(blockNumber+maliciousVoteSlashScope > pendingBlockNumber) {
		blockNumber = pendingBlockNumber - maliciousVoteSlashScope + 1
	}
	newVoteHash := newVote.Data.Hash()
	for ; blockNumber <= pendingBlockNumber+upperLimitOfVoteBlockNumber; blockNumber++ {
		if voteDataBuffer.Contains(blockNumber) {
			voteEnvelope, ok := voteDataBuffer.Get(blockNumber)
			if !ok {
				log.Error("Failed to get voteData info from LRU cache.")
				continue
			}
			maliciousVote := false
			if blockNumber == targetNumber && voteEnvelope.Data.Hash() != newVoteHash {
				violateRule1Counter.Inc(1)
				maliciousVote = true
			} else if (blockNumber < targetNumber && voteEnvelope.Data.SourceNumber > sourceNumber) ||
				(blockNumber > targetNumber && voteEnvelope.Data.SourceNumber < sourceNumber) {
				violateRule2Counter.Inc(1)
				maliciousVote = true
			}
			if maliciousVote {
				evidence := types.NewSlashIndicatorFinalityEvidenceWrapper(voteEnvelope, newVote)
				if evidence != nil {
					if evidenceJson, err := json.Marshal(evidence); err == nil {
						log.Warn("MaliciousVote", "evidence", string(evidenceJson))
					} else {
						log.Warn("MaliciousVote, Marshal evidence failed")
					}
				} else {
					log.Warn("MaliciousVote, construct evidence failed")
				}
				return true
			}
		}
	}

	// for simplicity, Just override even if the targetNumber has existed.
	voteDataBuffer.Add(newVote.Data.TargetNumber, newVote)
	return false
}
