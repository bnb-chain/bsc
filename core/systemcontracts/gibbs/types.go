package gibbs

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/TokenHubContract
	MainnetTokenHubContract string
	//go:embed mainnet/StakingContract
	MainnetStakingContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/TokenHubContract
	ChapelTokenHubContract string
	//go:embed chapel/StakingContract
	ChapelStakingContract string
)
