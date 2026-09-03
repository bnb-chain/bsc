package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestCAS20SingletonAddresses pins the three fixed addresses as literals. Every
// other test reaches them through these variables, so a change stays green
// everywhere. They are consensus constants published in BEP-702 3.1.
func TestCAS20SingletonAddresses(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  common.Address
		want string
	}{
		{"CAS20Factory", CAS20FactoryAddress, "0xCA5F000000000000000000000000000000000000"},
		{"ActivationRegistry", CAS20ActivationRegistryAddress, "0x7020000000000000000000000000000000000001"},
		{"PolicyRegistry", CAS20PolicyRegistryAddress, "0x7020000000000000000000000000000000000002"},
	} {
		if want := common.HexToAddress(tc.want); tc.got != want {
			t.Errorf("%s = %s, want %s", tc.name, tc.got.Hex(), want.Hex())
		}
	}

	// The factory must not be matched by the token-space check, or creating a
	// token could collide with the factory itself.
	if IsCAS20Address(CAS20FactoryAddress) {
		t.Error("the factory must fall outside the reserved token space")
	}
	for _, reg := range []common.Address{CAS20ActivationRegistryAddress, CAS20PolicyRegistryAddress} {
		if IsCAS20Address(reg) {
			t.Errorf("%s must fall outside the reserved token space", reg.Hex())
		}
	}
}
