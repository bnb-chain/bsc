package pool

import (
	"errors"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// LPType enumerates supported liquidity pool flavours.
type LPType string

const (
	LPTypePancakeV2 LPType = "pancake_v2"
	LPTypePancakeV3 LPType = "pancake_v3"
)

// LPConfig describes the static settings that do not change between updates.
type LPConfig struct {
	Address        common.Address
	Type           LPType
	Token0Symbol   string
	Token1Symbol   string
	Token0Decimals uint8
	Token1Decimals uint8
	// Fee is only meaningful for V3 pools but kept for completeness.
	Fee uint32
}

// LPState keeps the dynamic parameters required to quote prices.
type LPState struct {
	Config LPConfig

	BlockHeight uint64
	V2State     *V2State
	V3State     *V3State
}

// V2State mirrors PancakeSwap V2 style reserves.
type V2State struct {
	Reserve0 *big.Int
	Reserve1 *big.Int
}

// V3State mirrors PancakeSwap V3 (Uniswap V3) style sqrt price data.
type V3State struct {
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
}

// PriceToken0InToken1 returns the spot price of token0 denominated in token1.
// Nil is returned when the state for the underlying pool is incomplete.
func (s *LPState) PriceToken0InToken1() *big.Rat {
	switch s.Config.Type {
	case LPTypePancakeV2:
		if s.V2State == nil || s.V2State.Reserve0.Sign() == 0 || s.V2State.Reserve1.Sign() == 0 {
			return nil
		}
		return computeV2Price(s.V2State.Reserve0, s.V2State.Reserve1, int(s.Config.Token0Decimals), int(s.Config.Token1Decimals))
	case LPTypePancakeV3:
		if s.V3State == nil || s.V3State.SqrtPriceX96.Sign() == 0 {
			return nil
		}
		return computeV3Price(s.V3State.SqrtPriceX96, int(s.Config.Token0Decimals), int(s.Config.Token1Decimals))
	default:
		return nil
	}
}

// LPManager tracks multiple LPs concurrently.
type LPManager struct {
	mu    sync.RWMutex
	pools map[common.Address]*LPState
}

// Errors surfaced by LPManager operations.
var (
	ErrPoolExists   = errors.New("lp already registered")
	ErrPoolMissing  = errors.New("lp not registered")
	ErrTypeMismatch = errors.New("lp type mismatch for update")
	ErrStaleUpdate  = errors.New("received state from older block height")
)

// NewLPManager constructs an empty manager.
func NewLPManager() *LPManager {
	return &LPManager{
		pools: make(map[common.Address]*LPState),
	}
}

// RegisterPool adds a new LP to the manager.
func (m *LPManager) RegisterPool(cfg LPConfig) error {
	if cfg.Address == (common.Address{}) {
		return errors.New("lp address must not be zero")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pools[cfg.Address]; ok {
		return ErrPoolExists
	}
	m.pools[cfg.Address] = &LPState{Config: cfg}
	return nil
}

// UpdateV2 sets the latest PancakeSwap V2 style reserves for a pool.
func (m *LPManager) UpdateV2(addr common.Address, blockHeight uint64, reserve0, reserve1 *big.Int) error {
	if reserve0 == nil || reserve1 == nil {
		return errors.New("reserves cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.pools[addr]
	if !ok {
		return ErrPoolMissing
	}
	if state.Config.Type != LPTypePancakeV2 {
		return ErrTypeMismatch
	}
	if blockHeight < state.BlockHeight {
		return ErrStaleUpdate
	}

	state.BlockHeight = blockHeight
	state.V2State = &V2State{
		Reserve0: new(big.Int).Set(reserve0),
		Reserve1: new(big.Int).Set(reserve1),
	}
	return nil
}

// UpdateV3 applies the latest PancakeSwap V3 style state.
func (m *LPManager) UpdateV3(addr common.Address, blockHeight uint64, sqrtPriceX96, liquidity *big.Int, tick int32) error {
	if sqrtPriceX96 == nil || liquidity == nil {
		return errors.New("sqrt price and liquidity cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.pools[addr]
	if !ok {
		return ErrPoolMissing
	}
	if state.Config.Type != LPTypePancakeV3 {
		return ErrTypeMismatch
	}
	if blockHeight < state.BlockHeight {
		return ErrStaleUpdate
	}

	state.BlockHeight = blockHeight
	state.V3State = &V3State{
		SqrtPriceX96: new(big.Int).Set(sqrtPriceX96),
		Liquidity:    new(big.Int).Set(liquidity),
		Tick:         tick,
	}
	return nil
}

// Get returns a snapshot of the tracked LP state.
func (m *LPManager) Get(addr common.Address) (*LPState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.pools[addr]
	if !ok {
		return nil, false
	}
	clone := *state
	if state.V2State != nil {
		clone.V2State = &V2State{
			Reserve0: new(big.Int).Set(state.V2State.Reserve0),
			Reserve1: new(big.Int).Set(state.V2State.Reserve1),
		}
	}
	if state.V3State != nil {
		clone.V3State = &V3State{
			SqrtPriceX96: new(big.Int).Set(state.V3State.SqrtPriceX96),
			Liquidity:    new(big.Int).Set(state.V3State.Liquidity),
			Tick:         state.V3State.Tick,
		}
	}
	return &clone, true
}

func computeV2Price(reserve0, reserve1 *big.Int, dec0, dec1 int) *big.Rat {
	num := new(big.Int).Set(reserve1)
	den := new(big.Int).Set(reserve0)

	adjustDecimals(num, den, dec1-dec0)

	return new(big.Rat).SetFrac(num, den)
}

func computeV3Price(sqrtPriceX96 *big.Int, dec0, dec1 int) *big.Rat {
	num := new(big.Int).Mul(sqrtPriceX96, sqrtPriceX96)
	den := new(big.Int).Lsh(big.NewInt(1), 192) // 2^192

	adjustDecimals(num, den, dec1-dec0)

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
