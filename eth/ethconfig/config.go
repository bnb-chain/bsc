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
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool/blobpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
)

// FullNodeGPO contains default gasprice oracle settings for full node.
var FullNodeGPO = gasprice.Config{
	Blocks:          20,
	Percentile:      60,
	MaxPrice:        gasprice.DefaultMaxPrice,
	OracleThreshold: 1000,
	IgnorePrice:     gasprice.DefaultIgnorePrice,
}

// LightClientGPO contains default gasprice oracle settings for light client.
var LightClientGPO = gasprice.Config{
	Blocks:      2,
	Percentile:  60,
	MaxPrice:    gasprice.DefaultMaxPrice,
	IgnorePrice: gasprice.DefaultIgnorePrice,
}

// Defaults contains default settings for use on the BSC main net.
var Defaults = Config{
	SyncMode:           downloader.SnapSync,
	NetworkId:          56,
	TxLookupLimit:      2350000,
	TransactionHistory: 2350000,
	StateHistory:       params.FullImmutabilityThreshold,
	StateScheme:        rawdb.HashScheme,
	LightPeers:         100,
	DatabaseCache:      512,
	TrieCleanCache:     154,
	TrieDirtyCache:     256,
	TrieTimeout:        60 * time.Minute,
	TriesInMemory:      128,
	TriesVerifyMode:    core.LocalVerify,
	SnapshotCache:      102,
	DiffBlock:          uint64(86400),
	FilterLogCacheSize: 32,
	Miner:              miner.DefaultConfig,
	TxPool:             legacypool.DefaultConfig,
	BlobPool:           blobpool.DefaultConfig,
	RPCGasCap:          50000000,
	RPCEVMTimeout:      5 * time.Second,
	GPO:                FullNodeGPO,
	RPCTxFeeCap:        1, // 1 ether
}

//go:generate go run github.com/fjl/gencodec -type Config -formats toml -out gen_config.go

// Config contains configuration options for of the ETH and LES protocols.
type Config struct {
	// The genesis block, which is inserted if the database is empty.
	// If nil, the Ethereum main net block is used.
	Genesis *core.Genesis `toml:",omitempty"`

	// Protocol options
	NetworkId uint64 // Network ID to use for selecting peers to connect to
	SyncMode  downloader.SyncMode

	// DisablePeerTxBroadcast is an optional config and disabled by default, and usually you do not need it.
	// When this flag is enabled, you are requesting remote peers to stop broadcasting new transactions to you, and
	// it does not mean that your node will stop broadcasting transactions to remote peers.
	// If your node does care about new mempool transactions (e.g., running rpc services without the need of mempool
	// transactions) or is continuously under high pressure (e.g., mempool is always full), then you can consider
	// to turn it on.
	DisablePeerTxBroadcast bool

	// This can be set to list of enrtree:// URLs which will be queried for
	// for nodes to connect to.
	EthDiscoveryURLs   []string
	SnapDiscoveryURLs  []string
	TrustDiscoveryURLs []string
	BscDiscoveryURLs   []string

	NoPruning           bool // Whether to disable pruning and flush everything to disk
	NoPrefetch          bool
	DirectBroadcast     bool
	DisableSnapProtocol bool //Whether disable snap protocol
	EnableTrustProtocol bool //Whether enable trust protocol
	PipeCommit          bool
	RangeLimit          bool

	// Deprecated, use 'TransactionHistory' instead.
	TxLookupLimit      uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.
	TransactionHistory uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.
	StateHistory       uint64 `toml:",omitempty"` // The maximum number of blocks from head whose state histories are reserved.
	StateScheme        string `toml:",omitempty"` // State scheme used to store ethereum state and merkle trie nodes on top
	PathSyncFlush      bool   `toml:",omitempty"` // State scheme used to store ethereum state and merkle trie nodes on top

	// RequiredBlocks is a set of block number -> hash mappings which must be in the
	// canonical chain of all remote peers. Setting the option makes geth verify the
	// presence of these blocks for every new peer connection.
	RequiredBlocks map[uint64]common.Hash `toml:"-"`

	// Light client options
	LightServ        int  `toml:",omitempty"` // Maximum percentage of time allowed for serving LES requests
	LightIngress     int  `toml:",omitempty"` // Incoming bandwidth limit for light servers
	LightEgress      int  `toml:",omitempty"` // Outgoing bandwidth limit for light servers
	LightPeers       int  `toml:",omitempty"` // Maximum number of LES client peers
	LightNoPrune     bool `toml:",omitempty"` // Whether to disable light chain pruning
	LightNoSyncServe bool `toml:",omitempty"` // Whether to serve light clients before syncing

	// Database options
	SkipBcVersionCheck bool `toml:"-"`
	DatabaseHandles    int  `toml:"-"`
	DatabaseCache      int
	DatabaseFreezer    string
	DatabaseDiff       string
	PersistDiff        bool
	DiffBlock          uint64
	// PruneAncientData is an optional config and disabled by default, and usually you do not need it.
	// When this flag is enabled, only keep the latest 9w blocks' data, the older blocks' data will be
	// pruned instead of being dumped to freezerdb, the pruned data includes CanonicalHash, Header, Block,
	// Receipt and TotalDifficulty.
	// Notice: the PruneAncientData once be turned on, the get/chaindata/ancient dir will be removed,
	// if restart without the pruneancient flag, the ancient data will start with the previous point that
	// the oldest unpruned block number.
	PruneAncientData bool

	TrieCleanCache  int
	TrieDirtyCache  int
	TrieTimeout     time.Duration
	SnapshotCache   int
	TriesInMemory   uint64
	TriesVerifyMode core.VerifyMode
	Preimages       bool

	// This is the number of blocks for which logs will be cached in the filter system.
	FilterLogCacheSize int

	// Mining options
	Miner miner.Config

	// Transaction pool options
	TxPool   legacypool.Config
	BlobPool blobpool.Config

	// Gas Price Oracle options
	GPO gasprice.Config

	// Enables tracking of SHA3 preimages in the VM
	EnablePreimageRecording bool

	// Miscellaneous options
	DocRoot string `toml:"-"`

	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transaction variants. The unit is ether.
	RPCTxFeeCap float64

	// OverrideCancun (TODO: remove after the fork)
	OverrideCancun *uint64 `toml:",omitempty"`

	// OverrideVerkle (TODO: remove after the fork)
	OverrideVerkle *uint64 `toml:",omitempty"`
}

// CreateConsensusEngine creates a consensus engine for the given chain config.
// Clique is allowed for now to live standalone, but ethash is forbidden and can
// only exist on already merged networks.
func CreateConsensusEngine(config *params.ChainConfig, db ethdb.Database, ee *ethapi.BlockChainAPI, genesisHash common.Hash) (consensus.Engine, error) {
	if config.Parlia != nil {
		return parlia.New(config, db, ee, genesisHash), nil
	}
	// If proof-of-authority is requested, set it up
	if config.Clique != nil {
		return clique.New(config.Clique, db), nil
	}
	// If defaulting to proof-of-work, enforce an already merged network since
	// we cannot run PoW algorithms and more, so we cannot even follow a chain
	// not coordinated by a beacon node.
	if !config.TerminalTotalDifficultyPassed {
		return nil, errors.New("ethash is only supported as a historical component of already merged networks")
	}
	return beacon.New(ethash.NewFaker()), nil
}
