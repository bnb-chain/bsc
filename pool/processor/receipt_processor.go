package processor

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Snapshot represents a point-in-time LP state that can be cloned safely.
type Snapshot interface {
	Clone() Snapshot
}

// V2State mirrors PancakeSwap V2 style reserves.
type V2State struct {
	Reserve0 *big.Int
	Reserve1 *big.Int
}

// Clone implements the Snapshot interface for V2 reserves.
func (s *V2State) Clone() Snapshot {
	if s == nil {
		return nil
	}
	return &V2State{
		Reserve0: new(big.Int).Set(s.Reserve0),
		Reserve1: new(big.Int).Set(s.Reserve1),
	}
}

// V3State mirrors PancakeSwap V3 (Uniswap V3) style sqrt price data.
type V3State struct {
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
}

// Clone implements the Snapshot interface for V3 state.
func (s *V3State) Clone() Snapshot {
	if s == nil {
		return nil
	}
	return &V3State{
		SqrtPriceX96: new(big.Int).Set(s.SqrtPriceX96),
		Liquidity:    new(big.Int).Set(s.Liquidity),
		Tick:         s.Tick,
	}
}

// Handler is responsible for hydrating LP state from receipts and exposing derived data such as price.
type Handler interface {
	ApplyReceipt(receipt *types.Receipt) (bool, error)
	PriceToken0InToken1() *big.Rat
	Snapshot() Snapshot
}

// Metadata captures static LP configuration required by handlers.
type Metadata struct {
	Address        common.Address
	Token0Symbol   string
	Token1Symbol   string
	Token0Decimals uint8
	Token1Decimals uint8
	Fee            uint32
}

// HandlerFactory constructs a handler for a specific LP type.
type HandlerFactory func(meta Metadata) (Handler, error)

var (
	handlerRegistryMu sync.RWMutex
	handlerRegistry   = make(map[string]HandlerFactory)
)

// RegisterHandler allows external packages to register or override a handler factory for a LP type.
func RegisterHandler(lpType string, factory HandlerFactory) {
	handlerRegistryMu.Lock()
	defer handlerRegistryMu.Unlock()
	handlerRegistry[lpType] = factory
}

// BuildHandler constructs a handler for the provided LP type and metadata.
func BuildHandler(lpType string, meta Metadata) (Handler, error) {
	handlerRegistryMu.RLock()
	factory, ok := handlerRegistry[lpType]
	handlerRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no handler registered for lp type %s", lpType)
	}
	return factory(meta)
}
