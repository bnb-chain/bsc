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
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

type blockPropagationBackend struct {
	*testBackend
	packets chan Packet
}

func (b *blockPropagationBackend) Handle(_ *Peer, packet Packet) error {
	b.packets <- packet
	return nil
}

// BSC still propagates blocks through eth peers, including on eth/71. Exercise
// dispatch and decoding rather than calling the propagation handlers directly.
func TestBSCBlockPropagation(t *testing.T) {
	backend := newTestBackend(2)
	defer backend.close()
	block := backend.chain.GetBlockByNumber(1)
	td := backend.chain.GetTd(block.Hash(), block.NumberU64())
	for _, version := range []uint{ETH68, ETH70, ETH71} {
		t.Run(fmt.Sprintf("eth/%d", version), func(t *testing.T) {
			receiver := &blockPropagationBackend{testBackend: backend, packets: make(chan Packet, 1)}
			peer, errc := newTestPeer("propagation", version, receiver)
			defer peer.close()
			for _, packet := range []Packet{
				&NewBlockHashesPacket{{Hash: block.Hash(), Number: block.NumberU64()}},
				&NewBlockPacket{Block: block, TD: td},
			} {
				if err := p2p.Send(peer.app, uint64(packet.Kind()), packet); err != nil {
					t.Fatal(err)
				}
				select {
				case received := <-receiver.packets:
					switch got := received.(type) {
					case *NewBlockHashesPacket:
						if len(*got) != 1 || (*got)[0].Hash != block.Hash() || (*got)[0].Number != block.NumberU64() {
							t.Fatalf("wrong block announcement: %+v", got)
						}
					case *NewBlockPacket:
						if got.Block.Hash() != block.Hash() || got.TD.Cmp(td) != 0 {
							t.Fatal("block propagation lost block or TD")
						}
					default:
						t.Fatalf("unexpected packet %T", received)
					}
				case err := <-errc:
					t.Fatalf("propagation rejected: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("propagation was not delivered")
				}
			}
		})
	}
}

// Pre-Amsterdam blocks and unknown hashes have no BAL. Advertising eth/71 must
// still serve the unavailable markers and leave ordinary block requests usable.
func TestEth71WithoutBAL(t *testing.T) {
	backend := newTestBackend(2)
	defer backend.close()
	peer, _ := newTestPeer("no-bal", ETH71, backend)
	defer peer.close()
	hashes := []common.Hash{backend.chain.CurrentBlock().Hash(), {0xde, 0xad}, backend.chain.Genesis().Hash()}
	var list rlp.RawList[rlp.RawValue]
	for range hashes {
		list.AppendRaw(rlp.EmptyString)
	}
	if err := p2p.Send(peer.app, GetBlockAccessListsMsg, &GetBlockAccessListsPacket{RequestId: 1, GetBlockAccessListsRequest: hashes}); err != nil {
		t.Fatal(err)
	}
	if err := p2p.ExpectMsg(peer.app, BlockAccessListsMsg, &BlockAccessListPacket{RequestId: 1, List: list}); err != nil {
		t.Fatal(err)
	}
	block := backend.chain.GetBlockByNumber(1)
	if err := p2p.Send(peer.app, GetBlockBodiesMsg, &GetBlockBodiesPacket{RequestId: 2, GetBlockBodiesRequest: []common.Hash{block.Hash()}}); err != nil {
		t.Fatal(err)
	}
	if err := p2p.ExpectMsg(peer.app, BlockBodiesMsg, &BlockBodiesPacket{RequestId: 2, List: encodeRL([]BlockBody{encodeBody(block)})}); err != nil {
		t.Fatal(err)
	}
}
