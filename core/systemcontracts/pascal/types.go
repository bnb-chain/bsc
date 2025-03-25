package pascal

import _ "embed"

// contract codes for Mainnet upgrade
var (

	//go:embed mainnet/ValidatorContract
	MainnetValidatorContract string

	//go:embed mainnet/SlashContract
	MainnetSlashContract string

	//go:embed mainnet/SystemRewardContract
	MainnetSystemRewardContract string

	//go:embed mainnet/LightClientContract
	MainnetLightClientContract string

	//go:embed mainnet/TokenHubContract
	MainnetTokenHubContract string

	//go:embed mainnet/RelayerIncentivizeContract
	MainnetRelayerIncentivizeContract string

	//go:embed mainnet/RelayerHubContract
	MainnetRelayerHubContract string

	//go:embed mainnet/GovHubContract
	MainnetGovHubContract string

	//go:embed mainnet/TokenManagerContract
	MainnetTokenManagerContract string

	//go:embed mainnet/CrossChainContract
	MainnetCrossChainContract string

	//go:embed mainnet/StakingContract
	MainnetStakingContract string

	//go:embed mainnet/StakeHubContract
	MainnetStakeHubContract string

	//go:embed mainnet/StakeCreditContract
	MainnetStakeCreditContract string

	//go:embed mainnet/GovernorContract
	MainnetGovernorContract string

	//go:embed mainnet/GovTokenContract
	MainnetGovTokenContract string

	//go:embed mainnet/TimelockContract
	MainnetTimelockContract string

	//go:embed mainnet/TokenRecoverPortalContract
	MainnetTokenRecoverPortalContract string
)

// contract codes for Chapel upgrade
var (

	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string

	//go:embed chapel/SlashContract
	ChapelSlashContract string

	//go:embed chapel/SystemRewardContract
	ChapelSystemRewardContract string

	//go:embed chapel/LightClientContract
	ChapelLightClientContract string

	//go:embed chapel/TokenHubContract
	ChapelTokenHubContract string

	//go:embed chapel/RelayerIncentivizeContract
	ChapelRelayerIncentivizeContract string

	//go:embed chapel/RelayerHubContract
	ChapelRelayerHubContract string

	//go:embed chapel/GovHubContract
	ChapelGovHubContract string

	//go:embed chapel/TokenManagerContract
	ChapelTokenManagerContract string

	//go:embed chapel/CrossChainContract
	ChapelCrossChainContract string

	//go:embed chapel/StakingContract
	ChapelStakingContract string

	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string

	//go:embed chapel/StakeCreditContract
	ChapelStakeCreditContract string

	//go:embed chapel/GovernorContract
	ChapelGovernorContract string

	//go:embed chapel/GovTokenContract
	ChapelGovTokenContract string

	//go:embed chapel/TimelockContract
	ChapelTimelockContract string

	//go:embed chapel/TokenRecoverPortalContract
	ChapelTokenRecoverPortalContract string
)
