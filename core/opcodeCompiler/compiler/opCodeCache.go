package compiler

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
)

type OpCodeCache struct {
	optimizedCodeCache *lru.Cache[common.Hash, []byte]
	bitvecCache        *lru.Cache[common.Hash, []byte]
	blockCache         *lru.Cache[common.Hash, map[uint64]*BasicBlock]
}

func (c *OpCodeCache) GetCachedBitvec(codeHash common.Hash) []byte {
	bitvec, _ := c.bitvecCache.Get(codeHash)
	return bitvec
}

func (c *OpCodeCache) AddBitvecCache(codeHash common.Hash, bitvec []byte) {
	c.bitvecCache.Add(codeHash, bitvec)
}

func (c *OpCodeCache) RemoveCachedCode(hash common.Hash) {
	c.optimizedCodeCache.Remove(hash)
}

func (c *OpCodeCache) GetCachedCode(hash common.Hash) []byte {
	processedCode, _ := c.optimizedCodeCache.Get(hash)
	return processedCode
}

func (c *OpCodeCache) AddCodeCache(hash common.Hash, optimizedCode []byte) {
	c.optimizedCodeCache.Add(hash, optimizedCode)
}

// 新增：BasicBlock cache相关方法
func (c *OpCodeCache) AddBlockCache(codeHash common.Hash, pcToBlock map[uint64]*BasicBlock) {
	c.blockCache.Add(codeHash, pcToBlock)
}

func (c *OpCodeCache) GetCachedBlock(codeHash common.Hash, pc uint64) (*BasicBlock, bool) {
	pcToBlock, exists := c.blockCache.Get(codeHash)
	if !exists {
		return nil, false
	}

	if block, found := pcToBlock[pc]; found {
		return block, true
	}
	return nil, false
}

func (c *OpCodeCache) IsBlockCached(codeHash common.Hash) bool {
	_, exists := c.blockCache.Get(codeHash)
	return exists
}

func (c *OpCodeCache) RemoveBlockCache(codeHash common.Hash) {
	c.blockCache.Remove(codeHash)
}

var opcodeCache *OpCodeCache

const (
	optimizedCodeCacheCap = 1024 * 1024
	bitvecCacheCap        = 1024 * 1024
	blockCacheCap         = 1024 * 1024
)

func init() {
	opcodeCache = &OpCodeCache{
		optimizedCodeCache: lru.NewCache[common.Hash, []byte](optimizedCodeCacheCap),
		bitvecCache:        lru.NewCache[common.Hash, []byte](bitvecCacheCap),
		blockCache:         lru.NewCache[common.Hash, map[uint64]*BasicBlock](blockCacheCap),
	}
}

func getOpCodeCacheInstance() *OpCodeCache {
	return opcodeCache
}
