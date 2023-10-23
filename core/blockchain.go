// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package core implements the Ethereum consensus protocol.
package core

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	exlru "github.com/hashicorp/golang-lru"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/monitor"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/syncx"
	"github.com/ethereum/go-ethereum/internal/version"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/triedb/pathdb"
	"golang.org/x/exp/slices"
)

var (
	headBlockGauge     = metrics.NewRegisteredGauge("chain/head/block", nil)
	headHeaderGauge    = metrics.NewRegisteredGauge("chain/head/header", nil)
	headFastBlockGauge = metrics.NewRegisteredGauge("chain/head/receipt", nil)

	justifiedBlockGauge = metrics.NewRegisteredGauge("chain/head/justified", nil)
	finalizedBlockGauge = metrics.NewRegisteredGauge("chain/head/finalized", nil)

	accountReadTimer   = metrics.NewRegisteredTimer("chain/account/reads", nil)
	accountHashTimer   = metrics.NewRegisteredTimer("chain/account/hashes", nil)
	accountUpdateTimer = metrics.NewRegisteredTimer("chain/account/updates", nil)
	accountCommitTimer = metrics.NewRegisteredTimer("chain/account/commits", nil)

	storageReadTimer   = metrics.NewRegisteredTimer("chain/storage/reads", nil)
	storageHashTimer   = metrics.NewRegisteredTimer("chain/storage/hashes", nil)
	storageUpdateTimer = metrics.NewRegisteredTimer("chain/storage/updates", nil)
	storageCommitTimer = metrics.NewRegisteredTimer("chain/storage/commits", nil)

	snapshotAccountReadTimer = metrics.NewRegisteredTimer("chain/snapshot/account/reads", nil)
	snapshotStorageReadTimer = metrics.NewRegisteredTimer("chain/snapshot/storage/reads", nil)
	snapshotCommitTimer      = metrics.NewRegisteredTimer("chain/snapshot/commits", nil)

	triedbCommitTimer = metrics.NewRegisteredTimer("chain/triedb/commits", nil)

	blockInsertTimer     = metrics.NewRegisteredTimer("chain/inserts", nil)
	blockValidationTimer = metrics.NewRegisteredTimer("chain/validation", nil)
	blockExecutionTimer  = metrics.NewRegisteredTimer("chain/execution", nil)
	blockWriteTimer      = metrics.NewRegisteredTimer("chain/write", nil)

	blockReorgMeter     = metrics.NewRegisteredMeter("chain/reorg/executes", nil)
	blockReorgAddMeter  = metrics.NewRegisteredMeter("chain/reorg/add", nil)
	blockReorgDropMeter = metrics.NewRegisteredMeter("chain/reorg/drop", nil)

	errStateRootVerificationFailed = errors.New("state root verification failed")
	errInsertionInterrupted        = errors.New("insertion is interrupted")
	errChainStopped                = errors.New("blockchain is stopped")
	errInvalidOldChain             = errors.New("invalid old chain")
	errInvalidNewChain             = errors.New("invalid new chain")
)

const (
	bodyCacheLimit      = 256
	blockCacheLimit     = 256
	diffLayerCacheLimit = 1024
	receiptsCacheLimit  = 10000
	txLookupCacheLimit  = 1024
	maxBadBlockLimit    = 16
	maxFutureBlocks     = 256
	maxTimeFutureBlocks = 30
	TriesInMemory       = 128
	maxBeyondBlocks     = 2048
	prefetchTxNumber    = 100

	diffLayerFreezerRecheckInterval = 3 * time.Second
	maxDiffForkDist                 = 11 // Maximum allowed backward distance from the chain head

	rewindBadBlockInterval = 1 * time.Second

	// BlockChainVersion ensures that an incompatible database forces a resync from scratch.
	//
	// Changelog:
	//
	// - Version 4
	//   The following incompatible database changes were added:
	//   * the `BlockNumber`, `TxHash`, `TxIndex`, `BlockHash` and `Index` fields of log are deleted
	//   * the `Bloom` field of receipt is deleted
	//   * the `BlockIndex` and `TxIndex` fields of txlookup are deleted
	// - Version 5
	//  The following incompatible database changes were added:
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are no longer stored for a receipt
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are computed by looking up the
	//      receipts' corresponding block
	// - Version 6
	//  The following incompatible database changes were added:
	//    * Transaction lookup information stores the corresponding block number instead of block hash
	// - Version 7
	//  The following incompatible database changes were added:
	//    * Use freezer as the ancient database to maintain all ancient data
	// - Version 8
	//  The following incompatible database changes were added:
	//    * New scheme for contract code in order to separate the codes and trie nodes
	BlockChainVersion uint64 = 8
)

// CacheConfig contains the configuration values for the trie database
// and state snapshot these are resident in a blockchain.
type CacheConfig struct {
	TrieCleanLimit      int           // Memory allowance (MB) to use for caching trie nodes in memory
	TrieCleanNoPrefetch bool          // Whether to disable heuristic state prefetching for followup blocks
	TrieDirtyLimit      int           // Memory limit (MB) at which to start flushing dirty trie nodes to disk
	TrieDirtyDisabled   bool          // Whether to disable trie write caching and GC altogether (archive node)
	TrieTimeLimit       time.Duration // Time limit after which to flush the current in-memory trie to disk
	SnapshotLimit       int           // Memory allowance (MB) to use for caching snapshot entries in memory
	Preimages           bool          // Whether to store preimage of trie key to the disk
	TriesInMemory       uint64        // How many tries keeps in memory
	NoTries             bool          // Insecure settings. Do not have any tries in databases if enabled.
	StateHistory        uint64        // Number of blocks from head whose state histories are reserved.
	StateScheme         string        // Scheme used to store ethereum states and merkle tree nodes on top

	SnapshotNoBuild bool // Whether the background generation is allowed
	SnapshotWait    bool // Wait for snapshot construction on startup. TODO(karalabe): This is a dirty hack for testing, nuke it
}

// triedbConfig derives the configures for trie database.
func (c *CacheConfig) triedbConfig() *trie.Config {
	config := &trie.Config{
		Cache:     c.TrieCleanLimit,
		Preimages: c.Preimages,
		NoTries:   c.NoTries,
	}
	if c.StateScheme == rawdb.HashScheme {
		config.HashDB = &hashdb.Config{
			CleanCacheSize: c.TrieCleanLimit * 1024 * 1024,
		}
	}
	if c.StateScheme == rawdb.PathScheme {
		config.PathDB = &pathdb.Config{
			StateHistory:   c.StateHistory,
			CleanCacheSize: c.TrieCleanLimit * 1024 * 1024,
			DirtyCacheSize: c.TrieDirtyLimit * 1024 * 1024,
		}
	}
	return config
}

// defaultCacheConfig are the default caching values if none are specified by the
// user (also used during testing).
var defaultCacheConfig = &CacheConfig{
	TrieCleanLimit: 256,
	TrieDirtyLimit: 256,
	TrieTimeLimit:  5 * time.Minute,
	SnapshotLimit:  256,
	TriesInMemory:  128,
	SnapshotWait:   true,
	StateScheme:    rawdb.HashScheme,
}

// DefaultCacheConfigWithScheme returns a deep copied default cache config with
// a provided trie node scheme.
func DefaultCacheConfigWithScheme(scheme string) *CacheConfig {
	config := *defaultCacheConfig
	config.StateScheme = scheme
	return &config
}

type BlockChainOption func(*BlockChain) (*BlockChain, error)

// BlockChain represents the canonical chain given a database with a genesis
// block. The Blockchain manages chain imports, reverts, chain reorganisations.
//
// Importing blocks in to the block chain happens according to the set of rules
// defined by the two stage Validator. Processing of blocks is done using the
// Processor which processes the included transaction. The validation of the state
// is done in the second part of the Validator. Failing results in aborting of
// the import.
//
// The BlockChain also helps in returning blocks from **any** chain included
// in the database as well as blocks that represents the canonical chain. It's
// important to note that GetBlock can return any block and does not need to be
// included in the canonical one where as GetBlockByNumber always represents the
// canonical chain.
type BlockChain struct {
	chainConfig *params.ChainConfig // Chain & network configuration
	cacheConfig *CacheConfig        // Cache configuration for pruning

	db            ethdb.Database                   // Low level persistent database to store final content in
	snaps         *snapshot.Tree                   // Snapshot tree for fast trie leaf access
	triegc        *prque.Prque[int64, common.Hash] // Priority queue mapping block numbers to tries to gc
	gcproc        time.Duration                    // Accumulates canonical block processing for trie dumping
	commitLock    sync.Mutex                       // CommitLock is used to protect above field from being modified concurrently
	lastWrite     uint64                           // Last block when the state was flushed
	flushInterval atomic.Int64                     // Time interval (processing time) after which to flush a state
	triedb        *trie.Database                   // The database handler for maintaining trie nodes.
	stateCache    state.Database                   // State database to reuse between imports (contains state cache)

	// txLookupLimit is the maximum number of blocks from head whose tx indices
	// are reserved:
	//  * 0:   means no limit and regenerate any missing indexes
	//  * N:   means N block limit [HEAD-N+1, HEAD] and delete extra indexes
	//  * nil: disable tx reindexer/deleter, but still index new blocks
	txLookupLimit uint64
	triesInMemory uint64

	hc                  *HeaderChain
	rmLogsFeed          event.Feed
	chainFeed           event.Feed
	chainSideFeed       event.Feed
	chainHeadFeed       event.Feed
	chainBlockFeed      event.Feed
	logsFeed            event.Feed
	blockProcFeed       event.Feed
	finalizedHeaderFeed event.Feed
	scope               event.SubscriptionScope
	genesisBlock        *types.Block

	// This mutex synchronizes chain write operations.
	// Readers don't need to take it, they can just read the database.
	chainmu *syncx.ClosableMutex

	highestVerifiedHeader atomic.Pointer[types.Header]
	currentBlock          atomic.Pointer[types.Header] // Current head of the chain
	currentSnapBlock      atomic.Pointer[types.Header] // Current head of snap-sync

	bodyCache     *lru.Cache[common.Hash, *types.Body]
	bodyRLPCache  *lru.Cache[common.Hash, rlp.RawValue]
	receiptsCache *lru.Cache[common.Hash, []*types.Receipt]
	blockCache    *lru.Cache[common.Hash, *types.Block]
	txLookupCache *lru.Cache[common.Hash, *rawdb.LegacyTxLookupEntry]

	// future blocks are blocks added for later processing
	futureBlocks *lru.Cache[common.Hash, *types.Block]
	// Cache for the blocks that failed to pass MPT root verification
	badBlockCache *lru.Cache[common.Hash, time.Time]

	// trusted diff layers
	diffLayerCache             *exlru.Cache                          // Cache for the diffLayers
	diffLayerChanCache         *exlru.Cache                          // Cache for the difflayer channel
	diffQueue                  *prque.Prque[int64, *types.DiffLayer] // A Priority queue to store recent diff layer
	diffQueueBuffer            chan *types.DiffLayer
	diffLayerFreezerBlockLimit uint64

	wg            sync.WaitGroup //
	quit          chan struct{}  // shutdown signal, closed in Stop.
	stopping      atomic.Bool    // false if chain is running, true when stopped
	procInterrupt atomic.Bool    // interrupt signaler for block processing

	engine     consensus.Engine
	prefetcher Prefetcher
	validator  Validator // Block and state validator interface
	processor  Processor // Block transaction processor interface
	forker     *ForkChoice
	vmConfig   vm.Config
	pipeCommit bool

	// monitor
	doubleSignMonitor *monitor.DoubleSignMonitor
}

// NewBlockChain returns a fully initialised block chain using information
// available in the database. It initialises the default Ethereum Validator and
// Processor.
func NewBlockChain(db ethdb.Database, cacheConfig *CacheConfig, genesis *Genesis, overrides *ChainOverrides, engine consensus.Engine,
	vmConfig vm.Config, shouldPreserve func(block *types.Header) bool, txLookupLimit *uint64,
	options ...BlockChainOption) (*BlockChain, error) {
	if cacheConfig == nil {
		cacheConfig = defaultCacheConfig
	}
	if cacheConfig.TriesInMemory != 128 {
		log.Warn("TriesInMemory isn't the default value(128), you need specify exact same TriesInMemory when prune data",
			"triesInMemory", cacheConfig.TriesInMemory)
	}

	diffLayerCache, _ := exlru.New(diffLayerCacheLimit)
	diffLayerChanCache, _ := exlru.New(diffLayerCacheLimit)

	// Open trie database with provided config
	triedb := trie.NewDatabase(db, cacheConfig.triedbConfig())
	// Setup the genesis block, commit the provided genesis specification
	// to database if the genesis block is not present yet, or load the
	// stored one from database.
	chainConfig, genesisHash, genesisErr := SetupGenesisBlockWithOverride(db, triedb, genesis, overrides)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)
	// Description of chainConfig is empty now
	/*
		log.Info("")
		log.Info(strings.Repeat("-", 153))
		for _, line := range strings.Split(chainConfig.Description(), "\n") {
			log.Info(line)
		}
		log.Info(strings.Repeat("-", 153))
		log.Info("")
	*/

	bc := &BlockChain{
		chainConfig:        chainConfig,
		cacheConfig:        cacheConfig,
		db:                 db,
		triedb:             triedb,
		triegc:             prque.New[int64, common.Hash](nil),
		quit:               make(chan struct{}),
		triesInMemory:      cacheConfig.TriesInMemory,
		chainmu:            syncx.NewClosableMutex(),
		bodyCache:          lru.NewCache[common.Hash, *types.Body](bodyCacheLimit),
		bodyRLPCache:       lru.NewCache[common.Hash, rlp.RawValue](bodyCacheLimit),
		receiptsCache:      lru.NewCache[common.Hash, []*types.Receipt](receiptsCacheLimit),
		blockCache:         lru.NewCache[common.Hash, *types.Block](blockCacheLimit),
		txLookupCache:      lru.NewCache[common.Hash, *rawdb.LegacyTxLookupEntry](txLookupCacheLimit),
		futureBlocks:       lru.NewCache[common.Hash, *types.Block](maxFutureBlocks),
		badBlockCache:      lru.NewCache[common.Hash, time.Time](maxBadBlockLimit),
		diffLayerCache:     diffLayerCache,
		diffLayerChanCache: diffLayerChanCache,
		engine:             engine,
		vmConfig:           vmConfig,
		diffQueue:          prque.New[int64, *types.DiffLayer](nil),
		diffQueueBuffer:    make(chan *types.DiffLayer),
	}
	bc.flushInterval.Store(int64(cacheConfig.TrieTimeLimit))
	bc.forker = NewForkChoice(bc, shouldPreserve)
	bc.stateCache = state.NewDatabaseWithNodeDB(bc.db, bc.triedb)
	bc.validator = NewBlockValidator(chainConfig, bc, engine)
	bc.prefetcher = NewStatePrefetcher(chainConfig, bc, engine)
	bc.processor = NewStateProcessor(chainConfig, bc, engine)

	var err error
	bc.hc, err = NewHeaderChain(db, chainConfig, engine, bc.insertStopped)
	if err != nil {
		return nil, err
	}
	bc.genesisBlock = bc.GetBlockByNumber(0)
	if bc.genesisBlock == nil {
		return nil, ErrNoGenesis
	}

	bc.highestVerifiedHeader.Store(nil)
	bc.currentBlock.Store(nil)
	bc.currentSnapBlock.Store(nil)

	// If Geth is initialized with an external ancient store, re-initialize the
	// missing chain indexes and chain flags. This procedure can survive crash
	// and can be resumed in next restart since chain flags are updated in last step.
	if bc.empty() {
		rawdb.InitDatabaseFromFreezer(bc.db)
	}
	// Load blockchain states from disk
	if err := bc.loadLastState(); err != nil {
		return nil, err
	}
	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if !bc.stateCache.NoTries() && !bc.HasState(head.Root) {
		// Head state is missing, before the state recovery, find out the
		// disk layer point of snapshot(if it's enabled). Make sure the
		// rewound point is lower than disk layer.
		var diskRoot common.Hash
		if bc.cacheConfig.SnapshotLimit > 0 {
			diskRoot = rawdb.ReadSnapshotRoot(bc.db)
		} else if bc.triedb.Scheme() == rawdb.PathScheme {
			_, diskRoot = rawdb.ReadAccountTrieNode(bc.db, nil)
		}
		if diskRoot != (common.Hash{}) {
			log.Warn("Head state missing, repairing", "number", head.Number, "hash", head.Hash(), "snaproot", diskRoot)

			snapDisk, err := bc.setHeadBeyondRoot(head.Number.Uint64(), 0, diskRoot, true)
			if err != nil {
				return nil, err
			}
			// Chain rewound, persist old snapshot number to indicate recovery procedure
			if snapDisk != 0 {
				rawdb.WriteSnapshotRecoveryNumber(bc.db, snapDisk)
			}
		} else {
			log.Warn("Head state missing, repairing", "number", head.Number, "hash", head.Hash())
			if _, err := bc.setHeadBeyondRoot(head.Number.Uint64(), 0, common.Hash{}, true); err != nil {
				return nil, err
			}
		}
	}
	// Ensure that a previous crash in SetHead doesn't leave extra ancients
	if frozen, err := bc.db.ItemAmountInAncient(); err == nil && frozen > 0 {
		frozen, err = bc.db.Ancients()
		if err != nil {
			return nil, err
		}
		var (
			needRewind bool
			low        uint64
		)
		// The head full block may be rolled back to a very low height due to
		// blockchain repair. If the head full block is even lower than the ancient
		// chain, truncate the ancient store.
		fullBlock := bc.CurrentBlock()
		if fullBlock != nil && fullBlock.Hash() != bc.genesisBlock.Hash() && fullBlock.Number.Uint64() < frozen-1 {
			needRewind = true
			low = fullBlock.Number.Uint64()
		}
		// In snap sync, it may happen that ancient data has been written to the
		// ancient store, but the LastFastBlock has not been updated, truncate the
		// extra data here.
		snapBlock := bc.CurrentSnapBlock()
		if snapBlock != nil && snapBlock.Number.Uint64() < frozen-1 {
			needRewind = true
			if snapBlock.Number.Uint64() < low || low == 0 {
				low = snapBlock.Number.Uint64()
			}
		}
		if needRewind {
			log.Error("Truncating ancient chain", "from", bc.CurrentHeader().Number.Uint64(), "to", low)
			if err := bc.SetHead(low); err != nil {
				return nil, err
			}
		}
	}
	// The first thing the node will do is reconstruct the verification data for
	// the head block (ethash cache or clique voting snapshot). Might as well do
	// it in advance.
	bc.engine.VerifyHeader(bc, bc.CurrentHeader())

	// Check the current state of the block hashes and make sure that we do not have any of the bad blocks in our chain
	for hash := range BadHashes {
		if header := bc.GetHeaderByHash(hash); header != nil {
			// get the canonical block corresponding to the offending header's number
			headerByNumber := bc.GetHeaderByNumber(header.Number.Uint64())
			// make sure the headerByNumber (if present) is in our current canonical chain
			if headerByNumber != nil && headerByNumber.Hash() == header.Hash() {
				log.Error("Found bad hash, rewinding chain", "number", header.Number, "hash", header.ParentHash)
				if err := bc.SetHead(header.Number.Uint64() - 1); err != nil {
					return nil, err
				}
				log.Error("Chain rewind was successful, resuming normal operation")
			}
		}
	}

	// Load any existing snapshot, regenerating it if loading failed
	if bc.cacheConfig.SnapshotLimit > 0 {
		// If the chain was rewound past the snapshot persistent layer (causing
		// a recovery block number to be persisted to disk), check if we're still
		// in recovery mode and in that case, don't invalidate the snapshot on a
		// head mismatch.
		var recover bool

		head := bc.CurrentBlock()
		if layer := rawdb.ReadSnapshotRecoveryNumber(bc.db); layer != nil && *layer >= head.Number.Uint64() {
			log.Warn("Enabling snapshot recovery", "chainhead", head.Number, "diskbase", *layer)
			recover = true
		}
		snapconfig := snapshot.Config{
			CacheSize:  bc.cacheConfig.SnapshotLimit,
			Recovery:   recover,
			NoBuild:    bc.cacheConfig.SnapshotNoBuild,
			AsyncBuild: !bc.cacheConfig.SnapshotWait,
		}
		bc.snaps, _ = snapshot.New(snapconfig, bc.db, bc.triedb, head.Root, int(bc.cacheConfig.TriesInMemory), bc.stateCache.NoTries())
	}
	// do options before start any routine
	for _, option := range options {
		bc, err = option(bc)
		if err != nil {
			return nil, err
		}
	}
	// Start future block processor.
	bc.wg.Add(1)
	go bc.updateFutureBlocks()

	// Need persist and prune diff layer
	if bc.db.DiffStore() != nil {
		bc.wg.Add(1)
		go bc.trustedDiffLayerLoop()
	}
	if bc.pipeCommit {
		// check current block and rewind invalid one
		bc.wg.Add(1)
		go bc.rewindInvalidHeaderBlockLoop()
	}

	if bc.doubleSignMonitor != nil {
		bc.wg.Add(1)
		go bc.startDoubleSignMonitor()
	}

	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		if compat.RewindToTime > 0 {
			bc.SetHeadWithTimestamp(compat.RewindToTime)
		} else {
			bc.SetHead(compat.RewindToBlock)
		}
		rawdb.WriteChainConfig(db, genesisHash, chainConfig)
	}
	// Start tx indexer/unindexer if required.
	if txLookupLimit != nil {
		bc.txLookupLimit = *txLookupLimit

		bc.wg.Add(1)
		go bc.maintainTxIndex()
	}
	return bc, nil
}

// GetVMConfig returns the block chain VM config.
func (bc *BlockChain) GetVMConfig() *vm.Config {
	return &bc.vmConfig
}

func (bc *BlockChain) cacheReceipts(hash common.Hash, receipts types.Receipts) {
	// TODO, This is a hot fix for the block hash of logs is `0x0000000000000000000000000000000000000000000000000000000000000000` for system tx
	// Please check details in https://github.com/bnb-chain/bsc/issues/443
	// This is a temporary fix, the official fix should be a hard fork.
	const possibleSystemReceipts = 3 // One slash tx, two reward distribute txs.
	numOfReceipts := len(receipts)
	for i := numOfReceipts - 1; i >= 0 && i >= numOfReceipts-possibleSystemReceipts; i-- {
		for j := 0; j < len(receipts[i].Logs); j++ {
			receipts[i].Logs[j].BlockHash = hash
		}
	}
	bc.receiptsCache.Add(hash, receipts)
}

func (bc *BlockChain) cacheDiffLayer(diffLayer *types.DiffLayer, diffLayerCh chan struct{}) {
	// The difflayer in the system is stored by the map structure,
	// so it will be out of order.
	// It must be sorted first and then cached,
	// otherwise the DiffHash calculated by different nodes will be inconsistent
	sort.SliceStable(diffLayer.Codes, func(i, j int) bool {
		return diffLayer.Codes[i].Hash.Hex() < diffLayer.Codes[j].Hash.Hex()
	})
	sort.SliceStable(diffLayer.Destructs, func(i, j int) bool {
		return diffLayer.Destructs[i].Hex() < (diffLayer.Destructs[j].Hex())
	})
	sort.SliceStable(diffLayer.Accounts, func(i, j int) bool {
		return diffLayer.Accounts[i].Account.Hex() < diffLayer.Accounts[j].Account.Hex()
	})
	sort.SliceStable(diffLayer.Storages, func(i, j int) bool {
		return diffLayer.Storages[i].Account.Hex() < diffLayer.Storages[j].Account.Hex()
	})
	for index := range diffLayer.Storages {
		// Sort keys and vals by key.
		sort.Sort(&diffLayer.Storages[index])
	}

	if bc.diffLayerCache.Len() >= diffLayerCacheLimit {
		bc.diffLayerCache.RemoveOldest()
	}

	bc.diffLayerCache.Add(diffLayer.BlockHash, diffLayer)
	close(diffLayerCh)

	if bc.db.DiffStore() != nil {
		// push to priority queue before persisting
		bc.diffQueueBuffer <- diffLayer
	}
}

func (bc *BlockChain) cacheBlock(hash common.Hash, block *types.Block) {
	bc.blockCache.Add(hash, block)
}

// empty returns an indicator whether the blockchain is empty.
// Note, it's a special case that we connect a non-empty ancient
// database with an empty node, so that we can plugin the ancient
// into node seamlessly.
func (bc *BlockChain) empty() bool {
	genesis := bc.genesisBlock.Hash()
	for _, hash := range []common.Hash{rawdb.ReadHeadBlockHash(bc.db), rawdb.ReadHeadHeaderHash(bc.db), rawdb.ReadHeadFastBlockHash(bc.db)} {
		if hash != genesis {
			return false
		}
	}
	return true
}

// GetJustifiedNumber returns the highest justified blockNumber on the branch including and before `header`.
func (bc *BlockChain) GetJustifiedNumber(header *types.Header) uint64 {
	if p, ok := bc.engine.(consensus.PoSA); ok {
		justifiedBlockNumber, _, err := p.GetJustifiedNumberAndHash(bc, header)
		if err == nil {
			return justifiedBlockNumber
		}
	}
	// return 0 when err!=nil
	// so the input `header` will at a disadvantage during reorg
	return 0
}

// getFinalizedNumber returns the highest finalized number before the specific block.
func (bc *BlockChain) getFinalizedNumber(header *types.Header) uint64 {
	if p, ok := bc.engine.(consensus.PoSA); ok {
		if finalizedHeader := p.GetFinalizedHeader(bc, header); finalizedHeader != nil {
			return finalizedHeader.Number.Uint64()
		}
	}

	return 0
}

// loadLastState loads the last known chain state from the database. This method
// assumes that the chain manager mutex is held.
func (bc *BlockChain) loadLastState() error {
	// Restore the last known head block
	head := rawdb.ReadHeadBlockHash(bc.db)
	if head == (common.Hash{}) {
		// Corrupt or empty database, init from scratch
		log.Warn("Empty database, resetting chain")
		return bc.Reset()
	}
	// Make sure the entire head block is available
	headBlock := bc.GetBlockByHash(head)
	if headBlock == nil {
		// Corrupt or empty database, init from scratch
		log.Warn("Head block missing, resetting chain", "hash", head)
		return bc.Reset()
	}

	// Everything seems to be fine, set as the head block
	bc.currentBlock.Store(headBlock.Header())
	headBlockGauge.Update(int64(headBlock.NumberU64()))
	justifiedBlockGauge.Update(int64(bc.GetJustifiedNumber(headBlock.Header())))
	finalizedBlockGauge.Update(int64(bc.getFinalizedNumber(headBlock.Header())))

	// Restore the last known head header
	headHeader := headBlock.Header()
	if head := rawdb.ReadHeadHeaderHash(bc.db); head != (common.Hash{}) {
		if header := bc.GetHeaderByHash(head); header != nil {
			headHeader = header
		}
	}
	bc.hc.SetCurrentHeader(headHeader)

	// Restore the last known head snap block
	bc.currentSnapBlock.Store(headBlock.Header())
	headFastBlockGauge.Update(int64(headBlock.NumberU64()))

	if head := rawdb.ReadHeadFastBlockHash(bc.db); head != (common.Hash{}) {
		if block := bc.GetBlockByHash(head); block != nil {
			bc.currentSnapBlock.Store(block.Header())
			headFastBlockGauge.Update(int64(block.NumberU64()))
		}
	}

	// Issue a status log for the user
	var (
		currentSnapBlock = bc.CurrentSnapBlock()

		headerTd = bc.GetTd(headHeader.Hash(), headHeader.Number.Uint64())
		blockTd  = bc.GetTd(headBlock.Hash(), headBlock.NumberU64())
	)
	if headHeader.Hash() != headBlock.Hash() {
		log.Info("Loaded most recent local header", "number", headHeader.Number, "hash", headHeader.Hash(), "td", headerTd, "age", common.PrettyAge(time.Unix(int64(headHeader.Time), 0)))
	}
	log.Info("Loaded most recent local block", "number", headBlock.Number(), "hash", headBlock.Hash(), "td", blockTd, "age", common.PrettyAge(time.Unix(int64(headBlock.Time()), 0)))
	if headBlock.Hash() != currentSnapBlock.Hash() {
		snapTd := bc.GetTd(currentSnapBlock.Hash(), currentSnapBlock.Number.Uint64())
		log.Info("Loaded most recent local snap block", "number", currentSnapBlock.Number, "hash", currentSnapBlock.Hash(), "td", snapTd, "age", common.PrettyAge(time.Unix(int64(currentSnapBlock.Time), 0)))
	}
	if posa, ok := bc.engine.(consensus.PoSA); ok {
		if currentFinalizedHeader := posa.GetFinalizedHeader(bc, headHeader); currentFinalizedHeader != nil {
			if currentFinalizedBlock := bc.GetBlockByHash(currentFinalizedHeader.Hash()); currentFinalizedBlock != nil {
				finalTd := bc.GetTd(currentFinalizedBlock.Hash(), currentFinalizedBlock.NumberU64())
				log.Info("Loaded most recent local finalized block", "number", currentFinalizedBlock.Number(), "hash", currentFinalizedBlock.Hash(), "td", finalTd, "age", common.PrettyAge(time.Unix(int64(currentFinalizedBlock.Time()), 0)))
			}
		}
	}

	if pivot := rawdb.ReadLastPivotNumber(bc.db); pivot != nil {
		log.Info("Loaded last snap-sync pivot marker", "number", *pivot)
	}
	return nil
}

// SetHead rewinds the local chain to a new head. Depending on whether the node
// was snap synced or full synced and in which state, the method will try to
// delete minimal data from disk whilst retaining chain consistency.
func (bc *BlockChain) SetHead(head uint64) error {
	if _, err := bc.setHeadBeyondRoot(head, 0, common.Hash{}, false); err != nil {
		return err
	}
	// Send chain head event to update the transaction pool
	header := bc.CurrentBlock()
	block := bc.GetBlock(header.Hash(), header.Number.Uint64())
	if block == nil {
		// This should never happen. In practice, previsouly currentBlock
		// contained the entire block whereas now only a "marker", so there
		// is an ever so slight chance for a race we should handle.
		log.Error("Current block not found in database", "block", header.Number, "hash", header.Hash())
		return fmt.Errorf("current block missing: #%d [%x..]", header.Number, header.Hash().Bytes()[:4])
	}
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
	return nil
}

// SetHeadWithTimestamp rewinds the local chain to a new head that has at max
// the given timestamp. Depending on whether the node was snap synced or full
// synced and in which state, the method will try to delete minimal data from
// disk whilst retaining chain consistency.
func (bc *BlockChain) SetHeadWithTimestamp(timestamp uint64) error {
	if _, err := bc.setHeadBeyondRoot(0, timestamp, common.Hash{}, false); err != nil {
		return err
	}
	// Send chain head event to update the transaction pool
	header := bc.CurrentBlock()
	block := bc.GetBlock(header.Hash(), header.Number.Uint64())
	if block == nil {
		// This should never happen. In practice, previsouly currentBlock
		// contained the entire block whereas now only a "marker", so there
		// is an ever so slight chance for a race we should handle.
		log.Error("Current block not found in database", "block", header.Number, "hash", header.Hash())
		return fmt.Errorf("current block missing: #%d [%x..]", header.Number, header.Hash().Bytes()[:4])
	}
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
	return nil
}

func (bc *BlockChain) tryRewindBadBlocks() {
	if !bc.chainmu.TryLock() {
		return
	}
	defer bc.chainmu.Unlock()
	block := bc.CurrentBlock()
	snaps := bc.snaps
	// Verified and Result is false
	if snaps != nil && snaps.Snapshot(block.Root) != nil &&
		snaps.Snapshot(block.Root).Verified() && !snaps.Snapshot(block.Root).WaitAndGetVerifyRes() {
		// Rewind by one block
		log.Warn("current block verified failed, rewind to its parent", "height", block.Number.Uint64(), "hash", block.Hash())
		bc.futureBlocks.Remove(block.Hash())
		bc.badBlockCache.Add(block.Hash(), time.Now())
		bc.diffLayerCache.Remove(block.Hash())
		bc.reportBlock(bc.GetBlockByHash(block.Hash()), nil, errStateRootVerificationFailed)
		bc.setHeadBeyondRoot(block.Number.Uint64()-1, 0, common.Hash{}, false)
	}
}

// setHeadBeyondRoot rewinds the local chain to a new head with the extra condition
// that the rewind must pass the specified state root. This method is meant to be
// used when rewinding with snapshots enabled to ensure that we go back further than
// persistent disk layer. Depending on whether the node was snap synced or full, and
// in which state, the method will try to delete minimal data from disk whilst
// retaining chain consistency.
//
// The method also works in timestamp mode if `head == 0` but `time != 0`. In that
// case blocks are rolled back until the new head becomes older or equal to the
// requested time. If both `head` and `time` is 0, the chain is rewound to genesis.
//
// The method returns the block number where the requested root cap was found.
func (bc *BlockChain) setHeadBeyondRoot(head uint64, time uint64, root common.Hash, repair bool) (uint64, error) {
	if !bc.chainmu.TryLock() {
		return 0, errChainStopped
	}
	defer bc.chainmu.Unlock()

	// Track the block number of the requested root hash
	var rootNumber uint64 // (no root == always 0)

	// Retrieve the last pivot block to short circuit rollbacks beyond it and the
	// current freezer limit to start nuking id underflown
	pivot := rawdb.ReadLastPivotNumber(bc.db)
	frozen, _ := bc.db.Ancients()

	// resetState resets the persistent state to genesis if it's not available.
	resetState := func() {
		// Short circuit if the genesis state is already present.
		if bc.HasState(bc.genesisBlock.Root()) {
			return
		}
		// Reset the state database to empty for committing genesis state.
		// Note, it should only happen in path-based scheme and Reset function
		// is also only call-able in this mode.
		if bc.triedb.Scheme() == rawdb.PathScheme {
			if err := bc.triedb.Reset(types.EmptyRootHash); err != nil {
				log.Crit("Failed to clean state", "err", err) // Shouldn't happen
			}
		}
		// Write genesis state into database.
		if err := CommitGenesisState(bc.db, bc.triedb, bc.genesisBlock.Hash()); err != nil {
			log.Crit("Failed to commit genesis state", "err", err)
		}
	}
	updateFn := func(db ethdb.KeyValueWriter, header *types.Header) (*types.Header, bool) {
		// Rewind the blockchain, ensuring we don't end up with a stateless head
		// block. Note, depth equality is permitted to allow using SetHead as a
		// chain reparation mechanism without deleting any data!
		if currentBlock := bc.CurrentBlock(); currentBlock != nil && header.Number.Uint64() <= currentBlock.Number.Uint64() {
			newHeadBlock := bc.GetBlock(header.Hash(), header.Number.Uint64())
			lastBlockNum := header.Number.Uint64()
			if newHeadBlock == nil {
				log.Error("Gap in the chain, rewinding to genesis", "number", header.Number, "hash", header.Hash())
				newHeadBlock = bc.genesisBlock
				resetState()
			} else {
				// Block exists, keep rewinding until we find one with state,
				// keeping rewinding until we exceed the optional threshold
				// root hash
				beyondRoot := (root == common.Hash{}) // Flag whether we're beyond the requested root (no root, always true)
				enoughBeyondCount := false
				beyondCount := 0
				for {
					beyondCount++
					// If a root threshold was requested but not yet crossed, check
					if root != (common.Hash{}) && !beyondRoot && newHeadBlock.Root() == root {
						beyondRoot, rootNumber = true, newHeadBlock.NumberU64()
					}
					if !bc.HasState(newHeadBlock.Root()) && !bc.stateRecoverable(newHeadBlock.Root()) {
						log.Trace("Block state missing, rewinding further", "number", newHeadBlock.NumberU64(), "hash", newHeadBlock.Hash())
						if pivot == nil || newHeadBlock.NumberU64() > *pivot {
							parent := bc.GetBlock(newHeadBlock.ParentHash(), newHeadBlock.NumberU64()-1)
							if parent != nil {
								newHeadBlock = parent
								continue
							}
							log.Error("Missing block in the middle, aiming genesis", "number", newHeadBlock.NumberU64()-1, "hash", newHeadBlock.ParentHash())
							newHeadBlock = bc.genesisBlock
						} else {
							log.Trace("Rewind passed pivot, aiming genesis", "number", newHeadBlock.NumberU64(), "hash", newHeadBlock.Hash(), "pivot", *pivot)
							newHeadBlock = bc.genesisBlock
						}
					}
					if beyondRoot || (enoughBeyondCount && root != common.Hash{}) || newHeadBlock.NumberU64() == 0 {
						if enoughBeyondCount && (root != common.Hash{}) && rootNumber == 0 {
							for {
								lastBlockNum++
								block := bc.GetBlockByNumber(lastBlockNum)
								if block == nil {
									break
								}
								if block.Root() == root {
									rootNumber = block.NumberU64()
									break
								}
							}
						}
						if newHeadBlock.NumberU64() == 0 {
							resetState()
						} else if !bc.HasState(newHeadBlock.Root()) {
							// Rewind to a block with recoverable state. If the state is
							// missing, run the state recovery here.
							if err := bc.triedb.Recover(newHeadBlock.Root()); err != nil {
								log.Crit("Failed to rollback state", "err", err) // Shouldn't happen
							}
						}
						log.Info("Rewound to block with state", "number", newHeadBlock.NumberU64(), "hash", newHeadBlock.Hash())
						break
					}
					log.Debug("Skipping block with threshold state", "number", newHeadBlock.NumberU64(), "hash", newHeadBlock.Hash(), "root", newHeadBlock.Root())
					newHeadBlock = bc.GetBlock(newHeadBlock.ParentHash(), newHeadBlock.NumberU64()-1) // Keep rewinding
				}
			}
			rawdb.WriteHeadBlockHash(db, newHeadBlock.Hash())

			// Degrade the chain markers if they are explicitly reverted.
			// In theory we should update all in-memory markers in the
			// last step, however the direction of SetHead is from high
			// to low, so it's safe to update in-memory markers directly.
			bc.currentBlock.Store(newHeadBlock.Header())
			headBlockGauge.Update(int64(newHeadBlock.NumberU64()))
			justifiedBlockGauge.Update(int64(bc.GetJustifiedNumber(newHeadBlock.Header())))
			finalizedBlockGauge.Update(int64(bc.getFinalizedNumber(newHeadBlock.Header())))
		}
		// Rewind the snap block in a simpleton way to the target head
		if currentSnapBlock := bc.CurrentSnapBlock(); currentSnapBlock != nil && header.Number.Uint64() < currentSnapBlock.Number.Uint64() {
			newHeadSnapBlock := bc.GetBlock(header.Hash(), header.Number.Uint64())
			// If either blocks reached nil, reset to the genesis state
			if newHeadSnapBlock == nil {
				newHeadSnapBlock = bc.genesisBlock
			}
			rawdb.WriteHeadFastBlockHash(db, newHeadSnapBlock.Hash())

			// Degrade the chain markers if they are explicitly reverted.
			// In theory we should update all in-memory markers in the
			// last step, however the direction of SetHead is from high
			// to low, so it's safe the update in-memory markers directly.
			bc.currentSnapBlock.Store(newHeadSnapBlock.Header())
			headFastBlockGauge.Update(int64(newHeadSnapBlock.NumberU64()))
		}
		var (
			headHeader = bc.CurrentBlock()
			headNumber = headHeader.Number.Uint64()
		)
		// If setHead underflown the freezer threshold and the block processing
		// intent afterwards is full block importing, delete the chain segment
		// between the stateful-block and the sethead target.
		var wipe bool
		if headNumber+1 < frozen {
			wipe = pivot == nil || headNumber >= *pivot
		}
		return headHeader, wipe // Only force wipe if full synced
	}
	// Rewind the header chain, deleting all block bodies until then
	delFn := func(db ethdb.KeyValueWriter, hash common.Hash, num uint64) {
		// Ignore the error here since light client won't hit this path
		frozen, _ := bc.db.Ancients()
		if num+1 <= frozen {
			// Truncate all relative data(header, total difficulty, body, receipt
			// and canonical hash) from ancient store.
			if _, err := bc.db.TruncateHead(num); err != nil {
				log.Crit("Failed to truncate ancient data", "number", num, "err", err)
			}
			// Remove the hash <-> number mapping from the active store.
			rawdb.DeleteHeaderNumber(db, hash)
		} else {
			// Remove relative body and receipts from the active store.
			// The header, total difficulty and canonical hash will be
			// removed in the hc.SetHead function.
			rawdb.DeleteBody(db, hash, num)
			rawdb.DeleteReceipts(db, hash, num)
		}
		// Todo(rjl493456442) txlookup, bloombits, etc
	}
	// If SetHead was only called as a chain reparation method, try to skip
	// touching the header chain altogether, unless the freezer is broken
	if repair {
		if target, force := updateFn(bc.db, bc.CurrentBlock()); force {
			bc.hc.SetHead(target.Number.Uint64(), updateFn, delFn)
		}
	} else {
		// Rewind the chain to the requested head and keep going backwards until a
		// block with a state is found or snap sync pivot is passed
		if time > 0 {
			log.Warn("Rewinding blockchain to timestamp", "target", time)
			bc.hc.SetHeadWithTimestamp(time, updateFn, delFn)
		} else {
			log.Warn("Rewinding blockchain to block", "target", head)
			bc.hc.SetHead(head, updateFn, delFn)
		}
	}
	// Clear out any stale content from the caches
	bc.bodyCache.Purge()
	bc.bodyRLPCache.Purge()
	bc.receiptsCache.Purge()
	bc.blockCache.Purge()
	bc.txLookupCache.Purge()
	bc.futureBlocks.Purge()

	return rootNumber, bc.loadLastState()
}

// SnapSyncCommitHead sets the current head block to the one defined by the hash
// irrelevant what the chain contents were prior.
func (bc *BlockChain) SnapSyncCommitHead(hash common.Hash) error {
	// Make sure that both the block as well at its state trie exists
	block := bc.GetBlockByHash(hash)
	if block == nil {
		return fmt.Errorf("non existent block [%x..]", hash[:4])
	}
	// Reset the trie database with the fresh snap synced state.
	root := block.Root()
	if bc.triedb.Scheme() == rawdb.PathScheme {
		if err := bc.triedb.Reset(root); err != nil {
			return err
		}
	}
	if !bc.HasState(root) {
		return fmt.Errorf("non existent state [%x..]", root[:4])
	}
	// If all checks out, manually set the head block.
	if !bc.chainmu.TryLock() {
		return errChainStopped
	}
	bc.currentBlock.Store(block.Header())
	headBlockGauge.Update(int64(block.NumberU64()))
	justifiedBlockGauge.Update(int64(bc.GetJustifiedNumber(block.Header())))
	finalizedBlockGauge.Update(int64(bc.getFinalizedNumber(block.Header())))
	bc.chainmu.Unlock()

	// Destroy any existing state snapshot and regenerate it in the background,
	// also resuming the normal maintenance of any previously paused snapshot.
	if bc.snaps != nil {
		bc.snaps.Rebuild(root)
	}
	log.Info("Committed new head block", "number", block.Number(), "hash", hash)
	return nil
}

// StateAtWithSharedPool returns a new mutable state based on a particular point in time with sharedStorage
func (bc *BlockChain) StateAtWithSharedPool(root common.Hash) (*state.StateDB, error) {
	return state.NewWithSharedPool(root, bc.stateCache, bc.snaps)
}

// Reset purges the entire blockchain, restoring it to its genesis state.
func (bc *BlockChain) Reset() error {
	return bc.ResetWithGenesisBlock(bc.genesisBlock)
}

// ResetWithGenesisBlock purges the entire blockchain, restoring it to the
// specified genesis state.
func (bc *BlockChain) ResetWithGenesisBlock(genesis *types.Block) error {
	// Dump the entire block chain and purge the caches
	if err := bc.SetHead(0); err != nil {
		return err
	}
	if !bc.chainmu.TryLock() {
		return errChainStopped
	}
	defer bc.chainmu.Unlock()

	// Prepare the genesis block and reinitialise the chain
	batch := bc.db.NewBatch()
	rawdb.WriteTd(batch, genesis.Hash(), genesis.NumberU64(), genesis.Difficulty())
	rawdb.WriteBlock(batch, genesis)
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write genesis block", "err", err)
	}
	bc.writeHeadBlock(genesis)

	// Last update all in-memory chain markers
	bc.genesisBlock = genesis
	bc.currentBlock.Store(bc.genesisBlock.Header())
	headBlockGauge.Update(int64(bc.genesisBlock.NumberU64()))
	justifiedBlockGauge.Update(int64(bc.genesisBlock.NumberU64()))
	finalizedBlockGauge.Update(int64(bc.genesisBlock.NumberU64()))
	bc.hc.SetGenesis(bc.genesisBlock.Header())
	bc.hc.SetCurrentHeader(bc.genesisBlock.Header())
	bc.currentSnapBlock.Store(bc.genesisBlock.Header())
	headFastBlockGauge.Update(int64(bc.genesisBlock.NumberU64()))
	return nil
}

// Export writes the active chain to the given writer.
func (bc *BlockChain) Export(w io.Writer) error {
	return bc.ExportN(w, uint64(0), bc.CurrentBlock().Number.Uint64())
}

// ExportN writes a subset of the active chain to the given writer.
func (bc *BlockChain) ExportN(w io.Writer, first uint64, last uint64) error {
	if first > last {
		return fmt.Errorf("export failed: first (%d) is greater than last (%d)", first, last)
	}
	log.Info("Exporting batch of blocks", "count", last-first+1)

	var (
		parentHash common.Hash
		start      = time.Now()
		reported   = time.Now()
	)
	for nr := first; nr <= last; nr++ {
		block := bc.GetBlockByNumber(nr)
		if block == nil {
			return fmt.Errorf("export failed on #%d: not found", nr)
		}
		if nr > first && block.ParentHash() != parentHash {
			return errors.New("export failed: chain reorg during export")
		}
		parentHash = block.Hash()
		if err := block.EncodeRLP(w); err != nil {
			return err
		}
		if time.Since(reported) >= statsReportLimit {
			log.Info("Exporting blocks", "exported", block.NumberU64()-first, "elapsed", common.PrettyDuration(time.Since(start)))
			reported = time.Now()
		}
	}
	return nil
}

// writeHeadBlock injects a new head block into the current block chain. This method
// assumes that the block is indeed a true head. It will also reset the head
// header and the head snap sync block to this very same block if they are older
// or if they are on a different side chain.
//
// Note, this function assumes that the `mu` mutex is held!
func (bc *BlockChain) writeHeadBlock(block *types.Block) {
	// Add the block to the canonical chain number scheme and mark as the head
	batch := bc.db.NewBatch()
	rawdb.WriteHeadHeaderHash(batch, block.Hash())
	rawdb.WriteHeadFastBlockHash(batch, block.Hash())
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	rawdb.WriteTxLookupEntriesByBlock(batch, block)
	rawdb.WriteHeadBlockHash(batch, block.Hash())

	// Flush the whole batch into the disk, exit the node if failed
	if err := batch.Write(); err != nil {
		log.Crit("Failed to update chain indexes and markers", "err", err)
	}
	// Update all in-memory chain markers in the last step
	bc.hc.SetCurrentHeader(block.Header())

	bc.currentSnapBlock.Store(block.Header())
	headFastBlockGauge.Update(int64(block.NumberU64()))

	bc.currentBlock.Store(block.Header())
	headBlockGauge.Update(int64(block.NumberU64()))
	justifiedBlockGauge.Update(int64(bc.GetJustifiedNumber(block.Header())))
	finalizedBlockGauge.Update(int64(bc.getFinalizedNumber(block.Header())))
}

// stopWithoutSaving stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt. This method stops all running
// goroutines, but does not do all the post-stop work of persisting data.
// OBS! It is generally recommended to use the Stop method!
// This method has been exposed to allow tests to stop the blockchain while simulating
// a crash.
func (bc *BlockChain) stopWithoutSaving() {
	if !bc.stopping.CompareAndSwap(false, true) {
		return
	}

	// Unsubscribe all subscriptions registered from blockchain.
	bc.scope.Close()

	// Signal shutdown to all goroutines.
	close(bc.quit)
	bc.StopInsert()

	// Now wait for all chain modifications to end and persistent goroutines to exit.
	//
	// Note: Close waits for the mutex to become available, i.e. any running chain
	// modification will have exited when Close returns. Since we also called StopInsert,
	// the mutex should become available quickly. It cannot be taken again after Close has
	// returned.
	bc.chainmu.Close()
	bc.wg.Wait()
}

// Stop stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt.
func (bc *BlockChain) Stop() {
	bc.stopWithoutSaving()

	// Ensure that the entirety of the state snapshot is journalled to disk.
	var snapBase common.Hash
	if bc.snaps != nil {
		var err error
		if snapBase, err = bc.snaps.Journal(bc.CurrentBlock().Root); err != nil {
			log.Error("Failed to journal state snapshot", "err", err)
		}
	}
	if bc.triedb.Scheme() == rawdb.PathScheme {
		// Ensure that the in-memory trie nodes are journaled to disk properly.
		if err := bc.triedb.Journal(bc.CurrentBlock().Root); err != nil {
			log.Info("Failed to journal in-memory trie nodes", "err", err)
		}
	} else {
		// Ensure the state of a recent block is also stored to disk before exiting.
		// We're writing three different states to catch different restart scenarios:
		//  - HEAD:     So we don't need to reprocess any blocks in the general case
		//  - HEAD-1:   So we don't do large reorgs if our HEAD becomes an uncle
		//  - HEAD-127: So we have a hard limit on the number of blocks reexecuted
		if !bc.cacheConfig.TrieDirtyDisabled {
			triedb := bc.triedb
		var once sync.Once

			for _, offset := range []uint64{0, 1, TriesInMemory - 1} {
				if number := bc.CurrentBlock().Number.Uint64(); number > offset {
					recent := bc.GetBlockByNumber(number - offset)
					log.Info("Writing cached state to disk", "block", recent.Number(), "hash", recent.Hash(), "root", recent.Root())
					if err := triedb.Commit(recent.Root(), true); err != nil {
						log.Error("Failed to commit recent state trie", "err", err)
					} else {
						rawdb.WriteSafePointBlockNumber(bc.db, recent.NumberU64())
						once.Do(func() {
						  rawdb.WriteHeadBlockHash(bc.db, recent.Hash())
						})
          }
				}
			}
			if snapBase != (common.Hash{}) {
				log.Info("Writing snapshot state to disk", "root", snapBase)
				if err := triedb.Commit(snapBase, true); err != nil {
					log.Error("Failed to commit recent state trie", "err", err)
				} else {
					rawdb.WriteSafePointBlockNumber(bc.db, bc.CurrentBlock().Number.Uint64())
				}
			}

			if snapBase != (common.Hash{}) {
				log.Info("Writing snapshot state to disk", "root", snapBase)
				if err := bc.triedb.Commit(snapBase, true); err != nil {
					log.Error("Failed to commit recent state trie", "err", err)
				} else {
					rawdb.WriteSafePointBlockNumber(bc.db, bc.CurrentBlock().Number.Uint64())
				}
			}
			for !bc.triegc.Empty() {
				triedb.Dereference(bc.triegc.PopItem())
			}
			if size, _ := triedb.Size(); size != 0 {
				log.Error("Dangling trie nodes after full cleanup")
			}
		}
	}
	// Close the trie database, release all the held resources as the last step.
	if err := bc.triedb.Close(); err != nil {
		log.Error("Failed to close trie database", "err", err)
	}
	log.Info("Blockchain stopped")
}

// StopInsert interrupts all insertion methods, causing them to return
// errInsertionInterrupted as soon as possible. Insertion is permanently disabled after
// calling this method.
func (bc *BlockChain) StopInsert() {
	bc.procInterrupt.Store(true)
}

// insertStopped returns true after StopInsert has been called.
func (bc *BlockChain) insertStopped() bool {
	return bc.procInterrupt.Load()
}

func (bc *BlockChain) procFutureBlocks() {
	blocks := make([]*types.Block, 0, bc.futureBlocks.Len())
	for _, hash := range bc.futureBlocks.Keys() {
		if block, exist := bc.futureBlocks.Peek(hash); exist {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) > 0 {
		slices.SortFunc(blocks, func(a, b *types.Block) int {
			return a.Number().Cmp(b.Number())
		})
		// Insert one by one as chain insertion needs contiguous ancestry between blocks
		for i := range blocks {
			bc.InsertChain(blocks[i : i+1])
		}
	}
}

// WriteStatus status of write
type WriteStatus byte

const (
	NonStatTy WriteStatus = iota
	CanonStatTy
	SideStatTy
)

// InsertReceiptChain attempts to complete an already existing header chain with
// transaction and receipt data.
func (bc *BlockChain) InsertReceiptChain(blockChain types.Blocks, receiptChain []types.Receipts, ancientLimit uint64) (int, error) {
	// We don't require the chainMu here since we want to maximize the
	// concurrency of header insertion and receipt insertion.
	bc.wg.Add(1)
	defer bc.wg.Done()

	var (
		ancientBlocks, liveBlocks     types.Blocks
		ancientReceipts, liveReceipts []types.Receipts
	)
	// Do a sanity check that the provided chain is actually ordered and linked
	for i := 0; i < len(blockChain); i++ {
		if i != 0 {
			if blockChain[i].NumberU64() != blockChain[i-1].NumberU64()+1 || blockChain[i].ParentHash() != blockChain[i-1].Hash() {
				log.Error("Non contiguous receipt insert", "number", blockChain[i].Number(), "hash", blockChain[i].Hash(), "parent", blockChain[i].ParentHash(),
					"prevnumber", blockChain[i-1].Number(), "prevhash", blockChain[i-1].Hash())
				return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x..], item %d is #%d [%x..] (parent [%x..])", i-1, blockChain[i-1].NumberU64(),
					blockChain[i-1].Hash().Bytes()[:4], i, blockChain[i].NumberU64(), blockChain[i].Hash().Bytes()[:4], blockChain[i].ParentHash().Bytes()[:4])
			}
		}
		if blockChain[i].NumberU64() <= ancientLimit {
			ancientBlocks, ancientReceipts = append(ancientBlocks, blockChain[i]), append(ancientReceipts, receiptChain[i])
		} else {
			liveBlocks, liveReceipts = append(liveBlocks, blockChain[i]), append(liveReceipts, receiptChain[i])
		}
	}

	var (
		stats = struct{ processed, ignored int32 }{}
		start = time.Now()
		size  = int64(0)
	)

	// updateHead updates the head snap sync block if the inserted blocks are better
	// and returns an indicator whether the inserted blocks are canonical.
	updateHead := func(head *types.Block) bool {
		if !bc.chainmu.TryLock() {
			return false
		}
		defer bc.chainmu.Unlock()

		// Rewind may have occurred, skip in that case.
		if bc.CurrentHeader().Number.Cmp(head.Number()) >= 0 {
			reorg, err := bc.forker.ReorgNeededWithFastFinality(bc.CurrentSnapBlock(), head.Header())
			if err != nil {
				log.Warn("Reorg failed", "err", err)
				return false
			} else if !reorg {
				return false
			}
			rawdb.WriteHeadFastBlockHash(bc.db, head.Hash())
			bc.currentSnapBlock.Store(head.Header())
			headFastBlockGauge.Update(int64(head.NumberU64()))
			return true
		}
		return false
	}
	// writeAncient writes blockchain and corresponding receipt chain into ancient store.
	//
	// this function only accepts canonical chain data. All side chain will be reverted
	// eventually.
	writeAncient := func(blockChain types.Blocks, receiptChain []types.Receipts) (int, error) {
		first := blockChain[0]
		last := blockChain[len(blockChain)-1]

		// Ensure genesis is in ancients.
		if first.NumberU64() == 1 {
			if frozen, _ := bc.db.Ancients(); frozen == 0 {
				b := bc.genesisBlock
				td := bc.genesisBlock.Difficulty()
				writeSize, err := rawdb.WriteAncientBlocks(bc.db, []*types.Block{b}, []types.Receipts{nil}, td)
				size += writeSize
				if err != nil {
					log.Error("Error writing genesis to ancients", "err", err)
					return 0, err
				}
				log.Info("Wrote genesis to ancients")
			}
		}
		// Before writing the blocks to the ancients, we need to ensure that
		// they correspond to the what the headerchain 'expects'.
		// We only check the last block/header, since it's a contiguous chain.
		if !bc.HasHeader(last.Hash(), last.NumberU64()) {
			return 0, fmt.Errorf("containing header #%d [%x..] unknown", last.Number(), last.Hash().Bytes()[:4])
		}

		// Write all chain data to ancients.
		td := bc.GetTd(first.Hash(), first.NumberU64())
		writeSize, err := rawdb.WriteAncientBlocks(bc.db, blockChain, receiptChain, td)
		size += writeSize
		if err != nil {
			log.Error("Error importing chain data to ancients", "err", err)
			return 0, err
		}

		// Write tx indices if any condition is satisfied:
		// * If user requires to reserve all tx indices(txlookuplimit=0)
		// * If all ancient tx indices are required to be reserved(txlookuplimit is even higher than ancientlimit)
		// * If block number is large enough to be regarded as a recent block
		// It means blocks below the ancientLimit-txlookupLimit won't be indexed.
		//
		// But if the `TxIndexTail` is not nil, e.g. Geth is initialized with
		// an external ancient database, during the setup, blockchain will start
		// a background routine to re-indexed all indices in [ancients - txlookupLimit, ancients)
		// range. In this case, all tx indices of newly imported blocks should be
		// generated.
		var batch = bc.db.NewBatch()
		for i, block := range blockChain {
			if bc.txLookupLimit == 0 || ancientLimit <= bc.txLookupLimit || block.NumberU64() >= ancientLimit-bc.txLookupLimit {
				rawdb.WriteTxLookupEntriesByBlock(batch, block)
			} else if rawdb.ReadTxIndexTail(bc.db) != nil {
				rawdb.WriteTxLookupEntriesByBlock(batch, block)
			}
			stats.processed++

			if batch.ValueSize() > ethdb.IdealBatchSize || i == len(blockChain)-1 {
				size += int64(batch.ValueSize())
				if err = batch.Write(); err != nil {
					snapBlock := bc.CurrentSnapBlock().Number.Uint64()
					if _, err := bc.db.TruncateHead(snapBlock + 1); err != nil {
						log.Error("Can't truncate ancient store after failed insert", "err", err)
					}
					return 0, err
				}
				batch.Reset()
			}
		}

		// Sync the ancient store explicitly to ensure all data has been flushed to disk.
		if err := bc.db.Sync(); err != nil {
			return 0, err
		}
		// Update the current snap block because all block data is now present in DB.
		previousSnapBlock := bc.CurrentSnapBlock().Number.Uint64()
		if !updateHead(blockChain[len(blockChain)-1]) {
			// We end up here if the header chain has reorg'ed, and the blocks/receipts
			// don't match the canonical chain.
			if _, err := bc.db.TruncateHead(previousSnapBlock + 1); err != nil {
				log.Error("Can't truncate ancient store after failed insert", "err", err)
			}
			return 0, errSideChainReceipts
		}

		// Delete block data from the main database.
		batch.Reset()
		canonHashes := make(map[common.Hash]struct{})
		for _, block := range blockChain {
			canonHashes[block.Hash()] = struct{}{}
			if block.NumberU64() == 0 {
				continue
			}
			rawdb.DeleteCanonicalHash(batch, block.NumberU64())
			rawdb.DeleteBlockWithoutNumber(batch, block.Hash(), block.NumberU64())
		}
		// Delete side chain hash-to-number mappings.
		for _, nh := range rawdb.ReadAllHashesInRange(bc.db, first.NumberU64(), last.NumberU64()) {
			if _, canon := canonHashes[nh.Hash]; !canon {
				rawdb.DeleteHeader(batch, nh.Hash, nh.Number)
			}
		}
		if err := batch.Write(); err != nil {
			return 0, err
		}
		return 0, nil
	}

	// writeLive writes blockchain and corresponding receipt chain into active store.
	writeLive := func(blockChain types.Blocks, receiptChain []types.Receipts) (int, error) {
		skipPresenceCheck := false
		batch := bc.db.NewBatch()
		for i, block := range blockChain {
			// Short circuit insertion if shutting down or processing failed
			if bc.insertStopped() {
				return 0, errInsertionInterrupted
			}
			// Short circuit if the owner header is unknown
			if !bc.HasHeader(block.Hash(), block.NumberU64()) {
				return i, fmt.Errorf("containing header #%d [%x..] unknown", block.Number(), block.Hash().Bytes()[:4])
			}
			if !skipPresenceCheck {
				// Ignore if the entire data is already known
				if bc.HasBlock(block.Hash(), block.NumberU64()) {
					stats.ignored++
					continue
				} else {
					// If block N is not present, neither are the later blocks.
					// This should be true, but if we are mistaken, the shortcut
					// here will only cause overwriting of some existing data
					skipPresenceCheck = true
				}
			}
			// Write all the data out into the database
			rawdb.WriteBody(batch, block.Hash(), block.NumberU64(), block.Body())
			rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), receiptChain[i])
			rawdb.WriteTxLookupEntriesByBlock(batch, block) // Always write tx indices for live blocks, we assume they are needed

			// Write everything belongs to the blocks into the database. So that
			// we can ensure all components of body is completed(body, receipts,
			// tx indexes)
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					return 0, err
				}
				size += int64(batch.ValueSize())
				batch.Reset()
			}
			stats.processed++
		}
		// Write everything belongs to the blocks into the database. So that
		// we can ensure all components of body is completed(body, receipts,
		// tx indexes)
		if batch.ValueSize() > 0 {
			size += int64(batch.ValueSize())
			if err := batch.Write(); err != nil {
				return 0, err
			}
		}
		updateHead(blockChain[len(blockChain)-1])
		return 0, nil
	}

	// Write downloaded chain data and corresponding receipt chain data
	if len(ancientBlocks) > 0 {
		if n, err := writeAncient(ancientBlocks, ancientReceipts); err != nil {
			if err == errInsertionInterrupted {
				return 0, nil
			}
			return n, err
		}
	}
	// Write the tx index tail (block number from where we index) before write any live blocks
	if len(liveBlocks) > 0 && liveBlocks[0].NumberU64() == ancientLimit+1 {
		// The tx index tail can only be one of the following two options:
		// * 0: all ancient blocks have been indexed
		// * ancient-limit: the indices of blocks before ancient-limit are ignored
		if tail := rawdb.ReadTxIndexTail(bc.db); tail == nil {
			if bc.txLookupLimit == 0 || ancientLimit <= bc.txLookupLimit {
				rawdb.WriteTxIndexTail(bc.db, 0)
			} else {
				rawdb.WriteTxIndexTail(bc.db, ancientLimit-bc.txLookupLimit)
			}
		}
	}
	if len(liveBlocks) > 0 {
		if n, err := writeLive(liveBlocks, liveReceipts); err != nil {
			if err == errInsertionInterrupted {
				return 0, nil
			}
			return n, err
		}
	}

	head := blockChain[len(blockChain)-1]
	context := []interface{}{
		"count", stats.processed, "elapsed", common.PrettyDuration(time.Since(start)),
		"number", head.Number(), "hash", head.Hash(), "age", common.PrettyAge(time.Unix(int64(head.Time()), 0)),
		"size", common.StorageSize(size),
	}
	if stats.ignored > 0 {
		context = append(context, []interface{}{"ignored", stats.ignored}...)
	}
	log.Debug("Imported new block receipts", context...)

	return 0, nil
}

// writeBlockWithoutState writes only the block and its metadata to the database,
// but does not write any state. This is used to construct competing side forks
// up to the point where they exceed the canonical total difficulty.
func (bc *BlockChain) writeBlockWithoutState(block *types.Block, td *big.Int) (err error) {
	if bc.insertStopped() {
		return errInsertionInterrupted
	}

	batch := bc.db.NewBatch()
	rawdb.WriteTd(batch, block.Hash(), block.NumberU64(), td)
	rawdb.WriteBlock(batch, block)
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write block into disk", "err", err)
	}
	return nil
}

// writeKnownBlock updates the head block flag with a known block
// and introduces chain reorg if necessary.
func (bc *BlockChain) writeKnownBlock(block *types.Block) error {
	current := bc.CurrentBlock()
	if block.ParentHash() != current.Hash() {
		if err := bc.reorg(current, block); err != nil {
			return err
		}
	}
	bc.writeHeadBlock(block)
	return nil
}

// writeBlockWithState writes block, metadata and corresponding state data to the
// database.
func (bc *BlockChain) writeBlockWithState(block *types.Block, receipts []*types.Receipt, state *state.StateDB) error {
	// Calculate the total difficulty of the block
	ptd := bc.GetTd(block.ParentHash(), block.NumberU64()-1)
	if ptd == nil {
		state.StopPrefetcher()
		return consensus.ErrUnknownAncestor
	}
	// Make sure no inconsistent state is leaked during insertion
	externTd := new(big.Int).Add(block.Difficulty(), ptd)

	// Irrelevant of the canonical status, write the block itself to the database.
	//
	// Note all the components of block(td, hash->number map, header, body, receipts)
	// should be written atomically. BlockBatch is used for containing all components.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		blockBatch := bc.db.NewBatch()
		rawdb.WriteTd(blockBatch, block.Hash(), block.NumberU64(), externTd)
		rawdb.WriteBlock(blockBatch, block)
		rawdb.WriteReceipts(blockBatch, block.Hash(), block.NumberU64(), receipts)
		rawdb.WritePreimages(blockBatch, state.Preimages())
		if err := blockBatch.Write(); err != nil {
			log.Crit("Failed to write block into disk", "err", err)
		}
		wg.Done()
	}()

	tryCommitTrieDB := func() error {
		bc.commitLock.Lock()
		defer bc.commitLock.Unlock()

		// If node is running in path mode, skip explicit gc operation
		// which is unnecessary in this mode.
		if bc.triedb.Scheme() == rawdb.PathScheme {
			return nil
		}

		triedb := bc.stateCache.TrieDB()
		// If we're running an archive node, always flush
		if bc.cacheConfig.TrieDirtyDisabled {
			err := triedb.Commit(block.Root(), false)
			if err != nil {
				return err
			}
		} else {
			// Full but not archive node, do proper garbage collection
			triedb.Reference(block.Root(), common.Hash{}) // metadata reference to keep trie alive
			bc.triegc.Push(block.Root(), -int64(block.NumberU64()))

			if current := block.NumberU64(); current > bc.triesInMemory {
				// If we exceeded our memory allowance, flush matured singleton nodes to disk
				var (
					nodes, imgs = triedb.Size()
					limit       = common.StorageSize(bc.cacheConfig.TrieDirtyLimit) * 1024 * 1024
				)
				if nodes > limit || imgs > 4*1024*1024 {
					triedb.Cap(limit - ethdb.IdealBatchSize)
				}
				// Find the next state trie we need to commit
				chosen := current - bc.triesInMemory

				flushInterval := time.Duration(bc.flushInterval.Load())

				// If we exceeded out time allowance, flush an entire trie to disk
				if bc.gcproc > flushInterval {
					canWrite := true
					if posa, ok := bc.engine.(consensus.PoSA); ok {
						if !posa.EnoughDistance(bc, block.Header()) {
							canWrite = false
						}
					}
					if canWrite {
						// If the header is missing (canonical chain behind), we're reorging a low
						// diff sidechain. Suspend committing until this operation is completed.
						header := bc.GetHeaderByNumber(chosen)
						if header == nil {
							log.Warn("Reorg in progress, trie commit postponed", "number", chosen)
						} else {
							// If we're exceeding limits but haven't reached a large enough memory gap,
							// warn the user that the system is becoming unstable.
							if chosen < bc.lastWrite+bc.triesInMemory && bc.gcproc >= 2*flushInterval {
								log.Info("State in memory for too long, committing", "time", bc.gcproc, "allowance", flushInterval, "optimum", float64(chosen-bc.lastWrite)/float64(bc.triesInMemory))
							}
							// Flush an entire trie and restart the counters
							triedb.Commit(header.Root, true)
							rawdb.WriteSafePointBlockNumber(bc.db, chosen)
							bc.lastWrite = chosen
							bc.gcproc = 0
						}
					}
				}
				// Garbage collect anything below our required write retention
				wg2 := sync.WaitGroup{}
				for !bc.triegc.Empty() {
					root, number := bc.triegc.Pop()
					if uint64(-number) > chosen {
						bc.triegc.Push(root, number)
						break
					}
					wg2.Add(1)
					go func() {
						triedb.Dereference(root)
						wg2.Done()
					}()
				}
				wg2.Wait()
			}
		}
		return nil
	}
	// Commit all cached state changes into underlying memory database.
	_, diffLayer, err := state.Commit(block.NumberU64(), bc.tryRewindBadBlocks, tryCommitTrieDB)
	if err != nil {
		return err
	}

	// Ensure no empty block body
	if diffLayer != nil && block.Header().TxHash != types.EmptyRootHash {
		// Filling necessary field
		diffLayer.Receipts = receipts
		diffLayer.BlockHash = block.Hash()
		diffLayer.Number = block.NumberU64()

		diffLayerCh := make(chan struct{})
		if bc.diffLayerChanCache.Len() >= diffLayerCacheLimit {
			bc.diffLayerChanCache.RemoveOldest()
		}
		bc.diffLayerChanCache.Add(diffLayer.BlockHash, diffLayerCh)

		go bc.cacheDiffLayer(diffLayer, diffLayerCh)
	}
	wg.Wait()
	return nil
}

// WriteBlockAndSetHead writes the given block and all associated state to the database,
// and applies the block as the new chain head.
func (bc *BlockChain) WriteBlockAndSetHead(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB, emitHeadEvent bool) (status WriteStatus, err error) {
	if !bc.chainmu.TryLock() {
		return NonStatTy, errChainStopped
	}
	defer bc.chainmu.Unlock()

	return bc.writeBlockAndSetHead(block, receipts, logs, state, emitHeadEvent)
}

// writeBlockAndSetHead is the internal implementation of WriteBlockAndSetHead.
// This function expects the chain mutex to be held.
func (bc *BlockChain) writeBlockAndSetHead(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB, emitHeadEvent bool) (status WriteStatus, err error) {
	if err := bc.writeBlockWithState(block, receipts, state); err != nil {
		return NonStatTy, err
	}
	currentBlock := bc.CurrentBlock()
	reorg, err := bc.forker.ReorgNeededWithFastFinality(currentBlock, block.Header())
	if err != nil {
		return NonStatTy, err
	}
	if reorg {
		// Reorganise the chain if the parent is not the head block
		if block.ParentHash() != currentBlock.Hash() {
			if err := bc.reorg(currentBlock, block); err != nil {
				return NonStatTy, err
			}
		}
		status = CanonStatTy
	} else {
		status = SideStatTy
	}
	// Set new head.
	if status == CanonStatTy {
		bc.writeHeadBlock(block)
	}
	bc.futureBlocks.Remove(block.Hash())

	if status == CanonStatTy {
		bc.chainFeed.Send(ChainEvent{Block: block, Hash: block.Hash(), Logs: logs})
		if len(logs) > 0 {
			bc.logsFeed.Send(logs)
		}
		// In theory, we should fire a ChainHeadEvent when we inject
		// a canonical block, but sometimes we can insert a batch of
		// canonical blocks. Avoid firing too many ChainHeadEvents,
		// we will fire an accumulated ChainHeadEvent and disable fire
		// event here.
		if emitHeadEvent {
			bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
			if posa, ok := bc.Engine().(consensus.PoSA); ok {
				if finalizedHeader := posa.GetFinalizedHeader(bc, block.Header()); finalizedHeader != nil {
					bc.finalizedHeaderFeed.Send(FinalizedHeaderEvent{finalizedHeader})
				}
			}
		}
	} else {
		bc.chainSideFeed.Send(ChainSideEvent{Block: block})
	}
	return status, nil
}

// addFutureBlock checks if the block is within the max allowed window to get
// accepted for future processing, and returns an error if the block is too far
// ahead and was not added.
//
// TODO after the transition, the future block shouldn't be kept. Because
// it's not checked in the Geth side anymore.
func (bc *BlockChain) addFutureBlock(block *types.Block) error {
	max := uint64(time.Now().Unix() + maxTimeFutureBlocks)
	if block.Time() > max {
		return fmt.Errorf("future block timestamp %v > allowed %v", block.Time(), max)
	}
	if block.Difficulty().Cmp(common.Big0) == 0 {
		// Never add PoS blocks into the future queue
		return nil
	}
	bc.futureBlocks.Add(block.Hash(), block)
	return nil
}

// InsertChain attempts to insert the given batch of blocks in to the canonical
// chain or, otherwise, create a fork. If an error is returned it will return
// the index number of the failing block as well an error describing what went
// wrong. After insertion is done, all accumulated events will be fired.
func (bc *BlockChain) InsertChain(chain types.Blocks) (int, error) {
	// Sanity check that we have something meaningful to import
	if len(chain) == 0 {
		return 0, nil
	}
	bc.blockProcFeed.Send(true)
	defer bc.blockProcFeed.Send(false)

	// Do a sanity check that the provided chain is actually ordered and linked.
	for i := 1; i < len(chain); i++ {
		block, prev := chain[i], chain[i-1]
		if block.NumberU64() != prev.NumberU64()+1 || block.ParentHash() != prev.Hash() {
			log.Error("Non contiguous block insert",
				"number", block.Number(),
				"hash", block.Hash(),
				"parent", block.ParentHash(),
				"prevnumber", prev.Number(),
				"prevhash", prev.Hash(),
			)
			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x..], item %d is #%d [%x..] (parent [%x..])", i-1, prev.NumberU64(),
				prev.Hash().Bytes()[:4], i, block.NumberU64(), block.Hash().Bytes()[:4], block.ParentHash().Bytes()[:4])
		}
	}
	// Pre-checks passed, start the full block imports
	if !bc.chainmu.TryLock() {
		return 0, errChainStopped
	}
	defer bc.chainmu.Unlock()
	return bc.insertChain(chain, true)
}

// insertChain is the internal implementation of InsertChain, which assumes that
// 1) chains are contiguous, and 2) The chain mutex is held.
//
// This method is split out so that import batches that require re-injecting
// historical blocks can do so without releasing the lock, which could lead to
// racey behaviour. If a sidechain import is in progress, and the historic state
// is imported, but then new canon-head is added before the actual sidechain
// completes, then the historic state could be pruned again
func (bc *BlockChain) insertChain(chain types.Blocks, setHead bool) (int, error) {
	// If the chain is terminating, don't even bother starting up.
	if bc.insertStopped() {
		return 0, nil
	}

	// Start a parallel signature recovery (signer will fluke on fork transition, minimal perf loss)
	signer := types.MakeSigner(bc.chainConfig, chain[0].Number(), chain[0].Time())
	go SenderCacher.RecoverFromBlocks(signer, chain)

	var (
		stats     = insertStats{startTime: mclock.Now()}
		lastCanon *types.Block
	)
	// Fire a single chain head event if we've progressed the chain
	defer func() {
		if lastCanon != nil && bc.CurrentBlock().Hash() == lastCanon.Hash() {
			bc.chainHeadFeed.Send(ChainHeadEvent{lastCanon})
			if posa, ok := bc.Engine().(consensus.PoSA); ok {
				if finalizedHeader := posa.GetFinalizedHeader(bc, lastCanon.Header()); finalizedHeader != nil {
					bc.finalizedHeaderFeed.Send(FinalizedHeaderEvent{finalizedHeader})
				}
			}
		}
	}()
	// Start the parallel header verifier
	headers := make([]*types.Header, len(chain))
	for i, block := range chain {
		headers[i] = block.Header()
	}
	abort, results := bc.engine.VerifyHeaders(bc, headers)
	defer close(abort)

	// Peek the error for the first block to decide the directing import logic
	it := newInsertIterator(chain, results, bc.validator)
	block, err := it.next()

	// Left-trim all the known blocks that don't need to build snapshot
	if bc.skipBlock(err, it) {
		// First block (and state) is known
		//   1. We did a roll-back, and should now do a re-import
		//   2. The block is stored as a sidechain, and is lying about it's stateroot, and passes a stateroot
		//      from the canonical chain, which has not been verified.
		// Skip all known blocks that are behind us.
		var (
			reorg   bool
			current = bc.CurrentBlock()
		)
		for block != nil && bc.skipBlock(err, it) {
			reorg, err = bc.forker.ReorgNeededWithFastFinality(current, block.Header())
			if err != nil {
				return it.index, err
			}
			if reorg {
				// Switch to import mode if the forker says the reorg is necessary
				// and also the block is not on the canonical chain.
				// In eth2 the forker always returns true for reorg decision (blindly trusting
				// the external consensus engine), but in order to prevent the unnecessary
				// reorgs when importing known blocks, the special case is handled here.
				if block.NumberU64() > current.Number.Uint64() || bc.GetCanonicalHash(block.NumberU64()) != block.Hash() {
					break
				}
			}
			log.Debug("Ignoring already known block", "number", block.Number(), "hash", block.Hash())
			stats.ignored++

			block, err = it.next()
		}
		// The remaining blocks are still known blocks, the only scenario here is:
		// During the snap sync, the pivot point is already submitted but rollback
		// happens. Then node resets the head full block to a lower height via `rollback`
		// and leaves a few known blocks in the database.
		//
		// When node runs a snap sync again, it can re-import a batch of known blocks via
		// `insertChain` while a part of them have higher total difficulty than current
		// head full block(new pivot point).
		for block != nil && bc.skipBlock(err, it) {
			log.Debug("Writing previously known block", "number", block.Number(), "hash", block.Hash())
			if err := bc.writeKnownBlock(block); err != nil {
				return it.index, err
			}
			lastCanon = block

			block, err = it.next()
		}
		// Falls through to the block import
	}
	switch {
	// First block is pruned
	case errors.Is(err, consensus.ErrPrunedAncestor):
		if setHead {
			// First block is pruned, insert as sidechain and reorg only if TD grows enough
			log.Debug("Pruned ancestor, inserting as sidechain", "number", block.Number(), "hash", block.Hash())
			return bc.insertSideChain(block, it)
		} else {
			// We're post-merge and the parent is pruned, try to recover the parent state
			log.Debug("Pruned ancestor", "number", block.Number(), "hash", block.Hash())
			_, err := bc.recoverAncestors(block)
			return it.index, err
		}
	// First block is future, shove it (and all children) to the future queue (unknown ancestor)
	case errors.Is(err, consensus.ErrFutureBlock) || (errors.Is(err, consensus.ErrUnknownAncestor) && bc.futureBlocks.Contains(it.first().ParentHash())):
		for block != nil && (it.index == 0 || errors.Is(err, consensus.ErrUnknownAncestor)) {
			log.Debug("Future block, postponing import", "number", block.Number(), "hash", block.Hash())
			if err := bc.addFutureBlock(block); err != nil {
				return it.index, err
			}
			block, err = it.next()
		}
		stats.queued += it.processed()
		stats.ignored += it.remaining()

		// If there are any still remaining, mark as ignored
		return it.index, err

	// Some other error(except ErrKnownBlock) occurred, abort.
	// ErrKnownBlock is allowed here since some known blocks
	// still need re-execution to generate snapshots that are missing
	case err != nil && !errors.Is(err, ErrKnownBlock):
		bc.futureBlocks.Remove(block.Hash())
		stats.ignored += len(it.chain)
		bc.reportBlock(block, nil, err)
		return it.index, err
	}

	for ; block != nil && err == nil || errors.Is(err, ErrKnownBlock); block, err = it.next() {
		// If the chain is terminating, stop processing blocks
		if bc.insertStopped() {
			log.Debug("Abort during block processing")
			break
		}
		// If the header is a banned one, straight out abort
		if BadHashes[block.Hash()] {
			bc.reportBlock(block, nil, ErrBannedHash)
			return it.index, ErrBannedHash
		}
		// If the block is known (in the middle of the chain), it's a special case for
		// Clique blocks where they can share state among each other, so importing an
		// older block might complete the state of the subsequent one. In this case,
		// just skip the block (we already validated it once fully (and crashed), since
		// its header and body was already in the database). But if the corresponding
		// snapshot layer is missing, forcibly rerun the execution to build it.
		if bc.skipBlock(err, it) {
			logger := log.Debug
			if bc.chainConfig.Clique == nil {
				logger = log.Warn
			}
			logger("Inserted known block", "number", block.Number(), "hash", block.Hash(),
				"uncles", len(block.Uncles()), "txs", len(block.Transactions()), "gas", block.GasUsed(),
				"root", block.Root())

			// Special case. Commit the empty receipt slice if we meet the known
			// block in the middle. It can only happen in the clique chain. Whenever
			// we insert blocks via `insertSideChain`, we only commit `td`, `header`
			// and `body` if it's non-existent. Since we don't have receipts without
			// reexecution, so nothing to commit. But if the sidechain will be adopted
			// as the canonical chain eventually, it needs to be reexecuted for missing
			// state, but if it's this special case here(skip reexecution) we will lose
			// the empty receipt entry.
			if len(block.Transactions()) == 0 {
				rawdb.WriteReceipts(bc.db, block.Hash(), block.NumberU64(), nil)
			} else {
				log.Error("Please file an issue, skip known block execution without receipt",
					"hash", block.Hash(), "number", block.NumberU64())
			}
			if err := bc.writeKnownBlock(block); err != nil {
				return it.index, err
			}
			stats.processed++

			// We can assume that logs are empty here, since the only way for consecutive
			// Clique blocks to have the same state is if there are no transactions.
			lastCanon = block
			continue
		}

		// Retrieve the parent block and it's state to execute on top
		start := time.Now()
		parent := it.previous()
		if parent == nil {
			parent = bc.GetHeader(block.ParentHash(), block.NumberU64()-1)
		}
		statedb, err := state.NewWithSharedPool(parent.Root, bc.stateCache, bc.snaps)
		if err != nil {
			return it.index, err
		}
		bc.updateHighestVerifiedHeader(block.Header())

		// Enable prefetching to pull in trie node paths while processing transactions
		statedb.StartPrefetcher("chain")
		interruptCh := make(chan struct{})
		// For diff sync, it may fallback to full sync, so we still do prefetch
		if len(block.Transactions()) >= prefetchTxNumber {
			// do Prefetch in a separate goroutine to avoid blocking the critical path

			// 1.do state prefetch for snapshot cache
			throwaway := statedb.CopyDoPrefetch()
			go bc.prefetcher.Prefetch(block, throwaway, &bc.vmConfig, interruptCh)

			// 2.do trie prefetch for MPT trie node cache
			// it is for the big state trie tree, prefetch based on transaction's From/To address.
			// trie prefetcher is thread safe now, ok to prefetch in a separate routine
			go throwaway.TriePrefetchInAdvance(block, signer)
		}

		//Process block using the parent state as reference point
		if bc.pipeCommit {
			statedb.EnablePipeCommit()
		}
		statedb.SetExpectedStateRoot(block.Root())
		pstart := time.Now()
		statedb, receipts, logs, usedGas, err := bc.processor.Process(block, statedb, bc.vmConfig)
		close(interruptCh) // state prefetch can be stopped
		if err != nil {
			bc.reportBlock(block, receipts, err)
			statedb.StopPrefetcher()
			return it.index, err
		}
		ptime := time.Since(pstart)

		// Validate the state using the default validator
		vstart := time.Now()
		if err := bc.validator.ValidateState(block, statedb, receipts, usedGas); err != nil {
			log.Error("validate state failed", "error", err)
			bc.reportBlock(block, receipts, err)
			statedb.StopPrefetcher()
			return it.index, err
		}
		vtime := time.Since(vstart)
		proctime := time.Since(start) // processing + validation

		bc.cacheReceipts(block.Hash(), receipts)
		bc.cacheBlock(block.Hash(), block)

		// Update the metrics touched during block processing and validation
		accountReadTimer.Update(statedb.AccountReads)                   // Account reads are complete(in processing)
		storageReadTimer.Update(statedb.StorageReads)                   // Storage reads are complete(in processing)
		snapshotAccountReadTimer.Update(statedb.SnapshotAccountReads)   // Account reads are complete(in processing)
		snapshotStorageReadTimer.Update(statedb.SnapshotStorageReads)   // Storage reads are complete(in processing)
		accountUpdateTimer.Update(statedb.AccountUpdates)               // Account updates are complete(in validation)
		storageUpdateTimer.Update(statedb.StorageUpdates)               // Storage updates are complete(in validation)
		accountHashTimer.Update(statedb.AccountHashes)                  // Account hashes are complete(in validation)
		storageHashTimer.Update(statedb.StorageHashes)                  // Storage hashes are complete(in validation)
		triehash := statedb.AccountHashes + statedb.StorageHashes       // The time spent on tries hashing
		trieUpdate := statedb.AccountUpdates + statedb.StorageUpdates   // The time spent on tries update
		trieRead := statedb.SnapshotAccountReads + statedb.AccountReads // The time spent on account read
		trieRead += statedb.SnapshotStorageReads + statedb.StorageReads // The time spent on storage read
		blockExecutionTimer.Update(ptime - trieRead)                    // The time spent on EVM processing
		blockValidationTimer.Update(vtime - (triehash + trieUpdate))    // The time spent on block validation

		// Write the block to the chain and get the status.
		var (
			wstart = time.Now()
			status WriteStatus
		)
		if !setHead {
			// Don't set the head, only insert the block
			err = bc.writeBlockWithState(block, receipts, statedb)
		} else {
			status, err = bc.writeBlockAndSetHead(block, receipts, logs, statedb, false)
		}
		if err != nil {
			return it.index, err
		}
		// Update the metrics touched during block commit
		accountCommitTimer.Update(statedb.AccountCommits)   // Account commits are complete, we can mark them
		storageCommitTimer.Update(statedb.StorageCommits)   // Storage commits are complete, we can mark them
		snapshotCommitTimer.Update(statedb.SnapshotCommits) // Snapshot commits are complete, we can mark them
		triedbCommitTimer.Update(statedb.TrieDBCommits)     // Trie database commits are complete, we can mark them

		blockWriteTimer.Update(time.Since(wstart) - statedb.AccountCommits - statedb.StorageCommits - statedb.SnapshotCommits - statedb.TrieDBCommits)
		blockInsertTimer.UpdateSince(start)

		// Report the import stats before returning the various results
		stats.processed++
		stats.usedGas += usedGas

		dirty, _ := bc.triedb.Size()
		stats.report(chain, it.index, dirty, setHead)

		if !setHead {
			// After merge we expect few side chains. Simply count
			// all blocks the CL gives us for GC processing time
			bc.gcproc += proctime

			return it.index, nil // Direct block insertion of a single block
		}
		switch status {
		case CanonStatTy:
			log.Debug("Inserted new block", "number", block.Number(), "hash", block.Hash(),
				"uncles", len(block.Uncles()), "txs", len(block.Transactions()), "gas", block.GasUsed(),
				"elapsed", common.PrettyDuration(time.Since(start)),
				"root", block.Root())

			lastCanon = block

			// Only count canonical blocks for GC processing time
			bc.gcproc += proctime

		case SideStatTy:
			log.Debug("Inserted forked block", "number", block.Number(), "hash", block.Hash(),
				"diff", block.Difficulty(), "elapsed", common.PrettyDuration(time.Since(start)),
				"txs", len(block.Transactions()), "gas", block.GasUsed(), "uncles", len(block.Uncles()),
				"root", block.Root())

		default:
			// This in theory is impossible, but lets be nice to our future selves and leave
			// a log, instead of trying to track down blocks imports that don't emit logs.
			log.Warn("Inserted block with unknown status", "number", block.Number(), "hash", block.Hash(),
				"diff", block.Difficulty(), "elapsed", common.PrettyDuration(time.Since(start)),
				"txs", len(block.Transactions()), "gas", block.GasUsed(), "uncles", len(block.Uncles()),
				"root", block.Root())
		}
		bc.chainBlockFeed.Send(ChainHeadEvent{block})
	}

	// Any blocks remaining here? The only ones we care about are the future ones
	if block != nil && errors.Is(err, consensus.ErrFutureBlock) {
		if err := bc.addFutureBlock(block); err != nil {
			return it.index, err
		}
		block, err = it.next()

		for ; block != nil && errors.Is(err, consensus.ErrUnknownAncestor); block, err = it.next() {
			if err := bc.addFutureBlock(block); err != nil {
				return it.index, err
			}
			stats.queued++
		}
	}
	stats.ignored += it.remaining()

	return it.index, err
}

func (bc *BlockChain) updateHighestVerifiedHeader(header *types.Header) {
	if header == nil || header.Number == nil {
		return
	}
	currentHeader := bc.highestVerifiedHeader.Load()
	if currentHeader == nil {
		bc.highestVerifiedHeader.Store(types.CopyHeader(header))
		return
	}

	newParentTD := bc.GetTd(header.ParentHash, header.Number.Uint64()-1)
	if newParentTD == nil {
		newParentTD = big.NewInt(0)
	}
	oldParentTD := bc.GetTd(currentHeader.ParentHash, currentHeader.Number.Uint64()-1)
	if oldParentTD == nil {
		oldParentTD = big.NewInt(0)
	}
	newTD := big.NewInt(0).Add(newParentTD, header.Difficulty)
	oldTD := big.NewInt(0).Add(oldParentTD, currentHeader.Difficulty)

	if newTD.Cmp(oldTD) > 0 {
		bc.highestVerifiedHeader.Store(types.CopyHeader(header))
		return
	}
}

func (bc *BlockChain) GetHighestVerifiedHeader() *types.Header {
	return bc.highestVerifiedHeader.Load()
}

// insertSideChain is called when an import batch hits upon a pruned ancestor
// error, which happens when a sidechain with a sufficiently old fork-block is
// found.
//
// The method writes all (header-and-body-valid) blocks to disk, then tries to
// switch over to the new chain if the TD exceeded the current chain.
// insertSideChain is only used pre-merge.
func (bc *BlockChain) insertSideChain(block *types.Block, it *insertIterator) (int, error) {
	var (
		externTd  *big.Int
		lastBlock = block
		current   = bc.CurrentBlock()
	)
	// The first sidechain block error is already verified to be ErrPrunedAncestor.
	// Since we don't import them here, we expect ErrUnknownAncestor for the remaining
	// ones. Any other errors means that the block is invalid, and should not be written
	// to disk.
	err := consensus.ErrPrunedAncestor
	for ; block != nil && errors.Is(err, consensus.ErrPrunedAncestor); block, err = it.next() {
		// Check the canonical state root for that number
		if number := block.NumberU64(); current.Number.Uint64() >= number {
			canonical := bc.GetBlockByNumber(number)
			if canonical != nil && canonical.Hash() == block.Hash() {
				// Not a sidechain block, this is a re-import of a canon block which has it's state pruned

				// Collect the TD of the block. Since we know it's a canon one,
				// we can get it directly, and not (like further below) use
				// the parent and then add the block on top
				externTd = bc.GetTd(block.Hash(), block.NumberU64())
				continue
			}
			if canonical != nil && canonical.Root() == block.Root() {
				// This is most likely a shadow-state attack. When a fork is imported into the
				// database, and it eventually reaches a block height which is not pruned, we
				// just found that the state already exist! This means that the sidechain block
				// refers to a state which already exists in our canon chain.
				//
				// If left unchecked, we would now proceed importing the blocks, without actually
				// having verified the state of the previous blocks.
				log.Warn("Sidechain ghost-state attack detected", "number", block.NumberU64(), "sideroot", block.Root(), "canonroot", canonical.Root())

				// If someone legitimately side-mines blocks, they would still be imported as usual. However,
				// we cannot risk writing unverified blocks to disk when they obviously target the pruning
				// mechanism.
				return it.index, errors.New("sidechain ghost-state attack")
			}
		}
		if externTd == nil {
			externTd = bc.GetTd(block.ParentHash(), block.NumberU64()-1)
		}
		externTd = new(big.Int).Add(externTd, block.Difficulty())

		if !bc.HasBlock(block.Hash(), block.NumberU64()) {
			start := time.Now()
			if err := bc.writeBlockWithoutState(block, externTd); err != nil {
				return it.index, err
			}
			log.Debug("Injected sidechain block", "number", block.Number(), "hash", block.Hash(),
				"diff", block.Difficulty(), "elapsed", common.PrettyDuration(time.Since(start)),
				"txs", len(block.Transactions()), "gas", block.GasUsed(), "uncles", len(block.Uncles()),
				"root", block.Root())
		}
		lastBlock = block
	}
	// At this point, we've written all sidechain blocks to database. Loop ended
	// either on some other error or all were processed. If there was some other
	// error, we can ignore the rest of those blocks.
	//
	// If the externTd was larger than our local TD, we now need to reimport the previous
	// blocks to regenerate the required state
	reorg, err := bc.forker.ReorgNeededWithFastFinality(current, lastBlock.Header())
	if err != nil {
		return it.index, err
	}
	if !reorg {
		localTd := bc.GetTd(current.Hash(), current.Number.Uint64())
		log.Info("Sidechain written to disk", "start", it.first().NumberU64(), "end", it.previous().Number, "sidetd", externTd, "localtd", localTd)
		return it.index, err
	}
	// Gather all the sidechain hashes (full blocks may be memory heavy)
	var (
		hashes  []common.Hash
		numbers []uint64
	)
	parent := it.previous()
	for parent != nil && !bc.HasState(parent.Root) {
		if bc.stateRecoverable(parent.Root) {
			if err := bc.triedb.Recover(parent.Root); err != nil {
				return 0, err
			}
			break
		}
		hashes = append(hashes, parent.Hash())
		numbers = append(numbers, parent.Number.Uint64())

		parent = bc.GetHeader(parent.ParentHash, parent.Number.Uint64()-1)
	}
	if parent == nil {
		return it.index, errors.New("missing parent")
	}
	// Import all the pruned blocks to make the state available
	var (
		blocks []*types.Block
		memory uint64
	)
	for i := len(hashes) - 1; i >= 0; i-- {
		// Append the next block to our batch
		block := bc.GetBlock(hashes[i], numbers[i])
		if block == nil {
			log.Crit("Importing heavy sidechain block is nil", "hash", hashes[i], "number", numbers[i])
		}

		blocks = append(blocks, block)
		memory += block.Size()

		// If memory use grew too large, import and continue. Sadly we need to discard
		// all raised events and logs from notifications since we're too heavy on the
		// memory here.
		if len(blocks) >= 2048 || memory > 64*1024*1024 {
			log.Info("Importing heavy sidechain segment", "blocks", len(blocks), "start", blocks[0].NumberU64(), "end", block.NumberU64())
			if _, err := bc.insertChain(blocks, true); err != nil {
				return 0, err
			}
			blocks, memory = blocks[:0], 0

			// If the chain is terminating, stop processing blocks
			if bc.insertStopped() {
				log.Debug("Abort during blocks processing")
				return 0, nil
			}
		}
	}
	if len(blocks) > 0 {
		log.Info("Importing sidechain segment", "start", blocks[0].NumberU64(), "end", blocks[len(blocks)-1].NumberU64())
		return bc.insertChain(blocks, true)
	}
	return 0, nil
}

// recoverAncestors finds the closest ancestor with available state and re-execute
// all the ancestor blocks since that.
// recoverAncestors is only used post-merge.
// We return the hash of the latest block that we could correctly validate.
func (bc *BlockChain) recoverAncestors(block *types.Block) (common.Hash, error) {
	// Gather all the sidechain hashes (full blocks may be memory heavy)
	var (
		hashes  []common.Hash
		numbers []uint64
		parent  = block
	)
	for parent != nil && !bc.HasState(parent.Root()) {
		if bc.stateRecoverable(parent.Root()) {
			if err := bc.triedb.Recover(parent.Root()); err != nil {
				return common.Hash{}, err
			}
			break
		}
		hashes = append(hashes, parent.Hash())
		numbers = append(numbers, parent.NumberU64())
		parent = bc.GetBlock(parent.ParentHash(), parent.NumberU64()-1)

		// If the chain is terminating, stop iteration
		if bc.insertStopped() {
			log.Debug("Abort during blocks iteration")
			return common.Hash{}, errInsertionInterrupted
		}
	}
	if parent == nil {
		return common.Hash{}, errors.New("missing parent")
	}
	// Import all the pruned blocks to make the state available
	for i := len(hashes) - 1; i >= 0; i-- {
		// If the chain is terminating, stop processing blocks
		if bc.insertStopped() {
			log.Debug("Abort during blocks processing")
			return common.Hash{}, errInsertionInterrupted
		}
		var b *types.Block
		if i == 0 {
			b = block
		} else {
			b = bc.GetBlock(hashes[i], numbers[i])
		}
		if _, err := bc.insertChain(types.Blocks{b}, false); err != nil {
			return b.ParentHash(), err
		}
	}
	return block.Hash(), nil
}

// collectLogs collects the logs that were generated or removed during
// the processing of a block. These logs are later announced as deleted or reborn.
func (bc *BlockChain) collectLogs(b *types.Block, removed bool) []*types.Log {
	var blobGasPrice *big.Int
	excessBlobGas := b.ExcessBlobGas()
	if excessBlobGas != nil {
		blobGasPrice = eip4844.CalcBlobFee(*excessBlobGas)
	}
	receipts := rawdb.ReadRawReceipts(bc.db, b.Hash(), b.NumberU64())
	if err := receipts.DeriveFields(bc.chainConfig, b.Hash(), b.NumberU64(), b.Time(), b.BaseFee(), blobGasPrice, b.Transactions()); err != nil {
		log.Error("Failed to derive block receipts fields", "hash", b.Hash(), "number", b.NumberU64(), "err", err)
	}
	var logs []*types.Log
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			if removed {
				log.Removed = true
			}
			logs = append(logs, log)
		}
	}
	return logs
}

// reorg takes two blocks, an old chain and a new chain and will reconstruct the
// blocks and inserts them to be part of the new canonical chain and accumulates
// potential missing transactions and post an event about them.
// Note the new head block won't be processed here, callers need to handle it
// externally.
func (bc *BlockChain) reorg(oldHead *types.Header, newHead *types.Block) error {
	var (
		newChain    types.Blocks
		oldChain    types.Blocks
		commonBlock *types.Block

		deletedTxs []common.Hash
		addedTxs   []common.Hash
	)
	oldBlock := bc.GetBlock(oldHead.Hash(), oldHead.Number.Uint64())
	if oldBlock == nil {
		return errors.New("current head block missing")
	}
	newBlock := newHead

	// Reduce the longer chain to the same number as the shorter one
	if oldBlock.NumberU64() > newBlock.NumberU64() {
		// Old chain is longer, gather all transactions and logs as deleted ones
		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
			for _, tx := range oldBlock.Transactions() {
				deletedTxs = append(deletedTxs, tx.Hash())
			}
		}
	} else {
		// New chain is longer, stash all blocks away for subsequent insertion
		for ; newBlock != nil && newBlock.NumberU64() != oldBlock.NumberU64(); newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1) {
			newChain = append(newChain, newBlock)
		}
	}
	if oldBlock == nil {
		return errInvalidOldChain
	}
	if newBlock == nil {
		return errInvalidNewChain
	}
	// Both sides of the reorg are at the same number, reduce both until the common
	// ancestor is found
	for {
		// If the common ancestor was found, bail out
		if oldBlock.Hash() == newBlock.Hash() {
			commonBlock = oldBlock
			break
		}
		// Remove an old block as well as stash away a new block
		oldChain = append(oldChain, oldBlock)
		for _, tx := range oldBlock.Transactions() {
			deletedTxs = append(deletedTxs, tx.Hash())
		}
		newChain = append(newChain, newBlock)

		// Step back with both chains
		oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1)
		if oldBlock == nil {
			return errInvalidOldChain
		}
		newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1)
		if newBlock == nil {
			return errInvalidNewChain
		}
	}

	// Ensure the user sees large reorgs
	if len(oldChain) > 0 && len(newChain) > 0 {
		logFn := log.Info
		msg := "Chain reorg detected"
		if len(oldChain) > 63 {
			msg = "Large chain reorg detected"
			logFn = log.Warn
		}
		logFn(msg, "number", commonBlock.Number(), "hash", commonBlock.Hash(),
			"drop", len(oldChain), "dropfrom", oldChain[0].Hash(), "add", len(newChain), "addfrom", newChain[0].Hash())
		blockReorgAddMeter.Mark(int64(len(newChain)))
		blockReorgDropMeter.Mark(int64(len(oldChain)))
		blockReorgMeter.Mark(1)
	} else if len(newChain) > 0 {
		// Special case happens in the post merge stage that current head is
		// the ancestor of new head while these two blocks are not consecutive
		log.Info("Extend chain", "add", len(newChain), "number", newChain[0].Number(), "hash", newChain[0].Hash())
		blockReorgAddMeter.Mark(int64(len(newChain)))
	} else {
		// len(newChain) == 0 && len(oldChain) > 0
		// rewind the canonical chain to a lower point.
		log.Error("Impossible reorg, please file an issue", "oldnum", oldBlock.Number(), "oldhash", oldBlock.Hash(), "oldblocks", len(oldChain), "newnum", newBlock.Number(), "newhash", newBlock.Hash(), "newblocks", len(newChain))
	}
	// Insert the new chain(except the head block(reverse order)),
	// taking care of the proper incremental order.
	for i := len(newChain) - 1; i >= 1; i-- {
		// Insert the block in the canonical way, re-writing history
		bc.writeHeadBlock(newChain[i])

		// Collect the new added transactions.
		for _, tx := range newChain[i].Transactions() {
			addedTxs = append(addedTxs, tx.Hash())
		}
	}

	// Delete useless indexes right now which includes the non-canonical
	// transaction indexes, canonical chain indexes which above the head.
	indexesBatch := bc.db.NewBatch()
	for _, tx := range types.HashDifference(deletedTxs, addedTxs) {
		rawdb.DeleteTxLookupEntry(indexesBatch, tx)
	}

	// Delete all hash markers that are not part of the new canonical chain.
	// Because the reorg function does not handle new chain head, all hash
	// markers greater than or equal to new chain head should be deleted.
	number := commonBlock.NumberU64()
	if len(newChain) > 1 {
		number = newChain[1].NumberU64()
	}
	for i := number + 1; ; i++ {
		hash := rawdb.ReadCanonicalHash(bc.db, i)
		if hash == (common.Hash{}) {
			break
		}
		rawdb.DeleteCanonicalHash(indexesBatch, i)
	}
	if err := indexesBatch.Write(); err != nil {
		log.Crit("Failed to delete useless indexes", "err", err)
	}

	// Send out events for logs from the old canon chain, and 'reborn'
	// logs from the new canon chain. The number of logs can be very
	// high, so the events are sent in batches of size around 512.

	// Deleted logs + blocks:
	var deletedLogs []*types.Log
	for i := len(oldChain) - 1; i >= 0; i-- {
		// Also send event for blocks removed from the canon chain.
		bc.chainSideFeed.Send(ChainSideEvent{Block: oldChain[i]})

		// Collect deleted logs for notification
		if logs := bc.collectLogs(oldChain[i], true); len(logs) > 0 {
			deletedLogs = append(deletedLogs, logs...)
		}
		if len(deletedLogs) > 512 {
			bc.rmLogsFeed.Send(RemovedLogsEvent{deletedLogs})
			deletedLogs = nil
		}
	}
	if len(deletedLogs) > 0 {
		bc.rmLogsFeed.Send(RemovedLogsEvent{deletedLogs})
	}

	// New logs:
	var rebirthLogs []*types.Log
	for i := len(newChain) - 1; i >= 1; i-- {
		if logs := bc.collectLogs(newChain[i], false); len(logs) > 0 {
			rebirthLogs = append(rebirthLogs, logs...)
		}
		if len(rebirthLogs) > 512 {
			bc.logsFeed.Send(rebirthLogs)
			rebirthLogs = nil
		}
	}
	if len(rebirthLogs) > 0 {
		bc.logsFeed.Send(rebirthLogs)
	}
	return nil
}

// InsertBlockWithoutSetHead executes the block, runs the necessary verification
// upon it and then persist the block and the associate state into the database.
// The key difference between the InsertChain is it won't do the canonical chain
// updating. It relies on the additional SetCanonical call to finalize the entire
// procedure.
func (bc *BlockChain) InsertBlockWithoutSetHead(block *types.Block) error {
	if !bc.chainmu.TryLock() {
		return errChainStopped
	}
	defer bc.chainmu.Unlock()

	_, err := bc.insertChain(types.Blocks{block}, false)
	return err
}

// SetCanonical rewinds the chain to set the new head block as the specified
// block. It's possible that the state of the new head is missing, and it will
// be recovered in this function as well.
func (bc *BlockChain) SetCanonical(head *types.Block) (common.Hash, error) {
	if !bc.chainmu.TryLock() {
		return common.Hash{}, errChainStopped
	}
	defer bc.chainmu.Unlock()

	// Re-execute the reorged chain in case the head state is missing.
	if !bc.HasState(head.Root()) {
		if latestValidHash, err := bc.recoverAncestors(head); err != nil {
			return latestValidHash, err
		}
		log.Info("Recovered head state", "number", head.Number(), "hash", head.Hash())
	}
	// Run the reorg if necessary and set the given block as new head.
	start := time.Now()
	if head.ParentHash() != bc.CurrentBlock().Hash() {
		if err := bc.reorg(bc.CurrentBlock(), head); err != nil {
			return common.Hash{}, err
		}
	}
	bc.writeHeadBlock(head)

	// Emit events
	logs := bc.collectLogs(head, false)
	bc.chainFeed.Send(ChainEvent{Block: head, Hash: head.Hash(), Logs: logs})
	if len(logs) > 0 {
		bc.logsFeed.Send(logs)
	}
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: head})

	context := []interface{}{
		"number", head.Number(),
		"hash", head.Hash(),
		"root", head.Root(),
		"elapsed", time.Since(start),
	}
	if timestamp := time.Unix(int64(head.Time()), 0); time.Since(timestamp) > time.Minute {
		context = append(context, []interface{}{"age", common.PrettyAge(timestamp)}...)
	}
	log.Info("Chain head was updated", context...)
	return head.Hash(), nil
}

func (bc *BlockChain) updateFutureBlocks() {
	futureTimer := time.NewTicker(5 * time.Second)
	defer futureTimer.Stop()
	defer bc.wg.Done()
	for {
		select {
		case <-futureTimer.C:
			bc.procFutureBlocks()
		case <-bc.quit:
			return
		}
	}
}

func (bc *BlockChain) rewindInvalidHeaderBlockLoop() {
	recheck := time.NewTicker(rewindBadBlockInterval)
	defer func() {
		recheck.Stop()
		bc.wg.Done()
	}()
	for {
		select {
		case <-recheck.C:
			bc.tryRewindBadBlocks()
		case <-bc.quit:
			return
		}
	}
}

func (bc *BlockChain) trustedDiffLayerLoop() {
	recheck := time.NewTicker(diffLayerFreezerRecheckInterval)
	defer func() {
		recheck.Stop()
		bc.wg.Done()
	}()
	for {
		select {
		case diff := <-bc.diffQueueBuffer:
			bc.diffQueue.Push(diff, -(int64(diff.Number)))
		case <-bc.quit:
			// Persist all diffLayers when shutdown, it will introduce redundant storage, but it is acceptable.
			// If the client been ungracefully shutdown, it will missing all cached diff layers, it is acceptable as well.
			var batch ethdb.Batch
			for !bc.diffQueue.Empty() {
				diffLayer, _ := bc.diffQueue.Pop()
				if batch == nil {
					batch = bc.db.DiffStore().NewBatch()
				}
				rawdb.WriteDiffLayer(batch, diffLayer.BlockHash, diffLayer)
				if batch.ValueSize() > ethdb.IdealBatchSize {
					if err := batch.Write(); err != nil {
						log.Error("Failed to write diff layer", "err", err)
						return
					}
					batch.Reset()
				}
			}
			if batch != nil {
				// flush data
				if err := batch.Write(); err != nil {
					log.Error("Failed to write diff layer", "err", err)
					return
				}
				batch.Reset()
			}
			return
		case <-recheck.C:
			currentHeight := bc.CurrentBlock().Number.Uint64()
			var batch ethdb.Batch
			for !bc.diffQueue.Empty() {
				diffLayer, prio := bc.diffQueue.Pop()

				// if the block not old enough
				if int64(currentHeight)+prio < int64(bc.triesInMemory) {
					bc.diffQueue.Push(diffLayer, prio)
					break
				}
				canonicalHash := bc.GetCanonicalHash(uint64(-prio))
				// on the canonical chain
				if canonicalHash == diffLayer.BlockHash {
					if batch == nil {
						batch = bc.db.DiffStore().NewBatch()
					}
					rawdb.WriteDiffLayer(batch, diffLayer.BlockHash, diffLayer)
					staleHash := bc.GetCanonicalHash(uint64(-prio) - bc.diffLayerFreezerBlockLimit)
					rawdb.DeleteDiffLayer(batch, staleHash)
				}
				if batch != nil && batch.ValueSize() > ethdb.IdealBatchSize {
					if err := batch.Write(); err != nil {
						panic(fmt.Sprintf("Failed to write diff layer, error %v", err))
					}
					batch.Reset()
				}
			}
			if batch != nil {
				if err := batch.Write(); err != nil {
					panic(fmt.Sprintf("Failed to write diff layer, error %v", err))
				}
				batch.Reset()
			}
		}
	}
}

func (bc *BlockChain) startDoubleSignMonitor() {
	eventChan := make(chan ChainHeadEvent, monitor.MaxCacheHeader)
	sub := bc.SubscribeChainHeadEvent(eventChan)
	defer func() {
		sub.Unsubscribe()
		close(eventChan)
		bc.wg.Done()
	}()

	for {
		select {
		case event := <-eventChan:
			if bc.doubleSignMonitor != nil {
				bc.doubleSignMonitor.Verify(event.Block.Header())
			}
		case <-bc.quit:
			return
		}
	}
}

// skipBlock returns 'true', if the block being imported can be skipped over, meaning
// that the block does not need to be processed but can be considered already fully 'done'.
func (bc *BlockChain) skipBlock(err error, it *insertIterator) bool {
	// We can only ever bypass processing if the only error returned by the validator
	// is ErrKnownBlock, which means all checks passed, but we already have the block
	// and state.
	if !errors.Is(err, ErrKnownBlock) {
		return false
	}
	// If we're not using snapshots, we can skip this, since we have both block
	// and (trie-) state
	if bc.snaps == nil {
		return true
	}
	var (
		header     = it.current() // header can't be nil
		parentRoot common.Hash
	)
	// If we also have the snapshot-state, we can skip the processing.
	if bc.snaps.Snapshot(header.Root) != nil {
		return true
	}
	// In this case, we have the trie-state but not snapshot-state. If the parent
	// snapshot-state exists, we need to process this in order to not get a gap
	// in the snapshot layers.
	// Resolve parent block
	if parent := it.previous(); parent != nil {
		parentRoot = parent.Root
	} else if parent = bc.GetHeaderByHash(header.ParentHash); parent != nil {
		parentRoot = parent.Root
	}
	if parentRoot == (common.Hash{}) {
		return false // Theoretically impossible case
	}
	// Parent is also missing snapshot: we can skip this. Otherwise process.
	if bc.snaps.Snapshot(parentRoot) == nil {
		return true
	}
	return false
}

// indexBlocks reindexes or unindexes transactions depending on user configuration
func (bc *BlockChain) indexBlocks(tail *uint64, head uint64, done chan struct{}) {
	defer func() { close(done) }()

	// The tail flag is not existent, it means the node is just initialized
	// and all blocks(may from ancient store) are not indexed yet.
	if tail == nil {
		from := uint64(0)
		if bc.txLookupLimit != 0 && head >= bc.txLookupLimit {
			from = head - bc.txLookupLimit + 1
		}
		rawdb.IndexTransactions(bc.db, from, head+1, bc.quit)
		return
	}
	// The tail flag is existent, but the whole chain is required to be indexed.
	if bc.txLookupLimit == 0 || head < bc.txLookupLimit {
		if *tail > 0 {
			// It can happen when chain is rewound to a historical point which
			// is even lower than the indexes tail, recap the indexing target
			// to new head to avoid reading non-existent block bodies.
			end := *tail
			if end > head+1 {
				end = head + 1
			}
			rawdb.IndexTransactions(bc.db, 0, end, bc.quit)
		}
		return
	}
	// Update the transaction index to the new chain state
	if head-bc.txLookupLimit+1 < *tail {
		// Reindex a part of missing indices and rewind index tail to HEAD-limit
		rawdb.IndexTransactions(bc.db, head-bc.txLookupLimit+1, *tail, bc.quit)
	} else {
		// Unindex a part of stale indices and forward index tail to HEAD-limit
		rawdb.UnindexTransactions(bc.db, *tail, head-bc.txLookupLimit+1, bc.quit)
	}
}

// maintainTxIndex is responsible for the construction and deletion of the
// transaction index.
//
// User can use flag `txlookuplimit` to specify a "recentness" block, below
// which ancient tx indices get deleted. If `txlookuplimit` is 0, it means
// all tx indices will be reserved.
//
// The user can adjust the txlookuplimit value for each launch after sync,
// Geth will automatically construct the missing indices or delete the extra
// indices.
func (bc *BlockChain) maintainTxIndex() {
	defer bc.wg.Done()

	// Listening to chain events and manipulate the transaction indexes.
	var (
		done   chan struct{}                  // Non-nil if background unindexing or reindexing routine is active.
		headCh = make(chan ChainHeadEvent, 1) // Buffered to avoid locking up the event feed
	)
	sub := bc.SubscribeChainHeadEvent(headCh)
	if sub == nil {
		return
	}
	defer sub.Unsubscribe()
	log.Info("Initialized transaction indexer", "limit", bc.TxLookupLimit())

	for {
		select {
		case head := <-headCh:
			if done == nil {
				done = make(chan struct{})
				go bc.indexBlocks(rawdb.ReadTxIndexTail(bc.db), head.Block.NumberU64(), done)
			}
		case <-done:
			done = nil
		case <-bc.quit:
			if done != nil {
				log.Info("Waiting background transaction indexer to exit")
				<-done
			}
			return
		}
	}
}

func (bc *BlockChain) isCachedBadBlock(block *types.Block) bool {
	if timeAt, exist := bc.badBlockCache.Get(block.Hash()); exist {
		if time.Since(timeAt) >= badBlockCacheExpire {
			bc.badBlockCache.Remove(block.Hash())
			return false
		}
		return true
	}
	return false
}

// reportBlock logs a bad block error.
func (bc *BlockChain) reportBlock(block *types.Block, receipts types.Receipts, err error) {
	rawdb.WriteBadBlock(bc.db, block)
	log.Error(summarizeBadBlock(block, receipts, bc.Config(), err))
}

// summarizeBadBlock returns a string summarizing the bad block and other
// relevant information.
func summarizeBadBlock(block *types.Block, receipts []*types.Receipt, config *params.ChainConfig, err error) string {
	var receiptString string
	for i, receipt := range receipts {
		receiptString += fmt.Sprintf("\n  %d: cumulative: %v gas: %v contract: %v status: %v tx: %v logs: %v bloom: %x state: %x",
			i, receipt.CumulativeGasUsed, receipt.GasUsed, receipt.ContractAddress.Hex(),
			receipt.Status, receipt.TxHash.Hex(), receipt.Logs, receipt.Bloom, receipt.PostState)
	}
	version, vcs := version.Info()
	platform := fmt.Sprintf("%s %s %s %s", version, runtime.Version(), runtime.GOARCH, runtime.GOOS)
	if vcs != "" {
		vcs = fmt.Sprintf("\nVCS: %s", vcs)
	}
	return fmt.Sprintf(`
########## BAD BLOCK #########
Block: %v (%#x)
Error: %v
Platform: %v%v
Chain config: %#v
Receipts: %v
##############################
`, block.Number(), block.Hash(), err, platform, vcs, config, receiptString)
}

// InsertHeaderChain attempts to insert the given header chain in to the local
// chain, possibly creating a reorg. If an error is returned, it will return the
// index number of the failing header as well an error describing what went wrong.
func (bc *BlockChain) InsertHeaderChain(chain []*types.Header) (int, error) {
	if len(chain) == 0 {
		return 0, nil
	}
	start := time.Now()
	if i, err := bc.hc.ValidateHeaderChain(chain); err != nil {
		return i, err
	}

	if !bc.chainmu.TryLock() {
		return 0, errChainStopped
	}
	defer bc.chainmu.Unlock()
	_, err := bc.hc.InsertHeaderChain(chain, start, bc.forker)
	return 0, err
}

func (bc *BlockChain) TriesInMemory() uint64 { return bc.triesInMemory }

func EnablePipelineCommit(bc *BlockChain) (*BlockChain, error) {
	bc.pipeCommit = false
	return bc, nil
}

func EnablePersistDiff(limit uint64) BlockChainOption {
	return func(chain *BlockChain) (*BlockChain, error) {
		chain.diffLayerFreezerBlockLimit = limit
		return chain, nil
	}
}

func EnableBlockValidator(chainConfig *params.ChainConfig, engine consensus.Engine, mode VerifyMode, peers verifyPeers) BlockChainOption {
	return func(bc *BlockChain) (*BlockChain, error) {
		if mode.NeedRemoteVerify() {
			vm, err := NewVerifyManager(bc, peers, mode == InsecureVerify)
			if err != nil {
				return nil, err
			}
			go vm.mainLoop()
			bc.validator = NewBlockValidator(chainConfig, bc, engine, EnableRemoteVerifyManager(vm))
		}
		return bc, nil
	}
}

func EnableDoubleSignChecker(bc *BlockChain) (*BlockChain, error) {
	bc.doubleSignMonitor = monitor.NewDoubleSignMonitor()
	return bc, nil
}

func (bc *BlockChain) GetVerifyResult(blockNumber uint64, blockHash common.Hash, diffHash common.Hash) *VerifyResult {
	var res VerifyResult
	res.BlockNumber = blockNumber
	res.BlockHash = blockHash

	if blockNumber > bc.CurrentHeader().Number.Uint64()+maxDiffForkDist {
		res.Status = types.StatusBlockTooNew
		return &res
	} else if blockNumber > bc.CurrentHeader().Number.Uint64() {
		res.Status = types.StatusBlockNewer
		return &res
	}

	header := bc.GetHeaderByHash(blockHash)
	if header == nil {
		if blockNumber > bc.CurrentHeader().Number.Uint64()-maxDiffForkDist {
			res.Status = types.StatusPossibleFork
			return &res
		}

		res.Status = types.StatusImpossibleFork
		return &res
	}

	diff := bc.GetTrustedDiffLayer(blockHash)
	if diff != nil {
		if diff.DiffHash.Load() == nil {
			hash, err := CalculateDiffHash(diff)
			if err != nil {
				res.Status = types.StatusUnexpectedError
				return &res
			}

			diff.DiffHash.Store(hash)
		}

		if diffHash != diff.DiffHash.Load().(common.Hash) {
			res.Status = types.StatusDiffHashMismatch
			return &res
		}

		res.Status = types.StatusFullVerified
		res.Root = header.Root
		return &res
	}

	res.Status = types.StatusPartiallyVerified
	res.Root = header.Root
	return &res
}

func (bc *BlockChain) GetTrustedDiffLayer(blockHash common.Hash) *types.DiffLayer {
	var diff *types.DiffLayer
	if cached, ok := bc.diffLayerCache.Get(blockHash); ok {
		diff = cached.(*types.DiffLayer)
		return diff
	}

	diffStore := bc.db.DiffStore()
	if diffStore != nil {
		diff = rawdb.ReadDiffLayer(diffStore, blockHash)
	}
	return diff
}

func CalculateDiffHash(d *types.DiffLayer) (common.Hash, error) {
	if d == nil {
		return common.Hash{}, fmt.Errorf("nil diff layer")
	}

	diff := &types.ExtDiffLayer{
		BlockHash: d.BlockHash,
		Receipts:  make([]*types.ReceiptForStorage, 0),
		Number:    d.Number,
		Codes:     d.Codes,
		Destructs: d.Destructs,
		Accounts:  d.Accounts,
		Storages:  d.Storages,
	}

	for index, account := range diff.Accounts {
		full, err := types.FullAccount(account.Blob)
		if err != nil {
			return common.Hash{}, fmt.Errorf("decode full account error: %v", err)
		}
		// set account root to empty root
		full.Root = types.EmptyRootHash
		diff.Accounts[index].Blob = types.SlimAccountRLP(*full)
	}

	rawData, err := rlp.EncodeToBytes(diff)
	if err != nil {
		return common.Hash{}, fmt.Errorf("encode new diff error: %v", err)
	}

	hasher := sha3.NewLegacyKeccak256()
	_, err = hasher.Write(rawData)
	if err != nil {
		return common.Hash{}, fmt.Errorf("hasher write error: %v", err)
	}

	var hash common.Hash
	hasher.Sum(hash[:0])
	return hash, nil
}

// SetBlockValidatorAndProcessorForTesting sets the current validator and processor.
// This method can be used to force an invalid blockchain to be verified for tests.
// This method is unsafe and should only be used before block import starts.
func (bc *BlockChain) SetBlockValidatorAndProcessorForTesting(v Validator, p Processor) {
	bc.validator = v
	bc.processor = p
}

// SetTrieFlushInterval configures how often in-memory tries are persisted to disk.
// The interval is in terms of block processing time, not wall clock.
// It is thread-safe and can be called repeatedly without side effects.
func (bc *BlockChain) SetTrieFlushInterval(interval time.Duration) {
	bc.flushInterval.Store(int64(interval))
}

// GetTrieFlushInterval gets the in-memroy tries flush interval
func (bc *BlockChain) GetTrieFlushInterval() time.Duration {
	return time.Duration(bc.flushInterval.Load())
}
