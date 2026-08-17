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
