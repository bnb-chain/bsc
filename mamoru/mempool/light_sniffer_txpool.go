package mempool

import (
	"context"
	"os"
	"os/signal"
	"syscall"
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
	txPool       LightTxPool
	chain        lightBlockChain
	chainConfig  *params.ChainConfig
	currentBlock *types.Block
	tracer       *mamoru.Tracer

	newHeadEvent chan core.ChainHeadEvent

	TxSub       event.Subscription
	headSub     event.Subscription
	newTxsEvent chan core.NewTxsEvent

	scope event.SubscriptionScope

	ctx context.Context
}

func NewLightSniffer(ctx context.Context, txPool LightTxPool, chain lightBlockChain, chainConfig *params.ChainConfig, tracer *mamoru.Tracer) *LightSnifferBackend {
	sb := &LightSnifferBackend{
		txPool:       txPool,
		chain:        chain,
		chainConfig:  chainConfig,
		newHeadEvent: make(chan core.ChainHeadEvent, 10),
		newTxsEvent:  make(chan core.NewTxsEvent, 1024),
		tracer:       tracer,

		ctx: ctx,
	}
	sb.headSub = sb.SubscribeChainHeadEvent(sb.newHeadEvent)
	sb.TxSub = sb.SubscribeNewTxsEvent(sb.newTxsEvent)
	return sb
}

func (bc *LightSnifferBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return bc.txPool.SubscribeNewTxsEvent(ch)
	//return b.scope.Track(b.feed.Subscribe(ch))
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *LightSnifferBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return bc.chain.SubscribeChainHeadEvent(ch)
}

func (bc *LightSnifferBackend) SnifferLoop() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()

	defer func() {
		bc.headSub.Unsubscribe()
	}()

	var head = bc.chain.CurrentHeader()

	for {
		select {
		case <-bc.ctx.Done():
			return
		case <-bc.headSub.Err():
			return
		case sig := <-sigs:
			log.Info("Signal", "sig", sig)
			return
		case newTx := <-bc.newTxsEvent: //work
			log.Info("Mamoru LightTxPool Sniffer start", "number", head.Number.Uint64())
			log.Info("NewTx Event", "txs", len(newTx.Txs))
			bc.process(ctx, head, newTx.Txs)

		case newHead := <-bc.newHeadEvent:
			if newHead.Block != nil { //work
				head = newHead.Block.Header()
				bc.process(ctx, head, newHead.Block.Transactions())
			}
		}
	}
}

func (bc *LightSnifferBackend) process(ctx context.Context, head *types.Header, txs types.Transactions) {
	log.Info("Mamoru LightTxPool Sniffer start", "number", head.Number.Uint64())
	log.Info("New Header Event receive", "number", head.Number.Uint64())

	if !mamoru.IsSnifferEnable() || !mamoru.Connect() {
		return
	}

	startTime := time.Now()
	stateDb := light.NewState(ctx, head, bc.chain.Odr())

	lastBlock, err := bc.chain.GetBlockByHash(ctx, head.Hash())
	if err != nil {
		log.Error("GetBlockByHash", "err", err)
		return
	}
	callFrames, err := call_tracer.TraceBlock(ctx, call_tracer.NewTracerConfig(stateDb.Copy(), bc.chainConfig, bc.chain), lastBlock)
	if err != nil {
		log.Error("Mamoru Sniffer Tracer Error", "err", err)
		return
	}
	for _, call := range callFrames {
		result := call.Result
		bc.tracer.FeedCalTraces(result, head.Number.Uint64())
	}

	receipts, err := light.GetBlockReceipts(ctx, bc.chain.Odr(), head.Hash(), head.Number.Uint64())
	if err != nil {
		log.Error("Mamoru tracer result", "err", err, "number", head.Number.Uint64())
		return
	}
	log.Info("receipts", "number", head.Number.Uint64(), "len", len(receipts))

	log.Info("Last Block", "number", lastBlock.NumberU64(), "txs", lastBlock.Transactions().Len())
	log.Info("Tracer Transactions", "number", head.Number.Uint64(), "head.txs", len(txs), "receipts", len(receipts))
	bc.tracer.FeedTransactions(head.Number, txs, receipts)
	log.Info("Tracer Events", "receipts", len(receipts))
	bc.tracer.FeedEvents(receipts)
	log.Info("Tracer Send", "number", head.Number.Uint64(), "hash", head.Hash())
	bc.tracer.Send(startTime, head.Number, head.Hash())
}
