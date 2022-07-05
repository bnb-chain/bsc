// Copyright 2015 The go-ethereum Authors
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

package diff

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
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
	db     ethdb.Database
	chain  *core.BlockChain
	txpool *core.TxPool
}

// newTestBackend creates an empty chain and wraps it into a mock backend.
func newTestBackend(blocks int) *testBackend {
	return newTestBackendWithGenerator(blocks)
}

// newTestBackend creates a chain with a number of explicitly defined blocks and
// wraps it into a mock backend.
func newTestBackendWithGenerator(blocks int) *testBackend {
	signer := types.HomesteadSigner{}
	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	(&core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testAddr: {Balance: big.NewInt(100000000000000000)}},
	}).MustCommit(db)

	chain, _ := core.NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFaker(), vm.Config{}, nil, nil)
	generator := func(i int, block *core.BlockGen) {
		// The chain maker doesn't have access to a chain, so the difficulty will be
		// lets unset (nil). Set it here to the correct value.
		block.SetCoinbase(testAddr)

		// We want to simulate an empty middle block, having the same state as the
		// first one. The last is needs a state change again to force a reorg.
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), common.Address{0x01}, big.NewInt(1), params.TxGas, big.NewInt(params.InitialBaseFee), nil), signer, testKey)
		if err != nil {
			panic(err)
		}
		block.AddTxWithChain(chain, tx)
	}
	bs, _ := core.GenerateChain(params.TestChainConfig, chain.Genesis(), ethash.NewFaker(), db, blocks, generator)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}
	txconfig := core.DefaultTxPoolConfig
	txconfig.Journal = "" // Don't litter the disk with test journals

	return &testBackend{
		db:     db,
		chain:  chain,
		txpool: core.NewTxPool(txconfig, params.TestChainConfig, chain),
	}
}

// close tears down the transaction pool and chain behind the mock backend.
func (b *testBackend) close() {
	b.txpool.Stop()
	b.chain.Stop()
}

func (b *testBackend) Chain() *core.BlockChain { return b.chain }

func (b *testBackend) RunPeer(peer *Peer, handler Handler) error {
	// Normally the backend would do peer mainentance and handshakes. All that
	// is omitted and we will just give control back to the handler.
	return handler(peer)
}
func (b *testBackend) PeerInfo(enode.ID) interface{} { panic("not implemented") }

func (b *testBackend) Handle(*Peer, Packet) error {
	panic("data processing tests should be done in the handler package")
}

func TestGetDiffLayers(t *testing.T) { testGetDiffLayers(t, Diff1) }

func testGetDiffLayers(t *testing.T, protocol uint) {
	t.Parallel()

	blockNum := 2048
	backend := newTestBackend(blockNum)
	defer backend.close()

	peer, _ := newTestPeer("peer", protocol, backend)
	defer peer.close()

	foundDiffBlockHashes := make([]common.Hash, 0)
	foundDiffPackets := make([]FullDiffLayersPacket, 0)
	foundDiffRlps := make([]rlp.RawValue, 0)
	missDiffBlockHashes := make([]common.Hash, 0)
	missDiffPackets := make([]FullDiffLayersPacket, 0)

	for i := 0; i < 100; i++ {
		number := uint64(rand.Int63n(1024))
		if number == 0 {
			continue
		}
		foundHash := backend.chain.GetCanonicalHash(number + 1024)
		missHash := backend.chain.GetCanonicalHash(number)
		foundRlp := backend.chain.GetDiffLayerRLP(foundHash)

		if len(foundHash) == 0 {
			t.Fatalf("Faild to fond rlp encoded diff layer %v", foundHash)
		}
		foundDiffPackets = append(foundDiffPackets, FullDiffLayersPacket{
			RequestId:        uint64(i),
			DiffLayersPacket: []rlp.RawValue{foundRlp},
		})
		foundDiffRlps = append(foundDiffRlps, foundRlp)

		missDiffPackets = append(missDiffPackets, FullDiffLayersPacket{
			RequestId:        uint64(i),
			DiffLayersPacket: []rlp.RawValue{},
		})

		missDiffBlockHashes = append(missDiffBlockHashes, missHash)
		foundDiffBlockHashes = append(foundDiffBlockHashes, foundHash)
	}

	for idx, blockHash := range foundDiffBlockHashes {
		p2p.Send(peer.app, GetDiffLayerMsg, GetDiffLayersPacket{RequestId: uint64(idx), BlockHashes: []common.Hash{blockHash}})
		if err := p2p.ExpectMsg(peer.app, FullDiffLayerMsg, foundDiffPackets[idx]); err != nil {
			t.Errorf("test %d: diff layer mismatch: %v", idx, err)
		}
	}

	for idx, blockHash := range missDiffBlockHashes {
		p2p.Send(peer.app, GetDiffLayerMsg, GetDiffLayersPacket{RequestId: uint64(idx), BlockHashes: []common.Hash{blockHash}})
		if err := p2p.ExpectMsg(peer.app, FullDiffLayerMsg, missDiffPackets[idx]); err != nil {
			t.Errorf("test %d: diff layer mismatch: %v", idx, err)
		}
	}

	p2p.Send(peer.app, GetDiffLayerMsg, GetDiffLayersPacket{RequestId: 111, BlockHashes: foundDiffBlockHashes})
	if err := p2p.ExpectMsg(peer.app, FullDiffLayerMsg, FullDiffLayersPacket{
		111,
		foundDiffRlps,
	}); err != nil {
		t.Errorf("test: diff layer mismatch: %v", err)
	}

	p2p.Send(peer.app, GetDiffLayerMsg, GetDiffLayersPacket{RequestId: 111, BlockHashes: missDiffBlockHashes})
	if err := p2p.ExpectMsg(peer.app, FullDiffLayerMsg, FullDiffLayersPacket{
		111,
		nil,
	}); err != nil {
		t.Errorf("test: diff layer mismatch: %v", err)
	}
}
