package tracer

import (
	"github.com/Mamoru-Foundation/mamoru-sniffer-go/evm_types"
	"github.com/ethereum/go-ethereum/core/types"
)

type Feeder interface {
	FeedBlock(*types.Block) evm_types.Block
	FeedTransactions(*types.Block, types.Receipts) []evm_types.Transaction
	FeedCalTraces([]*TxTraceResult, uint64) []evm_types.CallTrace
	FeedEvents(types.Receipts) []evm_types.Event
}
