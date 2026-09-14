package jenner

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/PaymentLaneContract
	MainnetPaymentLaneContract string
	//go:embed mainnet/ValidatorContract
	MainnetValidatorContract string
	//go:embed mainnet/SlashContract
	MainnetSlashContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/PaymentLaneContract
	ChapelPaymentLaneContract string
	//go:embed chapel/ValidatorContract
	ChapelValidatorContract string
	//go:embed chapel/SlashContract
	ChapelSlashContract string
)

// contract codes for Rialto upgrade
var (
	//go:embed rialto/PaymentLaneContract
	RialtoPaymentLaneContract string
	//go:embed rialto/ValidatorContract
	RialtoValidatorContract string
	//go:embed rialto/SlashContract
	RialtoSlashContract string
)
