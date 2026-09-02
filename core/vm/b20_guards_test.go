package vm

import (
	"bytes"
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
	// A refused write is a revert carrying StaticCallNotAllowed(), not an
	// exceptional halt: the caller keeps its gas and can decode the reason.
	budget := NewGasBudget(5_000_000)
	ret, left, err := evm.StaticCall(caller, B20FactoryAddress, input, budget)
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("STATICCALL createB20 gave %v, want a revert", err)
	}
	wantData, _ := finishB20(nil, revB20("StaticCallNotAllowed()", errSelStaticCallDenied))
	if !bytes.Equal(ret, wantData) {
		t.Errorf("returndata = %x, want StaticCallNotAllowed() = %x", ret, wantData)
	}
	if left.RegularGas == 0 {
		t.Error("the whole budget was consumed; a revert refunds what it did not spend")
	}

	// Nothing may have been written: no sentinel, so the address stays free.
	addr := b20DeriveAddress(b20VariantAsset, caller, salt)
	if code := evm.StateDB.GetCode(addr); len(code) != 0 {
		t.Errorf("code at %s after a refused STATICCALL: %x", addr.Hex(), code)
	}
	if newUnmeteredB20Storage(evm.StateDB, addr).hasRole(roleMint, minter) {
		t.Error("the bundle's grantRole took effect under STATICCALL")
	}

	// The registries' write paths under STATICCALL too, since they share the
	// same class of guard.
	for _, tc := range []struct {
		name  string
		to    common.Address
		input []byte
	}{
		{"updateParam", B20ActivationRegistryAddress, encodeUpdateParam("bsc.something_later", true)},
		{"createPolicy", B20PolicyRegistryAddress, b20Call(selCreatePolicy, addrKey(caller), u256hash(b20PolicyBlocklist))},
	} {
		if _, _, err := evm.StaticCall(b20TestCaller, tc.to, tc.input, NewGasBudget(5_000_000)); err == nil {
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
	view := func() *uint256.Int { return newUnmeteredB20Storage(statedb, token).adminCount() }

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
	if newUnmeteredB20Storage(evm.StateDB, token).balanceOf(b20Alice).Sign() != 0 {
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

	// Both revert with DelegateCallNotAllowed() and refund; see
	// TestB20DelegateCallGuard for the payload.
	wantDelegate, _ := finishB20(nil, revB20("DelegateCallNotAllowed()", errSelDelegateCallDenied))
	for _, tc := range []struct {
		name string
		run  func() ([]byte, GasBudget, error)
	}{
		{"CALLCODE", func() ([]byte, GasBudget, error) {
			return evm.CallCode(caller, token, b20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(7))
		}},
		{"DELEGATECALL", func() ([]byte, GasBudget, error) {
			return evm.DelegateCall(origin, caller, token, b20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
		}},
	} {
		ret, left, err := tc.run()
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s err = %v, want a revert", tc.name, err)
		}
		if !bytes.Equal(ret, wantDelegate) {
			t.Errorf("%s returndata = %x, want %x", tc.name, ret, wantDelegate)
		}
		if left.RegularGas == 0 {
			t.Errorf("%s consumed the whole budget; a revert refunds the rest", tc.name)
		}
	}
}

// TestB20MeteringRefusesWritesInStaticFrames covers the backstop under the 25
// hand-written ReadOnly guards. Removing any one of them used to let a
// STATICCALL write: approve() with its guard deleted returned nil and left the
// allowance slot holding the amount. The metering layer now refuses first, as
// gasSStoreEIP2200 does, so a missing guard costs a revert rather than consensus.
func TestB20MeteringRefusesWritesInStaticFrames(t *testing.T) {
	statedb, evm := newB20EVM(t)
	ret, _, err := evm.Call(b20TestCaller, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x5711"), b20TestCaller, nil),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	// Driven with readOnly set but reaching the handler directly, which is the
	// state a frame is in when its own guard is missing.
	tok := b20Token{
		ctx: &PrecompileContext{evm: evm, StateDB: statedb, Self: token, Caller: b20Alice,
			ReadOnly: true, DirectCall: true, Value: uint256.NewInt(0), gas: &GasBudget{RegularGas: 5_000_000}},
	}
	tok.s = newMeteredB20StorageAt(tok.ctx, token)

	if ok := tok.s.setWord(tok.s.allowanceSlot(b20Alice, b20Bob), u256hash(4242)); ok {
		t.Error("a metered write reported success in a read-only frame")
	}
	if got := statedb.GetState(token, tok.s.allowanceSlot(b20Alice, b20Bob)); got != (common.Hash{}) {
		t.Errorf("the allowance slot holds %x, want empty — a read-only frame wrote state", got)
	}
	if !tok.ctx.writeProtectionViolated() {
		t.Fatal("the refusal was not recorded, so the exit cannot report it")
	}
	if tok.ctx.OutOfGas() {
		t.Error("the frame was marked out of gas; a static write is a protection failure, not exhaustion")
	}

	// And the exit turns it into the same typed revert a hand-written guard gives.
	_, err = finishB20Metered(tok.ctx, nil, nil)
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("exit err = %v, want the StaticCallNotAllowed revert", err)
	}

	// A log is equally a write.
	before := len(statedb.Logs())
	if tok.ctx.AddLog([]common.Hash{b20TopicApproval}, nil) {
		t.Error("AddLog reported success in a read-only frame")
	}
	if n := len(statedb.Logs()) - before; n != 0 {
		t.Errorf("a read-only frame emitted %d log(s)", n)
	}
}
