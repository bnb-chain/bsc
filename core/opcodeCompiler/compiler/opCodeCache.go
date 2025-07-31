package compiler

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
)

type OpCodeCache struct {
	optimizedCodeCache *lru.Cache[common.Hash, []byte]
	bitvecCache        *lru.Cache[common.Hash, []byte]
	staticGasCache     *lru.Cache[common.Hash, uint64]
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
	c.staticGasCache.Remove(hash)
}

func (c *OpCodeCache) GetCachedCode(hash common.Hash) []byte {
	processedCode, _ := c.optimizedCodeCache.Get(hash)
	return processedCode
}

func (c *OpCodeCache) AddCodeCache(hash common.Hash, optimizedCode []byte) {
	c.optimizedCodeCache.Add(hash, optimizedCode)
}

func (c *OpCodeCache) GetCachedStaticGas(hash common.Hash) uint64 {
	staticGas, _ := c.staticGasCache.Get(hash)
	return staticGas
}

func (c *OpCodeCache) AddStaticGasCache(hash common.Hash, staticGas uint64) {
	c.staticGasCache.Add(hash, staticGas)
}

var opcodeCache *OpCodeCache

const (
	optimizedCodeCacheCap = 1024 * 1024
	bitvecCacheCap        = 1024 * 1024
	staticGasCacheCap     = 1024 * 1024
)

func init() {
	opcodeCache = &OpCodeCache{
		optimizedCodeCache: lru.NewCache[common.Hash, []byte](optimizedCodeCacheCap),
		bitvecCache:        lru.NewCache[common.Hash, []byte](bitvecCacheCap),
		staticGasCache:     lru.NewCache[common.Hash, uint64](staticGasCacheCap),
	}
}

func getOpCodeCacheInstance() *OpCodeCache {
	return opcodeCache
}
