package legacypool

import (
	containerList "container/list"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type LRUBuffer struct {
	capacity int
	buffer   *containerList.List
	index    map[common.Hash]*containerList.Element
	mu       sync.Mutex
	size     int // Total number of slots used
}

func NewLRUBuffer(capacity int) *LRUBuffer {
	return &LRUBuffer{
		capacity: capacity,
		buffer:   containerList.New(),
		index:    make(map[common.Hash]*containerList.Element),
		size:     0, // Initialize size to 0
	}
}

func (lru *LRUBuffer) Add(tx *types.Transaction) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.index[tx.Hash()]; ok {
		lru.buffer.MoveToFront(elem)
		return
	}

	txSlots := numSlots(tx)

	// Remove elements until there is enough capacity
	for lru.size+txSlots > lru.capacity && lru.buffer.Len() > 0 {
		back := lru.buffer.Back()
		removedTx := back.Value.(*types.Transaction)
		lru.buffer.Remove(back)
		delete(lru.index, removedTx.Hash())
		lru.size -= numSlots(removedTx) // Decrease size by the slots of the removed transaction
	}

	elem := lru.buffer.PushFront(tx)
	lru.index[tx.Hash()] = elem
	lru.size += txSlots // Increase size by the slots of the new transaction
	// Update pool3Gauge
	pool3Gauge.Inc(1)
}

func (lru *LRUBuffer) Get(hash common.Hash) (*types.Transaction, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.index[hash]; ok {
		lru.buffer.MoveToFront(elem)
		return elem.Value.(*types.Transaction), true
	}
	return nil, false
}

func (lru *LRUBuffer) Flush(maxTransactions int) []*types.Transaction {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	txs := make([]*types.Transaction, 0, maxTransactions)
	count := 0
	for count < maxTransactions && lru.buffer.Len() > 0 {
		back := lru.buffer.Back()
		removedTx := back.Value.(*types.Transaction)
		txs = append(txs, removedTx)
		lru.buffer.Remove(back)
		delete(lru.index, removedTx.Hash())
		lru.size -= numSlots(removedTx) // Decrease size by the slots of the removed transaction
		count++
		// Update pool3Gauge
		pool3Gauge.Dec(1)
	}
	return txs
}

// New method to get the current size of the buffer in terms of slots
func (lru *LRUBuffer) Size() int {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	return lru.size
}

// New iterator method to iterate over all transactions, ONLY used for printing and debugging
func (lru *LRUBuffer) iterate() <-chan *types.Transaction {
	ch := make(chan *types.Transaction)
	go func() {
		lru.mu.Lock()
		defer lru.mu.Unlock()
		defer close(ch)

		for e := lru.buffer.Front(); e != nil; e = e.Next() {
			ch <- e.Value.(*types.Transaction)
		}
	}()
	return ch
}

func (lru *LRUBuffer) PrintTxStats() {
	// Iterating over the transactions
	for tx := range lru.iterate() {
		// Print transaction details or process them as needed
		fmt.Println(tx.Hash().String(), tx.GasFeeCap().String(), tx.GasTipCap().String())
	}
}
