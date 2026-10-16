package vm

import (
	"bytes"
	"math/big"
	"testing"
)

// FuzzMilliTimestamp fuzzes the BEP-706 precompile through its real entry
// point. Deliberately NOT the tests/fuzzers/bls12381 pattern (which grabs a
// precompile out of the map and calls .Run() directly): milliTimestamp
// dispatches via RunWithBlockContext, and its Run() is a guarded error stub —
// see the comment on (*milliTimestamp).Run.
func FuzzMilliTimestamp(f *testing.F) {
	f.Add(uint64(0), uint64(0), []byte(nil))
	f.Add(uint64(1_800_000_000), uint64(0), []byte{0x01})
	f.Add(uint64(1_800_000_000), uint64(1_800_000_000_123), bytes.Repeat([]byte{0xff}, 128))
	f.Add(uint64(1), uint64(999), []byte{})

	c := &milliTimestamp{}
	f.Fuzz(func(t *testing.T, time uint64, milli uint64, input []byte) {
		ctx := BlockContext{Time: time, MilliTimestamp: milli}

		out, err := c.RunWithBlockContext(ctx, input)
		if err != nil {
			t.Fatalf("RunWithBlockContext must never fail: %v", err)
		}
		if len(out) != 32 {
			t.Fatalf("output must be exactly 32 bytes, got %d", len(out))
		}
		// The output is fully determined by the block context: calldata is
		// ignored (BEP-706 §4.2), so any input must produce the same bytes.
		ref, err := c.RunWithBlockContext(ctx, nil)
		if err != nil {
			t.Fatalf("RunWithBlockContext(nil input): %v", err)
		}
		if !bytes.Equal(out, ref) {
			t.Fatalf("output depends on calldata: %x (input %x) != %x (nil input)", out, input, ref)
		}
		// Value semantics: MilliTimestamp verbatim, or the Time*1000 fallback
		// when the context carries no millisecond value.
		want := milli
		if want == 0 {
			want = time * 1000
		}
		if got := new(big.Int).SetBytes(out).Uint64(); got != want {
			t.Fatalf("value mismatch: got %d, want %d (time=%d milli=%d)", got, want, time, milli)
		}
		// Gas never depends on the input either.
		if got := c.RequiredGas(input); got != 20 {
			t.Fatalf("RequiredGas = %d, want 20", got)
		}
	})
}
