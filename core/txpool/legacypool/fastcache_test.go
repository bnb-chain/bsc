package legacypool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLRUBufferFastCache(t *testing.T) {
	capacity := 10
	lru := NewLRUBufferFastCache(capacity)

	assert.Equal(t, capacity, lru.capacity, "capacity should match the given value")
	assert.Equal(t, 0, lru.Size(), "size should be 0 for a new buffer")
	assert.Equal(t, 0, len(lru.order), "order should be empty for a new buffer")
}

func TestAddAndGetFastCache(t *testing.T) {
	lru := NewLRUBufferFastCache(10)

	tx1 := createDummyTransaction(500)
	tx2 := createDummyTransaction(1500)

	lru.Add(tx1)
	lru.Add(tx2)

	assert.Equal(t, 2, lru.Size(), "size should be 2 after adding two transactions")

	retrievedTx, ok := lru.Get(tx1.Hash())
	assert.True(t, ok, "tx1 should be found in the buffer")
	assert.Equal(t, tx1.Hash(), retrievedTx.Hash(), "retrieved tx1 hash should match the original hash")

	retrievedTx, ok = lru.Get(tx2.Hash())
	assert.True(t, ok, "tx2 should be found in the buffer")
	assert.Equal(t, tx2.Hash(), retrievedTx.Hash(), "retrieved tx2 hash should match the original hash")
}

func TestBufferCapacityFastCache(t *testing.T) {
	lru := NewLRUBufferFastCache(2) // Capacity in slots

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 1 slot
	tx3 := createDummyTransaction(1000) // 1 slot

	lru.Add(tx1)
	lru.Add(tx2)

	assert.Equal(t, 2, lru.Size(), "size should be 2 after adding two transactions")

	lru.Add(tx3)

	assert.Equal(t, 2, lru.Size(), "size should still be 2 after adding the third transaction")
	_, ok := lru.Get(tx1.Hash())
	assert.False(t, ok, "tx1 should have been evicted")
}

func TestFlushFastCache(t *testing.T) {
	lru := NewLRUBufferFastCache(10)

	tx1 := createDummyTransaction(500)
	tx2 := createDummyTransaction(1500)
	tx3 := createDummyTransaction(1000)

	lru.Add(tx1)
	lru.Add(tx2)
	lru.Add(tx3)

	lru.PrintTxStats()

	flushedTxs := lru.Flush(2)

	assert.Equal(t, 2, len(flushedTxs), "should flush 2 transactions")
	assert.Equal(t, tx1.Hash().String(), flushedTxs[0].Hash().String(), "correct flushed transaction")
	assert.Equal(t, tx2.Hash().String(), flushedTxs[1].Hash().String(), "correct flushed transaction")
	assert.Equal(t, 1, lru.Size(), "size should be 1 after flushing 2 transactions")

	lru.PrintTxStats()
}

func TestSizeFastCache(t *testing.T) {
	lru := NewLRUBufferFastCache(10)

	tx1 := createDummyTransaction(500)  // 1 slot
	tx2 := createDummyTransaction(1500) // 1 slot

	lru.Add(tx1)
	assert.Equal(t, 1, lru.Size(), "size should be 1 after adding tx1")

	lru.Add(tx2)
	assert.Equal(t, 2, lru.Size(), "size should be 2 after adding tx2")

	lru.Flush(1)
	assert.Equal(t, 1, lru.Size(), "size should be 1 after flushing one transaction")
}
