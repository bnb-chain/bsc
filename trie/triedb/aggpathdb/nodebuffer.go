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

// nodebuffer is a collection of modified trie nodes to aggregate the disk
// write. The content of the nodebuffer must be checked before diving into
// disk (since it basically is not-yet-written data).
type nodebuffer struct {
	layers uint64                                               // The number of diff layers aggregated inside
	size   uint64                                               // The size of aggregated writes
	limit  uint64                                               // The maximum memory allowance in bytes
	nodes  map[common.Hash]map[string]map[string]*trienode.Node // The dirty node set, mapped by owner, aggpath and path
}

// newNodeBuffer initializes the node buffer with the provided nodes.
func newNodeBuffer(limit int, nodes map[common.Hash]map[string]*trienode.Node, layers uint64) *nodebuffer {
	if nodes == nil {
		nodes = make(map[common.Hash]map[string]*trienode.Node)
	}
	var size uint64
	for _, subset := range nodes {
		for path, n := range subset {
			size += uint64(len(n.Blob) + len(path))
		}
	}
	b := &nodebuffer{
		layers: layers,
		size:   size,
		nodes:  make(map[common.Hash]map[string]map[string]*trienode.Node),
		limit:  uint64(limit),
	}
	preAggregateNodes(nodes, b.nodes)
	return b
}

// node retrieves the trie node with given node info.
func (b *nodebuffer) node(owner common.Hash, path []byte, hash common.Hash) (*trienode.Node, error) {
	subset, ok := b.nodes[owner]
	if !ok {
		return nil, nil
	}

	cs, ok := subset[string(toAggPath(path))]
	if !ok {
		return nil, nil
	}

	n, ok := cs[string(path)]
	if !ok {
		return nil, nil
	}
	if n.Hash != hash {
		dirtyFalseMeter.Mark(1)
		log.Error("Unexpected trie node in node buffer", "owner", owner, "path", path, "expect", hash, "got", n.Hash)
		return nil, newUnexpectedNodeError("dirty", hash, n.Hash, owner, path, n.Blob)
	}
	return n, nil
}

// commit merges the dirty nodes into the nodebuffer. This operation won't take
// the ownership of the nodes map which belongs to the bottom-most diff layer.
// It will just hold the node references from the given map which are safe to
// copy.
func (b *nodebuffer) commit(nodes map[common.Hash]map[string]*trienode.Node) *nodebuffer {
	var (
		delta         int64
		overwrite     int64
		overwriteSize int64
	)
	for owner, subset := range nodes {
		current, exist := b.nodes[owner]
		if !exist {
			// Allocate a new map for the subset instead of claiming it directly
			// from the passed map to avoid potential concurrent map read/write.
			// The nodes belong to original diff layer are still accessible even
			// after merging, thus the ownership of nodes map should still belong
			// to original layer and any mutation on it should be prevented.
			current = make(map[string]map[string]*trienode.Node)

			for path, n := range subset {
				aggPath := toAggPath([]byte(path))
				cs, exist := current[string(aggPath)]
				if !exist {
					cs = make(map[string]*trienode.Node)
				}
				cs[path] = n
				current[string(aggPath)] = cs
				delta += int64(len(n.Blob) + len(path))
			}
			b.nodes[owner] = current
			continue
		}

		for path, n := range subset {
			aggPath := toAggPath([]byte(path))
			cs, exist := current[string(aggPath)]
			if !exist {
				cs = make(map[string]*trienode.Node)
			}
			if orig, exist := cs[path]; !exist {
				delta += int64(len(n.Blob) + len(path))
			} else {
				delta += int64(len(n.Blob) - len(orig.Blob))
				overwrite++
				overwriteSize += int64(len(orig.Blob) + len(path))
			}
			cs[path] = n
			current[string(aggPath)] = cs
		}
		b.nodes[owner] = current
	}
	b.updateSize(delta)
	b.layers++
	gcNodesMeter.Mark(overwrite)
	gcBytesMeter.Mark(overwriteSize)
	return b
}

// revert is the reverse operation of commit. It also merges the provided nodes
// into the nodebuffer, the difference is that the provided node set should
// revert the changes made by the last state transition.
func (b *nodebuffer) revert(db ethdb.KeyValueReader, nodes map[common.Hash]map[string]*trienode.Node) error {
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
	for owner, subset := range nodes {
		current, ok := b.nodes[owner]
		if !ok {
			panic(fmt.Sprintf("non-existent subset (%x)", owner))
		}
		for path, n := range subset {
			aggPath := toAggPath([]byte(path))
			cs, ok := current[string(aggPath)]
			if !ok {
				panic(fmt.Sprintf("non-existent changeset (%x) (%x)", owner, aggPath))
			}
			orig, ok := cs[path]
			if !ok {
				// There is a special case in MPT that one child is removed from
				// a fullNode which only has two children, and then a new child
				// with different position is immediately inserted into the fullNode.
				// In this case, the clean child of the fullNode will also be
				// marked as dirty because of node collapse and expansion.
				//
				// In case of database rollback, don't panic if this "clean"
				// node occurs which is not present in buffer.
				var nhash common.Hash
				_, nhash = ReadTrieNodeFromAggNode(db, owner, []byte(path))
				// Ignore the clean node in the case described above.
				if nhash == n.Hash {
					continue
				}
				panic(fmt.Sprintf("non-existent node (%x %v) blob: %v", owner, path, crypto.Keccak256Hash(n.Blob).Hex()))
			}
			cs[path] = n
			current[string(aggPath)] = cs
			delta += int64(len(n.Blob)) - int64(len(orig.Blob))
		}
	}
	b.updateSize(delta)
	return nil
}

// updateSize updates the total cache size by the given delta.
func (b *nodebuffer) updateSize(delta int64) {
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
func (b *nodebuffer) reset() {
	b.layers = 0
	b.size = 0
	b.nodes = make(map[common.Hash]map[string]map[string]*trienode.Node)
}

// empty returns an indicator if nodebuffer contains any state transition inside.
func (b *nodebuffer) empty() bool {
	return b.layers == 0
}

// setSize sets the buffer size to the provided number, and invokes a flush
// operation if the current memory usage exceeds the new limit.
func (b *nodebuffer) setSize(size int, db ethdb.KeyValueStore, cleans *aggnodecache, id uint64) error {
	b.limit = uint64(size)
	return b.flush(db, cleans, id, false)
}

// flush persists the in-memory dirty trie node into the disk if the configured
// memory threshold is reached. Note, all data must be written atomically.
func (b *nodebuffer) flush(db ethdb.KeyValueStore, cleans *aggnodecache, id uint64, force bool) error {
	if b.size <= b.limit && !force {
		return nil
	}
	// Ensure the target state id is aligned with the internal counter.
	head := rawdb.ReadPersistentStateID(db)
	if head+b.layers != id {
		return fmt.Errorf("buffer layers (%d) cannot be applied on top of persisted state id (%d) to reach requested state id (%d)", b.layers, head, id)
	}
	var (
		start = time.Now()
		batch = db.NewBatchWithSize(int(b.size))
	)

	nodes := aggregateAndWriteNodes(cleans, batch, b.nodes)
	rawdb.WritePersistentStateID(batch, id)

	// Flush all mutations in a single batch
	size := batch.ValueSize()
	if err := batch.Write(); err != nil {
		return err
	}
	commitBytesMeter.Mark(int64(size))
	commitNodesMeter.Mark(int64(nodes))
	commitTimeTimer.UpdateSince(start)
	log.Debug("Persisted aggPathDB nodes", "nodes", len(b.nodes), "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	b.reset()
	return nil
}

func preAggregateNodes(nodes map[common.Hash]map[string]*trienode.Node, changeSet map[common.Hash]map[string]map[string]*trienode.Node) {
	for owner, subset := range nodes {
		current, exist := changeSet[owner]
		if !exist {
			current = make(map[string]map[string]*trienode.Node)
		}
		for path, n := range subset {
			aggPath := toAggPath([]byte(path))
			aggChangeSet, exist := current[string(aggPath)]
			if !exist {
				aggChangeSet = make(map[string]*trienode.Node)
			}
			aggChangeSet[path] = n
			current[string(aggPath)] = aggChangeSet
		}
		changeSet[owner] = current
	}
}

// aggregateAndWriteNodes will aggregate the trienode into trie aggnode and persist into the database
// Note this function will inject all the clean aggNode into the cleanCache
func aggregateAndWriteNodes(cleans *aggnodecache, batch ethdb.Batch, nodes map[common.Hash]map[string]map[string]*trienode.Node) (total int) {
	// load the aggNode from clean memory cache and update it, then persist it.
	for owner, subset := range nodes {
		for aggPath, cs := range subset {
			aggNode, err := cleans.aggNode(owner, []byte(aggPath))
			if err != nil {
				panic(fmt.Sprintf("Decode aggNode failed, error %v", err))
			}
			if aggNode == nil {
				aggNode = &AggNode{}
			}
			for path, n := range cs {
				if n.IsDeleted() {
					aggNode.Delete([]byte(path))
				} else {
					aggNode.Update([]byte(path), n.Blob)
				}
			}
			if aggNode.Empty() {
				deleteAggNode(batch, owner, []byte(aggPath))
				if cleans.cleans != nil {
					cleans.cleans.Del(cacheKey(owner, []byte(aggPath)))
				}
			} else {
				aggNodeBytes := aggNode.encodeTo()
				writeAggNode(batch, owner, []byte(aggPath), aggNodeBytes)
				if cleans.cleans != nil {
					cleans.cleans.Set(cacheKey(owner, []byte(aggPath)), aggNodeBytes)
				}
			}
			// If we exceeded the ideal batch size, commit and reset
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					log.Error("Failed to write flush list to disk", "err", err)
					panic("Failed to write flush list to disk")
				}
				commitBytesMeter.Mark(int64(batch.ValueSize()))
				batch.Reset()
			}
			total++
		}
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
