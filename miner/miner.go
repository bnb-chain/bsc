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
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Backend wraps all methods required for mining. Only full node is capable
// to offer all the functions here.
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *txpool.TxPool
	AccountManager() *accounts.Manager
}

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase     common.Address `toml:",omitempty"` // Public address for block mining rewards
	ExtraData     hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	DelayLeftOver time.Duration  // Time reserved to finalize a block(calculate root, distribute income...)
	GasFloor      uint64         // Target gas floor for mined blocks.
	GasCeil       uint64         // Target gas ceiling for mined blocks.
	GasPrice      *big.Int       // Minimum gas price for mining a transaction
	Recommit      time.Duration  // The time interval for miner to re-create mining work.
	VoteEnable    bool           // Whether to vote when mining

	MevGasPriceFloor int64 `toml:",omitempty"`

	NewPayloadTimeout      time.Duration // The maximum time allowance for creating a new payload
	DisableVoteAttestation bool          // Whether to skip assembling vote attestation

	Mev MevConfig // Mev configuration
}

// DefaultConfig contains default settings for miner.
var DefaultConfig = Config{
	GasCeil:  30000000,
	GasPrice: big.NewInt(params.GWei),

	// The default recommit time is chosen as two seconds since
	// consensus-layer usually will wait a half slot of time(6s)
	// for payload generation. It should be enough for Geth to
	// run 3 rounds.
	Recommit:          3 * time.Second,
	NewPayloadTimeout: 2 * time.Second,
	DelayLeftOver:     50 * time.Millisecond,

	Mev: DefaultMevConfig,
}

// Miner creates blocks and searches for proof-of-work values.
type Miner struct {
	mux     *event.TypeMux
	eth     Backend
	engine  consensus.Engine
	exitCh  chan struct{}
	startCh chan struct{}
	stopCh  chan struct{}
	worker  *worker

	bidSimulator *bidSimulator

	wg sync.WaitGroup
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(header *types.Header) bool) *Miner {
	miner := &Miner{
		mux:     mux,
		eth:     eth,
		engine:  engine,
		exitCh:  make(chan struct{}),
		startCh: make(chan struct{}),
		stopCh:  make(chan struct{}),
		worker:  newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, false),
	}

	miner.bidSimulator = newBidSimulator(&config.Mev, config.DelayLeftOver, config.GasPrice, eth, chainConfig, engine, miner.worker)
	miner.worker.setBestBidFetcher(miner.bidSimulator)

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

	shouldStart := false
	canStart := true
	dlEventCh := events.Chan()
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
				miner.bidSimulator.stop()
				canStart = false
				if wasMining {
					// Resume mining after sync was finished
					shouldStart = true
					log.Info("Mining aborted due to sync")
				}
				miner.worker.syncing.Store(true)

			case downloader.FailedEvent:
				canStart = true
				if shouldStart {
					miner.worker.start()
					miner.bidSimulator.start()
				}
				miner.worker.syncing.Store(false)

			case downloader.DoneEvent:
				canStart = true
				if shouldStart {
					miner.worker.start()
					miner.bidSimulator.start()
				}
				miner.worker.syncing.Store(false)

				// Stop reacting to downloader events
				events.Unsubscribe()
			}
		case <-miner.startCh:
			if canStart {
				miner.worker.start()
				miner.bidSimulator.start()
			}
			shouldStart = true
		case <-miner.stopCh:
			shouldStart = false
			miner.worker.stop()
			miner.bidSimulator.stop()
		case <-miner.exitCh:
			miner.worker.close()
			miner.bidSimulator.close()
			return
		}
	}
}

func (miner *Miner) Start() {
	miner.startCh <- struct{}{}
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

func (miner *Miner) InTurn() bool {
	return miner.worker.inTurn()
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

func (miner *Miner) SetGasTip(tip *big.Int) error {
	miner.worker.setGasTip(tip)
	return nil
}

// SetRecommitInterval sets the interval for sealing work resubmitting.
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// Pending returns the currently pending block and associated state. The returned
// values can be nil in case the pending block is not initialized
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
	stateDb, err := miner.worker.chain.StateAt(block.Root)
	if err != nil {
		return nil, nil
	}
	return miner.worker.chain.GetBlockByHash(block.Hash()), stateDb
}

// PendingBlock returns the currently pending block. The returned block can be
// nil in case the pending block is not initialized.
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
	return miner.worker.chain.GetBlockByHash(miner.worker.chain.CurrentBlock().Hash())
}

// PendingBlockAndReceipts returns the currently pending block and corresponding receipts.
// The returned values can be nil in case the pending block is not initialized.
func (miner *Miner) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return miner.worker.pendingBlockAndReceipts()
}

func (miner *Miner) SetEtherbase(addr common.Address) {
	miner.worker.setEtherbase(addr)
}

// SetGasCeil sets the gaslimit to strive for when mining blocks post 1559.
// For pre-1559 blocks, it sets the ceiling.
func (miner *Miner) SetGasCeil(ceil uint64) {
	miner.worker.setGasCeil(ceil)
}

// SubscribePendingLogs starts delivering logs from pending transactions
// to the given channel.
func (miner *Miner) SubscribePendingLogs(ch chan<- []*types.Log) event.Subscription {
	return miner.worker.pendingLogsFeed.Subscribe(ch)
}

// BuildPayload builds the payload according to the provided parameters.
func (miner *Miner) BuildPayload(args *BuildPayloadArgs) (*Payload, error) {
	return miner.worker.buildPayload(args)
}

func (miner *Miner) GasCeil() uint64 {
	return miner.worker.getGasCeil()
}

func (miner *Miner) SimulateBundle(bundle *types.Bundle) (*big.Int, error) {
	parent := miner.eth.BlockChain().CurrentBlock()

	parentState, err := miner.eth.BlockChain().StateAt(parent.Root)
	if err != nil {
		return nil, err
	}

	env, err := miner.prepareSimulationEnv(parent, parentState)
	if err != nil {
		return nil, err
	}

	s, err := miner.worker.simulateBundle(env, bundle, parentState, env.gasPool, 0, true, true)
	if err != nil {
		return nil, err
	}

	return s.BundleGasPrice, nil
}

func (miner *Miner) SimulateGaslessBundle(bundle *types.Bundle) (*types.SimulateGaslessBundleResp, error) {
	parent := miner.eth.BlockChain().CurrentBlock()

	parentState, err := miner.eth.BlockChain().StateAt(parent.Root)
	if err != nil {
		return nil, err
	}

	env, err := miner.prepareSimulationEnv(parent, parentState)
	if err != nil {
		return nil, err
	}

	resp, err := miner.worker.simulateGaslessBundle(env, bundle)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (miner *Miner) prepareSimulationEnv(parent *types.Header, state *state.StateDB) (*environment, error) {
	timestamp := time.Now().Unix()
	if parent.Time >= uint64(timestamp) {
		timestamp = int64(parent.Time + 1)
	}

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit, miner.worker.config.GasCeil),
		Extra:      miner.worker.extra,
		Time:       uint64(timestamp),
		Coinbase:   miner.worker.etherbase(),
	}

	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if miner.worker.chainConfig.IsLondon(header.Number) {
		header.BaseFee = eip1559.CalcBaseFee(miner.worker.chainConfig, parent)
	}

	if err := miner.worker.engine.Prepare(miner.eth.BlockChain(), header); err != nil {
		return nil, err
	}

	// Apply EIP-4844, EIP-4788.
	if miner.worker.chainConfig.IsCancun(header.Number, header.Time) {
		var excessBlobGas uint64
		if miner.worker.chainConfig.IsCancun(parent.Number, parent.Time) {
			excessBlobGas = eip4844.CalcExcessBlobGas(*parent.ExcessBlobGas, *parent.BlobGasUsed)
		} else {
			// For the first post-fork block, both parent.data_gas_used and parent.excess_data_gas are evaluated as 0
			excessBlobGas = eip4844.CalcExcessBlobGas(0, 0)
		}
		header.BlobGasUsed = new(uint64)
		header.ExcessBlobGas = &excessBlobGas
		if miner.worker.chainConfig.Parlia != nil {
			header.WithdrawalsHash = &types.EmptyWithdrawalsHash
		}
		// if miner.worker.chainConfig.Parlia == nil {
		// 	header.ParentBeaconRoot = genParams.beaconRoot
		// }
	}

	env := &environment{
		header:  header,
		state:   state.Copy(),
		signer:  types.MakeSigner(miner.worker.chainConfig, header.Number, header.Time),
		gasPool: prepareGasPool(header.GasLimit),
	}

	if !miner.worker.chainConfig.IsFeynman(header.Number, header.Time) {
		// Handle upgrade build-in system contract code
		systemcontracts.UpgradeBuildInSystemContract(miner.worker.chainConfig, header.Number, parent.Time, header.Time, env.state)
	}

	return env, nil
}
