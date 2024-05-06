package mirror

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/TokenManagerContract
	MainnetTokenManagerContract string
	//go:embed mainnet/TokenHubContract
	MainnetTokenHubContract string
	//go:embed mainnet/RelayerIncentivizeContract
	MainnetRelayerIncentivizeContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/TokenManagerContract
	ChapelTokenManagerContract string
	//go:embed chapel/TokenHubContract
	ChapelTokenHubContract string
	//go:embed chapel/RelayerIncentivizeContract
	ChapelRelayerIncentivizeContract string
)
