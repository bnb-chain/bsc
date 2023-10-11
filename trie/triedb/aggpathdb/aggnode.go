package aggpathdb

import (
	"fmt"
	"io"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// AggNode is a basic structure for aggregate and store two layer trie node.
type AggNode struct {
	root   *trienode.Node
	childs [16]*trienode.Node
}

func DecodeAggNode(data []byte) (*AggNode, error) {
	aggNode := &AggNode{}
	err := aggNode.decodeFrom(data)
	if err != nil {
		return nil, err
	}
	return aggNode, nil
}

func EncodeAggNode(aggNode *AggNode) []byte {
	return aggNode.encodeTo()
}

func getAggNodePath(path []byte) []byte {
	if len(path)%2 == 0 {
		// even path
		return path
	} else {
		// odd path
		return path[:len(path)-1]
	}
}

func (n *AggNode) Update(path []byte, node *trienode.Node) {
	if len(path)%2 == 0 {
		n.root = node
	} else {
		i := path[len(path)-1]
		n.childs[int(i)] = node
	}
}

func (n *AggNode) Delete(path []byte) {
	if len(path)%2 == 0 {
		n.root = nil
	} else {
		i := path[len(path)-1]
		n.childs[int(i)] = nil
	}
}

func (n *AggNode) Node(path []byte) *trienode.Node {
	if len(path)%2 == 0 {
		return n.root
	} else {
		i := path[len(path)-1]
		return n.childs[int(i)]
	}
}

func (n *AggNode) decodeFrom(buf []byte) error {
	if len(buf) == 0 {
		return io.ErrUnexpectedEOF
	}

	elems, _, err := rlp.SplitList(buf)
	if err != nil {
		return fmt.Errorf("decode error: %v", err)
	}

	if c, _ := rlp.CountValues(elems); c != 17 {
		return fmt.Errorf("Invalid number of list elements: %v", c)
	}

	var rest []byte
	n.root, rest, err = decodeNode(elems)
	if err != nil {
		return fmt.Errorf("decode root Node failed in AggNode: %v", err)
	}

	for i := 0; i < 16; i++ {
		var cn *trienode.Node
		cn, rest, err = decodeNode(rest)
		if err != nil {
			return fmt.Errorf("decode childs Node(%d) failed in AggNode: %v", i, err)
		}
		n.childs[i] = cn
	}
	return nil
}

func (n *AggNode) encodeTo() []byte {
	w := rlp.NewEncoderBuffer(nil)
	offset := w.List()

	writeNode(w, n.root)
	for _, c := range n.childs {
		writeNode(w, c)
	}
	w.ListEnd(offset)
	result := w.ToBytes()
	w.Flush()
	return result
}

func writeNode(w rlp.EncoderBuffer, n *trienode.Node) {
	if n == nil {
		w.Write(rlp.EmptyString)
	} else {
		w.WriteBytes(n.Blob)
	}
}

func decodeNode(buf []byte) (*trienode.Node, []byte, error) {
	kind, val, rest, err := rlp.Split(buf)
	if err != nil {
		return nil, buf, err
	}

	if kind == rlp.String && len(val) == 0 {
		return nil, rest, nil
	}

	// Hashes are not calculated here to avoid unnecessary overhead
	return trienode.New(common.Hash{}, val), rest, nil
}

// aggregateAndWriteNodes will aggregate the trienode into trie aggnode and persist into the database
// Note this function will inject all the clean aggNode into the cleanCache
func aggregateAndWriteNodes(reader ethdb.KeyValueReader, batch ethdb.Batch, nodes map[common.Hash]map[string]*trienode.Node,
	clean *fastcache.Cache) (total int) {
	// pre-aggregate the node
	// Note: temporary impl. When a node writes to a diskLayer, it should be aggregated to aggNode.
	changeSets := make(map[common.Hash]map[string]map[string]*trienode.Node)
	for owner, subset := range nodes {
		current, exist := changeSets[owner]
		if !exist {
			current = make(map[string]map[string]*trienode.Node)
			changeSets[owner] = current
		}
		for path, n := range subset {
			aggPath := getAggNodePath([]byte(path))
			aggChangeSet, exist := changeSets[owner][string(aggPath)]
			if !exist {
				aggChangeSet = make(map[string]*trienode.Node)
			}
			aggChangeSet[path] = n
			changeSets[owner][string(aggPath)] = aggChangeSet
		}
	}

	for owner, subset := range changeSets {
		for aggPath, cs := range subset {
			aggNode := getOrNewAggNode(reader, clean, owner, []byte(aggPath))
			for path, n := range cs {
				if n.IsDeleted() {
					aggNode.Delete([]byte(path))
				} else {
					aggNode.Update([]byte(path), n)
				}
			}
			aggNodeBytes := EncodeAggNode(aggNode)
			writeAggNode(batch, []byte(aggPath), owner, aggNodeBytes)
			if len(aggPath) == 0 {
				fmt.Println("root Hash: ", aggNode.root.Hash.String())
			}
			fmt.Println("WriteAggNode, aggpath: ", common.Bytes2Hex([]byte(aggPath)))
			if clean != nil {
				clean.Set(cacheKey(owner, []byte(aggPath)), aggNodeBytes)
			}
			total++
		}
	}
	return total
}

func getOrNewAggNode(reader ethdb.KeyValueReader, clean *fastcache.Cache, owner common.Hash, aggPath []byte) *AggNode {
	aggNode, err := getAggNodeFromCacheOrDiskDB(reader, clean, owner, aggPath)
	if err != nil {
		panic("must get or load aggNode failed")
	}
	if aggNode == nil {
		return &AggNode{}
	}
	return aggNode
}

func writeAggNode(batch ethdb.Batch, aggPath []byte, owner common.Hash, aggNodeBytes []byte) {
	if owner == (common.Hash{}) {
		rawdb.WriteAccountTrieAggNode(batch, aggPath, aggNodeBytes)
	} else {
		rawdb.WriteStorageTrieAggNode(batch, owner, aggPath, aggNodeBytes)
	}
}

// getAggNodeFromCacheOrDiskDB read the aggnode from the clean cache (if hit) or database
func getAggNodeFromCacheOrDiskDB(reader ethdb.KeyValueReader, clean *fastcache.Cache, owner common.Hash, aggPath []byte) (*AggNode, error) {
	var val []byte

	cacheHit := false
	if clean != nil {
		val, cacheHit = clean.HasGet(nil, aggPath)
	}

	if !cacheHit {
		if owner == (common.Hash{}) {
			val = rawdb.ReadAccountTrieAggNode(reader, aggPath)
		} else {
			val = rawdb.ReadStorageTrieAggNode(reader, owner, aggPath)
		}
	}

	// not found
	if val == nil {
		return nil, nil
	}
	return DecodeAggNode(val)
}

func ReadTrieNode(reader ethdb.KeyValueReader, owner common.Hash, path []byte) ([]byte, common.Hash) {
	aggPath := getAggNodePath(path)
	var aggNodeBytes []byte
	if owner == (common.Hash{}) {
		aggNodeBytes = rawdb.ReadAccountTrieAggNode(reader, aggPath)
	} else {
		aggNodeBytes = rawdb.ReadStorageTrieAggNode(reader, owner, aggPath)
	}

	fmt.Println("read trie aggnode, aggpath: ", common.Bytes2Hex(aggPath), "bytes: ", len(aggNodeBytes))

	if aggNodeBytes == nil {
		return nil, common.Hash{}
	}

	aggNode, err := DecodeAggNode(aggNodeBytes)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}

	node := aggNode.Node(path)
	h := newHasher()
	defer h.release()
	node.Hash = h.hash(node.Blob)
	fmt.Println("read trie node, node hash: ", node.Hash.String())

	return node.Blob, node.Hash
}

func DeleteTrieNode(db ethdb.KeyValueStore, owner common.Hash, path []byte) {
	aggPath := getAggNodePath(path)
	var aggNodeBytes []byte
	if owner == (common.Hash{}) {
		aggNodeBytes = rawdb.ReadAccountTrieAggNode(db, aggPath)
	} else {
		aggNodeBytes = rawdb.ReadStorageTrieAggNode(db, owner, aggPath)
	}

	fmt.Println("read trie aggnode, aggpath: ", common.Bytes2Hex(aggPath), "bytes: ", len(aggNodeBytes))

	if aggNodeBytes == nil {
		return
	}

	aggNode, err := DecodeAggNode(aggNodeBytes)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}
	aggNode.Delete(path)

	if owner == (common.Hash{}) {
		rawdb.WriteAccountTrieAggNode(db, aggPath, aggNode.encodeTo())
	} else {
		rawdb.WriteStorageTrieAggNode(db, owner, aggPath, aggNode.encodeTo())
	}
}
