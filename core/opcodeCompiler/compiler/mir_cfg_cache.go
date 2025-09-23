package compiler

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
)

type mirCFGCache struct {
	cache *lru.Cache[common.Hash, *CFG]
}

var globalMIRCFGCache *mirCFGCache

const mirCFGCacheCap = 1024 * 1024

func init() {
	globalMIRCFGCache = &mirCFGCache{
		cache: lru.NewCache[common.Hash, *CFG](mirCFGCacheCap),
	}
}

func StoreMIRCFG(hash common.Hash, cfg *CFG) {
	if cfg == nil {
		return
	}
	globalMIRCFGCache.cache.Add(hash, cfg)
}

func LoadMIRCFG(hash common.Hash) *CFG {
	if cfg, ok := globalMIRCFGCache.cache.Get(hash); ok {
		return cfg
	}
	return nil
}
