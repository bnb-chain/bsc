package blockarchiver

import (
	"context"
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	GetBlockTimeout = 5 * time.Second

	RPCTimeout = 30 * time.Second
)

var _ BlockArchiver = (*BlockArchiverService)(nil)

type BlockArchiver interface {
	GetLatestBlock() (*GeneralBlock, error)
	GetBlockByNumber(number uint64) (*types.Body, *types.Header, error)
	GetBlockByHash(hash common.Hash) (*types.Body, *types.Header, error)
}

type BlockArchiverService struct {
	// client to interact with the block archiver service
	client *Client
	// injected from BlockChain, the specified block is always read and write simultaneously in bodyCache and headerCache.
	bodyCache *lru.Cache[common.Hash, *types.Body]
	// injected from BlockChain.headerChain
	headerCache *lru.Cache[common.Hash, *types.Header]
	// hashCache is a cache for block number to hash mapping
	hashCache *lru.Cache[uint64, common.Hash]
	// requestLock is a lock to avoid concurrent fetching of the same bundle of blocks
	requestLock *RequestLock
}

// NewBlockArchiverService creates a new block archiver service
// the bodyCache and headerCache are injected from the BlockChain
func NewBlockArchiverService(blockArchiver, sp, bucketName string,
	bodyCache *lru.Cache[common.Hash, *types.Body],
	headerCache *lru.Cache[common.Hash, *types.Header],
	cacheSize int,
) (BlockArchiver, error) {
	client, err := New(blockArchiver, sp, bucketName)
	if err != nil {
		return nil, err
	}
	b := &BlockArchiverService{
		client:      client,
		bodyCache:   bodyCache,
		headerCache: headerCache,
		hashCache:   lru.NewCache[uint64, common.Hash](cacheSize),
		requestLock: NewRequestLock(),
	}
	go b.cacheStats()
	return b, nil
}

// GetLatestBlock returns the latest block
func (c *BlockArchiverService) GetLatestBlock() (*GeneralBlock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	blockResp, err := c.client.GetLatestBlock(ctx)
	if err != nil {
		log.Error("failed to get latest block", "err", err)
		return nil, err
	}
	block, err := convertBlock(blockResp)
	if err != nil {
		log.Error("failed to convert block", "block", blockResp, "err", err)
		return nil, err
	}
	return block, nil
}

// GetLatestHeader returns the latest header
func (c *BlockArchiverService) GetLatestHeader() (*types.Header, error) {
	block, err := c.GetLatestBlock()
	if err != nil {
		log.Error("failed to get latest block", "err", err)
		return nil, err
	}
	return block.Header(), nil
}

// GetBlockByNumber returns the block by number
func (c *BlockArchiverService) GetBlockByNumber(number uint64) (*types.Body, *types.Header, error) {
	log.Debug("get block by number", "number", number)
	hash, found := c.hashCache.Get(number)
	if found {
		log.Debug("GetBlockByNumber found in cache", number)
		body, foundB := c.bodyCache.Get(hash)
		header, foundH := c.headerCache.Get(hash)
		if foundB && foundH {
			return body, header, nil
		}
	}
	return c.getBlockByNumber(number)
}

// getBlockByNumber returns the block by number
func (c *BlockArchiverService) getBlockByNumber(number uint64) (*types.Body, *types.Header, error) {
	// to avoid concurrent fetching of the same bundle of blocks, requestLock applies here
	// if the number is within any of the ranges, should not fetch the bundle from the block archiver service but
	// wait for a while and fetch from the cache
	if c.requestLock.IsWithinAnyRange(number) {
		log.Debug("getBlockByNumber is within any range", number)
		if blockRange := c.requestLock.GetRangeForNumber(number); blockRange != nil {
			select {
			case <-blockRange.done:
				hash, found := c.hashCache.Get(number)
				if found {
					body, foundB := c.bodyCache.Get(hash)
					header, foundH := c.headerCache.Get(hash)
					if foundB && foundH {
						return body, header, nil
					}
				}
			case <-time.After(GetBlockTimeout):
				return nil, nil, errors.New("block not found")
			}
		}
	}
	// fetch the bundle range
	log.Info("fetching bundle of blocks", "number", number)
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()

	bundleName, err := c.client.GetBundleName(ctx, number)
	if err != nil {
		log.Error("failed to get bundle name", "number", number, "err", err)
		return nil, nil, err
	}

	start, end, err := ParseBundleName(bundleName)
	if err != nil {
		log.Error("failed to parse bundle name", "bundleName", bundleName, "err", err)
		return nil, nil, err
	}
	// add lock to avoid concurrent fetching of the same bundle of blocks
	c.requestLock.AddRange(start, end)
	defer c.requestLock.RemoveRange(start, end)
	ctx, cancel = context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()

	blocks, err := c.client.GetBundleBlocks(ctx, bundleName)
	if err != nil {
		log.Error("failed to get bundle blocks", "bundleName", bundleName, "err", err)
		return nil, nil, err
	}
	var body *types.Body
	var header *types.Header

	log.Debug("populating block cache", "start", start, "end", end)
	for _, b := range blocks {
		block, err := convertBlock(b)
		if err != nil {
			log.Error("failed to convert block", "block", b, "err", err)
			return nil, nil, err
		}
		c.bodyCache.Add(block.Hash(), block.Body())
		c.headerCache.Add(block.Hash(), block.Header())
		c.hashCache.Add(block.NumberU64(), block.Hash())
		if block.NumberU64() == number {
			body = block.Body()
			header = block.Header()
		}
	}
	return body, header, nil
}

// GetBlockByHash returns the block by hash
func (c *BlockArchiverService) GetBlockByHash(hash common.Hash) (*types.Body, *types.Header, error) {
	log.Debug("get block by hash", "hash", hash.Hex())
	body, foundB := c.bodyCache.Get(hash)
	header, foundH := c.headerCache.Get(hash)
	if foundB && foundH {
		return body, header, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	block, err := c.client.GetBlockByHash(ctx, hash)
	if err != nil {
		log.Error("failed to get block by hash", "hash", hash, "err", err)
		return nil, nil, err
	}
	if block == nil {
		log.Debug("block is nil", "hash", hash)
		return nil, nil, nil
	}
	number, err := HexToUint64(block.Number)
	if err != nil {
		log.Error("failed to convert block number", "block", block, "err", err)
		return nil, nil, err
	}
	return c.getBlockByNumber(number)
}

func (c *BlockArchiverService) cacheStats() {
	for range time.NewTicker(1 * time.Minute).C {
		log.Info("block archiver cache stats", "bodyCache", c.bodyCache.Len(), "headerCache", c.headerCache.Len(), "hashCache", c.hashCache.Len())
	}
}
