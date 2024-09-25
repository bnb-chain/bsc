package legacypool

import (
	"math/big"
	rand2 "math/rand"
	"testing"
	"time"

	"github.com/cometbft/cometbft/libs/rand"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Helper function to create a test transaction
func createTestTx(nonce uint64, gasPrice *big.Int) *types.Transaction {
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	return types.NewTransaction(nonce, to, big.NewInt(1000), 21000, gasPrice, nil)
}

func TestNewTxPool3Heap(t *testing.T) {
	pool := NewTxPool3Heap()
	if pool == nil {
		t.Fatal("NewTxPool3Heap returned nil")
	}
	if pool.Len() != 0 {
		t.Errorf("New pool should be empty, got length %d", pool.Len())
	}
}

func TestTxPool3HeapAdd(t *testing.T) {
	pool := NewTxPool3Heap()
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

func TestTxPool3HeapGet(t *testing.T) {
	pool := NewTxPool3Heap()
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

func TestTxPool3HeapRemove(t *testing.T) {
	pool := NewTxPool3Heap()
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

func TestTxPool3HeapPopN(t *testing.T) {
	pool := NewTxPool3Heap()
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

func TestTxPool3HeapOrdering(t *testing.T) {
	pool := NewTxPool3Heap()
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

func TestTxPool3HeapLen(t *testing.T) {
	pool := NewTxPool3Heap()
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

// goos: darwin
// goarch: arm64
// pkg: github.com/ethereum/go-ethereum/core/txpool/legacypool
// BenchmarkTxPool3HeapAdd-8   	   45870	     24270 ns/op
func BenchmarkTxPool3HeapAdd(b *testing.B) {
	pool := NewTxPool3Heap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
	}
}

// BenchmarkTxPool3HeapGet-8   	34522438	        37.05 ns/op
func BenchmarkTxPool3HeapGet(b *testing.B) {
	pool := NewTxPool3Heap()
	txs := make([]*types.Transaction, 1000)
	for i := 0; i < 1000; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
		txs[i] = tx
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Get(txs[i%1000].Hash())
	}
}

// BenchmarkTxPool3HeapRemove-8   	 2643650	       539.2 ns/op
func BenchmarkTxPool3HeapRemove(b *testing.B) {
	pool := NewTxPool3Heap()
	txs := make([]*types.Transaction, b.N)
	for i := 0; i < b.N; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
		txs[i] = tx
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Remove(txs[i].Hash())
	}
}

// BenchmarkTxPool3HeapPopN-8   	47899808	        23.80 ns/op
func BenchmarkTxPool3HeapPopN(b *testing.B) {
	pool := NewTxPool3Heap()
	for i := 0; i < 1000; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Flush(10)
	}
}

// BenchmarkTxPool3HeapLen-8   	86149902	        13.32 ns/op
func BenchmarkTxPool3HeapLen(b *testing.B) {
	pool := NewTxPool3Heap()
	for i := 0; i < 1000; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Len()
	}
}

// BenchmarkTxPool3HeapAddRemove-8   	   46156	     25019 ns/op
func BenchmarkTxPool3HeapAddRemove(b *testing.B) {
	pool := NewTxPool3Heap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := createRandomTestTx()
		pool.Add(tx)
		pool.Remove(tx.Hash())
	}
}

// BenchmarkTxPool3HeapAddPopN-8   	     470	   2377928 ns/op pool.PopN(100)
// BenchmarkTxPool3HeapAddPopN-8   	    4694	    285026 ns/op pool.PopN(10)
func BenchmarkTxPool3HeapAddPopN(b *testing.B) {
	pool := NewTxPool3Heap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10; j++ {
			tx := createRandomTestTx()
			pool.Add(tx)
		}
		pool.Flush(10)
	}
}
