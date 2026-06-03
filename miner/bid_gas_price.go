// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
)

var errBidBlockGasPriceTooLow = errors.New("average non-system tx gas price too low")

func validateBidBlockGasPrice(
	txs types.Transactions,
	receipts types.Receipts,
	systemTxStart int,
	baseFee *big.Int,
	minGasPrice *big.Int,
) (*big.Int, uint64, error) {
	avgGasPrice, gasUsed := calcAverageTxGasPrice(txs[:systemTxStart], receipts[:systemTxStart], baseFee)
	if gasUsed == 0 {
		return avgGasPrice, gasUsed, nil
	}
	if avgGasPrice.Cmp(minGasPrice) < 0 {
		return avgGasPrice, gasUsed, fmt.Errorf("%w, avg:%v, min:%v", errBidBlockGasPriceTooLow, avgGasPrice, minGasPrice)
	}
	return avgGasPrice, gasUsed, nil
}

func calcAverageTxGasPrice(
	txs types.Transactions,
	receipts types.Receipts,
	baseFee *big.Int,
) (*big.Int, uint64) {
	gasUsed := uint64(0)
	gasFee := new(big.Int)

	for i, tx := range txs {
		receipt := receipts[i]

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
