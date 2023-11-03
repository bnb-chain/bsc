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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/tsdb/fileutil"

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
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	cli "github.com/urfave/cli/v2"
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
				Flags: flags.Merge([]cli.Flag{
					utils.BloomFilterSizeFlag,
					utils.TriesInMemoryFlag,
				}, utils.NetworkFlags, utils.DatabasePathFlags),
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
the brief workflow is to backup the the number of this specified amount blocks backward in original ancientdb 
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
				Flags: flags.Merge([]cli.Flag{
					utils.StateSchemeFlag,
				}, utils.NetworkFlags, utils.DatabasePathFlags),
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
				Flags:     flags.Merge(utils.NetworkFlags, utils.DatabasePathFlags),
				Description: `
geth snapshot check-dangling-storage <state-root> traverses the snap storage 
data, and verifies that all snapshot storage data has a corresponding account. 
`,
			},
			{
				Name:      "inspect-account",
				Usage:     "Check all snapshot layers for the a specific account",
				ArgsUsage: "<address | hash>",
				Action:    checkAccount,
				Flags:     flags.Merge(utils.NetworkFlags, utils.DatabasePathFlags),
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
				Flags: flags.Merge([]cli.Flag{
					utils.StateSchemeFlag,
				}, utils.NetworkFlags, utils.DatabasePathFlags),
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
				Flags: flags.Merge([]cli.Flag{
					utils.StateSchemeFlag,
				}, utils.NetworkFlags, utils.DatabasePathFlags),
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
				Flags: flags.Merge([]cli.Flag{
					utils.ExcludeCodeFlag,
					utils.ExcludeStorageFlag,
					utils.StartKeyFlag,
					utils.DumpLimitFlag,
					utils.TriesInMemoryFlag,
					utils.StateSchemeFlag,
				}, utils.NetworkFlags, utils.DatabasePathFlags),
				Description: `
This command is semantically equivalent to 'geth dump', but uses the snapshots
as the backend data source, making this command a lot faster. 

The argument is interpreted as block number or hash. If none is provided, the latest
block is used.
`,
			},
		},
	}
)

func accessDb(ctx *cli.Context, stack *node.Node) (ethdb.Database, error) {
	//The layer of tries trees that keep in memory.
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
	//Make sure the MPT and snapshot matches before pruning, otherwise the node can not start.
	snapconfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	snaptree, err := snapshot.New(snapconfig, chaindb, trie.NewDatabase(chaindb, nil), headBlock.Root(), TriesInMemory, false)
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
		oldAncientPath = ctx.String(utils.AncientFlag.Name)
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
		// No file lock found for old ancientDB but new ancientDB exsisted, indicating the geth was interrupted
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

	//After backing up successfully, rename the new ancientdb name to the original one, and delete the old ancientdb
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

	if rawdb.ReadStateScheme(chaindb) != rawdb.HashScheme {
		log.Crit("Offline pruning is not required for path scheme")
	}
	prunerconfig := pruner.Config{
		Datadir:   stack.ResolvePath(""),
		BloomSize: ctx.Uint64(utils.BloomFilterSizeFlag.Name),
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
	triedb := utils.MakeTrieDatabase(ctx, chaindb, false, true)
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

	return snapshot.CheckDanglingStorage(utils.MakeChainDatabase(ctx, stack, true, false))
}

// traverseState is a helper function used for pruning verification.
// Basically it just iterates the trie, ensure all nodes and associated
// contract codes are present.
func traverseState(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	defer chaindb.Close()

	triedb := utils.MakeTrieDatabase(ctx, chaindb, false, true)
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

	triedb := utils.MakeTrieDatabase(ctx, chaindb, false, true)
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
	reader, err := triedb.Reader(root)
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

	conf, db, root, err := parseDumpConfig(ctx, stack)
	if err != nil {
		return err
	}
	triedb := utils.MakeTrieDatabase(ctx, db, false, true)
	defer triedb.Close()

	snapConfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	triesInMemory := ctx.Uint64(utils.TriesInMemoryFlag.Name)
	snaptree, err := snapshot.New(snapConfig, db, trie.NewDatabase(db, nil), root, int(triesInMemory), false)
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
			Balance:   account.Balance.String(),
			Nonce:     account.Nonce,
			Root:      account.Root.Bytes(),
			CodeHash:  account.CodeHash,
			SecureKey: accIt.Hash().Bytes(),
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
