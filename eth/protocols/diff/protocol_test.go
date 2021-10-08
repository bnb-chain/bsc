// Copyright 2014 The go-ethereum Authors
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

package diff

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Tests that the custom union field encoder and decoder works correctly.
func TestDiffLayersPacketDataEncodeDecode(t *testing.T) {
	// Create a "random" hash for testing
	var hash common.Hash
	for i := range hash {
		hash[i] = byte(i)
	}

	testDiffLayers := []*types.DiffLayer{
		{
			BlockHash: common.HexToHash("0x1e9624dcd0874958723aa3dae1fe299861e93ef32b980143d798c428bdd7a20a"),
			Number:    10479133,
			Receipts: []*types.Receipt{{
				GasUsed:          100,
				TransactionIndex: 1,
			}},
			Codes: []types.DiffCode{{
				Hash: common.HexToHash("0xaece2dbf80a726206cf4df299afa09f9d8f3dcd85ff39bb4b3f0402a3a6af2f5"),
				Code: []byte{1, 2, 3, 4},
			}},
			Destructs: []common.Address{
				common.HexToAddress("0x0205bb28ece9289d3fb8eb0c9e999bbd5be2b931"),
			},
			Accounts: []types.DiffAccount{{
				Account: common.HexToAddress("0x18b2a687610328590bc8f2e5fedde3b582a49cda"),
				Blob:    []byte{2, 3, 4, 5},
			}},
			Storages: []types.DiffStorage{{
				Account: common.HexToAddress("0x18b2a687610328590bc8f2e5fedde3b582a49cda"),
				Keys:    []string{"abc"},
				Vals:    [][]byte{{1, 2, 3}},
			}},
		},
	}
	// Assemble some table driven tests
	tests := []struct {
		diffLayers []*types.DiffLayer
		fail       bool
	}{
		{fail: false, diffLayers: testDiffLayers},
	}
	// Iterate over each of the tests and try to encode and then decode
	for i, tt := range tests {
		originPacket := make([]rlp.RawValue, 0)
		for _, diff := range tt.diffLayers {
			bz, err := rlp.EncodeToBytes(diff)
			assert.NoError(t, err)
			originPacket = append(originPacket, bz)
		}

		bz, err := rlp.EncodeToBytes(DiffLayersPacket(originPacket))
		if err != nil && !tt.fail {
			t.Fatalf("test %d: failed to encode packet: %v", i, err)
		} else if err == nil && tt.fail {
			t.Fatalf("test %d: encode should have failed", i)
		}
		if !tt.fail {
			packet := new(DiffLayersPacket)
			if err := rlp.DecodeBytes(bz, packet); err != nil {
				t.Fatalf("test %d: failed to decode packet: %v", i, err)
			}
			diffLayers, err := packet.Unpack()
			assert.NoError(t, err)

			if len(diffLayers) != len(tt.diffLayers) {
				t.Fatalf("test %d: encode length mismatch: have %+v, want %+v", i, len(diffLayers), len(tt.diffLayers))
			}
			expectedPacket := make([]rlp.RawValue, 0)
			for _, diff := range diffLayers {
				bz, err := rlp.EncodeToBytes(diff)
				assert.NoError(t, err)
				expectedPacket = append(expectedPacket, bz)
			}
			for i := 0; i < len(expectedPacket); i++ {
				if !bytes.Equal(expectedPacket[i], originPacket[i]) {
					t.Fatalf("test %d: data change during encode and decode", i)
				}
			}
		}
	}
}

func TestDiffMessages(t *testing.T) {

	for i, tc := range []struct {
		message interface{}
		want    []byte
	}{
		{
			DiffCapPacket{true, defaultExtra},
			common.FromHex("c20100"),
		},
		{
			GetDiffLayersPacket{1111, []common.Hash{common.HexToHash("0xaece2dbf80a726206cf4df299afa09f9d8f3dcd85ff39bb4b3f0402a3a6af2f5")}},
			common.FromHex("e5820457e1a0aece2dbf80a726206cf4df299afa09f9d8f3dcd85ff39bb4b3f0402a3a6af2f5"),
		},
	} {
		if have, _ := rlp.EncodeToBytes(tc.message); !bytes.Equal(have, tc.want) {
			t.Errorf("test %d, type %T, have\n\t%x\nwant\n\t%x", i, tc.message, have, tc.want)
		}
	}
}
