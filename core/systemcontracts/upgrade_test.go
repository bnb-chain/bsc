package systemcontracts

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
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

// TestGaussPaymentLaneCode pins the Gauss hardfork's PaymentLane bytecode. It is a separate
// test rather than an entry in TestAllCodesHash because that one is a rolling digest over 13
// hardforks: extending it re-pins every historical fork at once.
//
// Four things fail here if the deployment breaks: a missing network registration, a wrong
// target address, bytecode that is not valid hex (which would panic mid-block inside
// applySystemContractUpgrade rather than return a consensus error), and any bytecode change.
// The single expected hash also encodes the invariant that all three networks ship identical
// code - PaymentLane has no network-specific constants.
func TestGaussPaymentLaneCode(t *testing.T) {
	const wantCodeHash = "290015376dcf26ec7e889c82b67ef6382a277d3bfbe48fda1901d9ab00d15ef7"

	for _, network := range []string{mainNet, chapelNet, rialtoNet} {
		upgrade := gaussUpgrade[network]
		require.NotNil(t, upgrade, network)
		require.Equal(t, "gauss", upgrade.UpgradeName, network)
		require.Len(t, upgrade.Configs, 1, network)

		config := upgrade.Configs[0]
		require.Equal(t, common.HexToAddress(PaymentLaneContract), config.ContractAddr, network)

		code, err := hex.DecodeString(strings.TrimSpace(config.Code))
		require.NoError(t, err, network)

		codeHash := sha256.Sum256(code)
		require.Equal(t, wantCodeHash, hex.EncodeToString(codeHash[:]), network)
	}
}

// TestGaussUpgradeApplies drives the real dispatcher and asserts PaymentLane's code lands at
// 0x2007 on the transition block and only there. TestGaussPaymentLaneCode above checks the
// registration data; this checks that the IsOnGauss branch in upgradeBuildInSystemContract
// exists and is reached, which the data test alone would not catch.
func TestGaussUpgradeApplies(t *testing.T) {
	const (
		gaussTime  uint64 = 1_800_000_000
		blockTime         = gaussTime + 3 // first block at or after the fork time
		parentTime        = gaussTime - 3
	)
	addr := common.HexToAddress(PaymentLaneContract)
	blockNumber := big.NewInt(60_000_000)

	forkTime := uint64(gaussTime)
	config := *params.BSCChainConfig // copy: never mutate the shared mainnet config
	config.GaussTime = &forkTime
	GenesisHash = params.BSCGenesisHash

	// The transition block installs the code onto a previously non-existent account.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	require.Empty(t, statedb.GetCode(addr))

	require.True(t, config.IsOnGauss(blockNumber, parentTime, blockTime))
	upgradeBuildInSystemContract(&config, blockNumber, parentTime, blockTime, statedb)

	want, err := hex.DecodeString(strings.TrimSpace(gauss.MainnetPaymentLaneContract))
	require.NoError(t, err)
	require.Equal(t, want, statedb.GetCode(addr))

	// A later block is not the transition block, so nothing is installed.
	next, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	require.False(t, config.IsOnGauss(new(big.Int).Add(blockNumber, common.Big1), blockTime, blockTime+3))
	upgradeBuildInSystemContract(&config, new(big.Int).Add(blockNumber, common.Big1), blockTime, blockTime+3, next)
	require.Empty(t, next.GetCode(addr))
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
