package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
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

// milliTimestampAddr is test-local on purpose: production code inlines the
// address literal in the PrecompiledContractsJenner map and exports no
// constant for it.
var milliTimestampAddr = common.BytesToAddress([]byte{0x70})

// newJennerTestEVM builds an EVM on a Chapel-derived Parlia config with
// JennerTime scheduled; jennerActive selects a block right after or right
// before activation. It returns the EVM and the block's millisecond
// timestamp the precompile is expected to report.
func newJennerTestEVM(t *testing.T, jennerActive bool) (*EVM, uint64) {
	t.Helper()
	cfg := *params.ChapelChainConfig
	jennerTime := uint64(1_800_000_000)
	cfg.JennerTime = &jennerTime

	blockTime := jennerTime + 1
	if !jennerActive {
		blockTime = jennerTime - 1
	}
	wantMilli := blockTime*1000 + 123

	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	blockCtx := BlockContext{
		CanTransfer:    func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:       func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber:    big.NewInt(60_000_000),
		Time:           blockTime,
		Difficulty:     big.NewInt(2),
		GasLimit:       30_000_000,
		MilliTimestamp: wantMilli,
	}
	return NewEVM(blockCtx, statedb, &cfg, Config{}), wantMilli
}

// TestMilliTimestamp_PreActivation verifies that before Jenner, calling 0x70
// behaves exactly like calling an empty, codeless account: success with empty
// return data — not a revert, not an "invalid precompile" error.
func TestMilliTimestamp_PreActivation(t *testing.T) {
	evm, _ := newJennerTestEVM(t, false)
	if _, isPrecompile := evm.precompile(milliTimestampAddr); isPrecompile {
		t.Fatalf("0x70 must not be a precompile before Jenner")
	}
	for _, addr := range ActivePrecompiles(evm.chainRules) {
		if addr == milliTimestampAddr {
			t.Fatalf("0x70 must not be in ActivePrecompiles before Jenner")
		}
	}
	ret, gas, err := evm.Call(common.Address{1}, milliTimestampAddr, nil, NewGasBudget(100_000), new(uint256.Int))
	if err != nil {
		t.Fatalf("pre-activation call must succeed like an empty account call, got %v", err)
	}
	if len(ret) != 0 {
		t.Fatalf("pre-activation call must return empty data, got %x", ret)
	}
	if gas.RegularGas != 100_000 {
		t.Fatalf("pre-activation call must not consume gas in the callee, left %d", gas.RegularGas)
	}
}

// TestMilliTimestamp_PostActivation verifies that after Jenner, 0x70 is
// recognized as a precompile (including by the independent ActivePrecompiles
// switch feeding EIP-2929 warm-address preheating) and returns the block's
// millisecond timestamp for exactly 20 gas.
func TestMilliTimestamp_PostActivation(t *testing.T) {
	evm, wantMilli := newJennerTestEVM(t, true)
	if _, isPrecompile := evm.precompile(milliTimestampAddr); !isPrecompile {
		t.Fatalf("0x70 must be a precompile after Jenner")
	}
	found := false
	for _, addr := range ActivePrecompiles(evm.chainRules) {
		if addr == milliTimestampAddr {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("0x70 must be in ActivePrecompiles(rules) after Jenner (EIP-2929 warm set)")
	}
	ret, gas, err := evm.Call(common.Address{1}, milliTimestampAddr, nil, NewGasBudget(100_000), new(uint256.Int))
	if err != nil {
		t.Fatalf("post-activation call: %v", err)
	}
	if got := new(big.Int).SetBytes(ret).Uint64(); got != wantMilli {
		t.Fatalf("post-activation call returned %d, want %d", got, wantMilli)
	}
	if used := 100_000 - gas.RegularGas; used != params.MilliTimestampGas {
		t.Fatalf("call must consume exactly %d gas, used %d", params.MilliTimestampGas, used)
	}
}

// TestMilliTimestamp_StaticCall verifies the precompile works in a static
// (read-only) context: it is a pure read and must not be rejected.
func TestMilliTimestamp_StaticCall(t *testing.T) {
	evm, wantMilli := newJennerTestEVM(t, true)
	ret, _, err := evm.StaticCall(common.Address{1}, milliTimestampAddr, nil, NewGasBudget(100_000))
	if err != nil {
		t.Fatalf("STATICCALL must succeed: %v", err)
	}
	if got := new(big.Int).SetBytes(ret).Uint64(); got != wantMilli {
		t.Fatalf("STATICCALL returned %d, want %d", got, wantMilli)
	}
}

// TestMilliTimestamp_AllCallKindsUseBlockContext pins down that all four real
// dispatch sites (Call/CallCode/DelegateCall/StaticCall) route through
// RunWithBlockContext and none of them ever hits the Run() error branch.
func TestMilliTimestamp_AllCallKindsUseBlockContext(t *testing.T) {
	evm, wantMilli := newJennerTestEVM(t, true)
	caller := common.Address{1}
	zero := new(uint256.Int)

	kinds := map[string]func() ([]byte, GasBudget, error){
		"CALL": func() ([]byte, GasBudget, error) {
			return evm.Call(caller, milliTimestampAddr, nil, NewGasBudget(100_000), zero)
		},
		"CALLCODE": func() ([]byte, GasBudget, error) {
			return evm.CallCode(caller, milliTimestampAddr, nil, NewGasBudget(100_000), zero)
		},
		"DELEGATECALL": func() ([]byte, GasBudget, error) {
			return evm.DelegateCall(caller, caller, milliTimestampAddr, nil, NewGasBudget(100_000), zero)
		},
		"STATICCALL": func() ([]byte, GasBudget, error) {
			return evm.StaticCall(caller, milliTimestampAddr, nil, NewGasBudget(100_000))
		},
	}
	for kind, call := range kinds {
		ret, _, err := call()
		if err != nil {
			t.Fatalf("%s must dispatch via RunWithBlockContext, got error %v", kind, err)
		}
		if got := new(big.Int).SetBytes(ret).Uint64(); got != wantMilli {
			t.Fatalf("%s returned %d, want %d", kind, got, wantMilli)
		}
	}
}

// TestMilliTimestamp_BSCOnlyGate_VM verifies that a non-Parlia config can
// never select PrecompiledContractsJenner, even with JennerTime set and
// passed (params-level assertions live in params/config_jenner_test.go).
func TestMilliTimestamp_BSCOnlyGate_VM(t *testing.T) {
	cfg := *params.ChapelChainConfig
	jennerTime := uint64(1_800_000_000)
	cfg.JennerTime = &jennerTime
	cfg.Parlia = nil // not a BSC chain anymore

	rules := cfg.Rules(big.NewInt(60_000_000), false, jennerTime+1)
	if rules.IsJenner {
		t.Fatalf("Rules().IsJenner must be false on a non-Parlia config")
	}
	if _, ok := activePrecompiledContracts(rules)[milliTimestampAddr]; ok {
		t.Fatalf("non-Parlia config must not activate the Jenner precompile set")
	}
	for _, addr := range ActivePrecompiles(rules) {
		if addr == milliTimestampAddr {
			t.Fatalf("non-Parlia config must not list 0x70 in ActivePrecompiles")
		}
	}
}

// TestPrecompiles_UBTShadowsJenner pins down the selection precedence when
// both UBT and Jenner rules are active (only representable on a hypothetical
// Parlia+UBT devnet via RPC simulation — consensus never sets both): the UBT
// set keeps winning, exactly as it already shadows Pasteur and every other
// BSC fork set, and every UBT precompile stays available.
func TestPrecompiles_UBTShadowsJenner(t *testing.T) {
	m := activePrecompiledContracts(params.Rules{IsJenner: true, IsUBT: true})
	if _, ok := m[milliTimestampAddr]; ok {
		t.Fatalf("UBT must keep shadowing Jenner: 0x70 must not be in the active set")
	}
	for addr := range PrecompiledContractsVerkle {
		if _, ok := m[addr]; !ok {
			t.Fatalf("UBT precompile %v must remain available when Jenner is also active", addr)
		}
	}
	if len(m) != len(PrecompiledContractsVerkle) {
		t.Fatalf("active set must be exactly the UBT set: got %d entries, want %d", len(m), len(PrecompiledContractsVerkle))
	}
}

// TestPrecompiledContractsJenner_IsFreshMap guards against the map-aliasing
// pitfall: adding 0x70 to the Jenner map must not leak into the previous
// fork's map (maps are reference types; PrecompiledContractsBLS/Verkle are
// existing aliases of that dangerous kind).
func TestPrecompiledContractsJenner_IsFreshMap(t *testing.T) {
	if _, ok := PrecompiledContractsPasteur[milliTimestampAddr]; ok {
		t.Fatalf("0x70 leaked into PrecompiledContractsPasteur: PrecompiledContractsJenner must be an independent map literal")
	}
	if _, ok := PrecompiledContractsJenner[milliTimestampAddr]; !ok {
		t.Fatalf("PrecompiledContractsJenner must contain 0x70")
	}
	// Jenner must be a strict superset of the prior fork's map.
	for addr := range PrecompiledContractsPasteur {
		if _, ok := PrecompiledContractsJenner[addr]; !ok {
			t.Fatalf("PrecompiledContractsJenner is missing entry %v from the prior fork", addr)
		}
	}
	if len(PrecompiledContractsJenner) != len(PrecompiledContractsPasteur)+1 {
		t.Fatalf("PrecompiledContractsJenner must be the prior fork's map plus exactly 0x70")
	}
}
