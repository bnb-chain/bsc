package monitor

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	MaxCacheHeader = 100
)

func NewDoubleSignMonitor() *DoubleSignMonitor {
	return &DoubleSignMonitor{
		headerNumbers: prque.New(nil),
		headers:       make(map[uint64]*types.Header, MaxCacheHeader),
		quit:          make(chan struct{}),
	}
}

type DoubleSignMonitor struct {
	headerNumbers *prque.Prque
	headers       map[uint64]*types.Header
	quit          chan struct{}
}

func (m *DoubleSignMonitor) isDoubleSignHeaders(h1, h2 *types.Header) (bool, error) {
	if h1 == nil || h2 == nil {
		return false, nil
	}
	if h1.Number.Cmp(h2.Number) != 0 {
		return false, nil
	}
	if !bytes.Equal(h1.ParentHash[:], h2.ParentHash[:]) {
		return false, nil
	}
	// if the Hash is different the signature should not be equal
	if bytes.Equal(h1.Hash().Bytes(), h2.Hash().Bytes()) {
		return false, nil
	}
	// signer is already verified in sync program, we can trust coinbase.
	if !bytes.Equal(h1.Coinbase.Bytes(), h2.Coinbase.Bytes()) {
		return false, nil
	}

	return true, nil
}

func (m *DoubleSignMonitor) deleteOldHeader() {
	v, _ := m.headerNumbers.Pop()
	h := v.(*types.Header)
	delete(m.headers, h.Number.Uint64())
}

func (m *DoubleSignMonitor) checkHeader(h *types.Header) (bool, *types.Header, error) {
	h2, exist := m.headers[h.Number.Uint64()]
	if !exist {
		if m.headerNumbers.Size() > MaxCacheHeader {
			m.deleteOldHeader()
		}
		m.headers[h.Number.Uint64()] = h
		m.headerNumbers.Push(h, -h.Number.Int64())
		return false, nil, nil
	}

	isDoubleSign, err := m.isDoubleSignHeaders(h, h2)
	if err != nil {
		return false, nil, err
	}
	if isDoubleSign {
		return true, h2, nil
	}

	return false, nil, nil
}

func (m *DoubleSignMonitor) Start(ch <-chan *types.Header) {
	for {
		select {
		case h := <-ch:
			isDoubleSign, h2, err := m.checkHeader(h)
			if err != nil {
				log.Error("check double sign header error", "err", err)
				continue
			}
			if isDoubleSign {
				// found a double sign header
				log.Error("found a double sign header", "number", h.Number.Uint64(),
					"first_hash", h.Hash(), "first_miner", h.Coinbase,
					"second_hash", h2.Hash(), "second_miner", h2.Coinbase)
			}
		case <-m.quit:
			return
		}
	}
}

func (m *DoubleSignMonitor) Close() {
	close(m.quit)
}
