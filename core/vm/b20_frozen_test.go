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

// TestB20BootstrapIsNotAWayBack covers the resurrection the privileged bootstrap
// allowed. Renouncing the last admin advertises a permanent transition —
// LastAdminRenounced is emitted for exactly that — but the window skips the
// zero-admin freeze so a later call in the same bundle could grant the role
// again. adminCount went 1 -> 0 -> 1 and a new admin held it.
//
// base-std keys the freeze on having been "transitioned to admin-less via
// renounceLastAdmin" (IB20.grantRole's natspec), not on the count alone, which is
// what distinguishes this from an ownerless token: that one also starts at zero
// admins and must still accept role grants inside the window. Both directions are
// checked here, because a fix that simply stopped skipping the freeze would break
// the second.
func TestB20BootstrapIsNotAWayBack(t *testing.T) {
	creator := common.HexToAddress("0xc4ea70")
	newToken := func(t *testing.T, admin common.Address, calls [][]byte) (*EVM, common.Address, error) {
		t.Helper()
		_, evm := newB20EVM(t)
		params := b20AssetParams("T", "T", admin, 18)
		ret, _, err := evm.Call(creator, B20FactoryAddress,
			encodeCreateB20WithParams(b20VariantAsset, common.HexToHash("0x8007"), params, calls),
			NewGasBudget(9_000_000), uint256.NewInt(0))
		return evm, common.BytesToAddress(ret), err
	}
	// Where the token would have been, so the rollback can be checked at an
	// address the factory never returned.
	derived := b20DeriveAddress(b20VariantAsset, creator, common.HexToHash("0x8007"))

	// Renouncing and re-granting inside one bundle must fail, and take the whole
	// creation with it.
	evm, _, err := newToken(t, creator, [][]byte{
		b20Call(selRenounceLastAdmin),
		b20Call(selGrantRole, roleDefaultAdmin, addrKey(b20Bob)),
	})
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("renounce-then-grant inside the bootstrap: %v, want a revert", err)
	}
	// The creation is undone, not merely reported as failed: no sentinel at the
	// derived address, and none of the bundle's grants survive. An earlier version
	// asserted len(address.Bytes()) != 0 here, which is 20 for every address and so
	// could not fail — it checked nothing at all.
	if code := evm.StateDB.GetCode(derived); len(code) != 0 {
		t.Errorf("code at %s after the refused creation: %x", derived.Hex(), code)
	}
	view := newUnmeteredB20Storage(evm.StateDB, derived)
	if view.hasRole(roleDefaultAdmin, b20Bob) {
		t.Error("bob holds DEFAULT_ADMIN_ROLE after the creation was refused")
	}
	if !view.adminCount().IsZero() {
		t.Errorf("adminCount at the refused address = %s, want 0", view.adminCount())
	}

	// An ownerless token still configures its roles in the window: the freeze
	// yields to privilege until a renunciation happens, not always.
	evm, token, err := newToken(t, common.Address{}, [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(b20Bob)),
	})
	if err != nil {
		t.Fatalf("ownerless token granting MINT_ROLE in the window: %v", err)
	}
	view = newUnmeteredB20Storage(evm.StateDB, token)
	if !view.hasRole(roleMint, b20Bob) {
		t.Error("the ownerless token's bootstrap grant did not take effect")
	}
	if !view.adminCount().IsZero() {
		t.Errorf("ownerless adminCount = %s, want 0", view.adminCount())
	}

	// And renouncing as the last act of a bundle is still allowed — it is only
	// the grant *after* it that is refused.
	evm, token, err = newToken(t, creator, [][]byte{
		b20Call(selGrantRole, roleMint, addrKey(b20Bob)),
		b20Call(selRenounceLastAdmin),
	})
	if err != nil {
		t.Fatalf("configure-then-renounce inside the bootstrap: %v", err)
	}
	view = newUnmeteredB20Storage(evm.StateDB, token)
	if !view.adminCount().IsZero() || view.hasRole(roleDefaultAdmin, creator) {
		t.Error("the renunciation did not take effect")
	}
	if !view.hasRole(roleMint, b20Bob) {
		t.Error("the grant before the renunciation was lost")
	}
}
