package puissantpool

import (
	"errors"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

const (
	// txSlotSize is used to calculate how many data slots a single transaction
	// takes up based on its size. The slots are used as DoS protection, ensuring
	// that validating a new transaction remains a constant operation (in reality
	// O(maxslots), where max slots are 4 currently).
	txSlotSize = 32 * 1024

	// txMaxSize is the maximum size a single transaction can have. This field has
	// non-trivial consequences: larger transactions are significantly harder and
	// more expensive to propagate; larger transactions also take more resources
	// to validate whether they fit into the pool or not.
	txMaxSize = 4 * txSlotSize // 128KB
)

var (
	ErrInBlackList = errors.New("sender or to in black list")
)

// BlockChain defines the minimal set of methods needed to back a tx pool with
// a chain. Exists to allow mocking the live chain out of tests.
type BlockChain interface {
	// Config retrieves the chain's fork configuration.
	Config() *params.ChainConfig

	// CurrentBlock returns the current head of the chain.
	CurrentBlock() *types.Header

	// GetBlock retrieves a specific block, used during pool resets.
	GetBlock(hash common.Hash, number uint64) *types.Block

	// StateAt returns a state database for a given root hash (generally the head).
	StateAt(root common.Hash) (*state.StateDB, error)
}

// Config are the configuration parameters of the transaction pool.
type Config struct {
	PriceLimit uint64 // Minimum gas price to enforce for acceptance into the pool
	PriceBump  uint64 // Minimum price bump percentage to replace an already existing transaction (nonce)

	MaxPuissantPreBlock int              // Maximum amount of puissant sending to miner pre block
	TrustRelays         []common.Address // Addresses that should be treated as sources of puissant package
}

// DefaultConfig contains the default configurations for the transaction pool.
var DefaultConfig = Config{
	PriceLimit: 1,
	PriceBump:  10,

	MaxPuissantPreBlock: 25,
}

// sanitize checks the provided user configurations and changes anything that's
// unreasonable or unworkable.
func (config *Config) sanitize() Config {
	conf := *config
	if conf.PriceLimit < 1 {
		log.Warn("Sanitizing invalid txpool price limit", "provided", conf.PriceLimit, "updated", DefaultConfig.PriceLimit)
		conf.PriceLimit = DefaultConfig.PriceLimit
	}
	if conf.PriceBump < 1 {
		log.Warn("Sanitizing invalid txpool price bump", "provided", conf.PriceBump, "updated", DefaultConfig.PriceBump)
		conf.PriceBump = DefaultConfig.PriceBump
	}
	if conf.MaxPuissantPreBlock < 1 {
		log.Warn("Sanitizing invalid txpool MaxPuissantPreBlock", "provided", conf.MaxPuissantPreBlock, "updated", DefaultConfig.MaxPuissantPreBlock)
		conf.MaxPuissantPreBlock = DefaultConfig.MaxPuissantPreBlock
	}
	return conf
}

type PuissantPool struct {
	config      Config
	chainconfig *params.ChainConfig
	chain       BlockChain
	gasTip      atomic.Pointer[big.Int]
	signer      types.Signer
	mu          sync.RWMutex

	currentHead   atomic.Pointer[types.Header] // Current head of the blockchain
	currentState  *state.StateDB               // Current state in the blockchain head
	pendingNonces *noncer                      // Pending state tracking virtual nonces

	reqResetCh      chan *txpoolResetRequest
	reorgDoneCh     chan chan struct{}
	reorgShutdownCh chan struct{}  // requests shutdown of scheduleReorgLoop
	wg              sync.WaitGroup // tracks loop, scheduleReorgLoop

	// puissantPool is a map of puissant packages, key is bnb payment sender address
	// (to avoid multiple sending, only one pending-puissant is allowed for each sender)
	puissantPool map[common.Address]*types.PuissantBundle

	// trustRelay is a map of trust relay
	trustRelay mapset.Set[common.Address]
}

type txpoolResetRequest struct {
	oldHead, newHead *types.Header
}

// New creates a new transaction pool to gather, sort and filter inbound
// transactions from the network.
func New(config Config, chain BlockChain) *PuissantPool {
	// Sanitize the input to ensure no vulnerable gas prices are set
	config = (&config).sanitize()

	if len(config.TrustRelays) == 0 {
		return nil
	}

	// Create the transaction pool with its initial settings
	pool := &PuissantPool{
		config:          config,
		chain:           chain,
		chainconfig:     chain.Config(),
		signer:          types.LatestSigner(chain.Config()),
		reqResetCh:      make(chan *txpoolResetRequest),
		reorgDoneCh:     make(chan chan struct{}),
		reorgShutdownCh: make(chan struct{}),
	}
	for _, addr := range config.TrustRelays {
		log.Info("Setting new trustRelay", "address", addr)
		pool.trustRelay.Add(addr)
	}
	return pool
}

func (pool *PuissantPool) Init(gasTip *big.Int, head *types.Header) error {
	// Set the basic pool parameters
	pool.gasTip.Store(gasTip)
	pool.reset(nil, head)

	// Start the reorg loop early, so it can handle requests generated during
	// journal loading.
	pool.wg.Add(1)
	go pool.scheduleReorgLoop()

	return nil
}

func (pool *PuissantPool) Close() error {
	close(pool.reorgShutdownCh)
	pool.wg.Wait()

	log.Info("Puissant pool stopped")
	return nil
}

func (pool *PuissantPool) Reset(oldHead, newHead *types.Header) {
	wait := pool.requestReset(oldHead, newHead)
	<-wait
}

func (pool *PuissantPool) AddPuissantBundle(pid types.PuissantID, txs types.Transactions, maxTimestamp uint64, relaySignature hexutil.Bytes) error {
	if err := pool.isFromTrustedRelay(pid, relaySignature); err != nil {
		return err
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if err := pool.validatePuissantTxs(txs); err != nil {
		return err
	}

	senderID, _ := types.Sender(pool.signer, txs[0])

	newPuissant := types.NewPuissantBundle(pid, txs, maxTimestamp)
	if v, has := pool.puissantPool[senderID]; has && v.HasHigherBidPriceThan(newPuissant) {
		return errors.New("rejected, only one pending-puissant per sender is allowed")
	} else {
		pool.puissantPool[senderID] = newPuissant
	}
	return nil
}

func (pool *PuissantPool) requestReset(oldHead *types.Header, newHead *types.Header) chan struct{} {
	select {
	case pool.reqResetCh <- &txpoolResetRequest{oldHead, newHead}:
		return <-pool.reorgDoneCh
	case <-pool.reorgShutdownCh:
		return pool.reorgShutdownCh
	}
}

func (pool *PuissantPool) scheduleReorgLoop() {
	defer pool.wg.Done()

	var (
		curDone       chan struct{} // non-nil while runReorg is active
		nextDone      = make(chan struct{})
		launchNextRun bool
		reset         *txpoolResetRequest
	)
	for {
		// Launch next background reorg if needed
		if curDone == nil && launchNextRun {
			// Run the background reorg and announcements
			go pool.runReorg(nextDone, reset)

			// Prepare everything for the next round of reorg
			curDone, nextDone = nextDone, make(chan struct{})
			launchNextRun = false

			reset = nil
		}

		select {
		case req := <-pool.reqResetCh:
			// Reset request: update head if request is already pending.
			if reset == nil {
				reset = req
			} else {
				reset.newHead = req.newHead
			}
			launchNextRun = true
			pool.reorgDoneCh <- nextDone

		case <-curDone:
			curDone = nil

		case <-pool.reorgShutdownCh:
			// Wait for current run to finish.
			if curDone != nil {
				<-curDone
			}
			close(nextDone)
			return
		}
	}
}

func (pool *PuissantPool) runReorg(done chan struct{}, reset *txpoolResetRequest) {
	defer close(done)

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if reset != nil {
		// Reset from the old head to the new, rescheduling any reorged transactions
		pool.reset(reset.oldHead, reset.newHead)
		pool.demoteBundleLocked()
	}
}

// reset retrieves the current state of the blockchain and ensures the content
// of the transaction pool is valid with regard to the chain state.
func (pool *PuissantPool) reset(oldHead, newHead *types.Header) {
	// Initialize the internal state to the current head
	if newHead == nil {
		newHead = pool.chain.CurrentBlock() // Special case during testing
	}
	statedb, err := pool.chain.StateAt(newHead.Root)
	if err != nil {
		log.Error("Failed to reset txpool state", "err", err)
		return
	}
	pool.currentHead.Store(newHead)
	pool.currentState = statedb
	pool.pendingNonces = newNoncer(statedb)
}

func (pool *PuissantPool) isFromTrustedRelay(pid types.PuissantID, relaySignature hexutil.Bytes) error {
	//recovered, err := crypto.SigToPub(accounts.TextHash(pid[:]), relaySignature)
	//if err != nil {
	//	return err
	//}
	//relayAddr := crypto.PubkeyToAddress(*recovered)
	//if !pool.trustRelay.Contains(relayAddr) {
	//	return fmt.Errorf("invalid relay address %s", relayAddr.String())
	//}
	return nil
}

func (pool *PuissantPool) PendingPuissantBundles(blockTimestamp uint64) types.PuissantBundles {
	var poolPx types.PuissantBundles

	pool.mu.Lock()
	defer pool.mu.Unlock()

	for senderID, each := range pool.puissantPool {
		if blockTimestamp > each.ExpireAt() {
			delete(pool.puissantPool, senderID)
			continue
		}
		poolPx = append(poolPx, each)
	}
	sort.Sort(poolPx)

	for bundleIndex, each := range poolPx {
		for _, tx := range each.Txs() {
			tx.SetPuissantSeq(bundleIndex)
		}
	}

	if len(poolPx) <= pool.config.MaxPuissantPreBlock {
		return poolPx
	}
	return poolPx[:pool.config.MaxPuissantPreBlock]
}

func (pool *PuissantPool) DeletePuissantPackages(set mapset.Set[types.PuissantID]) {
	if set.Cardinality() == 0 {
		return
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	for senderID, each := range pool.puissantPool {
		if set.Contains(each.ID()) {
			delete(pool.puissantPool, senderID)
		}
	}
}

func (pool *PuissantPool) demoteBundleLocked() {
	deleted := mapset.NewThreadUnsafeSet[common.Hash]()

	for senderID, bundle := range pool.puissantPool {
		var del bool
		for _, tx := range bundle.Txs() {
			if deleted.Contains(tx.Hash()) {
				del = true
				break
			} else {
				from, _ := types.Sender(pool.signer, tx)
				if pool.pendingNonces.get(from) > tx.Nonce() {
					del = true
					deleted.Add(tx.Hash())
					break
				}
			}
		}
		if del {
			delete(pool.puissantPool, senderID)
		}
	}
}
