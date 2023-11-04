/*
	Copyright 2023 48Club
	This file is part of the puissant-bsc-validator library and is intended for the implementation of puissant services.
	Parts of the code in this file are derived from the go-ethereum library.
	No one is authorized to copy, modify, or publish this file in any form without permission from 48Club.
	Any unauthorized use of this file constitutes an infringement of copyright.
*/

package ethapi

import (
	"context"
	"errors"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// PuissantAPI offers an API for accepting bundled transactions
type PuissantAPI struct {
	b Backend
}

// NewPuissantAPI creates a new Tx Bundle API instance.
func NewPuissantAPI(b Backend) *PuissantAPI {
	return &PuissantAPI{b: b}
}

type SendPuissantArgs struct {
	Txs            []hexutil.Bytes `json:"txs"`
	MaxTimestamp   uint64          `json:"maxTimestamp"`
	Revertible     []common.Hash   `json:"revertible"`
	RelaySignature hexutil.Bytes   `json:"relaySignature"`
}

// SendPuissant should only be called from PUISSANT-API
func (s *PuissantAPI) SendPuissant(ctx context.Context, args SendPuissantArgs) error {
	// Check if the transaction is valid.
	// count of txs must be greater than 0
	// if count = 1, len(revertible) can be empty or 1 (for private transaction)
	// if count > 1, len(revertible) should less than txCount (for puissant-package)
	if txCount := len(args.Txs); txCount == 0 {
		return errors.New("invalid")
	} else if txCount > 1 && len(args.Revertible) >= txCount {
		return errors.New("invalid revertible hash size")
	}

	var (
		txs           types.Transactions                         // final puissant-package transactions
		tmpGasPrice   *big.Int                                   // tmp gas price for txs-sort-check, txs must be sorted by gas price descending
		txHash        = mapset.NewThreadUnsafeSet[common.Hash]() // tx hash set for duplicate check
		revertibleSet = mapset.NewThreadUnsafeSet[common.Hash]() // revertible tx hash set for marking revertible
	)
	for _, each := range args.Revertible {
		revertibleSet.Add(each)
	}

	for index, encodedTx := range args.Txs {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(encodedTx); err != nil {
			return err
		}
		if !s.b.UnprotectedAllowed() && !tx.Protected() {
			// Ensure only eip155 signed transactions are submitted if EIP155Required is set.
			return errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
		}

		// txs-sort-check
		if txGP := tx.GasPrice(); tmpGasPrice == nil || tmpGasPrice.Cmp(txGP) >= 0 {
			tmpGasPrice = txGP
		} else {
			return errors.New("invalid, require txs descending sort by gas price")
		}
		txHash.Add(tx.Hash())

		// mark tx seq and revertible for puissant-package
		tx.SetPuissantTxSeq(index)
		if revertibleSet.Contains(tx.Hash()) {
			tx.SetPuissantAcceptReverting()
		}
		txs = append(txs, tx)
	}
	// check duplicate transaction in txs
	if txHash.Cardinality() != len(txs) {
		return errors.New("duplicate transaction found")
	}

	pid := types.GenPuissantID(txs)
	for _, tx := range txs {
		tx.SetPuissantID(pid)
	}
	return s.b.SendPuissant(ctx, pid, txs, args.MaxTimestamp, args.RelaySignature)
}
