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
	parallelNum          int                    // leave a CPU to dispatcher
	queueSize            int                    // parallel slot's maximum number of pending Txs
	txResultChan         chan *ParallelTxResult // to notify dispatcher that a tx is done
	slotState            []*SlotState           // idle, or pending messages
	mergedTxIndex        int                    // the latest finalized tx index
	debugErrorRedoNum    int
	debugConflictRedoNum int
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
	tailTxReq        *ParallelTxRequest // tail pending Tx of the slot, should be accessed on dispatcher only.
	pendingTxReqChan chan *ParallelTxRequest
	pendingTxReqList []*ParallelTxRequest // maintained by dispatcher for dispatch policy
	mergedChangeList []state.SlotChangeList
	slotdbChan       chan *state.StateDB // dispatch will create and send this slotDB to slot
}

type ParallelTxResult struct {
	updateSlotDB bool  // for redo and pending tx quest, slot needs new slotDB,
	keepSystem   bool  // for redo, should keep system address's balance
	slotIndex    int   // slot index
	err          error // to describe error message?
	txReq        *ParallelTxRequest
	receipt      *types.Receipt
	slotDB       *state.StateDB // if updated, it is not equal to txReq.slotDB
}

type ParallelTxRequest struct {
	txIndex        int
	tx             *types.Transaction
	slotDB         *state.StateDB
	gasLimit       uint64
	msg            types.Message
	block          *types.Block
	vmConfig       vm.Config
	bloomProcessor *AsyncReceiptBloomGenerator
	usedGas        *uint64
	waitTxChan     chan struct{}
	curTxChan      chan struct{}
}

// to create and start the execution slot goroutines
func (p *ParallelStateProcessor) init() {
	log.Info("Parallel execution mode is enabled", "Parallel Num", p.parallelNum,
		"CPUNum", runtime.NumCPU(),
		"QueueSize", p.queueSize)
	p.txResultChan = make(chan *ParallelTxResult, p.parallelNum)
	p.slotState = make([]*SlotState, p.parallelNum)

	for i := 0; i < p.parallelNum; i++ {
		p.slotState[i] = &SlotState{
			slotdbChan:       make(chan *state.StateDB, 1),
			pendingTxReqChan: make(chan *ParallelTxRequest, p.queueSize),
		}
		// start the slot's goroutine
		go func(slotIndex int) {
			p.runSlotLoop(slotIndex) // this loop will be permanent live
		}(i)
	}
}

// conflict check uses conflict window, it will check all state changes from (cfWindowStart + 1)
// to the previous Tx, if any state in readDb is updated in changeList, then it is conflicted
func (p *ParallelStateProcessor) hasStateConflict(readDb *state.StateDB, changeList state.SlotChangeList) bool {
	// check KV change
	reads := readDb.StateReadsInSlot()
	writes := changeList.StateChangeSet
	for readAddr, readKeys := range reads {
		if _, exist := changeList.AddrStateChangeSet[readAddr]; exist {
			log.Debug("conflict: read addr changed state", "addr", readAddr)
			return true
		}
		if writeKeys, ok := writes[readAddr]; ok {
			// readAddr exist
			for writeKey := range writeKeys {
				// same addr and same key, mark conflicted
				if _, ok := readKeys[writeKey]; ok {
					log.Debug("conflict: state conflict", "addr", readAddr, "key", writeKey)
					return true
				}
			}
		}
	}
	// check balance change
	balanceReads := readDb.BalanceReadsInSlot()
	balanceWrite := changeList.BalanceChangeSet
	for readAddr := range balanceReads {
		if _, exist := changeList.AddrStateChangeSet[readAddr]; exist {
			// SystemAddress is special, SystemAddressRedo() is prepared for it.
			// Since txIndex = 0 will create StateObject for SystemAddress, skip its state change check
			if readAddr != consensus.SystemAddress {
				log.Debug("conflict: read addr changed balance", "addr", readAddr)
				return true
			}
		}
		if _, ok := balanceWrite[readAddr]; ok {
			if readAddr != consensus.SystemAddress {
				log.Debug("conflict: balance conflict", "addr", readAddr)
				return true
			}
		}
	}

	// check code change
	codeReads := readDb.CodeReadsInSlot()
	codeWrite := changeList.CodeChangeSet
	for readAddr := range codeReads {
		if _, exist := changeList.AddrStateChangeSet[readAddr]; exist {
			log.Debug("conflict: read addr changed code", "addr", readAddr)
			return true
		}
		if _, ok := codeWrite[readAddr]; ok {
			log.Debug("conflict: code conflict", "addr", readAddr)
			return true
		}
	}

	// check address state change: create, suicide...
	addrReads := readDb.AddressReadsInSlot()
	addrWrite := changeList.AddrStateChangeSet
	nonceWrite := changeList.NonceChangeSet
	for readAddr := range addrReads {
		if _, ok := addrWrite[readAddr]; ok {
			// SystemAddress is special, SystemAddressRedo() is prepared for it.
			// Since txIndex = 0 will create StateObject for SystemAddress, skip its state change check
			if readAddr != consensus.SystemAddress {
				log.Debug("conflict: address state conflict", "addr", readAddr)
				return true
			}
		}
		if _, ok := nonceWrite[readAddr]; ok {
			log.Debug("conflict: address nonce conflict", "addr", readAddr)
			return true
		}
	}

	return false
}

// for parallel execute, we put contracts of same address in a slot,
// since these txs probably would have conflicts
func (p *ParallelStateProcessor) queueSameToAddress(txReq *ParallelTxRequest) bool {
	txToAddr := txReq.tx.To()
	// To() == nil means contract creation, no same To address
	if txToAddr == nil {
		return false
	}
	for i, slot := range p.slotState {
		if slot.tailTxReq == nil { // this slot is idle
			continue
		}
		for _, pending := range slot.pendingTxReqList {
			// To() == nil means contract creation, skip it.
			if pending.tx.To() == nil {
				continue
			}
			// same to address, put it on slot's pending list.
			if *txToAddr == *pending.tx.To() {
				select {
				case slot.pendingTxReqChan <- txReq:
					slot.tailTxReq = txReq
					slot.pendingTxReqList = append(slot.pendingTxReqList, txReq)
					log.Debug("queue same To address", "Slot", i, "txIndex", txReq.txIndex)
					return true
				default:
					log.Debug("queue same To address, but queue is full", "Slot", i, "txIndex", txReq.txIndex)
					break // try next slot
				}
			}
		}
	}
	return false
}

// for parallel execute, we put contracts of same address in a slot,
// since these txs probably would have conflicts
func (p *ParallelStateProcessor) queueSameFromAddress(txReq *ParallelTxRequest) bool {
	txFromAddr := txReq.msg.From()
	for i, slot := range p.slotState {
		if slot.tailTxReq == nil { // this slot is idle
			continue
		}
		for _, pending := range slot.pendingTxReqList {
			// same from address, put it on slot's pending list.
			if txFromAddr == pending.msg.From() {
				select {
				case slot.pendingTxReqChan <- txReq:
					slot.tailTxReq = txReq
					slot.pendingTxReqList = append(slot.pendingTxReqList, txReq)
					log.Debug("queue same From address", "Slot", i, "txIndex", txReq.txIndex)
					return true
				default:
					log.Debug("queue same From address, but queue is full", "Slot", i, "txIndex", txReq.txIndex)
					break // try next slot
				}
			}
		}
	}
	return false
}

// if there is idle slot, dispatch the msg to the first idle slot
func (p *ParallelStateProcessor) dispatchToIdleSlot(statedb *state.StateDB, txReq *ParallelTxRequest) bool {
	for i, slot := range p.slotState {
		if slot.tailTxReq == nil {
			if len(slot.mergedChangeList) == 0 {
				// first transaction of a slot, there is no usable SlotDB, have to create one for it.
				txReq.slotDB = state.NewSlotDB(statedb, consensus.SystemAddress, p.mergedTxIndex, false)
			}
			log.Debug("dispatchToIdleSlot", "Slot", i, "txIndex", txReq.txIndex)
			slot.tailTxReq = txReq
			slot.pendingTxReqList = append(slot.pendingTxReqList, txReq)
			slot.pendingTxReqChan <- txReq
			return true
		}
	}
	return false
}

// wait until the next Tx is executed and its result is merged to the main stateDB
func (p *ParallelStateProcessor) waitUntilNextTxDone(statedb *state.StateDB, gp *GasPool) *ParallelTxResult {
	var result *ParallelTxResult
	for {
		result = <-p.txResultChan
		// slot may request new slotDB, if slotDB is outdated
		// such as:
		//   tx in pending tx request, previous tx in same queue is likely "damaged" the slotDB
		//   tx redo for conflict
		//   tx stage 1 failed, nonce out of order...
		if result.updateSlotDB {
			// the target slot is waiting for new slotDB
			slotState := p.slotState[result.slotIndex]
			slotDB := state.NewSlotDB(statedb, consensus.SystemAddress, p.mergedTxIndex, result.keepSystem)
			slotState.slotdbChan <- slotDB
			continue
		}
		// ok, the tx result is valid and can be merged
		break
	}

	if err := gp.SubGas(result.receipt.GasUsed); err != nil {
		log.Error("gas limit reached", "block", result.txReq.block.Number(),
			"txIndex", result.txReq.txIndex, "GasUsed", result.receipt.GasUsed, "gp.Gas", gp.Gas())
	}

	resultSlotIndex := result.slotIndex
	resultTxIndex := result.txReq.txIndex
	resultSlotState := p.slotState[resultSlotIndex]
	resultSlotState.pendingTxReqList = resultSlotState.pendingTxReqList[1:]
	if resultSlotState.tailTxReq.txIndex == resultTxIndex {
		log.Debug("ProcessParallel slot is idle", "Slot", resultSlotIndex)
		resultSlotState.tailTxReq = nil
	}

	// Slot's mergedChangeList is produced by dispatcher, while consumed by slot.
	// It is safe, since write and read is in sequential, do write -> notify -> read
	// It is not good, but work right now.
	changeList := statedb.MergeSlotDB(result.slotDB, result.receipt, resultTxIndex)
	resultSlotState.mergedChangeList = append(resultSlotState.mergedChangeList, changeList)

	if resultTxIndex != p.mergedTxIndex+1 {
		log.Error("ProcessParallel tx result out of order", "resultTxIndex", resultTxIndex,
			"p.mergedTxIndex", p.mergedTxIndex)
	}
	p.mergedTxIndex = resultTxIndex
	// notify the following Tx, it is merged,
	// todo(optimize): if next tx is in same slot, it do not need to wait; save this channel cost.
	close(result.txReq.curTxChan)
	return result
}

func (p *ParallelStateProcessor) execInSlot(slotIndex int, txReq *ParallelTxRequest) *ParallelTxResult {
	txIndex := txReq.txIndex
	tx := txReq.tx
	slotDB := txReq.slotDB
	slotGasLimit := txReq.gasLimit // not accurate, but it is ok for block import.
	msg := txReq.msg
	block := txReq.block
	header := block.Header()
	cfg := txReq.vmConfig
	bloomProcessor := txReq.bloomProcessor

	blockContext := NewEVMBlockContext(header, p.bc, nil) // can share blockContext within a block for efficiency
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, slotDB, p.config, cfg)

	var receipt *types.Receipt
	var result *ExecutionResult
	var err error
	var evm *vm.EVM

	slotDB.Prepare(tx.Hash(), txIndex)
	log.Debug("exec In Slot", "Slot", slotIndex, "txIndex", txIndex, "slotDB.baseTxIndex", slotDB.BaseTxIndex())

	gpSlot := new(GasPool).AddGas(slotGasLimit) // each slot would use its own gas pool, and will do gaslimit check later
	evm, result, err = applyTransactionStageExecution(msg, gpSlot, slotDB, vmenv)
	log.Debug("Stage Execution done", "Slot", slotIndex, "txIndex", txIndex, "slotDB.baseTxIndex", slotDB.BaseTxIndex())

	// wait until the previous tx is finalized.
	if txReq.waitTxChan != nil {
		log.Debug("Stage wait previous Tx done", "Slot", slotIndex, "txIndex", txIndex)
		<-txReq.waitTxChan // close the channel
	}

	// in parallel mode, tx can run into trouble, for example: err="nonce too high"
	// in these cases, we will wait and re-run.
	if err != nil {
		p.debugErrorRedoNum++
		log.Debug("Stage Execution err", "Slot", slotIndex, "txIndex", txIndex,
			"current slotDB.baseTxIndex", slotDB.BaseTxIndex(), "err", err)
		redoResult := &ParallelTxResult{
			updateSlotDB: true,
			slotIndex:    slotIndex,
			txReq:        txReq,
			receipt:      receipt,
			err:          err,
		}
		p.txResultChan <- redoResult
		slotDB = <-p.slotState[slotIndex].slotdbChan
		slotDB.Prepare(tx.Hash(), txIndex)
		log.Debug("Stage Execution get new slotdb to redo", "Slot", slotIndex,
			"txIndex", txIndex, "new slotDB.baseTxIndex", slotDB.BaseTxIndex())
		gpSlot = new(GasPool).AddGas(slotGasLimit)
		evm, result, err = applyTransactionStageExecution(msg, gpSlot, slotDB, vmenv)
		if err != nil {
			log.Error("Stage Execution redo, error", err)
		}
	}

	// do conflict detect
	hasConflict := false
	systemAddrConflict := false
	log.Debug("Stage Execution done, do conflict check", "Slot", slotIndex, "txIndex", txIndex)
	if slotDB.SystemAddressRedo() {
		hasConflict = true
		systemAddrConflict = true
	} else {
		for index := 0; index < p.parallelNum; index++ {
			if index == slotIndex {
				continue
			}

			// check all finalizedDb from current slot's
			for _, changeList := range p.slotState[index].mergedChangeList {
				if changeList.TxIndex <= slotDB.BaseTxIndex() {
					continue
				}
				if p.hasStateConflict(slotDB, changeList) {
					log.Debug("Stage Execution conflict", "Slot", slotIndex,
						"txIndex", txIndex, " conflict slot", index, "slotDB.baseTxIndex", slotDB.BaseTxIndex(),
						"conflict txIndex", changeList.TxIndex)
					hasConflict = true
					break
				}
			}
			if hasConflict {
				break
			}
		}
	}

	if hasConflict {
		p.debugConflictRedoNum++
		// re-run should not have conflict, since it has the latest world state.
		redoResult := &ParallelTxResult{
			updateSlotDB: true,
			keepSystem:   systemAddrConflict,
			slotIndex:    slotIndex,
			txReq:        txReq,
			receipt:      receipt,
			err:          err,
		}
		p.txResultChan <- redoResult
		slotDB = <-p.slotState[slotIndex].slotdbChan
		slotDB.Prepare(tx.Hash(), txIndex)
		gpSlot = new(GasPool).AddGas(slotGasLimit)
		evm, result, err = applyTransactionStageExecution(msg, gpSlot, slotDB, vmenv)
		if err != nil {
			log.Error("Stage Execution conflict redo, error", err)
		}
	}

	// goroutine unsafe operation will be handled from here for safety
	gasConsumed := slotGasLimit - gpSlot.Gas()
	if gasConsumed != result.UsedGas {
		log.Error("gasConsumed != result.UsedGas mismatch",
			"gasConsumed", gasConsumed, "result.UsedGas", result.UsedGas)
	}

	log.Debug("ok to finalize this TX",
		"Slot", slotIndex, "txIndex", txIndex, "result.UsedGas", result.UsedGas, "txReq.usedGas", *txReq.usedGas)
	// ok, time to do finalize, stage2 should not be parallel
	receipt, err = applyTransactionStageFinalization(evm, result, msg, p.config, slotDB, header, tx, txReq.usedGas, bloomProcessor)

	if result.Failed() {
		// if Tx is reverted, all its state change will be discarded
		log.Debug("TX reverted?", "Slot", slotIndex, "txIndex", txIndex, "result.Err", result.Err)
		slotDB.RevertSlotDB(msg.From())
	}

	return &ParallelTxResult{
		updateSlotDB: false,
		slotIndex:    slotIndex,
		txReq:        txReq,
		receipt:      receipt,
		slotDB:       slotDB,
		err:          err,
	}
}

func (p *ParallelStateProcessor) runSlotLoop(slotIndex int) {
	curSlot := p.slotState[slotIndex]
	for {
		// wait for new TxReq
		txReq := <-curSlot.pendingTxReqChan
		// receive a dispatched message
		log.Debug("SlotLoop received a new TxReq", "Slot", slotIndex, "txIndex", txReq.txIndex)

		// SlotDB create rational:
		// ** for a dispatched tx,
		//    the slot should be idle, it is better to create a new SlotDB, since new Tx is not related to previous Tx
		// ** for a queued tx,
		//    it is better to create a new SlotDB, since COW is used.
		if txReq.slotDB == nil {
			result := &ParallelTxResult{
				updateSlotDB: true,
				slotIndex:    slotIndex,
				err:          nil,
			}
			p.txResultChan <- result
			txReq.slotDB = <-curSlot.slotdbChan
		}
		result := p.execInSlot(slotIndex, txReq)
		log.Debug("SlotLoop the TxReq is done", "Slot", slotIndex, "err", result.err)
		p.txResultChan <- result
	}
}

// clear slot state for each block.
func (p *ParallelStateProcessor) resetState(txNum int, statedb *state.StateDB) {
	if txNum == 0 {
		return
	}
	p.mergedTxIndex = -1
	p.debugErrorRedoNum = 0
	p.debugConflictRedoNum = 0

	statedb.PrepareForParallel()

	for _, slot := range p.slotState {
		slot.tailTxReq = nil
		slot.mergedChangeList = make([]state.SlotChangeList, 0)
		slot.pendingTxReqList = make([]*ParallelTxRequest, 0)
	}
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

	// Iterate over and process the individual transactions
	posa, isPoSA := p.engine.(consensus.PoSA)
	commonTxs := make([]*types.Transaction, 0, txNum)
	// usually do have two tx, one for validator set contract, another for system reward contract.
	systemTxs := make([]*types.Transaction, 0, 2)

	signer, _, bloomProcessor := p.preExecute(block, statedb, cfg, true)
	var waitTxChan, curTxChan chan struct{}
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
		msg, err := tx.AsMessage(signer, header.BaseFee)
		if err != nil {
			bloomProcessor.Close()
			return statedb, nil, nil, 0, err
		}

		// parallel start, wrap an exec message, which will be dispatched to a slot
		waitTxChan = curTxChan // can be nil, if this is the tx of first batch, otherwise, it is previous Tx's wait channel
		curTxChan = make(chan struct{}, 1)

		txReq := &ParallelTxRequest{
			txIndex:        i,
			tx:             tx,
			slotDB:         nil,
			gasLimit:       gp.Gas(),
			msg:            msg,
			block:          block,
			vmConfig:       cfg,
			bloomProcessor: bloomProcessor,
			usedGas:        usedGas,
			waitTxChan:     waitTxChan,
			curTxChan:      curTxChan,
		}

		// to optimize the for { for {} } loop code style? it is ok right now.
		for {
			if p.queueSameFromAddress(txReq) {
				break
			}

			if p.queueSameToAddress(txReq) {
				break
			}
			// if idle slot available, just dispatch and process next tx.
			if p.dispatchToIdleSlot(statedb, txReq) {
				break
			}
			log.Debug("ProcessParallel no slot available, wait", "txIndex", txReq.txIndex)
			// no idle slot, wait until a tx is executed and merged.
			result := p.waitUntilNextTxDone(statedb, gp)

			// update tx result
			if result.err != nil {
				log.Warn("ProcessParallel a failed tx", "resultSlotIndex", result.slotIndex,
					"resultTxIndex", result.txReq.txIndex, "result.err", result.err)
				bloomProcessor.Close()
				return statedb, nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", result.txReq.txIndex, result.txReq.tx.Hash().Hex(), result.err)
			}

			commonTxs = append(commonTxs, result.txReq.tx)
			receipts = append(receipts, result.receipt)
		}
	}

	// wait until all tx request are done
	for len(commonTxs)+len(systemTxs) < txNum {
		result := p.waitUntilNextTxDone(statedb, gp)
		// update tx result
		if result.err != nil {
			log.Warn("ProcessParallel a failed tx", "resultSlotIndex", result.slotIndex,
				"resultTxIndex", result.txReq.txIndex, "result.err", result.err)
			return statedb, nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", result.txReq.txIndex, result.txReq.tx.Hash().Hex(), result.err)
		}
		commonTxs = append(commonTxs, result.txReq.tx)
		receipts = append(receipts, result.receipt)
	}

	// len(commonTxs) could be 0, such as: https://bscscan.com/block/14580486
	if len(commonTxs) > 0 {
		log.Info("ProcessParallel tx all done", "block", header.Number, "usedGas", *usedGas,
			"txNum", txNum,
			"len(commonTxs)", len(commonTxs),
			"errorNum", p.debugErrorRedoNum,
			"conflictNum", p.debugConflictRedoNum,
			"redoRate(%)", 100*(p.debugErrorRedoNum+p.debugConflictRedoNum)/len(commonTxs))
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

func applyTransactionStageExecution(msg types.Message, gp *GasPool, statedb *state.StateDB, evm *vm.EVM) (*vm.EVM, *ExecutionResult, error) {
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

func applyTransactionStageFinalization(evm *vm.EVM, result *ExecutionResult, msg types.Message, config *params.ChainConfig, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, receiptProcessors ...ReceiptProcessor) (*types.Receipt, error) {
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
