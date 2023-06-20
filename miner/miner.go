// Copyright 2014 The go-ethereum Authors
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

// Package miner implements Ethereum block creation and mining.
package miner

import (
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// Backend wraps all methods required for mining. Only full node is capable
// to offer all the functions here.
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *core.TxPool
}

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase     common.Address `toml:",omitempty"` // Public address for block mining rewards (default = first account)
	Notify        []string       `toml:",omitempty"` // HTTP URL list to be notified of new work packages (only useful in ethash).
	NotifyFull    bool           `toml:",omitempty"` // Notify with pending block headers instead of work packages
	ExtraData     hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	DelayLeftOver time.Duration  // Time reserved to finalize a block(calculate root, distribute income...)
	GasFloor      uint64         // Target gas floor for mined blocks.
	GasCeil       uint64         // Target gas ceiling for mined blocks.
	GasPrice      *big.Int       // Minimum gas price for mining a transaction
	Recommit      time.Duration  // The time interval for miner to re-create mining work.
	Noverify      bool           // Disable remote mining solution verification(only useful in ethash).
	VoteEnable    bool           // Whether to vote when mining

	MEVRelays                   map[string]*rpc.Client // RPC clients to register validator each epoch
	ProposedBlockUri            string                 // received eth_proposedBlocks on that uri
	RegisterValidatorSignedHash []byte                 // signed value of crypto.Keccak256([]byte(ProposedBlockUri))
}

// Miner creates blocks and searches for proof-of-work values.
type Miner struct {
	mux      *event.TypeMux
	worker   *worker
	coinbase common.Address
	eth      Backend
	engine   consensus.Engine
	exitCh   chan struct{}
	startCh  chan common.Address
	stopCh   chan struct{}

	wg sync.WaitGroup

	mevRelays              map[string]*rpc.Client
	proposedBlockUri       string
	signedProposedBlockUri []byte
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(header *types.Header) bool) *Miner {
	miner := &Miner{
		eth:     eth,
		mux:     mux,
		engine:  engine,
		exitCh:  make(chan struct{}),
		startCh: make(chan common.Address),
		stopCh:  make(chan struct{}),
		worker:  newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, false),

		mevRelays:              config.MEVRelays,
		proposedBlockUri:       config.ProposedBlockUri,
		signedProposedBlockUri: config.RegisterValidatorSignedHash,
	}
	miner.wg.Add(1)
	go miner.update()
	return miner
}

// update keeps track of the downloader events. Please be aware that this is a one shot type of update loop.
// It's entered once and as soon as `Done` or `Failed` has been broadcasted the events are unregistered and
// the loop is exited. This to prevent a major security vuln where external parties can DOS you with blocks
// and halt your mining operation for as long as the DOS continues.
func (miner *Miner) update() {
	defer miner.wg.Done()

	events := miner.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	chainBlockCh := make(chan core.ChainHeadEvent, chainHeadChanSize)

	chainBlockSub := miner.eth.BlockChain().SubscribeChainBlockEvent(chainBlockCh)
	defer chainBlockSub.Unsubscribe()

	shouldStart := false
	canStart := true
	dlEventCh := events.Chan()

	// miner started at the middle of an epoch, we want to register it
	miner.registerValidator()

	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				// Unsubscription done, stop listening
				dlEventCh = nil
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				wasMining := miner.Mining()
				miner.worker.stop()
				canStart = false
				if wasMining {
					// Resume mining after sync was finished
					shouldStart = true
					log.Info("Mining aborted due to sync")
				}
			case downloader.FailedEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
			case downloader.DoneEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
				// Stop reacting to downloader events
				events.Unsubscribe()
			}
		case addr := <-miner.startCh:
			miner.SetEtherbase(addr)
			if canStart {
				miner.worker.start()
			}
			shouldStart = true

		case block := <-chainBlockCh:
			// ToDo check if epoch, if so send eth_registerValidator to list of Relays
			if block.Block.NumberU64()%params.BSCChainConfig.Parlia.Epoch == 0 {
				miner.registerValidator()
			}
		case <-miner.stopCh:
			shouldStart = false
			miner.worker.stop()
		case <-miner.exitCh:
			miner.worker.close()
			return
		case <-chainBlockSub.Err():
			return
		}
	}
}

func (miner *Miner) Start(coinbase common.Address) {
	miner.startCh <- coinbase
}

func (miner *Miner) Stop() {
	miner.stopCh <- struct{}{}
}

func (miner *Miner) Close() {
	close(miner.exitCh)
	miner.wg.Wait()
}

func (miner *Miner) Mining() bool {
	return miner.worker.isRunning()
}

func (miner *Miner) Hashrate() uint64 {
	if pow, ok := miner.engine.(consensus.PoW); ok {
		return uint64(pow.Hashrate())
	}
	return 0
}

func (miner *Miner) SetExtra(extra []byte) error {
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
	}
	miner.worker.setExtra(extra)
	return nil
}

// SetRecommitInterval sets the interval for sealing work resubmitting.
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// Pending returns the currently pending block and associated state.
func (miner *Miner) Pending() (*types.Block, *state.StateDB) {
	if miner.worker.isRunning() {
		pendingBlock, pendingState := miner.worker.pending()
		if pendingState != nil && pendingBlock != nil {
			return pendingBlock, pendingState
		}
	}
	// fallback to latest block
	block := miner.worker.chain.CurrentBlock()
	if block == nil {
		return nil, nil
	}
	stateDb, err := miner.worker.chain.StateAt(block.Root())
	if err != nil {
		return nil, nil
	}
	return block, stateDb
}

// PendingBlock returns the currently pending block.
//
// Note, to access both the pending block and the pending state
// simultaneously, please use Pending(), as the pending state can
// change between multiple method calls
func (miner *Miner) PendingBlock() *types.Block {
	if miner.worker.isRunning() {
		pendingBlock := miner.worker.pendingBlock()
		if pendingBlock != nil {
			return pendingBlock
		}
	}
	// fallback to latest block
	return miner.worker.chain.CurrentBlock()
}

// PendingBlockAndReceipts returns the currently pending block and corresponding receipts.
func (miner *Miner) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return miner.worker.pendingBlockAndReceipts()
}

func (miner *Miner) SetEtherbase(addr common.Address) {
	miner.coinbase = addr
	miner.worker.setEtherbase(addr)
}

// SetGasCeil sets the gaslimit to strive for when mining blocks post 1559.
// For pre-1559 blocks, it sets the ceiling.
func (miner *Miner) SetGasCeil(ceil uint64) {
	miner.worker.setGasCeil(ceil)
}

// GetSealingBlock retrieves a sealing block based on the given parameters.
// The returned block is not sealed but all other fields should be filled.
func (miner *Miner) GetSealingBlock(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash) (*types.Block, error) {
	return miner.worker.getSealingBlock(parent, timestamp, coinbase, random)
}

// SubscribePendingLogs starts delivering logs from pending transactions
// to the given channel.
func (miner *Miner) SubscribePendingLogs(ch chan<- []*types.Log) event.Subscription {
	return miner.worker.pendingLogsFeed.Subscribe(ch)
}

// ProposedBlock add the block to the list of works
func (miner *Miner) ProposedBlock(MEVRelay string, blockNumber *big.Int, prevBlockHash common.Hash, reward *big.Int, gasLimit uint64, gasUsed uint64, txs types.Transactions) error {
	log.Debug("Received ProposedBlock", "number", blockNumber, "MEVRelay", MEVRelay, "prevHash", prevBlockHash.Hex(), "potential reward", reward, "gasLimit", gasLimit, "gasUsed", gasUsed, "txcount", len(txs))
	currentGasLimit := atomic.LoadUint64(miner.worker.currentGasLimit)

	if gasUsed > currentGasLimit {
		log.Debug("Skipping the block as gas used exceeds the current block gas limit", "number", blockNumber.Int64(), "proposedBlockGasUsed", gasUsed, "currentGasLimit", currentGasLimit, "chainCurrentBlock", miner.worker.current.header.Number.Int64(), "MEVRelay", MEVRelay)
		return fmt.Errorf("gasUsed exceeds the current block gas limit %v", currentGasLimit)
	}
	miner.worker.proposedCh <- &ProposedBlockArgs{
		mevRelay:      MEVRelay,
		blockNumber:   blockNumber,
		prevBlockHash: prevBlockHash,
		blockReward:   reward,
		gasLimit:      gasLimit,
		gasUsed:       gasUsed,
		txs:           txs,
	}
	return nil
}

func (miner *Miner) registerValidator() {
	log.Info("register validator to MEV relays")
	registerValidatorArgs := &ethapi.RegisterValidatorArgs{
		Data:      []byte(miner.proposedBlockUri),
		Signature: miner.signedProposedBlockUri,
	}
	for dest, destClient := range miner.mevRelays {
		go func(dest string, destinationClient *rpc.Client, registerValidatorArgs *ethapi.RegisterValidatorArgs) {
			var result any

			if err := destinationClient.Call(
				&result, "eth_registerValidator", registerValidatorArgs,
			); err != nil {
				log.Warn("Failed to register validator to MEV relay ", "dest", dest, "err", err)
				return
			}

			log.Debug("register validator to MEV relay", "dest", dest, "result", result)
		}(dest, destClient, registerValidatorArgs)
	}
}
