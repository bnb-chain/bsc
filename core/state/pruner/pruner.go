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

package pruner

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/tsdb/fileutil"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	// stateBloomFilePrefix is the filename prefix of state bloom filter.
	stateBloomFilePrefix = "statebloom"

	// stateBloomFilePrefix is the filename suffix of state bloom filter.
	stateBloomFileSuffix = "bf.gz"

	// stateBloomFileTempSuffix is the filename suffix of state bloom filter
	// while it is being written out to detect write aborts.
	stateBloomFileTempSuffix = ".tmp"

	// rangeCompactionThreshold is the minimal deleted entry number for
	// triggering range compaction. It's a quite arbitrary number but just
	// to avoid triggering range compaction because of small deletion.
	rangeCompactionThreshold = 100000
)

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// emptyCode is the known hash of the empty EVM bytecode.
	emptyCode = crypto.Keccak256(nil)
)

// Pruner is an offline tool to prune the stale state with the
// help of the snapshot. The workflow of pruner is very simple:
//
//   - iterate the snapshot, reconstruct the relevant state
//   - iterate the database, delete all other state entries which
//     don't belong to the target state and the genesis state
//
// It can take several hours(around 2 hours for mainnet) to finish
// the whole pruning work. It's recommended to run this offline tool
// periodically in order to release the disk usage and improve the
// disk read performance to some extent.
type Pruner struct {
	db            ethdb.Database
	stateBloom    *stateBloom
	datadir       string
	trieCachePath string
	headHeader    *types.Header
	snaptree      *snapshot.Tree
	triesInMemory uint64
}

type BlockPruner struct {
	db                  ethdb.Database
	oldAncientPath      string
	newAncientPath      string
	node                *node.Node
	BlockAmountReserved uint64
}

// NewPruner creates the pruner instance.
func NewPruner(db ethdb.Database, datadir, trieCachePath string, bloomSize, triesInMemory uint64) (*Pruner, error) {
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return nil, errors.New("Failed to load head block")
	}
	snaptree, err := snapshot.New(db, trie.NewDatabase(db), 256, int(triesInMemory), headBlock.Root(), false, false, false, false)
	if err != nil {
		return nil, err // The relevant snapshot(s) might not exist
	}
	// Sanitize the bloom filter size if it's too small.
	if bloomSize < 256 {
		log.Warn("Sanitizing bloomfilter size", "provided(MB)", bloomSize, "updated(MB)", 256)
		bloomSize = 256
	}
	stateBloom, err := newStateBloomWithSize(bloomSize)

	if err != nil {
		return nil, err
	}
	return &Pruner{
		db:            db,
		stateBloom:    stateBloom,
		datadir:       datadir,
		trieCachePath: trieCachePath,
		triesInMemory: triesInMemory,
		headHeader:    headBlock.Header(),
		snaptree:      snaptree,
	}, nil
}

func NewBlockPruner(db ethdb.Database, n *node.Node, oldAncientPath, newAncientPath string, BlockAmountReserved uint64) *BlockPruner {
	return &BlockPruner{
		db:                  db,
		oldAncientPath:      oldAncientPath,
		newAncientPath:      newAncientPath,
		node:                n,
		BlockAmountReserved: BlockAmountReserved,
	}
}

func NewAllPruner(db ethdb.Database) (*Pruner, error) {
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return nil, errors.New("Failed to load head block")
	}
	return &Pruner{
		db: db,
	}, nil
}

func (p *Pruner) PruneAll(genesis *core.Genesis) error {
	deleteCleanTrieCache(p.trieCachePath)
	return pruneAll(p.db, genesis)
}

func pruneAll(maindb ethdb.Database, g *core.Genesis) error {
	var (
		count  int
		size   common.StorageSize
		pstart = time.Now()
		logged = time.Now()
		batch  = maindb.NewBatch()
		iter   = maindb.NewIterator(nil, nil)
	)
	start := time.Now()
	for iter.Next() {
		key := iter.Key()
		if len(key) == common.HashLength {
			count += 1
			size += common.StorageSize(len(key) + len(iter.Value()))
			batch.Delete(key)

			var eta time.Duration // Realistically will never remain uninited
			if done := binary.BigEndian.Uint64(key[:8]); done > 0 {
				var (
					left  = math.MaxUint64 - binary.BigEndian.Uint64(key[:8])
					speed = done/uint64(time.Since(pstart)/time.Millisecond+1) + 1 // +1s to avoid division by zero
				)
				eta = time.Duration(left/speed) * time.Millisecond
			}
			if time.Since(logged) > 8*time.Second {
				log.Info("Pruning state data", "nodes", count, "size", size,
					"elapsed", common.PrettyDuration(time.Since(pstart)), "eta", common.PrettyDuration(eta))
				logged = time.Now()
			}
			// Recreate the iterator after every batch commit in order
			// to allow the underlying compactor to delete the entries.
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				batch.Write()
				batch.Reset()

				iter.Release()
				iter = maindb.NewIterator(nil, key)
			}
		}
	}
	if batch.ValueSize() > 0 {
		batch.Write()
		batch.Reset()
	}
	iter.Release()
	log.Info("Pruned state data", "nodes", count, "size", size, "elapsed", common.PrettyDuration(time.Since(pstart)))

	// Start compactions, will remove the deleted data from the disk immediately.
	// Note for small pruning, the compaction is skipped.
	if count >= rangeCompactionThreshold {
		cstart := time.Now()
		for b := 0x00; b <= 0xf0; b += 0x10 {
			var (
				start = []byte{byte(b)}
				end   = []byte{byte(b + 0x10)}
			)
			if b == 0xf0 {
				end = nil
			}
			log.Info("Compacting database", "range", fmt.Sprintf("%#x-%#x", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
			if err := maindb.Compact(start, end); err != nil {
				log.Error("Database compaction failed", "error", err)
				return err
			}
		}
		log.Info("Database compaction finished", "elapsed", common.PrettyDuration(time.Since(cstart)))
	}
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(maindb), nil)
	for addr, account := range g.Alloc {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root := statedb.IntermediateRoot(false)
	statedb.Commit(nil)
	statedb.Database().TrieDB().Commit(root, true, nil)
	log.Info("State pruning successful", "pruned", size, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

func prune(snaptree *snapshot.Tree, root common.Hash, maindb ethdb.Database, stateBloom *stateBloom, bloomPath string, middleStateRoots map[common.Hash]struct{}, start time.Time) error {
	// Delete all stale trie nodes in the disk. With the help of state bloom
	// the trie nodes(and codes) belong to the active state will be filtered
	// out. A very small part of stale tries will also be filtered because of
	// the false-positive rate of bloom filter. But the assumption is held here
	// that the false-positive is low enough(~0.05%). The probablity of the
	// dangling node is the state root is super low. So the dangling nodes in
	// theory will never ever be visited again.
	var (
		count  int
		size   common.StorageSize
		pstart = time.Now()
		logged = time.Now()
		batch  = maindb.NewBatch()
		iter   = maindb.NewIterator(nil, nil)
	)
	for iter.Next() {
		key := iter.Key()

		// All state entries don't belong to specific state and genesis are deleted here
		// - trie node
		// - legacy contract code
		// - new-scheme contract code
		isCode, codeKey := rawdb.IsCodeKey(key)
		if len(key) == common.HashLength || isCode {
			checkKey := key
			if isCode {
				checkKey = codeKey
			}
			if _, exist := middleStateRoots[common.BytesToHash(checkKey)]; exist {
				log.Debug("Forcibly delete the middle state roots", "hash", common.BytesToHash(checkKey))
			} else {
				if ok, err := stateBloom.Contain(checkKey); err != nil {
					return err
				} else if ok {
					continue
				}
			}
			count += 1
			size += common.StorageSize(len(key) + len(iter.Value()))
			batch.Delete(key)

			var eta time.Duration // Realistically will never remain uninited
			if done := binary.BigEndian.Uint64(key[:8]); done > 0 {
				var (
					left  = math.MaxUint64 - binary.BigEndian.Uint64(key[:8])
					speed = done/uint64(time.Since(pstart)/time.Millisecond+1) + 1 // +1s to avoid division by zero
				)
				eta = time.Duration(left/speed) * time.Millisecond
			}
			if time.Since(logged) > 8*time.Second {
				log.Info("Pruning state data", "nodes", count, "size", size,
					"elapsed", common.PrettyDuration(time.Since(pstart)), "eta", common.PrettyDuration(eta))
				logged = time.Now()
			}
			// Recreate the iterator after every batch commit in order
			// to allow the underlying compactor to delete the entries.
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				batch.Write()
				batch.Reset()

				iter.Release()
				iter = maindb.NewIterator(nil, key)
			}
		}
	}
	if batch.ValueSize() > 0 {
		batch.Write()
		batch.Reset()
	}
	iter.Release()
	log.Info("Pruned state data", "nodes", count, "size", size, "elapsed", common.PrettyDuration(time.Since(pstart)))

	// Pruning is done, now drop the "useless" layers from the snapshot.
	// Firstly, flushing the target layer into the disk. After that all
	// diff layers below the target will all be merged into the disk.
	if root != snaptree.DiskRoot() {
		if err := snaptree.Cap(root, 0); err != nil {
			return err
		}
	}
	// Secondly, flushing the snapshot journal into the disk. All diff
	// layers upon are dropped silently. Eventually the entire snapshot
	// tree is converted into a single disk layer with the pruning target
	// as the root.
	if _, err := snaptree.Journal(root); err != nil {
		return err
	}
	// Delete the state bloom, it marks the entire pruning procedure is
	// finished. If any crashes or manual exit happens before this,
	// `RecoverPruning` will pick it up in the next restarts to redo all
	// the things.
	os.RemoveAll(bloomPath)

	// Start compactions, will remove the deleted data from the disk immediately.
	// Note for small pruning, the compaction is skipped.
	if count >= rangeCompactionThreshold {
		cstart := time.Now()
		for b := 0x00; b <= 0xf0; b += 0x10 {
			var (
				start = []byte{byte(b)}
				end   = []byte{byte(b + 0x10)}
			)
			if b == 0xf0 {
				end = nil
			}
			log.Info("Compacting database", "range", fmt.Sprintf("%#x-%#x", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
			if err := maindb.Compact(start, end); err != nil {
				log.Error("Database compaction failed", "error", err)
				return err
			}
		}
		log.Info("Database compaction finished", "elapsed", common.PrettyDuration(time.Since(cstart)))
	}
	log.Info("State pruning successful", "pruned", size, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

func (p *BlockPruner) backUpOldDb(name string, cache, handles int, namespace string, readonly, interrupt bool) error {
	// Open old db wrapper.
	chainDb, err := p.node.OpenDatabaseWithFreezer(name, cache, handles, p.oldAncientPath, namespace, readonly, true, interrupt, false, true)
	if err != nil {
		log.Error("Failed to open ancient database", "err=", err)
		return err
	}
	defer chainDb.Close()
	log.Info("chainDB opened successfully")

	// Get the number of items in old ancient db.
	itemsOfAncient, err := chainDb.ItemAmountInAncient()
	log.Info("the number of items in ancientDB is ", "itemsOfAncient", itemsOfAncient)

	// If we can't access the freezer or it's empty, abort.
	if err != nil || itemsOfAncient == 0 {
		log.Error("can't access the freezer or it's empty, abort")
		return errors.New("can't access the freezer or it's empty, abort")
	}

	// If the items in freezer is less than the block amount that we want to reserve, it is not enough, should stop.
	if itemsOfAncient < p.BlockAmountReserved {
		log.Error("the number of old blocks is not enough to reserve,", "ancient items", itemsOfAncient, "the amount specified", p.BlockAmountReserved)
		return errors.New("the number of old blocks is not enough to reserve")
	}

	var oldOffSet uint64
	if interrupt {
		// The interrupt scecario within this function is specific for old and new ancientDB exsisted concurrently,
		// should use last version of offset for oldAncientDB, because current offset is
		// actually of the new ancientDB_Backup, but what we want is the offset of ancientDB being backup.
		oldOffSet = rawdb.ReadOffSetOfLastAncientFreezer(chainDb)
	} else {
		// Using current version of ancientDB for oldOffSet because the db for backup is current version.
		oldOffSet = rawdb.ReadOffSetOfCurrentAncientFreezer(chainDb)
	}
	log.Info("the oldOffSet is ", "oldOffSet", oldOffSet)

	// Get the start BlockNumber for pruning.
	startBlockNumber := oldOffSet + itemsOfAncient - p.BlockAmountReserved
	log.Info("new offset/new startBlockNumber is ", "new offset", startBlockNumber)

	// Create new ancientdb backup and record the new and last version of offset in kvDB as well.
	// For every round, newoffset actually equals to the startBlockNumber in ancient backup db.
	frdbBack, err := rawdb.NewFreezerDb(chainDb, p.newAncientPath, namespace, readonly, startBlockNumber)
	if err != nil {
		log.Error("Failed to create ancient freezer backup", "err=", err)
		return err
	}
	defer frdbBack.Close()

	offsetBatch := chainDb.NewBatch()
	rawdb.WriteOffSetOfCurrentAncientFreezer(offsetBatch, startBlockNumber)
	rawdb.WriteOffSetOfLastAncientFreezer(offsetBatch, oldOffSet)
	if err := offsetBatch.Write(); err != nil {
		log.Crit("Failed to write offset into disk", "err", err)
	}

	// It's guaranteed that the old/new offsets are updated as well as the new ancientDB are created if this flock exist.
	lock, _, err := fileutil.Flock(filepath.Join(p.newAncientPath, "PRUNEFLOCKBACK"))
	if err != nil {
		log.Error("file lock error", "err", err)
		return err
	}

	log.Info("prune info", "old offset", oldOffSet, "number of items in ancientDB", itemsOfAncient, "amount to reserve", p.BlockAmountReserved)
	log.Info("new offset/new startBlockNumber recorded successfully ", "new offset", startBlockNumber)

	start := time.Now()
	// All ancient data after and including startBlockNumber should write into new ancientDB ancient_back.
	for blockNumber := startBlockNumber; blockNumber < itemsOfAncient+oldOffSet; blockNumber++ {
		blockHash := rawdb.ReadCanonicalHash(chainDb, blockNumber)
		block := rawdb.ReadBlock(chainDb, blockHash, blockNumber)
		receipts := rawdb.ReadRawReceipts(chainDb, blockHash, blockNumber)
		// Calculate the total difficulty of the block
		td := rawdb.ReadTd(chainDb, blockHash, blockNumber)
		if td == nil {
			return consensus.ErrUnknownAncestor
		}
		// Write into new ancient_back db.
		if _, err := rawdb.WriteAncientBlocks(frdbBack, []*types.Block{block}, []types.Receipts{receipts}, td); err != nil {
			log.Error("failed to write new ancient", "error", err)
			return err
		}
		// Print the log every 5s for better trace.
		if common.PrettyDuration(time.Since(start)) > common.PrettyDuration(5*time.Second) {
			log.Info("block backup process running successfully", "current blockNumber for backup", blockNumber)
			start = time.Now()
		}
	}
	lock.Release()
	log.Info("block back up done", "current start blockNumber in ancientDB", startBlockNumber)
	return nil
}

// Backup the ancient data for the old ancient db, i.e. the most recent 128 blocks in ancient db.
func (p *BlockPruner) BlockPruneBackUp(name string, cache, handles int, namespace string, readonly, interrupt bool) error {

	start := time.Now()

	if err := p.backUpOldDb(name, cache, handles, namespace, readonly, interrupt); err != nil {
		return err
	}

	log.Info("Block pruning BackUp successfully", "time duration since start is", common.PrettyDuration(time.Since(start)))
	return nil
}

func (p *BlockPruner) RecoverInterruption(name string, cache, handles int, namespace string, readonly bool) error {
	log.Info("RecoverInterruption for block prune")
	newExist, err := CheckFileExist(p.newAncientPath)
	if err != nil {
		log.Error("newAncientDb path error")
		return err
	}

	if newExist {
		log.Info("New ancientDB_backup existed in interruption scenario")
		flockOfAncientBack, err := CheckFileExist(filepath.Join(p.newAncientPath, "PRUNEFLOCKBACK"))
		if err != nil {
			log.Error("Failed to check flock of ancientDB_Back %v", err)
			return err
		}

		// Indicating both old and new ancientDB existed concurrently.
		// Delete directly for the new ancientdb to prune from start, e.g.: path ../chaindb/ancient_backup
		if err := os.RemoveAll(p.newAncientPath); err != nil {
			log.Error("Failed to remove old ancient directory %v", err)
			return err
		}
		if flockOfAncientBack {
			// Indicating the oldOffset/newOffset have already been updated.
			if err := p.BlockPruneBackUp(name, cache, handles, namespace, readonly, true); err != nil {
				log.Error("Failed to prune")
				return err
			}
		} else {
			// Indicating the flock did not exist and the new offset did not be updated, so just handle this case as usual.
			if err := p.BlockPruneBackUp(name, cache, handles, namespace, readonly, false); err != nil {
				log.Error("Failed to prune")
				return err
			}
		}

		if err := p.AncientDbReplacer(); err != nil {
			log.Error("Failed to replace ancientDB")
			return err
		}
	} else {
		log.Info("New ancientDB_backup did not exist in interruption scenario")
		// Indicating new ancientDB even did not be created, just prune starting at backup from startBlockNumber as usual,
		// in this case, the new offset have not been written into kvDB.
		if err := p.BlockPruneBackUp(name, cache, handles, namespace, readonly, false); err != nil {
			log.Error("Failed to prune")
			return err
		}
		if err := p.AncientDbReplacer(); err != nil {
			log.Error("Failed to replace ancientDB")
			return err
		}
	}

	return nil
}

func CheckFileExist(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Indicating the file didn't exist.
			return false, nil
		}
		return true, err
	}
	return true, nil
}

func (p *BlockPruner) AncientDbReplacer() error {
	// Delete directly for the old ancientdb, e.g.: path ../chaindb/ancient
	if err := os.RemoveAll(p.oldAncientPath); err != nil {
		log.Error("Failed to remove old ancient directory %v", err)
		return err
	}

	// Rename the new ancientdb path same to the old
	if err := os.Rename(p.newAncientPath, p.oldAncientPath); err != nil {
		log.Error("Failed to rename new ancient directory")
		return err
	}
	return nil
}

// Prune deletes all historical state nodes except the nodes belong to the
// specified state version. If user doesn't specify the state version, use
// the bottom-most snapshot diff layer as the target.
func (p *Pruner) Prune(root common.Hash) error {
	// If the state bloom filter is already committed previously,
	// reuse it for pruning instead of generating a new one. It's
	// mandatory because a part of state may already be deleted,
	// the recovery procedure is necessary.
	_, stateBloomRoot, err := findBloomFilter(p.datadir)
	if err != nil {
		return err
	}
	if stateBloomRoot != (common.Hash{}) {
		return RecoverPruning(p.datadir, p.db, p.trieCachePath, p.triesInMemory)
	}
	// If the target state root is not specified, use the HEAD-(n-1) as the
	// target. The reason for picking it is:
	// - in most of the normal cases, the related state is available
	// - the probability of this layer being reorg is very low
	var layers []snapshot.Snapshot
	if root == (common.Hash{}) {
		// Retrieve all snapshot layers from the current HEAD.
		// In theory there are n difflayers + 1 disk layer present,
		// so n diff layers are expected to be returned.
		layers = p.snaptree.Snapshots(p.headHeader.Root, int(p.triesInMemory), true)
		if len(layers) != int(p.triesInMemory) {
			// Reject if the accumulated diff layers are less than n. It
			// means in most of normal cases, there is no associated state
			// with bottom-most diff layer.
			return fmt.Errorf("snapshot not old enough yet: need %d more blocks", int(p.triesInMemory)-len(layers))
		}
		// Use the bottom-most diff layer as the target
		root = layers[len(layers)-1].Root()
	}
	// Ensure the root is really present. The weak assumption
	// is the presence of root can indicate the presence of the
	// entire trie.
	if !rawdb.HasTrieNode(p.db, root) {
		// The special case is for clique based networks(rinkeby, goerli
		// and some other private networks), it's possible that two
		// consecutive blocks will have same root. In this case snapshot
		// difflayer won't be created. So HEAD-(n-1) may not paired with
		// head-(n-1) layer. Instead the paired layer is higher than the
		// bottom-most diff layer. Try to find the bottom-most snapshot
		// layer with state available.
		//
		// Note HEAD is ignored. Usually there is the associated
		// state available, but we don't want to use the topmost state
		// as the pruning target.
		var found bool
		for i := len(layers) - 2; i >= 1; i-- {
			if rawdb.HasTrieNode(p.db, layers[i].Root()) {
				root = layers[i].Root()
				found = true
				log.Info("Selecting middle-layer as the pruning target", "root", root, "depth", i)
				break
			}
		}
		if !found {
			if blob := rawdb.ReadTrieNode(p.db, p.snaptree.DiskRoot()); len(blob) != 0 {
				root = p.snaptree.DiskRoot()
				found = true
				log.Info("Selecting disk-layer as the pruning target", "root", root)
			}
		}
		if !found {
			if len(layers) > 0 {
				return errors.New("no snapshot paired state")
			}
			return fmt.Errorf("associated state[%x] is not present", root)
		}
	} else {
		if len(layers) > 0 {
			log.Info("Selecting bottom-most difflayer as the pruning target", "root", root, "height", p.headHeader.Number.Uint64()-127)
		} else {
			log.Info("Selecting user-specified state as the pruning target", "root", root)
		}
	}
	// Before start the pruning, delete the clean trie cache first.
	// It's necessary otherwise in the next restart we will hit the
	// deleted state root in the "clean cache" so that the incomplete
	// state is picked for usage.
	deleteCleanTrieCache(p.trieCachePath)

	// All the state roots of the middle layer should be forcibly pruned,
	// otherwise the dangling state will be left.
	middleRoots := make(map[common.Hash]struct{})
	for _, layer := range layers {
		if layer.Root() == root {
			break
		}
		middleRoots[layer.Root()] = struct{}{}
	}
	// Traverse the target state, re-construct the whole state trie and
	// commit to the given bloom filter.
	start := time.Now()
	if err := snapshot.GenerateTrie(p.snaptree, root, p.db, p.stateBloom); err != nil {
		return err
	}
	// Traverse the genesis, put all genesis state entries into the
	// bloom filter too.
	if err := extractGenesis(p.db, p.stateBloom); err != nil {
		return err
	}
	filterName := bloomFilterName(p.datadir, root)

	log.Info("Writing state bloom to disk", "name", filterName)
	if err := p.stateBloom.Commit(filterName, filterName+stateBloomFileTempSuffix); err != nil {
		return err
	}
	log.Info("State bloom filter committed", "name", filterName)
	return prune(p.snaptree, root, p.db, p.stateBloom, filterName, middleRoots, start)
}

// RecoverPruning will resume the pruning procedure during the system restart.
// This function is used in this case: user tries to prune state data, but the
// system was interrupted midway because of crash or manual-kill. In this case
// if the bloom filter for filtering active state is already constructed, the
// pruning can be resumed. What's more if the bloom filter is constructed, the
// pruning **has to be resumed**. Otherwise a lot of dangling nodes may be left
// in the disk.
func RecoverPruning(datadir string, db ethdb.Database, trieCachePath string, triesInMemory uint64) error {
	stateBloomPath, stateBloomRoot, err := findBloomFilter(datadir)
	if err != nil {
		return err
	}
	if stateBloomPath == "" {
		return nil // nothing to recover
	}
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return errors.New("Failed to load head block")
	}
	// Initialize the snapshot tree in recovery mode to handle this special case:
	// - Users run the `prune-state` command multiple times
	// - Neither these `prune-state` running is finished(e.g. interrupted manually)
	// - The state bloom filter is already generated, a part of state is deleted,
	//   so that resuming the pruning here is mandatory
	// - The state HEAD is rewound already because of multiple incomplete `prune-state`
	// In this case, even the state HEAD is not exactly matched with snapshot, it
	// still feasible to recover the pruning correctly.
	snaptree, err := snapshot.New(db, trie.NewDatabase(db), 256, int(triesInMemory), headBlock.Root(), false, false, true, false)
	if err != nil {
		return err // The relevant snapshot(s) might not exist
	}
	stateBloom, err := NewStateBloomFromDisk(stateBloomPath)
	if err != nil {
		return err
	}
	log.Info("Loaded state bloom filter", "path", stateBloomPath)

	// Before start the pruning, delete the clean trie cache first.
	// It's necessary otherwise in the next restart we will hit the
	// deleted state root in the "clean cache" so that the incomplete
	// state is picked for usage.
	deleteCleanTrieCache(trieCachePath)

	// All the state roots of the middle layers should be forcibly pruned,
	// otherwise the dangling state will be left.
	var (
		found       bool
		layers      = snaptree.Snapshots(headBlock.Root(), int(triesInMemory), true)
		middleRoots = make(map[common.Hash]struct{})
	)
	for _, layer := range layers {
		if layer.Root() == stateBloomRoot {
			found = true
			break
		}
		middleRoots[layer.Root()] = struct{}{}
	}
	if !found {
		log.Error("Pruning target state is not existent")
		return errors.New("non-existent target state")
	}
	return prune(snaptree, stateBloomRoot, db, stateBloom, stateBloomPath, middleRoots, time.Now())
}

// extractGenesis loads the genesis state and commits all the state entries
// into the given bloomfilter.
func extractGenesis(db ethdb.Database, stateBloom *stateBloom) error {
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return errors.New("missing genesis hash")
	}
	genesis := rawdb.ReadBlock(db, genesisHash, 0)
	if genesis == nil {
		return errors.New("missing genesis block")
	}
	t, err := trie.NewSecure(genesis.Root(), trie.NewDatabase(db))
	if err != nil {
		return err
	}
	accIter := t.NodeIterator(nil)
	for accIter.Next(true) {
		hash := accIter.Hash()

		// Embedded nodes don't have hash.
		if hash != (common.Hash{}) {
			stateBloom.Put(hash.Bytes(), nil)
		}
		// If it's a leaf node, yes we are touching an account,
		// dig into the storage trie further.
		if accIter.Leaf() {
			var acc types.StateAccount
			if err := rlp.DecodeBytes(accIter.LeafBlob(), &acc); err != nil {
				return err
			}
			if acc.Root != emptyRoot {
				storageTrie, err := trie.NewSecure(acc.Root, trie.NewDatabase(db))
				if err != nil {
					return err
				}
				storageIter := storageTrie.NodeIterator(nil)
				for storageIter.Next(true) {
					hash := storageIter.Hash()
					if hash != (common.Hash{}) {
						stateBloom.Put(hash.Bytes(), nil)
					}
				}
				if storageIter.Error() != nil {
					return storageIter.Error()
				}
			}
			if !bytes.Equal(acc.CodeHash, emptyCode) {
				stateBloom.Put(acc.CodeHash, nil)
			}
		}
	}
	return accIter.Error()
}

func bloomFilterName(datadir string, hash common.Hash) string {
	return filepath.Join(datadir, fmt.Sprintf("%s.%s.%s", stateBloomFilePrefix, hash.Hex(), stateBloomFileSuffix))
}

func isBloomFilter(filename string) (bool, common.Hash) {
	filename = filepath.Base(filename)
	if strings.HasPrefix(filename, stateBloomFilePrefix) && strings.HasSuffix(filename, stateBloomFileSuffix) {
		return true, common.HexToHash(filename[len(stateBloomFilePrefix)+1 : len(filename)-len(stateBloomFileSuffix)-1])
	}
	return false, common.Hash{}
}

func findBloomFilter(datadir string) (string, common.Hash, error) {
	var (
		stateBloomPath string
		stateBloomRoot common.Hash
	)
	if err := filepath.Walk(datadir, func(path string, info os.FileInfo, err error) error {
		if info != nil && !info.IsDir() {
			ok, root := isBloomFilter(path)
			if ok {
				stateBloomPath = path
				stateBloomRoot = root
			}
		}
		return nil
	}); err != nil {
		return "", common.Hash{}, err
	}
	return stateBloomPath, stateBloomRoot, nil
}

const warningLog = `

WARNING!

The clean trie cache is not found. Please delete it by yourself after the
pruning. Remember don't start the Geth without deleting the clean trie cache
otherwise the entire database may be damaged!

Check the command description "geth snapshot prune-state --help" for more details.
`

func deleteCleanTrieCache(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Warn(warningLog)
		return
	}
	os.RemoveAll(path)
	log.Info("Deleted trie clean cache", "path", path)
}
