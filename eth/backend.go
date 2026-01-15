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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/filtermaps"
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

const (
	MaxBlockHandleDelayMs = 3000 // max delay for block handles, max 3000 ms

	// This is the fairness knob for the discovery mixer. When looking for peers, we'll
	// wait this long for a single source of candidates before moving on and trying other
	// sources. If this timeout expires, the source will be skipped in this round, but it
	// will continue to fetch in the background and will have a chance with a new timeout
	// in the next rounds, giving it overall more time but a proportionally smaller share.
	// We expect a normal source to produce ~10 candidates per second.
	discmixTimeout = 100 * time.Millisecond

	// discoveryPrefetchBuffer is the number of peers to pre-fetch from a discovery
	// source. It is useful to avoid the negative effects of potential longer timeouts
	// in the discovery, keeping dial progress while waiting for the next batch of
	// candidates.
	discoveryPrefetchBuffer = 32

	// maxParallelENRRequests is the maximum number of parallel ENR requests that can be
	// performed by a disc/v4 source.
	maxParallelENRRequests = 16
)

var (
	sendBlockTimer        = metrics.NewRegisteredTimer("chain/delay/block/send", nil)
	recvBlockTimer        = metrics.NewRegisteredTimer("chain/delay/block/recv", nil)
	startInsertBlockTimer = metrics.NewRegisteredTimer("chain/delay/block/insert", nil)
	startMiningTimer      = metrics.NewRegisteredTimer("chain/delay/block/mining", nil)
	importedBlockTimer    = metrics.NewRegisteredTimer("chain/delay/block/imported", nil)
	sendVoteTimer         = metrics.NewRegisteredTimer("chain/delay/vote/send", nil)
	firstVoteTimer        = metrics.NewRegisteredTimer("chain/delay/vote/first", nil)
	majorityVoteTimer     = metrics.NewRegisteredTimer("chain/delay/vote/majority", nil)
)

// Config contains the configuration options of the ETH protocol.
// Deprecated: use ethconfig.Config instead.
type Config = ethconfig.Config

// Ethereum implements the Ethereum full node service.
type Ethereum struct {
	// core protocol objects
	config         *ethconfig.Config
	txPool         *txpool.TxPool
	blobTxPool     *blobpool.BlobPool
	localTxTracker *locals.TxTracker
	blockchain     *core.BlockChain

	handler *handler
	discmix *enode.FairMix
	dropper *dropper

	// DB interfaces
	chainDb ethdb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	filterMaps      *filtermaps.FilterMaps
	closeFilterMaps chan chan struct{}

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
	stopCh   chan struct{}
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
	if !config.HistoryMode.IsValid() {
		return nil, fmt.Errorf("invalid history mode %d", config.HistoryMode)
	}
	if config.Miner.GasPrice == nil || config.Miner.GasPrice.Sign() <= 0 {
		log.Warn("Sanitizing invalid miner gas price", "provided", config.Miner.GasPrice, "updated", ethconfig.Defaults.Miner.GasPrice)
		config.Miner.GasPrice = new(big.Int).Set(ethconfig.Defaults.Miner.GasPrice)
	}

	chainDb, err := stack.OpenAndMergeDatabase(ChainData, ChainDBNamespace, false, config)
	if err != nil {
		return nil, err
	}
	config.StateScheme, err = rawdb.ParseStateScheme(config.StateScheme, chainDb)
	if err != nil {
		return nil, err
	}
	noTries := config.TriesVerifyMode != core.LocalVerify
	if noTries && config.StateScheme != rawdb.HashScheme {
		config.StateScheme = rawdb.HashScheme
		log.Info("Using hash-based state scheme since tries are disabled")
	}

	if config.StateScheme == rawdb.HashScheme && config.NoPruning && config.TrieDirtyCache > 0 {
		if config.SnapshotCache > 0 {
			config.TrieCleanCache += config.TrieDirtyCache * 3 / 5
			config.SnapshotCache += config.TrieDirtyCache * 2 / 5
		} else {
			config.TrieCleanCache += config.TrieDirtyCache
		}
		config.TrieDirtyCache = 0
	}
	if config.StateScheme == rawdb.PathScheme && config.TrieDirtyCache > pathdb.MaxDirtyBufferSize()/1024/1024 {
		log.Info("Capped dirty cache size", "provided", common.StorageSize(config.TrieDirtyCache)*1024*1024,
			"adjusted", common.StorageSize(pathdb.MaxDirtyBufferSize()))
		log.Info("Clean cache size", "provided", common.StorageSize(config.TrieCleanCache)*1024*1024,
			"adjusted", common.StorageSize(config.TrieCleanCache+config.TrieDirtyCache-pathdb.MaxDirtyBufferSize()/1024/1024)*1024*1024)
		config.TrieCleanCache += config.TrieDirtyCache - pathdb.MaxDirtyBufferSize()/1024/1024
		config.TrieDirtyCache = pathdb.MaxDirtyBufferSize() / 1024 / 1024
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

	// Here we determine genesis hash and active ChainConfig.
	// We need these to figure out the consensus parameters and to set up history pruning.
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
		chainConfig.PascalTime = config.OverridePassedForkTime
		chainConfig.PragueTime = config.OverridePassedForkTime
		overrides.OverridePassedForkTime = config.OverridePassedForkTime
	}
	if config.OverrideLorentz != nil {
		chainConfig.LorentzTime = config.OverrideLorentz
		overrides.OverrideLorentz = config.OverrideLorentz
	}
	if config.OverrideMaxwell != nil {
		chainConfig.MaxwellTime = config.OverrideMaxwell
		overrides.OverrideMaxwell = config.OverrideMaxwell
	}
	if config.OverrideFermi != nil {
		chainConfig.FermiTime = config.OverrideFermi
		overrides.OverrideFermi = config.OverrideFermi
	}
	if config.OverrideOsaka != nil {
		chainConfig.OsakaTime = config.OverrideOsaka
		overrides.OverrideOsaka = config.OverrideOsaka
	}
	if config.OverrideMendel != nil {
		chainConfig.MendelTime = config.OverrideMendel
		overrides.OverrideMendel = config.OverrideMendel
	}
	if config.OverrideBPO1 != nil {
		chainConfig.BPO1Time = config.OverrideBPO1
		overrides.OverrideBPO1 = config.OverrideBPO1
	}
	if config.OverrideBPO2 != nil {
		chainConfig.BPO2Time = config.OverrideBPO2
		overrides.OverrideBPO2 = config.OverrideBPO2
	}
	if config.OverrideVerkle != nil {
		chainConfig.VerkleTime = config.OverrideVerkle
		overrides.OverrideVerkle = config.OverrideVerkle
	}

	// startup ancient freeze
	freezeDb := chainDb
	if err = freezeDb.SetupFreezerEnv(&ethdb.FreezerEnv{
		ChainCfg:         chainConfig,
		BlobExtraReserve: config.BlobExtraReserve,
	}, config.BlockHistory); err != nil {
		return nil, err
	}
	// Set networkID to chainID by default.
	networkID := config.NetworkId
	if networkID == 0 {
		networkID = chainConfig.ChainID.Uint64()
	}

	// Assemble the Ethereum object.
	eth := &Ethereum{
		config:          config,
		chainDb:         chainDb,
		eventMux:        stack.EventMux(),
		accountManager:  stack.AccountManager(),
		networkID:       networkID,
		gasPrice:        config.Miner.GasPrice,
		etherbase:       config.Miner.Etherbase,
		p2pServer:       stack.Server(),
		discmix:         enode.NewFairMix(discmixTimeout),
		shutdownTracker: shutdowncheck.NewShutdownTracker(chainDb),
		stopCh:          make(chan struct{}),
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

	// Create BlockChain object.
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

	path := ChainData
	if stack.CheckIfMultiDataBase() {
		path = ChainData + "/state"
	}
	journalFilePath := stack.ResolvePath(path) + "/" + JournalFileName
	var (
		options = &core.BlockChainConfig{
			TrieCleanLimit:        config.TrieCleanCache,
			NoPrefetch:            config.NoPrefetch,
			EnableBAL:             config.EnableBAL,
			TrieDirtyLimit:        config.TrieDirtyCache,
			ArchiveMode:           config.NoPruning,
			TrieTimeLimit:         config.TrieTimeout,
			NoTries:               noTries,
			SnapshotLimit:         config.SnapshotCache,
			TriesInMemory:         config.TriesInMemory,
			Preimages:             config.Preimages,
			StateHistory:          config.StateHistory,
			StateScheme:           config.StateScheme,
			PathSyncFlush:         config.PathSyncFlush,
			JournalFilePath:       journalFilePath,
			EnableIncr:            config.EnableIncrSnapshots,
			IncrHistoryPath:       config.IncrSnapshotPath,
			IncrHistory:           config.IncrSnapshotBlockInterval,
			IncrStateBuffer:       config.IncrSnapshotStateBuffer,
			IncrKeptBlocks:        config.IncrSnapshotKeptBlocks,
			UseRemoteIncrSnapshot: config.UseRemoteIncrSnapshot,
			RemoteIncrURL:         config.RemoteIncrSnapshotURL,
			ChainHistoryMode:      config.HistoryMode,
			TxLookupLimit:         int64(min(config.TransactionHistory, math.MaxInt64)),
			VmConfig: vm.Config{
				EnablePreimageRecording:   config.EnablePreimageRecording,
				EnableWitnessStats:        config.EnableWitnessStats,
				StatelessSelfValidation:   config.StatelessSelfValidation,
				EnableOpcodeOptimizations: config.EnableOpcodeOptimizing,
			},
			// Enables file journaling for the trie database. The journal files will be stored
			// within the data directory. The corresponding paths will be either:
			// - DATADIR/triedb/merkle.journal
			// - DATADIR/triedb/verkle.journal
			TrieJournalDirectory: stack.ResolvePath("triedb"),
			StateSizeTracking:    config.EnableStateSizeTracking,
		}
	)
	if config.DisableTxIndexer {
		log.Warn("The TxIndexer is disabled. Please note that the next time you re-enable it, it may affect the node performance because of rebuilding the tx index.")
		options.TxLookupLimit = -1
	}

	if config.VMTrace != "" {
		traceConfig := json.RawMessage("{}")
		if config.VMTraceJsonConfig != "" {
			traceConfig = json.RawMessage(config.VMTraceJsonConfig)
		}
		t, err := tracers.LiveDirectory.New(config.VMTrace, traceConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create tracer %s: %v", config.VMTrace, err)
		}
		options.VmConfig.Tracer = t
	}

	bcOps := make([]core.BlockChainOption, 0)
	if stack.Config().EnableDoubleSignMonitor {
		bcOps = append(bcOps, core.EnableDoubleSignChecker)
	}
	// Override the chain config with provided settings.
	options.Overrides = &overrides
	eth.blockchain, err = core.NewBlockChain(chainDb, config.Genesis, eth.engine, options, bcOps...)
	if err != nil {
		return nil, err
	}

	// Initialize filtermaps log index.
	// Auto-enable checkpoint file
	checkpointFile := filepath.Join(stack.DataDir(), "geth", "filtermap_checkpoints.json")

	fmConfig := filtermaps.Config{
		History:            config.LogHistory,
		Disabled:           config.LogNoHistory,
		CheckpointFileName: checkpointFile,
		HashScheme:         config.StateScheme == rawdb.HashScheme,
	}
	chainView := eth.newChainView(eth.blockchain.CurrentBlock())
	historyCutoff, _ := eth.blockchain.HistoryPruningCutoff()
	var finalBlock uint64
	if fb := eth.blockchain.CurrentFinalBlock(); fb != nil {
		finalBlock = fb.Number.Uint64()
	}
	filterMaps, err := filtermaps.NewFilterMaps(chainDb, chainView, historyCutoff, finalBlock, filtermaps.DefaultParams, fmConfig)
	if err != nil {
		return nil, err
	}
	eth.filterMaps = filterMaps
	eth.closeFilterMaps = make(chan chan struct{})

	// TxPool
	if config.TxPool.Journal != "" {
		config.TxPool.Journal = stack.ResolvePath(config.TxPool.Journal)
	}
	legacyPool := legacypool.New(config.TxPool, eth.blockchain)

	if config.BlobPool.Datadir != "" {
		config.BlobPool.Datadir = stack.ResolvePath(config.BlobPool.Datadir)
	}
	eth.blobTxPool = blobpool.New(config.BlobPool, eth.blockchain, legacyPool.HasPendingAuth)

	eth.txPool, err = txpool.New(config.TxPool.PriceLimit, eth.blockchain, []txpool.SubPool{legacyPool, eth.blobTxPool})
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
	cacheLimit := options.TrieCleanLimit + options.TrieDirtyLimit + options.SnapshotLimit
	if eth.handler, err = newHandler(&handlerConfig{
		NodeID:                    eth.p2pServer.Self().ID(),
		Database:                  chainDb,
		Chain:                     eth.blockchain,
		TxPool:                    eth.txPool,
		Network:                   networkID,
		Sync:                      config.SyncMode,
		BloomCache:                uint64(cacheLimit),
		EventMux:                  eth.eventMux,
		RequiredBlocks:            config.RequiredBlocks,
		DirectBroadcast:           config.DirectBroadcast,
		EnableEVNFeatures:         stack.Config().EnableEVNFeatures,
		EnableBAL:                 config.EnableBAL,
		EVNNodeIdsWhitelist:       stack.Config().P2P.EVNNodeIdsWhitelist,
		ProxyedValidatorAddresses: stack.Config().P2P.ProxyedValidatorAddresses,
		ProxyedNodeIds:            stack.Config().P2P.ProxyedNodeIds,
		DisablePeerTxBroadcast:    config.DisablePeerTxBroadcast,
		PeerSet:                   newPeerSet(),
		EnableQuickBlockFetching:  stack.Config().EnableQuickBlockFetching,
	}); err != nil {
		return nil, err
	}

	eth.dropper = newDropper(eth.p2pServer.MaxDialedConns(), eth.p2pServer.MaxInboundConns())

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
		// For version >= 1.6.4, use compact format: [version(uint32), commitID, go_version, os]
		commitID := ""
		git, ok := version.VCS()
		if ok && len(git.Commit) >= 8 {
			commitID = git.Commit[:8]
		}

		osName := runtime.GOOS
		if len(osName) > 3 {
			osName = osName[:3]
		}

		versionWord := uint32(gethversion.Major<<16 | gethversion.Minor<<8 | gethversion.Patch)
		extra, _ = rlp.EncodeToBytes([]interface{}{
			versionWord,
			commitID,
			runtime.Version(),
			osName,
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
	if p, ok := s.engine.(*parlia.Parlia); ok {
		apis = append(apis, p.APIs(s.BlockChain())...)
	}

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

// waitForSyncAndMaxwell waits for the node to be fully synced and Maxwell fork to be active
func (s *Ethereum) waitForSyncAndMaxwell(parlia *parlia.Parlia) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	retryCount := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if !s.Synced() {
				continue
			}
			// Check if Maxwell fork is active
			header := s.blockchain.CurrentHeader()
			if header == nil {
				continue
			}
			chainConfig := s.blockchain.Config()
			if !chainConfig.IsMaxwell(header.Number, header.Time) {
				continue
			}
			log.Info("Node is synced and Maxwell fork is active, proceeding with node ID registration")
			err := s.updateNodeID(parlia)
			if err == nil {
				return
			}
			retryCount++
			if retryCount > 3 {
				log.Error("Failed to update node ID exceed max retry count", "retryCount", retryCount, "err", err)
				return
			}
		}
	}
}

// updateNodeID registers the node ID with the StakeHub contract
func (s *Ethereum) updateNodeID(parlia *parlia.Parlia) error {
	nonce, err := s.APIBackend.GetPoolNonce(context.Background(), s.etherbase)
	if err != nil {
		return fmt.Errorf("failed to get nonce: %v", err)
	}

	// Get currently registered node IDs
	registeredIDs, err := parlia.GetNodeIDs()
	if err != nil {
		log.Error("Failed to get registered node IDs", "err", err)
		return err
	}

	// Create a set of registered IDs for quick lookup
	registeredSet := make(map[enode.ID]struct{}, len(registeredIDs))
	for _, id := range registeredIDs {
		registeredSet[id] = struct{}{}
	}

	// Handle removals first
	if err := s.handleRemovals(parlia, nonce, registeredSet); err != nil {
		return err
	}
	nonce++

	// Handle additions
	return s.handleAdditions(parlia, nonce, registeredSet)
}

func (s *Ethereum) handleRemovals(parlia *parlia.Parlia, nonce uint64, registeredSet map[enode.ID]struct{}) error {
	if len(s.config.EVNNodeIDsToRemove) == 0 {
		return nil
	}

	// Handle wildcard removal
	if len(s.config.EVNNodeIDsToRemove) == 1 {
		var zeroID enode.ID // This will be all zeros
		if s.config.EVNNodeIDsToRemove[0] == zeroID {
			trx, err := parlia.RemoveNodeIDs([]enode.ID{}, nonce)
			if err != nil {
				return fmt.Errorf("failed to create node ID removal transaction: %v", err)
			}
			if err := s.txPool.Add([]*types.Transaction{trx}, false); err != nil {
				return fmt.Errorf("failed to add node ID removal transaction to pool: %v", err)
			}
			log.Info("Submitted node ID removal transaction for all node IDs")
			return nil
		}
	}

	// Create a set of node IDs to add for quick lookup
	addSet := make(map[enode.ID]struct{}, len(s.config.EVNNodeIDsToAdd))
	for _, id := range s.config.EVNNodeIDsToAdd {
		addSet[id] = struct{}{}
	}

	// Filter out node IDs that are in the add set
	nodeIDsToRemove := make([]enode.ID, 0, len(s.config.EVNNodeIDsToRemove))
	for _, id := range s.config.EVNNodeIDsToRemove {
		if _, exists := registeredSet[id]; exists {
			if _, exists := addSet[id]; !exists {
				nodeIDsToRemove = append(nodeIDsToRemove, id)
			} else {
				log.Debug("Skipping node ID removal", "id", id, "reason", "also in EVNNodeIDsToAdd")
			}
		} else {
			log.Debug("Skipping node ID removal", "id", id, "reason", "not registered")
		}
	}

	if len(nodeIDsToRemove) == 0 {
		log.Debug("No node IDs to remove after filtering")
		return nil
	}

	trx, err := parlia.RemoveNodeIDs(nodeIDsToRemove, nonce)
	if err != nil {
		return fmt.Errorf("failed to create node ID removal transaction: %v", err)
	}
	if errs := s.txPool.Add([]*types.Transaction{trx}, false); len(errs) > 0 && errs[0] != nil {
		return fmt.Errorf("failed to add node ID removal transaction to pool: %v", errs)
	}
	log.Info("Submitted node ID removal transaction", "nodeIDs", nodeIDsToRemove)
	return nil
}

func (s *Ethereum) handleAdditions(parlia *parlia.Parlia, nonce uint64, registeredSet map[enode.ID]struct{}) error {
	if len(s.config.EVNNodeIDsToAdd) == 0 {
		return nil
	}

	// Filter out already registered IDs in a single pass
	nodeIDsToAdd := make([]enode.ID, 0, len(s.config.EVNNodeIDsToAdd))
	for _, id := range s.config.EVNNodeIDsToAdd {
		if _, exists := registeredSet[id]; !exists {
			nodeIDsToAdd = append(nodeIDsToAdd, id)
		}
	}

	if len(nodeIDsToAdd) == 0 {
		log.Info("No new node IDs to register after deduplication")
		return nil
	}

	trx, err := parlia.AddNodeIDs(nodeIDsToAdd, nonce)
	if err != nil {
		return fmt.Errorf("failed to create node ID registration transaction: %v", err)
	}
	if errs := s.txPool.Add([]*types.Transaction{trx}, false); len(errs) > 0 && errs[0] != nil {
		return fmt.Errorf("failed to add node ID registration transaction to pool: %v", errs)
	}
	log.Info("Submitted node ID registration transaction", "nodeIDs", nodeIDsToAdd)
	return nil
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

			// Start a goroutine to handle node ID registration after sync
			go func() {
				s.waitForSyncAndMaxwell(parlia)
			}()
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
func (s *Ethereum) BlobTxPool() *blobpool.BlobPool     { return s.blobTxPool }
func (s *Ethereum) VotePool() *vote.VotePool           { return s.votePool }
func (s *Ethereum) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Ethereum) Engine() consensus.Engine           { return s.engine }
func (s *Ethereum) ChainDb() ethdb.Database            { return s.chainDb }
func (s *Ethereum) IsListening() bool                  { return true } // Always listening
func (s *Ethereum) Downloader() *downloader.Downloader { return s.handler.downloader }
func (s *Ethereum) Synced() bool                       { return s.handler.synced.Load() }
func (s *Ethereum) SetSynced()                         { s.handler.enableSyncedFeatures() }
func (s *Ethereum) ArchiveMode() bool                  { return s.config.NoPruning }
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
	protos = append(protos, bsc.MakeProtocols((*bscHandler)(s.handler))...)

	return protos
}

// Start implements node.Lifecycle, starting all internal goroutines needed by the
// Ethereum protocol implementation.
func (s *Ethereum) Start() error {
	eth.StartENRFilter(s.blockchain, s.p2pServer)

	if err := s.setupDiscovery(); err != nil {
		return err
	}

	// Regularly update shutdown marker
	s.shutdownTracker.Start()

	// Start the networking layer
	s.handler.Start(s.p2pServer.MaxPeers, s.p2pServer.MaxPeersPerIP)

	go s.reportRecentBlocksLoop()

	// Start the connection manager
	s.dropper.Start(s.p2pServer, func() bool { return !s.Synced() })

	// start log indexer
	s.filterMaps.Start()
	go s.updateFilterMapsHeads()
	return nil
}

func (s *Ethereum) newChainView(head *types.Header) *filtermaps.ChainView {
	if head == nil {
		return nil
	}
	return filtermaps.NewChainView(s.blockchain, head.Number.Uint64(), head.Hash())
}

func (s *Ethereum) updateFilterMapsHeads() {
	headEventCh := make(chan core.ChainEvent, 10)
	blockProcCh := make(chan bool, 10)
	sub := s.blockchain.SubscribeChainEvent(headEventCh)
	sub2 := s.blockchain.SubscribeBlockProcessingEvent(blockProcCh)
	defer func() {
		sub.Unsubscribe()
		sub2.Unsubscribe()
		for {
			select {
			case <-headEventCh:
			case <-blockProcCh:
			default:
				return
			}
		}
	}()

	var head *types.Header
	setHead := func(newHead *types.Header) {
		if newHead == nil {
			return
		}
		if head == nil || newHead.Hash() != head.Hash() {
			head = newHead
			chainView := s.newChainView(head)
			historyCutoff, _ := s.blockchain.HistoryPruningCutoff()
			var finalBlock, currentBlock int64
			if fb := s.blockchain.CurrentFinalBlock(); fb != nil {
				finalBlock = fb.Number.Int64()
			}
			if cb := s.blockchain.CurrentBlock(); cb != nil {
				currentBlock = cb.Number.Int64()
			}

			// TODO(Nathan): use BlockChainAPI.getFinalizedNumber instead?
			finalBlock = max(finalBlock, currentBlock-16*21) // turnlength:16, validatorNum:21
			s.filterMaps.SetTarget(chainView, historyCutoff, uint64(finalBlock))
		}
	}
	setHead(s.blockchain.CurrentBlock())

	for {
		select {
		case ev := <-headEventCh:
			setHead(ev.Header)
		case blockProc := <-blockProcCh:
			s.filterMaps.SetBlockProcessing(blockProc)
		case <-time.After(time.Second * 10):
			setHead(s.blockchain.CurrentBlock())
		case ch := <-s.closeFilterMaps:
			close(ch)
			return
		}
	}
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

	// Add bsc nodes from DNS.
	if len(s.config.BscDiscoveryURLs) > 0 {
		iter, err := dnsclient.NewIterator(s.config.BscDiscoveryURLs...)
		if err != nil {
			return err
		}
		s.discmix.AddSource(iter)
	}

	// Add DHT nodes from discv4.
	if s.p2pServer.DiscoveryV4() != nil {
		iter := s.p2pServer.DiscoveryV4().RandomNodes()
		resolverFunc := func(ctx context.Context, enr *enode.Node) *enode.Node {
			// RequestENR does not yet support context. It will simply time out.
			// If the ENR can't be resolved, RequestENR will return nil. We don't
			// care about the specific error here, so we ignore it.
			nn, _ := s.p2pServer.DiscoveryV4().RequestENR(enr)
			return nn
		}
		iter = enode.AsyncFilter(iter, resolverFunc, maxParallelENRRequests)
		iter = enode.Filter(iter, eth.NewNodeFilter(s.blockchain))
		iter = enode.NewBufferIter(iter, discoveryPrefetchBuffer)
		s.discmix.AddSource(iter)
	}

	// Add DHT nodes from discv5.
	if s.p2pServer.DiscoveryV5() != nil {
		filter := eth.NewNodeFilter(s.blockchain)
		iter := enode.Filter(s.p2pServer.DiscoveryV5().RandomNodes(), filter)
		iter = enode.NewBufferIter(iter, discoveryPrefetchBuffer)
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
	s.dropper.Stop()
	s.handler.Stop()

	// Then stop everything else.
	ch := make(chan struct{})
	s.closeFilterMaps <- ch
	<-ch
	s.filterMaps.Stop()
	s.txPool.Close()
	s.miner.Close()
	s.blockchain.Stop()
	s.engine.Close()

	// Clean shutdown marker as the last thing before closing db
	s.shutdownTracker.Stop()

	s.chainDb.Close()
	s.eventMux.Stop()

	// stop report loop
	close(s.stopCh)
	return nil
}

func (s *Ethereum) reportRecentBlocksLoop() {
	reportCnt := uint64(2)
	reportTicker := time.NewTicker(time.Second)
	for {
		select {
		case <-reportTicker.C:
			cur := s.blockchain.CurrentBlock()
			if cur == nil || cur.Number.Uint64() <= reportCnt {
				continue
			}
			num := cur.Number.Uint64()
			stats := s.blockchain.GetBlockStats(cur.Hash())
			sendBlockTime := stats.SendBlockTime.Load()
			startImportBlockTime := stats.StartImportBlockTime.Load()
			recvNewBlockTime := stats.RecvNewBlockTime.Load()
			recvNewBlockHashTime := stats.RecvNewBlockHashTime.Load()
			sendVoteTime := stats.SendVoteTime.Load()
			firstVoteTime := stats.FirstRecvVoteTime.Load()
			recvMajorityTime := stats.RecvMajorityVoteTime.Load()
			startMiningTime := stats.StartMiningTime.Load()
			importedBlockTime := stats.ImportedBlockTime.Load()

			records := make(map[string]interface{})
			records["BlockNum"] = num
			records["SendBlockTime"] = common.FormatMilliTime(sendBlockTime)
			records["StartImportBlockTime"] = common.FormatMilliTime(startImportBlockTime)
			records["RecvNewBlockTime"] = common.FormatMilliTime(recvNewBlockTime)
			records["RecvNewBlockHashTime"] = common.FormatMilliTime(recvNewBlockHashTime)
			records["RecvNewBlockFrom"] = stats.RecvNewBlockFrom.Load()
			records["RecvNewBlockHashFrom"] = stats.RecvNewBlockHashFrom.Load()

			records["SendVoteTime"] = common.FormatMilliTime(sendVoteTime)
			records["FirstRecvVoteTime"] = common.FormatMilliTime(firstVoteTime)
			records["RecvMajorityVoteTime"] = common.FormatMilliTime(recvMajorityTime)

			records["StartMiningTime"] = common.FormatMilliTime(startMiningTime)
			records["ImportedBlockTime"] = common.FormatMilliTime(importedBlockTime)

			records["Coinbase"] = cur.Coinbase.String()
			blockMsTime := int64(cur.MilliTimestamp())
			records["BlockTime"] = common.FormatMilliTime(blockMsTime)
			metrics.GetOrRegisterLabel("report-blocks", nil).Mark(records)

			if validTimeMetric(blockMsTime, sendBlockTime) {
				sendBlockTimer.Update(time.Duration(sendBlockTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, recvNewBlockTime) {
				recvBlockTimer.Update(time.Duration(recvNewBlockTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, startImportBlockTime) {
				startInsertBlockTimer.Update(time.Duration(startImportBlockTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, sendVoteTime) {
				sendVoteTimer.Update(time.Duration(sendVoteTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, firstVoteTime) {
				firstVoteTimer.Update(time.Duration(firstVoteTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, recvMajorityTime) {
				majorityVoteTimer.Update(time.Duration(recvMajorityTime - blockMsTime))
			}
			if validTimeMetric(blockMsTime, importedBlockTime) {
				importedBlockTimer.Update(time.Duration(importedBlockTime - blockMsTime))
			}
			if validTimeMetric(startMiningTime, blockMsTime) {
				startMiningTimer.Update(time.Duration(blockMsTime - startMiningTime))
			}
		case <-s.stopCh:
			return
		}
	}
}

func validTimeMetric(startMs, endMs int64) bool {
	if startMs >= endMs {
		return false
	}
	return endMs-startMs <= MaxBlockHandleDelayMs
}
