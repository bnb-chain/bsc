package legacypool

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// txHeapItem implements the Interface interface (https://pkg.go.dev/container/heap#Interface) of heap so that it can be heapified
type txHeapItem struct {
	tx        *types.Transaction
	timestamp int64 // Unix timestamp (nanoseconds) of when the transaction was added
	index     int
}

type txHeap []*txHeapItem

func (h txHeap) Len() int { return len(h) }
func (h txHeap) Less(i, j int) bool {
	return h[i].timestamp < h[j].timestamp
}
func (h txHeap) Swap(i, j int) {
	if i < 0 || j < 0 || i >= len(h) || j >= len(h) {
		return // Silently fail if indices are out of bounds
	}
	h[i], h[j] = h[j], h[i]
	if h[i] != nil {
		h[i].index = i
	}
	if h[j] != nil {
		h[j].index = j
	}
}

func (h *txHeap) Push(x interface{}) {
	item, ok := x.(*txHeapItem)
	if !ok {
		return
	}
	n := len(*h)
	item.index = n
	*h = append(*h, item)
}

func (h *txHeap) Pop() interface{} {
	old := *h
	n := len(old)
	if n == 0 {
		return nil // Return nil if the heap is empty
	}
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[0 : n-1]
	if item != nil {
		item.index = -1 // for safety
	}
	return item
}

type TxOverflowPool struct {
	txHeap    txHeap
	index     map[common.Hash]*txHeapItem
	mu        sync.RWMutex
	maxSize   uint64 // Maximum slots
	totalSize uint64 // Total number of slots currently
}

func NewTxOverflowPoolHeap(estimatedMaxSize uint64) *TxOverflowPool {
	return &TxOverflowPool{
		txHeap:  make(txHeap, 0, estimatedMaxSize),
		index:   make(map[common.Hash]*txHeapItem, estimatedMaxSize),
		maxSize: estimatedMaxSize,
	}
}

func (tp *TxOverflowPool) Add(tx *types.Transaction) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if _, exists := tp.index[tx.Hash()]; exists {
		// Transaction already in pool, ignore
		return false
	}

	txSlots := uint64(numSlots(tx))

	// If the transaction is too big to ever fit (and the pool isn't empty right now), reject it
	if (txSlots > tp.maxSize) || (txSlots == tp.maxSize && tp.totalSize != 0) {
		log.Warn("Transaction too large to fit in OverflowPool", "transaction", tx.Hash().String(), "requiredSlots", txSlots, "maxSlots", tp.maxSize)
		return false
	}

	// Remove transactions until there is room for the new transaction
	for tp.totalSize+txSlots > tp.maxSize {
		if tp.txHeap.Len() == 0 {
			// No transactions left to remove, cannot make room
			log.Warn("Not enough space in OverflowPool even after clearing", "transaction", tx.Hash().String())
			return false
		}
		// Remove the oldest transaction
		oldestItem, ok := heap.Pop(&tp.txHeap).(*txHeapItem)
		if !ok || oldestItem == nil {
			log.Error("Failed to pop from txHeap during Add")
			return false
		}
		delete(tp.index, oldestItem.tx.Hash())
		tp.totalSize -= uint64(numSlots(oldestItem.tx))
		OverflowPoolGauge.Dec(1)
	}

	// Add the new transaction
	item := &txHeapItem{
		tx:        tx,
		timestamp: time.Now().UnixNano(),
	}
	heap.Push(&tp.txHeap, item)
	tp.index[tx.Hash()] = item
	tp.totalSize += txSlots
	OverflowPoolGauge.Inc(1)

	return true
}

func (tp *TxOverflowPool) Get(hash common.Hash) (*types.Transaction, bool) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	if item, ok := tp.index[hash]; ok {
		return item.tx, true
	}
	return nil, false
}

func (tp *TxOverflowPool) Remove(hash common.Hash) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if item, ok := tp.index[hash]; ok {
		heap.Remove(&tp.txHeap, item.index)
		delete(tp.index, hash)
		tp.totalSize -= uint64(numSlots(item.tx))
		OverflowPoolGauge.Dec(1)
	}
}

func (tp *TxOverflowPool) Flush(n int) []*types.Transaction {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if n > tp.txHeap.Len() {
		n = tp.txHeap.Len()
	}
	txs := make([]*types.Transaction, n)
	for i := 0; i < n; i++ {
		item, ok := heap.Pop(&tp.txHeap).(*txHeapItem)
		if !ok || item == nil {
			continue
		}
		txs[i] = item.tx
		delete(tp.index, item.tx.Hash())
		tp.totalSize -= uint64(numSlots(item.tx))
	}

	OverflowPoolGauge.Dec(int64(n))
	return txs
}

func (tp *TxOverflowPool) Len() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.txHeap.Len()
}

func (tp *TxOverflowPool) Size() uint64 {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.totalSize
}

func (tp *TxOverflowPool) PrintTxStats() {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for _, item := range tp.txHeap {
		tx := item.tx
		fmt.Printf("Hash: %s, Timestamp: %d, GasFeeCap: %s, GasTipCap: %s\n",
			tx.Hash().String(), item.timestamp, tx.GasFeeCap().String(), tx.GasTipCap().String())
	}
}
