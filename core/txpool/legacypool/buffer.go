package legacypool

import (
	containerList "container/list"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type LRUBuffer struct {
	capacity int
	buffer   *containerList.List
	index    map[common.Hash]*containerList.Element
	mu       sync.Mutex
}

func NewLRUBuffer(capacity int) *LRUBuffer {
	return &LRUBuffer{
		capacity: capacity,
		buffer:   containerList.New(),
		index:    make(map[common.Hash]*containerList.Element),
	}
}

func (lru *LRUBuffer) Add(tx *types.Transaction) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.index[tx.Hash()]; ok {
		lru.buffer.MoveToFront(elem)
		return
	}

	if lru.buffer.Len() >= lru.capacity {
		back := lru.buffer.Back()
		lru.buffer.Remove(back)
		delete(lru.index, back.Value.(*types.Transaction).Hash())
	}

	elem := lru.buffer.PushFront(tx)
	lru.index[tx.Hash()] = elem
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
		txs = append(txs, back.Value.(*types.Transaction))
		lru.buffer.Remove(back)
		delete(lru.index, back.Value.(*types.Transaction).Hash())
		count++
	}
	return txs
}
