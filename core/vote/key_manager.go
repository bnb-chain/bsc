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
	"github.com/pkg/errors"

	"github.com/prysmaticlabs/prysm/crypto/bls"
	"github.com/prysmaticlabs/prysm/crypto/bls/common"
	validatorpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/validator-client"
	"github.com/prysmaticlabs/prysm/validator/accounts"
	"github.com/prysmaticlabs/prysm/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"
	"github.com/prysmaticlabs/prysm/validator/keymanager"
	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type KeyManager struct {
	km     *keymanager.IKeymanager
	cliCtx *cli.Context
	pubKey types.BLSPublicKey
}

func NewKeyManager(cliCtx *cli.Context) (*KeyManager, error) {
	keyManager := &KeyManager{
		cliCtx: cliCtx,
	}
	w, err := wallet.OpenWalletOrElseCli(cliCtx, func(cliCtx *cli.Context) (*wallet.Wallet, error) {
		return nil, wallet.ErrNoWalletFound
	})

	if err != nil {
		return nil, errors.Wrap(err, "could not initialize wallet")
	}

	if w.KeymanagerKind() != keymanager.Imported {
		return nil, errors.New(
			"remote wallets cannot backup accounts",
		)
	}

	km, err := w.InitializeKeymanager(cliCtx.Context, iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		return nil, errors.Wrap(err, accounts.ErrCouldNotInitializeKeymanager)
	}
	keyManager.km = &km

	pubKeys, err := km.FetchValidatingPublicKeys(cliCtx.Context)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch validating public keys")
	}

	pubKey := pubKeys[0]

	keyManager.pubKey = pubKey

	return keyManager, nil
}

func (kmg *KeyManager) SignVote(vote *types.VoteEnvelope) error {
	hash := vote.CalcVoteHash()
	// Sign the vote

	signature, err := (*kmg.km).Sign(kmg.cliCtx.Context, &validatorpb.SignRequest{
		PublicKey:   kmg.pubKey[:],
		SigningRoot: hash[:],
	})
	if err != nil {
		log.Error("Failed to sign vote", "err", err)
	}
	vote.Signature = signature
	return nil
}

func (kmg *KeyManager) VerifyVote(vote *types.VoteEnvelope) error {
	blsPubKey, err := bls.PublicKeyFromBytes(kmg.pubKey[:])
	if err != nil {
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}

	sig := vote.Signature.(common.Signature)
	hash := vote.CalcVoteHash()

	if !sig.Verify(blsPubKey, hash[:]) {
		return errors.New("verify bls signature failed.")
	}
	return nil
}
