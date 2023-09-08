package trie

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type Hash2Path struct {
	trie            *Trie // traverse trie
	db              *Database
	blocknum        uint64
	root            node // root of triedb
	stateRootHash   common.Hash
	num             uint64 // block number
	concurrentQueue chan struct{}
	totalNum        uint64
	wg              sync.WaitGroup
	// batch           ethdb.Batch
}

const (
	DEFAULT_TRIEDBCACHE_SIZE = 1024 * 1024 * 1024
)

// NewHash2Path return a hash2Path obj
func NewHash2Path(tr *Trie, db *Database, stateRootHash common.Hash, blocknum uint64, jobnum uint64) (*Hash2Path, error) {
	if tr == nil {
		return nil, errors.New("trie is nil")
	}

	if tr.root == nil {
		return nil, errors.New("trie root is nil")
	}

	ins := &Hash2Path{
		trie:            tr,
		blocknum:        blocknum,
		db:              db,
		stateRootHash:   stateRootHash,
		root:            tr.root,
		concurrentQueue: make(chan struct{}, jobnum),
		wg:              sync.WaitGroup{},
		// batch:           db.diskdb.NewBatch(),
	}

	return ins, nil
}

func (t *Trie) resloveWithoutTrack(n node, prefix []byte) (node, error) {
	if n, ok := n.(hashNode); ok {
		blob, err := t.reader.node(prefix, common.BytesToHash(n))
		if err != nil {
			return nil, err
		}
		return mustDecodeNode(n, blob), nil
	}
	return n, nil
}

func (h2p *Hash2Path) writeNode(pathKey []byte, n *trienode.Node, owner common.Hash) {
	if owner == (common.Hash{}) {
		rawdb.WriteAccountTrieNode(h2p.db.DiskDB(), pathKey, n.Blob)
		fmt.Println("WriteNodes account node, ", "path: ", common.Bytes2Hex(pathKey), "Hash: ", n.Hash, "BlobHash: ", crypto.Keccak256Hash(n.Blob))
	} else {
		rawdb.WriteStorageTrieNode(h2p.db.DiskDB(), owner, pathKey, n.Blob)
		fmt.Println("WriteNodes storage node, ", "path: ", common.Bytes2Hex(pathKey), "owner: ", owner.String(), "Hash: ", n.Hash, "BlobHash: ", crypto.Keccak256Hash(n.Blob))
	}
	// if delete the nodes of the account trie here, the error will occur when open storage trie.
	// rawdb.DeleteTrieNode(h2p.db.DiskDB(), owner, nil, n.Hash, rawdb.HashScheme)

	// if h2p.batch.ValueSize() > 100000 {
	// 	err := h2p.batch.Write()
	// 	if err != nil {
	// 		fmt.Println("batch write error: ", err)
	// 	}
	// 	// reset for next batch write
	// 	h2p.batch.Reset()
	// }
}

// Run statistics, external call
func (h2p *Hash2Path) Run() {
	log.Info("Find Account Trie Tree, rootHash: ", h2p.trie.Hash().String(), "BlockNum: ", h2p.blocknum)

	h2p.ConcurrentTraversal(h2p.trie, h2p.root, []byte{})
	h2p.wg.Wait()

	fmt.Println("hash2Path run finished.")
	rawdb.WritePersistentStateID(h2p.db.DiskDB(), h2p.blocknum)
	rawdb.WriteStateID(h2p.db.DiskDB(), h2p.stateRootHash, h2p.blocknum)
	// err := h2p.batch.Write()
	// if err != nil {
	// 	fmt.Println("batch write error: ", err)
	// }
	// h2p.batch.Reset()
}

func (h2p *Hash2Path) SubConcurrentTraversal(theTrie *Trie, theNode node, path []byte) {
	h2p.concurrentQueue <- struct{}{}
	h2p.ConcurrentTraversal(theTrie, theNode, path)
	<-h2p.concurrentQueue
	h2p.wg.Done()
	return
}

func (h2p *Hash2Path) ConcurrentTraversal(theTrie *Trie, theNode node, path []byte) {
	// print process progress
	total_num := atomic.AddUint64(&h2p.totalNum, 1)
	if total_num%100000 == 0 {
		fmt.Printf("Complete progress: %v, go routines Num: %v, h2p concurrentQueue: %v\n", total_num, runtime.NumGoroutine(), len(h2p.concurrentQueue))
	}

	// nil node
	if theNode == nil {
		return
	}

	switch current := (theNode).(type) {
	case *shortNode:
		collapsed := current.copy()
		collapsed.Key = hexToCompact(current.Key)
		var hash, _ = current.cache()
		h2p.writeNode(path, trienode.New(common.BytesToHash(hash), nodeToBytes(collapsed)), theTrie.owner)

		h2p.ConcurrentTraversal(theTrie, current.Val, append(path, current.Key...))
	case *fullNode:
		// copy from trie/Committer (*committer).commit
		collapsed := current.copy()
		collapsed.Children = h2p.commitChildren(path, current)
		var hash, _ = collapsed.cache()
		h2p.writeNode(path, trienode.New(common.BytesToHash(hash), nodeToBytes(collapsed)), theTrie.owner)

		for idx, child := range current.Children {
			if child == nil {
				continue
			}
			childPath := append(path, byte(idx))
			if len(h2p.concurrentQueue)*2 < cap(h2p.concurrentQueue) {
				h2p.wg.Add(1)
				dst := make([]byte, len(childPath))
				copy(dst, childPath)
				go h2p.SubConcurrentTraversal(theTrie, child, dst)
			} else {
				h2p.ConcurrentTraversal(theTrie, child, childPath)
			}
		}
	case hashNode:
		n, err := theTrie.resloveWithoutTrack(current, path)
		if err != nil {
			fmt.Printf("Resolve HashNode error: %v, TrieRoot: %v, Path: %v\n", err, theTrie.Hash().String(), path)
			return
		}
		h2p.ConcurrentTraversal(theTrie, n, path)
		return
	case valueNode:
		if !hasTerm(path) {
			log.Info("ValueNode miss path term", "path", common.Bytes2Hex(path))
			break
		}
		var account types.StateAccount
		if err := rlp.Decode(bytes.NewReader(current), &account); err != nil {
			// log.Info("Rlp decode account failed.", "err", err)
			break
		}
		if account.Root == (common.Hash{}) || account.Root == types.EmptyRootHash {
			// log.Info("Not a storage trie.", "account", common.BytesToHash(path).String())
			break
		}

		ownerAddress := common.BytesToHash(hexToCompact(path))
		tr, err := New(StorageTrieID(h2p.stateRootHash, ownerAddress, account.Root), h2p.db)
		if err != nil {
			log.Error("New Storage trie error", "err", err, "root", account.Root.String(), "owner", ownerAddress.String())
			break
		}
		log.Info("Find Contract Trie Tree, rootHash: ", tr.Hash().String(), "")
		h2p.wg.Add(1)
		go h2p.SubConcurrentTraversal(tr, tr.root, []byte{})
	default:
		panic(errors.New("Invalid node type to traverse."))
	}
	return
}

// copy from trie/Commiter (*committer).commit
func (h2p *Hash2Path) commitChildren(path []byte, n *fullNode) [17]node {
	var children [17]node
	for i := 0; i < 16; i++ {
		child := n.Children[i]
		if child == nil {
			continue
		}
		// If it's the hashed child, save the hash value directly.
		// Note: it's impossible that the child in range [0, 15]
		// is a valueNode.
		if hn, ok := child.(hashNode); ok {
			children[i] = hn
			continue
		}
	}
	// For the 17th child, it's possible the type is valuenode.
	if n.Children[16] != nil {
		children[16] = n.Children[16]
	}
	return children
}
