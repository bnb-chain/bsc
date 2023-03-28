package mempool

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/light"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/mamoru"
	"github.com/ethereum/go-ethereum/mamoru/call_tracer"
	"github.com/ethereum/go-ethereum/params"
)

type lightBlockChain interface {
	core.ChainContext

	GetBlockByHash(context.Context, common.Hash) (*types.Block, error)
	CurrentHeader() *types.Header
	Odr() light.OdrBackend

	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
}

type LightSnifferBackend struct {
	txPool      LightTxPool
	chain       lightBlockChain
	chainConfig *params.ChainConfig

	newHeadEvent chan core.ChainHeadEvent
	newTxsEvent  chan core.NewTxsEvent

	TxSub   event.Subscription
	headSub event.Subscription

	ctx context.Context
}

func NewLightSniffer(ctx context.Context, txPool LightTxPool, chain lightBlockChain, chainConfig *params.ChainConfig) *LightSnifferBackend {
	sb := &LightSnifferBackend{
		txPool:       txPool,
		chain:        chain,
		chainConfig:  chainConfig,
		newHeadEvent: make(chan core.ChainHeadEvent, 10),
		newTxsEvent:  make(chan core.NewTxsEvent, 1024),

		ctx: ctx,
	}
	sb.headSub = sb.SubscribeChainHeadEvent(sb.newHeadEvent)
	sb.TxSub = sb.SubscribeNewTxsEvent(sb.newTxsEvent)

	go sb.SnifferLoop()

	return sb
}

func (bc *LightSnifferBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return bc.txPool.SubscribeNewTxsEvent(ch)
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *LightSnifferBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return bc.chain.SubscribeChainHeadEvent(ch)
}

func (bc *LightSnifferBackend) SnifferLoop() {
	ctx, cancel := context.WithCancel(bc.ctx)

	defer func() {
		bc.headSub.Unsubscribe()
		bc.TxSub.Unsubscribe()
	}()

	for {
		select {
		case <-bc.ctx.Done():
		case <-bc.headSub.Err():
		case <-bc.TxSub.Err():
			cancel()
			return

		case newHead := <-bc.newHeadEvent:
			if newHead.Block != nil {
				go bc.processHead(ctx, newHead.Block.Header())
			}
		}
	}
}

func (bc *LightSnifferBackend) processHead(ctx context.Context, head *types.Header) {
	if !mamoru.IsSnifferEnable() || !mamoru.Connect() || ctx.Err() != nil {
		return
	}

	log.Info("Mamoru LightTxPool Sniffer start", "number", head.Number.Uint64(), "ctx", "light txpool")
	startTime := time.Now()

	// Create tracer context
	tracer := mamoru.NewTracer(mamoru.NewFeed(bc.chainConfig))

	parentBlock, err := bc.chain.GetBlockByHash(ctx, head.ParentHash)
	if err != nil {
		log.Error("Mamoru parent block", "number", head.Number.Uint64(), "err", err, "ctx", "light txpool")
		return
	}

	stateDb := light.NewState(ctx, parentBlock.Header(), bc.chain.Odr())

	newBlock, err := bc.chain.GetBlockByHash(ctx, head.Hash())
	if err != nil {
		log.Error("Mamoru current block", "number", head.Number.Uint64(), "err", err, "ctx", "light txpool")
		return
	}
	tracer.FeedBlock(newBlock)

	callFrames, err := call_tracer.TraceBlock(ctx, call_tracer.NewTracerConfig(stateDb.Copy(), bc.chainConfig, bc.chain), newBlock)
	if err != nil {
		log.Error("Mamoru block trace", "number", head.Number.Uint64(), "err", err, "ctx", "light txpool")
		return
	}

	for _, call := range callFrames {
		result := call.Result
		tracer.FeedCalTraces(result, head.Number.Uint64())
	}

	receipts, err := light.GetBlockReceipts(ctx, bc.chain.Odr(), newBlock.Hash(), newBlock.NumberU64())
	if err != nil {
		log.Error("Mamoru block receipt", "number", head.Number.Uint64(), "err", err, "ctx", "light txpool")
		return
	}

	tracer.FeedTransactions(newBlock.Number(), newBlock.Transactions(), receipts)
	tracer.FeedEvents(receipts)

	// finish tracer context
	tracer.Send(startTime, newBlock.Number(), newBlock.Hash(), "light txpool")
}
