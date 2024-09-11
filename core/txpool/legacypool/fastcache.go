package legacypool

import (
	"fmt"
	"sync"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type LRUBufferFastCache struct {
	cache    *fastcache.Cache
	capacity int
	mu       sync.Mutex
	size     int           // Total number of slots used
	order    []common.Hash // Tracks the order of added transactions for LRU eviction
}

// SerializeTransaction converts a transaction to a byte slice using RLP or any other encoding
func SerializeTransaction(tx *types.Transaction) []byte {
	data, err := tx.MarshalBinary() // Ethereum uses RLP for transactions, so we can use this function
	if err != nil {
		return nil
	}
	return data
}

// DeserializeTransaction converts a byte slice back to a transaction
func DeserializeTransaction(data []byte) (*types.Transaction, error) {
	var tx types.Transaction
	err := tx.UnmarshalBinary(data)
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

// LRUBufferFastCache initializes an LRU buffer with a given capacity
func NewLRUBufferFastCache(capacity int) *LRUBufferFastCache {
	return &LRUBufferFastCache{
		cache:    fastcache.New(capacity * 1024 * 1024), // fastcache size is in bytes
		capacity: capacity,
		order:    make([]common.Hash, 0),
	}
}

func (lru *LRUBufferFastCache) Add(tx *types.Transaction) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	txHash := tx.Hash()

	// Check if the transaction already exists
	if lru.cache.Has(txHash.Bytes()) {
		// Move the transaction to the front in LRU order
		lru.moveToFront(txHash)
		return
	}

	txSlots := numSlots(tx)

	// Evict the oldest transactions if the new one doesn't fit
	for lru.size+txSlots > lru.capacity && len(lru.order) > 0 {
		lru.evictOldest()
	}

	// Add the transaction to the cache
	txData := SerializeTransaction(tx)
	lru.cache.Set(txHash.Bytes(), txData)
	lru.size += txSlots

	// Update pool3Gauge
	pool3Gauge.Inc(1)

	// Add to the order tracking
	lru.order = append(lru.order, txHash)
}

// Evict the oldest transaction in the LRU order
func (lru *LRUBufferFastCache) evictOldest() {
	oldestHash := lru.order[0]
	lru.order = lru.order[1:]

	// Remove from the cache
	txData := lru.cache.Get(nil, oldestHash.Bytes())
	if len(txData) > 0 {
		tx, err := DeserializeTransaction(txData)
		if err == nil {
			lru.size -= numSlots(tx)
		}
	}

	// Remove the oldest entry
	lru.cache.Del(oldestHash.Bytes())

	// Update pool3Gauge
	pool3Gauge.Dec(1)
}

// Move a transaction to the front of the LRU order
func (lru *LRUBufferFastCache) moveToFront(hash common.Hash) {
	for i, h := range lru.order {
		if h == hash {
			// Remove the hash from its current position
			lru.order = append(lru.order[:i], lru.order[i+1:]...)
			break
		}
	}
	// Add it to the front
	lru.order = append(lru.order, hash)
}

// Get retrieves a transaction from the cache and moves it to the front of the LRU order
func (lru *LRUBufferFastCache) Get(hash common.Hash) (*types.Transaction, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	txData := lru.cache.Get(nil, hash.Bytes())
	if len(txData) == 0 {
		return nil, false
	}

	tx, err := DeserializeTransaction(txData)
	if err != nil {
		return nil, false
	}

	// Move the accessed transaction to the front in LRU order
	lru.moveToFront(hash)

	return tx, true
}

// Flush removes and returns up to `maxTransactions` transactions from the cache
func (lru *LRUBufferFastCache) Flush(maxTransactions int) []*types.Transaction {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	var txs []*types.Transaction
	count := 0

	// Remove up to maxTransactions transactions from the oldest
	for count < maxTransactions && len(lru.order) > 0 {
		oldestHash := lru.order[0]
		lru.order = lru.order[1:]

		txData := lru.cache.Get(nil, oldestHash.Bytes())
		if len(txData) > 0 {
			tx, err := DeserializeTransaction(txData)
			if err == nil {
				txs = append(txs, tx)
				lru.size -= numSlots(tx)
				count++
			}
		}

		lru.cache.Del(oldestHash.Bytes())
		// Update pool3Gauge
		pool3Gauge.Dec(1)
	}

	return txs
}

// Size returns the current size of the buffer in terms of slots
func (lru *LRUBufferFastCache) Size() int {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	return lru.size
}

// PrintTxStats prints the hash, gas fee cap, and gas tip cap of all transactions
func (lru *LRUBufferFastCache) PrintTxStats() {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	for _, hash := range lru.order {
		txData := lru.cache.Get(nil, hash.Bytes())
		if len(txData) > 0 {
			tx, err := DeserializeTransaction(txData)
			if err == nil {
				fmt.Println(tx.Hash().String(), tx.GasFeeCap().String(), tx.GasTipCap().String())
			}
		}
	}
}
