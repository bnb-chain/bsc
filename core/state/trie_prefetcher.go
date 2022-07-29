// Copyright 2020 The go-ethereum Authors
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

package state

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

const (
	abortChanSize                 = 64
	concurrentChanSize            = 10
	parallelTriePrefetchThreshold = 10
	parallelTriePrefetchCapacity  = 20
)

var (
	// triePrefetchMetricsPrefix is the prefix under which to publis the metrics.
	triePrefetchMetricsPrefix = "trie/prefetch/"
)

type prefetchMsg struct {
	root        common.Hash
	accountHash common.Hash
	keys        [][]byte
}

// triePrefetcher is an active prefetcher, which receives accounts or storage
// items and does trie-loading of them. The goal is to get as much useful content
// into the caches as possible.
//
// Note, the prefetcher's API is not thread safe.
type triePrefetcher struct {
	db         Database                    // Database to fetch trie nodes through
	root       common.Hash                 // Root hash of theaccount trie for metrics
	rootParent common.Hash                 //Root has of the account trie from block before the prvious one, designed for pipecommit mode
	fetches    map[common.Hash]Trie        // Partially or fully fetcher tries
	fetchers   map[common.Hash]*subfetcher // Subfetchers for each trie

	abortChan         chan *subfetcher // to abort a single subfetcher and its children
	closed            int32
	closeMainChan     chan struct{} // it is to inform the mainLoop
	closeMainDoneChan chan struct{}
	fetchersMutex     sync.RWMutex
	prefetchChan      chan *prefetchMsg // no need to wait for return

	deliveryMissMeter metrics.Meter
	accountLoadMeter  metrics.Meter
	accountDupMeter   metrics.Meter
	accountSkipMeter  metrics.Meter
	accountWasteMeter metrics.Meter
	storageLoadMeter  metrics.Meter
	storageDupMeter   metrics.Meter
	storageSkipMeter  metrics.Meter
	storageWasteMeter metrics.Meter

	accountStaleLoadMeter  metrics.Meter
	accountStaleDupMeter   metrics.Meter
	accountStaleSkipMeter  metrics.Meter
	accountStaleWasteMeter metrics.Meter
}

// newTriePrefetcher
func newTriePrefetcher(db Database, root, rootParent common.Hash, namespace string) *triePrefetcher {
	prefix := triePrefetchMetricsPrefix + namespace
	p := &triePrefetcher{
		db:         db,
		root:       root,
		rootParent: rootParent,
		fetchers:   make(map[common.Hash]*subfetcher), // Active prefetchers use the fetchers map
		abortChan:  make(chan *subfetcher, abortChanSize),

		closeMainChan:     make(chan struct{}),
		closeMainDoneChan: make(chan struct{}),
		prefetchChan:      make(chan *prefetchMsg, concurrentChanSize),

		deliveryMissMeter: metrics.GetOrRegisterMeter(prefix+"/deliverymiss", nil),
		accountLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/account/load", nil),
		accountDupMeter:   metrics.GetOrRegisterMeter(prefix+"/account/dup", nil),
		accountSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/account/skip", nil),
		accountWasteMeter: metrics.GetOrRegisterMeter(prefix+"/account/waste", nil),
		storageLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/storage/load", nil),
		storageDupMeter:   metrics.GetOrRegisterMeter(prefix+"/storage/dup", nil),
		storageSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/storage/skip", nil),
		storageWasteMeter: metrics.GetOrRegisterMeter(prefix+"/storage/waste", nil),

		accountStaleLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/accountst/load", nil),
		accountStaleDupMeter:   metrics.GetOrRegisterMeter(prefix+"/accountst/dup", nil),
		accountStaleSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/accountst/skip", nil),
		accountStaleWasteMeter: metrics.GetOrRegisterMeter(prefix+"/accountst/waste", nil),
	}
	go p.mainLoop()
	return p
}

// the subfetcher's lifecycle will only be updated in this loop,
// include: subfetcher's creation & abort, child subfetcher's creation & abort.
// since the mainLoop will handle all the requests, each message handle should be lightweight
func (p *triePrefetcher) mainLoop() {
	for {
		select {
		case pMsg := <-p.prefetchChan:
			fetcher := p.fetchers[pMsg.root]
			if fetcher == nil {
				fetcher = newSubfetcher(p.db, pMsg.root, pMsg.accountHash)
				p.fetchersMutex.Lock()
				p.fetchers[pMsg.root] = fetcher
				p.fetchersMutex.Unlock()
			}
			select {
			case <-fetcher.stop:
			default:
				fetcher.schedule(pMsg.keys)
				// no need to run parallel trie prefetch if threshold is not reached.
				if atomic.LoadUint32(&fetcher.pendingSize) > parallelTriePrefetchThreshold {
					fetcher.scheduleParallel(pMsg.keys)
				}
			}

		case fetcher := <-p.abortChan:
			fetcher.abort()
			for _, child := range fetcher.paraChildren {
				child.abort()
			}

		case <-p.closeMainChan:
			for _, fetcher := range p.fetchers {
				fetcher.abort() // safe to do multiple times
				for _, child := range fetcher.paraChildren {
					child.abort()
				}
			}
			// make sure all subfetchers and child subfetchers are stopped
			for _, fetcher := range p.fetchers {
				<-fetcher.term
				for _, child := range fetcher.paraChildren {
					<-child.term
				}

				if metrics.EnabledExpensive {
					switch fetcher.root {
					case p.root:
						p.accountLoadMeter.Mark(int64(len(fetcher.seen)))
						p.accountDupMeter.Mark(int64(fetcher.dups))
						p.accountSkipMeter.Mark(int64(len(fetcher.tasks)))
						fetcher.lock.Lock()
						for _, key := range fetcher.used {
							delete(fetcher.seen, string(key))
						}
						fetcher.lock.Unlock()
						p.accountWasteMeter.Mark(int64(len(fetcher.seen)))

					case p.rootParent:
						p.accountStaleLoadMeter.Mark(int64(len(fetcher.seen)))
						p.accountStaleDupMeter.Mark(int64(fetcher.dups))
						p.accountStaleSkipMeter.Mark(int64(len(fetcher.tasks)))
						fetcher.lock.Lock()
						for _, key := range fetcher.used {
							delete(fetcher.seen, string(key))
						}
						fetcher.lock.Unlock()
						p.accountStaleWasteMeter.Mark(int64(len(fetcher.seen)))

					default:
						p.storageLoadMeter.Mark(int64(len(fetcher.seen)))
						p.storageDupMeter.Mark(int64(fetcher.dups))
						p.storageSkipMeter.Mark(int64(len(fetcher.tasks)))

						fetcher.lock.Lock()
						for _, key := range fetcher.used {
							delete(fetcher.seen, string(key))
						}
						fetcher.lock.Unlock()
						p.storageWasteMeter.Mark(int64(len(fetcher.seen)))

					}
				}
			}
			close(p.closeMainDoneChan)
			p.fetchersMutex.Lock()
			p.fetchers = nil
			p.fetchersMutex.Unlock()
			return
		}
	}
}

// close iterates over all the subfetchers, aborts any that were left spinning
// and reports the stats to the metrics subsystem.
func (p *triePrefetcher) close() {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}
	if atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		close(p.closeMainChan)
		<-p.closeMainDoneChan // wait until all subfetcher are stopped
	}
}

// copy creates a deep-but-inactive copy of the trie prefetcher. Any trie data
// already loaded will be copied over, but no goroutines will be started. This
// is mostly used in the miner which creates a copy of it's actively mutated
// state to be sealed while it may further mutate the state.
func (p *triePrefetcher) copy() *triePrefetcher {
	// If the prefetcher is already a copy, duplicate the data
	if p.fetches != nil {
		fetcherCopied := &triePrefetcher{
			db:      p.db,
			root:    p.root,
			fetches: make(map[common.Hash]Trie, len(p.fetches)),
		}
		// p.fetches is safe to be accessed outside of mainloop
		// if the triePrefetcher is active, fetches will not be used in mainLoop
		// otherwise, inactive triePrefetcher is readonly, it won't modify fetches
		for root, fetch := range p.fetches {
			fetcherCopied.fetches[root] = p.db.CopyTrie(fetch)
		}
		return fetcherCopied
	}

	select {
	case <-p.closeMainChan:
		// for closed trie prefetcher, fetchers is empty
		// but the fetches should not be nil, since fetches is used to check if it is a copied inactive one.
		fetcherCopied := &triePrefetcher{
			db:      p.db,
			root:    p.root,
			fetches: make(map[common.Hash]Trie),
		}
		return fetcherCopied
	default:
		p.fetchersMutex.RLock()
		fetcherCopied := &triePrefetcher{
			db:      p.db,
			root:    p.root,
			fetches: make(map[common.Hash]Trie, len(p.fetchers)),
		}
		// we're copying an active fetcher, retrieve the current states
		for root, fetcher := range p.fetchers {
			fetcherCopied.fetches[root] = fetcher.peek()
		}
		p.fetchersMutex.RUnlock()
		return fetcherCopied
	}
}

// prefetch schedules a batch of trie items to prefetch.
func (p *triePrefetcher) prefetch(root common.Hash, keys [][]byte, accountHash common.Hash) {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}
	select {
	case <-p.closeMainChan: // skip closed trie prefetcher
	case p.prefetchChan <- &prefetchMsg{root, accountHash, keys}:
	}
}

// trie returns the trie matching the root hash, or nil if the prefetcher doesn't
// have it.
func (p *triePrefetcher) trie(root common.Hash) Trie {
	// If the prefetcher is inactive, return from existing deep copies
	if p.fetches != nil {
		trie := p.fetches[root]
		if trie == nil {
			return nil
		}
		return p.db.CopyTrie(trie)
	}

	// use lock instead of request to mainLoop by chan to get the fetcher for performance concern.
	p.fetchersMutex.RLock()
	fetcher := p.fetchers[root]
	p.fetchersMutex.RUnlock()
	if fetcher == nil {
		p.deliveryMissMeter.Mark(1)
		return nil
	}

	// Interrupt the prefetcher if it's by any chance still running and return
	// a copy of any pre-loaded trie.
	select {
	case <-p.closeMainChan:
	case p.abortChan <- fetcher: // safe to abort a fecther multiple times
	}

	trie := fetcher.peek()
	if trie == nil {
		p.deliveryMissMeter.Mark(1)
		return nil
	}
	return trie
}

// used marks a batch of state items used to allow creating statistics as to
// how useful or wasteful the prefetcher is.
func (p *triePrefetcher) used(root common.Hash, used [][]byte) {
	if !metrics.EnabledExpensive {
		return
	}
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}
	select {
	case <-p.closeMainChan:
	default:
		p.fetchersMutex.RLock()
		if fetcher := p.fetchers[root]; fetcher != nil {
			fetcher.lock.Lock()
			fetcher.used = used
			fetcher.lock.Unlock()
		}
		p.fetchersMutex.RUnlock()
	}
}

// subfetcher is a trie fetcher goroutine responsible for pulling entries for a
// single trie. It is spawned when a new root is encountered and lives until the
// main prefetcher is paused and either all requested items are processed or if
// the trie being worked on is retrieved from the prefetcher.
type subfetcher struct {
	db   Database    // Database to load trie nodes through
	root common.Hash // Root hash of the trie to prefetch
	trie Trie        // Trie being populated with nodes

	tasks [][]byte   // Items queued up for retrieval
	lock  sync.Mutex // Lock protecting the task queue

	wake chan struct{}  // Wake channel if a new task is scheduled
	stop chan struct{}  // Channel to interrupt processing
	term chan struct{}  // Channel to signal iterruption
	copy chan chan Trie // Channel to request a copy of the current trie

	seen map[string]struct{} // Tracks the entries already loaded
	dups int                 // Number of duplicate preload tasks
	used [][]byte            // Tracks the entries used in the end

	accountHash common.Hash

	pendingSize  uint32
	paraChildren []*subfetcher // Parallel trie prefetch for address of massive change
}

// newSubfetcher creates a goroutine to prefetch state items belonging to a
// particular root hash.
func newSubfetcher(db Database, root common.Hash, accountHash common.Hash) *subfetcher {
	sf := &subfetcher{
		db:          db,
		root:        root,
		wake:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
		term:        make(chan struct{}),
		copy:        make(chan chan Trie),
		seen:        make(map[string]struct{}),
		accountHash: accountHash,
	}
	go sf.loop()
	return sf
}

// schedule adds a batch of trie keys to the queue to prefetch.
func (sf *subfetcher) schedule(keys [][]byte) {
	atomic.AddUint32(&sf.pendingSize, uint32(len(keys)))
	// Append the tasks to the current queue
	sf.lock.Lock()
	sf.tasks = append(sf.tasks, keys...)
	sf.lock.Unlock()
	// Notify the prefetcher, it's fine if it's already terminated
	select {
	case sf.wake <- struct{}{}:
	default:
	}
}

func (sf *subfetcher) scheduleParallel(keys [][]byte) {
	var keyIndex uint32 = 0
	childrenNum := len(sf.paraChildren)
	if childrenNum > 0 {
		// To feed the children first, if they are hungry.
		// A child can handle keys with capacity of parallelTriePrefetchCapacity.
		childIndex := len(keys) % childrenNum // randomly select the start child to avoid always feed the first one
		for i := 0; i < childrenNum; i++ {
			child := sf.paraChildren[childIndex]
			childIndex = (childIndex + 1) % childrenNum
			if atomic.LoadUint32(&child.pendingSize) >= parallelTriePrefetchCapacity {
				// the child is already full, skip it
				continue
			}
			feedNum := parallelTriePrefetchCapacity - atomic.LoadUint32(&child.pendingSize)
			if keyIndex+feedNum >= uint32(len(keys)) {
				// the new arrived keys are all consumed by children.
				child.schedule(keys[keyIndex:])
				return
			}
			child.schedule(keys[keyIndex : keyIndex+feedNum])
			keyIndex += feedNum
		}
	}
	// Children did not consume all the keys, to create new subfetch to handle left keys.
	keysLeft := keys[keyIndex:]
	keysLeftSize := len(keysLeft)
	for i := 0; i*parallelTriePrefetchCapacity < keysLeftSize; i++ {
		child := newSubfetcher(sf.db, sf.root, sf.accountHash)
		sf.paraChildren = append(sf.paraChildren, child)
		endIndex := (i + 1) * parallelTriePrefetchCapacity
		if endIndex >= keysLeftSize {
			child.schedule(keysLeft[i*parallelTriePrefetchCapacity:])
			return
		}
		child.schedule(keysLeft[i*parallelTriePrefetchCapacity : endIndex])
	}
}

// peek tries to retrieve a deep copy of the fetcher's trie in whatever form it
// is currently.
func (sf *subfetcher) peek() Trie {
	ch := make(chan Trie)
	select {
	case sf.copy <- ch:
		// Subfetcher still alive, return copy from it
		return <-ch

	case <-sf.term:
		// Subfetcher already terminated, return a copy directly
		if sf.trie == nil {
			return nil
		}
		return sf.db.CopyTrie(sf.trie)
	}
}

// abort interrupts the subfetcher immediately. It is safe to call abort multiple
// times but it is not thread safe.
func (sf *subfetcher) abort() {
	select {
	case <-sf.stop:
	default:
		close(sf.stop)
	}
	// no need to wait <-sf.term here, will check sf.term later
}

// loop waits for new tasks to be scheduled and keeps loading them until it runs
// out of tasks or its underlying trie is retrieved for committing.
func (sf *subfetcher) loop() {
	// No matter how the loop stops, signal anyone waiting that it's terminated
	defer close(sf.term)

	// Start by opening the trie and stop processing if it fails
	var trie Trie
	var err error
	if sf.accountHash == emptyAddr {
		trie, err = sf.db.OpenTrie(sf.root)
	} else {
		// address is useless
		trie, err = sf.db.OpenStorageTrie(sf.accountHash, sf.root)
	}
	if err != nil {
		log.Debug("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
		return
	}
	sf.trie = trie

	// Trie opened successfully, keep prefetching items
	for {
		select {
		case <-sf.wake:
			// Subfetcher was woken up, retrieve any tasks to avoid spinning the lock
			if sf.trie == nil {
				if sf.accountHash == emptyAddr {
					sf.trie, err = sf.db.OpenTrie(sf.root)
				} else {
					// address is useless
					sf.trie, err = sf.db.OpenStorageTrie(sf.accountHash, sf.root)
				}
				if err != nil {
					continue
				}
			}

			sf.lock.Lock()
			tasks := sf.tasks
			sf.tasks = nil
			sf.lock.Unlock()

			// Prefetch any tasks until the loop is interrupted
			for i, task := range tasks {
				select {
				case <-sf.stop:
					// If termination is requested, add any leftover back and return
					sf.lock.Lock()
					sf.tasks = append(sf.tasks, tasks[i:]...)
					sf.lock.Unlock()
					return

				case ch := <-sf.copy:
					// Somebody wants a copy of the current trie, grant them
					ch <- sf.db.CopyTrie(sf.trie)

				default:
					// No termination request yet, prefetch the next entry
					if _, ok := sf.seen[string(task)]; ok {
						sf.dups++
					} else {
						sf.trie.TryGet(task)
						sf.seen[string(task)] = struct{}{}
					}
					atomic.AddUint32(&sf.pendingSize, ^uint32(0)) // decrease
				}
			}

		case ch := <-sf.copy:
			// Somebody wants a copy of the current trie, grant them
			ch <- sf.db.CopyTrie(sf.trie)

		case <-sf.stop:
			// Termination is requested, abort and leave remaining tasks
			return
		}
	}
}
