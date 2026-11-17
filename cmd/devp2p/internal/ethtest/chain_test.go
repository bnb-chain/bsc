// Copyright 2020 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package ethtest

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/stretchr/testify/assert"
)

// TestEthProtocolNegotiation checks that version gaps do not create a common capability.
func TestEthProtocolNegotiation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                    string
		local, remote           []p2p.Cap
		ethVersion, snapVersion uint
	}{
		{
			name:       "prefer71",
			local:      []p2p.Cap{{Name: "eth", Version: 71}, {Name: "eth", Version: 68}},
			remote:     []p2p.Cap{{Name: "eth", Version: 68}, {Name: "eth", Version: 71}},
			ethVersion: 71,
		},
		{
			name:       "fallback68",
			local:      []p2p.Cap{{Name: "eth", Version: 71}, {Name: "eth", Version: 68}},
			remote:     []p2p.Cap{{Name: "eth", Version: 70}, {Name: "eth", Version: 68}},
			ethVersion: 68,
		},
		{
			name:       "only71",
			local:      []p2p.Cap{{Name: "eth", Version: 71}},
			remote:     []p2p.Cap{{Name: "eth", Version: 70}, {Name: "eth", Version: 71}},
			ethVersion: 71,
		},
		{
			name:   "noCommonVersion",
			local:  []p2p.Cap{{Name: "eth", Version: 71}, {Name: "eth", Version: 68}},
			remote: []p2p.Cap{{Name: "eth", Version: 70}},
		},
		{
			name:   "only71Rejects68",
			local:  []p2p.Cap{{Name: "eth", Version: 71}},
			remote: []p2p.Cap{{Name: "eth", Version: 68}},
		},
		{
			name:       "ignore72",
			local:      []p2p.Cap{{Name: "eth", Version: 71}, {Name: "eth", Version: 68}},
			remote:     []p2p.Cap{{Name: "eth", Version: 72}, {Name: "eth", Version: 68}},
			ethVersion: 68,
		},
		{
			name:       "eth70",
			local:      []p2p.Cap{{Name: "eth", Version: 70}, {Name: "eth", Version: 68}},
			remote:     []p2p.Cap{{Name: "eth", Version: 70}, {Name: "eth", Version: 68}},
			ethVersion: 70,
		},
		{
			name:        "snap1",
			local:       []p2p.Cap{{Name: "eth", Version: 71}, {Name: "snap", Version: 1}},
			remote:      []p2p.Cap{{Name: "snap", Version: 2}, {Name: "eth", Version: 71}, {Name: "snap", Version: 1}},
			ethVersion:  71,
			snapVersion: 1,
		},
		{
			name:        "snap2",
			local:       []p2p.Cap{{Name: "eth", Version: 71}, {Name: "snap", Version: 1}, {Name: "snap", Version: 2}},
			remote:      []p2p.Cap{{Name: "snap", Version: 2}, {Name: "eth", Version: 71}, {Name: "snap", Version: 1}},
			ethVersion:  71,
			snapVersion: 2,
		},
		{
			name:       "noCommonSnap",
			local:      []p2p.Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 2}},
			remote:     []p2p.Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
			ethVersion: 68,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &Conn{caps: test.local}
			conn.negotiateEthProtocol(test.remote)
			assert.Equal(t, test.ethVersion, conn.negotiatedProtoVersion)
			assert.Equal(t, test.snapVersion, conn.negotiatedSnapProtoVersion)
		})
	}
}

func TestProtocolOffsets(t *testing.T) {
	for _, test := range []struct {
		version uint
		length  uint64
	}{
		{eth.ETH68, 17}, {eth.ETH70, 18}, {eth.ETH71, 20}, {eth.ETH72, 22},
	} {
		t.Run(strconv.Itoa(int(test.version)), func(t *testing.T) {
			conn := &Conn{negotiatedProtoVersion: test.version}
			assert.Equal(t, uint64(16), conn.protoOffset(ethProto))
			assert.Equal(t, 16+test.length, conn.protoOffset(snapProto))
			assert.Equal(t, baseProto, conn.getProto(15))
			assert.Equal(t, ethProto, conn.getProto(16))
			assert.Equal(t, ethProto, conn.getProto(15+test.length))
			assert.Equal(t, snapProto, conn.getProto(16+test.length))
			assert.Equal(t, snapProto, conn.getProto(25+test.length))
		})
	}
}

// TestChainHasAmsterdamWindow ensures the checked-in fixture keeps the recent
// block window exercised by the eth/71 and snap/2 tests past Amsterdam.
func TestChainHasAmsterdamWindow(t *testing.T) {
	chain, err := NewChain("./testdata")
	if err != nil {
		t.Fatal(err)
	}
	start := chain.Len() - 16
	if start < 0 {
		start = 0
	}
	for i := start; i < chain.Len(); i++ {
		if chain.blocks[i].BlockAccessListHash() == nil {
			t.Fatalf("test chain block %d has no block access list commitment", i)
		}
	}
}

// TestChainGetHeaders tests whether the test suite can correctly
// respond to a GetBlockHeaders request from a node.
func TestChainGetHeaders(t *testing.T) {
	t.Parallel()

	dir, err := filepath.Abs("./testdata")
	if err != nil {
		t.Fatal(err)
	}
	chain, err := NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}

	var tests = []struct {
		req      eth.GetBlockHeadersPacket
		expected []*types.Header
	}{
		{
			req: eth.GetBlockHeadersPacket{
				GetBlockHeadersRequest: &eth.GetBlockHeadersRequest{
					Origin:  eth.HashOrNumber{Number: uint64(2)},
					Amount:  uint64(5),
					Skip:    1,
					Reverse: false,
				},
			},
			expected: []*types.Header{
				chain.blocks[2].Header(),
				chain.blocks[4].Header(),
				chain.blocks[6].Header(),
				chain.blocks[8].Header(),
				chain.blocks[10].Header(),
			},
		},
		{
			req: eth.GetBlockHeadersPacket{
				GetBlockHeadersRequest: &eth.GetBlockHeadersRequest{
					Origin:  eth.HashOrNumber{Number: uint64(chain.Len() - 1)},
					Amount:  uint64(3),
					Skip:    0,
					Reverse: true,
				},
			},
			expected: []*types.Header{
				chain.blocks[chain.Len()-1].Header(),
				chain.blocks[chain.Len()-2].Header(),
				chain.blocks[chain.Len()-3].Header(),
			},
		},
		{
			req: eth.GetBlockHeadersPacket{
				GetBlockHeadersRequest: &eth.GetBlockHeadersRequest{
					Origin:  eth.HashOrNumber{Hash: chain.Head().Hash()},
					Amount:  uint64(1),
					Skip:    0,
					Reverse: false,
				},
			},
			expected: []*types.Header{
				chain.Head().Header(),
			},
		},
		{
			req: eth.GetBlockHeadersPacket{
				GetBlockHeadersRequest: &eth.GetBlockHeadersRequest{
					Origin:  eth.HashOrNumber{Number: uint64(10)},
					Amount:  uint64(3),
					Skip:    1,
					Reverse: true,
				},
			},
			expected: []*types.Header{
				chain.blocks[10].Header(),
				chain.blocks[8].Header(),
				chain.blocks[6].Header(),
			},
		},
	}

	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			headers, err := chain.GetHeaders(&tt.req)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, headers, tt.expected)
		})
	}
}
