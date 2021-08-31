// Copyright 2020 The go-ethereum Authors
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

// Tests that abnormal program termination (i.e.crash) and restart doesn't leave
// the database in some strange state with gaps in the chain, nor with block data
// dangling in the future.

package core

import (
	"math/big"
	"testing"
	"time"

	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

// testBackend is a mock implementation of the live Ethereum message handler. Its
// purpose is to allow testing the request/reply workflows and wire serialization
// in the `eth` protocol without actually doing any data processing.
type testBackend struct {
	db    ethdb.Database
	chain *BlockChain
}

// newTestBackend creates an empty chain and wraps it into a mock backend.
func newTestBackend(blocks int, light bool) *testBackend {
	return newTestBackendWithGenerator(blocks, light)
}

// newTestBackend creates a chain with a number of explicitly defined blocks and
// wraps it into a mock backend.
func newTestBackendWithGenerator(blocks int, lightProcess bool) *testBackend {
	signer := types.HomesteadSigner{}
	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	db.SetDiffStore(memorydb.New())
	(&Genesis{
		Config: params.TestChainConfig,
		Alloc:  GenesisAlloc{testAddr: {Balance: big.NewInt(100000000000000000)}},
	}).MustCommit(db)

	chain, _ := NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFaker(), vm.Config{}, nil, nil)
	generator := func(i int, block *BlockGen) {
		// The chain maker doesn't have access to a chain, so the difficulty will be
		// lets unset (nil). Set it here to the correct value.
		block.SetCoinbase(testAddr)

		// We want to simulate an empty middle block, having the same state as the
		// first one. The last is needs a state change again to force a reorg.
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), common.Address{0x01}, big.NewInt(1), params.TxGas, big.NewInt(1), nil), signer, testKey)
		if err != nil {
			panic(err)
		}
		block.AddTxWithChain(chain, tx)
	}
	bs, _ := GenerateChain(params.TestChainConfig, chain.Genesis(), ethash.NewFaker(), db, blocks, generator)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}
	if lightProcess {
		EnableLightProcessor(chain)
	}

	return &testBackend{
		db:    db,
		chain: chain,
	}
}

// close tears down the transaction pool and chain behind the mock backend.
func (b *testBackend) close() {
	b.chain.Stop()
}

func (b *testBackend) Chain() *BlockChain { return b.chain }

func rawDataToDiffLayer(data rlp.RawValue) (*types.DiffLayer, error) {
	var diff types.DiffLayer
	hasher := sha3.NewLegacyKeccak256()
	err := rlp.DecodeBytes(data, &diff)
	if err != nil {
		return nil, err
	}
	hasher.Write(data)
	var diffHash common.Hash
	hasher.Sum(diffHash[:0])
	hasher.Reset()
	diff.DiffHash = diffHash
	return &diff, nil
}

func TestProcessDiffLayer(t *testing.T) {
	t.Parallel()

	blockNum := maxDiffLimit - 1
	fullBackend := newTestBackend(blockNum, false)
	falseDiff := 5
	defer fullBackend.close()

	lightBackend := newTestBackend(0, true)
	defer lightBackend.close()
	for i := 1; i <= blockNum-falseDiff; i++ {
		block := fullBackend.chain.GetBlockByNumber(uint64(i))
		if block == nil {
			t.Fatal("block should not be nil")
		}
		blockHash := block.Hash()
		rawDiff := fullBackend.chain.GetDiffLayerRLP(blockHash)
		diff, err := rawDataToDiffLayer(rawDiff)
		if err != nil {
			t.Errorf("failed to decode rawdata %v", err)
		}
		lightBackend.Chain().HandleDiffLayer(diff, "testpid")
		_, err = lightBackend.chain.insertChain([]*types.Block{block}, true)
		if err != nil {
			t.Errorf("failed to insert block %v", err)
		}
	}
	currentBlock := lightBackend.chain.CurrentBlock()
	nextBlock := fullBackend.chain.GetBlockByNumber(currentBlock.NumberU64() + 1)
	rawDiff := fullBackend.chain.GetDiffLayerRLP(nextBlock.Hash())
	diff, _ := rawDataToDiffLayer(rawDiff)
	latestAccount, _ := snapshot.FullAccount(diff.Accounts[0].Blob)
	latestAccount.Balance = big.NewInt(0)
	bz, _ := rlp.EncodeToBytes(&latestAccount)
	diff.Accounts[0].Blob = bz

	lightBackend.Chain().HandleDiffLayer(diff, "testpid")

	_, err := lightBackend.chain.insertChain([]*types.Block{nextBlock}, true)
	if err != nil {
		t.Errorf("failed to process block %v", err)
	}

	// the diff cache should be cleared
	if len(lightBackend.chain.diffPeersToDiffHashes) != 0 {
		t.Errorf("the size of diffPeersToDiffHashes should be 0, but get %d", len(lightBackend.chain.diffPeersToDiffHashes))
	}
	if len(lightBackend.chain.diffHashToPeers) != 0 {
		t.Errorf("the size of diffHashToPeers should be 0, but get %d", len(lightBackend.chain.diffHashToPeers))
	}
	if len(lightBackend.chain.diffHashToBlockHash) != 0 {
		t.Errorf("the size of diffHashToBlockHash should be 0, but get %d", len(lightBackend.chain.diffHashToBlockHash))
	}
	if len(lightBackend.chain.blockHashToDiffLayers) != 0 {
		t.Errorf("the size of blockHashToDiffLayers should be 0, but get %d", len(lightBackend.chain.blockHashToDiffLayers))
	}
}

func TestFreezeDiffLayer(t *testing.T) {
	t.Parallel()

	blockNum := 1024
	fullBackend := newTestBackend(blockNum, true)
	defer fullBackend.close()
	if fullBackend.chain.diffQueue.Size() != blockNum {
		t.Errorf("size of diff queue is wrong, expected: %d, get: %d", blockNum, fullBackend.chain.diffQueue.Size())
	}
	time.Sleep(diffLayerFreezerRecheckInterval + 1*time.Second)
	if fullBackend.chain.diffQueue.Size() != int(fullBackend.chain.triesInMemory) {
		t.Errorf("size of diff queue is wrong, expected: %d, get: %d", blockNum, fullBackend.chain.diffQueue.Size())
	}

	block := fullBackend.chain.GetBlockByNumber(uint64(blockNum / 2))
	diffStore := fullBackend.chain.db.DiffStore()
	rawData := rawdb.ReadDiffLayerRLP(diffStore, block.Hash())
	if len(rawData) == 0 {
		t.Error("do not find diff layer in db")
	}
}

func TestPruneDiffLayer(t *testing.T) {
	t.Parallel()

	blockNum := 1024
	fullBackend := newTestBackend(blockNum, true)
	defer fullBackend.close()

	anotherFullBackend := newTestBackend(2*blockNum, true)
	defer anotherFullBackend.close()

	for num := uint64(1); num < uint64(blockNum); num++ {
		header := fullBackend.chain.GetHeaderByNumber(num)
		rawDiff := fullBackend.chain.GetDiffLayerRLP(header.Hash())
		diff, _ := rawDataToDiffLayer(rawDiff)
		fullBackend.Chain().HandleDiffLayer(diff, "testpid1")
		fullBackend.Chain().HandleDiffLayer(diff, "testpid2")

	}
	fullBackend.chain.pruneDiffLayer()
	if len(fullBackend.chain.diffNumToBlockHashes) != maxDiffForkDist {
		t.Error("unexpected size of diffNumToBlockHashes")
	}
	if len(fullBackend.chain.diffPeersToDiffHashes) != 2 {
		t.Error("unexpected size of diffPeersToDiffHashes")
	}
	if len(fullBackend.chain.blockHashToDiffLayers) != maxDiffForkDist {
		t.Error("unexpected size of diffNumToBlockHashes")
	}
	if len(fullBackend.chain.diffHashToBlockHash) != maxDiffForkDist {
		t.Error("unexpected size of diffHashToBlockHash")
	}
	if len(fullBackend.chain.diffHashToPeers) != maxDiffForkDist {
		t.Error("unexpected size of diffHashToPeers")
	}

	blocks := make([]*types.Block, 0, blockNum)
	for i := blockNum + 1; i <= 2*blockNum; i++ {
		b := anotherFullBackend.chain.GetBlockByNumber(uint64(i))
		blocks = append(blocks, b)
	}
	fullBackend.chain.insertChain(blocks, true)
	fullBackend.chain.pruneDiffLayer()
	if len(fullBackend.chain.diffNumToBlockHashes) != 0 {
		t.Error("unexpected size of diffNumToBlockHashes")
	}
	if len(fullBackend.chain.diffPeersToDiffHashes) != 0 {
		t.Error("unexpected size of diffPeersToDiffHashes")
	}
	if len(fullBackend.chain.blockHashToDiffLayers) != 0 {
		t.Error("unexpected size of diffNumToBlockHashes")
	}
	if len(fullBackend.chain.diffHashToBlockHash) != 0 {
		t.Error("unexpected size of diffHashToBlockHash")
	}
	if len(fullBackend.chain.diffHashToPeers) != 0 {
		t.Error("unexpected size of diffHashToPeers")
	}

}
