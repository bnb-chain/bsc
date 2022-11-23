package monitor

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/log"
)

const (
	MaxCacheHeader = 100
)

func NewDoubleSignMonitor(
	extraSeal int,
	sealHash func(header *types.Header) (hash common.Hash),
) *DoubleSignMonitor {
	return &DoubleSignMonitor{
		sealHash:      sealHash,
		extraSeal:     extraSeal,
		headerNumbers: prque.New(nil),
		headers:       make(map[uint64]*types.Header, MaxCacheHeader),
		quit:          make(chan struct{}),
	}
}

type DoubleSignMonitor struct {
	extraSeal     int
	sealHash      func(header *types.Header) (hash common.Hash)
	headerNumbers *prque.Prque
	headers       map[uint64]*types.Header
	quit          chan struct{}
}

func (m *DoubleSignMonitor) getSignature(h *types.Header) ([]byte, error) {
	if len(h.Extra) < m.extraSeal {
		return nil, errors.New("extra-data 65 byte signature suffix missing")
	}
	signature := h.Extra[len(h.Extra)-m.extraSeal:]
	return signature, nil
}

func (m *DoubleSignMonitor) extractSignerFromHeader(h *types.Header) (signer common.Address, err error) {
	signature, err := m.getSignature(h)
	if err != nil {
		return
	}
	pubKey, err := secp256k1.RecoverPubkey(m.sealHash(h).Bytes(), signature)
	if err != nil {
		return
	}
	copy(signer[:], crypto.Keccak256(pubKey[1:])[12:])
	return
}

func (m *DoubleSignMonitor) isDoubleSignHeaders(h1, h2 *types.Header) (bool, []byte, []byte, error) {
	if h1 == nil || h2 == nil {
		return false, nil, nil, nil
	}
	if h1.Number.Cmp(h2.Number) != 0 {
		return false, nil, nil, nil
	}
	if bytes.Equal(h1.ParentHash[:], h2.ParentHash[:]) {
		return false, nil, nil, nil
	}
	signature1, err := m.getSignature(h1)
	if err != nil {
		return false, nil, nil, err
	}
	signature2, err := m.getSignature(h2)
	if err != nil {
		return false, nil, nil, err
	}
	if bytes.Equal(signature1, signature2) {
		return false, signature1, signature2, nil
	}

	signer1, err := m.extractSignerFromHeader(h1)
	if err != nil {
		return false, signature1, signature2, err
	}
	signer2, err := m.extractSignerFromHeader(h2)
	if err != nil {
		return false, signature1, signature2, err
	}
	if !bytes.Equal(signer1.Bytes(), signer2.Bytes()) {
		return false, signature1, signature2, nil
	}

	return true, signature1, signature2, nil
}

func (m *DoubleSignMonitor) deleteOldHeader() {
	v, _ := m.headerNumbers.Pop()
	h := v.(*types.Header)
	delete(m.headers, h.Number.Uint64())
}

func (m *DoubleSignMonitor) checkHeader(h *types.Header) (bool, *types.Header, []byte, []byte, error) {
	h2, exist := m.headers[h.Number.Uint64()]
	if !exist {
		if m.headerNumbers.Size() > MaxCacheHeader {
			m.deleteOldHeader()
		}
		m.headers[h.Number.Uint64()] = h
		m.headerNumbers.Push(h, -h.Number.Int64())
		return false, nil, nil, nil, nil
	}

	isDoubleSign, s1, s2, err := m.isDoubleSignHeaders(h, h2)
	if err != nil {
		return false, nil, s1, s2, err
	}
	if isDoubleSign {
		return true, h2, s1, s2, nil
	}

	return false, nil, s1, s2, nil
}

func (m *DoubleSignMonitor) Start(ch <-chan *types.Header) {
	for {
		select {
		case h := <-ch:
			isDoubleSign, h2, s1, s2, err := m.checkHeader(h)
			if err != nil {
				log.Error("check double sign header error", "err", err)
				continue
			}
			if isDoubleSign {
				// found a double sign header
				log.Error("found a double sign header", "number", h.Number.Uint64(),
					"first_hash", h.Hash(), "first_miner", h.Coinbase, "first_signature", s1,
					"second_hash", h2.Hash(), "second_miner", h2.Coinbase, "second_signature", s2)
			}
		case <-m.quit:
			return
		}
	}
}

func (m *DoubleSignMonitor) Close() {
	close(m.quit)
}
