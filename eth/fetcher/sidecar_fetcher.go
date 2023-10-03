package fetcher

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"time"
)

var (
	sidecarAnnounceInMeter   = metrics.NewRegisteredMeter("eth/fetcher/sidecar/announces/in", nil)
	sidecarAnnounceOutTimer  = metrics.NewRegisteredTimer("eth/fetcher/sidecar/announces/out", nil)
	sidecarAnnounceDropMeter = metrics.NewRegisteredMeter("eth/fetcher/sidecar/announces/drop", nil)
	sidecarAnnounceDOSMeter  = metrics.NewRegisteredMeter("eth/fetcher/sidecar/announces/dos", nil)

	sidecarBroadcastInMeter   = metrics.NewRegisteredMeter("eth/fetcher/sidecar/broadcasts/in", nil)
	sidecarBroadcastOutTimer  = metrics.NewRegisteredTimer("eth/fetcher/sidecar/broadcasts/out", nil)
	sidecarBroadcastDropMeter = metrics.NewRegisteredMeter("eth/fetcher/sidecar/broadcasts/drop", nil)
	sidecarBroadcastDOSMeter  = metrics.NewRegisteredMeter("eth/fetcher/sidecar/broadcasts/dos", nil)

	//headerFetchMeter = metrics.NewRegisteredMeter("eth/fetcher/block/headers", nil)
	//bodyFetchMeter   = metrics.NewRegisteredMeter("eth/fetcher/block/bodies", nil)

	//headerFilterInMeter  = metrics.NewRegisteredMeter("eth/fetcher/block/filter/headers/in", nil)
	//headerFilterOutMeter = metrics.NewRegisteredMeter("eth/fetcher/block/filter/headers/out", nil)
	//bodyFilterInMeter    = metrics.NewRegisteredMeter("eth/fetcher/block/filter/bodies/in", nil)
	//bodyFilterOutMeter   = metrics.NewRegisteredMeter("eth/fetcher/block/filter/bodies/out", nil)
)

// sidecarRetrievalFn is a callback type for retrieving a sidecar from the local database.
type sidecarRetrievalFn func(common.Hash) *types.Sidecar

// sidecarVerifierFn is a callback type to verify a sidecar for propagation.
type sidecarVerifierFn func(sidecar *types.Sidecar) error

// sidecarBroadcasterFn is a callback type for broadcasting a sidecar to connected peers.
type sidecarBroadcasterFn func(block *types.Sidecar, propagate bool)

// sidecarsInsertFn is a callback type to insert a batch of sidecars into the local database.
type sidecarsInsertFn func(types.Sidecars) (int, error)

// sidecarAnnounce is the hash notification of the availability of a new block in the
// network.
type sidecarAnnounce struct {
	hash common.Hash // Hash of the sidecar being announced, s.SidecarToHash()

	time time.Time // Timestamp of the announcement

	origin string // Identifier of the peer originating the notification

	fetchHeader headerRequesterFn // Fetcher function to retrieve the header of an announced block
	fetchBodies bodyRequesterFn   // Fetcher function to retrieve the body of an announced block
	fetchDiffs  DiffRequesterFn   // Fetcher function to retrieve the diff layer of an announced block

}

type SidecarFetcher struct {
	light bool // The indicator whether it's a light fetcher or normal one.

	// Various event channels
	inject chan *sidecarInject

	headerFilter chan chan *headerFilterTask
	bodyFilter   chan chan *bodyFilterTask

	done chan common.Hash // sidecar hash is delivered here when its importing is done
	quit chan struct{}

	requeue chan *sidecarInject

	// Announce states
	announces  map[string]int                     // Per peer blockAnnounce counts to prevent memory exhaustion
	announced  map[common.Hash][]*sidecarAnnounce // Announced sidecars, scheduled for fetching
	fetching   map[common.Hash]*sidecarAnnounce   // Announced sidecars, currently fetching
	fetched    map[common.Hash][]*sidecarAnnounce // Sidecars with headers fetched, scheduled for body retrieval
	completing map[common.Hash]*sidecarAnnounce   // Sidecars with headers, currently body-completing

	// Block cache
	queue  *prque.Prque                   // Queue containing the import operations (block number sorted)
	queues map[string]int                 // Per peer block counts to prevent memory exhaustion
	queued map[common.Hash]*sidecarInject // Set of already queued blocks (to dedup imports)

	// Callbacks
	//getSidecar       sidecarRetrievalFn // Retrieves a sidecar from the local chain, this is probably not needed
	getSidecar       sidecarRetrievalFn
	verifySidecar    sidecarVerifierFn    // Checks if a block's headers have a valid proof of work
	broadcastSidecar sidecarBroadcasterFn // Broadcasts a block to connected peers
	chainHeight      chainHeightFn        // Retrieves the current chain's height
	insertSidecars   sidecarsInsertFn     // Injects a batch of blocks into the chain
	dropPeer         peerDropFn           // Drops a peer for misbehaving

	// Testing hooks
	announceChangeHook func(common.Hash, bool)           // Method to call upon adding or deleting a hash from the blockAnnounce list
	queueChangeHook    func(common.Hash, bool)           // Method to call upon adding or deleting a block from the import queue
	fetchingHook       func([]common.Hash)               // Method to call upon starting a sidecar (eth/61) or header (eth/62) fetch
	completingHook     func([]common.Hash)               // Method to call upon starting a sidecar body fetch (eth/62)
	importedHook       func(*types.Header, *types.Block) // Method to call upon successful sidecar import (both eth/61 and eth/62)

}

// todo 4844 write Start() and Stop() functions for SidecarFetcher
// NewSidecarFetcher creates a sidecar fetcher to retrieve sidecars based on hash announcements.
func NewSidecarFetcher(light bool, getSidecar sidecarRetrievalFn, broadcastSidecar sidecarBroadcasterFn, chainHeight chainHeightFn, insertSidecar sidecarsInsertFn, dropPeer peerDropFn) *SidecarFetcher {
	return &SidecarFetcher{
		light:            light,
		inject:           make(chan *sidecarInject),
		headerFilter:     make(chan chan *headerFilterTask),
		bodyFilter:       make(chan chan *bodyFilterTask),
		done:             make(chan common.Hash),
		quit:             make(chan struct{}),
		requeue:          make(chan *sidecarInject),
		announces:        make(map[string]int),
		announced:        make(map[common.Hash][]*sidecarAnnounce),
		fetching:         make(map[common.Hash]*sidecarAnnounce),
		fetched:          make(map[common.Hash][]*sidecarAnnounce),
		completing:       make(map[common.Hash]*sidecarAnnounce),
		queue:            prque.New(nil),
		queues:           make(map[string]int),
		queued:           make(map[common.Hash]*sidecarInject),
		getSidecar:       getSidecar,
		broadcastSidecar: broadcastSidecar,
		chainHeight:      chainHeight,
		insertSidecars:   insertSidecar,
		dropPeer:         dropPeer,
	}
}

type sidecarInject struct {
	origin string

	sidecar *types.Sidecar
}

// Start boots up the announcement based synchroniser, accepting and processing
// hash notifications and sidecar fetches until termination requested.
func (f *SidecarFetcher) Start() {
	go f.loop()
}

// Stop terminates the announcement based synchroniser, canceling all pending
// operations.
func (f *SidecarFetcher) Stop() {
	close(f.quit)
}

// Loop is the main fetcher loop, checking and processing various notification
// events.
func (f *SidecarFetcher) loop() {
	// Iterate the block fetching until a quit is requested
	var (
		fetchTimer    = time.NewTimer(0)
		completeTimer = time.NewTimer(0)
	)
	<-fetchTimer.C // clear out the channel
	<-completeTimer.C
	defer fetchTimer.Stop()
	defer completeTimer.Stop()

	for {
		// Clean up any expired block fetches
		for hash, announce := range f.fetching {
			if time.Since(announce.time) > fetchTimeout {
				f.forgetHash(hash)
			}
		}
		// Import any queued blocks that could potentially fit
		height := f.chainHeight()
		for !f.queue.Empty() {
			op := f.queue.PopItem().(*sidecarInject)
			hash := op.sidecar.SidecarToHash()
			if f.queueChangeHook != nil {
				f.queueChangeHook(hash, false)
			}
			// If too high up the chain or phase, continue later
			number := op.sidecar.Index
			if number > height+1 {
				f.queue.Push(op, -int64(number))
				if f.queueChangeHook != nil {
					f.queueChangeHook(hash, true)
				}
				break
			}

			if !f.light {
				f.importSidecars(op)
			}
		}
		// Wait for an outside event to occur
		select {
		case <-f.quit:
			// BlockFetcher terminating, abort all operations
			return
		case op := <-f.requeue:
			// Re-queue blocks that have not been written due to fork block competition
			log.Info("Re-queue blocks", "block number", op.sidecar.Index, "sidecar hash", op.sidecar.SidecarToHash())
			f.enqueue(op.origin, op.sidecar)

		case op := <-f.inject:
			// A direct block insertion was requested, try and fill any pending gaps
			blockBroadcastInMeter.Mark(1)

			// Now only direct block injection is allowed, drop the header injection
			// here silently if we receive.
			if f.light {
				continue
			}
			f.enqueue(op.origin, op.sidecar)

		case hash := <-f.done:
			// A pending import finished, remove all traces of the notification
			f.forgetHash(hash)
			f.forgetSidecar(hash)
		}
	}
}

// forgetHash removes all traces of a sidecar announcement from the sidecar fetcher's
// internal state.
func (f *SidecarFetcher) forgetHash(hash common.Hash) {
	// Remove all pending announces and decrement DOS counters
	if announceMap, ok := f.announced[hash]; ok {
		for _, announce := range announceMap {
			f.announces[announce.origin]--
			if f.announces[announce.origin] <= 0 {
				delete(f.announces, announce.origin)
			}
		}
		delete(f.announced, hash)
		if f.announceChangeHook != nil {
			f.announceChangeHook(hash, false)
		}
	}
	// Remove any pending fetches and decrement the DOS counters
	if announce := f.fetching[hash]; announce != nil {
		f.announces[announce.origin]--
		if f.announces[announce.origin] <= 0 {
			delete(f.announces, announce.origin)
		}
		delete(f.fetching, hash)
	}

	// Remove any pending completion requests and decrement the DOS counters
	for _, announce := range f.fetched[hash] {
		f.announces[announce.origin]--
		if f.announces[announce.origin] <= 0 {
			delete(f.announces, announce.origin)
		}
	}
	delete(f.fetched, hash)

	// Remove any pending completions and decrement the DOS counters
	if announce := f.completing[hash]; announce != nil {
		f.announces[announce.origin]--
		if f.announces[announce.origin] <= 0 {
			delete(f.announces, announce.origin)
		}
		delete(f.completing, hash)
	}
}

// importSidecars spawns a new goroutine to run a sidecar insertion into the database.
func (f *SidecarFetcher) importSidecars(op *sidecarInject) {
	peer := op.origin
	sidecar := op.sidecar
	hash := sidecar.SidecarToHash()

	// Run the import on a new thread
	log.Debug("Importing propagated sidecar", "peer", peer, "block number", sidecar.Index, "hash", hash)
	go func() {

		defer func() { f.done <- hash }()
		// Validate the sidecar and propagate the block if it passes
		switch err := f.validateSidecar(op); err {
		case nil:
			// All ok, quickly propagate to our peers
			sidecarBroadcastOutTimer.UpdateSince(sidecar.ReceivedAt)
			go f.broadcastSidecar(sidecar, true)

		case consensus.ErrFutureBlock:
			log.Error("Received future block", "peer", peer, "number", sidecar.Index, "hash", hash, "err", err)
			f.dropPeer(peer)

		default:
			// Something went very wrong, drop the peer
			log.Error("Propagated block verification failed", "peer", peer, "number", sidecar.Index, "hash", hash, "err", err)
			f.dropPeer(peer)
			return
		}

		// Run the actual import and log any issues
		if _, err := f.insertSidecars(types.Sidecars{sidecar}); err != nil {
			log.Debug("Propagated block import failed", "peer", peer, "number", sidecar.Index, "hash", hash, "err", err)
			return
		}
		// If import succeeded, broadcast the block
		blockAnnounceOutTimer.UpdateSince(sidecar.ReceivedAt)
		go f.broadcastSidecar(sidecar, false)

		// todo 4844 check if the below code is needed
		//// Invoke the testing hook if needed
		//if f.importedHook != nil {
		//	f.importedHook(nil, sidecar)
		//}
	}()
}

func (f *SidecarFetcher) validateSidecar(op *sidecarInject) error {
	panic("Imeplement validateSidecar")
}

// enqueue schedules a new sidecar import operation, if the component
// to be imported has not yet been seen.
func (f *SidecarFetcher) enqueue(peer string, sidecar *types.Sidecar) {
	fmt.Println("Inside sidecar enqueue...")
	var (
		hash   common.Hash
		number uint64
	)

	hash, number = sidecar.SidecarToHash(), sidecar.Index

	// Ensure the peer isn't DOSing us
	count := f.queues[peer] + 1
	if count > blockLimit {
		log.Debug("Discarded delivered header or sidecar, exceeded allowance", "peer", peer, "number", number, "hash", hash, "limit", blockLimit)
		blockBroadcastDOSMeter.Mark(1)
		f.forgetHash(hash)
		return
	}
	// Discard any past or too distant sidecars
	if dist := int64(number) - int64(f.chainHeight()); dist < -maxUncleDist || dist > maxQueueDist {
		log.Debug("Discarded delivered sidecar, too far away", "peer", peer, "number", number, "hash", hash, "distance", dist)
		sidecarBroadcastDropMeter.Mark(1)
		f.forgetHash(hash)
		return
	}
	// Schedule the sidecar for future importing
	if _, ok := f.queued[hash]; !ok {
		op := &sidecarInject{origin: peer}
		op.sidecar = sidecar

		f.queues[peer] = count
		f.queued[hash] = op
		f.queue.Push(op, -int64(number))
		if f.queueChangeHook != nil {
			f.queueChangeHook(hash, true)
		}
		log.Debug("Queued delivered sidecar", "peer", peer, "number", number, "hash", hash, "queued", f.queue.Size())
	}
}

// forgetSidecar removes all traces of a queued sidecar from the fetcher's internal
// state.
func (f *SidecarFetcher) forgetSidecar(hash common.Hash) {
	if insert := f.queued[hash]; insert != nil {
		f.queues[insert.origin]--
		if f.queues[insert.origin] == 0 {
			delete(f.queues, insert.origin)
		}
		delete(f.queued, hash)
	}
}

// Enqueue tries to fill gaps the fetcher's future import queue.
func (f *SidecarFetcher) Enqueue(peer string, sidecar *types.Sidecar) error {
	op := &sidecarInject{
		origin:  peer,
		sidecar: sidecar,
	}
	select {
	case f.inject <- op:
		return nil
	case <-f.quit:
		return errTerminated
	}
}
