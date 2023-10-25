package aggpathdb

import (
	"fmt"
	"io"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// AggNode is a basic structure for aggregate and store two layer trie node.
type AggNode struct {
	root    *trienode.Node
	childes [16]*trienode.Node
}

func DecodeAggNode(data []byte) (*AggNode, error) {
	aggNode := &AggNode{}
	err := aggNode.decodeFrom(data)
	if err != nil {
		return nil, err
	}
	return aggNode, nil
}

func toAggPath(path []byte) []byte {
	if len(path)%2 == 0 {
		// even path
		return path
	} else {
		// odd path
		return path[:len(path)-1]
	}
}

func (n *AggNode) Empty() bool {
	return reflect.DeepEqual(n, AggNode{})
}

func (n *AggNode) Size() int {
	size := 0
	if n.root != nil {
		size += len(n.root.Blob)
	}
	for _, c := range n.childes {
		if c != nil {
			size += len(c.Blob)
		}
	}
	return size
}

func (n *AggNode) Update(path []byte, node *trienode.Node) {
	if len(path)%2 == 0 {
		n.root = node
	} else {
		i := path[len(path)-1]
		n.childes[int(i)] = node
	}
}

func (n *AggNode) Delete(path []byte) {
	if len(path)%2 == 0 {
		n.root = nil
	} else {
		i := path[len(path)-1]
		n.childes[int(i)] = nil
	}
}

func (n *AggNode) Has(path []byte) bool {
	if len(path)%2 == 0 {
		return n.root == nil
	} else {
		i := path[len(path)-1]
		return n.childes[int(i)] == nil
	}
}

func (n *AggNode) Node(path []byte) *trienode.Node {
	if len(path)%2 == 0 {
		return n.root
	} else {
		i := path[len(path)-1]
		return n.childes[int(i)]
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
		return fmt.Errorf("invalid number of list elements: %v", c)
	}

	var rest []byte
	n.root, rest, err = decodeRawNode(elems)
	if err != nil {
		return fmt.Errorf("decode root Node failed in AggNode: %v", err)
	}

	for i := 0; i < 16; i++ {
		var cn *trienode.Node
		cn, rest, err = decodeRawNode(rest)
		if err != nil {
			return fmt.Errorf("decode childes Node(%d) failed in AggNode: %v", i, err)
		}
		n.childes[i] = cn
	}
	return nil
}

func (n *AggNode) encodeTo() []byte {
	w := rlp.NewEncoderBuffer(nil)
	offset := w.List()

	if n.root != nil {
		writeRawNode(w, n.root.Blob)
	} else {
		writeRawNode(w, nil)
	}
	for _, c := range n.childes {
		if c != nil {
			writeRawNode(w, c.Blob)
		} else {
			writeRawNode(w, nil)
		}
	}
	w.ListEnd(offset)
	result := w.ToBytes()
	w.Flush()
	return result
}

func writeRawNode(w rlp.EncoderBuffer, n []byte) {
	if n == nil {
		w.Write(rlp.EmptyString)
	} else {
		w.WriteBytes(n)
	}
}

func decodeRawNode(buf []byte) (*trienode.Node, []byte, error) {
	kind, val, rest, err := rlp.Split(buf)
	if err != nil {
		return nil, buf, err
	}

	if kind == rlp.String && len(val) == 0 {
		return nil, rest, nil
	}

	// Hashes are not calculated here to avoid unnecessary overhead
	return &trienode.Node{Blob: val}, rest, nil
}

func writeAggNode(db ethdb.KeyValueWriter, owner common.Hash, aggPath []byte, aggNodeBytes []byte) {
	if owner == (common.Hash{}) {
		rawdb.WriteAccountTrieAggNode(db, aggPath, aggNodeBytes)
	} else {
		rawdb.WriteStorageTrieAggNode(db, owner, aggPath, aggNodeBytes)
	}
}

func deleteAggNode(db ethdb.KeyValueWriter, owner common.Hash, aggPath []byte) {
	if owner == (common.Hash{}) {
		rawdb.DeleteAccountTrieNode(db, aggPath)
	} else {
		rawdb.DeleteStorageTrieNode(db, owner, aggPath)
	}
}

func loadAggNodeFromDatabase(db ethdb.KeyValueReader, owner common.Hash, aggPath []byte) (*AggNode, error) {
	var blob []byte
	if owner == (common.Hash{}) {
		blob = rawdb.ReadAccountTrieAggNode(db, aggPath)
	} else {
		blob = rawdb.ReadStorageTrieAggNode(db, owner, aggPath)
	}

	if blob == nil {
		return nil, nil
	}

	return DecodeAggNode(blob)
}

func ReadTrieNodeFromAggNode(reader ethdb.KeyValueReader, owner common.Hash, path []byte) ([]byte, common.Hash) {
	aggPath := toAggPath(path)
	aggNode, err := loadAggNodeFromDatabase(reader, owner, aggPath)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}
	if aggNode == nil {
		return nil, common.Hash{}
	}

	node := aggNode.Node(path)

	if node.Hash == (common.Hash{}) && len(node.Blob) != 0 {
		h := newHasher()
		defer h.release()
		node.Hash = h.hash(node.Blob)
	}
	return node.Blob, node.Hash
}

func DeleteTrieNodeFromAggNode(writer ethdb.KeyValueWriter, reader ethdb.KeyValueReader, owner common.Hash, path []byte) {
	aggPath := toAggPath(path)
	aggNode, err := loadAggNodeFromDatabase(reader, owner, aggPath)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}
	if aggNode == nil {
		return
	}
	aggNode.Delete(path)

	if aggNode.Empty() {
		deleteAggNode(writer, owner, aggPath)
	} else {
		writeAggNode(writer, owner, aggPath, aggNode.encodeTo())
	}
}

func WriteTrieNodeWithAggNode(writer ethdb.KeyValueWriter, reader ethdb.KeyValueReader, owner common.Hash, path []byte, node []byte) {
	aggPath := toAggPath(path)
	aggNode, err := loadAggNodeFromDatabase(reader, owner, aggPath)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}
	if aggNode == nil {
		return
	}
	aggNode.Update(path, &trienode.Node{Blob: node})

	writeAggNode(writer, owner, aggPath, aggNode.encodeTo())
}

func HasTrieNodeInAggNode(db ethdb.KeyValueReader, owner common.Hash, path []byte) bool {
	aggPath := toAggPath(path)
	aggNode, err := loadAggNodeFromDatabase(db, owner, aggPath)
	if err != nil {
		panic(fmt.Sprintf("Decode account trie node failed. error: %v", err))
	}
	if aggNode == nil {
		return false
	}
	return aggNode.Has(path)
}
