// Copyright 2017 The go-ethereum Authors
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

package parlia

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	lru "github.com/hashicorp/golang-lru"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Snapshot is the state of the validatorSet at a given point.
type Snapshot struct {
	config   *params.ParliaConfig // Consensus engine parameters to fine tune behavior
	ethAPI   *ethapi.BlockChainAPI
	sigCache *lru.ARCCache // Cache of recent block signatures to speed up ecrecover

	Number           uint64                            `json:"number"`                // Block number where the snapshot was created
	Hash             common.Hash                       `json:"hash"`                  // Block hash where the snapshot was created
	EpochLength      uint64                            `json:"epoch_length"`          // Number of Blocks in one epoch
	BlockInterval    uint64                            `json:"block_interval"`        // Block Interval in milliseconds
	TurnLength       uint8                             `json:"turn_length"`           // Length of `turn`, meaning the consecutive number of blocks a validator receives priority for block production
	Validators       map[common.Address]*ValidatorInfo `json:"validators"`            // Set of authorized validators at this moment
	Recents          map[uint64]common.Address         `json:"recents"`               // Set of recent validators for spam protections
	RecentForkHashes map[uint64]string                 `json:"recent_fork_hashes"`    // Set of recent forkHash
	Attestation      *types.VoteData                   `json:"attestation:omitempty"` // Attestation for fast finality, but `Source` used as `Finalized`
}

type ValidatorInfo struct {
	Index       int                `json:"index:omitempty"` // The index should offset by 1
	VoteAddress types.BLSPublicKey `json:"vote_address,omitempty"`
}

// newSnapshot creates a new snapshot with the specified startup parameters. This
// method does not initialize the set of recent validators, so only ever use it for
// the genesis block.
func newSnapshot(
	config *params.ParliaConfig,
	sigCache *lru.ARCCache,
	number uint64,
	hash common.Hash,
	validators []common.Address,
	voteAddrs []types.BLSPublicKey,
	ethAPI *ethapi.BlockChainAPI,
) *Snapshot {
	snap := &Snapshot{
		config:           config,
		ethAPI:           ethAPI,
		sigCache:         sigCache,
		Number:           number,
		Hash:             hash,
		EpochLength:      defaultEpochLength,
		BlockInterval:    defaultBlockInterval,
		TurnLength:       defaultTurnLength,
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
		Validators:       make(map[common.Address]*ValidatorInfo),
	}
	for idx, v := range validators {
		// The luban fork from the genesis block
		if len(voteAddrs) == len(validators) {
			snap.Validators[v] = &ValidatorInfo{
				VoteAddress: voteAddrs[idx],
			}
		} else {
			snap.Validators[v] = &ValidatorInfo{}
		}
	}

	// The luban fork from the genesis block
	if len(voteAddrs) == len(validators) {
		validators := snap.validators()
		for idx, v := range validators {
			snap.Validators[v].Index = idx + 1 // offset by 1
		}
	}
	return snap
}

// validatorsAscending implements the sort interface to allow sorting a list of addresses
type validatorsAscending []common.Address

func (s validatorsAscending) Len() int           { return len(s) }
func (s validatorsAscending) Less(i, j int) bool { return bytes.Compare(s[i][:], s[j][:]) < 0 }
func (s validatorsAscending) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// loadSnapshot loads an existing snapshot from the database.
func loadSnapshot(config *params.ParliaConfig, sigCache *lru.ARCCache, db ethdb.Database, hash common.Hash, ethAPI *ethapi.BlockChainAPI) (*Snapshot, error) {
	blob, err := db.Get(append([]byte("parlia-"), hash[:]...))
	if err != nil {
		return nil, err
	}
	snap := new(Snapshot)
	if err := json.Unmarshal(blob, snap); err != nil {
		return nil, err
	}
	if snap.EpochLength == 0 { // no EpochLength field in old snapshots
		snap.EpochLength = defaultEpochLength
	}
	if snap.BlockInterval == 0 { // no BlockInterval field in old snapshots
		snap.BlockInterval = defaultBlockInterval
	}
	if snap.TurnLength == 0 { // no TurnLength field in old snapshots
		snap.TurnLength = defaultTurnLength
	}

	snap.config = config
	snap.sigCache = sigCache
	snap.ethAPI = ethAPI

	return snap, nil
}

// store inserts the snapshot into the database.
func (s *Snapshot) store(db ethdb.Database) error {
	blob, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return db.Put(append([]byte("parlia-"), s.Hash[:]...), blob)
}

// copy creates a deep copy of the snapshot
func (s *Snapshot) copy() *Snapshot {
	cpy := &Snapshot{
		config:           s.config,
		ethAPI:           s.ethAPI,
		sigCache:         s.sigCache,
		Number:           s.Number,
		Hash:             s.Hash,
		EpochLength:      s.EpochLength,
		BlockInterval:    s.BlockInterval,
		TurnLength:       s.TurnLength,
		Validators:       make(map[common.Address]*ValidatorInfo),
		Recents:          make(map[uint64]common.Address),
		RecentForkHashes: make(map[uint64]string),
	}

	for v := range s.Validators {
		cpy.Validators[v] = &ValidatorInfo{
			Index:       s.Validators[v].Index,
			VoteAddress: s.Validators[v].VoteAddress,
		}
	}
	for block, v := range s.Recents {
		cpy.Recents[block] = v
	}
	for block, id := range s.RecentForkHashes {
		cpy.RecentForkHashes[block] = id
	}
	if s.Attestation != nil {
		cpy.Attestation = &types.VoteData{
			SourceNumber: s.Attestation.SourceNumber,
			SourceHash:   s.Attestation.SourceHash,
			TargetNumber: s.Attestation.TargetNumber,
			TargetHash:   s.Attestation.TargetHash,
		}
	}
	return cpy
}

func (s *Snapshot) isMajorityFork(forkHash string) bool {
	ally := 0
	for _, h := range s.RecentForkHashes {
		if h == forkHash {
			ally++
		}
	}
	return ally > len(s.RecentForkHashes)/2
}

func (s *Snapshot) updateAttestation(header *types.Header, chainConfig *params.ChainConfig) {
	if !chainConfig.IsLuban(header.Number) {
		return
	}

	// The attestation should have been checked in verify header, update directly
	attestation, _ := getVoteAttestationFromHeader(header, chainConfig, s.EpochLength)
	if attestation == nil {
		return
	}

	// Headers with bad attestation are accepted before Plato upgrade,
	// but Attestation of snapshot is only updated when the target block is direct parent of the header
	targetNumber := attestation.Data.TargetNumber
	targetHash := attestation.Data.TargetHash
	if targetHash != header.ParentHash || targetNumber+1 != header.Number.Uint64() {
		log.Warn("updateAttestation failed", "error", fmt.Errorf("invalid attestation, target mismatch, expected block: %d, hash: %s; real block: %d, hash: %s",
			header.Number.Uint64()-1, header.ParentHash, targetNumber, targetHash))
		updateAttestationErrorCounter.Inc(1)
		return
	}

	// Update attestation
	// Two scenarios for s.Attestation being nil:
	// 1) The first attestation is assembled.
	// 2) The snapshot on disk is missing, prompting the creation of a new snapshot using `newSnapshot`.
	if s.Attestation != nil && attestation.Data.SourceNumber+1 != attestation.Data.TargetNumber {
		s.Attestation.TargetNumber = attestation.Data.TargetNumber
		s.Attestation.TargetHash = attestation.Data.TargetHash
	} else {
		s.Attestation = attestation.Data
	}
}

func (s *Snapshot) versionHistoryCheckLen() uint64 {
	return uint64(len(s.Validators)) * uint64(s.TurnLength)
}

func (s *Snapshot) minerHistoryCheckLen() uint64 {
	return (uint64(len(s.Validators))/2+1)*uint64(s.TurnLength) - 1
}

func (s *Snapshot) countRecents() map[common.Address]uint8 {
	leftHistoryBound := uint64(0) // the bound is excluded
	checkHistoryLength := s.minerHistoryCheckLen()
	if s.Number > checkHistoryLength {
		leftHistoryBound = s.Number - checkHistoryLength
	}
	counts := make(map[common.Address]uint8, len(s.Validators))
	for seen, recent := range s.Recents {
		if seen <= leftHistoryBound || recent == (common.Address{}) /*when seen == `epochKey`*/ {
			continue
		}
		counts[recent] += 1
	}
	return counts
}

func (s *Snapshot) signRecentlyByCounts(validator common.Address, counts map[common.Address]uint8) bool {
	if seenTimes, ok := counts[validator]; ok && seenTimes >= s.TurnLength {
		if seenTimes > s.TurnLength {
			log.Warn("produce more blocks than expected!", "validator", validator, "seenTimes", seenTimes)
		}
		return true
	}

	return false
}

func (s *Snapshot) SignRecently(validator common.Address) bool {
	return s.signRecentlyByCounts(validator, s.countRecents())
}

func (s *Snapshot) getFinalizedNumber() uint64 {
	if s.Attestation != nil {
		return s.Attestation.SourceNumber
	}
	return 0
}

func (s *Snapshot) apply(headers []*types.Header, chain consensus.ChainHeaderReader, parents []*types.Header, chainConfig *params.ChainConfig) (*Snapshot, error) {
	// Allow passing in no headers for cleaner code
	if len(headers) == 0 {
		return s, nil
	}
	// Sanity check that the headers can be applied
	for i := 0; i < len(headers)-1; i++ {
		if headers[i+1].Number.Uint64() != headers[i].Number.Uint64()+1 {
			return nil, errOutOfRangeChain
		}
		if !bytes.Equal(headers[i+1].ParentHash.Bytes(), headers[i].Hash().Bytes()) {
			return nil, errBlockHashInconsistent
		}
	}
	if headers[0].Number.Uint64() != s.Number+1 {
		return nil, errOutOfRangeChain
	}
	if !bytes.Equal(headers[0].ParentHash.Bytes(), s.Hash.Bytes()) {
		return nil, errBlockHashInconsistent
	}
	// Iterate through the headers and create a new snapshot
	snap := s.copy()

	for _, header := range headers {
		number := header.Number.Uint64()
		// Delete the oldest validator from the recent list to allow it signing again
		if limit := snap.minerHistoryCheckLen() + 1; number >= limit {
			delete(snap.Recents, number-limit)
		}
		if limit := snap.versionHistoryCheckLen(); number >= limit {
			delete(snap.RecentForkHashes, number-limit)
		}
		// Resolve the authorization key and check against signers
		validator, err := ecrecover(header, s.sigCache, chainConfig.ChainID)
		if err != nil {
			return nil, err
		}
		if _, ok := snap.Validators[validator]; !ok {
			return nil, errUnauthorizedValidator(validator.String())
		}
		if chainConfig.IsBohr(header.Number, header.Time) {
			if snap.SignRecently(validator) {
				return nil, errRecentlySigned
			}
		} else {
			for _, recent := range snap.Recents {
				if recent == validator {
					return nil, errRecentlySigned
				}
			}
		}

		snap.updateAttestation(header, chainConfig)

		snap.Recents[number] = validator
		if chainConfig.IsMaxwell(header.Number, header.Time) {
			latestFinalizedBlockNumber := snap.getFinalizedNumber()
			// BEP-524: Clear entries up to the latest finalized block
			for blockNumber := range snap.Recents {
				if blockNumber <= latestFinalizedBlockNumber {
					delete(snap.Recents, blockNumber)
				}
			}
		}

		snap.RecentForkHashes[number] = hex.EncodeToString(header.Extra[extraVanity-nextForkHashSize : extraVanity])

		if chainConfig.IsMaxwell(header.Number, header.Time) {
			snap.BlockInterval = maxwellBlockInterval
		} else if chainConfig.IsLorentz(header.Number, header.Time) {
			snap.BlockInterval = lorentzBlockInterval
		}

		epochLength := snap.EpochLength
		nextBlockNumber := header.Number.Uint64() + 1
		if snap.EpochLength == defaultEpochLength &&
			chainConfig.IsLorentz(header.Number, header.Time) &&
			// Without this condition, an incorrect block might be used to parse validators for certain blocks after the Lorentz hard fork.
			nextBlockNumber%lorentzEpochLength == 0 {
			snap.EpochLength = lorentzEpochLength
		}
		if snap.EpochLength == lorentzEpochLength &&
			chainConfig.IsMaxwell(header.Number, header.Time) &&
			nextBlockNumber%maxwellEpochLength == 0 {
			snap.EpochLength = maxwellEpochLength
		}
		// change validator set
		if number > 0 && number%epochLength == snap.minerHistoryCheckLen() {
			epochKey := math.MaxUint64 - header.Number.Uint64()/epochLength // impossible used as a block number
			if chainConfig.IsBohr(header.Number, header.Time) {
				// after switching the validator set, snap.Validators may become larger,
				// then the unexpected second switch will happen, just skip it.
				if _, ok := snap.Recents[epochKey]; ok {
					continue
				}
			}

			checkpointHeader := FindAncientHeader(header, snap.minerHistoryCheckLen(), chain, parents)
			if checkpointHeader == nil {
				return nil, consensus.ErrUnknownAncestor
			}

			oldVersionsLen := snap.versionHistoryCheckLen()
			// get turnLength from headers and use that for new turnLength
			turnLength, err := parseTurnLength(checkpointHeader, chainConfig, epochLength)
			if err != nil {
				return nil, err
			}
			if turnLength != nil {
				snap.TurnLength = *turnLength
				log.Debug("validator set switch", "turnLength", *turnLength)
			}

			// get validators from headers and use that for new validator set
			newValArr, voteAddrs, err := parseValidators(checkpointHeader, chainConfig, epochLength)
			if err != nil {
				return nil, err
			}
			newVals := make(map[common.Address]*ValidatorInfo, len(newValArr))
			for idx, val := range newValArr {
				if !chainConfig.IsLuban(header.Number) {
					newVals[val] = &ValidatorInfo{}
				} else {
					newVals[val] = &ValidatorInfo{
						VoteAddress: voteAddrs[idx],
					}
				}
			}
			if chainConfig.IsBohr(header.Number, header.Time) {
				// BEP-404: Clear Miner History when Switching Validators Set
				snap.Recents = make(map[uint64]common.Address)
				snap.Recents[epochKey] = common.Address{}
				log.Debug("Recents are cleared up", "blockNumber", number)
			} else {
				oldLimit := len(snap.Validators)/2 + 1
				newLimit := len(newVals)/2 + 1
				if newLimit < oldLimit {
					for i := 0; i < oldLimit-newLimit; i++ {
						delete(snap.Recents, number-uint64(newLimit)-uint64(i))
					}
				}
			}
			snap.Validators = newVals
			if chainConfig.IsLuban(header.Number) {
				validators := snap.validators()
				for idx, val := range validators {
					snap.Validators[val].Index = idx + 1 // offset by 1
				}
			}
			for i := snap.versionHistoryCheckLen(); i < oldVersionsLen; i++ {
				delete(snap.RecentForkHashes, number-i)
			}
		}
	}
	snap.Number += uint64(len(headers))
	snap.Hash = headers[len(headers)-1].Hash()
	return snap, nil
}

// validators retrieves the list of validators in ascending order.
func (s *Snapshot) validators() []common.Address {
	validators := make([]common.Address, 0, len(s.Validators))
	for v := range s.Validators {
		validators = append(validators, v)
	}
	sort.Sort(validatorsAscending(validators))
	return validators
}

// lastBlockInOneTurn returns if the block at height `blockNumber` is the last block in current turn.
func (s *Snapshot) lastBlockInOneTurn(blockNumber uint64) bool {
	return (blockNumber+1)%uint64(s.TurnLength) == 0
}

// inturn returns if a validator at a given block height is in-turn or not.
func (s *Snapshot) inturn(validator common.Address) bool {
	return s.inturnValidator() == validator
}

// inturnValidator returns the validator for the following block height.
func (s *Snapshot) inturnValidator() common.Address {
	validators := s.validators()
	offset := (s.Number + 1) / uint64(s.TurnLength) % uint64(len(validators))
	return validators[offset]
}

func (s *Snapshot) nexValidatorsChangeBlock() uint64 {
	epochLength := s.EpochLength
	currentEpoch := s.Number - s.Number%epochLength
	checkLen := s.minerHistoryCheckLen()
	if s.Number%epochLength < checkLen {
		return currentEpoch + checkLen
	}
	return currentEpoch + epochLength + checkLen
}

// nextProposalBlock returns the validator next proposal block.
func (s *Snapshot) nextProposalBlock(proposer common.Address) (uint64, uint64, error) {
	validators := s.validators()
	currentIndex := int(s.Number / uint64(s.TurnLength) % uint64(len(validators)))
	expectIndex := s.indexOfVal(proposer)
	if expectIndex < 0 {
		return 0, 0, errors.New("proposer not in validator set")
	}
	startBlock := s.Number + uint64(((expectIndex+len(validators)-currentIndex)%len(validators))*int(s.TurnLength))
	startBlock = startBlock - startBlock%uint64(s.TurnLength)
	endBlock := startBlock + uint64(s.TurnLength) - 1

	changeValidatorsBlock := s.nexValidatorsChangeBlock()
	if startBlock >= changeValidatorsBlock {
		return 0, 0, errors.New("next proposal block is out of current epoch")
	}
	if endBlock >= changeValidatorsBlock {
		endBlock = changeValidatorsBlock
	}
	return startBlock, endBlock, nil
}

func (s *Snapshot) enoughDistance(validator common.Address, header *types.Header) bool {
	idx := s.indexOfVal(validator)
	if idx < 0 {
		return true
	}
	validatorNum := int64(len(s.validators()))
	if validatorNum == 1 {
		return true
	}
	if validator == header.Coinbase {
		return false
	}
	offset := (int64(s.Number) + 1) % validatorNum
	if int64(idx) >= offset {
		return int64(idx)-offset >= validatorNum-2
	} else {
		return validatorNum+int64(idx)-offset >= validatorNum-2
	}
}

func (s *Snapshot) indexOfVal(validator common.Address) int {
	if validator, ok := s.Validators[validator]; ok && validator.Index > 0 {
		return validator.Index - 1 // Index is offset by 1
	}

	validators := s.validators()
	for idx, val := range validators {
		if val == validator {
			return idx
		}
	}
	return -1
}

func parseValidators(header *types.Header, chainConfig *params.ChainConfig, epochLength uint64) ([]common.Address, []types.BLSPublicKey, error) {
	validatorsBytes := getValidatorBytesFromHeader(header, chainConfig, epochLength)
	if len(validatorsBytes) == 0 {
		return nil, nil, errors.New("invalid validators bytes")
	}

	if !chainConfig.IsLuban(header.Number) {
		n := len(validatorsBytes) / validatorBytesLengthBeforeLuban
		result := make([]common.Address, n)
		for i := 0; i < n; i++ {
			result[i] = common.BytesToAddress(validatorsBytes[i*validatorBytesLengthBeforeLuban : (i+1)*validatorBytesLengthBeforeLuban])
		}
		return result, nil, nil
	}

	n := len(validatorsBytes) / validatorBytesLength
	cnsAddrs := make([]common.Address, n)
	voteAddrs := make([]types.BLSPublicKey, n)
	for i := 0; i < n; i++ {
		cnsAddrs[i] = common.BytesToAddress(validatorsBytes[i*validatorBytesLength : i*validatorBytesLength+common.AddressLength])
		copy(voteAddrs[i][:], validatorsBytes[i*validatorBytesLength+common.AddressLength:(i+1)*validatorBytesLength])
	}
	return cnsAddrs, voteAddrs, nil
}

func parseTurnLength(header *types.Header, chainConfig *params.ChainConfig, epochLength uint64) (*uint8, error) {
	if header.Number.Uint64()%epochLength != 0 ||
		!chainConfig.IsBohr(header.Number, header.Time) {
		return nil, nil
	}

	if len(header.Extra) <= extraVanity+extraSeal {
		return nil, errInvalidSpanValidators
	}
	num := int(header.Extra[extraVanity])
	pos := extraVanity + validatorNumberSize + num*validatorBytesLength
	if len(header.Extra) <= pos {
		return nil, errInvalidTurnLength
	}
	turnLength := header.Extra[pos]
	return &turnLength, nil
}

func FindAncientHeader(header *types.Header, ite uint64, chain consensus.ChainHeaderReader, candidateParents []*types.Header) *types.Header {
	ancient := header
	for i := uint64(1); i <= ite; i++ {
		parentHash := ancient.ParentHash
		parentHeight := ancient.Number.Uint64() - 1
		found := false
		if len(candidateParents) > 0 {
			index := sort.Search(len(candidateParents), func(i int) bool {
				return candidateParents[i].Number.Uint64() >= parentHeight
			})
			if index < len(candidateParents) && candidateParents[index].Number.Uint64() == parentHeight &&
				candidateParents[index].Hash() == parentHash {
				ancient = candidateParents[index]
				found = true
			}
		}
		if !found {
			ancient = chain.GetHeader(parentHash, parentHeight)
			found = true
		}
		if ancient == nil || !found {
			return nil
		}
	}
	return ancient
}
