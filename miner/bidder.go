package miner

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bidutil"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/miner/validatorclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

const maxBid int64 = 3

type validator struct {
	*validatorclient.Client
	BidSimulationLeftOver time.Duration
	GasCeil               uint64
}

type Bidder struct {
	config        *minerconfig.MevConfig
	delayLeftOver time.Duration
	engine        consensus.Engine
	chain         *core.BlockChain

	validatorsMu sync.RWMutex
	validators   map[common.Address]*validator // address -> validator

	bestWorksMu sync.RWMutex
	bestWorks   map[int64]*environment

	newBidCh chan *environment
	exitCh   chan struct{}

	wg sync.WaitGroup

	wallet accounts.Wallet
}

func NewBidder(config *minerconfig.MevConfig, delayLeftOver time.Duration, engine consensus.Engine, eth Backend) *Bidder {
	b := &Bidder{
		config:        config,
		delayLeftOver: delayLeftOver,
		engine:        engine,
		chain:         eth.BlockChain(),
		validators:    make(map[common.Address]*validator),
		bestWorks:     make(map[int64]*environment),
		newBidCh:      make(chan *environment, 10),
		exitCh:        make(chan struct{}),
	}

	if !config.BuilderEnabled {
		return b
	}

	wallet, err := eth.AccountManager().Find(accounts.Account{Address: config.BuilderAccount})
	if err != nil {
		log.Crit("Bidder: failed to find builder account", "err", err)
	}

	b.wallet = wallet

	for _, v := range config.Validators {
		b.register(v)
	}

	if len(b.validators) == 0 {
		log.Warn("Bidder: No valid validators")
	}

	b.wg.Add(2)
	go b.mainLoop()
	go b.reconnectLoop()

	return b
}

func (b *Bidder) mainLoop() {
	defer b.wg.Done()

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	var (
		bidNum          int64 = 0
		betterBidBefore time.Time
		currentHeight   = b.chain.CurrentBlock().Number.Int64()
	)
	for {
		select {
		case work := <-b.newBidCh:
			if work.header.Number.Int64() > currentHeight {
				currentHeight = work.header.Number.Int64()

				bidNum = 0
				parentHeader := b.chain.GetHeaderByHash(work.header.ParentHash)
				var bidSimulationLeftOver time.Duration
				b.validatorsMu.RLock()
				if b.validators[work.coinbase] != nil {
					bidSimulationLeftOver = b.validators[work.coinbase].BidSimulationLeftOver
				}
				b.validatorsMu.RUnlock()
				betterBidBefore = bidutil.BidBetterBefore(parentHeader, b.chain.Config().Parlia.Period, b.delayLeftOver,
					bidSimulationLeftOver)

				timer.Reset(0)
			}
			if bidNum < maxBid && b.isBestWork(work) {
				// update the bestWork and do bid
				b.setBestWork(work)
			}
		case <-timer.C:
			go func() {
				w := b.getBestWork(currentHeight)
				if w != nil {
					b.bid(w)
					bidNum++
					if bidNum < maxBid && time.Now().Before(betterBidBefore) {
						timer.Reset(time.Until(betterBidBefore) / time.Duration(maxBid-bidNum))
					}
				}
			}()
		case <-b.exitCh:
			return
		}
	}
}

func (b *Bidder) reconnectLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, v := range b.config.Validators {
				if b.isRegistered(v.Address) {
					continue
				}

				b.register(v)
			}
		case <-b.exitCh:
			return
		}
	}
}

func (b *Bidder) isRegistered(validator common.Address) bool {
	b.validatorsMu.RLock()
	defer b.validatorsMu.RUnlock()
	_, ok := b.validators[validator]
	return ok
}

func (b *Bidder) register(cfg minerconfig.ValidatorConfig) {
	b.validatorsMu.Lock()
	defer b.validatorsMu.Unlock()

	cl, err := validatorclient.DialOptions(context.Background(), cfg.URL, rpc.WithHTTPClient(client))
	if err != nil {
		log.Error("Bidder: failed to dial validator", "url", cfg.URL, "err", err)
		return
	}

	params, err := cl.MevParams(context.Background())
	if err != nil {
		log.Error("Bidder: failed to get mev params", "url", cfg.URL, "err", err)
		return
	}

	log.Debug("test: register validator", "address", cfg.Address, "params", params)

	b.validators[cfg.Address] = &validator{
		Client:                cl,
		BidSimulationLeftOver: params.BidSimulationLeftOver,
		GasCeil:               params.GasCeil,
	}
}

func (b *Bidder) unregister(validator common.Address) {
	b.validatorsMu.Lock()
	defer b.validatorsMu.Unlock()
	delete(b.validators, validator)
}

func (b *Bidder) newWork(work *environment) {
	if !b.enabled() {
		return
	}

	if work.profit.Cmp(common.Big0) <= 0 {
		return
	}

	b.newBidCh <- work
}

func (b *Bidder) exit() {
	close(b.exitCh)
	b.wg.Wait()
}

// bid notifies the next in-turn validator the work
// 1. compute the return profit for builder based on realtime traffic and validator commission
// 2. send bid to validator
func (b *Bidder) bid(work *environment) {
	var (
		parent  = b.chain.CurrentBlock()
		bidArgs types.BidArgs
		cli     *validator
	)

	b.validatorsMu.RLock()
	cli = b.validators[work.coinbase]
	b.validatorsMu.RUnlock()
	if cli == nil {
		log.Info("Bidder: validator not integrated", "validator", work.coinbase)
		return
	}

	if len(work.txs) > 0 {
		log.Debug("Bidder: bidding start", "txcount", len(work.txs), "txHash", work.txs[0].Hash())
	}

	// construct bid from work
	{
		var txs []hexutil.Bytes
		for _, tx := range work.txs {
			var txBytes []byte
			var err error
			txBytes, err = tx.MarshalBinary()
			if err != nil {
				log.Error("Bidder: fail to marshal tx", "tx", tx, "err", err)
				return
			}
			txs = append(txs, txBytes)
		}

		bid := types.RawBid{
			BlockNumber:  parent.Number.Uint64() + 1,
			ParentHash:   parent.Hash(),
			GasUsed:      work.header.GasUsed,
			GasFee:       work.state.GetBalance(consensus.SystemAddress).ToBig(),
			Txs:          txs,
			UnRevertible: work.UnRevertible,
			// TODO: decide builderFee according to realtime traffic and validator commission
		}

		signature, err := b.signBid(&bid)
		if err != nil {
			log.Error("Bidder: fail to sign bid", "err", err)
			return
		}

		bidArgs = types.BidArgs{
			RawBid:    &bid,
			Signature: signature,
		}
	}

	_, err := cli.SendBid(context.Background(), bidArgs)
	if err != nil {
		b.deleteBestWork(work)
		log.Error("Bidder: bidding failed", "err", err, "number", work.header.Number, "txcount", len(work.txs),
			"unrevertible", len(work.UnRevertible), "packing_duration", work.duration.Milliseconds())

		var bidErr rpc.Error
		ok := errors.As(err, &bidErr)
		if ok && bidErr.ErrorCode() == types.MevNotRunningError {
			b.unregister(work.coinbase)
		}

		return
	}

	b.deleteBestWork(work)
	log.Info("Bidder: bidding success", "number", work.header.Number, "txs", len(work.txs),
		"packing_duration", work.duration.Milliseconds())
}

// isBestWork returns the work is better than the current best work
func (b *Bidder) isBestWork(work *environment) bool {
	if work.profit == nil {
		return false
	}

	last := b.getBestWork(work.header.Number.Int64())
	if last == nil {
		return true
	}

	return last.profit.Cmp(work.profit) < 0
}

// setBestWork sets the best work
func (b *Bidder) setBestWork(work *environment) {
	b.bestWorksMu.Lock()
	defer b.bestWorksMu.Unlock()

	b.bestWorks[work.header.Number.Int64()] = work
}

// deleteBestWork sets the best work
func (b *Bidder) deleteBestWork(work *environment) {
	b.bestWorksMu.Lock()
	defer b.bestWorksMu.Unlock()

	delete(b.bestWorks, work.header.Number.Int64())
}

// getBestWork returns the best work
func (b *Bidder) getBestWork(blockNumber int64) *environment {
	b.bestWorksMu.RLock()
	defer b.bestWorksMu.RUnlock()

	return b.bestWorks[blockNumber]
}

// signBid signs the bid with builder's account
func (b *Bidder) signBid(bid *types.RawBid) ([]byte, error) {
	bz, err := rlp.EncodeToBytes(bid)
	if err != nil {
		return nil, err
	}

	return b.wallet.SignData(accounts.Account{Address: b.config.BuilderAccount}, accounts.MimetypeTextPlain, bz)
}

// enabled returns whether the bid is enabled
func (b *Bidder) enabled() bool {
	return b.config.BuilderEnabled
}
