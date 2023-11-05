// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package miner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	lru2 "github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	lru "github.com/hashicorp/golang-lru"
)

const (
	// resultQueueSize is the size of channel listening to sealing result.
	resultQueueSize = 10

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	// minRecommitInterval is the minimal time interval to recreate the sealing block with
	// any newly arrived transactions.
	minRecommitInterval = 1 * time.Second

	// staleThreshold is the maximum depth of the acceptable stale block.
	staleThreshold = 11

	// the current 4 mining loops could have asynchronous risk of mining block with
	// save height, keep recently mined blocks to avoid double sign for safety,
	recentMinedCacheLimit = 20
)

var (
	writeBlockTimer    = metrics.NewRegisteredTimer("worker/writeblock", nil)
	finalizeBlockTimer = metrics.NewRegisteredTimer("worker/finalizeblock", nil)

	errBlockInterruptedByNewHead  = errors.New("new head arrived while building block")
	errBlockInterruptedByRecommit = errors.New("recommit interrupt while building block")
	errBlockInterruptedByTimeout  = errors.New("timeout while building block")
	errBlockInterruptedByOutOfGas = errors.New("out of gas while building block")
)

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts  []*types.Receipt
	state     *state.StateDB
	block     *types.Block
	createdAt time.Time
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
	commitInterruptTimeout
	commitInterruptOutOfGas
)

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interruptCh chan int32
	timestamp   int64
}

// newPayloadResult represents a result struct corresponds to payload generation.
type newPayloadResult struct {
	err   error
	block *types.Block
	fees  *big.Int
}

// getWorkReq represents a request for getting a new sealing work with provided parameters.
type getWorkReq struct {
	params *generateParams
	result chan *newPayloadResult // non-blocking channel
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	config      *Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	chain       *core.BlockChain

	// Feeds
	pendingLogsFeed event.Feed

	// Subscriptions
	mux          *event.TypeMux
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	// Channels
	newWorkCh          chan *newWorkReq
	getWorkCh          chan *getWorkReq
	taskCh             chan *task
	resultCh           chan *types.Block
	startCh            chan struct{}
	exitCh             chan struct{}
	resubmitIntervalCh chan time.Duration

	wg sync.WaitGroup

	mu       sync.RWMutex // The lock used to protect the coinbase and extra fields
	coinbase common.Address
	extra    []byte

	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	snapshotMu       sync.RWMutex // The lock used to protect the snapshots below
	snapshotBlock    *types.Block
	snapshotReceipts types.Receipts
	snapshotState    *state.StateDB

	// atomic status counters
	running atomic.Bool // The indicator whether the consensus engine is running or not.
	syncing atomic.Bool // The indicator whether the node is still syncing.

	// newpayloadTimeout is the maximum timeout allowance for creating payload.
	// The default value is 2 seconds but node operator can set it to arbitrary
	// large value. A large timeout allowance may cause Geth to fail creating
	// a non-empty payload within the specified time and eventually miss the slot
	// in case there are some computation expensive transactions in txpool.
	newpayloadTimeout time.Duration

	// recommit is the time interval to re-create sealing work or to re-build
	// payload in proof-of-stake stage.
	recommit time.Duration

	// External functions
	isLocalBlock func(header *types.Header) bool // Function used to determine whether the specified block is mined by local miner.

	// Test hooks
	newTaskHook       func(*task)                        // Method to call upon receiving a new sealing task.
	skipSealHook      func(*task) bool                   // Method to decide whether skipping the sealing.
	fullTaskHook      func()                             // Method to call before pushing the full sealing task.
	resubmitHook      func(time.Duration, time.Duration) // Method to call upon updating resubmitting interval.
	recentMinedBlocks *lru.Cache
}

func newWorker(config *Config, chainConfig *params.ChainConfig, engine consensus.Engine, eth Backend, mux *event.TypeMux, isLocalBlock func(header *types.Header) bool, init bool) *worker {
	recentMinedBlocks, _ := lru.New(recentMinedCacheLimit)
	worker := &worker{
		config:             config,
		chainConfig:        chainConfig,
		engine:             engine,
		eth:                eth,
		chain:              eth.BlockChain(),
		mux:                mux,
		isLocalBlock:       isLocalBlock,
		coinbase:           config.Etherbase,
		extra:              config.ExtraData,
		pendingTasks:       make(map[common.Hash]*task),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		newWorkCh:          make(chan *newWorkReq),
		getWorkCh:          make(chan *getWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		startCh:            make(chan struct{}, 1),
		exitCh:             make(chan struct{}),
		resubmitIntervalCh: make(chan time.Duration),
		recentMinedBlocks:  recentMinedBlocks,
	}
	// Subscribe events for blockchain
	worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)

	// Sanitize recommit interval if the user-specified one is too short.
	recommit := worker.config.Recommit
	if recommit < minRecommitInterval {
		log.Warn("Sanitizing miner recommit interval", "provided", recommit, "updated", minRecommitInterval)
		recommit = minRecommitInterval
	}
	worker.recommit = recommit

	// Sanitize the timeout config for creating payload.
	newpayloadTimeout := worker.config.NewPayloadTimeout
	if newpayloadTimeout == 0 {
		// log.Warn("Sanitizing new payload timeout to default", "provided", newpayloadTimeout, "updated", DefaultConfig.NewPayloadTimeout)
		newpayloadTimeout = DefaultConfig.NewPayloadTimeout
	}
	if newpayloadTimeout < time.Millisecond*100 {
		log.Warn("Low payload timeout may cause high amount of non-full blocks", "provided", newpayloadTimeout, "default", DefaultConfig.NewPayloadTimeout)
	}
	worker.newpayloadTimeout = newpayloadTimeout

	worker.wg.Add(4)
	go worker.mainLoop()
	go worker.newWorkLoop(recommit)
	go worker.resultLoop()
	go worker.taskLoop()

	// Submit first work to initialize pending state.
	if init {
		worker.startCh <- struct{}{}
	}
	return worker
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

// etherbase retrieves the configured etherbase address.
func (w *worker) etherbase() common.Address {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.coinbase
}

func (w *worker) setGasCeil(ceil uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.GasCeil = ceil
}

// setExtra sets the content used to initialize the block extra field.
func (w *worker) setExtra(extra []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extra = extra
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *worker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

// pending returns the pending state and corresponding block. The returned
// values can be nil in case the pending block is not initialized.
func (w *worker) pending() (*types.Block, *state.StateDB) {
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendingBlock returns pending block. The returned block can be nil in case the
// pending block is not initialized.
func (w *worker) pendingBlock() *types.Block {
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// pendingBlockAndReceipts returns pending block and corresponding receipts.
// The returned values can be nil in case the pending block is not initialized.
func (w *worker) pendingBlockAndReceipts() (*types.Block, types.Receipts) {
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock, w.snapshotReceipts
}

// start sets the running status as 1 and triggers new work submitting.
func (w *worker) start() {
	w.running.Store(true)
	w.startCh <- struct{}{}
}

// stop sets the running status as 0.
func (w *worker) stop() {
	w.running.Store(false)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return w.running.Load()
}

// close terminates all background threads maintained by the worker.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	w.running.Store(false)
	close(w.exitCh)
	w.wg.Wait()
}

// newWorkLoop is a standalone goroutine to submit new sealing work upon received events.
func (w *worker) newWorkLoop(recommit time.Duration) {
	defer w.wg.Done()
	var (
		interruptCh chan int32
		minRecommit = recommit // minimal resubmit interval specified by user.
		timestamp   int64      // timestamp for each round of sealing.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(reason int32) {
		if interruptCh != nil {
			// each commit work will have its own interruptCh to stop work with a reason
			interruptCh <- reason
			close(interruptCh)
		}
		interruptCh = make(chan int32, 1)
		select {
		case w.newWorkCh <- &newWorkReq{interruptCh: interruptCh, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
	}
	// clearPending cleans the stale pending tasks.
	clearPending := func(number uint64) {
		w.pendingMu.Lock()
		for h, t := range w.pendingTasks {
			if t.block.NumberU64()+staleThreshold <= number {
				delete(w.pendingTasks, h)
			}
		}
		w.pendingMu.Unlock()
	}

	for {
		select {
		case <-w.startCh:
			clearPending(w.chain.CurrentBlock().Number.Uint64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case head := <-w.chainHeadCh:
			if !w.isRunning() {
				continue
			}
			clearPending(head.Block.NumberU64())
			timestamp = time.Now().Unix()
			if p, ok := w.engine.(*parlia.Parlia); ok {
				signedRecent, err := p.SignRecently(w.chain, head.Block)
				if err != nil {
					log.Info("Not allowed to propose block", "err", err)
					continue
				}
				if signedRecent {
					log.Info("Signed recently, must wait")
					continue
				}
			}
			commit(commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() && ((w.chainConfig.Clique != nil &&
				w.chainConfig.Clique.Period > 0) || (w.chainConfig.Parlia != nil && w.chainConfig.Parlia.Period > 0)) {
				// Short circuit if no new transaction arrives.
				commit(commitInterruptResubmit)
			}

		case interval := <-w.resubmitIntervalCh:
			// Adjust resubmit interval explicitly by user.
			if interval < minRecommitInterval {
				log.Warn("Sanitizing miner recommit interval", "provided", interval, "updated", minRecommitInterval)
				interval = minRecommitInterval
			}
			log.Info("Miner recommit interval update", "from", minRecommit, "to", interval)
			minRecommit, recommit = interval, interval

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case <-w.exitCh:
			return
		}
	}
}

// mainLoop is responsible for generating and submitting sealing work based on
// the received event. It can support two modes: automatically generate task and
// submit it or return task according to given parameters for various proposes.
func (w *worker) mainLoop() {
	defer w.wg.Done()
	defer w.chainHeadSub.Unsubscribe()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interruptCh, req.timestamp)

		case req := <-w.getWorkCh:
			block, fees, err := w.generateWork(req.params)
			req.result <- &newPayloadResult{
				err:   err,
				block: block,
				fees:  fees,
			}

		// System stopped
		case <-w.exitCh:
			return
		case <-w.chainHeadSub.Err():
			return
		}
	}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *worker) taskLoop() {
	defer w.wg.Done()
	var (
		stopCh chan struct{}
		prev   common.Hash
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	for {
		select {
		case task := <-w.taskCh:
			if w.newTaskHook != nil {
				w.newTaskHook(task)
			}
			// Reject duplicate sealing work due to resubmitting.
			sealHash := w.engine.SealHash(task.block.Header())
			if sealHash == prev {
				continue
			}
			// Interrupt previous sealing operation
			interrupt()
			stopCh, prev = make(chan struct{}), sealHash

			if w.skipSealHook != nil && w.skipSealHook(task) {
				continue
			}
			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = task
			w.pendingMu.Unlock()

			if err := w.engine.Seal(w.chain, task.block, w.resultCh, stopCh); err != nil {
				log.Warn("Block sealing failed", "err", err)
				w.pendingMu.Lock()
				delete(w.pendingTasks, sealHash)
				w.pendingMu.Unlock()
			}
		case <-w.exitCh:
			interrupt()
			return
		}
	}
}

// resultLoop is a standalone goroutine to handle sealing result submitting
// and flush relative data to the database.
func (w *worker) resultLoop() {
	defer w.wg.Done()
	for {
		select {
		case block := <-w.resultCh:
			// Short circuit when receiving empty result.
			if block == nil {
				continue
			}
			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				continue
			}
			var (
				sealhash = w.engine.SealHash(block.Header())
				hash     = block.Hash()
			)
			w.pendingMu.RLock()
			task, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", hash)
				continue
			}
			// Different block could share same sealhash, deep copy here to prevent write-write conflict.
			var (
				receipts = make([]*types.Receipt, len(task.receipts))
				logs     []*types.Log
			)
			for i, taskReceipt := range task.receipts {
				receipt := new(types.Receipt)
				receipts[i] = receipt
				*receipt = *taskReceipt

				// add block location fields
				receipt.BlockHash = hash
				receipt.BlockNumber = block.Number()
				receipt.TransactionIndex = uint(i)

				// Update the block hash in all logs since it is now available and not when the
				// receipt/log of individual transactions were created.
				receipt.Logs = make([]*types.Log, len(taskReceipt.Logs))
				for i, taskLog := range taskReceipt.Logs {
					log := new(types.Log)
					receipt.Logs[i] = log
					*log = *taskLog
					log.BlockHash = hash
				}
				logs = append(logs, receipt.Logs...)
			}

			if prev, ok := w.recentMinedBlocks.Get(block.NumberU64()); ok {
				doubleSign := false
				prevParents, _ := prev.([]common.Hash)
				for _, prevParent := range prevParents {
					if prevParent == block.ParentHash() {
						log.Error("Reject Double Sign!!", "block", block.NumberU64(),
							"hash", block.Hash(),
							"root", block.Root(),
							"ParentHash", block.ParentHash())
						doubleSign = true
						break
					}
				}
				if doubleSign {
					continue
				}
				prevParents = append(prevParents, block.ParentHash())
				w.recentMinedBlocks.Add(block.NumberU64(), prevParents)
			} else {
				// Add() will call removeOldest internally to remove the oldest element
				// if the LRU Cache is full
				w.recentMinedBlocks.Add(block.NumberU64(), []common.Hash{block.ParentHash()})
			}

			// Commit block and state to database.
			task.state.SetExpectedStateRoot(block.Root())
			start := time.Now()
			status, err := w.chain.WriteBlockAndSetHead(block, receipts, logs, task.state, true)
			if status != core.CanonStatTy {
				if err != nil {
					log.Error("Failed writing block to chain", "err", err, "status", status)
				} else {
					log.Info("Written block as SideChain and avoid broadcasting", "status", status)
				}
				continue
			}
			writeBlockTimer.UpdateSince(start)
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", hash,
				"elapsed", common.PrettyDuration(time.Since(task.createdAt)))
			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

		case <-w.exitCh:
			return
		}
	}
}

// updateSnapshot updates pending snapshot block, receipts and state.
func (w *worker) updateSnapshot(env *core.MinerEnvironment) {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	w.snapshotBlock = types.NewBlock(
		env.Header,
		env.PackedTxs,
		nil,
		env.Receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = types.CopyReceipts(env.Receipts)
	w.snapshotState = env.State.Copy()
}

// prepareWork constructs the sealing task according to the given parameters,
// either based on the last chain head or specified parent. In this function
// the pending transactions are not filled yet, only the empty task returned.
func (w *worker) prepareWork(genParams *generateParams) (*core.MinerEnvironment, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Find the parent block for sealing task
	parent := w.chain.CurrentBlock()
	if genParams.parentHash != (common.Hash{}) {
		block := w.chain.GetBlockByHash(genParams.parentHash)
		if block == nil {
			return nil, fmt.Errorf("missing parent")
		}
		parent = block.Header()
	}
	// Sanity check the timestamp correctness, recap the timestamp
	// to parent+1 if the mutation is allowed.
	timestamp := genParams.timestamp
	if parent.Time >= timestamp {
		if genParams.forceTime {
			return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time, timestamp)
		}
		timestamp = parent.Time + 1
	}
	// Construct the sealing block header.
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit, w.config.GasCeil),
		Time:       timestamp,
		Coinbase:   genParams.coinbase,
	}
	// Set the extra field.
	if len(w.extra) != 0 {
		header.Extra = w.extra
	}
	// Set the randomness field from the beacon chain if it's available.
	if genParams.random != (common.Hash{}) {
		header.MixDigest = genParams.random
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = eip1559.CalcBaseFee(w.chainConfig, parent)
		// if !w.chainConfig.IsLondon(parent.Number) {
		// 	parentGasLimit := parent.GasLimit * w.chainConfig.ElasticityMultiplier()
		// 	header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		// }
	}
	// Run the consensus preparation with the default or customized consensus engine.
	if err := w.engine.Prepare(w.chain, header); err != nil {
		log.Error("Failed to prepare header for sealing", "err", err)
		return nil, err
	}
	// Could potentially happen if starting to mine in an odd state.
	// Note genParams.coinbase can be different with header.Coinbase
	// since clique algorithm can modify the coinbase field in header.
	env, err := w.makeEnv(parent.Root, header)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}

	// Handle upgrade build-in system contract code
	systemcontracts.UpgradeBuildInSystemContract(w.chainConfig, header.Number, env.State)

	return env, nil
}

// generateWork generates a sealing block based on the given parameters.
func (w *worker) generateWork(params *generateParams) (*types.Block, *big.Int, error) {
	work, err := w.prepareWork(params)
	if err != nil {
		return nil, nil, err
	}
	defer work.Discard()

	fees := work.State.GetBalance(consensus.SystemAddress)
	block, _, err := w.engine.FinalizeAndAssemble(w.chain, work.Header, work.State, work.PackedTxs, nil, work.Receipts, params.withdrawals)
	if err != nil {
		return nil, nil, err
	}
	return block, fees, nil
}

// commitWork generates several new sealing tasks based on the parent block
// and submit them to the sealer.
func (w *worker) commitWork(interruptCh chan int32, timestamp int64) {
	// Abort committing if node is still syncing
	if w.syncing.Load() {
		return
	}
	start := time.Now()

	// Set the coinbase if the worker is running or it's required
	var coinbase common.Address
	if w.isRunning() {
		coinbase = w.etherbase()
		if coinbase == (common.Address{}) {
			log.Error("Refusing to mine without etherbase")
			return
		}
	}

	// validator can try several times to get the most profitable block,
	// as long as the timestamp is not reached.
	var (
		workList = make([]*multiPackingWork, 0, 20)
		//pReporter = core.NewPuissantReporter()
	)

LOOP:
	for round := 0; round < math.MaxInt64; round++ {
		roundStart := time.Now()

		work, err := w.prepareWork(&generateParams{
			timestamp: uint64(timestamp),
			coinbase:  coinbase,
		})
		if err != nil {
			return
		}
		thisRound := &multiPackingWork{round: round, work: work, income: new(big.Int)}
		workList = append(workList, thisRound)

		timeLeft := blockMiningTimeLeft(work.Header.Time)
		if timeLeft <= 0 {
			log.Warn(" 📦 ⚠️ not enough time for this round, jump out",
				"round", round,
				"income", types.WeiToEther(thisRound.income),
				"elapsed", common.PrettyDuration(time.Since(roundStart)),
				"timeLeft", timeLeft,
			)
			break
		}

		// empty block at first round
		if round == 0 {
			log.Info(" 📦 packing", "round", round, "elapsed", common.PrettyDuration(time.Since(roundStart)))
			continue
		}

		pendingTxs, pendingPuissant := w.eth.TxPool().PendingTxsAndPuissant(false, work.Header.Time)

		var pendings = make(map[common.Address][]*types.Transaction)
		for from, txs := range pendingTxs {
			for _, tx := range txs {
				pendings[from] = append(pendings[from], tx.Tx.Tx)
			}
		}
		report, err := core.RunPuissantCommitter(
			timeLeft,
			work,
			pendings,
			pendingPuissant,
			w.chain,
			w.chainConfig,
			w.eth.TxPool().DeletePuissantPackages,

			lru2.NewCache[common.Hash, struct{}](1000),
		)
		//pReporter.Update(report, round)
		thisRound.err = err
		thisRound.income = work.State.GetBalance(consensus.SystemAddress)

		roundElapsed := time.Since(roundStart)
		select {
		case signal, ok := <-interruptCh:
			if !ok {
				// should never be here, since interruptCh should not be read before
				log.Warn("commit transactions stopped unknown")
				return
			}
			if err := signalToErr(signal); errors.Is(err, errBlockInterruptedByNewHead) {
				log.Debug("commitWork abort", "err", err)
				return
			} else if errors.Is(err, errBlockInterruptedByTimeout) || errors.Is(err, errBlockInterruptedByOutOfGas) {
				// break the loop to get the best work
				log.Debug("commitWork finish", "reason", err)
				break LOOP
			}
		default:
		}

		roundInterval := repackingInterval(roundElapsed, work.Header.Time)

		log.Info(" 📦 packing",
			"round", round,
			"txs", work.PackedTxs.Len(),
			"triedPuissant", len(report),
			"income", types.WeiToEther(thisRound.income),
			"elapsed", common.PrettyDuration(roundElapsed),
			"timeLeft", blockMiningTimeLeft(work.Header.Time),
			"interval", common.PrettyDuration(roundInterval),
			"err", thisRound.err,
		)

		//break LOOP // TODO testnet

		if roundInterval > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), roundInterval)
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-interruptCh:
						cancel()
					}
				}
			}()
			<-ctx.Done()
			if errors.Is(ctx.Err(), context.Canceled) {
				log.Info(" ⚠️ jump out due to interruption")
				return
			}
		} else {
			// no time for another round, commit now
			break LOOP
		}
	}
	// get the most profitable work
	bestWork := pickTheMostProfitableWork(workList)
	w.commit(bestWork.work, w.fullTaskHook, true, start)

	//go pReporter.Done(bestWork.round, initHeader.Number.Uint64(), bestWork.income, w.sendMessage, w.engine.SignText)
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
// Note the assumption is held that the mutation is allowed to the passed env, do
// the deep copy first.
func (w *worker) commit(env *core.MinerEnvironment, interval func(), update bool, start time.Time) error {
	if w.isRunning() {
		if interval != nil {
			interval()
		}

		err := env.State.WaitPipeVerification()
		if err != nil {
			return err
		}
		env.State.CorrectAccountsRoot(w.chain.CurrentBlock().Root)

		// Withdrawals are set to nil here, because this is only called in PoW.
		finalizeStart := time.Now()
		block, receipts, err := w.engine.FinalizeAndAssemble(w.chain, types.CopyHeader(env.Header), env.State, env.PackedTxs, nil, env.Receipts, nil)
		if err != nil {
			return err
		}
		// env.receipts = receipts
		finalizeBlockTimer.UpdateSince(finalizeStart)

		// Create a local environment copy, avoid the data race with snapshot state.
		// https://github.com/ethereum/go-ethereum/issues/24299
		env := env.Copy()

		// If we're post merge, just ignore
		if !w.isTTDReached(block.Header()) {
			select {
			case w.taskCh <- &task{receipts: receipts, state: env.State, block: block, createdAt: time.Now()}:
				fees := env.State.GetBalance(consensus.SystemAddress)
				feesInEther := new(big.Float).Quo(new(big.Float).SetInt(fees), big.NewFloat(params.Ether))
				log.Info("Commit new sealing work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
					"txs", env.TxCount, "gas", block.GasUsed(), "fees", feesInEther, "elapsed", common.PrettyDuration(time.Since(start)))

			case <-w.exitCh:
				log.Info("Worker has exited")
			}
		}
	}
	if update {
		w.updateSnapshot(env)
	}
	return nil
}

// getSealingBlock generates the sealing block based on the given parameters.
// The generation result will be passed back via the given channel no matter
// the generation itself succeeds or not.
func (w *worker) getSealingBlock(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash, withdrawals types.Withdrawals, noTxs bool) (*types.Block, *big.Int, error) {
	req := &getWorkReq{
		params: &generateParams{
			timestamp:   timestamp,
			forceTime:   true,
			parentHash:  parent,
			coinbase:    coinbase,
			random:      random,
			withdrawals: withdrawals,
			noTxs:       noTxs,
		},
		result: make(chan *newPayloadResult, 1),
	}
	select {
	case w.getWorkCh <- req:
		result := <-req.result
		if result.err != nil {
			return nil, nil, result.err
		}
		return result.block, result.fees, nil
	case <-w.exitCh:
		return nil, nil, errors.New("miner closed")
	}
}

// isTTDReached returns the indicator if the given block has reached the total
// terminal difficulty for The Merge transition.
func (w *worker) isTTDReached(header *types.Header) bool {
	td, ttd := w.chain.GetTd(header.ParentHash, header.Number.Uint64()-1), w.chain.Config().TerminalTotalDifficulty
	return td != nil && ttd != nil && td.Cmp(ttd) >= 0
}

// signalToErr converts the interruption signal to a concrete error type for return.
// The given signal must be a valid interruption signal.
func signalToErr(signal int32) error {
	switch signal {
	case commitInterruptNone:
		return nil
	case commitInterruptNewHead:
		return errBlockInterruptedByNewHead
	case commitInterruptResubmit:
		return errBlockInterruptedByRecommit
	case commitInterruptTimeout:
		return errBlockInterruptedByTimeout
	case commitInterruptOutOfGas:
		return errBlockInterruptedByOutOfGas
	default:
		panic(fmt.Errorf("undefined signal %d", signal))
	}
}
