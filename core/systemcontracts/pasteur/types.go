package pasteur

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/StakeHubContract
	MainnetStakeHubContract string

	//go:embed mainnet/GovernorContract
	MainnetGovernorContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string

	//go:embed chapel/GovernorContract
	ChapelGovernorContract string
)

// contract codes for Rialto upgrade
var (
	//go:embed rialto/StakeHubContract
	RialtoStakeHubContract string

	//go:embed rialto/GovernorContract
	RialtoGovernorContract string
)
