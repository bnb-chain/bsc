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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package rawdb

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/sha3"
)

// HashScheme is the legacy hash-based state scheme with which trie nodes are
// stored in the disk with node hash as the database key. The advantage of this
// scheme is that different versions of trie nodes can be stored in disk, which
// is very beneficial for constructing archive nodes. The drawback is it will
// store different trie nodes on the same path to different locations on the disk
// with no data locality, and it's unfriendly for designing state pruning.
//
// Now this scheme is still kept for backward compatibility, and it will be used
// for archive node and some other tries(e.g. light trie).
const HashScheme = "hash"

// PathScheme is the new path-based state scheme with which trie nodes are stored
// in the disk with node path as the database key. This scheme will only store one
// version of state data in the disk, which means that the state pruning operation
// is native. At the same time, this scheme will put adjacent trie nodes in the same
// area of the disk with good data locality property. But this scheme needs to rely
// on extra state diffs to survive deep reorg.
const PathScheme = "path"

// AggPathScheme is the new state scheme based on PathScheme. This scheme merge two
// or more layers trie nodes and aims to improve the performance and reduce the disk space
// of MPT.
const AggPathScheme = "aggpath"

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

// ReadAccountTrieNode retrieves the account trie node and the associated node
// hash with the specified node path.
func ReadAccountTrieNode(db ethdb.KeyValueReader, path []byte) ([]byte, common.Hash) {
	data, err := db.Get(accountTrieNodeKey(path))
	if err != nil {
		return nil, common.Hash{}
	}
	h := newHasher()
	defer h.release()
	return data, h.hash(data)
}

// HasAccountTrieNode checks the account trie node presence with the specified
// node path and the associated node hash.
func HasAccountTrieNode(db ethdb.KeyValueReader, path []byte, hash common.Hash) bool {
	data, err := db.Get(accountTrieNodeKey(path))
	if err != nil {
		return false
	}
	h := newHasher()
	defer h.release()
	return h.hash(data) == hash
}

// ExistsAccountTrieNode checks the presence of the account trie node with the
// specified node path, regardless of the node hash.
func ExistsAccountTrieNode(db ethdb.KeyValueReader, path []byte) bool {
	has, err := db.Has(accountTrieNodeKey(path))
	if err != nil {
		return false
	}
	return has
}

// WriteAccountTrieNode writes the provided account trie node into database.
func WriteAccountTrieNode(db ethdb.KeyValueWriter, path []byte, node []byte) {
	if err := db.Put(accountTrieNodeKey(path), node); err != nil {
		log.Crit("Failed to store account trie node", "err", err)
	}
}

// DeleteAccountTrieNode deletes the specified account trie node from the database.
func DeleteAccountTrieNode(db ethdb.KeyValueWriter, path []byte) {
	if err := db.Delete(accountTrieNodeKey(path)); err != nil {
		log.Crit("Failed to delete account trie node", "err", err)
	}
}

// ReadStorageTrieNode retrieves the storage trie node and the associated node
// hash with the specified node path.
func ReadStorageTrieNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) ([]byte, common.Hash) {
	data, err := db.Get(storageTrieNodeKey(accountHash, path))
	if err != nil {
		return nil, common.Hash{}
	}
	h := newHasher()
	defer h.release()
	return data, h.hash(data)
}

// HasStorageTrieNode checks the storage trie node presence with the provided
// node path and the associated node hash.
func HasStorageTrieNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte, hash common.Hash) bool {
	data, err := db.Get(storageTrieNodeKey(accountHash, path))
	if err != nil {
		return false
	}
	h := newHasher()
	defer h.release()
	return h.hash(data) == hash
}

// ExistsStorageTrieNode checks the presence of the storage trie node with the
// specified account hash and node path, regardless of the node hash.
func ExistsStorageTrieNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) bool {
	has, err := db.Has(storageTrieNodeKey(accountHash, path))
	if err != nil {
		return false
	}
	return has
}

// WriteStorageTrieNode writes the provided storage trie node into database.
func WriteStorageTrieNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte, node []byte) {
	if err := db.Put(storageTrieNodeKey(accountHash, path), node); err != nil {
		log.Crit("Failed to store storage trie node", "err", err)
	}
}

// DeleteStorageTrieNode deletes the specified storage trie node from the database.
func DeleteStorageTrieNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte) {
	if err := db.Delete(storageTrieNodeKey(accountHash, path)); err != nil {
		log.Crit("Failed to delete storage trie node", "err", err)
	}
}

// ReadAccountTrieAggNode retrieves the account trie agg node and return the raw bytes
func ReadAccountTrieAggNode(db ethdb.KeyValueReader, path []byte) []byte {
	data, err := db.Get(accountTrieAggNodeKey(path))
	if err != nil {
		return nil
	}
	return data
}

// HasAccountTrieAggNode checks if the trie aggnode with the provided path is present in db.
func HasAccountTrieAggNode(db ethdb.KeyValueReader, path []byte) bool {
	ok, _ := db.Has(accountTrieAggNodeKey(path))
	return ok
}

// WriteAccountTrieAggNode writes the provided account trie aggnode into database.
func WriteAccountTrieAggNode(db ethdb.KeyValueWriter, path []byte, node []byte) {
	if err := db.Put(accountTrieAggNodeKey(path), node); err != nil {
		log.Crit("Failed to store account trie agg node", "err", err)
	}
}

// DeleteAccountTrieAggNode deletes the specified account trie node from the database.
func DeleteAccountTrieAggNode(db ethdb.KeyValueWriter, path []byte) {
	if err := db.Delete(accountTrieAggNodeKey(path)); err != nil {
		log.Crit("Failed to delete account trie agg node", "err", err)
	}
}

// ReadStorageTrieAggNode retrieves the storage trie agg node and return the raw bytes
func ReadStorageTrieAggNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) []byte {
	data, err := db.Get(storageTrieAggNodeKey(accountHash, path))
	if err != nil {
		return nil
	}
	return data
}

// HasAccountTrieAggNode checks if the trie aggnode with the provided path is present in db.
func HasStorageTrieAggNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) bool {
	ok, _ := db.Has(storageTrieAggNodeKey(accountHash, path))
	return ok
}

// DeleteStorageTrieAggNode deletes the specified storage trie node from the database.
func DeleteStorageTrieAggNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte) {
	if err := db.Delete(storageTrieAggNodeKey(accountHash, path)); err != nil {
		log.Crit("Failed to delete storage trie agg node", "err", err)
	}
}

// WriteStorageTrieAggNode writes the priovided storage trie aggnode into database
func WriteStorageTrieAggNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte, node []byte) {
	if err := db.Put(storageTrieAggNodeKey(accountHash, path), node); err != nil {
		log.Crit("Failed to store account trie agg node", "err", err)
	}
}

// ReadLegacyTrieNode retrieves the legacy trie node with the given
// associated node hash.
func ReadLegacyTrieNode(db ethdb.KeyValueReader, hash common.Hash) []byte {
	data, err := db.Get(hash.Bytes())
	if err != nil {
		return nil
	}
	return data
}

// HasLegacyTrieNode checks if the trie node with the provided hash is present in db.
func HasLegacyTrieNode(db ethdb.KeyValueReader, hash common.Hash) bool {
	ok, _ := db.Has(hash.Bytes())
	return ok
}

// WriteLegacyTrieNode writes the provided legacy trie node to database.
func WriteLegacyTrieNode(db ethdb.KeyValueWriter, hash common.Hash, node []byte) {
	if err := db.Put(hash.Bytes(), node); err != nil {
		log.Crit("Failed to store legacy trie node", "err", err)
	}
}

// DeleteLegacyTrieNode deletes the specified legacy trie node from database.
func DeleteLegacyTrieNode(db ethdb.KeyValueWriter, hash common.Hash) {
	if err := db.Delete(hash.Bytes()); err != nil {
		log.Crit("Failed to delete legacy trie node", "err", err)
	}
}

// ReadStateScheme reads the state scheme of persistent state, or none
// if the state is not present in database.
func ReadStateScheme(db ethdb.Reader) string {
	// Check if state in path-based scheme is present
	blob, _ := ReadAccountTrieNode(db, nil)
	if len(blob) != 0 {
		return PathScheme
	}
	// Check if state in aggregated-path-based scheme is present
	blob = ReadAccountTrieAggNode(db, nil)
	if len(blob) != 0 {
		return AggPathScheme
	}
	// In a hash-based scheme, the genesis state is consistently stored
	// on the disk. To assess the scheme of the persistent state, it
	// suffices to inspect the scheme of the genesis state.
	header := ReadHeader(db, ReadCanonicalHash(db, 0), 0)
	if header == nil {
		return "" // empty datadir
	}
	blob = ReadLegacyTrieNode(db, header.Root)
	if len(blob) == 0 {
		return "" // no state in disk
	}
	return HashScheme
}
