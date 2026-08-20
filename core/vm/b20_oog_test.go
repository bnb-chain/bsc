package vm

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20ExhaustedBudgetStopsWork covers what chargeStateGas does not do: it marks
// the frame out of gas and returns, leaving the caller to continue. Every
// individual charge was correct, and the dispatcher failed the call at the end, so
// the state was always discarded — but the node had already done all the work.
func TestB20ExhaustedBudgetStopsWork(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x0c9"), creator,
			[][]byte{b20Call(selGrantRole, roleMint, addrKey(creator))}),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	const n = 4000
	recips := make([]common.Address, n)
	amts := make([]uint64, n)
	for i := range recips {
		recips[i] = common.BigToAddress(uint256.NewInt(uint64(0x400000 + i)).ToBig())
		amts[i] = 1
	}
	input := encodeBatchMint(recips, amts)

	// Best of several runs, so a scheduling hiccup cannot fail the test.
	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			sdb := statedb.Copy()
			e := NewEVM(b20BlockContext(1), sdb, b20TestChainConfig(), Config{})
			start := time.Now()
			_, _, err := e.Call(creator, token, input, NewGasBudget(budget), uint256.NewInt(0))
			if d := time.Since(start); d < out {
				out = d
			}
			if wantErr && err == nil {
				t.Fatalf("a %d-recipient batch on %d gas should not succeed", n, budget)
			}
			if !wantErr && err != nil {
				t.Fatalf("a %d-recipient batch on %d gas should succeed: %v", n, budget, err)
			}
		}
		return out
	}

	// 25,000 gas cannot even pay the calldata charge for this payload, so the
	// batch must abandon immediately rather than mint 4000 times.
	// The funded run is the baseline for what all 4000 mints actually cost.
	starved, funded := best(25_000, true), best(200_000_000, false)
	if starved*3 > funded {
		t.Errorf("a starved batch took %v against %v for one that could pay — the loop "+
			"is still running past exhaustion", starved, funded)
	}
}

// TestB20BootstrapStopsOnExhaustion is the same property for the factory's
// initCalls loop, which the first pass missed: each entry dispatches a whole
// token call, making it the most expensive per iteration of all the
// caller-sized loops. Found by sweeping for the shape, not by a failing test.
func TestB20BootstrapStopsOnExhaustion(t *testing.T) {
	creator := common.HexToAddress("0xc4ea70")

	// A long bundle of real grants, so every iteration writes storage.
	const n = 1500
	calls := make([][]byte, 0, n+1)
	for i := 0; i < n; i++ {
		who := common.BigToAddress(uint256.NewInt(uint64(0x500000 + i)).ToBig())
		calls = append(calls, b20Call(selGrantRole, roleMint, addrKey(who)))
	}
	input := encodeCreateB20(b20VariantAsset, common.HexToHash("0xb007"), creator, calls)

	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			_, e := newB20EVM(t)
			start := time.Now()
			_, _, err := e.Call(creator, B20FactoryAddress, input, NewGasBudget(budget), uint256.NewInt(0))
			if d := time.Since(start); d < out {
				out = d
			}
			if wantErr && err == nil {
				t.Fatalf("a %d-call bootstrap on %d gas should not succeed", n, budget)
			}
			if !wantErr && err != nil {
				t.Fatalf("a %d-call bootstrap on %d gas should succeed: %v", n, budget, err)
			}
		}
		return out
	}

	starved, funded := best(60_000, true), best(400_000_000, false)
	if starved*3 > funded {
		t.Errorf("a starved bootstrap took %v against %v for one that could pay — the "+
			"initCalls loop is still running past exhaustion", starved, funded)
	}
}

// TestB20OldTailReleaseStopsOnExhaustion covers the one loop whose bound comes
// from state rather than from the call's own calldata: replacing a long stored
// string with a short one releases the old tail, and the old length is whatever a
// previous caller paid to store.
func TestB20OldTailReleaseStopsOnExhaustion(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0xta11"), creator,
			[][]byte{b20Call(selGrantRole, roleMetadata, addrKey(creator))}),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// A name long enough that clearing its tail is measurable, stored and paid for.
	long := strings.Repeat("N", 40_000)
	if _, _, err := evm.Call(creator, token, encodeStringCall(selUpdateName, long),
		NewGasBudget(500_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("storing the long name: %v", err)
	}
	chunks := newUnmeteredB20Storage(statedb, token).stringChunks(slotAt(b20SlotName))
	if chunks < 1000 {
		t.Fatalf("the long name occupies %d chunks, too few for this to measure", chunks)
	}

	short := encodeStringCall(selUpdateName, "x")
	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			sdb := statedb.Copy()
			e := NewEVM(b20BlockContext(1), sdb, b20TestChainConfig(), Config{})
			start := time.Now()
			_, _, err := e.Call(creator, token, short, NewGasBudget(budget), uint256.NewInt(0))
			if d := time.Since(start); d < out {
				out = d
			}
			if wantErr && err == nil {
				t.Fatalf("a starved updateName should not succeed")
			}
			if !wantErr && err != nil {
				t.Fatalf("a funded updateName should succeed: %v", err)
			}
			// The long name surviving a failed attempt is what makes the work
			// re-buyable, so assert it rather than assuming it.
			if wantErr {
				if got := newUnmeteredB20Storage(sdb, token).stringChunks(slotAt(b20SlotName)); got != chunks {
					t.Fatalf("after a reverted attempt the name occupies %d chunks, want %d", got, chunks)
				}
			}
		}
		return out
	}

	starved, funded := best(30_000, true), best(200_000_000, false)
	if starved*3 > funded {
		t.Errorf("a starved release took %v against %v for one that could pay — the "+
			"old-tail loop is still running past exhaustion", starved, funded)
	}
}

// TestB20UnaffordableCallDoesNoWork covers the DoS shape RequiredGas() == 0 opens.
func TestB20UnaffordableCallDoesNoWork(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x006"), creator, [][]byte{
			b20Call(selGrantRole, roleMint, addrKey(creator)),
			b20Call(selMint, addrKey(b20Alice), u256hash(100)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// A slot no earlier call touched, which balanceOf would warm.
	slot := b20Storage{token: token}.balanceSlot(b20Carol)
	if warmed(statedb, token, slot) {
		t.Fatal("carol's slot is already warm; the fixture cannot show a skipped read")
	}

	// Driven without evm.Call's snapshot: it reverts on error, and the access list
	// reverts with it, so through evm.Call the slot reads cold whether or not the
	// handler touched it. Asserting the end state there would prove nothing.
	p, ok := resolveB20(token)
	if !ok {
		t.Fatal("the token address does not resolve to a precompile")
	}
	// One gas: enough for the call frame, not for the calldata charge.
	input := b20Call(selBalanceOf, addrKey(b20Carol))
	out, left, err := runStatefulPrecompiledContract(evm, p.(StatefulPrecompiledContract),
		creator, token, input, NewGasBudget(1), false, true, uint256.NewInt(0))
	if !errors.Is(err, ErrOutOfGas) {
		t.Errorf("an unaffordable call err = %v, want ErrOutOfGas", err)
	}
	if len(out) != 0 {
		t.Errorf("returndata = %x, want empty", out)
	}
	if left.RegularGas != 0 {
		t.Errorf("gas left = %d, want 0", left.RegularGas)
	}
	if warmed(statedb, token, slot) {
		t.Error("the handler read state it could not pay for: carol's slot is warm after a " +
			"call that could not afford its calldata. RequiredGas is zero, so the entry check " +
			"is the only thing standing between a ~100-gas CALL and a handler's worth of work")
	}
}
