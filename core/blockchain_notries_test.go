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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/params"
)

func newMockVerifyPeer() *mockVerifyPeer {
	return &mockVerifyPeer{}
}

type requestRoot struct {
	blockNumber uint64
	blockHash   common.Hash
	diffHash    common.Hash
}

type verifFailedStatus struct {
	status      types.VerifyStatus
	blockNumber uint64
}

// mockVerifyPeer is a mocking struct that simulates p2p signals for verification tasks.
type mockVerifyPeer struct {
	callback func(*requestRoot)
}

func (peer *mockVerifyPeer) setCallBack(callback func(*requestRoot)) {
	peer.callback = callback
}

func (peer *mockVerifyPeer) RequestRoot(blockNumber uint64, blockHash common.Hash, diffHash common.Hash) error {
	if peer.callback != nil {
		peer.callback(&requestRoot{blockNumber, blockHash, diffHash})
	}
	return nil
}

func (peer *mockVerifyPeer) ID() string {
	return "mock_peer"
}

type mockVerifyPeers struct {
	peers []VerifyPeer
}

func (peers *mockVerifyPeers) GetVerifyPeers() []VerifyPeer {
	return peers.peers
}

func newMockRemoteVerifyPeer(peers []VerifyPeer) *mockVerifyPeers {
	return &mockVerifyPeers{peers}
}

func makeTestBackendWithRemoteValidator(blocks int, mode VerifyMode, failed *verifFailedStatus) (*testBackend, *testBackend, []*types.Block, error) {
	signer := types.HomesteadSigner{}

	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	db.SetDiffStore(memorydb.New())
	gspec := &Genesis{
		Config: params.TestChainConfig,
		Alloc:  GenesisAlloc{testAddr: {Balance: big.NewInt(100000000000000000)}},
	}
	engine := ethash.NewFaker()

	db2 := rawdb.NewMemoryDatabase()
	db2.SetDiffStore(memorydb.New())
	gspec2 := &Genesis{
		Config: params.TestChainConfig,
		Alloc:  GenesisAlloc{testAddr: {Balance: big.NewInt(100000000000000000)}},
	}
	engine2 := ethash.NewFaker()

	peer := newMockVerifyPeer()
	peers := []VerifyPeer{peer}

	verifier, err := NewBlockChain(db, nil, gspec, nil, engine, vm.Config{},
		nil, nil, EnablePersistDiff(100000), EnableBlockValidator(params.TestChainConfig, engine2, LocalVerify, nil))
	if err != nil {
		return nil, nil, nil, err
	}

	fastnode, err := NewBlockChain(db2, nil, gspec2, nil, engine2, vm.Config{},
		nil, nil, EnableBlockValidator(params.TestChainConfig, engine2, mode, newMockRemoteVerifyPeer(peers)))
	if err != nil {
		return nil, nil, nil, err
	}

	generator := func(i int, block *BlockGen) {
		// The chain maker doesn't have access to a chain, so the difficulty will be
		// lets unset (nil). Set it here to the correct value.
		block.SetCoinbase(testAddr)

		for idx, testBlock := range testBlocks {
			// Specific block setting, the index in this generator has 1 diff from specified blockNr.
			if i+1 == testBlock.blockNr {
				for _, testTransaction := range testBlock.txs {
					var transaction *types.Transaction
					if testTransaction.to == nil {
						transaction = types.NewContractCreation(block.TxNonce(testAddr),
							testTransaction.value, uint64(commonGas), testTransaction.gasPrice, testTransaction.data)
					} else {
						transaction = types.NewTransaction(block.TxNonce(testAddr), *testTransaction.to,
							testTransaction.value, uint64(commonGas), testTransaction.gasPrice, testTransaction.data)
					}
					tx, err := types.SignTx(transaction, signer, testKey)
					if err != nil {
						panic(err)
					}
					block.AddTxWithChain(verifier, tx)
				}
				break
			}

			// Default block setting.
			if idx == len(testBlocks)-1 {
				// We want to simulate an empty middle block, having the same state as the
				// first one. The last is needs a state change again to force a reorg.
				for _, testTransaction := range testBlocks[0].txs {
					tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), *testTransaction.to,
						testTransaction.value, uint64(commonGas), testTransaction.gasPrice, testTransaction.data), signer, testKey)
					if err != nil {
						panic(err)
					}
					block.AddTxWithChain(verifier, tx)
				}
			}
		}
	}
	peer.setCallBack(func(req *requestRoot) {
		if fastnode.validator != nil && fastnode.validator.RemoteVerifyManager() != nil {
			resp := verifier.GetVerifyResult(req.blockNumber, req.blockHash, req.diffHash)
			if failed != nil && req.blockNumber == failed.blockNumber {
				resp.Status = failed.status
			}
			fastnode.validator.RemoteVerifyManager().
				HandleRootResponse(
					resp, peer.ID())
		}
	})

	bs, _ := GenerateChain(params.TestChainConfig, verifier.Genesis(), ethash.NewFaker(), db, blocks, generator)
	if _, err := verifier.InsertChain(bs); err != nil {
		return nil, nil, nil, err
	}
	waitDifflayerCached(verifier, bs)

	return &testBackend{
			db:    db,
			chain: verifier,
		},
		&testBackend{
			db:    db2,
			chain: fastnode,
		}, bs, nil
}

func TestFastNode(t *testing.T) {
	// test full mode and succeed
	_, fastnode, blocks, err := makeTestBackendWithRemoteValidator(2048, FullVerify, nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	_, err = fastnode.chain.InsertChain(blocks)
	if err != nil {
		t.Fatalf(err.Error())
	}
	// test full mode and failed
	failed := &verifFailedStatus{status: types.StatusDiffHashMismatch, blockNumber: 204}
	_, fastnode, blocks, err = makeTestBackendWithRemoteValidator(2048, FullVerify, failed)
	if err != nil {
		t.Fatalf(err.Error())
	}
	_, err = fastnode.chain.InsertChain(blocks)
	if err == nil || fastnode.chain.CurrentBlock().Number.Uint64() != failed.blockNumber+10 {
		t.Fatalf("blocks insert should be failed at height %d", failed.blockNumber+11)
	}
	// test insecure mode and succeed
	_, fastnode, blocks, err = makeTestBackendWithRemoteValidator(2048, InsecureVerify, nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	_, err = fastnode.chain.InsertChain(blocks)
	if err != nil {
		t.Fatalf(err.Error())
	}
	// test insecure mode and failed
	failed = &verifFailedStatus{status: types.StatusImpossibleFork, blockNumber: 204}
	_, fastnode, blocks, err = makeTestBackendWithRemoteValidator(2048, FullVerify, failed)
	if err != nil {
		t.Fatalf(err.Error())
	}
	_, err = fastnode.chain.InsertChain(blocks)
	if err == nil || fastnode.chain.CurrentBlock().Number.Uint64() != failed.blockNumber+10 {
		t.Fatalf("blocks insert should be failed at height %d", failed.blockNumber+11)
	}
}
