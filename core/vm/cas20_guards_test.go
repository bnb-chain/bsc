package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Guards that were correct and that nothing failed on when removed.

// TestCAS20CreateRejectsStaticCall uses grantRole in a non-empty bootstrap bundle so
// it covers both createCAS20's ReadOnly rejection and the flag spawnBootstrap
// carries; with no initCalls the factory's own writes land regardless, since
// StateDB and cas20Storage do not consult ReadOnly.
func TestCAS20CreateRejectsStaticCall(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x51a71c")

	minter := common.HexToAddress("0x33333")
	bundle := [][]byte{cas20Call(selGrantRole, roleMint, addrKey(minter))}
	input := encodeCreateCAS20(cas20VariantAsset, salt, caller, bundle)
	// A refused write is a revert carrying StaticCallNotAllowed(), not an
	// exceptional halt: the caller keeps its gas and can decode the reason.
	budget := NewGasBudget(5_000_000)
	ret, left, err := evm.StaticCall(caller, CAS20FactoryAddress, input, budget)
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("STATICCALL createCAS20 gave %v, want a revert", err)
	}
	wantData, _ := finishCAS20(nil, revCAS20("StaticCallNotAllowed()", errSelStaticCallDenied))
	if !bytes.Equal(ret, wantData) {
		t.Errorf("returndata = %x, want StaticCallNotAllowed() = %x", ret, wantData)
	}
	if left.RegularGas == 0 {
		t.Error("the whole budget was consumed; a revert refunds what it did not spend")
	}

	// Nothing may have been written: no sentinel, so the address stays free.
	addr := cas20DeriveAddress(cas20VariantAsset, caller, salt)
	if code := evm.StateDB.GetCode(addr); len(code) != 0 {
		t.Errorf("code at %s after a refused STATICCALL: %x", addr.Hex(), code)
	}
	if newUnmeteredCAS20Storage(evm.StateDB, addr).hasRole(roleMint, minter) {
		t.Error("the bundle's grantRole took effect under STATICCALL")
	}

	// The registries' write paths under STATICCALL too, since they share the
	// same class of guard.
	for _, tc := range []struct {
		name  string
		to    common.Address
		input []byte
	}{
		{"activate", CAS20ActivationRegistryAddress, cas20Call(selActivate, common.HexToHash("0xf2"))},
		{"deactivate", CAS20ActivationRegistryAddress, cas20Call(selDeactivate, featureCAS20Asset)},
		// The governance entry point is refused for the same reason. Note that this
		// case cannot witness updateParam's own ReadOnly check: the metering layer
		// refuses the write in a read-only frame and the exit reports the identical
		// StaticCallNotAllowed, so removing the handler's guard changes no
		// returndata. What that guard buys is refusing before the decode and the
		// authorization, not the error itself.
		{"updateParam", CAS20ActivationRegistryAddress, encodeSetAdmin(common.HexToAddress("0xad4152"))},
		{"createPolicy", CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(caller), u256hash(cas20PolicyBlocklist))},
	} {
		caller := cas20TestCaller
		if tc.name == "updateParam" {
			caller = params.CAS20GovHubAddress // the one caller authorization would admit
		}
		if _, _, err := evm.StaticCall(caller, tc.to, tc.input, NewGasBudget(5_000_000)); err == nil {
			t.Errorf("%s succeeded under STATICCALL", tc.name)
		}
	}
}

// TestCAS20AdminCountGuards covers the two conditions that keep adminCount honest:
// grant counts only a role that was absent, revoke only one that was present. An
// inflated count makes the sole-admin protection see two admins where there is
// one, so the last becomes revocable. The other tests walk 1 -> 2 -> 1 -> 0 and
// never repeat a grant or remove an absent holder.
func TestCAS20AdminCountGuards(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	second := common.HexToAddress("0x5ec0nd")

	statedb, token, run := newTokenWithEVM(t, 1, func(s cas20Storage) {
		s.setRole(roleDefaultAdmin, admin, true)
		s.setAdminCount(uint256.NewInt(1))
	})
	view := func() *uint256.Int { return newUnmeteredCAS20Storage(statedb, token).adminCount() }

	// Granting a role its holder already has must not count twice.
	if _, err := run(admin, cas20Call(selGrantRole, roleDefaultAdmin, addrKey(admin))); err != nil {
		t.Fatalf("re-granting DEFAULT_ADMIN to its holder: %v", err)
	}
	if got := view(); got.Uint64() != 1 {
		t.Errorf("adminCount = %s after a duplicate grant, want 1", got)
	}

	// A real second admin does count.
	if _, err := run(admin, cas20Call(selGrantRole, roleDefaultAdmin, addrKey(second))); err != nil {
		t.Fatalf("granting DEFAULT_ADMIN to a new account: %v", err)
	}
	if got := view(); got.Uint64() != 2 {
		t.Fatalf("adminCount = %s after a real grant, want 2", got)
	}

	// Revoking an account that does not hold the role must not decrement.
	third := common.HexToAddress("0x7h1rd")
	if _, err := run(admin, cas20Call(selRevokeRole, roleDefaultAdmin, addrKey(third))); err != nil {
		t.Fatalf("revoking DEFAULT_ADMIN from a non-holder: %v", err)
	}
	if got := view(); got.Uint64() != 2 {
		t.Errorf("adminCount = %s after revoking a non-holder, want 2", got)
	}
}

// TestCAS20AnnounceKeepsInnerRoleChecks covers that an announcement does not lend
// its bundle the announcer's absent roles. The other announce test grants its
// operator MINT_ROLE too and bundles only updateMultiplier, which needs the same
// role the outer check already required, so nothing there distinguished "roles
// still apply" from "roles are skipped".
func TestCAS20AnnounceKeepsInnerRoleChecks(t *testing.T) {
	_, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	operator := common.HexToAddress("0x09e4a704")
	salt := common.HexToHash("0x0e2")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// OPERATOR_ROLE only — deliberately no MINT_ROLE.
	ret, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, salt, creator,
		[][]byte{cas20Call(selGrantRole, roleOperator, addrKey(operator))}))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// Positive control: an announcement this operator IS entitled to make must
	// succeed, so a failure below cannot be blamed on the setup.
	if _, err := call(operator, token, encodeAnnounce(
		[][]byte{cas20Call(selUpdateMultiplier, u256hash(2_000_000_000_000_000_000))}, "2026-Q1-NAV")); err != nil {
		t.Fatalf("an OPERATOR_ROLE holder could not announce updateMultiplier: %v", err)
	}

	// And the bundle must not gain a role its announcer lacks.
	inner := encodeBatchMint([]common.Address{cas20Alice}, []uint64{1000})
	if _, err := call(operator, token, encodeAnnounce([][]byte{inner}, "2026-Q2-NAV")); err == nil {
		t.Fatal("an announcer without MINT_ROLE ran batchMint inside its announcement")
	}
	if newUnmeteredCAS20Storage(evm.StateDB, token).balanceOf(cas20Alice).Sign() != 0 {
		t.Error("batchMint took effect despite the announcement failing")
	}
}

// TestCAS20NonDirectCallPlumbing drives the non-direct paths through the EVM's own
// entry points rather than a hand-built context, so the DirectCall flag evm.go
// sets is covered rather than assumed. It says nothing about the Caller and
// Value evm.go passes alongside it — the guard fires before either is read, so
// this test holds whatever they are; TestStatefulPrecompileCallContext pins
// those. TestCAS20DelegateCallGuard covers the guard itself.
func TestCAS20NonDirectCallPlumbing(t *testing.T) {
	_, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x9a11"), creator, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	caller := common.HexToAddress("0xca11e5")
	origin := common.HexToAddress("0x0416019")

	// Both revert with DelegateCallNotAllowed() and refund; see
	// TestCAS20DelegateCallGuard for the payload.
	wantDelegate, _ := finishCAS20(nil, revCAS20("DelegateCallNotAllowed()", errSelDelegateCallDenied))
	for _, tc := range []struct {
		name string
		run  func() ([]byte, GasBudget, error)
	}{
		{"CALLCODE", func() ([]byte, GasBudget, error) {
			return evm.CallCode(caller, token, cas20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(7))
		}},
		{"DELEGATECALL", func() ([]byte, GasBudget, error) {
			return evm.DelegateCall(origin, caller, token, cas20Call(selTotalSupply), NewGasBudget(100_000), uint256.NewInt(0))
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

// TestCAS20MeteringRefusesWritesInStaticFrames covers the backstop under the 25
// hand-written ReadOnly guards. Removing any one of them used to let a
// STATICCALL write: approve() with its guard deleted returned nil and left the
// allowance slot holding the amount. The metering layer now refuses first, as
// gasSStoreEIP2200 does, so a missing guard costs a revert rather than consensus.
func TestCAS20MeteringRefusesWritesInStaticFrames(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	ret, _, err := evm.Call(cas20TestCaller, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x5711"), cas20TestCaller, nil),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)
	// Driven with readOnly set but reaching the handler directly, which is the
	// state a frame is in when its own guard is missing.
	tok := cas20Token{
		ctx: &PrecompileContext{evm: evm, StateDB: statedb, Self: token, Caller: cas20Alice,
			ReadOnly: true, DirectCall: true, Value: uint256.NewInt(0), gas: &GasBudget{RegularGas: 5_000_000}},
	}
	tok.s = newMeteredCAS20StorageAt(tok.ctx, token)

	if ok := tok.s.setWord(tok.s.allowanceSlot(cas20Alice, cas20Bob), u256hash(4242)); ok {
		t.Error("a metered write reported success in a read-only frame")
	}
	if got := statedb.GetState(token, tok.s.allowanceSlot(cas20Alice, cas20Bob)); got != (common.Hash{}) {
		t.Errorf("the allowance slot holds %x, want empty — a read-only frame wrote state", got)
	}
	if !tok.ctx.writeProtectionViolated() {
		t.Fatal("the refusal was not recorded, so the exit cannot report it")
	}
	if tok.ctx.OutOfGas() {
		t.Error("the frame was marked out of gas; a static write is a protection failure, not exhaustion")
	}

	// And the exit turns it into the same typed revert a hand-written guard gives.
	_, err = finishCAS20Metered(tok.ctx, nil, nil)
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("exit err = %v, want the StaticCallNotAllowed revert", err)
	}

	// A log is equally a write.
	before := len(statedb.Logs())
	if tok.ctx.AddLog([]common.Hash{cas20TopicApproval}, nil) {
		t.Error("AddLog reported success in a read-only frame")
	}
	if n := len(statedb.Logs()) - before; n != 0 {
		t.Errorf("a read-only frame emitted %d log(s)", n)
	}
}

// TestCAS20PolicyWritesAllRefuseStaticFrames covers every write selector the
// PolicyRegistry dispatches, because one guard now stands for all nine. The
// handlers used to repeat the check; removing those left this switch as the only
// place a write path consults ReadOnly, so a selector added to the switch without
// the guard — or the guard removed — must fail here rather than in whichever
// handler happened to keep a copy.
//
// The metering layer refuses the write in a read-only frame regardless, and the
// exit reports the same error, so this cannot witness the guard by returndata
// alone. What it witnesses is that no write path is missing from the switch and
// that none of them mutates state under STATICCALL.
func TestCAS20PolicyWritesAllRefuseStaticFrames(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	caller := cas20TestCaller
	admin := addrKey(caller)
	list := u256hash(cas20PolicyBlocklist)
	id := u256hash(uint64(cas20PolicyBlocklist)<<56 | 2)

	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"createPolicy", cas20Call(selCreatePolicy, admin, list)},
		{"createPolicyWithAccounts", encodeCreatePolicyWithAccounts(caller, cas20PolicyBlocklist, []common.Address{caller})},
		{"updateAllowlist", encodeUpdateList(selUpdateAllowlist, uint64(cas20PolicyAllowlist)<<56|2, true, []common.Address{caller})},
		{"updateBlocklist", encodeUpdateList(selUpdateBlocklist, uint64(cas20PolicyBlocklist)<<56|2, true, []common.Address{caller})},
		{"stageUpdateAdmin", cas20Call(selStageUpdateAdmin, id, admin)},
		{"finalizeUpdateAdmin", cas20Call(selFinalizeUpdateAdmin, id)},
		{"renounceAdmin", cas20Call(selRenounceAdmin, id)},
		// The two composite selectors take a dynamic array; a bare selector is
		// enough here, since the guard precedes decoding.
		{"createCompositePolicy", selCreateComposite[:]},
		{"updateComposite", selUpdateComposite[:]},
	} {
		before := statedb.IntermediateRoot(true)
		_, _, err := evm.StaticCall(caller, CAS20PolicyRegistryAddress, tc.input, NewGasBudget(5_000_000))
		if err == nil {
			t.Errorf("%s succeeded under STATICCALL", tc.name)
		}
		if after := statedb.IntermediateRoot(true); after != before {
			t.Errorf("%s changed state under STATICCALL: %x -> %x", tc.name, before, after)
		}
	}

	// And the switch covers every write selector the registry declares: a new one
	// added below the guard rather than inside the case list would be routed to
	// the default arm and rejected as unknown, not silently let through.
	for _, sel := range [][4]byte{
		selCreatePolicy, selCreatePolicyWithAccounts, selUpdateAllowlist, selUpdateBlocklist,
		selStageUpdateAdmin, selFinalizeUpdateAdmin, selRenounceAdmin,
		selCreateComposite, selUpdateComposite,
	} {
		if _, _, err := evm.StaticCall(caller, CAS20PolicyRegistryAddress, sel[:], NewGasBudget(5_000_000)); err == nil {
			t.Errorf("selector %x accepted a static frame", sel)
		}
	}
}

// TestCAS20MalformedArgsRevertEmpty pins the returndata of every decode failure,
// not merely that one occurred.
//
// Solidity's external decoder validates each narrow argument and reverts with
// `revert(0, 0)` — empty returndata — both for dirty padding and for a clean
// value outside an enum's range. Panic(0x21) is what an *internal* uint-to-enum
// cast produces and is not what a caller passing 2 to an enum parameter receives.
// Asserting only that the call reverted cannot tell the two apart, so every case
// here compares the returndata itself.
func TestCAS20MalformedArgsRevertEmpty(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := cas20TestCaller

	assertEmpty := func(what string, ret []byte, err error) {
		t.Helper()
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want a revert", what, err)
			return
		}
		if len(ret) != 0 {
			t.Errorf("%s: returndata = %x, want empty. A decode failure is revert(0,0), "+
				"never Panic(0x21)", what, ret)
		}
	}

	// The registry's enum and bool arguments.
	outOfEnum := u256hash(uint64(cas20PolicyIntersect) + 1)
	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"createPolicy, policyType past the enum",
			cas20Call(selCreatePolicy, addrKey(caller), outOfEnum)},
		{"createPolicyWithAccounts, policyType past the enum",
			encodeCreatePolicyWithAccountsRaw(addrKey(caller), outOfEnum, nil)},
		{"createCompositePolicy, policyType past the enum",
			encodeComposite(selCreateComposite,
				[]common.Hash{addrKey(caller), outOfEnum}, []uint64{cas20PolicyAlwaysAllow})},
	} {
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress, tc.input,
			NewGasBudget(5_000_000), uint256.NewInt(0))
		assertEmpty(tc.name, ret, err)
	}

	// A live policy, so the bool below fails on its own encoding rather than on
	// the policy's absence.
	ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress,
		cas20Call(selCreatePolicy, addrKey(caller), u256hash(cas20PolicyAllowlist)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	id := new(uint256.Int).SetBytes(ret).Uint64()
	ret, _, err = evm.Call(caller, CAS20PolicyRegistryAddress,
		encodeUpdateListRaw(selUpdateAllowlist, id, u256hash(2), []common.Hash{addrKey(cas20Alice)}),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	assertEmpty("updateAllowlist, allowed = 2", ret, err)

	// The token's own narrow arguments. These go through evm.Call on a real token
	// rather than a bare dispatch: a revert's data is materialized on the way out
	// of the precompile, so a handler-level call returns nil returndata for every
	// failure and an emptiness assertion over it would hold vacuously.
	initCalls := [][]byte{cas20Call(selGrantRole, rolePause, addrKey(caller))}
	ret, _, err = evm.Call(caller, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x9174d5"), caller, initCalls),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	asset := common.BytesToAddress(ret)
	onToken := func(input []byte) ([]byte, error) {
		r, _, e := evm.Call(caller, asset, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return r, e
	}

	badFeature := uint64(cas20PauseSeize) + 1
	assertEmpty2 := func(what string, input []byte) {
		t.Helper()
		r, e := onToken(input)
		assertEmpty(what, r, e)
	}
	assertEmpty2("isPaused, feature past the enum", cas20Call(selIsPaused, u256hash(badFeature)))
	assertEmpty2("pause, element past the enum", cas20CallU8Array(selPause, byte(badFeature)))

	if _, err := onToken(cas20Call(selIsPaused, u256hash(uint64(cas20PauseSeize)))); err != nil {
		t.Errorf("isPaused with a valid feature: %v", err)
	}
	if _, err := onToken(cas20CallU8Array(selPause, byte(cas20PauseTransfer))); err != nil {
		t.Errorf("pause with a valid feature: %v", err)
	}

	dirtyID := common.Hash{}
	copy(dirtyID[:4], selIsPaused[:])
	dirtyID[31] = 1 // a nonzero byte outside the bytes4 value
	assertEmpty2("supportsInterface, bytes4 with nonzero padding",
		cas20Call(selSupportsInterface, dirtyID))

	// The clean form answers rather than reverting, so the guard above is not
	// simply refusing every bytes4.
	clean := common.Hash{}
	copy(clean[:4], selIsPaused[:])
	if ret, err := onToken(cas20Call(selSupportsInterface, clean)); err != nil {
		t.Errorf("supportsInterface with a clean bytes4: %v", err)
	} else if !bytes.Equal(ret, encBool(false)) {
		t.Errorf("supportsInterface(unknown id) = %x, want false", ret)
	}
}
