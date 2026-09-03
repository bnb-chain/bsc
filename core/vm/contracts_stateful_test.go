package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// probePrecompile records the context it was handed instead of doing work.
type probePrecompile struct {
	cas20StatefulBase
	got PrecompileContext
}

func (p *probePrecompile) Name() string { return "Probe" }

func (p *probePrecompile) RunStateful(ctx *PrecompileContext, _ []byte) ([]byte, error) {
	p.got = *ctx
	return nil, nil
}

// TestStatefulPrecompileCallContext pins the msg.sender and msg.value a stateful
// precompile is handed on each of the four call opcodes.
func TestStatefulPrecompileCallContext(t *testing.T) {
	var (
		probeAddr = common.HexToAddress("0x0b0be0")
		caller    = common.HexToAddress("0xca11e5")
		origin    = common.HexToAddress("0x0416019")
	)

	run := func(t *testing.T, invoke func(*EVM, *probePrecompile)) PrecompileContext {
		t.Helper()
		statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if err != nil {
			t.Fatal(err)
		}
		evm := NewEVM(BlockContext{
			CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
			Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
			BlockNumber: common.Big1,
		}, statedb, params.TestChainConfig, Config{})

		probe := &probePrecompile{}
		evm.SetPrecompiles(PrecompiledContracts{probeAddr: probe})
		invoke(evm, probe)
		return probe.got
	}

	const value = 7

	for _, tc := range []struct {
		name       string
		invoke     func(*EVM, *probePrecompile)
		wantCaller common.Address
		wantValue  uint64
		wantDirect bool
		wantRead   bool
	}{
		{
			name: "CALL",
			invoke: func(evm *EVM, _ *probePrecompile) {
				evm.Call(caller, probeAddr, nil, NewGasBudget(100_000), uint256.NewInt(value))
			},
			wantCaller: caller, wantValue: value, wantDirect: true,
		},
		{
			name: "STATICCALL",
			invoke: func(evm *EVM, _ *probePrecompile) {
				evm.StaticCall(caller, probeAddr, nil, NewGasBudget(100_000))
			},
			wantCaller: caller, wantValue: 0, wantDirect: true, wantRead: true,
		},
		{
			// CALLCODE transfers value to the caller itself, so the frame is
			// value-bearing and msg.sender is the immediate caller.
			name: "CALLCODE",
			invoke: func(evm *EVM, _ *probePrecompile) {
				evm.CallCode(caller, probeAddr, nil, NewGasBudget(100_000), uint256.NewInt(value))
			},
			wantCaller: caller, wantValue: value, wantDirect: false,
		},
		{
			// DELEGATECALL preserves the parent's msg.sender and inherits its
			// msg.value; neither is the immediate caller's.
			name: "DELEGATECALL",
			invoke: func(evm *EVM, _ *probePrecompile) {
				evm.DelegateCall(origin, caller, probeAddr, nil, NewGasBudget(100_000), uint256.NewInt(value))
			},
			wantCaller: origin, wantValue: value, wantDirect: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, tc.invoke)
			if got.Self != probeAddr {
				t.Errorf("Self = %s, want %s", got.Self.Hex(), probeAddr.Hex())
			}
			if got.Caller != tc.wantCaller {
				t.Errorf("Caller = %s, want %s", got.Caller.Hex(), tc.wantCaller.Hex())
			}
			switch {
			case got.Value == nil && tc.wantValue != 0:
				t.Errorf("Value = nil, want %d", tc.wantValue)
			case got.Value != nil && got.Value.Uint64() != tc.wantValue:
				t.Errorf("Value = %s, want %d", got.Value, tc.wantValue)
			}
			if got.DirectCall != tc.wantDirect {
				t.Errorf("DirectCall = %v, want %v", got.DirectCall, tc.wantDirect)
			}
			if got.ReadOnly != tc.wantRead {
				t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, tc.wantRead)
			}
		})
	}
}
