package fermi

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/SlashContract
	MainnetSlashContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/SlashContract
	ChapelSlashContract string
)

// contract codes for Rialto upgrade
var (
	//go:embed rialto/SlashContract
	RialtoSlashContract string
)
