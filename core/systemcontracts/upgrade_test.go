package systemcontracts

import (
	"crypto/sha256"
	"math/big"
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

func TestUpgradeBuildInSystemContractNilInterface(t *testing.T) {
	var (
		config               = params.BSCChainConfig
		blockNumber          = big.NewInt(37959559)
		lastBlockTime uint64 = 1713419337
		blockTime     uint64 = 1713419340
		statedb       vm.StateDB
	)

	GenesisHash = params.BSCGenesisHash

	upgradeBuildInSystemContract(config, blockNumber, lastBlockTime, blockTime, statedb)
}

func TestUpgradeBuildInSystemContractNilValue(t *testing.T) {
	var (
		config                   = params.BSCChainConfig
		blockNumber              = big.NewInt(37959559)
		lastBlockTime uint64     = 1713419337
		blockTime     uint64     = 1713419340
		statedb       vm.StateDB = (*state.StateDB)(nil)
	)

	GenesisHash = params.BSCGenesisHash

	upgradeBuildInSystemContract(config, blockNumber, lastBlockTime, blockTime, statedb)
}

// TestCAS20SentinelsPlantedAtFork pins the boundary hook: the two registries get
// their account sentinels on the block that crosses Jenner, and on no other. The
// hook writes nothing else — the activation authority is a constant, so there is
// no admin to seed.
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

	// A chain whose genesis is already Jenner-active. Nothing about it ever
	// crosses the fork, so block 1 has to stand in for the boundary: without it
	// the registries keep no code, GovHub refuses them as a proposal target, and
	// the activation admin can never be appointed.
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

		{"block 1 of a chain born active", bornActive(), big.NewInt(1), 100, 200, true, true},
		{"block 2 of a chain born active", bornActive(), big.NewInt(2), 200, 300, true, false},
		{"block 1 before the fork is scheduled", bscConfig(), big.NewInt(1), 1, 2, true, false},
	} {
		statedb := newState()
		TryUpdateBuildInSystemContract(tc.cfg, tc.number, tc.lastBlockTime, tc.blockTime, statedb, tc.atBlockBegin)
		if got := planted(statedb); got != tc.want {
			t.Errorf("%s: sentinels planted = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Planting is BSC-only: a non-Parlia chain gets nothing even at the boundary.
	statedb := newState()
	nonBSC := *bscConfig()
	nonBSC.Parlia = nil
	TryUpdateBuildInSystemContract(&nonBSC, postLondon, forkTime-1, forkTime, statedb, true)
	if planted(statedb) {
		t.Error("a non-BSC chain had sentinels planted")
	}
}
