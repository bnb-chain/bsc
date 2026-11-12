package core

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestSystemTransactionAccessList(t *testing.T) {
	for _, traced := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "traced"}[traced], func(t *testing.T) {
			config := *params.ParliaTestChainConfig
			config.AmsterdamTime = new(uint64)
			rules := config.Rules(common.Big1, false, 1)
			coinbase := common.HexToAddress("0xbeef")
			contract := common.HexToAddress("0xcafe")
			upgrade := common.HexToAddress("0xaaaa")
			omitted := common.HexToAddress("0xbbbb")
			readOnly := common.HexToAddress("0xcccc")
			tail := common.HexToAddress("0xdddd")
			code := []byte{byte(vm.PUSH1), 1, byte(vm.PUSH1), 0, byte(vm.SSTORE), byte(vm.STOP)}
			codeChange := []byte{byte(vm.STOP)}
			db := state.NewDatabaseForTesting()
			sdb, _ := state.New(types.EmptyRootHash, db)
			sdb.SetBalance(consensus.SystemAddress, uint256.NewInt(100), tracing.BalanceChangeUnspecified)
			sdb.SetCode(contract, code, tracing.CodeChangeUnspecified)
			sdb.SetCode(omitted, []byte{byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.REVERT)}, tracing.CodeChangeUnspecified)
			root, err := sdb.Commit(params.Rules{}, 0)
			if err != nil {
				t.Fatal(err)
			}
			sdb, _ = state.New(root, db)
			var inner vm.StateDB = sdb
			if traced {
				inner = state.NewHookedState(sdb, &tracing.Hooks{OnBalanceChange: func(common.Address, *big.Int, *big.Int, tracing.BalanceChangeReason) {}})
			}
			list := bal.NewConstructionBlockAccessList()
			systemState := &systemTxStateDB{StateDB: inner, accessList: list}
			// Finalize's direct reads are excluded; the transfer and upgrade
			// share the following system transaction's net-change baseline.
			systemState.GetBalance(readOnly)
			systemState.SetBalance(consensus.SystemAddress, common.U2560, tracing.BalanceChangeUnspecified)
			systemState.AddBalance(coinbase, uint256.NewInt(100), tracing.BalanceChangeUnspecified)
			systemState.SetCode(upgrade, codeChange, tracing.CodeChangeSystemContractUpgrade)
			systemState.Prepare(rules, coinbase, coinbase, &contract, vm.ActivePrecompiles(rules), nil)
			systemState.SetTxContext(common.Hash{1}, 0, 1)
			evm := vm.NewEVM(vm.BlockContext{BlockNumber: common.Big1, Time: 1, CanTransfer: CanTransfer, Transfer: Transfer}, systemState, &config, vm.Config{})
			defer evm.Release()
			if _, _, err := evm.Call(coinbase, contract, nil, vm.NewGasBudget(1_000_000, 0), uint256.NewInt(100)); err != nil {
				t.Fatal(err)
			}
			systemState.SetNonce(coinbase, 1, tracing.NonceChangeEoACall)
			systemState.Finalise(rules)

			// A failed, omitted attempt contributes no accesses. Its preceding
			// protocol write remains and belongs to n+1 because no tx follows.
			systemState.SetCode(tail, codeChange, tracing.CodeChangeSystemContractUpgrade)
			systemState.Prepare(rules, coinbase, coinbase, &omitted, vm.ActivePrecompiles(rules), nil)
			systemState.SetTxContext(common.Hash{2}, 1, 2)
			if _, _, err := evm.Call(coinbase, omitted, nil, vm.NewGasBudget(1_000_000, 0), common.U2560); !errors.Is(err, vm.ErrExecutionReverted) {
				t.Fatalf("omitted transaction: %v", err)
			}
			list.Merge(finaliseProtocolChanges(inner, rules, 2))

			want := bal.NewConstructionBlockAccessList()
			want.BalanceChange(1, consensus.SystemAddress, common.U2560)
			want.NonceChange(coinbase, 1, 1)
			want.BalanceChange(1, contract, uint256.NewInt(100))
			want.StorageWrite(1, contract, common.Hash{}, common.Hash{31: 1})
			want.CodeChange(upgrade, 1, codeChange)
			want.CodeChange(tail, 2, codeChange)
			if got := list.ToEncodingObj(); got.Hash() != want.ToEncodingObj().Hash() {
				t.Fatalf("unexpected BAL:\n%s\nwant:\n%s", list.PrettyPrint(), want.PrettyPrint())
			}
			replayed, _ := state.New(root, db)
			if err := replayed.ApplyBlockAccessList(list.ToEncodingObj()); err != nil {
				t.Fatal(err)
			}
			if got, want := replayed.IntermediateRoot(rules), sdb.IntermediateRoot(rules); got != want {
				t.Fatalf("BAL post-state %x, executed %x", got, want)
			}
		})
	}
}

type protocolBALTestEngine struct{ consensus.Engine }

func (e *protocolBALTestEngine) Finalize(_ consensus.ChainHeaderReader, _ *types.Header, s vm.StateDB, _ *types.Body, _ *[]*types.Receipt, _ *[]*types.Transaction, _ *uint64, _ *tracing.Hooks) error {
	s.GetBalance(common.HexToAddress("0xcccc"))
	s.SetCode(common.HexToAddress("0xdddd"), []byte{byte(vm.STOP)}, tracing.CodeChangeSystemContractUpgrade)
	return nil
}

func TestStateProcessorProtocolAccessList(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "pre-fork", true: "active"}[active], func(t *testing.T) {
			config := *params.ParliaTestChainConfig
			config.HaberTime, config.HaberFixTime, config.BohrTime, config.PascalTime = new(uint64), new(uint64), new(uint64), new(uint64)
			config.PragueTime = u64(1)
			config.LorentzTime, config.MaxwellTime, config.FermiTime = u64(1), u64(1), u64(1)
			config.OsakaTime, config.MendelTime, config.PasteurTime = u64(1), u64(1), u64(1)
			config.BlobScheduleConfig = &params.BlobScheduleConfig{Cancun: params.DefaultCancunBlobConfig, Prague: params.DefaultPragueBlobConfigBSC}
			if active {
				config.AmsterdamTime = u64(1)
			}
			db := rawdb.NewMemoryDatabase()
			defer db.Close()
			triedb := triedb.NewDatabase(db, nil)
			defer triedb.Close()
			genesis := (&Genesis{Config: &config}).MustCommit(db, triedb)
			engine := &protocolBALTestEngine{Engine: ethash.NewFaker()}
			defer engine.Close()
			chain, err := NewHeaderChain(db, &config, engine, func() bool { return false })
			if err != nil {
				t.Fatal(err)
			}
			sdb, _ := state.New(genesis.Root(), state.NewDatabase(triedb, nil))
			header := &types.Header{ParentHash: genesis.Hash(), Number: common.Big1, Time: 1, Difficulty: common.Big1, GasLimit: 30_000_000}
			block := types.NewBlockWithHeader(header)
			res, err := NewStateProcessor(chain).Process(context.Background(), block, sdb, nil, nil, vm.Config{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			list, hash := res.encodedAccessList()
			want := bal.NewConstructionBlockAccessList()
			if active {
				want.CodeChange(params.HistoryStorageAddress, 0, params.HistoryStorageCode)
				want.NonceChange(params.HistoryStorageAddress, 0, 1)
				want.StorageWrite(0, params.HistoryStorageAddress, common.Hash{}, genesis.Hash())
				want.CodeChange(common.HexToAddress("0xdddd"), 1, []byte{byte(vm.STOP)})
			}
			if hash != want.ToEncodingObj().Hash() {
				t.Fatalf("unexpected BAL:\n%s\nwant:\n%s", list.PrettyPrint(), want.PrettyPrint())
			}
			if res.GasUsed != 0 || len(res.Receipts) != 0 {
				t.Fatalf("protocol operations changed gas or receipts: %+v", res)
			}
			if active {
				replayed, _ := state.New(genesis.Root(), state.NewDatabase(triedb, nil))
				if err := replayed.ApplyBlockAccessList(list); err != nil {
					t.Fatal(err)
				}
				rules := config.Rules(common.Big1, false, 1)
				if got, want := replayed.IntermediateRoot(rules), sdb.IntermediateRoot(rules); got != want {
					t.Fatalf("BAL post-state %x, executed %x", got, want)
				}
			}
		})
	}
}
