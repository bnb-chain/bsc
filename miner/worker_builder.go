package miner

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const smallBundleGas = 10 * params.TxGas

var (
	errNonRevertingTxInBundleFailed = errors.New("non-reverting tx in bundle failed")
	errBundlePriceTooLow            = errors.New("bundle price too low")
)

// fillTransactions retrieves the pending bundles and transactions from the txpool and fills them
// into the given sealing block. The selection and ordering strategy can be extended in the future.
func (w *worker) fillTransactionsAndBundles(interruptCh chan int32, env *environment, stopTimer *time.Timer) error {
	env.state.StopPrefetcher() // no need to prefetch txs for a builder

	// commit bundles
	{
		var bundles []*types.Bundle
		log.Debug("test: start fillTransactionsAndBundles")
		bundles = w.eth.TxPool().PendingBundles(env.header.Number.Uint64(), env.header.Time)

		// if no bundles, not necessary to fill transactions
		if len(bundles) == 0 {
			return errors.New("no bundles in bundle pool")
		}

		log.Debug("Bidder: generateOrderedBundles start", "state", env.state.GetNonce(common.HexToAddress("0x8e929a314fbB8BE79441daE743F5E5F8605D3EA2")))

		txs, bundle, err := w.generateOrderedBundles(env, bundles)
		if err != nil {
			log.Error("fail to generate ordered bundles", "err", err)
			return err
		}

		log.Debug("Bidder: generateOrderedBundles done", "txs", len(txs), "bundle", bundle, "state", env.state.GetNonce(common.HexToAddress("0x8e929a314fbB8BE79441daE743F5E5F8605D3EA2")))

		if len(txs) > 1 {
			log.Debug("Bidder: commitBundles start", "txHash", txs[0].Hash())
		}

		if err = w.commitBundles(env, txs, interruptCh, stopTimer); err != nil {
			log.Error("Bidder: fail to commit bundles", "err", err)
			return err
		}

		if len(env.txs) > 1 {
			log.Debug("Bidder: commitBundles end", "envtxs", len(env.txs), "txHash", env.txs[0].Hash())
		}

		env.profit.Add(env.profit, bundle.EthSentToSystem)
		log.Info("Bidder: fill bundles", "bundles_count", len(bundles))
	}

	// commit normal transactions
	{
		w.confMu.RLock()
		tip := w.tip
		prio := w.prio
		w.confMu.RUnlock()

		// Retrieve the pending transactions pre-filtered by the 1559/4844 dynamic fees
		filter := txpool.PendingFilter{
			MinTip: tip,
		}
		if env.header.BaseFee != nil {
			filter.BaseFee = uint256.MustFromBig(env.header.BaseFee)
		}
		if env.header.ExcessBlobGas != nil {
			filter.BlobFee = uint256.MustFromBig(eip4844.CalcBlobFee(w.chainConfig, env.header))
		}
		filter.OnlyPlainTxs, filter.OnlyBlobTxs = true, false
		pendingPlainTxs := w.eth.TxPool().Pending(filter)

		filter.OnlyPlainTxs, filter.OnlyBlobTxs = false, true
		pendingBlobTxs := w.eth.TxPool().Pending(filter)

		// Split the pending transactions into locals and remotes.
		prioPlainTxs, normalPlainTxs := make(map[common.Address][]*txpool.LazyTransaction), pendingPlainTxs
		prioBlobTxs, normalBlobTxs := make(map[common.Address][]*txpool.LazyTransaction), pendingBlobTxs

		for _, account := range prio {
			if txs := normalPlainTxs[account]; len(txs) > 0 {
				delete(normalPlainTxs, account)
				prioPlainTxs[account] = txs
			}
			if txs := normalBlobTxs[account]; len(txs) > 0 {
				delete(normalBlobTxs, account)
				prioBlobTxs[account] = txs
			}
		}

		// Fill the block with all available pending transactions.
		if len(prioPlainTxs) > 0 || len(prioBlobTxs) > 0 {
			plainTxs := newTransactionsByPriceAndNonce(env.signer, prioPlainTxs, env.header.BaseFee)
			blobTxs := newTransactionsByPriceAndNonce(env.signer, prioBlobTxs, env.header.BaseFee)

			if err := w.commitTransactions(env, plainTxs, blobTxs, interruptCh, stopTimer); err != nil {
				return err
			}
		}
		if len(normalPlainTxs) > 0 || len(normalBlobTxs) > 0 {
			plainTxs := newTransactionsByPriceAndNonce(env.signer, normalPlainTxs, env.header.BaseFee)
			blobTxs := newTransactionsByPriceAndNonce(env.signer, normalBlobTxs, env.header.BaseFee)

			if err := w.commitTransactions(env, plainTxs, blobTxs, interruptCh, stopTimer); err != nil {
				return err
			}
		}

		log.Info("fill transactions", "plain_txs_count", len(prioPlainTxs)+len(normalPlainTxs),
			"blob_txs_count", len(prioBlobTxs)+len(normalBlobTxs))
	}

	log.Info("Bidder: fill bundles and transactions done", "total_txs_count", len(env.txs))
	return nil
}

func (w *worker) commitBundles(
	env *environment,
	txs types.Transactions,
	interruptCh chan int32,
	stopTimer *time.Timer,
) error {
	if env.gasPool == nil {
		env.gasPool = prepareGasPool(env.header.GasLimit)
	}

	var coalescedLogs []*types.Log
	signal := commitInterruptNone
LOOP:
	for _, tx := range txs {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the reason is 1
		// (2) worker start or restart, the reason is 1
		// (3) worker recreate the sealing block with any newly arrived transactions, the reason is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interruptCh != nil {
			select {
			case signal, ok := <-interruptCh:
				if !ok {
					// should never be here, since interruptCh should not be read before
					log.Warn("commit transactions stopped unknown")
				}
				return signalToErr(signal)
			default:
			}
		} // If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			signal = commitInterruptOutOfGas
			break
		}
		if tx == nil {
			log.Error("Unexpected nil transaction in bundle")
			return signalToErr(commitInterruptBundleTxNil)
		}
		if stopTimer != nil {
			select {
			case <-stopTimer.C:
				log.Info("Not enough time for further transactions", "txs", len(env.txs))
				stopTimer.Reset(0) // re-active the timer, in case it will be used later.
				signal = commitInterruptTimeout
				break LOOP
			default:
			}
		}

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(env.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Debug("Unexpected protected transaction in bundle")
			return signalToErr(commitInterruptBundleTxProtected)
		}
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)

		logs, err := w.commitTransaction(env, tx, core.NewReceiptBloomGenerator())
		switch err {
		case core.ErrGasLimitReached:
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Error("Unexpected gas limit exceeded for current block in the bundle", "sender", from)
			return signalToErr(commitInterruptBundleCommit)

		case core.ErrNonceTooLow:
			// New head notification data race between the transaction pool and miner, shift
			log.Error("Transaction with low nonce in the bundle", "sender", from, "nonce", tx.Nonce())
			return signalToErr(commitInterruptBundleCommit)

		case core.ErrNonceTooHigh:
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Error("Account with high nonce in the bundle", "sender", from, "nonce", tx.Nonce())
			return signalToErr(commitInterruptBundleCommit)

		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			continue

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Error("Transaction failed in the bundle", "hash", tx.Hash(), "err", err)
			return signalToErr(commitInterruptBundleCommit)
		}
	}

	if !w.isRunning() && len(coalescedLogs) > 0 {
		// We don't push the pendingLogsEvent while we are mining. The reason is that
		// when we are mining, the worker will regenerate a mining block every 3 seconds.
		// In order to avoid pushing the repeated pendingLog, we disable the pending log pushing.

		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		w.pendingLogsFeed.Send(cpy)
	}
	return signalToErr(signal)
}

// generateOrderedBundles generates ordered txs from the given bundles.
// 1. sort bundles according to computed gas price when received.
// 2. simulate bundles based on the same state, resort.
// 3. merge resorted simulateBundles based on the iterative state.
func (w *worker) generateOrderedBundles(
	env *environment,
	bundles []*types.Bundle,
) (types.Transactions, *types.SimulatedBundle, error) {
	// sort bundles according to gas price computed when received
	sort.SliceStable(bundles, func(i, j int) bool {
		priceI, priceJ := bundles[i].Price, bundles[j].Price

		return priceI.Cmp(priceJ) >= 0
	})

	// recompute bundle gas price based on the same state and current env
	simulatedBundles, err := w.simulateBundles(env, bundles)
	if err != nil {
		log.Error("fail to simulate bundles base on the same state", "err", err)
		return nil, nil, err
	}

	// sort bundles according to fresh gas price
	sort.SliceStable(simulatedBundles, func(i, j int) bool {
		priceI, priceJ := simulatedBundles[i].BundleGasPrice, simulatedBundles[j].BundleGasPrice

		return priceI.Cmp(priceJ) >= 0
	})

	// merge bundles based on iterative state
	includedTxs, mergedBundle, err := w.mergeBundles(env, simulatedBundles)
	if err != nil {
		log.Error("fail to merge bundles", "err", err)
		return nil, nil, err
	}

	return includedTxs, mergedBundle, nil
}

func (w *worker) simulateBundles(env *environment, bundles []*types.Bundle) ([]*types.SimulatedBundle, error) {
	headerHash := env.header.Hash()
	simCache := w.bundleCache.GetBundleCache(headerHash)
	simResult := make(map[common.Hash]*types.SimulatedBundle)

	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, bundle := range bundles {
		if simmed, ok := simCache.GetSimulatedBundle(bundle.Hash()); ok {
			mu.Lock()
			simResult[bundle.Hash()] = simmed
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(idx int, bundle *types.Bundle, state *state.StateDB) {
			defer wg.Done()

			gasPool := prepareGasPool(env.header.GasLimit)
			evm := vm.NewEVM(core.NewEVMBlockContext(env.header, w.chain, &env.coinbase), state, w.chainConfig, vm.Config{})
			simmed, err := w.simulateBundle(evm, env.header, bundle, state, gasPool, 0, true, true)

			if err != nil {
				log.Trace("Error computing gas for a simulateBundle", "error", err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			simResult[bundle.Hash()] = simmed
		}(i, bundle, env.state.Copy())
	}

	wg.Wait()

	simulatedBundles := make([]*types.SimulatedBundle, 0)

	for _, bundle := range simResult {
		if bundle == nil {
			continue
		}

		simulatedBundles = append(simulatedBundles, bundle)
	}

	simCache.UpdateSimulatedBundles(simResult, bundles)

	return simulatedBundles, nil
}

// mergeBundles merges the given simulateBundle into the given environment.
// It returns the merged simulateBundle and the number of transactions that were merged.
func (w *worker) mergeBundles(
	env *environment,
	bundles []*types.SimulatedBundle,
) (types.Transactions, *types.SimulatedBundle, error) {
	currentState := env.state.Copy()
	gasPool := prepareGasPool(env.header.GasLimit)

	includedTxs := types.Transactions{}
	mergedBundle := types.SimulatedBundle{
		BundleGasFees:   new(big.Int),
		BundleGasUsed:   0,
		BundleGasPrice:  new(big.Int),
		EthSentToSystem: new(big.Int),
	}

	evm := vm.NewEVM(core.NewEVMBlockContext(env.header, w.chain, &env.coinbase), env.state.Copy(), w.chainConfig, vm.Config{})

	for _, bundle := range bundles {
		// if we don't have enough gas for any further transactions then we're done
		if gasPool.Gas() < smallBundleGas {
			break
		}

		prevState := currentState.Copy()
		prevGasPool := new(core.GasPool).AddGas(gasPool.Gas())

		// the floor gas price is 99/100 what was simulated at the top of the block
		floorGasPrice := new(big.Int).Mul(bundle.BundleGasPrice, big.NewInt(99))
		floorGasPrice = floorGasPrice.Div(floorGasPrice, big.NewInt(100))

		simulatedBundle, err := w.simulateBundle(evm, env.header, bundle.OriginalBundle, currentState, gasPool, len(includedTxs), true, false)

		if err != nil || simulatedBundle.BundleGasPrice.Cmp(floorGasPrice) <= 0 {
			currentState = prevState
			gasPool = prevGasPool

			log.Error("failed to merge bundle", "floorGasPrice", floorGasPrice.String(), "err", err)
			continue
		}

		includedTxs = append(includedTxs, bundle.OriginalBundle.Txs...)

		mergedBundle.BundleGasFees.Add(mergedBundle.BundleGasFees, simulatedBundle.BundleGasFees)
		mergedBundle.BundleGasUsed += simulatedBundle.BundleGasUsed

		for _, tx := range bundle.OriginalBundle.Txs {
			if !containsHash(bundle.OriginalBundle.RevertingTxHashes, tx.Hash()) {
				env.UnRevertible = append(env.UnRevertible, tx.Hash())
			}
		}

		log.Info("included bundle",
			"gasUsed", simulatedBundle.BundleGasUsed,
			"gasPrice", simulatedBundle.BundleGasPrice,
			"txcount", len(simulatedBundle.OriginalBundle.Txs),
			"unrevertible", len(env.UnRevertible))
	}

	if len(includedTxs) == 0 {
		return nil, nil, errors.New("include no txs when merge bundles")
	}

	mergedBundle.BundleGasPrice.Div(mergedBundle.BundleGasFees, new(big.Int).SetUint64(mergedBundle.BundleGasUsed))

	return includedTxs, &mergedBundle, nil
}

// simulateBundle computes the gas price for a whole simulateBundle based on the same ctx
// named computeBundleGas in flashbots
func (w *worker) simulateBundle(
	evm *vm.EVM, header *types.Header, bundle *types.Bundle, state *state.StateDB, gasPool *core.GasPool, currentTxCount int,
	prune, pruneGasExceed bool,
) (*types.SimulatedBundle, error) {
	var (
		tempGasUsed     uint64
		bundleGasUsed   uint64
		bundleGasFees   = new(big.Int)
		ethSentToSystem = new(big.Int)
	)

	txsLen := len(bundle.Txs)
	for i := 0; i < txsLen; i++ {
		tx := bundle.Txs[i]

		state.SetTxContext(tx.Hash(), i+currentTxCount)
		sysBalanceBefore := state.GetBalance(consensus.SystemAddress)

		snap := state.Snapshot()
		gp := gasPool.Gas()

		receipt, err := core.ApplyTransaction(evm, gasPool, state, header, tx, &tempGasUsed)

		if err != nil {
			log.Warn("fail to simulate bundle", "hash", bundle.Hash().String(), "err", err)

			if containsHash(bundle.DroppingTxHashes, tx.Hash()) {
				log.Warn("drop tx in bundle", "hash", tx.Hash().String())
				state.RevertToSnapshot(snap)
				gasPool.SetGas(gp)
				bundle.Txs = bundle.Txs.Remove(i)
				txsLen = len(bundle.Txs)
				i--
				continue
			}

			if prune {
				if errors.Is(err, core.ErrGasLimitReached) && !pruneGasExceed {
					log.Warn("bundle gas limit exceed", "hash", bundle.Hash().String())
				} else {
					log.Warn("prune bundle", "hash", bundle.Hash().String(), "err", err)
					w.eth.TxPool().PruneBundle(bundle.Hash())
				}
			}

			return nil, err
		}

		if receipt.Status == types.ReceiptStatusFailed && !containsHash(bundle.RevertingTxHashes, receipt.TxHash) {
			// for unRevertible tx but itself can be dropped, we drop it and revert the state and gas pool
			if containsHash(bundle.DroppingTxHashes, receipt.TxHash) {
				log.Warn("drop tx in bundle", "hash", receipt.TxHash.String())
				// NOTE: here should not revert state, when no err returned by ApplyTransaction, state.clearJournalAndRefund()
				// must had been called to avoid reverting across transactions, so we can directly remove the tx from bundle
				gasPool.SetGas(gp)
				bundle.Txs = bundle.Txs.Remove(i)
				txsLen = len(bundle.Txs)
				i--
				continue
			}

			err = errNonRevertingTxInBundleFailed
			log.Warn("fail to simulate bundle", "hash", bundle.Hash().String(), "err", err)

			if prune {
				w.eth.TxPool().PruneBundle(bundle.Hash())
				log.Warn("prune bundle", "hash", bundle.Hash().String())
			}

			return nil, err
		}

		if !w.eth.TxPool().Has(tx.Hash()) {
			bundleGasUsed += receipt.GasUsed

			txGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
			effectiveTip, er := tx.EffectiveGasTip(header.BaseFee)
			if er != nil {
				return nil, er
			}

			if header.BaseFee != nil {
				effectiveTip.Add(effectiveTip, header.BaseFee)
			}

			txGasFees := new(big.Int).Mul(txGasUsed, effectiveTip)

			if tx.Type() == types.BlobTxType {
				blobFee := new(big.Int).SetUint64(receipt.BlobGasUsed)
				blobFee.Mul(blobFee, receipt.BlobGasPrice)
				txGasFees.Add(txGasFees, blobFee)
			}
			bundleGasFees.Add(bundleGasFees, txGasFees)
			sysBalanceAfter := state.GetBalance(consensus.SystemAddress)
			sysDelta := new(uint256.Int).Sub(sysBalanceAfter, sysBalanceBefore)
			sysDelta.Sub(sysDelta, uint256.MustFromBig(txGasFees))
			ethSentToSystem.Add(ethSentToSystem, sysDelta.ToBig())
		}
	}

	// prune bundle when all txs are dropped
	if len(bundle.Txs) == 0 {
		log.Warn("prune bundle", "hash", bundle.Hash().String(), "err", "empty bundle")
		w.eth.TxPool().PruneBundle(bundle.Hash())
		return nil, errors.New("empty bundle")
	}

	// if all txs in the bundle are from mempool, we accept the bundle without checking gas price
	bundleGasPrice := big.NewInt(0)

	if bundleGasUsed != 0 {
		bundleGasPrice = new(big.Int).Div(bundleGasFees, new(big.Int).SetUint64(bundleGasUsed))

		if bundleGasPrice.Cmp(big.NewInt(w.config.MevGasPriceFloor)) < 0 {
			err := errBundlePriceTooLow
			log.Warn("fail to simulate bundle", "hash", bundle.Hash().String(), "err", err)

			if prune {
				log.Warn("prune bundle", "hash", bundle.Hash().String())
				w.eth.TxPool().PruneBundle(bundle.Hash())
			}

			return nil, err
		}
	}

	return &types.SimulatedBundle{
		OriginalBundle:  bundle,
		BundleGasFees:   bundleGasFees,
		BundleGasPrice:  bundleGasPrice,
		BundleGasUsed:   bundleGasUsed,
		EthSentToSystem: ethSentToSystem,
	}, nil
}

func (w *worker) simulateGaslessBundle(env *environment, bundle *types.Bundle) (*types.SimulateGaslessBundleResp, error) {
	validResults := make([]types.GaslessTxSimResult, 0)
	gasReachedResults := make([]types.GaslessTxSimResult, 0)

	txIdx := 0
	for _, tx := range bundle.Txs {
		env.state.SetTxContext(tx.Hash(), txIdx)

		var (
			snap = env.state.Snapshot()
			gp   = env.gasPool.Gas()
		)

		receipt, err := core.ApplyTransaction(env.evm, env.gasPool, env.state, env.header, tx, &env.header.GasUsed)
		if err != nil {
			env.state.RevertToSnapshot(snap)
			env.gasPool.SetGas(gp)
			log.Error("fail to simulate gasless tx, skipped", "hash", tx.Hash(), "err", err)

			if err == core.ErrGasLimitReached {
				gasReachedResults = append(gasReachedResults, types.GaslessTxSimResult{Hash: tx.Hash()})
			}
		} else {
			txIdx++

			validResults = append(validResults, types.GaslessTxSimResult{
				Hash:    tx.Hash(),
				GasUsed: receipt.GasUsed,
			})
		}
	}

	return &types.SimulateGaslessBundleResp{
		ValidResults:      validResults,
		GasReachedResults: gasReachedResults,
		BasedBlockNumber:  env.header.Number.Int64(),
	}, nil
}

func containsHash(arr []common.Hash, match common.Hash) bool {
	for _, elem := range arr {
		if elem == match {
			return true
		}
	}
	return false
}

func prepareGasPool(gasLimit uint64) *core.GasPool {
	gasPool := new(core.GasPool).AddGas(gasLimit)
	gasPool.SubGas(params.SystemTxsGas) // reserve gas for system txs(keep align with mainnet)
	return gasPool
}
