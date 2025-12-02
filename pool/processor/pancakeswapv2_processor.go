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

// PancakeSwapV2Handler wires the PancakeSwap V2 processor into the generic LP handler interface.
type PancakeSwapV2Handler struct {
	meta    Metadata
	parser  *PancakeSwapV2Processor
	current *V2State
}

// NewPancakeSwapV2Handler constructs a handler that can keep the latest reserves for a pool.
func NewPancakeSwapV2Handler(meta Metadata) (Handler, error) {
	return &PancakeSwapV2Handler{
		meta:   meta,
		parser: NewPancakeSwapV2Processor(),
	}, nil
}

// ApplyReceipt inspects the receipt for Sync events and updates the cached reserves.
func (h *PancakeSwapV2Handler) ApplyReceipt(receipt *types.Receipt) (bool, error) {
	state, ok, err := h.parser.Process(receipt)
	if err != nil {
		return false, err
	}
	if !ok || state == nil {
		return false, nil
	}
	clone, _ := state.Clone().(*V2State)
	h.current = clone
	return true, nil
}

// PriceToken0InToken1 returns the spot price derived from the most recent reserves.
func (h *PancakeSwapV2Handler) PriceToken0InToken1() *big.Rat {
	if h.current == nil {
		return nil
	}
	return computeV2Price(h.current.Reserve0, h.current.Reserve1, int(h.meta.Token0Decimals), int(h.meta.Token1Decimals))
}

// Snapshot returns a clone of the current reserve state.
func (h *PancakeSwapV2Handler) Snapshot() Snapshot {
	if h.current == nil {
		return nil
	}
	return h.current.Clone()
}
