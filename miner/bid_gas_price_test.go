// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package miner

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestValidateBidBlockGasPriceTooLow(t *testing.T) {
	txs := types.Transactions{
		types.NewTransaction(0, common.Address{0x1}, big.NewInt(0), 21_000, big.NewInt(1), nil),
	}
	receipts := types.Receipts{{GasUsed: 21_000}}

	avg, gasUsed, err := validateBidBlockGasPrice(txs, receipts, 1, nil, big.NewInt(2))
	if !errors.Is(err, errBidBlockGasPriceTooLow) {
		t.Fatalf("expected low gas price error, got %v", err)
	}
	if avg.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("avg gas price: got %v, want 1", avg)
	}
	if gasUsed != 21_000 {
		t.Fatalf("gas used: got %d, want 21000", gasUsed)
	}
}

func TestValidateBidBlockGasPriceExcludesSystemTxs(t *testing.T) {
	txs := types.Transactions{
		types.NewTransaction(0, common.Address{0x1}, big.NewInt(0), 21_000, big.NewInt(1), nil),
		types.NewTransaction(1, common.Address{0x2}, big.NewInt(0), 21_000, big.NewInt(10), nil),
		types.NewTransaction(2, common.Address{0x3}, big.NewInt(0), 21_000, big.NewInt(100), nil),
	}
	receipts := types.Receipts{
		{GasUsed: 21_000},
		{GasUsed: 21_000},
		{GasUsed: 21_000},
	}

	avg, gasUsed, err := validateBidBlockGasPrice(txs, receipts, 2, nil, big.NewInt(5))
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
