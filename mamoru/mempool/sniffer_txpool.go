package mempool

import (
	"context"
	"sync"
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
	txPool      BcTxPool
	chain       blockChain
	chainConfig *params.ChainConfig
	feeder      mamoru.Feeder

	newHeadEvent chan core.ChainHeadEvent
	newTxsEvent  chan core.NewTxsEvent

	TxSub   event.Subscription
	headSub event.Subscription

	ctx context.Context
	mu  sync.RWMutex
}

func NewSniffer(ctx context.Context, txPool BcTxPool, chain blockChain, chainConfig *params.ChainConfig, feeder mamoru.Feeder) *SnifferBackend {
	sb := &SnifferBackend{
		txPool:      txPool,
		chain:       chain,
		chainConfig: chainConfig,

		newTxsEvent:  make(chan core.NewTxsEvent, core.DefaultTxPoolConfig.GlobalQueue),
		newHeadEvent: make(chan core.ChainHeadEvent, 10),
		feeder:       feeder,

		ctx: ctx,
		mu:  sync.RWMutex{},
	}
	sb.TxSub = sb.SubscribeNewTxsEvent(sb.newTxsEvent)
	sb.headSub = sb.SubscribeChainHeadEvent(sb.newHeadEvent)

	return sb
}

func (bc *SnifferBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return bc.txPool.SubscribeNewTxsEvent(ch)
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *SnifferBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return bc.chain.SubscribeChainHeadEvent(ch)
}

func (bc *SnifferBackend) SnifferLoop() {
	defer func() {
		bc.TxSub.Unsubscribe()
		bc.headSub.Unsubscribe()
	}()

	ctx, cancel := context.WithCancel(bc.ctx)
	var block = bc.chain.CurrentBlock()

	for {
		select {
		case <-bc.ctx.Done():
		case <-bc.TxSub.Err():
		case <-bc.headSub.Err():
			cancel()
			return

		case newTx := <-bc.newTxsEvent:
			go bc.process(ctx, block, newTx.Txs)

		case newHead := <-bc.newHeadEvent:
			if newHead.Block != nil {
				log.Info("New Header Event", "number", newHead.Block.NumberU64())
				bc.mu.RLock()
				block = newHead.Block
				bc.mu.RUnlock()
			}
		}
	}
}

func (bc *SnifferBackend) process(ctx context.Context, block *types.Block, txs types.Transactions) {
	if !mamoru.IsSnifferEnable() || !mamoru.Connect() || ctx.Err() != nil {
		return
	}

	log.Info("Mamoru LightTxPool Sniffer start", "number", block.NumberU64())
	startTime := time.Now()

	// Create tracer context
	tracer := mamoru.NewTracer(bc.feeder)

	var receipts types.Receipts

	stateDb, err := bc.chain.StateAt(block.Root())
	if err != nil {
		log.Error("Mamoru State", "err", err, "ctx", "txpool")
	}

	stateDb = stateDb.Copy()
	for _, tx := range txs {
		calltracer, err := mamoru.NewCallTracer(false)
		if err != nil {
			log.Error("Mamoru Call tracer", "err", err, "ctx", "txpool")
		}

		header := block.Header()
		chCtx := core.ChainContext(bc.chain)
		author, _ := types.LatestSigner(bc.chainConfig).Sender(tx)
		gasPool := new(core.GasPool).AddGas(tx.Gas())

		var gasUsed = new(uint64)
		*gasUsed = header.GasUsed

		stateDb.Prepare(tx.Hash(), len(txs))
		receipt, err := core.ApplyTransaction(bc.chainConfig, chCtx, &author, gasPool, stateDb, header, tx,
			gasUsed, vm.Config{Debug: true, Tracer: calltracer, NoBaseFee: true})
		if err != nil {
			log.Error("Mamoru Apply Transaction", "err", err, "number", block.NumberU64(),
				"tx.hash", tx.Hash().String(), "ctx", "txpool")
			break
		}

		receipts = append(receipts, receipt)

		callFrames, err := calltracer.GetResult()
		if err != nil {
			log.Error("Mamoru tracer result", "err", err, "number", block.NumberU64(),
				"ctx", "txpool")
			break
		}

		tracer.FeedCalTraces(callFrames, block.NumberU64())
	}

	tracer.FeedBlock(block)
	tracer.FeedTransactions(block.Number(), txs, receipts)
	tracer.FeedEvents(receipts)
	tracer.Send(startTime, block.Number(), block.Hash(), "txpool")
}
