package vm

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// chargeGas only marks the frame; the state is discarded either way, but the node
// must not do the work first.
func TestCAS20ExhaustedBudgetStopsWork(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x0c9"), creator,
			[][]byte{cas20Call(selGrantRole, roleMint, addrKey(creator))}),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
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

	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			sdb := statedb.Copy()
			e := NewEVM(cas20BlockContext(1), sdb, cas20TestChainConfig(), Config{})
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

	// 25,000 gas cannot pay the calldata charge, so the batch must abandon at once.
	starved, funded := best(25_000, true), best(200_000_000, false)
	if starved*3 > funded {
		t.Errorf("a starved batch took %v against %v for one that could pay — the loop "+
			"is still running past exhaustion", starved, funded)
	}
}

// The same property for the initCalls loop, the most expensive per iteration.
func TestCAS20BootstrapStopsOnExhaustion(t *testing.T) {
	creator := common.HexToAddress("0xc4ea70")

	const n = 1500
	calls := make([][]byte, 0, n+1)
	for i := 0; i < n; i++ {
		who := common.BigToAddress(uint256.NewInt(uint64(0x500000 + i)).ToBig())
		calls = append(calls, cas20Call(selGrantRole, roleMint, addrKey(who)))
	}
	input := encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xb007"), creator, calls)

	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			_, e := newCAS20EVM(t)
			start := time.Now()
			_, _, err := e.Call(creator, CAS20FactoryAddress, input, NewGasBudget(budget), uint256.NewInt(0))
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

// The one loop whose bound comes from state, not calldata: the old tail's length
// is whatever a previous caller paid to store.
func TestCAS20OldTailReleaseStopsOnExhaustion(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	ret, _, err := evm.Call(creator, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xta11"), creator,
			[][]byte{cas20Call(selGrantRole, roleMetadata, addrKey(creator))}),
		NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	long := strings.Repeat("N", 40_000)
	if _, _, err := evm.Call(creator, token, encodeStringCall(selUpdateName, long),
		NewGasBudget(500_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("storing the long name: %v", err)
	}
	chunks := newUnmeteredCAS20Storage(statedb, token).stringChunks(slotAt(cas20SlotName))
	if chunks < 1000 {
		t.Fatalf("the long name occupies %d chunks, too few for this to measure", chunks)
	}

	short := encodeStringCall(selUpdateName, "x")
	best := func(budget uint64, wantErr bool) time.Duration {
		t.Helper()
		out := time.Hour
		for rep := 0; rep < 5; rep++ {
			sdb := statedb.Copy()
			e := NewEVM(cas20BlockContext(1), sdb, cas20TestChainConfig(), Config{})
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
			// The surviving long name is what makes the work re-buyable.
			if wantErr {
				if got := newUnmeteredCAS20Storage(sdb, token).stringChunks(slotAt(cas20SlotName)); got != chunks {
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

func warmed(db *state.StateDB, addr common.Address, slot common.Hash) bool {
	_, ok := db.SlotInAccessList(addr, slot)
	return ok
}

func TestCAS20UnaffordableCallDoesNoWork(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	ret, _, err := evm.Call(creator, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0x006"), creator, [][]byte{
			cas20Call(selGrantRole, roleMint, addrKey(creator)),
			cas20Call(selMint, addrKey(cas20Alice), u256hash(100)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	slot := cas20Storage{token: token}.balanceSlot(cas20Carol)
	if warmed(statedb, token, slot) {
		t.Fatal("carol's slot is already warm; the fixture cannot show a skipped read")
	}

	// Without evm.Call's snapshot, which would revert the access list and read the
	// slot cold whether or not the handler touched it.
	p, ok := resolveCAS20(token)
	if !ok {
		t.Fatal("the token address does not resolve to a precompile")
	}
	input := cas20Call(selBalanceOf, addrKey(cas20Carol))
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

func TestCAS20CalldataGasMatchesTheBEP(t *testing.T) {
	charged := func(words int) uint64 {
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		gas := NewGasBudget(10_000_000)
		ctx := &PrecompileContext{StateDB: statedb, Self: cas20Addr(cas20VariantAsset, 1), gas: &gas}
		before := gas.RegularGas
		ctx.chargeCalldata(make([]byte, words*32))
		return before - gas.RegularGas
	}
	for _, words := range []int{0, 1, 2, 10, 1000} {
		want := uint64(words) * (params.CopyGas + params.MemoryGas)
		if got := charged(words); got != want {
			t.Errorf("%d words charged %d, want %d — the table in BEP-702 3.14 is exhaustive "+
				"and forbids synthesizing opcode or memory-expansion overhead", words, got, want)
		}
	}
	// A quadratic term would show up in the differences even if the absolute values matched.
	if d := charged(1000) - charged(999); d != params.CopyGas+params.MemoryGas {
		t.Errorf("the 1000th word costs %d, want %d — the charge is not linear",
			d, params.CopyGas+params.MemoryGas)
	}
}

// A read zeroed for want of gas once let announce publish a complete disclosure on
// a refused frame. EndAnnouncement is the observable: it is the last write.
func TestCAS20AnnounceStopsAtTheUnpaidRead(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	admin := cas20TestCaller
	ret, _, err := evm.Call(admin, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xaa11"), admin, [][]byte{
			cas20Call(selGrantRole, roleOperator, addrKey(admin)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)
	p, ok := resolveCAS20(token)
	if !ok {
		t.Fatal("the token does not resolve")
	}

	// An empty calls array so the loop guard never gets a turn; a fresh id per probe
	// so each meets a cold slot.
	long := strings.Repeat("x", 600)

	probes, refused, zero := 0, 0, 0
	for g := uint64(2_000); g <= 320_000; g += 2_000 {
		before := len(statedb.Logs())
		_, _, err := runStatefulPrecompiledContract(evm, p.(StatefulPrecompiledContract),
			admin, token, encodeAnnounceWith(nil, fmt.Sprintf("announcement-%d", g), long, long),
			NewGasBudget(g), false, true, uint256.NewInt(0))
		added := statedb.Logs()[before:]
		probes++
		if err == nil {
			continue
		}
		refused++
		if !errors.Is(err, ErrOutOfGas) {
			t.Fatalf("budget %d: err = %v, want ErrOutOfGas", g, err)
		}
		if len(added) == 0 {
			zero++
		}
		// A log the frame paid for may be here; EndAnnouncement, emitted after the
		// refusal point, may not.
		for _, l := range added {
			if l.Topics[0] == cas20TopicEndAnnouncement {
				t.Fatalf("budget %d: the call ran out of gas yet reached EndAnnouncement, "+
					"so it kept executing after a charge was refused", g)
			}
		}
	}
	if refused == 0 || refused == probes {
		t.Fatalf("%d of %d probes were refused; the sweep needs both outcomes to be "+
			"straddling the boundary", refused, probes)
	}
	if zero == 0 {
		t.Error("no probe was refused before its first log, so the sweep never reached " +
			"the unpaid read this test is named for")
	}
}

// Two independent barriers stand between an unpaid read and a wrong answer, so no
// single-mutation test can witness this; the end-to-end property is what is pinned.
func TestCAS20AnnouncementViewNeverAnswersFromAnUnpaidRead(t *testing.T) {
	_, evm := newCAS20EVM(t)
	admin := cas20TestCaller
	ret, _, err := evm.Call(admin, CAS20FactoryAddress,
		encodeCreateCAS20(cas20VariantAsset, common.HexToHash("0xbb22"), admin, [][]byte{
			cas20Call(selGrantRole, roleOperator, addrKey(admin)),
		}), NewGasBudget(9_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)
	p, ok := resolveCAS20(token)
	if !ok {
		t.Fatal("the token does not resolve")
	}

	const id = "disclosure-1"
	if _, _, err := evm.Call(admin, token, encodeAnnounceWith(nil, id, "d", "u"),
		NewGasBudget(1_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("announce: %v", err)
	}

	input := encodeStringCall(selIsAnnouncementIdUsed, id)
	answered, refused := 0, 0
	for g := uint64(20); g <= 2_000; g += 20 {
		out, _, err := runStatefulPrecompiledContract(evm, p.(StatefulPrecompiledContract),
			admin, token, input, NewGasBudget(g), true, true, uint256.NewInt(0))
		if err != nil {
			if !errors.Is(err, ErrOutOfGas) {
				t.Fatalf("budget %d: err = %v, want ErrOutOfGas", g, err)
			}
			refused++
			continue
		}
		answered++
		if !bytes.Equal(out, encBool(true)) {
			t.Fatalf("budget %d: the view answered %x for an id that was announced. A read "+
				"that could not be paid for must fail the call, never default to zero", g, out)
		}
	}
	if answered == 0 || refused == 0 {
		t.Fatalf("%d answered, %d refused; the sweep needs both to straddle the read's price",
			answered, refused)
	}
}
