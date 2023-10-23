// Copyright 2015 The go-ethereum Authors
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

package trie

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/trienode"

	"github.com/ethereum/go-ethereum/common"
)

type EmptyTrie struct{}

// NewSecure creates a dummy trie
func NewEmptyTrie() *EmptyTrie {
	return &EmptyTrie{}
}

func (t *EmptyTrie) GetKey(shaKey []byte) []byte {
	return nil
}

func (t *EmptyTrie) GetStorage(_ common.Address, key []byte) ([]byte, error) {
	return nil, nil
}

func (t *EmptyTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
	return nil, nil
}

func (t *EmptyTrie) UpdateStorage(_ common.Address, key, value []byte) error {
	return nil
}

// TryUpdateAccount abstract an account write in the trie.
func (t *EmptyTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
	return nil
}

func (t *EmptyTrie) UpdateContractCode(_ common.Address, _ common.Hash, _ []byte) error {
	return nil
}

func (t *EmptyTrie) DeleteStorage(_ common.Address, key []byte) error {
	return nil
}

func (t *EmptyTrie) DeleteAccount(address common.Address) error {
	return nil
}

func (t *EmptyTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

func (t *EmptyTrie) Hash() common.Hash {
	return common.Hash{}
}

// NodeIterator returns an iterator that returns nodes of the underlying trie. Iteration
// starts at the key after the given start key.
func (t *EmptyTrie) NodeIterator(startKey []byte) (NodeIterator, error) {
	return nil, nil
}

func (t *EmptyTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	return nil
}

// Copy returns a copy of SecureTrie.
func (t *EmptyTrie) Copy() *EmptyTrie {
	cpy := *t
	return &cpy
}
