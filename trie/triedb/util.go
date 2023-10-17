package triedb

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/triedb/aggpathdb"
)

// HasTrieNode checks the trie node presence with the provided node info and
// the associated node hash.
func HasTrieNode(db ethdb.KeyValueReader, owner common.Hash, path []byte, hash common.Hash, scheme string) bool {
	switch scheme {
	case rawdb.HashScheme:
		return rawdb.HasLegacyTrieNode(db, hash)
	case rawdb.PathScheme:
		if owner == (common.Hash{}) {
			return rawdb.HasAccountTrieNode(db, path, hash)
		}
		return rawdb.HasStorageTrieNode(db, owner, path, hash)
	case rawdb.AggPathScheme:
		return aggpathdb.HasTrieNodeInAggNode(db, owner, path)
	default:
		panic(fmt.Sprintf("Unknown scheme %v", scheme))
	}
}

// ReadTrieNode retrieves the trie node from database with the provided node info
// and associated node hash.
// hashScheme-based lookup requires the following:
//   - hash
//
// pathScheme-based lookup requires the following:
//   - owner
//   - path
//
// aggPath Scheme-based lookup requires the following:
//   - owner
//   - path
func ReadTrieNode(db ethdb.KeyValueReader, owner common.Hash, path []byte, hash common.Hash, scheme string) []byte {
	switch scheme {
	case rawdb.HashScheme:
		return rawdb.ReadLegacyTrieNode(db, hash)
	case rawdb.PathScheme:
		var (
			blob  []byte
			nHash common.Hash
		)
		if owner == (common.Hash{}) {
			blob, nHash = rawdb.ReadAccountTrieNode(db, path)
		} else {
			blob, nHash = rawdb.ReadStorageTrieNode(db, owner, path)
		}
		if nHash != hash {
			return nil
		}
		return blob
	case rawdb.AggPathScheme:
		blob, nHash := aggpathdb.ReadTrieNodeFromAggNode(db, owner, path)
		if nHash != hash {
			return nil
		}
		return blob
	default:
		panic(fmt.Sprintf("Unknown scheme %v", scheme))
	}
}

// WriteTrieNode writes the trie node into database with the provided node info
// and associated node hash.
// hashScheme-based lookup requires the following:
//   - hash
//
// pathScheme-based lookup requires the following:
//   - owner
//   - path
//
// aggPath Scheme-based lookup requires the following:
//   - owner
//   - path
func WriteTrieNode(db ethdb.KeyValueWriter, reader ethdb.KeyValueReader, owner common.Hash, path []byte, hash common.Hash, node []byte, scheme string) {
	switch scheme {
	case rawdb.HashScheme:
		rawdb.WriteLegacyTrieNode(db, hash, node)
	case rawdb.PathScheme:
		if owner == (common.Hash{}) {
			rawdb.WriteAccountTrieNode(db, path, node)
		} else {
			rawdb.WriteStorageTrieNode(db, owner, path, node)
		}
	case rawdb.AggPathScheme:
		aggpathdb.WriteTrieNodeFromAggNode(db, reader, owner, path, node)
	default:
		panic(fmt.Sprintf("Unknown scheme %v", scheme))
	}
}

// DeleteTrieNode deletes the trie node from database with the provided node info
// and associated node hash.
// hashScheme-based lookup requires the following:
//   - hash
//
// pathScheme-based lookup requires the following:
//   - owner
//   - path
//
// aggPath Scheme-based lookup requires the following:
//   - owner
//   - path
func DeleteTrieNode(writer ethdb.KeyValueWriter, reader ethdb.KeyValueReader, owner common.Hash, path []byte, hash common.Hash, scheme string) {
	switch scheme {
	case rawdb.HashScheme:
		rawdb.DeleteLegacyTrieNode(writer, hash)
	case rawdb.PathScheme:
		if owner == (common.Hash{}) {
			rawdb.DeleteAccountTrieNode(writer, path)
		} else {
			rawdb.DeleteStorageTrieNode(writer, owner, path)
		}
	case rawdb.AggPathScheme:
		aggpathdb.DeleteTrieNodeFromAggNode(writer, reader, owner, path)
	default:
		panic(fmt.Sprintf("Unknown scheme %v", scheme))
	}
}
