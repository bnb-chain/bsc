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

package core

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	fullProcessCheck       = 21 // On diff sync mode, will do full process every fullProcessCheck randomly
	recentTime             = 1024 * 3
	recentDiffLayerTimeout = 5
	farDiffLayerTimeout    = 2
	maxUnitSize            = 10

	parallelPrimarySlot = 0
	parallelShadowlot   = 1

	stage2CheckNumber = 30 // not fixed, use decrease?
	stage2RedoNumber  = 10
	stage2AheadNum    = 3 // ?
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// add for parallel executions
type ParallelStateProcessor struct {
	StateProcessor
	parallelNum int // leave a CPU to dispatcher
	queueSize   int // parallel slot's maximum number of pending Txs
	// pendingConfirmChan    chan *ParallelTxResult
	pendingConfirmResults map[int][]*ParallelTxResult // tx could be executed several times, with several result to check
	txResultChan          chan *ParallelTxResult      // to notify dispatcher that a tx is done
	// txReqAccountSorted   map[common.Address][]*ParallelTxRequest // fixme: *ParallelTxRequest => ParallelTxRequest?
	slotState             []*SlotState // idle, or pending messages
	mergedTxIndex         int          // the latest finalized tx index, fixme: use Atomic
	slotDBsToRelease      []*state.ParallelStateDB
	debugConflictRedoNum  int
	unconfirmedResults    *sync.Map // this is for stage2 confirm, since pendingConfirmResults can not be access in stage2 loop
	unconfirmedDBs        *sync.Map
	stopSlotChan          chan int      // fixme: use struct{}{}, to make sure all slot are idle
	stopConfirmChan       chan struct{} // fixme: use struct{}{}, to make sure all slot are idle
	confirmStage2Chan     chan int
	stopConfirmStage2Chan chan struct{}
	allTxReqs             []*ParallelTxRequest
	txReqExecuteRecord    map[int]int // for each the execute count of each Tx
	txReqExecuteCount     int
	inConfirmStage2       bool
	targetStage2Count     int // when executed txNUM reach it, enter stage2 RT confirm
	nextStage2TxIndex     int
}

func NewParallelStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine, parallelNum int, queueSize int) *ParallelStateProcessor {
	processor := &ParallelStateProcessor{
		StateProcessor: *NewStateProcessor(config, bc, engine),
		parallelNum:    parallelNum,
		queueSize:      queueSize,
	}
	processor.init()
	return processor
}

type LightStateProcessor struct {
	check int64
	StateProcessor
}

func NewLightStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *LightStateProcessor {
	randomGenerator := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	check := randomGenerator.Int63n(fullProcessCheck)
	return &LightStateProcessor{
		check:          check,
		StateProcessor: *NewStateProcessor(config, bc, engine),
	}
}

func (p *LightStateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (*state.StateDB, types.Receipts, []*types.Log, uint64, error) {
	allowLightProcess := true
	if posa, ok := p.engine.(consensus.PoSA); ok {
		allowLightProcess = posa.AllowLightProcess(p.bc, block.Header())
	}
	// random fallback to full process
	if allowLightProcess && block.NumberU64()%fullProcessCheck != uint64(p.check) && len(block.Transactions()) != 0 {
		var pid string
		if peer, ok := block.ReceivedFrom.(PeerIDer); ok {
			pid = peer.ID()
		}
		var diffLayer *types.DiffLayer
		var diffLayerTimeout = recentDiffLayerTimeout
		if time.Now().Unix()-int64(block.Time()) > recentTime {
			diffLayerTimeout = farDiffLayerTimeout
		}
		for tried := 0; tried < diffLayerTimeout; tried++ {
			// wait a bit for the diff layer
			diffLayer = p.bc.GetUnTrustedDiffLayer(block.Hash(), pid)
			if diffLayer != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if diffLayer != nil {
			if err := diffLayer.Receipts.DeriveFields(p.bc.chainConfig, block.Hash(), block.NumberU64(), block.Transactions()); err != nil {
				log.Error("Failed to derive block receipts fields", "hash", block.Hash(), "number", block.NumberU64(), "err", err)
				// fallback to full process
				return p.StateProcessor.Process(block, statedb, cfg)
			}

			receipts, logs, gasUsed, err := p.LightProcess(diffLayer, block, statedb)
			if err == nil {
				log.Info("do light process success at block", "num", block.NumberU64())
				return statedb, receipts, logs, gasUsed, nil
			}
			log.Error("do light process err at block", "num", block.NumberU64(), "err", err)
			p.bc.removeDiffLayers(diffLayer.DiffHash.Load().(common.Hash))
			// prepare new statedb
			statedb.StopPrefetcher()
			parent := p.bc.GetHeader(block.ParentHash(), block.NumberU64()-1)
			statedb, err = state.New(parent.Root, p.bc.stateCache, p.bc.snaps)
			if err != nil {
				return statedb, nil, nil, 0, err
			}
			statedb.SetExpectedStateRoot(block.Root())
			if p.bc.pipeCommit {
				statedb.EnablePipeCommit()
			}
			// Enable prefetching to pull in trie node paths while processing transactions
			statedb.StartPrefetcher("chain")
		}
	}
	// fallback to full process
	return p.StateProcessor.Process(block, statedb, cfg)
}

func (p *LightStateProcessor) LightProcess(diffLayer *types.DiffLayer, block *types.Block, statedb *state.StateDB) (types.Receipts, []*types.Log, uint64, error) {
	statedb.MarkLightProcessed()
	fullDiffCode := make(map[common.Hash][]byte, len(diffLayer.Codes))
	diffTries := make(map[common.Address]state.Trie)
	diffCode := make(map[common.Hash][]byte)

	snapDestructs, snapAccounts, snapStorage, err := statedb.DiffLayerToSnap(diffLayer)
	if err != nil {
		return nil, nil, 0, err
	}

	for _, c := range diffLayer.Codes {
		fullDiffCode[c.Hash] = c.Code
	}
	stateTrie, err := statedb.Trie()
	if err != nil {
		return nil, nil, 0, err
	}
	for des := range snapDestructs {
		stateTrie.TryDelete(des[:])
	}
	threads := gopool.Threads(len(snapAccounts))

	iteAccounts := make([]common.Address, 0, len(snapAccounts))
	for diffAccount := range snapAccounts {
		iteAccounts = append(iteAccounts, diffAccount)
	}

	errChan := make(chan error, threads)
	exitChan := make(chan struct{})
	var snapMux sync.RWMutex
	var stateMux, diffMux sync.Mutex
	for i := 0; i < threads; i++ {
		start := i * len(iteAccounts) / threads
		end := (i + 1) * len(iteAccounts) / threads
		if i+1 == threads {
			end = len(iteAccounts)
		}
		go func(start, end int) {
			for index := start; index < end; index++ {
				select {
				// fast fail
				case <-exitChan:
					return
				default:
				}
				diffAccount := iteAccounts[index]
				snapMux.RLock()
				blob := snapAccounts[diffAccount]
				snapMux.RUnlock()
				addrHash := crypto.Keccak256Hash(diffAccount[:])
				latestAccount, err := snapshot.FullAccount(blob)
				if err != nil {
					errChan <- err
					return
				}

				// fetch previous state
				var previousAccount types.StateAccount
				stateMux.Lock()
				enc, err := stateTrie.TryGet(diffAccount[:])
				stateMux.Unlock()
				if err != nil {
					errChan <- err
					return
				}
				if len(enc) != 0 {
					if err := rlp.DecodeBytes(enc, &previousAccount); err != nil {
						errChan <- err
						return
					}
				}
				if latestAccount.Balance == nil {
					latestAccount.Balance = new(big.Int)
				}
				if previousAccount.Balance == nil {
					previousAccount.Balance = new(big.Int)
				}
				if previousAccount.Root == (common.Hash{}) {
					previousAccount.Root = types.EmptyRootHash
				}
				if len(previousAccount.CodeHash) == 0 {
					previousAccount.CodeHash = types.EmptyCodeHash
				}

				// skip no change account
				if previousAccount.Nonce == latestAccount.Nonce &&
					bytes.Equal(previousAccount.CodeHash, latestAccount.CodeHash) &&
					previousAccount.Balance.Cmp(latestAccount.Balance) == 0 &&
					previousAccount.Root == common.BytesToHash(latestAccount.Root) {
					// It is normal to receive redundant message since the collected message is redundant.
					log.Debug("receive redundant account change in diff layer", "account", diffAccount, "num", block.NumberU64())
					snapMux.Lock()
					delete(snapAccounts, diffAccount)
					delete(snapStorage, diffAccount)
					snapMux.Unlock()
					continue
				}

				// update code
				codeHash := common.BytesToHash(latestAccount.CodeHash)
				if !bytes.Equal(latestAccount.CodeHash, previousAccount.CodeHash) &&
					!bytes.Equal(latestAccount.CodeHash, types.EmptyCodeHash) {
					if code, exist := fullDiffCode[codeHash]; exist {
						if crypto.Keccak256Hash(code) != codeHash {
							errChan <- fmt.Errorf("code and code hash mismatch, account %s", diffAccount.String())
							return
						}
						diffMux.Lock()
						diffCode[codeHash] = code
						diffMux.Unlock()
					} else {
						rawCode := rawdb.ReadCode(p.bc.db, codeHash)
						if len(rawCode) == 0 {
							errChan <- fmt.Errorf("missing code, account %s", diffAccount.String())
							return
						}
					}
				}

				//update storage
				latestRoot := common.BytesToHash(latestAccount.Root)
				if latestRoot != previousAccount.Root {
					accountTrie, err := statedb.Database().OpenStorageTrie(addrHash, previousAccount.Root)
					if err != nil {
						errChan <- err
						return
					}
					snapMux.RLock()
					storageChange, exist := snapStorage[diffAccount]
					snapMux.RUnlock()

					if !exist {
						errChan <- errors.New("missing storage change in difflayer")
						return
					}
					for k, v := range storageChange {
						if len(v) != 0 {
							accountTrie.TryUpdate([]byte(k), v)
						} else {
							accountTrie.TryDelete([]byte(k))
						}
					}

					// check storage root
					accountRootHash := accountTrie.Hash()
					if latestRoot != accountRootHash {
						errChan <- errors.New("account storage root mismatch")
						return
					}
					diffMux.Lock()
					diffTries[diffAccount] = accountTrie
					diffMux.Unlock()
				} else {
					snapMux.Lock()
					delete(snapStorage, diffAccount)
					snapMux.Unlock()
				}

				// can't trust the blob, need encode by our-self.
				latestStateAccount := types.StateAccount{
					Nonce:    latestAccount.Nonce,
					Balance:  latestAccount.Balance,
					Root:     common.BytesToHash(latestAccount.Root),
					CodeHash: latestAccount.CodeHash,
				}
				bz, err := rlp.EncodeToBytes(&latestStateAccount)
				if err != nil {
					errChan <- err
					return
				}
				stateMux.Lock()
				err = stateTrie.TryUpdate(diffAccount[:], bz)
				stateMux.Unlock()
				if err != nil {
					errChan <- err
					return
				}
			}
			errChan <- nil
		}(start, end)
	}

	for i := 0; i < threads; i++ {
		err := <-errChan
		if err != nil {
			close(exitChan)
			return nil, nil, 0, err
		}
	}

	var allLogs []*types.Log
	var gasUsed uint64
	for _, receipt := range diffLayer.Receipts {
		allLogs = append(allLogs, receipt.Logs...)
		gasUsed += receipt.GasUsed
	}

	// Do validate in advance so that we can fall back to full process
	if err := p.bc.validator.ValidateState(block, statedb, diffLayer.Receipts, gasUsed); err != nil {
		log.Error("validate state failed during diff sync", "error", err)
		return nil, nil, 0, err
	}

	// remove redundant storage change
	for account := range snapStorage {
		if _, exist := snapAccounts[account]; !exist {
			log.Warn("receive redundant storage change in diff layer")
			delete(snapStorage, account)
		}
	}

	// remove redundant code
	if len(fullDiffCode) != len(diffLayer.Codes) {
		diffLayer.Codes = make([]types.DiffCode, 0, len(diffCode))
		for hash, code := range diffCode {
			diffLayer.Codes = append(diffLayer.Codes, types.DiffCode{
				Hash: hash,
				Code: code,
			})
		}
	}

	statedb.SetSnapData(snapDestructs, snapAccounts, snapStorage)
	if len(snapAccounts) != len(diffLayer.Accounts) || len(snapStorage) != len(diffLayer.Storages) {
		diffLayer.Destructs, diffLayer.Accounts, diffLayer.Storages = statedb.SnapToDiffLayer()
	}
	statedb.SetDiff(diffLayer, diffTries, diffCode)

	return diffLayer.Receipts, allLogs, gasUsed, nil
}

type MergedTxInfo struct {
	slotDB              *state.StateDB // used for SlotDb reuse only, otherwise, it can be discarded
	StateObjectSuicided map[common.Address]struct{}
	StateChangeSet      map[common.Address]state.StateKeys
	BalanceChangeSet    map[common.Address]struct{}
	CodeChangeSet       map[common.Address]struct{}
	AddrStateChangeSet  map[common.Address]struct{}
	txIndex             int
}

type SlotState struct {
	pendingTxReqList  []*ParallelTxRequest
	primaryWakeUpChan chan *ParallelTxRequest
	shadowWakeUpChan  chan *ParallelTxRequest
	primaryStopChan   chan struct{}
	shadowStopChan    chan struct{}

	slotDBChan    chan *state.ParallelStateDB // to update SlotDB
	activatedType int32                       // 0: primary slot, 1: shadow slot
}

type ParallelTxResult struct {
	executedIndex int  // the TxReq can be executed several time, increase index for each execution
	updateSlotDB  bool // for redo and pending tx quest, slot needs new slotDB,
	// keepSystem    bool  // for redo, should keep system address's balance
	slotIndex int   // slot index
	err       error // to describe error message?
	txReq     *ParallelTxRequest
	receipt   *types.Receipt
	slotDB    *state.ParallelStateDB // if updated, it is not equal to txReq.slotDB
	gpSlot    *GasPool
	evm       *vm.EVM
	result    *ExecutionResult
}

type ParallelTxRequest struct {
	txIndex int
	// baseTxIndex     int
	baseStateDB     *state.StateDB
	staticSlotIndex int // static dispatched id
	tx              *types.Transaction
	// slotDB         *state.ParallelStateDB
	gasLimit       uint64
	msg            types.Message
	block          *types.Block
	vmConfig       vm.Config
	bloomProcessor *AsyncReceiptBloomGenerator
	usedGas        *uint64
	curTxChan      chan int
	systemAddrRedo bool
	// runnable 0: runnable
	// runnable 1: it has been executed, but results has not been confirmed
	// runnable 2: all confirmed,
	runnable    int32
	executedNum int
}

// to create and start the execution slot goroutines
func (p *ParallelStateProcessor) init() {
	log.Info("Parallel execution mode is enabled", "Parallel Num", p.parallelNum,
		"CPUNum", runtime.NumCPU(),
		"QueueSize", p.queueSize)
	// In extreme case, parallelNum*2 are requiring updateStateDB,
	// confirmLoop is deliverring the valid result or asking for AddrPrefetch.
	p.txResultChan = make(chan *ParallelTxResult, 200)
	p.stopSlotChan = make(chan int, 1)
	p.stopConfirmChan = make(chan struct{}, 1)
	p.stopConfirmStage2Chan = make(chan struct{}, 1)

	p.slotState = make([]*SlotState, p.parallelNum)
	for i := 0; i < p.parallelNum; i++ {
		p.slotState[i] = &SlotState{
			slotDBChan:        make(chan *state.ParallelStateDB, 1),
			primaryWakeUpChan: make(chan *ParallelTxRequest, 1),
			shadowWakeUpChan:  make(chan *ParallelTxRequest, 1),
			primaryStopChan:   make(chan struct{}, 1),
			shadowStopChan:    make(chan struct{}, 1),
		}
		// start the primary slot's goroutine
		go func(slotIndex int) {
			p.runSlotLoop(slotIndex, parallelPrimarySlot) // this loop will be permanent live
		}(i)

		// start the shadow slot.
		// It is back up of the primary slot to make sure transaction can be redo ASAP,
		// since the primary slot could be busy at executing another transaction
		go func(slotIndex int) {
			p.runSlotLoop(slotIndex, 1) // this loop will be permanent live
		}(i)

	}

	// p.pendingConfirmChan = make(chan *ParallelTxResult, 400)
	//go func() {
	//	p.runConfirmLoop() // this loop will be permanent live
	//}()
	p.confirmStage2Chan = make(chan int, 10)
	go func() {
		p.runConfirmStage2Loop() // this loop will be permanent live
	}()
}

// clear slot state for each block.
func (p *ParallelStateProcessor) resetState(txNum int, statedb *state.StateDB) {
	if txNum == 0 {
		return
	}
	p.mergedTxIndex = -1
	p.debugConflictRedoNum = 0
	p.inConfirmStage2 = false
	// p.txReqAccountSorted = make(map[common.Address][]*ParallelTxRequest) // fixme: to be reused?

	statedb.PrepareForParallel()
	p.allTxReqs = make([]*ParallelTxRequest, 0)
	p.slotDBsToRelease = make([]*state.ParallelStateDB, 0, txNum)

	stateDBsToRelease := p.slotDBsToRelease
	go func() {
		for _, slotDB := range stateDBsToRelease {
			slotDB.PutSyncPool()
		}
	}()
	for _, slot := range p.slotState {
		slot.pendingTxReqList = make([]*ParallelTxRequest, 0)
		slot.activatedType = 0
	}
	p.unconfirmedResults = new(sync.Map) // make(map[int]*state.ParallelStateDB)
	p.unconfirmedDBs = new(sync.Map)     // make(map[int]*state.ParallelStateDB)
	p.pendingConfirmResults = make(map[int][]*ParallelTxResult, 200)
	p.txReqExecuteRecord = make(map[int]int, 200)
	p.txReqExecuteCount = 0
	p.nextStage2TxIndex = 0
}

// Benefits of StaticDispatch:
//  ** try best to make Txs with same From() in same slot
//  ** reduce IPC cost by dispatch in Unit
//  ** make sure same From in same slot
//  ** try to make it balanced, queue to the most hungry slot for new Address
func (p *ParallelStateProcessor) doStaticDispatch(mainStatedb *state.StateDB, txReqs []*ParallelTxRequest) {
	fromSlotMap := make(map[common.Address]int, 100)
	toSlotMap := make(map[common.Address]int, 100)
	for _, txReq := range txReqs {
		var slotIndex int = -1
		if i, ok := fromSlotMap[txReq.msg.From()]; ok {
			// first: same From are all in same slot
			slotIndex = i
		} else if txReq.msg.To() != nil {
			// To Address, with txIndex sorted, could be in different slot.
			// fixme: Create will move to hungry slot
			if i, ok := toSlotMap[*txReq.msg.To()]; ok {
				slotIndex = i
			}
		}

		// not found, dispatch to most hungry slot
		if slotIndex == -1 {
			var workload int = len(p.slotState[0].pendingTxReqList)
			slotIndex = 0
			for i, slot := range p.slotState { // can start from index 1
				if len(slot.pendingTxReqList) < workload {
					slotIndex = i
					workload = len(slot.pendingTxReqList)
				}
			}
		}
		// update
		fromSlotMap[txReq.msg.From()] = slotIndex
		if txReq.msg.To() != nil {
			toSlotMap[*txReq.msg.To()] = slotIndex
		}

		slot := p.slotState[slotIndex]
		txReq.staticSlotIndex = slotIndex // txreq is better to be executed in this slot
		slot.pendingTxReqList = append(slot.pendingTxReqList, txReq)
	}
}

// do conflict detect
func (p *ParallelStateProcessor) hasConflict(txResult *ParallelTxResult, isStage2 bool) bool {
	slotDB := txResult.slotDB
	if txResult.err != nil {
		return true
	} else if slotDB.SystemAddressRedo() {
		if !isStage2 {
			// for system addr redo, it has to wait until it's turn to keep the system address balance
			txResult.txReq.systemAddrRedo = true
		}
		return true
	} else if slotDB.NeedsRedo() {
		// if this is any reason that indicates this transaction needs to redo, skip the conflict check
		return true
	} else {
		// to check if what the slot db read is correct.
		if !slotDB.IsParallelReadsValid(isStage2, p.mergedTxIndex) {
			return true
		}
	}
	return false
}

func (p *ParallelStateProcessor) switchSlot(slotIndex int) {
	slot := p.slotState[slotIndex]
	if atomic.CompareAndSwapInt32(&slot.activatedType, 0, 1) {
		// switch from normal to shadow slot
		if len(slot.shadowWakeUpChan) == 0 {
			slot.shadowWakeUpChan <- nil // only notify when target once
		}
	} else if atomic.CompareAndSwapInt32(&slot.activatedType, 1, 0) {
		// switch from shadow to normal slot
		if len(slot.primaryWakeUpChan) == 0 {
			slot.primaryWakeUpChan <- nil // only notify when target once
		}
	}
}

func (p *ParallelStateProcessor) executeInSlot(slotIndex int, txReq *ParallelTxRequest) *ParallelTxResult {
	txReq.executedNum++ // fixme: atomic?
	slotDB := state.NewSlotDB(txReq.baseStateDB, consensus.SystemAddress, txReq.txIndex,
		p.mergedTxIndex, txReq.systemAddrRedo, p.unconfirmedDBs)

	slotDB.Prepare(txReq.tx.Hash(), txReq.txIndex)
	blockContext := NewEVMBlockContext(txReq.block.Header(), p.bc, nil) // can share blockContext within a block for efficiency
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, slotDB, p.config, txReq.vmConfig)
	// gasLimit not accurate, but it is ok for block import.
	// each slot would use its own gas pool, and will do gaslimit check later
	gpSlot := new(GasPool).AddGas(txReq.gasLimit) // block.GasLimit()

	evm, result, err := applyTransactionStageExecution(txReq.msg, gpSlot, slotDB, vmenv)
	txResult := ParallelTxResult{
		executedIndex: txReq.executedNum,
		updateSlotDB:  false,
		slotIndex:     slotIndex,
		txReq:         txReq,
		receipt:       nil, // receipt is generated in finalize stage
		slotDB:        slotDB,
		err:           err,
		gpSlot:        gpSlot,
		evm:           evm,
		result:        result,
	}
	if err == nil {
		if result.Failed() {
			// if Tx is reverted, all its state change will be discarded
			slotDB.RevertSlotDB(txReq.msg.From())
		}
		slotDB.Finalise(true) // Finalise could write s.parallel.addrStateChangesInSlot[addr], keep Read and Write in same routine to avoid crash
		p.unconfirmedDBs.Store(txReq.txIndex, slotDB)
	} else {
		// the transaction failed at check(nonce or blanace), actually it has not been executed yet.
		atomic.CompareAndSwapInt32(&txReq.runnable, 0, 1)
		// the error could be caused by unconfirmed balance reference,
		// the balance could insufficient to pay its gas limit, which cause it preCheck.buyGas() failed
		// redo could solve it.
		log.Debug("In slot execution error", "error", err,
			"slotIndex", slotIndex, "txIndex", txReq.txIndex)
	}
	p.unconfirmedResults.Store(txReq.txIndex, &txResult)
	return &txResult
}

// to confirm a serial TxResults with same txIndex
func (p *ParallelStateProcessor) toConfirmTxIndex(targetTxIndex int, isStage2 bool) *ParallelTxResult {
	if isStage2 {
		if targetTxIndex <= p.mergedTxIndex+1 {
			// this is the one that can been merged,
			// others are for likely conflict check, since it is not their tuen.
			// log.Warn("to confirm in stage 2, invalid txIndex",
			//	"targetTxIndex", targetTxIndex, "p.mergedTxIndex", p.mergedTxIndex)
			return nil
		}
	}

	for {
		// handle a targetTxIndex in a loop
		// targetTxIndex = p.mergedTxIndex + 1
		// select a unconfirmedResult to check
		var targetResult *ParallelTxResult
		if isStage2 {
			result, ok := p.unconfirmedResults.Load(targetTxIndex)
			if !ok {
				return nil
			}
			targetResult = result.(*ParallelTxResult)
			// in stage 2, don't schedule a new redo if the TxReq is:
			//  a.runnable: it will be redo
			//  b.running: the new result will be more reliable, we skip check right now
			if atomic.CompareAndSwapInt32(&targetResult.txReq.runnable, 1, 1) {
				return nil
			}
			if targetResult.executedIndex < targetResult.txReq.executedNum {
				return nil
			}
		} else {
			results := p.pendingConfirmResults[targetTxIndex]
			resultsLen := len(results)
			if resultsLen == 0 { // no pending result can be verified, break and wait for incoming results
				return nil
			}
			targetResult = results[len(results)-1]                                                         // last is the most fresh, stack based priority
			p.pendingConfirmResults[targetTxIndex] = p.pendingConfirmResults[targetTxIndex][:resultsLen-1] // remove from the queue
		}

		valid := p.toConfirmTxIndexResult(targetResult, isStage2)
		if !valid {
			staticSlotIndex := targetResult.txReq.staticSlotIndex // it is better to run the TxReq in its static dispatch slot
			if isStage2 {
				atomic.CompareAndSwapInt32(&targetResult.txReq.runnable, 0, 1) // needs redo
				p.debugConflictRedoNum++
				// interrupt the slot's current routine, and switch to the other routine
				p.switchSlot(staticSlotIndex)
				return nil
			}
			if len(p.pendingConfirmResults[targetTxIndex]) == 0 { // this is the last result to check and it is not valid
				atomic.CompareAndSwapInt32(&targetResult.txReq.runnable, 0, 1) // needs redo
				p.debugConflictRedoNum++
				// interrupt its current routine, and switch to the other routine
				p.switchSlot(staticSlotIndex)
				return nil
			}
			continue
		}
		if isStage2 {
			// likely valid, but not sure, can not deliver
			// fixme: need to handle txResult repeatedly check?
			return nil
		}
		return targetResult
	}
}

// to confirm one txResult, return true if the result is valid
// if it is in Stage 2 it is a likely result, not 100% sure
func (p *ParallelStateProcessor) toConfirmTxIndexResult(txResult *ParallelTxResult, isStage2 bool) bool {
	txReq := txResult.txReq
	if p.hasConflict(txResult, isStage2) {
		return false
	}
	if isStage2 { // not its turn
		return true // likely valid, not sure, not finalized right now.
	}

	// goroutine unsafe operation will be handled from here for safety
	gasConsumed := txReq.gasLimit - txResult.gpSlot.Gas()
	if gasConsumed != txResult.result.UsedGas {
		log.Error("gasConsumed != result.UsedGas mismatch",
			"gasConsumed", gasConsumed, "result.UsedGas", txResult.result.UsedGas)
	}

	// ok, time to do finalize, stage2 should not be parallel
	header := txReq.block.Header()
	txResult.receipt, txResult.err = applyTransactionStageFinalization(txResult.evm, txResult.result,
		txReq.msg, p.config, txResult.slotDB, header,
		txReq.tx, txReq.usedGas, txReq.bloomProcessor)
	txResult.updateSlotDB = false
	return true
}

func (p *ParallelStateProcessor) runSlotLoop(slotIndex int, slotType int32) {
	curSlot := p.slotState[slotIndex]
	var wakeupChan chan *ParallelTxRequest
	var stopChan chan struct{}

	if slotType == parallelPrimarySlot {
		wakeupChan = curSlot.primaryWakeUpChan
		stopChan = curSlot.primaryStopChan
	} else {
		wakeupChan = curSlot.shadowWakeUpChan
		stopChan = curSlot.shadowStopChan
	}
	for {
		select {
		case <-stopChan:
			p.stopSlotChan <- slotIndex
			continue
		case <-wakeupChan:
		}

		interrupted := false
		for _, txReq := range curSlot.pendingTxReqList {
			if txReq.txIndex <= p.mergedTxIndex {
				continue
			}
			if curSlot.activatedType != slotType { // fixme: atomic compare?
				interrupted = true
				break
			}
			if !atomic.CompareAndSwapInt32(&txReq.runnable, 1, 0) {
				// not swapped: txReq.runnable == 0
				continue
			}
			result := p.executeInSlot(slotIndex, txReq)
			if result == nil { // fixme: code improve, nil means block processed, to be stopped
				break
			}
			p.txResultChan <- result
		}
		// switched to the other slot.
		if interrupted {
			continue
		}

		// txReq in this Slot have all been executed, try steal one from other slot.
		// as long as the TxReq is runable, we steal it, mark it as stolen
		// steal one by one
		for _, stealTxReq := range p.allTxReqs {
			if stealTxReq.txIndex <= p.mergedTxIndex {
				continue
			}
			if curSlot.activatedType != slotType {
				interrupted = true
				break
			}

			if !atomic.CompareAndSwapInt32(&stealTxReq.runnable, 1, 0) {
				// not swapped: txReq.runnable == 0
				continue
			}
			result := p.executeInSlot(slotIndex, stealTxReq)
			if result == nil { // fixme: code improve, nil means block processed, to be stopped
				break
			}
			p.txResultChan <- result
		}
	}
}

func (p *ParallelStateProcessor) runConfirmStage2Loop() {
	for {
		// var mergedTxIndex int
		select {
		case <-p.stopConfirmStage2Chan:
			for len(p.confirmStage2Chan) > 0 {
				<-p.confirmStage2Chan
			}
			p.stopSlotChan <- -1
			continue
		case <-p.confirmStage2Chan:
			for len(p.confirmStage2Chan) > 0 {
				<-p.confirmStage2Chan // drain the chan to get the latest merged txIndex
			}
		}
		// stage 2,if all tx have been executed at least once, and its result has been recevied.
		// in Stage 2, we will run check when merge is advanced.
		// more aggressive tx result confirm, even for these Txs not in turn
		// now we will be more aggressive:
		//   do conflcit check , as long as tx result is generated,
		//   if lucky, it is the Tx's turn, we will do conflict check with WBNB makeup
		//   otherwise, do conflict check without WBNB makeup, but we will ignor WBNB's balance conflict.
		// throw these likely conflicted tx back to re-execute
		startTxIndex := p.mergedTxIndex + 2 // stage 2's will start from the next target merge index
		endTxIndex := startTxIndex + stage2CheckNumber
		txSize := len(p.allTxReqs)
		if endTxIndex > (txSize - 1) {
			endTxIndex = txSize - 1
		}
		log.Debug("runConfirmStage2Loop", "startTxIndex", startTxIndex, "endTxIndex", endTxIndex)
		// conflictNumMark := p.debugConflictRedoNum
		for txIndex := startTxIndex; txIndex < endTxIndex; txIndex++ {
			p.toConfirmTxIndex(txIndex, true)
			// newConflictNum := p.debugConflictRedoNum - conflictNumMark
			// to avoid schedule too many redo each time.
			// if newConflictNum >= stage2RedoNumber {
			// break
			//}
		}
		// make sure all slots are wake up
		for i := 0; i < p.parallelNum; i++ {
			p.switchSlot(i)
		}
	}

}

func (p *ParallelStateProcessor) handleTxResults() *ParallelTxResult {
	log.Debug("handleTxResults", "p.mergedTxIndex", p.mergedTxIndex)
	confirmedResult := p.toConfirmTxIndex(p.mergedTxIndex+1, false)
	if confirmedResult == nil {
		return nil
	}
	// schedule stage 2 when new Tx has been merged, schedule once and ASAP
	// stage 2,if all tx have been executed at least once, and its result has been recevied.
	// in Stage 2, we will run check when main DB is advanced, i.e., new Tx result has been merged.
	if p.inConfirmStage2 && p.mergedTxIndex >= p.nextStage2TxIndex {
		p.nextStage2TxIndex = p.mergedTxIndex + stage2CheckNumber // fixme: more accurate one
		p.confirmStage2Chan <- p.mergedTxIndex
	}
	return confirmedResult
}

// wait until the next Tx is executed and its result is merged to the main stateDB
func (p *ParallelStateProcessor) confirmTxResults(statedb *state.StateDB, gp *GasPool) *ParallelTxResult {
	result := p.handleTxResults()
	if result == nil {
		return nil
	}
	// ok, the tx result is valid and can be merged

	if err := gp.SubGas(result.receipt.GasUsed); err != nil {
		log.Error("gas limit reached", "block", result.txReq.block.Number(),
			"txIndex", result.txReq.txIndex, "GasUsed", result.receipt.GasUsed, "gp.Gas", gp.Gas())
	}
	resultTxIndex := result.txReq.txIndex
	statedb.MergeSlotDB(result.slotDB, result.receipt, resultTxIndex)

	if resultTxIndex != p.mergedTxIndex+1 {
		log.Error("ProcessParallel tx result out of order", "resultTxIndex", resultTxIndex,
			"p.mergedTxIndex", p.mergedTxIndex)
	}
	p.mergedTxIndex = resultTxIndex
	// log.Debug("confirmTxResults result is merged", "result.slotIndex", result.slotIndex,
	//	"TxIndex", result.txReq.txIndex, "p.mergedTxIndex", p.mergedTxIndex)
	return result
}

func (p *ParallelStateProcessor) doCleanUp() {
	// 1.clean up all slot: primary and shadow, to make sure they are stopped
	for _, slot := range p.slotState {
		slot.primaryStopChan <- struct{}{}
		slot.shadowStopChan <- struct{}{}
		<-p.stopSlotChan
		<-p.stopSlotChan
	}
	// 2.discard delayed txResults if any
	for {
		if len(p.txResultChan) > 0 { // drop prefetch addr?
			<-p.txResultChan
			continue
		}
		break
	}
	// 3.make sure the confirm routines are stopped
	// p.stopConfirmChan <- struct{}{}
	// <-p.stopSlotChan
	log.Debug("ProcessParallel to stop confirm routine")
	p.stopConfirmStage2Chan <- struct{}{}
	<-p.stopSlotChan
	log.Debug("ProcessParallel stopped confirm routine")
}

// Implement BEP-130: Parallel Transaction Execution.
func (p *ParallelStateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (*state.StateDB, types.Receipts, []*types.Log, uint64, error) {
	var (
		usedGas = new(uint64)
		header  = block.Header()
		gp      = new(GasPool).AddGas(block.GasLimit())
	)
	var receipts = make([]*types.Receipt, 0)
	txNum := len(block.Transactions())
	p.resetState(txNum, statedb)
	if txNum > 0 {
		log.Info("ProcessParallel", "block", header.Number, "txNum", txNum)
	}

	// Iterate over and process the individual transactions
	posa, isPoSA := p.engine.(consensus.PoSA)
	commonTxs := make([]*types.Transaction, 0, txNum)
	// usually do have two tx, one for validator set contract, another for system reward contract.
	systemTxs := make([]*types.Transaction, 0, 2)

	signer, _, bloomProcessor := p.preExecute(block, statedb, cfg, true)
	// var txReqs []*ParallelTxRequest
	for i, tx := range block.Transactions() {
		if isPoSA {
			if isSystemTx, err := posa.IsSystemTransaction(tx, block.Header()); err != nil {
				bloomProcessor.Close()
				return statedb, nil, nil, 0, err
			} else if isSystemTx {
				systemTxs = append(systemTxs, tx)
				continue
			}
		}

		// can be moved it into slot for efficiency, but signer is not concurrent safe
		// Parallel Execution 1.0&2.0 is for full sync mode, Nonce PreCheck is not necessary
		// And since we will do out-of-order execution, the Nonce PreCheck could fail.
		// We will disable it and leave it to Parallel 3.0 which is for validator mode
		msg, err := tx.AsMessageNoNonceCheck(signer, header.BaseFee)
		if err != nil {
			bloomProcessor.Close()
			return statedb, nil, nil, 0, err
		}

		// parallel start, wrap an exec message, which will be dispatched to a slot
		txReq := &ParallelTxRequest{
			txIndex:         i,
			baseStateDB:     statedb,
			staticSlotIndex: -1,
			tx:              tx,
			gasLimit:        block.GasLimit(), // gp.Gas().
			msg:             msg,
			block:           block,
			vmConfig:        cfg,
			bloomProcessor:  bloomProcessor,
			usedGas:         usedGas,
			curTxChan:       make(chan int, 1),
			systemAddrRedo:  false, // set to true, when systemAddr access is detected.
			runnable:        1,     // 0: not runnable, 1: runnable
			executedNum:     0,
		}
		p.allTxReqs = append(p.allTxReqs, txReq)
	}
	// set up stage2 enter criteria
	p.targetStage2Count = len(p.allTxReqs)
	if p.targetStage2Count > 50 {
		// usually, the the last Tx could be the bottleneck it could be very slow,
		// so it is better for us to enter stage 2 a bit earlier
		p.targetStage2Count = p.targetStage2Count - stage2AheadNum
	}

	p.doStaticDispatch(statedb, p.allTxReqs) // todo: put txReqs in unit?
	// after static dispatch, we notify the slot to work.
	for _, slot := range p.slotState {
		slot.primaryWakeUpChan <- nil
	}
	// wait until all Txs have processed.
	for {
		if len(commonTxs)+len(systemTxs) == txNum {
			// put it ahead of chan receive to avoid waiting for empty block
			break
		}

		unconfirmedResult := <-p.txResultChan
		unconfirmedTxIndex := unconfirmedResult.txReq.txIndex
		if unconfirmedTxIndex <= p.mergedTxIndex {
			log.Warn("drop merged txReq", "unconfirmedTxIndex", unconfirmedTxIndex, "p.mergedTxIndex", p.mergedTxIndex)
			continue
		}
		p.pendingConfirmResults[unconfirmedTxIndex] = append(p.pendingConfirmResults[unconfirmedTxIndex], unconfirmedResult)

		// schedule prefetch once only when unconfirmedResult is valid
		if unconfirmedResult.err == nil {
			if _, ok := p.txReqExecuteRecord[unconfirmedTxIndex]; !ok {
				p.txReqExecuteRecord[unconfirmedTxIndex] = 0
				p.txReqExecuteCount++
				statedb.AddrPrefetch(unconfirmedResult.slotDB) // todo: prefetch when it is not merged
				// enter stage2, RT confirm
				if !p.inConfirmStage2 && p.txReqExecuteCount == p.targetStage2Count {
					p.inConfirmStage2 = true
				}

			}
			p.txReqExecuteRecord[unconfirmedTxIndex]++
		}

		for {
			result := p.confirmTxResults(statedb, gp)
			if result == nil {
				break
			}
			// update tx result
			if result.err != nil {
				log.Error("ProcessParallel a failed tx", "resultSlotIndex", result.slotIndex,
					"resultTxIndex", result.txReq.txIndex, "result.err", result.err)
				bloomProcessor.Close()
				return statedb, nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", result.txReq.txIndex, result.txReq.tx.Hash().Hex(), result.err)
			}
			commonTxs = append(commonTxs, result.txReq.tx)
			receipts = append(receipts, result.receipt)
		}
	}
	// to do clean up when the block is processed
	p.doCleanUp()

	// len(commonTxs) could be 0, such as: https://bscscan.com/block/14580486
	if len(commonTxs) > 0 {
		log.Info("ProcessParallel tx all done", "block", header.Number, "usedGas", *usedGas,
			"txNum", txNum,
			"len(commonTxs)", len(commonTxs),
			"conflictNum", p.debugConflictRedoNum,
			"redoRate(%)", 100*(p.debugConflictRedoNum)/len(commonTxs))
	}
	allLogs, err := p.postExecute(block, statedb, &commonTxs, &receipts, &systemTxs, usedGas, bloomProcessor)
	return statedb, receipts, allLogs, *usedGas, err
}

// Before transactions are executed, do shared preparation for Process() & ProcessParallel()
func (p *StateProcessor) preExecute(block *types.Block, statedb *state.StateDB, cfg vm.Config, parallel bool) (types.Signer, *vm.EVM, *AsyncReceiptBloomGenerator) {
	signer := types.MakeSigner(p.bc.chainConfig, block.Number())
	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	// Handle upgrade build-in system contract code
	systemcontracts.UpgradeBuildInSystemContract(p.config, block.Number(), statedb)

	// with parallel mode, vmenv will be created inside of slot
	var vmenv *vm.EVM
	if !parallel {
		blockContext := NewEVMBlockContext(block.Header(), p.bc, nil)
		vmenv = vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
	}

	// initialise bloom processors
	bloomProcessor := NewAsyncReceiptBloomGenerator(len(block.Transactions()))
	statedb.MarkFullProcessed()

	return signer, vmenv, bloomProcessor
}

func (p *StateProcessor) postExecute(block *types.Block, statedb *state.StateDB, commonTxs *[]*types.Transaction,
	receipts *[]*types.Receipt, systemTxs *[]*types.Transaction, usedGas *uint64, bloomProcessor *AsyncReceiptBloomGenerator) ([]*types.Log, error) {
	allLogs := make([]*types.Log, 0, len(*receipts))

	bloomProcessor.Close()

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	err := p.engine.Finalize(p.bc, block.Header(), statedb, commonTxs, block.Uncles(), receipts, systemTxs, usedGas)
	if err != nil {
		return allLogs, err
	}
	for _, receipt := range *receipts {
		allLogs = append(allLogs, receipt.Logs...)
	}
	return allLogs, nil
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (*state.StateDB, types.Receipts, []*types.Log, uint64, error) {
	var (
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = new(GasPool).AddGas(block.GasLimit())
	)

	var receipts = make([]*types.Receipt, 0)
	txNum := len(block.Transactions())
	if txNum > 0 {
		log.Info("Process", "block", header.Number, "txNum", txNum)
	}
	// Iterate over and process the individual transactions
	posa, isPoSA := p.engine.(consensus.PoSA)
	commonTxs := make([]*types.Transaction, 0, txNum)

	// usually do have two tx, one for validator set contract, another for system reward contract.
	systemTxs := make([]*types.Transaction, 0, 2)

	signer, vmenv, bloomProcessor := p.preExecute(block, statedb, cfg, false)
	for i, tx := range block.Transactions() {
		if isPoSA {
			if isSystemTx, err := posa.IsSystemTransaction(tx, block.Header()); err != nil {
				bloomProcessor.Close()
				return statedb, nil, nil, 0, err
			} else if isSystemTx {
				systemTxs = append(systemTxs, tx)
				continue
			}
		}

		msg, err := tx.AsMessage(signer, header.BaseFee)
		if err != nil {
			bloomProcessor.Close()
			return statedb, nil, nil, 0, err
		}
		statedb.Prepare(tx.Hash(), i)
		receipt, err := applyTransaction(msg, p.config, p.bc, nil, gp, statedb, blockNumber, blockHash, tx, usedGas, vmenv, bloomProcessor)
		if err != nil {
			bloomProcessor.Close()
			return statedb, nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		commonTxs = append(commonTxs, tx)
		receipts = append(receipts, receipt)
	}

	allLogs, err := p.postExecute(block, statedb, &commonTxs, &receipts, &systemTxs, usedGas, bloomProcessor)
	return statedb, receipts, allLogs, *usedGas, err
}

func applyTransaction(msg types.Message, config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM, receiptProcessors ...ReceiptProcessor) (*types.Receipt, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}

	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(blockNumber) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	// If the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockHash)
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	for _, receiptProcessor := range receiptProcessors {
		receiptProcessor.Apply(receipt)
	}
	return receipt, err
}

func applyTransactionStageExecution(msg types.Message, gp *GasPool, statedb *state.ParallelStateDB, evm *vm.EVM) (*vm.EVM, *ExecutionResult, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, nil, err
	}

	return evm, result, err
}

func applyTransactionStageFinalization(evm *vm.EVM, result *ExecutionResult, msg types.Message, config *params.ChainConfig, statedb *state.ParallelStateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, receiptProcessors ...ReceiptProcessor) (*types.Receipt, error) {
	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(header.Number) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(header.Number)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	// If the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), header.Hash())
	receipt.BlockHash = header.Hash()
	receipt.BlockNumber = header.Number
	receipt.TransactionIndex = uint(statedb.TxIndex())
	for _, receiptProcessor := range receiptProcessors {
		receiptProcessor.Apply(receipt)
	}
	return receipt, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config, receiptProcessors ...ReceiptProcessor) (*types.Receipt, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config, header.Number), header.BaseFee)
	if err != nil {
		return nil, err
	}
	// Create a new context to be used in the EVM environment
	blockContext := NewEVMBlockContext(header, bc, author)
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, statedb, config, cfg)
	defer func() {
		ite := vmenv.Interpreter()
		vm.EVMInterpreterPool.Put(ite)
		vm.EvmPool.Put(vmenv)
	}()
	return applyTransaction(msg, config, bc, author, gp, statedb, header.Number, header.Hash(), tx, usedGas, vmenv, receiptProcessors...)
}
