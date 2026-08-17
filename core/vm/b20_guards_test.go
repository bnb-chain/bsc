package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Guards that were correct and that nothing failed on when removed.

// TestB20CreateRejectsStaticCall uses grantRole in a non-empty bootstrap bundle so
// it covers both createB20's ReadOnly rejection and the flag spawnBootstrap
// carries; with no initCalls the factory's own writes land regardless, since
// StateDB and b20Storage do not consult ReadOnly.
func TestB20CreateRejectsStaticCall(t *testing.T) {
	_, evm := newB20EVM(t)
	caller := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x51a71c")

	minter := common.HexToAddress("0x33333")
	bundle := [][]byte{b20Call(selGrantRole, roleMint, addrKey(minter))}
	input := encodeCreateB20(b20VariantAsset, salt, caller, bundle)
	if _, _, err := evm.StaticCall(caller, B20FactoryAddress, input, NewGasBudget(5_000_000)); err == nil {
		t.Fatal("createB20 succeeded under STATICCALL")
	} else if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("STATICCALL createB20 gave %v, want write protection", err)
	}

	// Nothing may have been written: no sentinel, so the address stays free.
	addr := b20DeriveAddress(b20VariantAsset, caller, salt)
	if code := evm.StateDB.GetCode(addr); len(code) != 0 {
		t.Errorf("code at %s after a refused STATICCALL: %x", addr.Hex(), code)
	}
	if newB20Storage(evm.StateDB, addr).hasRole(roleMint, minter) {
		t.Error("the bundle's grantRole took effect under STATICCALL")
	}

	// The registries' write paths under STATICCALL too, since they share the
	// same class of guard.
	for _, tc := range []struct {
		name  string
		to    common.Address
		input []byte
	}{
		{"activate", B20ActivationRegistryAddress, b20Call(selActivate, common.HexToHash("0xf2"))},
		{"setAdmin", B20ActivationRegistryAddress, b20Call(selSetAdmin, addrKey(caller))},
		{"createPolicy", B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(caller), u256hash(b20PolicyBlocklist))},
	} {
		if _, _, err := evm.StaticCall(b20ActivationAdmin, tc.to, tc.input, NewGasBudget(5_000_000)); err == nil {
			t.Errorf("%s succeeded under STATICCALL", tc.name)
		}
	}
}

// TestB20AdminCountGuards covers the two conditions that keep adminCount honest:
// grant counts only a role that was absent, revoke only one that was present. An
// inflated count makes the sole-admin protection see two admins where there is
// one, so the last becomes revocable. The other tests walk 1 -> 2 -> 1 -> 0 and
// never repeat a grant or remove an absent holder.
func TestB20AdminCountGuards(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	second := common.HexToAddress("0x5ec0nd")

	statedb, token, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(roleDefaultAdmin, admin, true)
		s.setAdminCount(uint256.NewInt(1))
	})
	view := func() *uint256.Int { return newB20Storage(statedb, token).adminCount() }

	// Granting a role its holder already has must not count twice.
	if _, err := run(admin, b20Call(selGrantRole, roleDefaultAdmin, addrKey(admin))); err != nil {
		t.Fatalf("re-granting DEFAULT_ADMIN to its holder: %v", err)
	}
	if got := view(); got.Uint64() != 1 {
		t.Errorf("adminCount = %s after a duplicate grant, want 1", got)
	}

	// A real second admin does count.
	if _, err := run(admin, b20Call(selGrantRole, roleDefaultAdmin, addrKey(second))); err != nil {
		t.Fatalf("granting DEFAULT_ADMIN to a new account: %v", err)
	}
	if got := view(); got.Uint64() != 2 {
		t.Fatalf("adminCount = %s after a real grant, want 2", got)
	}

	// Revoking an account that does not hold the role must not decrement.
	third := common.HexToAddress("0x7h1rd")
	if _, err := run(admin, b20Call(selRevokeRole, roleDefaultAdmin, addrKey(third))); err != nil {
		t.Fatalf("revoking DEFAULT_ADMIN from a non-holder: %v", err)
	}
	if got := view(); got.Uint64() != 2 {
		t.Errorf("adminCount = %s after revoking a non-holder, want 2", got)
	}
}

// TestB20AnnounceKeepsInnerRoleChecks covers that an announcement does not lend
// its bundle the announcer's absent roles. The other announce test grants its
// operator MINT_ROLE too and bundles only updateMultiplier, which needs the same
// role the outer check already required, so nothing there distinguished "roles
// still apply" from "roles are skipped".
//
// The positive control is load-bearing: without it the negative assertion holds
// whenever the announcement fails for any reason at all.
func TestB20AnnounceKeepsInnerRoleChecks(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	operator := common.HexToAddress("0x09e4a704")
	salt := common.HexToHash("0x0e2")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// OPERATOR_ROLE only — deliberately no MINT_ROLE.
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, salt, creator,
		[][]byte{b20Call(selGrantRole, roleOperator, addrKey(operator))}))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// Positive control: an announcement this operator IS entitled to make must
	// succeed, so a failure below cannot be blamed on the setup.
	if _, err := call(operator, token, encodeAnnounce(
		[][]byte{b20Call(selUpdateMultiplier, u256hash(2_000_000_000_000_000_000))}, "2026-Q1-NAV")); err != nil {
		t.Fatalf("an OPERATOR_ROLE holder could not announce updateMultiplier: %v", err)
	}

	// And the bundle must not gain a role its announcer lacks.
	inner := encodeBatchMint([]common.Address{b20Alice}, []uint64{1000})
	if _, err := call(operator, token, encodeAnnounce([][]byte{inner}, "2026-Q2-NAV")); err == nil {
		t.Fatal("an announcer without MINT_ROLE ran batchMint inside its announcement")
	}
	if newB20Storage(evm.StateDB, token).balanceOf(b20Alice).Sign() != 0 {
		t.Error("batchMint took effect despite the announcement failing")
	}
}

// TestB20NonDirectCallPlumbing drives the non-direct paths through the EVM's own
// entry points rather than a hand-built context, so the DirectCall flag evm.go
// sets is covered rather than assumed. It says nothing about the Caller and
// Value evm.go passes alongside it — the guard fires before either is read, so
// this test holds whatever they are; TestStatefulPrecompileCallContext pins
// those. TestB20DelegateCallGuard covers the guard itself.
func TestB20NonDirectCallPlumbing(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x9a11"), creator, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	caller := common.HexToAddress("0xca11e5")
	origin := common.HexToAddress("0x0416019")

	if _, _, err := evm.CallCode(caller, token, b20Call(selTotalSupply),
		NewGasBudget(100_000), uint256.NewInt(7)); !errors.Is(err, ErrB20DelegateCall) {
		t.Errorf("CALLCODE err = %v, want ErrB20DelegateCall", err)
	}
	if _, _, err := evm.DelegateCall(origin, caller, token, b20Call(selTotalSupply),
		NewGasBudget(100_000), uint256.NewInt(0)); !errors.Is(err, ErrB20DelegateCall) {
		t.Errorf("DELEGATECALL err = %v, want ErrB20DelegateCall", err)
	}
}
