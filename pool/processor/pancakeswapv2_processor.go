package processor

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var syncEventSignature = crypto.Keccak256Hash([]byte("Sync(uint112,uint112)"))

// PancakeSwapV2Processor extracts reserve information from PancakeSwap(V2-compatible) receipts.
type PancakeSwapV2Processor struct {
	syncTopic common.Hash
}

// NewPancakeSwapV2Processor creates a processor ready to inspect receipts.
func NewPancakeSwapV2Processor() *PancakeSwapV2Processor {
	return &PancakeSwapV2Processor{
		syncTopic: syncEventSignature,
	}
}

// Process scans the receipt logs, returning the latest reserve snapshot if a Sync event is found.
func (p *PancakeSwapV2Processor) Process(receipt *types.Receipt) (*V2State, bool, error) {
	if receipt == nil {
		return nil, false, errors.New("receipt is nil")
	}
	var latest *V2State
	for _, logEntry := range receipt.Logs {
		if logEntry == nil || len(logEntry.Topics) == 0 {
			continue
		}
		if logEntry.Topics[0] != p.syncTopic {
			continue
		}
		state, err := decodeV2Sync(logEntry.Data)
		if err != nil {
			return nil, false, fmt.Errorf("decode sync log: %w", err)
		}
		latest = state
	}
	if latest == nil {
		return nil, false, nil
	}
	return latest, true, nil
}

func decodeV2Sync(data []byte) (*V2State, error) {
	const wordSize = 32
	if len(data) < wordSize*2 {
		return nil, fmt.Errorf("invalid sync data length %d", len(data))
	}
	reserve0 := new(big.Int).SetBytes(data[:wordSize])
	reserve1 := new(big.Int).SetBytes(data[wordSize : wordSize*2])
	return &V2State{
		Reserve0: reserve0,
		Reserve1: reserve1,
	}, nil
}
