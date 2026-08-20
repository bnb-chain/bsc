package params

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestB20ActivationAdminIsReachable refuses the two values that would make B20
// permanently inert on a public network.
//
// The seeding is one-shot: it runs on the fork boundary block and writes the
// admin only into an empty slot, and setAdmin requires the caller to be the
// current admin. An admin that cannot originate a call is therefore unreplaceable
// by any transaction — only a further hard fork can install one.
//
// Two such values are known:
//
//   - the zero address, which requireAdmin rejects outright
//   - BSC's governance timelock, which cannot reach the registry: BSCGovernor's
//     whitelistTargets holds only GovHub and has no setter, and the timelock's
//     sole executor is the governor, so every timelock action is gated by that
//     whitelist
//
// A multisig or an EOA is reachable and can hand the switch on through setAdmin
// once a governance path exists.
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
			t.Errorf("%s: activation admin is the governance timelock, which cannot call "+
				"the registry (BSCGovernor whitelists only GovHub). It could not even "+
				"call setAdmin, so the network would be permanently unable to activate.", tc.name)
		}
	}
}

// TestB20IsNotScheduledWithAnUnusableAdmin is the release gate.
//
// Seeding is one-shot and setAdmin requires the current admin to call, so a
// network that crosses the fork with an unusable admin can never open a feature
// and can never replace the admin either. The tripwire below only says the
// decision is still open; it passes precisely while the placeholder is in place,
// so it cannot catch the case that matters — a scheduled fork paired with an
// admin nobody holds.
//
// Chapel is why this exists: its PasteurTime is 2026-07-21, already in the past,
// while its admin is still the placeholder. B20 is gated on Pasteur only as a
// stand-in for a fork flag of its own, so without the B20Scheduled check that
// config would route the whole reserved address space and seed a registry whose
// switch nobody can ever touch — and, since the boundary has passed, a fresh sync
// would write state that nodes running the released client never wrote.
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
			t.Errorf("%s: B20Scheduled reports true with admin %v. B20Scheduled must refuse "+
				"nil, the zero address and the placeholder, or a network can ship a switch "+
				"nobody can ever throw", tc.name, admin)
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
			t.Errorf("%s: B20 is scheduled. If the real admin has been chosen, delete "+
				"TestB20ActivationAdminIsStillAPlaceholder and this loop, and verify the "+
				"account can reach the ActivationRegistry first", tc.name)
		}
	}
}

// TestB20ActivationAdminIsStillAPlaceholder is a tripwire, not an invariant.
//
// The three built-in configs name B20ActivationAdminPlaceholder, which is not a
// real account. The choice is one-shot and unrecoverable, so it must be replaced
// with the multisig that will hold the switch before this fork is scheduled on
// any public network.
//
// When that address is chosen, this test starts failing. That is the signal to
// delete it — not to update it to the new value, which would leave nothing
// marking the decision as made.
func TestB20ActivationAdminIsStillAPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"bsc", BSCChainConfig},
		{"chapel", ChapelChainConfig},
		{"rialto", RialtoChainConfig},
	} {
		if got := tc.cfg.B20ActivationAdmin; got == nil || *got != B20ActivationAdminPlaceholder {
			t.Errorf("%s: activation admin is %v, no longer the placeholder — if this is the "+
				"real multisig, delete this test; the switch cannot be changed after the fork",
				tc.name, got)
		}
	}
}
