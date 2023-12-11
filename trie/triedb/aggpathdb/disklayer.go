// Copyright 2022 The go-ethereum Authors
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

package aggpathdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
	"golang.org/x/crypto/sha3"
	"golang.org/x/sync/semaphore"
)

// diskLayer is a low level persistent layer built on top of a key-value store.
type diskLayer struct {
	root            common.Hash    // Immutable, root hash to which this layer was made for
	id              uint64         // Immutable, corresponding state id
	db              *Database      // Agg-Path-based trie database
	cleans          *aggNodeCache  // GC friendly memory cache of clean agg node RLPs
	buffer          *aggNodeBuffer // Agg node buffer to aggregate writes
	immutableBuffer *aggNodeBuffer // Agg node buffer to aggregate writes
	stale           bool           // Signals that the layer became stale (state progressed)
	lock            sync.RWMutex   // Lock used to protect stale flag
}

// newDiskLayer creates a new disk layer based on the passing arguments.
func newDiskLayer(root common.Hash, id uint64, db *Database, cleans *aggNodeCache, buffer *aggNodeBuffer, immutableBuffer *aggNodeBuffer) *diskLayer {
	// Initialize a clean cache if the memory allowance is not zero
	// or reuse the provided cache if it is not nil (inherited from
	// the original disk layer).
	if cleans == nil && db.config.CleanCacheSize != 0 {
		cleans = newAggNodeCache(db, nil, db.config.CleanCacheSize)
	}
	return &diskLayer{
		root:            root,
		id:              id,
		db:              db,
		cleans:          cleans,
		buffer:          buffer,
		immutableBuffer: immutableBuffer,
	}
}

// root implements the layer interface, returning root hash of corresponding state.
func (dl *diskLayer) rootHash() common.Hash {
	return dl.root
}

// stateID implements the layer interface, returning the state id of disk layer.
func (dl *diskLayer) stateID() uint64 {
	return dl.id
}

// parent implements the layer interface, returning nil as there's no layer
// below the disk.
func (dl *diskLayer) parentLayer() layer {
	return nil
}

// isStale return whether this layer has become stale (was flattened across) or if
// it's still live.
func (dl *diskLayer) isStale() bool {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	return dl.stale
}

// markStale sets the stale flag as true.
func (dl *diskLayer) markStale() {
	dl.lock.Lock()
	defer dl.lock.Unlock()

	if dl.stale {
		panic("triedb disk layer is stale") // we've committed into the same base from two children, boom
	}
	dl.stale = true
}

// Node implements the layer interface, retrieving the trie node with the
// provided node info. No error will be returned if the node is not found.
func (dl *diskLayer) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return nil, errSnapshotStale
	}
	// Try to retrieve the trie node from the not-yet-written
	// node buffer first. Note the buffer is lock free since
	// it's impossible to mutate the buffer before tagging the
	// layer as stale.
	n, err := dl.buffer.node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if n != nil {
		dirtyHitMeter.Mark(1)
		dirtyReadMeter.Mark(int64(len(n.Blob)))
		return n.Blob, nil
	}

	n, err = dl.immutableBuffer.node(owner, path, hash)
	if err != nil {
		dirtyHitMeter.Mark(1)
		dirtyReadMeter.Mark(int64(len(n.Blob)))
		return nil, err
	}

	if n != nil {
		return n.Blob, nil
	}
	dirtyMissMeter.Mark(1)

	// Try to retrieve the trie node from the agg node cache.
	blob, err := dl.cleans.node(owner, path, hash)
	if err != nil {
		return nil, err
	}

	return blob, nil
}

// update implements the layer interface, returning a new diff layer on top
// with the given state set.
func (dl *diskLayer) update(root common.Hash, id uint64, block uint64, nodes map[common.Hash]map[string]*trienode.Node, states *triestate.Set) *diffLayer {
	return newDiffLayer(dl, root, id, block, nodes, states)
}

// commit merges the given bottom-most diff layer into the node buffer
// and returns a newly constructed disk layer. Note the current disk
// layer must be tagged as stale first to prevent re-access.
func (dl *diskLayer) commit(bottom *diffLayer, force bool) (*diskLayer, error) {
	dl.lock.Lock()
	defer dl.lock.Unlock()

	start := time.Now()
	defer commitTimeTimer.UpdateSince(start)

	// Construct and store the state history first. If crash happens
	// after storing the state history but without flushing the
	// corresponding states(journal), the stored state history will
	// be truncated in the next restart.
	var (
		overflow bool
		oldest   uint64
	)
	if dl.db.freezer != nil {
		err := writeHistory(dl.db.freezer, bottom)
		if err != nil {
			return nil, err
		}
		// Determine if the persisted history object has exceeded the configured
		// limitation, set the overflow as true if so.
		tail, err := dl.db.freezer.Tail()
		if err != nil {
			return nil, err
		}
		limit := dl.db.config.StateHistory
		if limit != 0 && bottom.stateID()-tail > limit {
			overflow = true
			oldest = bottom.stateID() - limit + 1 // track the id of history **after truncation**
		}
	}
	commitWriteHistoryTimeTimer.UpdateSince(start)
	// Mark the diskLayer as stale before applying any mutations on top.
	dl.stale = true

	// Store the root->id lookup afterwards. All stored lookups are
	// identified by the **unique** state root. It's impossible that
	// in the same chain blocks are not adjacent but have the same
	// root.
	batch := dl.db.diskdb.NewBatch()
	if dl.id == 0 {
		rawdb.WriteStateID(batch, dl.root, 0)
	}
	rawdb.WriteStateID(batch, bottom.rootHash(), bottom.stateID())
	commitWriteStateIDTimeTimer.UpdateSince(start)

	// In a unique scenario where the ID of the oldest history object (after tail
	// truncation) surpasses the persisted state ID, we take the necessary action
	// of forcibly committing the cached dirty nodes to ensure that the persisted
	// state ID remains higher.
	if !force && rawdb.ReadPersistentStateID(dl.db.diskdb) < oldest {
		force = true
	}
	// Construct a new disk layer by merging the aggNodes from the provided
	// diff layer, and flush the content in disk layer if there are too
	// many aggNodes cached. The clean cache is inherited from the original
	// disk layer for reusing.
	var ndl *diskLayer
	dl.commitNodesV2(bottom.aggnodes)
	commitCommitNodesTimeTimer.UpdateSince(start)

	// To remove outdated history objects from the end, we set the 'tail' parameter
	// to 'oldest-1' due to the offset between the freezer index and the history ID.
	if overflow {
		pruned, err := truncateFromTail(batch, dl.db.freezer, oldest-1)
		if err != nil {
			return nil, err
		}
		log.Debug("Pruned state history", "items", pruned, "tailid", oldest)
	}
	commitTruncateHistoryTimer.UpdateSince(start)

	if dl.buffer.canFlush(force) {
		for {
			exit := false
			select {
			case err := <-dl.immutableBuffer.flushResult:
				// immutable buffer flush completed, get the result
				if err != nil {
					log.Error("Immutable buffer flush failed", "error", err)
					return nil, err
				} else {
					exit = true
				}
			default:
				if dl.immutableBuffer.empty() {
					exit = true
				} else {
					// wait until the immutable buffer flush completely.
					time.Sleep(5 * time.Microsecond)
				}
			}
			if exit {
				break
			}
		}

		ndl = newDiskLayer(bottom.root, bottom.stateID(), dl.db, dl.cleans, dl.immutableBuffer, dl.buffer)
		if force {
			err := ndl.immutableBuffer.flush(ndl.db.diskdb, batch, ndl.cleans, ndl.id)
			if err != nil {
				return nil, err
			}
		} else {
			go func() {
				err := ndl.immutableBuffer.flush(ndl.db.diskdb, batch, ndl.cleans, ndl.id)
				ndl.immutableBuffer.flushResult <- err
			}()
		}
	} else {
		go func() {
			err := batch.Write()
			if err != nil {
				panic(err)
			}
			batch.Reset()
		}()
		ndl = newDiskLayer(bottom.root, bottom.stateID(), dl.db, dl.cleans, dl.buffer, dl.immutableBuffer)
	}

	commitFlushTimer.UpdateSince(start)
	return ndl, nil
}

// revert applies the given state history and return a reverted disk layer.
func (dl *diskLayer) revert(h *history, loader triestate.TrieLoader) (*diskLayer, error) {
	if h.meta.root != dl.rootHash() {
		return nil, errUnexpectedHistory
	}
	// Reject if the provided state history is incomplete. It's due to
	// a large construct SELF-DESTRUCT which can't be handled because
	// of memory limitation.
	if len(h.meta.incomplete) > 0 {
		return nil, errors.New("incomplete state history")
	}
	if dl.id == 0 {
		return nil, fmt.Errorf("%w: zero state id", errStateUnrecoverable)
	}
	// Apply the reverse state changes upon the current state. This must
	// be done before holding the lock in order to access state in "this"
	// layer.
	nodes, err := triestate.Apply(h.meta.parent, h.meta.root, h.accounts, h.storages, loader)
	if err != nil {
		return nil, err
	}
	// Mark the diskLayer as stale before applying any mutations on top.
	dl.lock.Lock()
	defer dl.lock.Unlock()

	dl.stale = true

	// State change may be applied to node buffer, or the persistent
	// state, depends on if node buffer is empty or not. If the node
	// buffer is not empty, it means that the state transition that
	// needs to be reverted is not yet flushed and cached in node
	// buffer, otherwise, manipulate persistent state directly.
	if !dl.buffer.empty() {
		dl.commitNodes(nodes)
		err := dl.buffer.revert(dl.db.diskdb, dl.buffer.aggNodes)
		if err != nil {
			return nil, err
		}
	} else {
		batch := dl.db.diskdb.NewBatch()
		dl.commitNodes(nodes)
		writeAggNodes(dl.cleans, batch, dl.buffer.aggNodes)
		rawdb.WritePersistentStateID(batch, dl.id-1)
		if err := batch.Write(); err != nil {
			log.Crit("Failed to write states", "err", err)
		}
	}
	return newDiskLayer(h.meta.parent, dl.id-1, dl.db, dl.cleans, dl.buffer, dl.immutableBuffer), nil
}

func (dl *diskLayer) commitNodesV2(nodes map[common.Hash]map[string]*AggNode) {
	type subTree struct {
		owner         common.Hash
		aggPath       string
		aggNode       *AggNode
		delta         int64
		overwrite     int64
		overwriteSize int64
	}
	var (
		mx            sync.RWMutex // local concurrent mux
		stopCh        = make(chan struct{})
		mergeSubResCh = make(chan *subTree)
		wg            sync.WaitGroup
		subTreeWG     sync.WaitGroup

		sem = semaphore.NewWeighted(int64(2048))
		ctx = context.Background()
	)
	go func() {
		for {
			select {
			case res := <-mergeSubResCh:
				if res.aggNode != nil {
					mx.Lock()
					if _, ok := dl.buffer.aggNodes[res.owner]; !ok {
						dl.buffer.aggNodes[res.owner] = make(map[string]*AggNode)
					}
					dl.buffer.aggNodes[res.owner][res.aggPath] = res.aggNode
					mx.Unlock()
				}
				dl.buffer.updateSize(res.delta)
				gcNodesMeter.Mark(res.overwrite)
				gcBytesMeter.Mark(res.overwriteSize)
				wg.Done()
			case <-stopCh:
				return
			}
		}
	}()
	subTreeWG.Add(len(nodes))
	for owner, subset := range nodes {
		o := owner
		ss := subset
		go func(owner common.Hash, ss map[string]*AggNode) {
			wg.Add(len(ss))
			for aggPath, aggSubset := range ss {
				ap := aggPath
				delta := aggSubset
				go func(account common.Hash, aggPath string, delta *AggNode) {
					_ = sem.Acquire(ctx, 1)
					defer sem.Release(1)
					subRes := &subTree{owner: account, aggPath: aggPath, aggNode: nil}
					mx.RLock()
					aggNode := dl.buffer.aggNode(account, []byte(aggPath))
					mx.RUnlock()
					exit := true
					if aggNode == nil {
						exit = false
						var err error
						immutableAggNode := dl.immutableBuffer.aggNode(account, []byte(aggPath))
						if immutableAggNode == nil {
							aggNode, err = dl.cleans.aggNode(account, []byte(aggPath))
							if err != nil {
								panic(fmt.Sprintf("decode agg node failed from clean cache, err: %v", err))
							}
							if aggNode == nil {
								aggNode = &AggNode{}
							}
						} else {
							aggNode, err = immutableAggNode.copy()
							if err != nil {
								panic(fmt.Sprintf("decode agg node failed from immutable buffer, err: %v", err))
							}
						}
						subRes.aggNode = aggNode
					}
					pathSize := 0
					oldSize := aggNode.Size()

					aggNode.Merge(delta)
					newSize := aggNode.Size()
					if exit {
						subRes.overwrite++
						subRes.overwriteSize += int64(newSize - oldSize + pathSize + len(account))
						subRes.delta += int64(newSize - oldSize)
					} else {
						subRes.delta += int64(newSize + pathSize + len(account))
					}
					mergeSubResCh <- subRes
				}(o, ap, delta)
			}
			subTreeWG.Done()
		}(o, ss)
	}

	subTreeWG.Wait()
	wg.Wait()
	close(stopCh)
	dl.buffer.layers++
}

func (dl *diskLayer) commitNodes(nodes map[common.Hash]map[string]*trienode.Node) {
	start := time.Now()
	// owner -> aggPath -> path -> trienode
	aggTrie := make(map[common.Hash]map[string]map[string]*trienode.Node)
	for owner, subset := range nodes {
		if _, ok := aggTrie[owner]; !ok {
			aggTrie[owner] = make(map[string]map[string]*trienode.Node)
		}
		for path, node := range subset {
			aggPath := ToAggPath([]byte(path))
			if _, ok := aggTrie[owner][string(aggPath)]; !ok {
				aggTrie[owner][string(aggPath)] = make(map[string]*trienode.Node)
			}
			aggTrie[owner][string(aggPath)][path] = node
		}
	}
	commitNodesPart1Timer.UpdateSince(start)

	wgCounter := 0
	for _, subset := range aggTrie {
		wgCounter += len(subset)
	}

	type subTree struct {
		owner         common.Hash
		aggPath       string
		aggNode       *AggNode
		delta         int64
		overwrite     int64
		overwriteSize int64
	}
	var (
		mx            sync.RWMutex // local concurrent mux
		wg            sync.WaitGroup
		stopCh        = make(chan struct{})
		mergeSubResCh = make(chan *subTree, wgCounter)

		sem = semaphore.NewWeighted(int64(1024))
		ctx = context.Background()
	)
	wg.Add(wgCounter)
	go func() {
		for {
			select {
			case res := <-mergeSubResCh:
				if res.aggNode != nil {
					mx.Lock()
					if _, ok := dl.buffer.aggNodes[res.owner]; !ok {
						dl.buffer.aggNodes[res.owner] = make(map[string]*AggNode)
					}
					dl.buffer.aggNodes[res.owner][res.aggPath] = res.aggNode
					mx.Unlock()
				}
				dl.buffer.updateSize(res.delta)
				gcNodesMeter.Mark(res.overwrite)
				gcBytesMeter.Mark(res.overwriteSize)
				wg.Done()
			case <-stopCh:
				return
			}
		}
	}()
	start3 := time.Now()
	for owner, aggSubset := range aggTrie {
		o := owner
		for aPath, trieNodesSet := range aggSubset {
			ap := aPath
			trieSet := trieNodesSet
			go func(account common.Hash, aggPath string, trieNodes map[string]*trienode.Node) {
				start2 := time.Now()
				_ = sem.Acquire(ctx, 1)
				defer sem.Release(1)
				subRes := &subTree{owner: account, aggPath: aggPath, aggNode: nil}
				mx.RLock()
				aggNode := dl.buffer.aggNode(account, []byte(aggPath))
				mx.RUnlock()
				exit := true
				if aggNode == nil {
					exit = false
					var err error
					immutableAggNode := dl.immutableBuffer.aggNode(account, []byte(aggPath))
					if immutableAggNode == nil {
						aggNode, err = dl.cleans.aggNode(account, []byte(aggPath))
						if err != nil {
							panic(fmt.Sprintf("decode agg node failed from clean cache, err: %v", err))
						}
						if aggNode == nil {
							aggNode = &AggNode{}
						}
					} else {
						aggNode, err = immutableAggNode.copy()
						if err != nil {
							panic(fmt.Sprintf("decode agg node failed from immutable buffer, err: %v", err))
						}
					}
					subRes.aggNode = aggNode
				}
				pathSize := 0
				oldSize := aggNode.Size()
				for path, n := range trieNodes {
					pathSize += len(path)
					if n.IsDeleted() {
						aggNode.Delete([]byte(path))
					} else {
						aggNode.Update([]byte(path), n)
					}
				}
				newSize := aggNode.Size()
				if exit {
					subRes.overwrite++
					subRes.overwriteSize += int64(newSize - oldSize + pathSize + len(account))
					subRes.delta += int64(newSize - oldSize)
				} else {
					subRes.delta += int64(newSize + pathSize + len(account))
				}
				mergeSubResCh <- subRes
				commitNodesPart2Timer.UpdateSince(start2)
			}(o, ap, trieSet)
		}
	}
	commitNodesPart3Timer.UpdateSince(start3)
	wg.Wait()
	close(stopCh)
	dl.buffer.layers++

	//var (
	//	delta         int64
	//	overwrite     int64
	//	overwriteSize int64
	//)
	//for owner, subset := range nodes {
	//	current, exist := dl.buffer.aggNodes[owner]
	//	if !exist {
	//		// Allocate a new map for the subset instead of claiming it directly
	//		// from the passed map to avoid potential concurrent map read/write.
	//		// The nodes belong to original diff layer are still accessible even
	//		// after merging, thus the ownership of nodes map should still belong
	//		// to original layer and any mutation on it should be prevented.
	//		current = make(map[string]*AggNode)
	//	}
	//	for path, n := range subset {
	//		aggPath := ToAggPath([]byte(path))
	//		// retrieve aggNode from dirty buffer
	//		aggNode, ok := current[string(aggPath)]
	//		if aggNode == nil {
	//			var err error
	//			immutableAggNode := dl.immutableBuffer.aggNode(owner, aggPath)
	//			if immutableAggNode == nil {
	//				// retrieve aggNode from clean cache and disk
	//				aggNode, err = dl.cleans.aggNode(owner, aggPath)
	//				if err != nil {
	//					panic(fmt.Sprintf("decode agg node failed from clean cache, err: %v", err))
	//				}
	//				// if immutable buffer and cache missing, create a new aggNode
	//				if aggNode == nil {
	//					aggNode = &AggNode{}
	//				}
	//			} else {
	//				aggNode, err = immutableAggNode.copy()
	//				if err != nil {
	//					panic(fmt.Sprintf("decode agg node failed from immutable buffer, err: %v", err))
	//				}
	//			}
	//		}
	//		oldSize := aggNode.Size()
	//		if n.IsDeleted() {
	//			aggNode.Delete([]byte(path))
	//		} else {
	//			aggNode.Update([]byte(path), n)
	//		}
	//		newSize := aggNode.Size()
	//		if ok {
	//			overwrite++
	//			overwriteSize += int64(newSize - oldSize + len(path) + len(owner))
	//			delta += int64(newSize - oldSize)
	//		} else {
	//			delta += int64(newSize + len(path) + len(owner))
	//		}
	//		current[string(aggPath)] = aggNode
	//		dl.buffer.aggNodes[owner] = current
	//	}
	//}
	//dl.buffer.updateSize(delta)
	//dl.buffer.layers++
	//gcNodesMeter.Mark(overwrite)
	//gcBytesMeter.Mark(overwriteSize)
}

// setBufferSize sets the node buffer size to the provided value.
func (dl *diskLayer) setBufferSize(size int) error {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return errSnapshotStale
	}
	return dl.buffer.setSize(size, dl.db.diskdb, dl.cleans, dl.id)
}

// size returns the approximate size of cached aggNodes in the disk layer.
func (dl *diskLayer) size() (common.StorageSize, common.StorageSize) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return 0, 0
	}
	return common.StorageSize(dl.buffer.size), common.StorageSize(dl.immutableBuffer.size)
}

// resetCache releases the memory held by clean cache to prevent memory leak.
func (dl *diskLayer) resetCache() {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// Stale disk layer loses the ownership of clean cache.
	if dl.stale {
		return
	}
	if dl.cleans != nil {
		dl.cleans.Reset()
	}
}

// hasher is used to compute the sha256 hash of the provided data.
type hasher struct{ sha crypto.KeccakState }

var hasherPool = sync.Pool{
	New: func() interface{} { return &hasher{sha: sha3.NewLegacyKeccak256().(crypto.KeccakState)} },
}

func newHasher() *hasher {
	return hasherPool.Get().(*hasher)
}

func (h *hasher) hash(data []byte) common.Hash {
	return crypto.HashData(h.sha, data)
}

func (h *hasher) release() {
	hasherPool.Put(h)
}
