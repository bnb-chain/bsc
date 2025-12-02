package processor

import (
	"math/big"
	"sync"
)

func computeV2Price(reserve0, reserve1 *big.Int, dec0, dec1 int) *big.Rat {
	if reserve0 == nil || reserve1 == nil || reserve0.Sign() == 0 || reserve1.Sign() == 0 {
		return nil
	}
	num := new(big.Int).Set(reserve1)
	den := new(big.Int).Set(reserve0)

	adjustDecimals(num, den, dec0-dec1)

	return new(big.Rat).SetFrac(num, den)
}

func computeV3Price(sqrtPriceX96 *big.Int, dec0, dec1 int) *big.Rat {
	if sqrtPriceX96 == nil || sqrtPriceX96.Sign() == 0 {
		return nil
	}
	num := new(big.Int).Mul(sqrtPriceX96, sqrtPriceX96)
	den := new(big.Int).Lsh(big.NewInt(1), 192) // 2^192

	adjustDecimals(num, den, dec0-dec1)

	return new(big.Rat).SetFrac(num, den)
}

func adjustDecimals(num, den *big.Int, exp int) {
	if exp == 0 {
		return
	}
	if exp > 0 {
		num.Mul(num, pow10(exp))
	} else {
		den.Mul(den, pow10(-exp))
	}
}

var (
	pow10Mu    sync.Mutex
	pow10Cache = map[int]*big.Int{
		0: big.NewInt(1),
	}
)

func pow10(exp int) *big.Int {
	pow10Mu.Lock()
	defer pow10Mu.Unlock()
	if val, ok := pow10Cache[exp]; ok {
		return val
	}
	base := big.NewInt(10)
	result := big.NewInt(1)
	for i := 0; i < exp; i++ {
		result.Mul(result, base)
	}
	pow10Cache[exp] = result
	return result
}
