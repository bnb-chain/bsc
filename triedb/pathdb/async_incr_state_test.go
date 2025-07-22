package pathdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestComputedRLPEncodedSize(t *testing.T) {
	cache := newIncrNodeCache(1000, 1000000, nil, 0)
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

	computedSize := cache.computeRLPEncodedSize(jn)
	actualSize, err := getActualRLPSize(jn)
	if err != nil {
		t.Fatalf("Failed to get actual RLP size: %v", err)
	}

	// Verify estimation is reasonable (within 10% of actual)
	diff := uint64(0)
	if computedSize > actualSize {
		diff = computedSize - actualSize
	} else {
		diff = actualSize - computedSize
	}

	accuracy := float64(diff) / float64(actualSize)
	if accuracy > 0.1 {
		t.Errorf("Estimation accuracy too low: computed=%d, actual=%d, accuracy=%.2f%%",
			computedSize, actualSize, accuracy*100)
	}
	t.Logf("Computed size: %d, Actual size: %d, Accuracy: %.2f%%",
		computedSize, actualSize, (1-accuracy)*100)
}

func TestComputeNodeSize(t *testing.T) {
	cache := newIncrNodeCache(1000, 1000000, nil, 0)

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
			computedSize := cache.computeNodeSize(node)
			if computedSize == 0 {
				t.Error("Computed size should not be zero")
			}
			if computedSize < tc.expected {
				t.Errorf("Computed size too small: got %d, expected at least %d", computedSize, tc.expected)
			}
			t.Logf("%s: computed=%d", tc.name, computedSize)
		})
	}
}

func TestComputeEntrySize(t *testing.T) {
	cache := newIncrNodeCache(1000, 1000000, nil, 0)

	entry := journalNodes{
		Owner: common.Hash{0x01, 0x02, 0x03},
		Nodes: []journalNode{
			{Path: []byte{0x01, 0x02}, Blob: make([]byte, 100)},
			{Path: []byte{0x03, 0x04}, Blob: make([]byte, 200)},
		},
	}

	computedSize := cache.computeEntrySize(entry)
	if computedSize == 0 {
		t.Error("Computed size should not be zero")
	}
	// Should be at least the sum of individual node sizes plus owner size
	minExpected := uint64(32) // owner
	for _, node := range entry.Nodes {
		minExpected += cache.computeNodeSize(node)
	}
	if computedSize < minExpected {
		t.Errorf("Computed size too small: got %d, expected at least %d", computedSize, minExpected)
	}
	t.Logf("Entry computed size: %d", computedSize)
}

func TestRLPSizeCalculation(t *testing.T) {
	cache := newIncrNodeCache(1000, 1000000, nil, 0)

	// Test with different data sizes
	testCases := []struct {
		name      string
		pathSize  int
		blobSize  int
		nodeCount int
	}{
		{"small", 2, 10, 1},
		{"medium", 32, 100, 5},
		{"large", 64, 1000, 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jn := make([]journalNodes, 1)
			jn[0] = journalNodes{
				Owner: common.Hash{0x01},
				Nodes: make([]journalNode, tc.nodeCount),
			}

			for i := 0; i < tc.nodeCount; i++ {
				jn[0].Nodes[i] = journalNode{
					Path: make([]byte, tc.pathSize),
					Blob: make([]byte, tc.blobSize),
				}
			}

			computedSize := cache.computeRLPEncodedSize(jn)
			actualSize, err := getActualRLPSize(jn)
			if err != nil {
				t.Fatalf("Failed to get actual size: %v", err)
			}
			// Check if estimated size is reasonable
			if computedSize == 0 {
				t.Error("Estimated size should not be zero")
			}
			if computedSize < actualSize/2 || computedSize > actualSize*2 {
				t.Errorf("Estimation too far off: estimated=%d, actual=%d", computedSize, actualSize)
			}
			t.Logf("%s: estimated=%d, actual=%d", tc.name, computedSize, actualSize)
		})
	}
}

func TestBatchSizeLimit(t *testing.T) {
	cache := newIncrNodeCache(1000, 1000000, nil, 0)

	// Create a batch that should exceed the limit
	largeBatch := make([]journalNodes, 1000)
	for i := 0; i < 1000; i++ {
		largeBatch[i] = journalNodes{
			Owner: common.Hash{byte(i)},
			Nodes: []journalNode{
				{Path: make([]byte, 64), Blob: make([]byte, 10000)}, // 10KB per node
			},
		}
	}

	computedSize := cache.computeRLPEncodedSize(largeBatch)
	// Check if the estimated size is reasonable for a large batch
	if computedSize < 1000000 { // Should be at least 1MB
		t.Errorf("Estimated size too small for large batch: %d", computedSize)
	}
	t.Logf("Large batch computed size: %d bytes (%.2f MB)",
		computedSize, float64(computedSize)/(1024*1024))
}

func getActualRLPSize(jn []journalNodes) (uint64, error) {
	encoded, err := rlp.EncodeToBytes(jn)
	if err != nil {
		return 0, err
	}
	return uint64(len(encoded)), nil
}
