package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20RolesFreezeAtZeroAdmins covers the guard that makes renounceLastAdmin
// permanent.
//
// The discriminating case needs a caller the role check would otherwise admit.
// After the last DEFAULT_ADMIN renounces, nobody holds it, so an ordinary
// grantRole is refused by the role check whatever the freeze guard does — a test
// that stops there passes with the guard removed. So MINT_ROLE's admin is first
// moved to OPERATOR_ROLE and handed to an account that keeps it across the
// renunciation: that caller passes the role check, and only adminCount == 0
// stands between it and a grant.
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

// TestB20RequiredGasIsZero pins that no B20 precompile charges a flat up-front
// cost. RequiredGas is consensus-visible gas, and nothing failed when it was
// made non-zero: the suite drives these through EVM.Call, which pays it silently.
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
