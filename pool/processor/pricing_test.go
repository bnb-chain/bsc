package processor

import (
	"math/big"
	"testing"
)

func TestComputeV2PriceHandlesDecimalDifferences(t *testing.T) {
	reserve0 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 BNB in wei
	reserve1 := big.NewInt(850120000)                                 // 850.12 USDT with 6 decimals

	price := computeV2Price(reserve0, reserve1, 18, 6)
	if price == nil {
		t.Fatalf("expected price, got nil")
	}

	expected := big.NewRat(85012, 100) // 850.12
	if price.Cmp(expected) != 0 {
		t.Fatalf("expected %s, got %s", expected.FloatString(2), price.FloatString(2))
	}
}

func TestComputeV3PriceHandlesDecimalDifferences(t *testing.T) {
	sqrtPrice := new(big.Int).Lsh(big.NewInt(1), 96) // sqrtPrice representing base price 1

	price := computeV3Price(sqrtPrice, 18, 6)
	if price == nil {
		t.Fatalf("expected price, got nil")
	}

	expected := new(big.Rat).SetFrac(pow10(12), big.NewInt(1))
	if price.Cmp(expected) != 0 {
		t.Fatalf("expected %s, got %s", expected.FloatString(0), price.FloatString(0))
	}
}
