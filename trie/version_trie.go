package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

// VersionTrie is a wrapper around version state that implements the trie.Trie
// interface so that version trees can be reused verbatim.
type VersionTrie struct {
	db     database.Database
	reader *trieReader
	//tree  TreeHandler
}

// NewVersionTrie constructs a version state tree based on the specified root hash.
func NewVersionTrie(root common.Hash, db database.Database) (*VersionTrie, error) {
	reader, err := newTrieReader(root, common.Hash{}, db)
	if err != nil {
		return nil, err
	}

	// TODO
	// Open a tree
	// tree, err := OpenTree(state StateHandler, version int64, owner common.Hash, root common.Hash)
	if err != nil {
		return nil, err
	}

	return &VersionTrie{
		db:     db,
		reader: reader,
		// tree: tree,
	}, nil
}

// GetKey returns the sha3 preimage of a hashed key that was previously used
// to store a value.
func (t *VersionTrie) GetKey(key []byte) []byte {
	return key
}

// GetAccount implements state.Trie, retrieving the account with the specified
// account address. If the specified account is not in the tree, nil will
// be returned. If the tree is corrupted, an error will be returned.
func (t *VersionTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
	// TODO
}

// GetStorage returns the value for key stored in the trie. The value bytes
// must not be modified by the caller. If a node was not found in the database,
// a trie.MissingNodeError is returned.
func (t *VersionTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	// TODO
}

// UpdateAccount abstracts an account write to the trie. It encodes the
// provided account object with associated algorithm and then updates it
// in the trie with provided address.
func (t *VersionTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
	// TODO
}

// UpdateStorage associates key with value in the trie. If value has length zero,
// any existing value is deleted from the trie. The value bytes must not be modified
// by the caller while they are stored in the trie. If a node was not found in the
// database, a trie.MissingNodeError is returned.
func (t *VersionTrie) UpdateStorage(addr common.Address, key, value []byte) error {
	// TODO
}

// DeleteAccount abstracts an account deletion from the trie.
func (t *VersionTrie) DeleteAccount(address common.Address) error {
	// TODO
}

// DeleteStorage removes any existing value for key from the trie. If a node
// was not found in the database, a trie.MissingNodeError is returned.
func (t *VersionTrie) DeleteStorage(addr common.Address, key []byte) error {
	// TODO
}

// UpdateContractCode abstracts code write to the trie. It is expected
// to be moved to the stateWriter interface when the latter is ready.
func (t *VersionTrie) UpdateContractCode(address common.Address, codeHash common.Hash, code []byte) error {
	// TODO
}

// Hash returns the root hash of the trie. It does not write to the database and
// can be used even if the trie doesn't have one.
func (t *VersionTrie) Hash() common.Hash {
	// TODO
}

// Commit collects all dirty nodes in the trie and replace them with the
// corresponding node hash.
func (t *VersionTrie) Commit(collectLeaf bool) (common.Hash, error) {
	// TODO
}

// NodeIterator returns an iterator that returns nodes of the trie. Iteration
// starts at the key after the given start key. And error will be returned
// if fails to create node iterator.
func (t *VersionTrie) NodeIterator(startKey []byte) (trie.NodeIterator, error) {
	// TODO
}

// Prove constructs a Merkle proof for key. The result contains all encoded nodes
// on the path to the value at key. The value itself is also included in the last
// node and can be retrieved by verifying the proof.
//
// If the trie does not contain a value for key, the returned proof contains all
// nodes of the longest existing prefix of the key (at least the root), ending
// with the node that proves the absence of the key.
func (t *VersionTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	// TODO
}
