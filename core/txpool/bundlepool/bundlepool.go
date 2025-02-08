package bundlepool

import (
	"container/heap"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// TODO: decide on a good default value
	// bundleSlotSize is used to calculate how many data slots a single bundle
	// takes up based on its size. The slots are used as DoS protection, ensuring
	// that validating a new bundle remains a constant operation (in reality
	// O(maxslots), where max slots are 4 currently).
	bundleSlotSize = 128 * 1024 // 128KB

	maxMinTimestampFromNow = int64(300) // 5 minutes
)

var (
	bundleGauge = metrics.NewRegisteredGauge("bundlepool/bundles", nil)
	slotsGauge  = metrics.NewRegisteredGauge("bundlepool/slots", nil)
)

var (
	// ErrSimulatorMissing is returned if the bundle simulator is missing.
	ErrSimulatorMissing = errors.New("bundle simulator is missing")

	// ErrBundleTimestampTooHigh is returned if the bundle's MinTimestamp is too high.
	ErrBundleTimestampTooHigh = errors.New("bundle MinTimestamp is too high")

	// ErrBundleGasPriceLow is returned if the bundle gas price is too low.
	ErrBundleGasPriceLow = errors.New("bundle gas price is too low")

	// ErrBundleAlreadyExist is returned if the bundle is already contained
	// within the pool.
	ErrBundleAlreadyExist = errors.New("bundle already exist")
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

type BundleSimulator interface {
	SimulateBundle(bundle *types.Bundle) (*big.Int, error)
}

type BundlePool struct {
	config Config

	bundles    map[common.Hash]*types.Bundle
	bundleHeap BundleHeap // least price heap
	mu         sync.RWMutex

	slots uint64 // Number of slots currently allocated

	// bundleMetrics for mev data analyst
	bundleMetrics   map[int64][][]common.Hash // receivedBlockNumber -> [[bundle tx hashes],[bundle tx hashes]...]
	bundleMetricsMu sync.RWMutex

	simulator  BundleSimulator
	blockchain BlockChain
}

func (p *BundlePool) GetBlobs(vhashes []common.Hash) ([]*kzg4844.Blob, []*kzg4844.Proof) {
	// TODO implement me
	panic("implement me")
}

func (p *BundlePool) Clear() {
	// TODO implement me
	panic("implement me")
}

func New(config Config, chain BlockChain) *BundlePool {
	// Sanitize the input to ensure no vulnerable gas prices are set
	config = (&config).sanitize()

	pool := &BundlePool{
		config:        config,
		bundles:       make(map[common.Hash]*types.Bundle),
		bundleHeap:    make(BundleHeap, 0),
		blockchain:    chain,
		bundleMetrics: make(map[int64][][]common.Hash),
	}

	go pool.clearLoop()

	return pool
}

func (p *BundlePool) clearLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		currentNumber := p.blockchain.CurrentBlock().Number.Int64()

		for number := range p.bundleMetrics {
			if number <= currentNumber-types.MaxBundleAliveBlock {
				p.bundleMetricsMu.Lock()
				delete(p.bundleMetrics, number)
				p.bundleMetricsMu.Unlock()
			}
		}
	}
}

func (p *BundlePool) SetBundleSimulator(simulator BundleSimulator) {
	p.simulator = simulator
}

func (p *BundlePool) Init(gasTip uint64, head *types.Header, reserve txpool.AddressReserver) error {
	return nil
}

func (p *BundlePool) FilterBundle(bundle *types.Bundle) bool {
	for _, tx := range bundle.Txs {
		if !p.filter(tx) {
			return false
		}
	}
	return true
}

// AddBundle adds a mev bundle to the pool
func (p *BundlePool) AddBundle(bundle *types.Bundle) error {
	if p.simulator == nil {
		return ErrSimulatorMissing
	}

	if bundle.MinTimestamp > uint64(time.Now().Unix()+maxMinTimestampFromNow) {
		return ErrBundleTimestampTooHigh
	}

	price, err := p.simulator.SimulateBundle(bundle)
	if err != nil {
		log.Debug("simulation failed when add bundle into bundlepool", "err", err)
		return err
	}
	bundle.Price = price
	hash := bundle.Hash()

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.bundles[hash]; ok {
		return ErrBundleAlreadyExist
	}

	if price.Cmp(p.minimalBundleGasPrice()) < 0 && p.slots+numSlots(bundle) > p.config.GlobalSlots {
		return ErrBundleGasPriceLow
	}

	for p.slots+numSlots(bundle) > p.config.GlobalSlots {
		p.drop()
	}
	p.bundles[hash] = bundle
	heap.Push(&p.bundleHeap, bundle)
	p.slots += numSlots(bundle)

	bundleGauge.Update(int64(len(p.bundles)))
	slotsGauge.Update(int64(p.slots))

	p.bundleMetricsMu.Lock()
	defer p.bundleMetricsMu.Unlock()
	currentHeaderNumber := p.blockchain.CurrentBlock().Number.Int64()
	p.bundleMetrics[currentHeaderNumber] = append(p.bundleMetrics[currentHeaderNumber], bundle.TxHashes())

	return nil
}

func (p *BundlePool) BundleMetrics(fromBlock, toBlock int64) (ret map[int64][][]common.Hash) {
	p.bundleMetricsMu.RLock()
	defer p.bundleMetricsMu.RUnlock()

	for number := fromBlock; number <= toBlock; number++ {
		if bundles, ok := p.bundleMetrics[number]; ok {
			ret[number] = bundles
		}
	}

	return ret
}

func (p *BundlePool) GetBundle(hash common.Hash) *types.Bundle {
	p.mu.RUnlock()
	defer p.mu.RUnlock()

	return p.bundles[hash]
}

func (p *BundlePool) PruneBundle(hash common.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleteBundle(hash)
}

func (p *BundlePool) PendingBundles(blockNumber uint64, blockTimestamp uint64) []*types.Bundle {
	p.mu.Lock()
	defer p.mu.Unlock()

	ret := make([]*types.Bundle, 0)
	for hash, bundle := range p.bundles {
		// Prune outdated bundles
		if (bundle.MaxTimestamp != 0 && blockTimestamp > bundle.MaxTimestamp) ||
			(bundle.MaxBlockNumber != 0 && blockNumber > bundle.MaxBlockNumber) {
			p.deleteBundle(hash)
			continue
		}

		// Roll over future bundles
		if bundle.MinTimestamp != 0 && blockTimestamp < bundle.MinTimestamp {
			continue
		}

		// return the ones that are in time
		ret = append(ret, bundle)
	}

	bundleGauge.Update(int64(len(p.bundles)))
	slotsGauge.Update(int64(p.slots))
	return ret
}

// AllBundles returns all the bundles currently in the pool
func (p *BundlePool) AllBundles() []*types.Bundle {
	p.mu.RLock()
	defer p.mu.RUnlock()
	bundles := make([]*types.Bundle, 0, len(p.bundles))
	for _, bundle := range p.bundles {
		bundles = append(bundles, bundle)
	}
	return bundles
}

func (p *BundlePool) Filter(tx *types.Transaction) bool {
	return false
}

func (p *BundlePool) Close() error {
	log.Info("Bundle pool stopped")
	return nil
}

func (p *BundlePool) Reset(oldHead, newHead *types.Header) {
	p.reset(newHead)
}

// SetGasTip updates the minimum price required by the subpool for a new
// transaction, and drops all transactions below this threshold.
func (p *BundlePool) SetGasTip(tip *big.Int) {}

func (p *BundlePool) SetMaxGas(maxGas uint64) {
}

// Has returns an indicator whether subpool has a transaction cached with the
// given hash.
func (p *BundlePool) Has(hash common.Hash) bool {
	return false
}

// Get returns a transaction if it is contained in the pool, or nil otherwise.
func (p *BundlePool) Get(hash common.Hash) *types.Transaction {
	return nil
}

// Add enqueues a batch of transactions into the pool if they are valid. Due
// to the large transaction churn, add may postpone fully integrating the tx
// to a later point to batch multiple ones together.
func (p *BundlePool) Add(txs []*types.Transaction, sync bool) []error {
	return nil
}

// Pending retrieves all currently processable transactions, grouped by origin
// account and sorted by nonce.
func (p *BundlePool) Pending(filter txpool.PendingFilter) map[common.Address][]*txpool.LazyTransaction {
	return nil
}

// SubscribeTransactions subscribes to new transaction events.
func (p *BundlePool) SubscribeTransactions(ch chan<- core.NewTxsEvent, reorgs bool) event.Subscription {
	return nil
}

// SubscribeReannoTxsEvent should return an event subscription of
// ReannoTxsEvent and send events to the given channel.
func (p *BundlePool) SubscribeReannoTxsEvent(chan<- core.ReannoTxsEvent) event.Subscription {
	return nil
}

// Nonce returns the next nonce of an account, with all transactions executable
// by the pool already applied on topool.
func (p *BundlePool) Nonce(addr common.Address) uint64 {
	return 0
}

// Stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (p *BundlePool) Stats() (int, int) {
	return 0, 0
}

// Content retrieves the data content of the transaction pool, returning all the
// pending as well as queued transactions, grouped by account and sorted by nonce.
func (p *BundlePool) Content() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	return make(map[common.Address][]*types.Transaction), make(map[common.Address][]*types.Transaction)
}

// ContentFrom retrieves the data content of the transaction pool, returning the
// pending as well as queued transactions of this address, grouped by nonce.
func (p *BundlePool) ContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	return []*types.Transaction{}, []*types.Transaction{}
}

// Locals retrieves the accounts currently considered local by the pool.
func (p *BundlePool) Locals() []common.Address {
	return []common.Address{}
}

// Status returns the known status (unknown/pending/queued) of a transaction
// identified by their hashes.
func (p *BundlePool) Status(hash common.Hash) txpool.TxStatus {
	return txpool.TxStatusUnknown
}

func (p *BundlePool) filter(tx *types.Transaction) bool {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType, types.SetCodeTxType:
		return true
	default:
		return false
	}
}

func (p *BundlePool) reset(newHead *types.Header) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Prune outdated bundles
	for hash, bundle := range p.bundles {
		if (bundle.MaxTimestamp != 0 && newHead.Time > bundle.MaxTimestamp) ||
			(bundle.MaxBlockNumber != 0 && newHead.Number.Cmp(new(big.Int).SetUint64(bundle.MaxBlockNumber)) > 0) {
			p.slots -= numSlots(p.bundles[hash])
			delete(p.bundles, hash)
		}
	}
}

// deleteBundle deletes a bundle from the pool.
// It assumes that the caller holds the pool's lock.
func (p *BundlePool) deleteBundle(hash common.Hash) {
	if p.bundles[hash] == nil {
		return
	}

	p.slots -= numSlots(p.bundles[hash])
	delete(p.bundles, hash)
}

// drop removes the bundle with the lowest gas price from the pool.
func (p *BundlePool) drop() {
	for len(p.bundleHeap) > 0 {
		// Pop the bundle with the lowest gas price
		// the min element in the heap may not exist in the pool as it may be pruned
		leastPriceBundleHash := heap.Pop(&p.bundleHeap).(*types.Bundle).Hash()
		if _, ok := p.bundles[leastPriceBundleHash]; ok {
			p.deleteBundle(leastPriceBundleHash)
			break
		}
	}
}

// minimalBundleGasPrice return the lowest gas price from the pool.
func (p *BundlePool) minimalBundleGasPrice() *big.Int {
	for len(p.bundleHeap) != 0 {
		leastPriceBundleHash := p.bundleHeap[0].Hash()
		if bundle, ok := p.bundles[leastPriceBundleHash]; ok {
			return bundle.Price
		}
		heap.Pop(&p.bundleHeap)
	}
	return new(big.Int)
}

// =====================================================================================================================

// numSlots calculates the number of slots needed for a single bundle.
func numSlots(bundle *types.Bundle) uint64 {
	return (bundle.Size() + bundleSlotSize - 1) / bundleSlotSize
}

// =====================================================================================================================

type BundleHeap []*types.Bundle

func (h *BundleHeap) Len() int { return len(*h) }

func (h *BundleHeap) Less(i, j int) bool {
	return (*h)[i].Price.Cmp((*h)[j].Price) == -1
}

func (h *BundleHeap) Swap(i, j int) { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }

func (h *BundleHeap) Push(x interface{}) {
	*h = append(*h, x.(*types.Bundle))
}

func (h *BundleHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
