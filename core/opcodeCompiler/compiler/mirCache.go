package compiler

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
)

// MIRCache implements LRU cache for MIR CFG, similar to OpCodeCache
type MIRCache struct {
	cfgCache *lru.Cache[common.Hash, *CFG]
}

// Global MIR cache instance (similar to opcodeCache)
var mirCache *MIRCache

const (
	// Smaller than superinstruction cache due to larger memory footprint per CFG
	mirCFGCacheCap = 1024
)

func init() {
	mirCache = &MIRCache{
		cfgCache: lru.NewCache[common.Hash, *CFG](mirCFGCacheCap),
	}
}

// getMIRCacheInstance returns the global MIR cache instance
func getMIRCacheInstance() *MIRCache {
	return mirCache
}

// GetCachedCFG retrieves a cached MIR CFG by hash
func (c *MIRCache) GetCachedCFG(hash common.Hash) *CFG {
	cfg, _ := c.cfgCache.Get(hash)
	return cfg
}

// AddCFGCache adds a MIR CFG to the cache
func (c *MIRCache) AddCFGCache(hash common.Hash, cfg *CFG) {
	c.cfgCache.Add(hash, cfg)
}

// RemoveCFGCache removes a MIR CFG from the cache
func (c *MIRCache) RemoveCFGCache(hash common.Hash) {
	c.cfgCache.Remove(hash)
}

// Len returns the number of cached CFGs
func (c *MIRCache) Len() int {
	return c.cfgCache.Len()
}

// LoadMIRCFG loads a cached MIR CFG (similar to LoadOptimizedCode)
func LoadMIRCFG(hash common.Hash) *CFG {
	if !IsOpcodeParseEnabled() {
		return nil
	}
	return mirCache.GetCachedCFG(hash)
}
