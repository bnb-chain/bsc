package bsc

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p"
)

type Request struct {
	code      uint64
	want      uint64
	requestID uint64
	data      interface{}
	resCh     chan interface{}
	timeout   time.Duration
}

type Response struct {
	code      uint64
	requestID uint64
	data      interface{}
}

// Dispatcher handles message requests and responses
type Dispatcher struct {
	peer     *Peer
	requests map[uint64]*Request
	mu       sync.Mutex
}

// NewDispatcher creates a new message dispatcher
func NewDispatcher(peer *Peer) *Dispatcher {
	d := &Dispatcher{
		peer:     peer,
		requests: make(map[uint64]*Request),
	}

	return d
}

// GenRequestID get requestID for packet
func (d *Dispatcher) GenRequestID() uint64 {
	return rand.Uint64()
}

// DispatchRequest send the request, and track the later response
func (d *Dispatcher) DispatchRequest(req *Request) (interface{}, error) {
	err := p2p.Send(d.peer.rw, req.code, req.data)
	if err != nil {
		return nil, err
	}

	if req.resCh == nil {
		req.resCh = make(chan interface{}, 1)
	}
	d.mu.Lock()
	d.requests[req.requestID] = req
	d.mu.Unlock()

	timeout := time.NewTimer(req.timeout)
	select {
	case res := <-req.resCh:
		return res, nil
	case <-timeout.C:
		return nil, errors.New("request timeout")
	case <-d.peer.term:
		return nil, errors.New("peer disconnected")
	}
}

func (d *Dispatcher) DispatchResponse(res *Response) {
	d.mu.Lock()
	defer d.mu.Unlock()
	req := d.requests[res.requestID]
	if req == nil {
		d.peer.Log().Debug("handleResponse missing the request", "requestID", res.requestID)
		return
	}

	if req.want != res.code {
		d.peer.Log().Debug("handleResponse mismatch", "request", req, "response", res.code, "want", req.want)
	}

	req.resCh <- res.data
	delete(d.requests, res.requestID)
}
