package parlia

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
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
	config.OsakaTime, config.MendelTime, config.PasteurTime = new(uint64), new(uint64), new(uint64)
	forkTime := uint64(4)
	config.JennerTime, config.AmsterdamTime = &forkTime, &forkTime
	config.BlobScheduleConfig = &params.BlobScheduleConfig{Cancun: params.DefaultCancunBlobConfig, Prague: params.DefaultPragueBlobConfigBSC}
	validatorContract := common.HexToAddress(systemcontracts.ValidatorContract)
	userKey, err := crypto.ToECDSA(common.LeftPadBytes([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	user := crypto.PubkeyToAddress(userKey.PublicKey)
	recipient := common.HexToAddress("0xcafe")
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	tdb := triedb.NewDatabase(db, nil)
	defer tdb.Close()
	gspec := &core.Genesis{Config: &config, GasLimit: 30_000_000, Alloc: types.GenesisAlloc{
		user:                         {Balance: big.NewInt(2)},
		recipient:                    {Code: []byte{byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.LOG0), byte(vm.STOP)}, Balance: big.NewInt(1)},
		consensus.SystemAddress:      {Balance: big.NewInt(100)},
		validatorContract:            {Code: []byte{byte(vm.PUSH1), 1, byte(vm.PUSH1), 0, byte(vm.SSTORE), byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.LOG0), byte(vm.STOP)}, Nonce: 1},
		params.HistoryStorageAddress: {Code: params.HistoryStorageCode, Nonce: 1},
	}}
	genesis := gspec.MustCommit(db, tdb)
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
	header := &types.Header{ParentHash: parent.Hash(), Number: big.NewInt(2), Time: 5, Difficulty: diffInTurn, Coinbase: testAddr, GasLimit: 30_000_000, BaseFee: common.Big0}
	header.ExcessBlobGas, header.BlobGasUsed = new(uint64), new(uint64)
	data, err := engine.validatorSetABI.Pack("deposit", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	msg := engine.getSystemMessage(testAddr, validatorContract, data, big.NewInt(100))
	tx := types.MustSignNewTx(testKey, engine.signer, &types.LegacyTx{To: &validatorContract, Gas: msg.GasLimit, GasPrice: common.Big0, Value: big.NewInt(100), Data: data})
	userTx := types.MustSignNewTx(userKey, engine.signer, &types.LegacyTx{To: &recipient, Gas: 30_000, GasPrice: common.Big0, Value: common.Big1})
	body := types.Body{Transactions: []*types.Transaction{userTx, tx}}
	block := types.NewBlockWithHeader(header).WithBody(body)
	sdb, err := state.New(parent.Root, state.NewDatabase(tdb, nil))
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), block, sdb, nil, nil, vm.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Receipts) != 2 || res.Receipts[1].TransactionIndex != 1 || res.Receipts[1].TxHash != tx.Hash() || res.GasUsed == 0 {
		t.Fatalf("unexpected system transaction result: %+v", res)
	}
	want := bal.NewConstructionBlockAccessList()
	want.CodeChange(vm.CAS20ActivationRegistryAddress, 0, vm.CAS20MarkerCode)
	want.CodeChange(vm.CAS20PolicyRegistryAddress, 0, vm.CAS20MarkerCode)
	want.StorageWrite(0, params.HistoryStorageAddress, common.Hash{31: 1}, parent.Hash())
	want.BalanceChange(1, user, uint256.NewInt(1))
	want.NonceChange(user, 1, 1)
	want.BalanceChange(1, recipient, uint256.NewInt(2))
	want.BalanceChange(2, consensus.SystemAddress, common.U2560)
	want.NonceChange(testAddr, 2, 1)
	want.BalanceChange(2, validatorContract, uint256.NewInt(100))
	want.StorageWrite(2, validatorContract, common.Hash{}, common.Hash{31: 1})
	list := want.ToEncodingObj()
	replayed, _ := state.New(parent.Root, state.NewDatabase(tdb, nil))
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
	if _, _, err := core.AssembleBlock(engine, chain, types.CopyHeader(header), sdb, &types.Body{}, nil); err == nil {
		t.Fatal("Amsterdam assembly accepted a missing BAL")
	}
	t.Run("parallel", func(t *testing.T) {
		statedb, _ := state.New(parent.Root, state.NewDatabase(tdb, nil))
		block := block.WithAccessList(list)
		result, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), block, statedb, nil, nil, vm.Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := core.NewBlockValidator(&config, chain).ValidateState(block, statedb, result, false); err != nil {
			t.Fatalf("parallel execution differs from serial BAL or state: %v", err)
		}
		if len(result.Logs) != len(res.Logs) || result.Receipts[1].TransactionIndex != 1 {
			t.Fatal("parallel logs or receipt count/index differs from serial execution")
		}
		for i, lg := range result.Logs {
			if lg.Index != uint(i) || lg.TxIndex != res.Logs[i].TxIndex {
				t.Fatalf("log %d: unexpected indices %d/%d", i, lg.Index, lg.TxIndex)
			}
		}
		invalid := types.MustSignNewTx(testKey, engine.signer, &types.LegacyTx{To: &validatorContract, Gas: msg.GasLimit, GasPrice: common.Big0, Value: big.NewInt(99), Data: data})
		block = block.WithBody(types.Body{Transactions: []*types.Transaction{userTx, invalid}})
		statedb, _ = state.New(parent.Root, state.NewDatabase(tdb, nil))
		if _, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), block, statedb, nil, nil, vm.Config{}, nil); err == nil || !strings.Contains(err.Error(), "expected tx hash") {
			t.Fatalf("parallel execution bypassed Parlia system transaction validation: %v", err)
		}
	})
	t.Run("parallel-notries", func(t *testing.T) {
		tdbConfig := *triedb.HashDefaults
		tdbConfig.NoTries = true
		fastTries := triedb.NewDatabase(db, &tdbConfig)
		defer fastTries.Close()
		fastDB := state.NewMPTDatabase(fastTries, nil).WithSnapshot(chain.Snapshots())
		statedb, err := state.New(parent.Root, fastDB)
		if err != nil {
			t.Fatal(err)
		}
		statedb.SetExpectedStateRoot(header.Root)
		block := block.WithAccessList(list)
		result, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), block, statedb, nil, nil, vm.Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := core.NewBlockValidator(&config, chain).ValidateState(block, statedb, result, false); err != nil {
			t.Fatal(err)
		}
		root, err := statedb.Commit(config.Rules(header.Number, false, header.Time), header.Number.Uint64())
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := state.New(root, fastDB)
		if err != nil {
			t.Fatal(err)
		}
		if root != header.Root || reopened.GetBalance(recipient).Uint64() != 2 || reopened.GetState(validatorContract, common.Hash{}) != (common.Hash{31: 1}) || reopened.GetNonce(testAddr) != 1 {
			t.Fatal("NoTries snapshot lost ordinary or system transaction changes")
		}
	})
	engine.Authorize(testAddr, nil, func(_ accounts.Account, tx *types.Transaction, _ *big.Int) (*types.Transaction, error) {
		return types.SignTx(tx, engine.signer, testKey)
	})
	for _, mode := range []string{"mining", "bid"} {
		t.Run(mode, func(t *testing.T) {
			miningState, _ := state.New(parent.Root, state.NewDatabase(tdb, nil))
			miningHeader := types.CopyHeader(header)
			miningHeader.GasUsed, miningHeader.BlockAccessListHash = 0, nil
			evm := vm.NewEVM(core.NewEVMBlockContext(miningHeader, chain, nil), miningState, &config, vm.Config{})
			defer evm.Release()
			blockBAL := core.PreExecution(context.Background(), nil, parent, &config, evm, header.Number, header.Time)
			miningState.SetTxContext(userTx.Hash(), 0, 1)
			gp := core.NewGasPool(header.GasLimit)
			receipt, txBAL, err := core.ApplyTransaction(context.Background(), evm, gp, miningState, miningHeader, userTx)
			if err != nil {
				t.Fatal(err)
			}
			blockBAL.Merge(txBAL)
			miningHeader.GasUsed = gp.Used()
			body := &types.Body{Transactions: []*types.Transaction{userTx}}
			var packed *types.Block
			if mode == "mining" {
				packed, _, err = core.AssembleBlock(engine, chain, miningHeader, miningState, body, []*types.Receipt{receipt}, blockBAL)
			} else {
				packed, _, err = engine.FinalizeAndAssembleBidBlock(chain, miningHeader, miningState, body, []*types.Receipt{receipt}, nil, blockBAL)
			}
			if err != nil {
				t.Fatal(err)
			}
			if miningState.TxIndex() != len(packed.Transactions()) {
				t.Fatal("post-execution did not follow the packed system transactions")
			}
			if packed.Root() != header.Root {
				t.Fatal("assembled system transactions differ from imported post-state")
			}
			if packed.AccessList() == nil || packed.AccessList().Hash() != hash || packed.Header().BlockAccessListHash == nil || *packed.Header().BlockAccessListHash != hash {
				t.Fatal("assembled block does not carry the complete BAL and hash")
			}
		})
	}
	t.Run("chain-maker", func(t *testing.T) {
		blocks, _ := core.GenerateChain(&config, types.NewBlockWithHeader(parent), engine, db, 1, func(_ int, b *core.BlockGen) {
			b.SetCoinbase(testAddr)
			b.AddTx(userTx)
		})
		statedb, _ := state.New(parent.Root, state.NewDatabase(tdb, nil))
		result, err := core.NewStateProcessor(chain.HeaderChain()).Process(context.Background(), blocks[0], statedb, nil, nil, vm.Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := core.NewBlockValidator(&config, chain).ValidateState(blocks[0], statedb, result, false); err != nil {
			t.Fatalf("generated block differs from imported BAL or state: %v", err)
		}
	})
}
