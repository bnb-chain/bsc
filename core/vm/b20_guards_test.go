package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Guards that are present and correct, and that nothing failed on when removed.
// A review pass identified each from the call graph; the mutations below confirm
// the coverage gap it could only infer, since its sandbox could not run tests.

// TestB20CreateRejectsStaticCall covers the guard that is currently the only
// thing standing between a read-only frame and a state write.
//
// spawnBootstrap did not copy ReadOnly into the bootstrap context. That was
// unreachable because createB20 rejects ReadOnly before it ever spawns — but
// deleting that one line left the whole suite green, so the sole barrier was
// itself unguarded.
//
// The bundle is not empty on purpose. With no initCalls, removing the outer guard
// lets the factory's own writes through regardless of the propagation, because
// StateDB and b20Storage do not consult ReadOnly. One grantRole reaches a path
// that does, so this covers both halves: the guard, and the flag now reaching the
// bootstrap.
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

// TestB20AdminCountGuards covers the two conditions that keep adminCount honest.
//
// grantRole increments only when the role was absent and removeRole decrements
// only when it was present. Removing either left the suite green, and either
// would be serious: an inflated count makes the sole-admin protection believe
// there are two admins when there is one, so the last one becomes revocable and
// the token is stranded with no administrator at all.
//
// The existing tests walk 1 -> 2 -> 1 -> 0, which never repeats a grant or
// removes an absent holder — the two cases the guards exist for.
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
// its bundle the announcer's absent roles.
//
// announce requires OPERATOR_ROLE and then runs each entry against the same
// non-privileged token, so an inner call still needs its own role. Marking the
// bundle privileged left the suite green: the existing announce test grants its
// operator MINT_ROLE as well and its bundle only calls updateMultiplier, which
// needs the same OPERATOR_ROLE the outer check already required. Nothing
// distinguished "roles still apply" from "roles are skipped".
//
// The positive control matters as much as the negative one. A first draft seeded
// storage directly instead of creating the token, so the announcement reverted
// because the token was never initialized — the assertion held for a reason that
// had nothing to do with roles, and it passed with the privileged mutation in
// place too.
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
		[][]byte{b20Call(selUpdateMultiplier, u256hash(2_000_000_000_000_000_000))}, u256hash(1))); err != nil {
		t.Fatalf("an OPERATOR_ROLE holder could not announce updateMultiplier: %v", err)
	}

	// And the bundle must not gain a role its announcer lacks.
	inner := encodeBatchMint([]common.Address{b20Alice}, []uint64{1000})
	if _, err := call(operator, token, encodeAnnounce([][]byte{inner}, u256hash(2))); err == nil {
		t.Fatal("an announcer without MINT_ROLE ran batchMint inside its announcement")
	}
	if newB20Storage(evm.StateDB, token).balanceOf(b20Alice).Sign() != 0 {
		t.Error("batchMint took effect despite the announcement failing")
	}
}
