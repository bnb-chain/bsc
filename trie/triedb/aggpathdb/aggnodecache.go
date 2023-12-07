package aggpathdb

import (
	"fmt"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
)

type aggNodeCache struct {
	cleans *fastcache.Cache
	db     *Database // Agg-Path-based trie database
}

func newAggNodeCache(db *Database, cleans *fastcache.Cache, cacheSize int) *aggNodeCache {
	if cleans == nil {
		cleans = fastcache.New(cacheSize)
	}

	log.Info("Allocated node cache", "size", cacheSize)
	return &aggNodeCache{
		cleans: cleans,
		db:     db,
	}
}

func (c *aggNodeCache) node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	aggPath := ToAggPath(path)
	key := cacheKey(owner, aggPath)

	if c.cleans != nil {
		if blob := c.cleans.Get(nil, key); len(blob) > 0 {
			aggNode, err := DecodeAggNode(blob)
			if err != nil {
				return nil, fmt.Errorf("decode node failed from clean cache. error: %v", err)
			}

			n := aggNode.Node(path)
			if n == nil {
				// not found
				return []byte{}, nil
			}

			if n.Hash == hash {
				cleanHitMeter.Mark(1)
				cleanReadMeter.Mark(int64(len(blob)))
				return n.Blob, nil
			}
			cleanFalseMeter.Mark(1)
			log.Error("Unexpected trie node in clean cache", "owner", owner, "path", path, "expect", hash, "got", n.Hash)
		}
		cleanMissMeter.Mark(1)
	}

	// Try to retrieve the trie node from the disk.
	var (
		nBlob []byte
		nHash common.Hash
	)

	// try to get node from the database
	if owner == (common.Hash{}) {
		nBlob = rawdb.ReadAccountTrieAggNode(c.db.diskdb, aggPath)
	} else {
		nBlob = rawdb.ReadStorageTrieAggNode(c.db.diskdb, owner, aggPath)
	}
	if nBlob == nil {
		// not found
		return []byte{}, nil
	}
	aggNode, err := DecodeAggNode(nBlob)
	if err != nil {
		return nil, fmt.Errorf("decode node failed from diskdb. error: %v", err)
	}
	n := aggNode.Node(path)
	if n == nil {
		// not found
		return []byte{}, nil
	}

	if n.Hash != hash {
		diskFalseMeter.Mark(1)
		log.Error("Unexpected trie node in disk", "owner", owner, "path", path, "expect", hash, "got", nHash)
		return nil, newUnexpectedNodeError("disk", hash, nHash, owner, path, nBlob)
	}
	if c.cleans != nil {
		c.cleans.Set(key, nBlob)
		cleanWriteMeter.Mark(int64(len(nBlob)))
	}

	return n.Blob, nil
}

func (c *aggNodeCache) aggNode(owner common.Hash, aggPath []byte) (*AggNode, error) {
	var blob []byte
	cKey := cacheKey(owner, aggPath)
	if c.cleans != nil {
		cacheHit := false
		blob, cacheHit = c.cleans.HasGet(nil, cKey)
		if cacheHit {
			cleanHitMeter.Mark(1)
			cleanReadMeter.Mark(int64(len(blob)))
			return DecodeAggNode(blob)
		}
		cleanMissMeter.Mark(1)
	}

	// cache miss
	if owner == (common.Hash{}) {
		blob = rawdb.ReadAccountTrieAggNode(c.db.diskdb, aggPath)
	} else {
		blob = rawdb.ReadStorageTrieAggNode(c.db.diskdb, owner, aggPath)
	}
	if blob == nil {
		return nil, nil
	}

	return DecodeAggNode(blob)
}

func (c *aggNodeCache) Reset() {
	c.cleans.Reset()
}

func (c *aggNodeCache) Del(k []byte) {
	if c.cleans != nil {
		c.cleans.Del(k)
	}
}

func (c *aggNodeCache) Set(k, v []byte) {
	if c.cleans != nil {
		c.cleans.Set(k, v)
	}
}

func (c *aggNodeCache) HasGet(dst, k []byte) ([]byte, bool) {
	if c.cleans != nil {
		return c.cleans.HasGet(dst, k)
	}
	return nil, false
}
