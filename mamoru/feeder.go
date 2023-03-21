package mamoru

import (
	"math/big"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/evm_types"
	"github.com/ethereum/go-ethereum/core/types"
)

type Feeder interface {
	FeedBlock(*types.Block) evm_types.Block
	FeedTransactions(blockNumber *big.Int, txs types.Transactions, receipts types.Receipts) []evm_types.Transaction
	FeedEvents(types.Receipts) []evm_types.Event
	FeedCallTraces([]*CallFrame, uint64) []evm_types.CallTrace
}
