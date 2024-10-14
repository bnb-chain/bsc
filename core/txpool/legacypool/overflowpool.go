package legacypool

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// OverflowPool is an interface representing a transaction buffer
type OverflowPool interface {
	Add(tx *types.Transaction)                       // Adds a transaction to the buffer
	Get(hash common.Hash) (*types.Transaction, bool) // Retrieves a transaction by hash
	Flush(maxTransactions int) []*types.Transaction  // Flushes up to maxTransactions transactions
	Size() int                                       // Returns the current size of the buffer
	PrintTxStats()                                   // Prints the statistics of all transactions in the buffer
}
