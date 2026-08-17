package vm

import (
	"os"
	"regexp"
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

	// And the same three in the interface we publish. The mirror had the two
	// registries the other way round — a caller wiring POLICY_REGISTRY from
	// B20Constants reached the ActivationRegistry, which answers a different ABI
	// entirely. Nothing compiles the mirror, so only a check like this sees it.
	src, err := os.ReadFile("b20std/B20Std.sol")
	if err != nil {
		t.Fatalf("read the interface mirror: %v", err)
	}
	published := map[string]common.Address{}
	for _, m := range regexp.MustCompile(`address internal constant (\w+) = (0x[0-9a-fA-F]{40});`).
		FindAllStringSubmatch(string(src), -1) {
		published[m[1]] = common.HexToAddress(m[2])
	}
	for name, want := range map[string]common.Address{
		"B20_FACTORY":         B20FactoryAddress,
		"ACTIVATION_REGISTRY": B20ActivationRegistryAddress,
		"POLICY_REGISTRY":     B20PolicyRegistryAddress,
	} {
		got, ok := published[name]
		if !ok {
			t.Errorf("B20Constants declares no %s", name)
		} else if got != want {
			t.Errorf("B20Constants.%s = %s, the precompile is at %s", name, got.Hex(), want.Hex())
		}
	}
	if len(published) != 3 {
		t.Errorf("B20Constants declares %d address(es), 3 are pinned here", len(published))
	}
}
