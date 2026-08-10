package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestB20SingletonAddresses pins the three fixed addresses as literals.
//
// Every other test refers to them through these variables, so a change to any of
// them stays green everywhere — the same blind spot the address fingerprint had.
// They are consensus constants and appear in BEP-702 3.1, so changing one has to
// mean editing this list too.
//
// The registry slots follow base-std's order — activation first, then policy —
// which is the only reason the order is what it is. The prefix differs from Base
// by design (0x7020 for BSC, 0x8453 for Base, each naming its own network), so
// only the ordering is shared, not the addresses.
func TestB20SingletonAddresses(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  common.Address
		want string
	}{
		{"B20Factory", B20FactoryAddress, "0x20BF000000000000000000000000000000000000"},
		{"ActivationRegistry", B20ActivationRegistryAddress, "0x7020000000000000000000000000000000000001"},
		{"PolicyRegistry", B20PolicyRegistryAddress, "0x7020000000000000000000000000000000000002"},
	} {
		if want := common.HexToAddress(tc.want); tc.got != want {
			t.Errorf("%s = %s, want %s", tc.name, tc.got.Hex(), want.Hex())
		}
	}

	// The factory must not be matched by the token-space check, or creating a
	// token could collide with the factory itself.
	if IsB20Address(B20FactoryAddress) {
		t.Error("the factory must fall outside the reserved token space")
	}
	for _, reg := range []common.Address{B20ActivationRegistryAddress, B20PolicyRegistryAddress} {
		if IsB20Address(reg) {
			t.Errorf("%s must fall outside the reserved token space", reg.Hex())
		}
	}
}
