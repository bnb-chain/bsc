// Copyright 2019 The go-ethereum Authors
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
	"sync/atomic"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

const prefetchThread = 3
const checkInterval = 10

// statePrefetcher is a basic Prefetcher, which blindly executes a block on top
// of an arbitrary state with the goal of prefetching potentially useful state
// data from disk before the main block processor start executing.
type statePrefetcher struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStatePrefetcher initialises a new statePrefetcher.
func NewStatePrefetcher(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *statePrefetcher {
	return &statePrefetcher{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Prefetch processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to pre-cache transaction signatures and snapshot clean state.
func (p *statePrefetcher) Prefetch(block *types.Block, statedb *state.StateDB, cfg vm.Config, interrupt *uint32) {
	var (
		header = block.Header()
		signer = types.MakeSigner(p.config, header.Number)
	)
	transactions := block.Transactions()
	sortTransactions := make([][]*types.Transaction, prefetchThread)
	for i := 0; i < prefetchThread; i++ {
		sortTransactions[i] = make([]*types.Transaction, 0, len(transactions)/prefetchThread)
	}
	for idx := range transactions {
		threadIdx := idx % prefetchThread
		sortTransactions[threadIdx] = append(sortTransactions[threadIdx], transactions[idx])
	}
	// No need to execute the first batch, since the main processor will do it.
	for i := 0; i < prefetchThread; i++ {
		go func(idx int) {
			newStatedb := statedb.Copy()
			newStatedb.EnableWriteOnSharedStorage()
			gaspool := new(GasPool).AddGas(block.GasLimit())
			blockContext := NewEVMBlockContext(header, p.bc, nil)
			evm := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
			// Iterate over and process the individual transactions
			for i, tx := range sortTransactions[idx] {
				// If block precaching was interrupted, abort
				if interrupt != nil && atomic.LoadUint32(interrupt) == 1 {
					return
				}
				// Convert the transaction into an executable message and pre-cache its sender
				msg, err := tx.AsMessage(signer)
				if err != nil {
					return // Also invalid block, bail out
				}
				newStatedb.Prepare(tx.Hash(), header.Hash(), i)
				precacheTransaction(msg, p.config, gaspool, newStatedb, header, evm)
			}
		}(i)
	}
}

// PrefetchMining processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to pre-cache transaction signatures and snapshot clean state. Only used for mining stage
func (p *statePrefetcher) PrefetchMining(txs *types.TransactionsByPriceAndNonce, header *types.Header, gasLimit uint64, statedb *state.StateDB, cfg vm.Config, interruptCh <-chan struct{}, txCurr **types.Transaction) {
	var signer = types.MakeSigner(p.config, header.Number)

	txCh := make(chan *types.Transaction, 2*prefetchThread)
	for i := 0; i < prefetchThread; i++ {
		go func(startCh <-chan *types.Transaction, stopCh <-chan struct{}) {
			idx := 0
			newStatedb := statedb.Copy()
			newStatedb.EnableWriteOnSharedStorage()
			gaspool := new(GasPool).AddGas(gasLimit)
			blockContext := NewEVMBlockContext(header, p.bc, nil)
			evm := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
			// Iterate over and process the individual transactions
			for {
				select {
				case tx := <-startCh:
					// Convert the transaction into an executable message and pre-cache its sender
					msg, err := tx.AsMessageNoNonceCheck(signer)
					if err != nil {
						return // Also invalid block, bail out
					}
					idx++
					newStatedb.Prepare(tx.Hash(), header.Hash(), idx)
					precacheTransaction(msg, p.config, gaspool, newStatedb, header, evm)
					gaspool = new(GasPool).AddGas(gasLimit)
				case <-stopCh:
					return
				}
			}
		}(txCh, interruptCh)
	}
	go func(txset *types.TransactionsByPriceAndNonce) {
		count := 0
		for {
			select {
			case <-interruptCh:
				return
			default:
				if count++; count%checkInterval == 0 {
					txset.Forward(*txCurr)
				}
				tx := txset.Peek()
				if tx == nil {
					return
				}
				txCh <- tx
				txset.Shift()

			}
		}
	}(txs)
}

// precacheTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. The goal is not to execute
// the transaction successfully, rather to warm up touched data slots.
func precacheTransaction(msg types.Message, config *params.ChainConfig, gaspool *GasPool, statedb *state.StateDB, header *types.Header, evm *vm.EVM) {
	// Update the evm with the new transaction context.
	evm.Reset(NewEVMTxContext(msg), statedb)
	// Add addresses to access list if applicable
	ApplyMessage(evm, msg, gaspool)
}
