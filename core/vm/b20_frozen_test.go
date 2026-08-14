package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20RolesFreezeAtZeroAdmins gives a surviving OPERATOR_ROLE holder authority
// over MINT_ROLE, isolating adminCount == 0 as the only thing left to refuse a
// grant. Without that the role check refuses it anyway and the test passes with
// the guard removed.
func TestB20RolesFreezeAtZeroAdmins(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	operator := common.HexToAddress("0x09e4")
	target := common.HexToAddress("0x7a4form")

	_, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(roleDefaultAdmin, admin, true)
		s.setAdminCount(uint256.NewInt(1))
	})

	// OPERATOR_ROLE governs MINT_ROLE, and operator holds it.
	if _, err := run(admin, b20Call(selSetRoleAdmin, roleMint, roleOperator)); err != nil {
		t.Fatalf("setRoleAdmin: %v", err)
	}
	if _, err := run(admin, b20Call(selGrantRole, roleOperator, addrKey(operator))); err != nil {
		t.Fatalf("grantRole(OPERATOR): %v", err)
	}
	// While an admin exists, operator may grant MINT_ROLE.
	if _, err := run(operator, b20Call(selGrantRole, roleMint, addrKey(target))); err != nil {
		t.Fatalf("operator granting MINT_ROLE before the freeze: %v", err)
	}

	if _, err := run(admin, b20Call(selRenounceLastAdmin)); err != nil {
		t.Fatalf("renounceLastAdmin: %v", err)
	}

	// operator still holds OPERATOR_ROLE and is still MINT_ROLE's admin, so the
	// role check passes; the freeze is the only thing left to refuse it.
	other := common.HexToAddress("0x07e4")
	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"grantRole", b20Call(selGrantRole, roleMint, addrKey(other))},
		{"revokeRole", b20Call(selRevokeRole, roleMint, addrKey(target))},
		{"setRoleAdmin", b20Call(selSetRoleAdmin, roleMint, roleDefaultAdmin)},
	} {
		if _, err := run(operator, tc.input); !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s after the last admin renounced: %v, want a revert", tc.name, err)
		}
	}
}

// TestB20RequiredGasIsZero catches a consensus-visible charge that EVM.Call would
// otherwise pay silently.
func TestB20RequiredGasIsZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    PrecompiledContract
	}{
		{"factory", b20Factory},
		{"policy", b20Policy},
		{"activation", b20Activation},
		{"asset", b20Asset},
		{"stablecoin", b20Stablecoin},
	} {
		for _, input := range [][]byte{nil, make([]byte, 4), make([]byte, 1024)} {
			if got := tc.p.RequiredGas(input); got != 0 {
				t.Errorf("%s.RequiredGas(%d bytes) = %d, want 0 — B20 meters inside RunStateful",
					tc.name, len(input), got)
			}
		}
	}
}
