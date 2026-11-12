package parlia

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
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
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	triedb := triedb.NewDatabase(db, nil)
	defer triedb.Close()
	gspec := &core.Genesis{Config: &config, GasLimit: 30_000_000, Alloc: types.GenesisAlloc{
		consensus.SystemAddress:      {Balance: big.NewInt(100)},
		validatorContract:            {Code: []byte{byte(vm.PUSH1), 1, byte(vm.PUSH1), 0, byte(vm.SSTORE), byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.LOG0), byte(vm.STOP)}, Nonce: 1},
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
	header := &types.Header{ParentHash: parent.Hash(), Number: big.NewInt(2), Time: 3, Difficulty: diffInTurn, Coinbase: testAddr, GasLimit: 30_000_000}
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
	// Compare gas and receipts with Parlia's existing direct finalization path.
	rules := config.Rules(header.Number, false, header.Time)
	reference, _ := state.New(parent.Root, state.NewDatabase(triedb, nil))
	reference.SetState(params.HistoryStorageAddress, common.Hash{31: 1}, parent.Hash())
	reference.Finalise(rules)
	var referenceReceipts []*types.Receipt
	var referenceGas uint64
	received := []*types.Transaction{tx}
	if err := engine.Finalize(chain, header, reference, &types.Body{}, &referenceReceipts, &received, &referenceGas, nil); err != nil {
		t.Fatal(err)
	}
	if res.GasUsed != referenceGas || types.DeriveSha(res.Receipts, trie.NewStackTrie(nil)) != types.DeriveSha(types.Receipts(referenceReceipts), trie.NewStackTrie(nil)) {
		t.Fatal("BAL collection changed Parlia gas or receipts")
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
	header.Root = replayed.IntermediateRoot(rules)
	if header.Root != reference.IntermediateRoot(rules) {
		t.Fatal("BAL collection changed Parlia post-state")
	}
	header.GasUsed = res.GasUsed
	header.ReceiptHash = types.DeriveSha(res.Receipts, trie.NewStackTrie(nil))
	header.Bloom = types.MergeBloom(res.Receipts)
	hash := list.Hash()
	header.BlockAccessListHash = &hash
	block = types.NewBlockWithHeader(header).WithBody(body)
	if err := core.NewBlockValidator(&config, chain).ValidateState(block, sdb, res, false); err != nil {
		t.Fatalf("Parlia execution differs from the expected BAL and post-state: %v", err)
	}
}
