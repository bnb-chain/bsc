// Copyright 2024 The go-ethereum Authors
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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package snapshot

import (
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/VictoriaMetrics/fastcache"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func newTestLayerTree() *Tree {
	//db := New(rawdb.NewMemoryDatabase(), nil, false)
	//l := newDiskLayer(common.Hash{0x1}, 0, db, nil, newBuffer(0, nil, 0))
	//t := newLayerTree(l)

	// Create an empty base layer and a snapshot tree out of it
	base := &diskLayer{
		diskdb: rawdb.NewMemoryDatabase(),
		root:   common.HexToHash("0x01"),
		cache:  fastcache.New(1024 * 500),
	}
	snaps := &Tree{
		layers: map[common.Hash]snapshot{
			base.root: base,
		},
	}
	snaps.lookup = newLookup(snaps.layers[base.root])
	return snaps
}

func TestLayerCap(t *testing.T) {
	var cases = []struct {
		init     func() *Tree
		head     common.Hash
		layers   int
		base     common.Hash
		snapshot map[common.Hash]struct{}
	}{
		{
			// Chain:
			//   C1->C2->C3->C4 (HEAD)

			init: func() *Tree {
				tr := newTestLayerTree()
				tr.Update(common.Hash{0x2}, common.Hash{0x1}, nil, map[common.Hash][]byte{
					common.HexToHash("0xa1"): randomAccount(),
				}, nil)
				tr.Update(common.Hash{0x3}, common.Hash{0x1}, nil, map[common.Hash][]byte{
					common.HexToHash("0xa2"): randomAccount(),
				}, nil)
				tr.Update(common.Hash{0x4}, common.Hash{0x1}, nil, map[common.Hash][]byte{
					common.HexToHash("0xa3"): randomAccount(),
				}, nil)
				return tr
			},
			// Chain:
			//   C2->C3->C4 (HEAD)
			head:   common.Hash{0x4},
			layers: 2,
			base:   common.Hash{0x2},
			snapshot: map[common.Hash]struct{}{
				common.Hash{0x2}: {},
				common.Hash{0x3}: {},
				common.Hash{0x4}: {},
			},
		},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4}, common.Hash{0x3}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	// Chain:
		//	//   C3->C4 (HEAD)
		//	head:   common.Hash{0x4},
		//	layers: 1,
		//	base:   common.Hash{0x3},
		//	snapshot: map[common.Hash]struct{}{
		//		common.Hash{0x3}: {},
		//		common.Hash{0x4}: {},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4}, common.Hash{0x3}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	// Chain:
		//	//   C4 (HEAD)
		//	head:   common.Hash{0x4},
		//	layers: 0,
		//	base:   common.Hash{0x4},
		//	snapshot: map[common.Hash]struct{}{
		//		common.Hash{0x4}: {},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//     ->C2'->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2a}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2a}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x2b}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2b}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	// Chain:
		//	//   C2->C3->C4 (HEAD)
		//	head:   common.Hash{0x4a},
		//	layers: 2,
		//	base:   common.Hash{0x2a},
		//	snapshot: map[common.Hash]struct{}{
		//		common.Hash{0x4a}: {},
		//		common.Hash{0x3a}: {},
		//		common.Hash{0x2a}: {},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//     ->C2'->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2a}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2a}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x2b}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2b}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	// Chain:
		//	//   C3->C4 (HEAD)
		//	head:   common.Hash{0x4a},
		//	layers: 1,
		//	base:   common.Hash{0x3a},
		//	snapshot: map[common.Hash]struct{}{
		//		common.Hash{0x4a}: {},
		//		common.Hash{0x3a}: {},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//         ->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	// Chain:
		//	//   C2->C3->C4 (HEAD)
		//	//     ->C3'->C4'
		//	head:   common.Hash{0x4a},
		//	layers: 2,
		//	base:   common.Hash{0x2},
		//	snapshot: map[common.Hash]struct{}{
		//		common.Hash{0x4a}: {},
		//		common.Hash{0x3a}: {},
		//		common.Hash{0x4b}: {},
		//		common.Hash{0x3b}: {},
		//		common.Hash{0x2}:  {},
		//	},
		//},
	}
	for _, c := range cases {
		tr := c.init()
		if err := tr.Cap(c.head, c.layers); err != nil {
			t.Fatalf("Failed to cap the layer tree %v", err)
		}
		//if tr.bottom().root != c.base {
		//	t.Fatalf("Unexpected bottom layer tree root, want %v, got %v", c.base, tr.bottom().root)
		//}
		if len(c.snapshot) != len(tr.layers) {
			t.Fatalf("Unexpected layer tree size, want %v, got %v", len(c.snapshot), len(tr.layers))
		}
		for h := range tr.layers {
			if _, ok := c.snapshot[h]; !ok {
				t.Fatalf("Unexpected layer %v", h)
			}
		}
	}
}

func TestDescendant(t *testing.T) {
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelInfo, true)))

	var cases = []struct {
		init      func() *Tree
		snapshotA map[common.Hash]map[common.Hash]struct{}
		op        func(tr *Tree)
		snapshotB map[common.Hash]map[common.Hash]struct{}
	}{
		{
			// Chain:
			//   C1->C2 (HEAD)
			init: func() *Tree {
				tr := newTestLayerTree()
				err := tr.Update(common.Hash{0x2}, common.Hash{0x1}, nil, nil, nil)
				if err != nil {
					fmt.Printf("Update error: %v\n", err)
				}
				return tr
			},
			snapshotA: map[common.Hash]map[common.Hash]struct{}{},
			// Chain:
			//   C1->C2->C3 (HEAD)
			op: func(tr *Tree) {
				err := tr.Update(common.Hash{0x3}, common.Hash{0x2}, nil, nil, nil)
				if err != nil {
					fmt.Printf("Update error: %v\n", err)
				}
			},
			snapshotB: map[common.Hash]map[common.Hash]struct{}{
				common.Hash{0x2}: {
					common.Hash{0x3}: {},
				},
			},
		},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4}, common.Hash{0x3}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2}: {
		//			common.Hash{0x3}: {},
		//			common.Hash{0x4}: {},
		//		},
		//		common.Hash{0x3}: {
		//			common.Hash{0x4}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C2->C3->C4 (HEAD)
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4}, 2)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x3}: {
		//			common.Hash{0x4}: {},
		//		},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4}, common.Hash{0x3}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2}: {
		//			common.Hash{0x3}: {},
		//			common.Hash{0x4}: {},
		//		},
		//		common.Hash{0x3}: {
		//			common.Hash{0x4}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C3->C4 (HEAD)
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4}, 1)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4}, common.Hash{0x3}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2}: {
		//			common.Hash{0x3}: {},
		//			common.Hash{0x4}: {},
		//		},
		//		common.Hash{0x3}: {
		//			common.Hash{0x4}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C4 (HEAD)
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4}, 0)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//     ->C2'->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2a}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2a}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x2b}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2b}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2a}: {
		//			common.Hash{0x3a}: {},
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x3a}: {
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x2b}: {
		//			common.Hash{0x3b}: {},
		//			common.Hash{0x4b}: {},
		//		},
		//		common.Hash{0x3b}: {
		//			common.Hash{0x4b}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C2->C3->C4 (HEAD)
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4a}, 2)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x3a}: {
		//			common.Hash{0x4a}: {},
		//		},
		//	},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//     ->C2'->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2a}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2a}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x2b}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2b}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2a}: {
		//			common.Hash{0x3a}: {},
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x3a}: {
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x2b}: {
		//			common.Hash{0x3b}: {},
		//			common.Hash{0x4b}: {},
		//		},
		//		common.Hash{0x3b}: {
		//			common.Hash{0x4b}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C3->C4 (HEAD)
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4a}, 1)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{},
		//},
		//{
		//	// Chain:
		//	//   C1->C2->C3->C4 (HEAD)
		//	//         ->C3'->C4'
		//	init: func() *layerTree {
		//		tr := newTestLayerTree()
		//		tr.add(common.Hash{0x2}, common.Hash{0x1}, 1, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3a}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4a}, common.Hash{0x3a}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x3b}, common.Hash{0x2}, 2, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		tr.add(common.Hash{0x4b}, common.Hash{0x3b}, 3, trienode.NewMergedNodeSet(), NewStateSetWithOrigin(nil, nil))
		//		return tr
		//	},
		//	snapshotA: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x2}: {
		//			common.Hash{0x3a}: {},
		//			common.Hash{0x4a}: {},
		//			common.Hash{0x3b}: {},
		//			common.Hash{0x4b}: {},
		//		},
		//		common.Hash{0x3a}: {
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x3b}: {
		//			common.Hash{0x4b}: {},
		//		},
		//	},
		//	// Chain:
		//	//   C2->C3->C4 (HEAD)
		//	//     ->C3'->C4'
		//	op: func(tr *layerTree) {
		//		tr.cap(common.Hash{0x4a}, 2)
		//	},
		//	snapshotB: map[common.Hash]map[common.Hash]struct{}{
		//		common.Hash{0x3a}: {
		//			common.Hash{0x4a}: {},
		//		},
		//		common.Hash{0x3b}: {
		//			common.Hash{0x4b}: {},
		//		},
		//	},
		//},
	}
	check := func(setA, setB map[common.Hash]map[common.Hash]struct{}) bool {
		if len(setA) != len(setB) {
			return false
		}
		for h, subA := range setA {
			subB, ok := setB[h]
			if !ok {
				return false
			}
			if len(subA) != len(subB) {
				return false
			}
			for hh := range subA {
				if _, ok := subB[hh]; !ok {
					return false
				}
			}
		}
		return true
	}
	for _, c := range cases {
		tr := c.init()
		if !check(c.snapshotA, tr.lookup.descendants) {
			t.Fatalf("Unexpected descendants")
		}
		c.op(tr)
		if !check(c.snapshotB, tr.lookup.descendants) {
			// 打印 snapshotB 的所有内容
			fmt.Println("snapshotB contents:")
			for _, snapshot := range c.snapshotB {
				fmt.Printf("Snapshot: %v\n", snapshot)
			}

			// 打印 descendants 的所有内容
			fmt.Println("descendants contents:")
			for ancestor, descendants := range tr.lookup.descendants {
				fmt.Printf("Ancestor: %v\n", ancestor)
				for descendant := range descendants {
					fmt.Printf("  Descendant: %v\n", descendant)
				}
			}
			println("snapshotB", c.snapshotB, "descendants", tr.descendants)
			t.Fatalf("Unexpected descendants")
		}
	}
}

func TestSnaphotsDescendants(t *testing.T) {
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelInfo, true)))
	//fmt.Println(t.dump(false))

	// setAccount is a helper to construct a random account entry and assign it to
	// an account slot in a snapshot
	setAccount := func(accKey string) map[common.Hash][]byte {
		return map[common.Hash][]byte{
			common.HexToHash(accKey): randomAccount(),
		}
	}
	makeRoot := func(height uint64) common.Hash {
		var buffer [8]byte
		binary.BigEndian.PutUint64(buffer[:], height)
		return common.BytesToHash(buffer[:])
	}
	// Create a starting base layer and a snapshot tree out of it
	base := &diskLayer{
		diskdb: rawdb.NewMemoryDatabase(),
		root:   makeRoot(1),
		cache:  fastcache.New(1024 * 500),
	}
	snaps := &Tree{
		layers: map[common.Hash]snapshot{
			base.root: base,
		},
		lookup: newLookup(base),
	}
	// Construct the snapshots with 129 layers, flattening whatever's above that
	var (
		last = common.HexToHash("0x01")
		head common.Hash
	)
	for i := 1; i < 150; i++ {
		head = makeRoot(uint64(i + 2))
		snaps.Update(head, last, nil, setAccount(fmt.Sprintf("%d", i+2)), nil)
		last = head
		snaps.Cap(head, 128) // 130 layers (128 diffs + 1 accumulator + 1 disk)
	}

	{
		// flatten 的数据丢弃了 ? 如何找到 ?
		// 测试从0到200的账户哈希
		for i := 3; i <= 200; i++ {
			var lookupAccount *types.SlimAccount
			var err error

			// 将数字转换为十六进制字符串
			accountAddrHash := common.HexToHash(fmt.Sprintf("%d", i))

			// fastpath
			root := head
			targetLayer := snaps.LookupAccount(accountAddrHash, root)
			log.Info("LookupAccount result",
				"index", i,
				"accountAddrHash", accountAddrHash,
				"root", root,
				"targetLayer", targetLayer)
			if targetLayer == nil || reflect.ValueOf(targetLayer).IsNil() {
				log.Info("LookupAccount result targetLayer nil", "targetLayer", targetLayer)
				continue
			}
			// 如果真的不存在, 应该是如何 ? diskLayer 也不存在 ?
			log.Info("CurrentLayerAccount not nil", "index", i, "targetLayer", targetLayer)
			lookupAccount, err = targetLayer.CurrentLayerAccount(accountAddrHash)
			if err != nil {
				log.Info("GlobalLookup.lookupAccount err",
					"hash", accountAddrHash,
					"root", root,
					"err", err)
			}

			ret, err := snaps.Snapshot(head).Account(accountAddrHash)
			if types.AreSlimAccountsEqual(ret, lookupAccount) {
				//t.Errorf("missing account")
				log.Info("Snapshot match",
					"index", i,
					"accountAddrHash", accountAddrHash,
					"lookupAccount", lookupAccount,
					"ret", ret)
			} else {
				t.Errorf("missing account")
				log.Info("Snapshot mismatch",
					"index", i,
					"accountAddrHash", accountAddrHash,
					"lookupAccount", lookupAccount,
					"ret", ret)
			}
		}
	}

	// {
	// 	var lookupAccount *types.SlimAccount
	// 	var err error
	// 	accountAddrHash := common.HexToHash("105")

	// 	//log.Info("stateReader Account 11", "addr", addr, "hash", accountAddrHash)
	// 	{
	// 		// fastpath
	// 		root := head
	// 		//log.Info("stateReader Account", "new root", root, "old root", r.snap.Root())
	// 		targetLayer := snaps.LookupAccount(accountAddrHash, root)
	// 		if targetLayer != nil {
	// 			lookupAccount, err = targetLayer.CurrentLayerAccount(accountAddrHash)
	// 			if err != nil {
	// 				log.Info("GlobalLookup.lookupAccount err", "hash", accountAddrHash, "root", root, "err", err)
	// 			}
	// 			//log.Info("GlobalLookup.lookupAccount", "hash", accountAddrHash, "root", root, "res", lookupData, "targetLayer", targetLayer)
	// 		}
	// 	}

	// 	ret, err := snaps.Snapshot(head).Account(accountAddrHash)
	// 	if ret != lookupAccount {
	// 		log.Info("Snapshot", "accountAddrHash", accountAddrHash, "lookupAccount", lookupAccount, "ret", ret)
	// 	}
	// }

	//var cases = []struct {
	//	headRoot     common.Hash
	//	limit        int
	//	nodisk       bool
	//	expected     int
	//	expectBottom common.Hash
	//}{
	//	{head, 0, false, 0, common.Hash{}},
	//	{head, 64, false, 64, makeRoot(129 + 2 - 64)},
	//	{head, 128, false, 128, makeRoot(3)}, // Normal diff layers, no accumulator
	//	{head, 129, true, 129, makeRoot(2)},  // All diff layers, including accumulator
	//	{head, 130, false, 130, makeRoot(1)}, // All diff layers + disk layer
	//}
	//for i, c := range cases {
	//	layers := snaps.Snapshots(c.headRoot, c.limit, c.nodisk)
	//	if len(layers) != c.expected {
	//		t.Errorf("non-overflow test %d: returned snapshot layers are mismatched, want %v, got %v", i, c.expected, len(layers))
	//	}
	//	if len(layers) == 0 {
	//		continue
	//	}
	//	bottommost := layers[len(layers)-1]
	//	if bottommost.Root() != c.expectBottom {
	//		t.Errorf("non-overflow test %d: snapshot mismatch, want %v, get %v", i, c.expectBottom, bottommost.Root())
	//	}
	//}
	//// Above we've tested the normal capping, which leaves the accumulator live.
	//// Test that if the bottommost accumulator diff layer overflows the allowed
	//// memory limit, the snapshot tree gets capped to one less layer.
	//// Commit the diff layer onto the disk and ensure it's persisted
	//defer func(memcap uint64) { aggregatorMemoryLimit = memcap }(aggregatorMemoryLimit)
	//aggregatorMemoryLimit = 0
	//
	//snaps.Cap(head, 128) // 129 (128 diffs + 1 overflown accumulator + 1 disk)
	//
	//cases = []struct {
	//	headRoot     common.Hash
	//	limit        int
	//	nodisk       bool
	//	expected     int
	//	expectBottom common.Hash
	//}{
	//	{head, 0, false, 0, common.Hash{}},
	//	{head, 64, false, 64, makeRoot(129 + 2 - 64)},
	//	{head, 128, false, 128, makeRoot(3)}, // All diff layers, accumulator was flattened
	//	{head, 129, true, 128, makeRoot(3)},  // All diff layers, accumulator was flattened
	//	{head, 130, false, 129, makeRoot(2)}, // All diff layers + disk layer
	//}
	//for i, c := range cases {
	//	layers := snaps.Snapshots(c.headRoot, c.limit, c.nodisk)
	//	if len(layers) != c.expected {
	//		t.Errorf("overflow test %d: returned snapshot layers are mismatched, want %v, got %v", i, c.expected, len(layers))
	//	}
	//	if len(layers) == 0 {
	//		continue
	//	}
	//	bottommost := layers[len(layers)-1]
	//	if bottommost.Root() != c.expectBottom {
	//		t.Errorf("overflow test %d: snapshot mismatch, want %v, get %v", i, c.expectBottom, bottommost.Root())
	//	}
	//}
}
