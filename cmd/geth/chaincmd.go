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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/era"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

var (
	initCommand = &cli.Command{
		Action:    initGenesis,
		Name:      "init",
		Usage:     "Bootstrap and initialize a new genesis block",
		ArgsUsage: "<genesisPath>",
		Flags: slices.Concat([]cli.Flag{
			utils.CachePreimagesFlag,
			utils.OverridePassedForkTime,
			utils.OverrideLorentz,
			utils.OverrideMaxwell,
			utils.OverrideVerkle,
			utils.MultiDataBaseFlag,
		}, utils.DatabaseFlags),
		Description: `
The init command initializes a new genesis block and definition for the network.
This is a destructive action and changes the network in which you will be
participating.

It expects the genesis file as argument.`,
	}
	initNetworkCommand = &cli.Command{
		Action:    initNetwork,
		Name:      "init-network",
		Usage:     "Bootstrap and initialize a new genesis block, and nodekey, config files for network nodes",
		ArgsUsage: "<genesisPath>",
		Flags: []cli.Flag{
			utils.InitNetworkDir,
			utils.InitNetworkPort,
			utils.InitNetworkSize,
			utils.InitNetworkIps,
			utils.InitSentryNodeSize,
			utils.InitSentryNodeIPs,
			utils.InitSentryNodePorts,
			utils.InitFullNodeSize,
			utils.InitFullNodeIPs,
			utils.InitFullNodePorts,
			utils.InitEVNSentryWhitelist,
			utils.InitEVNValidatorWhitelist,
			utils.InitEVNSentryRegister,
			utils.InitEVNValidatorRegister,
			configFileFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The init-network command initializes a new genesis block, definition for the network, config files for network nodes.
It expects the genesis file as argument.`,
	}
	dumpGenesisCommand = &cli.Command{
		Action:    dumpGenesis,
		Name:      "dumpgenesis",
		Usage:     "Dumps genesis block JSON configuration to stdout",
		ArgsUsage: "",
		Flags:     append([]cli.Flag{utils.DataDirFlag}, utils.NetworkFlags...),
		Description: `
The dumpgenesis command prints the genesis configuration of the network preset
if one is set.  Otherwise it prints the genesis from the datadir.`,
	}
	importCommand = &cli.Command{
		Action:    importChain,
		Name:      "import",
		Usage:     "Import a blockchain file",
		ArgsUsage: "<filename> (<filename 2> ... <filename N>) ",
		Flags: slices.Concat([]cli.Flag{
			utils.CacheFlag,
			utils.SyncModeFlag,
			utils.GCModeFlag,
			utils.SnapshotFlag,
			utils.CacheDatabaseFlag,
			utils.CacheGCFlag,
			utils.MetricsEnabledFlag,
			utils.MetricsEnabledExpensiveFlag,
			utils.MetricsHTTPFlag,
			utils.MetricsPortFlag,
			utils.MetricsEnableInfluxDBFlag,
			utils.MetricsEnableInfluxDBV2Flag,
			utils.MetricsInfluxDBEndpointFlag,
			utils.MetricsInfluxDBDatabaseFlag,
			utils.MetricsInfluxDBUsernameFlag,
			utils.MetricsInfluxDBPasswordFlag,
			utils.MetricsInfluxDBTagsFlag,
			utils.MetricsInfluxDBTokenFlag,
			utils.MetricsInfluxDBBucketFlag,
			utils.MetricsInfluxDBOrganizationFlag,
			utils.TxLookupLimitFlag,
			utils.VMTraceFlag,
			utils.VMTraceJsonConfigFlag,
			utils.TransactionHistoryFlag,
			utils.StateHistoryFlag,
		}, utils.DatabaseFlags),
		Description: `
The import command imports blocks from an RLP-encoded form. The form can be one file
with several RLP-encoded blocks, or several files can be used.

If only one file is used, import error will result in failure. If several files are used,
processing will proceed even if an individual RLP-file import failure occurs.`,
	}
	exportCommand = &cli.Command{
		Action:    exportChain,
		Name:      "export",
		Usage:     "Export blockchain into file",
		ArgsUsage: "<filename> [<blockNumFirst> <blockNumLast>]",
		Flags: slices.Concat([]cli.Flag{
			utils.CacheFlag,
			utils.SyncModeFlag,
		}, utils.DatabaseFlags),
		Description: `
Requires a first argument of the file to write to.
Optional second and third arguments control the first and
last block to write. In this mode, the file will be appended
if already existing. If the file ends with .gz, the output will
be gzipped.`,
	}
	importHistoryCommand = &cli.Command{
		Action:    importHistory,
		Name:      "import-history",
		Usage:     "Import an Era archive",
		ArgsUsage: "<dir>",
		Flags: slices.Concat([]cli.Flag{
			utils.TxLookupLimitFlag,
		},
			utils.DatabaseFlags,
			utils.NetworkFlags,
		),
		Description: `
The import-history command will import blocks and their corresponding receipts
from Era archives.
`,
	}
	exportHistoryCommand = &cli.Command{
		Action:    exportHistory,
		Name:      "export-history",
		Usage:     "Export blockchain history to Era archives",
		ArgsUsage: "<dir> <first> <last>",
		Flags:     slices.Concat(utils.DatabaseFlags),
		Description: `
The export-history command will export blocks and their corresponding receipts
into Era archives. Eras are typically packaged in steps of 8192 blocks.
`,
	}
	importPreimagesCommand = &cli.Command{
		Action:    importPreimages,
		Name:      "import-preimages",
		Usage:     "Import the preimage database from an RLP stream",
		ArgsUsage: "<datafile>",
		Flags: slices.Concat([]cli.Flag{
			utils.CacheFlag,
			utils.SyncModeFlag,
		}, utils.DatabaseFlags),
		Description: `
The import-preimages command imports hash preimages from an RLP encoded stream.
It's deprecated, please use "geth db import" instead.
`,
	}

	dumpCommand = &cli.Command{
		Action:    dump,
		Name:      "dump",
		Usage:     "Dump a specific block from storage",
		ArgsUsage: "[? <blockHash> | <blockNum>]",
		Flags: slices.Concat([]cli.Flag{
			utils.CacheFlag,
			utils.IterativeOutputFlag,
			utils.ExcludeCodeFlag,
			utils.ExcludeStorageFlag,
			utils.IncludeIncompletesFlag,
			utils.StartKeyFlag,
			utils.DumpLimitFlag,
		}, utils.DatabaseFlags),
		Description: `
This command dumps out the state for a given block (or latest, if none provided).
If you use "dump" command in path mode, please firstly use "dump-roothash" command to get all available state root hash.
`,
	}
	dumpRootHashCommand = &cli.Command{
		Action: dumpAllRootHashInPath,
		Name:   "dump-roothash",
		Usage:  "Dump all available state root hash in path mode",
		Flags:  slices.Concat([]cli.Flag{}, utils.DatabaseFlags),
		Description: `
The dump-roothash command dump all available state root hash in path mode.
If you use "dump" command in path mode, please note that it only keeps at most 129 blocks which belongs to diffLayer or diskLayer.
Therefore, you must specify the blockNumber or blockHash that locates in diffLayer or diskLayer.
"geth" will print all available blockNumber and related block state root hash, and you can query block hash by block number.
`,
	}
)

const (
	DefaultSentryP2PPort   = 30411
	DefaultFullNodeP2PPort = 30511
)

// initGenesis will initialise the given JSON format genesis file and writes it as
// the zero'd block (i.e. genesis) or will fail hard if it can't succeed.
func initGenesis(ctx *cli.Context) error {
	if ctx.Args().Len() != 1 {
		utils.Fatalf("need genesis.json file as the only argument")
	}
	genesisPath := ctx.Args().First()
	if len(genesisPath) == 0 {
		utils.Fatalf("invalid path to genesis file")
	}
	file, err := os.Open(genesisPath)
	if err != nil {
		utils.Fatalf("Failed to read genesis file: %v", err)
	}
	defer file.Close()

	genesis := new(core.Genesis)
	if err := json.NewDecoder(file).Decode(genesis); err != nil {
		utils.Fatalf("invalid genesis file: %v", err)
	}
	// Open and initialise both full and light databases
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	var overrides core.ChainOverrides
	if ctx.IsSet(utils.OverridePassedForkTime.Name) {
		v := ctx.Uint64(utils.OverridePassedForkTime.Name)
		overrides.OverridePassedForkTime = &v
	}
	if ctx.IsSet(utils.OverrideLorentz.Name) {
		v := ctx.Uint64(utils.OverrideLorentz.Name)
		overrides.OverrideLorentz = &v
	}
	if ctx.IsSet(utils.OverrideMaxwell.Name) {
		v := ctx.Uint64(utils.OverrideMaxwell.Name)
		overrides.OverrideMaxwell = &v
	}
	if ctx.IsSet(utils.OverrideVerkle.Name) {
		v := ctx.Uint64(utils.OverrideVerkle.Name)
		overrides.OverrideVerkle = &v
	}
	name := "chaindata"
	chaindb, err := stack.OpenDatabaseWithFreezer(name, 0, 0, ctx.String(utils.AncientFlag.Name), "", false, false, false, false)
	if err != nil {
		utils.Fatalf("Failed to open database: %v", err)
	}
	defer chaindb.Close()

	// if the trie data dir has been set, new trie db with a new state database
	if ctx.IsSet(utils.MultiDataBaseFlag.Name) {
		statediskdb, dbErr := stack.OpenDatabaseWithFreezer(name+"/state", 0, 0, "", "", false, false, false, false)
		if dbErr != nil {
			utils.Fatalf("Failed to open separate trie database: %v", dbErr)
		}
		chaindb.SetStateStore(statediskdb)
		blockdb, err := stack.OpenDatabaseWithFreezer(name+"/block", 0, 0, "", "", false, false, false, false)
		if err != nil {
			utils.Fatalf("Failed to open separate block database: %v", err)
		}
		chaindb.SetBlockStore(blockdb)
		log.Warn("Multi-database is an experimental feature")
	}

	triedb := utils.MakeTrieDatabase(ctx, stack, chaindb, ctx.Bool(utils.CachePreimagesFlag.Name), false, genesis.IsVerkle())
	defer triedb.Close()

	_, hash, _, err := core.SetupGenesisBlockWithOverride(chaindb, triedb, genesis, &overrides)
	if err != nil {
		utils.Fatalf("Failed to write genesis block: %v", err)
	}
	log.Info("Successfully wrote genesis state", "database", name, "hash", hash.String())
	return nil
}

func parseIps(ipStr string, size int) ([]string, error) {
	var ips []string
	if len(ipStr) != 0 {
		ips = strings.Split(ipStr, ",")
		if len(ips) != size {
			return nil, errors.New("mismatch of size and length of ips")
		}
		for i := 0; i < size; i++ {
			_, err := net.ResolveIPAddr("", ips[i])
			if err != nil {
				return nil, errors.New("invalid format of ip")
			}
		}
	} else {
		ips = make([]string, size)
		for i := 0; i < size; i++ {
			ips[i] = "127.0.0.1"
		}
	}
	return ips, nil
}

func parsePorts(portStr string, defaultPort int, size int) ([]int, error) {
	var ports []int
	if strings.Contains(portStr, ",") {
		portParts := strings.Split(portStr, ",")
		if len(portParts) != size {
			return nil, errors.New("mismatch of size and length of ports")
		}
		for i := 0; i < size; i++ {
			port, err := strconv.Atoi(portParts[i])
			if err != nil {
				return nil, errors.New("invalid format of port")
			}
			ports[i] = port
		}
	} else if len(portStr) != 0 {
		startPort, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, errors.New("invalid format of port")
		}
		ports = make([]int, size)
		for i := 0; i < size; i++ {
			ports[i] = startPort + i
		}
	} else {
		ports = make([]int, size)
		for i := 0; i < size; i++ {
			ports[i] = defaultPort
		}
	}
	return ports, nil
}

func createPorts(ipStr string, port int, size int) []int {
	ports := make([]int, size)
	if len(ipStr) == 0 { // localhost , so different ports
		for i := 0; i < size; i++ {
			ports[i] = port + i
		}
	} else { // different machines, keep same port
		for i := 0; i < size; i++ {
			ports[i] = port
		}
	}
	return ports
}

// Create config for node i in the cluster
func createNodeConfig(baseConfig gethConfig, ip string, port int, enodes []*enode.Node, index int, staticConnect bool) gethConfig {
	baseConfig.Node.HTTPHost = ip
	baseConfig.Node.P2P.ListenAddr = fmt.Sprintf(":%d", port)
	connectEnodes := make([]*enode.Node, 0, len(enodes)-1)
	for j := 0; j < index; j++ {
		connectEnodes = append(connectEnodes, enodes[j])
	}
	for j := index + 1; j < len(enodes); j++ {
		connectEnodes = append(connectEnodes, enodes[j])
	}
	// Set the P2P connections between this node and the other nodes
	if staticConnect {
		baseConfig.Node.P2P.StaticNodes = connectEnodes
	} else {
		baseConfig.Node.P2P.BootstrapNodes = connectEnodes
	}
	return baseConfig
}

// initNetwork will bootstrap and initialize a new genesis block, and nodekey, config files for network nodes
func initNetwork(ctx *cli.Context) error {
	initDir := ctx.String(utils.InitNetworkDir.Name)
	if len(initDir) == 0 {
		utils.Fatalf("init.dir is required")
	}
	size := ctx.Int(utils.InitNetworkSize.Name)
	if size <= 0 {
		utils.Fatalf("size should be greater than 0")
	}
	port := ctx.Int(utils.InitNetworkPort.Name)
	if port <= 0 {
		utils.Fatalf("port should be greater than 0")
	}
	ipStr := ctx.String(utils.InitNetworkIps.Name)
	cfgFile := ctx.String(configFileFlag.Name)

	if len(cfgFile) == 0 {
		utils.Fatalf("config file is required")
	}

	ips, err := parseIps(ipStr, size)
	if err != nil {
		utils.Fatalf("Failed to pase ips string: %v", err)
	}

	ports := createPorts(ipStr, port, size)

	// Make sure we have a valid genesis JSON
	genesisPath := ctx.Args().First()
	if len(genesisPath) == 0 {
		utils.Fatalf("Must supply path to genesis JSON file")
	}
	inGenesisFile, err := os.Open(genesisPath)
	if err != nil {
		utils.Fatalf("Failed to read genesis file: %v", err)
	}
	defer inGenesisFile.Close()

	genesis := new(core.Genesis)
	if err := json.NewDecoder(inGenesisFile).Decode(genesis); err != nil {
		utils.Fatalf("invalid genesis file: %v", err)
	}

	// load config
	var config gethConfig
	err = loadConfig(cfgFile, &config)
	if err != nil {
		return err
	}
	enableSentryNode := ctx.Int(utils.InitSentryNodeSize.Name) > 0
	var (
		sentryConfigs         []gethConfig
		sentryEnodes          []*enode.Node
		sentryNodeIDs         []enode.ID
		connectOneExtraEnodes bool
		staticConnect         bool
	)
	if enableSentryNode {
		sentryConfigs, sentryEnodes, err = createSentryNodeConfigs(ctx, config, initDir)
		if err != nil {
			utils.Fatalf("Failed to create sentry node configs: %v", err)
		}
		sentryNodeIDs = make([]enode.ID, len(sentryEnodes))
		for i := 0; i < len(sentryEnodes); i++ {
			sentryNodeIDs[i] = sentryEnodes[i].ID()
		}
		connectOneExtraEnodes = true
		staticConnect = true
	}

	configs, enodes, accounts, err := createConfigs(config, initDir, "node", ips, ports, sentryEnodes, connectOneExtraEnodes, staticConnect)
	if err != nil {
		utils.Fatalf("Failed to create node configs: %v", err)
	}

	nodeIDs := make([]enode.ID, len(enodes))
	for i := 0; i < len(enodes); i++ {
		nodeIDs[i] = enodes[i].ID()
	}
	// add more feature configs
	if enableSentryNode {
		for i := 0; i < len(sentryConfigs); i++ {
			sentryConfigs[i].Node.P2P.ProxyedValidatorAddresses = accounts[i]
		}
	}
	if ctx.Bool(utils.InitEVNValidatorWhitelist.Name) {
		for i := 0; i < size; i++ {
			configs[i].Node.P2P.EVNNodeIdsWhitelist = nodeIDs
		}
	}
	if ctx.Bool(utils.InitEVNValidatorRegister.Name) {
		for i := 0; i < size; i++ {
			configs[i].Eth.EVNNodeIDsToAdd = []enode.ID{nodeIDs[i]}
		}
	}
	if enableSentryNode && ctx.Bool(utils.InitEVNSentryWhitelist.Name) {
		for i := 0; i < len(sentryConfigs); i++ {
			// whitelist all sentry nodes + proxyed validator NodeID
			wlNodeIDs := []enode.ID{nodeIDs[i]}
			wlNodeIDs = append(wlNodeIDs, sentryNodeIDs...)
			sentryConfigs[i].Node.P2P.EVNNodeIdsWhitelist = wlNodeIDs
		}
	}
	if enableSentryNode && ctx.Bool(utils.InitEVNSentryRegister.Name) {
		for i := 0; i < size; i++ {
			configs[i].Eth.EVNNodeIDsToAdd = append(configs[i].Eth.EVNNodeIDsToAdd, sentryNodeIDs[i])
		}
	}

	// write node & sentry configs
	for i, config := range configs {
		err = writeConfig(inGenesisFile, config, path.Join(initDir, fmt.Sprintf("node%d", i)))
		if err != nil {
			return err
		}
	}
	for i, config := range sentryConfigs {
		err = writeConfig(inGenesisFile, config, path.Join(initDir, fmt.Sprintf("sentry%d", i)))
		if err != nil {
			return err
		}
	}

	if ctx.Int(utils.InitFullNodeSize.Name) > 0 {
		var extraEnodes []*enode.Node
		if enableSentryNode {
			extraEnodes = sentryEnodes
		} else {
			extraEnodes = enodes
		}
		_, _, err := createAndSaveFullNodeConfigs(ctx, inGenesisFile, config, initDir, extraEnodes)
		if err != nil {
			utils.Fatalf("Failed to create full node configs: %v", err)
		}
	}

	return nil
}

func createSentryNodeConfigs(ctx *cli.Context, baseConfig gethConfig, initDir string) ([]gethConfig, []*enode.Node, error) {
	size := ctx.Int(utils.InitSentryNodeSize.Name)
	if size <= 0 {
		utils.Fatalf("size should be greater than 0")
	}
	ipStr := ctx.String(utils.InitSentryNodeIPs.Name)
	portStr := ctx.String(utils.InitSentryNodePorts.Name)
	ips, err := parseIps(ipStr, size)
	if err != nil {
		utils.Fatalf("Failed to parse ips: %v", err)
	}
	ports, err := parsePorts(portStr, DefaultSentryP2PPort, size)
	if err != nil {
		utils.Fatalf("Failed to parse ports: %v", err)
	}
	configs, enodes, _, err := createConfigs(baseConfig, initDir, "sentry", ips, ports, nil, false, true)
	if err != nil {
		utils.Fatalf("Failed to create config: %v", err)
	}
	return configs, enodes, nil
}

func createAndSaveFullNodeConfigs(ctx *cli.Context, inGenesisFile *os.File, baseConfig gethConfig, initDir string, extraEnodes []*enode.Node) ([]gethConfig, []*enode.Node, error) {
	size := ctx.Int(utils.InitFullNodeSize.Name)
	if size <= 0 {
		utils.Fatalf("size should be greater than 0")
	}
	ipStr := ctx.String(utils.InitFullNodeIPs.Name)
	portStr := ctx.String(utils.InitFullNodePorts.Name)
	ips, err := parseIps(ipStr, size)
	if err != nil {
		utils.Fatalf("Failed to parse ips: %v", err)
	}
	ports, err := parsePorts(portStr, DefaultFullNodeP2PPort, size)
	if err != nil {
		utils.Fatalf("Failed to parse ports: %v", err)
	}

	configs, enodes, _, err := createConfigs(baseConfig, initDir, "fullnode", ips, ports, extraEnodes, false, false)
	if err != nil {
		utils.Fatalf("Failed to create config: %v", err)
	}

	// write configs
	for i := 0; i < len(configs); i++ {
		err := writeConfig(inGenesisFile, configs[i], path.Join(initDir, fmt.Sprintf("fullnode%d", i)))
		if err != nil {
			utils.Fatalf("Failed to write config: %v", err)
		}
	}
	return configs, enodes, nil
}

func createConfigs(base gethConfig, initDir string, prefix string, ips []string, ports []int, extraEnodes []*enode.Node, connectOneExtraEnodes bool, staticConnect bool) ([]gethConfig, []*enode.Node, [][]common.Address, error) {
	if len(ips) != len(ports) {
		return nil, nil, nil, errors.New("mismatch of size and length of ports")
	}
	size := len(ips)
	enodes := make([]*enode.Node, size)
	accounts := make([][]common.Address, size)
	for i := 0; i < size; i++ {
		nodeConfig := base.Node
		nodeConfig.DataDir = path.Join(initDir, fmt.Sprintf("%s%d", prefix, i))
		stack, err := node.New(&nodeConfig)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := setAccountManagerBackends(stack.Config(), stack.AccountManager(), stack.KeyStoreDir()); err != nil {
			utils.Fatalf("Failed to set account manager backends: %v", err)
		}
		accounts[i] = stack.AccountManager().Accounts()
		pk := stack.Config().NodeKey()
		enodes[i] = enode.NewV4(&pk.PublicKey, net.ParseIP(ips[i]), ports[i], ports[i])
	}

	allEnodes := append(enodes, extraEnodes...)
	configs := make([]gethConfig, size)
	for i := 0; i < size; i++ {
		index := i
		if connectOneExtraEnodes {
			// only connect to one extra enode with same index
			allEnodes = []*enode.Node{enodes[i], extraEnodes[i]}
			index = 0
		}
		configs[i] = createNodeConfig(base, ips[i], ports[i], allEnodes, index, staticConnect)
	}
	return configs, enodes, accounts, nil
}

func writeConfig(inGenesisFile *os.File, config gethConfig, dir string) error {
	configBytes, err := tomlSettings.Marshal(config)
	if err != nil {
		return err
	}
	configFile, err := os.OpenFile(path.Join(dir, "config.toml"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer configFile.Close()
	_, err = configFile.Write(configBytes)
	if err != nil {
		return err
	}

	// Write the input genesis.json to the node's directory
	outGenesisFile, err := os.OpenFile(path.Join(dir, "genesis.json"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	_, err = inGenesisFile.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	_, err = io.Copy(outGenesisFile, inGenesisFile)
	if err != nil {
		return err
	}
	return nil
}

func dumpGenesis(ctx *cli.Context) error {
	// check if there is a testnet preset enabled
	var genesis *core.Genesis
	if utils.IsNetworkPreset(ctx) {
		genesis = utils.MakeGenesis(ctx)
	} else if ctx.IsSet(utils.DeveloperFlag.Name) && !ctx.IsSet(utils.DataDirFlag.Name) {
		genesis = core.DeveloperGenesisBlock(11_500_000, nil)
	}

	if genesis != nil {
		if err := json.NewEncoder(os.Stdout).Encode(genesis); err != nil {
			utils.Fatalf("could not encode genesis: %s", err)
		}
		return nil
	}

	// dump whatever already exists in the datadir
	stack, _ := makeConfigNode(ctx)
	db, err := stack.OpenDatabase("chaindata", 0, 0, "", true)
	if err != nil {
		return err
	}
	defer db.Close()

	// set the separate state & block database
	if stack.CheckIfMultiDataBase() && err == nil {
		stateDiskDb := utils.MakeStateDataBase(ctx, stack, true, false)
		db.SetStateStore(stateDiskDb)
		blockDb := utils.MakeBlockDatabase(ctx, stack, true, false)
		db.SetBlockStore(blockDb)
	}

	genesis, err = core.ReadGenesis(db)
	if err != nil {
		utils.Fatalf("failed to read genesis: %s", err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(*genesis); err != nil {
		utils.Fatalf("could not encode stored genesis: %s", err)
	}

	return nil
}

func importChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}
	stack, cfg := makeConfigNode(ctx)
	defer stack.Close()

	// Start metrics export if enabled
	utils.SetupMetrics(&cfg.Metrics)

	backend, err := eth.New(stack, &cfg.Eth)
	if err != nil {
		return err
	}

	chain := backend.BlockChain()
	db := backend.ChainDb()
	defer db.Close()

	// Start periodically gathering memory profiles
	var peakMemAlloc, peakMemSys atomic.Uint64
	go func() {
		stats := new(runtime.MemStats)
		for {
			runtime.ReadMemStats(stats)
			if peakMemAlloc.Load() < stats.Alloc {
				peakMemAlloc.Store(stats.Alloc)
			}
			if peakMemSys.Load() < stats.Sys {
				peakMemSys.Store(stats.Sys)
			}
			time.Sleep(5 * time.Second)
		}
	}()
	// Import the chain
	start := time.Now()

	var importErr error

	if ctx.Args().Len() == 1 {
		if err := utils.ImportChain(chain, ctx.Args().First()); err != nil {
			importErr = err
			log.Error("Import error", "err", err)
		}
	} else {
		for _, arg := range ctx.Args().Slice() {
			if err := utils.ImportChain(chain, arg); err != nil {
				importErr = err
				log.Error("Import error", "file", arg, "err", err)
			}
		}
	}
	chain.Stop()
	fmt.Printf("Import done in %v.\n\n", time.Since(start))

	// Output pre-compaction stats mostly to see the import trashing
	showDBStats(db)

	// Print the memory statistics used by the importing
	mem := new(runtime.MemStats)
	runtime.ReadMemStats(mem)

	fmt.Printf("Object memory: %.3f MB current, %.3f MB peak\n", float64(mem.Alloc)/1024/1024, float64(peakMemAlloc.Load())/1024/1024)
	fmt.Printf("System memory: %.3f MB current, %.3f MB peak\n", float64(mem.Sys)/1024/1024, float64(peakMemSys.Load())/1024/1024)
	fmt.Printf("Allocations:   %.3f million\n", float64(mem.Mallocs)/1000000)
	fmt.Printf("GC pause:      %v\n\n", time.Duration(mem.PauseTotalNs))

	if ctx.Bool(utils.NoCompactionFlag.Name) {
		return nil
	}

	// Compact the entire database to more accurately measure disk io and print the stats
	start = time.Now()
	fmt.Println("Compacting entire database...")
	if err := db.Compact(nil, nil); err != nil {
		utils.Fatalf("Compaction failed: %v", err)
	}
	fmt.Printf("Compaction done in %v.\n\n", time.Since(start))

	showDBStats(db)
	return importErr
}

func exportChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, db := utils.MakeChain(ctx, stack, true)
	defer db.Close()
	start := time.Now()

	var err error
	fp := ctx.Args().First()
	if ctx.Args().Len() < 3 {
		err = utils.ExportChain(chain, fp)
	} else {
		// This can be improved to allow for numbers larger than 9223372036854775807
		first, ferr := strconv.ParseInt(ctx.Args().Get(1), 10, 64)
		last, lerr := strconv.ParseInt(ctx.Args().Get(2), 10, 64)
		if ferr != nil || lerr != nil {
			utils.Fatalf("Export error in parsing parameters: block number not an integer\n")
		}
		if first < 0 || last < 0 {
			utils.Fatalf("Export error: block number must be greater than 0\n")
		}
		if head := chain.CurrentSnapBlock(); uint64(last) > head.Number.Uint64() {
			utils.Fatalf("Export error: block number %d larger than head block %d\n", uint64(last), head.Number.Uint64())
		}
		err = utils.ExportAppendChain(chain, fp, uint64(first), uint64(last))
	}
	if err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

func importHistory(ctx *cli.Context) error {
	if ctx.Args().Len() != 1 {
		utils.Fatalf("usage: %s", ctx.Command.ArgsUsage)
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, db := utils.MakeChain(ctx, stack, false)
	defer db.Close()

	var (
		start   = time.Now()
		dir     = ctx.Args().Get(0)
		network string
	)

	// Determine network.
	if utils.IsNetworkPreset(ctx) {
		switch {
		case ctx.Bool(utils.BSCMainnetFlag.Name):
			network = "mainnet"
		case ctx.Bool(utils.ChapelFlag.Name):
			network = "chapel"
		}
	} else {
		// No network flag set, try to determine network based on files
		// present in directory.
		var networks []string
		for _, n := range params.NetworkNames {
			entries, err := era.ReadDir(dir, n)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", dir, err)
			}
			if len(entries) > 0 {
				networks = append(networks, n)
			}
		}
		if len(networks) == 0 {
			return fmt.Errorf("no era1 files found in %s", dir)
		}
		if len(networks) > 1 {
			return errors.New("multiple networks found, use a network flag to specify desired network")
		}
		network = networks[0]
	}

	if err := utils.ImportHistory(chain, db, dir, network); err != nil {
		return err
	}
	fmt.Printf("Import done in %v\n", time.Since(start))
	return nil
}

// exportHistory exports chain history in Era archives at a specified
// directory.
func exportHistory(ctx *cli.Context) error {
	if ctx.Args().Len() != 3 {
		utils.Fatalf("usage: %s", ctx.Command.ArgsUsage)
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, _ := utils.MakeChain(ctx, stack, true)
	start := time.Now()

	var (
		dir         = ctx.Args().Get(0)
		first, ferr = strconv.ParseInt(ctx.Args().Get(1), 10, 64)
		last, lerr  = strconv.ParseInt(ctx.Args().Get(2), 10, 64)
	)
	if ferr != nil || lerr != nil {
		utils.Fatalf("Export error in parsing parameters: block number not an integer\n")
	}
	if first < 0 || last < 0 {
		utils.Fatalf("Export error: block number must be greater than 0\n")
	}
	if head := chain.CurrentSnapBlock(); uint64(last) > head.Number.Uint64() {
		utils.Fatalf("Export error: block number %d larger than head block %d\n", uint64(last), head.Number.Uint64())
	}
	err := utils.ExportHistory(chain, dir, uint64(first), uint64(last), uint64(era.MaxEra1Size))
	if err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

// importPreimages imports preimage data from the specified file.
// it is deprecated, and the export function has been removed, but
// the import function is kept around for the time being so that
// older file formats can still be imported.
func importPreimages(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false, false)
	defer db.Close()
	start := time.Now()

	if err := utils.ImportPreimages(db, ctx.Args().First()); err != nil {
		utils.Fatalf("Import error: %v\n", err)
	}
	fmt.Printf("Import done in %v\n", time.Since(start))
	return nil
}

func parseDumpConfig(ctx *cli.Context, stack *node.Node, db ethdb.Database) (*state.DumpConfig, common.Hash, error) {
	var header *types.Header
	if ctx.NArg() > 1 {
		return nil, common.Hash{}, fmt.Errorf("expected 1 argument (number or hash), got %d", ctx.NArg())
	}

	scheme, err := rawdb.ParseStateScheme(ctx.String(utils.StateSchemeFlag.Name), db)
	if err != nil {
		return nil, common.Hash{}, err
	}
	if scheme == rawdb.PathScheme {
		fmt.Println("You are using geth dump in path mode, please use `geth dump-roothash` command to get all available blocks.")
	}

	if ctx.NArg() == 1 {
		arg := ctx.Args().First()
		if hashish(arg) {
			hash := common.HexToHash(arg)
			if number := rawdb.ReadHeaderNumber(db, hash); number != nil {
				header = rawdb.ReadHeader(db, hash, *number)
			} else {
				return nil, common.Hash{}, fmt.Errorf("block %x not found", hash)
			}
		} else {
			number, err := strconv.ParseUint(arg, 10, 64)
			if err != nil {
				return nil, common.Hash{}, err
			}
			if hash := rawdb.ReadCanonicalHash(db, number); hash != (common.Hash{}) {
				header = rawdb.ReadHeader(db, hash, number)
			} else {
				return nil, common.Hash{}, fmt.Errorf("header for block %d not found", number)
			}
		}
	} else {
		// Use latest
		if scheme == rawdb.PathScheme {
			triedb := triedb.NewDatabase(db, &triedb.Config{PathDB: utils.PathDBConfigAddJournalFilePath(stack, pathdb.ReadOnly)})
			defer triedb.Close()
			if stateRoot := triedb.Head(); stateRoot != (common.Hash{}) {
				header.Root = stateRoot
			} else {
				return nil, common.Hash{}, errors.New("no top state root hash in path db")
			}
		} else {
			header = rawdb.ReadHeadHeader(db)
		}
	}
	if header == nil {
		return nil, common.Hash{}, errors.New("no head block found")
	}

	startArg := common.FromHex(ctx.String(utils.StartKeyFlag.Name))
	var start common.Hash
	switch len(startArg) {
	case 0: // common.Hash
	case 32:
		start = common.BytesToHash(startArg)
	case 20:
		start = crypto.Keccak256Hash(startArg)
		log.Info("Converting start-address to hash", "address", common.BytesToAddress(startArg), "hash", start.Hex())
	default:
		return nil, common.Hash{}, fmt.Errorf("invalid start argument: %x. 20 or 32 hex-encoded bytes required", startArg)
	}
	conf := &state.DumpConfig{
		SkipCode:          ctx.Bool(utils.ExcludeCodeFlag.Name),
		SkipStorage:       ctx.Bool(utils.ExcludeStorageFlag.Name),
		OnlyWithAddresses: !ctx.Bool(utils.IncludeIncompletesFlag.Name),
		Start:             start.Bytes(),
		Max:               ctx.Uint64(utils.DumpLimitFlag.Name),
	}
	conf.StateScheme = scheme
	log.Info("State dump configured", "block", header.Number, "hash", header.Hash().Hex(),
		"skipcode", conf.SkipCode, "skipstorage", conf.SkipStorage, "start", hexutil.Encode(conf.Start),
		"limit", conf.Max, "state scheme", conf.StateScheme)
	return conf, header.Root, nil
}

func dump(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()

	conf, root, err := parseDumpConfig(ctx, stack, db)
	if err != nil {
		return err
	}
	defer db.Close()
	triedb := utils.MakeTrieDatabase(ctx, stack, db, true, true, false) // always enable preimage lookup
	defer triedb.Close()

	state, err := state.New(root, state.NewDatabase(triedb, nil))
	if err != nil {
		return err
	}
	if ctx.Bool(utils.IterativeOutputFlag.Name) {
		state.IterativeDump(conf, json.NewEncoder(os.Stdout))
	} else {
		fmt.Println(string(state.Dump(conf)))
	}
	return nil
}

func dumpAllRootHashInPath(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()
	db := utils.MakeChainDatabase(ctx, stack, true, false)
	defer db.Close()
	triedb := triedb.NewDatabase(db, &triedb.Config{PathDB: utils.PathDBConfigAddJournalFilePath(stack, pathdb.ReadOnly)})
	defer triedb.Close()

	scheme, err := rawdb.ParseStateScheme(ctx.String(utils.StateSchemeFlag.Name), db)
	if err != nil {
		return err
	}
	if scheme == rawdb.HashScheme {
		return errors.New("incorrect state scheme, you should use it in path mode")
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Block Number", "Block State Root Hash"})
	table.AppendBulk(triedb.GetAllRooHash())
	table.Render()
	return nil
}

// hashish returns true for strings that look like hashes.
func hashish(x string) bool {
	_, err := strconv.Atoi(x)
	return err != nil
}
