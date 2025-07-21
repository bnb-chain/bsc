package pathdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestEstimatedRLPEncodedSize(t *testing.T) {
	cache := newIncrNodeCache(1000, nil, 0)

	// Create test data
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

	// Get estimated size
	estimatedSize := cache.estimatedRLPEncodedSize(jn)

	// Get actual size
	actualSize, err := getActualRLPSize(jn)
	if err != nil {
		t.Fatalf("Failed to get actual RLP size: %v", err)
	}

	// Verify estimation is reasonable (within 10% of actual)
	diff := uint64(0)
	if estimatedSize > actualSize {
		diff = estimatedSize - actualSize
	} else {
		diff = actualSize - estimatedSize
	}

	accuracy := float64(diff) / float64(actualSize)
	if accuracy > 0.1 {
		t.Errorf("Estimation accuracy too low: estimated=%d, actual=%d, accuracy=%.2f%%",
			estimatedSize, actualSize, accuracy*100)
	}

	t.Logf("Estimated size: %d, Actual size: %d, Accuracy: %.2f%%",
		estimatedSize, actualSize, (1-accuracy)*100)
}

func TestRLPSizeCalculation(t *testing.T) {
	cache := newIncrNodeCache(1000, nil, 0)

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

			estimatedSize := cache.estimatedRLPEncodedSize(jn)
			actualSize, err := getActualRLPSize(jn)
			if err != nil {
				t.Fatalf("Failed to get actual size: %v", err)
			}

			// Check if estimated size is reasonable
			if estimatedSize == 0 {
				t.Error("Estimated size should not be zero")
			}

			if estimatedSize < actualSize/2 || estimatedSize > actualSize*2 {
				t.Errorf("Estimation too far off: estimated=%d, actual=%d", estimatedSize, actualSize)
			}

			t.Logf("%s: estimated=%d, actual=%d", tc.name, estimatedSize, actualSize)
		})
	}
}

func TestBatchSizeLimit(t *testing.T) {
	cache := newIncrNodeCache(1000, nil, 0)

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

	estimatedSize := cache.estimatedRLPEncodedSize(largeBatch)

	// Check if the estimated size is reasonable for a large batch
	if estimatedSize < 1000000 { // Should be at least 1MB
		t.Errorf("Estimated size too small for large batch: %d", estimatedSize)
	}

	t.Logf("Large batch estimated size: %d bytes (%.2f MB)",
		estimatedSize, float64(estimatedSize)/(1024*1024))
}

func getActualRLPSize(jn []journalNodes) (uint64, error) {
	encoded, err := rlp.EncodeToBytes(jn)
	if err != nil {
		return 0, err
	}
	return uint64(len(encoded)), nil
}
