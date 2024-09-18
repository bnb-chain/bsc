package legacypool

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// Helper function to create a dummy transaction of specified size
func createDummyTransaction(size int) *types.Transaction {
	data := make([]byte, size)
	return types.NewTransaction(0, common.Address{}, nil, 0, nil, data)
}

func TestNewLRUBuffer(t *testing.T) {
	capacity := 10
	lru := NewLRUBuffer(capacity)

	require.Equal(t, capacity, lru.capacity, "expected capacity to match")
	require.Zero(t, lru.buffer.Len(), "expected buffer length to be zero")
	require.Zero(t, len(lru.index), "expected index length to be zero")
	require.Zero(t, lru.size, "expected size to be zero")
}

func TestAddAndGet(t *testing.T) {
	lru := NewLRUBuffer(10)

	tx1 := createDummyTransaction(500)
	tx2 := createDummyTransaction(1500)

	lru.Add(tx1)
	lru.Add(tx2)

	require.Equal(t, 2, lru.Size(), "expected size to be 2")

	retrievedTx, ok := lru.Get(tx1.Hash())
	require.True(t, ok, "expected to retrieve tx1")
	require.Equal(t, tx1.Hash(), retrievedTx.Hash(), "retrieved tx1 hash does not match")

	retrievedTx, ok = lru.Get(tx2.Hash())
	require.True(t, ok, "expected to retrieve tx2")
	require.Equal(t, tx2.Hash(), retrievedTx.Hash(), "retrieved tx2 hash does not match")
}

func TestBufferCapacity(t *testing.T) {
	lru := NewLRUBuffer(2) // Capacity in slots

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 1 slot
	tx3 := createDummyTransaction(1000) // 1 slot

	lru.Add(tx1)
	lru.Add(tx2)

	require.Equal(t, 2, lru.Size(), "expected size to be 2")

	lru.Add(tx3)

	require.Equal(t, 2, lru.Size(), "expected size to remain 2 after adding tx3")
	_, ok := lru.Get(tx1.Hash())
	require.False(t, ok, "expected tx1 to be evicted")
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

	require.Len(t, flushedTxs, 2, "expected to flush 2 transactions")

	expectedSize := 1
	actualSize := lru.Size()
	require.Equal(t, expectedSize, actualSize, "expected size after flush to match")
}

func TestSize(t *testing.T) {
	lru := NewLRUBuffer(10)

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 2 slots

	lru.Add(tx1)
	require.Equal(t, 1, lru.Size(), "expected size to be 1")

	lru.Add(tx2)
	require.Equal(t, 2, lru.Size(), "expected size to be 2")

	lru.Flush(1)
	require.Equal(t, 1, lru.Size(), "expected size to be 1 after flush")
}
