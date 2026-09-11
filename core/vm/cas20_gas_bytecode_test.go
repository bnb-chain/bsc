package vm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// Each operation accesses slot 1. Values fit PUSH1 so the reference needs only
// PUSH1, SLOAD, SSTORE and POP, with no Solidity compiler or ABI overhead.
type cas20StorageOp struct {
	write bool
	value byte
}

type cas20StorageProgram struct {
	cas20StatefulBase
	ops []cas20StorageOp
}

func (p cas20StorageProgram) Name() string { return "CAS20 storage test" }

func (p cas20StorageProgram) bytecode() []byte {
	var code []byte
	for _, op := range p.ops {
		if op.write {
			code = append(code, byte(PUSH1), op.value, byte(PUSH1), 1, byte(SSTORE))
		} else {
			code = append(code, byte(PUSH1), 1, byte(SLOAD), byte(POP))
		}
	}
	return code
}

func (p cas20StorageProgram) RunStateful(ctx *PrecompileContext, _ []byte) ([]byte, error) {
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
			if !s.setWord(common.Hash{31: 1}, common.Hash{31: op.value}) {
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
	return finishCAS20Metered(ctx, nil, nil)
}

// Compare the native storage layer against the fork-selected bytecode engine,
// including refunds and EVM.Call rollback. Both paths start with identical code
// and state at the same address, so their complete state roots are comparable.
func TestCAS20StorageGasAgainstBytecode(t *testing.T) {
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
				initial.Finalise(true)
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
				run := func(native bool, budget uint64) result {
					sdb := initial.Copy()
					evm := NewEVM(cas20BlockContext(1), sdb, cas20TestChainConfig(), Config{})
					defer evm.Release()
					if native {
						evm.SetPrecompiles(PrecompiledContracts{addr: program})
					}
					_, left, err := evm.Call(cas20Alice, addr, nil, NewGasBudget(budget), new(uint256.Int))
					refund := sdb.GetRefund() // IntermediateRoot finalises and clears refunds.
					return result{left, refund, sdb.IntermediateRoot(true), err}
				}
				const ample = uint64(1_000_000)
				oracle := run(false, ample)
				if oracle.err != nil {
					t.Fatalf("reference execution failed: %v", oracle.err)
				}
				used := ample - oracle.gas.RegularGas
				// The last three budgets leave 2299, 2300 and 2301 gas after
				// the two PUSH1s preceding a first SSTORE. Exact/short budgets
				// also exercise failures after earlier writes and refund changes.
				for _, budget := range []uint64{ample, used, used - 1, 2305, 2306, 2307} {
					t.Run(fmt.Sprintf("gas=%d", budget), func(t *testing.T) {
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
