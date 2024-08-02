package legacypool

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Helper function to create a dummy transaction of specified size
func createDummyTransaction(size int) *types.Transaction {
	data := make([]byte, size)
	return types.NewTransaction(0, common.Address{}, nil, 0, nil, data)
}

func TestNewLRUBuffer(t *testing.T) {
	capacity := 10
	lru := NewLRUBuffer(capacity)
	if lru.capacity != capacity {
		t.Errorf("expected capacity %d, got %d", capacity, lru.capacity)
	}
	if lru.buffer.Len() != 0 {
		t.Errorf("expected buffer length 0, got %d", lru.buffer.Len())
	}
	if len(lru.index) != 0 {
		t.Errorf("expected index length 0, got %d", len(lru.index))
	}
	if lru.size != 0 {
		t.Errorf("expected size 0, got %d", lru.size)
	}
}

func TestAddAndGet(t *testing.T) {
	lru := NewLRUBuffer(10)

	tx1 := createDummyTransaction(500)
	tx2 := createDummyTransaction(1500)

	lru.Add(tx1)
	lru.Add(tx2)

	if lru.Size() != 2 {
		t.Errorf("expected size 2, got %d", lru.Size())
	}

	retrievedTx, ok := lru.Get(tx1.Hash())
	if !ok || retrievedTx.Hash() != tx1.Hash() {
		t.Errorf("failed to retrieve tx1")
	}

	retrievedTx, ok = lru.Get(tx2.Hash())
	if !ok || retrievedTx.Hash() != tx2.Hash() {
		t.Errorf("failed to retrieve tx2")
	}
}

func TestBufferCapacity(t *testing.T) {
	lru := NewLRUBuffer(2) // Capacity in slots

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 1 slot
	tx3 := createDummyTransaction(1000) // 1 slot

	lru.Add(tx1)
	lru.Add(tx2)

	if lru.Size() != 2 {
		t.Errorf("expected size 2, got %d", lru.Size())
	}

	lru.Add(tx3)

	if lru.Size() != 2 {
		t.Errorf("expected size 2 after adding tx3, got %d", lru.Size())
	}

	if _, ok := lru.Get(tx1.Hash()); ok {
		t.Errorf("expected tx1 to be evicted")
	}
}

func TestFlush(t *testing.T) {
	lru := NewLRUBuffer(10)

	tx1 := createDummyTransaction(500)
	tx2 := createDummyTransaction(1500)
	tx3 := createDummyTransaction(1000)

	lru.Add(tx1)
	lru.Add(tx2)
	lru.Add(tx3)

	flushedTxs := lru.Flush(2)

	if len(flushedTxs) != 2 {
		t.Errorf("expected to flush 2 transactions, got %d", len(flushedTxs))
	}

	expectedSize := 1
	actualSize := lru.Size()
	if expectedSize != actualSize {
		t.Errorf("expected size after flush %d, got %d", expectedSize, actualSize)
	}
}

func TestSize(t *testing.T) {
	lru := NewLRUBuffer(10)

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 2 slots

	lru.Add(tx1)
	if lru.Size() != 1 {
		t.Errorf("expected size 1, got %d", lru.Size())
	}

	lru.Add(tx2)
	if lru.Size() != 2 {
		t.Errorf("expected size 2, got %d", lru.Size())
	}

	lru.Flush(1)
	if lru.Size() != 1 {
		t.Errorf("expected size 1 after flush, got %d", lru.Size())
	}
}
