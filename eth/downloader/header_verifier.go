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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// headerVerifier keeps verified headers available until block import persists them.
// It is owned by processHeaders and never changes the canonical header chain.
type headerVerifier struct {
	BlockChain
	headers map[common.Hash]*types.Header
	pending []*types.Header
}

func newHeaderVerifier(chain BlockChain) *headerVerifier {
	return &headerVerifier{BlockChain: chain, headers: make(map[common.Hash]*types.Header)}
}

func (v *headerVerifier) GetHeader(hash common.Hash, number uint64) *types.Header {
	if header := v.headers[hash]; header != nil && header.Number.Uint64() == number {
		return header
	}
	return v.BlockChain.GetHeader(hash, number)
}

func (v *headerVerifier) GetHeaderByHash(hash common.Hash) *types.Header {
	if header := v.headers[hash]; header != nil {
		return header
	}
	return v.BlockChain.GetHeaderByHash(hash)
}

func (v *headerVerifier) verify(headers []*types.Header, cancel <-chan struct{}) error {
	for _, header := range headers {
		select {
		case <-cancel:
			return errCanceled
		default:
		}
		if err := v.Engine().VerifyHeader(v, header); err != nil {
			return fmt.Errorf("%w: header %d: %v", errInvalidChain, header.Number, err)
		}
		v.headers[header.Hash()] = header
		v.pending = append(v.pending, header)
	}
	return nil
}

// prune releases the persisted prefix; later verification can read it from disk.
func (v *headerVerifier) prune() int {
	for len(v.pending) > 0 {
		header := v.pending[0]
		if !v.BlockChain.HasHeader(header.Hash(), header.Number.Uint64()) {
			break
		}
		delete(v.headers, header.Hash())
		v.pending[0] = nil
		v.pending = v.pending[1:]
	}
	return len(v.pending)
}
