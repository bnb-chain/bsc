package vote

import (
	"os"

	"github.com/pkg/errors"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

var pqVotesSigningErrorCounter = metrics.NewRegisteredCounter("pqVotesSigner/error", nil)

// PQVoteSigner signs votes using ML-DSA-44 post-quantum signatures.
type PQVoteSigner struct {
	privKey []byte
	PubKey  types.PQPublicKey
}

// NewPQVoteSigner creates a new PQ vote signer from a private key file.
// The key file should contain the raw ML-DSA-44 private key bytes.
func NewPQVoteSigner(pqKeyPath string) (*PQVoteSigner, error) {
	privKeyBytes, err := os.ReadFile(pqKeyPath)
	if err != nil {
		log.Error("Read PQ vote key file", "err", err)
		return nil, errors.Wrap(err, "failed to read PQ vote key file")
	}

	pubKeyBytes, err := mldsa.PublicKeyFromPrivate(privKeyBytes)
	if err != nil {
		log.Error("Derive PQ public key from private key", "err", err)
		return nil, errors.Wrap(err, "failed to derive PQ public key")
	}

	var pubKey types.PQPublicKey
	copy(pubKey[:], pubKeyBytes)

	log.Info("Created PQ vote signer successfully", "pubKeyLen", len(pubKeyBytes))

	return &PQVoteSigner{
		privKey: privKeyBytes,
		PubKey:  pubKey,
	}, nil
}

// NewPQVoteSignerFromRawKey creates a PQ vote signer from raw private key bytes.
// This is useful for testing and programmatic key generation.
func NewPQVoteSignerFromRawKey(privKey []byte) (*PQVoteSigner, error) {
	pubKeyBytes, err := mldsa.PublicKeyFromPrivate(privKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive PQ public key")
	}

	var pubKey types.PQPublicKey
	copy(pubKey[:], pubKeyBytes)

	return &PQVoteSigner{
		privKey: privKey,
		PubKey:  pubKey,
	}, nil
}

// SignVote signs a PQ vote envelope using ML-DSA-44.
func (signer *PQVoteSigner) SignVote(vote *types.PQVoteEnvelope) error {
	voteDataHash := vote.Data.Hash()

	sig, err := mldsa.Sign(signer.privKey, voteDataHash[:])
	if err != nil {
		pqVotesSigningErrorCounter.Inc(1)
		return errors.Wrap(err, "failed to sign vote with ML-DSA-44")
	}

	copy(vote.VoteAddress[:], signer.PubKey[:])
	copy(vote.Signature[:], sig)
	return nil
}
