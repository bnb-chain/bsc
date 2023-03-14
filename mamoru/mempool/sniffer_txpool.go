package mempool

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/mamoru"
	"github.com/ethereum/go-ethereum/params"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type blockChain interface {
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)

	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
}

type SnifferChainContext struct {
	chain *core.BlockChain
}

func (c *SnifferChainContext) Engine() consensus.Engine {
	return c.chain.Engine()
}

func (c *SnifferChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.chain.GetHeader(hash, number)
}

type SnifferBackend struct {
	txPool       *core.TxPool
	chain        blockChain
	chainConfig  *params.ChainConfig
	currentBlock *types.Block

	chCtx *SnifferChainContext

	chainHeadFeed event.Feed
	newHeadEvent  chan core.ChainHeadEvent
	newTxsEvent   chan core.NewTxsEvent
	txSub         event.Subscription
	headSub       event.Subscription
}

func NewSniffer(txPool *core.TxPool, chain *core.BlockChain, chainConfig *params.ChainConfig, currentBlock *types.Block) *SnifferBackend {
	sb := &SnifferBackend{
		txPool:       txPool,
		chain:        chain,
		chainConfig:  chainConfig,
		currentBlock: currentBlock,
		newTxsEvent:  make(chan core.NewTxsEvent, core.DefaultTxPoolConfig.GlobalQueue),
		newHeadEvent: make(chan core.ChainHeadEvent, 10),
		chCtx:        &SnifferChainContext{chain: chain},
	}
	sb.txSub = sb.subscribeNewTxsEvent(sb.newTxsEvent)
	sb.headSub = sb.subscribeNewHeadEvent(sb.newHeadEvent)
	return sb
}

func (b *SnifferBackend) subscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.txPool.SubscribeNewTxsEvent(ch)
}

func (b *SnifferBackend) subscribeNewHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.chain.SubscribeChainHeadEvent(ch)
}

func (b *SnifferBackend) SnifferLoop() {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	defer b.txSub.Unsubscribe()
	// Track the previous block headers for transaction reorgs
	var block = b.currentBlock

	for {
		select {
		// System shutdown.
		case <-b.txSub.Err():
			return
		case sig := <-sigs:
			log.Info("Signal", "sig", sig)
			return
		case newTx := <-b.newTxsEvent:
			log.Info("Mamoru Sniffer start", "number", block.NumberU64())
			log.Info("NewTx Event", "txs", len(newTx.Txs))
			startTime := time.Now()
			tracer := mamoru.NewTracer(mamoru.NewFeed(b.chainConfig))
			log.Info("Tracer Block", "number", block.NumberU64())
			//tracer.FeedBlock(block)

			var receipts []*types.Receipt

			for index, tx := range newTx.Txs {
				msg, err := tx.AsMessage(types.LatestSigner(b.chainConfig), block.BaseFee()) //big.NewInt(params.InitialBaseFee)
				if err != nil {
					log.Error("AsMessage", "err", err)
				}
				txContext := core.NewEVMTxContext(msg)
				// Creating CallTracer
				trac, err := mamoru.NewCallTracer(true)
				if err != nil {
					log.Error("mamoru.NewCallTracer", "err", err)
				}
				stateDb, err := b.chain.StateAt(block.Root())
				if err != nil {
					log.Error("b.chain.StateAt(block.Root())", "err", err)
				}
				chCtx := core.ChainContext(b.chCtx)
				header := block.Header()
				blockCtx := core.NewEVMBlockContext(header, chCtx, nil)
				vmConfig := vm.Config{Debug: true, Tracer: trac, NoBaseFee: true}
				vmenv := vm.NewEVM(blockCtx, txContext, stateDb, b.chainConfig, vmConfig)
				stateDb.Prepare(tx.Hash(), index)
				exResult, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
				if err != nil {
					log.Error("ApplyMessage", "err", err)
					break
				}
				exResult.UsedGas += msg.Gas()
				//receipt := &types.Receipt{Type: tx.Type(), PostState: stateDb, CumulativeGasUsed: exResult.UsedGas}
				callFrames, err := trac.GetResult()
				if err != nil {
					log.Error("trac.GetResult()", "err", err)
					break
				}
				tracer.FeedCalTraces(callFrames, block.NumberU64())

				authot, _ := types.LatestSigner(b.chainConfig).Sender(tx)
				gasPool := new(core.GasPool).AddGas(msg.Gas())

				receipt, err := core.ApplyTransaction(b.chainConfig, chCtx, &authot, gasPool, stateDb, header, tx, &exResult.UsedGas, vmConfig)
				if err != nil {
					log.Error("ApplyTransaction", "err", err)
					break
				}
				receipts = append(receipts, receipt)
			}
			log.Info("Tracer Transactions", "number", block.NumberU64(), "txs", block.Transactions().Len(), "receipts", len(receipts))
			tracer.FeedTransactions(block, receipts)
			log.Info("Tracer Events", "receipts", len(receipts))
			tracer.FeedEvents(receipts)
			log.Info("Tracer Send", "number", block.NumberU64(), "hash", block.Hash())
			tracer.Send(startTime, block.Number(), block.Hash())

		case newHead := <-b.newHeadEvent:
			log.Trace("New Head Event", "block.number", newHead.Block.NumberU64())
			log.Trace("Current Head", "block.number", b.chain.CurrentBlock().NumberU64())
			if newHead.Block != nil {
				block = newHead.Block
			}
		}
	}
}

func (b *SnifferBackend) buildBlock(tx *types.Transaction) {
	//block := b.chain.CurrentBlock()
	//fmt.Printf("Block num=%d txs=%d\n", block.NumberU64(), block.Transactions().Len())
	//fmt.Println("Block Start")
	//for _, btx := range block.Transactions() {
	//	fmt.Printf("Block transaction nonce=%d, tx.hash=%s\n", btx.Nonce(), btx.Hash())
	//}
	//fmt.Println("Block stop")
	fmt.Printf("Transaction tx.hash=%s, tx.to=%s\n", tx.Hash(), tx.To())

}
