package vote

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/prysmaticlabs/prysm/crypto/bls"
	validatorpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/validator-client"
	"github.com/prysmaticlabs/prysm/validator/keymanager"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	voteSignerTimeout = time.Second * 5
)

type VoteSigner struct {
	km *keymanager.IKeymanager
}

func NewVoteSigner(km *keymanager.IKeymanager) (*VoteSigner, error) {
	return &VoteSigner{
		km: km,
	}, nil
}

func (signer *VoteSigner) SignVote(vote *types.VoteEnvelope) error {
	// Sign the vote
	ctx, cancel := context.WithTimeout(context.Background(), voteSignerTimeout)
	defer cancel()

	pubKeys, err := (*signer.km).FetchValidatingPublicKeys(ctx)
	if err != nil {
		return errors.Wrap(err, "could not fetch validating public keys")
	}
	// Fetch the first pubKey as validator's bls public key.
	pubKey := pubKeys[0]
	blsPubKey, err := bls.PublicKeyFromBytes(pubKey[:])
	if err != nil {
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}

	voteDataHash := vote.Data.VoteDataHash()

	signature, err := (*signer.km).Sign(ctx, &validatorpb.SignRequest{
		PublicKey:   pubKey[:],
		SigningRoot: voteDataHash[:],
	})
	if err != nil {
		log.Error("Failed to sign vote", "err", err)
		return err
	}

	copy(vote.VoteAddress[:], blsPubKey.Marshal()[:])
	copy(vote.Signature[:], signature.Marshal()[:])
	return nil
}

// VerifyVoteWithBLS using BLS.
func VerifyVoteWithBLS(vote *types.VoteEnvelope) error {
	blsPubKey, err := bls.PublicKeyFromBytes(vote.VoteAddress[:])
	if err != nil {
		log.Error("Failed to get BLSpubkey from bytes", "err", err, "voteBlockNumber=", vote.Data.BlockNumber, "voteBlockHash=", vote.Data.BlockHash)
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}

	sig, err := bls.SignatureFromBytes(vote.Signature[:])
	if err != nil {
		log.Error("Failed to get BLSsignature from bytes", "err", err, "voteBlockNumber=", vote.Data.BlockNumber, "voteBlockHash=", vote.Data.BlockHash)
		return errors.Wrap(err, "invalid signature")
	}

	voteDataHash := vote.Data.VoteDataHash()
	if !sig.Verify(blsPubKey, voteDataHash[:]) {
		log.Error("Failed to verify bls", "err", err, "voteBlockNumber=", vote.Data.BlockNumber, "voteBlockHash=", vote.Data.BlockHash)
		return errors.New("verify bls signature failed.")
	}
	return nil
}
