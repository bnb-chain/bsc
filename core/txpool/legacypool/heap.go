package legacypool

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// txHeapItem implements the Interface interface of heap so that it can be heapified
type txHeapItem struct {
	tx        *types.Transaction
	timestamp int64  // Unix timestamp of when the transaction was added
	sequence  uint64 // Unique, monotonically increasing sequence number
	index     int
}

type txHeap []*txHeapItem

func (h txHeap) Len() int { return len(h) }
func (h txHeap) Less(i, j int) bool {
	// Order first by timestamp, then by sequence number if timestamps are equal
	if h[i].timestamp == h[j].timestamp {
		return h[i].sequence < h[j].sequence
	}
	return h[i].timestamp < h[j].timestamp
}
func (h txHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *txHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*txHeapItem)
	item.index = n
	*h = append(*h, item)
}

func (h *txHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*h = old[0 : n-1]
	return item
}

type TxPool3Heap struct {
	txHeap   txHeap
	index    map[common.Hash]*txHeapItem
	mu       sync.RWMutex
	sequence uint64 // Monotonically increasing sequence number
}

func NewTxPool3Heap(estimatedMaxSize uint64) *TxPool3Heap {
	return &TxPool3Heap{
		txHeap:   make(txHeap, 0, estimatedMaxSize),
		index:    make(map[common.Hash]*txHeapItem),
		sequence: 0,
	}
}

func (tp *TxPool3Heap) Add(tx *types.Transaction) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if _, exists := tp.index[tx.Hash()]; exists {
		// Transaction already in pool, ignore
		return
	}

	tp.sequence++
	item := &txHeapItem{
		tx:        tx,
		timestamp: time.Now().Unix(),
		sequence:  tp.sequence,
	}
	heap.Push(&tp.txHeap, item)
	tp.index[tx.Hash()] = item
}

func (tp *TxPool3Heap) Get(hash common.Hash) (*types.Transaction, bool) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if item, ok := tp.index[hash]; ok {
		return item.tx, true
	}
	return nil, false
}

func (tp *TxPool3Heap) Remove(hash common.Hash) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if item, ok := tp.index[hash]; ok {
		heap.Remove(&tp.txHeap, item.index)
		delete(tp.index, hash)
	}
}

func (tp *TxPool3Heap) Flush(n int) []*types.Transaction {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if n > tp.txHeap.Len() {
		n = tp.txHeap.Len()
	}

	txs := make([]*types.Transaction, n)
	for i := 0; i < n; i++ {
		item := heap.Pop(&tp.txHeap).(*txHeapItem)
		txs[i] = item.tx
		delete(tp.index, item.tx.Hash())
	}

	return txs
}

func (tp *TxPool3Heap) Len() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.txHeap.Len()
}

func (tp *TxPool3Heap) Size() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	totalSize := 0
	for _, item := range tp.txHeap {
		totalSize += numSlots(item.tx)
	}

	return totalSize
}

func (tp *TxPool3Heap) PrintTxStats() {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	for _, item := range tp.txHeap {
		tx := item.tx
		fmt.Printf("Hash: %s, Timestamp: %d, Sequence: %d, GasFeeCap: %s, GasTipCap: %s\n",
			tx.Hash().String(), item.timestamp, item.sequence, tx.GasFeeCap().String(), tx.GasTipCap().String())
	}
}
