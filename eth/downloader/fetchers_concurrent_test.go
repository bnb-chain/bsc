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
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

type bodyRetryRequest struct {
	req  *eth.Request
	sink chan *eth.Response
}

type bodyRetryQueue struct {
	*bodyQueue
	requests  chan bodyRetryRequest
	updates   chan string
	batchSize int
}

func (q *bodyRetryQueue) capacity(p *peerConnection, _ time.Duration) int {
	return map[string]int{"master": 40, "tail": 30, "slow": 20, "backup": 10}[p.id]
}

func (q *bodyRetryQueue) reserve(p *peerConnection, count int) (*fetchRequest, bool, bool) {
	req, progress, throttle := q.bodyQueue.reserve(p, min(count, q.batchSize))
	if req != nil && req.RetryAfter > 0 {
		req.RetryAfter = 25 * time.Millisecond
	}
	return req, progress, throttle
}

func (q *bodyRetryQueue) request(p *peerConnection, _ *fetchRequest, sink chan *eth.Response) (*eth.Request, error) {
	req := &eth.Request{Peer: p.id, Sent: time.Now()}
	q.requests <- bodyRetryRequest{req, sink}
	return req, nil
}

func (q *bodyRetryQueue) updateCapacity(p *peerConnection, _ int, _ time.Duration) {
	q.updates <- p.id
}

// Exercise the actual fetch loop: an early retry must preempt an older timer,
// keep the slow master connected, and ignore late responses without decoding.
func TestConcurrentBodyRetry(t *testing.T) {
	for _, lateResponse := range []bool{true, false} {
		name := "normal-deadline"
		if lateResponse {
			name = "late-response"
		}
		t.Run(name, func(t *testing.T) { testConcurrentBodyRetry(t, lateResponse, 2) })
	}
	t.Run("large-batch-deadline", func(t *testing.T) { testConcurrentBodyRetry(t, false, 3) })
}

func testConcurrentBodyRetry(t *testing.T, lateResponse bool, batchSize int) {
	dropped := make(chan string, 4)
	d := &Downloader{
		queue: newQueue(8, 8), peers: newPeerSet(), cancelCh: make(chan struct{}),
		cancelPeer: "slow", dropPeer: func(id string) { dropped <- id },
	}
	for _, id := range []string{"master", "tail", "slow", "backup"} {
		if err := d.peers.Register(newPeerConnection(id, eth.ETH68, nil, log.New("peer", id))); err != nil {
			t.Fatal(err)
		}
	}
	d.peers.rates.OverrideTTLLimit = time.Second
	d.queue.Prepare(1, FullSync)
	headers := make([]*types.Header, batchSize+1)
	hashes := make([]common.Hash, len(headers))
	for i := range headers {
		headers[i] = &types.Header{Number: big.NewInt(int64(i + 1)), TxHash: common.Hash{1}, UncleHash: types.EmptyUncleHash}
		if i > 0 {
			headers[i].ParentHash = hashes[i-1]
		}
		hashes[i] = headers[i].Hash()
	}
	if n := d.queue.Schedule(headers, hashes, 1); n != len(headers) {
		t.Fatalf("scheduled %d headers, want %d", n, len(headers))
	}
	q := &bodyRetryQueue{bodyQueue: (*bodyQueue)(d), requests: make(chan bodyRetryRequest, 8), updates: make(chan string, 8), batchSize: batchSize}
	done := make(chan error, 1)
	go func() { done <- d.concurrentFetch(q, false) }()
	t.Cleanup(func() {
		d.cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("fetcher did not stop")
		}
	})
	next := func(want string) bodyRetryRequest {
		t.Helper()
		select {
		case id := <-dropped:
			t.Fatalf("early retry dropped peer %s", id)
		case request := <-q.requests:
			if request.req.Peer != want {
				t.Fatalf("request peer %s, want %s", request.req.Peer, want)
			}
			return request
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("no request to %s before the normal tail timeout", want)
		}
		return bodyRetryRequest{}
	}
	first := next("master")
	next("tail")
	// Completing the initial batch leaves the long tail deadline armed. The
	// next early retry must reset that timer even though its heap is nonempty.
	empty := eth.BlockBodiesResponse{}
	first.sink <- &eth.Response{Req: first.req, Res: &empty, Meta: eth.BlockBodyHashes{}, Done: make(chan error, 1)}
	slow := next("slow")
	next("backup")
	select {
	case id := <-dropped:
		t.Fatalf("early retry dropped peer %s", id)
	case req := <-q.requests:
		t.Fatalf("unbounded early retry to %s", req.req.Peer)
	case <-time.After(100 * time.Millisecond):
	}
	if !lateResponse {
		if batchSize > 2 {
			deadline := time.After(2 * time.Second)
			for {
				select {
				case id := <-q.updates:
					if id == "slow" {
						select {
						case <-d.cancelCh:
							t.Fatal("large batch timeout canceled sync instead of reducing throughput")
						default:
						}
						return
					}
				case id := <-dropped:
					if id == "slow" {
						t.Fatal("large batch timeout lost its original item count")
					}
				case <-deadline:
					t.Fatal("early retry lost the large batch deadline")
				}
			}
		}
		select {
		case <-d.cancelCh:
			// A truly unresponsive master still terminates the cycle at the
			// original deadline, even though its body was already requeued.
		case <-time.After(2 * time.Second):
			t.Fatal("early retry lost the original request deadline")
		}
		return
	}
	// The old request no longer owns the body. A nil payload deliberately
	// detects any attempt to decode a late response through bodyQueue.deliver.
	ack := make(chan error, 1)
	slow.sink <- &eth.Response{Req: slow.req, Done: ack}
	select {
	case err := <-ack:
		if err != nil {
			t.Fatalf("late response rejected: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("late response was not released")
	}
	for len(q.updates) > 0 {
		if id := <-q.updates; id == "slow" {
			t.Fatal("early retry penalized slow peer throughput")
		}
	}
}
