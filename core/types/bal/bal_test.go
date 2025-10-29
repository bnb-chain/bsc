// Copyright 2025 The go-ethereum Authors
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

package bal

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/internal/testrand"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

func equalBALs(a *BlockAccessList, b *BlockAccessList) bool {
	if !reflect.DeepEqual(a, b) {
		return false
	}
	return true
}

func makeTestConstructionBAL() *ConstructionBlockAccessList {
	return &ConstructionBlockAccessList{
		map[common.Address]*ConstructionAccountAccess{
			common.BytesToAddress([]byte{0xff, 0xff}): {
				StorageWrites: map[common.Hash]map[uint16]common.Hash{
					common.BytesToHash([]byte{0x01}): {
						1: common.BytesToHash([]byte{1, 2, 3, 4}),
						2: common.BytesToHash([]byte{1, 2, 3, 4, 5, 6}),
					},
					common.BytesToHash([]byte{0x10}): {
						20: common.BytesToHash([]byte{1, 2, 3, 4}),
					},
				},
				StorageReads: map[common.Hash]struct{}{
					common.BytesToHash([]byte{1, 2, 3, 4, 5, 6, 7}): {},
				},
				BalanceChanges: map[uint16]*uint256.Int{
					1: uint256.NewInt(100),
					2: uint256.NewInt(500),
				},
				NonceChanges: map[uint16]uint64{
					1: 2,
					2: 6,
				},
				CodeChanges: map[uint16]CodeChange{0: {
					TxIdx: 0,
					Code:  common.Hex2Bytes("deadbeef"),
				}},
			},
			common.BytesToAddress([]byte{0xff, 0xff, 0xff}): {
				StorageWrites: map[common.Hash]map[uint16]common.Hash{
					common.BytesToHash([]byte{0x01}): {
						2: common.BytesToHash([]byte{1, 2, 3, 4, 5, 6}),
						3: common.BytesToHash([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
					},
					common.BytesToHash([]byte{0x10}): {
						21: common.BytesToHash([]byte{1, 2, 3, 4, 5}),
					},
				},
				StorageReads: map[common.Hash]struct{}{
					common.BytesToHash([]byte{1, 2, 3, 4, 5, 6, 7, 8}): {},
				},
				BalanceChanges: map[uint16]*uint256.Int{
					2: uint256.NewInt(100),
					3: uint256.NewInt(500),
				},
				NonceChanges: map[uint16]uint64{
					1: 2,
				},
			},
		},
	}
}

// TestBALEncoding tests that a populated access list can be encoded/decoded correctly.
func TestBALEncoding(t *testing.T) {
	var buf bytes.Buffer
	bal := makeTestConstructionBAL()
	err := bal.EncodeRLP(&buf)
	if err != nil {
		t.Fatalf("encoding failed: %v\n", err)
	}
	var dec BlockAccessList
	if err := dec.DecodeRLP(rlp.NewStream(bytes.NewReader(buf.Bytes()), 10000000)); err != nil {
		t.Fatalf("decoding failed: %v\n", err)
	}
	if dec.Hash() != bal.ToEncodingObj().Hash() {
		t.Fatalf("encoded block hash doesn't match decoded")
	}
	if !equalBALs(bal.ToEncodingObj(), &dec) {
		t.Fatal("decoded BAL doesn't match")
	}
}

func makeTestAccountAccess(sort bool) AccountAccess {
	var (
		storageWrites []encodingSlotWrites
		storageReads  []common.Hash
		balances      []encodingBalanceChange
		nonces        []encodingAccountNonce
	)
	for i := 0; i < 5; i++ {
		slot := encodingSlotWrites{
			Slot: testrand.Hash(),
		}
		for j := 0; j < 3; j++ {
			slot.Accesses = append(slot.Accesses, encodingStorageWrite{
				TxIdx:      uint16(2 * j),
				ValueAfter: testrand.Hash(),
			})
		}
		if sort {
			slices.SortFunc(slot.Accesses, func(a, b encodingStorageWrite) int {
				return cmp.Compare[uint16](a.TxIdx, b.TxIdx)
			})
		}
		storageWrites = append(storageWrites, slot)
	}
	if sort {
		slices.SortFunc(storageWrites, func(a, b encodingSlotWrites) int {
			return bytes.Compare(a.Slot[:], b.Slot[:])
		})
	}

	for i := 0; i < 5; i++ {
		storageReads = append(storageReads, testrand.Hash())
	}
	if sort {
		slices.SortFunc(storageReads, func(a, b common.Hash) int {
			return bytes.Compare(a[:], b[:])
		})
	}

	for i := 0; i < 5; i++ {
		balances = append(balances, encodingBalanceChange{
			TxIdx:   uint16(2 * i),
			Balance: new(uint256.Int).SetBytes(testrand.Bytes(32)),
		})
	}
	if sort {
		slices.SortFunc(balances, func(a, b encodingBalanceChange) int {
			return cmp.Compare[uint16](a.TxIdx, b.TxIdx)
		})
	}

	for i := 0; i < 5; i++ {
		nonces = append(nonces, encodingAccountNonce{
			TxIdx: uint16(2 * i),
			Nonce: uint64(i + 100),
		})
	}
	if sort {
		slices.SortFunc(nonces, func(a, b encodingAccountNonce) int {
			return cmp.Compare[uint16](a.TxIdx, b.TxIdx)
		})
	}

	return AccountAccess{
		Address:        [20]byte(testrand.Bytes(20)),
		StorageChanges: storageWrites,
		StorageReads:   storageReads,
		BalanceChanges: balances,
		NonceChanges:   nonces,
		CodeChanges: []CodeChange{
			{
				TxIdx: 100,
				Code:  testrand.Bytes(256),
			},
		},
	}
}

func makeTestBAL(sort bool) BlockAccessList {
	list := BlockAccessList{}
	for i := 0; i < 5; i++ {
		list = append(list, makeTestAccountAccess(sort))
	}
	if sort {
		slices.SortFunc(list, func(a, b AccountAccess) int {
			return bytes.Compare(a.Address[:], b.Address[:])
		})
	}
	return list
}

func TestBlockAccessListCopy(t *testing.T) {
	list := makeTestBAL(true)
	cpy := list.Copy()
	cpyCpy := cpy.Copy()

	if !reflect.DeepEqual(list, cpy) {
		t.Fatal("block access mismatch")
	}
	if !reflect.DeepEqual(cpy, cpyCpy) {
		t.Fatal("block access mismatch")
	}

	// Make sure the mutations on copy won't affect the origin
	for _, aa := range cpyCpy {
		for i := 0; i < len(aa.StorageReads); i++ {
			aa.StorageReads[i] = [32]byte(testrand.Bytes(32))
		}
	}
	if !reflect.DeepEqual(list, cpy) {
		t.Fatal("block access mismatch")
	}
}

func TestBlockAccessListValidation(t *testing.T) {
	// Validate the block access list after RLP decoding
	enc := makeTestBAL(true)
	if err := enc.Validate(); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}
	var buf bytes.Buffer
	if err := enc.EncodeRLP(&buf); err != nil {
		t.Fatalf("Unexpected encoding error: %v", err)
	}

	var dec BlockAccessList
	if err := dec.DecodeRLP(rlp.NewStream(bytes.NewReader(buf.Bytes()), 0)); err != nil {
		t.Fatalf("Unexpected RLP-decode error: %v", err)
	}
	if err := dec.Validate(); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}

	// Validate the derived block access list
	cBAL := makeTestConstructionBAL()
	listB := cBAL.ToEncodingObj()
	if err := listB.Validate(); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}
}

// BALReader test ideas
// * BAL which doesn't have any pre-tx system contracts should return an empty state diff at idx 0

// 在 /Users/fynn/Workspace/bsc/core/types/bal/bal_test.go 中添加

func DecodeBALFromHex(hexStr string) (*BlockAccessList, error) {
	// 移除可能的 0x 前缀
	if strings.HasPrefix(hexStr, "0x") || strings.HasPrefix(hexStr, "0X") {
		hexStr = hexStr[2:]
	}

	// 解码十六进制字符串为字节
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex string: %w", err)
	}

	// 使用 RLP 解码
	var bal BlockAccessList
	if err := bal.DecodeRLP(rlp.NewStream(bytes.NewReader(data), uint64(len(data)))); err != nil {
		return nil, fmt.Errorf("failed to decode RLP: %w", err)
	}

	return &bal, nil
}

// 辅助函数：打印 BAL 的详细信息
func PrintBAL(bal *BlockAccessList) {
	fmt.Printf("BlockAccessList contains %d accounts:\n", len(*bal))
	for i, access := range *bal {
		fmt.Printf("\n[%d] Account: %s\n", i, access.Address.Hex())

		// 打印存储变化
		if len(access.StorageChanges) > 0 {
			fmt.Printf("  Storage Changes (%d):\n", len(access.StorageChanges))
			for j, sc := range access.StorageChanges {
				fmt.Printf("    [%d] Slot: %s\n", j, sc.Slot.Hex())
				for k, acc := range sc.Accesses {
					fmt.Printf("      [%d] TxIdx: %d, Value: %s\n", k, acc.TxIdx, acc.ValueAfter.Hex())
				}
			}
		}

		// 打印余额变化
		if len(access.BalanceChanges) > 0 {
			fmt.Printf("  Balance Changes (%d):\n", len(access.BalanceChanges))
			for j, bc := range access.BalanceChanges {
				fmt.Printf("    [%d] TxIdx: %d, Balance: %s\n", j, bc.TxIdx, bc.Balance.String())
			}
		}

		// 打印 nonce 变化
		if len(access.NonceChanges) > 0 {
			fmt.Printf("  Nonce Changes (%d):\n", len(access.NonceChanges))
			for j, nc := range access.NonceChanges {
				fmt.Printf("    [%d] TxIdx: %d, Nonce: %d\n", j, nc.TxIdx, nc.Nonce)
			}
		}

		// 打印代码变化
		if len(access.CodeChanges) > 0 {
			fmt.Printf("  Code Changes (%d):\n", len(access.CodeChanges))
			for j, cc := range access.CodeChanges {
				fmt.Printf("    [%d] TxIdx: %d, Code length: %d\n", j, cc.TxIdx, len(cc.Code))
			}
		}
	}
}

// 测试函数
func TestDecodeBALFromHex(t *testing.T) {
	// 首先编码一个测试 BAL
	hexStr := "0xf90a40da940000000000000000000000000000000000000001c0c0c0c0c0f901df940000000000000000000000000000000000001000f88ef845a00000000000000000000000000000000000000000000000000000000000000003e3e203a00000000000000000000000000000000000000000000000008af71fc88a779524f845a0b10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0d11e3e203a000000000000000000000000000000000000000000000000018270b9d8f2ba13cf90129a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000001a00000000000000000000000000000000000000000000000000000000000000006a0000000000000000000000000000000000000000000000000000000000000000fa00000000000000000000000000000000000000000000000000000000000000014a00000000000000000000000000000000000000000000000000000000000000017a00000000000000000000000000000000000000000000000000000000000000018a0198728b9bd69d370ce008fd7b258794eb5797a01fadff50d453ff7b197597cb9a0b10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0d10cbca03888af71fc88a77973ec0c0e6940000000000000000000000000000000000001002c0c0cccb0389056bc9a5d6c3728415c0c0e794000000000000000000000000000000000000deadc0c0cdcc038a0b7a10f868a0651222b2c0c0f862940000f90827f1c53a10cb7a02335b175320002935f847f845a0000000000000000000000000000000000000000000000000000000000000126de3e280a0ad2482dd694ad2890ffc692c8c0aab6f4a853752ba2232d43e0526adfd65e88cc0c0c0c0f903fa940951ba1bbc1632fd3b6c8a93e71b4db3ad5350bcc0f903dea0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f29ca0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2a8a0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2a9a0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2aaa0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2aba0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2aca0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2ada0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2aea0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2afa0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2b0a0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2b1a0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2b2a0089b3a3d8caac419b4f11346d1353663934f220a7bb9fb6c5f4cf86ea282f2b3a032fc0bf8f957e6e758b584b369f308c218e5c4f2f3c3addc973072160013d188a032fc0bf8f957e6e758b584b369f308c218e5c4f2f3c3addc973072160013d189a043e5e53ce9cbecd36b96591ee51abb94e47dc8d240b677f5ca3b78b0dd8760efa043e5e53ce9cbecd36b96591ee51abb94e47dc8d240b677f5ca3b78b0dd8760f0a05cab65a845147af88fc17fecba1610a2581fb7793a014d661c6ba35646d13dfba088bae08c85def6baa201b16431cb772e655b9a213bad7102f27c89d640c32bfba088bae08c85def6baa201b16431cb772e655b9a213bad7102f27c89d640c32bfca0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed1a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed2a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed3a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed4a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed5a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed6a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed7a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed8a0cebce8186005cb2ecfe592acefaf81b94a9a42d7d1918a069dd43c83c29a7ed9a0e80e65db63c038b19c592fe9ed47a9614234963172f85c7893b45a984c741f4cc0c0c0f90297941616c13350c534634cd0c6cd09b6cf9c7033dda5f90238f845a00000000000000000000000000000000000000000000000000000000000000004e3e202a000000000000000000000000000000000000000000000000000011e185f157200f845a00000000000000000000000000000000000000000000000000000000000000006e3e202a000000000000000000000000000000000000000000000000000027770de6225cff845a00000000000000000000000000000000000000000000000000000000000000007e3e202a0000000000000000000000000000000000000000000000000000000006901b36ff845a00000000000000000000000000000000000000000000000000000000000000008e3e202a0000000000000000000000000000000000000000000000000000000000000f5c0f845a04578ebdb70601c52a0214afed4393abfd95e037c9747b9669f0f926f4acd1061e3e202a00000000000000000000000000000000000000000000000000000000000006de8f845a05e726d109f605da7a8f84777f11702456e48afd954b5112251e3866511168771e3e202a0000000000000000000000000000000000000000000000000000000006901b36ff845a0ecb333463d4aa73559d044b9325f6c27728e7d695a62ddc060f30d7021e7682ae3e202a0000000000000000000000000000000000000000000000000000046b334dd2b00f845a0f38918cc26f03fe7d53e5279807eb428d4622f28757ec5859a496560d4ce3fd9e3e202a0000000000000000000000000000000000000000000000000000000006901b36ff842a00000000000000000000000000000000000000000000000000000000000000005a08877a142d0d3a0ad333e97078ba9c5354cb77819a59971d3d7589214b41444b9c0c0c0da9424c36e9996eb84138ed7caa483b4c59ff7640e5cc0c0c0c0c0ea942daa67d75789fbcfb3dd014d2deccae8759ddbd8c0c0cbca0288099fc8328e51ef00c5c402826dfbc0da945e787a4131cf9fc902c99235df5c8314c816da11c0c0c0c0c0da9477b83a1ac194b1faf266dc66c77d2ea6a6b7f26dc0c0c0c0c0e09490409f56966b5da954166eb005cb1a8790430ba1c0c0c0c6c503831eeb3fc0eb94d0567bb38fa5bad45150026281c43fa6031577b9c0c0cbca0188ce6f47fa777b4100c6c50183067017c0ef94fffffffffffffffffffffffffffffffffffffffec0c0d5c80186e031cd287500c80286e802129f2c00c20380c0c0"

	// 从十六进制解码
	decoded, err := DecodeBALFromHex(hexStr)
	if err != nil {
		t.Fatalf("DecodeBALFromHex failed: %v", err)
	}

	PrintBAL(decoded)
}
