package maxwell

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/StakeHubContract
	MainnetStakeHubContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string
)
