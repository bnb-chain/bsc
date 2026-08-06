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

// TestGaussPaymentLaneCode pins PaymentLane's target address and bytecode. gaussUpgrade is not in
// TestAllCodesHash's list, which also skips rialtoNet. The address is a literal rather than
// common.HexToAddress(PaymentLaneContract), which would be tautological: a one-digit typo in that
// const lands on Timelock (0x2006) and would overwrite it chain-wide. One hash covers all three
// networks because they ship identical code - which also means no test here can detect a
// cross-network Code mis-wiring, and none needs to while the three blobs stay byte-identical.
func TestGaussPaymentLaneCode(t *testing.T) {
	const wantCodeHash = "c4589f51dc8be028cee0512617008a34a97abb535c3e463a6ba5a5d382ee917c"

	for _, network := range []string{mainNet, chapelNet, rialtoNet} {
		upgrade := gaussUpgrade[network]
		require.NotNil(t, upgrade, network)
		require.Len(t, upgrade.Configs, 1, network)

		config := upgrade.Configs[0]
		require.Equal(t, common.HexToAddress("0x0000000000000000000000000000000000002007"), config.ContractAddr, network)

		// TrimSpace mirrors applySystemContractUpgrade: invalid hex panics mid-block there.
		code, err := hex.DecodeString(strings.TrimSpace(config.Code))
		require.NoError(t, err, network)

		codeHash := sha256.Sum256(code)
		require.Equal(t, wantCodeHash, hex.EncodeToString(codeHash[:]), network)
	}
}

// TestGaussUpgradeApplies drives the real dispatcher: PaymentLane's code must land at 0x2007 on
// the transition block and only there - the data test above cannot see a missing IsOnGauss branch.
func TestGaussUpgradeApplies(t *testing.T) {
	const (
		gaussTime     uint64 = 1_800_000_000
		blockTime            = gaussTime + 3 // first block at or after the fork time
		lastBlockTime        = gaussTime - 3
	)
	addr := common.HexToAddress(PaymentLaneContract)
	blockNumber := big.NewInt(60_000_000)

	forkTime := gaussTime
	config := *params.BSCChainConfig // copy: never mutate the shared mainnet config
	config.GaussTime = &forkTime
	GenesisHash = params.BSCGenesisHash

	// The transition block installs the code onto a previously non-existent account.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	require.Empty(t, statedb.GetCode(addr))
	upgradeBuildInSystemContract(&config, blockNumber, lastBlockTime, blockTime, statedb)
	require.NotEmpty(t, statedb.GetCode(addr))

	// A later block is not the transition block, so nothing is installed.
	next, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
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
