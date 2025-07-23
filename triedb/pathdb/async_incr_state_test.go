package pathdb

import (
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

func mockFlushToAncientDB(nodes map[common.Hash]map[string]*trienode.Node) {
	batchSize := 1024 * 1024
	jn := make([]journalNodes, 0, len(nodes))
	currentBatchSize := uint64(0)
	// totalBatchSize := uint64(0)

	for owner, subset := range nodes {
		entry := journalNodes{Owner: owner}
		currentEntrySize := rlp.BytesSize(entry.Owner[:])
		for path, node := range subset {
			singleNode := journalNode{Path: []byte(path), Blob: node.Blob}
			entry.Nodes = append(entry.Nodes, singleNode)

			nodeSize := computeNodeSize(singleNode)
			currentEntrySize += rlp.ListSize(nodeSize)
			entrySize := rlp.ListSize(currentEntrySize)

			if currentBatchSize+entrySize >= uint64(batchSize) {
				// log.Info("Batch size limit reached during node iteration, flushing to ancient db",
				// 	"currentSize", currentBatchSize+entrySize, "batchSize", batchSize, "entryCount", len(jn)+1)
				jn = make([]journalNodes, 0, len(nodes))
				currentBatchSize = 0
				entry = journalNodes{Owner: owner} // Reset entry for remaining nodes
				currentEntrySize = rlp.BytesSize(entry.Owner[:])
			}
		}

		if len(entry.Nodes) > 0 {
			jn = append(jn, entry)
			currentBatchSize = computeRLPEncodedSize(jn)
		}

		if currentBatchSize >= uint64(batchSize) {
			// log.Info("Batch size limit reached after adding entry, flushing to ancient db",
			// 	"currentSize", currentBatchSize, "batchSize", c.batchSize, "entryCount", len(jn))
			jn = make([]journalNodes, 0, len(nodes))
			currentBatchSize = 0
		}
	}
}

func getActualRLPSize(val interface{}) uint64 {
	encoded, err := rlp.EncodeToBytes(val)
	if err != nil {
		panic("rlp.EncodeToBytes failed")
	}
	return uint64(len(encoded))
}
