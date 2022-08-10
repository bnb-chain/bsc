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

package eth

import (
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/protocols/diff"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// testBackend is a mock implementation of the live Ethereum message handler. Its
// purpose is to allow testing the request/reply workflows and wire serialization
// in the `eth` protocol without actually doing any data processing.
type testBackend struct {
	db     ethdb.Database
	chain  *core.BlockChain
	txpool *core.TxPool

	handler *handler
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
	txpool := newTestTxPool()
	votepool := newTestVotePool()

	handler, _ := newHandler(&handlerConfig{
		Database:   db,
		Chain:      chain,
		TxPool:     txpool,
		VotePool:   votepool,
		Network:    1,
		Sync:       downloader.FullSync,
		BloomCache: 1,
		Merger:     consensus.NewMerger(rawdb.NewMemoryDatabase()),
	})
	handler.Start(100)

	txconfig := core.DefaultTxPoolConfig
	txconfig.Journal = "" // Don't litter the disk with test journals

	return &testBackend{
		db:      db,
		chain:   chain,
		txpool:  core.NewTxPool(txconfig, params.TestChainConfig, chain),
		handler: handler,
	}
}

// close tears down the transaction pool and chain behind the mock backend.
func (b *testBackend) close() {
	b.txpool.Stop()
	b.chain.Stop()
	b.handler.Stop()
}

func (b *testBackend) Chain() *core.BlockChain { return b.chain }

func (b *testBackend) RunPeer(peer *diff.Peer, handler diff.Handler) error {
	// Normally the backend would do peer mainentance and handshakes. All that
	// is omitted and we will just give control back to the handler.
	return handler(peer)
}
func (b *testBackend) PeerInfo(enode.ID) interface{} { panic("not implemented") }

func (b *testBackend) Handle(*diff.Peer, diff.Packet) error {
	panic("data processing tests should be done in the handler package")
}

type testPeer struct {
	*diff.Peer

	net p2p.MsgReadWriter // Network layer reader/writer to simulate remote messaging
	app *p2p.MsgPipeRW    // Application layer reader/writer to simulate the local side
}

// newTestPeer creates a new peer registered at the given data backend.
func newTestPeer(name string, version uint, backend *testBackend) (*testPeer, <-chan error) {
	// Create a message pipe to communicate through
	app, net := p2p.MsgPipe()

	// Start the peer on a new thread
	var id enode.ID
	rand.Read(id[:])

	peer := diff.NewPeer(version, p2p.NewPeer(id, name, nil), net)
	errc := make(chan error, 1)
	go func() {
		errc <- backend.RunPeer(peer, func(peer *diff.Peer) error {

			return diff.Handle((*diffHandler)(backend.handler), peer)
		})
	}()
	return &testPeer{app: app, net: net, Peer: peer}, errc
}

// close terminates the local side of the peer, notifying the remote protocol
// manager of termination.
func (p *testPeer) close() {
	p.Peer.Close()
	p.app.Close()
}

func TestHandleDiffLayer(t *testing.T) {
	t.Parallel()

	blockNum := 1024
	waitInterval := 100 * time.Millisecond
	backend := newTestBackend(blockNum)
	defer backend.close()

	peer, _ := newTestPeer("peer", diff.Diff1, backend)
	defer peer.close()

	tests := []struct {
		DiffLayer *types.DiffLayer
		Valid     bool
	}{
		{DiffLayer: &types.DiffLayer{
			BlockHash: common.Hash{0x1},
			Number:    1025,
		}, Valid: true},
		{DiffLayer: &types.DiffLayer{
			BlockHash: common.Hash{0x2},
			Number:    3073,
		}, Valid: false},
		{DiffLayer: &types.DiffLayer{
			BlockHash: common.Hash{0x3},
			Number:    500,
		}, Valid: false},
	}

	for _, tt := range tests {
		bz, _ := rlp.EncodeToBytes(tt.DiffLayer)

		p2p.Send(peer.app, diff.DiffLayerMsg, diff.DiffLayersPacket{rlp.RawValue(bz)})
	}
	time.Sleep(waitInterval)
	for idx, tt := range tests {
		diff := backend.chain.GetUnTrustedDiffLayer(tt.DiffLayer.BlockHash, "")
		if (tt.Valid && diff == nil) || (!tt.Valid && diff != nil) {
			t.Errorf("test: %d, diff layer handle failed", idx)
		}
	}
}
