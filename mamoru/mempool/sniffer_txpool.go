package mempool

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/mamoru"
	"github.com/ethereum/go-ethereum/params"
)

type blockChain interface {
	core.ChainContext
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
	State() (*state.StateDB, error)

	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
	SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription
}

type SnifferBackend struct {
	txPool       BcTxPool
	chain        blockChain
	chainConfig  *params.ChainConfig
	currentBlock *types.Block
	tracer       *mamoru.Tracer

	feed      event.Feed
	chainFeed event.Feed

	newHeadEvent chan core.ChainHeadEvent
	newTxsEvent  chan core.NewTxsEvent
	chainEvent   chan core.ChainEvent

	TxSub    event.Subscription
	headSub  event.Subscription
	chainSub event.Subscription

	scope event.SubscriptionScope

	ctx context.Context
}

func NewSniffer(ctx context.Context, txPool BcTxPool, chain blockChain, chainConfig *params.ChainConfig, tracer *mamoru.Tracer) *SnifferBackend {
	sb := &SnifferBackend{
		txPool:       txPool,
		chain:        chain,
		chainConfig:  chainConfig,
		newTxsEvent:  make(chan core.NewTxsEvent, core.DefaultTxPoolConfig.GlobalQueue),
		newHeadEvent: make(chan core.ChainHeadEvent, 10),
		tracer:       tracer,

		chainEvent: make(chan core.ChainEvent, 10),
		ctx:        ctx,
	}
	sb.TxSub = sb.SubscribeNewTxsEvent(sb.newTxsEvent)
	sb.chainSub = sb.SubscribeChainEvent(sb.chainEvent)
	sb.headSub = sb.SubscribeChainHeadEvent(sb.newHeadEvent)

	return sb
}

func (bc *SnifferBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return bc.txPool.SubscribeNewTxsEvent(ch)
	//return b.scope.Track(b.feed.Subscribe(ch))
}

// SubscribeChainEvent registers a subscription of ChainEvent.
func (bc *SnifferBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return bc.scope.Track(bc.chainFeed.Subscribe(ch)) //delete
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *SnifferBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return bc.chain.SubscribeChainHeadEvent(ch)
}

func (bc *SnifferBackend) SnifferLoop() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		bc.TxSub.Unsubscribe()
		bc.headSub.Unsubscribe()
		bc.chainSub.Unsubscribe()
		bc.scope.Close()
	}()

	var block = bc.chain.CurrentBlock()

	for {
		select {
		case <-bc.ctx.Done():
			return
		case <-bc.TxSub.Err():
			return
		case sig := <-sigs:
			log.Info("Signal", "sig", sig)
			return
		case newTx := <-bc.newTxsEvent: //work
			if !mamoru.IsSnifferEnable() || !mamoru.Connect() {
				continue
			}
			log.Info("Mamoru TxPool Sniffer start", "number", block.NumberU64())
			log.Info("NewTx Event", "txs", len(newTx.Txs))
			startTime := time.Now()

			var receipts types.Receipts
			//wg := new(sync.WaitGroup)
			stateDb, err := bc.chain.StateAt(block.Root())
			if err != nil {
				log.Error("SetState", "err", err)
			}
			stateDb = stateDb.Copy()
			// todo Need to do parallel processing of transactions
			for _, tx := range newTx.Txs {

				trac, err := mamoru.NewCallTracer(false)
				if err != nil {
					log.Error("mamoru.NewCallTracer", "err", err)
				}
				//wg.Add(1)
				//_ = gopool.Submit(func() {
				//	defer wg.Done()
				//})

				chCtx := core.ChainContext(bc.chain)
				header := block.Header()

				author, _ := types.LatestSigner(bc.chainConfig).Sender(tx)
				gasPool := new(core.GasPool).AddGas(tx.Gas())

				var gasUsed = new(uint64)
				*gasUsed = header.GasUsed

				stateDb.Prepare(tx.Hash(), len(newTx.Txs))
				receipt, err := core.ApplyTransaction(bc.chainConfig, chCtx, &author, gasPool, stateDb, header, tx,
					gasUsed, vm.Config{Debug: true, Tracer: trac, NoBaseFee: true})
				if err != nil {
					log.Error("ApplyTransaction", "err", err, "number", block.NumberU64(), "tx.hash", tx.Hash().String())
					break
				}
				receipts = append(receipts, receipt)
				log.Info("receipts", "number", block.NumberU64(), "len", len(receipts))

				callFrames, err := trac.GetResult()
				if err != nil {
					log.Error("Mamoru tracer result", "err", err, "number", block.NumberU64(), "tx.hash", tx.Hash().String())
					break
				}
				log.Info("callFrames", "len", len(callFrames))
				bc.tracer.FeedCalTraces(callFrames, block.NumberU64())
			}
			//wg.Wait()

			log.Info("Tracer Transactions", "number", block.NumberU64(), "txs", block.Transactions().Len(), "receipts", len(receipts))
			bc.tracer.FeedTransactions(block.Number(), newTx.Txs, receipts)
			log.Info("Tracer Events", "receipts", len(receipts))
			bc.tracer.FeedEvents(receipts)
			log.Info("Tracer Send", "number", block.NumberU64(), "hash", block.Hash())
			bc.tracer.Send(startTime, block.Number(), block.Hash())

		case newHead := <-bc.chainEvent:
			if newHead.Block != nil {
				log.Info("New Chain Event", "number", bc.chain.CurrentBlock().NumberU64())
				block = newHead.Block
			}
		case newHead := <-bc.newHeadEvent:
			if newHead.Block != nil {
				log.Info("New Header Event", "number", newHead.Block.NumberU64())
				block = newHead.Block
			}
		}
	}
}
