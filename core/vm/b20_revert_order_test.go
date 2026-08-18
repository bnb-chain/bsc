package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Check-ordering tests, modelled on base-std's *_revertOrder.t.sol files.
//
// base-std documents the order of every entry point's checks in natspec, and the
// order is observable: it decides which error a caller receives when more than
// one condition fails. Nothing here tested it, and two of the cases below found
// real divergences — seizeWithMemo accepted a self-seize and a zero source, and
// updatePolicy reported an unknown scope as PolicyNotFound.
//
// The technique is what makes these order tests rather than per-check tests: each
// case violates its own condition *and every later one*, so passing means the
// earlier check ran first. A test that violated one condition at a time would
// hold under any order.

// wantRevert asserts a call reverted with a specific custom error.
func wantRevert(t *testing.T, ret []byte, err error, sel [4]byte, what string) {
	t.Helper()
	if !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("%s: err = %v, want a revert", what, err)
		return
	}
	if len(ret) < 4 {
		t.Errorf("%s: returndata is %d bytes, want a selector", what, len(ret))
		return
	}
	if got := [4]byte(ret[:4]); got != sel {
		t.Errorf("%s: reverted with %x, want %x", what, got, sel)
	}
}

// TestB20SeizeRevertOrder walks base-std's documented order for seizeWithMemo:
// ContractPaused(SEIZE), AccessControlUnauthorizedAccount, InvalidReceiver
// (to == 0 or from == to), InvalidSender (from == 0), AccountNotSeizable,
// PolicyForbids(SEIZE_RECEIVER), InsufficientBalance.
func TestB20SeizeRevertOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	stranger := common.HexToAddress("0x57ra496")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset,
		common.HexToHash("0x5e12"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selGrantRole, roleSeize, addrKey(creator)),
			b20Call(selGrantRole, rolePause, addrKey(creator)),
			b20Call(selGrantRole, roleUnpause, addrKey(creator)),
			b20Call(selMint, addrKey(b20Bob), u256hash(1000)),
		}))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	memo := common.HexToHash("0x5e12e")
	seize := func(caller, from, to common.Address, amount uint64) ([]byte, error) {
		return call(caller, token, b20Call(selSeizeWithMemo, addrKey(from), addrKey(to), u256hash(amount), memo))
	}

	// 1. Pause outranks everything, including a caller with no role at all and
	//    arguments that would fail every later check.
	if _, err := call(creator, token, b20CallU8Array(selPause, b20PauseSeize)); err != nil {
		t.Fatalf("pause(SEIZE): %v", err)
	}
	ret, err = seize(stranger, common.Address{}, common.Address{}, 1<<40)
	wantRevert(t, ret, err, errSelContractPaused, "paused, no role, zero addresses")
	if _, err := call(creator, token, b20CallU8Array(selUnpause, b20PauseSeize)); err != nil {
		t.Fatalf("unpause(SEIZE): %v", err)
	}

	// 2. The role check outranks every argument check.
	ret, err = seize(stranger, common.Address{}, common.Address{}, 1<<40)
	wantRevert(t, ret, err, errSelACUnauthorized, "no SEIZE_ROLE, zero addresses")

	// 3. A zero destination outranks a zero source.
	ret, err = seize(creator, common.Address{}, common.Address{}, 1<<40)
	wantRevert(t, ret, err, errSelInvalidReceiver, "zero to and zero from")

	// 3b. So does a self-seize, which would otherwise emit Seized over a no-op.
	ret, err = seize(creator, b20Bob, b20Bob, 1<<40)
	wantRevert(t, ret, err, errSelInvalidReceiver, "from == to")

	// 4. A zero source is its own error, not the InsufficientBalance its empty
	//    balance would otherwise produce.
	ret, err = seize(creator, common.Address{}, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInvalidSender, "zero from")

	// 5. Seizability outranks the balance check: bob is not blocked yet, and the
	//    amount is far beyond his balance.
	ret, err = seize(creator, b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelAccountNotSeizable, "not seizable, amount over balance")

	// Make bob seizable, and only then does the balance become the binding check.
	ret, err = call(creator, B20PolicyRegistryAddress,
		b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	blocklist := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, B20PolicyRegistryAddress,
		encodeUpdateList(selUpdateBlocklist, blocklist, true, []common.Address{b20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token, b20Call(selUpdatePolicy, scopeSeizeHolder, wU64(blocklist))); err != nil {
		t.Fatalf("updatePolicy(SEIZE_HOLDER): %v", err)
	}

	// 7. Balance, now that everything ahead of it passes.
	ret, err = seize(creator, b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInsufficientBalance, "seizable but amount over balance")

	// And the whole thing succeeds once the amount fits, so none of the reverts
	// above were the setup failing.
	if _, err := seize(creator, b20Bob, b20Alice, 100); err != nil {
		t.Fatalf("a seizure that should succeed: %v", err)
	}
}

// TestB20UpdatePolicyRevertOrder pins role, then scope, then id — base-std's
// order. Ours checked the id first, so binding a nonexistent id to an
// unrecognized scope reported PolicyNotFound where base-std reports
// UnsupportedPolicyType.
func TestB20UpdatePolicyRevertOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	stranger := common.HexToAddress("0x57ra496")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	ret, err := call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x5c09e"), creator, nil))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	unknownScope := crypto.Keccak256Hash([]byte("NOT_A_POLICY_SCOPE"))
	const missingID = uint64(0xdead)

	// 1. The role check outranks both argument checks.
	ret, err = call(stranger, token, b20Call(selUpdatePolicy, unknownScope, wU64(missingID)))
	wantRevert(t, ret, err, errSelACUnauthorized, "no admin role, bad scope and bad id")

	// 2. The scope outranks the id.
	ret, err = call(creator, token, b20Call(selUpdatePolicy, unknownScope, wU64(missingID)))
	wantRevert(t, ret, err, errSelUnsupportedScope, "unknown scope with a nonexistent id")

	// 3. And with a real scope, the id is what fails.
	ret, err = call(creator, token, b20Call(selUpdatePolicy, scopeMintReceiver, wU64(missingID)))
	wantRevert(t, ret, err, errSelPolicyNotFoundID, "known scope, nonexistent id")

	// A sentinel binds, so the checks above were not rejecting everything.
	if _, err := call(creator, token,
		b20Call(selUpdatePolicy, scopeMintReceiver, wU64(b20PolicyAlwaysAllow))); err != nil {
		t.Fatalf("binding ALWAYS_ALLOW: %v", err)
	}
}

// TestB20TransferFromRevertOrder pins base-std's order for transferFrom:
// ContractPaused(TRANSFER), InvalidReceiver, InvalidSender, InsufficientAllowance,
// PolicyForbids(TRANSFER_EXECUTOR), then the sender/receiver policies and the
// balance.
//
// Two of these were wrong. The zero-address checks lived in move(), which runs
// after the allowance is spent, and the executor policy was consulted before the
// allowance rather than after — so a transfer to the zero address, or an
// unauthorized executor with too little allowance, named the wrong failure.
func TestB20TransferFromRevertOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	spender := common.HexToAddress("0x59e6de4")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset,
		common.HexToHash("0x7fa05"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selGrantRole, rolePause, addrKey(creator)),
			b20Call(selGrantRole, roleUnpause, addrKey(creator)),
			b20Call(selMint, addrKey(b20Bob), u256hash(1000)),
		}))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	from := func(f, to common.Address, amount uint64) ([]byte, error) {
		return call(spender, token, b20Call(selTransferFrom, addrKey(f), addrKey(to), u256hash(amount)))
	}

	// 1. Pause outranks every argument check.
	if _, err := call(creator, token, b20CallU8Array(selPause, b20PauseTransfer)); err != nil {
		t.Fatalf("pause(TRANSFER): %v", err)
	}
	ret, err = from(common.Address{}, common.Address{}, 1<<40)
	wantRevert(t, ret, err, errSelContractPaused, "paused with zero addresses and no allowance")
	if _, err := call(creator, token, b20CallU8Array(selUnpause, b20PauseTransfer)); err != nil {
		t.Fatalf("unpause(TRANSFER): %v", err)
	}

	// 2. A zero destination outranks the allowance, which is zero here.
	ret, err = from(b20Bob, common.Address{}, 1<<40)
	wantRevert(t, ret, err, errSelInvalidReceiver, "zero to with no allowance")

	// 3. So does a zero source.
	ret, err = from(common.Address{}, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInvalidSender, "zero from with no allowance")

	// 4. Then the allowance, ahead of the balance it also exceeds.
	ret, err = from(b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInsufficientAllow, "no allowance and amount over balance")

	// 4b. And the allowance outranks the executor policy: block the spender on
	//     TRANSFER_EXECUTOR, and with no allowance it is still the allowance that
	//     is reported. Without a denied executor the previous case holds under
	//     either order, which is how this one was wrong to begin with.
	ret, err = call(creator, B20PolicyRegistryAddress,
		b20Call(selCreatePolicy, addrKey(creator), u256hash(b20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	execBlock := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, B20PolicyRegistryAddress,
		encodeUpdateList(selUpdateBlocklist, execBlock, true, []common.Address{spender})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token,
		b20Call(selUpdatePolicy, scopeTransferExecutor, wU64(execBlock))); err != nil {
		t.Fatalf("updatePolicy(TRANSFER_EXECUTOR): %v", err)
	}
	ret, err = from(b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInsufficientAllow, "blocked executor and no allowance")

	// With an allowance, the blocked executor is what binds — so the case above
	// really did have two failing conditions.
	if _, err := call(b20Bob, token, b20Call(selApprove, addrKey(spender), u256hash(1<<40))); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ret, err = from(b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelPolicyForbids, "blocked executor with an allowance")

	// Unblock it for the rest.
	if _, err := call(creator, token,
		b20Call(selUpdatePolicy, scopeTransferExecutor, wU64(b20PolicyAlwaysAllow))); err != nil {
		t.Fatalf("unbinding the executor policy: %v", err)
	}

	// 5. With enough allowance, the balance is what binds — so the allowance
	//    check above was not the only thing failing.
	if _, err := call(b20Bob, token, b20Call(selApprove, addrKey(spender), u256hash(1<<40))); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ret, err = from(b20Bob, b20Alice, 1<<40)
	wantRevert(t, ret, err, errSelInsufficientBalance, "allowance granted, amount over balance")

	// And a transfer inside both bounds succeeds.
	if _, err := from(b20Bob, b20Alice, 100); err != nil {
		t.Fatalf("a transferFrom that should succeed: %v", err)
	}
}

// TestB20PermitRevertOrder pins ExpiredSignature, InvalidSigner, InvalidSpender —
// base-std's order. The spender check was missing entirely, so permit could set an
// allowance for the zero address that approve() refuses.
func TestB20PermitRevertOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	ret, err := call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x9e12"), creator, nil))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// An expired deadline outranks a signature that is garbage and a spender that
	// is zero.
	bad := b20Call(selPermit, addrKey(b20Alice), common.Hash{}, u256hash(1), u256hash(0),
		wU8(27), common.Hash{}, common.Hash{})
	ret, err = call(creator, token, bad)
	wantRevert(t, ret, err, errSelExpiredSignature, "expired, bad signature, zero spender")

	// Unexpired, the garbage signature is next — still ahead of the zero spender.
	future := b20Call(selPermit, addrKey(b20Alice), common.Hash{}, u256hash(1), u256hash(1<<40),
		wU8(27), common.Hash{}, common.Hash{})
	ret, err = call(creator, token, future)
	wantRevert(t, ret, err, errSelInvalidSigner, "unexpired, bad signature, zero spender")

	// And a real signature over a zero spender reaches the spender check. Signing
	// it is what makes this case reachable at all — without a valid signature the
	// InvalidSigner check above would keep answering.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	gas := NewGasBudget(1)
	domTok := newB20Token(&PrecompileContext{evm: evm, StateDB: evm.StateDB, Self: token, gas: &gas}, 18)

	const value, deadline, nonce = uint64(1), uint64(1 << 40), uint64(0)
	sh := append([]byte{}, b20PermitTypehash.Bytes()...)
	sh = append(sh, addrKey(owner).Bytes()...)
	sh = append(sh, common.Hash{}.Bytes()...) // spender: the zero address
	sh = append(sh, u256hash(value).Bytes()...)
	sh = append(sh, u256hash(nonce).Bytes()...)
	sh = append(sh, u256hash(deadline).Bytes()...)
	digest := crypto.Keccak256([]byte{0x19, 0x01}, domTok.domainSeparator().Bytes(), crypto.Keccak256(sh))
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		t.Fatal(err)
	}
	ret, err = call(creator, token, b20Call(selPermit, addrKey(owner), common.Hash{},
		u256hash(value), u256hash(deadline), u256hash(uint64(sig[64]+27)),
		common.BytesToHash(sig[0:32]), common.BytesToHash(sig[32:64])))
	wantRevert(t, ret, err, errSelInvalidSpender, "valid signature over a zero spender")

	// The allowance must not have been written, which is the point of the guard.
	if got := newUnmeteredB20Storage(evm.StateDB, token).allowance(owner, common.Address{}); got.Sign() != 0 {
		t.Errorf("permit set an allowance of %s for the zero address", got)
	}
}

// TestB20RenounceLastAdminRevertOrder separates the two failures base-std
// distinguishes: a caller holding no admin role is unauthorized, and NotSoleAdmin
// is reserved for one who does hold it while others also do. A single collapsed
// condition told a stranger they were "not the sole admin".
func TestB20RenounceLastAdminRevertOrder(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	second := common.HexToAddress("0x5ec0nd")
	stranger := common.HexToAddress("0x57ra496")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	ret, err := call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x7e0"), creator, nil))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)
	renounce := b20Call(selRenounceLastAdmin)

	// A caller with no admin role, while there is exactly one admin — the
	// sole-admin condition holds, so only the role check can be speaking.
	ret, err = call(stranger, token, renounce)
	wantRevert(t, ret, err, errSelACUnauthorized, "no admin role, count is 1")

	// With two admins the holder is refused for the other reason.
	if _, err := call(creator, token, b20Call(selGrantRole, roleDefaultAdmin, addrKey(second))); err != nil {
		t.Fatalf("granting a second admin: %v", err)
	}
	ret, err = call(creator, token, renounce)
	wantRevert(t, ret, err, errSelNotSoleAdmin, "holds the role but is not the last")

	// And a stranger still gets the role error, not NotSoleAdmin.
	ret, err = call(stranger, token, renounce)
	wantRevert(t, ret, err, errSelACUnauthorized, "no admin role, count is 2")

	// Back to one admin, and it succeeds — so neither revert above was the setup.
	if _, err := call(creator, token, b20Call(selRevokeRole, roleDefaultAdmin, addrKey(second))); err != nil {
		t.Fatalf("revoking the second admin: %v", err)
	}
	if _, err := call(creator, token, renounce); err != nil {
		t.Fatalf("the sole admin could not renounce: %v", err)
	}
}
