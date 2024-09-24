package systemcontracts

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
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
