package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestB20CalldataGasMirrorsCalldatacopy pins chargeCalldata against the
// interpreter's cost for the operation it mirrors, computed here from the
// interpreter's own pieces so no step passes back through the function under
// test.
//
// TestB20GasNeverCheaperThanBytecode cannot catch an error here: it compares B20
// against B20, and its exact assertions hold between transfer shapes that all
// carry 68 bytes of calldata, so any scaling of chargeCalldata keeps them equal.
func TestB20CalldataGasMirrorsCalldatacopy(t *testing.T) {
	// interpreterCost is what bytecode pays to copy n bytes of its own calldata
	// into empty memory.
	interpreterCost := func(n int) uint64 {
		words := (uint64(n) + 31) / 32
		if words == 0 {
			return 0
		}
		return GasFastestStep + // the CALLDATACOPY itself
			params.CopyGas*words + // the copy
			params.MemoryGas*words + // memory expansion, linear
			words*words/params.QuadCoeffDiv // memory expansion, quadratic
	}

	charged := func(n int) uint64 {
		budget := NewGasBudget(100_000_000)
		ctx := &PrecompileContext{gas: &budget}
		ctx.chargeCalldata(make([]byte, n))
		return ctx.frameGas().stateGasUsed
	}

	for _, n := range []int{0, 1, 4, 32, 36, 68, 100, 1024, 32 * 1000} {
		want, got := interpreterCost(n), charged(n)
		if got != want {
			t.Errorf("chargeCalldata(%d bytes) = %d, interpreter charges %d (short by %d)",
				n, got, want, int64(want)-int64(got))
		}
	}

	// The quadratic term has to be present, not merely approximated: at a
	// thousand words it is the dominant part of the gap that was missing.
	const words = 1000
	quad := uint64(words) * words / params.QuadCoeffDiv
	if quad == 0 {
		t.Fatal("the quadratic term must be non-zero at this size for the check below to mean anything")
	}
	linearOnly := GasFastestStep + words*b20CalldataWordGas
	if got := charged(words * 32); got != linearOnly+quad {
		t.Errorf("at %d words: charged %d, want %d (linear %d + quadratic %d)",
			words, got, linearOnly+quad, linearOnly, quad)
	}

	// And BEP-702 3.14's rule, stated directly: never cheaper, at any size.
	for n := 0; n <= 4096; n += 32 {
		if charged(n) < interpreterCost(n) {
			t.Fatalf("charged %d < interpreter %d at %d bytes — cheaper than bytecode",
				charged(n), interpreterCost(n), n)
		}
	}
}
