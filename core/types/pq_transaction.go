package types

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const PQTxType = 0x05

// PQTxData represents an ML-DSA-44 signed typed transaction.
type PQTxData struct {
	ChainID     *big.Int
	Nonce       uint64
	GasPrice    *big.Int
	Gas         uint64
	To          *common.Address `rlp:"nil"`
	Value       *big.Int
	Data        []byte
	From        common.Address
	PQSignature []byte
}

// PQFrom returns the embedded sender address and true if tx is a PQ
// transaction. Unlike Sender(), this does NOT verify the signature.
// Use only when the signature will be verified separately (e.g. during
// block state processing after the block has already been seal-verified).
func PQFrom(tx *Transaction) (common.Address, bool) {
	pqtx, ok := tx.inner.(*PQTxData)
	if !ok {
		return common.Address{}, false
	}
	return pqtx.From, true
}

func (tx *PQTxData) copy() TxData {
	cpy := &PQTxData{
		Nonce:       tx.Nonce,
		To:          copyAddressPtr(tx.To),
		Data:        common.CopyBytes(tx.Data),
		Gas:         tx.Gas,
		From:        tx.From,
		PQSignature: common.CopyBytes(tx.PQSignature),
		ChainID:     new(big.Int),
		GasPrice:    new(big.Int),
		Value:       new(big.Int),
	}
	if tx.ChainID != nil {
		cpy.ChainID.Set(tx.ChainID)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice.Set(tx.GasPrice)
	}
	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}
	return cpy
}

func (tx *PQTxData) txType() byte           { return PQTxType }
func (tx *PQTxData) chainID() *big.Int      { return tx.ChainID }
func (tx *PQTxData) accessList() AccessList { return nil }
func (tx *PQTxData) data() []byte           { return tx.Data }
func (tx *PQTxData) gas() uint64            { return tx.Gas }
func (tx *PQTxData) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *PQTxData) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *PQTxData) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *PQTxData) value() *big.Int        { return tx.Value }
func (tx *PQTxData) nonce() uint64          { return tx.Nonce }
func (tx *PQTxData) to() *common.Address    { return tx.To }

func (tx *PQTxData) rawSignatureValues() (v, r, s *big.Int) {
	return new(big.Int), new(big.Int), new(big.Int)
}

func (tx *PQTxData) setSignatureValues(chainID, v, r, s *big.Int) {
	if chainID == nil {
		tx.ChainID = nil
		return
	}
	if tx.ChainID == nil {
		tx.ChainID = new(big.Int)
	}
	tx.ChainID.Set(chainID)
}

func (tx *PQTxData) effectiveGasPrice(dst *big.Int, baseFee *big.Int) *big.Int {
	return dst.Set(tx.GasPrice)
}

func (tx *PQTxData) encode(b *bytes.Buffer) error {
	return rlp.Encode(b, tx)
}

func (tx *PQTxData) decode(input []byte) error {
	return rlp.DecodeBytes(input, tx)
}

func (tx *PQTxData) sigHash(chainID *big.Int) common.Hash {
	if chainID == nil {
		chainID = tx.ChainID
	}
	return rlpHash([]any{
		chainID,
		tx.Nonce,
		tx.GasPrice,
		tx.Gas,
		tx.To,
		tx.Value,
		tx.Data,
	})
}
