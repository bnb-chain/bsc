package legacypool

import (
	rand3 "crypto/rand"
	"math/big"
	rand2 "math/rand"
	"testing"
	"time"

	"github.com/cometbft/cometbft/libs/rand"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

// Helper function to create a test transaction
func createTestTx(nonce uint64, gasPrice *big.Int) *types.Transaction {
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	return types.NewTransaction(nonce, to, big.NewInt(1000), 21000, gasPrice, nil)
}

func TestNewTxOverflowPoolHeap(t *testing.T) {
	pool := NewTxOverflowPoolHeap(0)
	if pool == nil {
		t.Fatal("NewTxOverflowPoolHeap returned nil")
	}
	if pool.Len() != 0 {
		t.Errorf("New pool should be empty, got length %d", pool.Len())
	}
}

func TestTxOverflowPoolHeapAdd(t *testing.T) {
	pool := NewTxOverflowPoolHeap(1)
	tx := createTestTx(1, big.NewInt(1000))

	pool.Add(tx)
	if pool.Len() != 1 {
		t.Errorf("Pool should have 1 transaction, got %d", pool.Len())
	}

	// Add the same transaction again
	pool.Add(tx)
	if pool.Len() != 1 {
		t.Errorf("Pool should still have 1 transaction after adding duplicate, got %d", pool.Len())
	}
}

func TestTxOverflowPoolHeapGet(t *testing.T) {
	pool := NewTxOverflowPoolHeap(1)
	tx := createTestTx(1, big.NewInt(1000))
	pool.Add(tx)

	gotTx, exists := pool.Get(tx.Hash())
	if !exists {
		t.Fatal("Get returned false for existing transaction")
	}
	if gotTx.Hash() != tx.Hash() {
		t.Errorf("Get returned wrong transaction. Want %v, got %v", tx.Hash(), gotTx.Hash())
	}

	_, exists = pool.Get(common.Hash{})
	if exists {
		t.Error("Get returned true for non-existent transaction")
	}
}

func TestTxOverflowPoolHeapRemove(t *testing.T) {
	pool := NewTxOverflowPoolHeap(1)
	tx := createTestTx(1, big.NewInt(1000))
	pool.Add(tx)

	pool.Remove(tx.Hash())
	if pool.Len() != 0 {
		t.Errorf("Pool should be empty after removing the only transaction, got length %d", pool.Len())
	}

	// Try to remove non-existent transaction
	pool.Remove(common.Hash{})
	if pool.Len() != 0 {
		t.Error("Removing non-existent transaction should not affect pool size")
	}
}

func TestTxOverflowPoolHeapPopN(t *testing.T) {
	pool := NewTxOverflowPoolHeap(3)
	tx1 := createTestTx(1, big.NewInt(1000))
	tx2 := createTestTx(2, big.NewInt(2000))
	tx3 := createTestTx(3, big.NewInt(3000))

	pool.Add(tx1)
	time.Sleep(time.Millisecond) // Ensure different timestamps
	pool.Add(tx2)
	time.Sleep(time.Millisecond)
	pool.Add(tx3)

	popped := pool.Flush(2)
	if len(popped) != 2 {
		t.Fatalf("PopN(2) should return 2 transactions, got %d", len(popped))
	}
	if popped[0].Hash() != tx1.Hash() || popped[1].Hash() != tx2.Hash() {
		t.Error("PopN returned transactions in wrong order")
	}
	if pool.Len() != 1 {
		t.Errorf("Pool should have 1 transaction left, got %d", pool.Len())
	}

	// Pop more than available
	popped = pool.Flush(2)
	if len(popped) != 1 {
		t.Fatalf("PopN(2) should return 1 transaction when only 1 is left, got %d", len(popped))
	}
	if popped[0].Hash() != tx3.Hash() {
		t.Error("PopN returned wrong transaction")
	}
	if pool.Len() != 0 {
		t.Errorf("Pool should be empty, got length %d", pool.Len())
	}
}

func TestTxOverflowPoolHeapOrdering(t *testing.T) {
	pool := NewTxOverflowPoolHeap(3)
	tx1 := createTestTx(1, big.NewInt(1000))
	tx2 := createTestTx(2, big.NewInt(2000))
	tx3 := createTestTx(3, big.NewInt(3000))

	pool.Add(tx2)
	time.Sleep(time.Millisecond) // Ensure different timestamps
	pool.Add(tx1)
	pool.Add(tx3) // Added immediately after tx1, should have same timestamp but higher sequence

	popped := pool.Flush(3)
	if len(popped) != 3 {
		t.Fatalf("PopN(3) should return 3 transactions, got %d", len(popped))
	}
	if popped[0].Hash() != tx2.Hash() || popped[1].Hash() != tx1.Hash() || popped[2].Hash() != tx3.Hash() {
		t.Error("Transactions not popped in correct order (earliest timestamp first, then by sequence)")
	}
}

func TestTxOverflowPoolHeapLen(t *testing.T) {
	pool := NewTxOverflowPoolHeap(2)
	if pool.Len() != 0 {
		t.Errorf("New pool should have length 0, got %d", pool.Len())
	}

	pool.Add(createTestTx(1, big.NewInt(1000)))
	if pool.Len() != 1 {
		t.Errorf("Pool should have length 1 after adding a transaction, got %d", pool.Len())
	}

	pool.Add(createTestTx(2, big.NewInt(2000)))
	if pool.Len() != 2 {
		t.Errorf("Pool should have length 2 after adding another transaction, got %d", pool.Len())
	}

	pool.Flush(1)
	if pool.Len() != 1 {
		t.Errorf("Pool should have length 1 after popping a transaction, got %d", pool.Len())
	}
}

func TestTxOverflowPoolSlotCalculation(t *testing.T) {
	// Initialize the pool with a maximum size of 2
	pool := NewTxOverflowPoolHeap(2)

	// Create two transactions with different slot requirements
	tx1 := createTestTx(1, big.NewInt(1000)) // tx1 takes 1 slot
	tx2 := createTestTx(2, big.NewInt(2000)) // tx2 takes 1 slot

	// Add both transactions to fill the pool
	pool.Add(tx1)
	pool.Add(tx2)

	if pool.Len() != 2 {
		t.Fatalf("Expected pool size 2, but got %d", pool.Len())
	}

	dataSize := 40000
	tx3 := createLargeTestTx(
		3,                        // nonce
		big.NewInt(100000000000), // gasPrice: 100 Gwei
		dataSize,
	) // takes 2 slots

	// Create a third transaction with more slots than tx1
	tx3Added := pool.Add(tx3)
	assert.Equal(t, false, tx3Added)
	assert.Equal(t, uint64(2), pool.totalSize)

	// Verify that the pool length remains at 2
	assert.Equal(t, 2, pool.Len(), "Expected pool size 2 after overflow")

	tx4 := createTestTx(4, big.NewInt(3000)) // tx4 takes 1 slot
	// Add tx4 to the pool
	assert.True(t, pool.Add(tx4), "Failed to add tx4")

	// The pool should evict the oldest transaction (tx1) to make room for tx4
	// Verify that tx1 is no longer in the pool
	_, exists := pool.Get(tx1.Hash())
	assert.False(t, exists, "Expected tx1 to be evicted from the pool")
}

func TestBiggerTx(t *testing.T) {
	// Create a transaction with 40KB of data (which should take 2 slots)
	dataSize := 40000
	tx := createLargeTestTx(
		0,                        // nonce
		big.NewInt(100000000000), // gasPrice: 100 Gwei
		dataSize,
	)
	numberOfSlots := numSlots(tx)
	assert.Equal(t, 2, numberOfSlots)
}

// Helper function to create a random test transaction
func createRandomTestTx() *types.Transaction {
	nonce := uint64(rand.Intn(1000000))
	to := common.BytesToAddress(rand.Bytes(20))
	amount := new(big.Int).Rand(rand2.New(rand2.NewSource(rand.Int63())), big.NewInt(1e18))
	gasLimit := uint64(21000)
	gasPrice := new(big.Int).Rand(rand2.New(rand2.NewSource(rand.Int63())), big.NewInt(1e9))
	data := rand.Bytes(100)
	return types.NewTransaction(nonce, to, amount, gasLimit, gasPrice, data)
}

func createRandomTestTxs(n int) []*types.Transaction {
	txs := make([]*types.Transaction, n)
	for i := 0; i < n; i++ {
		txs[i] = createRandomTestTx()
	}
	return txs
}

// createLargeTestTx creates a transaction with a large data payload
func createLargeTestTx(nonce uint64, gasPrice *big.Int, dataSize int) *types.Transaction {
	// Generate random data of specified size
	data := make([]byte, dataSize)
	rand3.Read(data)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Calculate gas needed for the data
	// Gas costs: 21000 (base) + 16 (per non-zero byte) or 4 (per zero byte)
	gasLimit := uint64(21000 + (16 * len(data)))

	return types.NewTransaction(
		nonce,
		to,
		big.NewInt(1000),
		gasLimit,
		gasPrice,
		data,
	)
}

// goos: darwin
// goarch: arm64
// pkg: github.com/ethereum/go-ethereum/core/txpool/legacypool
// BenchmarkTxOverflowPoolHeapAdd-8              	  813326	      2858 ns/op
func BenchmarkTxOverflowPoolHeapAdd(b *testing.B) {
	pool := NewTxOverflowPoolHeap(uint64(b.N))
	txs := createRandomTestTxs(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Add(txs[i])
	}
}

// BenchmarkTxOverflowPoolHeapGet-8              	32613938	        35.63 ns/op
func BenchmarkTxOverflowPoolHeapGet(b *testing.B) {
	pool := NewTxOverflowPoolHeap(1000)
	txs := createRandomTestTxs(1000)
	for _, tx := range txs {
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Get(txs[i%1000].Hash())
	}
}

// BenchmarkTxOverflowPoolHeapRemove-8           	 3020841	       417.8 ns/op
func BenchmarkTxOverflowPoolHeapRemove(b *testing.B) {
	pool := NewTxOverflowPoolHeap(uint64(b.N))
	txs := createRandomTestTxs(b.N)
	for _, tx := range txs {
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Remove(txs[i].Hash())
	}
}

// BenchmarkTxOverflowPoolHeapFlush-8            	42963656	        29.90 ns/op
func BenchmarkTxOverflowPoolHeapFlush(b *testing.B) {
	pool := NewTxOverflowPoolHeap(1000)
	txs := createRandomTestTxs(1000)
	for _, tx := range txs {
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Flush(10)
	}
}

// BenchmarkTxOverflowPoolHeapLen-8              	79147188	        20.07 ns/op
func BenchmarkTxOverflowPoolHeapLen(b *testing.B) {
	pool := NewTxOverflowPoolHeap(1000)
	txs := createRandomTestTxs(1000)
	for _, tx := range txs {
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Len()
	}
}

// BenchmarkTxOverflowPoolHeapAddRemove-8        	  902896	      1546 ns/op
func BenchmarkTxOverflowPoolHeapAddRemove(b *testing.B) {
	pool := NewTxOverflowPoolHeap(uint64(b.N))
	txs := createRandomTestTxs(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Add(txs[i])
		pool.Remove(txs[i].Hash())
	}
}

// BenchmarkTxOverflowPoolHeapAddFlush-8         	   84417	     14899 ns/op
func BenchmarkTxOverflowPoolHeapAddFlush(b *testing.B) {
	pool := NewTxOverflowPoolHeap(uint64(b.N * 10))
	txs := createRandomTestTxs(b.N * 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10; j++ {
			pool.Add(txs[i*10+j])
		}
		pool.Flush(10)
	}
}
