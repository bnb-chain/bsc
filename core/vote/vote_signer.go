// Copyright 2018 The go-ethereum Authors
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

	voteHash := vote.Hash()

	signature, err := (*signer.km).Sign(ctx, &validatorpb.SignRequest{
		PublicKey:   pubKey[:],
		SigningRoot: voteHash[:],
	})
	if err != nil {
		log.Error("Failed to sign vote", "err", err)
		return err
	}

	vote.VoteAddress = pubKey
	copy(vote.Signature[:], signature.Marshal()[:])
	return nil
}

// VerifyVoteWithBLS using BLS.
func VerifyVoteWithBLS(vote *types.VoteEnvelope) error {
	blsPubKey, err := bls.PublicKeyFromBytes(vote.VoteAddress[:])
	if err != nil {
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}
	sig, err := bls.SignatureFromBytes(vote.Signature[:])
	if err != nil {
		return errors.Wrap(err, "invalid signature")
	}
	voteHash := vote.Hash()

	if !sig.Verify(blsPubKey, voteHash[:]) {
		return errors.New("verify bls signature failed.")
	}
	return nil
}
