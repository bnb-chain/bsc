// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
)

var errBidBlockAverageGasPriceTooLow = errors.New("BidBlock average gas price too low")

func validateBidBlockAverageGasPrice(
	gasFee *big.Int,
	receipts types.Receipts,
	systemTxStart int,
	minGasPrice *big.Int,
) (*big.Int, uint64, error) {
	gasUsed := calcNonSystemGasUsed(receipts[:systemTxStart])
	if gasUsed == 0 {
		return nil, 0, nil
	}
	avgGasPrice := new(big.Int).Div(new(big.Int).Set(gasFee), new(big.Int).SetUint64(gasUsed))
	if avgGasPrice.Cmp(minGasPrice) < 0 {
		return avgGasPrice, gasUsed, fmt.Errorf("%w, avg:%v, min:%v", errBidBlockAverageGasPriceTooLow, avgGasPrice, minGasPrice)
	}
	return avgGasPrice, gasUsed, nil
}

func calcNonSystemGasUsed(
	receipts types.Receipts,
) uint64 {
	gasUsed := uint64(0)
	for _, receipt := range receipts {
		gasUsed += receipt.GasUsed
	}
	return gasUsed
}
