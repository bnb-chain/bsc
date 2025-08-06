// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethclient_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb"
)

// Verify that Client implements the ethereum interfaces.
var (
	_ = ethereum.ChainReader(&ethclient.Client{})
	_ = ethereum.TransactionReader(&ethclient.Client{})
	_ = ethereum.ChainStateReader(&ethclient.Client{})
	_ = ethereum.ChainSyncReader(&ethclient.Client{})
	_ = ethereum.ContractCaller(&ethclient.Client{})
	_ = ethereum.GasEstimator(&ethclient.Client{})
	_ = ethereum.GasPricer(&ethclient.Client{})
	_ = ethereum.LogFilterer(&ethclient.Client{})
	_ = ethereum.PendingStateReader(&ethclient.Client{})
	// _ = ethereum.PendingStateEventer(&ethclient.Client{})
	_ = ethereum.PendingContractCaller(&ethclient.Client{})
)

var (
	testKey, _         = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr           = crypto.PubkeyToAddress(testKey.PublicKey)
	testBalance        = big.NewInt(2e18)
	revertContractAddr = common.HexToAddress("290f1b36649a61e369c6276f6d29463335b4400c")
	revertCode         = common.FromHex("7f08c379a0000000000000000000000000000000000000000000000000000000006000526020600452600a6024527f75736572206572726f7200000000000000000000000000000000000000000000604452604e6000fd")
	testGasPrice       = big.NewInt(3e9) // 3Gwei
	testBlockNum       = 128
	testBlocks         = []testBlockParam{
		{
			blockNr: 1,
			txs: []testTransactionParam{
				{
					to:       common.Address{0x10},
					value:    big.NewInt(0),
					gasPrice: testGasPrice,
					data:     nil,
				},
				{
					to:       common.Address{0x11},
					value:    big.NewInt(0),
					gasPrice: testGasPrice,
					data:     nil,
				},
			},
		},
		{
			// This txs params also used to default block.
			blockNr: 10,
			txs:     []testTransactionParam{},
		},
		{
			blockNr: 11,
			txs: []testTransactionParam{
				{
					to:       common.Address{0x01},
					value:    big.NewInt(1),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
			},
		},
		{
			blockNr: 12,
			txs: []testTransactionParam{
				{
					to:       common.Address{0x01},
					value:    big.NewInt(1),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
				{
					to:       common.Address{0x02},
					value:    big.NewInt(2),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
			},
		},
		{
			blockNr: 13,
			txs: []testTransactionParam{
				{
					to:       common.Address{0x01},
					value:    big.NewInt(1),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
				{
					to:       common.Address{0x02},
					value:    big.NewInt(2),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
				{
					to:       common.Address{0x03},
					value:    big.NewInt(3),
					gasPrice: big.NewInt(params.InitialBaseFee),
					data:     nil,
				},
			},
		},
	}
)

var genesis = &core.Genesis{
	Config: params.AllEthashProtocolChanges, // AllDevChainProtocolChanges,
	Alloc: types.GenesisAlloc{
		testAddr:           {Balance: testBalance},
		revertContractAddr: {Code: revertCode},
	},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFeeForBSC),
}

var testTx1 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    254,
	Value:    big.NewInt(12),
	GasPrice: testGasPrice,
	Gas:      params.TxGas,
	To:       &common.Address{2},
})

var testTx2 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    255,
	Value:    big.NewInt(8),
	GasPrice: testGasPrice,
	Gas:      params.TxGas,
	To:       &common.Address{2},
})

type testTransactionParam struct {
	to       common.Address
	value    *big.Int
	gasPrice *big.Int
	data     []byte
}

type testBlockParam struct {
	blockNr int
	txs     []testTransactionParam
}

func newTestBackend(config *node.Config) (*node.Node, []*types.Block, error) {
	// Generate test chain.
	blocks := generateTestChain()

	// Create node
	if config == nil {
		config = new(node.Config)
	}
	n, err := node.New(config)
	if err != nil {
		return nil, nil, fmt.Errorf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	ecfg := &ethconfig.Config{Genesis: genesis, RPCGasCap: 1000000}
	ecfg.SnapshotCache = 256
	ecfg.TriesInMemory = 128
	ethservice, err := eth.New(n, ecfg)
	if err != nil {
		return nil, nil, fmt.Errorf("can't create new ethereum service: %v", err)
	}
	// Import the test chain.
	if err := n.Start(); err != nil {
		return nil, nil, fmt.Errorf("can't start test node: %v", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks[1:]); err != nil {
		return nil, nil, fmt.Errorf("can't import test blocks: %v", err)
	}
	// Ensure the tx indexing is fully generated
	for ; ; time.Sleep(time.Millisecond * 100) {
		progress, err := ethservice.BlockChain().TxIndexProgress()
		if err == nil && progress.Done() {
			break
		}
	}
	return n, blocks, nil
}

func generateTestChain() []*types.Block {
	signer := types.HomesteadSigner{}
	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	genesis.MustCommit(db, triedb.NewDatabase(db, nil))
	chain, _ := core.NewBlockChain(db, genesis, ethash.NewFaker(), nil)
	generate := func(i int, block *core.BlockGen) {
		block.OffsetTime(5)
		block.SetExtra([]byte("test"))
		//block.SetCoinbase(testAddr)

		for idx, testBlock := range testBlocks {
			// Specific block setting, the index in this generator has 1 diff from specified blockNr.
			if i+1 == testBlock.blockNr {
				for _, testTransaction := range testBlock.txs {
					tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), testTransaction.to,
						testTransaction.value, params.TxGas, testTransaction.gasPrice, testTransaction.data), signer, testKey)
					if err != nil {
						panic(err)
					}
					block.AddTxWithChain(chain, tx)
				}
				break
			}

			// Default block setting.
			if idx == len(testBlocks)-1 {
				// We want to simulate an empty middle block, having the same state as the
				// first one. The last is needs a state change again to force a reorg.
				for _, testTransaction := range testBlocks[0].txs {
					tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), testTransaction.to,
						testTransaction.value, params.TxGas, testTransaction.gasPrice, testTransaction.data), signer, testKey)
					if err != nil {
						panic(err)
					}
					block.AddTxWithChain(chain, tx)
				}
			}
		}
		// for testTransactionInBlock
		if i+1 == testBlockNum {
			block.AddTxWithChain(chain, testTx1)
			block.AddTxWithChain(chain, testTx2)
		}
	}
	gblock := genesis.MustCommit(db, triedb.NewDatabase(db, nil))
	engine := ethash.NewFaker()
	blocks, _ := core.GenerateChain(genesis.Config, gblock, engine, db, testBlockNum, generate)
	blocks = append([]*types.Block{gblock}, blocks...)
	return blocks
}

func TestEthClient(t *testing.T) {
	backend, chain, err := newTestBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := backend.Attach()
	defer backend.Close()
	defer client.Close()

	tests := map[string]struct {
		test func(t *testing.T)
	}{
		"Header": {
			func(t *testing.T) { testHeader(t, chain, client) },
		},
		"BalanceAt": {
			func(t *testing.T) { testBalanceAt(t, client) },
		},
		"TxInBlockInterrupted": {
			func(t *testing.T) { testTransactionInBlock(t, client) },
		},
		"ChainID": {
			func(t *testing.T) { testChainID(t, client) },
		},
		"GetBlock": {
			func(t *testing.T) { testGetBlock(t, client) },
		},
		"StatusFunctions": {
			func(t *testing.T) { testStatusFunctions(t, client) },
		},
		"CallContract": {
			func(t *testing.T) { testCallContract(t, client) },
		},
		"CallContractAtHash": {
			func(t *testing.T) { testCallContractAtHash(t, client) },
		},
		// DO not have TestAtFunctions now, because we do not have pending block now
		// "AtFunctions": {
		// 	func(t *testing.T) { testAtFunctions(t, client) },
		// },
		// TODO(Nathan): why skip this case?
		// "TransactionSender": {
		// 	func(t *testing.T) { testTransactionSender(t, client) },
		// },
		"TestSendTransactionConditional": {
			func(t *testing.T) { testSendTransactionConditional(t, client) },
		},
	}

	t.Parallel()
	for name, tt := range tests {
		t.Run(name, tt.test)
	}
}

func testHeader(t *testing.T, chain []*types.Block, client *rpc.Client) {
	tests := map[string]struct {
		block   *big.Int
		want    *types.Header
		wantErr error
	}{
		"genesis": {
			block: big.NewInt(0),
			want:  chain[0].Header(),
		},
		"first_block": {
			block: big.NewInt(1),
			want:  chain[1].Header(),
		},
		"future_block": {
			block:   big.NewInt(1000000000),
			want:    nil,
			wantErr: ethereum.NotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ec := ethclient.NewClient(client)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			got, err := ec.HeaderByNumber(ctx, tt.block)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HeaderByNumber(%v) error = %q, want %q", tt.block, err, tt.wantErr)
			}
			if got.Hash() != tt.want.Hash() {
				t.Fatalf("HeaderByNumber(%v) got = %v, want %v", tt.block, got, tt.want)
			}
		})
	}
}

func testBalanceAt(t *testing.T, client *rpc.Client) {
	tests := map[string]struct {
		account common.Address
		block   *big.Int
		want    *big.Int
		wantErr error
	}{
		"valid_account_genesis": {
			account: testAddr,
			block:   big.NewInt(0),
			want:    testBalance,
		},
		"valid_account": {
			account: testAddr,
			block:   big.NewInt(1),
			want:    big.NewInt(0).Sub(testBalance, big.NewInt(0).Mul(big.NewInt(2*21000), testGasPrice)),
		},
		"non_existent_account": {
			account: common.Address{1},
			block:   big.NewInt(1),
			want:    big.NewInt(0),
		},
		"future_block": {
			account: testAddr,
			block:   big.NewInt(1000000000),
			want:    big.NewInt(0),
			wantErr: errors.New("header not found"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ec := ethclient.NewClient(client)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			got, err := ec.BalanceAt(ctx, tt.account, tt.block)
			if tt.wantErr != nil && (err == nil || err.Error() != tt.wantErr.Error()) {
				t.Fatalf("BalanceAt(%x, %v) error = %q, want %q", tt.account, tt.block, err, tt.wantErr)
			}
			if got.Cmp(tt.want) != 0 {
				t.Fatalf("BalanceAt(%x, %v) = %v, want %v", tt.account, tt.block, got, tt.want)
			}
		})
	}
}

func testTransactionInBlock(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	// Get current block by number.
	block, err := ec.BlockByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test tx in block not found.
	if _, err := ec.TransactionInBlock(context.Background(), block.Hash(), 20); err != ethereum.NotFound {
		t.Fatal("error should be ethereum.NotFound")
	}

	// Test tx in block found.
	tx, err := ec.TransactionInBlock(context.Background(), block.Hash(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Hash() != testTx1.Hash() {
		t.Fatalf("unexpected transaction: %v", tx)
	}

	tx, err = ec.TransactionInBlock(context.Background(), block.Hash(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Hash() != testTx2.Hash() {
		t.Fatalf("unexpected transaction: %v", tx)
	}

	// Test pending block
	_, err = ec.BlockByNumber(context.Background(), big.NewInt(int64(rpc.PendingBlockNumber)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testChainID(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)
	id, err := ec.ChainID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil || id.Cmp(params.AllDevChainProtocolChanges.ChainID) != 0 {
		t.Fatalf("ChainID returned wrong number: %+v", id)
	}
}

func testGetBlock(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	// Get current block number
	blockNumber, err := ec.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blockNumber != uint64(testBlockNum) {
		t.Fatalf("BlockNumber returned wrong number: %d", blockNumber)
	}
	// Get current block by number
	block, err := ec.BlockByNumber(context.Background(), new(big.Int).SetUint64(blockNumber))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.NumberU64() != blockNumber {
		t.Fatalf("BlockByNumber returned wrong block: want %d got %d", blockNumber, block.NumberU64())
	}
	// Get current block by hash
	blockH, err := ec.BlockByHash(context.Background(), block.Hash())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Hash() != blockH.Hash() {
		t.Fatalf("BlockByHash returned wrong block: want %v got %v", block.Hash().Hex(), blockH.Hash().Hex())
	}
	// Get header by number
	header, err := ec.HeaderByNumber(context.Background(), new(big.Int).SetUint64(blockNumber))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Header().Hash() != header.Hash() {
		t.Fatalf("HeaderByNumber returned wrong header: want %v got %v", block.Header().Hash().Hex(), header.Hash().Hex())
	}
	// Get header by hash
	headerH, err := ec.HeaderByHash(context.Background(), block.Hash())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Header().Hash() != headerH.Hash() {
		t.Fatalf("HeaderByHash returned wrong header: want %v got %v", block.Header().Hash().Hex(), headerH.Hash().Hex())
	}
}

func testStatusFunctions(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	// Sync progress
	progress, err := ec.SyncProgress(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if progress != nil {
		t.Fatalf("unexpected progress: %v", progress)
	}

	// NetworkID
	networkID, err := ec.NetworkID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if networkID.Cmp(big.NewInt(1337)) != 0 {
		t.Fatalf("unexpected networkID: %v", networkID)
	}

	// SuggestGasPrice
	gasPrice, err := ec.SuggestGasPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasPrice.Cmp(testGasPrice) != 0 {
		t.Fatalf("unexpected gas price: %v", gasPrice)
	}

	// SuggestGasTipCap
	gasTipCap, err := ec.SuggestGasTipCap(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasTipCap.Cmp(testGasPrice) != 0 {
		t.Fatalf("unexpected gas tip cap: %v", gasTipCap)
	}

	// // BlobBaseFee
	// blobBaseFee, err := ec.BlobBaseFee(context.Background())
	// if err != nil {
	// 	t.Fatalf("unexpected error: %v", err)
	// }
	// if blobBaseFee.Cmp(big.NewInt(1)) != 0 {
	// 	t.Fatalf("unexpected blob base fee: %v", blobBaseFee)
	// }

	// FeeHistory
	history, err := ec.FeeHistory(context.Background(), 1, big.NewInt(2), []float64{95, 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &ethereum.FeeHistory{
		OldestBlock: big.NewInt(2),
		Reward: [][]*big.Int{
			{
				testGasPrice,
				testGasPrice,
			},
		},
		BaseFee: []*big.Int{
			big.NewInt(params.InitialBaseFeeForBSC),
			big.NewInt(params.InitialBaseFeeForBSC),
		},
		GasUsedRatio: []float64{0.008912678667376286},
	}
	if !reflect.DeepEqual(history, want) {
		t.Fatalf("FeeHistory result doesn't match expected: (got: %v, want: %v)", history, want)
	}
}

func testCallContractAtHash(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	// EstimateGas
	msg := ethereum.CallMsg{
		From:  testAddr,
		To:    &common.Address{},
		Gas:   21000,
		Value: big.NewInt(1),
	}
	gas, err := ec.EstimateGas(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas price: %v", gas)
	}
	block, err := ec.HeaderByNumber(context.Background(), big.NewInt(1))
	if err != nil {
		t.Fatalf("BlockByNumber error: %v", err)
	}
	// CallContract
	if _, err := ec.CallContractAtHash(context.Background(), msg, block.Hash()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testCallContract(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	// EstimateGas
	msg := ethereum.CallMsg{
		From:  testAddr,
		To:    &common.Address{},
		Gas:   21000,
		Value: big.NewInt(1),
	}
	gas, err := ec.EstimateGas(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas price: %v", gas)
	}
	// CallContract
	if _, err := ec.CallContract(context.Background(), msg, big.NewInt(1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PendingCallContract
	if _, err := ec.PendingCallContract(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testSendTransactionConditional(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)

	if err := sendTransactionConditional(ec); err != nil {
		t.Fatalf("error: %v", err)
	}
	// Use HeaderByNumber to get a header for EstimateGasAtBlock and EstimateGasAtBlockHash
	latestHeader, err := ec.HeaderByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// EstimateGasAtBlock
	msg := ethereum.CallMsg{
		From:  testAddr,
		To:    &common.Address{},
		Gas:   21000,
		Value: big.NewInt(1),
	}
	gas, err := ec.EstimateGasAtBlock(context.Background(), msg, latestHeader.Number)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas limit: %v", gas)
	}
	// EstimateGasAtBlockHash
	gas, err = ec.EstimateGasAtBlockHash(context.Background(), msg, latestHeader.Hash())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas limit: %v", gas)
	}
}

//nolint:unused
func testTransactionSender(t *testing.T, client *rpc.Client) {
	ec := ethclient.NewClient(client)
	ctx := context.Background()

	// Retrieve testTx1 via RPC.
	block2, err := ec.HeaderByNumber(ctx, big.NewInt(2))
	if err != nil {
		t.Fatal("can't get block 1:", err)
	}
	tx1, err := ec.TransactionInBlock(ctx, block2.Hash(), 0)
	if err != nil {
		t.Fatal("can't get tx:", err)
	}
	if tx1.Hash() != testTx1.Hash() {
		t.Fatalf("wrong tx hash %v, want %v", tx1.Hash(), testTx1.Hash())
	}

	// The sender address is cached in tx1, so no additional RPC should be required in
	// TransactionSender. Ensure the server is not asked by canceling the context here.
	sender1, err := ec.TransactionSender(newCanceledContext(), tx1, block2.Hash(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if sender1 != testAddr {
		t.Fatal("wrong sender:", sender1)
	}

	// Now try to get the sender of testTx2, which was not fetched through RPC.
	// TransactionSender should query the server here.
	sender2, err := ec.TransactionSender(ctx, testTx2, block2.Hash(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sender2 != testAddr {
		t.Fatal("wrong sender:", sender2)
	}
}

//nolint:unused
func newCanceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-ctx.Done() // Ensure the close of the Done channel
	return ctx
}

func sendTransactionConditional(ec *ethclient.Client) error {
	chainID, err := ec.ChainID(context.Background())
	if err != nil {
		return err
	}

	nonce, err := ec.PendingNonceAt(context.Background(), testAddr)
	if err != nil {
		return err
	}

	signer := types.LatestSignerForChainID(chainID)

	tx, err := types.SignNewTx(testKey, signer, &types.LegacyTx{
		Nonce:    nonce,
		To:       &common.Address{2},
		Value:    big.NewInt(1),
		Gas:      22000,
		GasPrice: big.NewInt(params.InitialBaseFee),
	})
	if err != nil {
		return err
	}

	root := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	return ec.SendTransactionConditional(context.Background(), tx, types.TransactionOpts{
		KnownAccounts: map[common.Address]types.AccountStorage{
			testAddr: types.AccountStorage{
				StorageRoot: &root,
			},
		},
	})
}

// Here we show how to get the error message of reverted contract call.
func ExampleRevertErrorData() {
	// First create an ethclient.Client instance.
	ctx := context.Background()
	ec, _ := ethclient.DialContext(ctx, exampleNode.HTTPEndpoint())

	// Call the contract.
	// Note we expect the call to return an error.
	contract := common.HexToAddress("290f1b36649a61e369c6276f6d29463335b4400c")
	call := ethereum.CallMsg{To: &contract, Gas: 30000}
	result, err := ec.CallContract(ctx, call, nil)
	if len(result) > 0 {
		panic("got result")
	}
	if err == nil {
		panic("call did not return error")
	}

	// Extract the low-level revert data from the error.
	revertData, ok := ethclient.RevertErrorData(err)
	if !ok {
		panic("unpacking revert failed")
	}
	fmt.Printf("revert: %x\n", revertData)

	// Parse the revert data to obtain the error message.
	message, err := abi.UnpackRevert(revertData)
	if err != nil {
		panic("parsing ABI error failed: " + err.Error())
	}
	fmt.Println("message:", message)

	// Output:
	// revert: 08c379a00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000a75736572206572726f72
	// message: user error
}
