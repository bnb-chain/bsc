package kepler

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/ValidatorContract
	MainnetValidatorContract string
	//go:embed mainnet/SlashContract
	MainnetSlashContract string
	//go:embed mainnet/SystemRewardContract
	MainnetSystemRewardContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string
	//go:embed chapel/SlashContract
	ChapelSlashContract string
	//go:embed chapel/SystemRewardContract
	ChapelSystemRewardContract string
)
