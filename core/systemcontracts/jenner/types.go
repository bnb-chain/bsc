package jenner

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/PaymentLaneContract
	MainnetPaymentLaneContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/PaymentLaneContract
	ChapelPaymentLaneContract string
)

// contract codes for Rialto upgrade
var (
	//go:embed rialto/PaymentLaneContract
	RialtoPaymentLaneContract string
)
