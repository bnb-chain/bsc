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
	const wantCodeHash = "8fd1686e0e53d7d6840d05ac4a3a1da1d322c3a742012428eaa63b6931add259"

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
