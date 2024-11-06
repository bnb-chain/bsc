package legacypool

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type NonceAwareTxOverflowPool struct {
	mu      sync.RWMutex
	txs     map[common.Address]*list
	signer  types.Signer
	maxSize uint64
	size    int // Total number of slots used
}

func NewNonceAwareTxOverflowPool(maxSize uint64, signer types.Signer) *NonceAwareTxOverflowPool {
	return &NonceAwareTxOverflowPool{
		txs:     make(map[common.Address]*list),
		signer:  signer,
		maxSize: maxSize,
	}
}

func (tp *NonceAwareTxOverflowPool) Add(tx *types.Transaction) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	from, _ := types.Sender(tp.signer, tx)
	lst, exists := tp.txs[from]
	if !exists {
		lst = newList(false) // 'false' indicates non-strict nonce sequence
		tp.txs[from] = lst
	}
	inserted, old := lst.Add(tx, 0)
	if inserted {
		if old != nil {
			tp.size -= numSlots(old)
		}
		tp.size += numSlots(tx)
	}
}

func (tp *NonceAwareTxOverflowPool) Get(hash common.Hash) (*types.Transaction, bool) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for _, lst := range tp.txs {
		if tx := lst.GetByHash(hash); tx != nil {
			return tx, true
		}
	}
	return nil, false
}

func (tp *NonceAwareTxOverflowPool) Remove(hash common.Hash) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for addr, lst := range tp.txs {
		if tx, found := lst.RemoveByHash(hash); found {
			tp.size -= numSlots(tx)
			if lst.Empty() {
				delete(tp.txs, addr)
			}
			return
		}
	}
}

func (tp *NonceAwareTxOverflowPool) Len() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	count := 0
	for _, lst := range tp.txs {
		count += lst.Len()
	}
	return count
}

func (tp *NonceAwareTxOverflowPool) Size() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.size
}

func (tp *NonceAwareTxOverflowPool) GetExecutableTransactions(pendingNonces *noncer, n int) []*types.Transaction {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	var txs []*types.Transaction
	for addr, lst := range tp.txs {
		pendingNonce := pendingNonces.get(addr)
		readyTxs := lst.Ready(pendingNonce)
		for _, tx := range readyTxs {
			txs = append(txs, tx)
			lst.Remove(tx)
			tp.size -= numSlots(tx)
			if len(txs) >= n {
				break
			}
		}
		if lst.Empty() {
			delete(tp.txs, addr)
		}
		if len(txs) >= n {
			break
		}
	}
	return txs
}
