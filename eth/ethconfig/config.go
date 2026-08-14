// Copyright 2021 The go-ethereum Authors
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

// Package ethconfig contains the configuration of the ETH and LES protocols.
package ethconfig

import (
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/txpool/blobpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

// FullNodeGPO contains default gasprice oracle settings for full node.
var FullNodeGPO = gasprice.Config{
	Blocks:           20,
	Percentile:       60,
	MaxHeaderHistory: 1024,
	MaxBlockHistory:  1024,
	MaxPrice:         gasprice.DefaultMaxPrice,
	OracleThreshold:  1000,
	IgnorePrice:      gasprice.DefaultIgnorePrice,
}

// Defaults contains default settings for use on the BSC main net.
var Defaults = Config{
	HistoryMode:             history.KeepAll,
	SyncMode:                SnapSync,
	NetworkId:               0, // enable auto configuration of networkID == chainID
	TxLookupLimit:           2350000,
	TransactionHistory:      2350000,
	LogHistory:              576000,
	BlockHistory:            0,
	StateHistory:            pathdb.Defaults.StateHistory,
	TrienodeHistory:         pathdb.Defaults.TrienodeHistory,
	NodeFullValueCheckpoint: pathdb.Defaults.FullValueCheckpoint,
	DatabaseCache:           2048,
	TrieCleanCache:          614,
	TrieDirtyCache:          1024,
	SnapshotCache:           409,
	TrieTimeout:             10 * time.Minute,
	TriesInMemory:           128,
	TriesVerifyMode:         core.LocalVerify,
	FilterLogCacheSize:      32,
	Miner:                   minerconfig.DefaultConfig,
	TxPool:                  legacypool.DefaultConfig,
	BlobPool:                blobpool.DefaultConfig,
	RPCGasCap:               50000000,
	RPCEVMTimeout:           5 * time.Second,
	GPO:                     FullNodeGPO,
	RPCTxFeeCap:             1,
	TxSyncDefaultTimeout:    5 * time.Second,
	TxSyncMaxTimeout:        10 * time.Second,
	SlowBlockThreshold:      -1, // Disabled by default; set via --debug.logslowblock flag
	RangeLimit:              5000,
	BlobExtraReserve:        params.DefaultExtraReserveForBlobRequests, // Extra reserve threshold for blob, blob never expires when -1 is set, default 28800
}

//go:generate go run github.com/fjl/gencodec -type Config -formats toml -out gen_config.go

// Config contains configuration options for ETH and LES protocols.
type Config struct {
	// The genesis block, which is inserted if the database is empty.
	// If nil, the Ethereum main net block is used.
	Genesis *core.Genesis `toml:",omitempty"`

	// Network ID separates blockchains on the peer-to-peer networking level. When left
	// zero, the chain ID is used as network ID.
	NetworkId uint64
	SyncMode  SyncMode

	// DisablePeerTxBroadcast is an optional config and disabled by default, and usually you do not need it.
	// When this flag is enabled, you are requesting remote peers to stop broadcasting new transactions to you, and
	// it does not mean that your node will stop broadcasting transactions to remote peers.
	// If your node does care about new mempool transactions (e.g., running rpc services without the need of mempool
	// transactions) or is continuously under high pressure (e.g., mempool is always full), then you can consider
	// to turn it on.
	//
	// Deprecated: only effective on eth/68 connections. It will be removed
	// together with eth/68 support.
	DisablePeerTxBroadcast bool
	EVNNodeIDsToAdd        []enode.ID
	EVNNodeIDsToRemove     []enode.ID
	// HistoryMode configures chain history retention.
	HistoryMode history.HistoryMode

	// This can be set to list of enrtree:// URLs which will be queried for
	// nodes to connect to.
	EthDiscoveryURLs  []string
	SnapDiscoveryURLs []string
	BscDiscoveryURLs  []string

	// State options.
	NoPruning  bool // Whether to disable pruning and flush everything to disk
	NoPrefetch bool // Whether to disable prefetching and only load state on demand

	DirectBroadcast     bool
	DisableSnapProtocol bool // Whether disable snap protocol

	// Deprecated: use 'TransactionHistory' instead.
	TxLookupLimit uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.

	TransactionHistory uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.
	BlockHistory       uint64 `toml:",omitempty"` // The maximum number of blocks from head whose block body/header/receipt/diff/hash are reserved.
	LogHistory         uint64 `toml:",omitempty"` // The maximum number of blocks from head where a log search index is maintained.
	LogNoHistory       bool   `toml:",omitempty"` // No log search index is maintained.
	// Deprecated: checkpoint file is auto-enabled at datadir/geth/filtermap_checkpoints.json.
	LogExportCheckpoints string
	StateHistory         uint64 `toml:",omitempty"` // The maximum number of blocks from head whose state histories are reserved.
	TrienodeHistory      int64  `toml:",omitempty"` // Number of blocks from the chain head for which trienode histories are retained

	// The frequency of full-value encoding. For example, a value of 16 means
	// that, on average, for a given trie node across its 16 consecutive historical
	// versions, only one version is stored in full format, while the others
	// are stored in diff mode for storage compression.
	NodeFullValueCheckpoint uint32 `toml:",omitempty"`

	// State scheme represents the scheme used to store ethereum states and trie
	// nodes on top. It can be 'hash', 'path', or none which means use the scheme
	// consistent with persistent state.
	StateScheme   string `toml:",omitempty"` // State scheme used to store ethereum state and merkle trie nodes on top
	PathSyncFlush bool   `toml:",omitempty"` // State scheme used to store ethereum state and merkle trie nodes on top

	DisableTxIndexer bool `toml:",omitempty"` // Whether to enable the transaction indexer

	// BinTrieGroupDepth is the number of levels per serialized group in binary trie.
	// Valid values are 1-8, with 8 being the default (byte-aligned groups).
	// Lower values create smaller groups with more nodes.
	BinTrieGroupDepth int `toml:",omitempty"`

	// RequiredBlocks is a set of block number -> hash mappings which must be in the
	// canonical chain of all remote peers. Setting the option makes geth verify the
	// presence of these blocks for every new peer connection.
	RequiredBlocks map[uint64]common.Hash `toml:"-"`

	// SlowBlockThreshold is the block execution time threshold beyond which
	// detailed statistics are logged. Negative means disabled (default), zero
	// logs all blocks, positive filters by execution time.
	SlowBlockThreshold time.Duration `toml:",omitempty"`

	// Database options
	SkipBcVersionCheck bool `toml:"-"`
	DatabaseHandles    int  `toml:"-"`
	DatabaseCache      int
	DatabaseFreezer    string
	DatabaseEra        string

	// PruneAncientData is an optional config and disabled by default, and usually you do not need it.
	// When this flag is enabled, only keep the latest 9w blocks' data, the older blocks' data will be
	// pruned instead of being dumped to freezerdb, the pruned data includes CanonicalHash, Header, Block,
	// Receipt and TotalDifficulty.
	// Notice: the PruneAncientData once be turned on, the get/chaindata/ancient dir will be removed,
	// if restart without the pruneancient flag, the ancient data will start with the previous point that
	// the oldest unpruned block number.
	// !!Deprecated: use 'BlockHistory' instead.
	PruneAncientData bool
	TrieCleanCache   int
	TrieDirtyCache   int
	TrieTimeout      time.Duration
	SnapshotCache    int
	TriesInMemory    uint64
	TriesVerifyMode  core.VerifyMode
	Preimages        bool

	// This is the number of blocks for which logs will be cached in the filter system.
	FilterLogCacheSize int

	// This is the maximum number of addresses or topics allowed in filter criteria
	// for eth_getLogs.
	LogQueryLimit int

	// Mining options
	Miner minerconfig.Config

	// Transaction pool options
	TxPool   legacypool.Config
	BlobPool blobpool.Config

	// Gas Price Oracle options
	GPO gasprice.Config

	// Enables tracking of SHA3 preimages in the VM
	EnablePreimageRecording bool

	// Enables collection of witness trie access statistics
	EnableWitnessStats bool

	// Generate execution witnesses and self-check against them (testing purpose)
	StatelessSelfValidation bool

	// Enables tracking of state size
	EnableStateSizeTracking bool

	// Enables VM tracing
	VMTrace           string
	VMTraceJsonConfig string

	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration

	// RPCTxFeeCap is the global transaction fee (price * gas limit) cap for
	// send-transaction variants. The unit is ether.
	RPCTxFeeCap float64

	// OverridePassedForkTime
	OverridePassedForkTime *uint64 `toml:",omitempty"`

	// OverrideLorentz (TODO: remove after the fork)
	OverrideLorentz *uint64 `toml:",omitempty"`

	// OverrideMaxwell (TODO: remove after the fork)
	OverrideMaxwell *uint64 `toml:",omitempty"`

	// OverrideFermi (TODO: remove after the fork)
	OverrideFermi *uint64 `toml:",omitempty"`

	// OverrideOsaka (TODO: remove after the fork)
	OverrideOsaka *uint64 `toml:",omitempty"`

	// OverrideMendel (TODO: remove after the fork)
	OverrideMendel *uint64 `toml:",omitempty"`

	// OverridePasteur (TODO: remove after the fork)
	OverridePasteur *uint64 `toml:",omitempty"`

	// OverrideJenner (TODO: remove after the fork)
	OverrideJenner *uint64 `toml:",omitempty"`

	// OverrideBPO1 (TODO: remove after the fork)
	OverrideBPO1 *uint64 `toml:",omitempty"`

	// OverrideBPO2 (TODO: remove after the fork)
	OverrideBPO2 *uint64 `toml:",omitempty"`

	// OverrideUBT (TODO: remove after the fork)
	OverrideUBT *uint64 `toml:",omitempty"`

	// EIP-7966: eth_sendRawTransactionSync timeouts
	TxSyncDefaultTimeout time.Duration `toml:",omitempty"`
	TxSyncMaxTimeout     time.Duration `toml:",omitempty"`

	// RangeLimit restricts the maximum range (end - start) for range queries.
	RangeLimit uint64 `toml:",omitempty"`

	// blob setting
	BlobExtraReserve uint64
}

// CreateConsensusEngine creates a consensus engine for the given chain config.
// Clique is allowed for now to live standalone, but ethash is forbidden and can
// only exist on already merged networks.
func CreateConsensusEngine(config *params.ChainConfig, db ethdb.Database, ee *ethapi.BlockChainAPI, genesisHash common.Hash) (consensus.Engine, error) {
	if config.IsInBSC() {
		return parlia.New(config, db, ee, genesisHash), nil
	}
	if config.TerminalTotalDifficulty == nil {
		log.Error("Geth only supports PoS networks. Please transition legacy networks using Geth v1.13.x.")
		return nil, errors.New("'terminalTotalDifficulty' is not set in genesis block")
	}
	// If proof-of-authority is requested, set it up
	if config.Clique != nil {
		return clique.New(config.Clique, db), nil
	}
	return beacon.New(ethash.NewFaker()), nil
}

func ApplyDefaultEthConfig(cfg *Config) {
	if cfg == nil {
		log.Warn("ApplyDefaultEthConfig cfg == nil")
		return
	}

	minerconfig.ApplyDefaultMinerConfig(&cfg.Miner)
}
