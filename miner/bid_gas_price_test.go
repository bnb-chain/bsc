// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
)

func TestValidateBidBlockAverageGasPriceTooLow(t *testing.T) {
	receipts := types.Receipts{{GasUsed: 21_000}}
	gasFee := big.NewInt(21_000)

	avg, gasUsed, err := validateBidBlockAverageGasPrice(gasFee, receipts, 1, big.NewInt(2))
	if !errors.Is(err, errBidBlockAverageGasPriceTooLow) {
		t.Fatalf("expected low gas price error, got %v", err)
	}
	if avg.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("avg gas price: got %v, want 1", avg)
	}
	if gasUsed != 21_000 {
		t.Fatalf("gas used: got %d, want 21000", gasUsed)
	}
}

func TestValidateBidBlockAverageGasPriceExcludesSystemTxs(t *testing.T) {
	receipts := types.Receipts{
		{GasUsed: 21_000},
		{GasUsed: 21_000},
		{GasUsed: 21_000},
	}
	gasFee := big.NewInt(5 * 42_000)

	avg, gasUsed, err := validateBidBlockAverageGasPrice(gasFee, receipts, 2, big.NewInt(5))
	if err != nil {
		t.Fatalf("check gas price failed: %v", err)
	}
	if avg.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("avg gas price: got %v, want 5", avg)
	}
	if gasUsed != 42_000 {
		t.Fatalf("gas used: got %d, want 42000", gasUsed)
	}
}
