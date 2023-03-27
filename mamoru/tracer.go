package mamoru

import (
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/evm_types"
	"github.com/Mamoru-Foundation/mamoru-sniffer-go/mamoru_sniffer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

var (
	sniffer            *mamoru_sniffer.Sniffer
	SnifferConnectFunc = mamoru_sniffer.Connect
)

func init() {
	if IsSnifferEnable() {
		Connect()
	}
}

type Tracer struct {
	feeder  Feeder
	builder mamoru_sniffer.BlockchainDataCtxBuilder
}

func NewTracer(feeder Feeder) *Tracer {
	builder := mamoru_sniffer.NewBlockchainDataCtxBuilder()
	tr := &Tracer{builder: builder, feeder: feeder}
	return tr
}

func (t *Tracer) FeedBlock(block *types.Block) {
	t.builder.AddData(evm_types.NewBlockData([]evm_types.Block{
		t.feeder.FeedBlock(block),
	}))
}

func (t *Tracer) FeedTransactions(blockNumber *big.Int, txs types.Transactions, receipts types.Receipts) {
	t.builder.AddData(evm_types.NewTransactionData(
		t.feeder.FeedTransactions(blockNumber, txs, receipts),
	))
}

func (t *Tracer) FeedEvents(receipts types.Receipts) {
	t.builder.AddData(evm_types.NewEventData(
		t.feeder.FeedEvents(receipts),
	))
}

func (t *Tracer) FeedCalTraces(callFrames []*CallFrame, blockNumber uint64) {
	t.builder.AddData(evm_types.NewCallTraceData(
		t.feeder.FeedCallTraces(callFrames, blockNumber),
	))
}

func (t *Tracer) Send(start time.Time, blockNumber *big.Int, blockHash common.Hash) {
	if sniffer != nil {
		sniffer.ObserveData(t.builder.Finish(blockNumber.String(), blockHash.String()))
	}
	logCtx := []interface{}{
		"elapsed", common.PrettyDuration(time.Since(start)),
		"number", blockNumber,
		"hash", blockHash,
	}
	log.Info("Mamoru Sniffer finish", logCtx...)
}

func IsSnifferEnable() bool {
	isEnable, ok := os.LookupEnv("MAMORU_SNIFFER_ENABLE")

	return ok && isEnable == "true"
}

func Connect() bool {
	if sniffer != nil {
		return true
	}
	var err error
	sniffer, err = SnifferConnectFunc()
	if err != nil {
		erst := strings.Replace(err.Error(), "\t", "", -1)
		erst = strings.Replace(erst, "\n", "", -1)
		erst = strings.Replace(erst, " ", "", -1)
		log.Error("Mamoru Sniffer connect", "err", erst)
		return false
	}
	return true
}
