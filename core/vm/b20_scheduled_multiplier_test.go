package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

const b20WadU64 = uint64(1_000_000_000_000_000_000)

// newScheduledAssetToken builds an Asset token whose creator holds OPERATOR_ROLE,
// plus a helper that runs a call at an arbitrary block timestamp. The clock is the
// whole point of this surface, so the harness has to move it.
func newScheduledAssetToken(t *testing.T, born uint64) (common.Address, func(uint64) *EVM, common.Address) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	seedActivation(statedb, b20ActivationAdmin)
	cfg := b20TestChainConfig()
	at := func(now uint64) *EVM { return NewEVM(b20BlockContext(now), statedb, cfg, Config{}) }

	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := at(born).Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x8056"), creator, [][]byte{
			b20Call(selGrantRole, roleOperator, addrKey(creator)),
			b20Call(selGrantRole, roleMint, addrKey(creator)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	return common.BytesToAddress(ret), at, creator
}

// TestB20ScheduledMultiplierFlipsWithoutAnEvent is the property that made adopting
// ERC-8056 a decision rather than an addition: multiplier() stops being a pure
// function of state written by transactions.
//
// Once a schedule matures the value changes on read, with no transaction and no
// log at the flip. An indexer rebuilding balances from the event stream alone
// diverges from the chain at that instant, which is why BEP-702 3.12 says so
// explicitly rather than leaving it to be discovered.
func TestB20ScheduledMultiplierFlipsWithoutAnEvent(t *testing.T) {
	const born, flip = uint64(100), uint64(500)
	token, at, operator := newScheduledAssetToken(t, born)

	read := func(now uint64, sel [4]byte, args ...common.Hash) *uint256.Int {
		t.Helper()
		ret, _, err := at(now).Call(operator, token, b20Call(sel, args...),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("read at t=%d: %v", now, err)
		}
		return new(uint256.Int).SetBytes(ret)
	}

	// Schedule 3x for the future. Nothing changes yet.
	statedb := at(born).StateDB
	ret, _, err := at(born).Call(operator, token,
		b20Call(selUpdateUIMultiplier, u256hash(3*b20WadU64), u256hash(flip)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("updateUIMultiplier: %v", err)
	}
	_ = ret
	logsBefore := len(statedb.(*state.StateDB).Logs())

	for _, now := range []uint64{born, flip - 1} {
		if got := read(now, selMultiplier); got.Uint64() != b20WadU64 {
			t.Errorf("t=%d multiplier = %s, want 1e18 — the schedule is not live yet", now, got)
		}
	}
	if got := read(born, selNewUIMultiplier); got.Uint64() != 3*b20WadU64 {
		t.Errorf("newUIMultiplier = %s, want 3e18", got)
	}
	if got := read(born, selEffectiveAt); got.Uint64() != flip {
		t.Errorf("effectiveAt = %s, want %d", got, flip)
	}

	// At the flip timestamp, and forever after, every conversion follows the new
	// value — and no log was written to say so.
	for _, now := range []uint64{flip, flip + 1_000_000} {
		if got := read(now, selMultiplier); got.Uint64() != 3*b20WadU64 {
			t.Errorf("t=%d multiplier = %s, want 3e18 after maturity", now, got)
		}
		if got := read(now, selUIMultiplier); got.Uint64() != 3*b20WadU64 {
			t.Errorf("t=%d uiMultiplier = %s, want it to agree with multiplier()", now, got)
		}
		if got := read(now, selToUIAmount, u256hash(100)); got.Uint64() != 300 {
			t.Errorf("t=%d toUIAmount(100) = %s, want 300", now, got)
		}
		if got := read(now, selFromUIAmount, u256hash(300)); got.Uint64() != 100 {
			t.Errorf("t=%d fromUIAmount(300) = %s, want 100", now, got)
		}
	}
	if got := len(statedb.(*state.StateDB).Logs()); got != logsBefore {
		t.Errorf("%d logs were written between the schedule and the flip, want none — "+
			"maturity is silent by design", got-logsBefore)
	}

	// effectiveAt keeps its now-past timestamp rather than clearing, which is how a
	// caller tells a matured schedule from one that never existed.
	if got := read(flip+1, selEffectiveAt); got.Uint64() != flip {
		t.Errorf("effectiveAt after maturity = %s, want it to stay at %d", got, flip)
	}
}

// TestB20ScheduleRevertOrder walks base-std's documented order for
// updateUIMultiplier: role, the multiplier's bounds, effectiveAt in the past,
// effectiveAt beyond uint64, then a live schedule already existing.
func TestB20ScheduleRevertOrder(t *testing.T) {
	const born = uint64(100)
	token, at, operator := newScheduledAssetToken(t, born)
	stranger := common.HexToAddress("0x57ra496")

	call := func(caller common.Address, now uint64, input []byte) ([]byte, error) {
		ret, _, err := at(now).Call(caller, token, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	sched := func(mul, when uint64) []byte {
		return b20Call(selUpdateUIMultiplier, u256hash(mul), u256hash(when))
	}
	overU64 := common.BigToHash(new(uint256.Int).Lsh(uint256.NewInt(1), 64).ToBig())

	// 1. Role outranks every argument.
	ret, err := call(stranger, born, sched(0, 1))
	wantRevert(t, ret, err, errSelACUnauthorized, "no OPERATOR_ROLE, zero multiplier, past timestamp")

	// 2. A zero multiplier outranks the timestamp.
	ret, err = call(operator, born, sched(0, 1))
	wantRevert(t, ret, err, errSelInvalidMultiplier, "zero multiplier with a past timestamp")

	// 3. A past timestamp outranks one that is also out of range... which cannot
	//    both hold, so check them separately with the other argument valid.
	ret, err = call(operator, born, sched(2*b20WadU64, born))
	wantRevert(t, ret, err, errSelEffectiveAtInPast, "effectiveAt equal to now is not in the future")

	// 4. Beyond uint64.
	ret, err = call(operator, born, append(append([]byte{}, selUpdateUIMultiplier[:]...),
		append(u256hash(2*b20WadU64).Bytes(), overU64.Bytes()...)...))
	wantRevert(t, ret, err, errSelEffectiveAtTooFar, "effectiveAt above type(uint64).max")

	// 5. And only once everything else passes does an existing live schedule bind.
	if _, err := call(operator, born, sched(2*b20WadU64, 500)); err != nil {
		t.Fatalf("the first schedule should succeed: %v", err)
	}
	ret, err = call(operator, born, sched(3*b20WadU64, 600))
	wantRevert(t, ret, err, errSelUIMulExists, "a second schedule while one is live")

	// A matured schedule is not a commitment, so it can be replaced silently.
	if _, err := call(operator, 500, sched(4*b20WadU64, 900)); err != nil {
		t.Fatalf("rescheduling over a matured record should succeed: %v", err)
	}
}

// TestB20CancelAndInstantOverride covers the two ways a schedule ends early, and
// the events base-std requires of each.
func TestB20CancelAndInstantOverride(t *testing.T) {
	const born = uint64(100)
	token, at, operator := newScheduledAssetToken(t, born)

	call := func(now uint64, input []byte) ([]types.Log, error) {
		evm := at(now)
		before := len(evm.StateDB.(*state.StateDB).Logs())
		_, _, err := evm.Call(operator, token, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		all := evm.StateDB.(*state.StateDB).Logs()
		out := make([]types.Log, 0, len(all)-before)
		for _, l := range all[before:] {
			out = append(out, *l)
		}
		return out, err
	}

	// Cancelling with nothing scheduled is refused.
	if _, err := call(born, b20Call(selCancelUIMultiplier)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("cancel with no schedule: %v, want a revert", err)
	}

	// Schedule, then cancel: one UIMultiplierUpdateCancelled carrying what was
	// dropped, and the pending reads go back to zero.
	if _, err := call(born, b20Call(selUpdateUIMultiplier, u256hash(3*b20WadU64), u256hash(500))); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	logs, err := call(born, b20Call(selCancelUIMultiplier))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(logs) != 1 || logs[0].Topics[0] != b20TopicUIMultiplierCancelled {
		t.Fatalf("cancel emitted %d logs, want one UIMultiplierUpdateCancelled", len(logs))
	}
	wantData := append(u256hash(3*b20WadU64).Bytes(), u256hash(500).Bytes()...)
	if !bytes.Equal(logs[0].Data, wantData) {
		t.Errorf("cancel data = %x, want the dropped multiplier and timestamp %x", logs[0].Data, wantData)
	}

	// The instant setter overrides a live schedule: cancellation first, then both
	// multiplier events, so one stream carries every change.
	if _, err := call(born, b20Call(selUpdateUIMultiplier, u256hash(5*b20WadU64), u256hash(500))); err != nil {
		t.Fatalf("re-schedule: %v", err)
	}
	logs, err = call(born, b20Call(selUpdateMultiplier, u256hash(2*b20WadU64)))
	if err != nil {
		t.Fatalf("instant override: %v", err)
	}
	if len(logs) != 3 ||
		logs[0].Topics[0] != b20TopicUIMultiplierCancelled ||
		logs[1].Topics[0] != b20TopicMultiplierUpdated ||
		logs[2].Topics[0] != b20TopicUIMultiplierUpdated {
		t.Errorf("instant override over a live schedule produced %d logs in the wrong "+
			"order; want Cancelled, MultiplierUpdated, UIMultiplierUpdated", len(logs))
	}

	// A matured schedule goes quietly — its value was already in force, so there is
	// nothing pending to withdraw.
	if _, err := call(born, b20Call(selUpdateUIMultiplier, u256hash(7*b20WadU64), u256hash(500))); err != nil {
		t.Fatalf("schedule again: %v", err)
	}
	logs, err = call(600, b20Call(selUpdateMultiplier, u256hash(9*b20WadU64)))
	if err != nil {
		t.Fatalf("instant over a matured schedule: %v", err)
	}
	for _, l := range logs {
		if l.Topics[0] == b20TopicUIMultiplierCancelled {
			t.Error("a matured schedule was announced as cancelled; it was already in force")
		}
	}
}

// TestB20AssetInterfaceIDs pins exactly which ERC-165 ids the variant advertises.
// Conversion (0x57854fc3) is absent even though toUIAmount and fromUIAmount are
// implemented, matching base-std — the advertised set is an observable value, not
// a summary of the implementation.
func TestB20AssetInterfaceIDs(t *testing.T) {
	token, at, caller := newScheduledAssetToken(t, 100)
	supports := func(id string) bool {
		t.Helper()
		var w common.Hash
		copy(w[:], common.FromHex(id))
		ret, _, err := at(100).Call(caller, token, b20Call(selSupportsInterface, w),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("supportsInterface(%s): %v", id, err)
		}
		return bytes.Equal(ret, encBool(true))
	}
	for _, tc := range []struct {
		id   string
		want bool
		name string
	}{
		{"0x01ffc9a7", true, "IERC165"},
		{"0xa60bf13d", true, "IScaledUIAmount"},
		{"0x4bd27648", true, "IScaledUIAmountNewUIMultiplier"},
		{"0xd890fd71", true, "IScaledUIAmountBalances"},
		{"0x57854fc3", false, "IScaledUIAmountConversion, deliberately unadvertised"},
		{"0xffffffff", false, "the ERC-165 invalid sentinel"},
	} {
		if got := supports(tc.id); got != tc.want {
			t.Errorf("supportsInterface(%s) = %v, want %v (%s)", tc.id, got, tc.want, tc.name)
		}
	}
}

// TestB20MaturedScheduleIsSettledBeforeReplacement covers the one path where the
// lazy effective-multiplier model can lose a value.
//
// Reads compute the effective multiplier and cannot write, so a matured schedule
// lives on in the pending slot rather than in the stored one. Replacing it with a
// new schedule therefore has to fold it into storage first. Without that the token
// silently drops back to its pre-maturity multiplier for the whole gap until the
// new schedule arrives — measured before the fix: 3x reverted to 1x for 400
// seconds and then jumped to 5x, revaluing every holder's balance twice with no
// event either time.
//
// Reachable with two ordinary successful calls, which is why it is worth its own
// test rather than a note.
func TestB20MaturedScheduleIsSettledBeforeReplacement(t *testing.T) {
	token, at, operator := newScheduledAssetToken(t, 100)
	mul := func(now uint64) uint64 {
		t.Helper()
		ret, _, err := at(now).Call(operator, token, b20Call(selMultiplier),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("multiplier() at t=%d: %v", now, err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64() / b20WadU64
	}
	schedule := func(now, factor, when uint64) {
		t.Helper()
		if _, _, err := at(now).Call(operator, token,
			b20Call(selUpdateUIMultiplier, u256hash(factor*b20WadU64), u256hash(when)),
			NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
			t.Fatalf("scheduling %dx at t=%d: %v", factor, now, err)
		}
	}

	schedule(100, 3, 500)
	if got := mul(600); got != 3 {
		t.Fatalf("after maturity multiplier = %dx, want 3x", got)
	}

	// Replace the matured schedule. The value it put in force must survive.
	schedule(600, 5, 1000)
	for _, now := range []uint64{600, 999} {
		if got := mul(now); got != 3 {
			t.Errorf("t=%d multiplier = %dx, want 3x — replacing a matured schedule "+
				"reverted the token to its pre-maturity value", now, got)
		}
	}
	if got := mul(1000); got != 5 {
		t.Errorf("t=1000 multiplier = %dx, want 5x", got)
	}

	// And it chains, so the fold is not a one-off.
	schedule(1100, 7, 1500)
	if got := mul(1200); got != 5 {
		t.Errorf("t=1200 multiplier = %dx, want 5x", got)
	}
	if got := mul(1500); got != 7 {
		t.Errorf("t=1500 multiplier = %dx, want 7x", got)
	}
}
