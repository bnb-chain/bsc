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

package ethclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

// Verify that Client implements the ethereum interfaces.
var (
	_ = ethereum.ChainReader(&Client{})
	_ = ethereum.TransactionReader(&Client{})
	_ = ethereum.ChainStateReader(&Client{})
	_ = ethereum.ChainSyncReader(&Client{})
	_ = ethereum.ContractCaller(&Client{})
	_ = ethereum.GasEstimator(&Client{})
	_ = ethereum.GasPricer(&Client{})
	_ = ethereum.LogFilterer(&Client{})
	_ = ethereum.PendingStateReader(&Client{})
	// _ = ethereum.PendingStateEventer(&Client{})
	_ = ethereum.PendingContractCaller(&Client{})
)

func TestToFilterArg(t *testing.T) {
	blockHashErr := fmt.Errorf("cannot specify both BlockHash and FromBlock/ToBlock")
	addresses := []common.Address{
		common.HexToAddress("0xD36722ADeC3EdCB29c8e7b5a47f352D701393462"),
	}
	blockHash := common.HexToHash(
		"0xeb94bb7d78b73657a9d7a99792413f50c0a45c51fc62bdcb08a53f18e9a2b4eb",
	)

	for _, testCase := range []struct {
		name   string
		input  ethereum.FilterQuery
		output interface{}
		err    error
	}{
		{
			"without BlockHash",
			ethereum.FilterQuery{
				Addresses: addresses,
				FromBlock: big.NewInt(1),
				ToBlock:   big.NewInt(2),
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "0x1",
				"toBlock":   "0x2",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with nil fromBlock and nil toBlock",
			ethereum.FilterQuery{
				Addresses: addresses,
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "0x0",
				"toBlock":   "latest",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with negative fromBlock and negative toBlock",
			ethereum.FilterQuery{
				Addresses: addresses,
				FromBlock: big.NewInt(-1),
				ToBlock:   big.NewInt(-1),
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "pending",
				"toBlock":   "pending",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with blockhash",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"blockHash": blockHash,
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with blockhash and from block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				FromBlock: big.NewInt(1),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
		{
			"with blockhash and to block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				ToBlock:   big.NewInt(1),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
		{
			"with blockhash and both from / to block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				FromBlock: big.NewInt(1),
				ToBlock:   big.NewInt(2),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := toFilterArg(testCase.input)
			if (testCase.err == nil) != (err == nil) {
				t.Fatalf("expected error %v but got %v", testCase.err, err)
			}
			if testCase.err != nil {
				if testCase.err.Error() != err.Error() {
					t.Fatalf("expected error %v but got %v", testCase.err, err)
				}
			} else if !reflect.DeepEqual(testCase.output, output) {
				t.Fatalf("expected filter arg %v but got %v", testCase.output, output)
			}
		})
	}
}

var (
	testKey, _   = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr     = crypto.PubkeyToAddress(testKey.PublicKey)
	testBalance  = big.NewInt(2e15)
	testBlockNum = 128
	testBlocks   = []testBlockParam{
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
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
}

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

func newTestBackend(t *testing.T) (*node.Node, []*types.Block) {
	// Generate test chain.
	blocks := generateTestChain()

	// Create node
	n, err := node.New(&node.Config{})
	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis}
	config.Ethash.PowMode = ethash.ModeFake
	config.SnapshotCache = 256
	config.TriesInMemory = 128
	ethservice, err := eth.New(n, config)
	if err != nil {
		t.Fatalf("can't create new ethereum service: %v", err)
	}
	// Import the test chain.
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks[1:]); err != nil {
		t.Fatalf("can't import test blocks: %v", err)
	}
	return n, blocks
}

func generateTestChain() []*types.Block {
	signer := types.HomesteadSigner{}
	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	db.SetDiffStore(memorydb.New())
	genesis.MustCommit(db)
	chain, _ := core.NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFaker(), vm.Config{}, nil, nil, core.EnablePersistDiff(860000))
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
	}
	gblock := genesis.ToBlock(db)
	engine := ethash.NewFaker()
	blocks, _ := core.GenerateChain(genesis.Config, gblock, engine, db, testBlockNum, generate)
	blocks = append([]*types.Block{gblock}, blocks...)
	return blocks
}

func TestEthClient(t *testing.T) {
	backend, chain := newTestBackend(t)
	client, _ := backend.Attach()
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
			func(t *testing.T) { testTransactionInBlockInterrupted(t, client) },
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
		"TestDiffAccounts": {
			func(t *testing.T) { testDiffAccounts(t, client) },
		},
		"TestSendTransactionConditional": {
			func(t *testing.T) { testSendTransactionConditional(t, client) },
		},
		// DO not have TestAtFunctions now, because we do not have pending block now
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
			ec := NewClient(client)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			got, err := ec.HeaderByNumber(ctx, tt.block)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HeaderByNumber(%v) error = %q, want %q", tt.block, err, tt.wantErr)
			}

			gotBytes, err := rlp.EncodeToBytes(got)
			if err != nil {
				t.Fatalf("Error serializing received block header.")
			}
			wantBytes, err := rlp.EncodeToBytes(tt.want)
			if err != nil {
				t.Fatalf("Error serializing wanted block header.")
			}

			// Instead of comparing the Header's compare the serialized bytes,
			// because reflect.DeepEqual(*types.Header, *types.Header) sometimes
			// returns false even though the underlying field values are exactly the same.
			if !reflect.DeepEqual(gotBytes, wantBytes) {
				t.Fatalf("HeaderByNumber(%v)\n   = %v\nwant %v", tt.block, got, tt.want)
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
			want:    testBalance,
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
			ec := NewClient(client)
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

func testTransactionInBlockInterrupted(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)

	// Get current block by number.
	block, err := ec.BlockByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test tx in block interupted.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx, err := ec.TransactionInBlock(ctx, block.Hash(), 0)
	if tx != nil {
		t.Fatal("transaction should be nil")
	}
	if err == nil || err == ethereum.NotFound {
		t.Fatal("error should not be nil/notfound")
	}

	// Test tx in block not found.
	if _, err := ec.TransactionInBlock(context.Background(), block.Hash(), 20); err != ethereum.NotFound {
		t.Fatal("error should be ethereum.NotFound")
	}
}

func testChainID(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)
	id, err := ec.ChainID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil || id.Cmp(params.AllEthashProtocolChanges.ChainID) != 0 {
		t.Fatalf("ChainID returned wrong number: %+v", id)
	}
}

func testGetBlock(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)

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
	ec := NewClient(client)

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
	if networkID.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("unexpected networkID: %v", networkID)
	}

	// SuggestGasPrice
	gasPrice, err := ec.SuggestGasPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasPrice.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("unexpected gas price: %v", gasPrice)
	}

	// SuggestGasTipCap
	gasTipCap, err := ec.SuggestGasTipCap(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasTipCap.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("unexpected gas tip cap: %v", gasTipCap)
	}
}

func testCallContract(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)

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
	// PendingCallCOntract
	if _, err := ec.PendingCallContract(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testDiffAccounts(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()

	for _, testBlock := range testBlocks {
		if testBlock.blockNr == 10 {
			continue
		}
		diffAccounts, err := ec.GetDiffAccounts(ctx, big.NewInt(int64(testBlock.blockNr)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		accounts := make([]common.Address, 0)
		for _, tx := range testBlock.txs {
			// tx.to should be in the accounts list.
			for idx, account := range diffAccounts {
				if tx.to == account {
					break
				}

				if idx == len(diffAccounts)-1 {
					t.Fatalf("address(%v) expected in the diff account list, but not", tx.to)
				}
			}

			accounts = append(accounts, tx.to)
		}

		diffDetail, err := ec.GetDiffAccountsWithScope(ctx, big.NewInt(int64(testBlock.blockNr)), accounts)
		if err != nil {
			t.Fatalf("get diff accounts in block error: %v", err)
		}
		// No contract deposit tx, so expect empty transactions.
		if len(diffDetail.Transactions) != 0 {
			t.Fatalf("expect ignore all transactions, but some transaction has recorded")
		}
	}
}

func testSendTransactionConditional(t *testing.T, client *rpc.Client) {
	ec := NewClient(client)

	if err := sendTransactionConditional(ec); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func sendTransactionConditional(ec *Client) error {
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
	return ec.SendTransactionConditional(context.Background(), tx, ethapi.TransactionOpts{
		KnownAccounts: map[common.Address]ethapi.AccountStorage{
			testAddr: ethapi.AccountStorage{
				StorageRoot: &root,
			},
		},
	})
}
