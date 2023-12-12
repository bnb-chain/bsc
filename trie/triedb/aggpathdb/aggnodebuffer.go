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
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// aggNodeBuffer is a collection of modified agg aggNodes to aggregate the disk
// write. The content of the aggNodeBuffer must be checked before diving into
// disk (since it basically is not-yet-written data).
type aggNodeBuffer struct {
	layers      uint64                              // The number of diff layers aggregated inside
	size        uint64                              // The size of aggregated writes
	limit       uint64                              // The maximum memory allowance in bytes
	aggNodes    map[common.Hash]map[string]*AggNode // The dirty node set, mapped by owner, aggpath and path
	flushResult chan error
}

func newEmptyAggNodeBuffer(limit int, layers uint64) *aggNodeBuffer {
	return &aggNodeBuffer{
		layers:      layers,
		limit:       uint64(limit),
		aggNodes:    make(map[common.Hash]map[string]*AggNode),
		flushResult: make(chan error, 1),
	}
}

// newAggNodeBuffer initializes the node buffer with the provided aggNodes.
func newAggNodeBuffer(limit int, aggNodes map[common.Hash]map[string]*AggNode, layers uint64) *aggNodeBuffer {
	if aggNodes == nil {
		aggNodes = make(map[common.Hash]map[string]*AggNode)
	}
	var size uint64
	for _, subset := range aggNodes {
		for path, n := range subset {
			size += uint64(n.Size() + len(path))
		}
	}
	b := &aggNodeBuffer{
		layers:   layers,
		size:     size,
		aggNodes: aggNodes,
		limit:    uint64(limit),
	}
	return b
}

// node retrieves the trie node with given node info.
func (b *aggNodeBuffer) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	subset, ok := b.aggNodes[owner]
	if !ok {
		return nil, nil
	}

	aggNode, ok := subset[string(ToAggPath(path))]
	if !ok {
		return nil, nil
	}

	n := aggNode.Node(path)
	if n == nil {
		return nil, nil
	}

	if n.Hash != hash {
		dirtyFalseMeter.Mark(1)
		log.Error("Unexpected trie node in node buffer", "owner", owner, "path", path, "expect", hash, "got", n.Hash)
		return nil, newUnexpectedNodeError("dirty", hash, n.Hash, owner, path, n.Blob)
	}
	return n, nil
}

// aggnode retrieves the agg node with given node info.
func (b *aggNodeBuffer) aggNode(owner common.Hash, aggPath []byte) *AggNode {
	subset, ok := b.aggNodes[owner]
	if !ok {
		return nil
	}

	aggNode, ok := subset[string(aggPath)]
	if !ok {
		return nil
	}

	return aggNode
}

// revert is the reverse operation of commit. It also merges the provided aggNodes
// into the aggNodeBuffer, the difference is that the provided node set should
// revert the changes made by the last state transition.
func (b *aggNodeBuffer) revert(db ethdb.KeyValueReader, aggNodes map[common.Hash]map[string]*AggNode) error {
	// Short circuit if no embedded state transition to revert.
	if b.layers == 0 {
		return errStateUnrecoverable
	}
	b.layers--

	// Reset the entire buffer if only a single transition left.
	if b.layers == 0 {
		b.reset()
		return nil
	}
	var delta int64
	for owner, subset := range aggNodes {
		current, ok := b.aggNodes[owner]
		if !ok {
			panic(fmt.Sprintf("non-existent subset (%x)", owner))
		}
		for path, n := range subset {
			orig, ok := current[path]
			if !ok {
				// There is a special case in MPT that one child is removed from
				// a fullNode which only has two children, and then a new child
				// with different position is immediately inserted into the fullNode.
				// In this case, the clean child of the fullNode will also be
				// marked as dirty because of node collapse and expansion.
				//
				// In case of database rollback, don't panic if this "clean"
				// node occurs which is not present in buffer.
				var nbytes []byte
				if owner == (common.Hash{}) {
					nbytes = rawdb.ReadAccountTrieAggNode(db, []byte(path))
				} else {
					nbytes = rawdb.ReadStorageTrieAggNode(db, owner, []byte(path))
				}

				h := newHasher()
				defer h.release()

				orighash := h.hash(orig.encodeTo())

				// Ignore the clean node in the case described above.
				if orighash == h.hash(nbytes) {
					continue
				}
				panic(fmt.Sprintf("non-existent node (%x %v) blob: %v", owner, path, crypto.Keccak256Hash(nbytes).Hex()))
			}
			current[path] = n
			delta += int64(n.Size()) - int64(orig.Size())
		}
	}
	b.updateSize(delta)
	return nil
}

// updateSize updates the total cache size by the given delta.
func (b *aggNodeBuffer) updateSize(delta int64) {
	size := int64(b.size) + delta
	if size >= 0 {
		b.size = uint64(size)
		return
	}
	s := b.size
	b.size = 0
	log.Error("Invalid pathdb buffer size", "prev", common.StorageSize(s), "delta", common.StorageSize(delta))
}

// reset cleans up the disk cache.
func (b *aggNodeBuffer) reset() {
	b.layers = 0
	b.size = 0
	b.aggNodes = make(map[common.Hash]map[string]*AggNode)
}

// empty returns an indicator if aggNodeBuffer contains any state transition inside.
func (b *aggNodeBuffer) empty() bool {
	return b.layers == 0
}

// setSize sets the buffer size to the provided number, and invokes a flush
// operation if the current memory usage exceeds the new limit.
func (b *aggNodeBuffer) setSize(size int, db ethdb.KeyValueStore, cleans *aggNodeCache, id uint64) error {
	b.limit = uint64(size)
	if b.canFlush(false) {
		return b.flush(db, nil, cleans, id)
	}
	return nil
}

func (b *aggNodeBuffer) canFlush(force bool) bool {
	if b.size <= b.limit && !force {
		return false
	}
	return true
}

// flush persists the in-memory dirty trie node into the disk if the configured
// memory threshold is reached. Note, all data must be written atomically.
func (b *aggNodeBuffer) flush(db ethdb.KeyValueStore, batch ethdb.Batch, cleans *aggNodeCache, id uint64) error {
	// Ensure the target state id is aligned with the internal counter.
	head := rawdb.ReadPersistentStateID(db)
	if head+b.layers != id {
		return fmt.Errorf("buffer layers (%d) cannot be applied on top of persisted state id (%d) to reach requested state id (%d)", b.layers, head, id)
	}
	var (
		start    = time.Now()
		newBatch = db.NewBatchWithSize(int(float64(b.size) * DefaultBatchRedundancyRate))
	)

	if batch != nil {
		err := batch.Replay(newBatch)
		if err != nil {
			return err
		}
		batch.Reset()
	}

	nodes := writeAggNodes(cleans, batch, b.aggNodes)
	rawdb.WritePersistentStateID(batch, id)

	// Flush all mutations in a single batch
	size := batch.ValueSize()
	if err := batch.Write(); err != nil {
		return err
	}
	flushBytesMeter.Mark(int64(size))
	flushNodesMeter.Mark(int64(nodes))
	flushTimeTimer.UpdateSince(start)
	log.Debug("Persisted aggPathDB aggNodes", "aggNodes", len(b.aggNodes), "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	b.reset()
	return nil
}

// writeAggNodes will persist all agg node into the database
// Note this function will inject all the clean node into the cleanCache
func writeAggNodes(cache *aggNodeCache, batch ethdb.Batch, nodes map[common.Hash]map[string]*AggNode) (total int) {
	// load the node from clean memory cache and update it, then persist it.
	for owner, subset := range nodes {
		for path, n := range subset {
			if n.Empty() {
				if owner == (common.Hash{}) {
					rawdb.DeleteAccountTrieAggNode(batch, []byte(path))
				} else {
					rawdb.DeleteStorageTrieAggNode(batch, owner, []byte(path))
				}
				if cache != nil {
					cache.cleans.Del(cacheKey(owner, []byte(path)))
				}
			} else {
				nbytes := n.encodeTo()
				if owner == (common.Hash{}) {
					rawdb.WriteAccountTrieAggNode(batch, []byte(path), nbytes)
				} else {
					rawdb.WriteStorageTrieAggNode(batch, owner, []byte(path), nbytes)
				}
				if cache != nil {
					cache.Set(cacheKey(owner, []byte(path)), nbytes)
				}
			}
		}
		total += len(subset)
	}
	return total
}

// cacheKey constructs the unique key of clean cache.
func cacheKey(owner common.Hash, path []byte) []byte {
	if owner == (common.Hash{}) {
		return path
	}
	return append(owner.Bytes(), path...)
}
