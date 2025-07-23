package pathdb

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

func TestComputedRLPEncodedSize(t *testing.T) {
	jn := []journalNodes{
		{
			Owner: common.Hash{0x01, 0x02, 0x03},
			Nodes: []journalNode{
				{Path: []byte{0x01, 0x02}, Blob: make([]byte, 100)},
				{Path: []byte{0x03, 0x04}, Blob: make([]byte, 200)},
			},
		},
		{
			Owner: common.Hash{0x04, 0x05, 0x06},
			Nodes: []journalNode{
				{Path: []byte{0x05, 0x06}, Blob: make([]byte, 150)},
			},
		},
	}

	computedSize := computeRLPEncodedSize(jn)
	actualSize := getActualRLPSize(jn)
	if computedSize != actualSize {
		t.Errorf("ComputedSize is not equal to actual size: computed=%d, actual=%d",
			computedSize, actualSize)
	}
}

func TestComputeNodeSize(t *testing.T) {
	testCases := []struct {
		name     string
		path     []byte
		blob     []byte
		expected uint64
	}{
		{"small", []byte{0x01}, []byte{0x02}, 3},              // 1 + 1 + list overhead
		{"medium", make([]byte, 32), make([]byte, 100), 137},  // 32 + 100 + list overhead
		{"large", make([]byte, 64), make([]byte, 1000), 1072}, // 64 + 1000 + list overhead
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := journalNode{Path: tc.path, Blob: tc.blob}
			computedSize := computeNodeSize(node)
			if computedSize == 0 {
				t.Error("Computed size should not be zero")
			}
			if computedSize < tc.expected {
				t.Errorf("Computed size too small: got %d, expected at least %d", computedSize, tc.expected)
			}
		})
	}
}

func TestComputeEntrySize(t *testing.T) {
	entry := journalNodes{
		Owner: common.Hash{0x01, 0x02, 0x03},
		Nodes: []journalNode{
			{Path: []byte{0x01, 0x02}, Blob: make([]byte, 100)},
			{Path: []byte{0x03, 0x04}, Blob: make([]byte, 200)},
		},
	}

	computedSize := computeEntrySize(entry)
	actualRLPSize := getActualRLPSize(entry)
	if computedSize != actualRLPSize {
		t.Errorf("Computed size: got %d, expected %d", actualRLPSize, computedSize)
	}
}

func TestFlushToAncientDB(t *testing.T) {
	hash := common.HexToHash("0x0123456789")
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	nodes[common.Hash{0x01, 0x02, 0x03}] = make(map[string]*trienode.Node)
	nodes[common.Hash{0x01, 0x02, 0x03}]["0x0102"] = &trienode.Node{Hash: hash, Blob: make([]byte, 100)}
	nodes[common.Hash{0x01, 0x02, 0x03}]["0x0304"] = &trienode.Node{Hash: common.Hash{}, Blob: make([]byte, 200)}
	nodes[common.Hash{0x04, 0x05, 0x06}] = make(map[string]*trienode.Node)
	nodes[common.Hash{0x04, 0x05, 0x06}]["0x0506"] = &trienode.Node{Hash: common.Hash{}, Blob: make([]byte, 150)}

	mockFlushToAncientDB(nodes)
	compressedNodes := compressTrieNodes(nodes)
	actual := getActualRLPSize(compressedNodes)
	fmt.Println("actual", actual)
}

func mockFlushToAncientDB(nodes map[common.Hash]map[string]*trienode.Node) {
	batchSize := 10 * 1024 * 1024
	jn := make([]journalNodes, 0, len(nodes))
	totalSize := uint64(0)

	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		ownerSize := rlp.BytesSize(owner[:])
		nodesListSize := uint64(0)

		for path, node := range subset {
			singleNode := journalNode{Path: []byte(path), Blob: node.Blob}
			entry.Nodes = append(entry.Nodes, singleNode)

			nodeSize := computeNodeSize(singleNode)
			nodesListSize += nodeSize

			batchTotalSize := totalSize + nodeSize + ownerSize
			if rlp.ListSize(batchTotalSize) >= uint64(batchSize) {
				fmt.Println("flush to ancient db when iterating", batchTotalSize)
				jn = make([]journalNodes, 0, len(nodes))
				totalSize = 0
				entry = journalNodes{Owner: owner}        // Reset entry for remaining nodes
				ownerSize = rlp.BytesSize(entry.Owner[:]) // Reset entry size
				nodesListSize = 0
			}
		}

		if len(entry.Nodes) > 0 {
			jn = append(jn, entry)
			entryRLPSize := rlp.ListSize(ownerSize + rlp.ListSize(nodesListSize))
			totalSize += entryRLPSize
		}
		if rlp.ListSize(totalSize) >= uint64(batchSize) {
			fmt.Println("flush to ancient db", totalSize)
			jn = make([]journalNodes, 0, len(nodes))
			totalSize = 0
		}
	}

	finalBatchSize := rlp.ListSize(totalSize)
	fmt.Println("totalSize", totalSize)
	fmt.Println("finalBatchSize", finalBatchSize)
}

func getActualRLPSize(val interface{}) uint64 {
	encoded, err := rlp.EncodeToBytes(val)
	if err != nil {
		panic("rlp.EncodeToBytes failed")
	}
	return uint64(len(encoded))
}

// compressTrieNodes returns a compressed journal nodes slice.
func compressTrieNodes(nodes map[common.Hash]map[string]*trienode.Node) []journalNodes {
	jn := make([]journalNodes, 0, len(nodes))
	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})
		}
		jn = append(jn, entry)
	}
	return jn
}

func TestDetailedSizeComparison(t *testing.T) {
	hash := common.HexToHash("0x0123456789")
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	nodes[common.Hash{0x01, 0x02, 0x03}] = make(map[string]*trienode.Node)
	nodes[common.Hash{0x01, 0x02, 0x03}]["0x0102"] = &trienode.Node{Hash: hash, Blob: make([]byte, 100)}
	nodes[common.Hash{0x01, 0x02, 0x03}]["0x0304"] = &trienode.Node{Hash: common.Hash{}, Blob: make([]byte, 200)}
	nodes[common.Hash{0x04, 0x05, 0x06}] = make(map[string]*trienode.Node)
	nodes[common.Hash{0x04, 0x05, 0x06}]["0x0506"] = &trienode.Node{Hash: common.Hash{}, Blob: make([]byte, 150)}

	fmt.Println("=== 详细对比分析 ===")

	// 方法1：Map 遍历计算
	fmt.Println("\n1. Map 遍历计算:")
	// mapSize := calculateMapSize(nodes)
	mapSize := calculateManualSize(nodes)
	fmt.Printf("Map 遍历计算的总大小: %d\n", mapSize)

	// 方法2：compressTrieNodes 后计算
	fmt.Println("\n2. compressTrieNodes 计算:")
	compressedNodes := compressTrieNodes(nodes)
	compressedSize := getActualRLPSize(compressedNodes)
	fmt.Printf("compressTrieNodes 后的大小: %d\n", compressedSize)

	// 方法3：手动构建相同的 slice
	fmt.Println("\n3. 手动构建 slice:")
	manualNodes := buildManualSlice(nodes)
	manualSize := getActualRLPSize(manualNodes)
	fmt.Printf("手动构建 slice 的大小: %d\n", manualSize)

	// 对比结果
	fmt.Println("\n=== 对比结果 ===")
	fmt.Printf("Map 遍历 vs compressTrieNodes: %d vs %d (差异: %d)\n",
		mapSize, compressedSize, int64(compressedSize)-int64(mapSize))
	fmt.Printf("Map 遍历 vs 手动构建: %d vs %d (差异: %d)\n",
		mapSize, manualSize, int64(manualSize)-int64(mapSize))
	fmt.Printf("compressTrieNodes vs 手动构建: %d vs %d (差异: %d)\n",
		compressedSize, manualSize, int64(manualSize)-int64(compressedSize))

	// 打印详细的数据结构
	fmt.Println("\n=== 数据结构详情 ===")
	fmt.Printf("Map 中的 owner 数量: %d\n", len(nodes))
	for owner, subset := range nodes {
		fmt.Printf("Owner %x: %d nodes\n", owner, len(subset))
		for path, node := range subset {
			fmt.Printf("  Path %s: blob size %d\n", path, len(node.Blob))
		}
	}

	fmt.Printf("\ncompressTrieNodes 结果: %d entries\n", len(compressedNodes))
	for i, entry := range compressedNodes {
		fmt.Printf("Entry %d: Owner %x, %d nodes\n", i, entry.Owner, len(entry.Nodes))
		for j, node := range entry.Nodes {
			fmt.Printf("  Node %d: path %x, blob size %d\n", j, node.Path, len(node.Blob))
		}
	}
}

func calculateMapSize(nodes map[common.Hash]map[string]*trienode.Node) uint64 {
	totalSize := uint64(0)

	for owner, subset := range nodes {
		entrySize := rlp.BytesSize(owner[:]) // Owner size

		for path, node := range subset {
			singleNode := journalNode{Path: []byte(path), Blob: node.Blob}
			nodeSize := computeNodeSize(singleNode)
			entrySize += rlp.ListSize(nodeSize)
		}

		// 每个 entry 是一个 list
		entryRLPSize := rlp.ListSize(entrySize)
		totalSize += entryRLPSize
	}

	// 整个 batch 也是一个 list
	return rlp.ListSize(totalSize)
}

func buildManualSlice(nodes map[common.Hash]map[string]*trienode.Node) []journalNodes {
	jn := make([]journalNodes, 0, len(nodes))

	// 按照与 compressTrieNodes 相同的逻辑构建
	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})
		}
		jn = append(jn, entry)
	}

	return jn
}

func TestRLPEncodingAccuracy(t *testing.T) {
	// 创建一个简单的测试用例
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	nodes[common.Hash{0x01, 0x02, 0x03}] = map[string]*trienode.Node{
		"path1": {Hash: common.Hash{}, Blob: []byte{0x01, 0x02, 0x03}},
	}

	fmt.Println("=== RLP 编码精度测试 ===")

	// 1. 手动计算
	fmt.Println("\n1. 手动计算:")
	manualSize := calculateManualSize(nodes)
	fmt.Printf("手动计算大小: %d\n", manualSize)

	// 2. 使用 computeRLPEncodedSize
	fmt.Println("\n2. 使用 computeRLPEncodedSize:")
	compressedNodes := compressTrieNodes(nodes)
	computedSize := computeRLPEncodedSize(compressedNodes)
	fmt.Printf("computeRLPEncodedSize: %d\n", computedSize)

	// 3. 实际 RLP 编码
	fmt.Println("\n3. 实际 RLP 编码:")
	actualSize := getActualRLPSize(compressedNodes)
	fmt.Printf("实际 RLP 编码大小: %d\n", actualSize)

	// 4. 详细分解
	fmt.Println("\n4. 详细分解:")
	detailedBreakdown(compressedNodes)

	// 对比结果
	fmt.Println("\n=== 对比结果 ===")
	fmt.Printf("手动计算 vs computeRLPEncodedSize: %d vs %d (差异: %d)\n",
		manualSize, computedSize, int64(computedSize)-int64(manualSize))
	fmt.Printf("手动计算 vs 实际编码: %d vs %d (差异: %d)\n",
		manualSize, actualSize, int64(actualSize)-int64(manualSize))
	fmt.Printf("computeRLPEncodedSize vs 实际编码: %d vs %d (差异: %d)\n",
		computedSize, actualSize, int64(actualSize)-int64(computedSize))
}

func calculateManualSize(nodes map[common.Hash]map[string]*trienode.Node) uint64 {
	totalSize := uint64(0)

	for owner, subset := range nodes {
		// Owner size
		ownerSize := rlp.BytesSize(owner[:])
		fmt.Printf("Owner %x: size = %d\n", owner, ownerSize)

		// Nodes list size
		nodesListSize := uint64(0)
		for path, node := range subset {
			// Individual node size
			singleNode := journalNode{Path: []byte(path), Blob: node.Blob}
			// nodePathSize := rlp.BytesSize([]byte(path))
			// nodeBlobSize := rlp.BytesSize(node.Blob)
			// nodeSize := rlp.ListSize(nodePathSize + nodeBlobSize)
			nodeSize := computeNodeSize(singleNode)
			nodesListSize += nodeSize
			// fmt.Printf("  Node %s: path=%d, blob=%d, total=%d\n",
			// 	path, nodePathSize, nodeBlobSize, nodeSize)
		}

		// Entry size (owner + nodes list)
		entrySize := ownerSize + rlp.ListSize(nodesListSize)
		entryRLPSize := rlp.ListSize(entrySize)
		totalSize += entryRLPSize

		fmt.Printf("Entry total: owner=%d + nodes_list=%d = %d, RLP_size=%d\n",
			ownerSize, rlp.ListSize(nodesListSize), entrySize, entryRLPSize)
	}

	// Final batch size
	finalSize := rlp.ListSize(totalSize)
	fmt.Printf("Final batch: total=%d, RLP_size=%d\n", totalSize, finalSize)

	return finalSize
}

func detailedBreakdown(jn []journalNodes) {
	for i, entry := range jn {
		fmt.Printf("Entry %d:\n", i)

		// Owner
		ownerSize := rlp.BytesSize(entry.Owner[:])
		fmt.Printf("  Owner %x: size = %d\n", entry.Owner, ownerSize)

		// Nodes
		nodesListSize := uint64(0)
		for j, node := range entry.Nodes {
			nodePathSize := rlp.BytesSize(node.Path)
			nodeBlobSize := rlp.BytesSize(node.Blob)
			nodeSize := rlp.ListSize(nodePathSize + nodeBlobSize)
			nodesListSize += nodeSize
			fmt.Printf("    Node %d: path=%d, blob=%d, total=%d\n",
				j, nodePathSize, nodeBlobSize, nodeSize)
		}

		// Entry
		entrySize := ownerSize + rlp.ListSize(nodesListSize)
		entryRLPSize := rlp.ListSize(entrySize)
		fmt.Printf("  Entry total: owner=%d + nodes_list=%d = %d, RLP_size=%d\n",
			ownerSize, rlp.ListSize(nodesListSize), entrySize, entryRLPSize)
	}
}
