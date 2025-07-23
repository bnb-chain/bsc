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

func TestComputeEntrySize(t *testing.T) {
	jn := journalNodes{
		Owner: common.Hash{0x01, 0x02, 0x03},
		Nodes: []journalNode{
			{Path: []byte{0x01, 0x02}, Blob: make([]byte, 100)},
		},
	}

	computedSize := computeEntrySize(jn)
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

func getActualRLPSize(val interface{}) uint64 {
	encoded, err := rlp.EncodeToBytes(val)
	if err != nil {
		panic("rlp.EncodeToBytes failed")
	}
	return uint64(len(encoded))
}

func TestAncientDBFlush(t *testing.T) {
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	// add multiple owners, each with different number of nodes
	for i := 0; i < 5; i++ {
		owner := common.Hash{byte(i), 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
		nodes[owner] = make(map[string]*trienode.Node)

		// add different number of nodes to each owner
		nodeCount := 3 + i*2 // 3, 5, 7, 9, 11 nodes
		for j := 0; j < nodeCount; j++ {
			path := fmt.Sprintf("path%d_%d", i, j)
			// create different size of blob
			blobSize := 500 + (i+j)*25 + (i*j)%100 // 50-300 bytes random size
			blob := make([]byte, blobSize)
			for k := range blob {
				blob[k] = byte((i + j + k) % 256) // use different seed to generate data
			}
			nodes[owner][path] = &trienode.Node{
				Hash: common.Hash{},
				Blob: blob,
			}
		}
	}

	fmt.Printf("created %d owners, each with 5 nodes\n", len(nodes))
	mockFlushToAncientDBWithBatchSize(t, nodes, 1000)
	mockFlushToAncientDBWithBatchSize(t, nodes, 100)
}

func mockFlushToAncientDBWithBatchSize(t *testing.T, nodes map[common.Hash]map[string]*trienode.Node, batchSize uint64) {
	jn := make([]journalNodes, 0, len(nodes))
	totalSize := uint64(0)
	flushCount := 0

	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		ownerSize := rlp.BytesSize(entry.Owner[:])
		nodesListSize := uint64(0)

		for path, node := range subset {
			singleNode := journalNode{Path: []byte(path), Blob: node.Blob}
			entry.Nodes = append(entry.Nodes, singleNode)

			nodeSize := computeNodeSize(singleNode)
			nodesListSize += nodeSize

			currentEntrySize := rlp.ListSize(ownerSize + rlp.ListSize(nodesListSize))
			newTotalSize := totalSize + currentEntrySize
			if rlp.ListSize(newTotalSize) >= batchSize {
				batchToWrite := append(jn, entry)
				computedSize := computeRLPEncodedSize(batchToWrite)
				actualSize := getActualRLPSize(batchToWrite)
				flushCount++

				t.Logf("Flush #%d: when iterating node: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d, computedSize=%d\n",
					flushCount, rlp.ListSize(newTotalSize), batchSize, len(jn)+1, actualSize, computedSize)
				if computedSize != actualSize && computedSize != rlp.ListSize(newTotalSize) {
					t.Errorf("Flush #%d: when iterating node, size is not consistent: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d, computedSize=%d\n",
						flushCount, rlp.ListSize(newTotalSize), batchSize, len(jn)+1, actualSize, computedSize)
				}

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

		if rlp.ListSize(totalSize) >= batchSize {
			actualSize := getActualRLPSize(jn)
			flushCount++
			t.Logf("Flush #%d: when adding entry: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d\n",
				flushCount, rlp.ListSize(totalSize), batchSize, len(jn), actualSize)
			if actualSize != rlp.ListSize(totalSize) {
				t.Errorf("Flush #%d: when adding entry, size is not consistent: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d\n",
					flushCount, rlp.ListSize(totalSize), batchSize, len(jn), actualSize)
			}

			jn = make([]journalNodes, 0, len(nodes))
			totalSize = 0
		}
	}

	// verify final batch size is within limit
	finalBatchSize := rlp.ListSize(totalSize)
	if finalBatchSize > batchSize {
		t.Errorf("Final batch size %d exceeds limit %d", finalBatchSize, batchSize)
	} else {
		t.Logf("Final batch size %d is within limit %d\n", finalBatchSize, batchSize)
	}
}

func TestFlush(t *testing.T) {
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	// add multiple owners, each with different number of nodes
	for i := 0; i < 5; i++ {
		owner := common.Hash{byte(i), 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
		nodes[owner] = make(map[string]*trienode.Node)

		// add different number of nodes to each owner
		nodeCount := 3 + i*2 // 3, 5, 7, 9, 11 nodes
		for j := 0; j < nodeCount; j++ {
			path := fmt.Sprintf("path%d_%d", i, j)
			// create different size of blob
			blobSize := 50000000 + (i+j)*25 + (i*j)%100 // 50-300 bytes random size
			blob := make([]byte, blobSize)
			for k := range blob {
				blob[k] = byte((i + j + k) % 256) // use different seed to generate data
			}
			nodes[owner][path] = &trienode.Node{
				Hash: common.Hash{},
				Blob: blob,
			}
		}
	}

	batchSize := uint64(1000)

	jn := make([]journalNodes, 0, len(nodes))
	totalSize := uint64(0)
	flushCount := 0

	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		ownerSize := uint64(len(owner[:]))
		nodesListSize := uint64(0)

		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})

			nodeSize := uint64(len([]byte(path)) + len(node.Blob))
			nodesListSize += nodeSize
			currentEntrySize := ownerSize + nodesListSize
			newTotalSize := totalSize + currentEntrySize
			if newTotalSize >= batchSize {
				batchToWrite := append(jn, entry)
				computedSize := computeRLPEncodedSize(batchToWrite)
				actualSize := getActualRLPSize(batchToWrite)
				flushCount++

				t.Logf("Flush #%d: when iterating node: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d, computedSize=%d\n",
					flushCount, newTotalSize, batchSize, len(jn)+1, actualSize, computedSize)
				if computedSize != actualSize && computedSize != rlp.ListSize(newTotalSize) {
					t.Errorf("Flush #%d: when iterating node, size is not consistent: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d, computedSize=%d\n",
						flushCount, newTotalSize, batchSize, len(jn)+1, actualSize, computedSize)
				}

				jn = make([]journalNodes, 0, len(nodes))
				totalSize = 0
				entry = journalNodes{Owner: owner} // Reset entry for remaining nodes
				ownerSize = uint64(len(owner[:]))  // Reset entry size
				nodesListSize = 0
			}
		}

		if len(entry.Nodes) > 0 {
			jn = append(jn, entry)
			entrySize := ownerSize + nodesListSize
			totalSize += uint64(entrySize)
		}

		if totalSize >= batchSize {
			actualSize := getActualRLPSize(jn)
			flushCount++
			t.Logf("Flush #%d: when adding entry: currentSize=%d, batchSize=%d, entryCount=%d, actualSize=%d\n",
				flushCount, totalSize, batchSize, len(jn), actualSize)

			jn = make([]journalNodes, 0, len(nodes))
			totalSize = 0
		}
	}
}
