package pool

import (
	"errors"
	"math/big"
	"sync"
	"sync/atomic"

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
	Config      LPConfig
	BlockHeight uint64
	Price       *big.Rat
	Snapshot    processor.Snapshot
}

// PriceToken0InToken1 returns the spot price of token0 denominated in token1.
// Nil is returned when the state for the underlying pool is incomplete.
func (s *LPState) PriceToken0InToken1() *big.Rat {
	if s == nil || s.Price == nil {
		return nil
	}
	return new(big.Rat).Set(s.Price)
}

type trackedPool struct {
	config      LPConfig
	handler     processor.Handler
	blockHeight uint64
}

// LPManager tracks multiple LPs concurrently.
type LPManager struct {
	mu                 sync.RWMutex
	pools              map[common.Address]*trackedPool
	poolSnapshot       atomic.Value // map[common.Address]*trackedPool
	currentBlockHeight uint64       // Tracks the current blockchain height
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
	manager := &LPManager{
		pools:              make(map[common.Address]*trackedPool),
		currentBlockHeight: 0,
	}
	manager.poolSnapshot.Store(map[common.Address]*trackedPool{})
	return manager
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

// IsProcessed returns true if the manager has already processed the given height.
func (m *LPManager) IsProcessed(height uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return height <= m.currentBlockHeight
}

// UpdateFromReceipts 解析一批 receipts 并更新对应 LP 的状态。
// 返回实际完成更新的 LP 数量。
func (m *LPManager) UpdateFromReceipts(blockNumber uint64, receiptsList []types.Receipts) int {
	if len(receiptsList) == 0 {
		return 0
	}

	snapshot := m.snapshotPools()
	if len(snapshot) == 0 {
		return 0
	}

	tracked := make(map[common.Address]*types.Receipt)
	for _, receipts := range receiptsList {
		for _, receipt := range receipts {
			if receipt == nil {
				continue
			}
			for _, logEntry := range receipt.Logs {
				if logEntry == nil {
					continue
				}
				if _, ok := snapshot[logEntry.Address]; ok {
					tracked[logEntry.Address] = receipt
				}
			}
		}
	}

	if len(tracked) == 0 {
		return 0
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	updatedCount := 0
	for addr, receipt := range tracked {
		pool, ok := m.pools[addr]
		if !ok {
			continue
		}

		updated, err := pool.handler.ApplyReceipt(receipt)
		if err != nil {
			log.Warn("解析receipt失败", "blockNumber", blockNumber, "address", addr, "err", err)
			continue
		}
		if !updated {
			continue
		}

		pool.blockHeight = blockNumber
		updatedCount++

		price := pool.handler.PriceToken0InToken1()
		priceStr := "nil"
		if price != nil {
			priceStr = price.FloatString(4)
		}
		log.Info("LP价格", "blockNumber", blockNumber, "address", addr, "priceToken0InToken1", priceStr)
	}

	return updatedCount
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
	handler, err := processor.BuildHandler(string(cfg.Type), metadataFromConfig(cfg))
	if err != nil {
		return err
	}
	m.pools[cfg.Address] = &trackedPool{
		config:      cfg,
		handler:     handler,
		blockHeight: 0,
	}
	m.refreshSnapshotLocked()

	return nil
}

// Get returns a snapshot of the tracked LP state.
func (m *LPManager) Get(addr common.Address) (*LPState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pool, ok := m.pools[addr]
	if !ok {
		return nil, false
	}
	snapshot := pool.handler.Snapshot()
	var clone processor.Snapshot
	if snapshot != nil {
		clone = snapshot.Clone()
	}
	var price *big.Rat
	if p := pool.handler.PriceToken0InToken1(); p != nil {
		price = new(big.Rat).Set(p)
	}
	return &LPState{
		Config:      pool.config,
		BlockHeight: pool.blockHeight,
		Price:       price,
		Snapshot:    clone,
	}, true
}

func metadataFromConfig(cfg LPConfig) processor.Metadata {
	return processor.Metadata{
		Address:        cfg.Address,
		Token0Symbol:   cfg.Token0Symbol,
		Token1Symbol:   cfg.Token1Symbol,
		Token0Decimals: cfg.Token0Decimals,
		Token1Decimals: cfg.Token1Decimals,
		Fee:            cfg.Fee,
	}
}

func (m *LPManager) snapshotPools() map[common.Address]*trackedPool {
	if snapshot, ok := m.poolSnapshot.Load().(map[common.Address]*trackedPool); ok {
		return snapshot
	}
	return nil
}

func (m *LPManager) refreshSnapshotLocked() {
	clone := make(map[common.Address]*trackedPool, len(m.pools))
	for addr, pool := range m.pools {
		clone[addr] = pool
	}
	m.poolSnapshot.Store(clone)
}
