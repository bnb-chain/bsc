package parlia

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestParliaSystemTransactionBAL(t *testing.T) {
	config := *params.ParliaTestChainConfig
	config.Parlia = &params.ParliaConfig{}
	config.HaberTime, config.HaberFixTime, config.BohrTime, config.PascalTime = new(uint64), new(uint64), new(uint64), new(uint64)
	config.PragueTime, config.LorentzTime, config.MaxwellTime, config.FermiTime = new(uint64), new(uint64), new(uint64), new(uint64)
	config.OsakaTime, config.MendelTime, config.PasteurTime, config.AmsterdamTime = new(uint64), new(uint64), new(uint64), new(uint64)
	config.BlobScheduleConfig = &params.BlobScheduleConfig{Cancun: params.DefaultCancunBlobConfig, Prague: params.DefaultPragueBlobConfigBSC}
	validatorContract := common.HexToAddress(systemcontracts.ValidatorContract)
	slashContract := common.HexToAddress(systemcontracts.SlashContract)
	code := []byte{byte(vm.PUSH1), 1, byte(vm.PUSH1), 0, byte(vm.SSTORE), byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.LOG0), byte(vm.STOP)}
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	triedb := triedb.NewDatabase(db, nil)
	defer triedb.Close()
	gspec := &core.Genesis{Config: &config, GasLimit: 30_000_000, Alloc: types.GenesisAlloc{
		consensus.SystemAddress:      {Balance: big.NewInt(100)},
		testAddr:                     {Balance: big.NewInt(1_000_000_000)},
		validatorContract:            {Code: code, Nonce: 1},
		slashContract:                {Code: code, Nonce: 1},
		params.HistoryStorageAddress: {Code: params.HistoryStorageCode, Nonce: 1},
	}}
	genesis := gspec.MustCommit(db, triedb)
	engine := New(&config, db, nil, genesis.Hash())
	defer engine.Close()
	engine.recentSnaps.Add(genesis.Hash(), newSnapshot(config.Parlia, engine.signatures, 0, genesis.Hash(), []common.Address{testAddr}, nil, nil))
	chain, err := core.NewBlockChain(db, gspec, engine, core.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	// Start after block one's contract-initialization transactions. The parent
	// uses the committed prestate, and its snapshot supplies the validator set.
	parent := genesis.Header()
	parent.Number = big.NewInt(1)
	parent.Coinbase, parent.Difficulty = testAddr, diffInTurn
	rawdb.WriteHeader(db, parent)
	engine.recentSnaps.Add(parent.Hash(), newSnapshot(config.Parlia, engine.signatures, 1, parent.Hash(), []common.Address{testAddr}, nil, nil))
	header := &types.Header{
		ParentHash: parent.Hash(), Number: big.NewInt(2), Time: 3,
		Difficulty: diffInTurn, Coinbase: testAddr, GasLimit: 30_000_000,
		BaseFee: common.Big0, ExcessBlobGas: new(uint64), BlobGasUsed: new(uint64),
		SlotNumber: new(uint64),
	}
	*header.SlotNumber = header.Number.Uint64()
	data, err := engine.validatorSetABI.Pack("deposit", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	msg := engine.getSystemMessage(testAddr, validatorContract, data, big.NewInt(100))
	tx := types.MustSignNewTx(testKey, engine.signer, &types.LegacyTx{To: &validatorContract, Gas: msg.GasLimit, GasPrice: common.Big0, Value: big.NewInt(100), Data: data})
	body := types.Body{Transactions: []*types.Transaction{tx}}
	block := types.NewBlockWithHeader(header).WithBody(body)
	sdb, err := state.New(parent.Root, state.NewDatabase(triedb, nil))
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), block, sdb, nil, nil, vm.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Receipts) != 1 || res.Receipts[0].TransactionIndex != 0 || res.Receipts[0].TxHash != tx.Hash() || res.GasUsed == 0 {
		t.Fatalf("unexpected system transaction result: %+v", res)
	}
	want := bal.NewConstructionBlockAccessList()
	want.StorageWrite(0, params.HistoryStorageAddress, common.Hash{31: 1}, parent.Hash())
	want.BalanceChange(1, consensus.SystemAddress, common.U2560)
	want.NonceChange(testAddr, 1, 1)
	want.BalanceChange(1, validatorContract, uint256.NewInt(100))
	want.StorageWrite(1, validatorContract, common.Hash{}, common.Hash{31: 1})
	list := want.ToEncodingObj()
	replayed, _ := state.New(parent.Root, state.NewDatabase(triedb, nil))
	if err := replayed.ApplyBlockAccessList(list); err != nil {
		t.Fatal(err)
	}
	header.Root = replayed.IntermediateRoot(config.Rules(header.Number, false, header.Time))
	header.GasUsed = res.GasUsed
	header.ReceiptHash = types.DeriveSha(res.Receipts, trie.NewStackTrie(nil))
	header.Bloom = types.MergeBloom(res.Receipts)
	hash := list.Hash()
	header.BlockAccessListHash = &hash
	block = types.NewBlockWithHeader(header).WithBody(body)
	if err := core.NewBlockValidator(&config, chain).ValidateState(block, sdb, res, false); err != nil {
		t.Fatalf("Parlia execution differs from the expected BAL and post-state: %v", err)
	}
	engine.Authorize(testAddr, nil, func(_ accounts.Account, tx *types.Transaction, _ *big.Int) (*types.Transaction, error) {
		return types.SignTx(tx, engine.signer, testKey)
	})
	t.Run("missing-bal", func(t *testing.T) {
		for _, lists := range [][]*bal.ConstructionBlockAccessList{nil, {nil}} {
			for _, packing := range []bool{false, true} {
				sdb, err := state.New(parent.Root, state.NewDatabase(triedb, nil))
				if err != nil {
					t.Fatal(err)
				}
				if packing {
					_, _, err = engine.FinalizeAndAssembleBidBlock(chain, types.CopyHeader(header), sdb, &types.Body{}, nil, nil, lists...)
				} else {
					_, _, err = core.AssembleBlock(engine, chain, types.CopyHeader(header), sdb, &types.Body{}, nil, lists...)
				}
				if err == nil {
					t.Fatal("assembled Amsterdam block without input BAL")
				}
				if sdb.GetNonce(testAddr) != 0 || sdb.GetBalance(consensus.SystemAddress).Uint64() != 100 {
					t.Fatal("missing BAL changed finalization state")
				}
			}
		}
	})
	for _, tc := range []struct {
		name    string
		packing bool
		user    bool
		traced  bool
		empty   bool
		slash   bool
	}{
		{name: "signed"},
		{name: "packing", packing: true},
		{name: "user-and-system", user: true},
		{name: "traced", traced: true},
		{name: "no-system-transactions", empty: true},
		{name: "multiple-system-transactions", slash: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := types.CopyHeader(parent)
			if tc.empty {
				sdb, err := state.New(parent.Root, state.NewDatabase(triedb, nil))
				if err != nil {
					t.Fatal(err)
				}
				sdb.SetBalance(consensus.SystemAddress, common.U2560, tracing.BalanceChangeUnspecified)
				parent.Root, err = sdb.Commit(config.Rules(parent.Number, false, parent.Time), parent.Number.Uint64())
				if err != nil {
					t.Fatal(err)
				}
				rawdb.WriteHeader(db, parent)
			}
			validators := []common.Address{testAddr}
			if tc.slash {
				validators = append(validators, common.HexToAddress("0x1234"))
			}
			engine.recentSnaps.Add(parent.Hash(), newSnapshot(config.Parlia, engine.signatures, 1, parent.Hash(), validators, nil, nil))
			newState := func() *state.StateDB {
				sdb, err := state.New(parent.Root, state.NewDatabase(triedb, nil))
				if err != nil {
					t.Fatal(err)
				}
				return sdb
			}
			miningState := newState()
			miningHeader := types.CopyHeader(header)
			miningHeader.ParentHash = parent.Hash()
			miningHeader.GasUsed = 0
			miningHeader.Bloom = types.Bloom{}
			if tc.slash {
				miningHeader.Difficulty = diffNoTurn
			}
			miningHeader.BlockAccessListHash = nil
			var hooks *tracing.Hooks
			var changes int
			if tc.traced {
				hooks = &tracing.Hooks{OnBalanceChange: func(common.Address, *big.Int, *big.Int, tracing.BalanceChangeReason) { changes++ }}
			}
			preEVM := vm.NewEVM(core.NewEVMBlockContext(miningHeader, chain, nil), miningState, &config, vm.Config{})
			defer preEVM.Release()
			list := core.PreExecution(context.Background(), nil, parent, &config, preEVM, header.Number, header.Time)
			body := &types.Body{}
			var receipts []*types.Receipt
			if tc.user {
				tx := types.MustSignNewTx(testKey, types.MakeSigner(&config, header.Number, header.Time), &types.LegacyTx{
					To: &validatorContract, Gas: 100_000, GasPrice: common.Big1,
				})
				miningState.SetTxContext(tx.Hash(), 0, 1)
				gp := core.NewGasPool(header.GasLimit)
				receipt, txBAL, err := core.ApplyTransaction(context.Background(), preEVM, gp, miningState, miningHeader, tx)
				if err != nil {
					t.Fatal(err)
				}
				list.Merge(txBAL)
				miningHeader.GasUsed = gp.Used()
				body.Transactions = append(body.Transactions, tx)
				receipts = append(receipts, receipt)
			}
			var produced *types.Block
			var err error
			if tc.packing {
				produced, receipts, err = engine.FinalizeAndAssembleBidBlock(chain, miningHeader, miningState, body, receipts, hooks, list)
			} else if tc.traced {
				produced, receipts, err = engine.FinalizeAndAssemble(chain, miningHeader, miningState, body, receipts, hooks, list)
			} else {
				produced, receipts, err = core.AssembleBlock(engine, chain, miningHeader, miningState, body, receipts, list)
			}
			if err != nil {
				t.Fatal(err)
			}
			if produced.AccessList() == nil || produced.BlockAccessListHash() == nil || produced.AccessList().Hash() != *produced.BlockAccessListHash() {
				t.Fatal("missing or inconsistent block access list")
			}
			if err := produced.AccessList().Validate(produced.GasLimit(), len(produced.Transactions())); err != nil {
				t.Fatal(err)
			}
			if !tc.user && !tc.empty && !tc.slash && produced.AccessList().Hash() != hash {
				t.Fatal("assembled BAL differs from expected system changes")
			}
			if tc.empty && len(produced.Transactions()) != 0 {
				t.Fatal("empty reward balance produced a system transaction")
			}
			if tc.slash {
				if len(produced.Transactions()) != 2 || list.Accounts[testAddr].NonceChanges[1] != 1 || list.Accounts[testAddr].NonceChanges[2] != 2 {
					t.Fatal("system transaction indices do not follow block order")
				}
			}
			if miningState.TxIndex() != len(produced.Transactions()) {
				t.Fatal("post-execution did not follow system transactions")
			}
			if tc.traced && changes == 0 {
				t.Fatal("finalization bypassed state tracing")
			}
			// Signing the packing template changes transaction hashes, not BAL indices or state.
			if tc.packing {
				body := produced.Body()
				body.Transactions = append([]*types.Transaction(nil), body.Transactions...)
				for i, tx := range body.Transactions {
					body.Transactions[i], err = engine.SignSystemTx(tx, config.ChainID)
					if err != nil {
						t.Fatal(err)
					}
				}
				produced = types.NewBlock(produced.Header(), body, receipts, trie.NewStackTrie(nil)).WithAccessListUnsafe(produced.AccessList())
			}
			imported := newState()
			res, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), produced, imported, nil, nil, vm.Config{Tracer: hooks}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := core.NewBlockValidator(&config, chain).ValidateState(produced, imported, res, false); err != nil {
				t.Fatalf("independent import: %v", err)
			}
			for i, receipt := range res.Receipts {
				if receipt.TxHash != produced.Transactions()[i].Hash() || receipt.TransactionIndex != uint(i) {
					t.Fatal("imported receipt has an incorrect transaction index or hash")
				}
				if receipt.GasUsed != receipts[i].GasUsed || receipt.CumulativeGasUsed != receipts[i].CumulativeGasUsed || len(receipt.Logs) != len(receipts[i].Logs) {
					t.Fatal("imported receipt differs from assembly")
				}
				for j, log := range receipt.Logs {
					if log.Index != receipts[i].Logs[j].Index || log.TxIndex != uint(i) || log.TxHash != receipt.TxHash || log.BlockHash != produced.Hash() {
						t.Fatal("imported log metadata does not follow block order")
					}
				}
			}
			applied := newState()
			if err := applied.ApplyBlockAccessList(produced.AccessList()); err != nil {
				t.Fatal(err)
			}
			if root := applied.IntermediateRoot(config.Rules(header.Number, false, header.Time)); root != produced.Root() {
				t.Fatalf("BAL replay root %s differs from produced root %s", root, produced.Root())
			}
			for _, sdb := range []*state.StateDB{imported, applied} {
				for _, addr := range []common.Address{testAddr, validatorContract, consensus.SystemAddress} {
					if sdb.GetBalance(addr).Cmp(miningState.GetBalance(addr)) != 0 || sdb.GetNonce(addr) != miningState.GetNonce(addr) {
						t.Fatalf("account state mismatch at %s", addr)
					}
				}
				if sdb.GetState(validatorContract, common.Hash{}) != miningState.GetState(validatorContract, common.Hash{}) {
					t.Fatal("system contract storage mismatch")
				}
			}
		})
	}
}
