package mamoru

import (
	"math/big"
	"sync"
	"time"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/mamoru_sniffer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type Tracer struct {
	feeder  Feeder
	mu      sync.Mutex
	builder mamoru_sniffer.EvmCtxBuilder
}

func NewTracer(feeder Feeder) *Tracer {
	builder := mamoru_sniffer.NewEvmCtxBuilder()
	tr := &Tracer{builder: builder, feeder: feeder}
	return tr
}

func (t *Tracer) FeedBlock(block *types.Block) {
	defer t.mu.Unlock()
	t.mu.Lock()
	t.builder.SetBlock(
		t.feeder.FeedBlock(block),
	)
}

func (t *Tracer) FeedTransactions(blockNumber *big.Int, txs types.Transactions, receipts types.Receipts) {
	defer t.mu.Unlock()
	t.mu.Lock()
	t.builder.AppendTxs(
		t.feeder.FeedTransactions(blockNumber, txs, receipts),
	)
}

func (t *Tracer) FeedEvents(receipts types.Receipts) {
	defer t.mu.Unlock()
	t.mu.Lock()
	t.builder.AppendEvents(
		t.feeder.FeedEvents(receipts),
	)
}

func (t *Tracer) FeedCalTraces(callFrames []*CallFrame, blockNumber uint64) {
	defer t.mu.Unlock()
	t.mu.Lock()
	t.builder.AppendCallTraces(
		t.feeder.FeedCallTraces(callFrames, blockNumber),
	)
}

func (t *Tracer) Send(start time.Time, blockNumber *big.Int, blockHash common.Hash, snifferContext string) {
	defer t.mu.Unlock()
	t.mu.Lock()

	if sniffer != nil {
		sniffer.ObserveEvmData(t.builder.Finish(blockNumber.String(), blockHash.String()))
	}
	logCtx := []interface{}{
		"elapsed", common.PrettyDuration(time.Since(start)),
		"number", blockNumber,
		"hash", blockHash,
		"ctx", snifferContext,
	}
	log.Info("Mamoru Sniffer finish", logCtx...)
}
