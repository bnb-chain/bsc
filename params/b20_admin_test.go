package params

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestB20ActivationAdminIsReachable refuses the two values that would make B20
// permanently inert on a public network.
func TestB20ActivationAdminIsReachable(t *testing.T) {
	timelock := common.HexToAddress("0x0000000000000000000000000000000000002006")

	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"bsc", BSCChainConfig},
		{"chapel", ChapelChainConfig},
		{"rialto", RialtoChainConfig},
	} {
		got := tc.cfg.B20ActivationAdmin
		if got == nil || *got == (common.Address{}) {
			t.Errorf("%s: no activation admin — B20 would ship permanently inert, "+
				"and no transaction could ever set one", tc.name)
			continue
		}
		if *got == timelock {
			t.Errorf("%s: activation admin is the governance timelock, which cannot call the registry (BSCGovernor whitelists only GovHub). It could not even", tc.name)
		}
	}
}

// TestB20IsNotScheduledWithAnUnusableAdmin is the release gate.
func TestB20IsNotScheduledWithAnUnusableAdmin(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"bsc", BSCChainConfig},
		{"chapel", ChapelChainConfig},
		{"rialto", RialtoChainConfig},
	} {
		// The gate the EVM and the fork hook both consult.
		if !tc.cfg.B20Scheduled() {
			continue
		}
		admin := tc.cfg.B20ActivationAdmin
		if admin == nil || *admin == (common.Address{}) || *admin == B20ActivationAdminPlaceholder {
			t.Errorf("%s: B20Scheduled reports true with admin %v. B20Scheduled must refuse nil, the zero address and the placeholder, or a network can ship a switch", tc.name, admin)
		}
	}

	// And the gate must actually be closed today, on every built-in config: the
	// admin is undecided, so nothing may route or seed.
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"bsc", BSCChainConfig},
		{"chapel", ChapelChainConfig},
		{"rialto", RialtoChainConfig},
	} {
		if tc.cfg.B20Scheduled() {
			t.Errorf("%s: B20 is scheduled. If the real admin has been chosen, delete TestB20ActivationAdminIsStillAPlaceholder and this loop, and verify the", tc.name)
		}
	}
}
