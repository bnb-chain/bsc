package feynman_fix

import _ "embed"

// contract codes for Mainnet upgrade
var ()

// contract codes for Chapel upgrade
var (
	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string
	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string
)

// contract codes for Rialto upgrade
var ()
