package aggpathdb

import (
	"fmt"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
)

type aggnodecache struct {
	cleans *fastcache.Cache
	db     *Database // Agg-Path-based trie database
}

func newAggNodeCache(db *Database, cleans *fastcache.Cache, cacheSize int) *aggnodecache {
	if cleans == nil {
		if cacheSize > maxCleanAggNodeSize {
			cacheSize = maxCleanAggNodeSize
		}
		cleans = fastcache.New(cacheSize)
	}

	log.Info("Allocated aggNode cache", "size", cacheSize)
	return &aggnodecache{
		cleans: cleans,
		db:     db,
	}
}

func (c *aggnodecache) node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	aggPath := toAggPath(path)
	key := cacheKey(owner, aggPath)

	if c.cleans != nil {
		if blob := c.cleans.Get(nil, key); len(blob) > 0 {
			aggNode, err := DecodeAggNode(blob)
			if err != nil {
				return nil, fmt.Errorf("decode aggNode failed. error: %v", err)
			}

			rawNode := aggNode.Node(path)
			if rawNode == nil {
				// not found
				return []byte{}, nil
			}
			h := newHasher()
			defer h.release()

			got := h.hash(rawNode)
			if got == hash {
				aggNodeCleanHitMeter.Mark(1)
				aggNodeCleanReadMeter.Mark(int64(len(blob)))
				return rawNode, nil
			}
			aggNodeCleanFalseMeter.Mark(1)
			log.Error("Unexpected trie node in clean cache", "owner", owner, "path", path, "expect", hash, "got", got)
		}
		aggNodeCleanMissMeter.Mark(1)
	}

	// Try to retrieve the trie node from the disk.
	var (
		nBlob []byte
		nHash common.Hash
	)

	// try to get aggNode from the database
	if owner == (common.Hash{}) {
		nBlob = rawdb.ReadAccountTrieAggNode(c.db.diskdb, aggPath)
	} else {
		nBlob = rawdb.ReadStorageTrieAggNode(c.db.diskdb, owner, aggPath)
	}
	aggNode, err := DecodeAggNode(nBlob)
	if err != nil {
		return nil, fmt.Errorf("decode aggNode failed. error: %v", err)
	}
	rawNode := aggNode.Node(path)
	if rawNode == nil {
		// not found
		return []byte{}, nil
	}
	h := newHasher()
	defer h.release()

	nHash = h.hash(rawNode)

	if nHash != hash {
		diskFalseMeter.Mark(1)
		log.Error("Unexpected trie node in disk", "owner", owner, "path", path, "expect", hash, "got", nHash)
		return nil, newUnexpectedNodeError("disk", hash, nHash, owner, path, nBlob)
	}
	if c.cleans != nil && len(nBlob) > 0 {
		c.cleans.Set(key, nBlob)
		aggNodeCleanWriteMeter.Mark(int64(len(nBlob)))
	}

	return rawNode, nil
}

func (c *aggnodecache) aggNode(owner common.Hash, aggPath []byte) (*AggNode, error) {
	var blob []byte
	if c.cleans != nil {
		cacheHit := false
		blob, cacheHit = c.cleans.HasGet(nil, cacheKey(owner, aggPath))
		if cacheHit {
			aggNodeCleanHitMeter.Mark(1)
			aggNodeCleanReadMeter.Mark(int64(len(blob)))
			return DecodeAggNode(blob)
		}
		aggNodeCleanMissMeter.Mark(1)
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
