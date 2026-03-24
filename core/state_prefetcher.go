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
	"bytes"
	"runtime"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/sync/errgroup"
)

const prefetchMiningThread = 3
const prefetchThreadBALSnapshot = 8
const prefetchThreadBALTrie = 8
const checkInterval = 10

// statePrefetcher is a basic Prefetcher that executes transactions from a block
// on top of the parent state, aiming to prefetch potentially useful state data
// from disk. Transactions are executed in parallel to fully leverage the
// SSD's read performance.
type statePrefetcher struct {
	config     *params.ChainConfig // Chain configuration options
	chain      *HeaderChain        // Canonical block chain
	mevEnabled bool                // Indicate whether MEV is enabled
}

// NewStatePrefetcher initialises a new statePrefetcher.
func NewStatePrefetcher(config *params.ChainConfig, chain *HeaderChain) *statePrefetcher {
	return &statePrefetcher{
		config: config,
		chain:  chain,
	}
}

// EnableMevMode enables MEV mode for this prefetcher.
func (p *statePrefetcher) EnableMevMode() {
	p.mevEnabled = true
}

// Prefetch processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to warm the state caches.
func (p *statePrefetcher) Prefetch(transactions types.Transactions, header *types.Header, gasLimit uint64, statedb *state.StateDB, cfg vm.Config, interrupt *atomic.Bool) {
	var (
		fails   atomic.Int64
		signer  = types.MakeSigner(p.config, header.Number, header.Time)
		workers errgroup.Group
		reader  = statedb.Reader()
	)
	workers.SetLimit(max(1, 3*runtime.NumCPU()/5)) // Aggressively run the prefetching

	// Iterate over and process the individual transactions
	for i, tx := range transactions {
		stateCpy := statedb.Copy() // closure
		workers.Go(func() error {
			// If block precaching was interrupted, abort
			if interrupt != nil && interrupt.Load() {
				return nil
			}
			// Preload the touched accounts and storage slots in advance
			sender, err := types.Sender(signer, tx)
			if err != nil {
				fails.Add(1)
				return nil
			}
			reader.Account(sender)

			if tx.To() != nil {
				account, _ := reader.Account(*tx.To())

				// Preload the contract code if the destination has non-empty code
				if account != nil && !bytes.Equal(account.CodeHash, types.EmptyCodeHash.Bytes()) {
					reader.Code(*tx.To(), common.BytesToHash(account.CodeHash))
				}
			}
			for _, list := range tx.AccessList() {
				reader.Account(list.Address)
				if len(list.StorageKeys) > 0 {
					for _, slot := range list.StorageKeys {
						reader.Storage(list.Address, slot)
					}
				}
			}
			// Execute the message to preload the implicit touched states
			evm := vm.NewEVM(NewEVMBlockContext(header, p.chain, nil), stateCpy, p.config, cfg)

			// Convert the transaction into an executable message and pre-cache its sender
			msg, err := TransactionToMessage(tx, signer, header.BaseFee)
			if err != nil {
				fails.Add(1)
				return nil // Also invalid block, bail out
			}
			// Disable the nonce check
			msg.SkipNonceChecks = true

			stateCpy.SetTxContext(tx.Hash(), i)

			// We attempt to apply a transaction. The goal is not to execute
			// the transaction successfully, rather to warm up touched data slots.
			if _, err := ApplyMessage(evm, msg, new(GasPool).AddGas(gasLimit)); err != nil {
				fails.Add(1)
				return nil // Ugh, something went horribly wrong, bail out
			}
			return nil
		})
	}
	workers.Wait()

	blockPrefetchTxsValidMeter.Mark(int64(len(transactions)) - fails.Load())
	blockPrefetchTxsInvalidMeter.Mark(fails.Load())
	return
}

func (p *statePrefetcher) PrefetchBALSnapshot(balPrefetch *types.BlockAccessListPrefetch, block *types.Block, txSize int, statedb *state.StateDB, interruptChan <-chan struct{}) {
	accChan := make(chan struct {
		txIndex uint32
		accAddr common.Address
	}, prefetchThreadBALSnapshot)

	keyChan := make(chan struct {
		txIndex uint32
		accAddr common.Address
		key     common.Hash
	}, prefetchThreadBALSnapshot)

	// prefetch snapshot cache
	for i := 0; i < prefetchThreadBALSnapshot; i++ {
		go func() {
			newStatedb := statedb.Copy()
			for {
				select {
				case accAddr := <-accChan:
					log.Debug("PrefetchBALSnapshot", "txIndex", accAddr.txIndex, "accAddr", accAddr.accAddr)
					newStatedb.PreloadAccount(accAddr.accAddr)
				case item := <-keyChan:
					log.Debug("PrefetchBALSnapshot", "txIndex", item.txIndex, "accAddr", item.accAddr, "key", item.key)
					newStatedb.PreloadStorage(item.accAddr, item.key)
				case <-interruptChan:
					return
				}
			}
		}()
	}
	for txIndex := 0; txIndex < txSize; txIndex++ {
		txAccessList := balPrefetch.AccessListItems[uint32(txIndex)]
		for accAddr, storageItems := range txAccessList.Accounts {
			select {
			case accChan <- struct {
				txIndex uint32
				accAddr common.Address
			}{
				txIndex: uint32(txIndex),
				accAddr: accAddr,
			}:
			case <-interruptChan:
				return
			}
			for _, storageItem := range storageItems {
				select {
				case keyChan <- struct {
					txIndex uint32
					accAddr common.Address
					key     common.Hash
				}{
					txIndex: uint32(txIndex),
					accAddr: accAddr,
					key:     storageItem.Key,
				}:
				case <-interruptChan:
					return
				}
			}
		}
	}
	log.Debug("PrefetchBALSnapshot dispatch finished")
}

func (p *statePrefetcher) PrefetchBALTrie(balPrefetch *types.BlockAccessListPrefetch, block *types.Block, statedb *state.StateDB, interruptChan <-chan struct{}) {
	accItemsChan := make(chan struct {
		txIndex uint32
		accAddr common.Address
		items   []types.StorageAccessItemPrefetch
	}, prefetchThreadBALTrie)

	for i := 0; i < prefetchThreadBALTrie; i++ {
		go func() {
			newStatedb := statedb.Copy()
			for {
				select {
				case accItem := <-accItemsChan:
					newStatedb.PreloadAccountTrie(accItem.accAddr)
					log.Debug("PrefetchBALTrie", "txIndex", accItem.txIndex, "accAddr", accItem.accAddr)
					for _, storageItem := range accItem.items {
						if storageItem.Dirty {
							log.Debug("PrefetchBALTrie", "txIndex", accItem.txIndex, "accAddr", accItem.accAddr, "storageItem", storageItem.Key, "dirty", storageItem.Dirty)
							statedb.PreloadStorageTrie(accItem.accAddr, storageItem.Key)
						}
					}
				case <-interruptChan:
					return
				}
			}
		}()
	}

	for txIndex, txAccessList := range balPrefetch.AccessListItems {
		for accAddr, storageItems := range txAccessList.Accounts {
			select {
			case accItemsChan <- struct {
				txIndex uint32
				accAddr common.Address
				items   []types.StorageAccessItemPrefetch
			}{
				txIndex: txIndex,
				accAddr: accAddr,
				items:   storageItems,
			}:
			case <-interruptChan:
				log.Warn("PrefetchBALTrie interrupted")
				return
			}
		}
	}
	log.Debug("PrefetchBALTrie dispatch finished")
}

func (p *statePrefetcher) PrefetchBAL(block *types.Block, statedb *state.StateDB, interruptChan <-chan struct{}) {
	if block.BAL() == nil {
		return
	}
	transactions := block.Transactions()
	blockAccessList := block.BAL()

	// get index sorted block access list, each transaction has a list of accounts, each account has a list of storage items
	// txIndex 0:
	// 			 account1: storage1_1, storage1_2, storage1_3
	// 			 account2: storage2_1, storage2_2, storage2_3
	// txIndex 1:
	// 			 account3: storage3_1, storage3_2, storage3_3
	// ...
	balPrefetch := types.BlockAccessListPrefetch{
		AccessListItems: make(map[uint32]types.TxAccessListPrefetch),
	}
	for _, account := range blockAccessList.Accounts {
		balPrefetch.Update(&account)
	}

	// prefetch snapshot cache
	go p.PrefetchBALSnapshot(&balPrefetch, block, len(transactions), statedb, interruptChan)

	// prefetch MPT trie node cache
	go p.PrefetchBALTrie(&balPrefetch, block, statedb, interruptChan)
}

// PrefetchMining processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to warm the state caches. Only used for mining stage.
func (p *statePrefetcher) PrefetchMining(txs TransactionsByPriceAndNonce, header *types.Header, gasLimit uint64, statedb *state.StateDB, cfg vm.Config, interruptCh <-chan struct{}, txCurr **types.Transaction) {
	if statedb == nil {
		return
	}
	var (
		reader = statedb.Reader()
		signer = types.MakeSigner(p.config, header.Number, header.Time)
	)

	// When MEV is not enabled, use more threads for local mining
	threadCount := prefetchMiningThread
	if !p.mevEnabled {
		threadCount = max(prefetchMiningThread, 3*runtime.NumCPU()/5)
	}

	txCh := make(chan *types.Transaction, 2*threadCount)
	for i := 0; i < threadCount; i++ {
		go func(startCh <-chan *types.Transaction, stopCh <-chan struct{}) {
			newStatedb := statedb.Copy()
			evm := vm.NewEVM(NewEVMBlockContext(header, p.chain, nil), newStatedb, p.config, cfg)
			idx := 0
			// Iterate over and process the individual transactions
			for {
				select {
				case tx := <-startCh:
					// Preload the touched accounts and storage slots in advance
					sender, err := types.Sender(signer, tx)
					if err == nil {
						reader.Account(sender)
					}

					if tx.To() != nil {
						account, _ := reader.Account(*tx.To())

						// Preload the contract code if the destination has non-empty code
						if account != nil && !bytes.Equal(account.CodeHash, types.EmptyCodeHash.Bytes()) {
							reader.Code(*tx.To(), common.BytesToHash(account.CodeHash))
						}
					}
					for _, list := range tx.AccessList() {
						reader.Account(list.Address)
						if len(list.StorageKeys) > 0 {
							for _, slot := range list.StorageKeys {
								reader.Storage(list.Address, slot)
							}
						}
					}

					// Convert the transaction into an executable message and pre-cache its sender
					msg, err := TransactionToMessage(tx, signer, header.BaseFee)
					if err != nil {
						continue // Skip invalid tx from txpool
					}
					// Disable the nonce check
					msg.SkipNonceChecks = true

					idx++
					newStatedb.SetTxContext(tx.Hash(), idx)
					ApplyMessage(evm, msg, new(GasPool).AddGas(gasLimit))

				case <-stopCh:
					return
				}
			}
		}(txCh, interruptCh)
	}
	go func(txset TransactionsByPriceAndNonce) {
		count := 0
		for {
			select {
			case <-interruptCh:
				return
			default:
				if count++; count%checkInterval == 0 {
					txset.Forward(*txCurr)
				}
				tx := txset.PeekWithUnwrap()
				if tx == nil {
					return
				}

				select {
				case <-interruptCh:
					return
				case txCh <- tx:
				}

				txset.Shift()
			}
		}
	}(txs)
}
