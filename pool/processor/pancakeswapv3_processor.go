package processor

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var swapEventSignature = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))

// PancakeSwapV3Processor extracts sqrtPrice, liquidity and tick information from swap receipts.
type PancakeSwapV3Processor struct {
	swapTopic common.Hash
}

// NewPancakeSwapV3Processor creates a processor ready to inspect receipts.
func NewPancakeSwapV3Processor() *PancakeSwapV3Processor {
	return &PancakeSwapV3Processor{
		swapTopic: swapEventSignature,
	}
}

// Process scans swap logs in the provided receipt and returns the last observed pool state.
func (p *PancakeSwapV3Processor) Process(receipt *types.Receipt) (*V3State, bool, error) {
	if receipt == nil {
		return nil, false, errors.New("receipt is nil")
	}
	var latest *V3State
	for _, logEntry := range receipt.Logs {
		if logEntry == nil || len(logEntry.Topics) == 0 {
			continue
		}
		if logEntry.Topics[0] != p.swapTopic {
			continue
		}
		state, err := decodeV3Swap(logEntry.Data)
		if err != nil {
			return nil, false, fmt.Errorf("decode swap log: %w", err)
		}
		latest = state
	}
	if latest == nil {
		return nil, false, nil
	}
	return latest, true, nil
}

func decodeV3Swap(data []byte) (*V3State, error) {
	const (
		wordSize   = 32
		fieldCount = 5 // amount0, amount1, sqrtPriceX96, liquidity, tick
	)
	if len(data) < wordSize*fieldCount {
		return nil, fmt.Errorf("invalid swap data length %d", len(data))
	}
	sqrtBytes := data[wordSize*2 : wordSize*3]
	liquidityBytes := data[wordSize*3 : wordSize*4]
	tickBytes := data[wordSize*4 : wordSize*5]

	sqrtPrice := new(big.Int).SetBytes(sqrtBytes)
	liquidity := new(big.Int).SetBytes(liquidityBytes)
	tick, err := decodeInt24(tickBytes)
	if err != nil {
		return nil, err
	}
	return &V3State{
		SqrtPriceX96: sqrtPrice,
		Liquidity:    liquidity,
		Tick:         tick,
	}, nil
}

func decodeInt24(data []byte) (int32, error) {
	if len(data) != 32 {
		return 0, fmt.Errorf("invalid int24 length %d", len(data))
	}
	n := int32(data[len(data)-3])<<16 | int32(data[len(data)-2])<<8 | int32(data[len(data)-1])
	if n&0x800000 != 0 {
		n |= ^0xFFFFFF
	}
	return n, nil
}
