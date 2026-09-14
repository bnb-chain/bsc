package parlia

import (
	"bytes"
	"errors"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func jennerTestConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	zero, fork := uint64(0), uint64(1000)
	config.BohrTime, config.LorentzTime, config.MaxwellTime = &zero, &zero, &zero
	config.JennerTime = &fork
	return &config
}

func jennerTestSnapshot(parent *types.Header) *Snapshot {
	snap := &Snapshot{
		Number: parent.Number.Uint64(), Hash: parent.Hash(), EpochLength: 1000,
		TurnLength: 4, BlockInterval: 450, Recents: make(map[uint64]common.Address),
		Validators: make(map[common.Address]*ValidatorInfo),
	}
	for i := 1; i <= 21; i++ {
		snap.Validators[common.BigToAddress(big.NewInt(int64(i)))] = &ValidatorInfo{}
	}
	return snap
}

func TestJennerBackoffAndTimestampBoundary(t *testing.T) {
	config := jennerTestConfig()
	p := &Parlia{chainConfig: config}
	for _, recent := range []bool{false, true} {
		for _, parentTime := range []uint64{999, 1000, 1001} {
			parent := &types.Header{Number: big.NewInt(998), Time: parentTime}
			header := &types.Header{Number: big.NewInt(999), Time: 1005}
			snap := jennerTestSnapshot(parent)
			if recent {
				for i := uint64(0); i < uint64(snap.TurnLength); i++ {
					snap.Recents[snap.Number-i] = snap.inturnValidator()
				}
			}
			var delays []uint64
			for _, validator := range snap.validators() {
				if snap.inturn(validator) {
					if delay := p.backOffTime(snap, parent, header, validator); delay != 0 {
						t.Fatalf("in-turn backoff = %d", delay)
					}
					continue
				}
				delay := p.backOffTime(snap, parent, header, validator)
				delays = append(delays, delay)
				header.Coinbase = validator
				timestamp := parent.MilliTimestamp() + snap.BlockInterval + delay
				header.Time = timestamp / 1000
				header.SetMilliseconds(timestamp)
				if err := p.blockTimeVerifyForRamanujanFork(snap, header, parent); err != nil {
					t.Fatal(err)
				}
				timestamp--
				header.Time = timestamp / 1000
				header.SetMilliseconds(timestamp)
				if err := p.blockTimeVerifyForRamanujanFork(snap, header, parent); err == nil {
					t.Fatal("accepted a block before its eligible backup timestamp")
				}
			}
			slices.Sort(delays)
			initial := uint64(2000)
			if parentTime >= 1000 {
				initial = 1000
			}
			for rank, delay := range delays {
				want := initial + uint64(rank)*wiggleTime
				if recent {
					if rank == 0 {
						want = 0
					} else {
						want = initial + uint64(rank-1)*wiggleTime
					}
				}
				if delay != want {
					t.Fatalf("recent=%t parentTime=%d rank=%d: delay=%d want=%d", recent, parentTime, rank, delay, want)
				}
			}
		}
	}
}

func TestJennerMaintenanceSystemTransaction(t *testing.T) {
	for _, tc := range []struct {
		name              string
		number, timestamp uint64
		want              bool
	}{
		{"before fork", 999, 999, false},
		{"activation at epoch end", 999, 1000, true},
		{"epoch end", 999, 1001, true},
		{"epoch header", 1000, 1001, false},
		{"ordinary block", 1001, 1001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := jennerTestConfig()
			p := New(config, rawdb.NewMemoryDatabase(), nil, common.Hash{})
			key, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			producer := crypto.PubkeyToAddress(key.PublicKey)
			p.Authorize(producer, nil, func(_ accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
				return types.SignTx(tx, types.LatestSigner(config), key)
			})
			parent := &types.Header{Number: new(big.Int).SetUint64(tc.number - 1), Time: tc.timestamp - 1}
			header := &types.Header{Number: new(big.Int).SetUint64(tc.number), Time: tc.timestamp,
				ParentHash: parent.Hash(), Coinbase: producer, GasLimit: 55_000_000, BaseFee: new(big.Int), Difficulty: big.NewInt(2)}
			chain := &finalizedHeaderChain{cfg: config, current: parent, byHash: map[common.Hash]*types.Header{parent.Hash(): parent}}
			p.recentSnaps.Add(parent.Hash(), jennerTestSnapshot(parent))
			newState := func() *state.StateDB {
				s, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
				if err != nil {
					t.Fatal(err)
				}
				// An observable stand-in for the contract: store 1 in slot 0.
				s.SetCode(common.HexToAddress(systemcontracts.ValidatorContract), common.FromHex("0x600160005500"), tracing.CodeChangeUnspecified)
				return s
			}
			var signed []*types.Transaction
			for _, mode := range []systemTxMode{systemTxMining, systemTxPacking, systemTxImporting} {
				s := newState()
				var txs []*types.Transaction
				var receipts []*types.Receipt
				var usedGas uint64
				incoming := append([]*types.Transaction{}, signed...)
				err := p.checkMaintenance(chain, s, header, &txs, &receipts, &incoming, &usedGas, mode, nil)
				if err != nil {
					t.Fatal(err)
				}
				if (len(txs) == 1) != tc.want {
					t.Fatalf("mode=%v: system tx count=%d", mode, len(txs))
				}
				if tc.want {
					selector := p.selectorFor("checkMaintenance")
					if !bytes.Equal(txs[0].Data(), selector[:]) {
						t.Fatal("wrong maintenance call")
					}
					if s.GetState(common.HexToAddress(systemcontracts.ValidatorContract), common.Hash{}) != common.HexToHash("0x1") {
						t.Fatal("maintenance operation did not execute")
					}
					if len(receipts) != 1 || usedGas == 0 || s.GetNonce(producer) != 1 {
						t.Fatal("missing system tx effects")
					}
					if mode == systemTxMining {
						signed = txs
					}
				}
			}

			// Exercise both finalization entry points as well as the helper.
			if tc.name == "epoch end" {
				assembledState := newState()
				block, assembledReceipts, err := p.FinalizeAndAssemble(chain, header, assembledState, &types.Body{}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(block.Transactions()) != 1 || len(assembledReceipts) != 1 {
					t.Fatal("assembly omitted maintenance")
				}
				importedState := newState()
				var importedTxs []*types.Transaction
				var importedReceipts []*types.Receipt
				incoming := append([]*types.Transaction{}, block.Transactions()...)
				var gas uint64
				if err := p.Finalize(chain, block.Header(), importedState, &importedTxs, nil, nil, &importedReceipts, &incoming, &gas, nil); err != nil {
					t.Fatal(err)
				}
				if len(incoming) != 0 || len(importedTxs) != 1 || gas != block.GasUsed() {
					t.Fatal("import did not consume maintenance")
				}
				if importedState.IntermediateRoot(config.IsEIP158(header.Number)) != block.Root() {
					t.Fatal("import and assembly state roots differ")
				}

				revertingState := func() *state.StateDB {
					s := newState()
					s.SetCode(common.HexToAddress(systemcontracts.ValidatorContract), common.FromHex("0x60006000fd"), tracing.CodeChangeUnspecified)
					return s
				}
				// Contract failures and malformed/missing system transactions
				// must remain block errors, rather than silently skip maintenance.
				for _, mode := range []systemTxMode{systemTxMining, systemTxPacking} {
					_, _, err := p.finalizeAndAssemble(chain, types.CopyHeader(header), revertingState(), &types.Body{}, nil, nil, mode)
					if !errors.Is(err, vm.ErrExecutionReverted) {
						t.Fatalf("mode=%v: maintenance revert was not propagated: %v", mode, err)
					}
				}
				incoming = append([]*types.Transaction{}, signed...)
				importedTxs, importedReceipts, gas = nil, nil, 0
				if err := p.Finalize(chain, header, revertingState(), &importedTxs, nil, nil, &importedReceipts, &incoming, &gas, nil); !errors.Is(err, vm.ErrExecutionReverted) {
					t.Fatalf("import swallowed maintenance revert: %v", err)
				}
				if len(importedTxs) != 0 || len(importedReceipts) != 0 || gas != 0 {
					t.Fatal("reverted maintenance acquired a receipt")
				}
				incoming = nil
				if err := p.Finalize(chain, header, newState(), &importedTxs, nil, nil, &importedReceipts, &incoming, &gas, nil); err == nil {
					t.Fatal("import accepted missing maintenance")
				}
				incoming = []*types.Transaction{types.NewTransaction(0, common.HexToAddress(systemcontracts.ValidatorContract), new(big.Int), 100000, new(big.Int), nil)}
				if err := p.Finalize(chain, header, newState(), &importedTxs, nil, nil, &importedReceipts, &incoming, &gas, nil); err == nil {
					t.Fatal("import accepted a malformed maintenance transaction")
				}
			}
			if tc.want {
				var txs []*types.Transaction
				var receipts []*types.Receipt
				var usedGas uint64
				missing := []*types.Transaction{}
				if err := p.checkMaintenance(chain, newState(), header, &txs, &receipts, &missing, &usedGas, systemTxImporting, nil); err == nil {
					t.Fatal("accepted a missing mandatory maintenance transaction")
				}
				if got := p.EstimateGasReservedForSystemTxs(chain, header); got != params.SystemTxsGasHardLimit {
					t.Fatalf("gas reservation = %d", got)
				}
			}
		})
	}
}

func TestJennerBidBlockMaintenanceOrder(t *testing.T) {
	config := jennerTestConfig()
	p := New(config, rawdb.NewMemoryDatabase(), nil, common.Hash{})
	parent := &types.Header{Number: big.NewInt(998), Time: 86399}
	header := &types.Header{Number: big.NewInt(999), Time: 86400, ParentHash: parent.Hash()}
	chain := &finalizedHeaderChain{cfg: config, current: parent, byHash: map[common.Hash]*types.Header{parent.Hash(): parent}}
	p.recentSnaps.Add(parent.Hash(), jennerTestSnapshot(parent))
	deposit, update, maintenance := p.selectorFor("deposit"), p.selectorFor("updateValidatorSetV2"), p.selectorFor("checkMaintenance")
	txs := []*types.Transaction{sysTx(deposit[:], big.NewInt(1)), sysTx(update[:], big.NewInt(0)), sysTx(maintenance[:], big.NewInt(0))}
	decoded := &buildertypes.DecodedBidBlock{Header: header, Txs: txs}
	if err := p.VerifyBidBlockSystemTxs(chain, decoded, parent, 0); err != nil {
		t.Fatal(err)
	}
	decoded.Txs = txs[:2]
	if err := p.VerifyBidBlockSystemTxs(chain, decoded, parent, 0); err == nil {
		t.Fatal("accepted missing maintenance")
	}
	decoded.Txs = []*types.Transaction{txs[0], txs[2], txs[1]}
	if err := p.VerifyBidBlockSystemTxs(chain, decoded, parent, 0); err == nil {
		t.Fatal("accepted maintenance before daily settlement")
	}
	future := uint64(86401)
	config.JennerTime = &future
	decoded.Txs = txs
	if err := p.VerifyBidBlockSystemTxs(chain, decoded, parent, 0); err == nil {
		t.Fatal("accepted maintenance before activation")
	}
}

// An epoch's old/new ordering can differ. Enumerate every possible position of
// the failed validator on both sides, using Parlia's actual turn selection.
func TestJennerMaintenanceTransitionBudget(t *testing.T) {
	for _, turnLength := range []uint8{4, 8} {
		parent := &types.Header{Number: big.NewInt(999)}
		snap := jennerTestSnapshot(parent)
		snap.TurnLength = turnLength
		transition := 1000 + snap.minerHistoryCheckLen()
		beforeSwitch, afterSwitch := map[common.Address]int{}, map[common.Address]int{}
		for number := uint64(1000); number < 2000; number++ {
			snap.Number = number - 1
			validator := snap.inturnValidator()
			if number <= transition {
				beforeSwitch[validator]++
			} else {
				afterSwitch[validator]++
			}
		}
		maxMisses := 0
		for _, oldPosition := range snap.validators() {
			for _, newPosition := range snap.validators() {
				maxMisses = max(maxMisses, beforeSwitch[oldPosition]+afterSwitch[newPosition])
			}
		}
		want := 52
		if turnLength == 8 {
			want = 56
		}
		if maxMisses != want {
			t.Fatalf("turnLength=%d: maximum missed opportunities=%d want=%d", turnLength, maxMisses, want)
		}
		// Starting below 40, detection can see at most 39+52 ordinary
		// misses; at most four more opportunities precede snapshot removal.
		if turnLength == 4 && 39+maxMisses+int(turnLength) != 95 {
			t.Fatal("BEP-714 transition allowance changed")
		}
	}
}
