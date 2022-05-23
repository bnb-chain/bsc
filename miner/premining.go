package miner

import (
	"bytes"
	"errors"
	"math/big"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

func (w *worker) preCommitBlock(poolTxsCh chan []map[common.Address]types.Transactions, interrupt *int32) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	parent := w.chain.CurrentBlock()

	timestamp := int64(parent.Time() + 3)

	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   core.CalcGasLimit(parent, w.config.GasFloor, w.config.GasCeil) * 4,
		Extra:      w.extra,
		Time:       uint64(timestamp),
		Nonce:      types.BlockNonce{},
		Difficulty: big.NewInt(2),
	}

	if err := w.engine.(*parlia.Parlia).Prepare4PreMining(w.chain, header); err != nil {
		log.Error("preCommitBlock: Failed to prepare header for mining", "err", err)
		return
	}
	// If we are care about TheDAO hard-fork check whether to override the extra-data or not
	if daoBlock := w.chainConfig.DAOForkBlock; daoBlock != nil {
		// Check whether the block is among the fork extra-override range
		limit := new(big.Int).Add(daoBlock, params.DAOForkExtraRange)
		if header.Number.Cmp(daoBlock) >= 0 && header.Number.Cmp(limit) < 0 {
			// Depending whether we support or oppose the fork, override differently
			if w.chainConfig.DAOForkSupport {
				header.Extra = common.CopyBytes(params.DAOForkBlockExtra)
			} else if bytes.Equal(header.Extra, params.DAOForkBlockExtra) {
				header.Extra = []byte{} // If miner opposes, don't let it use the reserved extra-data
			}
		}
	}
	// Could potentially happen if starting to mine in an odd state.
	err := w.makePreCurrent(parent, header)
	if err != nil {
		log.Error("preCommitBlock: Failed to create mining context", "err", err)
		return
	}
	// Create the current work task and check any fork transitions needed
	env := w.currentPre
	if w.chainConfig.DAOForkSupport && w.chainConfig.DAOForkBlock != nil && w.chainConfig.DAOForkBlock.Cmp(header.Number) == 0 {
		misc.ApplyDAOHardFork(env.state)
	}
	systemcontracts.UpgradeBuildInSystemContract(w.chainConfig, header.Number, env.state)
	// Accumulate the uncles for the current block
	uncles := make([]*types.Header, 0)

	tstart := time.Now()
	log.Info("preCommitBlock start", "blockNum", header.Number)
	ctxs := 0
	totalTxs := 0
	for txs := range poolTxsCh {
		if len(txs) == 2 {
			totalTxs += len(txs[0]) + len(txs[1])
		}
		//reset gaspool, diff new txs, state has been changed on this height , will just be shifted by nonce. same nonce with higher price will fail.
		if w.preExecute(txs, interrupt, uncles, header.Number, ctxs) {
			log.Info("preCommitBlock end-interrupted", "blockNum", header.Number, "batchTxs", ctxs+1, "countOfTxs", totalTxs, "elapsed", time.Now().Sub(tstart), "w.tcount", w.currentPre.tcount)
			return
		}
		ctxs++
		if ctxs > 5 {
			break
		}
	}
	log.Info("preCommitBlock end", "blockNum", header.Number, "batchTxs", ctxs, "countOfTxs", totalTxs, "elapsed", time.Now().Sub(tstart), "w.tcount", w.currentPre.tcount)
}

func (w *worker) preExecute(pendingTxs []map[common.Address]types.Transactions, interrupt *int32, uncles []*types.Header, num *big.Int, ctxs int) bool {
	if len(pendingTxs) == 0 {
		return false
	}
	totalTxs := 0
	tmp := w.currentPre.tcount
	if len(pendingTxs[0]) > 0 {
		totalTxs += len(pendingTxs[0])
		txs := types.NewTransactionsByPriceAndNonce(w.currentPre.signer, pendingTxs[0])
		if w.preCommitTransactions(txs, w.coinbase, interrupt) {
			log.Debug("preCommitBlock-preExecute, commit local txs interrupted and return", "blockNum", num, "batchTxs", ctxs+1, "len(localtxs)", len(pendingTxs[0]))
			return true
		}
	}
	if len(pendingTxs[1]) > 0 {
		totalTxs += len(pendingTxs[1])
		txs := types.NewTransactionsByPriceAndNonce(w.currentPre.signer, pendingTxs[1])
		if w.preCommitTransactions(txs, w.coinbase, interrupt) {
			log.Debug("preCommitBlock-preExecute, commit remote txs interrupted and return", "blockNum", num, "batchTxs", ctxs+1, "len(remotetxs)", len(pendingTxs[1]))
			return true
		}
	}
	s := w.currentPre.state
	if err := s.WaitPipeVerification(); err == nil {
		w.engine.(*parlia.Parlia).FinalizeAndAssemble4preMining(w.chain, types.CopyHeader(w.currentPre.header), s)
		log.Debug("preCommitBlock-preExecute, FinalizeAndAssemble done", "blockNum", num, "batchTxs", ctxs+1, "len(txs)", len(w.currentPre.txs), "len(receipts)", len(w.currentPre.receipts), "w.current.tcount", w.currentPre.tcount, "totalTxs", totalTxs, "tcounBefore", tmp)
	}
	return false
}

func (w *worker) preCommitTransactions(txs *types.TransactionsByPriceAndNonce, coinbase common.Address, interrupt *int32) bool {
	// Short circuit if current is nil
	if w.currentPre == nil {
		return true
	}

	if w.currentPre.gasPool == nil {
		w.currentPre.gasPool = new(core.GasPool).AddGas(w.currentPre.header.GasLimit)
		w.currentPre.gasPool.SubGas(params.SystemTxsGas)
	}

	var coalescedLogs []*types.Log
	var stopTimer *time.Timer
	delay := w.engine.Delay(w.chain, w.currentPre.header)
	if delay != nil {
		tmpD := *delay - w.config.DelayLeftOver
		if tmpD <= 100*time.Millisecond {
			tmpD = time.Duration(time.Second)
		}
		//		stopTimer = time.NewTimer(*delay - w.config.DelayLeftOver)
		stopTimer = time.NewTimer(tmpD)
		log.Debug("preCommitTransactions: Time left for mining work", "left", (*delay - w.config.DelayLeftOver).String(), "leftover", w.config.DelayLeftOver)
		defer stopTimer.Stop()
	}

	// initilise bloom processors
	processorCapacity := 100
	if txs.CurrentSize() < processorCapacity {
		processorCapacity = txs.CurrentSize()
	}
	bloomProcessors := core.NewAsyncReceiptBloomGenerator(processorCapacity)

LOOP:
	for {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the interrupt signal is 1
		// (2) worker start or restart, the interrupt signal is 1
		// (3) worker recreate the mining block with any newly arrived transactions, the interrupt signal is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interrupt != nil && atomic.LoadInt32(interrupt) != commitInterruptNone {
			log.Debug("preCommitTransactions: interrupted")
			return true
		}
		// If we don't have enough gas for any further transactions then we're done
		if w.currentPre.gasPool.Gas() < params.TxGas {
			log.Trace("preCommitTransactions: Not enough gas for further transactions", "have", w.currentPre.gasPool, "want", params.TxGas)
			break
		}
		if stopTimer != nil {
			select {
			case <-stopTimer.C:
				log.Info("preCommitTransactions: Not enough time for further transactions", "txs", len(w.currentPre.txs))
				break LOOP
			default:
			}
		}
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		//from, _ := types.Sender(w.current.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(w.currentPre.header.Number) {
			//log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)
			txs.Pop()
			continue
		}
		// Start executing the transaction
		w.currentPre.state.Prepare(tx.Hash(), common.Hash{}, w.currentPre.tcount)

		logs, err := w.preCommitTransaction(tx, coinbase, bloomProcessors)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			//log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			//log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			//log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			w.currentPre.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			//log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			//log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}
	bloomProcessors.Close()
	return false
}

//preCommitTransaction would execute transactions on pre-mining environment currentPre
func (w *worker) preCommitTransaction(tx *types.Transaction, coinbase common.Address, receiptProcessors ...core.ReceiptProcessor) ([]*types.Log, error) {
	snap := w.currentPre.state.Snapshot()
	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &coinbase, w.currentPre.gasPool, w.currentPre.state, w.currentPre.header, tx, &w.currentPre.header.GasUsed, *w.chain.GetVMConfig(), receiptProcessors...)
	if err != nil {
		w.currentPre.state.RevertToSnapshot(snap)
		return nil, err
	}
	w.currentPre.txs = append(w.currentPre.txs, tx)
	w.currentPre.receipts = append(w.currentPre.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) makePreCurrent(parent *types.Block, header *types.Header) error {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateAtWithSharedPool(parent.Root())
	if err != nil {
		return err
	}
	state.StartPrefetcher("preminer")

	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		uncles:    mapset.NewSet(),
		header:    header,
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0

	// Swap out the old work with the new one, terminating any leftover prefetcher
	// processes in the mean time and starting a new one.
	if w.currentPre != nil && w.currentPre.state != nil {
		w.currentPre.state.StopPrefetcher()
	}
	w.currentPre = env
	return nil
}
