package vm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Storage operations use slot 1, without Solidity compiler or ABI overhead.
type cas20StorageOp struct {
	write bool
	value byte
}

type cas20StorageProgram struct {
	cas20StatefulBase
	ops            []cas20StorageOp
	valueFromInput bool
	revert         bool
}

func (p cas20StorageProgram) Name() string { return "CAS20 storage test" }

func (p cas20StorageProgram) bytecode() []byte {
	var code []byte
	for _, op := range p.ops {
		if op.write {
			if p.valueFromInput {
				code = append(code, byte(PUSH1), 0, byte(CALLDATALOAD))
			} else {
				code = append(code, byte(PUSH1), op.value)
			}
			code = append(code, byte(PUSH1), 1, byte(SSTORE))
		} else {
			code = append(code, byte(PUSH1), 1, byte(SLOAD), byte(POP))
		}
	}
	if p.revert {
		code = append(code, byte(PUSH1), 0, byte(PUSH1), 0, byte(REVERT))
	}
	return code
}

func (p cas20StorageProgram) RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error) {
	s := newMeteredCAS20Storage(ctx)
	for _, op := range p.ops {
		// Pay only the reference's stack scaffolding here. Storage gas and refunds
		// come exclusively from CAS20; the oracle executes actual EVM opcodes.
		// Charging before each access also aligns the SSTORE sentry's gas budget.
		pushes := uint64(1)
		if op.write {
			pushes = 2
		}
		if !ctx.chargeGas(pushes * ctx.evm.table[PUSH1].constantGas) {
			return nil, ErrOutOfGas
		}
		if op.write {
			value := common.Hash{31: op.value}
			if p.valueFromInput {
				if !ctx.chargeGas(ctx.evm.table[CALLDATALOAD].constantGas) {
					return nil, ErrOutOfGas
				}
				value = common.Hash{}
				copy(value[:], input)
			}
			if !s.setWord(common.Hash{31: 1}, value) {
				return nil, ErrOutOfGas
			}
		} else {
			if _, ok := s.getWordChecked(common.Hash{31: 1}); !ok {
				return nil, ErrOutOfGas
			}
			if !ctx.chargeGas(ctx.evm.table[POP].constantGas) {
				return nil, ErrOutOfGas
			}
		}
	}
	if p.revert {
		if !ctx.chargeGas(2 * ctx.evm.table[PUSH1].constantGas) {
			return nil, ErrOutOfGas
		}
		return finishCAS20Metered(ctx, nil, ErrExecutionReverted)
	}
	return finishCAS20Metered(ctx, nil, nil)
}

func cas20GasChainConfig(amsterdam bool) *params.ChainConfig {
	cfg := *params.ParliaTestChainConfig
	cfg.HaberTime, cfg.HaberFixTime, cfg.BohrTime, cfg.PascalTime = new(uint64), new(uint64), new(uint64), new(uint64)
	cfg.PragueTime, cfg.LorentzTime, cfg.MaxwellTime, cfg.FermiTime = new(uint64), new(uint64), new(uint64), new(uint64)
	cfg.OsakaTime, cfg.MendelTime, cfg.PasteurTime = new(uint64), new(uint64), new(uint64)
	cfg.JennerTime = new(uint64)
	cfg.AmsterdamTime = nil
	if amsterdam {
		cfg.AmsterdamTime = new(uint64)
	}
	cfg.BlobScheduleConfig = &params.BlobScheduleConfig{Cancun: params.DefaultCancunBlobConfig, Prague: params.DefaultPragueBlobConfigBSC}
	return &cfg
}

// Compare the native storage layer against the fork-selected bytecode engine,
// including refunds and EVM.Call rollback. Both paths start with identical code
// and state at the same address, so their complete state roots are comparable.
func TestCAS20StorageGasAgainstBytecode(t *testing.T) {
	for _, amsterdam := range []bool{false, true} {
		for _, table := range []bool{false, true} {
			t.Run(fmt.Sprintf("amsterdam=%t/table=%t", amsterdam, table), func(t *testing.T) {
				testCAS20StorageGasAgainstBytecode(t, cas20GasChainConfig(amsterdam), table)
			})
		}
	}
}

func testCAS20StorageGasAgainstBytecode(t *testing.T, cfg *params.ChainConfig, table bool) {
	if err := cfg.CheckConfigForkOrder(); err != nil {
		t.Fatal(err)
	}
	bc := cas20BlockContext(1)
	bc.CostPerStateByte = params.CostPerStateByte

	read := cas20StorageOp{}
	write := func(v byte) cas20StorageOp { return cas20StorageOp{write: true, value: v} }
	for _, tc := range []struct {
		name     string
		original byte
		ops      []cas20StorageOp
	}{
		{"read-zero", 0, []cas20StorageOp{read, read}},
		{"read-existing", 1, []cas20StorageOp{read, read}},
		{"noop-zero", 0, []cas20StorageOp{write(0)}},
		{"noop-existing", 1, []cas20StorageOp{write(1)}},
		{"create", 0, []cas20StorageOp{write(1), read}},
		{"reset", 1, []cas20StorageOp{write(2), read}},
		{"clear", 1, []cas20StorageOp{write(0), read}},
		{"dirty-created-update", 0, []cas20StorageOp{write(1), write(2), read}},
		{"dirty-created-noop", 0, []cas20StorageOp{write(1), write(1)}},
		{"dirty-created-restore", 0, []cas20StorageOp{write(1), write(0), read}},
		{"dirty-existing-update", 1, []cas20StorageOp{write(2), write(3), read}},
		{"dirty-existing-noop", 1, []cas20StorageOp{write(2), write(2)}},
		{"dirty-existing-clear", 1, []cas20StorageOp{write(2), write(0), read}},
		{"dirty-existing-restore", 1, []cas20StorageOp{write(2), write(1), read}},
		{"cleared-recreate", 1, []cas20StorageOp{write(0), write(2), read}},
		{"cleared-restore", 1, []cas20StorageOp{write(0), write(1), read}},
		{"clear-recreate-clear", 1, []cas20StorageOp{write(0), write(2), write(0)}},
		{"read-before-write", 1, []cas20StorageOp{read, write(0), read}},
	} {
		for _, warm := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/warm=%t", tc.name, warm), func(t *testing.T) {
				program := cas20StorageProgram{ops: tc.ops}
				addr := common.HexToAddress("0x1234") // outside the native address family
				slot := common.Hash{31: 1}
				initial, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
				if err != nil {
					t.Fatal(err)
				}
				initial.SetCode(addr, program.bytecode(), tracing.CodeChangeContractCreation)
				initial.SetState(addr, slot, common.Hash{31: tc.original})
				// Finalise makes original the transaction's committed value. Dirty
				// slots and their refunds are then produced by the operation sequence.
				initial.Finalise(params.Rules{IsEIP158: true})
				initial.AddAddressToAccessList(addr)
				if warm {
					initial.AddSlotToAccessList(addr, slot)
				}
				type result struct {
					gas    GasBudget
					refund uint64
					root   common.Hash
					err    error
				}
				run := func(native bool, budget GasBudget) result {
					sdb := initial.Copy()
					evm := NewEVM(bc, sdb, cfg, Config{})
					evm.forceTableLoop = table
					defer evm.Release()
					if native {
						evm.SetPrecompiles(PrecompiledContracts{addr: program})
					}
					_, left, err := evm.Call(cas20Alice, addr, nil, budget, new(uint256.Int))
					refund := sdb.GetRefund() // IntermediateRoot finalises and clears refunds.
					return result{left, refund, sdb.IntermediateRoot(params.Rules{IsEIP158: true}), err}
				}
				ample := NewGasBudget(1_000_000, 0)
				oracle := run(false, ample)
				if oracle.err != nil {
					t.Fatalf("reference execution failed: %v", oracle.err)
				}
				used := ample.ExecutionGas - oracle.gas.ExecutionGas
				// Budgets 2305-2307 leave 2299, 2300 and 2301 gas after
				// the two PUSH1s preceding a first SSTORE. Exact/short budgets
				// also exercise failures after earlier writes and refund changes.
				budgets := []GasBudget{ample, NewGasBudget(used, 0), NewGasBudget(used-1, 0), NewGasBudget(2305, 0), NewGasBudget(2306, 0), NewGasBudget(2307, 0)}
				if cfg.IsAmsterdam(bc.BlockNumber, bc.Time) {
					budgets = append(budgets, NewGasBudget(20_000, 100_000), NewGasBudget(30_000, 0), NewGasBudget(1_000_000, 100_000), NewGasBudget(20_000, 10_000))
				}
				for _, budget := range budgets {
					t.Run(fmt.Sprintf("execution=%d/state=%d", budget.ExecutionGas, budget.StateGas), func(t *testing.T) {
						want, got := run(false, budget), run(true, budget)
						for _, err := range []error{want.err, got.err} {
							if err != nil && !errors.Is(err, ErrOutOfGas) {
								t.Fatalf("unexpected execution error: %v", err)
							}
						}
						if (got.err == nil) != (want.err == nil) || got.gas != want.gas || got.refund != want.refund || got.root != want.root {
							t.Fatalf("native: gas=%+v refund=%d root=%s err=%v; bytecode: gas=%+v refund=%d root=%s err=%v",
								got.gas, got.refund, got.root, got.err, want.gas, want.refund, want.root, want.err)
						}
					})
				}
			})
		}
	}
}

func TestCAS20StorageFramesAgainstBytecode(t *testing.T) {
	for _, amsterdam := range []bool{false, true} {
		for _, table := range []bool{false, true} {
			for _, tc := range []struct {
				name           string
				values         []byte
				forward, state uint64
				revert         bool
			}{
				{"limited-forward", []byte{1}, 20_000, 100_000, false},
				{"refill", []byte{1, 0}, 200_000, 200_000, false},
				{"spill-refill", []byte{1, 0}, 200_000, 0, false},
				{"revert", []byte{1}, 200_000, 100_000, true},
				{"oog", []byte{1}, 3_000, 100_000, false},
			} {
				t.Run(fmt.Sprintf("amsterdam=%t/table=%t/%s", amsterdam, table, tc.name), func(t *testing.T) {
					cfg := cas20GasChainConfig(amsterdam)
					bc := cas20BlockContext(1)
					bc.CostPerStateByte = params.CostPerStateByte
					target, wrapper := common.HexToAddress("0x1234"), common.HexToAddress("0x5678")
					program := cas20StorageProgram{ops: []cas20StorageOp{{write: true}}, valueFromInput: true, revert: tc.revert}
					var code []byte
					for i, v := range tc.values {
						code = append(code, byte(PUSH1), v, byte(PUSH1), 0, byte(MSTORE))
						code = append(code, byte(PUSH1), 0, byte(PUSH1), 0, byte(PUSH1), 32, byte(PUSH1), 0, byte(PUSH1), 0, byte(PUSH20))
						code = append(code, target[:]...)
						code = append(code, byte(PUSH3), byte(tc.forward>>16), byte(tc.forward>>8), byte(tc.forward), byte(CALL), byte(PUSH1), byte((i+1)*32), byte(MSTORE))
					}
					code = append(code, byte(PUSH1), byte(len(tc.values)*32), byte(PUSH1), 32, byte(RETURN))
					initial, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
					initial.SetCode(target, program.bytecode(), tracing.CodeChangeUnspecified)
					initial.SetCode(wrapper, code, tracing.CodeChangeUnspecified)
					initial.Finalise(cfg.Rules(bc.BlockNumber, true, bc.Time))
					type result struct {
						output []byte
						gas    GasBudget
						refund uint64
						root   common.Hash
					}
					run := func(native bool) result {
						st := initial.Copy()
						evm := NewEVM(bc, st, cfg, Config{})
						defer evm.Release()
						evm.forceTableLoop = table
						if native {
							evm.SetPrecompiles(PrecompiledContracts{target: program})
						}
						reservoir := tc.state
						if !amsterdam {
							reservoir = 0
						}
						ret, left, err := evm.Call(cas20Alice, wrapper, nil, NewGasBudget(1_000_000, reservoir), new(uint256.Int))
						if err != nil {
							t.Fatal(err)
						}
						refund := st.GetRefund()
						return result{ret, left, refund, st.IntermediateRoot(evm.GetRules())}
					}
					want, got := run(false), run(true)
					if !bytes.Equal(want.output, got.output) || want.gas != got.gas || want.refund != got.refund || want.root != got.root {
						t.Fatalf("native %+v; bytecode %+v", got, want)
					}
					if amsterdam && tc.name == "limited-forward" && got.output[31] != 1 {
						t.Fatal("state reservoir did not fund the forwarded call")
					}
				})
			}
		}
	}
}

func TestCAS20StateRefillGasTrace(t *testing.T) {
	for _, reservoir := range []uint64{0, 200_000} {
		cfg := cas20GasChainConfig(true)
		bc := cas20BlockContext(1)
		bc.CostPerStateByte = params.CostPerStateByte
		st, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		addr := common.HexToAddress("0x1234")
		program := cas20StorageProgram{ops: []cas20StorageOp{{write: true, value: 1}, {write: true, value: 0}}}
		st.SetCode(addr, program.bytecode(), tracing.CodeChangeUnspecified)
		st.Finalise(cfg.Rules(bc.BlockNumber, true, bc.Time))
		gas := NewGasBudget(200_000, reservoir)
		last := gas.AsTracing()
		hooks := &tracing.Hooks{OnGasChangeV2: func(before, after tracing.Gas, _ tracing.GasChangeReason) {
			if before != last {
				t.Errorf("gas event gap: previous %v, next %v", last, before)
			}
			last = after
		}}
		evm := NewEVM(bc, st, cfg, Config{Tracer: hooks})
		defer evm.Release()
		ctx := &PrecompileContext{evm: evm, StateDB: st, Self: addr, gas: &gas}
		if _, err := program.RunStateful(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if gas.UsedStateGas != 0 || last != gas.AsTracing() {
			t.Fatalf("refill gas=%v, final event=%v", gas, last)
		}
	}
}
