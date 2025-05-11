package planck

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/SlashContract
	MainnetSlashContract string
	//go:embed mainnet/TokenHubContract
	MainnetTokenHubContract string
	//go:embed mainnet/CrossChainContract
	MainnetCrossChainContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/SlashContract
	ChapelSlashContract string
	//go:embed chapel/TokenHubContract
	ChapelTokenHubContract string
	//go:embed chapel/CrossChainContract
	ChapelCrossChainContract string
	//go:embed chapel/StakingContract
	ChapelStakingContract string
)
