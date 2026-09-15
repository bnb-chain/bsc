package systemcontracts

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestAllCodesHash(t *testing.T) {
	upgradesList := [13]map[string]*Upgrade{
		ramanujanUpgrade,
		nielsUpgrade,
		mirrorUpgrade,
		brunoUpgrade,
		eulerUpgrade,
		gibbsUpgrade,
		moranUpgrade,
		planckUpgrade,
		lubanUpgrade,
		platoUpgrade,
		keplerUpgrade,
		feynmanUpgrade,
		feynmanFixUpgrade}

	allCodes := make([]byte, 0, 10_000_000)
	for _, hardfork := range upgradesList {
		for _, network := range []string{mainNet, chapelNet} {
			allCodes = append(allCodes, []byte(network)...)
			if hardfork[network] != nil {
				for _, addressConfig := range hardfork[network].Configs {
					allCodes = append(allCodes, addressConfig.ContractAddr[:]...)
					allCodes = append(allCodes, addressConfig.Code[:]...)
				}
			}
		}
	}
	allCodeHash := sha256.Sum256(allCodes)
	require.Equal(t, allCodeHash[:], common.Hex2Bytes("833cc0fc87c46ad8a223e44ccfdc16a51a7e7383525136441bd0c730f06023df"))
}

// Pin the Jenner payment-lane address and bytecode on all three networks.
func TestJennerPaymentLaneCode(t *testing.T) {
	const wantCodeHash = "ebee6a014126a2c3c3549e1b9e8924eff695f3f9c99324ada3104616b164a213"

	for _, network := range []string{mainNet, chapelNet, rialtoNet} {
		upgrade := jennerUpgrade[network]
		require.NotNil(t, upgrade, network)
		require.Len(t, upgrade.Configs, 1, network)

		config := upgrade.Configs[0]
		require.Equal(t, common.HexToAddress("0x0000000000000000000000000000000000002007"), config.ContractAddr, network)

		// applySystemContractUpgrade trims before decoding too.
		code, err := hex.DecodeString(strings.TrimSpace(config.Code))
		require.NoError(t, err, network)

		codeHash := sha256.Sum256(code)
		require.Equal(t, wantCodeHash, hex.EncodeToString(codeHash[:]), network)
	}
}

// Drive the real dispatcher so a missing IsOnJenner branch is caught.
func TestJennerUpgradeApplies(t *testing.T) {
	const (
		jennerTime    uint64 = 1_800_000_000
		blockTime            = jennerTime + 3 // first block at or after the fork time
		lastBlockTime        = jennerTime - 3
	)
	addr := common.HexToAddress(PaymentLaneContract)
	blockNumber := big.NewInt(60_000_000)

	forkTime := jennerTime
	config := *params.BSCChainConfig // copy: never mutate the shared mainnet config
	config.JennerTime = &forkTime
	GenesisHash = params.BSCGenesisHash

	// The transition block installs the code.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	require.Empty(t, statedb.GetCode(addr))
	upgradeBuildInSystemContract(&config, blockNumber, lastBlockTime, blockTime, statedb)
	require.NotEmpty(t, statedb.GetCode(addr))

	// Later blocks do not reinstall it.
	next, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	upgradeBuildInSystemContract(&config, new(big.Int).Add(blockNumber, common.Big1), blockTime, blockTime+3, next)
	require.Empty(t, next.GetCode(addr))
}

// TestCAS20SentinelsPlantedAtFork pins the boundary hook: the two registries get
// their account sentinels on the block that crosses Jenner, and on no other.
// The constant is duplicated because this package imports core/vm; a drift would
// silently move the ActivationRegistry's governance root.
func TestCAS20GovHubAddressMatchesTheSystemContract(t *testing.T) {
	if params.CAS20GovHubAddress != common.HexToAddress(GovHubContract) {
		t.Fatalf("params.CAS20GovHubAddress = %s, GovHubContract = %s", params.CAS20GovHubAddress, GovHubContract)
	}
}

func TestCAS20SentinelsPlantedAtFork(t *testing.T) {
	const forkTime = 1000
	// The fork is timestamp-based but still requires London, which on BSC is at
	// block 31302048 — a block number below it would make the predicate false for
	// reasons that have nothing to do with the fork under test.
	postLondon := big.NewInt(50_000_000)

	newState := func() *state.StateDB {
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		return statedb
	}
	bscConfig := func() *params.ChainConfig {
		cfg := *params.BSCChainConfig
		ft := uint64(forkTime)
		cfg.JennerTime = &ft
		return &cfg
	}
	planted := func(statedb *state.StateDB) bool {
		return len(statedb.GetCode(vm.CAS20ActivationRegistryAddress)) != 0 &&
			len(statedb.GetCode(vm.CAS20PolicyRegistryAddress)) != 0
	}

	// Born Jenner-active: nothing ever crosses the fork, so the sentinels have to
	// come from the genesis alloc.
	nonBSC := func() *params.ChainConfig {
		cfg := *bscConfig()
		cfg.Parlia = nil
		return &cfg
	}
	bornActive := func() *params.ChainConfig {
		cfg := *params.BSCChainConfig
		zero := uint64(0)
		cfg.JennerTime = &zero
		cfg.LondonBlock = big.NewInt(0)
		return &cfg
	}

	for _, tc := range []struct {
		name                     string
		cfg                      *params.ChainConfig
		number                   *big.Int
		lastBlockTime, blockTime uint64
		atBlockBegin             bool
		want                     bool
	}{
		{"the block crossing the fork", bscConfig(), postLondon, forkTime - 1, forkTime, true, true},
		{"wholly before the fork", bscConfig(), postLondon, forkTime - 2, forkTime - 1, true, false},
		{"wholly after the boundary", bscConfig(), postLondon, forkTime + 1, forkTime + 2, true, false},
		{"the block-end pass", bscConfig(), postLondon, forkTime - 1, forkTime, false, false},

		{"block 1 of a chain born active", bornActive(), big.NewInt(1), 100, 200, true, false},
		{"block 2 of a chain born active", bornActive(), big.NewInt(2), 200, 300, true, false},
		{"block 1 before the fork is scheduled", bscConfig(), big.NewInt(1), 1, 2, true, false},
		{"a non-BSC chain at the boundary", nonBSC(), postLondon, forkTime - 1, forkTime, true, false},
	} {
		statedb := newState()
		TryUpdateBuildInSystemContract(tc.cfg, tc.number, tc.lastBlockTime, tc.blockTime, statedb, tc.atBlockBegin)
		if got := planted(statedb); got != tc.want {
			t.Errorf("%s: sentinels planted = %v, want %v", tc.name, got, tc.want)
		}
	}
}
