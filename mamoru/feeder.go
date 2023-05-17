package mamoru

import (
	"math/big"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/mamoru_sniffer"
	"github.com/ethereum/go-ethereum/core/types"
)

type Feeder interface {
	FeedBlock(*types.Block) mamoru_sniffer.Block
	FeedTransactions(blockNumber *big.Int, txs types.Transactions, receipts types.Receipts) []mamoru_sniffer.Transaction
	FeedEvents(types.Receipts) []mamoru_sniffer.Event
	FeedCallTraces([]*CallFrame, uint64) []mamoru_sniffer.CallTrace
}
