package moran

import _ "embed"

// contract codes for Mainnet upgrade
var (
	//go:embed mainnet/RelayerHubContract
	MainnetRelayerHubContract string
	//go:embed mainnet/LightClientContract
	MainnetLightClientContract string
	//go:embed mainnet/CrossChainContract
	MainnetCrossChainContract string
)

// contract codes for Chapel upgrade
var (
	//go:embed chapel/RelayerHubContract
	ChapelRelayerHubContract string
	//go:embed chapel/LightClientContract
	ChapelLightClientContract string
	//go:embed chapel/CrossChainContract
	ChapelCrossChainContract string
)
