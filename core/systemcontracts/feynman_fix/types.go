package feynman_fix

import _ "embed"

// contract codes for Chapel upgrade
var (
	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string
	//go:embed chapel/StakeHubContract
	ChapelStakeHubContract string
)
