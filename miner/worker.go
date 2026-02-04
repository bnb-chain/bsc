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
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/params"
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

	// the current mining loops could have asynchronous risk of mining block with
	// same height, keep recently mined blocks to avoid double sign for safety,
	recentMinedCacheLimit = 20
)

var (
	bidExistGauge        = metrics.NewRegisteredGauge("worker/bidExist", nil)
	bidWinGauge          = metrics.NewRegisteredGauge("worker/bidWin", nil)
	inturnBlocksGauge    = metrics.NewRegisteredGauge("worker/inturnBlocks", nil)
	bestBidGasUsedGauge  = metrics.NewRegisteredGauge("worker/bestBidGasUsed", nil)  // MGas
	bestWorkGasUsedGauge = metrics.NewRegisteredGauge("worker/bestWorkGasUsed", nil) // MGas

	writeBlockTimer      = metrics.NewRegisteredTimer("worker/writeblock", nil)
	finalizeBlockTimer   = metrics.NewRegisteredTimer("worker/finalizeblock", nil)
	pendingPlainTxsTimer = metrics.NewRegisteredTimer("worker/pendingPlainTxs", nil)
	pendingBlobTxsTimer  = metrics.NewRegisteredTimer("worker/pendingBlobTxs", nil)

	errBlockInterruptedByNewHead   = errors.New("new head arrived while building block")
	errBlockInterruptedByRecommit  = errors.New("recommit interrupt while building block")
	errBlockInterruptedByTimeout   = errors.New("timeout while building block")
	errBlockInterruptedByOutOfGas  = errors.New("out of gas while building block")
	errBlockInterruptedByBetterBid = errors.New("better bid arrived while building block")
)

// environment is the worker's current environment and holds all
// information of the sealing block generation.
type environment struct {
	signer   types.Signer
	state    *state.StateDB // apply state changes here
	tcount   int            // count of non-system transactions in cycle
	gasPool  *core.GasPool  // available gas used to pack transactions
	coinbase common.Address
	evm      *vm.EVM

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
	sidecars types.BlobSidecars
	blobs    int

	witness *stateless.Witness

	committed bool

	// Mining phase timing breakdown
	miningStats *MiningStats
}

// discard terminates the background prefetcher go-routine. It should
// always be called for all created environment instances otherwise
// the go-routine leak can happen.
func (env *environment) discard() {
	if env.state == nil {
		return
	}
	env.state.StopPrefetcher()
}

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts []*types.Receipt
	state    *state.StateDB
	block    *types.Block

	createdAt     time.Time
	miningStartAt time.Time
	sealStartAt   time.Time // When Seal() was called

	// Mining phase timing breakdown
	miningStats *MiningStats
}

// MiningStats holds timing measurements for different phases of block mining.
// Similar to StateDB's timing fields (AccountReads, StorageReads, etc.)
//
// High-level aggregation (use the methods below):
//   - Parlia()    = PrepareTime + FinalizeSystemTxTime + SealTime
//   - MEV()       = MEVCompareTime (waiting is separate)
//   - Execution() = FillTxsTime + ExecutionTime, with IO() = ExecutionIOTime
//   - RootCalc()  = RootCalcTime (state root calculation, note: Assembly is in FinalizeSystemTxTime)
//   - Write()     = WriteTime (separate, database persistence)
//   - Waiting()   = WaitingOutOfTurnTime + WaitingTxCollectTime + WaitingMEVTime (separate)
type MiningStats struct {
	// === Top-level independent timing ===
	// MinePhaseTotal is independently measured from mine start to end (not sum of parts)
	// Use this to cross-check: if MinePhaseTotal >> WallClockTime(), something is missing
	MinePhaseTotal time.Duration // Independent measurement: commitWork start → WriteBlockAndSetHead end
	MinePhaseStart time.Time     // Start time of mine phase (for calculating MinePhaseTotal)

	// === Low-level timing fields ===

	// ========== Parlia consensus - Level 1 (existing) ==========
	PrepareTime time.Duration // engine.Prepare() - set header fields
	SealTime    time.Duration // engine.Seal() - wait for block time + sign

	// ========== Parlia consensus - Level 2 (new breakdown) ==========
	// PrepareTime breakdown:
	//   - HeaderPrepareTime: setting header fields (time, difficulty, extra, coinbase)
	HeaderPrepareTime time.Duration // Header field preparation (= PrepareTime, since Prepare mainly sets header)

	// FinalizeSystemTxTime breakdown (measured in consensus/parlia/parlia.go FinalizeAndAssemble):
	//   - SystemTxExecTime: executing system transactions (slash/distribute/upgrade/init)
	//   - BlockAssemblyTime: IntermediateRoot + tx/receipt roots calculation + types.NewBlock()
	SystemTxExecTime  time.Duration // System transaction execution (init/slash/distribute/upgrade)
	BlockAssemblyTime time.Duration // IntermediateRoot + NewBlock (runs in parallel)

	// SealTime breakdown (measured in consensus/parlia/parlia.go Seal goroutine):
	//   - SealWaitTime: waiting for block timestamp (time.After(delay))
	//   - SealSignTime: vote attestation assembly + signing + extra seal
	SealWaitTime time.Duration // Waiting for block timestamp
	SealSignTime time.Duration // Signing and finalizing

	// ========== Waiting phases (sequential, not overlapping) ==========
	WaitingOutOfTurnTime time.Duration // Waiting-1: out-of-turn validator backoff
	WaitingTxCollectTime time.Duration // Waiting-4: wait for new txs in LOOP_WAIT
	WaitingMEVTime       time.Duration // Waiting-2: MEV window waiting for builder bids

	// ========== Transaction fetching ==========
	FillTxsTime time.Duration // TxPool().Pending() - fetch pending transactions

	// ========== Transaction execution (VM + IO) ==========
	ExecutionTime   time.Duration // commitTransactions total time
	ExecutionIOTime time.Duration // IO portion: AccountReads + StorageReads + AccountUpdates + StorageUpdates

	// ========== MEV - Level 1 (existing) ==========
	MEVCompareTime time.Duration // GetBestBid() + reward comparison

	// ========== MEV - Level 2 (new breakdown) ==========
	//   - GetBestBidTime: time to retrieve best bid from memory
	//   - RewardCompareTime: time to compare rewards and make decision
	GetBestBidTime    time.Duration // Retrieving best bid from bidSimulator
	RewardCompareTime time.Duration // Comparing local vs bid rewards

	// ========== FinalizeAndAssemble (existing) ==========
	// Note: FinalizeSystemTxTime includes system txs (init/slash/distribute) AND types.NewBlock() assembly
	// Note: RootCalcTime only covers state.IntermediateRoot() (state root), not receipt root
	FinalizeSystemTxTime time.Duration // System txs + block assembly (= FinalizeAndAssemble - RootCalcTime)
	RootCalcTime         time.Duration // state.IntermediateRoot() - state trie root calculation

	// ========== Write phase (database persistence) ==========
	WriteTime time.Duration // WriteBlockAndSetHead - write block/receipts/state to DB

	// ========== Network timing - Level 1 (existing) ==========
	BroadcastStartTime int64 // Unix millis when BroadcastBlock(propagate=true) starts

	// ========== Network timing - Level 2 (new breakdown) ==========
	// WriteEndTime: when WriteBlockAndSetHead completes (for LocalProcessDelay calculation)
	// LocalProcessDelay = BroadcastStartTime - WriteEndTime
	//   (includes: Post(NewSealedBlockEvent) + eventmux dispatch + goroutine scheduling)
	WriteEndTime int64 // Unix millis when WriteBlockAndSetHead completes
}

// === High-level aggregation methods ===

// Parlia returns the total Parlia consensus time.
// Includes: header preparation + system transactions + block assembly + seal/sign.
func (ms *MiningStats) Parlia() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.PrepareTime + ms.FinalizeSystemTxTime + ms.SealTime
}

// MEV returns the MEV comparison time (excluding MEV waiting).
// MEV waiting is counted separately in Waiting().
func (ms *MiningStats) MEV() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.MEVCompareTime
}

// Execution returns the total transaction execution time (VM + IO).
// Use IO() to get the IO portion breakdown.
func (ms *MiningStats) Execution() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.FillTxsTime + ms.ExecutionTime
}

// IO returns the IO portion of execution time.
// This is the time spent on state reads/writes during transaction execution.
func (ms *MiningStats) IO() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.ExecutionIOTime
}

// RootCalc returns the state root calculation time.
// Note: This only covers state.IntermediateRoot(), not types.NewBlock() assembly.
func (ms *MiningStats) RootCalc() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.RootCalcTime
}

// Write returns the database write time.
// Includes: WriteBlockAndSetHead (block/receipts/state persistence).
func (ms *MiningStats) Write() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.WriteTime
}

// Waiting returns the total waiting time (strategic/timing waits).
// These are intentional waits, not compute bottlenecks.
func (ms *MiningStats) Waiting() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.WaitingOutOfTurnTime + ms.WaitingTxCollectTime + ms.WaitingMEVTime
}

// ActiveTime returns the sum of all compute/active phases (excluding waiting).
// Use this to find optimization hotspots.
func (ms *MiningStats) ActiveTime() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.PrepareTime + ms.FillTxsTime + ms.ExecutionTime +
		ms.MEVCompareTime + ms.FinalizeSystemTxTime + ms.RootCalcTime + ms.SealTime + ms.WriteTime
}

// WallClockTime returns the total wall clock time (active + waiting).
// Use this to check if 450ms block time budget is sufficient.
func (ms *MiningStats) WallClockTime() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.ActiveTime() + ms.Waiting()
}

// Uncovered returns the time not covered by the breakdown (MinePhaseTotal - WallClockTime).
// If this is significantly positive, there are phases not yet instrumented.
// If this is negative, there may be overlapping measurements.
func (ms *MiningStats) Uncovered() time.Duration {
	if ms == nil {
		return 0
	}
	return ms.MinePhaseTotal - ms.WallClockTime()
}

// String returns a formatted string of mining stats (high-level view).
func (ms *MiningStats) String() string {
	if ms == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"Parlia=%v MEV=%v Execution=%v(IO=%v) RootCalc=%v Write=%v | Waiting=%v | Active=%v WallClock=%v",
		ms.Parlia(), ms.MEV(), ms.Execution(), ms.IO(), ms.RootCalc(), ms.Write(),
		ms.Waiting(), ms.ActiveTime(), ms.WallClockTime(),
	)
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
	commitInterruptTimeout
	commitInterruptOutOfGas
	commitInterruptBetterBid
)

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interruptCh chan int32
	timestamp   int64
}

// newPayloadResult is the result of payload generation.
type newPayloadResult struct {
	err      error
	block    *types.Block
	fees     *big.Int               // total block fees
	sidecars []*types.BlobTxSidecar // collected blobs of blob transactions
	stateDB  *state.StateDB         // StateDB after executing the transactions
	receipts []*types.Receipt       // Receipts collected during construction
	requests [][]byte               // Consensus layer requests collected during block construction
	witness  *stateless.Witness     // Witness is an optional stateless proof
}

// getWorkReq represents a request for getting a new sealing work with provided parameters.
type getWorkReq struct {
	params *generateParams
	result chan *newPayloadResult // non-blocking channel
}

type bidFetcher interface {
	GetBestBid(parentHash common.Hash) *BidRuntime
	GetSimulatingBid(prevBlockHash common.Hash) *BidRuntime
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	bidFetcher  bidFetcher
	prefetcher  core.Prefetcher
	config      *minerconfig.Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	prio        []common.Address // A list of senders to prioritize
	chain       *core.BlockChain

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

	current *environment // An environment for current running cycle.

	confMu   sync.RWMutex // The lock used to protect the config fields: GasCeil, GasTip and Extradata
	coinbase common.Address
	extra    []byte
	tip      *uint256.Int // Minimum tip needed for non-local transaction to include them

	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	// atomic status counters
	running atomic.Bool // The indicator whether the consensus engine is running or not.
	syncing atomic.Bool // The indicator whether the node is still syncing.

	// recommit is the time interval to re-create sealing work or to re-build
	// payload in proof-of-stake stage.
	recommit          time.Duration
	recentMinedBlocks *lru.Cache[uint64, []common.Hash]

	// Test hooks
	newTaskHook  func(*task)                        // Method to call upon receiving a new sealing task.
	skipSealHook func(*task) bool                   // Method to decide whether skipping the sealing.
	fullTaskHook func()                             // Method to call before pushing the full sealing task.
	resubmitHook func(time.Duration, time.Duration) // Method to call upon updating resubmitting interval.
}

func newWorker(config *minerconfig.Config, engine consensus.Engine, eth Backend, mux *event.TypeMux) *worker {
	chainConfig := eth.BlockChain().Config()
	worker := &worker{
		prefetcher:         core.NewStatePrefetcher(chainConfig, eth.BlockChain().HeadChain()),
		config:             config,
		chainConfig:        chainConfig,
		engine:             engine,
		eth:                eth,
		chain:              eth.BlockChain(),
		mux:                mux,
		coinbase:           config.Etherbase,
		extra:              config.ExtraData,
		tip:                uint256.MustFromBig(config.GasPrice),
		pendingTasks:       make(map[common.Hash]*task),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		newWorkCh:          make(chan *newWorkReq),
		getWorkCh:          make(chan *getWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		startCh:            make(chan struct{}, 1),
		exitCh:             make(chan struct{}),
		resubmitIntervalCh: make(chan time.Duration),
		recentMinedBlocks:  lru.NewCache[uint64, []common.Hash](recentMinedCacheLimit),
	}
	// Subscribe events for blockchain
	worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)

	// Sanitize recommit interval if the user-specified one is too short.
	recommit := minRecommitInterval
	if worker.config.Recommit != nil && *worker.config.Recommit > minRecommitInterval {
		recommit = *worker.config.Recommit
	}
	worker.recommit = recommit

	worker.wg.Add(4)
	go worker.mainLoop()
	go worker.newWorkLoop(recommit)
	go worker.resultLoop()
	go worker.taskLoop()

	return worker
}

func (w *worker) setBestBidFetcher(fetcher bidFetcher) {
	w.bidFetcher = fetcher
}

func (w *worker) getPrefetcher() core.Prefetcher {
	return w.prefetcher
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	w.coinbase = addr
}

// etherbase retrieves the configured etherbase address.
func (w *worker) etherbase() common.Address {
	w.confMu.RLock()
	defer w.confMu.RUnlock()
	return w.coinbase
}

func (w *worker) setGasCeil(ceil uint64) {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	w.config.GasCeil = ceil
}

func (w *worker) getGasCeil() uint64 {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	return w.config.GasCeil
}

func (w *worker) getTxGasLimit() uint64 {
	w.confMu.RLock()
	defer w.confMu.RUnlock()
	return w.config.TxGasLimit
}

// setExtra sets the content used to initialize the block extra field.
func (w *worker) setExtra(extra []byte) {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	w.extra = extra
}

// setGasTip sets the minimum miner tip needed to include a non-local transaction.
func (w *worker) setGasTip(tip *big.Int) {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	w.tip = uint256.MustFromBig(tip)
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *worker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

// setPrioAddresses sets a list of addresses to prioritize for transaction inclusion.
func (w *worker) setPrioAddresses(prio []common.Address) {
	w.confMu.Lock()
	defer w.confMu.Unlock()
	w.prio = prio
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
			if interruptCh != nil {
				interruptCh <- commitInterruptNewHead
				close(interruptCh)
				interruptCh = nil
			}
			clearPending(head.Header.Number.Uint64())
			timestamp = time.Now().Unix()
			if p, ok := w.engine.(*parlia.Parlia); ok {
				signedRecent, err := p.SignRecently(w.chain, head.Header)
				if err != nil {
					timer.Reset(recommit)
					log.Debug("Not allowed to propose block", "err", err)
					continue
				}
				if signedRecent {
					timer.Reset(recommit)
					log.Info("Signed recently, must wait")
					continue
				}
			}
			commit(commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() && ((w.chainConfig.Clique != nil &&
				w.chainConfig.Clique.Period > 0) || (w.chainConfig.Parlia != nil)) {
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
	defer func() {
		if w.current != nil {
			w.current.discard()
		}
	}()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interruptCh, req.timestamp)

		case req := <-w.getWorkCh:
			req.result <- w.generateWork(req.params, false)

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
			// Record seal start time
			task.sealStartAt = time.Now()

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

	// [Mining Sliding Window] Statistics collection
	// Window size: 1000 blocks, report every 100 blocks
	const windowSize = 1000
	const reportInterval = 100
	type miningSample struct {
		parlia    time.Duration
		mev       time.Duration
		execution time.Duration
		io        time.Duration
		rootCalc  time.Duration
		write     time.Duration
		waiting   time.Duration
		active    time.Duration
		wallClock time.Duration
		mineTotal time.Duration
	}
	window := make([]miningSample, 0, windowSize)
	blocksProcessed := 0

	// Helper: compute percentile from sorted slice
	percentile := func(sorted []time.Duration, p float64) time.Duration {
		if len(sorted) == 0 {
			return 0
		}
		idx := int(float64(len(sorted)-1) * p)
		return sorted[idx]
	}

	// Helper: sort duration slice
	sortDurations := func(d []time.Duration) {
		slices.Sort(d)
	}

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
				prevParents := prev
				if slices.Contains(prevParents, block.ParentHash()) {
					log.Error("Reject Double Sign!!", "block", block.NumberU64(),
						"hash", block.Hash(),
						"root", block.Root(),
						"ParentHash", block.ParentHash())
					doubleSign = true
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

			// Calculate Seal time (from sealStartAt to now)
			if task.miningStats != nil && !task.sealStartAt.IsZero() {
				task.miningStats.SealTime = time.Since(task.sealStartAt)

				// [Parlia-L2] Read SealWaitTime and SealSignTime from Parlia
				// (set by consensus/parlia/parlia.go Seal goroutine)
				if parliaEngine, ok := w.engine.(*parlia.Parlia); ok {
					if sealTiming := parliaEngine.GetSealTiming(block.Hash()); sealTiming != nil {
						task.miningStats.SealWaitTime = sealTiming.WaitTime
						task.miningStats.SealSignTime = sealTiming.SignTime
					}
				}
			}

			// add BAL to the block
			bal := task.state.GetEncodedBlockAccessList(block)
			if bal != nil && w.engine.SignBAL(bal) == nil {
				block = block.WithBAL(bal)
			}
			task.state.DumpAccessList(block)

			// Commit block and state to database.
			writeStart := time.Now()
			status, err := w.chain.WriteBlockAndSetHead(block, receipts, logs, task.state, w.mux)
			if status != core.CanonStatTy {
				if err != nil {
					log.Error("Failed writing block to chain", "err", err, "status", status)
				} else {
					log.Info("Written block as SideChain and avoid broadcasting", "status", status)
				}
				continue
			}
			writeBlockTimer.UpdateSince(writeStart)

			// Record WriteBlock time and calculate MinePhaseTotal
			if task.miningStats != nil {
				task.miningStats.WriteTime = time.Since(writeStart)
				// [Network-L2] Record WriteEndTime for LocalProcessDelay calculation
				// LocalProcessDelay = BroadcastStartTime - WriteEndTime
				task.miningStats.WriteEndTime = time.Now().UnixMilli()
				// Calculate independent total time (for cross-checking)
				if !task.miningStats.MinePhaseStart.IsZero() {
					task.miningStats.MinePhaseTotal = time.Since(task.miningStats.MinePhaseStart)
				}
			}

			stats := w.chain.GetBlockStats(block.Hash())
			stats.SendBlockTime.Store(time.Now().UnixMilli())
			stats.StartMiningTime.Store(task.miningStartAt.UnixMilli())
			// [Network-L2] Store WriteEndTime in BlockStats for LocalProcessDelay calculation
			if task.miningStats != nil {
				stats.WriteEndTime.Store(task.miningStats.WriteEndTime)
			}

			// Log mining stats breakdown (high-level aggregation)
			if task.miningStats != nil {
				ms := task.miningStats
				log.Info("Mining phase breakdown",
					"number", block.Number(),
					"hash", hash.TerminalString(),
					// High-level phases (what you want to see)
					"Parlia", ms.Parlia(),
					"MEV", ms.MEV(),
					"Execution", ms.Execution(),
					"IO", ms.IO(),
					"RootCalc", ms.RootCalc(),
					"Write", ms.Write(),
					"Waiting", ms.Waiting(),
					// Summary (WallClock = sum of parts, MinePhaseTotal = independent measurement)
					"Active", ms.ActiveTime(),
					"WallClock", ms.WallClockTime(),
					"MineTotal", ms.MinePhaseTotal,  // Independent measurement for cross-check
					"Uncovered", ms.Uncovered(),     // MineTotal - WallClock (should be ~0 if complete)
				)
				// Detailed breakdown for debugging (Level 2 breakdown)
				log.Debug("Mining stats detail - Parlia",
					"number", block.Number(),
					// Parlia Level 1
					"prepare", ms.PrepareTime,
					"finalizeSysTx", ms.FinalizeSystemTxTime,
					"seal", ms.SealTime,
					// Parlia Level 2 (new)
					"headerPrepare", ms.HeaderPrepareTime,
					"systemTxExec", ms.SystemTxExecTime,
					"blockAssembly", ms.BlockAssemblyTime,
					"sealWait", ms.SealWaitTime,
					"sealSign", ms.SealSignTime,
				)
				log.Debug("Mining stats detail - Execution & MEV",
					"number", block.Number(),
					// Execution breakdown
					"fillTxs", ms.FillTxsTime,
					"execTime", ms.ExecutionTime,
					"execIO", ms.ExecutionIOTime,
					// MEV Level 1
					"mevCompare", ms.MEVCompareTime,
					// MEV Level 2 (new)
					"getBestBid", ms.GetBestBidTime,
					"rewardCompare", ms.RewardCompareTime,
				)
				log.Debug("Mining stats detail - Waiting & Network",
					"number", block.Number(),
					// Waiting breakdown
					"waitOutOfTurn", ms.WaitingOutOfTurnTime,
					"waitTxCollect", ms.WaitingTxCollectTime,
					"waitMEV", ms.WaitingMEVTime,
					// Network Level 2 (new)
					"writeEndTime", ms.WriteEndTime,
					"broadcastStartTime", ms.BroadcastStartTime,
				)

				// [Mining Sliding Window] Add sample to window
				sample := miningSample{
					parlia:    ms.Parlia(),
					mev:       ms.MEV(),
					execution: ms.Execution(),
					io:        ms.IO(),
					rootCalc:  ms.RootCalc(),
					write:     ms.Write(),
					waiting:   ms.Waiting(),
					active:    ms.ActiveTime(),
					wallClock: ms.WallClockTime(),
					mineTotal: ms.MinePhaseTotal,
				}
				window = append(window, sample)
				if len(window) > windowSize {
					window = window[1:] // Remove oldest
				}
				blocksProcessed++

				// Report sliding window stats every N blocks
				if blocksProcessed%reportInterval == 0 && len(window) >= reportInterval {
					// Collect all values for percentile calculation
					parliaVals := make([]time.Duration, len(window))
					mevVals := make([]time.Duration, len(window))
					execVals := make([]time.Duration, len(window))
					ioVals := make([]time.Duration, len(window))
					rootVals := make([]time.Duration, len(window))
					writeVals := make([]time.Duration, len(window))
					waitVals := make([]time.Duration, len(window))
					activeVals := make([]time.Duration, len(window))
					wallClockVals := make([]time.Duration, len(window))
					mineTotalVals := make([]time.Duration, len(window))

					for i, s := range window {
						parliaVals[i] = s.parlia
						mevVals[i] = s.mev
						execVals[i] = s.execution
						ioVals[i] = s.io
						rootVals[i] = s.rootCalc
						writeVals[i] = s.write
						waitVals[i] = s.waiting
						activeVals[i] = s.active
						wallClockVals[i] = s.wallClock
						mineTotalVals[i] = s.mineTotal
					}

					// Sort for percentile calculation
					sortDurations(parliaVals)
					sortDurations(mevVals)
					sortDurations(execVals)
					sortDurations(ioVals)
					sortDurations(rootVals)
					sortDurations(writeVals)
					sortDurations(waitVals)
					sortDurations(activeVals)
					sortDurations(wallClockVals)
					sortDurations(mineTotalVals)

					// Output Mining Sliding Window with all key metrics
				// Format: Parlia, MEV, Execution (VM), IO, RootCalc, Write, Waiting, Active, Total
				log.Info("Mining stats (sliding window)",
						"samples", len(window),
						// === Parlia (consensus engine) ===
						"Parlia_p50", percentile(parliaVals, 0.5),
						"Parlia_p90", percentile(parliaVals, 0.9),
						"Parlia_p99", percentile(parliaVals, 0.99),
						// === MEV (builder bid comparison) ===
						"MEV_p50", percentile(mevVals, 0.5),
						"MEV_p90", percentile(mevVals, 0.9),
						"MEV_p99", percentile(mevVals, 0.99),
						// === Execution (VM - transaction execution) ===
						"Exec_p50", percentile(execVals, 0.5),
						"Exec_p90", percentile(execVals, 0.9),
						"Exec_p99", percentile(execVals, 0.99),
						// === IO (state reads/writes during execution) ===
						"IO_p50", percentile(ioVals, 0.5),
						"IO_p90", percentile(ioVals, 0.9),
						"IO_p99", percentile(ioVals, 0.99),
						// === RootCalc (state root calculation) ===
						"RootCalc_p50", percentile(rootVals, 0.5),
						"RootCalc_p90", percentile(rootVals, 0.9),
						"RootCalc_p99", percentile(rootVals, 0.99),
						// === Write (database persistence) ===
						"Write_p50", percentile(writeVals, 0.5),
						"Write_p90", percentile(writeVals, 0.9),
						"Write_p99", percentile(writeVals, 0.99),
						// === Waiting (strategic waits) ===
						"Wait_p50", percentile(waitVals, 0.5),
						"Wait_p90", percentile(waitVals, 0.9),
						"Wait_p99", percentile(waitVals, 0.99),
						// === Active (compute time excluding waits) ===
						"Active_p50", percentile(activeVals, 0.5),
						"Active_p90", percentile(activeVals, 0.9),
						"Active_p99", percentile(activeVals, 0.99),
						// === Total (wall clock including waiting) ===
						"Total_p50", percentile(mineTotalVals, 0.5),
						"Total_p90", percentile(mineTotalVals, 0.9),
						"Total_p99", percentile(mineTotalVals, 0.99),
					)
				}
			}

			log.Info("Successfully seal and write new block", "number", block.Number(), "hash", hash, "time", block.Header().MilliTimestamp(), "sealhash", sealhash,
				"block size(noBal)", block.Size(), "balSize", block.BALSize(), "elapsed", common.PrettyDuration(time.Since(task.createdAt)))
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

		case <-w.exitCh:
			return
		}
	}
}

// makeEnv creates a new environment for the sealing block.
func (w *worker) makeEnv(parent *types.Header, header *types.Header, coinbase common.Address,
	prevEnv *environment, witness bool) (*environment, error) {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit
	state, err := w.chain.StateWithCacheAt(parent.Root)
	if err != nil {
		return nil, err
	}
	if witness {
		bundle, err := stateless.NewWitness(header, w.chain)
		if err != nil {
			return nil, err
		}
		state.StartPrefetcher("miner", bundle)
	} else {
		if prevEnv == nil {
			state.StartPrefetcher("miner", nil)
		} else {
			state.TransferPrefetcher(prevEnv.state)
		}
	}

	// Note the passed coinbase may be different with header.Coinbase.
	env := &environment{
		signer:   types.MakeSigner(w.chainConfig, header.Number, header.Time),
		state:    state,
		coinbase: coinbase,
		header:   header,
		witness:  state.Witness(),
		evm:      vm.NewEVM(core.NewEVMBlockContext(header, w.chain, &coinbase), state, w.chainConfig, vm.Config{}),
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0
	return env, nil
}

func (w *worker) commitTransaction(env *environment, tx *types.Transaction, receiptProcessors ...core.ReceiptProcessor) ([]*types.Log, error) {
	if tx.Type() == types.BlobTxType {
		return w.commitBlobTransaction(env, tx, receiptProcessors...)
	}

	receipt, err := w.applyTransaction(env, tx, receiptProcessors...)
	if err != nil {
		return nil, err
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)
	return receipt.Logs, nil
}

func (w *worker) commitBlobTransaction(env *environment, tx *types.Transaction, receiptProcessors ...core.ReceiptProcessor) ([]*types.Log, error) {
	sc := types.NewBlobSidecarFromTx(tx)
	if sc == nil {
		panic("blob transaction without blobs in miner")
	}
	// Checking against blob gas limit: It's kind of ugly to perform this check here, but there
	// isn't really a better place right now. The blob gas limit is checked at block validation time
	// and not during execution. This means core.ApplyTransaction will not return an error if the
	// tx has too many blobs. So we have to explicitly check it here.
	maxBlobs := eip4844.MaxBlobsPerBlock(w.chainConfig, env.header.Time)
	if env.blobs+len(sc.Blobs) > maxBlobs {
		return nil, errors.New("max data blobs reached")
	}

	receipt, err := w.applyTransaction(env, tx, receiptProcessors...)
	if err != nil {
		return nil, err
	}
	sc.TxIndex = uint64(len(env.txs))
	env.txs = append(env.txs, tx.WithoutBlobTxSidecar())
	env.receipts = append(env.receipts, receipt)
	env.sidecars = append(env.sidecars, sc)
	env.blobs += len(sc.Blobs)
	*env.header.BlobGasUsed += receipt.BlobGasUsed
	return receipt.Logs, nil
}

// applyTransaction runs the transaction. If execution fails, state and gas pool are reverted.
func (w *worker) applyTransaction(env *environment, tx *types.Transaction, receiptProcessors ...core.ReceiptProcessor) (*types.Receipt, error) {
	var (
		snap = env.state.Snapshot()
		gp   = env.gasPool.Gas()
	)

	receipt, err := core.ApplyTransaction(env.evm, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, receiptProcessors...)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
	}
	return receipt, err
}

func (w *worker) commitTransactions(env *environment, plainTxs, blobTxs *transactionsByPriceAndNonce,
	interruptCh chan int32, stopTimer *time.Timer) error {
	gasLimit := env.header.GasLimit
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(gasLimit)
		if p, ok := w.engine.(*parlia.Parlia); ok {
			gasReserved := p.EstimateGasReservedForSystemTxs(w.chain, env.header)
			env.gasPool.SubGas(gasReserved)
			log.Debug("commitTransactions", "number", env.header.Number.Uint64(), "time", env.header.Time, "EstimateGasReservedForSystemTxs", gasReserved)
		}
	}

	// initialize bloom processors
	processorCapacity := 100
	if plainTxs.CurrentSize() < processorCapacity {
		processorCapacity = plainTxs.CurrentSize()
	}
	bloomProcessors := core.NewAsyncReceiptBloomGenerator(processorCapacity)
	defer bloomProcessors.Close()

	stopPrefetchCh := make(chan struct{})
	defer close(stopPrefetchCh)
	// prefetch plainTxs txs, don't bother to prefetch a few blobTxs
	txsPrefetch := plainTxs.Copy()
	tx := txsPrefetch.PeekWithUnwrap()
	if tx != nil {
		txCurr := &tx
		w.prefetcher.PrefetchMining(txsPrefetch, env.header, env.gasPool.Gas(), env.state.StateForPrefetch(), *w.chain.GetVMConfig(), stopPrefetchCh, txCurr)
	}

	signal := commitInterruptNone
LOOP:
	for {
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
		}
		// If we don't have enough gas for any further transactions then we're done.
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			signal = commitInterruptOutOfGas
			break
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

		// If we don't have enough blob space for any further blob transactions,
		// skip that list altogether
		if !blobTxs.Empty() && env.blobs >= eip4844.MaxBlobsPerBlock(w.chainConfig, env.header.Time) {
			log.Trace("Not enough blob space for further blob transactions")
			blobTxs.Clear()
			// Fall though to pick up any plain txs
		}
		// Retrieve the next transaction and abort if all done.
		var (
			ltx *txpool.LazyTransaction
			txs *transactionsByPriceAndNonce
		)
		pltx, ptip := plainTxs.Peek()
		bltx, btip := blobTxs.Peek()

		switch {
		case pltx == nil:
			txs, ltx = blobTxs, bltx
		case bltx == nil:
			txs, ltx = plainTxs, pltx
		default:
			if ptip.Lt(btip) {
				txs, ltx = blobTxs, bltx
			} else {
				txs, ltx = plainTxs, pltx
			}
		}
		if ltx == nil {
			break
		}

		// If we don't have enough space for the next transaction, skip the account.
		if env.gasPool.Gas() < ltx.Gas {
			log.Trace("Not enough gas left for transaction", "hash", ltx.Hash, "left", env.gasPool.Gas(), "needed", ltx.Gas)
			txs.Pop()
			continue
		}

		// Most of the blob gas logic here is agnostic as to if the chain supports
		// blobs or not, however the max check panics when called on a chain without
		// a defined schedule, so we need to verify it's safe to call.
		if w.chainConfig.IsCancun(env.header.Number, env.header.Time) {
			left := eip4844.MaxBlobsPerBlock(w.chainConfig, env.header.Time) - env.blobs
			if left < int(ltx.BlobGas/params.BlobTxBlobGasPerBlob) {
				log.Trace("Not enough blob space left for transaction", "hash", ltx.Hash, "left", left, "needed", ltx.BlobGas/params.BlobTxBlobGasPerBlob)
				txs.Pop()
				continue
			}
		}

		// Transaction seems to fit, pull it up from the pool
		tx := ltx.Resolve()
		if tx == nil {
			log.Trace("Ignoring evicted transaction", "hash", ltx.Hash)
			txs.Pop()
			continue
		}

		// Make sure all transactions after osaka have cell proofs
		if w.chainConfig.IsOsaka(env.header.Number, env.header.Time) {
			if sidecar := tx.BlobTxSidecar(); sidecar != nil {
				if sidecar.Version == 0 {
					log.Info("Including blob tx with v0 sidecar, recomputing proofs", "hash", ltx.Hash)
					sidecar.Proofs = make([]kzg4844.Proof, 0, len(sidecar.Blobs)*kzg4844.CellProofsPerBlob)
					for _, blob := range sidecar.Blobs {
						cellProofs, err := kzg4844.ComputeCellProofs(&blob)
						if err != nil {
							panic(err)
						}
						sidecar.Proofs = append(sidecar.Proofs, cellProofs...)
					}
				}
			}
		}

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance in the transaction pool.
		from, _ := types.Sender(env.signer, tx)

		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring replay protected transaction", "hash", ltx.Hash, "eip155", w.chainConfig.EIP155Block)
			txs.Pop()
			continue
		}
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)

		_, err := w.commitTransaction(env, tx, bloomProcessors)
		switch {
		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "hash", ltx.Hash, "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, nil):
			// Everything ok, shift in the next transaction from the same account
			env.tcount++
			txs.Shift()

		default:
			// Transaction is regarded as invalid, drop all consecutive transactions from
			// the same sender because of `nonce-too-high` clause.
			log.Debug("Transaction failed, account skipped", "hash", ltx.Hash, "err", err)
			txs.Pop()
		}
	}

	return signalToErr(signal)
}

// generateParams wraps various of settings for generating sealing task.
type generateParams struct {
	timestamp   uint64            // The timestamp for sealing task
	forceTime   bool              // Flag whether the given timestamp is immutable or not
	parentHash  common.Hash       // Parent block hash, empty means the latest chain head
	coinbase    common.Address    // The fee recipient address for including transaction
	random      common.Hash       // The randomness generated by beacon chain, empty before the merge
	withdrawals types.Withdrawals // List of withdrawals to include in block.
	prevWork    *environment
	beaconRoot  *common.Hash // The beacon root (cancun field).
	noTxs       bool         // Flag whether an empty block without any transaction is expected
}

// prepareWork constructs the sealing task according to the given parameters,
// either based on the last chain head or specified parent. In this function
// the pending transactions are not filled yet, only the empty task returned.
func (w *worker) prepareWork(genParams *generateParams, witness bool) (*environment, error) {
	w.confMu.RLock()
	defer w.confMu.RUnlock()

	// Find the parent block for sealing task
	parent := w.chain.CurrentBlock()
	if genParams.parentHash != (common.Hash{}) {
		block := w.chain.GetBlockByHash(genParams.parentHash)
		if block == nil {
			return nil, errors.New("missing parent")
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
		if w.chainConfig.Parlia == nil && !w.chainConfig.IsLondon(parent.Number) {
			parentGasLimit := parent.GasLimit * w.chainConfig.ElasticityMultiplier()
			header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		}
	}
	// Run the consensus preparation with the default or customized consensus engine.
	// Note that the `header.Time` may be changed.
	if err := w.engine.Prepare(w.chain, header); err != nil {
		log.Error("Failed to prepare header for sealing", "err", err)
		return nil, err
	}
	// Apply EIP-4844, EIP-4788.
	if w.chainConfig.IsCancun(header.Number, header.Time) {
		var excessBlobGas uint64
		if w.chainConfig.IsCancun(parent.Number, parent.Time) {
			excessBlobGas = eip4844.CalcExcessBlobGas(w.chainConfig, parent, header.Time)
		}
		header.BlobGasUsed = new(uint64)
		header.ExcessBlobGas = &excessBlobGas
		if w.chainConfig.Parlia == nil {
			header.ParentBeaconRoot = genParams.beaconRoot
		} else {
			header.WithdrawalsHash = &types.EmptyWithdrawalsHash
			if w.chainConfig.IsBohr(header.Number, header.Time) {
				header.ParentBeaconRoot = new(common.Hash)
			}
			if w.chainConfig.IsPrague(header.Number, header.Time) {
				header.RequestsHash = &types.EmptyRequestsHash
			}
		}
	}
	// Could potentially happen if starting to mine in an odd state.
	// Note genParams.coinbase can be different with header.Coinbase
	// since clique algorithm can modify the coinbase field in header.
	env, err := w.makeEnv(parent, header, genParams.coinbase, genParams.prevWork, witness)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}

	// Handle upgrade built-in system contract code
	systemcontracts.TryUpdateBuildInSystemContract(w.chainConfig, header.Number, parent.Time, header.Time, env.state, true)

	if header.ParentBeaconRoot != nil {
		core.ProcessBeaconBlockRoot(*header.ParentBeaconRoot, env.evm)
	}

	if w.chainConfig.IsPrague(header.Number, header.Time) {
		core.ProcessParentBlockHash(header.ParentHash, env.evm)
	}
	return env, nil
}

// fillTransactions retrieves the pending transactions from the txpool and fills them
// into the given sealing block. The transaction selection and ordering strategy can
// be customized with the plugin in the future.
func (w *worker) fillTransactions(interruptCh chan int32, env *environment, stopTimer *time.Timer, bidTxs mapset.Set[common.Hash]) (err error) {
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

	if cap := w.getTxGasLimit(); cap > 0 {
		filter.GasLimitCap = cap
	}

	// Track FillTxs time (fetching from txpool)
	fillTxsStart := time.Now()

	filter.OnlyPlainTxs, filter.OnlyBlobTxs = true, false
	plainTxsStart := time.Now()
	pendingPlainTxs := w.eth.TxPool().Pending(filter)
	pendingPlainTxsTimer.UpdateSince(plainTxsStart)

	filter.OnlyPlainTxs, filter.OnlyBlobTxs = false, true
	blobTxsStart := time.Now()
	pendingBlobTxs := w.eth.TxPool().Pending(filter)
	pendingBlobTxsTimer.UpdateSince(blobTxsStart)

	if bidTxs != nil {
		filterBidTxs := func(commonTxs map[common.Address][]*txpool.LazyTransaction) {
			for acc, txs := range commonTxs {
				for i := len(txs) - 1; i >= 0; i-- {
					if bidTxs.Contains(txs[i].Hash) {
						if i == len(txs)-1 {
							delete(commonTxs, acc)
						} else {
							commonTxs[acc] = txs[i+1:]
						}
						break
					}
				}
			}
		}

		filterBidTxs(pendingPlainTxs)
		filterBidTxs(pendingBlobTxs)
	}

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

	// Record FillTxs time before execution
	if env.miningStats != nil {
		env.miningStats.FillTxsTime += time.Since(fillTxsStart)
	}

	// Fill the block with all available pending transactions.
	// Track Execution time separately (high-level, not per-tx)
	execStart := time.Now()

	// Record IO baseline from statedb before execution (high-level IO measurement)
	// We calculate IO delta only once at the end using defer to avoid duplication
	var ioBaseline time.Duration
	if env.miningStats != nil && env.state != nil {
		ioBaseline = env.state.AccountReads + env.state.StorageReads +
			env.state.AccountUpdates + env.state.StorageUpdates
	}

	// Use a named return and defer to ensure we record stats exactly once
	var execErr error
	defer func() {
		if env.miningStats != nil && env.state != nil {
			env.miningStats.ExecutionTime += time.Since(execStart)
			// Calculate IO delta (only once, at function exit)
			ioNow := env.state.AccountReads + env.state.StorageReads +
				env.state.AccountUpdates + env.state.StorageUpdates
			env.miningStats.ExecutionIOTime += ioNow - ioBaseline
		}
	}()

	if len(prioPlainTxs) > 0 || len(prioBlobTxs) > 0 {
		plainTxs := newTransactionsByPriceAndNonce(env.signer, prioPlainTxs, env.header.BaseFee)
		blobTxs := newTransactionsByPriceAndNonce(env.signer, prioBlobTxs, env.header.BaseFee)

		if execErr = w.commitTransactions(env, plainTxs, blobTxs, interruptCh, stopTimer); execErr != nil {
			return execErr
		}
	}
	if len(normalPlainTxs) > 0 || len(normalBlobTxs) > 0 {
		plainTxs := newTransactionsByPriceAndNonce(env.signer, normalPlainTxs, env.header.BaseFee)
		blobTxs := newTransactionsByPriceAndNonce(env.signer, normalBlobTxs, env.header.BaseFee)

		if execErr = w.commitTransactions(env, plainTxs, blobTxs, interruptCh, stopTimer); execErr != nil {
			return execErr
		}
	}

	return nil
}

// generateWork generates a sealing block based on the given parameters.
func (w *worker) generateWork(params *generateParams, witness bool) *newPayloadResult {
	work, err := w.prepareWork(params, witness)
	if err != nil {
		return &newPayloadResult{err: err}
	}
	defer work.discard()

	if !params.noTxs {
		interrupt := new(atomic.Int32)
		timer := time.AfterFunc(*w.config.Recommit, func() {
			interrupt.Store(commitInterruptTimeout)
		})
		defer timer.Stop()

		err := w.fillTransactions(nil, work, nil, nil)
		if errors.Is(err, errBlockInterruptedByTimeout) {
			log.Warn("Block building is interrupted", "allowance", common.PrettyDuration(w.recommit))
		}
	}
	body := types.Body{Transactions: work.txs, Withdrawals: params.withdrawals}
	allLogs := make([]*types.Log, 0)
	for _, r := range work.receipts {
		allLogs = append(allLogs, r.Logs...)
	}
	// Collect consensus-layer requests if Prague is enabled.
	var requests [][]byte
	if w.chainConfig.IsPrague(work.header.Number, work.header.Time) && w.chainConfig.Parlia == nil {
		requests = [][]byte{}
		// EIP-6110 deposits
		if err := core.ParseDepositLogs(&requests, allLogs, w.chainConfig); err != nil {
			return &newPayloadResult{err: err}
		}
		// EIP-7002
		if err := core.ProcessWithdrawalQueue(&requests, work.evm); err != nil {
			return &newPayloadResult{err: err}
		}
		// EIP-7251 consolidations
		if err := core.ProcessConsolidationQueue(&requests, work.evm); err != nil {
			return &newPayloadResult{err: err}
		}
	}
	if requests != nil {
		reqHash := types.CalcRequestsHash(requests)
		work.header.RequestsHash = &reqHash
	}

	fees := work.state.GetBalance(consensus.SystemAddress)
	block, receipts, err := w.engine.FinalizeAndAssemble(w.chain, work.header, work.state, &body, work.receipts, nil)
	if err != nil {
		return &newPayloadResult{err: err}
	}

	return &newPayloadResult{
		block:    block,
		fees:     fees.ToBig(),
		sidecars: work.sidecars.BlobTxSidecarList(),
		stateDB:  work.state,
		receipts: receipts,
		requests: requests,
		witness:  work.witness,
	}
}

// commitWork generates several new sealing tasks based on the parent block
// and submit them to the sealer.
func (w *worker) commitWork(interruptCh chan int32, timestamp int64) {
	// Abort committing if node is still syncing
	if w.syncing.Load() {
		return
	}
	start := time.Now()

	// Initialize mining stats for timing breakdown
	miningStats := &MiningStats{
		MinePhaseStart: start, // Record start time for independent total measurement
	}

	// Set the coinbase if the worker is running or it's required
	var coinbase common.Address
	if w.isRunning() {
		coinbase = w.etherbase()
		if coinbase == (common.Address{}) {
			log.Error("Refusing to mine without etherbase")
			return
		}
	}

	stopTimer := time.NewTimer(0)
	defer stopTimer.Stop()
	<-stopTimer.C // discard the initial tick

	stopWaitTimer := time.NewTimer(0)
	defer stopWaitTimer.Stop()
	<-stopWaitTimer.C // discard the initial tick

	// validator can try several times to get the most profitable block,
	// as long as the timestamp is not reached.
	workList := make([]*environment, 0, 10)
	parentHash := w.chain.CurrentBlock().Hash()
	var prevWork *environment
	// workList clean up
	defer func() {
		for _, wk := range workList {
			// only keep the best work, discard others.
			if wk == w.current {
				continue
			}
			wk.discard()
		}
	}()

LOOP:
	for {
		// Track Prepare time (Parlia.Prepare is called inside prepareWork)
		prepareStart := time.Now()
		work, err := w.prepareWork(&generateParams{
			timestamp:  uint64(timestamp),
			parentHash: parentHash,
			coinbase:   coinbase,
			prevWork:   prevWork,
		}, false)
		if err != nil {
			return
		}
		miningStats.PrepareTime += time.Since(prepareStart)
		// [Parlia-L2] HeaderPrepareTime is essentially the same as PrepareTime
		// because engine.Prepare() mainly sets header fields (time, difficulty, extra, coinbase)
		miningStats.HeaderPrepareTime += time.Since(prepareStart)

		// Attach miningStats to work environment
		work.miningStats = miningStats

		prevWork = work
		workList = append(workList, work)

		delay := w.engine.Delay(w.chain, work.header, w.config.DelayLeftOver)
		if delay == nil {
			log.Warn("commitWork delay is nil, something is wrong")
			stopTimer = nil
		} else if *delay <= 0 {
			log.Debug("Not enough time for commitWork")
			break
		} else {
			// [Waiting-1] Out-of-turn backoff
			if !w.inTurn() && len(workList) == 1 {
				if parliaEngine, ok := w.engine.(*parlia.Parlia); ok {
					// When mining out of turn, continuous access to the txpool and trie database
					// may cause lock contention, slowing down transaction insertion and block importing.
					// Applying a backoff delay mitigates this issue and significantly reduces CPU usage.
					if blockInterval, err := parliaEngine.BlockInterval(w.chain, w.chain.CurrentBlock()); err == nil {
						beforeSealing := time.Until(time.UnixMilli(int64(work.header.MilliTimestamp())))
						if wait := beforeSealing - time.Duration(blockInterval)*time.Millisecond; wait > 0 {
							log.Debug("Applying backoff before mining", "block", work.header.Number, "waiting(ms)", wait.Milliseconds())
							waitStart := time.Now()
							select {
							case <-time.After(wait):
								// Record Waiting-1: out-of-turn backoff time
								if miningStats != nil {
									miningStats.WaitingOutOfTurnTime += time.Since(waitStart)
								}
							case <-interruptCh:
								log.Debug("CommitWork interrupted: new block imported or resubmission triggered", "block", work.header.Number)
								return
							}
						}
					}
				}
			}
			log.Debug("commitWork stopTimer", "block", work.header.Number,
				"header time", time.UnixMilli(int64(work.header.MilliTimestamp())),
				"commit delay", *delay, "DelayLeftOver", w.config.DelayLeftOver)
			stopTimer.Reset(*delay)
		}

		// subscribe before fillTransactions
		txsCh := make(chan core.NewTxsEvent, txChanSize)
		// Subscribe for transaction insertion events (whether from network or resurrects)
		sub := w.eth.TxPool().SubscribeTransactions(txsCh, true)
		// if TxPool has been stopped, `sub` would be nil, it could happen on shutdown.
		if sub == nil {
			log.Info("commitWork SubscribeTransactions return nil")
		} else {
			defer sub.Unsubscribe()
		}

		// Fill pending transactions from the txpool into the block.
		fillStart := time.Now()
		err = w.fillTransactions(interruptCh, work, stopTimer, nil)
		fillDuration := time.Since(fillStart)
		switch {
		case errors.Is(err, errBlockInterruptedByNewHead):
			// work.discard()
			log.Debug("commitWork abort", "err", err)
			return
		case errors.Is(err, errBlockInterruptedByRecommit):
			fallthrough
		case errors.Is(err, errBlockInterruptedByTimeout):
			fallthrough
		case errors.Is(err, errBlockInterruptedByOutOfGas):
			// break the loop to get the best work
			log.Debug("commitWork finish", "reason", err)
			break LOOP
		}

		if interruptCh == nil || stopTimer == nil {
			// it is single commit work, no need to try several time.
			log.Info("commitWork interruptCh or stopTimer is nil")
			break
		}

		newTxsNum := 0
		// stopTimer was the maximum delay for each fillTransactions
		// but now it is used to wait until (head.Time - DelayLeftOver) is reached.
		stopTimer.Reset(time.Until(time.UnixMilli(int64(work.header.MilliTimestamp()))) - *w.config.DelayLeftOver)

		// [Waiting-4] Tx-collection / resubmit waiting
		txCollectWaitStart := time.Now()
	LOOP_WAIT:
		for {
			select {
			case <-stopTimer.C:
				log.Debug("commitWork stopTimer expired")
				// Record Waiting-4: tx-collection waiting time
				if miningStats != nil {
					miningStats.WaitingTxCollectTime += time.Since(txCollectWaitStart)
				}
				break LOOP
			case <-interruptCh:
				log.Debug("commitWork interruptCh closed, new block imported or resubmit triggered")
				return
			case ev := <-txsCh:
				delay := w.engine.Delay(w.chain, work.header, w.config.DelayLeftOver)
				log.Debug("commitWork txsCh arrived", "fillDuration", fillDuration.String(),
					"delay", delay.String(), "work.tcount", work.tcount,
					"newTxsNum", newTxsNum, "len(ev.Txs)", len(ev.Txs))
				if *delay < fillDuration {
					// There may not have enough time for another fillTransactions.
					// Record Waiting-4: tx-collection waiting time
					if miningStats != nil {
						miningStats.WaitingTxCollectTime += time.Since(txCollectWaitStart)
					}
					break LOOP
				} else if *delay < fillDuration*2 {
					// We can schedule another fillTransactions, but the time is limited,
					// probably it is the last chance, schedule it immediately.
					// Record Waiting-4: tx-collection waiting time
					if miningStats != nil {
						miningStats.WaitingTxCollectTime += time.Since(txCollectWaitStart)
					}
					break LOOP_WAIT
				} else {
					// There is still plenty of time left.
					// We can wait a while to collect more transactions before
					// schedule another fillTransaction to reduce CPU cost.
					// There will be 2 cases to schedule another fillTransactions:
					//   1.newTxsNum >= work.tcount
					//   2.no much time left, have to schedule it immediately.
					newTxsNum = newTxsNum + len(ev.Txs)
					if newTxsNum >= work.tcount {
						// Record Waiting-4: tx-collection waiting time
						if miningStats != nil {
							miningStats.WaitingTxCollectTime += time.Since(txCollectWaitStart)
						}
						break LOOP_WAIT
					}
					stopWaitTimer.Reset(*delay - fillDuration*2)
				}
			case <-stopWaitTimer.C:
				if newTxsNum > 0 {
					// Record Waiting-4: tx-collection waiting time
					if miningStats != nil {
						miningStats.WaitingTxCollectTime += time.Since(txCollectWaitStart)
					}
					break LOOP_WAIT
				}
			}
		}
		// if sub's channel if full, it will block other NewTxsEvent subscribers,
		// so unsubscribe ASAP and Unsubscribe() is re-enterable, safe to call several time.
		if sub != nil {
			sub.Unsubscribe()
		}
	}
	// get the most profitable work
	bestWork := workList[0]
	bestReward := new(uint256.Int)
	for i, wk := range workList {
		balance := wk.state.GetBalance(consensus.SystemAddress)
		log.Debug("Get the most profitable work", "index", i, "balance", balance, "bestReward", bestReward)
		if balance.Cmp(bestReward) > 0 {
			bestWork = wk
			bestReward = balance
		}
	}

	// when out-turn, use bestWork to prevent bundle leakage.
	// when in-turn, compare with remote work.
	if w.bidFetcher != nil && bestWork.header.Difficulty.Cmp(diffInTurn) == 0 {
		inturnBlocksGauge.Inc(1)
		// We want to start sealing the block as late as possible here if mev is enabled, so we could give builder the chance to send their final bid.
		// Time left till sealing the block.
		tillSealingTime := time.Until(time.UnixMilli(int64(bestWork.header.MilliTimestamp()))) - *w.config.DelayLeftOver
		if tillSealingTime > 0 {
			// [Waiting-2] MEV window waiting
			// Still some time left, wait for the best bid.
			// This happens during the peak time of the network, the local block building LOOP would break earlier than
			// the final sealing time by meeting the errBlockInterruptedByOutOfGas criteria.
			log.Info("commitWork local building finished, wait for the best bid", "tillSealingTime", common.PrettyDuration(tillSealingTime))
			mevWaitStart := time.Now()
			stopTimer.Reset(tillSealingTime)
			select {
			case <-stopTimer.C:
				// Record Waiting-2: MEV window waiting time
				if miningStats != nil {
					miningStats.WaitingMEVTime += time.Since(mevWaitStart)
				}
			case <-interruptCh:
				log.Debug("commitWork interruptCh closed, new block imported or resubmit triggered")
				return
			}
		}

		// [MEV Compare] Track MEV comparison time (excluding waiting)
		mevCompareStart := time.Now()

		// [MEV-L2] GetBestBid: retrieve best bid from memory (bidSimulator)
		getBestBidStart := time.Now()
		bestBid := w.bidFetcher.GetBestBid(bestWork.header.ParentHash)
		if miningStats != nil {
			miningStats.GetBestBidTime += time.Since(getBestBidStart)
		}

		// [MEV-L2] RewardCompare: compare local reward vs bid reward
		rewardCompareStart := time.Now()
		if bestBid != nil {
			bidExistGauge.Inc(1)
			bestBidGasUsedGauge.Update(int64(bestBid.bid.GasUsed) / 1_000_000)
			bestWorkGasUsedGauge.Update(int64(bestWork.header.GasUsed) / 1_000_000)

			log.Debug("BidSimulator: final compare", "block", bestWork.header.Number.Uint64(),
				"localBlockReward", bestReward.String(),
				"bidBlockReward", bestBid.packedBlockReward.String())
		}

		if bestBid != nil && bestReward.CmpBig(bestBid.packedBlockReward) < 0 {
			// localValidatorReward is the reward for the validator self by the local block.
			localValidatorReward := new(uint256.Int).Mul(bestReward, uint256.NewInt(*w.config.Mev.ValidatorCommission))
			localValidatorReward.Div(localValidatorReward, uint256.NewInt(10000))

			log.Debug("BidSimulator: final compare", "block", bestWork.header.Number.Uint64(),
				"localValidatorReward", localValidatorReward.String(),
				"bidValidatorReward", bestBid.packedValidatorReward.String())

			// blockReward(benefits delegators) and validatorReward(benefits the validator) are both optimal
			if localValidatorReward.CmpBig(bestBid.packedValidatorReward) < 0 {
				bidWinGauge.Inc(1)

				bestWork = bestBid.env

				log.Info("[BUILDER BLOCK]",
					"block", bestWork.header.Number.Uint64(),
					"builder", bestBid.bid.Builder,
					"blockReward", weiToEtherStringF6(bestBid.packedBlockReward),
					"validatorReward", weiToEtherStringF6(bestBid.packedValidatorReward),
					"bid", bestBid.bid.Hash().TerminalString(),
				)
			}
		}
		if miningStats != nil {
			miningStats.RewardCompareTime += time.Since(rewardCompareStart)
		}

		// Record MEV compare time (excluding waiting)
		if miningStats != nil {
			miningStats.MEVCompareTime += time.Since(mevCompareStart)
		}
	}

	w.commit(bestWork, w.fullTaskHook, start)

	// Swap out the old work with the new one, terminating any leftover
	// prefetcher processes in the mean time and starting a new one.
	if w.current != nil {
		w.current.discard()
	}
	w.current = bestWork
}

// inTurn return true if the current worker is in turn.
func (w *worker) inTurn() bool {
	validator, _ := w.engine.NextInTurnValidator(w.chain, w.chain.CurrentBlock())
	return validator != common.Address{} && validator == w.etherbase()
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
func (w *worker) commit(env *environment, interval func(), start time.Time) error {
	if w.isRunning() {
		if env.committed {
			log.Warn("Invalid work commit: already committed", "number", env.header.Number.Uint64())
			return nil
		}
		if interval != nil {
			interval()
		}
		fees := env.state.GetBalance(consensus.SystemAddress).ToBig()
		feesInEther := new(big.Float).Quo(new(big.Float).SetInt(fees), big.NewFloat(params.Ether))

		// Note: RootCalcTime is now measured directly in parlia.go FinalizeAndAssemble
		// (independent of metrics.EnabledExpensive, which defaults to false)

		// Withdrawals are set to nil here, because this is only called in PoW.
		finalizeStart := time.Now()
		body := types.Body{Transactions: env.txs}
		if env.header.EmptyWithdrawalsHash() {
			body.Withdrawals = make([]*types.Withdrawal, 0)
		}
		block, receipts, err := w.engine.FinalizeAndAssemble(w.chain, types.CopyHeader(env.header), env.state, &body, env.receipts, nil)
		env.committed = true
		if err != nil {
			return err
		}
		env.txs = body.Transactions
		env.receipts = receipts
		finalizeBlockTimer.UpdateSince(finalizeStart)

		// Read timing from StateDB (measured directly in parlia.go FinalizeAndAssemble)
		// This is independent of metrics.EnabledExpensive (which defaults to false)
		if env.miningStats != nil {
			totalFinalizeTime := time.Since(finalizeStart)

			// [Parlia-L2] Read all timing fields from StateDB
			env.miningStats.SystemTxExecTime = env.state.SystemTxExecTime
			env.miningStats.BlockAssemblyTime = env.state.BlockAssemblyTime
			env.miningStats.RootCalcTime = env.state.RootCalcTime

			// FinalizeSystemTxTime = total - RootCalc (since RootCalc runs in parallel with NewBlock)
			// Note: BlockAssemblyTime includes both IntermediateRoot and NewBlock in parallel
			if totalFinalizeTime > env.miningStats.RootCalcTime {
				env.miningStats.FinalizeSystemTxTime = totalFinalizeTime - env.miningStats.RootCalcTime
			}
		}

		// If Cancun enabled, sidecars can't be nil then.
		if w.chainConfig.IsCancun(env.header.Number, env.header.Time) && env.sidecars == nil {
			env.sidecars = make(types.BlobSidecars, 0)
		}
		block = block.WithSidecars(env.sidecars)

		select {
		case w.taskCh <- &task{receipts: receipts, state: env.state, block: block, createdAt: time.Now(), miningStartAt: start, miningStats: env.miningStats}:
			log.Info("Commit new sealing work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
				"txs", len(env.txs), "blobs", env.blobs, "gas", block.GasUsed(), "fees", feesInEther, "elapsed", common.PrettyDuration(time.Since(start)))

		case <-w.exitCh:
			log.Info("Worker has exited")
		}
	}
	return nil
}

// getSealingBlock generates the sealing block based on the given parameters.
// The generation result will be passed back via the given channel no matter
// the generation itself succeeds or not.
func (w *worker) getSealingBlock(params *generateParams) *newPayloadResult {
	req := &getWorkReq{
		params: params,
		result: make(chan *newPayloadResult, 1),
	}
	select {
	case w.getWorkCh <- req:
		return <-req.result
	case <-w.exitCh:
		return &newPayloadResult{err: errors.New("miner closed")}
	}
}

func (w *worker) tryWaitProposalDoneWhenStopping() {
	parlia, ok := w.engine.(*parlia.Parlia)
	// if the consensus is not parlia, just skip waiting
	if !ok {
		return
	}

	currentHeader := w.chain.CurrentBlock()
	currentBlock := currentHeader.Number.Uint64()
	startBlock, endBlock, err := parlia.NextProposalBlock(w.chain, currentHeader, w.coinbase)
	if err != nil {
		log.Warn("Failed to get next proposal block, skip waiting", "err", err)
		return
	}

	log.Info("Checking miner's next proposal block", "current", currentBlock,
		"proposalStart", startBlock, "proposalEnd", endBlock, "maxWait", *w.config.MaxWaitProposalInSecs)
	if endBlock <= currentBlock {
		log.Warn("next proposal end block has passed, ignore")
		return
	}
	blockInterval, err := parlia.BlockInterval(w.chain, currentHeader)
	if err != nil {
		log.Debug("failed to get BlockInterval when tryWaitProposalDoneWhenStopping")
	}
	if startBlock > currentBlock && ((startBlock-currentBlock)*blockInterval/1000) > *w.config.MaxWaitProposalInSecs {
		log.Warn("the next proposal start block is too far, just skip waiting")
		return
	}

	// wait one more block for safety
	waitSecs := (endBlock - currentBlock + 1) * blockInterval / 1000
	log.Info("The miner will propose in later, waiting for the proposal to be done",
		"currentBlock", currentBlock, "nextProposalStart", startBlock, "nextProposalEnd", endBlock, "waitTime", waitSecs)
	time.Sleep(time.Duration(waitSecs) * time.Second)
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
	case commitInterruptBetterBid:
		return errBlockInterruptedByBetterBid
	default:
		panic(fmt.Errorf("undefined signal %d", signal))
	}
}
