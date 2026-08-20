package vm

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// milliTimestamp implements the BEP-706 millisecond-precision block timestamp
// precompile, active from the Jenner fork at address 0x70. It returns the
// executing block's millisecond timestamp (Header.MilliTimestamp(), BEP-520)
// as a left-padded 32-byte big-endian integer. Calldata is ignored entirely.
type milliTimestamp struct{}

func (c *milliTimestamp) RequiredGas(input []byte) uint64 {
	return params.MilliTimestampGas
}

// Run only exists to satisfy PrecompiledContract. Every real call path
// (Call/CallCode/DelegateCall/StaticCall, all via RunPrecompiledContract)
// dispatches to RunWithBlockContext instead, because milliTimestamp also
// implements BlockContextPrecompiledContract — see the dispatch test that
// pins this down. The only known way to actually reach this method is code
// that fetches a contract straight out of a PrecompiledContracts map and
// calls .Run() on it directly, bypassing the dispatcher — an existing
// pattern in this repo (see tests/fuzzers/bls12381/precompile_fuzzer.go for
// the BLS precompiles). Do NOT copy that pattern for milliTimestamp: call
// RunWithBlockContext instead. Returning a loud, explicit error here is
// deliberate: it fails safely (no state change, no silently-wrong value)
// rather than fabricating a plausible-looking but fake timestamp.
func (c *milliTimestamp) Run(input []byte) ([]byte, error) {
	return nil, errors.New("milliTimestamp: must be dispatched with block context, direct Run() is not supported")
}

func (c *milliTimestamp) Name() string {
	return "MILLI_TIMESTAMP"
}

// RunWithBlockContext is the actual entry point, dispatched from
// RunPrecompiledContract when the block context carrying the millisecond
// timestamp is available. It falls back to Time*1000 if MilliTimestamp is
// zero: some non-live-path BlockContext constructors (core/vm/runtime,
// evm t8n, various tests) don't fill the new field, and a second-precision
// value padded with .000 is "degraded but correct", unlike a near-1970
// garbage value.
func (c *milliTimestamp) RunWithBlockContext(blockCtx BlockContext, input []byte) ([]byte, error) {
	ts := blockCtx.MilliTimestamp
	if ts == 0 {
		ts = blockCtx.Time * 1000
	}
	return common.LeftPadBytes(new(big.Int).SetUint64(ts).Bytes(), 32), nil
}
