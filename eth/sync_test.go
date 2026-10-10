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
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestNextSyncOpRandomPeer(t *testing.T) {
	tester := newTestHandler(ethconfig.FullSync)
	defer tester.close()
	// Keep the selection test separate from the handler's running sync loop.
	h := &handler{chain: tester.chain, database: tester.db, peers: newPeerSet(), maxPeers: 10}
	cs := newChainSyncer(h)
	cs.forced = true
	_, localTD := cs.modeAndLocalHead()
	addPeer := func(id byte, delta int64, lagging bool) *eth.Peer {
		rw, remote := p2p.MsgPipe()
		peer := eth.NewPeer(eth.ETH70, p2p.NewPeer(enode.ID{id}, "", nil), rw, nil, nil)
		other := eth.NewPeer(eth.ETH70, p2p.NewPeer(enode.ID{0xff}, "", nil), remote, nil, nil)
		t.Cleanup(func() {
			peer.Close()
			other.Close()
			rw.Close()
			remote.Close()
		})
		td := new(big.Int).Add(localTD, big.NewInt(delta))
		blockRange := eth.BlockRangeUpdatePacket{LatestBlockHash: common.Hash{id}}
		handshake := make(chan error, 1)
		go func() { handshake <- other.Handshake(1, tester.chain, blockRange, td, nil) }()
		require.NoError(t, peer.Handshake(1, tester.chain, blockRange, localTD, nil))
		require.NoError(t, <-handshake)
		if lagging {
			peer.MarkLagging()
		}
		require.NoError(t, h.peers.registerPeer(peer, nil, nil))
		return peer
	}
	require.Nil(t, cs.nextSyncOp())
	require.False(t, h.acceptTxs.Load(), "no peers must not imply caught up")
	addPeer(1, 1000000, true)
	require.Nil(t, cs.nextSyncOp())
	require.False(t, h.acceptTxs.Load(), "lagging peers must not imply caught up")
	addPeer(2, -1, false)
	addPeer(3, 0, false)
	require.Nil(t, cs.nextSyncOp())
	require.True(t, h.acceptTxs.Load(), "preserve the already-synced path")
	h.acceptTxs.Store(false)
	first := addPeer(4, 10, false)
	for range 32 {
		op := cs.nextSyncOp()
		require.NotNil(t, op, "behind peers must not hide an eligible sync source")
		require.Same(t, first, op.peer)
	}
	second := addPeer(5, 1000000000000, false)
	counts := make(map[*eth.Peer]int)
	for range 256 {
		op := cs.nextSyncOp()
		require.NotNil(t, op)
		require.True(t, op.peer == first || op.peer == second, "selected an ineligible peer")
		require.Equal(t, common.Hash{op.peer.NodeID()[0]}, op.head)
		require.Positive(t, op.td.Cmp(localTD))
		counts[op.peer]++
	}
	// A deliberately broad bound detects highest-TD selection or TD weighting.
	for _, peer := range []*eth.Peer{first, second} {
		require.GreaterOrEqual(t, counts[peer], 64)
		require.LessOrEqual(t, counts[peer], 192)
	}
	require.False(t, h.acceptTxs.Load(), "choosing a source must not mark sync complete")
	cs.doneCh = make(chan error)
	require.Nil(t, cs.nextSyncOp(), "do not replace the master during an active sync")
}

// Tests that snap sync is disabled after a successful sync cycle.
func TestSnapSyncDisabling68(t *testing.T) { testSnapSyncDisabling(t, eth.ETH68, snap.SNAP1) }

// Tests that snap sync gets disabled as soon as a real block is successfully
// imported into the blockchain.
func testSnapSyncDisabling(t *testing.T, ethVer uint, snapVer uint) {
	t.Parallel()

	// Create an empty handler and ensure it's in snap sync mode
	empty := newTestHandler(ethconfig.SnapSync)
	defer empty.close()

	// Create a full handler and ensure snap sync ends up disabled
	full := newTestHandlerWithBlocks(1024, ethconfig.SnapSync)
	defer full.close()

	// Sync up the two handlers via both `eth` and `snap`
	caps := []p2p.Cap{{Name: "eth", Version: ethVer}, {Name: "snap", Version: snapVer}}

	emptyPipeEth, fullPipeEth := p2p.MsgPipe()
	defer emptyPipeEth.Close()
	defer fullPipeEth.Close()

	emptyPeerEth := eth.NewPeer(ethVer, p2p.NewPeer(enode.ID{1}, "", caps), emptyPipeEth, empty.txpool, nil)
	fullPeerEth := eth.NewPeer(ethVer, p2p.NewPeer(enode.ID{2}, "", caps), fullPipeEth, full.txpool, nil)
	defer emptyPeerEth.Close()
	defer fullPeerEth.Close()

	go empty.handler.runEthPeer(emptyPeerEth, func(peer *eth.Peer) error {
		return eth.Handle((*ethHandler)(empty.handler), peer)
	})
	go full.handler.runEthPeer(fullPeerEth, func(peer *eth.Peer) error {
		return eth.Handle((*ethHandler)(full.handler), peer)
	})

	emptyPipeSnap, fullPipeSnap := p2p.MsgPipe()
	defer emptyPipeSnap.Close()
	defer fullPipeSnap.Close()

	emptyPeerSnap := snap.NewPeer(snapVer, p2p.NewPeer(enode.ID{1}, "", caps), emptyPipeSnap)
	fullPeerSnap := snap.NewPeer(snapVer, p2p.NewPeer(enode.ID{2}, "", caps), fullPipeSnap)

	go empty.handler.runSnapExtension(emptyPeerSnap, func(peer *snap.Peer) error {
		return snap.Handle((*snapHandler)(empty.handler), peer)
	})
	go full.handler.runSnapExtension(fullPeerSnap, func(peer *snap.Peer) error {
		return snap.Handle((*snapHandler)(full.handler), peer)
	})
	// Wait a bit for the above handlers to start
	time.Sleep(250 * time.Millisecond)

	// Check that snap sync was disabled
	op := peerToSyncOp(ethconfig.SnapSync, empty.handler.peers.peerWithHighestTD())
	if err := empty.handler.doSync(op); err != nil {
		t.Fatal("sync failed:", err)
	}
	// Snap sync and mode switching happen asynchronously, poll for completion.
	timeout := time.NewTimer(15 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer timeout.Stop()
	defer tick.Stop()

	for {
		select {
		case <-timeout.C:
			t.Fatalf("snap sync not disabled after successful synchronisation")
		case <-tick.C:
			if empty.handler.synced.Load() && empty.handler.downloader.ConfigSyncMode() == ethconfig.FullSync {
				return
			}
		}
	}
}

func TestFullSyncWithBlobs(t *testing.T) {
	testChainSyncWithBlobs(t, ethconfig.FullSync, 128, 128)
}

func TestSnapSyncWithBlobs(t *testing.T) {
	testChainSyncWithBlobs(t, ethconfig.SnapSync, 128, 128)
}

func testChainSyncWithBlobs(t *testing.T, mode downloader.SyncMode, preCancunBlks, postCancunBlks uint64) {
	t.Parallel()
	config := *params.ParliaTestChainConfig
	cancunTime := (preCancunBlks + 1) * 10
	config.CancunTime = &cancunTime

	// Create an empty handler
	empty := newTestParliaHandlerAfterCancun(t, &config, mode, 0, 0)
	defer empty.close()
	if ethconfig.SnapSync == mode && !empty.handler.snapSync.Load() {
		t.Fatalf("snap sync disabled on pristine blockchain")
	}

	// Create a full handler
	full := newTestParliaHandlerAfterCancun(t, &config, mode, preCancunBlks, postCancunBlks)
	defer full.close()
	if ethconfig.SnapSync == mode && full.handler.snapSync.Load() {
		t.Fatalf("snap sync not disabled on non-empty blockchain")
	}

	// check blocks and blobs
	checkChainWithBlobs(t, full.chain, preCancunBlks, postCancunBlks)

	// Sync up the two handlers via both `eth` and `snap`
	ethVer := uint(eth.ETH68)
	snapVer := uint(snap.SNAP1)

	// Sync up the two handlers via both `eth` and `snap`
	caps := []p2p.Cap{{Name: "eth", Version: ethVer}, {Name: "snap", Version: snapVer}}

	emptyPipeEth, fullPipeEth := p2p.MsgPipe(true)
	defer emptyPipeEth.Close()
	defer fullPipeEth.Close()

	emptyPeerEth := eth.NewPeer(ethVer, p2p.NewPeer(enode.ID{1}, "", caps), emptyPipeEth, empty.txpool, empty.chain.Config())
	fullPeerEth := eth.NewPeer(ethVer, p2p.NewPeer(enode.ID{2}, "", caps), fullPipeEth, full.txpool, full.chain.Config())
	defer emptyPeerEth.Close()
	defer fullPeerEth.Close()

	go empty.handler.runEthPeer(emptyPeerEth, func(peer *eth.Peer) error {
		return eth.Handle((*ethHandler)(empty.handler), peer)
	})
	go full.handler.runEthPeer(fullPeerEth, func(peer *eth.Peer) error {
		return eth.Handle((*ethHandler)(full.handler), peer)
	})

	emptyPipeSnap, fullPipeSnap := p2p.MsgPipe(true)
	defer emptyPipeSnap.Close()
	defer fullPipeSnap.Close()

	emptyPeerSnap := snap.NewPeer(snapVer, p2p.NewPeer(enode.ID{1}, "", caps), emptyPipeSnap)
	fullPeerSnap := snap.NewPeer(snapVer, p2p.NewPeer(enode.ID{2}, "", caps), fullPipeSnap)

	go empty.handler.runSnapExtension(emptyPeerSnap, func(peer *snap.Peer) error {
		return snap.Handle((*snapHandler)(empty.handler), peer)
	})
	go full.handler.runSnapExtension(fullPeerSnap, func(peer *snap.Peer) error {
		return snap.Handle((*snapHandler)(full.handler), peer)
	})

	for empty.handler.peers.snapLen() < 1 {
		// Wait a bit for the above handlers to start
		time.Sleep(100 * time.Millisecond)
	}

	op := peerToSyncOp(mode, empty.handler.peers.peerWithHighestTD())
	if err := empty.handler.doSync(op); err != nil {
		t.Fatal("sync failed:", err)
	}
	// Check that snap sync was disabled
	if !empty.handler.synced.Load() {
		t.Fatalf("full sync not done after successful synchronisation")
	}

	// check blocks and blobs
	checkChainWithBlobs(t, empty.chain, preCancunBlks, postCancunBlks)
}

func checkChainWithBlobs(t *testing.T, chain *core.BlockChain, preCancunBlks, postCancunBlks uint64) {
	block := chain.GetBlockByNumber(preCancunBlks)
	require.NotNil(t, block, preCancunBlks)
	require.Nil(t, chain.GetSidecarsByHash(block.Hash()), preCancunBlks)
	block = chain.GetBlockByNumber(preCancunBlks + 1)
	require.NotNil(t, block, preCancunBlks+1)
	require.NotNil(t, chain.GetSidecarsByHash(block.Hash()), preCancunBlks+1)
	block = chain.GetBlockByNumber(preCancunBlks + postCancunBlks)
	require.NotNil(t, block, preCancunBlks+postCancunBlks)
	require.NotNil(t, chain.GetSidecarsByHash(block.Hash()), preCancunBlks+postCancunBlks)
}
