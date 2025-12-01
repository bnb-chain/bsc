package pool

import (
	"errors"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/pool/processor"
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
	mu                 sync.RWMutex
	pools              map[common.Address]*LPState
	currentBlockHeight uint64 // Tracks the current blockchain height
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
		pools:              make(map[common.Address]*LPState),
		currentBlockHeight: 0,
	}
}

// NewLPManagerFromConfig creates a new LPManager and initializes it with pools from a config file.
func NewLPManagerFromConfig() (*LPManager, error) {
	manager := NewLPManager()
	config, err := LoadLPConfig("./pool/lp_config.json")
	if err != nil {
		return nil, err
	}

	for _, poolEntry := range config.Pools {
		lpConfig, err := poolEntry.ToLPConfig()
		if err != nil {
			return nil, err
		}

		if err := manager.RegisterPool(lpConfig); err != nil {
			return nil, err
		}
	}

	return manager, nil
}

// 尝试更新高度
func (m *LPManager) TryUpdateBlockHeight(height uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	//竞争到了修改
	if height > m.currentBlockHeight {
		m.currentBlockHeight = height
		return true
	}

	//没竞争到
	return false
}

// 判断是否需要更新
func (m *LPManager) NeedUpdate(height uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if height <= m.currentBlockHeight {
		log.Warn("区块高度小于lpManager区块高度,丢弃")
		return false
	}
	return true
}

// 更新操作
func (m *LPManager) Update(blockNumber uint64, receiptMap map[common.Address]*types.Receipt) {
	if len(receiptMap) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for addr, receipt := range receiptMap {
		state, ok := m.pools[addr]
		if !ok {
			log.Warn("未注册的LP, 跳过更新", "address", addr)
			continue
		}

		derived, okState, err := processor.ProcessReceipt(string(state.Config.Type), receipt)
		if err != nil {
			log.Warn("解析receipt失败", "blockNumber", blockNumber, "address", addr, "err", err)
			continue
		}
		if !okState || derived == nil {
			continue
		}

		switch state.Config.Type {
		case LPTypePancakeV2:
			if derived.V2 == nil {
				continue
			}
			state.V2State = cloneV2State(derived.V2)
			state.BlockHeight = blockNumber
		case LPTypePancakeV3:
			if derived.V3 == nil {
				continue
			}
			state.V3State = cloneV3State(derived.V3)
			state.BlockHeight = blockNumber
		default:
			log.Warn("未知LP类型, 无法更新", "blockNumber", blockNumber, "address", addr, "type", state.Config.Type)
		}

		log.Info("LP价格", "blockNumber", blockNumber, "address", addr, "priceToken0InToken1", state.PriceToken0InToken1().FloatString(4))
	}
}

// 获取所有LP
func (m *LPManager) AllLPInManager() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	addresses := make([]string, 0, len(m.pools))
	for addr := range m.pools {
		addresses = append(addresses, addr.String())
	}
	return addresses
}

// RegisterPool adds a new LP to the manager.
func (m *LPManager) RegisterPool(cfg LPConfig) error {
	if cfg.Address == (common.Address{}) {
		return errors.New("lp地址为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pools[cfg.Address]; ok {
		return ErrPoolExists
	}
	m.pools[cfg.Address] = &LPState{Config: cfg, BlockHeight: 0}

	// pancakeswapv2
	if cfg.Type == LPTypePancakeV2 {
		m.pools[cfg.Address].V2State = &V2State{Reserve0: big.NewInt(0), Reserve1: big.NewInt(0)}
	}

	//pancakeswapv3
	if cfg.Type == LPTypePancakeV3 {
		m.pools[cfg.Address].V3State = &V3State{SqrtPriceX96: big.NewInt(0), Liquidity: big.NewInt(0), Tick: 0}
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

func cloneV2State(src *processor.V2State) *V2State {
	if src == nil {
		return nil
	}
	return &V2State{
		Reserve0: new(big.Int).Set(src.Reserve0),
		Reserve1: new(big.Int).Set(src.Reserve1),
	}
}

func cloneV3State(src *processor.V3State) *V3State {
	if src == nil {
		return nil
	}
	return &V3State{
		SqrtPriceX96: new(big.Int).Set(src.SqrtPriceX96),
		Liquidity:    new(big.Int).Set(src.Liquidity),
		Tick:         src.Tick,
	}
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
