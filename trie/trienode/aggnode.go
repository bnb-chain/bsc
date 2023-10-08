package trienode

import (
	"fmt"
	"io"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

// AggNode is a basic structure for aggregate and store two layer trie node.
type AggNode struct {
	root   *Node
	childs [16]*Node
}

func DecodeAggNode(data []byte) (*AggNode, error) {
	aggNode := &AggNode{}
	err := aggNode.decodeFrom(data)
	if err != nil {
		return nil, err
	}
	return aggNode, nil
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
		var cn *Node
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

func writeNode(w rlp.EncoderBuffer, n *Node) {
	if n == nil {
		w.Write(rlp.EmptyString)
	} else {
		w.WriteBytes(n.Blob)
	}
}

func decodeNode(buf []byte) (*Node, []byte, error) {
	kind, val, rest, err := rlp.Split(buf)
	if err != nil {
		return nil, buf, err
	}

	if kind == rlp.String && len(val) == 0 {
		return nil, rest, nil
	}

	h := newHasher()
	defer h.release()
	return New(h.hash(val), val), rest, nil
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
