// Copyright 2021 The go-ethereum Authors
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

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/pruner"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/prometheus/tsdb/fileutil"
	"github.com/urfave/cli/v2"
)

var (
	snapshotCommand = &cli.Command{
		Name:        "snapshot",
		Usage:       "A set of commands based on the snapshot",
		Description: "",
		Subcommands: []*cli.Command{
			{
				Name:      "prune-state",
				Usage:     "Prune stale ethereum state data based on the snapshot",
				ArgsUsage: "<root>",
				Action:    pruneState,
				Flags: slices.Concat([]cli.Flag{
					utils.BloomFilterSizeFlag,
					utils.TriesInMemoryFlag,
				}, utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot prune-state <state-root>
will prune historical state data with the help of the state snapshot.
All trie nodes and contract codes that do not belong to the specified
version state will be deleted from the database. After pruning, only
two version states are available: genesis and the specific one.

The default pruning target is the HEAD-127 state.

WARNING: it's only supported in hash mode(--state.scheme=hash)".
`,
			},
			{
				Name:     "prune-block",
				Usage:    "Prune block data offline",
				Action:   pruneBlock,
				Category: "MISCELLANEOUS COMMANDS",
				Flags: []cli.Flag{
					utils.DataDirFlag,
					utils.AncientFlag,
					utils.BlockAmountReserved,
					utils.TriesInMemoryFlag,
					utils.CheckSnapshotWithMPT,
				},
				Description: `
geth offline prune-block for block data in ancientdb.
The amount of blocks expected for remaining after prune can be specified via block-amount-reserved in this command,
will prune and only remain the specified amount of old block data in ancientdb.
the brief workflow is to backup the number of this specified amount blocks backward in original ancientdb
into new ancient_backup, then delete the original ancientdb dir and rename the ancient_backup to original one for replacement,
finally assemble the statedb and new ancientDb together.
The purpose of doing it is because the block data will be moved into the ancient store when it
becomes old enough(exceed the Threshold 90000), the disk usage will be very large over time, and is occupied mainly by ancientDb,
so it's very necessary to do block data prune, this feature will handle it.
`,
			},
			{
				Name:      "verify-state",
				Usage:     "Recalculate state hash based on the snapshot for verification",
				ArgsUsage: "<root>",
				Action:    verifyState,
				Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot verify-state <state-root>
will traverse the whole accounts and storages set based on the specified
snapshot and recalculate the root hash of state for verification.
In other words, this command does the snapshot to trie conversion.
`,
			},
			{
				Name: "insecure-prune-all",
				Usage: "Prune all trie state data except genesis block, it will break storage for fullnode, only suitable for fast node " +
					"who do not need trie storage at all",
				ArgsUsage: "<genesisPath>",
				Action:    pruneAllState,
				Category:  "MISCELLANEOUS COMMANDS",
				Flags: []cli.Flag{
					utils.DataDirFlag,
					utils.AncientFlag,
				},
				Description: `
will prune all historical trie state data except genesis block.
All trie nodes will be deleted from the database. 

It expects the genesis file as argument.

WARNING: It's necessary to delete the trie clean cache after the pruning.
If you specify another directory for the trie clean cache via "--cache.trie.journal"
during the use of Geth, please also specify it here for correct deletion. Otherwise
the trie clean cache with default directory will be deleted.
`,
			},
			{
				Name:      "check-dangling-storage",
				Usage:     "Check that there is no 'dangling' snap storage",
				ArgsUsage: "<root>",
				Action:    checkDanglingStorage,
				Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot check-dangling-storage <state-root> traverses the snap storage
data, and verifies that all snapshot storage data has a corresponding account.
`,
			},
			{
				Name:      "inspect-account",
				Usage:     "Check all snapshot layers for the specific account",
				ArgsUsage: "<address | hash>",
				Action:    checkAccount,
				Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot inspect-account <address | hash> checks all snapshot layers and prints out
information about the specified address.
`,
			},
			{
				Name:      "traverse-state",
				Usage:     "Traverse the state with given root hash and perform quick verification",
				ArgsUsage: "<root>",
				Action:    traverseState,
				Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot traverse-state <state-root>
will traverse the whole state from the given state root and will abort if any
referenced trie node or contract code is missing. This command can be used for
state integrity verification. The default checking target is the HEAD state.

It's also usable without snapshot enabled.
`,
			},
			{
				Name:      "traverse-rawstate",
				Usage:     "Traverse the state with given root hash and perform detailed verification",
				ArgsUsage: "<root>",
				Action:    traverseRawState,
				Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
geth snapshot traverse-rawstate <state-root>
will traverse the whole state from the given root and will abort if any referenced
trie node or contract code is missing. This command can be used for state integrity
verification. The default checking target is the HEAD state. It's basically identical
to traverse-state, but the check granularity is smaller.

It's also usable without snapshot enabled.
`,
			},
			{
				Name:      "dump",
				Usage:     "Dump a specific block from storage (same as 'geth dump' but using snapshots)",
				ArgsUsage: "[? <blockHash> | <blockNum>]",
				Action:    dumpState,
				Flags: slices.Concat([]cli.Flag{
					utils.ExcludeCodeFlag,
					utils.ExcludeStorageFlag,
					utils.StartKeyFlag,
					utils.DumpLimitFlag,
					utils.TriesInMemoryFlag,
				}, utils.NetworkFlags, utils.DatabaseFlags),
				Description: `
This command is semantically equivalent to 'geth dump', but uses the snapshots
as the backend data source, making this command a lot faster.

The argument is interpreted as block number or hash. If none is provided, the latest
block is used.
`,
			},
			{
				Action:    snapshotExportPreimages,
				Name:      "export-preimages",
				Usage:     "Export the preimage in snapshot enumeration order",
				ArgsUsage: "<dumpfile> [<root>]",
				Flags:     utils.DatabaseFlags,
				Description: `
The export-preimages command exports hash preimages to a flat file, in exactly
the expected order for the overlay tree migration.
`,
			},
			{
				Action:    mergeIncrSnapshot,
				Name:      "merge-incr-snapshot",
				Usage:     "Merge the incremental snapshot into base snapshot",
				ArgsUsage: "",
				Flags: slices.Concat([]cli.Flag{utils.IncrementalSnapshotPathFlag},
					utils.DatabaseFlags),
				Description: `This command aims to help merge multiple incremental snapshot into base snapshot`,
			},
			{
				Action:    compareBlockAndStateID,
				Name:      "compare-state-block",
				Usage:     "Compare two snapshots block and state id",
				ArgsUsage: "",
				Flags: slices.Concat([]cli.Flag{utils.IncrementalSnapshotPathFlag, utils.IncrementalSnapshotFlag},
					utils.DatabaseFlags),
				Description: `This command is used to debug incremental snapshots issues`,
			},
			{
				Action:    forceFlushToAncient,
				Name:      "force-flush-ancient",
				Usage:     "Force flush block data in pebble into ancient db",
				ArgsUsage: "",
				Flags: slices.Concat([]cli.Flag{utils.IncrementalSnapshotPathFlag, utils.IncrementalSnapshotFlag},
					utils.DatabaseFlags),
				Description: `This command is used to debug incremental snapshots issues`,
			},
		},
	}
)

func accessDb(ctx *cli.Context, stack *node.Node) (ethdb.Database, error) {
	// The layer of tries trees that keep in memory.
	TriesInMemory := int(ctx.Uint64(utils.TriesInMemoryFlag.Name))
	chaindb := utils.MakeChainDatabase(ctx, stack, false, true)
	defer chaindb.Close()

	if !ctx.Bool(utils.CheckSnapshotWithMPT.Name) {
		return chaindb, nil
	}
	headBlock := rawdb.ReadHeadBlock(chaindb)
	if headBlock == nil {
		return nil, errors.New("failed to load head block")
	}
	headHeader := headBlock.Header()
	// Make sure the MPT and snapshot matches before pruning, otherwise the node can not start.
	snapconfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	dbScheme := rawdb.ReadStateScheme(chaindb)
	var config *triedb.Config
	if dbScheme == rawdb.PathScheme {
		config = &triedb.Config{
			PathDB: utils.PathDBConfigAddJournalFilePath(stack, pathdb.ReadOnly),
		}
	} else if dbScheme == rawdb.HashScheme {
		config = triedb.HashDefaults
	}
	snaptree, err := snapshot.New(snapconfig, chaindb, triedb.NewDatabase(chaindb, config), headBlock.Root(), TriesInMemory, false)
	if err != nil {
		log.Error("snaptree error", "err", err)
		return nil, err // The relevant snapshot(s) might not exist
	}

	// Use the HEAD-(n-1) as the target root. The reason for picking it is:
	// - in most of the normal cases, the related state is available
	// - the probability of this layer being reorg is very low

	// Retrieve all snapshot layers from the current HEAD.
	// In theory there are n difflayers + 1 disk layer present,
	// so n diff layers are expected to be returned.
	layers := snaptree.Snapshots(headHeader.Root, TriesInMemory, true)
	if len(layers) != TriesInMemory {
		// Reject if the accumulated diff layers are less than n. It
		// means in most of normal cases, there is no associated state
		// with bottom-most diff layer.
		log.Error("snapshot layers != TriesInMemory", "err", err)
		return nil, fmt.Errorf("snapshot not old enough yet: need %d more blocks", TriesInMemory-len(layers))
	}
	// Use the bottom-most diff layer as the target
	targetRoot := layers[len(layers)-1].Root()

	// Ensure the root is really present. The weak assumption
	// is the presence of root can indicate the presence of the
	// entire trie.
	if blob := rawdb.ReadLegacyTrieNode(chaindb, targetRoot); len(blob) == 0 {
		// The special case is for clique based networks(rinkeby, goerli
		// and some other private networks), it's possible that two
		// consecutive blocks will have same root. In this case snapshot
		// difflayer won't be created. So HEAD-(n-1) may not paired with
		// head-(n-1) layer. Instead the paired layer is higher than the
		// bottom-most diff layer. Try to find the bottom-most snapshot
		// layer with state available.
		//
		// Note HEAD is ignored. Usually there is the associated
		// state available, but we don't want to use the topmost state
		// as the pruning target.
		var found bool
		for i := len(layers) - 2; i >= 1; i-- {
			if blob := rawdb.ReadLegacyTrieNode(chaindb, layers[i].Root()); len(blob) != 0 {
				targetRoot = layers[i].Root()
				found = true
				log.Info("Selecting middle-layer as the pruning target", "root", targetRoot, "depth", i)
				break
			}
		}
		if !found {
			if blob := rawdb.ReadLegacyTrieNode(chaindb, snaptree.DiskRoot()); len(blob) != 0 {
				targetRoot = snaptree.DiskRoot()
				found = true
				log.Info("Selecting disk-layer as the pruning target", "root", targetRoot)
			}
		}
		if !found {
			if len(layers) > 0 {
				log.Error("no snapshot paired state")
				return nil, errors.New("no snapshot paired state")
			}
			return nil, fmt.Errorf("associated state[%x] is not present", targetRoot)
		}
	} else {
		if len(layers) > 0 {
			log.Info("Selecting bottom-most difflayer as the pruning target", "root", targetRoot, "height", headHeader.Number.Uint64()-uint64(len(layers)-1))
		} else {
			log.Info("Selecting user-specified state as the pruning target", "root", targetRoot)
		}
	}
	return chaindb, nil
}

func pruneBlock(ctx *cli.Context) error {
	var (
		stack   *node.Node
		config  gethConfig
		chaindb ethdb.Database
		err     error

		oldAncientPath      string
		newAncientPath      string
		blockAmountReserved uint64
		blockpruner         *pruner.BlockPruner
	)

	stack, config = makeConfigNode(ctx)
	defer stack.Close()
	blockAmountReserved = ctx.Uint64(utils.BlockAmountReserved.Name)
	if blockAmountReserved < params.FullImmutabilityThreshold {
		return fmt.Errorf("block-amount-reserved must be greater than or equal to %d", params.FullImmutabilityThreshold)
	}
	chaindb, err = accessDb(ctx, stack)
	if err != nil {
		return err
	}

	// Most of the problems reported by users when first using the prune-block
	// tool are due to incorrect directory settings.Here, the default directory
	// and relative directory are canceled, and the user is forced to formulate
	// an absolute path to guide users to run the prune-block command correctly.
	if !ctx.IsSet(utils.DataDirFlag.Name) {
		return errors.New("datadir must be set")
	} else {
		datadir := ctx.String(utils.DataDirFlag.Name)
		if !filepath.IsAbs(datadir) {
			// force absolute paths, which often fail due to the splicing of relative paths
			return errors.New("datadir not abs path")
		}
	}

	if !ctx.IsSet(utils.AncientFlag.Name) {
		return errors.New("datadir.ancient must be set")
	} else {
		if stack.CheckIfMultiDataBase() {
			ancientPath := ctx.String(utils.AncientFlag.Name)
			index := strings.LastIndex(ancientPath, "/ancient/chain")
			if index != -1 {
				oldAncientPath = ancientPath[:index] + "/block/ancient/chain"
			}
		} else {
			oldAncientPath = ctx.String(utils.AncientFlag.Name)
		}
		if !filepath.IsAbs(oldAncientPath) {
			// force absolute paths, which often fail due to the splicing of relative paths
			return errors.New("datadir.ancient not abs path")
		}
	}

	path, _ := filepath.Split(oldAncientPath)
	if path == "" {
		return errors.New("prune failed, did not specify the AncientPath")
	}
	newVersionPath := false
	files, err := os.ReadDir(oldAncientPath)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.IsDir() && file.Name() == "chain" {
			newVersionPath = true
		}
	}
	if newVersionPath && !strings.HasSuffix(oldAncientPath, "geth/chaindata/ancient/chain") {
		log.Error("datadir.ancient subdirectory incorrect", "got path", oldAncientPath, "want subdirectory", "geth/chaindata/ancient/chain/")
		return errors.New("datadir.ancient subdirectory incorrect")
	}
	newAncientPath = filepath.Join(path, "chain_back")

	blockpruner = pruner.NewBlockPruner(chaindb, stack, oldAncientPath, newAncientPath, blockAmountReserved)

	lock, exist, err := fileutil.Flock(filepath.Join(oldAncientPath, "PRUNEFLOCK"))
	if err != nil {
		log.Error("file lock error", "err", err)
		return err
	}
	if exist {
		defer lock.Release()
		log.Info("file lock existed, waiting for prune recovery and continue", "err", err)
		if err := blockpruner.RecoverInterruption("chaindata", config.Eth.DatabaseCache, utils.MakeDatabaseHandles(0), "", false); err != nil {
			log.Error("Pruning failed", "err", err)
			return err
		}
		log.Info("Block prune successfully")
		return nil
	}

	if _, err := os.Stat(newAncientPath); err == nil {
		// No file lock found for old ancientDB but new ancientDB existed, indicating the geth was interrupted
		// after old ancientDB removal, this happened after backup successfully, so just rename the new ancientDB
		if err := blockpruner.AncientDbReplacer(); err != nil {
			log.Error("Failed to rename new ancient directory")
			return err
		}
		log.Info("Block prune successfully")
		return nil
	}
	name := "chaindata"
	if err := blockpruner.BlockPruneBackUp(name, config.Eth.DatabaseCache, utils.MakeDatabaseHandles(0), "", false, false); err != nil {
		log.Error("Failed to back up block", "err", err)
		return err
	}

	log.Info("backup block successfully")

	// After backing up successfully, rename the new ancientdb name to the original one, and delete the old ancientdb
	if err := blockpruner.AncientDbReplacer(); err != nil {
		return err
	}

	lock.Release()
	log.Info("Block prune successfully")
	return nil
}

// Deprecation: this command should be deprecated once the hash-based
// scheme is deprecated.
func pruneState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, false, false)
	defer chaindb.Close()

	prunerconfig := pruner.Config{
		Datadir:   stack.ResolvePath(""),
		BloomSize: ctx.Uint64(utils.BloomFilterSizeFlag.Name),
	}

	if rawdb.ReadStateScheme(chaindb) != rawdb.HashScheme {
		log.Crit("Offline pruning is not required for path scheme")
	}

	pruner, err := pruner.NewPruner(chaindb, prunerconfig, ctx.Uint64(utils.TriesInMemoryFlag.Name))
	if err != nil {
		log.Error("Failed to open snapshot tree", "err", err)
		return err
	}
	if ctx.NArg() > 1 {
		log.Error("Too many arguments given")
		return errors.New("too many arguments")
	}
	var targetRoot common.Hash
	if ctx.NArg() == 1 {
		targetRoot, err = parseRoot(ctx.Args().First())
		if err != nil {
			log.Error("Failed to resolve state root", "err", err)
			return err
		}
	}
	if err = pruner.Prune(targetRoot); err != nil {
		log.Error("Failed to prune state", "err", err)
		return err
	}
	return nil
}

func pruneAllState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	genesisPath := ctx.Args().First()
	if len(genesisPath) == 0 {
		utils.Fatalf("Must supply path to genesis JSON file")
	}
	file, err := os.Open(genesisPath)
	if err != nil {
		utils.Fatalf("Failed to read genesis file: %v", err)
	}
	defer file.Close()

	g := new(core.Genesis)
	if err := json.NewDecoder(file).Decode(g); err != nil {
		cfg := gethConfig{
			Eth:     ethconfig.Defaults,
			Node:    defaultNodeConfig(),
			Metrics: metrics.DefaultConfig,
		}

		// Load config file.
		if err := loadConfig(genesisPath, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
		g = cfg.Eth.Genesis
	}

	chaindb := utils.MakeChainDatabase(ctx, stack, false, false)
	defer chaindb.Close()
	pruner, err := pruner.NewAllPruner(chaindb)
	if err != nil {
		log.Error("Failed to open snapshot tree", "err", err)
		return err
	}
	if err = pruner.PruneAll(g); err != nil {
		log.Error("Failed to prune state", "err", err)
		return err
	}
	return nil
}

func verifyState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()
	headBlock := rawdb.ReadHeadBlock(chaindb)
	if headBlock == nil {
		log.Error("Failed to load head block")
		return errors.New("no head block")
	}
	triedb := utils.MakeTrieDatabase(ctx, stack, chaindb, false, true, false)
	defer triedb.Close()

	snapConfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	snaptree, err := snapshot.New(snapConfig, chaindb, triedb, headBlock.Root(), 128, false)
	if err != nil {
		log.Error("Failed to open snapshot tree", "err", err)
		return err
	}
	if ctx.NArg() > 1 {
		log.Error("Too many arguments given")
		return errors.New("too many arguments")
	}
	var root = headBlock.Root()
	if ctx.NArg() == 1 {
		root, err = parseRoot(ctx.Args().First())
		if err != nil {
			log.Error("Failed to resolve state root", "err", err)
			return err
		}
	}
	if err := snaptree.Verify(root); err != nil {
		log.Error("Failed to verify state", "root", root, "err", err)
		return err
	}
	log.Info("Verified the state", "root", root)
	return snapshot.CheckDanglingStorage(chaindb)
}

// checkDanglingStorage iterates the snap storage data, and verifies that all
// storage also has corresponding account data.
func checkDanglingStorage(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	return snapshot.CheckDanglingStorage(db)
}

// traverseState is a helper function used for pruning verification.
// Basically it just iterates the trie, ensure all nodes and associated
// contract codes are present.
func traverseState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()

	triedb := utils.MakeTrieDatabase(ctx, stack, chaindb, false, true, false)
	defer triedb.Close()

	headBlock := rawdb.ReadHeadBlock(chaindb)
	if headBlock == nil {
		log.Error("Failed to load head block")
		return errors.New("no head block")
	}
	if ctx.NArg() > 1 {
		log.Error("Too many arguments given")
		return errors.New("too many arguments")
	}
	var (
		root common.Hash
		err  error
	)
	if ctx.NArg() == 1 {
		root, err = parseRoot(ctx.Args().First())
		if err != nil {
			log.Error("Failed to resolve state root", "err", err)
			return err
		}
		log.Info("Start traversing the state", "root", root)
	} else {
		root = headBlock.Root()
		log.Info("Start traversing the state", "root", root, "number", headBlock.NumberU64())
	}
	t, err := trie.NewStateTrie(trie.StateTrieID(root), triedb)
	if err != nil {
		log.Error("Failed to open trie", "root", root, "err", err)
		return err
	}
	var (
		accounts   int
		slots      int
		codes      int
		lastReport time.Time
		start      = time.Now()
	)
	acctIt, err := t.NodeIterator(nil)
	if err != nil {
		log.Error("Failed to open iterator", "root", root, "err", err)
		return err
	}
	accIter := trie.NewIterator(acctIt)
	for accIter.Next() {
		accounts += 1
		var acc types.StateAccount
		if err := rlp.DecodeBytes(accIter.Value, &acc); err != nil {
			log.Error("Invalid account encountered during traversal", "err", err)
			return err
		}
		if acc.Root != types.EmptyRootHash {
			id := trie.StorageTrieID(root, common.BytesToHash(accIter.Key), acc.Root)
			storageTrie, err := trie.NewStateTrie(id, triedb)
			if err != nil {
				log.Error("Failed to open storage trie", "root", acc.Root, "err", err)
				return err
			}
			storageIt, err := storageTrie.NodeIterator(nil)
			if err != nil {
				log.Error("Failed to open storage iterator", "root", acc.Root, "err", err)
				return err
			}
			storageIter := trie.NewIterator(storageIt)
			for storageIter.Next() {
				slots += 1

				if time.Since(lastReport) > time.Second*8 {
					log.Info("Traversing state", "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
					lastReport = time.Now()
				}
			}
			if storageIter.Err != nil {
				log.Error("Failed to traverse storage trie", "root", acc.Root, "err", storageIter.Err)
				return storageIter.Err
			}
		}
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash.Bytes()) {
			if !rawdb.HasCode(chaindb, common.BytesToHash(acc.CodeHash)) {
				log.Error("Code is missing", "hash", common.BytesToHash(acc.CodeHash))
				return errors.New("missing code")
			}
			codes += 1
		}
		if time.Since(lastReport) > time.Second*8 {
			log.Info("Traversing state", "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
			lastReport = time.Now()
		}
	}
	if accIter.Err != nil {
		log.Error("Failed to traverse state trie", "root", root, "err", accIter.Err)
		return accIter.Err
	}
	log.Info("State is complete", "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// traverseRawState is a helper function used for pruning verification.
// Basically it just iterates the trie, ensure all nodes and associated
// contract codes are present. It's basically identical to traverseState
// but it will check each trie node.
func traverseRawState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()

	triedb := utils.MakeTrieDatabase(ctx, stack, chaindb, false, true, false)
	defer triedb.Close()

	headBlock := rawdb.ReadHeadBlock(chaindb)
	if headBlock == nil {
		log.Error("Failed to load head block")
		return errors.New("no head block")
	}
	if ctx.NArg() > 1 {
		log.Error("Too many arguments given")
		return errors.New("too many arguments")
	}
	var (
		root common.Hash
		err  error
	)
	if ctx.NArg() == 1 {
		root, err = parseRoot(ctx.Args().First())
		if err != nil {
			log.Error("Failed to resolve state root", "err", err)
			return err
		}
		log.Info("Start traversing the state", "root", root)
	} else {
		root = headBlock.Root()
		log.Info("Start traversing the state", "root", root, "number", headBlock.NumberU64())
	}
	t, err := trie.NewStateTrie(trie.StateTrieID(root), triedb)
	if err != nil {
		log.Error("Failed to open trie", "root", root, "err", err)
		return err
	}
	var (
		nodes      int
		accounts   int
		slots      int
		codes      int
		lastReport time.Time
		start      = time.Now()
		hasher     = crypto.NewKeccakState()
		got        = make([]byte, 32)
	)
	accIter, err := t.NodeIterator(nil)
	if err != nil {
		log.Error("Failed to open iterator", "root", root, "err", err)
		return err
	}
	reader, err := triedb.NodeReader(root)
	if err != nil {
		log.Error("State is non-existent", "root", root)
		return nil
	}
	for accIter.Next(true) {
		nodes += 1
		node := accIter.Hash()

		// Check the present for non-empty hash node(embedded node doesn't
		// have their own hash).
		if node != (common.Hash{}) {
			blob, _ := reader.Node(common.Hash{}, accIter.Path(), node)
			if len(blob) == 0 {
				log.Error("Missing trie node(account)", "hash", node)
				return errors.New("missing account")
			}
			hasher.Reset()
			hasher.Write(blob)
			hasher.Read(got)
			if !bytes.Equal(got, node.Bytes()) {
				log.Error("Invalid trie node(account)", "hash", node.Hex(), "value", blob)
				return errors.New("invalid account node")
			}
		}
		// If it's a leaf node, yes we are touching an account,
		// dig into the storage trie further.
		if accIter.Leaf() {
			accounts += 1
			var acc types.StateAccount
			if err := rlp.DecodeBytes(accIter.LeafBlob(), &acc); err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return errors.New("invalid account")
			}
			if acc.Root != types.EmptyRootHash {
				id := trie.StorageTrieID(root, common.BytesToHash(accIter.LeafKey()), acc.Root)
				storageTrie, err := trie.NewStateTrie(id, triedb)
				if err != nil {
					log.Error("Failed to open storage trie", "root", acc.Root, "err", err)
					return errors.New("missing storage trie")
				}
				storageIter, err := storageTrie.NodeIterator(nil)
				if err != nil {
					log.Error("Failed to open storage iterator", "root", acc.Root, "err", err)
					return err
				}
				for storageIter.Next(true) {
					nodes += 1
					node := storageIter.Hash()

					// Check the presence for non-empty hash node(embedded node doesn't
					// have their own hash).
					if node != (common.Hash{}) {
						blob, _ := reader.Node(common.BytesToHash(accIter.LeafKey()), storageIter.Path(), node)
						if len(blob) == 0 {
							log.Error("Missing trie node(storage)", "hash", node)
							return errors.New("missing storage")
						}
						hasher.Reset()
						hasher.Write(blob)
						hasher.Read(got)
						if !bytes.Equal(got, node.Bytes()) {
							log.Error("Invalid trie node(storage)", "hash", node.Hex(), "value", blob)
							return errors.New("invalid storage node")
						}
					}
					// Bump the counter if it's leaf node.
					if storageIter.Leaf() {
						slots += 1
					}
					if time.Since(lastReport) > time.Second*8 {
						log.Info("Traversing state", "nodes", nodes, "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
						lastReport = time.Now()
					}
				}
				if storageIter.Error() != nil {
					log.Error("Failed to traverse storage trie", "root", acc.Root, "err", storageIter.Error())
					return storageIter.Error()
				}
			}
			if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash.Bytes()) {
				if !rawdb.HasCode(chaindb, common.BytesToHash(acc.CodeHash)) {
					log.Error("Code is missing", "account", common.BytesToHash(accIter.LeafKey()))
					return errors.New("missing code")
				}
				codes += 1
			}
			if time.Since(lastReport) > time.Second*8 {
				log.Info("Traversing state", "nodes", nodes, "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
				lastReport = time.Now()
			}
		}
	}
	if accIter.Error() != nil {
		log.Error("Failed to traverse state trie", "root", root, "err", accIter.Error())
		return accIter.Error()
	}
	log.Info("State is complete", "nodes", nodes, "accounts", accounts, "slots", slots, "codes", codes, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

func parseRoot(input string) (common.Hash, error) {
	var h common.Hash
	if err := h.UnmarshalText([]byte(input)); err != nil {
		return h, err
	}
	return h, nil
}

func dumpState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()

	conf, root, err := parseDumpConfig(ctx, stack, db)
	if err != nil {
		return err
	}
	defer db.Close()
	triedb := utils.MakeTrieDatabase(ctx, stack, db, false, true, false)
	defer triedb.Close()

	snapConfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	triesInMemory := ctx.Uint64(utils.TriesInMemoryFlag.Name)
	snaptree, err := snapshot.New(snapConfig, db, triedb, root, int(triesInMemory), false)
	if err != nil {
		return err
	}
	accIt, err := snaptree.AccountIterator(root, common.BytesToHash(conf.Start))
	if err != nil {
		return err
	}
	defer accIt.Release()

	log.Info("Snapshot dumping started", "root", root)
	var (
		start    = time.Now()
		logged   = time.Now()
		accounts uint64
	)
	enc := json.NewEncoder(os.Stdout)
	enc.Encode(struct {
		Root common.Hash `json:"root"`
	}{root})
	for accIt.Next() {
		account, err := types.FullAccount(accIt.Account())
		if err != nil {
			return err
		}
		da := &state.DumpAccount{
			Balance:     account.Balance.String(),
			Nonce:       account.Nonce,
			Root:        account.Root.Bytes(),
			CodeHash:    account.CodeHash,
			AddressHash: accIt.Hash().Bytes(),
		}
		if !conf.SkipCode && !bytes.Equal(account.CodeHash, types.EmptyCodeHash.Bytes()) {
			da.Code = rawdb.ReadCode(db, common.BytesToHash(account.CodeHash))
		}
		if !conf.SkipStorage {
			da.Storage = make(map[common.Hash]string)

			stIt, err := snaptree.StorageIterator(root, accIt.Hash(), common.Hash{})
			if err != nil {
				return err
			}
			for stIt.Next() {
				da.Storage[stIt.Hash()] = common.Bytes2Hex(stIt.Slot())
			}
		}
		enc.Encode(da)
		accounts++
		if time.Since(logged) > 8*time.Second {
			log.Info("Snapshot dumping in progress", "at", accIt.Hash(), "accounts", accounts,
				"elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
		if conf.Max > 0 && accounts >= conf.Max {
			break
		}
	}
	log.Info("Snapshot dumping complete", "accounts", accounts,
		"elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// snapshotExportPreimages dumps the preimage data to a flat file.
func snapshotExportPreimages(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		utils.Fatalf("This command requires an argument.")
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()

	triedb := utils.MakeTrieDatabase(ctx, stack, chaindb, false, true, false)
	defer triedb.Close()

	var root common.Hash
	if ctx.NArg() > 1 {
		rootBytes := common.FromHex(ctx.Args().Get(1))
		if len(rootBytes) != common.HashLength {
			return fmt.Errorf("invalid hash: %s", ctx.Args().Get(1))
		}
		root = common.BytesToHash(rootBytes)
	} else {
		headBlock := rawdb.ReadHeadBlock(chaindb)
		if headBlock == nil {
			log.Error("Failed to load head block")
			return errors.New("no head block")
		}
		root = headBlock.Root()
	}
	snapConfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	snaptree, err := snapshot.New(snapConfig, chaindb, triedb, root, 128, false)
	if err != nil {
		return err
	}
	return utils.ExportSnapshotPreimages(chaindb, snaptree, ctx.Args().First(), root)
}

// checkAccount iterates the snap data layers, and looks up the given account
// across all layers.
func checkAccount(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return errors.New("need <address|hash> arg")
	}
	var (
		hash common.Hash
		addr common.Address
	)
	switch arg := ctx.Args().First(); len(arg) {
	case 40, 42:
		addr = common.HexToAddress(arg)
		hash = crypto.Keccak256Hash(addr.Bytes())
	case 64, 66:
		hash = common.HexToHash(arg)
	default:
		return errors.New("malformed address or hash")
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()
	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()
	start := time.Now()
	log.Info("Checking difflayer journal", "address", addr, "hash", hash)
	if err := snapshot.CheckJournalAccount(chaindb, hash); err != nil {
		return err
	}
	log.Info("Checked the snapshot journalled storage", "time", common.PrettyDuration(time.Since(start)))
	return nil
}

func compareBlockAndStateID(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chainDB := utils.MakeChainDatabase(ctx, stack, false, false)
	defer chainDB.Close()

	ancientsDir, _ := chainDB.AncientDatadir()

	// trieDB := utils.MakeTrieDatabase(ctx, stack, chainDB, false, false, false)
	// defer trieDB.Close()

	stateFreezer, err := rawdb.NewStateFreezer(ancientsDir, false, true, 0)
	if err != nil {
		log.Error("Failed to load state freezer", "err", err)
		return err
	}

	path := ctx.String(utils.IncrementalSnapshotPathFlag.Name)
	log.Info("Print incremental snapshot info", "path", path, "ancientsDir", ancientsDir)

	if ctx.IsSet(utils.IncrementalSnapshotFlag.Name) {
		log.Info("Compare incremental data with synced data")
		incrChainFreezer, err := rawdb.OpenIncrChainFreezer(path, true, 0)
		if err != nil {
			log.Error("Failed to open incremental chain freezer", "err", err)
			return err
		}
		defer incrChainFreezer.Close()
		ancients, _ := incrChainFreezer.Ancients()
		tail, _ := incrChainFreezer.Tail()
		count, _ := incrChainFreezer.ItemAmountInAncient()
		log.Info("Incr chain info", "ancients", ancients, "tail", tail, "count", count)

		incrStateFreezer, err := rawdb.OpenIncrStateFreezer(path, true, 0)
		if err != nil {
			log.Error("Failed to open incremental state freezer", "err", err)
			return err
		}
		incrStateFreezer = incrStateFreezer
		defer incrStateFreezer.Close()

		ancientsState, _ := incrStateFreezer.Ancients()
		tailState, _ := incrStateFreezer.Tail()
		countState, _ := incrStateFreezer.ItemAmountInAncient()
		log.Info("Incr state info", "ancients", ancientsState, "tail", tailState, "count", countState)

		// iterate incr chain freezer
		a := 0
		nonEmptyMap := make(map[uint64]uint64)
		for i := tail; i < ancients; i++ {
			data, err := incrChainFreezer.Ancient(rawdb.IncrChainFreezerBlockStateIDMappingTable, i)
			if err != nil {
				log.Error("Failed to get mapping in incr chain freezer", "err", err)
			}
			stateID := binary.BigEndian.Uint64(data)
			if stateID == 0 {
				a++
				// log.Warn("State id is empty", "stateID", stateID, "block", i)
				continue
			}
			nonEmptyMap[stateID] = i
		}
		log.Info("Incr chain freezer", "count", len(nonEmptyMap), "empty count", a)

		for i := tailState + 1; i <= ancientsState; i++ {
			blob, err := incrStateFreezer.Ancient("history.meta", i-1)
			if err != nil {
				log.Error("Failed to get history meta in incr state freezer", "err", err)
			}
			if len(blob) == 0 {
				return fmt.Errorf("state history not found %d in incr state freezer", i)
			}
			var m meta
			if err = m.decode(blob); err != nil {
				log.Error("Failed to decode state meta", "err", err)
				return err
			}
			// log.Info("Incr chain freezer", "stateID", i, "block", i, "meta", m.block, "root", m.root.String())

			c, err := stateFreezer.Ancient("history.meta", i-1)
			if err != nil {
				log.Error("Failed to get history meta in state freezer", "err", err)
			}
			if len(c) == 0 {
				return fmt.Errorf("state history not found %d in state freezer", i)
			}
			var m1 meta
			if err = m1.decode(c); err != nil {
				log.Error("Failed to decode state meta", "err", err)
				return err
			}
			if i == 10867 {
				log.Info("Print 10867 state", "incr_block", m.block, "raw_block", m1.block,
					"incr_root", m.root.String(), "raw_root", m1.root.String())
			}

			if m1.block != m.block {
				log.Error("Unequal block number", "state_id", i, "incr_block", m.block, "raw_block", m1.block)
				continue
			}
			if m1.root != m.root {
				log.Error("Unequal root hash", "state_id", i, "incr_root", m.root.String(),
					"raw_root", m1.root.String())
				continue
			}

			if i%2000 == 0 {
				log.Info("Iterating", "i", i)
			}
		}
	} else {
		log.Info("Compare merged data with synced data")
		mergedAncient := path
		mergedStateFreezer, err := rawdb.NewStateFreezer(mergedAncient, false, true, 0)
		if err != nil {
			log.Error("Failed to load merged state freezer", "err", err)
			return err
		}
		ancientsState, _ := mergedStateFreezer.Ancients()
		tailState, _ := mergedStateFreezer.Tail()
		countState, _ := mergedStateFreezer.ItemAmountInAncient()
		log.Info("Merged state info", "ancients", ancientsState, "tail", tailState, "count", countState)

		latestRoot := common.Hash{}
		for i := uint64(1); i <= ancientsState; i++ {
			blob, err := mergedStateFreezer.Ancient("history.meta", i-1)
			if err != nil {
				log.Error("Failed to get history meta in incr state freezer", "err", err)
			}
			if len(blob) == 0 {
				return fmt.Errorf("state history not found %d in incr state freezer", i)
			}
			var m meta
			if err = m.decode(blob); err != nil {
				log.Error("Failed to decode state meta", "err", err)
				return err
			}

			c, err := stateFreezer.Ancient("history.meta", i-1)
			if err != nil {
				log.Error("Failed to get history meta in state freezer", "err", err)
			}
			if len(c) == 0 {
				return fmt.Errorf("state history not found %d in state freezer", i)
			}
			var m1 meta
			if err = m1.decode(c); err != nil {
				log.Error("Failed to decode state meta", "err", err)
				return err
			}

			if m1.block != m.block {
				log.Error("Unequal block number", "state_id", i, "incr_block", m.block, "raw_block", m1.block)
				continue
			}
			if m1.root != m.root {
				log.Error("Unequal root hash", "state_id", i, "incr_root", m.root.String(),
					"raw_root", m1.root.String())
				continue
			}

			if i == ancientsState {
				log.Info("ancientsState block number", "state_id", i, "incr_block", m.block, "raw_block", m1.block,
					"incr_root", m.root.String(), "raw_root", m1.root.String())
				latestRoot = m1.root
			}
			if m.block == 197025 {
				log.Info("197025 block number", "state_id", i, "incr_block", m.block, "raw_block", m1.block,
					"incr_root", m.root.String(), "raw_root", m1.root.String())
				latestRoot = m1.root
			}

			if i%2000 == 0 {
				log.Info("Iterating", "i", i)
			}
		}

		id := rawdb.ReadStateID(chainDB.BlockStore(), latestRoot)
		log.Info("Read state info", "id", *id)
	}
	return nil
}

func forceFlushToAncient(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chainDB := utils.MakeChainDatabase(ctx, stack, false, false)
	defer chainDB.Close()

	if err := chainDB.ForceFreeze(chainDB.BlockStore()); err != nil {
		log.Error("Failed to force freeze to ancients", "err", err)
		return err
	}

	return nil
}

// mergeIncrSnapshot
func mergeIncrSnapshot(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chainDB := utils.MakeChainDatabase(ctx, stack, false, false)
	defer chainDB.Close()

	trieDB := utils.MakeTrieDatabase(ctx, stack, chainDB, false, false, false)
	defer trieDB.Close()

	if !ctx.IsSet(utils.IncrementalSnapshotPathFlag.Name) {
		return errors.New("incremental snapshot path is not set")
	}

	path := ctx.String(utils.IncrementalSnapshotPathFlag.Name)
	log.Info("Start merging incremental snapshot", "path", path)

	// if err := checkStateWithBlock(path); err != nil {
	// 	log.Error("Failed to check incremental snapshot", "path", path, "err", err)
	// 	return err
	// }

	if err := trieDB.InsertIncrState(path); err != nil {
		log.Error("Failed to insert incremental state", "err", err)
		return err
	}

	if err := insertIncrBlock(path, chainDB); err != nil {
		log.Error("Failed to insert increment block", "err", err)
		return err
	}

	if err := insertContractCodes(path, chainDB); err != nil {
		log.Error("Failed to insert contract codes", "err", err)
		return err
	}

	return nil
}

const (
	historyMetaSize = 9 + 2*common.HashLength // The length of encoded history meta

	stateHistoryV0 = uint8(0) // initial version of state history structure
	stateHistoryV1 = uint8(1) // use the storage slot raw key as the identifier instead of the key hash
)

type meta struct {
	version uint8       // version tag of history object
	parent  common.Hash // prev-state root before the state transition
	root    common.Hash // post-state root after the state transition
	block   uint64      // associated block number
}

// decode unpacks the meta object from byte stream.
func (m *meta) decode(blob []byte) error {
	if len(blob) < 1 {
		return errors.New("no version tag")
	}
	switch blob[0] {
	case stateHistoryV0, stateHistoryV1:
		if len(blob) != historyMetaSize {
			return fmt.Errorf("invalid state history meta, len: %d", len(blob))
		}
		m.version = blob[0]
		m.parent = common.BytesToHash(blob[1 : 1+common.HashLength])
		m.root = common.BytesToHash(blob[1+common.HashLength : 1+2*common.HashLength])
		m.block = binary.BigEndian.Uint64(blob[1+2*common.HashLength : historyMetaSize])
		return nil
	default:
		return fmt.Errorf("unknown version %d", blob[0])
	}
}

// before inserting incremental snapshot into base snapshot, should check the latest block number
// is existent with the latest state id
func checkStateWithBlock(incrDir string) error {
	incrChainFreezer, err := rawdb.OpenIncrChainFreezer(incrDir, true, 0)
	if err != nil {
		log.Error("Failed to open incremental chain freezer", "err", err)
		return err
	}
	defer incrChainFreezer.Close()
	ancients, _ := incrChainFreezer.Ancients()
	tail, _ := incrChainFreezer.Tail()
	count, _ := incrChainFreezer.ItemAmountInAncient()
	log.Info("Incr chain info", "ancients", ancients, "tail", tail, "count", count)

	incrStateFreezer, err := rawdb.OpenIncrStateFreezer(incrDir, true, 0)
	if err != nil {
		log.Error("Failed to open incremental state freezer", "err", err)
		return err
	}
	incrStateFreezer = incrStateFreezer
	defer incrStateFreezer.Close()

	ancientsState, _ := incrStateFreezer.Ancients()
	tailState, _ := incrStateFreezer.Tail()
	countState, _ := incrStateFreezer.ItemAmountInAncient()
	log.Info("Incr state info", "ancients", ancientsState, "tail", tailState, "count", countState)

	// TODO: traverse in descending order, check state id with block is right
	for i := ancients - 1; i >= tail; i-- {
		data, err := incrChainFreezer.Ancient(rawdb.IncrChainFreezerBlockStateIDMappingTable, i)
		if err != nil {
			log.Error("Failed to get mapping in chain freezer", "err", err)
		}
		stateID := binary.BigEndian.Uint64(data)
		if stateID == 0 {
			log.Warn("State id is empty", "stateID", stateID, "block", i)
			continue
		}
		blob, err := incrStateFreezer.Ancient("history.meta", stateID-1)
		if len(blob) == 0 {
			return fmt.Errorf("state history not found %d", stateID)
		}
		var m meta
		if err := m.decode(blob); err != nil {
			log.Error("Failed to decode state meta", "err", err)
			return err
		}
		log.Info("Incr chain freezer", "stateID", stateID, "block", i, "meta", m.block, "root", m.root.String())
	}
	return nil
}

func insertIncrBlock(incrDir string, chainDB ethdb.Database) error {
	incrChainFreezer, err := rawdb.OpenIncrChainFreezer(incrDir, true, 0)
	if err != nil {
		log.Error("Failed to open incremental chain freezer", "err", err)
		return err
	}
	defer incrChainFreezer.Close()

	ancients, _ := incrChainFreezer.Ancients()
	tail, _ := incrChainFreezer.Tail()
	count, _ := incrChainFreezer.ItemAmountInAncient()
	log.Info("Incr chain info", "ancients", ancients, "tail", tail, "count", count)

	// Get chain config from database
	genesisHash := rawdb.ReadCanonicalHash(chainDB.BlockStore(), 0)
	if genesisHash == (common.Hash{}) {
		return errors.New("genesis hash not found")
	}

	chainConfig := rawdb.ReadChainConfig(chainDB.BlockStore(), genesisHash)
	if chainConfig == nil {
		return errors.New("chain config not found")
	}

	log.Info("Loaded chain config", "chainID", chainConfig.ChainID, "cancunTime", chainConfig.CancunTime)

	// Step 1: Force migration of existing blocks from pebble to chainFreezer
	// This ensures all non-genesis blocks are in ancient store before adding incremental data
	log.Info("Starting forced migration of existing blocks to chainFreezer")

	// Step 2: Directly write incremental data to chainFreezer
	log.Info("Starting direct write of incremental data to chainFreezer")

	existCount := 0
	// TODO: start two goroutines, optimize pebble batch commit
	for i := tail; i < ancients; i++ {
		hashBytes, err := rawdb.ReadIncrChainHash(incrChainFreezer, i)
		if err != nil {
			log.Error("Failed to read increment chain hash", "err", err)
			return err
		}
		hash := common.BytesToHash(hashBytes)
		header, err := rawdb.ReadIncrChainHeader(incrChainFreezer, i)
		if err != nil {
			log.Error("Failed to read increment chain header", "err", err)
			return err
		}
		body, err := rawdb.ReadIncrChainBodies(incrChainFreezer, i)
		if err != nil {
			log.Error("Failed to read increment chain bodies", "err", err)
			return err
		}
		receipts, err := rawdb.ReadIncrChainReceipts(incrChainFreezer, i)
		if err != nil {
			log.Error("Failed to read increment chain receipts", "err", err)
			return err
		}
		td, err := rawdb.ReadIncrChainDifficulty(incrChainFreezer, i)
		if err != nil {
			log.Error("Failed to read increment chain difficulty", "err", err)
			return err
		}

		// Decode header to get block number and timestamp for Cancun check
		var h types.Header
		if err := rlp.DecodeBytes(header, &h); err != nil {
			log.Error("Failed to decode header", "block", i, "err", err)
			return err
		}

		// Check if Cancun hardfork is active for this block
		isCancunActive := chainConfig.IsCancun(h.Number, h.Time)

		// Read blob sidecars only if Cancun is active
		var blobs rlp.RawValue
		if isCancunActive {
			blobs, err = rawdb.ReadIncrChainBlobSideCars(incrChainFreezer, i)
			if err != nil {
				log.Error("Failed to read increment chain blob side car", "err", err)
				return err
			}
		}
		if exist := rawdb.HasBody(chainDB.BlockStore(), hash, i); exist {
			existCount++
		}
		blockBatch := chainDB.BlockStore().NewBatch()

		// td, block(body and header), receipts, blob(if present)
		rawdb.WriteCanonicalHash(blockBatch, hash, i)
		rawdb.WriteTdRLP(blockBatch, hash, i, td)
		rawdb.WriteBodyRLP(blockBatch, hash, i, body)
		rawdb.WriteHeaderRLP(blockBatch, hash, i, header)
		rawdb.WriteReceiptsRLP(blockBatch, hash, i, receipts)
		if false {
			rawdb.WriteBlobSidecarsRLP(blockBatch, hash, i, blobs)
		}
		if err = blockBatch.Write(); err != nil {
			log.Crit("Failed to write block into disk", "err", err)
		}
	}
	log.Info("After merging block", "exist count", existCount)

	// TODO: should wait background chain freezer finish

	// set blockchain metadata: current snap block and current block
	hashBytes, err := rawdb.ReadIncrChainHash(incrChainFreezer, ancients-1)
	if err != nil {
		log.Error("Failed to read increment chain hash for metadata", "err", err)
		return err
	}
	hash := common.BytesToHash(hashBytes)
	blockBatch := chainDB.BlockStore().NewBatch()
	rawdb.WriteHeadBlockHash(blockBatch, hash)
	rawdb.WriteHeadHeaderHash(blockBatch, hash)
	rawdb.WriteHeadFastBlockHash(blockBatch, hash)
	rawdb.WriteFinalizedBlockHash(blockBatch, hash)
	if err = blockBatch.Write(); err != nil {
		log.Crit("Failed to update block metadata into disk", "err", err)
	}

	return nil
}

func insertContractCodes(incrDir string, chainDB ethdb.Database) error {
	newDB, err := pebble.New(incrDir, 10, 10, "incremental", true)
	if err != nil {
		log.Error("Failed to open pebble to read incremental data", "err", err)
		return err
	}
	defer newDB.Close()

	it := newDB.NewIterator(rawdb.CodePrefix, nil)
	defer it.Release()

	codeCount := 0
	for it.Next() {
		key := it.Key()
		value := it.Value()

		isCode, hashBytes := rawdb.IsCodeKey(key)
		if !isCode {
			log.Warn("Invalid code key found", "key", fmt.Sprintf("%x", key))
			continue
		}

		codeHash := common.BytesToHash(hashBytes)
		if rawdb.HasCodeWithPrefix(chainDB.BlockStore(), codeHash) {
			log.Debug("Code already exists, skipping", "hash", codeHash.Hex())
			continue
		}
		rawdb.WriteCode(chainDB.BlockStore(), codeHash, value)

		codeCount++
		if codeCount%1000 == 0 {
			log.Info("Inserting contract codes", "processed", codeCount)
		}
	}

	if err := it.Error(); err != nil {
		log.Error("Iterator error while reading contract codes", "err", err)
		return err
	}

	log.Info("Contract codes insertion completed", "total", codeCount)
	return nil
}
