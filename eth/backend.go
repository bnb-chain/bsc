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

// Package eth implements the Ethereum protocol.
package eth

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/monitor"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/pruner"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/blobpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/txpool/locals"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vote"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/eth/protocols/bsc"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/eth/protocols/trust"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/internal/shutdowncheck"
	"github.com/ethereum/go-ethereum/internal/version"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/dnsdisc"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	gethversion "github.com/ethereum/go-ethereum/version"
)

const (
	ChainDBNamespace = "eth/db/chaindata/"
	JournalFileName  = "trie.journal"
	ChainData        = "chaindata"
)

// Config contains the configuration options of the ETH protocol.
// Deprecated: use ethconfig.Config instead.
type Config = ethconfig.Config

// Ethereum implements the Ethereum full node service.
type Ethereum struct {
	// core protocol objects
	config         *ethconfig.Config
	txPool         *txpool.TxPool
	localTxTracker *locals.TxTracker
	blockchain     *core.BlockChain

	handler *handler
	discmix *enode.FairMix

	// DB interfaces
	chainDb ethdb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests     chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer      *core.ChainIndexer             // Bloom indexer operating during block imports
	closeBloomHandler chan struct{}

	APIBackend *EthAPIBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	etherbase common.Address

	networkID     uint64
	netRPCService *ethapi.NetAPI

	p2pServer *p2p.Server

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and etherbase)

	shutdownTracker *shutdowncheck.ShutdownTracker // Tracks if and when the node has shutdown ungracefully

	votePool *vote.VotePool
}

// New creates a new Ethereum object (including the initialisation of the common Ethereum object),
// whose lifecycle will be managed by the provided node.
func New(stack *node.Node, config *ethconfig.Config) (*Ethereum, error) {
	// Ensure configuration values are compatible and sane
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	if !config.TriesVerifyMode.IsValid() {
		return nil, fmt.Errorf("invalid tries verify mode %d", config.TriesVerifyMode)
	}
	if config.Miner.GasPrice == nil || config.Miner.GasPrice.Sign() <= 0 {
		log.Warn("Sanitizing invalid miner gas price", "provided", config.Miner.GasPrice, "updated", ethconfig.Defaults.Miner.GasPrice)
		config.Miner.GasPrice = new(big.Int).Set(ethconfig.Defaults.Miner.GasPrice)
	}

	// Assemble the Ethereum object
	chainDb, err := stack.OpenAndMergeDatabase(ChainData, ChainDBNamespace, false, config)
	if err != nil {
		return nil, err
	}
	config.StateScheme, err = rawdb.ParseStateScheme(config.StateScheme, chainDb)
	if err != nil {
		return nil, err
	}
	// Redistribute memory allocation from in-memory trie node garbage collection
	// to other caches when an archive node is requested.
	if config.StateScheme == rawdb.HashScheme && config.NoPruning && config.TrieDirtyCache > 0 {
		if config.SnapshotCache > 0 {
			config.TrieCleanCache += config.TrieDirtyCache * 3 / 5
			config.SnapshotCache += config.TrieDirtyCache * 2 / 5
		} else {
			config.TrieCleanCache += config.TrieDirtyCache
		}
		config.TrieDirtyCache = 0
	}
	// Optimize memory distribution by reallocating surplus allowance from the
	// dirty cache to the clean cache.
	if config.StateScheme == rawdb.PathScheme && config.TrieDirtyCache > pathdb.MaxDirtyBufferSize/1024/1024 {
		log.Info("Capped dirty cache size", "provided", common.StorageSize(config.TrieDirtyCache)*1024*1024,
			"adjusted", common.StorageSize(pathdb.MaxDirtyBufferSize))
		log.Info("Clean cache size", "provided", common.StorageSize(config.TrieCleanCache)*1024*1024,
			"adjusted", common.StorageSize(config.TrieCleanCache+config.TrieDirtyCache-pathdb.MaxDirtyBufferSize/1024/1024)*1024*1024)
		config.TrieCleanCache += config.TrieDirtyCache - pathdb.MaxDirtyBufferSize/1024/1024
		config.TrieDirtyCache = pathdb.MaxDirtyBufferSize / 1024 / 1024
	}
	log.Info("Allocated memory caches",
		"state_scheme", config.StateScheme,
		"trie_clean_cache", common.StorageSize(config.TrieCleanCache)*1024*1024,
		"trie_dirty_cache", common.StorageSize(config.TrieDirtyCache)*1024*1024,
		"snapshot_cache", common.StorageSize(config.SnapshotCache)*1024*1024)
	// Try to recover offline state pruning only in hash-based.
	if config.StateScheme == rawdb.HashScheme {
		if err := pruner.RecoverPruning(stack.ResolvePath(""), chainDb, config.TriesInMemory); err != nil {
			log.Error("Failed to recover state", "error", err)
		}
	}
	chainConfig, genesisHash, err := core.LoadChainConfig(chainDb, config.Genesis)
	if err != nil {
		return nil, err
	}
	// Override the chain config with provided settings.
	var overrides core.ChainOverrides
	if config.OverridePassedForkTime != nil {
		chainConfig.ShanghaiTime = config.OverridePassedForkTime
		chainConfig.KeplerTime = config.OverridePassedForkTime
		chainConfig.FeynmanTime = config.OverridePassedForkTime
		chainConfig.FeynmanFixTime = config.OverridePassedForkTime
		chainConfig.CancunTime = config.OverridePassedForkTime
		chainConfig.HaberTime = config.OverridePassedForkTime
		chainConfig.HaberFixTime = config.OverridePassedForkTime
		chainConfig.BohrTime = config.OverridePassedForkTime
		overrides.OverridePassedForkTime = config.OverridePassedForkTime
	}
	if config.OverridePascal != nil {
		chainConfig.PascalTime = config.OverridePascal
		overrides.OverridePascal = config.OverridePascal
	}
	if config.OverridePrague != nil {
		chainConfig.PragueTime = config.OverridePrague
		overrides.OverridePrague = config.OverridePrague
	}
	if config.OverrideVerkle != nil {
		chainConfig.VerkleTime = config.OverrideVerkle
		overrides.OverrideVerkle = config.OverrideVerkle
	}

	// startup ancient freeze
	freezeDb := chainDb
	if stack.CheckIfMultiDataBase() {
		freezeDb = chainDb.BlockStore()
	}
	if err = freezeDb.SetupFreezerEnv(&ethdb.FreezerEnv{
		ChainCfg:         chainConfig,
		BlobExtraReserve: config.BlobExtraReserve,
	}); err != nil {
		return nil, err
	}

	networkID := config.NetworkId
	if networkID == 0 {
		networkID = chainConfig.ChainID.Uint64()
	}
	eth := &Ethereum{
		config:            config,
		chainDb:           chainDb,
		eventMux:          stack.EventMux(),
		accountManager:    stack.AccountManager(),
		closeBloomHandler: make(chan struct{}),
		networkID:         networkID,
		gasPrice:          config.Miner.GasPrice,
		etherbase:         config.Miner.Etherbase,
		bloomRequests:     make(chan chan *bloombits.Retrieval),
		bloomIndexer:      core.NewBloomIndexer(chainDb, params.BloomBitsBlocks, params.BloomConfirms),
		p2pServer:         stack.Server(),
		discmix:           enode.NewFairMix(0),
		shutdownTracker:   shutdowncheck.NewShutdownTracker(chainDb),
	}

	eth.APIBackend = &EthAPIBackend{stack.Config().ExtRPCEnabled(), stack.Config().AllowUnprotectedTxs, eth, nil}
	if eth.APIBackend.allowUnprotectedTxs {
		log.Info("Unprotected transactions allowed")
	}
	ethAPI := ethapi.NewBlockChainAPI(eth.APIBackend)
	eth.engine, err = ethconfig.CreateConsensusEngine(chainConfig, chainDb, ethAPI, genesisHash)
	if err != nil {
		return nil, err
	}

	bcVersion := rawdb.ReadDatabaseVersion(chainDb)
	var dbVer = "<nil>"
	if bcVersion != nil {
		dbVer = fmt.Sprintf("%d", *bcVersion)
	}
	log.Info("Initialising Ethereum protocol", "network", networkID, "dbversion", dbVer)

	if !config.SkipBcVersionCheck {
		if bcVersion != nil && *bcVersion > core.BlockChainVersion {
			return nil, fmt.Errorf("database version is v%d, Geth %s only supports v%d", *bcVersion, version.WithMeta, core.BlockChainVersion)
		} else if bcVersion == nil || *bcVersion < core.BlockChainVersion {
			if bcVersion != nil { // only print warning on upgrade, not on init
				log.Warn("Upgrade blockchain database version", "from", dbVer, "to", core.BlockChainVersion)
			}
			rawdb.WriteDatabaseVersion(chainDb, core.BlockChainVersion)
		}
	}
	var (
		journalFilePath string
		path            string
	)
	if stack.CheckIfMultiDataBase() {
		path = ChainData + "/state"
	} else {
		path = ChainData
	}
	journalFilePath = stack.ResolvePath(path) + "/" + JournalFileName
	var (
		vmConfig = vm.Config{
			EnablePreimageRecording: config.EnablePreimageRecording,
		}
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:      config.TrieCleanCache,
			TrieCleanNoPrefetch: config.NoPrefetch,
			TrieDirtyLimit:      config.TrieDirtyCache,
			TrieDirtyDisabled:   config.NoPruning,
			TrieTimeLimit:       config.TrieTimeout,
			NoTries:             config.TriesVerifyMode != core.LocalVerify,
			SnapshotLimit:       config.SnapshotCache,
			TriesInMemory:       config.TriesInMemory,
			Preimages:           config.Preimages,
			StateHistory:        config.StateHistory,
			StateScheme:         config.StateScheme,
			PathSyncFlush:       config.PathSyncFlush,
			JournalFilePath:     journalFilePath,
			JournalFile:         config.JournalFileEnabled,
		}
	)
	if config.VMTrace != "" {
		traceConfig := json.RawMessage("{}")
		if config.VMTraceJsonConfig != "" {
			traceConfig = json.RawMessage(config.VMTraceJsonConfig)
		}
		t, err := tracers.LiveDirectory.New(config.VMTrace, traceConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create tracer %s: %v", config.VMTrace, err)
		}
		vmConfig.Tracer = t
	}

	bcOps := make([]core.BlockChainOption, 0)
	if config.PersistDiff {
		bcOps = append(bcOps, core.EnablePersistDiff(config.DiffBlock))
	}
	if stack.Config().EnableDoubleSignMonitor {
		bcOps = append(bcOps, core.EnableDoubleSignChecker)
	}

	peers := newPeerSet()
	bcOps = append(bcOps, core.EnableBlockValidator(chainConfig, config.TriesVerifyMode, peers))
	// TODO (MariusVanDerWijden) get rid of shouldPreserve in a follow-up PR
	shouldPreserve := func(header *types.Header) bool {
		return false
	}
	eth.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, config.Genesis, &overrides, eth.engine, vmConfig, shouldPreserve, &config.TransactionHistory, bcOps...)
	if err != nil {
		return nil, err
	}
	eth.bloomIndexer.Start(eth.blockchain)

	if config.BlobPool.Datadir != "" {
		config.BlobPool.Datadir = stack.ResolvePath(config.BlobPool.Datadir)
	}
	blobPool := blobpool.New(config.BlobPool, eth.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = stack.ResolvePath(config.TxPool.Journal)
	}
	legacyPool := legacypool.New(config.TxPool, eth.blockchain)

	eth.txPool, err = txpool.New(config.TxPool.PriceLimit, eth.blockchain, []txpool.SubPool{legacyPool, blobPool})
	if err != nil {
		return nil, err
	}

	if !config.TxPool.NoLocals {
		rejournal := config.TxPool.Rejournal
		if rejournal < time.Second {
			log.Warn("Sanitizing invalid txpool journal time", "provided", rejournal, "updated", time.Second)
			rejournal = time.Second
		}
		eth.localTxTracker = locals.New(config.TxPool.Journal, rejournal, eth.blockchain.Config(), eth.txPool)
		stack.RegisterLifecycle(eth.localTxTracker)
	}
	// Permit the downloader to use the trie cache allowance during fast sync
	cacheLimit := cacheConfig.TrieCleanLimit + cacheConfig.TrieDirtyLimit + cacheConfig.SnapshotLimit
	if eth.handler, err = newHandler(&handlerConfig{
		NodeID:                 eth.p2pServer.Self().ID(),
		Database:               chainDb,
		Chain:                  eth.blockchain,
		TxPool:                 eth.txPool,
		Network:                networkID,
		Sync:                   config.SyncMode,
		BloomCache:             uint64(cacheLimit),
		EventMux:               eth.eventMux,
		RequiredBlocks:         config.RequiredBlocks,
		DirectBroadcast:        config.DirectBroadcast,
		DisablePeerTxBroadcast: config.DisablePeerTxBroadcast,
		PeerSet:                peers,
	}); err != nil {
		return nil, err
	}

	eth.miner = miner.New(eth, &config.Miner, eth.EventMux(), eth.engine)
	eth.miner.SetExtra(makeExtraData(config.Miner.ExtraData))
	eth.miner.SetPrioAddresses(config.TxPool.Locals)

	// Create voteManager instance
	if posa, ok := eth.engine.(consensus.PoSA); ok {
		// Create votePool instance
		votePool := vote.NewVotePool(eth.blockchain, posa)
		eth.votePool = votePool
		if parlia, ok := eth.engine.(*parlia.Parlia); ok {
			if !config.Miner.DisableVoteAttestation {
				// if there is no VotePool in Parlia Engine, the miner can't get votes for assembling
				parlia.VotePool = votePool
			}
		} else {
			return nil, errors.New("Engine is not Parlia type")
		}
		log.Info("Create votePool successfully")
		eth.handler.votepool = votePool
		if stack.Config().EnableMaliciousVoteMonitor {
			eth.handler.maliciousVoteMonitor = monitor.NewMaliciousVoteMonitor()
			log.Info("Create MaliciousVoteMonitor successfully")
		}

		if config.Miner.VoteEnable {
			conf := stack.Config()
			blsPasswordPath := stack.ResolvePath(conf.BLSPasswordFile)
			blsWalletPath := stack.ResolvePath(conf.BLSWalletDir)
			voteJournalPath := stack.ResolvePath(conf.VoteJournalDir)
			if _, err := vote.NewVoteManager(eth, eth.blockchain, votePool, voteJournalPath, blsPasswordPath, blsWalletPath, posa); err != nil {
				log.Error("Failed to Initialize voteManager", "err", err)
				return nil, err
			}
			log.Info("Create voteManager successfully")
		}
	}
	eth.APIBackend.gpo = gasprice.NewOracle(eth.APIBackend, config.GPO, config.Miner.GasPrice)

	// Start the RPC service
	eth.netRPCService = ethapi.NewNetAPI(eth.p2pServer, networkID)

	// Register the backend on the node
	stack.RegisterAPIs(eth.APIs())
	stack.RegisterProtocols(eth.Protocols())
	stack.RegisterLifecycle(eth)

	// Successful startup; push a marker and check previous unclean shutdowns.
	eth.shutdownTracker.MarkStartup()

	return eth, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(gethversion.Major<<16 | gethversion.Minor<<8 | gethversion.Patch),
			"geth",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize-params.ForkIDSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize-params.ForkIDSize)
		extra = nil
	}
	return extra
}

// APIs return the collection of RPC services the ethereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Ethereum) APIs() []rpc.API {
	apis := ethapi.GetAPIs(s.APIBackend)

	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "eth",
			Service:   NewEthereumAPI(s),
		}, {
			Namespace: "miner",
			Service:   NewMinerAPI(s),
		}, {
			Namespace: "eth",
			Service:   downloader.NewDownloaderAPI(s.handler.downloader, s.blockchain, s.eventMux),
		}, {
			Namespace: "eth",
			Service:   filters.NewFilterAPI(filters.NewFilterSystem(s.APIBackend, filters.Config{}), s.config.RangeLimit),
		}, {
			Namespace: "admin",
			Service:   NewAdminAPI(s),
		}, {
			Namespace: "debug",
			Service:   NewDebugAPI(s),
		}, {
			Namespace: "net",
			Service:   s.netRPCService,
		},
	}...)
}

func (s *Ethereum) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Ethereum) Etherbase() (eb common.Address, err error) {
	s.lock.RLock()
	etherbase := s.etherbase
	s.lock.RUnlock()

	if etherbase != (common.Address{}) {
		return etherbase, nil
	}
	return common.Address{}, errors.New("etherbase must be explicitly specified")
}

// SetEtherbase sets the mining reward address.
func (s *Ethereum) SetEtherbase(etherbase common.Address) {
	s.lock.Lock()
	s.etherbase = etherbase
	s.lock.Unlock()

	s.miner.SetEtherbase(etherbase)
}

// StartMining starts the miner with the given number of CPU threads. If mining
// is already running, this method adjust the number of threads allowed to use
// and updates the minimum price required by the transaction pool.
func (s *Ethereum) StartMining() error {
	// If the miner was not running, initialize it
	if !s.IsMining() {
		// Propagate the initial price point to the transaction pool
		s.lock.RLock()
		price := s.gasPrice
		s.lock.RUnlock()
		s.txPool.SetGasTip(price)

		// Configure the local mining address
		eb, err := s.Etherbase()
		if err != nil {
			log.Error("Cannot start mining without etherbase", "err", err)
			return fmt.Errorf("etherbase missing: %v", err)
		}
		if parlia, ok := s.engine.(*parlia.Parlia); ok {
			wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
			if wallet == nil || err != nil {
				log.Error("Etherbase account unavailable locally", "err", err)
				return fmt.Errorf("signer missing: %v", err)
			}
			parlia.Authorize(eb, wallet.SignData, wallet.SignTx)
		}

		go s.miner.Start()
	}
	return nil
}

// StopMining terminates the miner, both at the consensus engine level as well as
// at the block creation level.
func (s *Ethereum) StopMining() {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		th.SetThreads(-1)
	}
	// Stop the block creating itself
	s.miner.Stop()
}

func (s *Ethereum) IsMining() bool      { return s.miner.Mining() }
func (s *Ethereum) Miner() *miner.Miner { return s.miner }

func (s *Ethereum) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *Ethereum) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *Ethereum) TxPool() *txpool.TxPool             { return s.txPool }
func (s *Ethereum) VotePool() *vote.VotePool           { return s.votePool }
func (s *Ethereum) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Ethereum) Engine() consensus.Engine           { return s.engine }
func (s *Ethereum) ChainDb() ethdb.Database            { return s.chainDb }
func (s *Ethereum) IsListening() bool                  { return true } // Always listening
func (s *Ethereum) Downloader() *downloader.Downloader { return s.handler.downloader }
func (s *Ethereum) Synced() bool                       { return s.handler.synced.Load() }
func (s *Ethereum) SetSynced()                         { s.handler.enableSyncedFeatures() }
func (s *Ethereum) ArchiveMode() bool                  { return s.config.NoPruning }
func (s *Ethereum) BloomIndexer() *core.ChainIndexer   { return s.bloomIndexer }
func (s *Ethereum) SyncMode() downloader.SyncMode {
	mode, _ := s.handler.chainSync.modeAndLocalHead()
	return mode
}

// Protocols returns all the currently configured
// network protocols to start.
func (s *Ethereum) Protocols() []p2p.Protocol {
	protos := eth.MakeProtocols((*ethHandler)(s.handler), s.networkID, s.discmix)
	if !s.config.DisableSnapProtocol && s.config.SnapshotCache > 0 {
		protos = append(protos, snap.MakeProtocols((*snapHandler)(s.handler))...)
	}
	if s.config.EnableTrustProtocol {
		protos = append(protos, trust.MakeProtocols((*trustHandler)(s.handler))...)
	}
	protos = append(protos, bsc.MakeProtocols((*bscHandler)(s.handler))...)

	return protos
}

// Start implements node.Lifecycle, starting all internal goroutines needed by the
// Ethereum protocol implementation.
func (s *Ethereum) Start() error {
	eth.StartENRFilter(s.blockchain, s.p2pServer)
	s.setupDiscovery()

	// Start the bloom bits servicing goroutines
	s.startBloomHandlers(params.BloomBitsBlocks)

	// Regularly update shutdown marker
	s.shutdownTracker.Start()

	// Start the networking layer
	s.handler.Start(s.p2pServer.MaxPeers, s.p2pServer.MaxPeersPerIP)
	return nil
}

func (s *Ethereum) setupDiscovery() error {
	eth.StartENRUpdater(s.blockchain, s.p2pServer.LocalNode())

	// Add eth nodes from DNS.
	dnsclient := dnsdisc.NewClient(dnsdisc.Config{})
	if len(s.config.EthDiscoveryURLs) > 0 {
		iter, err := dnsclient.NewIterator(s.config.EthDiscoveryURLs...)
		if err != nil {
			return err
		}
		s.discmix.AddSource(iter)
	}

	// Add snap nodes from DNS.
	if len(s.config.SnapDiscoveryURLs) > 0 {
		iter, err := dnsclient.NewIterator(s.config.SnapDiscoveryURLs...)
		if err != nil {
			return err
		}
		s.discmix.AddSource(iter)
	}

	// Add trust nodes from DNS.
	if len(s.config.TrustDiscoveryURLs) > 0 {
		iter, err := dnsclient.NewIterator(s.config.TrustDiscoveryURLs...)
		if err != nil {
			return err
		}
		s.discmix.AddSource(iter)
	}

	// Add bsc nodes from DNS.
	if len(s.config.BscDiscoveryURLs) > 0 {
		iter, err := dnsclient.NewIterator(s.config.BscDiscoveryURLs...)
		if err != nil {
			return err
		}
		s.discmix.AddSource(iter)
	}

	// Add DHT nodes from discv5.
	if s.p2pServer.DiscoveryV5() != nil {
		filter := eth.NewNodeFilter(s.blockchain)
		iter := enode.Filter(s.p2pServer.DiscoveryV5().RandomNodes(), filter)
		s.discmix.AddSource(iter)
	}

	return nil
}

// Stop implements node.Lifecycle, terminating all internal goroutines used by the
// Ethereum protocol.
func (s *Ethereum) Stop() error {
	if s.miner.Mining() {
		s.miner.TryWaitProposalDoneWhenStopping()
	}
	// Stop all the peer-related stuff first.
	s.discmix.Close()
	s.handler.Stop()

	// Then stop everything else.
	s.bloomIndexer.Close()
	close(s.closeBloomHandler)
	s.txPool.Close()
	s.miner.Close()
	s.blockchain.Stop()
	s.engine.Close()

	// Clean shutdown marker as the last thing before closing db
	s.shutdownTracker.Stop()

	s.chainDb.Close()
	s.eventMux.Stop()

	return nil
}
