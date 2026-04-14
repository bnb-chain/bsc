package parlia

import (
	"crypto/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func generateTestVotes(n int) ([]PQVoteData, common.Hash) {
	voteDataHash := common.BytesToHash([]byte("test-vote-data-hash-1234567890ab"))
	votes := make([]PQVoteData, n)
	for i := 0; i < n; i++ {
		sig := make([]byte, 2420) // ML-DSA-44 signature size
		rand.Read(sig)
		pubkey := make([]byte, 1312) // ML-DSA-44 public key size
		rand.Read(pubkey)
		votes[i] = PQVoteData{
			TargetNumber:   100,
			TargetHash:     common.BytesToHash([]byte("target-hash-abcdefghijklmnopqrst")),
			SourceNumber:   99,
			SourceHash:     common.BytesToHash([]byte("source-hash-abcdefghijklmnopqrst")),
			PQSignature:    sig,
			PQPublicKey:    pubkey,
			ValidatorIndex: i,
		}
	}
	return votes, voteDataHash
}

func TestSTARKAggregation_Basic(t *testing.T) {
	votes, voteDataHash := generateTestVotes(21) // BSC has 21 validators
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}
	if agg == nil {
		t.Fatal("Aggregate returned nil")
	}
	if agg.NumValidators != 21 {
		t.Errorf("expected 21 validators, got %d", agg.NumValidators)
	}
	if agg.VoteDataHash != voteDataHash {
		t.Error("vote data hash mismatch")
	}
}

func TestSTARKAggregation_Verify(t *testing.T) {
	votes, voteDataHash := generateTestVotes(15)
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	// Extract pubkeys for verification.
	pubkeys := make([][]byte, len(votes))
	for i, v := range votes {
		pubkeys[i] = v.PQPublicKey
	}

	valid, err := aggregator.Verify(agg, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !valid {
		t.Error("expected valid aggregation")
	}
}

func TestSTARKAggregation_VerifyMismatchedPubkeys(t *testing.T) {
	votes, voteDataHash := generateTestVotes(10)
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	// Use different pubkeys for verification — should fail committee root check.
	wrongPubkeys := make([][]byte, len(votes))
	for i := range votes {
		wrongPubkeys[i] = make([]byte, 1312)
		rand.Read(wrongPubkeys[i])
	}

	valid, err := aggregator.Verify(agg, wrongPubkeys, voteDataHash)
	if err == nil || valid {
		t.Error("expected verification to fail with mismatched pubkeys")
	}
}

func TestSTARKAggregation_EmptyVotes(t *testing.T) {
	aggregator := NewSTARKSignatureAggregator()
	_, err := aggregator.Aggregate(nil, common.Hash{})
	if err != ErrSTARKAggNoSigs {
		t.Errorf("expected ErrSTARKAggNoSigs, got %v", err)
	}
}

func TestSTARKAggregation_MarshalUnmarshal(t *testing.T) {
	votes, voteDataHash := generateTestVotes(21)
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	// Marshal
	data, err := MarshalSTARKAggregation(agg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Marshal returned empty data")
	}

	// Unmarshal
	agg2, err := UnmarshalSTARKAggregation(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if agg2.NumValidators != agg.NumValidators {
		t.Errorf("NumValidators mismatch: %d vs %d", agg2.NumValidators, agg.NumValidators)
	}
	if agg2.CommitteeRoot != agg.CommitteeRoot {
		t.Error("CommitteeRoot mismatch after marshal/unmarshal")
	}
	if agg2.VoteDataHash != agg.VoteDataHash {
		t.Error("VoteDataHash mismatch after marshal/unmarshal")
	}

	// Verify the unmarshaled aggregation
	pubkeys := make([][]byte, len(votes))
	for i, v := range votes {
		pubkeys[i] = v.PQPublicKey
	}
	valid, err := aggregator.Verify(agg2, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify after unmarshal failed: %v", err)
	}
	if !valid {
		t.Error("expected valid after unmarshal")
	}
}

func TestSTARKAggregation_SingleVote(t *testing.T) {
	votes, voteDataHash := generateTestVotes(1)
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate with single vote failed: %v", err)
	}
	if agg.NumValidators != 1 {
		t.Errorf("expected 1 validator, got %d", agg.NumValidators)
	}

	pubkeys := [][]byte{votes[0].PQPublicKey}
	valid, err := aggregator.Verify(agg, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify single vote failed: %v", err)
	}
	if !valid {
		t.Error("expected valid for single vote")
	}
}

func TestSTARKAggregation_NilVerify(t *testing.T) {
	aggregator := NewSTARKSignatureAggregator()

	valid, err := aggregator.Verify(nil, nil, common.Hash{})
	if err != ErrSTARKAggNilResult {
		t.Errorf("expected ErrSTARKAggNilResult, got %v", err)
	}
	if valid {
		t.Error("expected invalid for nil aggregation")
	}
}

func TestSTARKAggregation_VoteDataHashMismatch(t *testing.T) {
	votes, voteDataHash := generateTestVotes(10)
	aggregator := NewSTARKSignatureAggregator()

	agg, err := aggregator.Aggregate(votes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	pubkeys := make([][]byte, len(votes))
	for i, v := range votes {
		pubkeys[i] = v.PQPublicKey
	}

	// Use a different vote data hash — should fail (C3 fix: prevents cross-block replay).
	wrongHash := common.BytesToHash([]byte("wrong-hash-abcdefghijklmnopqrstuv"))
	valid, err := aggregator.Verify(agg, pubkeys, wrongHash)
	if err == nil || valid {
		t.Error("expected verification to fail with mismatched vote data hash")
	}
}

func BenchmarkSTARKAggregation_21Validators(b *testing.B) {
	votes, voteDataHash := generateTestVotes(21)
	aggregator := NewSTARKSignatureAggregator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agg, err := aggregator.Aggregate(votes, voteDataHash)
		if err != nil {
			b.Fatal(err)
		}
		pubkeys := make([][]byte, len(votes))
		for j, v := range votes {
			pubkeys[j] = v.PQPublicKey
		}
		_, err = aggregator.Verify(agg, pubkeys, voteDataHash)
		if err != nil {
			b.Fatal(err)
		}
	}
}
