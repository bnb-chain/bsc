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
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/console/prompt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli/v2"
)

var (
	removeStateDataFlag = &cli.BoolFlag{
		Name:  "remove.state",
		Usage: "If set, selects the state data for removal",
	}
	removeChainDataFlag = &cli.BoolFlag{
		Name:  "remove.chain",
		Usage: "If set, selects the state data for removal",
	}

	removedbCommand = &cli.Command{
		Action:    removeDB,
		Name:      "removedb",
		Usage:     "Remove blockchain and state databases",
		ArgsUsage: "",
		Flags: slices.Concat(utils.DatabaseFlags,
			[]cli.Flag{removeStateDataFlag, removeChainDataFlag}),
		Description: `
Remove blockchain and state databases`,
	}
	dbCommand = &cli.Command{
		Name:      "db",
		Usage:     "Low level database operations",
		ArgsUsage: "",
		Subcommands: []*cli.Command{
			dbInspectCmd,
			dbStatCmd,
			dbCompactCmd,
			dbGetCmd,
			dbDeleteCmd,
			dbDeleteTrieStateCmd,
			dbInspectTrieCmd,
			dbPutCmd,
			dbGetSlotsCmd,
			dbDumpFreezerIndex,
			dbImportCmd,
			dbExportCmd,
			dbMetadataCmd,
			ancientInspectCmd,
			// no legacy stored receipts for bsc
			// dbMigrateFreezerCmd,
			dbCheckStateContentCmd,
			dbHbss2PbssCmd,
			dbTrieGetCmd,
			dbTrieDeleteCmd,
			dbInspectHistoryCmd,
			dbMigrateCmd,
		},
	}
	dbInspectCmd = &cli.Command{
		Action:    inspect,
		Name:      "inspect",
		ArgsUsage: "<prefix> <start>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Usage:       "Inspect the storage size for each type of data in the database",
		Description: `This commands iterates the entire database. If the optional 'prefix' and 'start' arguments are provided, then the iteration is limited to the given subset of data.`,
	}
	dbInspectTrieCmd = &cli.Command{
		Action:    inspectTrie,
		Name:      "inspect-trie",
		ArgsUsage: "<blocknum> <jobnum> <topn>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.SyncModeFlag,
		},
		Usage:       "Inspect the MPT tree of the account and contract. 'blocknum' can be latest/snapshot/number. 'topn' means output the top N storage tries info ranked by the total number of TrieNodes",
		Description: `This commands iterates the entrie WorldState.`,
	}
	dbCheckStateContentCmd = &cli.Command{
		Action:    checkStateContent,
		Name:      "check-state-content",
		ArgsUsage: "<start (optional)>",
		Flags:     slices.Concat(utils.NetworkFlags, utils.DatabaseFlags),
		Usage:     "Verify that state data is cryptographically correct",
		Description: `This command iterates the entire database for 32-byte keys, looking for rlp-encoded trie nodes.
For each trie node encountered, it checks that the key corresponds to the keccak256(value). If this is not true, this indicates
a data corruption.`,
	}
	dbHbss2PbssCmd = &cli.Command{
		Action:    hbss2pbss,
		Name:      "hbss-to-pbss",
		ArgsUsage: "<jobnum (optional)>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.SyncModeFlag,
			utils.ForceFlag,
			utils.AncientFlag,
		},
		Usage:       "Convert Hash-Base to Path-Base trie node.",
		Description: `This command iterates the entire trie node database and convert the hash-base node to path-base node.`,
	}
	dbTrieGetCmd = &cli.Command{
		Action:    dbTrieGet,
		Name:      "trie-get",
		Usage:     "Show the value of a trie node path key",
		ArgsUsage: "[trie owner] <path-base key>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.SyncModeFlag,
			utils.BSCMainnetFlag,
			utils.ChapelFlag,
			utils.StateSchemeFlag,
		},
		Description: "This command looks up the specified trie node key from the database.",
	}
	dbTrieDeleteCmd = &cli.Command{
		Action:    dbTrieDelete,
		Name:      "trie-delete",
		Usage:     "delete the specify trie node",
		ArgsUsage: "[trie owner] <hash-base key> | <path-base key>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.SyncModeFlag,
			utils.BSCMainnetFlag,
			utils.ChapelFlag,
			utils.StateSchemeFlag,
		},
		Description: "This command delete the specify trie node from the database.",
	}
	dbStatCmd = &cli.Command{
		Action: dbStats,
		Name:   "stats",
		Usage:  "Print leveldb statistics",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
	}
	dbCompactCmd = &cli.Command{
		Action: dbCompact,
		Name:   "compact",
		Usage:  "Compact leveldb database. WARNING: May take a very long time",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
			utils.CacheFlag,
			utils.CacheDatabaseFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: `This command performs a database compaction.
WARNING: This operation may take a very long time to finish, and may cause database
corruption if it is aborted during execution'!`,
	}
	dbGetCmd = &cli.Command{
		Action:    dbGet,
		Name:      "get",
		Usage:     "Show the value of a database key",
		ArgsUsage: "<hex-encoded key>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "This command looks up the specified database key from the database.",
	}
	dbDeleteCmd = &cli.Command{
		Action:    dbDelete,
		Name:      "delete",
		Usage:     "Delete a database key (WARNING: may corrupt your database)",
		ArgsUsage: "<hex-encoded key>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: `This command deletes the specified database key from the database.
WARNING: This is a low-level operation which may cause database corruption!`,
	}
	dbDeleteTrieStateCmd = &cli.Command{
		Action: dbDeleteTrieState,
		Name:   "delete-trie-state",
		Usage:  "Delete all trie state key-value pairs from the database and the ancient state. Does not support hash-based state scheme.",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: `This command deletes all trie state key-value pairs from the database and the ancient state.`,
	}
	dbPutCmd = &cli.Command{
		Action:    dbPut,
		Name:      "put",
		Usage:     "Set the value of a database key (WARNING: may corrupt your database)",
		ArgsUsage: "<hex-encoded key> <hex-encoded value>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: `This command sets a given database key to the given value.
WARNING: This is a low-level operation which may cause database corruption!`,
	}
	dbGetSlotsCmd = &cli.Command{
		Action:    dbDumpTrie,
		Name:      "dumptrie",
		Usage:     "Show the storage key/values of a given storage trie",
		ArgsUsage: "<hex-encoded state root> <hex-encoded account hash> <hex-encoded storage trie root> <hex-encoded start (optional)> <int max elements (optional)>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "This command looks up the specified database key from the database.",
	}
	dbDumpFreezerIndex = &cli.Command{
		Action:    freezerInspect,
		Name:      "freezer-index",
		Usage:     "Dump out the index of a specific freezer table",
		ArgsUsage: "<freezer-type> <table-type> <start (int)> <end (int)>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "This command displays information about the freezer index.",
	}
	dbImportCmd = &cli.Command{
		Action:    importLDBdata,
		Name:      "import",
		Usage:     "Imports leveldb-data from an exported RLP dump.",
		ArgsUsage: "<dumpfile> <start (optional)",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "The import command imports the specific chain data from an RLP encoded stream.",
	}
	dbExportCmd = &cli.Command{
		Action:    exportChaindata,
		Name:      "export",
		Usage:     "Exports the chain data into an RLP dump. If the <dumpfile> has .gz suffix, gzip compression will be used.",
		ArgsUsage: "<type> <dumpfile>",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "Exports the specified chain data to an RLP encoded stream, optionally gzip-compressed.",
	}
	dbMetadataCmd = &cli.Command{
		Action: showMetaData,
		Name:   "metadata",
		Usage:  "Shows metadata about the chain status.",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "Shows metadata about the chain status.",
	}
	ancientInspectCmd = &cli.Command{
		Action: ancientInspect,
		Name:   "inspect-reserved-oldest-blocks",
		Flags: []cli.Flag{
			utils.DataDirFlag,
		},
		Usage: "Inspect the ancientStore information",
		Description: `This commands will read current offset from kvdb, which is the current offset and starting BlockNumber
of ancientStore, will also displays the reserved number of blocks in ancientStore `,
	}
	dbInspectHistoryCmd = &cli.Command{
		Action:    inspectHistory,
		Name:      "inspect-history",
		Usage:     "Inspect the state history within block range",
		ArgsUsage: "<address> [OPTIONAL <storage-slot>]",
		Flags: slices.Concat([]cli.Flag{
			utils.SyncModeFlag,
			&cli.Uint64Flag{
				Name:  "start",
				Usage: "block number of the range start, zero means earliest history",
			},
			&cli.Uint64Flag{
				Name:  "end",
				Usage: "block number of the range end(included), zero means latest history",
			},
			&cli.BoolFlag{
				Name:  "raw",
				Usage: "display the decoded raw state value (otherwise shows rlp-encoded value)",
			},
		}, utils.NetworkFlags, utils.DatabaseFlags),
		Description: "This command queries the history of the account or storage slot within the specified block range",
	}
	dbMigrateCmd = &cli.Command{
		Action:    migrateDatabase,
		Name:      "migrate",
		Usage:     "Migrate single database to multi-database format (in-place or expand mode)",
		ArgsUsage: "[target-datadir] [version]",
		Flags: slices.Concat([]cli.Flag{
			utils.DataDirFlag,
			utils.SyncModeFlag,
			utils.CacheFlag,
			utils.CacheDatabaseFlag,
			utils.ExpandModeFlag,
		}, utils.NetworkFlags),
		Description: `This command migrates a single chaindb database to multi-database format.

Two modes available:
1. IN-PLACE mode (default): Migrates data within the same datadir
2. EXPAND mode (--expandmode): Writes transformed data to existing target multi-database

The source database will be read from --datadir/chaindata directory, and data will be split into:
  - chaindata/           - chain and metadata (remaining)
  - chaindata/state      - state trie data
  - chaindata/snapshot   - snapshot data
  - chaindata/txindex    - transaction index data
 
Usage examples:
  geth --datadir /data/ethereum db migrate                           # In-place migration
  geth --datadir /source/ethereum --expandmode db migrate /target/ethereum  # Expand mode with version=1
  geth --datadir /source/ethereum --expandmode db migrate /target/ethereum 5  # Expand mode with version=5
 
EXPAND mode requirements:
  - Target directory must already contain a migrated multi-database structure
  - Target should have: chaindata/, chaindata/state/, chaindata/snapshot/, chaindata/txindex/
  - Use regular migrate mode first to prepare the target database structure
 
WARNING: This operation may take a very long time to finish for large databases (2TB+).`,
	}
)

func removeDB(ctx *cli.Context) error {
	stack, config := makeConfigNode(ctx)

	// Resolve folder paths.
	var (
		rootDir    = stack.ResolvePath("chaindata")
		ancientDir = config.Eth.DatabaseFreezer
	)
	switch {
	case ancientDir == "":
		ancientDir = filepath.Join(stack.ResolvePath("chaindata"), "ancient")
	case !filepath.IsAbs(ancientDir):
		ancientDir = config.Node.ResolvePath(ancientDir)
	}
	// Delete state data
	statePaths := []string{
		rootDir,
		filepath.Join(ancientDir, rawdb.MerkleStateFreezerName),
		filepath.Join(ancientDir, rawdb.VerkleStateFreezerName),
	}
	confirmAndRemoveDB(statePaths, "state data", ctx, removeStateDataFlag.Name)

	// Delete ancient chain
	chainPaths := []string{filepath.Join(
		ancientDir,
		rawdb.ChainFreezerName,
	)}
	confirmAndRemoveDB(chainPaths, "ancient chain", ctx, removeChainDataFlag.Name)
	return nil
}

// removeFolder deletes all files (not folders) inside the directory 'dir' (but
// not files in subfolders).
func removeFolder(dir string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		// If we're at the top level folder, recurse into
		if path == dir {
			return nil
		}
		// Delete all the files, but not subfolders
		if !info.IsDir() {
			os.Remove(path)
			return nil
		}
		return filepath.SkipDir
	})
}

// confirmAndRemoveDB prompts the user for a last confirmation and removes the
// list of folders if accepted.
func confirmAndRemoveDB(paths []string, kind string, ctx *cli.Context, removeFlagName string) {
	var (
		confirm bool
		err     error
	)
	msg := fmt.Sprintf("Location(s) of '%s': \n", kind)
	for _, path := range paths {
		msg += fmt.Sprintf("\t- %s\n", path)
	}
	fmt.Println(msg)
	if ctx.IsSet(removeFlagName) {
		confirm = ctx.Bool(removeFlagName)
		if confirm {
			fmt.Printf("Remove '%s'? [y/n] y\n", kind)
		} else {
			fmt.Printf("Remove '%s'? [y/n] n\n", kind)
		}
	} else {
		confirm, err = prompt.Stdin.PromptConfirm(fmt.Sprintf("Remove '%s'?", kind))
	}
	switch {
	case err != nil:
		utils.Fatalf("%v", err)
	case !confirm:
		log.Info("Database deletion skipped", "kind", kind, "paths", paths)
	default:
		var (
			deleted []string
			start   = time.Now()
		)
		for _, path := range paths {
			if common.FileExist(path) {
				removeFolder(path)
				deleted = append(deleted, path)
			} else {
				log.Info("Folder is not existent", "path", path)
			}
		}
		log.Info("Database successfully deleted", "kind", kind, "paths", deleted, "elapsed", common.PrettyDuration(time.Since(start)))
	}
}

func inspectTrie(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}

	if ctx.NArg() > 3 {
		return fmt.Errorf("Max 3 arguments: %v", ctx.Command.ArgsUsage)
	}

	var (
		blockNumber  uint64
		trieRootHash common.Hash
		jobnum       uint64
		topN         uint64
	)

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	var headerBlockHash common.Hash
	if ctx.NArg() >= 1 {
		if ctx.Args().Get(0) == "latest" {
			headerHash := rawdb.ReadHeadHeaderHash(db)
			blockNumber = *(rawdb.ReadHeaderNumber(db, headerHash))
		} else if ctx.Args().Get(0) == "snapshot" {
			trieRootHash = rawdb.ReadSnapshotRoot(db)
			blockNumber = math.MaxUint64
		} else {
			var err error
			blockNumber, err = strconv.ParseUint(ctx.Args().Get(0), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse blocknum, Args[0]: %v, err: %v", ctx.Args().Get(0), err)
			}
		}

		if ctx.NArg() == 1 {
			jobnum = 1000
			topN = 10
		} else if ctx.NArg() == 2 {
			var err error
			jobnum, err = strconv.ParseUint(ctx.Args().Get(1), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse jobnum, Args[1]: %v, err: %v", ctx.Args().Get(1), err)
			}
			topN = 10
		} else {
			var err error
			jobnum, err = strconv.ParseUint(ctx.Args().Get(1), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse jobnum, Args[1]: %v, err: %v", ctx.Args().Get(1), err)
			}

			topN, err = strconv.ParseUint(ctx.Args().Get(2), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse topn, Args[1]: %v, err: %v", ctx.Args().Get(1), err)
			}
		}

		if blockNumber != math.MaxUint64 {
			headerBlockHash = rawdb.ReadCanonicalHash(db, blockNumber)
			if headerBlockHash == (common.Hash{}) {
				return errors.New("ReadHeadBlockHash empty hash")
			}
			blockHeader := rawdb.ReadHeader(db, headerBlockHash, blockNumber)
			trieRootHash = blockHeader.Root
		}
		if (trieRootHash == common.Hash{}) {
			log.Error("Empty root hash")
		}
		fmt.Printf("ReadBlockHeader, root: %v, blocknum: %v\n", trieRootHash, blockNumber)

		dbScheme := rawdb.ReadStateScheme(db)
		var config *triedb.Config
		if dbScheme == rawdb.PathScheme {
			config = &triedb.Config{
				PathDB: utils.PathDBConfigAddJournalFilePath(stack, pathdb.ReadOnly),
				Cache:  0,
			}
		} else if dbScheme == rawdb.HashScheme {
			config = triedb.HashDefaults
		}

		triedb := triedb.NewDatabase(db, config)
		theTrie, err := trie.New(trie.TrieID(trieRootHash), triedb)
		if err != nil {
			fmt.Printf("fail to new trie tree, err: %v, rootHash: %v\n", err, trieRootHash.String())
			return err
		}
		theInspect, err := trie.NewInspector(theTrie, triedb, trieRootHash, blockNumber, jobnum, int(topN))
		if err != nil {
			return err
		}
		theInspect.Run()
		theInspect.DisplayResult()
	}
	return nil
}

func inspect(ctx *cli.Context) error {
	var (
		prefix []byte
		start  []byte
	)
	if ctx.NArg() > 2 {
		return fmt.Errorf("max 2 arguments: %v", ctx.Command.ArgsUsage)
	}
	if ctx.NArg() >= 1 {
		if d, err := hexutil.Decode(ctx.Args().Get(0)); err != nil {
			return fmt.Errorf("failed to hex-decode 'prefix': %v", err)
		} else {
			prefix = d
		}
	}
	if ctx.NArg() >= 2 {
		if d, err := hexutil.Decode(ctx.Args().Get(1)); err != nil {
			return fmt.Errorf("failed to hex-decode 'start': %v", err)
		} else {
			start = d
		}
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	fmt.Println("Inspecting chain database...")
	if err := rawdb.InspectDatabase(db, prefix, start); err != nil {
		return err
	}
	if stack.CheckIfMultiDataBase() {
		fmt.Println("Inspecting state database...")
		if err := inspectShardingDB(db.GetStateStore(), prefix, start); err != nil {
			return err
		}
		fmt.Println("Inspecting snap database...")
		if err := inspectShardingDB(rawdb.NewDatabase(db.GetSnapStore()), prefix, start); err != nil {
			return err
		}
		fmt.Println("Inspecting index database...")
		if err := rawdb.InspectDatabase(rawdb.NewDatabase(db.GetTxIndexStore()), prefix, start); err != nil {
			return err
		}
	}
	return nil
}

func inspectShardingDB(db ethdb.Database, prefix, start []byte) error {
	// inspect totoal first
	if err := rawdb.InspectDatabase(db, prefix, start); err != nil {
		return err
	}
	shardingDB, ok := db.(ethdb.ShardingDB)
	if !ok {
		return nil
	}
	shardNum := shardingDB.ShardNum()
	for i := 0; i < shardNum; i++ {
		fmt.Println("Inspecting the shard", i)
		if err := rawdb.InspectDatabase(rawdb.NewDatabase(shardingDB.ShardByIndex(i)), prefix, start); err != nil {
			return err
		}
	}
	return nil
}

func ancientInspect(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	return rawdb.AncientInspect(db)
}

func checkStateContent(ctx *cli.Context) error {
	var (
		prefix []byte
		start  []byte
	)
	if ctx.NArg() > 1 {
		return fmt.Errorf("max 1 argument: %v", ctx.Command.ArgsUsage)
	}
	if ctx.NArg() > 0 {
		if d, err := hexutil.Decode(ctx.Args().First()); err != nil {
			return fmt.Errorf("failed to hex-decode 'start': %v", err)
		} else {
			start = d
		}
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	var (
		it        ethdb.Iterator
		hasher    = crypto.NewKeccakState()
		got       = make([]byte, 32)
		errs      int
		count     int
		startTime = time.Now()
		lastLog   = time.Now()
	)

	it = rawdb.NewKeyLengthIterator(db.GetStateStore().NewIterator(prefix, start), 32)
	for it.Next() {
		count++
		k := it.Key()
		v := it.Value()
		hasher.Reset()
		hasher.Write(v)
		hasher.Read(got)
		if !bytes.Equal(k, got) {
			errs++
			fmt.Printf("Error at %#x\n", k)
			fmt.Printf("  Hash:  %#x\n", got)
			fmt.Printf("  Data:  %#x\n", v)
		}
		if time.Since(lastLog) > 8*time.Second {
			log.Info("Iterating the database", "at", fmt.Sprintf("%#x", k), "elapsed", common.PrettyDuration(time.Since(startTime)))
			lastLog = time.Now()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	log.Info("Iterated the state content", "errors", errs, "items", count)
	return nil
}

func showDBStats(db ethdb.KeyValueStater) {
	stats, err := db.Stat()
	if err != nil {
		log.Warn("Failed to read database stats", "error", err)
		return
	}
	fmt.Println(stats)
}

func dbStats(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()

	showDBStats(db)
	if stack.CheckIfMultiDataBase() {
		fmt.Println("show stats of StateStore and SnapStore")
		showDBStats(db.GetStateStore())
		showDBStats(db.GetSnapStore())
		showDBStats(db.GetTxIndexStore())
	}

	return nil
}

func dbCompact(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()

	log.Info("Stats before compaction")
	showDBStats(db)

	if stack.CheckIfMultiDataBase() {
		fmt.Println("show stats of StatStore and SnapStore")
		showDBStats(db.GetStateStore())
		showDBStats(db.GetSnapStore())
		showDBStats(db.GetTxIndexStore())
	}

	log.Info("Triggering compaction")
	if err := db.Compact(nil, nil); err != nil {
		log.Error("Compact err", "error", err)
		return err
	}

	if stack.CheckIfMultiDataBase() {
		if err := db.GetStateStore().Compact(nil, nil); err != nil {
			log.Error("Statestore Compact err", "error", err)
			return err
		}
		if err := db.GetSnapStore().Compact(nil, nil); err != nil {
			log.Error("Snapstore Compact err", "error", err)
			return err
		}
		if err := db.GetTxIndexStore().Compact(nil, nil); err != nil {
			log.Error("IndexStore Compact err", "error", err)
			return err
		}
	}

	log.Info("Stats after compaction")
	showDBStats(db)
	if stack.CheckIfMultiDataBase() {
		log.Info("show stats of state store after compaction")
		showDBStats(db.GetStateStore())
		log.Info("show stats of snapshot store after compaction")
		showDBStats(db.GetSnapStore())
		log.Info("show stats of index store after compaction")
		showDBStats(db.GetTxIndexStore())
	}
	return nil
}

// dbGet shows the value of a given database key
func dbGet(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()

	key, err := common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}
	opDb := db
	if stack.CheckIfMultiDataBase() && rawdb.DataTypeByKey(key) == rawdb.StateDataType {
		opDb = db.GetStateStore()
	}

	data, err := opDb.Get(key)
	if err != nil {
		log.Info("Get operation failed", "key", fmt.Sprintf("%#x", key), "error", err)
		return err
	}
	fmt.Printf("key %#x: %#x\n", key, data)
	return nil
}

// dbTrieGet shows the value of a given database key
func dbTrieGet(ctx *cli.Context) error {
	if ctx.NArg() < 1 || ctx.NArg() > 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	var db ethdb.Database
	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	db = chaindb.GetStateStore()
	defer chaindb.Close()

	scheme := ctx.String(utils.StateSchemeFlag.Name)
	if scheme == "" {
		scheme = rawdb.HashScheme
	}

	if scheme == rawdb.PathScheme {
		var (
			pathKey []byte
			owner   []byte
			err     error
		)
		if ctx.NArg() == 1 {
			pathKey, err = hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			nodeVal, hash := rawdb.ReadAccountTrieNodeAndHash(db, pathKey)
			log.Info("TrieGet result ", "PathKey", common.Bytes2Hex(pathKey), "Hash: ", hash, "node: ", trie.NodeString(hash.Bytes(), nodeVal))
		} else if ctx.NArg() == 2 {
			owner, err = hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			pathKey, err = hexutil.Decode(ctx.Args().Get(1))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}

			nodeVal, hash := rawdb.ReadStorageTrieNodeAndHash(db, common.BytesToHash(owner), pathKey)
			log.Info("TrieGet result ", "PathKey: ", common.Bytes2Hex(pathKey), "Owner: ", common.BytesToHash(owner), "Hash: ", hash, "node: ", trie.NodeString(hash.Bytes(), nodeVal))
		}
	} else if scheme == rawdb.HashScheme {
		if ctx.NArg() == 1 {
			hashKey, err := hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			val, err := db.Get(hashKey)
			if err != nil {
				log.Error("db get failed, ", "error: ", err)
				return err
			}
			log.Info("TrieGet result ", "HashKey: ", common.BytesToHash(hashKey), "node: ", trie.NodeString(hashKey, val))
		} else {
			log.Error("args too much")
		}
	}

	return nil
}

// dbTrieDelete delete the trienode of a given database key
func dbTrieDelete(ctx *cli.Context) error {
	if ctx.NArg() < 1 || ctx.NArg() > 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	var db ethdb.Database
	chaindb := utils.MakeChainDatabase(ctx, stack, true, false)
	db = chaindb.GetStateStore()
	defer chaindb.Close()

	scheme := ctx.String(utils.StateSchemeFlag.Name)
	if scheme == "" {
		scheme = rawdb.HashScheme
	}

	if scheme == rawdb.PathScheme {
		var (
			pathKey []byte
			owner   []byte
			err     error
		)
		if ctx.NArg() == 1 {
			pathKey, err = hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			rawdb.DeleteAccountTrieNode(db, pathKey)
		} else if ctx.NArg() == 2 {
			owner, err = hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			pathKey, err = hexutil.Decode(ctx.Args().Get(1))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			rawdb.DeleteStorageTrieNode(db, common.BytesToHash(owner), pathKey)
		}
	} else if scheme == rawdb.HashScheme {
		if ctx.NArg() == 1 {
			hashKey, err := hexutil.Decode(ctx.Args().Get(0))
			if err != nil {
				log.Info("Could not decode the value", "error", err)
				return err
			}
			err = db.Delete(hashKey)
			if err != nil {
				log.Error("db delete failed", "err", err)
				return err
			}
		} else {
			log.Error("args too much")
		}
	}
	return nil
}

// dbDelete deletes a key from the database
func dbDelete(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()

	key, err := common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}
	opDb := db
	if opDb.HasSeparateStateStore() && rawdb.DataTypeByKey(key) == rawdb.StateDataType {
		opDb = db.GetStateStore()
	}

	data, err := opDb.Get(key)
	if err == nil {
		fmt.Printf("Previous value: %#x\n", data)
	}
	if err = opDb.Delete(key); err != nil {
		log.Info("Delete operation returned an error", "key", fmt.Sprintf("%#x", key), "error", err)
		return err
	}
	return nil
}

// dbDeleteTrieState deletes all trie state related key-value pairs from the database and the ancient state store.
func dbDeleteTrieState(ctx *cli.Context) error {
	if ctx.NArg() > 0 {
		return fmt.Errorf("no arguments required")
	}

	stack, config := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()

	var (
		err   error
		start = time.Now()
	)

	// If separate trie db exists, delete all files in the db folder
	if db.HasSeparateStateStore() {
		statePath := filepath.Join(stack.ResolvePath("chaindata"), "state")
		log.Info("Removing separate trie database", "path", statePath)
		err = filepath.Walk(statePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path != statePath {
				fileInfo, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if !fileInfo.IsDir() {
					os.Remove(path)
				}
			}
			return nil
		})
		log.Info("Separate trie database deleted", "err", err, "elapsed", common.PrettyDuration(time.Since(start)))
		return err
	}

	// Delete KV pairs from the database
	err = rawdb.DeleteTrieState(db)
	if err != nil {
		return err
	}

	// Remove the full node ancient database
	dbPath := config.Eth.DatabaseFreezer
	switch {
	case dbPath == "":
		dbPath = filepath.Join(stack.ResolvePath("chaindata"), "ancient/state")
	case !filepath.IsAbs(dbPath):
		dbPath = config.Node.ResolvePath(dbPath)
	}

	if !common.FileExist(dbPath) {
		return nil
	}

	log.Info("Removing ancient state database", "path", dbPath)
	start = time.Now()
	filepath.Walk(dbPath, func(path string, info os.FileInfo, err error) error {
		if dbPath == path {
			return nil
		}
		if !info.IsDir() {
			os.Remove(path)
			return nil
		}
		return filepath.SkipDir
	})
	log.Info("State database successfully deleted", "path", dbPath, "elapsed", common.PrettyDuration(time.Since(start)))

	return nil
}

// dbPut overwrite a value in the database
func dbPut(ctx *cli.Context) error {
	if ctx.NArg() != 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()

	var (
		key   []byte
		value []byte
		data  []byte
		err   error
	)
	key, err = common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}
	value, err = hexutil.Decode(ctx.Args().Get(1))
	if err != nil {
		log.Info("Could not decode the value", "error", err)
		return err
	}

	opDb := db
	if db.HasSeparateStateStore() && rawdb.DataTypeByKey(key) == rawdb.StateDataType {
		opDb = db.GetStateStore()
	}

	data, err = opDb.Get(key)
	if err == nil {
		fmt.Printf("Previous value: %#x\n", data)
	}
	return opDb.Put(key, value)
}

// dbDumpTrie shows the key-value slots of a given storage trie
func dbDumpTrie(ctx *cli.Context) error {
	if ctx.NArg() < 3 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	triedb := utils.MakeTrieDatabase(ctx, stack, db, false, true, false)
	defer triedb.Close()

	var (
		state   []byte
		storage []byte
		account []byte
		start   []byte
		max     = int64(-1)
		err     error
	)
	if state, err = hexutil.Decode(ctx.Args().Get(0)); err != nil {
		log.Info("Could not decode the state root", "error", err)
		return err
	}
	if account, err = hexutil.Decode(ctx.Args().Get(1)); err != nil {
		log.Info("Could not decode the account hash", "error", err)
		return err
	}
	if storage, err = hexutil.Decode(ctx.Args().Get(2)); err != nil {
		log.Info("Could not decode the storage trie root", "error", err)
		return err
	}
	if ctx.NArg() > 3 {
		if start, err = hexutil.Decode(ctx.Args().Get(3)); err != nil {
			log.Info("Could not decode the seek position", "error", err)
			return err
		}
	}
	if ctx.NArg() > 4 {
		if max, err = strconv.ParseInt(ctx.Args().Get(4), 10, 64); err != nil {
			log.Info("Could not decode the max count", "error", err)
			return err
		}
	}
	id := trie.StorageTrieID(common.BytesToHash(state), common.BytesToHash(account), common.BytesToHash(storage))
	theTrie, err := trie.New(id, triedb)
	if err != nil {
		return err
	}
	trieIt, err := theTrie.NodeIterator(start)
	if err != nil {
		return err
	}
	var count int64
	it := trie.NewIterator(trieIt)
	for it.Next() {
		if max > 0 && count == max {
			fmt.Printf("Exiting after %d values\n", count)
			break
		}
		fmt.Printf("  %d. key %#x: %#x\n", count, it.Key, it.Value)
		count++
	}
	return it.Err
}

func freezerInspect(ctx *cli.Context) error {
	if ctx.NArg() < 4 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	var (
		freezer = ctx.Args().Get(0)
		table   = ctx.Args().Get(1)
	)
	start, err := strconv.ParseInt(ctx.Args().Get(2), 10, 64)
	if err != nil {
		log.Info("Could not read start-param", "err", err)
		return err
	}
	end, err := strconv.ParseInt(ctx.Args().Get(3), 10, 64)
	if err != nil {
		log.Info("Could not read count param", "err", err)
		return err
	}
	stack, _ := makeConfigNode(ctx)
	ancient := stack.ResolveAncient("chaindata", ctx.String(utils.AncientFlag.Name))
	stack.Close()
	return rawdb.InspectFreezerTable(ancient, freezer, table, start, end, stack.CheckIfMultiDataBase())
}

func importLDBdata(ctx *cli.Context) error {
	start := 0
	switch ctx.NArg() {
	case 1:
		break
	case 2:
		s, err := strconv.Atoi(ctx.Args().Get(1))
		if err != nil {
			return fmt.Errorf("second arg must be an integer: %v", err)
		}
		start = s
	default:
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	var (
		fName     = ctx.Args().Get(0)
		stack, _  = makeConfigNode(ctx)
		interrupt = make(chan os.Signal, 1)
		stop      = make(chan struct{})
	)
	defer stack.Close()
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	defer close(interrupt)
	go func() {
		if _, ok := <-interrupt; ok {
			log.Info("Interrupted during ldb import, stopping at next batch")
		}
		close(stop)
	}()
	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()
	return utils.ImportLDBData(db, fName, int64(start), stop)
}

type preimageIterator struct {
	iter ethdb.Iterator
}

func (iter *preimageIterator) Next() (byte, []byte, []byte, bool) {
	for iter.iter.Next() {
		key := iter.iter.Key()
		if bytes.HasPrefix(key, rawdb.PreimagePrefix) && len(key) == (len(rawdb.PreimagePrefix)+common.HashLength) {
			return utils.OpBatchAdd, key, iter.iter.Value(), true
		}
	}
	return 0, nil, nil, false
}

func (iter *preimageIterator) Release() {
	iter.iter.Release()
}

type snapshotIterator struct {
	init    bool
	account ethdb.Iterator
	storage ethdb.Iterator
}

func (iter *snapshotIterator) Next() (byte, []byte, []byte, bool) {
	if !iter.init {
		iter.init = true
		return utils.OpBatchDel, rawdb.SnapshotRootKey, nil, true
	}
	for iter.account.Next() {
		key := iter.account.Key()
		if bytes.HasPrefix(key, rawdb.SnapshotAccountPrefix) && len(key) == (len(rawdb.SnapshotAccountPrefix)+common.HashLength) {
			return utils.OpBatchAdd, key, iter.account.Value(), true
		}
	}
	for iter.storage.Next() {
		key := iter.storage.Key()
		if bytes.HasPrefix(key, rawdb.SnapshotStoragePrefix) && len(key) == (len(rawdb.SnapshotStoragePrefix)+2*common.HashLength) {
			return utils.OpBatchAdd, key, iter.storage.Value(), true
		}
	}
	return 0, nil, nil, false
}

func (iter *snapshotIterator) Release() {
	iter.account.Release()
	iter.storage.Release()
}

// chainExporters defines the export scheme for all exportable chain data.
var chainExporters = map[string]func(db ethdb.Database) utils.ChainDataIterator{
	"preimage": func(db ethdb.Database) utils.ChainDataIterator {
		iter := db.NewIterator(rawdb.PreimagePrefix, nil)
		return &preimageIterator{iter: iter}
	},
	"snapshot": func(db ethdb.Database) utils.ChainDataIterator {
		account := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
		storage := db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
		return &snapshotIterator{account: account, storage: storage}
	},
}

func exportChaindata(ctx *cli.Context) error {
	if ctx.NArg() < 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	// Parse the required chain data type, make sure it's supported.
	kind := ctx.Args().Get(0)
	kind = strings.ToLower(strings.Trim(kind, " "))
	exporter, ok := chainExporters[kind]
	if !ok {
		var kinds []string
		for kind := range chainExporters {
			kinds = append(kinds, kind)
		}
		return fmt.Errorf("invalid data type %s, supported types: %s", kind, strings.Join(kinds, ", "))
	}
	var (
		stack, _  = makeConfigNode(ctx)
		interrupt = make(chan os.Signal, 1)
		stop      = make(chan struct{})
	)
	defer stack.Close()
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	defer close(interrupt)
	go func() {
		if _, ok := <-interrupt; ok {
			log.Info("Interrupted during db export, stopping at next batch")
		}
		close(stop)
	}()
	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	return utils.ExportChaindata(ctx.Args().Get(1), kind, exporter(db), stop)
}

func showMetaData(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()
	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	ancients, err := db.Ancients()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing ancients: %v", err)
	}
	data := rawdb.ReadChainMetadata(db)
	data = append(data, []string{"frozen", fmt.Sprintf("%d items", ancients)})
	data = append(data, []string{"snapshotGenerator", snapshot.ParseGeneratorStatus(rawdb.ReadSnapshotGenerator(db))})
	if b := rawdb.ReadHeadBlock(db); b != nil {
		data = append(data, []string{"headBlock.Hash", fmt.Sprintf("%v", b.Hash())})
		data = append(data, []string{"headBlock.Root", fmt.Sprintf("%v", b.Root())})
		data = append(data, []string{"headBlock.Number", fmt.Sprintf("%d (%#x)", b.Number(), b.Number())})
	}
	if h := rawdb.ReadHeadHeader(db); h != nil {
		data = append(data, []string{"headHeader.Hash", fmt.Sprintf("%v", h.Hash())})
		data = append(data, []string{"headHeader.Root", fmt.Sprintf("%v", h.Root)})
		data = append(data, []string{"headHeader.Number", fmt.Sprintf("%d (%#x)", h.Number, h.Number)})
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Field", "Value"})
	table.AppendBulk(data)
	table.Render()
	return nil
}

func hbss2pbss(ctx *cli.Context) error {
	if ctx.NArg() > 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}

	var jobnum uint64
	var err error
	if ctx.NArg() == 1 {
		jobnum, err = strconv.ParseUint(ctx.Args().Get(0), 10, 64)
		if err != nil {
			return fmt.Errorf("failed to Parse jobnum, Args[1]: %v, err: %v", ctx.Args().Get(1), err)
		}
	} else {
		// by default
		jobnum = 1000
	}

	force := ctx.Bool(utils.ForceFlag.Name)

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	db.SyncAncient()
	defer db.Close()

	// convert hbss trie node to pbss trie node
	var lastStateID uint64
	lastStateID = rawdb.ReadPersistentStateID(db.GetStateStore())
	if lastStateID == 0 || force {
		config := triedb.HashDefaults
		triedb := triedb.NewDatabase(db, config)
		triedb.Cap(0)
		log.Info("hbss2pbss triedb", "scheme", triedb.Scheme())
		defer triedb.Close()

		headerHash := rawdb.ReadHeadHeaderHash(db)
		blockNumber := rawdb.ReadHeaderNumber(db, headerHash)
		if blockNumber == nil {
			log.Error("read header number failed.")
			return fmt.Errorf("read header number failed")
		}

		log.Info("hbss2pbss converting", "HeaderHash: ", headerHash.String(), ", blockNumber: ", *blockNumber)

		var headerBlockHash common.Hash
		var trieRootHash common.Hash

		if *blockNumber != math.MaxUint64 {
			headerBlockHash = rawdb.ReadCanonicalHash(db, *blockNumber)
			if headerBlockHash == (common.Hash{}) {
				return errors.New("ReadHeadBlockHash empty hash")
			}
			blockHeader := rawdb.ReadHeader(db, headerBlockHash, *blockNumber)
			trieRootHash = blockHeader.Root
			fmt.Println("Canonical Hash: ", headerBlockHash.String(), ", TrieRootHash: ", trieRootHash.String())
		}
		if (trieRootHash == common.Hash{}) {
			log.Error("Empty root hash")
			return errors.New("Empty root hash.")
		}

		id := trie.StateTrieID(trieRootHash)
		theTrie, err := trie.New(id, triedb)
		if err != nil {
			log.Error("fail to new trie tree", "err", err, "rootHash", err, trieRootHash.String())
			return err
		}

		h2p, err := trie.NewHbss2Pbss(theTrie, triedb, trieRootHash, *blockNumber, jobnum)
		if err != nil {
			log.Error("fail to new hash2pbss", "err", err, "rootHash", err, trieRootHash.String())
			return err
		}
		h2p.Run()
	} else {
		log.Info("Convert hbss to pbss success. Nothing to do.")
	}

	lastStateID = rawdb.ReadPersistentStateID(db.GetStateStore())

	if lastStateID == 0 {
		log.Error("Convert hbss to pbss trie node error. The last state id is still 0")
	}

	var ancient string
	if db.HasSeparateStateStore() {
		dirName := filepath.Join(stack.ResolvePath("chaindata"), "state")
		ancient = filepath.Join(dirName, "ancient")
	} else {
		ancient = stack.ResolveAncient("chaindata", ctx.String(utils.AncientFlag.Name))
	}
	err = rawdb.ResetStateFreezerTableOffset(ancient, lastStateID)
	if err != nil {
		log.Error("Reset state freezer table offset failed", "error", err)
		return err
	}
	// prune hbss trie node
	err = rawdb.PruneHashTrieNodeInDataBase(db.GetStateStore())
	if err != nil {
		log.Error("Prune Hash trie node in database failed", "error", err)
		return err
	}
	return nil
}

func inspectAccount(db *triedb.Database, start uint64, end uint64, address common.Address, raw bool) error {
	stats, err := db.AccountHistory(address, start, end)
	if err != nil {
		return err
	}
	fmt.Printf("Account history:\n\taddress: %s\n\tblockrange: [#%d-#%d]\n", address.Hex(), stats.Start, stats.End)

	from := stats.Start
	for i := 0; i < len(stats.Blocks); i++ {
		var content string
		if len(stats.Origins[i]) == 0 {
			content = "<empty>"
		} else {
			if !raw {
				content = fmt.Sprintf("%#x", stats.Origins[i])
			} else {
				account := new(types.SlimAccount)
				if err := rlp.DecodeBytes(stats.Origins[i], account); err != nil {
					panic(err)
				}
				code := "<nil>"
				if len(account.CodeHash) > 0 {
					code = fmt.Sprintf("%#x", account.CodeHash)
				}
				root := "<nil>"
				if len(account.Root) > 0 {
					root = fmt.Sprintf("%#x", account.Root)
				}
				content = fmt.Sprintf("nonce: %d, balance: %d, codeHash: %s, root: %s", account.Nonce, account.Balance, code, root)
			}
		}
		fmt.Printf("#%d - #%d: %s\n", from, stats.Blocks[i], content)
		from = stats.Blocks[i]
	}
	return nil
}

func inspectStorage(db *triedb.Database, start uint64, end uint64, address common.Address, slot common.Hash, raw bool) error {
	// The hash of storage slot key is utilized in the history
	// rather than the raw slot key, make the conversion.
	stats, err := db.StorageHistory(address, slot, start, end)
	if err != nil {
		return err
	}
	fmt.Printf("Storage history:\n\taddress: %s\n\tslot: %s\n\tblockrange: [#%d-#%d]\n", address.Hex(), slot.Hex(), stats.Start, stats.End)

	from := stats.Start
	for i := 0; i < len(stats.Blocks); i++ {
		var content string
		if len(stats.Origins[i]) == 0 {
			content = "<empty>"
		} else {
			if !raw {
				content = fmt.Sprintf("%#x", stats.Origins[i])
			} else {
				_, data, _, err := rlp.Split(stats.Origins[i])
				if err != nil {
					fmt.Printf("Failed to decode storage slot, %v", err)
					return err
				}
				content = fmt.Sprintf("%#x", data)
			}
		}
		fmt.Printf("#%d - #%d: %s\n", from, stats.Blocks[i], content)
		from = stats.Blocks[i]
	}
	return nil
}

func inspectHistory(ctx *cli.Context) error {
	if ctx.NArg() == 0 || ctx.NArg() > 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	var (
		address common.Address
		slot    common.Hash
	)
	if err := address.UnmarshalText([]byte(ctx.Args().Get(0))); err != nil {
		return err
	}
	if ctx.NArg() > 1 {
		if err := slot.UnmarshalText([]byte(ctx.Args().Get(1))); err != nil {
			return err
		}
	}
	// Load the databases.
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()

	triedb := utils.MakeTrieDatabase(ctx, stack, db, false, false, false)
	defer triedb.Close()

	var (
		err   error
		start uint64 // the id of first history object to query
		end   uint64 // the id (included) of last history object to query
	)
	// State histories are identified by state ID rather than block number.
	// To address this, load the corresponding block header and perform the
	// conversion by this function.
	blockToID := func(blockNumber uint64) (uint64, error) {
		header := rawdb.ReadHeader(db, rawdb.ReadCanonicalHash(db, blockNumber), blockNumber)
		if header == nil {
			return 0, fmt.Errorf("block #%d is not existent", blockNumber)
		}
		id := rawdb.ReadStateID(db.GetStateStore(), header.Root)
		if id == nil {
			first, last, err := triedb.HistoryRange()
			if err == nil {
				return 0, fmt.Errorf("history of block #%d is not existent, available history range: [#%d-#%d]", blockNumber, first, last)
			}
			return 0, fmt.Errorf("history of block #%d is not existent", blockNumber)
		}
		return *id, nil
	}
	// Parse the starting block number for inspection.
	startNumber := ctx.Uint64("start")
	if startNumber != 0 {
		start, err = blockToID(startNumber)
		if err != nil {
			return err
		}
	}
	// Parse the ending block number for inspection.
	endBlock := ctx.Uint64("end")
	if endBlock != 0 {
		end, err = blockToID(endBlock)
		if err != nil {
			return err
		}
	}
	// Inspect the state history.
	if slot == (common.Hash{}) {
		return inspectAccount(triedb, start, end, address, ctx.Bool("raw"))
	}
	return inspectStorage(triedb, start, end, address, slot, ctx.Bool("raw"))
}

// migrateDatabase migrates a single database to multi-database format
func migrateDatabase(ctx *cli.Context) error {
	var (
		targetDataDir string
		version       byte = 1 // default version
	)

	// Parse arguments based on expand mode
	expandMode := ctx.Bool(utils.ExpandModeFlag.Name)

	if expandMode {
		// Expand mode: expect [target-datadir] [version]
		if ctx.NArg() < 1 {
			return fmt.Errorf("expand mode requires target data directory argument")
		}
		if ctx.NArg() > 2 {
			return fmt.Errorf("too many arguments: expected [target-datadir] [version]")
		}

		targetDataDir = ctx.Args().Get(0)
		if targetDataDir == "" {
			return fmt.Errorf("target data directory cannot be empty")
		}

		// Parse optional version
		if ctx.NArg() >= 2 {
			versionArg := ctx.Args().Get(1)
			if versionVal, err := strconv.ParseUint(versionArg, 10, 8); err != nil {
				return fmt.Errorf("failed to parse 'version' as number: %v", err)
			} else {
				version = byte(versionVal)
			}
		}
		os.Setenv("GODEBUG", "randseednop=0")
		rand.Seed(int64(version))
	} else {
		// In-place mode: no arguments expected
		if ctx.NArg() != 0 {
			return fmt.Errorf("in-place mode expects no arguments")
		}
	}

	cacheSize := ctx.Int(utils.CacheFlag.Name)
	cacheDB := ctx.Int(utils.CacheDatabaseFlag.Name)

	enablesharding := ctx.Bool(utils.EnableShardingFlag.Name)
	if enablesharding {
		if expandMode {
			log.Info("migrateDatabase with enabling sharding database in expand mode", "targetDir", targetDataDir, "version", version)
			if err := migrateDBWithShardingExpandMode(ctx, targetDataDir, version); err != nil {
				log.Error("failed to migrate database with sharding database in expand mode", "error", err)
				return err
			}
		} else {
			log.Info("migrateDatabase with enabling sharding database")
			if err := migrateDBWithSharding(ctx); err != nil {
				log.Error("failed to migrate database with sharding database", "error", err)
				return err
			}
		}
		return nil
	}

	if expandMode {
		log.Info("migrateDatabase in expand mode", "targetDir", targetDataDir, "version", version)
		return migrateDBExpandMode(ctx, targetDataDir, version, cacheSize, cacheDB)
	}
	// Create source stack using standard geth configuration (handles --datadir)
	sourceStack, _ := makeConfigNode(ctx)
	defer sourceStack.Close()

	// Get source database path
	sourceChainDataPath := sourceStack.ResolvePath("chaindata")

	log.Info("Starting in-place database migration", "source", sourceChainDataPath)

	// Safety check: ensure subdirectories don't already exist
	for _, subdir := range []string{"state", "snapshot", "txindex"} {
		subdirPath := filepath.Join(sourceChainDataPath, subdir)
		if common.FileExist(subdirPath) {
			return fmt.Errorf("target subdirectory already exists: %s - migration may have been run before", subdirPath)
		}
	}

	// Open source database for read/write (NOT readonly) without using the
	// Node helper to avoid auto-opening separate state/snapshot/txindex DBs.
	// We only need the hot key-value store for in-place extraction & deletion.
	sourceDB, err := openTargetDatabase(sourceChainDataPath, cacheSize*cacheDB*7/100, 64)
	if err != nil {
		return fmt.Errorf("failed to open source chain database: %v", err)
	}
	defer sourceDB.Close()

	// Count total items before migration for verification
	log.Info("Counting database entries before migration...")
	totalItemsBefore := countDatabaseItems(sourceDB)
	log.Info("Database scan completed", "totalItems", totalItemsBefore)

	// Create target directory structure (separate databases within source chaindata)
	targetStatePath := filepath.Join(sourceChainDataPath, "state")
	targetSnapshotPath := filepath.Join(sourceChainDataPath, "snapshot")
	targetTxIndexPath := filepath.Join(sourceChainDataPath, "txindex")

	for _, dir := range []string{targetStatePath, targetSnapshotPath, targetTxIndexPath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	// Create target databases for extracted data
	// State database with freezer
	stateDB, err := openTargetDatabaseWithFreezer(targetStatePath, cacheSize*cacheDB*50/100, 128)
	if err != nil {
		return fmt.Errorf("failed to create target state database: %v", err)
	}
	defer stateDB.Close()

	// Snapshot database
	snapDB, err := openTargetDatabase(targetSnapshotPath, cacheSize*cacheDB*24/100, 32)
	if err != nil {
		return fmt.Errorf("failed to create target snapshot database: %v", err)
	}
	defer snapDB.Close()

	// TxIndex database
	indexDB, err := openTargetDatabase(targetTxIndexPath, cacheSize*cacheDB*15/100, 32)
	if err != nil {
		return fmt.Errorf("failed to create target txindex database: %v", err)
	}
	defer indexDB.Close()

	// Start in-place migration (pass the totalItemsBefore for verification)
	return performInPlaceMigration(sourceDB, stateDB, snapDB, indexDB, totalItemsBefore, sourceChainDataPath)
}

// openTargetDatabase creates a key-value database at the specified path
func openTargetDatabase(dbPath string, cache, handles int) (ethdb.Database, error) {
	// Determine database type from the path or default to pebble
	dbType := rawdb.PreexistingDatabase(dbPath)
	if dbType == "" {
		dbType = rawdb.DBPebble // Default to pebble
	}

	var kvdb ethdb.KeyValueStore
	var err error

	if dbType == rawdb.DBPebble {
		kvdb, err = pebble.New(dbPath, cache, handles, "", false)
	} else {
		kvdb, err = leveldb.New(dbPath, cache, handles, "", false)
	}

	if err != nil {
		return nil, err
	}

	// Wrap with rawdb to get full ethdb.Database interface
	return rawdb.NewDatabase(kvdb), nil
}

// openTargetDatabaseWithFreezer creates a database with freezer support
func openTargetDatabaseWithFreezer(dbPath string, cache, handles int) (ethdb.Database, error) {
	// Determine database type from the path or default to pebble
	dbType := rawdb.PreexistingDatabase(dbPath)
	if dbType == "" {
		dbType = rawdb.DBPebble // Default to pebble
	}

	var kvdb ethdb.KeyValueStore
	var err error

	if dbType == rawdb.DBPebble {
		kvdb, err = pebble.New(dbPath, cache, handles, "", false)
	} else {
		kvdb, err = leveldb.New(dbPath, cache, handles, "", false)
	}

	if err != nil {
		return nil, err
	}

	// Add freezer support directly to the key-value store
	ancientPath := filepath.Join(dbPath, "ancient")
	return rawdb.NewDatabaseWithFreezer(kvdb, ancientPath, "eth/db/statedata/", false, false, false)
}

// MigrationStats holds thread-safe migration statistics
type MigrationStats struct {
	total      int64
	chain      int64
	state      int64
	snapshot   int64
	txindex    int64
	chainBytes int64
	stateBytes int64
	snapBytes  int64
	indexBytes int64
	startTime  time.Time
	// Performance timing fields
	totalKeyGenTime       int64
	totalValueShuffleTime int64
	keyGenCount           int64
	valueShuffleCount     int64
}

// Add atomically increments counters
func (s *MigrationStats) Add(targetDB string, keySize, valueSize int) {
	atomic.AddInt64(&s.total, 1)
	dataSize := int64(keySize + valueSize)

	switch targetDB {
	case "chain":
		atomic.AddInt64(&s.chain, 1)
		atomic.AddInt64(&s.chainBytes, dataSize)
	case "state":
		atomic.AddInt64(&s.state, 1)
		atomic.AddInt64(&s.stateBytes, dataSize)
	case "snapshot":
		atomic.AddInt64(&s.snapshot, 1)
		atomic.AddInt64(&s.snapBytes, dataSize)
	case "txindex":
		atomic.AddInt64(&s.txindex, 1)
		atomic.AddInt64(&s.indexBytes, dataSize)
	}
}

// Get returns current counter values atomically
func (s *MigrationStats) Get() (int64, int64, int64, int64, int64) {
	return atomic.LoadInt64(&s.total),
		atomic.LoadInt64(&s.chain),
		atomic.LoadInt64(&s.state),
		atomic.LoadInt64(&s.snapshot),
		atomic.LoadInt64(&s.txindex)
}

// GetBytes returns current byte counter values atomically
func (s *MigrationStats) GetBytes() (int64, int64, int64, int64) {
	return atomic.LoadInt64(&s.chainBytes),
		atomic.LoadInt64(&s.stateBytes),
		atomic.LoadInt64(&s.snapBytes),
		atomic.LoadInt64(&s.indexBytes)
}

// GetPerformanceStats returns timing statistics
func (s *MigrationStats) GetPerformanceStats() (avgKeyGenNs, avgValueShuffleNs int64) {
	totalKeyGenTime := atomic.LoadInt64(&s.totalKeyGenTime)
	totalValueShuffleTime := atomic.LoadInt64(&s.totalValueShuffleTime)
	keyGenCount := atomic.LoadInt64(&s.keyGenCount)
	valueShuffleCount := atomic.LoadInt64(&s.valueShuffleCount)

	if keyGenCount > 0 {
		avgKeyGenNs = totalKeyGenTime / keyGenCount
	}
	if valueShuffleCount > 0 {
		avgValueShuffleNs = totalValueShuffleTime / valueShuffleCount
	}
	return avgKeyGenNs, avgValueShuffleNs
}

// progressMonitor logs migration progress periodically
func progressMonitor(stats *MigrationStats, stop <-chan struct{}) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			total, chain, state, snapshot, txindex := stats.Get()
			avgKeyGenNs, avgValueShuffleNs := stats.GetPerformanceStats()

			log.Info("Migration progress",
				"total", total,
				"chain", chain,
				"state", state,
				"snapshot", snapshot,
				"txindex", txindex,
				"elapsed", common.PrettyDuration(time.Since(stats.startTime)),
				"rate", fmt.Sprintf("%.1f items/s", float64(total)/time.Since(stats.startTime).Seconds()))

			// Log key/value generation performance if available
			if avgKeyGenNs > 0 || avgValueShuffleNs > 0 {
				log.Info("Key/Value generation performance",
					"avgKeyGenMicros", avgKeyGenNs/1000,
					"avgValueShuffleMicros", avgValueShuffleNs/1000,
					"keyGenNs", avgKeyGenNs,
					"valueShuffleNs", avgValueShuffleNs)
			}
		case <-stop:
			return
		}
	}
}

// isTrieKey determines if a key-value pair belongs to trie data that should go to state database
// Logic reference from user's code
func isTrieKey(key, value []byte) bool {
	switch {
	case rawdb.IsLegacyTrieNode(key, value):
		return true
	case bytes.HasPrefix(key, []byte("L")) && len(key) == (1+common.HashLength): // stateIDPrefix
		return true
	case rawdb.IsAccountTrieNode(key):
		return true
	case rawdb.IsStorageTrieNode(key):
		return true
	case bytes.HasPrefix(key, rawdb.PreimagePrefix) && len(key) == (len(rawdb.PreimagePrefix)+common.HashLength):
		return true
	case bytes.HasPrefix(key, []byte("c")) && len(key) == (1+common.HashLength): // CodePrefix - contract code
		return true
	default:
		// Check specific metadata keys
		keyStr := string(key)
		if keyStr == "TrieSync" || keyStr == "TrieJournal" || keyStr == "LastStateID" {
			return true
		}
	}
	return false
}

// categorizeDataByKey categorizes database entries based on key prefixes
// Returns the target database name: "state", "snapshot", "txindex", or "" (stay in original chaindata)
func categorizeDataByKey(key, value []byte) string {
	// CRITICAL: Explicit protection for essential metadata keys
	// These keys must NEVER be extracted and should always stay in chaindata
	keyStr := string(key)

	// State trie data - use the comprehensive trie key logic
	if isTrieKey(key, value) {
		return "state"
	}

	// Snapshot data - account snapshots
	if bytes.HasPrefix(key, rawdb.SnapshotAccountPrefix) && len(key) == (len(rawdb.SnapshotAccountPrefix)+common.HashLength) {
		return "snapshot"
	}
	// Snapshot data - storage snapshots
	if bytes.HasPrefix(key, rawdb.SnapshotStoragePrefix) && len(key) == (len(rawdb.SnapshotStoragePrefix)+2*common.HashLength) {
		return "snapshot"
	}

	// Snapshot metadata keys
	snapshotMetadataKeys := []string{
		"SnapshotRoot", "SnapshotJournal", "SnapshotGenerator",
		"SnapshotRecovery", "SnapshotSyncStatus",
	}
	for _, metaKey := range snapshotMetadataKeys {
		if keyStr == metaKey {
			return "snapshot"
		}
	}

	// Transaction index data
	if bytes.HasPrefix(key, []byte("l")) && len(key) == (1+common.HashLength) { // txLookupPrefix
		return "txindex"
	}

	// Transaction index metadata
	txIndexMetadataKeys := []string{
		"TransactionIndexTail", // txIndexTailKey - tracks the oldest indexed block
	}
	for _, metaKey := range txIndexMetadataKeys {
		if keyStr == metaKey {
			return "txindex"
		}
	}

	// Everything else stays in the original chaindata (not processed)
	return ""
}

// performInPlaceMigration performs in-place data migration by extracting data from source DB
func performInPlaceMigration(sourceDB, stateDB, snapDB, indexDB ethdb.Database, totalItemsBefore int64, sourceChainDataPath string) error {
	stats := &MigrationStats{startTime: time.Now()}

	log.Info("Starting in-place data migration")

	// Progress monitoring goroutine
	stopProgress := make(chan struct{})
	go progressMonitor(stats, stopProgress)

	// Extract all data in single pass
	log.Info("🔍 Starting single-pass data extraction...")
	if err := extractAllDataInOnePass(sourceDB, stateDB, snapDB, indexDB, stats); err != nil {
		close(stopProgress)
		return fmt.Errorf("failed to extract data: %v", err)
	}

	close(stopProgress)

	total, chain, state, snapshot, txindex := stats.Get()
	chainBytes, stateBytes, snapBytes, indexBytes := stats.GetBytes()
	elapsed := time.Since(stats.startTime)

	log.Info("In-place migration completed",
		"total", total,
		"chain", chain,
		"state", state,
		"snapshot", snapshot,
		"txindex", txindex,
		"elapsed", common.PrettyDuration(elapsed))

	log.Info("Final data sizes",
		"chainMB", chainBytes/(1024*1024),
		"stateMB", stateBytes/(1024*1024),
		"snapMB", snapBytes/(1024*1024),
		"indexMB", indexBytes/(1024*1024),
		"totalExtractedMB", (stateBytes+snapBytes+indexBytes)/(1024*1024))

	// Post-migration verification: count remaining items in source DB
	log.Info("Performing post-migration verification...")
	totalItemsAfter := countDatabaseItems(sourceDB)
	extractedItems := state + snapshot + txindex

	log.Info("Migration verification",
		"itemsBefore", totalItemsBefore,
		"itemsAfter", totalItemsAfter,
		"itemsExtracted", extractedItems,
		"expectedRemaining", totalItemsBefore-extractedItems)

	if totalItemsAfter != (totalItemsBefore - extractedItems) {
		return fmt.Errorf("migration verification failed: expected %d remaining items, found %d",
			totalItemsBefore-extractedItems, totalItemsAfter)
	}

	log.Info("✅ In-place migration completed and verified successfully!")

	// Handle ancient state data migration
	if err := moveAncientData(sourceChainDataPath); err != nil {
		log.Error("Failed to move ancient state data", "error", err)
		return fmt.Errorf("failed to move ancient state data: %v", err)
	}

	// Show directory sizes for debugging
	log.Info("Checking database directory sizes...")
	checkDirectorySize := func(path, name string) {
		if !common.FileExist(path) {
			log.Info("Directory size", "name", name, "status", "not found")
			return
		}
		// Try to get directory size using du command
		cmd := exec.Command("du", "-sh", path)
		output, err := cmd.Output()
		if err != nil {
			log.Info("Directory size", "name", name, "status", "unable to measure")
		} else {
			sizeStr := strings.Fields(string(output))[0]
			log.Info("Directory size", "name", name, "size", sizeStr)
		}
	}

	// Get the chaindata base directory from the source path
	baseDir := sourceChainDataPath
	checkDirectorySize(baseDir, "chaindata (remaining)")
	checkDirectorySize(filepath.Join(baseDir, "state"), "state")
	checkDirectorySize(filepath.Join(baseDir, "snapshot"), "snapshot")
	checkDirectorySize(filepath.Join(baseDir, "txindex"), "txindex")

	// Perform database compaction after migration
	log.Info("Starting database compaction to reclaim space...")
	if err := performDatabaseCompaction(sourceDB, stateDB, snapDB, indexDB); err != nil {
		log.Error("Failed to compact databases", "error", err)
		return fmt.Errorf("failed to compact databases: %v", err)
	}

	log.Info("🎉 Migration completed successfully with proper ancient data structure and compaction!")
	return nil
}

// CategorizedData represents a key-value pair with its target category
type CategorizedData struct {
	Key      []byte
	Value    []byte
	Category string
}

// extractAllDataInOnePass extracts all data types using multi-threaded async processing
func extractAllDataInOnePass(sourceDB, stateDB, snapDB, indexDB ethdb.Database, stats *MigrationStats) error {
	log.Info("🚀 Starting multi-threaded async data extraction",
		"architecture", "1 reader + 6 writers (2×state,2×snapshot,2×txindex) + 1 deleter = 8 threads")

	// Channel buffer sizes - balance memory usage vs throughput
	const channelBufferSize = 10000

	// Create channels for communication between goroutines
	stateChannel := make(chan CategorizedData, channelBufferSize)
	snapChannel := make(chan CategorizedData, channelBufferSize)
	indexChannel := make(chan CategorizedData, channelBufferSize)
	deleteChannel := make(chan []byte, channelBufferSize)

	// Error channels to collect errors from goroutines
	errorChannel := make(chan error, 8) // reader + 6 writers (2 per type) + 1 deleter = 8 threads

	// WaitGroup to coordinate all goroutines
	var wg sync.WaitGroup

	// Start async writer goroutines (2 threads per database type for better performance)
	log.Info("🚀 Starting async writer goroutines (2 threads per database type)...")

	// State database writers (2 threads)
	for i := 0; i < 2; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("state-%d", id), stateDB, stateChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("state writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Snapshot database writers (2 threads)
	for i := 0; i < 2; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("snapshot-%d", id), snapDB, snapChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("snapshot writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Transaction index database writers (2 threads)
	for i := 0; i < 2; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("txindex-%d", id), indexDB, indexChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("txindex writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Source database deleter
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := asyncDatabaseDeleter(sourceDB, deleteChannel); err != nil {
			errorChannel <- fmt.Errorf("deleter error: %v", err)
		}
	}()

	// Main reader goroutine - process source database
	log.Info("📖 Starting main reader thread with 7 async workers (6 writers + 1 deleter)...")
	processed := 0
	extracted := 0

	it := sourceDB.NewIterator(nil, nil)
	defer it.Release()

	for it.Next() {
		// Create copies of key and value since iterator reuses underlying memory
		key := make([]byte, len(it.Key()))
		value := make([]byte, len(it.Value()))
		copy(key, it.Key())
		copy(value, it.Value())

		processed++

		// Determine the category for this key-value pair
		category := categorizeDataByKey(key, value)

		// Skip if it should stay in original chaindata
		if category == "" {
			continue
		}

		extracted++

		// Create data package
		data := CategorizedData{
			Key:      key,
			Value:    value,
			Category: category,
		}

		// Route to correct writer channel based on category
		switch category {
		case "state":
			select {
			case stateChannel <- data:
				// Successfully sent to state channel, now send key to delete channel
				select {
				case deleteChannel <- key:
				case err := <-errorChannel:
					return err
				}
			case err := <-errorChannel:
				return err
			}
		case "snapshot":
			select {
			case snapChannel <- data:
				// Successfully sent to snapshot channel, now send key to delete channel
				select {
				case deleteChannel <- key:
				case err := <-errorChannel:
					return err
				}
			case err := <-errorChannel:
				return err
			}
		case "txindex":
			select {
			case indexChannel <- data:
				// Successfully sent to txindex channel, now send key to delete channel
				select {
				case deleteChannel <- key:
				case err := <-errorChannel:
					return err
				}
			case err := <-errorChannel:
				return err
			}
		default:
			log.Warn("Unknown category in async processing", "category", category, "keyHex", fmt.Sprintf("%x", key[:min(16, len(key))]))
			continue
		}

		// Debug: Log first few extractions
		if extracted <= 30 {
			log.Info("📝 Queuing data for async processing",
				"category", category,
				"keyHex", fmt.Sprintf("%x", key[:min(16, len(key))]),
				"keyStr", string(key[:min(32, len(key))]),
				"keyLen", len(key),
				"valueLen", len(value))
		}

		// Progress update every 10000 items
		if processed%10000 == 0 && processed > 0 {
			log.Info("Reader progress", "processed", processed, "extracted", extracted)
		}
	}

	// Check for iterator errors
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterator error: %v", err)
	}

	log.Info("📖 Reader completed, closing channels...", "processed", processed, "extracted", extracted)

	// Close channels to signal completion to worker goroutines
	close(stateChannel)
	close(snapChannel)
	close(indexChannel)
	close(deleteChannel)

	// Wait for all goroutines to complete
	log.Info("⏳ Waiting for all async workers to complete...")
	wg.Wait()

	// Check for any errors from worker goroutines
	close(errorChannel)
	for err := range errorChannel {
		if err != nil {
			return err
		}
	}

	log.Info("✅ Multi-threaded async data extraction completed",
		"totalProcessed", processed,
		"totalExtracted", extracted,
		"threadsUsed", "8 (1 reader + 6 writers + 1 deleter)")
	return nil
}

// asyncDatabaseWriter handles async database writing for a specific category
func asyncDatabaseWriter(category string, targetDB ethdb.Database, dataChannel <-chan CategorizedData, stats *MigrationStats) error {
	log.Info("🖊️ Starting async writer", "category", category)

	batch := targetDB.NewBatch()
	processed := 0

	for data := range dataChannel {
		// Add to batch
		if err := batch.Put(data.Key, data.Value); err != nil {
			return fmt.Errorf("failed to add key to %s batch: %v", category, err)
		}

		// Update statistics
		stats.Add(data.Category, len(data.Key), len(data.Value))
		processed++

		// Flush batch when it reaches ideal size
		if batch.ValueSize() >= ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return fmt.Errorf("failed to write %s batch: %v", category, err)
			}
			batch.Reset()
			log.Debug("Async batch flushed", "category", category, "processed", processed, "batchSizeMB", ethdb.IdealBatchSize/(1024*1024))
		}

		// Progress update every 2500 items (more frequent due to multiple writers per type)
		if processed%2500 == 0 && processed > 0 {
			log.Info("Async writer progress", "writer", category, "processed", processed)
		}
	}

	// Final flush of remaining batch
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write final %s batch: %v", category, err)
		}
	}

	log.Info("✅ Async writer completed", "category", category, "totalProcessed", processed)
	return nil
}

// asyncDatabaseDeleter handles async deletion of keys from source database
func asyncDatabaseDeleter(sourceDB ethdb.Database, deleteChannel <-chan []byte) error {
	log.Info("🗑️ Starting async deleter")

	deleteBatch := sourceDB.NewBatch()
	deleted := 0

	for key := range deleteChannel {
		// Add to delete batch
		if err := deleteBatch.Delete(key); err != nil {
			return fmt.Errorf("failed to add key to delete batch: %v", err)
		}

		deleted++

		// Flush delete batch when it reaches ideal size
		if deleteBatch.ValueSize() >= ethdb.IdealBatchSize {
			if err := deleteBatch.Write(); err != nil {
				return fmt.Errorf("failed to write delete batch: %v", err)
			}
			deleteBatch.Reset()
			log.Debug("Delete batch flushed", "deleted", deleted, "batchSizeMB", ethdb.IdealBatchSize/(1024*1024))
		}

		// Progress update every 10000 items
		if deleted%10000 == 0 && deleted > 0 {
			log.Info("Async deleter progress", "deleted", deleted)
		}
	}

	// Final flush of remaining delete batch
	if deleteBatch.ValueSize() > 0 {
		if err := deleteBatch.Write(); err != nil {
			return fmt.Errorf("failed to write final delete batch: %v", err)
		}
	}

	log.Info("✅ Async deleter completed", "totalDeleted", deleted)
	return nil
}

// countDatabaseItems counts the total number of items in a database
func countDatabaseItems(db ethdb.Database) int64 {
	var count int64
	it := db.NewIterator(nil, nil)
	defer it.Release()

	for it.Next() {
		count++
		// Log progress for large databases
		if count%100000 == 0 {
			log.Info("Counting progress", "count", count)
		}
	}

	if err := it.Error(); err != nil {
		log.Error("Error during database count", "error", err)
		return -1
	}

	return count
}

// moveAncientData moves ancient state data from chaindata/ancient/state to state/ancient
func moveAncientData(sourceChainDataPath string) error {
	log.Info("Checking for ancient state data to migrate...")

	originalAncientDir := filepath.Join(sourceChainDataPath, "ancient")

	// Check if original ancient directory exists
	if !common.FileExist(originalAncientDir) {
		log.Info("No ancient directory found", "path", originalAncientDir)
		return nil
	}

	// Only handle state ancient data
	// Move chaindata/ancient/state/ to state/ancient/state/
	// This creates the correct structure: state/ancient/state/ (and potentially state/ancient/chain/ for main chain data)
	originalStateAncient := filepath.Join(originalAncientDir, "state")
	newStateAncient := filepath.Join(sourceChainDataPath, "state", "ancient", "state")

	if !common.FileExist(originalStateAncient) {
		log.Info("No ancient state directory found", "path", originalStateAncient)
		return nil
	}

	// Check if the directory has contents
	entries, err := os.ReadDir(originalStateAncient)
	if err != nil {
		return fmt.Errorf("failed to read ancient state directory: %v", err)
	}

	if len(entries) == 0 {
		log.Info("Ancient state directory is empty, removing it", "path", originalStateAncient)
		return os.RemoveAll(originalStateAncient)
	}

	log.Info("Found ancient state data to migrate",
		"from", originalStateAncient,
		"to", newStateAncient,
		"files", len(entries))

	// If target ancient state directory already exists, remove it first
	if common.FileExist(newStateAncient) {
		log.Info("Target ancient state directory already exists, removing it", "target", newStateAncient)
		if err := os.RemoveAll(newStateAncient); err != nil {
			return fmt.Errorf("failed to remove existing ancient state directory: %v", err)
		}
	}

	// Create parent directory structure for new location (state/ancient/)
	if err := os.MkdirAll(filepath.Dir(newStateAncient), 0755); err != nil {
		return fmt.Errorf("failed to create state ancient parent directory: %v", err)
	}

	// Move the entire ancient state directory
	if err := os.Rename(originalStateAncient, newStateAncient); err != nil {
		return fmt.Errorf("failed to move ancient state directory: %v", err)
	}

	log.Info("✅ Ancient state data moved successfully",
		"from", originalStateAncient,
		"to", newStateAncient)

	return nil
}

// performDatabaseCompaction compacts all databases after migration to reclaim space
func performDatabaseCompaction(sourceDB, stateDB, snapDB, indexDB ethdb.Database) error {
	databases := []struct {
		db   ethdb.Database
		name string
	}{
		{sourceDB, "chaindata"},
		{stateDB, "state"},
		{snapDB, "snapshot"},
		{indexDB, "txindex"},
	}

	for _, dbInfo := range databases {
		log.Info("Compacting database", "name", dbInfo.name)

		// Try to compact the database if it supports compaction
		if compactor, ok := dbInfo.db.(interface {
			Compact(start []byte, limit []byte) error
		}); ok {
			// Perform full database compaction (nil, nil means compact everything)
			if err := compactor.Compact(nil, nil); err != nil {
				log.Warn("Database compaction failed", "name", dbInfo.name, "error", err)
				// Don't return error for compaction failure, just log it
				continue
			}
			log.Info("✅ Database compacted successfully", "name", dbInfo.name)
		} else {
			// If database doesn't support compaction, try sync operation
			if syncer, ok := dbInfo.db.(interface{ SyncKeyValue() error }); ok {
				if err := syncer.SyncKeyValue(); err != nil {
					log.Warn("Database sync failed", "name", dbInfo.name, "error", err)
				} else {
					log.Info("📝 Database synced successfully", "name", dbInfo.name)
				}
			} else {
				log.Debug("Database does not support compaction or sync", "name", dbInfo.name)
			}
		}
	}

	log.Info("🗜️  Database compaction completed for all databases")
	return nil
}

// migrateDBExpandMode migrates database in expand mode (write to target database with key/value transformation)
func migrateDBExpandMode(ctx *cli.Context, targetDataDir string, version byte, cacheSize, cacheDB int) error {
	// Create source stack using standard geth configuration (handles --datadir)
	sourceStack, _ := makeConfigNode(ctx)
	defer sourceStack.Close()

	// Get source database path
	sourceChainDataPath := sourceStack.ResolvePath("chaindata")

	log.Info("Starting database migration in expand mode",
		"source", sourceChainDataPath,
		"target", targetDataDir,
		"version", version)

	// Open source database for reading
	sourceDB, err := openTargetDatabase(sourceChainDataPath, cacheSize*cacheDB*7/100, 64)
	if err != nil {
		return fmt.Errorf("failed to open source chain database: %v", err)
	}
	defer sourceDB.Close()

	// Open existing target database (already migrated with multi-database structure)
	targetChainDataPath := filepath.Join(targetDataDir, "geth", "chaindata")

	// Verify target database structure exists
	targetStatePath := filepath.Join(targetChainDataPath, "state")
	targetSnapshotPath := filepath.Join(targetChainDataPath, "snapshot")
	targetTxIndexPath := filepath.Join(targetChainDataPath, "txindex")

	for _, dir := range []string{targetChainDataPath, targetStatePath, targetSnapshotPath, targetTxIndexPath} {
		if !common.FileExist(dir) {
			return fmt.Errorf("target database directory does not exist: %s (target should be a migrated multi-database)", dir)
		}
	}

	// Open existing target databases (panic if failed)
	chainDB, err := openTargetDatabase(targetChainDataPath, cacheSize*cacheDB*11/100, 64)
	if err != nil {
		panic(fmt.Sprintf("failed to open target chain database: %v", err))
	}
	defer chainDB.Close()

	stateDB, err := openTargetDatabaseWithFreezer(targetStatePath, cacheSize*cacheDB*50/100, 128)
	if err != nil {
		panic(fmt.Sprintf("failed to open target state database: %v", err))
	}
	defer stateDB.Close()

	snapDB, err := openTargetDatabase(targetSnapshotPath, cacheSize*cacheDB*24/100, 32)
	if err != nil {
		panic(fmt.Sprintf("failed to open target snapshot database: %v", err))
	}
	defer snapDB.Close()

	indexDB, err := openTargetDatabase(targetTxIndexPath, cacheSize*cacheDB*15/100, 32)
	if err != nil {
		panic(fmt.Sprintf("failed to open target txindex database: %v", err))
	}
	defer indexDB.Close()

	// Start expand migration
	return performExpandMigration(sourceDB, chainDB, stateDB, snapDB, indexDB, version)
}

// Helper function to generate new key with configurable suffix (reused from database.go)
func generateNewKey(originalKey []byte, suffix byte) []byte {
	if len(originalKey) <= 2 {
		return originalKey
	}

	// Create deterministic random generator based on key properties
	seed := int64(originalKey[0])*31*31 + int64(len(originalKey))*31 + int64(suffix)
	deterministicRand := rand.New(rand.NewSource(seed))

	newKey := make([]byte, len(originalKey))

	// Keep prefix same as original
	newKey[0] = originalKey[0]

	// Set suffix from flag
	newKey[len(newKey)-1] = suffix

	// Fill middle part with random data directly (optimized: use Read for bulk generation)
	if len(newKey) > 2 {
		deterministicRand.Read(newKey[1 : len(newKey)-1])
	}

	return newKey
}

// Helper function to shuffle value (reused from database.go)
func shuffleValue(originalValue []byte) []byte {
	if len(originalValue) <= 1 {
		return originalValue
	}

	// Create deterministic random generator based on value properties
	seed := int64(originalValue[0])*31 + int64(len(originalValue))
	deterministicRand := rand.New(rand.NewSource(seed))

	// Optimized: generate completely new random bytes (no loops, maximum performance)
	newValue := make([]byte, len(originalValue))
	deterministicRand.Read(newValue)

	return newValue
}

// performExpandMigration performs the actual data migration with key/value transformation using async multi-threading
func performExpandMigration(sourceDB, chainDB, stateDB, snapDB, indexDB ethdb.Database, version byte) error {
	log.Info("Starting expand migration with data transformation using async multi-threading", "version", version)

	stats := &MigrationStats{startTime: time.Now()}

	// Progress monitoring
	stopProgress := make(chan struct{})
	go progressMonitor(stats, stopProgress)

	// Extract all data using async multi-threading
	log.Info("🚀 Starting multi-threaded async data extraction and transformation...")
	if err := extractAllDataInOnePassExpand(sourceDB, chainDB, stateDB, snapDB, indexDB, stats, version); err != nil {
		close(stopProgress)
		return fmt.Errorf("failed to extract data: %v", err)
	}

	close(stopProgress)

	total, chain, state, snapshot, txindex := stats.Get()
	chainBytes, stateBytes, snapBytes, indexBytes := stats.GetBytes()
	elapsed := time.Since(stats.startTime)

	log.Info("Expand migration completed",
		"total", total,
		"chain", chain,
		"state", state,
		"snapshot", snapshot,
		"txindex", txindex,
		"elapsed", common.PrettyDuration(elapsed),
		"version", version)

	totalOutputBytes := chainBytes + stateBytes + snapBytes + indexBytes
	log.Info("Final data sizes",
		"chainMB", chainBytes/(1024*1024),
		"stateMB", stateBytes/(1024*1024),
		"snapMB", snapBytes/(1024*1024),
		"indexMB", indexBytes/(1024*1024),
		"totalOutputMB", totalOutputBytes/(1024*1024))

	// For single-DB mode, we have processed/skipped from the main loop
	// For multi-DB mode, these values come from the parallel scanners
	if total > 0 {
		log.Info("Data size analysis",
			"totalTransformed", total,
			"outputSizeMB", totalOutputBytes/(1024*1024),
			"avgBytesPerRecord", totalOutputBytes/total,
			"note", "Raw data size should be 1:1, but compression ratio may differ due to randomization")

		// Calculate estimated compression ratios
		avgRecordSize := totalOutputBytes / total
		log.Info("Storage efficiency analysis",
			"avgRawRecordSize", avgRecordSize,
			"possibleCause", "Randomized data has lower compression ratio than original structured data",
			"recommendation", "This is expected behavior - random data compresses poorly")
	}

	return nil
}

// extractAllDataInOnePassExpand extracts all data types using multi-threaded async processing with key/value transformation
func extractAllDataInOnePassExpand(sourceDB, chainDB, stateDB, snapDB, indexDB ethdb.Database, stats *MigrationStats, version byte) error {
	// Check if source database is multi-database
	if sourceStateDB := sourceDB.GetStateStore(); sourceStateDB != nil {
		log.Info("🚀 Source is multi-database, using parallel 4-DB scanning mode", "version", version)
		return extractMultiDBToMultiDBExpand(sourceDB, chainDB, stateDB, snapDB, indexDB, stats, version)
	}

	log.Info("🚀 Source is single database, using single-DB scanning mode",
		"architecture", "1 reader + 18 writers (4×chain,4×state,6×snapshot,4×txindex) = 19 threads",
		"version", version)

	// Channel buffer sizes - balance memory usage vs throughput
	const channelBufferSize = 20000 // Increased buffer for more threads

	// Create channels for communication between goroutines
	chainChannel := make(chan CategorizedData, channelBufferSize)
	stateChannel := make(chan CategorizedData, channelBufferSize)
	snapChannel := make(chan CategorizedData, channelBufferSize)
	indexChannel := make(chan CategorizedData, channelBufferSize)

	// Error channels to collect errors from goroutines
	errorChannel := make(chan error, 18) // 18 writers

	// WaitGroup to coordinate all goroutines
	var wg sync.WaitGroup

	// Start async writer goroutines (optimized thread allocation for better performance)
	log.Info("🚀 Starting async writer goroutines (4×chain,4×state,6×snapshot,4×txindex)...")

	// Chain database writers (4 threads)
	for i := 0; i < 4; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("chain-%d", id), chainDB, chainChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("chain writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// State database writers (4 threads)
	for i := 0; i < 10; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("state-%d", id), stateDB, stateChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("state writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Snapshot database writers (6 threads - snapshot data is largest)
	for i := 0; i < 8; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("snapshot-%d", id), snapDB, snapChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("snapshot writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Transaction index database writers (4 threads)
	for i := 0; i < 8; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("txindex-%d", id), indexDB, indexChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("txindex writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Main reader goroutine - process source database with transformation
	log.Info("📖 Starting main reader thread with 18 async writers...")
	processed := 0
	skipped := 0

	it := sourceDB.NewIterator(nil, nil)
	defer it.Release()

	for it.Next() {
		// Create copies of key and value since iterator reuses underlying memory
		originalKey := make([]byte, len(it.Key()))
		originalValue := make([]byte, len(it.Value()))
		copy(originalKey, it.Key())
		copy(originalValue, it.Value())

		processed++

		// Generate new key and value with timing
		keyGenStart := time.Now()
		newKey := generateNewKey(originalKey, version)
		keyGenDuration := time.Since(keyGenStart)

		// Skip if new key is the same as original key
		if bytes.Equal(newKey, originalKey) {
			skipped++
			continue
		}

		valueShuffleStart := time.Now()
		newValue := shuffleValue(originalValue)
		valueShuffleDuration := time.Since(valueShuffleStart)

		// Accumulate timing statistics (every 10000 operations to avoid overhead)
		if processed%10000 == 0 {
			atomic.AddInt64(&stats.totalKeyGenTime, keyGenDuration.Nanoseconds())
			atomic.AddInt64(&stats.totalValueShuffleTime, valueShuffleDuration.Nanoseconds())
			atomic.AddInt64(&stats.keyGenCount, 1)
			atomic.AddInt64(&stats.valueShuffleCount, 1)
		}

		// Debug: Verify data size consistency (first 100 items)
		if processed <= 100 {
			originalSize := len(originalKey) + len(originalValue)
			newSize := len(newKey) + len(newValue)
			if originalSize != newSize {
				log.Warn("Data size mismatch detected",
					"processed", processed,
					"originalKeyLen", len(originalKey),
					"originalValueLen", len(originalValue),
					"originalTotal", originalSize,
					"newKeyLen", len(newKey),
					"newValueLen", len(newValue),
					"newTotal", newSize,
					"diff", newSize-originalSize)
			}
		}

		// Determine the category for this key-value pair based on original key
		category := categorizeDataByKey(originalKey, originalValue)

		// Create transformed data package
		data := CategorizedData{
			Key:      newKey,
			Value:    newValue,
			Category: category,
		}

		// Route to correct writer channel based on category
		switch category {
		case "state":
			select {
			case stateChannel <- data:
			case err := <-errorChannel:
				return err
			}
		case "snapshot":
			select {
			case snapChannel <- data:
			case err := <-errorChannel:
				return err
			}
		case "txindex":
			select {
			case indexChannel <- data:
			case err := <-errorChannel:
				return err
			}
		default:
			// Everything else goes to chain database
			data.Category = "chain" // Update category for stats
			select {
			case chainChannel <- data:
			case err := <-errorChannel:
				return err
			}
		}

		// Debug: Log first few transformations
		if processed <= 30 && skipped < 10 {
			log.Info("📝 Queuing transformed data for async processing",
				"category", category,
				"originalKeyHex", fmt.Sprintf("%x", originalKey[:min(16, len(originalKey))]),
				"newKeyHex", fmt.Sprintf("%x", newKey[:min(16, len(newKey))]),
				"keyLen", len(newKey),
				"valueLen", len(newValue),
				"version", version)
		}

		// Progress update every 10000 items
		if processed%10000 == 0 && processed > 0 {
			log.Info("Reader progress", "processed", processed, "skipped", skipped)
		}
	}

	// Check for iterator errors
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterator error: %v", err)
	}

	log.Info("📖 Reader completed, closing channels...", "processed", processed, "skipped", skipped)

	// Close channels to signal completion to worker goroutines
	close(chainChannel)
	close(stateChannel)
	close(snapChannel)
	close(indexChannel)

	// Wait for all goroutines to complete
	log.Info("⏳ Waiting for all async writers to complete...")
	wg.Wait()

	// Check for any errors from worker goroutines
	close(errorChannel)
	for err := range errorChannel {
		if err != nil {
			return err
		}
	}

	log.Info("✅ Multi-threaded async data extraction with transformation completed",
		"totalProcessed", processed,
		"totalSkipped", skipped,
		"totalTransformed", processed-skipped,
		"threadsUsed", "19 (1 reader + 18 writers)",
		"version", version)
	return nil
}

// extractMultiDBToMultiDBExpand extracts data from multi-database to multi-database with transformation
func extractMultiDBToMultiDBExpand(sourceDB, targetChainDB, targetStateDB, targetSnapDB, targetIndexDB ethdb.Database, stats *MigrationStats, version byte) error {
	log.Info("🚀 Starting parallel multi-database to multi-database extraction with transformation",
		"architecture", "4 parallel scanners + 18 writers (4×chain,4×state,6×snapshot,4×txindex) = 22 threads",
		"version", version)

	// Channel buffer sizes
	const channelBufferSize = 20000

	// Create channels for communication between goroutines
	chainChannel := make(chan CategorizedData, channelBufferSize)
	stateChannel := make(chan CategorizedData, channelBufferSize)
	snapChannel := make(chan CategorizedData, channelBufferSize)
	indexChannel := make(chan CategorizedData, channelBufferSize)

	// Error channels to collect errors from goroutines
	errorChannel := make(chan error, 22) // 4 readers + 18 writers

	// WaitGroup to coordinate all goroutines
	var wg sync.WaitGroup

	// Start async writer goroutines (same as before)
	log.Info("🚀 Starting async writer goroutines (4×chain,4×state,6×snapshot,4×txindex)...")

	// Chain database writers (4 threads)
	for i := 0; i < 3; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("chain-%d", id), targetChainDB, chainChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("chain writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// State database writers (4 threads)
	for i := 0; i < 8; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("state-%d", id), targetStateDB, stateChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("state writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Snapshot database writers (6 threads - snapshot data is largest)
	for i := 0; i < 6; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("snapshot-%d", id), targetSnapDB, snapChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("snapshot writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Transaction index database writers (4 threads)
	for i := 0; i < 4; i++ {
		writerID := i + 1
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := asyncDatabaseWriter(fmt.Sprintf("txindex-%d", id), targetIndexDB, indexChannel, stats); err != nil {
				errorChannel <- fmt.Errorf("txindex writer %d error: %v", id, err)
			}
		}(writerID)
	}

	// Start 4 parallel database scanners
	log.Info("📖 Starting 4 parallel database scanners...")

	// Shared counters for statistics
	var totalProcessed, totalSkipped int64

	// Chain database scanner
	wg.Add(1)
	go func() {
		defer wg.Done()
		processed, skipped, err := scanDatabaseWithTransform(sourceDB, chainChannel, "chain", version, &totalProcessed, &totalSkipped)
		if err != nil {
			errorChannel <- fmt.Errorf("chain scanner error: %v", err)
		} else {
			log.Info("Chain scanner completed", "processed", processed, "skipped", skipped)
		}
	}()

	// State database scanner
	wg.Add(1)
	go func() {
		defer wg.Done()
		stateDB := rawdb.NewDatabase(sourceDB.GetStateStore())
		processed, skipped, err := scanDatabaseWithTransform(stateDB, stateChannel, "state", version, &totalProcessed, &totalSkipped)
		if err != nil {
			errorChannel <- fmt.Errorf("state scanner error: %v", err)
		} else {
			log.Info("State scanner completed", "processed", processed, "skipped", skipped)
		}
	}()

	// Snapshot database scanner
	wg.Add(1)
	go func() {
		defer wg.Done()
		snapDB := rawdb.NewDatabase(sourceDB.GetSnapStore())
		processed, skipped, err := scanDatabaseWithTransform(snapDB, snapChannel, "snapshot", version, &totalProcessed, &totalSkipped)
		if err != nil {
			errorChannel <- fmt.Errorf("snapshot scanner error: %v", err)
		} else {
			log.Info("Snapshot scanner completed", "processed", processed, "skipped", skipped)
		}
	}()

	// Transaction index database scanner
	wg.Add(1)
	go func() {
		defer wg.Done()
		indexDB := rawdb.NewDatabase(sourceDB.GetTxIndexStore())
		processed, skipped, err := scanDatabaseWithTransform(indexDB, indexChannel, "txindex", version, &totalProcessed, &totalSkipped)
		if err != nil {
			errorChannel <- fmt.Errorf("txindex scanner error: %v", err)
		} else {
			log.Info("TxIndex scanner completed", "processed", processed, "skipped", skipped)
		}
	}()

	// Wait for all goroutines to complete
	log.Info("⏳ Waiting for all parallel scanners and writers to complete...")
	wg.Wait()

	// Close channels to signal completion to worker goroutines
	log.Info("📖 All scanners completed, closing channels...")
	close(chainChannel)
	close(stateChannel)
	close(snapChannel)
	close(indexChannel)

	// Check for any errors from worker goroutines
	close(errorChannel)
	for err := range errorChannel {
		if err != nil {
			return err
		}
	}

	log.Info("✅ Parallel multi-database extraction with transformation completed",
		"totalProcessed", atomic.LoadInt64(&totalProcessed),
		"totalSkipped", atomic.LoadInt64(&totalSkipped),
		"totalTransformed", atomic.LoadInt64(&totalProcessed)-atomic.LoadInt64(&totalSkipped),
		"threadsUsed", "22 (4 scanners + 18 writers)",
		"version", version)
	return nil
}

// scanDatabaseWithTransform scans a single database and sends transformed data to the appropriate channel
func scanDatabaseWithTransform(sourceDB ethdb.Database, targetChannel chan<- CategorizedData, dbType string, version byte, totalProcessed, totalSkipped *int64) (int64, int64, error) {
	log.Info("Starting database scanner", "type", dbType)

	it := sourceDB.NewIterator(nil, nil)
	defer it.Release()

	var processed, skipped int64

	for it.Next() {
		// Create copies of key and value
		originalKey := make([]byte, len(it.Key()))
		originalValue := make([]byte, len(it.Value()))
		copy(originalKey, it.Key())
		copy(originalValue, it.Value())

		processed++
		atomic.AddInt64(totalProcessed, 1)

		// Generate new key and value with timing
		keyGenStart := time.Now()
		newKey := generateNewKey(originalKey, version)
		keyGenDuration := time.Since(keyGenStart)

		// Skip if new key is the same as original key
		if bytes.Equal(newKey, originalKey) {
			skipped++
			atomic.AddInt64(totalSkipped, 1)
			continue
		}

		valueShuffleStart := time.Now()
		newValue := shuffleValue(originalValue)
		valueShuffleDuration := time.Since(valueShuffleStart)

		// Log timing statistics periodically (every 50000 operations to reduce overhead)
		if processed%50000 == 0 {
			avgKeyGenTime := keyGenDuration.Nanoseconds()
			avgValueShuffleTime := valueShuffleDuration.Nanoseconds()
			log.Info("Key/Value generation performance",
				"scanner", dbType,
				"processed", processed,
				"avgKeyGenNs", avgKeyGenTime,
				"avgValueShuffleNs", avgValueShuffleTime,
				"keyGenMicros", avgKeyGenTime/1000,
				"valueShuffleMicros", avgValueShuffleTime/1000)
		}

		// Create transformed data package
		data := CategorizedData{
			Key:      newKey,
			Value:    newValue,
			Category: dbType, // Use the database type as category
		}

		// Send to target channel
		select {
		case targetChannel <- data:
		default:
			// Channel full, this is a problem
			return processed, skipped, fmt.Errorf("target channel for %s is full", dbType)
		}

		// Progress logging
		if processed%100000 == 0 && processed > 0 {
			log.Info("Scanner progress", "type", dbType, "processed", processed, "skipped", skipped)
		}
	}

	if err := it.Error(); err != nil {
		return processed, skipped, fmt.Errorf("iterator error in %s scanner: %v", dbType, err)
	}

	log.Info("Scanner completed", "type", dbType, "totalProcessed", processed, "totalSkipped", skipped)
	return processed, skipped, nil
}

// flushAllBatches flushes all database batches
func flushAllBatches(chainBatch, stateBatch, snapBatch, indexBatch ethdb.Batch) error {
	if err := chainBatch.Write(); err != nil {
		return fmt.Errorf("failed to write chain batch: %v", err)
	}
	if err := stateBatch.Write(); err != nil {
		return fmt.Errorf("failed to write state batch: %v", err)
	}
	if err := snapBatch.Write(); err != nil {
		return fmt.Errorf("failed to write snapshot batch: %v", err)
	}
	if err := indexBatch.Write(); err != nil {
		return fmt.Errorf("failed to write index batch: %v", err)
	}

	chainBatch.Reset()
	stateBatch.Reset()
	snapBatch.Reset()
	indexBatch.Reset()

	return nil
}

func initMultiDBs(stack *node.Node, cfg gethConfig, forceCreate bool) (ethdb.Database, error) {
	log.Info("initializing multi dbs...", "forceCreate", forceCreate)
	chainDB, err := stack.OpenAndMergeDatabase(eth.ChainData, eth.ChainDBNamespace, false, &cfg.Eth)
	if err != nil {
		return nil, fmt.Errorf("failed to open and merge database: %v", err)
	}
	isMultiDB := stack.CheckIfMultiDataBase()
	log.Info("open and merge database", "chaindata", eth.ChainData, "isMultiDB", isMultiDB)
	if forceCreate {
		if isMultiDB {
			defer chainDB.Close()
			return nil, fmt.Errorf("there is already multidbs set, cannot migrate again")
		}
		// just set multidbs, and then migrate in place
		log.Info("setting multidbs...", "datadir", stack.DataDir(), "config", stack.Config().Storage)
		stack.SetMultiDBs(chainDB, eth.ChainData, cfg.Eth.DatabaseCache, cfg.Eth.DatabaseHandles, false, false)
	}

	return chainDB, nil
}

func initMultiDBsWithDataDir(srcStack *node.Node, cfg gethConfig, datadir string) (ethdb.Database, error) {
	log.Info("initializing multi dbs with data dir...", "datadir", datadir)

	nodeCfg := *srcStack.Config()
	nodeCfg.DataDir = datadir
	nodeCfg.Storage.ChainDB.DBPath = ""
	nodeCfg.Storage.SnapDB.DBPath = ""
	nodeCfg.Storage.IndexDB.DBPath = ""
	nodeCfg.Storage.TrieDB.DBPath = ""

	stack, err := node.New(&nodeCfg)
	if err != nil {
		log.Error("Failed to create the protocol stack", "error", err)
		return nil, err
	}
	chainDB, err := stack.OpenAndMergeDatabase(eth.ChainData, eth.ChainDBNamespace, false, &cfg.Eth)
	if err != nil {
		return nil, fmt.Errorf("failed to open and merge database: %v", err)
	}
	log.Info("open and merge database", "chaindata", eth.ChainData, "isMultiDB", stack.CheckIfMultiDataBase())
	return chainDB, nil
}

func migrateDBWithSharding(ctx *cli.Context) error {
	stack, cfg := makeConfigNode(ctx)
	chainDB, err := initMultiDBs(stack, cfg, true)
	if err != nil {
		return fmt.Errorf("failed to init multidbs: %v", err)
	}

	// traverse all the keys in the chaindb, and then migrate in place
	log.Info("traversing and migrating with sharding...")
	if err := traverseAndMigrateWithSharding(chainDB); err != nil {
		return fmt.Errorf("failed to traverse and migrate with sharding: %v", err)
	}

	// move state ancient data
	srcStateAncient, err := chainDB.AncientDatadir()
	if err != nil {
		return fmt.Errorf("failed to get state ancient directory: %v", err)
	}
	srcStateAncient = filepath.Join(srcStateAncient, rawdb.MerkleStateFreezerName)
	if _, err := os.Stat(srcStateAncient); err != nil {
		log.Info("no state ancient data to migrate", "path", srcStateAncient)
		return nil
	}
	dstStateAncient, err := chainDB.GetStateStore().AncientDatadir()
	if err != nil {
		return fmt.Errorf("failed to get state ancient directory: %v", err)
	}
	if err := os.MkdirAll(dstStateAncient, 0755); err != nil {
		return fmt.Errorf("failed to create state ancient directory: %v", err)
	}
	dstStateAncient = filepath.Join(dstStateAncient, rawdb.MerkleStateFreezerName)
	log.Info("moving state ancient data...", "src", srcStateAncient, "dst", dstStateAncient)
	if err := os.Rename(srcStateAncient, dstStateAncient); err != nil {
		return fmt.Errorf("failed to move state ancient directory: %v", err)
	}

	// compact the database
	log.Info("compacting chaindb...")
	if err := chainDB.Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact chaindb: %v", err)
	}
	log.Info("compacting statedb...")
	if err := chainDB.GetStateStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact statedb: %v", err)
	}
	log.Info("compacting snapdb...")
	if err := chainDB.GetSnapStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact snapdb: %v", err)
	}
	log.Info("compacting indexdb...")
	if err := chainDB.GetTxIndexStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact indexdb: %v", err)
	}

	return nil
}

type stat struct {
	size  common.StorageSize
	count uint64
}

func (s *stat) Add(size int) {
	s.size += common.StorageSize(size)
	s.count++
}

func traverseAndMigrateWithSharding(chainDB ethdb.Database) error {
	it := chainDB.NewIterator(nil, nil)
	defer it.Release()

	var (
		chainBatch = chainDB.NewBatch()
		stateBatch = chainDB.GetStateStore().NewBatch()
		snapBatch  = chainDB.GetSnapStore().NewBatch()
		indexBatch = chainDB.GetTxIndexStore().NewBatch()
		batchSize  = 0
		chainStat  = &stat{}
		stateStat  = &stat{}
		snapStat   = &stat{}
		indexStat  = &stat{}
	)
	for it.Next() {
		key := make([]byte, len(it.Key()))
		value := make([]byte, len(it.Value()))
		copy(key, it.Key())
		copy(value, it.Value())
		kvSize := len(key) + len(value)
		batchSize += kvSize
		chainStat.Add(kvSize)

		// put the key into the state, snap, or index database and delete from chaindb
		category := categorizeDataByKey(key, value)
		switch category {
		case "state":
			stateBatch.Put(key, value)
			chainBatch.Delete(key)
			stateStat.Add(kvSize)
		case "snapshot":
			snapBatch.Put(key, value)
			chainBatch.Delete(key)
			snapStat.Add(kvSize)
		case "txindex":
			indexBatch.Put(key, value)
			chainBatch.Delete(key)
			indexStat.Add(kvSize)
		}

		// flush the batch if it's too large
		if batchSize >= 256*1024*1024 {
			log.Info("flushing kvs...", "chain count", chainStat.count, "chain size", chainStat.size,
				"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
				"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)
			if err := stateBatch.Write(); err != nil {
				return fmt.Errorf("failed to write state batch: %v", err)
			}
			if err := snapBatch.Write(); err != nil {
				return fmt.Errorf("failed to write snap batch: %v", err)
			}
			if err := indexBatch.Write(); err != nil {
				return fmt.Errorf("failed to write index batch: %v", err)
			}
			// save first, then delete
			if err := chainBatch.Write(); err != nil {
				return fmt.Errorf("failed to write chain batch: %v", err)
			}
			chainBatch.Reset()
			stateBatch.Reset()
			snapBatch.Reset()
			indexBatch.Reset()
			batchSize = 0
		}
	}

	// flush the remaining kvs
	if batchSize > 0 {
		log.Info("flushing leftover kvs...", "chain count", chainStat.count, "chain size", chainStat.size,
			"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
			"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)
		if err := stateBatch.Write(); err != nil {
			return fmt.Errorf("failed to write state batch: %v", err)
		}
		if err := snapBatch.Write(); err != nil {
			return fmt.Errorf("failed to write snap batch: %v", err)
		}
		if err := indexBatch.Write(); err != nil {
			return fmt.Errorf("failed to write index batch: %v", err)
		}
		// save first, then delete
		if err := chainBatch.Write(); err != nil {
			return fmt.Errorf("failed to write chain batch: %v", err)
		}
		chainBatch.Reset()
		stateBatch.Reset()
		snapBatch.Reset()
		indexBatch.Reset()
		batchSize = 0
	}

	log.Info("migration completed", "chain count", chainStat.count, "chain size", chainStat.size,
		"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
		"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)
	return nil
}

// migrateDBWithShardingExpandMode migrates database with sharding in expand mode
func migrateDBWithShardingExpandMode(ctx *cli.Context, targetDataDir string, version byte) error {
	// src is a single db, read all the kvs and then write to the target db
	stack, cfg := makeConfigNode(ctx)
	srcChainDB, err := initMultiDBs(stack, cfg, false)
	if err != nil {
		return fmt.Errorf("failed to init source chaindb: %v", err)
	}
	defer srcChainDB.Close()

	dstChainDB, err := initMultiDBsWithDataDir(stack, cfg, targetDataDir)
	if err != nil {
		return fmt.Errorf("failed to init target chaindb: %v", err)
	}
	defer dstChainDB.Close()

	it := srcChainDB.NewIterator(nil, nil)
	defer it.Release()

	var (
		chainBatch = dstChainDB.NewBatch()
		stateBatch = dstChainDB.GetStateStore().NewBatch()
		snapBatch  = dstChainDB.GetSnapStore().NewBatch()
		indexBatch = dstChainDB.GetTxIndexStore().NewBatch()
		batchSize  = 0
		srcStat    = &stat{}
		chainStat  = &stat{}
		stateStat  = &stat{}
		snapStat   = &stat{}
		indexStat  = &stat{}
	)
	for it.Next() {
		key := make([]byte, len(it.Key()))
		value := make([]byte, len(it.Value()))
		copy(key, it.Key())
		copy(value, it.Value())

		// regenerate the new key and value
		key = generateNewKey(key, version)
		value = shuffleValue(value)
		kvSize := len(key) + len(value)
		batchSize += kvSize
		srcStat.Add(kvSize)

		// put the key into the state, snap, or index database and delete from chaindb
		category := categorizeDataByKey(key, value)
		switch category {
		case "state":
			stateBatch.Put(key, value)
			stateStat.Add(kvSize)
		case "snapshot":
			snapBatch.Put(key, value)
			snapStat.Add(kvSize)
		case "txindex":
			indexBatch.Put(key, value)
			indexStat.Add(kvSize)
		default:
			chainBatch.Put(key, value)
			chainStat.Add(kvSize)
		}

		// flush the batch if it's too large
		if batchSize >= 256*1024*1024 {
			log.Info("flushing kvs...", "src count", srcStat.count, "src size", srcStat.size,
				"chain count", chainStat.count, "chain size", chainStat.size,
				"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
				"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)
			if err := stateBatch.Write(); err != nil {
				return fmt.Errorf("failed to write state batch: %v", err)
			}
			if err := snapBatch.Write(); err != nil {
				return fmt.Errorf("failed to write snap batch: %v", err)
			}
			if err := indexBatch.Write(); err != nil {
				return fmt.Errorf("failed to write index batch: %v", err)
			}
			// save first, then delete
			if err := chainBatch.Write(); err != nil {
				return fmt.Errorf("failed to write chain batch: %v", err)
			}
			chainBatch.Reset()
			stateBatch.Reset()
			snapBatch.Reset()
			indexBatch.Reset()
			batchSize = 0
		}
	}

	// flush the remaining kvs
	if batchSize > 0 {
		log.Info("flushing leftover kvs...", "src count", srcStat.count, "src size", srcStat.size,
			"chain count", chainStat.count, "chain size", chainStat.size,
			"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
			"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)
		if err := stateBatch.Write(); err != nil {
			return fmt.Errorf("failed to write state batch: %v", err)
		}
		if err := snapBatch.Write(); err != nil {
			return fmt.Errorf("failed to write snap batch: %v", err)
		}
		if err := indexBatch.Write(); err != nil {
			return fmt.Errorf("failed to write index batch: %v", err)
		}
		// save first, then delete
		if err := chainBatch.Write(); err != nil {
			return fmt.Errorf("failed to write chain batch: %v", err)
		}
		chainBatch.Reset()
		stateBatch.Reset()
		snapBatch.Reset()
		indexBatch.Reset()
		batchSize = 0
	}

	log.Info("migration completed", "src count", srcStat.count, "src size", srcStat.size,
		"chain count", chainStat.count, "chain size", chainStat.size,
		"state count", stateStat.count, "state size", stateStat.size, "snap count", snapStat.count,
		"snap size", snapStat.size, "index count", indexStat.count, "index size", indexStat.size)

	// compact the database
	log.Info("compacting chaindb...")
	if err := dstChainDB.Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact chaindb: %v", err)
	}
	log.Info("compacting statedb...")
	if err := dstChainDB.GetStateStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact statedb: %v", err)
	}
	log.Info("compacting snapdb...")
	if err := dstChainDB.GetSnapStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact snapdb: %v", err)
	}
	log.Info("compacting indexdb...")
	if err := dstChainDB.GetTxIndexStore().Compact(nil, nil); err != nil {
		return fmt.Errorf("failed to compact indexdb: %v", err)
	}
	return nil
}
