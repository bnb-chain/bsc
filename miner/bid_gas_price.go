// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
)

var errBidTxGasPriceTooLow = errors.New("average bid tx gas price too low")

func validateBidTxGasPrice(
	txs types.Transactions,
	receipts types.Receipts,
	txIndexes []int,
	baseFee *big.Int,
	minGasPrice *big.Int,
) (*big.Int, uint64, error) {
	avgGasPrice, gasUsed := calcAverageBidTxGasPrice(txs, receipts, txIndexes, baseFee)
	if gasUsed == 0 {
		return avgGasPrice, gasUsed, nil
	}
	if avgGasPrice.Cmp(minGasPrice) < 0 {
		return avgGasPrice, gasUsed, fmt.Errorf("%w, bid:%v, min:%v", errBidTxGasPriceTooLow, avgGasPrice, minGasPrice)
	}
	return avgGasPrice, gasUsed, nil
}

func calcAverageBidTxGasPrice(
	txs types.Transactions,
	receipts types.Receipts,
	txIndexes []int,
	baseFee *big.Int,
) (*big.Int, uint64) {
	gasUsed := uint64(0)
	gasFee := new(big.Int)

	for _, txIndex := range txIndexes {
		tx := txs[txIndex]
		receipt := receipts[txIndex]

		gasUsed += receipt.GasUsed
		effectiveGasPrice := tx.EffectiveGasTipValue(baseFee)
		if baseFee != nil {
			effectiveGasPrice.Add(effectiveGasPrice, baseFee)
		}

		txGasFee := new(big.Int).Mul(effectiveGasPrice, new(big.Int).SetUint64(receipt.GasUsed))
		gasFee.Add(gasFee, txGasFee)

		if tx.Type() == types.BlobTxType && receipt.BlobGasUsed != 0 {
			blobFee := new(big.Int).Mul(receipt.BlobGasPrice, new(big.Int).SetUint64(receipt.BlobGasUsed))
			gasFee.Add(gasFee, blobFee)
		}
	}
	if gasUsed == 0 {
		return nil, 0
	}
	return gasFee.Div(gasFee, new(big.Int).SetUint64(gasUsed)), gasUsed
}
