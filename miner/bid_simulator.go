package miner

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bidutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/miner/builderclient"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

const prefetchTxNumber = 50

var (
	bidPreCheckTimer     = metrics.NewRegisteredTimer("bid/preCheck", nil)
	bidTryInterruptTimer = metrics.NewRegisteredTimer("bid/sim/tryInterrupt", nil)
	bidSim1stBidTimer    = metrics.NewRegisteredTimer("bid/sim/sim1stBid", nil)
	bidSimTimer          = metrics.NewRegisteredTimer("bid/sim/duration", nil)

	simulateSpeedGauge = metrics.NewRegisteredGauge("bid/sim/simulateSpeed", nil) // mgasps

	bidSimTimeoutCounter = metrics.NewRegisteredCounter("bid/sim/simTimeout", nil)
)

var (
	diffInTurn = big.NewInt(2) // the difficulty of a block that proposed by an in-turn validator

	dialer = &net.Dialer{
		Timeout:   time.Second,
		KeepAlive: 60 * time.Second,
	}

	transport = &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
	}

	client = &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	errBetterBid  = errors.New("simulation abort due to better bid arrived")
	errNoTimeLeft = errors.New("bid discarded due to lack of simulation time")
)

type bidWorker interface {
	prepareWork(params *generateParams, witness bool) (*environment, error)
	etherbase() common.Address
	getPrefetcher() core.Prefetcher
	fillTransactions(interruptCh chan int32, env *environment, stopTimer *time.Timer, bidTxs mapset.Set[common.Hash]) (err error)
}

// simBidReq is the request for simulating a bid
type simBidReq struct {
	bid         *BidRuntime
	interruptCh chan int32
}

// newBidPackage is the warp of a new bid and a feedback channel
type newBidPackage struct {
	bid         *types.Bid
	feedback    chan error
	receiveTime int64
}

// bidSimulator is in charge of receiving bid from builders, reporting issue to builders.
// And take care of bid simulation, rewards computing, best bid maintaining.
type bidSimulator struct {
	config        *minerconfig.MevConfig
	delayLeftOver time.Duration
	minGasPrice   *big.Int
	chain         *core.BlockChain
	txpool        *txpool.TxPool
	chainConfig   *params.ChainConfig
	engine        consensus.Engine
	bidWorker     bidWorker

	running atomic.Bool // controlled by miner
	exitCh  chan struct{}

	bidReceiving atomic.Bool // controlled by config and eth.AdminAPI

	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	sentryCli *builderclient.Client

	// builder info (warning: only keep status in memory!)
	buildersMu sync.RWMutex
	builders   map[common.Address]*builderclient.Client

	// channels
	simBidCh chan *simBidReq
	newBidCh chan newBidPackage

	pendingMu sync.RWMutex
	pending   map[uint64]map[common.Address]map[common.Hash]struct{} // blockNumber -> builder -> bidHash -> struct{}

	bestBidMu    sync.RWMutex
	bestBid      map[common.Hash]*BidRuntime // prevBlockHash -> bidRuntime
	bestBidToRun map[common.Hash]*types.Bid  // prevBlockHash -> *types.Bid

	simBidMu      sync.RWMutex
	simulatingBid map[common.Hash]*BidRuntime // prevBlockHash -> bidRuntime, in the process of simulation
	bidsToSim     map[uint64][]*BidRuntime    // blockNumber -->  bidRuntime list, used to discard envs

	maxBidsPerBuilder uint32 // Maximum number of bids allowed per builder per block
}

func newBidSimulator(
	config *minerconfig.MevConfig,
	delayLeftOver *time.Duration,
	minGasPrice *big.Int,
	eth Backend,
	chainConfig *params.ChainConfig,
	engine consensus.Engine,
	bidWorker bidWorker,
) *bidSimulator {
	b := &bidSimulator{
		config:        config,
		minGasPrice:   minGasPrice,
		chain:         eth.BlockChain(),
		txpool:        eth.TxPool(),
		chainConfig:   chainConfig,
		engine:        engine,
		bidWorker:     bidWorker,
		exitCh:        make(chan struct{}),
		chainHeadCh:   make(chan core.ChainHeadEvent, chainHeadChanSize),
		builders:      make(map[common.Address]*builderclient.Client),
		simBidCh:      make(chan *simBidReq),
		newBidCh:      make(chan newBidPackage, 100),
		pending:       make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		bestBid:       make(map[common.Hash]*BidRuntime),
		bestBidToRun:  make(map[common.Hash]*types.Bid),
		simulatingBid: make(map[common.Hash]*BidRuntime),
		bidsToSim:     make(map[uint64][]*BidRuntime),
	}
	if delayLeftOver != nil {
		b.delayLeftOver = *delayLeftOver
	}
	if config.MaxBidsPerBuilder != nil {
		b.maxBidsPerBuilder = *config.MaxBidsPerBuilder
	}

	b.chainHeadSub = b.chain.SubscribeChainHeadEvent(b.chainHeadCh)

	if config.Enabled != nil && *config.Enabled {
		b.bidReceiving.Store(true)
		b.dialSentryAndBuilders()

		if len(b.builders) == 0 {
			log.Warn("BidSimulator: no valid builders")
		}
	}

	go b.clearLoop()
	go b.mainLoop()
	go b.newBidLoop()

	return b
}

func (b *bidSimulator) dialSentryAndBuilders() {
	var sentryCli *builderclient.Client
	var err error

	if b.config.SentryURL != "" {
		sentryCli, err = builderclient.DialOptions(context.Background(), b.config.SentryURL, rpc.WithHTTPClient(client))
		if err != nil {
			log.Error("BidSimulator: failed to dial sentry", "url", b.config.SentryURL, "err", err)
		}
	}

	b.sentryCli = sentryCli

	for _, v := range b.config.Builders {
		_ = b.AddBuilder(v.Address, v.URL)
	}
}

func (b *bidSimulator) start() {
	b.running.Store(true)
}

func (b *bidSimulator) stop() {
	b.running.Store(false)
}

func (b *bidSimulator) close() {
	b.running.Store(false)
	close(b.exitCh)
}

func (b *bidSimulator) isRunning() bool {
	return b.running.Load()
}

func (b *bidSimulator) receivingBid() bool {
	return b.bidReceiving.Load()
}

func (b *bidSimulator) startReceivingBid() {
	b.dialSentryAndBuilders()
	b.bidReceiving.Store(true)
}

func (b *bidSimulator) stopReceivingBid() {
	b.bidReceiving.Store(false)
}

func (b *bidSimulator) AddBuilder(builder common.Address, url string) error {
	b.buildersMu.Lock()
	defer b.buildersMu.Unlock()

	if b.sentryCli != nil {
		b.builders[builder] = b.sentryCli
	} else {
		var builderCli *builderclient.Client

		if url != "" {
			var err error

			builderCli, err = builderclient.DialOptions(context.Background(), url, rpc.WithHTTPClient(client))
			if err != nil {
				log.Error("BidSimulator: failed to dial builder", "url", url, "err", err)
				return err
			}
		}

		b.builders[builder] = builderCli
	}

	return nil
}

func (b *bidSimulator) RemoveBuilder(builder common.Address) error {
	b.buildersMu.Lock()
	defer b.buildersMu.Unlock()

	delete(b.builders, builder)

	return nil
}

func (b *bidSimulator) ExistBuilder(builder common.Address) bool {
	b.buildersMu.RLock()
	defer b.buildersMu.RUnlock()

	_, ok := b.builders[builder]

	return ok
}

// best bid here is based on packedBlockReward after the bid is simulated
func (b *bidSimulator) SetBestBid(prevBlockHash common.Hash, bid *BidRuntime) {
	b.bestBidMu.Lock()
	defer b.bestBidMu.Unlock()

	b.bestBid[prevBlockHash] = bid
}

func (b *bidSimulator) GetBestBid(prevBlockHash common.Hash) *BidRuntime {
	b.bestBidMu.RLock()
	defer b.bestBidMu.RUnlock()

	return b.bestBid[prevBlockHash]
}

// best bid to run is based on bid's expectedBlockReward before the bid is simulated
func (b *bidSimulator) SetBestBidToRun(prevBlockHash common.Hash, bid *types.Bid) {
	b.bestBidMu.Lock()
	defer b.bestBidMu.Unlock()

	b.bestBidToRun[prevBlockHash] = bid
}

// in case the bid is invalid(invalid GasUsed,Reward,GasPrice...), remove it.
func (b *bidSimulator) DelBestBidToRun(prevBlockHash common.Hash, delBid *types.Bid) {
	b.bestBidMu.Lock()
	defer b.bestBidMu.Unlock()
	cur := b.bestBidToRun[prevBlockHash]
	if cur == nil || delBid == nil {
		return
	}
	if cur.Hash() == delBid.Hash() {
		delete(b.bestBidToRun, prevBlockHash)
	}
}

func (b *bidSimulator) GetBestBidToRun(prevBlockHash common.Hash) *types.Bid {
	b.bestBidMu.RLock()
	defer b.bestBidMu.RUnlock()

	return b.bestBidToRun[prevBlockHash]
}

func (b *bidSimulator) SetSimulatingBid(prevBlockHash common.Hash, bid *BidRuntime) {
	b.simBidMu.Lock()
	defer b.simBidMu.Unlock()

	b.simulatingBid[prevBlockHash] = bid
}

func (b *bidSimulator) GetSimulatingBid(prevBlockHash common.Hash) *BidRuntime {
	b.simBidMu.RLock()
	defer b.simBidMu.RUnlock()

	return b.simulatingBid[prevBlockHash]
}

func (b *bidSimulator) RemoveSimulatingBid(prevBlockHash common.Hash) {
	b.simBidMu.Lock()
	defer b.simBidMu.Unlock()

	delete(b.simulatingBid, prevBlockHash)
}

func (b *bidSimulator) AddBidToSim(bidRuntime *BidRuntime) {
	b.simBidMu.Lock()
	defer b.simBidMu.Unlock()

	if bidRuntime == nil || bidRuntime.bid == nil {
		return
	}

	blockNumber := bidRuntime.bid.BlockNumber
	if _, ok := b.bidsToSim[blockNumber]; !ok {
		b.bidsToSim[blockNumber] = make([]*BidRuntime, 0, 10)
	}
	b.bidsToSim[blockNumber] = append(b.bidsToSim[blockNumber], bidRuntime)
}

func (b *bidSimulator) mainLoop() {
	defer b.chainHeadSub.Unsubscribe()

	for {
		select {
		case req := <-b.simBidCh:
			if !b.isRunning() {
				continue
			}

			b.simBid(req.interruptCh, req.bid)

		// System stopped
		case <-b.exitCh:
			return

		case <-b.chainHeadSub.Err():
			return
		}
	}
}

func (b *bidSimulator) canBeInterrupted(targetTime uint64) bool {
	if targetTime == 0 {
		// invalid targetTime, disable the interrupt check
		return true
	}
	left := time.Until(time.UnixMilli(int64(targetTime)))
	return left >= *b.config.NoInterruptLeftOver
}

func (b *bidSimulator) newBidLoop() {
	var (
		interruptCh chan int32
	)

	// commit aborts in-flight bid execution with given signal and resubmits a new one.
	commit := func(reason int32, bidRuntime *BidRuntime) {
		if interruptCh != nil {
			// each commit work will have its own interruptCh to stop work with a reason
			interruptCh <- reason
			close(interruptCh)
		}
		interruptCh = make(chan int32, 1)
		bidRuntime.bid.Commit()
		select {
		case b.simBidCh <- &simBidReq{interruptCh: interruptCh, bid: bidRuntime}:
			log.Debug("BidSimulator: commit", "builder", bidRuntime.bid.Builder, "bidHash", bidRuntime.bid.Hash().Hex())
		case <-b.exitCh:
			return
		}
	}

	for {
		select {
		case newBid := <-b.newBidCh:
			if !b.isRunning() {
				continue
			}

			if latestBlockNumber := b.chain.CurrentBlock().Number.Uint64(); newBid.bid.BlockNumber <= latestBlockNumber {
				if newBid.feedback != nil {
					log.Debug("bid is discarded", "blockNumber", newBid.bid.BlockNumber, "latest block", latestBlockNumber)
					newBid.feedback <- fmt.Errorf("bid is discarded, stale block number: %d, latest block: %d", newBid.bid.BlockNumber, latestBlockNumber)
				}
				continue
			}

			bidRuntime, err := newBidRuntime(newBid.bid, *b.config.ValidatorCommission)
			if err != nil {
				if newBid.feedback != nil {
					newBid.feedback <- err
				}
				continue
			}

			var replyErr error
			toCommit := true
			bidAcceptted := true
			bestBidToRun := b.GetBestBidToRun(newBid.bid.ParentHash)
			if bestBidToRun != nil {
				bestBidRuntime, _ := newBidRuntime(bestBidToRun, *b.config.ValidatorCommission)
				if bidRuntime.isExpectedBetterThan(bestBidRuntime) {
					// new bid has better expectedBlockReward, use bidRuntime
					log.Debug("new bid has better expectedBlockReward",
						"builder", bidRuntime.bid.Builder, "bidHash", bidRuntime.bid.Hash().TerminalString())
				} else if !bestBidToRun.IsCommitted() {
					// bestBidToRun is not committed yet, this newBid will trigger bestBidToRun to commit
					bidRuntime = bestBidRuntime
					bidAcceptted = false
					log.Debug("discard new bid and to simulate the non-committed bestBidToRun",
						"builder", bestBidToRun.Builder, "bidHash", bestBidToRun.Hash().TerminalString())
				} else {
					// new bid will be discarded, as it is useless now.
					toCommit = false
					bidAcceptted = false
					log.Debug("new bid will be discarded", "builder", bestBidToRun.Builder,
						"bidHash", bestBidToRun.Hash().TerminalString())
				}
			}

			if toCommit {
				b.SetBestBidToRun(bidRuntime.bid.ParentHash, bidRuntime.bid)
				// try to commit the new bid
				// but if there is a simulating bid and with a short time left, don't interrupt it
				if simulatingBid := b.GetSimulatingBid(newBid.bid.ParentHash); simulatingBid != nil {
					parentHeader := b.chain.GetHeaderByHash(newBid.bid.ParentHash)
					blockInterval := b.getBlockInterval(parentHeader)
					blockTime := parentHeader.MilliTimestamp() + blockInterval
					left := time.Until(time.UnixMilli(int64(blockTime)))
					bidTryInterruptTimer.UpdateSince(time.UnixMilli(newBid.receiveTime))
					if b.canBeInterrupted(blockTime) {
						log.Debug("simulate in progress, interrupt",
							"blockTime", blockTime, "left", left.Milliseconds(),
							"NoInterruptLeftOver", b.config.NoInterruptLeftOver.Milliseconds(),
							"builder", bidRuntime.bid.Builder, "bidHash", bidRuntime.bid.Hash().TerminalString())
						commit(commitInterruptBetterBid, bidRuntime)
					} else {
						log.Debug("simulate in progress, no interrupt",
							"blockTime", blockTime, "left", left.Milliseconds(),
							"NoInterruptLeftOver", b.config.NoInterruptLeftOver.Milliseconds(),
							"builder", bidRuntime.bid.Builder, "bidHash", bidRuntime.bid.Hash().TerminalString())
						if newBid.bid.Hash() == bidRuntime.bid.Hash() {
							replyErr = fmt.Errorf("bid is pending as no enough time to interrupt, left:%d, NoInterruptLeftOver:%d",
								left.Milliseconds(), b.config.NoInterruptLeftOver.Milliseconds())
						}
					}
				} else {
					commit(commitInterruptBetterBid, bidRuntime)
				}
			}

			if newBid.feedback != nil {
				newBid.feedback <- replyErr
				log.Info("[BID ARRIVED]",
					"block", newBid.bid.BlockNumber,
					"builder", newBid.bid.Builder,
					"accepted", bidAcceptted,
					"blockReward", weiToEtherStringF6(bidRuntime.expectedBlockReward),
					"validatorReward", weiToEtherStringF6(bidRuntime.expectedValidatorReward),
					"tx", len(newBid.bid.Txs),
					"hash", newBid.bid.Hash().TerminalString(),
				)
			}

		case <-b.exitCh:
			return
		}
	}
}

// get block interval for current block by using parent header
func (b *bidSimulator) getBlockInterval(parentHeader *types.Header) uint64 {
	if parentHeader == nil {
		return 750 // maxwellBlockInterval
	}
	parlia, _ := b.engine.(*parlia.Parlia)
	// only `Number` and `ParentHash` are used when `BlockInterval`
	tmpHeader := &types.Header{ParentHash: parentHeader.Hash(), Number: new(big.Int).Add(parentHeader.Number, common.Big1)}
	blockInterval, err := parlia.BlockInterval(b.chain, tmpHeader)
	if err != nil {
		log.Debug("failed to get BlockInterval when bidBetterBefore")
	}
	return blockInterval
}

func (b *bidSimulator) bidBetterBefore(parentHash common.Hash) time.Time {
	parentHeader := b.chain.GetHeaderByHash(parentHash)
	return bidutil.BidBetterBefore(parentHeader, b.getBlockInterval(parentHeader), b.delayLeftOver, *b.config.BidSimulationLeftOver)
}

func (b *bidSimulator) clearLoop() {
	clearFn := func(parentHash common.Hash, blockNumber uint64) {
		b.pendingMu.Lock()
		delete(b.pending, blockNumber)
		b.pendingMu.Unlock()

		// clearThreshold := b.chain.GetFinalizedNumber(b.chain.GetHeaderByHash(parentHash))
		clearThreshold := uint64(0) // Leave a sufficient buffer to avoid clearing active bids, which could cause panic
		if blockNumber > b.chain.TriesInMemory() {
			clearThreshold = max(clearThreshold, blockNumber-b.chain.TriesInMemory())
		}

		b.bestBidMu.Lock()
		for k, v := range b.bestBid {
			if v.bid.BlockNumber <= clearThreshold {
				delete(b.bestBid, k)
			}
		}
		for k, v := range b.bestBidToRun {
			if v.BlockNumber <= clearThreshold {
				delete(b.bestBidToRun, k)
			}
		}
		b.bestBidMu.Unlock()

		b.simBidMu.Lock()
		for k, v := range b.simulatingBid {
			if v.bid.BlockNumber <= clearThreshold {
				delete(b.simulatingBid, k)
			}
		}
		for blockNumber, bidList := range b.bidsToSim {
			if blockNumber <= clearThreshold {
				for _, bid := range bidList {
					if bid.env != nil {
						// envs for simulating only discard here
						bid.env.discard()
					}
				}
				delete(b.bidsToSim, blockNumber)
			}
		}
		b.simBidMu.Unlock()
	}

	for {
		select {
		case head := <-b.chainHeadCh:
			if !b.isRunning() {
				continue
			}

			clearFn(head.Header.ParentHash, head.Header.Number.Uint64())

		// System stopped
		case <-b.exitCh:
			return

		case <-b.chainHeadSub.Err():
			return
		}
	}
}

// sendBid checks if the bid is already exists or if the builder sends too many bids,
// if yes, return error, if not, add bid into newBid chan waiting for judge profit.
func (b *bidSimulator) sendBid(ctx context.Context, bid *types.Bid) error {
	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	replyCh := make(chan error, 1)

	receiveTime, ok := ctx.Value("receiveTime").(int64)
	if ok {
		bidPreCheckTimer.UpdateSince(time.UnixMilli(receiveTime))
	} else {
		receiveTime = time.Now().UnixMilli()
	}

	select {
	case b.newBidCh <- newBidPackage{bid: bid, feedback: replyCh, receiveTime: receiveTime}:
		b.AddPending(bid.BlockNumber, bid.Builder, bid.Hash())
	case <-timer.C:
		return types.ErrMevBusy
	}

	select {
	case reply := <-replyCh:
		return reply
	case <-timer.C:
		return types.ErrMevBusy
	}
}

func (b *bidSimulator) CheckPending(blockNumber uint64, builder common.Address, bidHash common.Hash) error {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	// check if bid exists or if builder sends too many bids
	if _, ok := b.pending[blockNumber]; !ok {
		b.pending[blockNumber] = make(map[common.Address]map[common.Hash]struct{})
	}

	if _, ok := b.pending[blockNumber][builder]; !ok {
		b.pending[blockNumber][builder] = make(map[common.Hash]struct{})
	}

	if _, ok := b.pending[blockNumber][builder][bidHash]; ok {
		return errors.New("bid already exists")
	}

	if len(b.pending[blockNumber][builder]) >= int(b.maxBidsPerBuilder) {
		return fmt.Errorf("too many bids: exceeded limit of %d bids per builder per block", b.maxBidsPerBuilder)
	}

	return nil
}

func (b *bidSimulator) AddPending(blockNumber uint64, builder common.Address, bidHash common.Hash) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	b.pending[blockNumber][builder][bidHash] = struct{}{}
}

// simBid simulates a newBid with txs.
// simBid does not enable state prefetching when commit transaction.
func (b *bidSimulator) simBid(interruptCh chan int32, bidRuntime *BidRuntime) {
	// prevent from stopping happen in time interval from sendBid to simBid
	if !b.isRunning() || !b.receivingBid() {
		return
	}

	var (
		startTS = time.Now()

		blockNumber = bidRuntime.bid.BlockNumber
		parentHash  = bidRuntime.bid.ParentHash
		builder     = bidRuntime.bid.Builder

		bidTxs   = bidRuntime.bid.Txs
		bidTxLen = len(bidTxs)
		payBidTx = bidTxs[bidTxLen-1]

		err     error
		success bool
	)

	// ensure simulation exited then start next simulation
	b.SetSimulatingBid(parentHash, bidRuntime)

	defer func(simStart time.Time) {
		logCtx := []any{
			"blockNumber", blockNumber,
			"parentHash", parentHash,
			"builder", builder,
			"bidHash", bidRuntime.bid.Hash().Hex(),
			"gasUsed", bidRuntime.bid.GasUsed,
			"simElapsed", time.Since(simStart),
		}

		if bidRuntime.env != nil {
			logCtx = append(logCtx, "gasLimit", bidRuntime.env.header.GasLimit)
		}

		if err != nil {
			logCtx = append(logCtx, "err", err)
			log.Info("BidSimulator: simulation failed", logCtx...)
			if !errors.Is(errBetterBid, err) && !errors.Is(errNoTimeLeft, err) {
				go b.reportIssue(bidRuntime, err)
			}
		}

		b.RemoveSimulatingBid(parentHash)
		close(bidRuntime.finished)

		if !success {
			b.DelBestBidToRun(parentHash, bidRuntime.bid)
		}

		if err != nil {
			return
		}
		// only recommit last best bid when newBidCh is empty
		if len(b.newBidCh) > 0 {
			return
		}
		toRecommit := b.GetBestBid(parentHash)
		if toRecommit != nil {
			select {
			case b.newBidCh <- newBidPackage{bid: toRecommit.bid}:
				log.Debug("BidSimulator: recommit", "builder", toRecommit.bid.Builder, "bidHash", toRecommit.bid.Hash().Hex())
			default:
			}
		}
	}(startTS)

	// prepareWork will configure header with a suitable time according to consensus
	// prepareWork will start trie prefetching
	if bidRuntime.env, err = b.bidWorker.prepareWork(&generateParams{
		parentHash: bidRuntime.bid.ParentHash,
		coinbase:   b.bidWorker.etherbase(),
	}, false); err != nil {
		return
	}
	b.AddBidToSim(bidRuntime)

	// if the left time is not enough to do simulation, return
	delay := b.engine.Delay(b.chain, bidRuntime.env.header, &b.delayLeftOver)
	if delay == nil || *delay <= 0 {
		err = errNoTimeLeft
		return
	}

	gasLimit := bidRuntime.env.header.GasLimit
	if bidRuntime.env.gasPool == nil {
		bidRuntime.env.gasPool = new(core.GasPool).AddGas(gasLimit)
		if p, ok := b.engine.(*parlia.Parlia); ok {
			bidRuntime.env.gasPool.SubGas(p.EstimateGasReservedForSystemTxs(b.chain, bidRuntime.env.header))
		}
		bidRuntime.env.gasPool.SubGas(params.PayBidTxGasLimit)
	}

	// error log:
	// 	simulation failed blockNumber=47630147 parentHash=0x2476bcc93db4c924a2c8079c6d5d783441a72d6ff70c5850b1afd778102e175e builder=0x48a5Ed9abC1a8FBe86ceC4900483f43a7f2dBB48
	// 	gasUsed=136807406 gasLimit=137816878 err="gas used exceeds gas limit"
	// error tracing:
	// 	left: b.RawBid.GasUsed + b.PayBidTxGasUsed => (136782406 + 25000 = 136807406)
	// 	right: headerGasLimit - b.PayBidTxGasLimit - systemGasReserved => (137816878 - 25000 - 1000000 = 136791878)
	// 	cause: 136807406 > 136791878 => true
	// error reason:
	//	left should not be added with PayBidTxGasUsed, Or right should be not be subtracted with PayBidTxGasLimit
	// error fix:
	//	136782406 > 136791878 => false, Or 136807406 > 136816878 => false
	if bidRuntime.bid.GasUsed > bidRuntime.env.gasPool.Gas() {
		err = errors.New("gas used exceeds gas limit")
		return
	}

	if len(b.bidsToSim[bidRuntime.bid.BlockNumber]) == 1 {
		bidSim1stBidTimer.UpdateSince(time.UnixMilli(int64(b.chain.GetHeaderByHash(bidRuntime.bid.ParentHash).MilliTimestamp())))
	}

	if len(bidRuntime.bid.Txs) > prefetchTxNumber {
		var interrupt atomic.Bool
		defer interrupt.Store(true) // terminate the prefetch at the end
		// TODO(Nathan): use ReadersWithCacheStats to accelerate
		throwaway := bidRuntime.env.state.CopyDoPrefetch()
		// Disable tracing for prefetcher executions.
		vmCfg := *b.chain.GetVMConfig()
		vmCfg.Tracer = nil
		go b.bidWorker.getPrefetcher().Prefetch(bidRuntime.bid.Txs, bidRuntime.env.header, gasLimit, throwaway, vmCfg, &interrupt)
	}

	// commit transactions in bid
	for _, tx := range bidRuntime.bid.Txs {
		select {
		case <-interruptCh:
			err = errBetterBid
			return

		case <-b.exitCh:
			err = errors.New("miner exit")
			return

		default:
		}

		if bidRuntime.env.tcount == bidTxLen-1 {
			break
		}

		err = bidRuntime.commitTransaction(b.chain, b.chainConfig, tx, bidRuntime.bid.UnRevertible.Contains(tx.Hash()))
		if err != nil {
			log.Error("BidSimulator: failed to commit tx", "bidHash", bidRuntime.bid.Hash(), "tx", tx.Hash(), "err", err)
			err = fmt.Errorf("invalid tx in bid, %v", err)
			return
		}
	}

	// check whether time `NoInterruptLeftOver-delayLeftOver` is enough for simulating
	delay = b.engine.Delay(b.chain, bidRuntime.env.header, &b.delayLeftOver)
	if delay != nil && *delay < 0 {
		bidSimTimeoutCounter.Inc(1)
		err = errNoTimeLeft
		return
	}

	// check if bid reward is valid
	{
		bidRuntime.packReward(*b.config.ValidatorCommission)
		if !bidRuntime.validReward() {
			err = errors.New("reward does not achieve the expectation")
			return
		}
	}

	// check if bid gas price is lower than min gas price
	{
		bidGasUsed := uint64(0)
		bidGasFee := big.NewInt(0)

		for i, receipt := range bidRuntime.env.receipts {
			tx := bidRuntime.env.txs[i]
			if !b.txpool.Has(tx.Hash()) {
				bidGasUsed += receipt.GasUsed
				effectiveTip, er := tx.EffectiveGasTip(bidRuntime.env.header.BaseFee)
				if er != nil {
					err = errors.New("failed to calculate effective tip")
					return
				}

				if bidRuntime.env.header.BaseFee != nil {
					effectiveTip.Add(effectiveTip, bidRuntime.env.header.BaseFee)
				}

				gasFee := new(big.Int).Mul(effectiveTip, new(big.Int).SetUint64(receipt.GasUsed))
				bidGasFee.Add(bidGasFee, gasFee)

				if tx.Type() == types.BlobTxType {
					blobFee := new(big.Int).Mul(receipt.BlobGasPrice, new(big.Int).SetUint64(receipt.BlobGasUsed))
					bidGasFee.Add(bidGasFee, blobFee)
				}
			}
		}

		// if bid txs are all from mempool, do not check gas price
		if bidGasUsed != 0 {
			bidGasPrice := new(big.Int).Div(bidGasFee, new(big.Int).SetUint64(bidGasUsed))
			if bidGasPrice.Cmp(b.minGasPrice) < 0 {
				err = fmt.Errorf("bid gas price is lower than min gas price, bid:%v, min:%v", bidGasPrice, b.minGasPrice)
				return
			}
		}
	}

	// if enable greedy merge, fill bid env with transactions from mempool
	greedyMergeElapsed := time.Duration(0)
	if *b.config.GreedyMergeTx {
		endingBidsExtra := 20 * time.Millisecond // Add a buffer to ensure ending bids before `delayLeftOver`
		minTimeLeftForEndingBids := b.delayLeftOver + endingBidsExtra
		delay := b.engine.Delay(b.chain, bidRuntime.env.header, &minTimeLeftForEndingBids)
		if delay != nil && *delay > 0 {
			greedyMergeStartTs := time.Now()
			bidTxsSet := mapset.NewThreadUnsafeSetWithSize[common.Hash](len(bidRuntime.bid.Txs))
			for _, tx := range bidRuntime.bid.Txs {
				bidTxsSet.Add(tx.Hash())
			}
			stopTimer := time.NewTimer(*delay)
			defer stopTimer.Stop()
			fillErr := b.bidWorker.fillTransactions(interruptCh, bidRuntime.env, stopTimer, bidTxsSet)

			// recalculate the packed reward
			bidRuntime.packReward(*b.config.ValidatorCommission)
			greedyMergeElapsed = time.Since(greedyMergeStartTs)

			log.Debug("BidSimulator: greedy merge stopped", "block", bidRuntime.env.header.Number,
				"builder", bidRuntime.bid.Builder, "tx count", bidRuntime.env.tcount-bidTxLen+1, "err", fillErr, "greedyMergeElapsed", greedyMergeElapsed)
		}
	}

	// commit payBidTx at the end of the block
	bidRuntime.env.gasPool.AddGas(params.PayBidTxGasLimit)
	err = bidRuntime.commitTransaction(b.chain, b.chainConfig, payBidTx, true)
	if err != nil {
		log.Error("BidSimulator: failed to commit tx", "builder", bidRuntime.bid.Builder,
			"bidHash", bidRuntime.bid.Hash(), "tx", payBidTx.Hash(), "err", err)
		err = fmt.Errorf("invalid tx in bid, %v", err)
		return
	}

	bestBid := b.GetBestBid(parentHash)
	simElapsed := time.Since(startTS)
	if bestBid == nil {
		winResult := "true[first]"
		log.Info("[BID RESULT]", "win", winResult, "builder", bidRuntime.bid.Builder, "hash", bidRuntime.bid.Hash().TerminalString(), "simElapsed", simElapsed)
	} else if bidRuntime.bid.Hash() != bestBid.bid.Hash() { // skip log flushing when only one bid is present
		log.Info("[BID RESULT]",
			"win", bidRuntime.packedBlockReward.Cmp(bestBid.packedBlockReward) > 0,

			"bidHash", bidRuntime.bid.Hash().TerminalString(),
			"bestHash", bestBid.bid.Hash().TerminalString(),

			"bidGasFee", weiToEtherStringF6(bidRuntime.packedBlockReward),
			"bestGasFee", weiToEtherStringF6(bestBid.packedBlockReward),

			"bidBlockTx", bidRuntime.env.tcount,
			"bestBlockTx", bestBid.env.tcount,

			"simElapsed", simElapsed,
		)
	}
	const minGasForSpeedMetric = 30_000_000
	if bidRuntime.bid.GasUsed > minGasForSpeedMetric {
		timeCostMs := (simElapsed - greedyMergeElapsed).Milliseconds()
		if timeCostMs > 0 {
			simulateSpeedGauge.Update(int64(float64(bidRuntime.bid.GasUsed) / float64(timeCostMs) / 1000))
		}
	}

	// this is the simplest strategy: best for all the delegators.
	if bestBid == nil || bidRuntime.packedBlockReward.Cmp(bestBid.packedBlockReward) > 0 {
		b.SetBestBid(bidRuntime.bid.ParentHash, bidRuntime)
		bidRuntime.duration = time.Since(startTS)
		bidSimTimer.UpdateSince(startTS)
		success = true
	}
}

// reportIssue reports the issue to the mev-sentry
func (b *bidSimulator) reportIssue(bidRuntime *BidRuntime, err error) {
	metrics.GetOrRegisterCounter(fmt.Sprintf("bid/err/%v", bidRuntime.bid.Builder), nil).Inc(1)

	cli := b.builders[bidRuntime.bid.Builder]
	if cli != nil {
		err = cli.ReportIssue(context.Background(), &types.BidIssue{
			Validator: bidRuntime.env.header.Coinbase,
			Builder:   bidRuntime.bid.Builder,
			BidHash:   bidRuntime.bid.Hash(),
			Message:   err.Error(),
		})

		if err != nil {
			log.Warn("BidSimulator: failed to report issue", "builder", bidRuntime.bid.Builder, "err", err)
		}
	}
}

type BidRuntime struct {
	bid *types.Bid

	env *environment

	expectedBlockReward     *big.Int
	expectedValidatorReward *big.Int

	packedBlockReward     *big.Int
	packedValidatorReward *big.Int

	finished chan struct{}
	duration time.Duration
}

func newBidRuntime(newBid *types.Bid, validatorCommission uint64) (*BidRuntime, error) {
	// check the block reward and validator reward of the newBid
	expectedBlockReward := newBid.GasFee
	expectedValidatorReward := new(big.Int).Mul(expectedBlockReward, big.NewInt(int64(validatorCommission)))
	expectedValidatorReward.Div(expectedValidatorReward, big.NewInt(10000))
	expectedValidatorReward.Sub(expectedValidatorReward, newBid.BuilderFee)

	if expectedValidatorReward.Cmp(big.NewInt(0)) < 0 {
		// damage self profit, ignore
		log.Debug("BidSimulator: invalid bid, validator reward is less than 0, ignore",
			"builder", newBid.Builder, "bidHash", newBid.Hash().Hex())
		return nil, fmt.Errorf("validator reward is less than 0, value: %s, commissionConfig: %d", expectedValidatorReward, validatorCommission)
	}

	bidRuntime := &BidRuntime{
		bid:                     newBid,
		expectedBlockReward:     expectedBlockReward,
		expectedValidatorReward: expectedValidatorReward,
		packedBlockReward:       big.NewInt(0),
		packedValidatorReward:   big.NewInt(0),
		finished:                make(chan struct{}),
	}

	return bidRuntime, nil
}

func (r *BidRuntime) validReward() bool {
	return r.packedBlockReward.Cmp(r.expectedBlockReward) >= 0 &&
		r.packedValidatorReward.Cmp(r.expectedValidatorReward) >= 0
}

func (r *BidRuntime) isExpectedBetterThan(other *BidRuntime) bool {
	return r.expectedBlockReward.Cmp(other.expectedBlockReward) >= 0 &&
		r.expectedValidatorReward.Cmp(other.expectedValidatorReward) >= 0
}

// packReward calculates packedBlockReward and packedValidatorReward
func (r *BidRuntime) packReward(validatorCommission uint64) {
	r.packedBlockReward = r.env.state.GetBalance(consensus.SystemAddress).ToBig()
	r.packedValidatorReward = new(big.Int).Mul(r.packedBlockReward, big.NewInt(int64(validatorCommission)))
	r.packedValidatorReward.Div(r.packedValidatorReward, big.NewInt(10000))
	r.packedValidatorReward.Sub(r.packedValidatorReward, r.bid.BuilderFee)
}

func (r *BidRuntime) commitTransaction(chain *core.BlockChain, chainConfig *params.ChainConfig, tx *types.Transaction, unRevertible bool) error {
	var (
		env = r.env
		sc  *types.BlobSidecar
	)

	// Start executing the transaction
	r.env.state.SetTxContext(tx.Hash(), r.env.tcount)

	if tx.Type() == types.BlobTxType {
		sc = types.NewBlobSidecarFromTx(tx)
		if sc == nil {
			return errors.New("blob transaction without blobs in miner")
		}
		// Checking against blob gas limit: It's kind of ugly to perform this check here, but there
		// isn't really a better place right now. The blob gas limit is checked at block validation time
		// and not during execution. This means core.ApplyTransaction will not return an error if the
		// tx has too many blobs. So we have to explicitly check it here.
		if (env.blobs + len(sc.Blobs)) > eip4844.MaxBlobsPerBlock(chainConfig, r.env.header.Time) {
			return errors.New("max data blobs reached")
		}
	}

	receipt, err := core.ApplyTransaction(env.evm, env.gasPool, env.state, env.header, tx,
		&env.header.GasUsed, core.NewReceiptBloomGenerator())
	if err != nil {
		return err
	} else if unRevertible && receipt.Status == types.ReceiptStatusFailed {
		return errors.New("no revertible transaction failed")
	}

	if tx.Type() == types.BlobTxType {
		sc.TxIndex = uint64(len(env.txs))
		env.txs = append(env.txs, tx.WithoutBlobTxSidecar())
		env.receipts = append(env.receipts, receipt)
		env.sidecars = append(env.sidecars, sc)
		env.blobs += len(sc.Blobs)
		*env.header.BlobGasUsed += receipt.BlobGasUsed
	} else {
		env.txs = append(env.txs, tx)
		env.receipts = append(env.receipts, receipt)
	}

	r.env.tcount++

	return nil
}

func weiToEtherStringF6(wei *big.Int) string {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(params.Ether)).Float64()
	return strconv.FormatFloat(f, 'f', 6, 64)
}
