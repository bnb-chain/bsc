// Copyright 2026 The go-ethereum Authors
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

package downloader

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/tracker"
	"github.com/ethereum/go-ethereum/params"
)

// requestLimitPeer routes requests through a real, saturated eth dispatcher.
// For ancestor tests, the initial head response comes from the test chain.
type requestLimitPeer struct {
	*downloadTesterPeer
	limited  *eth.Peer
	failHead bool
}

func (p *requestLimitPeer) RequestHeadersByHash(origin common.Hash, amount int, skip int, reverse bool, sink chan *eth.Response) (*eth.Request, error) {
	if p.failHead {
		return p.limited.RequestHeadersByHash(origin, amount, skip, reverse, sink)
	}
	return p.downloadTesterPeer.RequestHeadersByHash(origin, amount, skip, reverse, sink)
}

func (p *requestLimitPeer) RequestHeadersByNumber(origin uint64, amount int, skip int, reverse bool, sink chan *eth.Response) (*eth.Request, error) {
	return p.limited.RequestHeadersByNumber(origin, amount, skip, reverse, sink)
}

// TestLegacySyncRequestLimit exercises the real tracker and dispatcher without
// synchroniseMock. Cancelled requests remain tracked until a response or expiry.
func TestLegacySyncRequestLimit(t *testing.T) {
	for _, mode := range []SyncMode{FullSync, SnapSync} {
		for _, stage := range []string{"head", "ancestor"} {
			t.Run(fmt.Sprintf("%s/%s", mode, stage), func(t *testing.T) {
				tester := newTester(t, mode)
				defer tester.terminate()
				chain := testChainBase.shorten(MaxHeaderFetch)
				peer := tester.newPeer("limited", eth.ETH68, chain.blocks[1:])
				healthy := tester.newPeer("healthy", eth.ETH68, chain.blocks[1:])

				rw, remote := p2p.MsgPipe()
				limited := eth.NewPeer(eth.ETH68, p2p.NewPeer(enode.ID{1}, "limited", nil), rw, nil, params.TestChainConfig)
				done := make(chan struct{})
				go func() {
					defer close(done)
					for {
						msg, err := remote.ReadMsg()
						if err != nil {
							return
						}
						// Consume requests without sending any replies.
						if err := msg.Discard(); err != nil {
							return
						}
					}
				}()
				defer func() {
					limited.Close()
					remote.Close()
					<-done
				}()

				// Fill the tracker until it rejects a request, without depending
				// on its private capacity. Closing requests clears dispatcher
				// state, but retains tracker state.
				for i := 0; ; i++ {
					req, err := limited.RequestHeadersByHash(common.Hash{}, 1, 0, false, nil)
					if errors.Is(err, tracker.ErrLimitReached) {
						break
					}
					if err != nil {
						t.Fatalf("request %d: %v", i, err)
					}
					if err := req.Close(); err != nil {
						t.Fatalf("cancel request %d: %v", i, err)
					}
					if i >= 1_000_000 {
						t.Fatal("tracker never saturated after more than 1,000,000 requests")
					}
				}
				tester.downloader.peers.Peer(peer.id).peer = &requestLimitPeer{
					downloadTesterPeer: peer,
					limited:            limited,
					failHead:           stage == "head",
				}
				var dropped []string
				tester.downloader.dropPeer = func(id string) {
					dropped = append(dropped, id)
					tester.dropPeer(id)
				}
				head, td := peer.Head()
				if err := tester.downloader.LegacySync(peer.id, head, "", td, nil, mode); !errors.Is(err, tracker.ErrLimitReached) {
					t.Fatalf("sync error: have %v, want %v", err, tracker.ErrLimitReached)
				}
				if len(dropped) != 1 || dropped[0] != peer.id {
					t.Fatalf("dropped peers: have %v, want [%s]", dropped, peer.id)
				}
				if tester.downloader.peers.Peer(peer.id) != nil {
					t.Fatal("request-limited peer is still registered")
				}
				if tester.downloader.peers.Peer(healthy.id) == nil {
					t.Fatal("healthy peer was dropped")
				}
				head, td = healthy.Head()
				if err := tester.downloader.LegacySync(healthy.id, head, "", td, nil, mode); err != nil {
					t.Fatalf("sync with healthy peer: %v", err)
				}
				if got := tester.chain.CurrentBlock().Hash(); got != head {
					t.Fatalf("synced head: have %s, want %s", got, head)
				}
			})
		}
	}
}

// A missing drop callback is supported for local copy operations.
func TestLegacySyncRequestLimitWithoutDropPeer(t *testing.T) {
	for _, err := range []error{tracker.ErrLimitReached, fmt.Errorf("header request: %w", tracker.ErrLimitReached)} {
		d := &Downloader{
			synchroniseMock: func(string, common.Hash) error { return err },
		}
		if got := d.LegacySync("local", common.Hash{}, "", big.NewInt(1), nil, FullSync); got != err {
			t.Fatalf("sync error: have %v, want %v", got, err)
		}
	}
}
