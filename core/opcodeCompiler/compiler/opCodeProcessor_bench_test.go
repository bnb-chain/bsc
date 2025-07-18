package compiler_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

func BenchmarkOpCodeFusionPerformance(b *testing.B) {
	// Example EVM bytecode sequence (can be replaced with more realistic contract code)
	code := []byte{
		byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.PUSH1), 0x03, byte(compiler.MUL), byte(compiler.STOP),
	}

	// Apply fusion to get optimized code
	fusedCode, err := compiler.DoCodeFusion(append([]byte{}, code...))
	if err != nil {
		b.Fatalf("doCodeFusion failed: %v", err)
	}

	cfg := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
	}

	b.Run("OriginalCode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := runtime.Execute(code, nil, cfg)
			if err != nil {
				b.Fatalf("EVM execution failed (original): %v", err)
			}
		}
	})

	b.Run("FusedCode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := runtime.Execute(fusedCode, nil, cfg)
			if err != nil {
				b.Fatalf("EVM execution failed (fused): %v", err)
			}
		}
	})
}
