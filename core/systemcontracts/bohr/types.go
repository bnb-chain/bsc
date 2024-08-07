package bohr

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/ValidatorContract
	MainnetValidatorContract string
	//go:embed mainnet/StakeHubContract
	MainnetStakeHubContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string
	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string
)

// contract codes for Rialto upgrade
var (
	//go:embed rialto/ValidatorContract
	RialtoValidatorContract string
	//go:embed rialto/StakeHubContract
	RialtoStakeHubContract string
)
