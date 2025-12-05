package runtime_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestMIRUSDT_Allowance_BaseTrace(t *testing.T) {
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	base := &runtime.Config{
		ChainConfig: params.BSCChainConfig,
		GasLimit:    100_000,
		Origin:      common.Address{},
		BlockNumber: compatBlock,
		Value:       big.NewInt(0),
		Debug:       true,
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	if base.State == nil {
		base.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}

	addr := common.BytesToAddress([]byte("contract_usdt_allowance_base"))
	evmB := runtime.NewEnv(base)
	evmB.StateDB.CreateAccount(addr)
	evmB.StateDB.SetCode(addr, code)

	zeroAddress := make([]byte, 32)
	input := append([]byte{0x39, 0x50, 0x93, 0x51}, zeroAddress...)
	input = append(input, zeroAddress...)

	base.EVMConfig.Tracer = &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			fmt.Printf("BASE: pc=%d op=%x stack=%d\n", pc, op, scope.StackData())
		},
	}

	senderB := vm.AccountRef(base.Origin)
	_, _, errB := evmB.Call(senderB, addr, input, base.GasLimit, uint256.MustFromBig(base.Value))
	fmt.Printf("Base result: %v\n", errB)
}
