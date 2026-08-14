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

// TestB20ActivationAdminIsConfigured pins that the seeded admin comes from chain
// configuration (BEP-702 3.15) rather than being fixed in the fork hook. It has
// to: the public networks name a governance timelock, which is a contract and so
// cannot sign anything, while a QA network needs the switch held by a key it can
// actually transact from. Hard-coding the timelock made the post-activation path
// reachable only through a governance proposal or a state override.
func TestB20ActivationAdminIsConfigured(t *testing.T) {
	const forkTime = 1000
	postLondon := big.NewInt(50_000_000)

	seedWith := func(admin *common.Address) common.Address {
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		cfg := *params.BSCChainConfig
		ft := uint64(forkTime)
		cfg.PasteurTime = &ft
		cfg.B20ActivationAdmin = admin
		TryUpdateBuildInSystemContract(&cfg, postLondon, forkTime-1, forkTime, statedb, true)
		return vm.B20ActivationAdmin(statedb)
	}

	// The public configs name their timelock, and it is what gets installed.
	if params.BSCChainConfig.B20ActivationAdmin == nil {
		t.Error("BSCChainConfig names no activation admin — B20 would ship inert on mainnet")
	}
	if got := seedWith(params.BSCChainConfig.B20ActivationAdmin); got != params.B20ActivationAdminPlaceholder {
		t.Errorf("seeded admin = %s, want the configured placeholder %s", got.Hex(), params.B20ActivationAdminPlaceholder.Hex())
	}

	// A QA network can hold the switch with an ordinary account.
	qa := common.HexToAddress("0x0ead11")
	if got := seedWith(&qa); got != qa {
		t.Errorf("seeded admin = %s, want the configured %s", got.Hex(), qa.Hex())
	}

	// And an unset setting ships the code with the switch shut, which is a valid
	// choice rather than an error.
	if got := seedWith(nil); got != (common.Address{}) {
		t.Errorf("seeded admin = %s, want zero when unconfigured", got.Hex())
	}
}

// TestB20ActivationSeededAtFork covers the fork wiring, not the seeding
// itself: that the boundary predicate fires on exactly the first activating
// block, that it is gated to BSC, and that the timelock is what gets installed.
// Without this the seeding could silently never run and B20 would ship inert.
func TestB20ActivationSeededAtFork(t *testing.T) {
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
		cfg.PasteurTime = &ft
		return &cfg
	}
	wantAdmin := params.B20ActivationAdminPlaceholder

	// The block that crosses the fork time seeds; the one after it does not have
	// to, and must not disturb what is already there.
	statedb := newState()
	TryUpdateBuildInSystemContract(bscConfig(), postLondon, forkTime-1, forkTime, statedb, true)
	if got := vm.B20ActivationAdmin(statedb); got != wantAdmin {
		t.Errorf("admin after the fork block = %s, want the configured admin %s", got.Hex(), wantAdmin.Hex())
	}
	if len(statedb.GetCode(vm.B20ActivationRegistryAddress)) == 0 {
		t.Error("activation registry carries no sentinel after the fork block")
	}
	if len(statedb.GetCode(vm.B20PolicyRegistryAddress)) == 0 {
		t.Error("policy registry carries no sentinel after the fork block")
	}

	// A block wholly before the fork seeds nothing.
	statedb = newState()
	TryUpdateBuildInSystemContract(bscConfig(), postLondon, forkTime-2, forkTime-1, statedb, true)
	if got := vm.B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Errorf("admin before the fork = %s, want zero", got.Hex())
	}
	if len(statedb.GetCode(vm.B20ActivationRegistryAddress)) != 0 {
		t.Error("activation registry seeded before the fork")
	}

	// A block wholly after it seeds nothing either: the predicate is a boundary,
	// not a "fork is active" test.
	statedb = newState()
	TryUpdateBuildInSystemContract(bscConfig(), postLondon, forkTime+1, forkTime+2, statedb, true)
	if got := vm.B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Errorf("admin after the boundary = %s, want zero", got.Hex())
	}

	// Seeding is BSC-only: a non-Parlia chain gets nothing even at the boundary.
	statedb = newState()
	nonBSC := *params.BSCChainConfig
	ft := uint64(forkTime)
	nonBSC.PasteurTime = &ft
	nonBSC.Parlia = nil
	TryUpdateBuildInSystemContract(&nonBSC, postLondon, forkTime-1, forkTime, statedb, true)
	if got := vm.B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Errorf("non-BSC chain seeded an admin: %s", got.Hex())
	}

	// The seeding belongs to block begin; the block-end pass must not do it.
	statedb = newState()
	TryUpdateBuildInSystemContract(bscConfig(), postLondon, forkTime-1, forkTime, statedb, false)
	if got := vm.B20ActivationAdmin(statedb); got != (common.Address{}) {
		t.Errorf("block-end pass seeded an admin: %s", got.Hex())
	}
}
