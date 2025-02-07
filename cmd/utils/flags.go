// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// Package utils contains internal helper functions for go-ethereum commands.
package utils

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	godebug "runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/beacon/fakebeacon"
	bparams "github.com/ethereum/go-ethereum/beacon/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool/blobpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/remotedb"
	"github.com/ethereum/go-ethereum/ethstats"
	"github.com/ethereum/go-ethereum/graphql"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/internal/version"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/metrics/exp"
	"github.com/ethereum/go-ethereum/metrics/influxdb"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/p2p/netutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/fatih/structs"
	pcsclite "github.com/gballet/go-libpcsclite"
	gopsutil "github.com/shirou/gopsutil/mem"
	"github.com/urfave/cli/v2"
)

// These are all the command line flags we support.
// If you add to this list, please remember to include the
// flag in the appropriate command definition.
//
// The flags are defined here so their names and help texts
// are the same for all commands.

var (
	// General settings
	DataDirFlag = &flags.DirectoryFlag{
		Name:     "datadir",
		Usage:    "Data directory for the databases and keystore",
		Value:    flags.DirectoryString(node.DefaultDataDir()),
		Category: flags.EthCategory,
	}
	MultiDataBaseFlag = &cli.BoolFlag{
		Name: "multidatabase",
		Usage: "Enable a separated state and block database, it will be created within two subdirectory called state and block, " +
			"Users can copy this state or block directory to another directory or disk, and then create a symbolic link to the state directory under the chaindata",
		Category: flags.EthCategory,
	}
	DirectBroadcastFlag = &cli.BoolFlag{
		Name:     "directbroadcast",
		Usage:    "Enable directly broadcast mined block to all peers",
		Category: flags.EthCategory,
	}
	DisableSnapProtocolFlag = &cli.BoolFlag{
		Name:     "disablesnapprotocol",
		Usage:    "Disable snap protocol",
		Category: flags.EthCategory,
	}
	EnableTrustProtocolFlag = &cli.BoolFlag{
		Name:     "enabletrustprotocol",
		Usage:    "Enable trust protocol",
		Category: flags.FastNodeCategory,
	}
	RangeLimitFlag = &cli.BoolFlag{
		Name:     "rangelimit",
		Usage:    "Enable 5000 blocks limit for range query",
		Category: flags.APICategory,
	}
	DiffFlag = flags.DirectoryFlag{
		Name:     "datadir.diff",
		Usage:    "Data directory for difflayer segments (default = inside chaindata)",
		Category: flags.FastNodeCategory,
	}
	RemoteDBFlag = &cli.StringFlag{
		Name:     "remotedb",
		Usage:    "URL for remote database",
		Category: flags.LoggingCategory,
	}
	DBEngineFlag = &cli.StringFlag{
		Name:     "db.engine",
		Usage:    "Backing database implementation to use ('pebble' or 'leveldb')",
		Value:    node.DefaultConfig.DBEngine,
		Category: flags.EthCategory,
	}
	AncientFlag = &flags.DirectoryFlag{
		Name:     "datadir.ancient",
		Usage:    "Root directory for ancient data (default = inside chaindata)",
		Category: flags.EthCategory,
	}
	MinFreeDiskSpaceFlag = &flags.DirectoryFlag{
		Name:     "datadir.minfreedisk",
		Usage:    "Minimum free disk space in MB, once reached triggers auto shut down (default = --cache.gc converted to MB, 0 = disabled)",
		Category: flags.EthCategory,
	}
	InstanceFlag = &cli.IntFlag{
		Name:     "instance",
		Usage:    "Configures the ports to avoid conflicts when running multiple nodes on the same machine. Maximum is 200. Only applicable for: port, authrpc.port, discovery,port, http.port, ws.port",
		Value:    1,
		Category: flags.EthCategory,
	}
	KeyStoreDirFlag = &flags.DirectoryFlag{
		Name:     "keystore",
		Usage:    "Directory for the keystore (default = inside the datadir)",
		Category: flags.AccountCategory,
	}
	USBFlag = &cli.BoolFlag{
		Name:     "usb",
		Usage:    "Enable monitoring and management of USB hardware wallets",
		Category: flags.AccountCategory,
	}
	SmartCardDaemonPathFlag = &cli.StringFlag{
		Name:     "pcscdpath",
		Usage:    "Path to the smartcard daemon (pcscd) socket file",
		Value:    pcsclite.PCSCDSockName,
		Category: flags.AccountCategory,
	}
	NetworkIdFlag = &cli.Uint64Flag{
		Name:     "networkid",
		Usage:    "Explicitly set network id (integer)(For testnets: use --chapel instead)",
		Value:    ethconfig.Defaults.NetworkId,
		Category: flags.EthCategory,
	}
	BSCMainnetFlag = &cli.BoolFlag{
		Name:     "mainnet",
		Usage:    "BSC mainnet",
		Category: flags.EthCategory,
	}
	ChapelFlag = &cli.BoolFlag{
		Name:     "chapel",
		Usage:    "Chapel network: pre-configured Proof-of-Stake-Authority BSC test network",
		Category: flags.EthCategory,
	}
	DeveloperFlag = &cli.BoolFlag{
		Name:     "dev",
		Usage:    "Ephemeral proof-of-authority network with a pre-funded developer account, mining enabled",
		Category: flags.DevCategory,
	}
	DeveloperPeriodFlag = &cli.Uint64Flag{
		Name:     "dev.period",
		Usage:    "Block period to use in developer mode (0 = mine only if transaction pending)",
		Category: flags.DevCategory,
	}
	DeveloperGasLimitFlag = &cli.Uint64Flag{
		Name:     "dev.gaslimit",
		Usage:    "Initial block gas limit",
		Value:    11500000,
		Category: flags.DevCategory,
	}

	IdentityFlag = &cli.StringFlag{
		Name:     "identity",
		Usage:    "Custom node name",
		Category: flags.NetworkingCategory,
	}
	ExitWhenSyncedFlag = &cli.BoolFlag{
		Name:     "exitwhensynced",
		Usage:    "Exits after block synchronisation completes",
		Category: flags.EthCategory,
	}
	// hbss2pbss command options
	ForceFlag = &cli.BoolFlag{
		Name:  "force",
		Usage: "Force convert hbss trie node to pbss trie node. Ignore any metadata",
		Value: false,
	}
	// Dump command options.
	IterativeOutputFlag = &cli.BoolFlag{
		Name:  "iterative",
		Usage: "Print streaming JSON iteratively, delimited by newlines",
		Value: true,
	}
	ExcludeStorageFlag = &cli.BoolFlag{
		Name:  "nostorage",
		Usage: "Exclude storage entries (save db lookups)",
	}
	IncludeIncompletesFlag = &cli.BoolFlag{
		Name:  "incompletes",
		Usage: "Include accounts for which we don't have the address (missing preimage)",
	}
	ExcludeCodeFlag = &cli.BoolFlag{
		Name:  "nocode",
		Usage: "Exclude contract code (save db lookups)",
	}
	StartKeyFlag = &cli.StringFlag{
		Name:  "start",
		Usage: "Start position. Either a hash or address",
		Value: "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	DumpLimitFlag = &cli.Uint64Flag{
		Name:  "limit",
		Usage: "Max number of elements (0 = no limit)",
		Value: 0,
	}

	SnapshotFlag = &cli.BoolFlag{
		Name:     "snapshot",
		Usage:    `Enables snapshot-database mode (default = enable)`,
		Value:    true,
		Category: flags.EthCategory,
	}
	LightKDFFlag = &cli.BoolFlag{
		Name:     "lightkdf",
		Usage:    "Reduce key-derivation RAM & CPU usage at some expense of KDF strength",
		Category: flags.AccountCategory,
	}
	EthRequiredBlocksFlag = &cli.StringFlag{
		Name:     "eth.requiredblocks",
		Usage:    "Comma separated block number-to-hash mappings to require for peering (<number>=<hash>)",
		Category: flags.EthCategory,
	}
	BloomFilterSizeFlag = &cli.Uint64Flag{
		Name:     "bloomfilter.size",
		Usage:    "Megabytes of memory allocated to bloom-filter for pruning",
		Value:    2048,
		Category: flags.EthCategory,
	}
	TriesInMemoryFlag = &cli.Uint64Flag{
		Name:     "triesInMemory",
		Usage:    "The layer of tries trees that keep in memory",
		Value:    128,
		Category: flags.PerfCategory,
	}
	TriesVerifyModeFlag = &cli.StringFlag{
		Name: "tries-verify-mode",
		Usage: `tries verify mode:
				"local(default): a normal full node with complete state world(both MPT and snapshot), merkle state root will
				                 be verified against the block header.",
				"full: a fast node with only snapshot state world. Merkle state root is verified by the trustworthy remote verify node
					   by comparing the diffhash(an identify of difflayer generated by the block) and state root.",
				"insecure: same as full mode, except that it can tolerate without verifying the diffhash when verify node does not have it.",
				"none: no merkle state root verification at all, there is no need to setup or connect remote verify node at all,
				       it is more light comparing to full and insecure mode, but get a very small chance that the state is not consistent
						with other peers."`,
		Value:    ethconfig.Defaults.TriesVerifyMode.String(),
		Category: flags.FastNodeCategory,
	}
	RialtoHash = &cli.StringFlag{
		Name:     "rialtohash",
		Usage:    "Manually specify the Rialto Genesis Hash, to trigger builtin network logic",
		Category: flags.EthCategory,
	}
	OverridePassedForkTime = &cli.Uint64Flag{
		Name:     "override.passedforktime",
		Usage:    "Manually specify the hard fork timestamps which have passed on the mainnet, overriding the bundled setting",
		Category: flags.EthCategory,
	}
	OverridePascal = &cli.Uint64Flag{
		Name:     "override.pascal",
		Usage:    "Manually specify the Pascal fork timestamp, overriding the bundled setting",
		Category: flags.EthCategory,
	}
	OverridePrague = &cli.Uint64Flag{
		Name:     "override.prague",
		Usage:    "Manually specify the Prague fork timestamp, overriding the bundled setting",
		Category: flags.EthCategory,
	}
	OverrideVerkle = &cli.Uint64Flag{
		Name:     "override.verkle",
		Usage:    "Manually specify the Verkle fork timestamp, overriding the bundled setting",
		Category: flags.EthCategory,
	}
	OverrideFullImmutabilityThreshold = &cli.Uint64Flag{
		Name:     "override.immutabilitythreshold",
		Usage:    "It is the number of blocks after which a chain segment is considered immutable, only for testing purpose",
		Value:    params.FullImmutabilityThreshold,
		Category: flags.EthCategory,
	}
	OverrideMinBlocksForBlobRequests = &cli.Uint64Flag{
		Name:     "override.minforblobrequest",
		Usage:    "It keeps blob data available for min blocks in local, only for testing purpose",
		Value:    params.MinBlocksForBlobRequests,
		Category: flags.EthCategory,
	}
	OverrideDefaultExtraReserveForBlobRequests = &cli.Uint64Flag{
		Name:     "override.defaultextrareserve",
		Usage:    "It adds more extra time for expired blobs for some request cases, only for testing purpose",
		Value:    params.DefaultExtraReserveForBlobRequests,
		Category: flags.EthCategory,
	}
	OverrideBreatheBlockInterval = &cli.Uint64Flag{
		Name:     "override.breatheblockinterval",
		Usage:    "It changes the interval between breathe blocks, only for testing purpose",
		Value:    params.BreatheBlockInterval,
		Category: flags.EthCategory,
	}
	OverrideFixedTurnLength = &cli.Uint64Flag{
		Name:     "override.fixedturnlength",
		Usage:    "It use fixed or random values for turn length instead of reading from the contract, only for testing purpose",
		Value:    params.FixedTurnLength,
		Category: flags.EthCategory,
	}
	SyncModeFlag = &cli.StringFlag{
		Name:     "syncmode",
		Usage:    `Blockchain sync mode ("snap" or "full")`,
		Value:    ethconfig.Defaults.SyncMode.String(),
		Category: flags.StateCategory,
	}
	GCModeFlag = &cli.StringFlag{
		Name:     "gcmode",
		Usage:    `Blockchain garbage collection mode, only relevant in state.scheme=hash ("full", "archive")`,
		Value:    "full",
		Category: flags.StateCategory,
	}
	StateSchemeFlag = &cli.StringFlag{
		Name:     "state.scheme",
		Usage:    "Scheme to use for storing ethereum state ('hash' or 'path')",
		Category: flags.StateCategory,
	}
	PathDBSyncFlag = &cli.BoolFlag{
		Name:     "pathdb.sync",
		Usage:    "sync flush nodes cache to disk in path schema",
		Value:    false,
		Category: flags.StateCategory,
	}
	JournalFileFlag = &cli.BoolFlag{
		Name:     "journalfile",
		Usage:    "Enable using journal file to store the TrieJournal instead of KVDB in pbss (default = false)",
		Value:    false,
		Category: flags.StateCategory,
	}
	StateHistoryFlag = &cli.Uint64Flag{
		Name:     "history.state",
		Usage:    "Number of recent blocks to retain state history for (default = 90,000 blocks, 0 = entire chain)",
		Value:    ethconfig.Defaults.StateHistory,
		Category: flags.StateCategory,
	}
	TransactionHistoryFlag = &cli.Uint64Flag{
		Name:     "history.transactions",
		Usage:    "Number of recent blocks to maintain transactions index for (default = about one year, 0 = entire chain)",
		Value:    ethconfig.Defaults.TransactionHistory,
		Category: flags.StateCategory,
	}
	// Beacon client light sync settings
	BeaconApiFlag = &cli.StringSliceFlag{
		Name:     "beacon.api",
		Usage:    "Beacon node (CL) light client API URL. This flag can be given multiple times.",
		Category: flags.BeaconCategory,
	}
	BeaconApiHeaderFlag = &cli.StringSliceFlag{
		Name:     "beacon.api.header",
		Usage:    "Pass custom HTTP header fields to the remote beacon node API in \"key:value\" format. This flag can be given multiple times.",
		Category: flags.BeaconCategory,
	}
	BeaconThresholdFlag = &cli.IntFlag{
		Name:     "beacon.threshold",
		Usage:    "Beacon sync committee participation threshold",
		Value:    bparams.SyncCommitteeSupermajority,
		Category: flags.BeaconCategory,
	}
	BeaconNoFilterFlag = &cli.BoolFlag{
		Name:     "beacon.nofilter",
		Usage:    "Disable future slot signature filter",
		Category: flags.BeaconCategory,
	}
	BeaconConfigFlag = &cli.StringFlag{
		Name:     "beacon.config",
		Usage:    "Beacon chain config YAML file",
		Category: flags.BeaconCategory,
	}
	BeaconGenesisRootFlag = &cli.StringFlag{
		Name:     "beacon.genesis.gvroot",
		Usage:    "Beacon chain genesis validators root",
		Category: flags.BeaconCategory,
	}
	BeaconGenesisTimeFlag = &cli.Uint64Flag{
		Name:     "beacon.genesis.time",
		Usage:    "Beacon chain genesis time",
		Category: flags.BeaconCategory,
	}
	BeaconCheckpointFlag = &cli.StringFlag{
		Name:     "beacon.checkpoint",
		Usage:    "Beacon chain weak subjectivity checkpoint block hash",
		Category: flags.BeaconCategory,
	}
	BlsyncApiFlag = &cli.StringFlag{
		Name:     "blsync.engine.api",
		Usage:    "Target EL engine API URL",
		Category: flags.BeaconCategory,
	}
	BlsyncJWTSecretFlag = &flags.DirectoryFlag{
		Name:     "blsync.jwtsecret",
		Usage:    "Path to a JWT secret to use for target engine API endpoint",
		Category: flags.BeaconCategory,
	}
	// Transaction pool settings
	TxPoolLocalsFlag = &cli.StringFlag{
		Name:     "txpool.locals",
		Usage:    "Comma separated accounts to treat as locals (no flush, priority inclusion)",
		Category: flags.TxPoolCategory,
	}
	TxPoolNoLocalsFlag = &cli.BoolFlag{
		Name:     "txpool.nolocals",
		Usage:    "Disables price exemptions for locally submitted transactions",
		Category: flags.TxPoolCategory,
	}
	TxPoolJournalFlag = &cli.StringFlag{
		Name:     "txpool.journal",
		Usage:    "Disk journal for local transaction to survive node restarts",
		Value:    ethconfig.Defaults.TxPool.Journal,
		Category: flags.TxPoolCategory,
	}
	TxPoolRejournalFlag = &cli.DurationFlag{
		Name:     "txpool.rejournal",
		Usage:    "Time interval to regenerate the local transaction journal",
		Value:    ethconfig.Defaults.TxPool.Rejournal,
		Category: flags.TxPoolCategory,
	}
	TxPoolPriceLimitFlag = &cli.Uint64Flag{
		Name:     "txpool.pricelimit",
		Usage:    "Minimum gas price tip to enforce for acceptance into the pool",
		Value:    ethconfig.Defaults.TxPool.PriceLimit,
		Category: flags.TxPoolCategory,
	}
	TxPoolPriceBumpFlag = &cli.Uint64Flag{
		Name:     "txpool.pricebump",
		Usage:    "Price bump percentage to replace an already existing transaction",
		Value:    ethconfig.Defaults.TxPool.PriceBump,
		Category: flags.TxPoolCategory,
	}
	TxPoolAccountSlotsFlag = &cli.Uint64Flag{
		Name:     "txpool.accountslots",
		Usage:    "Minimum number of executable transaction slots guaranteed per account",
		Value:    ethconfig.Defaults.TxPool.AccountSlots,
		Category: flags.TxPoolCategory,
	}
	TxPoolGlobalSlotsFlag = &cli.Uint64Flag{
		Name:     "txpool.globalslots",
		Usage:    "Maximum number of executable transaction slots for all accounts",
		Value:    ethconfig.Defaults.TxPool.GlobalSlots,
		Category: flags.TxPoolCategory,
	}
	TxPoolAccountQueueFlag = &cli.Uint64Flag{
		Name:     "txpool.accountqueue",
		Usage:    "Maximum number of non-executable transaction slots permitted per account",
		Value:    ethconfig.Defaults.TxPool.AccountQueue,
		Category: flags.TxPoolCategory,
	}
	TxPoolGlobalQueueFlag = &cli.Uint64Flag{
		Name:     "txpool.globalqueue",
		Usage:    "Maximum number of non-executable transaction slots for all accounts",
		Value:    ethconfig.Defaults.TxPool.GlobalQueue,
		Category: flags.TxPoolCategory,
	}
	TxPoolOverflowPoolSlotsFlag = &cli.Uint64Flag{
		Name:     "txpool.overflowpoolslots",
		Usage:    "Maximum number of transaction slots in overflow pool",
		Value:    ethconfig.Defaults.TxPool.OverflowPoolSlots,
		Category: flags.TxPoolCategory,
	}
	TxPoolLifetimeFlag = &cli.DurationFlag{
		Name:     "txpool.lifetime",
		Usage:    "Maximum amount of time non-executable transaction are queued",
		Value:    ethconfig.Defaults.TxPool.Lifetime,
		Category: flags.TxPoolCategory,
	}
	TxPoolReannounceTimeFlag = &cli.DurationFlag{
		Name:     "txpool.reannouncetime",
		Usage:    "Duration for announcing local pending transactions again (default = 10 years, minimum = 1 minute)",
		Value:    ethconfig.Defaults.TxPool.ReannounceTime,
		Category: flags.TxPoolCategory,
	}
	// Blob transaction pool settings
	BlobPoolDataDirFlag = &cli.StringFlag{
		Name:     "blobpool.datadir",
		Usage:    "Data directory to store blob transactions in",
		Value:    ethconfig.Defaults.BlobPool.Datadir,
		Category: flags.BlobPoolCategory,
	}
	BlobPoolDataCapFlag = &cli.Uint64Flag{
		Name:     "blobpool.datacap",
		Usage:    "Disk space to allocate for pending blob transactions (soft limit)",
		Value:    ethconfig.Defaults.BlobPool.Datacap,
		Category: flags.BlobPoolCategory,
	}
	BlobPoolPriceBumpFlag = &cli.Uint64Flag{
		Name:     "blobpool.pricebump",
		Usage:    "Price bump percentage to replace an already existing blob transaction",
		Value:    ethconfig.Defaults.BlobPool.PriceBump,
		Category: flags.BlobPoolCategory,
	}
	// Performance tuning settings
	CacheFlag = &cli.IntFlag{
		Name:     "cache",
		Usage:    "Megabytes of memory allocated to internal caching (default = 4096 mainnet full node, 128 light mode)",
		Value:    1024,
		Category: flags.PerfCategory,
	}
	CacheDatabaseFlag = &cli.IntFlag{
		Name:     "cache.database",
		Usage:    "Percentage of cache memory allowance to use for database io",
		Value:    40,
		Category: flags.PerfCategory,
	}
	CacheTrieFlag = &cli.IntFlag{
		Name:     "cache.trie",
		Usage:    "Percentage of cache memory allowance to use for trie caching (default = 15% full mode, 30% archive mode)",
		Value:    15,
		Category: flags.PerfCategory,
	}
	CacheGCFlag = &cli.IntFlag{
		Name:     "cache.gc",
		Usage:    "Percentage of cache memory allowance to use for trie pruning (default = 25% full mode, 0% archive mode)",
		Value:    25,
		Category: flags.PerfCategory,
	}
	CacheSnapshotFlag = &cli.IntFlag{
		Name:     "cache.snapshot",
		Usage:    "Percentage of cache memory allowance to use for snapshot caching (default = 20%)",
		Value:    20,
		Category: flags.PerfCategory,
	}
	CacheNoPrefetchFlag = &cli.BoolFlag{
		Name:     "cache.noprefetch",
		Usage:    "Disable heuristic state prefetch during block import (less CPU and disk IO, more time waiting for data)",
		Category: flags.PerfCategory,
	}
	CachePreimagesFlag = &cli.BoolFlag{
		Name:     "cache.preimages",
		Usage:    "Enable recording the SHA3/keccak preimages of trie keys",
		Category: flags.PerfCategory,
	}
	PersistDiffFlag = &cli.BoolFlag{
		Name:     "persistdiff",
		Usage:    "Enable persistence of the diff layer",
		Category: flags.FastNodeCategory,
	}
	DiffBlockFlag = &cli.Uint64Flag{
		Name:     "diffblock",
		Usage:    "The number of blocks should be persisted in db (default = 86400)",
		Value:    uint64(86400),
		Category: flags.FastNodeCategory,
	}
	PruneAncientDataFlag = &cli.BoolFlag{
		Name:     "pruneancient",
		Usage:    "Prune ancient data, is an optional config and disabled by default. Only keep the latest 9w blocks' data,the older blocks' data will be permanently pruned. Notice:the geth/chaindata/ancient dir will be removed, if restart without the flag, the ancient data will start with the previous point that the oldest unpruned block number. Recommends to the user who don't care about the ancient data.",
		Category: flags.BlockHistoryCategory,
	}
	CacheLogSizeFlag = &cli.IntFlag{
		Name:     "cache.blocklogs",
		Usage:    "Size (in number of blocks) of the log cache for filtering",
		Category: flags.PerfCategory,
		Value:    ethconfig.Defaults.FilterLogCacheSize,
	}
	FDLimitFlag = &cli.IntFlag{
		Name:     "fdlimit",
		Usage:    "Raise the open file descriptor resource limit (default = system fd limit)",
		Category: flags.PerfCategory,
	}
	CryptoKZGFlag = &cli.StringFlag{
		Name:     "crypto.kzg",
		Usage:    "KZG library implementation to use; gokzg (recommended) or ckzg",
		Value:    "gokzg",
		Category: flags.PerfCategory,
	}

	// Miner settings
	MiningEnabledFlag = &cli.BoolFlag{
		Name:     "mine",
		Usage:    "Enable mining",
		Category: flags.MinerCategory,
	}
	MinerGasLimitFlag = &cli.Uint64Flag{
		Name:     "miner.gaslimit",
		Usage:    "Target gas ceiling for mined blocks",
		Value:    ethconfig.Defaults.Miner.GasCeil,
		Category: flags.MinerCategory,
	}
	MinerGasPriceFlag = &flags.BigFlag{
		Name:     "miner.gasprice",
		Usage:    "Minimum gas price for mining a transaction",
		Value:    ethconfig.Defaults.Miner.GasPrice,
		Category: flags.MinerCategory,
	}
	MinerEtherbaseFlag = &cli.StringFlag{
		Name:     "miner.etherbase",
		Usage:    "0x prefixed public address for block mining rewards",
		Category: flags.MinerCategory,
	}
	MinerExtraDataFlag = &cli.StringFlag{
		Name:     "miner.extradata",
		Usage:    "Block extra data set by the miner (default = client version)",
		Category: flags.MinerCategory,
	}
	MinerRecommitIntervalFlag = &cli.DurationFlag{
		Name:     "miner.recommit",
		Usage:    "Time interval to recreate the block being mined",
		Value:    ethconfig.Defaults.Miner.Recommit,
		Category: flags.MinerCategory,
	}
	MinerDelayLeftoverFlag = &cli.DurationFlag{
		Name:     "miner.delayleftover",
		Usage:    "Time reserved to finalize a block",
		Value:    ethconfig.Defaults.Miner.DelayLeftOver,
		Category: flags.MinerCategory,
	}

	// Account settings
	UnlockedAccountFlag = &cli.StringFlag{
		Name:     "unlock",
		Usage:    "Comma separated list of accounts to unlock",
		Value:    "",
		Category: flags.AccountCategory,
	}
	PasswordFileFlag = &cli.PathFlag{
		Name:      "password",
		Usage:     "Password file to use for non-interactive password input",
		TakesFile: true,
		Category:  flags.AccountCategory,
	}
	ExternalSignerFlag = &cli.StringFlag{
		Name:     "signer",
		Usage:    "External signer (url or path to ipc file)",
		Value:    "",
		Category: flags.AccountCategory,
	}
	InsecureUnlockAllowedFlag = &cli.BoolFlag{
		Name:     "allow-insecure-unlock",
		Usage:    "Allow insecure account unlocking when account-related RPCs are exposed by http",
		Category: flags.AccountCategory,
	}

	// EVM settings
	VMEnableDebugFlag = &cli.BoolFlag{
		Name:     "vmdebug",
		Usage:    "Record information useful for VM and contract debugging",
		Category: flags.VMCategory,
	}
	VMTraceFlag = &cli.StringFlag{
		Name:     "vmtrace",
		Usage:    "Name of tracer which should record internal VM operations (costly)",
		Category: flags.VMCategory,
	}
	VMTraceJsonConfigFlag = &cli.StringFlag{
		Name:     "vmtrace.jsonconfig",
		Usage:    "Tracer configuration (JSON)",
		Value:    "{}",
		Category: flags.VMCategory,
	}
	// API options.
	RPCGlobalGasCapFlag = &cli.Uint64Flag{
		Name:     "rpc.gascap",
		Usage:    "Sets a cap on gas that can be used in eth_call/estimateGas (0=infinite)",
		Value:    ethconfig.Defaults.RPCGasCap,
		Category: flags.APICategory,
	}
	RPCGlobalEVMTimeoutFlag = &cli.DurationFlag{
		Name:     "rpc.evmtimeout",
		Usage:    "Sets a timeout used for eth_call (0=infinite)",
		Value:    ethconfig.Defaults.RPCEVMTimeout,
		Category: flags.APICategory,
	}
	RPCGlobalTxFeeCapFlag = &cli.Float64Flag{
		Name:     "rpc.txfeecap",
		Usage:    "Sets a cap on transaction fee (in ether) that can be sent via the RPC APIs (0 = no cap)",
		Value:    ethconfig.Defaults.RPCTxFeeCap,
		Category: flags.APICategory,
	}
	// Authenticated RPC HTTP settings
	AuthListenFlag = &cli.StringFlag{
		Name:  "authrpc.addr",
		Usage: "Listening address for authenticated APIs",
		Value: node.DefaultConfig.AuthAddr,
		// Category: flags.APICategory,
	}
	AuthPortFlag = &cli.IntFlag{
		Name:  "authrpc.port",
		Usage: "Listening port for authenticated APIs",
		Value: node.DefaultConfig.AuthPort,
		// Category: flags.APICategory,
	}
	AuthVirtualHostsFlag = &cli.StringFlag{
		Name:  "authrpc.vhosts",
		Usage: "Comma separated list of virtual hostnames from which to accept requests (server enforced). Accepts '*' wildcard.",
		Value: strings.Join(node.DefaultConfig.AuthVirtualHosts, ","),
		// Category: flags.APICategory,
	}
	JWTSecretFlag = &flags.DirectoryFlag{
		Name:  "authrpc.jwtsecret",
		Usage: "Path to a JWT secret to use for authenticated RPC endpoints",
		// Category: flags.APICategory,
	}

	// Logging and debug settings
	EthStatsURLFlag = &cli.StringFlag{
		Name:     "ethstats",
		Usage:    "Reporting URL of a ethstats service (nodename:secret@host:port)",
		Category: flags.MetricsCategory,
	}
	NoCompactionFlag = &cli.BoolFlag{
		Name:     "nocompaction",
		Usage:    "Disables db compaction after import",
		Category: flags.LoggingCategory,
	}

	// MISC settings
	SyncTargetFlag = &cli.StringFlag{
		Name:      "synctarget",
		Usage:     `Hash of the block to full sync to (dev testing feature)`,
		TakesFile: true,
		Category:  flags.MiscCategory,
	}

	// RPC settings
	IPCDisabledFlag = &cli.BoolFlag{
		Name:     "ipcdisable",
		Usage:    "Disable the IPC-RPC server",
		Category: flags.APICategory,
	}
	IPCPathFlag = &flags.DirectoryFlag{
		Name:     "ipcpath",
		Usage:    "Filename for IPC socket/pipe within the datadir (explicit paths escape it)",
		Category: flags.APICategory,
	}
	HTTPEnabledFlag = &cli.BoolFlag{
		Name:     "http",
		Usage:    "Enable the HTTP-RPC server",
		Category: flags.APICategory,
	}
	HTTPListenAddrFlag = &cli.StringFlag{
		Name:     "http.addr",
		Usage:    "HTTP-RPC server listening interface",
		Value:    node.DefaultHTTPHost,
		Category: flags.APICategory,
	}
	HTTPPortFlag = &cli.IntFlag{
		Name:     "http.port",
		Usage:    "HTTP-RPC server listening port",
		Value:    node.DefaultHTTPPort,
		Category: flags.APICategory,
	}
	HTTPCORSDomainFlag = &cli.StringFlag{
		Name:     "http.corsdomain",
		Usage:    "Comma separated list of domains from which to accept cross origin requests (browser enforced)",
		Value:    "",
		Category: flags.APICategory,
	}
	HTTPVirtualHostsFlag = &cli.StringFlag{
		Name:     "http.vhosts",
		Usage:    "Comma separated list of virtual hostnames from which to accept requests (server enforced). Accepts '*' wildcard.",
		Value:    strings.Join(node.DefaultConfig.HTTPVirtualHosts, ","),
		Category: flags.APICategory,
	}
	HTTPApiFlag = &cli.StringFlag{
		Name:     "http.api",
		Usage:    "API's offered over the HTTP-RPC interface",
		Value:    "",
		Category: flags.APICategory,
	}
	HTTPPathPrefixFlag = &cli.StringFlag{
		Name:     "http.rpcprefix",
		Usage:    "HTTP path prefix on which JSON-RPC is served. Use '/' to serve on all paths.",
		Value:    "",
		Category: flags.APICategory,
	}
	GraphQLEnabledFlag = &cli.BoolFlag{
		Name:     "graphql",
		Usage:    "Enable GraphQL on the HTTP-RPC server. Note that GraphQL can only be started if an HTTP server is started as well.",
		Category: flags.APICategory,
	}
	GraphQLCORSDomainFlag = &cli.StringFlag{
		Name:     "graphql.corsdomain",
		Usage:    "Comma separated list of domains from which to accept cross origin requests (browser enforced)",
		Value:    "",
		Category: flags.APICategory,
	}
	GraphQLVirtualHostsFlag = &cli.StringFlag{
		Name:     "graphql.vhosts",
		Usage:    "Comma separated list of virtual hostnames from which to accept requests (server enforced). Accepts '*' wildcard.",
		Value:    strings.Join(node.DefaultConfig.GraphQLVirtualHosts, ","),
		Category: flags.APICategory,
	}
	WSEnabledFlag = &cli.BoolFlag{
		Name:     "ws",
		Usage:    "Enable the WS-RPC server",
		Category: flags.APICategory,
	}
	WSListenAddrFlag = &cli.StringFlag{
		Name:     "ws.addr",
		Usage:    "WS-RPC server listening interface",
		Value:    node.DefaultWSHost,
		Category: flags.APICategory,
	}
	WSPortFlag = &cli.IntFlag{
		Name:     "ws.port",
		Usage:    "WS-RPC server listening port",
		Value:    node.DefaultWSPort,
		Category: flags.APICategory,
	}
	WSApiFlag = &cli.StringFlag{
		Name:     "ws.api",
		Usage:    "API's offered over the WS-RPC interface",
		Value:    "",
		Category: flags.APICategory,
	}
	WSAllowedOriginsFlag = &cli.StringFlag{
		Name:     "ws.origins",
		Usage:    "Origins from which to accept websockets requests",
		Value:    "",
		Category: flags.APICategory,
	}
	WSPathPrefixFlag = &cli.StringFlag{
		Name:     "ws.rpcprefix",
		Usage:    "HTTP path prefix on which JSON-RPC is served. Use '/' to serve on all paths.",
		Value:    "",
		Category: flags.APICategory,
	}
	ExecFlag = &cli.StringFlag{
		Name:     "exec",
		Usage:    "Execute JavaScript statement",
		Category: flags.APICategory,
	}
	PreloadJSFlag = &cli.StringFlag{
		Name:     "preload",
		Usage:    "Comma separated list of JavaScript files to preload into the console",
		Category: flags.APICategory,
	}
	AllowUnprotectedTxs = &cli.BoolFlag{
		Name:     "rpc.allow-unprotected-txs",
		Usage:    "Allow for unprotected (non EIP155 signed) transactions to be submitted via RPC",
		Category: flags.APICategory,
	}
	BatchRequestLimit = &cli.IntFlag{
		Name:     "rpc.batch-request-limit",
		Usage:    "Maximum number of requests in a batch",
		Value:    node.DefaultConfig.BatchRequestLimit,
		Category: flags.APICategory,
	}
	BatchResponseMaxSize = &cli.IntFlag{
		Name:     "rpc.batch-response-max-size",
		Usage:    "Maximum number of bytes returned from a batched call",
		Value:    node.DefaultConfig.BatchResponseMaxSize,
		Category: flags.APICategory,
	}

	// Network Settings
	MaxPeersFlag = &cli.IntFlag{
		Name:     "maxpeers",
		Usage:    "Maximum number of network peers (network disabled if set to 0)",
		Value:    node.DefaultConfig.P2P.MaxPeers,
		Category: flags.NetworkingCategory,
	}

	MaxPeersPerIPFlag = &cli.IntFlag{
		Name:     "maxpeersperip",
		Usage:    "Maximum number of network peers from a single IP address, (default used if set to <= 0, which is same as MaxPeers)",
		Value:    node.DefaultConfig.P2P.MaxPeersPerIP,
		Category: flags.NetworkingCategory,
	}

	MaxPendingPeersFlag = &cli.IntFlag{
		Name:     "maxpendpeers",
		Usage:    "Maximum number of pending connection attempts (defaults used if set to 0)",
		Value:    node.DefaultConfig.P2P.MaxPendingPeers,
		Category: flags.NetworkingCategory,
	}
	ListenPortFlag = &cli.IntFlag{
		Name:     "port",
		Usage:    "Network listening port",
		Value:    30303,
		Category: flags.NetworkingCategory,
	}
	BootnodesFlag = &cli.StringFlag{
		Name:     "bootnodes",
		Usage:    "Comma separated enode URLs for P2P discovery bootstrap",
		Value:    "",
		Category: flags.NetworkingCategory,
	}
	NodeKeyFileFlag = &cli.StringFlag{
		Name:     "nodekey",
		Usage:    "P2P node key file",
		Category: flags.NetworkingCategory,
	}
	NodeKeyHexFlag = &cli.StringFlag{
		Name:     "nodekeyhex",
		Usage:    "P2P node key as hex (for testing)",
		Category: flags.NetworkingCategory,
	}
	NATFlag = &cli.StringFlag{
		Name:     "nat",
		Usage:    "NAT port mapping mechanism (any|none|upnp|pmp|pmp:<IP>|extip:<IP>|stun:<IP:PORT>)",
		Value:    "any",
		Category: flags.NetworkingCategory,
	}
	NoDiscoverFlag = &cli.BoolFlag{
		Name:     "nodiscover",
		Usage:    "Disables the peer discovery mechanism (manual peer addition)",
		Category: flags.NetworkingCategory,
	}
	PeerFilterPatternsFlag = &cli.StringSliceFlag{
		Name:     "peerfilter",
		Usage:    "Disallow peers connection if peer name matches the given regular expressions",
		Category: flags.NetworkingCategory,
	}
	DiscoveryV4Flag = &cli.BoolFlag{
		Name:     "discovery.v4",
		Aliases:  []string{"discv4"},
		Usage:    "Enables the V4 discovery mechanism",
		Category: flags.NetworkingCategory,
		Value:    true,
	}
	DiscoveryV5Flag = &cli.BoolFlag{
		Name:     "discovery.v5",
		Aliases:  []string{"discv5"},
		Usage:    "Enables the V5 discovery mechanism",
		Category: flags.NetworkingCategory,
		Value:    false,
	}
	NetrestrictFlag = &cli.StringFlag{
		Name:     "netrestrict",
		Usage:    "Restricts network communication to the given IP networks (CIDR masks)",
		Category: flags.NetworkingCategory,
	}
	DNSDiscoveryFlag = &cli.StringFlag{
		Name:     "discovery.dns",
		Usage:    "Sets DNS discovery entry points (use \"\" to disable DNS)",
		Category: flags.NetworkingCategory,
	}
	DiscoveryPortFlag = &cli.IntFlag{
		Name:     "discovery.port",
		Usage:    "Use a custom UDP port for P2P discovery",
		Value:    30303,
		Category: flags.NetworkingCategory,
	}

	// Console
	JSpathFlag = &flags.DirectoryFlag{
		Name:     "jspath",
		Usage:    "JavaScript root path for `loadScript`",
		Value:    flags.DirectoryString("."),
		Category: flags.APICategory,
	}
	HttpHeaderFlag = &cli.StringSliceFlag{
		Name:     "header",
		Aliases:  []string{"H"},
		Usage:    "Pass custom headers to the RPC server when using --" + RemoteDBFlag.Name + " or the geth attach console. This flag can be given multiple times.",
		Category: flags.APICategory,
	}

	// Gas price oracle settings
	GpoBlocksFlag = &cli.IntFlag{
		Name:     "gpo.blocks",
		Usage:    "Number of recent blocks to check for gas prices",
		Value:    ethconfig.Defaults.GPO.Blocks,
		Category: flags.GasPriceCategory,
	}
	GpoPercentileFlag = &cli.IntFlag{
		Name:     "gpo.percentile",
		Usage:    "Suggested gas price is the given percentile of a set of recent transaction gas prices",
		Value:    ethconfig.Defaults.GPO.Percentile,
		Category: flags.GasPriceCategory,
	}
	GpoMaxGasPriceFlag = &cli.Int64Flag{
		Name:     "gpo.maxprice",
		Usage:    "Maximum transaction priority fee (or gasprice before London fork) to be recommended by gpo",
		Value:    ethconfig.Defaults.GPO.MaxPrice.Int64(),
		Category: flags.GasPriceCategory,
	}
	GpoIgnoreGasPriceFlag = &cli.Int64Flag{
		Name:     "gpo.ignoreprice",
		Usage:    "Gas price below which gpo will ignore transactions",
		Value:    ethconfig.Defaults.GPO.IgnorePrice.Int64(),
		Category: flags.GasPriceCategory,
	}

	// Metrics flags
	MetricsEnabledFlag = &cli.BoolFlag{
		Name:     "metrics",
		Usage:    "Enable metrics collection and reporting",
		Category: flags.MetricsCategory,
	}
	// MetricsHTTPFlag defines the endpoint for a stand-alone metrics HTTP endpoint.
	// Since the pprof service enables sensitive/vulnerable behavior, this allows a user
	// to enable a public-OK metrics endpoint without having to worry about ALSO exposing
	// other profiling behavior or information.
	MetricsHTTPFlag = &cli.StringFlag{
		Name:     "metrics.addr",
		Usage:    `Enable stand-alone metrics HTTP server listening interface.`,
		Category: flags.MetricsCategory,
	}
	MetricsPortFlag = &cli.IntFlag{
		Name: "metrics.port",
		Usage: `Metrics HTTP server listening port.
Please note that --` + MetricsHTTPFlag.Name + ` must be set to start the server.`,
		Value:    metrics.DefaultConfig.Port,
		Category: flags.MetricsCategory,
	}
	MetricsEnableInfluxDBFlag = &cli.BoolFlag{
		Name:     "metrics.influxdb",
		Usage:    "Enable metrics export/push to an external InfluxDB database",
		Category: flags.MetricsCategory,
	}
	MetricsInfluxDBEndpointFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.endpoint",
		Usage:    "InfluxDB API endpoint to report metrics to",
		Value:    metrics.DefaultConfig.InfluxDBEndpoint,
		Category: flags.MetricsCategory,
	}
	MetricsInfluxDBDatabaseFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.database",
		Usage:    "InfluxDB database name to push reported metrics to",
		Value:    metrics.DefaultConfig.InfluxDBDatabase,
		Category: flags.MetricsCategory,
	}
	MetricsInfluxDBUsernameFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.username",
		Usage:    "Username to authorize access to the database",
		Value:    metrics.DefaultConfig.InfluxDBUsername,
		Category: flags.MetricsCategory,
	}
	MetricsInfluxDBPasswordFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.password",
		Usage:    "Password to authorize access to the database",
		Value:    metrics.DefaultConfig.InfluxDBPassword,
		Category: flags.MetricsCategory,
	}
	// Tags are part of every measurement sent to InfluxDB. Queries on tags are faster in InfluxDB.
	// For example `host` tag could be used so that we can group all nodes and average a measurement
	// across all of them, but also so that we can select a specific node and inspect its measurements.
	// https://docs.influxdata.com/influxdb/v1.4/concepts/key_concepts/#tag-key
	MetricsInfluxDBTagsFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.tags",
		Usage:    "Comma-separated InfluxDB tags (key/values) attached to all measurements",
		Value:    metrics.DefaultConfig.InfluxDBTags,
		Category: flags.MetricsCategory,
	}

	MetricsEnableInfluxDBV2Flag = &cli.BoolFlag{
		Name:     "metrics.influxdbv2",
		Usage:    "Enable metrics export/push to an external InfluxDB v2 database",
		Category: flags.MetricsCategory,
	}

	MetricsInfluxDBTokenFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.token",
		Usage:    "Token to authorize access to the database (v2 only)",
		Value:    metrics.DefaultConfig.InfluxDBToken,
		Category: flags.MetricsCategory,
	}

	MetricsInfluxDBBucketFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.bucket",
		Usage:    "InfluxDB bucket name to push reported metrics to (v2 only)",
		Value:    metrics.DefaultConfig.InfluxDBBucket,
		Category: flags.MetricsCategory,
	}

	// Init network
	InitNetworkSize = &cli.IntFlag{
		Name:  "init.size",
		Usage: "the size of the network",
		Value: 1,
	}

	InitNetworkDir = &cli.StringFlag{
		Name:  "init.dir",
		Usage: "the direction to store initial network data",
		Value: "",
	}

	InitNetworkIps = &cli.StringFlag{
		Name:  "init.ips",
		Usage: "the ips of each node in the network, example '192.168.0.1,192.168.0.2'",
		Value: "",
	}

	InitNetworkPort = &cli.IntFlag{
		Name:  "init.p2p-port",
		Usage: "the p2p port of the nodes in the network",
		Value: 30311,
	}
	MetricsInfluxDBOrganizationFlag = &cli.StringFlag{
		Name:     "metrics.influxdb.organization",
		Usage:    "InfluxDB organization name (v2 only)",
		Value:    metrics.DefaultConfig.InfluxDBOrganization,
		Category: flags.MetricsCategory,
	}

	BlockAmountReserved = &cli.Uint64Flag{
		Name:     "block-amount-reserved",
		Usage:    "Sets the expected remained amount of blocks for offline block prune",
		Category: flags.BlockHistoryCategory,
		Value:    params.FullImmutabilityThreshold,
	}

	CheckSnapshotWithMPT = &cli.BoolFlag{
		Name:     "check-snapshot-with-mpt",
		Usage:    "Enable checking between snapshot and MPT ",
		Category: flags.FastNodeCategory,
	}

	EnableDoubleSignMonitorFlag = &cli.BoolFlag{
		Name:     "monitor.doublesign",
		Usage:    "Enable double sign monitor to check whether any validator signs multiple blocks",
		Category: flags.MinerCategory,
	}

	VotingEnabledFlag = &cli.BoolFlag{
		Name:     "vote",
		Usage:    "Enable voting when mining",
		Category: flags.FastFinalityCategory,
	}

	DisableVoteAttestationFlag = &cli.BoolFlag{
		Name:     "disablevoteattestation",
		Usage:    "Disable assembling vote attestation ",
		Category: flags.FastFinalityCategory,
	}

	EnableMaliciousVoteMonitorFlag = &cli.BoolFlag{
		Name:     "monitor.maliciousvote",
		Usage:    "Enable malicious vote monitor to check whether any validator violates the voting rules of fast finality",
		Category: flags.FastFinalityCategory,
	}

	BLSPasswordFileFlag = &cli.StringFlag{
		Name:     "blspassword",
		Usage:    "Password file path for the BLS wallet, which contains the password to unlock BLS wallet for managing votes in fast_finality feature",
		Category: flags.AccountCategory,
	}

	BLSWalletDirFlag = &flags.DirectoryFlag{
		Name:     "blswallet",
		Usage:    "Path for the blsWallet dir in fast finality feature (default = inside the datadir)",
		Category: flags.AccountCategory,
	}

	VoteJournalDirFlag = &flags.DirectoryFlag{
		Name:     "vote-journal-path",
		Usage:    "Path for the voteJournal dir in fast finality feature (default = inside the datadir)",
		Category: flags.FastFinalityCategory,
	}

	// Blob setting
	BlobExtraReserveFlag = &cli.Uint64Flag{
		Name:     "blob.extra-reserve",
		Usage:    "Extra reserve threshold for blob, blob never expires when 0 is set, default 28800",
		Value:    params.DefaultExtraReserveForBlobRequests,
		Category: flags.MiscCategory,
	}

	// Fake beacon
	FakeBeaconEnabledFlag = &cli.BoolFlag{
		Name:     "fake-beacon",
		Usage:    "Enable the HTTP-RPC server of fake-beacon",
		Category: flags.APICategory,
	}
	FakeBeaconAddrFlag = &cli.StringFlag{
		Name:     "fake-beacon.addr",
		Usage:    "HTTP-RPC server listening addr of fake-beacon",
		Value:    fakebeacon.DefaultAddr,
		Category: flags.APICategory,
	}
	FakeBeaconPortFlag = &cli.IntFlag{
		Name:     "fake-beacon.port",
		Usage:    "HTTP-RPC server listening port of fake-beacon",
		Value:    fakebeacon.DefaultPort,
		Category: flags.APICategory,
	}
)

var (
	// TestnetFlags is the flag group of all built-in supported testnets.
	TestnetFlags = []cli.Flag{
		ChapelFlag,
	}
	// NetworkFlags is the flag group of all built-in supported networks.
	NetworkFlags = append([]cli.Flag{BSCMainnetFlag}, TestnetFlags...)

	// DatabaseFlags is the flag group of all database flags.
	DatabaseFlags = []cli.Flag{
		DataDirFlag,
		AncientFlag,
		RemoteDBFlag,
		DBEngineFlag,
		StateSchemeFlag,
		HttpHeaderFlag,
	}
)

// MakeDataDir retrieves the currently requested data directory, terminating
// if none (or the empty string) is specified. If the node is starting a testnet,
// then a subdirectory of the specified datadir will be used.
func MakeDataDir(ctx *cli.Context) string {
	if path := ctx.String(DataDirFlag.Name); path != "" {
		return path
	}
	Fatalf("Cannot determine default data directory, please set manually (--datadir)")
	return ""
}

// setNodeKey creates a node key from set command line flags, either loading it
// from a file or as a specified hex value. If neither flags were provided, this
// method returns nil and an ephemeral key is to be generated.
func setNodeKey(ctx *cli.Context, cfg *p2p.Config) {
	var (
		hex  = ctx.String(NodeKeyHexFlag.Name)
		file = ctx.String(NodeKeyFileFlag.Name)
		key  *ecdsa.PrivateKey
		err  error
	)
	switch {
	case file != "" && hex != "":
		Fatalf("Options %q and %q are mutually exclusive", NodeKeyFileFlag.Name, NodeKeyHexFlag.Name)
	case file != "":
		if key, err = crypto.LoadECDSA(file); err != nil {
			Fatalf("Option %q: %v", NodeKeyFileFlag.Name, err)
		}
		cfg.PrivateKey = key
	case hex != "":
		if key, err = crypto.HexToECDSA(hex); err != nil {
			Fatalf("Option %q: %v", NodeKeyHexFlag.Name, err)
		}
		cfg.PrivateKey = key
	}
}

// setNodeUserIdent creates the user identifier from CLI flags.
func setNodeUserIdent(ctx *cli.Context, cfg *node.Config) {
	if identity := ctx.String(IdentityFlag.Name); len(identity) > 0 {
		cfg.UserIdent = identity
	}
}

// setBootstrapNodes creates a list of bootstrap nodes from the command line
// flags, reverting to pre-configured ones if none have been specified.
// Priority order for bootnodes configuration:
//
// 1. --bootnodes flag
// 2. Config file
// 3. Network preset flags (e.g. --holesky)
// 4. default to mainnet nodes
func setBootstrapNodes(ctx *cli.Context, cfg *p2p.Config) {
	urls := params.MainnetBootnodes
	if ctx.IsSet(BootnodesFlag.Name) {
		urls = SplitAndTrim(ctx.String(BootnodesFlag.Name))
	} else {
		if cfg.BootstrapNodes != nil {
			return // Already set by config file, don't apply defaults.
		}
	}
	cfg.BootstrapNodes = mustParseBootnodes(urls)
}

func mustParseBootnodes(urls []string) []*enode.Node {
	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		if url != "" {
			node, err := enode.Parse(enode.ValidSchemes, url)
			if err != nil {
				log.Crit("Bootstrap URL invalid", "enode", url, "err", err)
				return nil
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// setBootstrapNodesV5 creates a list of bootstrap nodes from the command line
// flags, reverting to pre-configured ones if none have been specified.
func setBootstrapNodesV5(ctx *cli.Context, cfg *p2p.Config) {
	urls := params.V5Bootnodes
	switch {
	case ctx.IsSet(BootnodesFlag.Name):
		urls = SplitAndTrim(ctx.String(BootnodesFlag.Name))
	case cfg.BootstrapNodesV5 != nil:
		return // already set, don't apply defaults.
	}

	cfg.BootstrapNodesV5 = make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		if url != "" {
			node, err := enode.Parse(enode.ValidSchemes, url)
			if err != nil {
				log.Error("Bootstrap URL invalid", "enode", url, "err", err)
				continue
			}
			cfg.BootstrapNodesV5 = append(cfg.BootstrapNodesV5, node)
		}
	}
}

// setListenAddress creates TCP/UDP listening address strings from set command
// line flags
func setListenAddress(ctx *cli.Context, cfg *p2p.Config) {
	if ctx.IsSet(ListenPortFlag.Name) {
		cfg.ListenAddr = fmt.Sprintf(":%d", ctx.Int(ListenPortFlag.Name))
	}
	if ctx.IsSet(DiscoveryPortFlag.Name) {
		cfg.DiscAddr = fmt.Sprintf(":%d", ctx.Int(DiscoveryPortFlag.Name))
	}
}

// setNAT creates a port mapper from command line flags.
func setNAT(ctx *cli.Context, cfg *p2p.Config) {
	if ctx.IsSet(NATFlag.Name) {
		natif, err := nat.Parse(ctx.String(NATFlag.Name))
		if err != nil {
			Fatalf("Option %s: %v", NATFlag.Name, err)
		}
		cfg.NAT = natif
	}
}

// SplitAndTrim splits input separated by a comma
// and trims excessive white space from the substrings.
func SplitAndTrim(input string) (ret []string) {
	l := strings.Split(input, ",")
	for _, r := range l {
		if r = strings.TrimSpace(r); r != "" {
			ret = append(ret, r)
		}
	}
	return ret
}

// setHTTP creates the HTTP RPC listener interface string from the set
// command line flags, returning empty if the HTTP endpoint is disabled.
func setHTTP(ctx *cli.Context, cfg *node.Config) {
	if ctx.Bool(HTTPEnabledFlag.Name) {
		if cfg.HTTPHost == "" {
			cfg.HTTPHost = "127.0.0.1"
		}
		if ctx.IsSet(HTTPListenAddrFlag.Name) {
			cfg.HTTPHost = ctx.String(HTTPListenAddrFlag.Name)
		}
	}

	if ctx.IsSet(HTTPPortFlag.Name) {
		cfg.HTTPPort = ctx.Int(HTTPPortFlag.Name)
	}

	if ctx.IsSet(AuthListenFlag.Name) {
		cfg.AuthAddr = ctx.String(AuthListenFlag.Name)
	}

	if ctx.IsSet(AuthPortFlag.Name) {
		cfg.AuthPort = ctx.Int(AuthPortFlag.Name)
	}

	if ctx.IsSet(AuthVirtualHostsFlag.Name) {
		cfg.AuthVirtualHosts = SplitAndTrim(ctx.String(AuthVirtualHostsFlag.Name))
	}

	if ctx.IsSet(HTTPCORSDomainFlag.Name) {
		cfg.HTTPCors = SplitAndTrim(ctx.String(HTTPCORSDomainFlag.Name))
	}

	if ctx.IsSet(HTTPApiFlag.Name) {
		cfg.HTTPModules = SplitAndTrim(ctx.String(HTTPApiFlag.Name))
	}

	if ctx.IsSet(HTTPVirtualHostsFlag.Name) {
		cfg.HTTPVirtualHosts = SplitAndTrim(ctx.String(HTTPVirtualHostsFlag.Name))
	}

	if ctx.IsSet(HTTPPathPrefixFlag.Name) {
		cfg.HTTPPathPrefix = ctx.String(HTTPPathPrefixFlag.Name)
	}
	if ctx.IsSet(AllowUnprotectedTxs.Name) {
		cfg.AllowUnprotectedTxs = ctx.Bool(AllowUnprotectedTxs.Name)
	}

	if ctx.IsSet(BatchRequestLimit.Name) {
		cfg.BatchRequestLimit = ctx.Int(BatchRequestLimit.Name)
	}

	if ctx.IsSet(BatchResponseMaxSize.Name) {
		cfg.BatchResponseMaxSize = ctx.Int(BatchResponseMaxSize.Name)
	}
}

// setGraphQL creates the GraphQL listener interface string from the set
// command line flags, returning empty if the GraphQL endpoint is disabled.
func setGraphQL(ctx *cli.Context, cfg *node.Config) {
	if ctx.IsSet(GraphQLCORSDomainFlag.Name) {
		cfg.GraphQLCors = SplitAndTrim(ctx.String(GraphQLCORSDomainFlag.Name))
	}
	if ctx.IsSet(GraphQLVirtualHostsFlag.Name) {
		cfg.GraphQLVirtualHosts = SplitAndTrim(ctx.String(GraphQLVirtualHostsFlag.Name))
	}
}

// setWS creates the WebSocket RPC listener interface string from the set
// command line flags, returning empty if the HTTP endpoint is disabled.
func setWS(ctx *cli.Context, cfg *node.Config) {
	if ctx.Bool(WSEnabledFlag.Name) {
		if cfg.WSHost == "" {
			cfg.WSHost = "127.0.0.1"
		}
		if ctx.IsSet(WSListenAddrFlag.Name) {
			cfg.WSHost = ctx.String(WSListenAddrFlag.Name)
		}
	}
	if ctx.IsSet(WSPortFlag.Name) {
		cfg.WSPort = ctx.Int(WSPortFlag.Name)
	}

	if ctx.IsSet(WSAllowedOriginsFlag.Name) {
		cfg.WSOrigins = SplitAndTrim(ctx.String(WSAllowedOriginsFlag.Name))
	}

	if ctx.IsSet(WSApiFlag.Name) {
		cfg.WSModules = SplitAndTrim(ctx.String(WSApiFlag.Name))
	}

	if ctx.IsSet(WSPathPrefixFlag.Name) {
		cfg.WSPathPrefix = ctx.String(WSPathPrefixFlag.Name)
	}
}

// setIPC creates an IPC path configuration from the set command line flags,
// returning an empty string if IPC was explicitly disabled, or the set path.
func setIPC(ctx *cli.Context, cfg *node.Config) {
	CheckExclusive(ctx, IPCDisabledFlag, IPCPathFlag)
	switch {
	case ctx.Bool(IPCDisabledFlag.Name):
		cfg.IPCPath = ""
	case ctx.IsSet(IPCPathFlag.Name):
		cfg.IPCPath = ctx.String(IPCPathFlag.Name)
	}
}

// setLes shows the deprecation warnings for LES flags.
func setLes(ctx *cli.Context, cfg *ethconfig.Config) {
	if ctx.IsSet(LightServeFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightServeFlag.Name)
	}
	if ctx.IsSet(LightIngressFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightIngressFlag.Name)
	}
	if ctx.IsSet(LightEgressFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightEgressFlag.Name)
	}
	if ctx.IsSet(LightMaxPeersFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightMaxPeersFlag.Name)
	}
	if ctx.IsSet(LightNoPruneFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightNoPruneFlag.Name)
	}
	if ctx.IsSet(LightNoSyncServeFlag.Name) {
		log.Warn("The light server has been deprecated, please remove this flag", "flag", LightNoSyncServeFlag.Name)
	}
}

// setMonitors enable monitors from the command line flags.
func setMonitors(ctx *cli.Context, cfg *node.Config) {
	if ctx.Bool(EnableDoubleSignMonitorFlag.Name) {
		cfg.EnableDoubleSignMonitor = true
	}
	if ctx.Bool(EnableMaliciousVoteMonitorFlag.Name) {
		cfg.EnableMaliciousVoteMonitor = true
	}
}

// MakeDatabaseHandles raises out the number of allowed file handles per process
// for Geth and returns half of the allowance to assign to the database.
func MakeDatabaseHandles(max int) int {
	limit, err := fdlimit.Maximum()
	if err != nil {
		Fatalf("Failed to retrieve file descriptor allowance: %v", err)
	}
	switch {
	case max == 0:
		// User didn't specify a meaningful value, use system limits
	case max < 128:
		// User specified something unhealthy, just use system defaults
		log.Error("File descriptor limit invalid (<128)", "had", max, "updated", limit)
	case max > limit:
		// User requested more than the OS allows, notify that we can't allocate it
		log.Warn("Requested file descriptors denied by OS", "req", max, "limit", limit)
	default:
		// User limit is meaningful and within allowed range, use that
		limit = max
	}
	raised, err := fdlimit.Raise(uint64(limit))
	if err != nil {
		Fatalf("Failed to raise file descriptor allowance: %v", err)
	}
	return int(raised / 2) // Leave half for networking and other stuff
}

// MakeAddress converts an account specified directly as a hex encoded string or
// a key index in the key store to an internal account representation.
func MakeAddress(ks *keystore.KeyStore, account string) (accounts.Account, error) {
	// If the specified account is a valid address, return it
	if common.IsHexAddress(account) {
		return accounts.Account{Address: common.HexToAddress(account)}, nil
	}
	// Otherwise try to interpret the account as a keystore index
	index, err := strconv.Atoi(account)
	if err != nil || index < 0 {
		return accounts.Account{}, fmt.Errorf("invalid account address or index %q", account)
	}
	log.Warn("-------------------------------------------------------------------")
	log.Warn("Referring to accounts by order in the keystore folder is dangerous!")
	log.Warn("This functionality is deprecated and will be removed in the future!")
	log.Warn("Please use explicit addresses! (can search via `geth account list`)")
	log.Warn("-------------------------------------------------------------------")

	accs := ks.Accounts()
	if len(accs) <= index {
		return accounts.Account{}, fmt.Errorf("index %d higher than number of accounts %d", index, len(accs))
	}
	return accs[index], nil
}

// setEtherbase retrieves the etherbase from the directly specified command line flags.
func setEtherbase(ctx *cli.Context, cfg *ethconfig.Config) {
	if !ctx.IsSet(MinerEtherbaseFlag.Name) {
		return
	}
	addr := ctx.String(MinerEtherbaseFlag.Name)
	if strings.HasPrefix(addr, "0x") || strings.HasPrefix(addr, "0X") {
		addr = addr[2:]
	}
	b, err := hex.DecodeString(addr)
	if err != nil || len(b) != common.AddressLength {
		Fatalf("-%s: invalid etherbase address %q", MinerEtherbaseFlag.Name, addr)
		return
	}
	cfg.Miner.Etherbase = common.BytesToAddress(b)
}

// MakePasswordList reads password lines from the file specified by the global --password flag.
func MakePasswordList(ctx *cli.Context) []string {
	return MakePasswordListFromPath(ctx.Path(PasswordFileFlag.Name))
}

func MakePasswordListFromPath(path string) []string {
	if path == "" {
		return nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		Fatalf("Failed to read password file: %v", err)
	}
	lines := strings.Split(string(text), "\n")
	// Sanitise DOS line endings.
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	return lines
}

func SetP2PConfig(ctx *cli.Context, cfg *p2p.Config) {
	setNodeKey(ctx, cfg)
	setNAT(ctx, cfg)
	setListenAddress(ctx, cfg)
	setBootstrapNodes(ctx, cfg)
	setBootstrapNodesV5(ctx, cfg)

	if ctx.IsSet(MaxPeersFlag.Name) {
		cfg.MaxPeers = ctx.Int(MaxPeersFlag.Name)
	}
	// if max peers per ip is not set, use max peers
	if cfg.MaxPeersPerIP <= 0 {
		cfg.MaxPeersPerIP = cfg.MaxPeers
	}
	// flag like: `--maxpeersperip 10` could override the setting in config.toml
	if ctx.IsSet(MaxPeersPerIPFlag.Name) {
		cfg.MaxPeersPerIP = ctx.Int(MaxPeersPerIPFlag.Name)
	}

	ethPeers := cfg.MaxPeers
	log.Info("Maximum peer count", "ETH", ethPeers, "total", cfg.MaxPeers)

	if ctx.IsSet(MaxPendingPeersFlag.Name) {
		cfg.MaxPendingPeers = ctx.Int(MaxPendingPeersFlag.Name)
	}
	if ctx.IsSet(NoDiscoverFlag.Name) {
		cfg.NoDiscovery = true
	}
	if ctx.IsSet(PeerFilterPatternsFlag.Name) {
		cfg.PeerFilterPatterns = ctx.StringSlice(PeerFilterPatternsFlag.Name)
	}

	CheckExclusive(ctx, DiscoveryV4Flag, NoDiscoverFlag)
	CheckExclusive(ctx, DiscoveryV5Flag, NoDiscoverFlag)
	cfg.DiscoveryV4 = ctx.Bool(DiscoveryV4Flag.Name)
	cfg.DiscoveryV5 = ctx.Bool(DiscoveryV5Flag.Name)

	if netrestrict := ctx.String(NetrestrictFlag.Name); netrestrict != "" {
		list, err := netutil.ParseNetlist(netrestrict)
		if err != nil {
			Fatalf("Option %q: %v", NetrestrictFlag.Name, err)
		}
		cfg.NetRestrict = list
	}

	if ctx.Bool(DeveloperFlag.Name) {
		// --dev mode can't use p2p networking.
		cfg.MaxPeers = 0
		cfg.ListenAddr = ""
		cfg.NoDial = true
		cfg.NoDiscovery = true
		cfg.DiscoveryV5 = false
	}
}

// SetNodeConfig applies node-related command line flags to the config.
func SetNodeConfig(ctx *cli.Context, cfg *node.Config) {
	setInstance(ctx, cfg)
	SetP2PConfig(ctx, &cfg.P2P)
	setIPC(ctx, cfg)
	setHTTP(ctx, cfg)
	setGraphQL(ctx, cfg)
	setWS(ctx, cfg)
	setNodeUserIdent(ctx, cfg)
	SetDataDir(ctx, cfg)
	setSmartCard(ctx, cfg)
	setMonitors(ctx, cfg)
	setBLSWalletDir(ctx, cfg)
	setVoteJournalDir(ctx, cfg)

	if ctx.IsSet(JWTSecretFlag.Name) {
		cfg.JWTSecret = ctx.String(JWTSecretFlag.Name)
	}
	if ctx.IsSet(EnablePersonal.Name) {
		log.Warn(fmt.Sprintf("Option --%s is deprecated. The 'personal' RPC namespace has been removed.", EnablePersonal.Name))
	}

	if ctx.IsSet(ExternalSignerFlag.Name) {
		cfg.ExternalSigner = ctx.String(ExternalSignerFlag.Name)
	}

	if ctx.IsSet(KeyStoreDirFlag.Name) {
		cfg.KeyStoreDir = ctx.String(KeyStoreDirFlag.Name)
	}
	if ctx.IsSet(DeveloperFlag.Name) {
		cfg.UseLightweightKDF = true
	}
	if ctx.IsSet(LightKDFFlag.Name) {
		cfg.UseLightweightKDF = ctx.Bool(LightKDFFlag.Name)
	}
	if ctx.IsSet(NoUSBFlag.Name) || cfg.NoUSB {
		log.Warn("Option nousb is deprecated and USB is deactivated by default. Use --usb to enable")
	}
	if ctx.IsSet(USBFlag.Name) {
		cfg.USB = ctx.Bool(USBFlag.Name)
	}
	if ctx.IsSet(DirectBroadcastFlag.Name) {
		cfg.DirectBroadcast = ctx.Bool(DirectBroadcastFlag.Name)
	}
	if ctx.IsSet(DisableSnapProtocolFlag.Name) {
		cfg.DisableSnapProtocol = ctx.Bool(DisableSnapProtocolFlag.Name)
	}
	if ctx.IsSet(RangeLimitFlag.Name) {
		cfg.RangeLimit = ctx.Bool(RangeLimitFlag.Name)
	}
	if ctx.IsSet(InsecureUnlockAllowedFlag.Name) {
		cfg.InsecureUnlockAllowed = ctx.Bool(InsecureUnlockAllowedFlag.Name)
	}
	if ctx.IsSet(BLSPasswordFileFlag.Name) {
		cfg.BLSPasswordFile = ctx.String(BLSPasswordFileFlag.Name)
	}
	if ctx.IsSet(DBEngineFlag.Name) {
		dbEngine := ctx.String(DBEngineFlag.Name)
		if dbEngine != "leveldb" && dbEngine != "pebble" {
			Fatalf("Invalid choice for db.engine '%s', allowed 'leveldb' or 'pebble'", dbEngine)
		}
		log.Info(fmt.Sprintf("Using %s as db engine", dbEngine))
		cfg.DBEngine = dbEngine
	}
	// deprecation notice for log debug flags (TODO: find a more appropriate place to put these?)
	if ctx.IsSet(LogBacktraceAtFlag.Name) {
		log.Warn("log.backtrace flag is deprecated")
	}
	if ctx.IsSet(LogDebugFlag.Name) {
		log.Warn("log.debug flag is deprecated")
	}
}

func setSmartCard(ctx *cli.Context, cfg *node.Config) {
	// Skip enabling smartcards if no path is set
	path := ctx.String(SmartCardDaemonPathFlag.Name)
	if path == "" {
		return
	}
	// Sanity check that the smartcard path is valid
	fi, err := os.Stat(path)
	if err != nil {
		log.Info("Smartcard socket not found, disabling", "err", err)
		return
	}
	if fi.Mode()&os.ModeType != os.ModeSocket {
		log.Error("Invalid smartcard daemon path", "path", path, "type", fi.Mode().String())
		return
	}
	// Smartcard daemon path exists and is a socket, enable it
	cfg.SmartCardDaemonPath = path
}

func SetDataDir(ctx *cli.Context, cfg *node.Config) {
	switch {
	case ctx.IsSet(DataDirFlag.Name):
		cfg.DataDir = ctx.String(DataDirFlag.Name)
	case ctx.Bool(DeveloperFlag.Name):
		cfg.DataDir = "" // unless explicitly requested, use memory databases
	}
}

func setVoteJournalDir(ctx *cli.Context, cfg *node.Config) {
	dataDir := cfg.DataDir
	if ctx.IsSet(VoteJournalDirFlag.Name) {
		cfg.VoteJournalDir = ctx.String(VoteJournalDirFlag.Name)
	} else if cfg.VoteJournalDir == "" {
		cfg.VoteJournalDir = filepath.Join(dataDir, "voteJournal")
	}
}

func setBLSWalletDir(ctx *cli.Context, cfg *node.Config) {
	dataDir := cfg.DataDir
	if ctx.IsSet(BLSWalletDirFlag.Name) {
		cfg.BLSWalletDir = ctx.String(BLSWalletDirFlag.Name)
	} else if cfg.BLSWalletDir == "" {
		cfg.BLSWalletDir = filepath.Join(dataDir, "bls/wallet")
	}
}

func setGPO(ctx *cli.Context, cfg *gasprice.Config) {
	if ctx.IsSet(GpoBlocksFlag.Name) {
		cfg.Blocks = ctx.Int(GpoBlocksFlag.Name)
	}
	if ctx.IsSet(GpoPercentileFlag.Name) {
		cfg.Percentile = ctx.Int(GpoPercentileFlag.Name)
	}
	if ctx.IsSet(GpoMaxGasPriceFlag.Name) {
		cfg.MaxPrice = big.NewInt(ctx.Int64(GpoMaxGasPriceFlag.Name))
	}
	if ctx.IsSet(GpoIgnoreGasPriceFlag.Name) {
		cfg.IgnorePrice = big.NewInt(ctx.Int64(GpoIgnoreGasPriceFlag.Name))
	}
}

func setTxPool(ctx *cli.Context, cfg *legacypool.Config) {
	if ctx.IsSet(TxPoolLocalsFlag.Name) {
		locals := strings.Split(ctx.String(TxPoolLocalsFlag.Name), ",")
		for _, account := range locals {
			if trimmed := strings.TrimSpace(account); !common.IsHexAddress(trimmed) {
				Fatalf("Invalid account in --txpool.locals: %s", trimmed)
			} else {
				cfg.Locals = append(cfg.Locals, common.HexToAddress(account))
			}
		}
	}
	if ctx.IsSet(TxPoolNoLocalsFlag.Name) {
		cfg.NoLocals = ctx.Bool(TxPoolNoLocalsFlag.Name)
	}
	if ctx.IsSet(TxPoolJournalFlag.Name) {
		cfg.Journal = ctx.String(TxPoolJournalFlag.Name)
	}
	if ctx.IsSet(TxPoolRejournalFlag.Name) {
		cfg.Rejournal = ctx.Duration(TxPoolRejournalFlag.Name)
	}
	if ctx.IsSet(TxPoolPriceLimitFlag.Name) {
		cfg.PriceLimit = ctx.Uint64(TxPoolPriceLimitFlag.Name)
	}
	if ctx.IsSet(TxPoolPriceBumpFlag.Name) {
		cfg.PriceBump = ctx.Uint64(TxPoolPriceBumpFlag.Name)
	}
	if ctx.IsSet(TxPoolAccountSlotsFlag.Name) {
		cfg.AccountSlots = ctx.Uint64(TxPoolAccountSlotsFlag.Name)
	}
	if ctx.IsSet(TxPoolGlobalSlotsFlag.Name) {
		cfg.GlobalSlots = ctx.Uint64(TxPoolGlobalSlotsFlag.Name)
	}
	if ctx.IsSet(TxPoolAccountQueueFlag.Name) {
		cfg.AccountQueue = ctx.Uint64(TxPoolAccountQueueFlag.Name)
	}
	if ctx.IsSet(TxPoolGlobalQueueFlag.Name) {
		cfg.GlobalQueue = ctx.Uint64(TxPoolGlobalQueueFlag.Name)
	}
	if ctx.IsSet(TxPoolOverflowPoolSlotsFlag.Name) {
		cfg.OverflowPoolSlots = ctx.Uint64(TxPoolOverflowPoolSlotsFlag.Name)
	}
	if ctx.IsSet(TxPoolLifetimeFlag.Name) {
		cfg.Lifetime = ctx.Duration(TxPoolLifetimeFlag.Name)
	}
	if ctx.IsSet(TxPoolReannounceTimeFlag.Name) {
		cfg.ReannounceTime = ctx.Duration(TxPoolReannounceTimeFlag.Name)
	}
}

func setBlobPool(ctx *cli.Context, cfg *blobpool.Config) {
	if ctx.IsSet(BlobPoolDataDirFlag.Name) {
		cfg.Datadir = ctx.String(BlobPoolDataDirFlag.Name)
	}
	if ctx.IsSet(BlobPoolDataCapFlag.Name) {
		cfg.Datacap = ctx.Uint64(BlobPoolDataCapFlag.Name)
	}
	if ctx.IsSet(BlobPoolPriceBumpFlag.Name) {
		cfg.PriceBump = ctx.Uint64(BlobPoolPriceBumpFlag.Name)
	}
}

func setMiner(ctx *cli.Context, cfg *minerconfig.Config) {
	if ctx.IsSet(MinerExtraDataFlag.Name) {
		cfg.ExtraData = []byte(ctx.String(MinerExtraDataFlag.Name))
	}
	if ctx.IsSet(MinerGasLimitFlag.Name) {
		cfg.GasCeil = ctx.Uint64(MinerGasLimitFlag.Name)
	}
	if ctx.IsSet(MinerGasPriceFlag.Name) {
		cfg.GasPrice = flags.GlobalBig(ctx, MinerGasPriceFlag.Name)
	}
	if ctx.IsSet(MinerRecommitIntervalFlag.Name) {
		cfg.Recommit = ctx.Duration(MinerRecommitIntervalFlag.Name)
	}
	if ctx.IsSet(MinerDelayLeftoverFlag.Name) {
		cfg.DelayLeftOver = ctx.Duration(MinerDelayLeftoverFlag.Name)
	}
	if ctx.Bool(VotingEnabledFlag.Name) {
		cfg.VoteEnable = true
	}
	if ctx.IsSet(MinerNewPayloadTimeoutFlag.Name) {
		log.Warn("The flag --miner.newpayload-timeout is deprecated and will be removed, please use --miner.recommit")
		cfg.Recommit = ctx.Duration(MinerNewPayloadTimeoutFlag.Name)
	}
	if ctx.Bool(DisableVoteAttestationFlag.Name) {
		cfg.DisableVoteAttestation = true
	}
}

func setRequiredBlocks(ctx *cli.Context, cfg *ethconfig.Config) {
	requiredBlocks := ctx.String(EthRequiredBlocksFlag.Name)
	if requiredBlocks == "" {
		if ctx.IsSet(LegacyWhitelistFlag.Name) {
			log.Warn("The flag --whitelist is deprecated and will be removed, please use --eth.requiredblocks")
			requiredBlocks = ctx.String(LegacyWhitelistFlag.Name)
		} else {
			return
		}
	}
	cfg.RequiredBlocks = make(map[uint64]common.Hash)
	for _, entry := range strings.Split(requiredBlocks, ",") {
		parts := strings.Split(entry, "=")
		if len(parts) != 2 {
			Fatalf("Invalid required block entry: %s", entry)
		}
		number, err := strconv.ParseUint(parts[0], 0, 64)
		if err != nil {
			Fatalf("Invalid required block number %s: %v", parts[0], err)
		}
		var hash common.Hash
		if err = hash.UnmarshalText([]byte(parts[1])); err != nil {
			Fatalf("Invalid required block hash %s: %v", parts[1], err)
		}
		cfg.RequiredBlocks[number] = hash
	}
}

// CheckExclusive verifies that only a single instance of the provided flags was
// set by the user. Each flag might optionally be followed by a string type to
// specialize it further.
func CheckExclusive(ctx *cli.Context, args ...interface{}) {
	set := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		// Make sure the next argument is a flag and skip if not set
		flag, ok := args[i].(cli.Flag)
		if !ok {
			panic(fmt.Sprintf("invalid argument, not cli.Flag type: %T", args[i]))
		}
		// Check if next arg extends current and expand its name if so
		name := flag.Names()[0]

		if i+1 < len(args) {
			switch option := args[i+1].(type) {
			case string:
				// Extended flag check, make sure value set doesn't conflict with passed in option
				if ctx.String(flag.Names()[0]) == option {
					name += "=" + option
					set = append(set, "--"+name)
				}
				// shift arguments and continue
				i++
				continue

			case cli.Flag:
			default:
				panic(fmt.Sprintf("invalid argument, not cli.Flag or string extension: %T", args[i+1]))
			}
		}
		// Mark the flag if it's set
		if ctx.IsSet(flag.Names()[0]) {
			set = append(set, "--"+name)
		}
	}
	if len(set) > 1 {
		Fatalf("Flags %v can't be used at the same time", strings.Join(set, ", "))
	}
}

// SetEthConfig applies eth-related command line flags to the config.
func SetEthConfig(ctx *cli.Context, stack *node.Node, cfg *ethconfig.Config) {
	// Avoid conflicting network flags
	CheckExclusive(ctx, BSCMainnetFlag, DeveloperFlag)
	CheckExclusive(ctx, DeveloperFlag, ExternalSignerFlag) // Can't use both ephemeral unlocked and external signer

	// Set configurations from CLI flags
	setEtherbase(ctx, cfg)
	setGPO(ctx, &cfg.GPO)
	setTxPool(ctx, &cfg.TxPool)
	setBlobPool(ctx, &cfg.BlobPool)
	setMiner(ctx, &cfg.Miner)
	setRequiredBlocks(ctx, cfg)
	setLes(ctx, cfg)

	// Cap the cache allowance and tune the garbage collector
	mem, err := gopsutil.VirtualMemory()
	if err == nil {
		if 32<<(^uintptr(0)>>63) == 32 && mem.Total > 2*1024*1024*1024 {
			log.Warn("Lowering memory allowance on 32bit arch", "available", mem.Total/1024/1024, "addressable", 2*1024)
			mem.Total = 2 * 1024 * 1024 * 1024
		}
		allowance := int(mem.Total / 1024 / 1024 / 3)
		if cache := ctx.Int(CacheFlag.Name); cache > allowance {
			log.Warn("Sanitizing cache to Go's GC limits", "provided", cache, "updated", allowance)
			ctx.Set(CacheFlag.Name, strconv.Itoa(allowance))
		}
	}
	// Ensure Go's GC ignores the database cache for trigger percentage
	cache := ctx.Int(CacheFlag.Name)
	gogc := math.Max(20, math.Min(100, 100/(float64(cache)/1024)))

	log.Debug("Sanitizing Go's GC trigger", "percent", int(gogc))
	godebug.SetGCPercent(int(gogc))

	if ctx.IsSet(SyncTargetFlag.Name) {
		cfg.SyncMode = ethconfig.FullSync // dev sync target forces full sync
	} else if ctx.IsSet(SyncModeFlag.Name) {
		if err = cfg.SyncMode.UnmarshalText([]byte(ctx.String(SyncModeFlag.Name))); err != nil {
			Fatalf("invalid --syncmode flag: %v", err)
		}
	}
	if ctx.IsSet(NetworkIdFlag.Name) {
		cfg.NetworkId = ctx.Uint64(NetworkIdFlag.Name)
	}
	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheDatabaseFlag.Name) {
		cfg.DatabaseCache = ctx.Int(CacheFlag.Name) * ctx.Int(CacheDatabaseFlag.Name) / 100
	}
	cfg.DatabaseHandles = MakeDatabaseHandles(ctx.Int(FDLimitFlag.Name))
	if ctx.IsSet(AncientFlag.Name) {
		cfg.DatabaseFreezer = ctx.String(AncientFlag.Name)
	}
	if ctx.IsSet(DiffFlag.Name) {
		cfg.DatabaseDiff = ctx.String(DiffFlag.Name)
	}
	if ctx.IsSet(PersistDiffFlag.Name) {
		cfg.PersistDiff = ctx.Bool(PersistDiffFlag.Name)
	}
	if ctx.IsSet(DiffBlockFlag.Name) {
		cfg.DiffBlock = ctx.Uint64(DiffBlockFlag.Name)
	}
	if ctx.IsSet(PruneAncientDataFlag.Name) {
		if cfg.SyncMode != ethconfig.FullSync {
			log.Warn("pruneancient parameter can only be used with syncmode=full, force to full sync")
			cfg.SyncMode = ethconfig.FullSync
		}
		cfg.PruneAncientData = ctx.Bool(PruneAncientDataFlag.Name)
	}
	if gcmode := ctx.String(GCModeFlag.Name); gcmode != "full" && gcmode != "archive" {
		Fatalf("--%s must be either 'full' or 'archive'", GCModeFlag.Name)
	}
	if ctx.IsSet(GCModeFlag.Name) {
		cfg.NoPruning = ctx.String(GCModeFlag.Name) == "archive"
	}
	if ctx.IsSet(DirectBroadcastFlag.Name) {
		cfg.DirectBroadcast = ctx.Bool(DirectBroadcastFlag.Name)
	}
	if ctx.IsSet(DisableSnapProtocolFlag.Name) {
		cfg.DisableSnapProtocol = ctx.Bool(DisableSnapProtocolFlag.Name)
	}
	if ctx.IsSet(EnableTrustProtocolFlag.Name) {
		cfg.EnableTrustProtocol = ctx.IsSet(EnableTrustProtocolFlag.Name)
	}
	if ctx.IsSet(RangeLimitFlag.Name) {
		cfg.RangeLimit = ctx.Bool(RangeLimitFlag.Name)
	}
	if ctx.IsSet(CacheNoPrefetchFlag.Name) {
		cfg.NoPrefetch = ctx.Bool(CacheNoPrefetchFlag.Name)
	}
	// Read the value from the flag no matter if it's set or not.
	cfg.Preimages = ctx.Bool(CachePreimagesFlag.Name)
	if cfg.NoPruning && !cfg.Preimages {
		cfg.Preimages = true
		log.Info("Enabling recording of key preimages since archive mode is used")
	}
	if ctx.IsSet(StateHistoryFlag.Name) {
		cfg.StateHistory = ctx.Uint64(StateHistoryFlag.Name)
	}
	scheme, err := ParseCLIAndConfigStateScheme(ctx.String(StateSchemeFlag.Name), cfg.StateScheme)
	if err != nil {
		Fatalf("%v", err)
	}
	cfg.StateScheme = scheme
	// Parse transaction history flag, if user is still using legacy config
	// file with 'TxLookupLimit' configured, copy the value to 'TransactionHistory'.
	if cfg.TransactionHistory == ethconfig.Defaults.TransactionHistory && cfg.TxLookupLimit != ethconfig.Defaults.TxLookupLimit {
		log.Warn("The config option 'TxLookupLimit' is deprecated and will be removed, please use 'TransactionHistory'")
		cfg.TransactionHistory = cfg.TxLookupLimit
	}
	if ctx.IsSet(TransactionHistoryFlag.Name) {
		cfg.TransactionHistory = ctx.Uint64(TransactionHistoryFlag.Name)
	} else if ctx.IsSet(TxLookupLimitFlag.Name) {
		log.Warn("The flag --txlookuplimit is deprecated and will be removed, please use --history.transactions")
		cfg.TransactionHistory = ctx.Uint64(TxLookupLimitFlag.Name)
	}
	if ctx.IsSet(PathDBSyncFlag.Name) {
		cfg.PathSyncFlush = true
	}
	if ctx.IsSet(JournalFileFlag.Name) {
		cfg.JournalFileEnabled = true
	}

	if ctx.String(GCModeFlag.Name) == "archive" && cfg.TransactionHistory != 0 {
		cfg.TransactionHistory = 0
		log.Warn("Disabled transaction unindexing for archive node")

		cfg.StateScheme = rawdb.HashScheme
		log.Warn("Forcing hash state-scheme for archive mode")
	}
	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheTrieFlag.Name) {
		cfg.TrieCleanCache = ctx.Int(CacheFlag.Name) * ctx.Int(CacheTrieFlag.Name) / 100
	}
	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheGCFlag.Name) {
		cfg.TrieDirtyCache = ctx.Int(CacheFlag.Name) * ctx.Int(CacheGCFlag.Name) / 100
	}
	if ctx.IsSet(TriesInMemoryFlag.Name) {
		cfg.TriesInMemory = ctx.Uint64(TriesInMemoryFlag.Name)
	}
	if ctx.IsSet(TriesVerifyModeFlag.Name) {
		if err = cfg.TriesVerifyMode.UnmarshalText([]byte(ctx.String(TriesVerifyModeFlag.Name))); err != nil {
			Fatalf("invalid --tries-verify-mode flag: %v", err)
		}
		// If a node sets verify mode to full or insecure, it's a fast node and need
		// to verify blocks from verify nodes, then it should enable trust protocol.
		if cfg.TriesVerifyMode.NeedRemoteVerify() {
			cfg.EnableTrustProtocol = true
		}
		// A node without trie is not able to provide snap data, so it should disable snap protocol.
		if cfg.TriesVerifyMode != core.LocalVerify {
			log.Info("Automatically disables snap protocol due to verify mode", "mode", cfg.TriesVerifyMode)
			cfg.DisableSnapProtocol = true
		}

		if cfg.SyncMode == ethconfig.SnapSync && cfg.TriesVerifyMode.NoTries() {
			log.Warn("Only local TriesVerifyMode can support snap sync, resetting to full sync", "mode", cfg.TriesVerifyMode)
			cfg.SyncMode = ethconfig.FullSync
		}
	}
	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheSnapshotFlag.Name) {
		cfg.SnapshotCache = ctx.Int(CacheFlag.Name) * ctx.Int(CacheSnapshotFlag.Name) / 100
	}
	if ctx.IsSet(CacheLogSizeFlag.Name) {
		cfg.FilterLogCacheSize = ctx.Int(CacheLogSizeFlag.Name)
	}
	if !ctx.Bool(SnapshotFlag.Name) || cfg.SnapshotCache == 0 {
		// If snap-sync is requested, this flag is also required
		if cfg.SyncMode == ethconfig.SnapSync {
			if !ctx.Bool(SnapshotFlag.Name) {
				log.Warn("Snap sync requested, enabling --snapshot")
			}
			if cfg.SnapshotCache == 0 {
				log.Warn("Snap sync requested, resetting --cache.snapshot")
				cfg.SnapshotCache = ctx.Int(CacheFlag.Name) * CacheSnapshotFlag.Value / 100
			}
		} else {
			cfg.TrieCleanCache += cfg.SnapshotCache
			cfg.SnapshotCache = 0 // Disabled
		}
	}
	if ctx.IsSet(VMEnableDebugFlag.Name) {
		// TODO(fjl): force-enable this in --dev mode
		cfg.EnablePreimageRecording = ctx.Bool(VMEnableDebugFlag.Name)
	}

	if ctx.IsSet(RPCGlobalGasCapFlag.Name) {
		cfg.RPCGasCap = ctx.Uint64(RPCGlobalGasCapFlag.Name)
	}
	if cfg.RPCGasCap != 0 {
		log.Info("Set global gas cap", "cap", cfg.RPCGasCap)
	} else {
		log.Info("Global gas cap disabled")
	}
	if ctx.IsSet(RPCGlobalEVMTimeoutFlag.Name) {
		cfg.RPCEVMTimeout = ctx.Duration(RPCGlobalEVMTimeoutFlag.Name)
	}
	if ctx.IsSet(RPCGlobalTxFeeCapFlag.Name) {
		cfg.RPCTxFeeCap = ctx.Float64(RPCGlobalTxFeeCapFlag.Name)
	}
	if ctx.IsSet(NoDiscoverFlag.Name) {
		cfg.EthDiscoveryURLs, cfg.SnapDiscoveryURLs, cfg.TrustDiscoveryURLs, cfg.BscDiscoveryURLs = []string{}, []string{}, []string{}, []string{}
	} else if ctx.IsSet(DNSDiscoveryFlag.Name) {
		urls := ctx.String(DNSDiscoveryFlag.Name)
		if urls == "" {
			cfg.EthDiscoveryURLs = []string{}
		} else {
			cfg.EthDiscoveryURLs = SplitAndTrim(urls)
		}
	}
	// Override any default configs for hard coded networks.
	switch {
	case ctx.Bool(BSCMainnetFlag.Name):
		if !ctx.IsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 56
		}
		cfg.Genesis = core.DefaultBSCGenesisBlock()
		SetDNSDiscoveryDefaults(cfg, params.BSCGenesisHash)
	case ctx.Bool(ChapelFlag.Name) || cfg.NetworkId == 97:
		if !ctx.IsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 97
		}
		cfg.Genesis = core.DefaultChapelGenesisBlock()
		SetDNSDiscoveryDefaults(cfg, params.ChapelGenesisHash)
	case ctx.Bool(DeveloperFlag.Name):
		if !ctx.IsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 1337
		}
		cfg.SyncMode = ethconfig.FullSync
		// Create new developer account or reuse existing one
		var (
			developer  accounts.Account
			passphrase string
			err        error
		)
		if list := MakePasswordList(ctx); len(list) > 0 {
			// Just take the first value. Although the function returns a possible multiple values and
			// some usages iterate through them as attempts, that doesn't make sense in this setting,
			// when we're definitely concerned with only one account.
			passphrase = list[0]
		}

		// Unlock the developer account by local keystore.
		var ks *keystore.KeyStore
		if keystores := stack.AccountManager().Backends(keystore.KeyStoreType); len(keystores) > 0 {
			ks = keystores[0].(*keystore.KeyStore)
		}
		if ks == nil {
			Fatalf("Keystore is not available")
		}

		// Figure out the dev account address.
		// setEtherbase has been called above, configuring the miner address from command line flags.
		if cfg.Miner.Etherbase != (common.Address{}) {
			developer = accounts.Account{Address: cfg.Miner.Etherbase}
		} else if accs := ks.Accounts(); len(accs) > 0 {
			developer = ks.Accounts()[0]
		} else {
			developer, err = ks.NewAccount(passphrase)
			if err != nil {
				Fatalf("Failed to create developer account: %v", err)
			}
		}
		// Make sure the address is configured as fee recipient, otherwise
		// the miner will fail to start.
		cfg.Miner.Etherbase = developer.Address

		if err := ks.Unlock(developer, passphrase); err != nil {
			Fatalf("Failed to unlock developer account: %v", err)
		}
		log.Info("Using developer account", "address", developer.Address)

		// Create a new developer genesis block or reuse existing one
		cfg.Genesis = core.DeveloperGenesisBlock(ctx.Uint64(DeveloperGasLimitFlag.Name), &developer.Address)
		if ctx.IsSet(DataDirFlag.Name) {
			// If datadir doesn't exist we need to open db in write-mode
			// so leveldb can create files.
			readonly := true
			if !common.FileExist(stack.ResolvePath("chaindata")) {
				readonly = false
			}
			// Check if we have an already initialized chain and fall back to
			// that if so. Otherwise, we need to generate a new genesis spec.
			chaindb := MakeChainDatabase(ctx, stack, readonly, false)
			if rawdb.ReadCanonicalHash(chaindb, 0) != (common.Hash{}) {
				cfg.Genesis = nil // fallback to db content

				// validate genesis has PoS enabled in block 0
				genesis, err := core.ReadGenesis(chaindb)
				if err != nil {
					Fatalf("Could not read genesis from database: %v", err)
				}
				if genesis.Config.TerminalTotalDifficulty == nil {
					Fatalf("Bad developer-mode genesis configuration: terminalTotalDifficulty must be specified")
				} else if genesis.Config.TerminalTotalDifficulty.Cmp(big.NewInt(0)) != 0 {
					Fatalf("Bad developer-mode genesis configuration: terminalTotalDifficulty must be 0")
				}
				if genesis.Difficulty.Cmp(big.NewInt(0)) != 0 {
					Fatalf("Bad developer-mode genesis configuration: difficulty must be 0")
				}
			}
			chaindb.Close()
		}
		if !ctx.IsSet(MinerGasPriceFlag.Name) {
			cfg.Miner.GasPrice = big.NewInt(1)
		}
	default:
		if cfg.NetworkId == 1 {
			SetDNSDiscoveryDefaults(cfg, params.MainnetGenesisHash)
		}
	}
	// Set any dangling config values
	if ctx.String(CryptoKZGFlag.Name) != "gokzg" && ctx.String(CryptoKZGFlag.Name) != "ckzg" {
		Fatalf("--%s flag must be 'gokzg' or 'ckzg'", CryptoKZGFlag.Name)
	}
	log.Info("Initializing the KZG library", "backend", ctx.String(CryptoKZGFlag.Name))
	if err := kzg4844.UseCKZG(ctx.String(CryptoKZGFlag.Name) == "ckzg"); err != nil {
		Fatalf("Failed to set KZG library implementation to %s: %v", ctx.String(CryptoKZGFlag.Name), err)
	}

	// blob setting
	if ctx.IsSet(OverrideDefaultExtraReserveForBlobRequests.Name) {
		cfg.BlobExtraReserve = ctx.Uint64(OverrideDefaultExtraReserveForBlobRequests.Name)
	}
	if ctx.IsSet(BlobExtraReserveFlag.Name) {
		extraReserve := ctx.Uint64(BlobExtraReserveFlag.Name)
		if extraReserve > 0 && extraReserve < params.DefaultExtraReserveForBlobRequests {
			extraReserve = params.DefaultExtraReserveForBlobRequests
		}
		cfg.BlobExtraReserve = extraReserve
	}
	// VM tracing config.
	if ctx.IsSet(VMTraceFlag.Name) {
		if name := ctx.String(VMTraceFlag.Name); name != "" {
			cfg.VMTrace = name
			cfg.VMTraceJsonConfig = ctx.String(VMTraceJsonConfigFlag.Name)
		}
	}
}

// SetDNSDiscoveryDefaults configures DNS discovery with the given URL if
// no URLs are set.
func SetDNSDiscoveryDefaults(cfg *ethconfig.Config, genesis common.Hash) {
	if cfg.EthDiscoveryURLs != nil {
		return // already set through flags/config
	}
	protocol := "all"
	if url := params.KnownDNSNetwork(genesis, protocol); url != "" {
		cfg.EthDiscoveryURLs = []string{url}
		cfg.SnapDiscoveryURLs = cfg.EthDiscoveryURLs
		cfg.TrustDiscoveryURLs = cfg.EthDiscoveryURLs
		cfg.BscDiscoveryURLs = cfg.EthDiscoveryURLs
	}
}

// RegisterEthService adds an Ethereum client to the stack.
// The second return value is the full node instance.
func RegisterEthService(stack *node.Node, cfg *ethconfig.Config) (ethapi.Backend, *eth.Ethereum) {
	backend, err := eth.New(stack, cfg)
	if err != nil {
		Fatalf("Failed to register the Ethereum service: %v", err)
	}
	stack.RegisterAPIs(tracers.APIs(backend.APIBackend))
	return backend.APIBackend, backend
}

// RegisterEthStatsService configures the Ethereum Stats daemon and adds it to the node.
func RegisterEthStatsService(stack *node.Node, backend ethapi.Backend, url string) {
	if err := ethstats.New(stack, backend, backend.Engine(), url); err != nil {
		Fatalf("Failed to register the Ethereum Stats service: %v", err)
	}
}

// RegisterGraphQLService adds the GraphQL API to the node.
func RegisterGraphQLService(stack *node.Node, backend ethapi.Backend, filterSystem *filters.FilterSystem, cfg *node.Config) {
	err := graphql.New(stack, backend, filterSystem, cfg.GraphQLCors, cfg.GraphQLVirtualHosts)
	if err != nil {
		Fatalf("Failed to register the GraphQL service: %v", err)
	}
}

type SetupMetricsOption func()

func EnableBuildInfo(gitCommit, gitDate string) SetupMetricsOption {
	return func() {
		// register build info into metrics
		metrics.GetOrRegisterLabel("build-info", nil).Mark(map[string]interface{}{
			"version":          version.WithMeta,
			"git-commit":       gitCommit,
			"git-commit-date":  gitDate,
			"go-version":       runtime.Version(),
			"operating-system": runtime.GOOS,
			"architecture":     runtime.GOARCH,
		})
	}
}

func EnableMinerInfo(ctx *cli.Context, minerConfig *minerconfig.Config) SetupMetricsOption {
	return func() {
		if ctx.Bool(MiningEnabledFlag.Name) {
			// register miner info into metrics
			minerInfo := structs.Map(minerConfig)
			minerInfo[UnlockedAccountFlag.Name] = ctx.String(UnlockedAccountFlag.Name)
			metrics.GetOrRegisterLabel("miner-info", nil).Mark(minerInfo)
		}
	}
}

// RegisterFilterAPI adds the eth log filtering RPC API to the node.
func RegisterFilterAPI(stack *node.Node, backend ethapi.Backend, ethcfg *ethconfig.Config) *filters.FilterSystem {
	filterSystem := filters.NewFilterSystem(backend, filters.Config{
		LogCacheSize: ethcfg.FilterLogCacheSize,
	})
	stack.RegisterAPIs([]rpc.API{{
		Namespace: "eth",
		Service:   filters.NewFilterAPI(filterSystem, ethcfg.RangeLimit),
	}})
	return filterSystem
}

func EnableNodeInfo(poolConfig *legacypool.Config, nodeInfo *p2p.NodeInfo) SetupMetricsOption {
	return func() {
		// register node info into metrics
		metrics.GetOrRegisterLabel("node-info", nil).Mark(map[string]interface{}{
			"Enode":             nodeInfo.Enode,
			"ENR":               nodeInfo.ENR,
			"ID":                nodeInfo.ID,
			"PriceLimit":        poolConfig.PriceLimit,
			"PriceBump":         poolConfig.PriceBump,
			"AccountSlots":      poolConfig.AccountSlots,
			"GlobalSlots":       poolConfig.GlobalSlots,
			"AccountQueue":      poolConfig.AccountQueue,
			"GlobalQueue":       poolConfig.GlobalQueue,
			"OverflowPoolSlots": poolConfig.OverflowPoolSlots,
			"Lifetime":          poolConfig.Lifetime,
		})
	}
}

func EnableNodeTrack(ctx *cli.Context, cfg *ethconfig.Config, stack *node.Node) SetupMetricsOption {
	nodeInfo := stack.Server().NodeInfo()
	return func() {
		// register node info into metrics
		metrics.GetOrRegisterLabel("node-stats", nil).Mark(map[string]interface{}{
			"NodeType":       parseNodeType(),
			"ENR":            nodeInfo.ENR,
			"Mining":         ctx.Bool(MiningEnabledFlag.Name),
			"Etherbase":      parseEtherbase(cfg),
			"MiningFeatures": parseMiningFeatures(ctx, cfg),
			"DBFeatures":     parseDBFeatures(cfg, stack),
		})
	}
}

func parseEtherbase(cfg *ethconfig.Config) string {
	if cfg.Miner.Etherbase == (common.Address{}) {
		return ""
	}
	return cfg.Miner.Etherbase.String()
}

func parseNodeType() string {
	git, _ := version.VCS()
	version := []string{version.WithMeta}
	if len(git.Commit) >= 7 {
		version = append(version, git.Commit[:7])
	}
	if git.Date != "" {
		version = append(version, git.Date)
	}
	arch := []string{runtime.GOOS, runtime.GOARCH}
	infos := []string{"BSC", strings.Join(version, "-"), strings.Join(arch, "-"), runtime.Version()}
	return strings.Join(infos, "/")
}

func parseDBFeatures(cfg *ethconfig.Config, stack *node.Node) string {
	var features []string
	if cfg.StateScheme == rawdb.PathScheme {
		features = append(features, "PBSS")
	} else if cfg.StateScheme == rawdb.HashScheme {
		features = append(features, "HBSS")
	}
	if stack.CheckIfMultiDataBase() {
		features = append(features, "MultiDB")
	}
	if cfg.PruneAncientData {
		features = append(features, "PruneAncient")
	}
	return strings.Join(features, "|")
}

func parseMiningFeatures(ctx *cli.Context, cfg *ethconfig.Config) string {
	if !ctx.Bool(MiningEnabledFlag.Name) {
		return ""
	}
	var features []string
	if cfg.Miner.Mev.Enabled {
		features = append(features, "MEV")
	}
	if cfg.Miner.VoteEnable {
		features = append(features, "FFVoting")
	}
	return strings.Join(features, "|")
}

func SetupMetrics(cfg *metrics.Config, options ...SetupMetricsOption) {
	if !cfg.Enabled {
		return
	}
	log.Info("Enabling metrics collection")
	metrics.Enable()
	if cfg.EnabledExpensive {
		log.Info("Enabling expensive metrics collection")
		metrics.EnableExpensive()
	}

	// InfluxDB exporter.
	var (
		enableExport   = cfg.EnableInfluxDB
		enableExportV2 = cfg.EnableInfluxDBV2
	)
	if cfg.EnableInfluxDB && cfg.EnableInfluxDBV2 {
		Fatalf("Flags %v can't be used at the same time", strings.Join([]string{MetricsEnableInfluxDBFlag.Name, MetricsEnableInfluxDBV2Flag.Name}, ", "))
	}
	var (
		endpoint = cfg.InfluxDBEndpoint
		database = cfg.InfluxDBDatabase
		username = cfg.InfluxDBUsername
		password = cfg.InfluxDBPassword

		token        = cfg.InfluxDBToken
		bucket       = cfg.InfluxDBBucket
		organization = cfg.InfluxDBOrganization
		tagsMap      = SplitTagsFlag(cfg.InfluxDBTags)
	)
	if enableExport {
		log.Info("Enabling metrics export to InfluxDB")
		go influxdb.InfluxDBWithTags(metrics.DefaultRegistry, 10*time.Second, endpoint, database, username, password, "geth.", tagsMap)
	} else if enableExportV2 {
		tagsMap := SplitTagsFlag(cfg.InfluxDBTags)
		log.Info("Enabling metrics export to InfluxDB (v2)")
		go influxdb.InfluxDBV2WithTags(metrics.DefaultRegistry, 10*time.Second, endpoint, token, bucket, organization, "geth.", tagsMap)
	}

	// Expvar exporter.
	if cfg.HTTP != "" {
		address := net.JoinHostPort(cfg.HTTP, fmt.Sprintf("%d", cfg.Port))
		log.Info("Enabling stand-alone metrics HTTP endpoint", "address", address)
		exp.Setup(address)
	} else if cfg.HTTP == "" && cfg.Port != 0 {
		log.Warn(fmt.Sprintf("--%s specified without --%s, metrics server will not start.", MetricsPortFlag.Name, MetricsHTTPFlag.Name))
	}

	for _, opt := range options {
		opt()
	}

	// Enable system metrics collection.
	go metrics.CollectProcessMetrics(3 * time.Second)
}

// SplitTagsFlag parses a comma-separated list of k=v metrics tags.
func SplitTagsFlag(tagsFlag string) map[string]string {
	tags := strings.Split(tagsFlag, ",")
	tagsMap := map[string]string{}

	for _, t := range tags {
		if t != "" {
			kv := strings.Split(t, "=")

			if len(kv) == 2 {
				tagsMap[kv[0]] = kv[1]
			}
		}
	}

	return tagsMap
}

// MakeChainDatabase opens a database using the flags passed to the client and will hard crash if it fails.
func MakeChainDatabase(ctx *cli.Context, stack *node.Node, readonly, disableFreeze bool) ethdb.Database {
	var (
		cache   = ctx.Int(CacheFlag.Name) * ctx.Int(CacheDatabaseFlag.Name) / 100
		handles = MakeDatabaseHandles(ctx.Int(FDLimitFlag.Name))
		err     error
		chainDb ethdb.Database
	)
	switch {
	case ctx.IsSet(RemoteDBFlag.Name):
		log.Info("Using remote db", "url", ctx.String(RemoteDBFlag.Name), "headers", len(ctx.StringSlice(HttpHeaderFlag.Name)))
		client, err := DialRPCWithHeaders(ctx.String(RemoteDBFlag.Name), ctx.StringSlice(HttpHeaderFlag.Name))
		if err != nil {
			break
		}
		chainDb = remotedb.New(client)
	default:
		chainDb, err = stack.OpenDatabaseWithFreezer("chaindata", cache, handles, ctx.String(AncientFlag.Name), "", readonly, disableFreeze, false, false)
		// set the separate state database
		if stack.CheckIfMultiDataBase() && err == nil {
			stateDiskDb := MakeStateDataBase(ctx, stack, readonly, false)
			chainDb.SetStateStore(stateDiskDb)
			blockDb := MakeBlockDatabase(ctx, stack, readonly, false)
			chainDb.SetBlockStore(blockDb)
		}
	}
	if err != nil {
		Fatalf("Could not open database: %v", err)
	}
	return chainDb
}

// MakeStateDataBase open a separate state database using the flags passed to the client and will hard crash if it fails.
func MakeStateDataBase(ctx *cli.Context, stack *node.Node, readonly, disableFreeze bool) ethdb.Database {
	cache := ctx.Int(CacheFlag.Name) * ctx.Int(CacheDatabaseFlag.Name) / 100
	handles := MakeDatabaseHandles(ctx.Int(FDLimitFlag.Name)) * 90 / 100
	statediskdb, err := stack.OpenDatabaseWithFreezer("chaindata/state", cache, handles, "", "", readonly, disableFreeze, false, false)
	if err != nil {
		Fatalf("Failed to open separate trie database: %v", err)
	}
	return statediskdb
}

// MakeBlockDatabase open a separate block database using the flags passed to the client and will hard crash if it fails.
func MakeBlockDatabase(ctx *cli.Context, stack *node.Node, readonly, disableFreeze bool) ethdb.Database {
	cache := ctx.Int(CacheFlag.Name) * ctx.Int(CacheDatabaseFlag.Name) / 100
	handles := MakeDatabaseHandles(ctx.Int(FDLimitFlag.Name)) / 10
	blockDb, err := stack.OpenDatabaseWithFreezer("chaindata/block", cache, handles, "", "", readonly, disableFreeze, false, false)
	if err != nil {
		Fatalf("Failed to open separate block database: %v", err)
	}
	return blockDb
}

func PathDBConfigAddJournalFilePath(stack *node.Node, config *pathdb.Config) *pathdb.Config {
	path := fmt.Sprintf("%s/%s", stack.ResolvePath("chaindata"), eth.JournalFileName)
	config.JournalFilePath = path
	return config
}

// tryMakeReadOnlyDatabase try to open the chain database in read-only mode,
// or fallback to write mode if the database is not initialized.
//
//nolint:unused
func tryMakeReadOnlyDatabase(ctx *cli.Context, stack *node.Node) ethdb.Database {
	// If datadir doesn't exist we need to open db in write-mode
	// so database engine can create files.
	readonly := true
	if !common.FileExist(stack.ResolvePath("chaindata")) || ctx.Bool(PruneAncientDataFlag.Name) {
		readonly = false
	}
	return MakeChainDatabase(ctx, stack, readonly, false)
}

func IsNetworkPreset(ctx *cli.Context) bool {
	for _, flag := range NetworkFlags {
		bFlag, _ := flag.(*cli.BoolFlag)
		if ctx.IsSet(bFlag.Name) {
			return true
		}
	}
	return false
}

func DialRPCWithHeaders(endpoint string, headers []string) (*rpc.Client, error) {
	if endpoint == "" {
		return nil, errors.New("endpoint must be specified")
	}
	if strings.HasPrefix(endpoint, "rpc:") || strings.HasPrefix(endpoint, "ipc:") {
		// Backwards compatibility with geth < 1.5 which required
		// these prefixes.
		endpoint = endpoint[4:]
	}
	var opts []rpc.ClientOption
	if len(headers) > 0 {
		customHeaders := make(http.Header)
		for _, h := range headers {
			kv := strings.Split(h, ":")
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid http header directive: %q", h)
			}
			customHeaders.Add(kv[0], kv[1])
		}
		opts = append(opts, rpc.WithHeaders(customHeaders))
	}
	return rpc.DialOptions(context.Background(), endpoint, opts...)
}

func MakeGenesis(ctx *cli.Context) *core.Genesis {
	var genesis *core.Genesis
	switch {
	case ctx.Bool(BSCMainnetFlag.Name):
		genesis = core.DefaultBSCGenesisBlock()
	case ctx.Bool(ChapelFlag.Name):
		genesis = core.DefaultChapelGenesisBlock()
	case ctx.Bool(DeveloperFlag.Name):
		Fatalf("Developer chains are ephemeral")
	}
	return genesis
}

// MakeChain creates a chain manager from set command line flags.
func MakeChain(ctx *cli.Context, stack *node.Node, readonly bool) (*core.BlockChain, ethdb.Database) {
	var (
		gspec   = MakeGenesis(ctx)
		chainDb = MakeChainDatabase(ctx, stack, readonly, false)
	)
	config, genesisHash, err := core.LoadChainConfig(chainDb, gspec)
	if err != nil {
		Fatalf("%v", err)
	}
	engine, err := ethconfig.CreateConsensusEngine(config, chainDb, nil, genesisHash)
	if err != nil {
		Fatalf("%v", err)
	}
	if gcmode := ctx.String(GCModeFlag.Name); gcmode != "full" && gcmode != "archive" {
		Fatalf("--%s must be either 'full' or 'archive'", GCModeFlag.Name)
	}
	scheme, err := rawdb.ParseStateScheme(ctx.String(StateSchemeFlag.Name), chainDb)
	if err != nil {
		Fatalf("%v", err)
	}
	cache := &core.CacheConfig{
		TrieCleanLimit:      ethconfig.Defaults.TrieCleanCache,
		TrieCleanNoPrefetch: ctx.Bool(CacheNoPrefetchFlag.Name),
		TrieDirtyLimit:      ethconfig.Defaults.TrieDirtyCache,
		TrieDirtyDisabled:   ctx.String(GCModeFlag.Name) == "archive",
		TrieTimeLimit:       ethconfig.Defaults.TrieTimeout,
		TriesInMemory:       ethconfig.Defaults.TriesInMemory,
		SnapshotLimit:       ethconfig.Defaults.SnapshotCache,
		Preimages:           ctx.Bool(CachePreimagesFlag.Name),
		StateScheme:         scheme,
		StateHistory:        ctx.Uint64(StateHistoryFlag.Name),
	}
	if cache.TrieDirtyDisabled && !cache.Preimages {
		cache.Preimages = true
		log.Info("Enabling recording of key preimages since archive mode is used")
	}
	if !ctx.Bool(SnapshotFlag.Name) {
		cache.SnapshotLimit = 0 // Disabled
	}
	// If we're in readonly, do not bother generating snapshot data.
	if readonly {
		cache.SnapshotNoBuild = true
	}

	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheTrieFlag.Name) {
		cache.TrieCleanLimit = ctx.Int(CacheFlag.Name) * ctx.Int(CacheTrieFlag.Name) / 100
	}
	if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheGCFlag.Name) {
		cache.TrieDirtyLimit = ctx.Int(CacheFlag.Name) * ctx.Int(CacheGCFlag.Name) / 100
	}
	if ctx.IsSet(TriesInMemoryFlag.Name) {
		cache.TriesInMemory = ctx.Uint64(TriesInMemoryFlag.Name)
	}
	vmcfg := vm.Config{
		EnablePreimageRecording: ctx.Bool(VMEnableDebugFlag.Name),
	}
	if ctx.IsSet(VMTraceFlag.Name) {
		if name := ctx.String(VMTraceFlag.Name); name != "" {
			config := json.RawMessage(ctx.String(VMTraceJsonConfigFlag.Name))
			t, err := tracers.LiveDirectory.New(name, config)
			if err != nil {
				Fatalf("Failed to create tracer %q: %v", name, err)
			}
			vmcfg.Tracer = t
		}
	}
	// Disable transaction indexing/unindexing by default.
	chain, err := core.NewBlockChain(chainDb, cache, gspec, nil, engine, vmcfg, nil, nil)
	if err != nil {
		Fatalf("Can't create BlockChain: %v", err)
	}

	return chain, chainDb
}

// MakeConsolePreloads retrieves the absolute paths for the console JavaScript
// scripts to preload before starting.
func MakeConsolePreloads(ctx *cli.Context) []string {
	// Skip preloading if there's nothing to preload
	if ctx.String(PreloadJSFlag.Name) == "" {
		return nil
	}
	// Otherwise resolve absolute paths and return them
	var preloads []string

	for _, file := range strings.Split(ctx.String(PreloadJSFlag.Name), ",") {
		preloads = append(preloads, strings.TrimSpace(file))
	}
	return preloads
}

// MakeTrieDatabase constructs a trie database based on the configured scheme.
func MakeTrieDatabase(ctx *cli.Context, stack *node.Node, disk ethdb.Database, preimage bool, readOnly bool, isVerkle bool) *triedb.Database {
	config := &triedb.Config{
		Preimages: preimage,
		IsVerkle:  isVerkle,
	}
	scheme, err := rawdb.ParseStateScheme(ctx.String(StateSchemeFlag.Name), disk)
	if err != nil {
		Fatalf("%v", err)
	}
	if scheme == rawdb.HashScheme {
		// Read-only mode is not implemented in hash mode,
		// ignore the parameter silently. TODO(rjl493456442)
		// please config it if read mode is implemented.
		config.HashDB = hashdb.Defaults
		return triedb.NewDatabase(disk, config)
	}
	if readOnly {
		config.PathDB = pathdb.ReadOnly
	} else {
		config.PathDB = pathdb.Defaults
	}
	config.PathDB.JournalFilePath = fmt.Sprintf("%s/%s", stack.ResolvePath("chaindata"), eth.JournalFileName)
	return triedb.NewDatabase(disk, config)
}

// ParseCLIAndConfigStateScheme parses state scheme in CLI and config.
func ParseCLIAndConfigStateScheme(cliScheme, cfgScheme string) (string, error) {
	if cliScheme == "" {
		if cfgScheme != "" {
			log.Info("Use config state scheme", "config", cfgScheme)
		}
		return cfgScheme, nil
	}

	if !rawdb.ValidateStateScheme(cliScheme) {
		return "", fmt.Errorf("invalid state scheme in CLI: %s", cliScheme)
	}
	if cfgScheme == "" || cliScheme == cfgScheme {
		log.Info("Use CLI state scheme", "CLI", cliScheme)
		return cliScheme, nil
	}
	return "", fmt.Errorf("incompatible state scheme, CLI: %s, config: %s", cliScheme, cfgScheme)
}

// setInstance configures the port numbers for the given instance.
func setInstance(ctx *cli.Context, cfg *node.Config) {
	if ctx.IsSet(InstanceFlag.Name) {
		cfg.Instance = ctx.Int(InstanceFlag.Name)
	}

	if cfg.Instance > 200 {
		Fatalf("Instance number %d is too high, maximum is 200", cfg.Instance)
	}

	if cfg.Instance == 1 { // using default ports
		return
	}

	cfg.AuthPort = node.DefaultConfig.AuthPort + cfg.Instance*100 - 100
	cfg.HTTPPort = node.DefaultHTTPPort - cfg.Instance + 1
	cfg.WSPort = node.DefaultWSPort + cfg.Instance*2 - 2
	cfg.P2P.ListenAddr = fmt.Sprintf(":%d", node.DefaultListenPort+cfg.Instance-1)
	cfg.P2P.DiscAddr = fmt.Sprintf(":%d", node.DefaultDiscPort+cfg.Instance-1)
}
