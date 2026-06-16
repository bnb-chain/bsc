// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package parlia

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
)

// sysTx builds an unsigned system-tx candidate: to=ValidatorContract, gasPrice=0,
// zero signature, data prefixed with the given selector.
func sysTx(selector []byte, value *big.Int) *types.Transaction {
	to := common.HexToAddress(systemcontracts.ValidatorContract)
	return types.NewTx(&types.LegacyTx{
		GasPrice: big.NewInt(0),
		Gas:      100000,
		To:       &to,
		Value:    value,
		Data:     selector,
	})
}

// userTx is a normal user tx (nonzero gasPrice, non-system recipient): never a
// system-tx candidate, so it stops the trailing-region scan.
func userTx() *types.Transaction {
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	return types.NewTx(&types.LegacyTx{GasPrice: big.NewInt(1), Gas: 21000, To: &to, Value: big.NewInt(0)})
}

func TestVerifyBidBlockSystemTxs(t *testing.T) {
	p := &Parlia{}
	// Number 100 (not a multiple of finalityRewardInterval=200) + same-UTC-day
	// timestamps (not a breathe block) => expected shape is exactly [deposit].
	header := &types.Header{Number: big.NewInt(100), Time: 1003}
	parent := &types.Header{Number: big.NewInt(99), Time: 1000}

	depositSel := signableSystemTxSelectors["deposit"]
	finalitySel := signableSystemTxSelectors["distributeFinalityReward"]
	deposit := func(v int64) *types.Transaction { return sysTx(depositSel[:], big.NewInt(v)) }

	tests := []struct {
		name     string
		txs      types.Transactions
		sysStart int
		wantErr  bool
	}{
		{"valid [deposit]", types.Transactions{userTx(), deposit(5)}, 1, false},
		{"non-whitelist selector", types.Transactions{userTx(), sysTx([]byte{0xde, 0xad, 0xbe, 0xef}, big.NewInt(5))}, 1, true},
		{"wrong order (finalityReward where deposit expected)", types.Transactions{userTx(), sysTx(finalitySel[:], big.NewInt(0))}, 1, true},
		{"missing deposit (empty trailing region)", types.Transactions{userTx()}, 1, true},
		{"extra system tx", types.Transactions{userTx(), deposit(5), deposit(6)}, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded := &types.DecodedBidBlock{Header: header, Txs: tc.txs}
			err := p.VerifyBidBlockSystemTxs(decoded, parent, tc.sysStart)
			if (err != nil) != tc.wantErr {
				t.Fatalf("VerifyBidBlockSystemTxs err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestExtractBidBlockDepositValue(t *testing.T) {
	p := &Parlia{}
	depositSel := signableSystemTxSelectors["deposit"]

	// systemTxStart > 0: deposit value is returned as GasFee.
	start, fee := p.ExtractBidBlockDepositValue(types.Transactions{userTx(), sysTx(depositSel[:], big.NewInt(7))})
	if start != 1 || fee.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("with user tx: got (start=%d, fee=%v), want (1, 7)", start, fee)
	}

	// systemTxStart == 0 (no preceding user tx): GasFee must be zero so admission rejects it.
	start, fee = p.ExtractBidBlockDepositValue(types.Transactions{sysTx(depositSel[:], big.NewInt(7))})
	if start != 0 || fee.Sign() != 0 {
		t.Fatalf("no user tx: got (start=%d, fee=%v), want (0, 0)", start, fee)
	}
}
