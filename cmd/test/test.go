package main

import (
	"fmt"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/shortcut"
)

const (
	epochNum = 10000000
)

func main() {
	//start := time.Now()

	addr := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	addr2 := common.HexToAddress("0x55d398326f99059fF775485246999027B3197952")
	callData := hexutil.MustDecode("0xa9059cbb00000000000000000000000036421722ef63cbaed70f0c68f315c4bbadfa21dc00000000000000000000000000000000000000000000000ad78ebc5ac6200000")

	start := time.Now()

	for i := 0; i < epochNum; i++ {
		var Pc uint64
		var GasUsed uint64
		var Stack []uint256.Int
		var Mem []byte
		var LastGasCost uint64
		addr_ := addr
		if false {
			addr_ = addr2
		}
		impl := shortcut.GetShortcutV2(addr_)
		if impl == nil {
			continue
		}
		pc, gas, stack, mem, lastGasUsed, expected, err := impl.Shortcut(callData, addr, addr, uint256.NewInt(0))
		if !expected || err != nil {

		} else {
			Pc = pc
			GasUsed = gas
			Stack = stack
			Mem = mem
			LastGasCost = lastGasUsed
		}

		_, _, _, _, _ = Pc, GasUsed, Stack, Mem, LastGasCost
	}

	fmt.Println(time.Since(start) / epochNum)

}
