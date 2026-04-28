package parlia

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
	"github.com/ethereum/go-ethereum/rlp"
)

// signPQVote signs a PQVoteEnvelope using ML-DSA-44 (test helper, avoids core/vote import cycle).
func signPQVote(env *types.PQVoteEnvelope, privKey, pubKey []byte) error {
	voteDataHash := env.Data.Hash()
	sig, err := mldsa.Sign(privKey, voteDataHash[:])
	if err != nil {
		return err
	}
	copy(env.VoteAddress[:], pubKey)
	copy(env.Signature[:], sig)
	return nil
}

// TestPQE2E_FullFlow tests the complete post-quantum vote attestation pipeline:
// ML-DSA-44 key generation → PQ vote signing → individual signature verification →
// STARK aggregation → marshal/unmarshal → STARK verification → RLP round-trip.
func TestPQE2E_FullFlow(t *testing.T) {
	const numValidators = 21

	// Step 1: Generate ML-DSA-44 keypairs for validators.
	type validatorKeys struct {
		privKey []byte
		pubKey  []byte
	}
	validators := make([]validatorKeys, numValidators)
	for i := 0; i < numValidators; i++ {
		pub, priv, err := mldsa.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey[%d]: %v", i, err)
		}
		validators[i] = validatorKeys{privKey: priv, pubKey: pub}
	}

	// Step 2: Create vote data (simulating source/target blocks).
	voteData := &types.VoteData{
		SourceNumber: 99,
		SourceHash:   common.BytesToHash([]byte("source-hash-for-e2e-test-padding")),
		TargetNumber: 100,
		TargetHash:   common.BytesToHash([]byte("target-hash-for-e2e-test-padding")),
	}
	voteDataHash := voteData.Hash()

	// Step 3: Sign votes and verify each individual ML-DSA-44 signature.
	pqEnvelopes := make([]*types.PQVoteEnvelope, numValidators)
	for i, val := range validators {
		env := &types.PQVoteEnvelope{Data: voteData}
		if err := signPQVote(env, val.privKey, val.pubKey); err != nil {
			t.Fatalf("SignVote[%d]: %v", i, err)
		}
		if err := env.Verify(); err != nil {
			t.Fatalf("Verify individual vote[%d]: %v", i, err)
		}
		pqEnvelopes[i] = env
	}

	// Step 4: Build PQVoteData array (mirrors pqAssembleVoteAttestation logic).
	pqVotes := make([]PQVoteData, numValidators)
	for i, env := range pqEnvelopes {
		pqVotes[i] = PQVoteData{
			TargetNumber:   env.Data.TargetNumber,
			TargetHash:     env.Data.TargetHash,
			SourceNumber:   env.Data.SourceNumber,
			SourceHash:     env.Data.SourceHash,
			PQSignature:    env.Signature[:],
			PQPublicKey:    env.VoteAddress[:],
			ValidatorIndex: i,
		}
	}

	// Step 5: STARK aggregate.
	aggregator := NewSTARKSignatureAggregator()
	agg, err := aggregator.Aggregate(pqVotes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if agg.NumValidators != numValidators {
		t.Errorf("NumValidators: got %d, want %d", agg.NumValidators, numValidators)
	}
	if agg.VoteDataHash != voteDataHash {
		t.Error("VoteDataHash mismatch after aggregation")
	}

	// Step 6: Verify aggregation with correct pubkeys.
	pubkeys := make([][]byte, numValidators)
	for i, val := range validators {
		pubkeys[i] = val.pubKey
	}
	valid, err := aggregator.Verify(agg, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify aggregation: %v", err)
	}
	if !valid {
		t.Error("expected valid aggregation")
	}

	// Step 7: Marshal → Unmarshal round-trip (simulates header storage).
	proofBytes, err := MarshalSTARKAggregation(agg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(proofBytes) == 0 {
		t.Fatal("Marshal returned empty bytes")
	}
	agg2, err := UnmarshalSTARKAggregation(proofBytes)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if agg2.NumValidators != agg.NumValidators {
		t.Errorf("NumValidators mismatch after roundtrip: %d vs %d", agg2.NumValidators, agg.NumValidators)
	}
	if agg2.CommitteeRoot != agg.CommitteeRoot {
		t.Error("CommitteeRoot mismatch after roundtrip")
	}
	if agg2.VoteDataHash != agg.VoteDataHash {
		t.Error("VoteDataHash mismatch after roundtrip")
	}

	// Step 8: Verify the unmarshaled aggregation.
	valid, err = aggregator.Verify(agg2, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify after unmarshal: %v", err)
	}
	if !valid {
		t.Error("expected valid after unmarshal")
	}

	// Step 9: PQVoteAttestation RLP encode/decode round-trip.
	attestation := &types.PQVoteAttestation{
		VoteAddressSet: types.ValidatorsBitSet((1 << numValidators) - 1), // all bits set
		AggProof:       proofBytes,
		Data:           voteData,
	}
	encoded, err := rlp.EncodeToBytes(attestation)
	if err != nil {
		t.Fatalf("RLP encode PQVoteAttestation: %v", err)
	}
	var decoded types.PQVoteAttestation
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("RLP decode PQVoteAttestation: %v", err)
	}
	if decoded.Data.TargetNumber != voteData.TargetNumber {
		t.Error("RLP roundtrip: TargetNumber mismatch")
	}
	if decoded.Data.SourceHash != voteData.SourceHash {
		t.Error("RLP roundtrip: SourceHash mismatch")
	}
	if uint64(decoded.VoteAddressSet) != uint64(attestation.VoteAddressSet) {
		t.Error("RLP roundtrip: VoteAddressSet mismatch")
	}

	// Verify decoded proof still works.
	agg3, err := UnmarshalSTARKAggregation(decoded.AggProof)
	if err != nil {
		t.Fatalf("Unmarshal from decoded attestation: %v", err)
	}
	valid, err = aggregator.Verify(agg3, pubkeys, voteDataHash)
	if err != nil {
		t.Fatalf("Verify from decoded attestation: %v", err)
	}
	if !valid {
		t.Error("expected valid from decoded attestation")
	}
}

// TestPQE2E_NegativeCases tests that verification correctly rejects invalid inputs.
func TestPQE2E_NegativeCases(t *testing.T) {
	const numValidators = 15

	// Setup: generate keys, sign votes, aggregate.
	pqVotes := make([]PQVoteData, numValidators)
	pubkeys := make([][]byte, numValidators)
	voteData := &types.VoteData{
		SourceNumber: 50,
		SourceHash:   common.BytesToHash([]byte("neg-source-hash-padding-1234567")),
		TargetNumber: 51,
		TargetHash:   common.BytesToHash([]byte("neg-target-hash-padding-1234567")),
	}
	voteDataHash := voteData.Hash()

	for i := 0; i < numValidators; i++ {
		pub, priv, err := mldsa.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey[%d]: %v", i, err)
		}
		env := &types.PQVoteEnvelope{Data: voteData}
		if err := signPQVote(env, priv, pub); err != nil {
			t.Fatalf("SignVote[%d]: %v", i, err)
		}
		pqVotes[i] = PQVoteData{
			TargetNumber:   env.Data.TargetNumber,
			TargetHash:     env.Data.TargetHash,
			SourceNumber:   env.Data.SourceNumber,
			SourceHash:     env.Data.SourceHash,
			PQSignature:    env.Signature[:],
			PQPublicKey:    env.VoteAddress[:],
			ValidatorIndex: i,
		}
		pubkeys[i] = pub
	}

	aggregator := NewSTARKSignatureAggregator()
	agg, err := aggregator.Aggregate(pqVotes, voteDataHash)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	t.Run("WrongPubkeys", func(t *testing.T) {
		wrongPubkeys := make([][]byte, numValidators)
		for i := 0; i < numValidators; i++ {
			pub, _, err := mldsa.GenerateKey()
			if err != nil {
				t.Fatalf("GenerateKey: %v", err)
			}
			wrongPubkeys[i] = pub
		}
		valid, err := aggregator.Verify(agg, wrongPubkeys, voteDataHash)
		if err == nil || valid {
			t.Error("expected failure with wrong pubkeys")
		}
	})

	t.Run("WrongVoteDataHash", func(t *testing.T) {
		wrongHash := common.BytesToHash([]byte("wrong-vote-data-hash-1234567890a"))
		valid, err := aggregator.Verify(agg, pubkeys, wrongHash)
		if err == nil || valid {
			t.Error("expected failure with wrong vote data hash")
		}
	})

	t.Run("NilAggregation", func(t *testing.T) {
		valid, err := aggregator.Verify(nil, pubkeys, voteDataHash)
		if err != ErrSTARKAggNilResult {
			t.Errorf("expected ErrSTARKAggNilResult, got %v", err)
		}
		if valid {
			t.Error("expected invalid")
		}
	})

	t.Run("EmptyVotes", func(t *testing.T) {
		_, err := aggregator.Aggregate(nil, voteDataHash)
		if err != ErrSTARKAggNoSigs {
			t.Errorf("expected ErrSTARKAggNoSigs, got %v", err)
		}
	})

	t.Run("TamperedProof", func(t *testing.T) {
		proofBytes, err := MarshalSTARKAggregation(agg)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		// Tamper with the commitment root area (bytes 68-100).
		tampered := make([]byte, len(proofBytes))
		copy(tampered, proofBytes)
		for i := 68; i < 100 && i < len(tampered); i++ {
			tampered[i] ^= 0xFF
		}
		agg2, err := UnmarshalSTARKAggregation(tampered)
		if err != nil {
			// Tamper may cause unmarshal failure — acceptable.
			return
		}
		valid, err := aggregator.Verify(agg2, pubkeys, voteDataHash)
		if valid && err == nil {
			t.Error("expected failure with tampered proof")
		}
	})

	t.Run("TruncatedProof", func(t *testing.T) {
		_, err := UnmarshalSTARKAggregation([]byte{0x01, 0x02, 0x03})
		if err != ErrSTARKAggInvalidProof {
			t.Errorf("expected ErrSTARKAggInvalidProof for truncated data, got %v", err)
		}
	})
}

// TestPQE2E_CommitteeRootDeterminism verifies that committee root is deterministic
// for the same input order, and order-sensitive (validates the C4 fix).
func TestPQE2E_CommitteeRootDeterminism(t *testing.T) {
	const n = 10
	pubkeys := make([][]byte, n)
	for i := 0; i < n; i++ {
		pub, _, err := mldsa.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey[%d]: %v", i, err)
		}
		pubkeys[i] = pub
	}

	root1 := computeCommitteeRoot(pubkeys)
	root2 := computeCommitteeRoot(pubkeys)
	if root1 != root2 {
		t.Error("committee root is not deterministic for same input order")
	}

	// Reversed order should produce a different root.
	reversed := make([][]byte, n)
	for i := range pubkeys {
		reversed[n-1-i] = pubkeys[i]
	}
	rootReversed := computeCommitteeRoot(reversed)
	if root1 == rootReversed {
		t.Error("committee root should differ for different pubkey orders")
	}
}

// TestPQE2E_IndividualSignatureVerification tests ML-DSA-44 sign/verify
// and that tampered signatures are rejected.
func TestPQE2E_IndividualSignatureVerification(t *testing.T) {
	pub, priv, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Verify pubkey derivation round-trip.
	derivedPub, err := mldsa.PublicKeyFromPrivate(priv)
	if err != nil {
		t.Fatalf("PublicKeyFromPrivate: %v", err)
	}
	if !bytesEqual(pub, derivedPub) {
		t.Fatal("public key derivation mismatch")
	}

	voteData := &types.VoteData{
		SourceNumber: 1,
		SourceHash:   common.BytesToHash([]byte("sig-test-source-hash-padding123")),
		TargetNumber: 2,
		TargetHash:   common.BytesToHash([]byte("sig-test-target-hash-padding123")),
	}

	env := &types.PQVoteEnvelope{Data: voteData}
	if err := signPQVote(env, priv, pub); err != nil {
		t.Fatalf("SignVote: %v", err)
	}

	// Valid signature should verify.
	if err := env.Verify(); err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}

	// Tampered signature should fail.
	env.Signature[0] ^= 0xFF
	if err := env.Verify(); err == nil {
		t.Error("expected tampered signature to fail verification")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
