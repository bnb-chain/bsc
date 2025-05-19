package bsc

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
)

type Request struct {
	code      uint64
	want      uint64
	requestID uint64
	data      interface{}
	resCh     chan interface{}
	cancelCh  chan string
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
	return &Dispatcher{
		peer:     peer,
		requests: make(map[uint64]*Request),
	}
}

// GenRequestID get requestID for packet
func (d *Dispatcher) GenRequestID() uint64 {
	return rand.Uint64()
}

// DispatchRequest send the request, and block until the later response
func (d *Dispatcher) DispatchRequest(req *Request) (interface{}, error) {
	// record the request before sending
	req.resCh = make(chan interface{}, 1)
	req.cancelCh = make(chan string, 1)
	d.mu.Lock()
	d.requests[req.requestID] = req
	d.mu.Unlock()

	log.Debug("send BlocksByRange request", "code", req.code, "requestId", req.requestID)
	err := p2p.Send(d.peer.rw, req.code, req.data)
	if err != nil {
		return nil, err
	}

	// clean the requests when the request is done
	defer func() {
		d.mu.Lock()
		delete(d.requests, req.requestID)
		d.mu.Unlock()
	}()

	timeout := time.NewTimer(req.timeout)
	select {
	case res := <-req.resCh:
		return res, nil
	case <-timeout.C:
		req.cancelCh <- "timeout"
		return nil, errors.New("request timeout")
	case <-d.peer.term:
		return nil, errors.New("peer disconnected")
	}
}

// getRequestByResp get the request by the response, and delete the request if it is matched
func (d *Dispatcher) getRequestByResp(res *Response) (*Request, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	req := d.requests[res.requestID]
	if req == nil {
		return nil, errors.New("missing the request")
	}

	if req.want != res.code {
		return nil, fmt.Errorf("response mismatch: %d != %d", res.code, req.want)
	}
	log.Debug("get the request, then clean it", "requestId", req.requestID)
	delete(d.requests, req.requestID)
	return req, nil
}

func (d *Dispatcher) DispatchResponse(res *Response) error {
	req, err := d.getRequestByResp(res)
	if err != nil {
		return err
	}

	select {
	case req.resCh <- res.data:
		return nil
	case reason := <-req.cancelCh:
		return fmt.Errorf("request cancelled: %d , reason: %s", res.requestID, reason)
	case <-d.peer.term:
		return errors.New("peer disconnected")
	}
}
