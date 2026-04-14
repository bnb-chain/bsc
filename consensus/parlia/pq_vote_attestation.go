package parlia

import (
	"bytes"
	"fmt"

	"github.com/bits-and-blooms/bitset"
	"github.com/ethereum/go-ethereum/common"
	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// pqAssembleVoteAttestation collects PQ votes and assembles the STARK-aggregated
// vote attestation into the block header. This replaces BLS aggregation post-PQFork.
func (p *Parlia) pqAssembleVoteAttestation(chain consensus.ChainHeaderReader, header *types.Header) error {
	if !p.chainConfig.IsLuban(header.Number) || header.Number.Uint64() < 3 || p.VotePool == nil {
		return nil
	}

	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return fmt.Errorf("parent not found")
	}
	justifiedBlockNumber, justifiedBlockHash, err := p.GetJustifiedNumberAndHash(chain, []*types.Header{parent})
	if err != nil {
		return fmt.Errorf("unexpected error when getting the highest justified number and hash")
	}

	var (
		votes                  []*types.VoteEnvelope
		targetHeader           = parent
		targetHeaderParentSnap *Snapshot
	)
	for range p.GetAncestorGenerationDepth(header) {
		snap, err := p.snapshot(chain, targetHeader.Number.Uint64()-1, targetHeader.ParentHash, nil)
		if err != nil {
			return err
		}
		votes = p.VotePool.FetchVotesByBlockHash(targetHeader.Hash(), justifiedBlockNumber)
		quorum := cmath.CeilDiv(len(snap.Validators)*2, 3)
		if len(votes) >= quorum {
			targetHeaderParentSnap = snap
			break
		}

		targetHeader = chain.GetHeaderByHash(targetHeader.ParentHash)
		if targetHeader == nil {
			return fmt.Errorf("parent not found")
		}
		if targetHeader.Number.Uint64() <= justifiedBlockNumber {
			break
		}
	}
	if targetHeaderParentSnap == nil {
		return nil
	}

	// Build PQ vote attestation with STARK aggregation.
	attestation := &types.PQVoteAttestation{
		Data: &types.VoteData{
			SourceNumber: justifiedBlockNumber,
			SourceHash:   justifiedBlockHash,
			TargetNumber: targetHeader.Number.Uint64(),
			TargetHash:   targetHeader.Hash(),
		},
	}

	// Validate vote data consistency.
	for _, vote := range votes {
		if vote.Data.Hash() != attestation.Data.Hash() {
			return fmt.Errorf("vote check error, expected: %v, real: %v", attestation.Data, vote.Data)
		}
	}

	// Build a map from vote address hash to vote for deduplication and lookup.
	voteAddrSet := make(map[common.Hash]*types.VoteEnvelope, len(votes))
	for _, vote := range votes {
		addrHash := crypto.Keccak256Hash(vote.VoteAddress[:])
		voteAddrSet[addrHash] = vote
	}

	// Collect votes in canonical validator order (sorted by address, matching snap.validators()).
	// This ensures the committee root is deterministic across all nodes.
	validators := targetHeaderParentSnap.validators()
	pqVotes := make([]PQVoteData, 0, len(votes))
	for idx, val := range validators {
		addrHash := crypto.Keccak256Hash(targetHeaderParentSnap.Validators[val].VoteAddress[:])
		vote, ok := voteAddrSet[addrHash]
		if !ok {
			continue
		}
		pqVotes = append(pqVotes, PQVoteData{
			TargetNumber:   vote.Data.TargetNumber,
			TargetHash:     vote.Data.TargetHash,
			SourceNumber:   vote.Data.SourceNumber,
			SourceHash:     vote.Data.SourceHash,
			PQSignature:    vote.Signature[:],
			PQPublicKey:    vote.VoteAddress[:],
			ValidatorIndex: idx,
		})
	}

	// STARK aggregate the signatures.
	aggregator := NewSTARKSignatureAggregator()
	agg, err := aggregator.Aggregate(pqVotes, attestation.Data.Hash())
	if err != nil {
		log.Error("Failed to STARK aggregate vote signatures", "err", err)
		return err
	}

	// Marshal the STARK aggregation proof.
	proofBytes, err := MarshalSTARKAggregation(agg)
	if err != nil {
		log.Error("Failed to marshal STARK aggregation", "err", err)
		return err
	}
	attestation.AggProof = proofBytes
	log.Info("PQ STARK attestation assembled",
		"block", header.Number, "target", attestation.Data.TargetNumber,
		"source", attestation.Data.SourceNumber, "votes", len(pqVotes),
		"proofSize", len(proofBytes))

	// Prepare vote address bitset.
	for _, valInfo := range targetHeaderParentSnap.Validators {
		addrHash := crypto.Keccak256Hash(valInfo.VoteAddress[:])
		if _, ok := voteAddrSet[addrHash]; ok {
			attestation.VoteAddressSet |= 1 << (valInfo.Index - 1)
		}
	}

	bitsetCount := bitset.From([]uint64{uint64(attestation.VoteAddressSet)}).Count()
	if bitsetCount < uint(len(votes)) {
		log.Warn(fmt.Sprintf("pqAssembleVoteAttestation, check VoteAddress Set failed, expected:%d, real:%d", len(votes), bitsetCount))
		return fmt.Errorf("invalid attestation, check VoteAddress Set failed")
	}

	// Encode & insert into header extra.
	buf := new(bytes.Buffer)
	if err = rlp.Encode(buf, attestation); err != nil {
		return fmt.Errorf("attestation: failed to encode: %w", err)
	}
	extraSealStart := len(header.Extra) - extraSeal
	extraSealBytes := header.Extra[extraSealStart:]
	header.Extra = append(header.Extra[:extraSealStart], buf.Bytes()...)
	header.Extra = append(header.Extra, extraSealBytes...)

	return nil
}

// pqVerifyVoteAttestation verifies a PQ vote attestation using STARK proof verification.
func (p *Parlia) pqVerifyVoteAttestation(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	epochLength, err := p.epochLength(chain, header, parents)
	if err != nil {
		return err
	}
	attestation, err := getPQVoteAttestationFromHeader(header, chain.Config(), epochLength)
	if err != nil {
		return err
	}
	if attestation == nil {
		return nil
	}
	if attestation.Data == nil {
		return fmt.Errorf("invalid attestation, vote data is nil")
	}
	if len(attestation.Extra) > types.MaxAttestationExtraLength {
		return fmt.Errorf("invalid attestation, too large extra length: %d", len(attestation.Extra))
	}
	if attestation.Data.SourceNumber >= attestation.Data.TargetNumber {
		return fmt.Errorf("invalid attestation, SourceNumber not lower than TargetNumber")
	}

	// Verify source block.
	parent, err := p.getParent(chain, header, parents)
	if err != nil {
		return err
	}
	sourceNumber := attestation.Data.SourceNumber
	sourceHash := attestation.Data.SourceHash
	headers := []*types.Header{parent}
	if len(parents) > 0 {
		headers = parents
	}
	justifiedBlockNumber, justifiedBlockHash, err := p.GetJustifiedNumberAndHash(chain, headers)
	if err != nil {
		return fmt.Errorf("unexpected error when getting the highest justified number and hash")
	}
	if sourceNumber != justifiedBlockNumber || sourceHash != justifiedBlockHash {
		return fmt.Errorf("invalid attestation, source mismatch, expected block: %d, hash: %s; real block: %d, hash: %s",
			justifiedBlockNumber, justifiedBlockHash, sourceNumber, sourceHash)
	}

	// Verify target block.
	targetNumber := attestation.Data.TargetNumber
	targetHash := attestation.Data.TargetHash
	match := false
	ancestor := parent
	ancestorParents := trimParents(parents)
	for range p.GetAncestorGenerationDepth(header) {
		if targetNumber == ancestor.Number.Uint64() && targetHash == ancestor.Hash() {
			match = true
			break
		}
		ancestor, err = p.getParent(chain, ancestor, ancestorParents)
		if err != nil {
			return err
		}
		ancestorParents = trimParents(ancestorParents)
	}
	if !match {
		return fmt.Errorf("invalid attestation, target mismatch, real block: %d, hash: %s", targetNumber, targetHash)
	}

	// Check quorum.
	snap, err := p.snapshot(chain, ancestor.Number.Uint64()-1, ancestor.ParentHash, ancestorParents)
	if err != nil {
		return err
	}
	validators := snap.validators()
	validatorsBitSet := bitset.From([]uint64{uint64(attestation.VoteAddressSet)})
	if validatorsBitSet.Count() > uint(len(validators)) {
		return fmt.Errorf("invalid attestation, vote number larger than validators number")
	}

	// Collect voted validator public keys for committee root verification.
	votedPubkeys := make([][]byte, 0, validatorsBitSet.Count())
	votedCount := 0
	for index, val := range validators {
		if !validatorsBitSet.Test(uint(index)) {
			continue
		}
		votedPubkeys = append(votedPubkeys, snap.Validators[val].VoteAddress[:])
		votedCount++
	}

	// Check 2/3 quorum.
	if votedCount < cmath.CeilDiv(len(snap.Validators)*2, 3) {
		return fmt.Errorf("invalid attestation, not enough validators voted")
	}

	// Verify STARK aggregate proof.
	agg, err := UnmarshalSTARKAggregation(attestation.AggProof)
	if err != nil {
		return fmt.Errorf("failed to unmarshal STARK aggregation: %v", err)
	}

	aggregator := NewSTARKSignatureAggregator()
	valid, err := aggregator.Verify(agg, votedPubkeys, attestation.Data.Hash())
	if err != nil {
		return fmt.Errorf("STARK verification failed: %v", err)
	}
	if !valid {
		return fmt.Errorf("invalid attestation, STARK signature verify failed")
	}

	log.Info("PQ STARK attestation verified",
		"block", header.Number, "target", attestation.Data.TargetNumber,
		"source", attestation.Data.SourceNumber, "voters", votedCount,
		"proofSize", len(attestation.AggProof))

	return nil
}

// getPQVoteAttestationFromHeader extracts PQ vote attestation from the block header.
// This mirrors getVoteAttestationFromHeader but decodes PQVoteAttestation.
func getPQVoteAttestationFromHeader(header *types.Header, chainConfig *params.ChainConfig, epochLength uint64) (*types.PQVoteAttestation, error) {
	if len(header.Extra) <= extraVanity+extraSeal {
		return nil, nil
	}

	number := header.Number.Uint64()

	var attestationBytes []byte
	if number%epochLength != 0 {
		attestationBytes = header.Extra[extraVanity : len(header.Extra)-extraSeal]
	} else {
		// Epoch block: skip the validator set bytes.
		num := int(header.Extra[extraVanity])
		start := extraVanity + validatorNumberSize + num*validatorBytesLength
		if chainConfig.IsBohr(header.Number, header.Time) {
			start += turnLengthSize
		}
		end := len(header.Extra) - extraSeal
		if end <= start {
			return nil, nil
		}
		attestationBytes = header.Extra[start:end]
	}

	if len(attestationBytes) == 0 {
		return nil, nil
	}

	var attestation types.PQVoteAttestation
	if err := rlp.DecodeBytes(attestationBytes, &attestation); err != nil {
		return nil, fmt.Errorf("block %d has vote attestation info, decode err: %s", number, err)
	}

	return &attestation, nil
}
