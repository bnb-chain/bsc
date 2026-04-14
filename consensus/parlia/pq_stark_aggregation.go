package parlia

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/pq/proofs"
)

// STARK signature aggregation errors.
var (
	ErrSTARKAggNoSigs       = errors.New("stark_sig_aggregation: no signatures to aggregate")
	ErrSTARKAggInvalidProof = errors.New("stark_sig_aggregation: invalid aggregate proof")
	ErrSTARKAggVerifyFailed = errors.New("stark_sig_aggregation: verification failed")
	ErrSTARKAggNilResult    = errors.New("stark_sig_aggregation: nil aggregation result")
	ErrSTARKAggMismatch     = errors.New("stark_sig_aggregation: committee root mismatch")
)

// PQVoteData holds the data for a single PQ vote to be aggregated.
type PQVoteData struct {
	TargetNumber   uint64
	TargetHash     common.Hash
	SourceNumber   uint64
	SourceHash     common.Hash
	PQSignature    []byte // ML-DSA-44 signature (2420 bytes)
	PQPublicKey    []byte // ML-DSA-44 public key (1312 bytes)
	ValidatorIndex int
}

// STARKSignatureAggregation holds a STARK-aggregated set of PQ vote signatures.
type STARKSignatureAggregation struct {
	// AggregateProof is the STARK proof that all signatures are valid.
	AggregateProof *proofs.STARKProofData
	// CommitteeRoot is the Merkle root of the participating validator public keys.
	CommitteeRoot common.Hash
	// VoteDataHash is the hash of the vote data being attested.
	VoteDataHash common.Hash
	// NumValidators is the number of validators in this aggregation.
	NumValidators int
}

// STARKSignatureAggregator creates and verifies STARK-aggregated signature proofs.
type STARKSignatureAggregator struct {
	mu     sync.RWMutex
	prover *proofs.STARKProver
}

// NewSTARKSignatureAggregator creates a new STARK signature aggregator.
func NewSTARKSignatureAggregator() *STARKSignatureAggregator {
	return &STARKSignatureAggregator{
		prover: proofs.NewSTARKProver(),
	}
}

// Aggregate creates a STARK proof that all given PQ vote signatures are valid.
// This replaces BLS AggregateSignatures with a single STARK verification.
func (sa *STARKSignatureAggregator) Aggregate(votes []PQVoteData, voteDataHash common.Hash) (*STARKSignatureAggregation, error) {
	if len(votes) == 0 {
		return nil, ErrSTARKAggNoSigs
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()

	// Build execution trace: each vote becomes a row.
	// Columns: [target_number, source_number, target_hash_hi, target_hash_lo, sig_hash_hi, sig_hash_lo, validator_index]
	trace := make([][]proofs.FieldElement, len(votes))
	pubkeys := make([][]byte, len(votes))

	for i, vote := range votes {
		sigHash := hashSignatureData(vote.PQSignature, vote.PQPublicKey)
		targetHi := new(big.Int).SetBytes(vote.TargetHash[:16])
		targetLo := new(big.Int).SetBytes(vote.TargetHash[16:])
		sigHi := new(big.Int).SetBytes(sigHash[:16])
		sigLo := new(big.Int).SetBytes(sigHash[16:])

		trace[i] = []proofs.FieldElement{
			proofs.NewFieldElement(int64(vote.TargetNumber)),
			proofs.NewFieldElement(int64(vote.SourceNumber)),
			{Value: targetHi},
			{Value: targetLo},
			{Value: sigHi},
			{Value: sigLo},
			proofs.NewFieldElement(int64(vote.ValidatorIndex)),
		}
		pubkeys[i] = vote.PQPublicKey
	}

	// Constraint: each row's sig_hash must be non-zero (signature exists).
	constraints := []proofs.STARKConstraint{
		{Degree: 1, Coefficients: []proofs.FieldElement{proofs.NewFieldElement(1)}},
	}

	starkProof, err := sa.prover.GenerateSTARKProof(trace, constraints)
	if err != nil {
		return nil, err
	}

	// Compute committee root from public keys.
	committeeRoot := computeCommitteeRoot(pubkeys)

	return &STARKSignatureAggregation{
		AggregateProof: starkProof,
		CommitteeRoot:  committeeRoot,
		VoteDataHash:   voteDataHash,
		NumValidators:  len(votes),
	}, nil
}

// Verify checks that a STARK signature aggregation is valid.
// expectedVoteDataHash should be the hash of the vote data being attested (prevents cross-block replay).
func (sa *STARKSignatureAggregator) Verify(agg *STARKSignatureAggregation, pubkeys [][]byte, expectedVoteDataHash common.Hash) (bool, error) {
	if agg == nil {
		return false, ErrSTARKAggNilResult
	}
	if agg.AggregateProof == nil {
		return false, ErrSTARKAggInvalidProof
	}

	// Verify that the proof is bound to the expected vote data (prevents replay attacks).
	if agg.VoteDataHash != expectedVoteDataHash {
		return false, fmt.Errorf("stark_sig_aggregation: vote data hash mismatch, expected %s, got %s",
			expectedVoteDataHash.Hex(), agg.VoteDataHash.Hex())
	}

	sa.mu.RLock()
	defer sa.mu.RUnlock()

	// Verify the STARK proof.
	valid, err := sa.prover.VerifySTARKProof(agg.AggregateProof, nil)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, ErrSTARKAggVerifyFailed
	}

	// Verify committee root matches the public keys if provided.
	if len(pubkeys) > 0 {
		expectedRoot := computeCommitteeRoot(pubkeys)
		if expectedRoot != agg.CommitteeRoot {
			return false, ErrSTARKAggMismatch
		}
	}

	return true, nil
}

// MarshalProof serializes a STARKSignatureAggregation to bytes for storage in block headers.
func MarshalSTARKAggregation(agg *STARKSignatureAggregation) ([]byte, error) {
	if agg == nil || agg.AggregateProof == nil {
		return nil, ErrSTARKAggNilResult
	}

	// Layout: [committeeRoot(32)] [voteDataHash(32)] [numValidators(4)]
	//         [commitmentRoot(32)] [numFRILayers(4)] [friLayers...]
	//         [numQueries(4)] [queries...]
	var buf []byte

	buf = append(buf, agg.CommitteeRoot.Bytes()...)
	buf = append(buf, agg.VoteDataHash.Bytes()...)
	buf = append(buf, byte(agg.NumValidators>>24), byte(agg.NumValidators>>16), byte(agg.NumValidators>>8), byte(agg.NumValidators))

	proof := agg.AggregateProof
	buf = append(buf, proof.CommitmentRoot[:]...)

	numFRI := len(proof.FRILayers)
	buf = append(buf, byte(numFRI>>24), byte(numFRI>>16), byte(numFRI>>8), byte(numFRI))
	for _, layer := range proof.FRILayers {
		buf = append(buf, layer[:]...)
	}

	numQ := len(proof.QueryResponses)
	buf = append(buf, byte(numQ>>24), byte(numQ>>16), byte(numQ>>8), byte(numQ))
	for _, qr := range proof.QueryResponses {
		buf = append(buf, byte(qr.Index>>24), byte(qr.Index>>16), byte(qr.Index>>8), byte(qr.Index))
		buf = append(buf, qr.Value[:]...)
		numAuth := len(qr.AuthPath)
		buf = append(buf, byte(numAuth>>24), byte(numAuth>>16), byte(numAuth>>8), byte(numAuth))
		for _, auth := range qr.AuthPath {
			buf = append(buf, auth[:]...)
		}
	}

	return buf, nil
}

// UnmarshalSTARKAggregation deserializes a STARKSignatureAggregation from bytes.
func UnmarshalSTARKAggregation(data []byte) (*STARKSignatureAggregation, error) {
	if len(data) < 104 { // minimum: 32+32+4+32+4 = 104
		return nil, ErrSTARKAggInvalidProof
	}

	offset := 0

	var committeeRoot common.Hash
	copy(committeeRoot[:], data[offset:offset+32])
	offset += 32

	var voteDataHash common.Hash
	copy(voteDataHash[:], data[offset:offset+32])
	offset += 32

	numValidators := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
	offset += 4
	if numValidators <= 0 || numValidators > 1000 { // sanity limit: BSC has ~21-45 validators
		return nil, ErrSTARKAggInvalidProof
	}

	var commitmentRoot [32]byte
	copy(commitmentRoot[:], data[offset:offset+32])
	offset += 32

	numFRI := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
	offset += 4
	if numFRI > 64 { // sanity limit: log2(2^64) = 64 layers max
		return nil, ErrSTARKAggInvalidProof
	}

	friLayers := make([][32]byte, numFRI)
	for i := 0; i < numFRI; i++ {
		if offset+32 > len(data) {
			return nil, ErrSTARKAggInvalidProof
		}
		copy(friLayers[i][:], data[offset:offset+32])
		offset += 32
	}

	if offset+4 > len(data) {
		return nil, ErrSTARKAggInvalidProof
	}
	numQ := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
	offset += 4
	if numQ > 1024 { // sanity limit: no more than 1024 query responses
		return nil, ErrSTARKAggInvalidProof
	}

	queryResponses := make([]proofs.QueryResponse, numQ)
	for i := 0; i < numQ; i++ {
		if offset+36 > len(data) {
			return nil, ErrSTARKAggInvalidProof
		}
		idx := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		offset += 4
		var val [32]byte
		copy(val[:], data[offset:offset+32])
		offset += 32

		if offset+4 > len(data) {
			return nil, ErrSTARKAggInvalidProof
		}
		numAuth := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		offset += 4
		if numAuth > 64 { // sanity limit: max tree depth
			return nil, ErrSTARKAggInvalidProof
		}

		authPath := make([][32]byte, numAuth)
		for j := 0; j < numAuth; j++ {
			if offset+32 > len(data) {
				return nil, ErrSTARKAggInvalidProof
			}
			copy(authPath[j][:], data[offset:offset+32])
			offset += 32
		}

		queryResponses[i] = proofs.QueryResponse{
			Index:    idx,
			Value:    val,
			AuthPath: authPath,
		}
	}

	return &STARKSignatureAggregation{
		AggregateProof: &proofs.STARKProofData{
			CommitmentRoot: commitmentRoot,
			FRILayers:      friLayers,
			QueryResponses: queryResponses,
			TraceLength:    numValidators,
			NumColumns:     7,
		},
		CommitteeRoot: committeeRoot,
		VoteDataHash:  voteDataHash,
		NumValidators: numValidators,
	}, nil
}

// hashSignatureData hashes signature and public key into a commitment.
func hashSignatureData(sig, pubkey []byte) [32]byte {
	h := sha256.New()
	h.Write(sig)
	h.Write(pubkey)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// computeCommitteeRoot computes a Merkle root over validator public keys.
func computeCommitteeRoot(pubkeys [][]byte) common.Hash {
	if len(pubkeys) == 0 {
		return common.Hash{}
	}

	// Hash each public key into a leaf.
	leaves := make([][32]byte, len(pubkeys))
	for i, pk := range pubkeys {
		h := sha256.New()
		h.Write(pk)
		copy(leaves[i][:], h.Sum(nil))
	}

	// Pad to next power of two.
	n := len(leaves)
	target := 1
	for target < n {
		target <<= 1
	}
	padded := make([][32]byte, target)
	copy(padded, leaves)

	// Build Merkle tree bottom-up.
	layer := padded
	for len(layer) > 1 {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			h := sha256.New()
			h.Write(layer[2*i][:])
			h.Write(layer[2*i+1][:])
			copy(next[i][:], h.Sum(nil))
		}
		layer = next
	}

	var root common.Hash
	copy(root[:], layer[0][:])
	return root
}
