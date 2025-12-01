package processor

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

type V2State struct {
	Reserve0 *big.Int
	Reserve1 *big.Int
}

type V3State struct {
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
}

// State describes the dynamic values we can derive from a receipt for a tracked LP.
type State struct {
	V2 *V2State
	V3 *V3State
}

// Processor transforms a receipt into the latest liquidity state snapshot.
type Processor interface {
	Process(receipt *types.Receipt) (*State, bool, error)
}

type Func func(receipt *types.Receipt) (*State, bool, error)

func (f Func) Process(receipt *types.Receipt) (*State, bool, error) {
	return f(receipt)
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Processor)
)

// RegisterProcessor allows external packages to register or override a processor for a LP type.
func RegisterProcessor(lpType string, processor Processor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[lpType] = processor
}

// ProcessorFor retrieves a registered processor for the given LP type.
func ProcessorFor(lpType string) (Processor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	proc, ok := registry[lpType]
	return proc, ok
}

// ProcessReceipt runs the corresponding processor for the LP type and returns the derived state.
func ProcessReceipt(lpType string, receipt *types.Receipt) (*State, bool, error) {
	proc, ok := ProcessorFor(lpType)
	if !ok {
		return nil, false, fmt.Errorf("no processor registered for lp type %s", lpType)
	}
	return proc.Process(receipt)
}
