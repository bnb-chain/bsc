package niels

import _ "embed"

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
)
