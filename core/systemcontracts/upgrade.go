package systemcontracts

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/bohr"
	"github.com/ethereum/go-ethereum/core/systemcontracts/bruno"
	"github.com/ethereum/go-ethereum/core/systemcontracts/euler"
	"github.com/ethereum/go-ethereum/core/systemcontracts/feynman"
	feynmanFix "github.com/ethereum/go-ethereum/core/systemcontracts/feynman_fix"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gibbs"
	haberFix "github.com/ethereum/go-ethereum/core/systemcontracts/haber_fix"
	"github.com/ethereum/go-ethereum/core/systemcontracts/kepler"
	"github.com/ethereum/go-ethereum/core/systemcontracts/luban"
	"github.com/ethereum/go-ethereum/core/systemcontracts/mirror"
	"github.com/ethereum/go-ethereum/core/systemcontracts/moran"
	"github.com/ethereum/go-ethereum/core/systemcontracts/niels"
	"github.com/ethereum/go-ethereum/core/systemcontracts/planck"
	"github.com/ethereum/go-ethereum/core/systemcontracts/plato"
	"github.com/ethereum/go-ethereum/core/systemcontracts/ramanujan"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

type UpgradeConfig struct {
	BeforeUpgrade upgradeHook
	AfterUpgrade  upgradeHook
	ContractAddr  common.Address
	CommitUrl     string
	Code          string
}

type Upgrade struct {
	UpgradeName string
	Configs     []*UpgradeConfig
}

type upgradeHook func(blockNumber *big.Int, contractAddr common.Address, statedb *state.StateDB) error

const (
	mainNet    = "Mainnet"
	chapelNet  = "Chapel"
	rialtoNet  = "Rialto"
	defaultNet = "Default"
)

var (
	GenesisHash common.Hash
	// upgrade config
	ramanujanUpgrade = make(map[string]*Upgrade)

	nielsUpgrade = make(map[string]*Upgrade)

	mirrorUpgrade = make(map[string]*Upgrade)

	brunoUpgrade = make(map[string]*Upgrade)

	eulerUpgrade = make(map[string]*Upgrade)

	gibbsUpgrade = make(map[string]*Upgrade)

	moranUpgrade = make(map[string]*Upgrade)

	planckUpgrade = make(map[string]*Upgrade)

	lubanUpgrade = make(map[string]*Upgrade)

	platoUpgrade = make(map[string]*Upgrade)

	keplerUpgrade = make(map[string]*Upgrade)

	feynmanUpgrade = make(map[string]*Upgrade)

	feynmanFixUpgrade = make(map[string]*Upgrade)

	haberFixUpgrade = make(map[string]*Upgrade)

	bohrUpgrade = make(map[string]*Upgrade)
)

func init() {
	// For contract upgrades, the following information is from `bsc-genesis-contract`, to be specifically,
	// 1) `CommitUrl` is the specific git commit, based on which the byte code is compiled from;
	// 2) `Code` is the byte code of the contract, which is generated by compiling `bsc-genesis-contract`.
	// You can refer to `https://github.com/bnb-chain/bsc-genesis-contract` to compile the smart contracts and do the verification.

	ramanujanUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "ramanujan",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelSystemRewardContract,
			},
			{
				ContractAddr: common.HexToAddress(LightClientContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelLightClientContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerIncentivizeContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelRelayerIncentivizeContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(GovHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelGovHubContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenManagerContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelTokenManagerContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         ramanujan.ChapelCrossChainContract,
			},
		},
	}

	nielsUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "niels",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelSystemRewardContract,
			},
			{
				ContractAddr: common.HexToAddress(LightClientContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelLightClientContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerIncentivizeContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelRelayerIncentivizeContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(GovHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelGovHubContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenManagerContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelTokenManagerContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/823b953232a344ba3c32d6690e70a245477e5760",
				Code:         niels.ChapelCrossChainContract,
			},
		},
	}

	mirrorUpgrade[mainNet] = &Upgrade{
		UpgradeName: "mirror",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(TokenManagerContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.MainnetTokenManagerContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.MainnetTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerIncentivizeContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.MainnetRelayerIncentivizeContract,
			},
		},
	}

	mirrorUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "mirror",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(TokenManagerContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.ChapelTokenManagerContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerIncentivizeContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/af4f3993303213052222f55c721e661862d19638",
				Code:         mirror.ChapelRelayerIncentivizeContract,
			},
		},
	}

	brunoUpgrade[mainNet] = &Upgrade{
		UpgradeName: "bruno",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/ce622fef469d84ee418fa6181f3ac962412a5f4f",
				Code:         bruno.MainnetValidatorContract,
			},
		},
	}

	brunoUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "bruno",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/ce622fef469d84ee418fa6181f3ac962412a5f4f",
				Code:         bruno.ChapelValidatorContract,
			},
		},
	}

	eulerUpgrade[mainNet] = &Upgrade{
		UpgradeName: "euler",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/db8bb560ac5a1265c685b719c7e976dced162310",
				Code:         euler.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/db8bb560ac5a1265c685b719c7e976dced162310",
				Code:         euler.MainnetSlashContract,
			},
		},
	}

	eulerUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "euler",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/db8bb560ac5a1265c685b719c7e976dced162310",
				Code:         euler.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/db8bb560ac5a1265c685b719c7e976dced162310",
				Code:         euler.ChapelSlashContract,
			},
		},
	}

	gibbsUpgrade[mainNet] = &Upgrade{
		UpgradeName: "gibbs",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/8cfa94e657670d60ac1ff0563cddcf4664f77227",
				Code:         gibbs.MainnetTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(StakingContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/8cfa94e657670d60ac1ff0563cddcf4664f77227",
				Code:         gibbs.MainnetStakingContract,
			},
		},
	}

	gibbsUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "gibbs",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d45b31c12b2c04757284717f4351cb44e81a3a7",
				Code:         gibbs.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(StakingContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d45b31c12b2c04757284717f4351cb44e81a3a7",
				Code:         gibbs.ChapelStakingContract,
			},
		},
	}

	moranUpgrade[mainNet] = &Upgrade{
		UpgradeName: "moran",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.MainnetRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(LightClientContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.MainnetLightClientContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.MainnetCrossChainContract,
			},
		},
	}

	moranUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "moran",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.ChapelRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(LightClientContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.ChapelLightClientContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/c184a00160b6a2d884b4d6efebe1358a047e9e57",
				Code:         moran.ChapelCrossChainContract,
			},
		},
	}

	planckUpgrade[mainNet] = &Upgrade{
		UpgradeName: "planck",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.MainnetSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.MainnetTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.MainnetCrossChainContract,
			},
		},
	}

	planckUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "planck",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.ChapelCrossChainContract,
			},
			{
				ContractAddr: common.HexToAddress(StakingContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/78e13b1d3a5a1b08c9208af94a9b14fc1efda213",
				Code:         planck.ChapelStakingContract,
			},
		},
	}

	lubanUpgrade[mainNet] = &Upgrade{
		UpgradeName: "luban",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.MainnetSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.MainnetSystemRewardContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.MainnetRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.MainnetCrossChainContract,
			},
		},
	}

	lubanUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "luban",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.ChapelSystemRewardContract,
			},
			{
				ContractAddr: common.HexToAddress(RelayerHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.ChapelRelayerHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b144718e94d7a1ebb24a7103202300f08826f369",
				Code:         luban.ChapelCrossChainContract,
			},
		},
	}

	platoUpgrade[mainNet] = &Upgrade{
		UpgradeName: "plato",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/0f352c4623898d92664a46cbfc26c52b79aad838",
				Code:         plato.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/0f352c4623898d92664a46cbfc26c52b79aad838",
				Code:         plato.MainnetSlashContract,
			},
		},
	}

	platoUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "plato",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/0f352c4623898d92664a46cbfc26c52b79aad838",
				Code:         plato.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/0f352c4623898d92664a46cbfc26c52b79aad838",
				Code:         plato.ChapelSlashContract,
			},
		},
	}

	keplerUpgrade[mainNet] = &Upgrade{
		UpgradeName: "kepler",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.MainnetSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.MainnetSystemRewardContract,
			},
		},
	}

	keplerUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "kepler",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(SystemRewardContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b3a5c1fa8882c0e546dc5ba913ce4db77ec9befe",
				Code:         kepler.ChapelSystemRewardContract,
			},
		},
	}

	feynmanUpgrade[mainNet] = &Upgrade{
		UpgradeName: "feynman",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(GovHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetGovHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetCrossChainContract,
			},
			{
				ContractAddr: common.HexToAddress(StakingContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetStakingContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetStakeHubContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeCreditContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetStakeCreditContract,
			},
			{
				ContractAddr: common.HexToAddress(GovernorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetGovernorContract,
			},
			{
				ContractAddr: common.HexToAddress(GovTokenContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetGovTokenContract,
			},
			{
				ContractAddr: common.HexToAddress(TimelockContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetTimelockContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenRecoverPortalContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2dbebb57a0d436d6a30b78c1f123395035249035",
				Code:         feynman.MainnetTokenRecoverPortalContract,
			},
		},
	}

	feynmanUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "feynman",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelSlashContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelTokenHubContract,
			},
			{
				ContractAddr: common.HexToAddress(GovHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelGovHubContract,
			},
			{
				ContractAddr: common.HexToAddress(CrossChainContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelCrossChainContract,
			},
			{
				ContractAddr: common.HexToAddress(StakingContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelStakingContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelStakeHubContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeCreditContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.MainnetStakeCreditContract,
			},
			{
				ContractAddr: common.HexToAddress(GovernorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelGovernorContract,
			},
			{
				ContractAddr: common.HexToAddress(GovTokenContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.MainnetGovTokenContract,
			},
			{
				ContractAddr: common.HexToAddress(TimelockContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelTimelockContract,
			},
			{
				ContractAddr: common.HexToAddress(TokenRecoverPortalContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/9d09d0a3d6332460a810188261ec8195e05aa218",
				Code:         feynman.ChapelTokenRecoverPortalContract,
			},
		},
	}

	// This upgrade is to fix an error on testnet only. So the upgrade config of mainnet is empty.
	feynmanFixUpgrade[mainNet] = &Upgrade{
		UpgradeName: "feynmanFix",
		Configs:     []*UpgradeConfig{},
	}

	feynmanFixUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "feynmanFix",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2d6372ddba77902ef01e45887a425938376d5a5c",
				Code:         feynmanFix.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/2d6372ddba77902ef01e45887a425938376d5a5c",
				Code:         feynmanFix.ChapelStakeHubContract,
			},
		},
	}

	haberFixUpgrade[mainNet] = &Upgrade{
		UpgradeName: "haberFix",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b743ce3f1f1e94c349b175cd6593bc263463b33b",
				Code:         haberFix.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b743ce3f1f1e94c349b175cd6593bc263463b33b",
				Code:         haberFix.MainnetSlashContract,
			},
		},
	}

	haberFixUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "haberFix",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b743ce3f1f1e94c349b175cd6593bc263463b33b",
				Code:         haberFix.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(SlashContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/b743ce3f1f1e94c349b175cd6593bc263463b33b",
				Code:         haberFix.ChapelSlashContract,
			},
		},
	}

	bohrUpgrade[mainNet] = &Upgrade{
		UpgradeName: "bohr",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/04bc57c1876dd543dd3133b2755ba87cc5f1796a",
				Code:         bohr.MainnetValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/04bc57c1876dd543dd3133b2755ba87cc5f1796a",
				Code:         bohr.MainnetStakeHubContract,
			},
		},
	}

	bohrUpgrade[chapelNet] = &Upgrade{
		UpgradeName: "bohr",
		Configs: []*UpgradeConfig{
			{
				ContractAddr: common.HexToAddress(ValidatorContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/04bc57c1876dd543dd3133b2755ba87cc5f1796a",
				Code:         bohr.ChapelValidatorContract,
			},
			{
				ContractAddr: common.HexToAddress(StakeHubContract),
				CommitUrl:    "https://github.com/bnb-chain/bsc-genesis-contract/commit/04bc57c1876dd543dd3133b2755ba87cc5f1796a",
				Code:         bohr.ChapelStakeHubContract,
			},
		},
	}
}

func UpgradeBuildInSystemContract(config *params.ChainConfig, blockNumber *big.Int, lastBlockTime uint64, blockTime uint64, statedb *state.StateDB) {
	if config == nil || blockNumber == nil || statedb == nil {
		return
	}
	var network string
	switch GenesisHash {
	/* Add mainnet genesis hash */
	case params.BSCGenesisHash:
		network = mainNet
	case params.ChapelGenesisHash:
		network = chapelNet
	case params.RialtoGenesisHash:
		network = rialtoNet
	default:
		network = defaultNet
	}

	logger := log.New("system-contract-upgrade", network)
	if config.IsOnRamanujan(blockNumber) {
		applySystemContractUpgrade(ramanujanUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnNiels(blockNumber) {
		applySystemContractUpgrade(nielsUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnMirrorSync(blockNumber) {
		applySystemContractUpgrade(mirrorUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnBruno(blockNumber) {
		applySystemContractUpgrade(brunoUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnEuler(blockNumber) {
		applySystemContractUpgrade(eulerUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnGibbs(blockNumber) {
		applySystemContractUpgrade(gibbsUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnMoran(blockNumber) {
		applySystemContractUpgrade(moranUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnPlanck(blockNumber) {
		applySystemContractUpgrade(planckUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnLuban(blockNumber) {
		applySystemContractUpgrade(lubanUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnPlato(blockNumber) {
		applySystemContractUpgrade(platoUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnShanghai(blockNumber, lastBlockTime, blockTime) {
		logger.Info("Empty upgrade config for shanghai", "height", blockNumber.String())
	}

	if config.IsOnKepler(blockNumber, lastBlockTime, blockTime) {
		applySystemContractUpgrade(keplerUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnFeynman(blockNumber, lastBlockTime, blockTime) {
		applySystemContractUpgrade(feynmanUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnFeynmanFix(blockNumber, lastBlockTime, blockTime) {
		applySystemContractUpgrade(feynmanFixUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnHaberFix(blockNumber, lastBlockTime, blockTime) {
		applySystemContractUpgrade(haberFixUpgrade[network], blockNumber, statedb, logger)
	}

	if config.IsOnBohr(blockNumber, lastBlockTime, blockTime) {
		applySystemContractUpgrade(bohrUpgrade[network], blockNumber, statedb, logger)
	}

	/*
		apply other upgrades
	*/
}

func applySystemContractUpgrade(upgrade *Upgrade, blockNumber *big.Int, statedb *state.StateDB, logger log.Logger) {
	if upgrade == nil {
		logger.Info("Empty upgrade config", "height", blockNumber.String())
		return
	}

	logger.Info(fmt.Sprintf("Apply upgrade %s at height %d", upgrade.UpgradeName, blockNumber.Int64()))
	for _, cfg := range upgrade.Configs {
		logger.Info(fmt.Sprintf("Upgrade contract %s to commit %s", cfg.ContractAddr.String(), cfg.CommitUrl))

		if cfg.BeforeUpgrade != nil {
			err := cfg.BeforeUpgrade(blockNumber, cfg.ContractAddr, statedb)
			if err != nil {
				panic(fmt.Errorf("contract address: %s, execute beforeUpgrade error: %s", cfg.ContractAddr.String(), err.Error()))
			}
		}

		newContractCode, err := hex.DecodeString(strings.TrimSpace(cfg.Code))
		if err != nil {
			panic(fmt.Errorf("failed to decode new contract code: %s", err.Error()))
		}
		statedb.SetCode(cfg.ContractAddr, newContractCode)

		if cfg.AfterUpgrade != nil {
			err := cfg.AfterUpgrade(blockNumber, cfg.ContractAddr, statedb)
			if err != nil {
				panic(fmt.Errorf("contract address: %s, execute afterUpgrade error: %s", cfg.ContractAddr.String(), err.Error()))
			}
		}
	}
}
