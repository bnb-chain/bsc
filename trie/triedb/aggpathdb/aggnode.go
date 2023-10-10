package aggpathdb

import (
	"fmt"
	"io"

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

func (n *AggNode) Add(path []byte, node *trienode.Node) {
	if len(path)%2 == 0 {
		n.root = node
	} else {
		i := path[len(path)-1]
		n.childs[int(i)] = node
	}
}

func EncodeAggNode(aggNode *AggNode) []byte {
	return aggNode.encodeTo()
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

	h := newHasher()
	defer h.release()
	return trienode.New(h.hash(val), val), rest, nil
}
