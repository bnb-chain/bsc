package vm

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20ExhaustedBudgetStopsWork covers what chargeStateGas does not do: it marks
// the frame out of gas and returns, leaving the caller to continue. Every
// individual charge was correct, and the dispatcher failed the call at the end, so
// the state was always discarded — but the node had already done all the work.
//
// Measured before the loop guards: a 2000-recipient batchMint given 25,000 gas ran
// in the same wall-clock as one given 200,000,000, because gas ran out during the
// calldata charge and all 2000 mints proceeded anyway. At 8000 recipients that was
// 17ms bought for 25,000 gas. BEP-702 3.14 names worst-case execution time as a
// property to bound, and a chain targeting sub-second blocks cannot price work it
// then performs for free.
//
// Timing is the only way to see this: the state is reverted either way, so no
// storage assertion can tell the two apart. The threshold is deliberately loose —
// the point is order of magnitude, not a benchmark.
//
// The ABI decoders are deliberately left unguarded, and measured rather than
// assumed. They also run after exhaustion, but their work is linear in the
// calldata while the cost of delivering that calldata is quadratic in it — memory
// expansion carries a words^2/512 term, and a transaction pays 16 gas per byte.
// Measured on a starved budget: 1KB of calldata buys 3.2us of decoding for 107
// gas of memory, 512KB buys 1.44ms for 573,641. The ratio worsens with size,
// where batchMint's did the opposite: the same 574k bought 17ms there, because
// each recipient triggered trie work rather than a byte copy. Guarding the
// decoders would cost a branch per word to remove nothing.
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
