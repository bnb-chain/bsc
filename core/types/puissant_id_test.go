package types_test

import (
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"testing"
)

var (
	tx1 = types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(11111),
		Gas:      1111,
		Value:    big.NewInt(111),
		Data:     []byte{0x11, 0x11, 0x11},
	})
	tx2 = types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(11111),
		Gas:      1111111,
		Value:    big.NewInt(111),
		Data:     []byte{0x11, 0x11, 0x11},
	})
)

func TestGenPuissantID(t *testing.T) {
	if types.GenPuissantID([]*types.Transaction{tx1, tx2}).Hex() != "0x07302bbf3e4e309d96f3d71a07830400" {
		t.Fatal("pid generate error")
	}
}

func BenchmarkGenPuissantID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = types.GenPuissantID([]*types.Transaction{tx1, tx2})
	}
}
