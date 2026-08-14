package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestB20SingletonAddresses pins the three fixed addresses as literals. Every
// other test reaches them through these variables, so a change stays green
// everywhere. They are consensus constants published in BEP-702 3.1.
//
// The registries follow base-std's order, activation first; the prefix differs by
// design, so only the ordering is shared.
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
