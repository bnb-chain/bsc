package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// milliTimestampTestHeader returns a header with the given second-precision
// time and sub-second millisecond remainder encoded the BEP-520 way (the
// remainder lives in MixDigest).
func milliTimestampTestHeader(seconds, millis uint64) *types.Header {
	var mix common.Hash
	if millis != 0 {
		mix = common.Hash(uint256.NewInt(millis).Bytes32())
	}
	return &types.Header{
		Number:     big.NewInt(1),
		Time:       seconds,
		MixDigest:  mix,
		Difficulty: big.NewInt(2),
	}
}

func TestMilliTimestamp_ReturnsHeaderValue(t *testing.T) {
	header := milliTimestampTestHeader(1_800_000_000, 750)
	want := header.MilliTimestamp()
	if want != 1_800_000_000*1000+750 {
		t.Fatalf("test setup wrong: header.MilliTimestamp() = %d", want)
	}

	c := &milliTimestamp{}
	out, err := c.RunWithBlockContext(BlockContext{Time: header.Time, MilliTimestamp: header.MilliTimestamp()}, nil)
	if err != nil {
		t.Fatalf("RunWithBlockContext: %v", err)
	}
	if len(out) != 32 {
		t.Fatalf("output must be 32 bytes, got %d", len(out))
	}
	got := new(big.Int).SetBytes(out)
	if got.Uint64() != want {
		t.Fatalf("got %d, want %d", got.Uint64(), want)
	}
	// Big-endian, left-padded: the value must live in the trailing bytes.
	wantBytes := common.LeftPadBytes(new(big.Int).SetUint64(want).Bytes(), 32)
	if !bytes.Equal(out, wantBytes) {
		t.Fatalf("encoding mismatch: got %x, want %x", out, wantBytes)
	}
}

func TestMilliTimestamp_IgnoresInput(t *testing.T) {
	c := &milliTimestamp{}
	ctx := BlockContext{MilliTimestamp: 1_800_000_000_123}

	ref, err := c.RunWithBlockContext(ctx, nil)
	if err != nil {
		t.Fatalf("RunWithBlockContext(nil input): %v", err)
	}
	for _, input := range [][]byte{
		{},
		{0x00},
		{0xde, 0xad, 0xbe, 0xef},
		bytes.Repeat([]byte{0xff}, 4096),
	} {
		out, err := c.RunWithBlockContext(ctx, input)
		if err != nil {
			t.Fatalf("RunWithBlockContext(%d-byte input): %v", len(input), err)
		}
		if !bytes.Equal(out, ref) {
			t.Fatalf("output must not depend on calldata: got %x with %d-byte input, want %x", out, len(input), ref)
		}
	}
}

func TestMilliTimestamp_GasCost(t *testing.T) {
	c := &milliTimestamp{}
	if params.MilliTimestampGas != 20 {
		t.Fatalf("MilliTimestampGas = %d, want 20 (BEP-706)", params.MilliTimestampGas)
	}
	for _, input := range [][]byte{nil, {0x01}, bytes.Repeat([]byte{0xff}, 1024)} {
		if got := c.RequiredGas(input); got != params.MilliTimestampGas {
			t.Fatalf("RequiredGas(%d-byte input) = %d, want 20", len(input), got)
		}
	}
}

// TestMilliTimestamp_ZeroMilliTimestampFallback covers callers that build a
// BlockContext by hand (core/vm/runtime, evm t8n, tests) and leave the new
// MilliTimestamp field zero: the precompile must degrade to Time*1000, not
// return a bare zero.
func TestMilliTimestamp_ZeroMilliTimestampFallback(t *testing.T) {
	c := &milliTimestamp{}
	out, err := c.RunWithBlockContext(BlockContext{Time: 1_800_000_000}, nil)
	if err != nil {
		t.Fatalf("RunWithBlockContext: %v", err)
	}
	if got := new(big.Int).SetBytes(out).Uint64(); got != 1_800_000_000*1000 {
		t.Fatalf("zero-MilliTimestamp fallback: got %d, want %d", got, uint64(1_800_000_000)*1000)
	}
}

// TestMilliTimestamp_DirectRunFails pins down that bypassing the dispatcher
// (the BLS-fuzzer style p.Run(input) pattern) fails loudly instead of
// fabricating a timestamp.
func TestMilliTimestamp_DirectRunFails(t *testing.T) {
	c := &milliTimestamp{}
	if _, err := c.Run(nil); err == nil {
		t.Fatalf("direct Run() must return an error")
	}
}
