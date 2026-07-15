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

package eth

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// Tests that handshake failures are detected and reported correctly.
func TestHandshake68(t *testing.T) { testHandshake(t, ETH68) }
func TestHandshake70(t *testing.T) { testHandshake(t, ETH70) }

func testHandshake(t *testing.T, protocol uint) {
	t.Parallel()

	// Create a test backend only to have some valid genesis chain
	backend := newTestBackend(3)
	defer backend.close()

	var (
		genesis = backend.chain.Genesis()
		head    = backend.chain.CurrentBlock()
		td      = backend.chain.GetTd(head.Hash(), head.Number.Uint64())
		forkID  = forkid.NewID(backend.chain.Config(), backend.chain.Genesis(), backend.chain.CurrentHeader().Number.Uint64(), backend.chain.CurrentHeader().Time)
	)
	tests := []struct {
		code uint64
		data interface{}
		want error
	}{
		{
			code: TransactionsMsg, data: []interface{}{},
			want: errNoStatusMsg,
		},
	}
	if protocol == ETH68 {
		tests = append(tests,
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket68{10, 1, td, head.Hash(), genesis.Hash(), forkID}, want: errProtocolVersionMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket68{uint32(protocol), 999, td, head.Hash(), genesis.Hash(), forkID}, want: errNetworkIDMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket68{uint32(protocol), 1, td, head.Hash(), common.Hash{3}, forkID}, want: errGenesisMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket68{uint32(protocol), 1, td, head.Hash(), genesis.Hash(), forkid.ID{Hash: [4]byte{0x00, 0x01, 0x02, 0x03}}}, want: errForkIDRejected},
		)
	} else {
		tests = append(tests,
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket{10, 1, td, genesis.Hash(), forkID, 0, head.Number.Uint64(), head.Hash()}, want: errProtocolVersionMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket{uint32(protocol), 999, td, genesis.Hash(), forkID, 0, head.Number.Uint64(), head.Hash()}, want: errNetworkIDMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket{uint32(protocol), 1, td, common.Hash{3}, forkID, 0, head.Number.Uint64(), head.Hash()}, want: errGenesisMismatch},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket{uint32(protocol), 1, td, genesis.Hash(), forkid.ID{Hash: [4]byte{0x00, 0x01, 0x02, 0x03}}, 0, head.Number.Uint64(), head.Hash()}, want: errForkIDRejected},
			struct {
				code uint64
				data interface{}
				want error
			}{code: StatusMsg, data: StatusPacket{uint32(protocol), 1, td, genesis.Hash(), forkID, head.Number.Uint64() + 1, head.Number.Uint64(), head.Hash()}, want: errInvalidBlockRange},
		)
	}
	for i, test := range tests {
		// Create the two peers to shake with each other
		app, net := p2p.MsgPipe()
		defer app.Close()
		defer net.Close()

		peer := NewPeer(protocol, p2p.NewPeer(enode.ID{}, "peer", nil), net, nil, nil)
		defer peer.Close()

		// Send the junk test with one peer, check the handshake failure
		go p2p.Send(app, test.code, test.data)

		err := peer.Handshake(1, backend.chain, BlockRangeUpdatePacket{}, td, nil)
		if err == nil {
			t.Errorf("test %d: protocol returned nil error, want %q", i, test.want)
		} else if !errors.Is(err, test.want) {
			t.Errorf("test %d: wrong error: got %q, want %q", i, err, test.want)
		}
	}
}

func TestHandshakeSuccess(t *testing.T) {
	for _, protocol := range []uint{ETH68, ETH70} {
		t.Run(fmt.Sprintf("eth/%d", protocol), func(t *testing.T) {
			backend := newTestBackend(3)
			defer backend.close()

			head := backend.chain.CurrentBlock()
			td := backend.chain.GetTd(head.Hash(), head.Number.Uint64())
			blockRange := BlockRangeUpdatePacket{
				EarliestBlock:   0,
				LatestBlock:     head.Number.Uint64(),
				LatestBlockHash: head.Hash(),
			}
			app, net := p2p.MsgPipe()
			defer app.Close()
			defer net.Close()

			peerA := NewPeer(protocol, p2p.NewPeer(enode.ID{1}, "peer-a", nil), app, nil, backend.chain.Config())
			defer peerA.Close()
			peerB := NewPeer(protocol, p2p.NewPeer(enode.ID{2}, "peer-b", nil), net, nil, backend.chain.Config())
			defer peerB.Close()

			extension := &UpgradeStatusExtension{DisablePeerTxBroadcast: true}
			errc := make(chan error, 2)
			go func() { errc <- peerA.Handshake(1, backend.chain, blockRange, td, extension) }()
			go func() { errc <- peerB.Handshake(1, backend.chain, blockRange, td, extension) }()
			for range 2 {
				if err := <-errc; err != nil {
					t.Fatalf("handshake failed: %v", err)
				}
			}
			for _, peer := range []*Peer{peerA, peerB} {
				gotHead, gotTD := peer.Head()
				if gotHead != head.Hash() || gotTD.Cmp(td) != 0 {
					t.Fatalf("peer head mismatch: have (%s, %s), want (%s, %s)", gotHead, gotTD, head.Hash(), td)
				}
				if protocol == ETH70 {
					if peer.statusExtension != nil {
						t.Fatal("eth/70 negotiated an upgrade status extension")
					}
					if got := peer.BlockRange(); got == nil || *got != blockRange {
						t.Fatalf("peer block range mismatch: have %+v, want %+v", got, blockRange)
					}
				} else if peer.statusExtension == nil || !peer.statusExtension.DisablePeerTxBroadcast {
					t.Fatal("eth/68 did not negotiate the upgrade status extension")
				}
			}
		})
	}
}
