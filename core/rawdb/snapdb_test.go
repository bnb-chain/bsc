package rawdb

import (
	"bytes"
	"testing"
)

func shardIndex_bytes_hasprefix(key []byte, shardNum int) int {
	// SnapshotAccountPrefix + account hash -> account trie value
	if bytes.HasPrefix(key, SnapshotAccountPrefix) {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]) % shardNum
	}
	// SnapshotStoragePrefix + account hash + storage hash -> storage trie value
	if bytes.HasPrefix(key, SnapshotStoragePrefix) {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]) % shardNum
	}
	// some metadata, journal save in shard0
	// such as snapshotDisabledKey, SnapshotRootKey, snapshotGeneratorKey, snapshotJournalKey, etc.
	return 0
}

func shardIndex_bytes_comparison(key []byte, shardNum int) int {
	if len(key) < 1 {
		return 0
	}
	// SnapshotAccountPrefix + account hash -> account trie value
	if SnapshotAccountPrefix[0] == key[0] {
		if len(key) < 2 {
			return 0
		}
		return int(key[1]) % shardNum
	}
	// SnapshotStoragePrefix + account hash + storage hash -> storage trie value
	if SnapshotStoragePrefix[0] == key[0] {
		if len(key) < 34 {
			return 0
		}
		return int(key[33]) % shardNum
	}
	// some metadata, journal save in shard0
	// such as snapshotDisabledKey, SnapshotRootKey, snapshotGeneratorKey, snapshotJournalKey, etc.
	return 0
}

func BenchmarkSnapShardingDB_ShardIndex_bytes_hasprefix(b *testing.B) {
	keys := [][]byte{
		append(SnapshotAccountPrefix, []byte("key-001")...),
		append(SnapshotStoragePrefix, []byte("key-002")...),
		append(SnapshotAccountPrefix, []byte("key-003")...),
		append(SnapshotStoragePrefix, []byte("key-004")...),
		[]byte("key-005"),
		[]byte("key-006"),
	}
	keys_len := len(keys)

	for i := 0; i < b.N; i++ {
		shardIndex_bytes_hasprefix(keys[i%keys_len], 8)
	}
}
func BenchmarkSnapShardingDB_ShardIndex_bytes_comparison(b *testing.B) {
	keys := [][]byte{
		append(SnapshotAccountPrefix, []byte("key-001")...),
		append(SnapshotStoragePrefix, []byte("key-002")...),
		append(SnapshotAccountPrefix, []byte("key-003")...),
		append(SnapshotStoragePrefix, []byte("key-004")...),
		[]byte("key-005"),
		[]byte("key-006"),
	}
	keys_len := len(keys)

	for i := 0; i < b.N; i++ {
		shardIndex_bytes_comparison(keys[i%keys_len], 8)
	}
}
