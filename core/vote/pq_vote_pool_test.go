package vote

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
)

// newSignedPQVote creates a valid ML-DSA-44 signed PQ vote for testing.
func newSignedPQVote(t *testing.T, target uint64, targetHash common.Hash) (*types.PQVoteEnvelope, []byte) {
	t.Helper()
	pub, priv, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewPQVoteSignerFromRawKey(priv)
	if err != nil {
		t.Fatalf("NewPQVoteSignerFromRawKey: %v", err)
	}
	env := &types.PQVoteEnvelope{
		Data: &types.VoteData{
			SourceNumber: target - 1,
			SourceHash:   common.BytesToHash([]byte("source-hash-padding-0123456789ab")),
			TargetNumber: target,
			TargetHash:   targetHash,
		},
	}
	if err := signer.SignVote(env); err != nil {
		t.Fatalf("SignVote: %v", err)
	}
	return env, pub
}

func TestPQVotePool_PutAndFetch(t *testing.T) {
	pool := NewPQVotePool(nil) // nil chain → skip head-range check
	defer pool.Stop()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	env, _ := newSignedPQVote(t, 100, targetHash)

	pool.putIntoVotePool(env)

	got := pool.FetchVotesByBlockHash(targetHash, 99)
	if len(got) != 1 {
		t.Fatalf("expected 1 vote, got %d", len(got))
	}
	if got[0].Hash() != env.Hash() {
		t.Error("vote hash mismatch")
	}
}

func TestPQVotePool_Dedup(t *testing.T) {
	pool := NewPQVotePool(nil)
	defer pool.Stop()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	env, _ := newSignedPQVote(t, 100, targetHash)

	if !pool.putIntoVotePool(env) {
		t.Fatal("first put should succeed")
	}
	if pool.putIntoVotePool(env) {
		t.Error("duplicate put should be rejected")
	}
	if n := len(pool.GetVotes()); n != 1 {
		t.Errorf("expected 1 vote after dedup, got %d", n)
	}
}

func TestPQVotePool_RejectBadSignature(t *testing.T) {
	pool := NewPQVotePool(nil)
	defer pool.Stop()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	env, _ := newSignedPQVote(t, 100, targetHash)
	env.Signature[0] ^= 0xFF // tamper

	if pool.putIntoVotePool(env) {
		t.Error("tampered vote should be rejected")
	}
	if n := len(pool.GetVotes()); n != 0 {
		t.Errorf("expected 0 votes, got %d", n)
	}
}

func TestPQVotePool_BoxCap(t *testing.T) {
	pool := NewPQVotePool(nil)
	defer pool.Stop()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	// Fill past cap — each call produces a distinct keypair/signature so no dedup.
	accepted := 0
	for i := 0; i < maxPQCurVotesPerBlock+5; i++ {
		env, _ := newSignedPQVote(t, 100, targetHash)
		if pool.putIntoVotePool(env) {
			accepted++
		}
	}
	if accepted != maxPQCurVotesPerBlock {
		t.Errorf("expected %d accepted, got %d", maxPQCurVotesPerBlock, accepted)
	}
}

func TestPQVotePool_SubscribeEvent(t *testing.T) {
	pool := NewPQVotePool(nil)
	defer pool.Stop()

	ch := make(chan core.NewPQVoteEvent, 1)
	sub := pool.SubscribeNewPQVoteEvent(ch)
	defer sub.Unsubscribe()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	env, _ := newSignedPQVote(t, 100, targetHash)
	if !pool.putIntoVotePool(env) {
		t.Fatal("put failed")
	}
	select {
	case ev := <-ch:
		if ev.Vote.Hash() != env.Hash() {
			t.Error("event vote hash mismatch")
		}
	default:
		t.Error("expected NewPQVoteEvent to be delivered")
	}
}

func TestPQVotePool_Prune(t *testing.T) {
	pool := NewPQVotePool(nil)
	defer pool.Stop()

	targetHash := common.BytesToHash([]byte("target-hash-abcdef0123456789abcd"))
	env, _ := newSignedPQVote(t, 10, targetHash)
	pool.putIntoVotePool(env)
	if n := len(pool.GetVotes()); n != 1 {
		t.Fatalf("pre-prune: expected 1 vote, got %d", n)
	}

	// latestBlockNumber well past vote target+lowerLimit.
	pool.prune(10 + lowerLimitOfVoteBlockNumber + 5)
	if n := len(pool.GetVotes()); n != 0 {
		t.Errorf("post-prune: expected 0 votes, got %d", n)
	}
}
