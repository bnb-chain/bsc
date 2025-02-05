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

package eth

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/monitor"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/fetcher"
	"github.com/ethereum/go-ethereum/eth/protocols/bsc"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/eth/protocols/trust"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

const (
	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// voteChanSize is the size of channel listening to NewVotesEvent.
	voteChanSize = 256

	// deltaTdThreshold is the threshold of TD difference for peers to broadcast votes.
	deltaTdThreshold = 20

	// txMaxBroadcastSize is the max size of a transaction that will be broadcasted.
	// All transactions with a higher size will be announced and need to be fetched
	// by the peer.
	txMaxBroadcastSize = 4096
)

var (
	syncChallengeTimeout        = 15 * time.Second // Time allowance for a node to reply to the sync progress challenge
	accountBlacklistPeerCounter = metrics.NewRegisteredCounter("eth/count/blacklist", nil)
)

// txPool defines the methods needed from a transaction pool implementation to
// support all the operations needed by the Ethereum chain protocols.
type txPool interface {
	// Has returns an indicator whether txpool has a transaction
	// cached with the given hash.
	Has(hash common.Hash) bool

	// Get retrieves the transaction from local txpool with given
	// tx hash.
	Get(hash common.Hash) *types.Transaction

	// Add should add the given transactions to the pool.
	Add(txs []*types.Transaction, sync bool) []error

	// Pending should return pending transactions.
	// The slice should be modifiable by the caller.
	Pending(filter txpool.PendingFilter) map[common.Address][]*txpool.LazyTransaction

	// SubscribeTransactions subscribes to new transaction events. The subscriber
	// can decide whether to receive notifications only for newly seen transactions
	// or also for reorged out ones.
	SubscribeTransactions(ch chan<- core.NewTxsEvent, reorgs bool) event.Subscription

	// SubscribeReannoTxsEvent should return an event subscription of
	// ReannoTxsEvent and send events to the given channel.
	SubscribeReannoTxsEvent(chan<- core.ReannoTxsEvent) event.Subscription
}

// votePool defines the methods needed from a votes pool implementation to
// support all the operations needed by the Ethereum chain protocols.
type votePool interface {
	PutVote(vote *types.VoteEnvelope)
	GetVotes() []*types.VoteEnvelope

	// SubscribeNewVoteEvent should return an event subscription of
	// NewVotesEvent and send events to the given channel.
	SubscribeNewVoteEvent(ch chan<- core.NewVoteEvent) event.Subscription
}

// handlerConfig is the collection of initialization parameters to create a full
// node network handler.
type handlerConfig struct {
	NodeID                 enode.ID         // P2P node ID used for tx propagation topology
	Database               ethdb.Database   // Database for direct sync insertions
	Chain                  *core.BlockChain // Blockchain to serve data from
	TxPool                 txPool           // Transaction pool to propagate from
	VotePool               votePool
	Network                uint64                 // Network identifier to adfvertise
	Sync                   ethconfig.SyncMode     // Whether to snap or full sync
	BloomCache             uint64                 // Megabytes to alloc for snap sync bloom
	EventMux               *event.TypeMux         // Legacy event mux, deprecate for `feed`
	RequiredBlocks         map[uint64]common.Hash // Hard coded map of required block hashes for sync challenges
	DirectBroadcast        bool
	DisablePeerTxBroadcast bool
	PeerSet                *peerSet
}

type handler struct {
	nodeID                 enode.ID
	networkID              uint64
	forkFilter             forkid.Filter // Fork ID filter, constant across the lifetime of the node
	disablePeerTxBroadcast bool

	snapSync        atomic.Bool // Flag whether snap sync is enabled (gets disabled if we already have blocks)
	synced          atomic.Bool // Flag whether we're considered synchronised (enables transaction processing)
	acceptTxs       atomic.Bool
	directBroadcast bool

	database             ethdb.Database
	txpool               txPool
	votepool             votePool
	maliciousVoteMonitor *monitor.MaliciousVoteMonitor
	chain                *core.BlockChain
	maxPeers             int
	maxPeersPerIP        int
	peersPerIP           map[string]int
	peerPerIPLock        sync.Mutex

	downloader   *downloader.Downloader
	blockFetcher *fetcher.BlockFetcher
	txFetcher    *fetcher.TxFetcher
	peers        *peerSet

	eventMux       *event.TypeMux
	txsCh          chan core.NewTxsEvent
	txsSub         event.Subscription
	reannoTxsCh    chan core.ReannoTxsEvent
	reannoTxsSub   event.Subscription
	minedBlockSub  *event.TypeMuxSubscription
	voteCh         chan core.NewVoteEvent
	votesSub       event.Subscription
	voteMonitorSub event.Subscription

	requiredBlocks map[uint64]common.Hash

	// channels for fetcher, syncer, txsyncLoop
	quitSync chan struct{}
	stopCh   chan struct{}

	chainSync *chainSyncer
	wg        sync.WaitGroup

	handlerStartCh chan struct{}
	handlerDoneCh  chan struct{}
}

// newHandler returns a handler for all Ethereum chain management protocol.
func newHandler(config *handlerConfig) (*handler, error) {
	// Create the protocol manager with the base fields
	if config.EventMux == nil {
		config.EventMux = new(event.TypeMux) // Nicety initialization for tests
	}
	if config.PeerSet == nil {
		config.PeerSet = newPeerSet() // Nicety initialization for tests
	}
	h := &handler{
		nodeID:                 config.NodeID,
		networkID:              config.Network,
		forkFilter:             forkid.NewFilter(config.Chain),
		disablePeerTxBroadcast: config.DisablePeerTxBroadcast,
		eventMux:               config.EventMux,
		database:               config.Database,
		txpool:                 config.TxPool,
		votepool:               config.VotePool,
		chain:                  config.Chain,
		peers:                  config.PeerSet,
		peersPerIP:             make(map[string]int),
		requiredBlocks:         config.RequiredBlocks,
		directBroadcast:        config.DirectBroadcast,
		quitSync:               make(chan struct{}),
		handlerDoneCh:          make(chan struct{}),
		handlerStartCh:         make(chan struct{}),
		stopCh:                 make(chan struct{}),
	}
	if config.Sync == ethconfig.FullSync {
		// The database seems empty as the current block is the genesis. Yet the snap
		// block is ahead, so snap sync was enabled for this node at a certain point.
		// The scenarios where this can happen is
		// * if the user manually (or via a bad block) rolled back a snap sync node
		//   below the sync point.
		// * the last snap sync is not finished while user specifies a full sync this
		//   time. But we don't have any recent state for full sync.
		// In these cases however it's safe to reenable snap sync.
		fullBlock, snapBlock := h.chain.CurrentBlock(), h.chain.CurrentSnapBlock()
		if fullBlock.Number.Uint64() == 0 && snapBlock.Number.Uint64() > 0 {
			if rawdb.ReadAncientType(h.database) == rawdb.PruneFreezerType {
				log.Crit("Fast Sync not finish, can't enable pruneancient mode")
			}
			h.snapSync.Store(true)
			log.Warn("Switch sync mode from full sync to snap sync", "reason", "snap sync incomplete")
		} else if !h.chain.NoTries() && !h.chain.HasState(fullBlock.Root) {
			h.snapSync.Store(true)
			log.Warn("Switch sync mode from full sync to snap sync", "reason", "head state missing")
		}
	} else {
		head := h.chain.CurrentBlock()
		if head.Number.Uint64() > 0 && h.chain.HasState(head.Root) {
			// Print warning log if database is not empty to run snap sync.
			log.Warn("Switch sync mode from snap sync to full sync", "reason", "snap sync complete")
		} else {
			// If snap sync was requested and our database is empty, grant it
			h.snapSync.Store(true)
			log.Info("Enabled snap sync", "head", head.Number, "hash", head.Hash())
		}
	}
	// If snap sync is requested but snapshots are disabled, fail loudly
	if h.snapSync.Load() && config.Chain.Snapshots() == nil {
		return nil, errors.New("snap sync not supported with snapshots disabled")
	}
	// Construct the downloader (long sync)
	h.downloader = downloader.New(config.Database, h.eventMux, h.chain, h.removePeer, nil)

	// Construct the fetcher (short sync)
	validator := func(header *types.Header) error {
		// Reject all the PoS style headers in the first place. No matter
		// the chain has finished the transition or not, the PoS headers
		// should only come from the trusted consensus layer instead of
		// p2p network.
		if beacon, ok := h.chain.Engine().(*beacon.Beacon); ok {
			if beacon.IsPoSHeader(header) {
				return errors.New("unexpected post-merge header")
			}
		}
		return h.chain.Engine().VerifyHeader(h.chain, header)
	}
	heighter := func() uint64 {
		return h.chain.CurrentBlock().Number.Uint64()
	}
	finalizeHeighter := func() uint64 {
		fblock := h.chain.CurrentFinalBlock()
		if fblock == nil {
			return 0
		}
		return fblock.Number.Uint64()
	}
	inserter := func(blocks types.Blocks) (int, error) {
		// If snap sync is running, deny importing weird blocks. This is a problematic
		// clause when starting up a new network, because snap-syncing miners might not
		// accept each others' blocks until a restart. Unfortunately we haven't figured
		// out a way yet where nodes can decide unilaterally whether the network is new
		// or not. This should be fixed if we figure out a solution.
		if !h.synced.Load() {
			log.Warn("Syncing, discarded propagated block", "number", blocks[0].Number(), "hash", blocks[0].Hash())
			return 0, nil
		}
		return h.chain.InsertChain(blocks)
	}

	broadcastBlockWithCheck := func(block *types.Block, propagate bool) {
		if propagate {
			if !(block.Header().WithdrawalsHash == nil && block.Withdrawals() == nil) &&
				!(block.Header().EmptyWithdrawalsHash() && block.Withdrawals() != nil && len(block.Withdrawals()) == 0) {
				log.Error("Propagated block has invalid withdrawals")
				return
			}
			if err := core.IsDataAvailable(h.chain, block); err != nil {
				log.Error("Propagating block with invalid sidecars", "number", block.Number(), "hash", block.Hash(), "err", err)
				return
			}
		}
		h.BroadcastBlock(block, propagate)
	}

	h.blockFetcher = fetcher.NewBlockFetcher(h.chain.GetBlockByHash, validator, broadcastBlockWithCheck,
		heighter, finalizeHeighter, inserter, h.removePeer)

	fetchTx := func(peer string, hashes []common.Hash) error {
		p := h.peers.peer(peer)
		if p == nil {
			return errors.New("unknown peer")
		}
		return p.RequestTxs(hashes)
	}
	addTxs := func(peer string, txs []*types.Transaction) []error {
		errors := h.txpool.Add(txs, false)
		for _, err := range errors {
			if err == txpool.ErrInBlackList {
				accountBlacklistPeerCounter.Inc(1)
				p := h.peers.peer(peer)
				if p != nil {
					remoteAddr := p.remoteAddr()
					if remoteAddr != nil {
						log.Warn("blacklist account detected from other peer", "remoteAddr", remoteAddr, "ID", p.ID())
					}
				}
			}
		}
		return errors
	}
	h.txFetcher = fetcher.NewTxFetcher(h.txpool.Has, addTxs, fetchTx, h.removePeer)
	h.chainSync = newChainSyncer(h)
	return h, nil
}

// protoTracker tracks the number of active protocol handlers.
func (h *handler) protoTracker() {
	defer h.wg.Done()
	var active int
	for {
		select {
		case <-h.handlerStartCh:
			active++
		case <-h.handlerDoneCh:
			active--
		case <-h.quitSync:
			// Wait for all active handlers to finish.
			for ; active > 0; active-- {
				<-h.handlerDoneCh
			}
			return
		}
	}
}

// incHandlers signals to increment the number of active handlers if not
// quitting.
func (h *handler) incHandlers() bool {
	select {
	case h.handlerStartCh <- struct{}{}:
		return true
	case <-h.quitSync:
		return false
	}
}

// decHandlers signals to decrement the number of active handlers.
func (h *handler) decHandlers() {
	h.handlerDoneCh <- struct{}{}
}

// runEthPeer registers an eth peer into the joint eth/snap peerset, adds it to
// various subsystems and starts handling messages.
func (h *handler) runEthPeer(peer *eth.Peer, handler eth.Handler) error {
	if !h.incHandlers() {
		return p2p.DiscQuitting
	}
	defer h.decHandlers()

	// If the peer has a `snap` extension, wait for it to connect so we can have
	// a uniform initialization/teardown mechanism
	snap, err := h.peers.waitSnapExtension(peer)
	if err != nil {
		peer.Log().Error("Snapshot extension barrier failed", "err", err)
		return err
	}
	trust, err := h.peers.waitTrustExtension(peer)
	if err != nil {
		peer.Log().Error("Trust extension barrier failed", "err", err)
		return err
	}
	bsc, err := h.peers.waitBscExtension(peer)
	if err != nil {
		peer.Log().Error("Bsc extension barrier failed", "err", err)
		return err
	}

	// Execute the Ethereum handshake
	var (
		genesis = h.chain.Genesis()
		head    = h.chain.CurrentHeader()
		hash    = head.Hash()
		number  = head.Number.Uint64()
		td      = h.chain.GetTd(hash, number)
	)
	forkID := forkid.NewID(h.chain.Config(), genesis, number, head.Time)
	if err := peer.Handshake(h.networkID, td, hash, genesis.Hash(), forkID, h.forkFilter, &eth.UpgradeStatusExtension{DisablePeerTxBroadcast: h.disablePeerTxBroadcast}); err != nil {
		peer.Log().Debug("Ethereum handshake failed", "err", err)
		return err
	}
	reject := false // reserved peer slots
	if h.snapSync.Load() {
		if snap == nil {
			// If we are running snap-sync, we want to reserve roughly half the peer
			// slots for peers supporting the snap protocol.
			// The logic here is; we only allow up to 5 more non-snap peers than snap-peers.
			if all, snp := h.peers.len(), h.peers.snapLen(); all-snp > snp+5 {
				reject = true
			}
		}
	}
	// Ignore maxPeers if this is a trusted peer
	peerInfo := peer.Peer.Info()
	if !peerInfo.Network.Trusted {
		if reject || h.peers.len() >= h.maxPeers {
			return p2p.DiscTooManyPeers
		}
	}

	remoteAddr := peerInfo.Network.RemoteAddress
	indexIP := strings.LastIndex(remoteAddr, ":")
	if indexIP == -1 {
		// there could be no IP address, such as a pipe
		peer.Log().Debug("runEthPeer", "no ip address, remoteAddress", remoteAddr)
	} else if !peerInfo.Network.Trusted {
		remoteIP := remoteAddr[:indexIP]
		h.peerPerIPLock.Lock()
		if num, ok := h.peersPerIP[remoteIP]; ok && num >= h.maxPeersPerIP {
			h.peerPerIPLock.Unlock()
			peer.Log().Info("The IP has too many peers", "ip", remoteIP, "maxPeersPerIP", h.maxPeersPerIP,
				"name", peerInfo.Name, "Enode", peerInfo.Enode)
			return p2p.DiscTooManyPeers
		}
		h.peersPerIP[remoteIP] = h.peersPerIP[remoteIP] + 1
		h.peerPerIPLock.Unlock()
	}

	// Register the peer locally
	if err := h.peers.registerPeer(peer, snap, trust, bsc); err != nil {
		peer.Log().Error("Ethereum peer registration failed", "err", err)
		return err
	}
	peer.Log().Debug("Ethereum peer connected", "name", peer.Name(), "peers.len", h.peers.len())
	defer h.unregisterPeer(peer.ID())

	p := h.peers.peer(peer.ID())
	if p == nil {
		return errors.New("peer dropped during handling")
	}
	// Register the peer in the downloader. If the downloader considers it banned, we disconnect
	if err := h.downloader.RegisterPeer(peer.ID(), peer.Version(), peer); err != nil {
		peer.Log().Error("Failed to register peer in eth syncer", "err", err)
		return err
	}
	if snap != nil {
		if err := h.downloader.SnapSyncer.Register(snap); err != nil {
			peer.Log().Error("Failed to register peer in snap syncer", "err", err)
			return err
		}
	}
	h.chainSync.handlePeerEvent()

	// Propagate existing transactions and votes. new transactions and votes appearing
	// after this will be sent via broadcasts.
	h.syncTransactions(peer)
	if h.votepool != nil && p.bscExt != nil {
		h.syncVotes(p.bscExt)
	}

	// Create a notification channel for pending requests if the peer goes down
	dead := make(chan struct{})
	defer close(dead)

	// If we have any explicit peer required block hashes, request them
	for number, hash := range h.requiredBlocks {
		resCh := make(chan *eth.Response)

		req, err := peer.RequestHeadersByNumber(number, 1, 0, false, resCh)
		if err != nil {
			return err
		}
		go func(number uint64, hash common.Hash, req *eth.Request) {
			// Ensure the request gets cancelled in case of error/drop
			defer req.Close()

			timeout := time.NewTimer(syncChallengeTimeout)
			defer timeout.Stop()

			select {
			case res := <-resCh:
				headers := ([]*types.Header)(*res.Res.(*eth.BlockHeadersRequest))
				if len(headers) == 0 {
					// Required blocks are allowed to be missing if the remote
					// node is not yet synced
					res.Done <- nil
					return
				}
				// Validate the header and either drop the peer or continue
				if len(headers) > 1 {
					res.Done <- errors.New("too many headers in required block response")
					return
				}
				if headers[0].Number.Uint64() != number || headers[0].Hash() != hash {
					peer.Log().Info("Required block mismatch, dropping peer", "number", number, "hash", headers[0].Hash(), "want", hash)
					res.Done <- errors.New("required block mismatch")
					return
				}
				peer.Log().Debug("Peer required block verified", "number", number, "hash", hash)
				res.Done <- nil
			case <-timeout.C:
				peer.Log().Warn("Required block challenge timed out, dropping", "addr", peer.RemoteAddr(), "type", peer.Name())
				h.removePeer(peer.ID())
			}
		}(number, hash, req)
	}
	// Handle incoming messages until the connection is torn down
	return handler(peer)
}

// runSnapExtension registers a `snap` peer into the joint eth/snap peerset and
// starts handling inbound messages. As `snap` is only a satellite protocol to
// `eth`, all subsystem registrations and lifecycle management will be done by
// the main `eth` handler to prevent strange races.
func (h *handler) runSnapExtension(peer *snap.Peer, handler snap.Handler) error {
	if !h.incHandlers() {
		return p2p.DiscQuitting
	}
	defer h.decHandlers()

	if err := h.peers.registerSnapExtension(peer); err != nil {
		if metrics.Enabled() {
			if peer.Inbound() {
				snap.IngressRegistrationErrorMeter.Mark(1)
			} else {
				snap.EgressRegistrationErrorMeter.Mark(1)
			}
		}
		peer.Log().Debug("Snapshot extension registration failed", "err", err)
		return err
	}
	return handler(peer)
}

// runTrustExtension registers a `trust` peer into the joint eth/trust peerset and
// starts handling inbound messages. As `trust` is only a satellite protocol to
// `eth`, all subsystem registrations and lifecycle management will be done by
// the main `eth` handler to prevent strange races.
func (h *handler) runTrustExtension(peer *trust.Peer, handler trust.Handler) error {
	if !h.incHandlers() {
		return p2p.DiscQuitting
	}
	defer h.decHandlers()

	if err := h.peers.registerTrustExtension(peer); err != nil {
		if metrics.Enabled() {
			if peer.Inbound() {
				trust.IngressRegistrationErrorMeter.Mark(1)
			} else {
				trust.EgressRegistrationErrorMeter.Mark(1)
			}
		}
		peer.Log().Error("Trust extension registration failed", "err", err)
		return err
	}
	return handler(peer)
}

// runBscExtension registers a `bsc` peer into the joint eth/bsc peerset and
// starts handling inbound messages. As `bsc` is only a satellite protocol to
// `eth`, all subsystem registrations and lifecycle management will be done by
// the main `eth` handler to prevent strange races.
func (h *handler) runBscExtension(peer *bsc.Peer, handler bsc.Handler) error {
	if !h.incHandlers() {
		return p2p.DiscQuitting
	}
	defer h.decHandlers()

	if err := h.peers.registerBscExtension(peer); err != nil {
		if metrics.Enabled() {
			if peer.Inbound() {
				bsc.IngressRegistrationErrorMeter.Mark(1)
			} else {
				bsc.EgressRegistrationErrorMeter.Mark(1)
			}
		}
		peer.Log().Error("Bsc extension registration failed", "err", err, "name", peer.Name())
		return err
	}
	return handler(peer)
}

// removePeer requests disconnection of a peer.
func (h *handler) removePeer(id string) {
	peer := h.peers.peer(id)
	if peer != nil {
		// Hard disconnect at the networking layer. Handler will get an EOF and terminate the peer. defer unregisterPeer will do the cleanup task after then.
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
}

// unregisterPeer removes a peer from the downloader, fetchers and main peer set.
func (h *handler) unregisterPeer(id string) {
	// Create a custom logger to avoid printing the entire id
	var logger log.Logger
	if len(id) < 16 {
		// Tests use short IDs, don't choke on them
		logger = log.New("peer", id)
	} else {
		logger = log.New("peer", id[:8])
	}
	// Abort if the peer does not exist
	peer := h.peers.peer(id)
	if peer == nil {
		logger.Error("Ethereum peer removal failed", "err", errPeerNotRegistered)
		return
	}
	// Remove the `eth` peer if it exists
	logger.Debug("Removing Ethereum peer", "snap", peer.snapExt != nil)

	// Remove the `snap` extension if it exists
	if peer.snapExt != nil {
		h.downloader.SnapSyncer.Unregister(id)
	}
	h.downloader.UnregisterPeer(id)
	h.txFetcher.Drop(id)

	if err := h.peers.unregisterPeer(id); err != nil {
		logger.Error("Ethereum peer removal failed", "err", err)
	}

	peerInfo := peer.Peer.Info()
	remoteAddr := peerInfo.Network.RemoteAddress
	indexIP := strings.LastIndex(remoteAddr, ":")
	if indexIP == -1 {
		// there could be no IP address, such as a pipe
		peer.Log().Debug("unregisterPeer", "name", peerInfo.Name, "no ip address, remoteAddress", remoteAddr)
	} else if !peerInfo.Network.Trusted {
		remoteIP := remoteAddr[:indexIP]
		h.peerPerIPLock.Lock()
		if h.peersPerIP[remoteIP] <= 0 {
			peer.Log().Error("unregisterPeer without record", "name", peerInfo.Name, "remoteAddress", remoteAddr)
		} else {
			h.peersPerIP[remoteIP] = h.peersPerIP[remoteIP] - 1
			logger.Debug("unregisterPeer", "name", peerInfo.Name, "connectNum", h.peersPerIP[remoteIP])
			if h.peersPerIP[remoteIP] == 0 {
				delete(h.peersPerIP, remoteIP)
			}
		}
		h.peerPerIPLock.Unlock()
	}
}

func (h *handler) Start(maxPeers int, maxPeersPerIP int) {
	h.maxPeers = maxPeers
	h.maxPeersPerIP = maxPeersPerIP
	// broadcast and announce transactions (only new ones, not resurrected ones)
	h.wg.Add(1)
	h.txsCh = make(chan core.NewTxsEvent, txChanSize)
	h.txsSub = h.txpool.SubscribeTransactions(h.txsCh, false)
	go h.txBroadcastLoop()

	// broadcast votes
	if h.votepool != nil {
		h.wg.Add(1)
		h.voteCh = make(chan core.NewVoteEvent, voteChanSize)
		h.votesSub = h.votepool.SubscribeNewVoteEvent(h.voteCh)
		go h.voteBroadcastLoop()

		if h.maliciousVoteMonitor != nil {
			h.wg.Add(1)
			go h.startMaliciousVoteMonitor()
		}
	}

	// announce local pending transactions again
	h.wg.Add(1)
	h.reannoTxsCh = make(chan core.ReannoTxsEvent, txChanSize)
	h.reannoTxsSub = h.txpool.SubscribeReannoTxsEvent(h.reannoTxsCh)
	go h.txReannounceLoop()

	// broadcast mined blocks
	h.wg.Add(1)
	h.minedBlockSub = h.eventMux.Subscribe(core.NewMinedBlockEvent{})
	go h.minedBroadcastLoop()

	// start sync handlers
	h.wg.Add(1)
	go h.chainSync.loop()

	// start peer handler tracker
	h.wg.Add(1)
	go h.protoTracker()
}

func (h *handler) startMaliciousVoteMonitor() {
	defer h.wg.Done()
	voteCh := make(chan core.NewVoteEvent, voteChanSize)
	h.voteMonitorSub = h.votepool.SubscribeNewVoteEvent(voteCh)
	for {
		select {
		case event := <-voteCh:
			pendingBlockNumber := h.chain.CurrentHeader().Number.Uint64() + 1
			h.maliciousVoteMonitor.ConflictDetect(event.Vote, pendingBlockNumber)
		case <-h.voteMonitorSub.Err():
			return
		case <-h.stopCh:
			return
		}
	}
}

func (h *handler) Stop() {
	h.txsSub.Unsubscribe()        // quits txBroadcastLoop
	h.reannoTxsSub.Unsubscribe()  // quits txReannounceLoop
	h.minedBlockSub.Unsubscribe() // quits blockBroadcastLoop
	if h.votepool != nil {
		h.votesSub.Unsubscribe() // quits voteBroadcastLoop
		if h.maliciousVoteMonitor != nil {
			h.voteMonitorSub.Unsubscribe()
		}
	}
	close(h.stopCh)
	// Quit chainSync and txsync64.
	// After this is done, no new peers will be accepted.
	close(h.quitSync)

	// Disconnect existing sessions.
	// This also closes the gate for any new registrations on the peer set.
	// sessions which are already established but not added to h.peers yet
	// will exit when they try to register.
	h.peers.close()
	h.wg.Wait()

	log.Info("Ethereum protocol stopped")
}

// BroadcastBlock will either propagate a block to a subset of its peers, or
// will only announce its availability (depending what's requested).
func (h *handler) BroadcastBlock(block *types.Block, propagate bool) {
	// Disable the block propagation if it's the post-merge block.
	if beacon, ok := h.chain.Engine().(*beacon.Beacon); ok {
		if beacon.IsPoSHeader(block.Header()) {
			return
		}
	}
	hash := block.Hash()
	peers := h.peers.peersWithoutBlock(hash)

	// If propagation is requested, send to a subset of the peer
	if propagate {
		// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
		var td *big.Int
		if parent := h.chain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
			td = new(big.Int).Add(block.Difficulty(), h.chain.GetTd(block.ParentHash(), block.NumberU64()-1))
		} else {
			log.Error("Propagating dangling block", "number", block.Number(), "hash", hash)
			return
		}
		// Send the block to a subset of our peers
		var transfer []*ethPeer
		if h.directBroadcast {
			transfer = peers[:]
		} else {
			transfer = peers[:int(math.Sqrt(float64(len(peers))))]
		}

		for _, peer := range transfer {
			peer.AsyncSendNewBlock(block, td)
		}

		log.Trace("Propagated block", "hash", hash, "recipients", len(transfer), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
		return
	}
	// Otherwise if the block is indeed in our own chain, announce it
	if h.chain.HasBlock(hash, block.NumberU64()) {
		for _, peer := range peers {
			peer.AsyncSendNewBlockHash(block)
		}
		log.Trace("Announced block", "hash", hash, "recipients", len(peers), "duration", common.PrettyDuration(time.Since(block.ReceivedAt)))
	}
}

// BroadcastTransactions will propagate a batch of transactions
// - To a square root of all peers for non-blob transactions
// - And, separately, as announcements to all peers which are not known to
// already have the given transaction.
func (h *handler) BroadcastTransactions(txs types.Transactions) {
	var (
		blobTxs  int // Number of blob transactions to announce only
		largeTxs int // Number of large transactions to announce only

		directCount int // Number of transactions sent directly to peers (duplicates included)
		annCount    int // Number of transactions announced across all peers (duplicates included)

		txset = make(map[*ethPeer][]common.Hash) // Set peer->hash to transfer directly
		annos = make(map[*ethPeer][]common.Hash) // Set peer->hash to announce
	)
	// Broadcast transactions to a batch of peers not knowing about it
	direct := big.NewInt(int64(math.Sqrt(float64(h.peers.len())))) // Approximate number of peers to broadcast to
	if direct.BitLen() == 0 {
		direct = big.NewInt(1)
	}
	total := new(big.Int).Exp(direct, big.NewInt(2), nil) // Stabilise total peer count a bit based on sqrt peers

	var (
		signer = types.LatestSignerForChainID(h.chain.Config().ChainID) // Don't care about chain status, we just need *a* sender
		hasher = crypto.NewKeccakState()
		hash   = make([]byte, 32)
	)
	for _, tx := range txs {
		var maybeDirect bool
		switch {
		case tx.Type() == types.BlobTxType:
			blobTxs++
		case tx.Size() > txMaxBroadcastSize:
			largeTxs++
		default:
			maybeDirect = true
		}
		// Send the transaction (if it's small enough) directly to a subset of
		// the peers that have not received it yet, ensuring that the flow of
		// transactions is grouped by account to (try and) avoid nonce gaps.
		//
		// To do this, we hash the local enode IW with together with a peer's
		// enode ID together with the transaction sender and broadcast if
		// `sha(self, peer, sender) mod peers < sqrt(peers)`.
		for _, peer := range h.peers.peersWithoutTransaction(tx.Hash()) {
			var broadcast bool
			if maybeDirect {
				hasher.Reset()
				hasher.Write(h.nodeID.Bytes())
				hasher.Write(peer.Node().ID().Bytes())

				from, _ := types.Sender(signer, tx) // Ignore error, we only use the addr as a propagation target splitter
				hasher.Write(from.Bytes())

				hasher.Read(hash)
				if new(big.Int).Mod(new(big.Int).SetBytes(hash), total).Cmp(direct) < 0 {
					broadcast = true
				}
			}
			if broadcast {
				txset[peer] = append(txset[peer], tx.Hash())
			} else {
				annos[peer] = append(annos[peer], tx.Hash())
			}
		}
	}
	for peer, hashes := range txset {
		directCount += len(hashes)
		peer.AsyncSendTransactions(hashes)
	}
	for peer, hashes := range annos {
		annCount += len(hashes)
		peer.AsyncSendPooledTransactionHashes(hashes)
	}
	log.Debug("Distributed transactions", "plaintxs", len(txs)-blobTxs-largeTxs, "blobtxs", blobTxs, "largetxs", largeTxs,
		"bcastpeers", len(txset), "bcastcount", directCount, "annpeers", len(annos), "anncount", annCount)
}

// ReannounceTransactions will announce a batch of local pending transactions
// to a square root of all peers.
func (h *handler) ReannounceTransactions(txs types.Transactions) {
	hashes := make([]common.Hash, 0, txs.Len())
	for _, tx := range txs {
		hashes = append(hashes, tx.Hash())
	}

	// Announce transactions hash to a batch of peers
	peersCount := uint(math.Sqrt(float64(h.peers.len())))
	peers := h.peers.headPeers(peersCount)
	for _, peer := range peers {
		peer.AsyncSendPooledTransactionHashes(hashes)
	}
	log.Debug("Transaction reannounce", "txs", len(txs),
		"announce packs", peersCount, "announced hashes", peersCount*uint(len(hashes)))
}

// BroadcastVote will propagate a batch of votes to all peers
// which are not known to already have the given vote.
func (h *handler) BroadcastVote(vote *types.VoteEnvelope) {
	var (
		directCount int // Count of announcements made
		directPeers int

		voteMap = make(map[*ethPeer]*types.VoteEnvelope) // Set peer->hash to transfer directly
	)

	// Broadcast vote to a batch of peers not knowing about it
	peers := h.peers.peersWithoutVote(vote.Hash())
	headBlock := h.chain.CurrentBlock()
	currentTD := h.chain.GetTd(headBlock.Hash(), headBlock.Number.Uint64())
	for _, peer := range peers {
		_, peerTD := peer.Head()
		deltaTD := new(big.Int).Abs(new(big.Int).Sub(currentTD, peerTD))
		if deltaTD.Cmp(big.NewInt(deltaTdThreshold)) < 1 && peer.bscExt != nil {
			voteMap[peer] = vote
		}
	}

	for peer, _vote := range voteMap {
		directPeers++
		directCount += 1
		votes := []*types.VoteEnvelope{_vote}
		peer.bscExt.AsyncSendVotes(votes)
	}
	log.Debug("Vote broadcast", "vote packs", directPeers, "broadcast vote", directCount)
}

// minedBroadcastLoop sends mined blocks to connected peers.
func (h *handler) minedBroadcastLoop() {
	defer h.wg.Done()

	for {
		select {
		case obj := <-h.minedBlockSub.Chan():
			if obj == nil {
				continue
			}
			if ev, ok := obj.Data.(core.NewMinedBlockEvent); ok {
				h.BroadcastBlock(ev.Block, true)  // First propagate block to peers
				h.BroadcastBlock(ev.Block, false) // Only then announce to the rest
			}
		case <-h.stopCh:
			return
		}
	}
}

// txBroadcastLoop announces new transactions to connected peers.
func (h *handler) txBroadcastLoop() {
	defer h.wg.Done()
	for {
		select {
		case event := <-h.txsCh:
			h.BroadcastTransactions(event.Txs)
		case <-h.txsSub.Err():
			return
		case <-h.stopCh:
			return
		}
	}
}

// txReannounceLoop announces local pending transactions to connected peers again.
func (h *handler) txReannounceLoop() {
	defer h.wg.Done()
	for {
		select {
		case event := <-h.reannoTxsCh:
			h.ReannounceTransactions(event.Txs)
		case <-h.reannoTxsSub.Err():
			return
		case <-h.stopCh:
			return
		}
	}
}

// voteBroadcastLoop announces new vote to connected peers.
func (h *handler) voteBroadcastLoop() {
	defer h.wg.Done()
	for {
		select {
		case event := <-h.voteCh:
			// The timeliness of votes is very important,
			// so one vote will be sent instantly without waiting for other votes for batch sending by design.
			h.BroadcastVote(event.Vote)
		case <-h.votesSub.Err():
			return
		}
	}
}

// enableSyncedFeatures enables the post-sync functionalities when the initial
// sync is finished.
func (h *handler) enableSyncedFeatures() {
	// Mark the local node as synced.
	h.synced.Store(true)
	if !h.acceptTxs.Load() {
		h.acceptTxs.Store(true)
		log.Info("Enable transaction acceptance when synced.")
	}

	// If we were running snap sync and it finished, disable doing another
	// round on next sync cycle
	if h.snapSync.Load() {
		log.Info("Snap sync complete, auto disabling")
		h.snapSync.Store(false)
	}
}
