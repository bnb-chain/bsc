// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package parlia

import (
	"math/big"
	"testing"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/params"
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
			decoded := &buildertypes.DecodedBidBlock{Header: header, Txs: tc.txs}
			err := p.VerifyBidBlockSystemTxs(decoded, parent, tc.sysStart)
			if (err != nil) != tc.wantErr {
				t.Fatalf("VerifyBidBlockSystemTxs err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// typedSysTx builds a deposit-shaped system tx in a typed envelope, with every
// gas price field zeroed so only the tx type distinguishes it from sysTx.
func typedSysTx(txType byte, selector []byte) *types.Transaction {
	to := common.HexToAddress(systemcontracts.ValidatorContract)
	zero := uint256.NewInt(0)
	switch txType {
	case types.AccessListTxType:
		return types.NewTx(&types.AccessListTx{
			ChainID: big.NewInt(1), GasPrice: big.NewInt(0), Gas: 100000,
			To: &to, Value: big.NewInt(5), Data: selector,
		})
	case types.DynamicFeeTxType:
		return types.NewTx(&types.DynamicFeeTx{
			ChainID: big.NewInt(1), GasTipCap: big.NewInt(0), GasFeeCap: big.NewInt(0),
			Gas: 100000, To: &to, Value: big.NewInt(5), Data: selector,
		})
	case types.BlobTxType:
		return types.NewTx(&types.BlobTx{
			ChainID: uint256.NewInt(1), GasTipCap: zero, GasFeeCap: zero, Gas: 100000,
			To: to, Value: uint256.NewInt(5), Data: selector,
			BlobFeeCap: uint256.NewInt(params.GWei), BlobHashes: []common.Hash{{0x01}},
		})
	case types.SetCodeTxType:
		return types.NewTx(&types.SetCodeTx{
			ChainID: uint256.NewInt(1), GasTipCap: zero, GasFeeCap: zero, Gas: 100000,
			To: to, Value: uint256.NewInt(5), Data: selector,
			AuthList: []types.SetCodeAuthorization{{}},
		})
	default:
		panic("unsupported tx type")
	}
}

// Only legacy system txs may be blind-signed: a typed envelope would let a
// builder pick fields the shape check does not bind.
func TestBidBlockSystemTxRejectsTypedTx(t *testing.T) {
	p := &Parlia{}
	depositSel := signableSystemTxSelectors["deposit"]

	for _, tc := range []struct {
		name   string
		txType byte
	}{
		{"blob", types.BlobTxType},
		{"dynamic fee", types.DynamicFeeTxType},
		{"access list", types.AccessListTxType},
		{"set code", types.SetCodeTxType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typed := typedSysTx(tc.txType, depositSel[:])
			if p.isUnsignedSystemTxCandidate(typed) {
				t.Fatal("typed tx must not be an unsigned system-tx candidate")
			}
		})
	}

	// A deposit-shaped BlobTx must not define the system-tx region or GasFee.
	// Admission rejects the BidBlock before the validator can blind-sign it.
	txs := types.Transactions{userTx(), typedSysTx(types.BlobTxType, depositSel[:])}
	start, fee := p.ExtractBidBlockDepositValue(txs)
	if start != len(txs) || fee.Sign() != 0 {
		t.Fatalf("got (start=%d, fee=%v), want (%d, 0)", start, fee, len(txs))
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
