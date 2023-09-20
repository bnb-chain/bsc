package trienode

// AggNode is a basic structure for aggregate and store two layer trie node.
type AggNode struct {
	root   *Node
	childs [16]Node
}

func (n *AggNode) DecodeFrom([]byte) {

}

func (n *AggNode) EncodeTo() []byte {
	return nil
}
