package shardingdb

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testShardIndexFunc(key []byte, shardNum int) int {
	if len(key) == 0 {
		return 0
	}
	return int(key[0]) % shardNum
}

func TestParseShardIndexes(t *testing.T) {
	tests := []struct {
		src      string
		shardNum int
		want     []int
		wantErr  bool
	}{
		{src: "0", shardNum: 8, want: []int{0}},
		{src: "0-7", shardNum: 8, want: []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{src: "0-1,6-7", shardNum: 8, want: []int{0, 1, 6, 7}},
		{src: "2,3,4,5", shardNum: 8, want: []int{2, 3, 4, 5}},
		{src: "-1,0", shardNum: 8, wantErr: true},
		{src: "7-0", shardNum: 8, wantErr: true},
		{src: "0-7,8", shardNum: 8, wantErr: true},
		{src: "0->7", shardNum: 8, wantErr: true},
		{src: "1,,2", shardNum: 8, wantErr: true},
	}
	for _, test := range tests {
		got, err := parseShardIndexes(test.src, test.shardNum)
		if test.wantErr {
			assert.Error(t, err)
			continue
		}
		assert.NoError(t, err)
		assert.Equal(t, test.want, got)
	}
}

func TestShardConfig(t *testing.T) {
	tests := []struct {
		src  *Config
		want []ShardConfig
	}{
		{
			src: &Config{
				ShardNum: 4,
				DBPath:   "/disk0/geth/state/",
				Shards: []ShardConfig{
					{
						DBPath:  "/disk0/state/",
						Indexes: "0-2",
					},
					{
						DBPath:  "/disk1/state/",
						Indexes: "3",
					},
				},
				EnableSharding: true,
			},
			want: []ShardConfig{
				0: {DBPath: "/disk0/state/shard0000"},
				1: {DBPath: "/disk0/state/shard0001"},
				2: {DBPath: "/disk0/state/shard0002"},
				3: {DBPath: "/disk1/state/shard0003"},
			},
		},
		{
			src: &Config{
				ShardNum: 2,
				DBPath:   "/disk0/geth/state/",
				Shards: []ShardConfig{
					{
						DBPath:  "/disk0/state/",
						Indexes: "0,1",
					},
				},
				EnableSharding: true,
			},
			want: []ShardConfig{
				{DBPath: "/disk0/state/shard0000"},
				{DBPath: "/disk0/state/shard0001"},
			},
		},
		{
			src: &Config{
				ShardNum: 3,
				DBPath:   "/disk0/geth/state/",
				Shards: []ShardConfig{
					{
						Indexes: "0-1",
					},
					{
						Indexes: "2",
					},
				},
				EnableSharding: true,
			},
			want: []ShardConfig{
				{DBPath: "/disk0/geth/state/shard0000"},
				{DBPath: "/disk0/geth/state/shard0001"},
				{DBPath: "/disk0/geth/state/shard0002"},
			},
		},
	}
	for _, test := range tests {
		got, err := test.src.parseShards()
		assert.NoError(t, err, "parseShards(%v) = %v", test.src, err)
		assert.Equal(t, test.want, got)
	}
}

func TestShardingDB_BasicSharding(t *testing.T) {
	cfg := &Config{
		CacheRatio:     40,
		Namespace:      "test",
		DBType:         "memorydb",
		ShardNum:       8,
		DBPath:         "/tmp/state/",
		EnableSharding: true,
		Shards: []ShardConfig{
			{Indexes: "0-7"},
		},
	}
	db, err := New(cfg, 1024, 1024, false, testShardIndexFunc)
	assert.NoError(t, err, "New(%v) = %v", cfg, err)
	assert.Equal(t, db.ShardNum(), 8)
	assert.Equal(t, db.shards[0], db.Shard([]byte{0}))
	assert.Equal(t, db.shards[1], db.Shard([]byte{1}))
	assert.Equal(t, db.shards[6], db.Shard([]byte{6}))
	assert.Equal(t, db.shards[7], db.Shard([]byte{7}))
}

// createMemoryShardingDB creates a sharding database with memory backends for testing
func createMemoryShardingDB(shardCount int) *Database {
	shards := make([]ethdb.KeyValueStore, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = memorydb.New()
	}
	return &Database{
		cfg:            &Config{ShardNum: shardCount},
		shards:         shards,
		shardIndexFunc: testShardIndexFunc,
	}
}

func TestShardingDatabase_BasicOperations(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Test Put/Get
	key := []byte("test-key")
	value := []byte("test-value")

	err := db.Put(key, value)
	require.NoError(t, err)

	got, err := db.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, got)

	// Test Has
	exists, err := db.Has(key)
	require.NoError(t, err)
	assert.True(t, exists)

	// Test Delete
	err = db.Delete(key)
	require.NoError(t, err)

	exists, err = db.Has(key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestShardingDatabase_ShardRouting(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Test keys route to different shards
	keys := [][]byte{
		{0x00}, {0x01}, {0x02}, {0x03}, // should go to shards 0,1,2,3
		{0x04}, {0x05}, {0x06}, {0x07}, // should go to shards 0,1,2,3 again
	}

	for i, key := range keys {
		expectedShard := i % 4
		actualShard := db.shardIndexFunc(key, db.ShardNum())
		assert.Equal(t, expectedShard, actualShard, "key %x should route to shard %d, got %d", key, expectedShard, actualShard)
	}

	// Test empty key routes to shard 0
	assert.Equal(t, 0, db.shardIndexFunc([]byte{}, db.ShardNum()))
	assert.Equal(t, 0, db.shardIndexFunc(nil, db.ShardNum()))
}

func TestShardingBatch_BasicOperations(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	batch := db.NewBatch()
	defer batch.Reset()

	// Test batch Put
	keys := [][]byte{
		{0x00, 0x01}, {0x01, 0x02}, {0x02, 0x03}, {0x03, 0x04},
	}
	values := [][]byte{
		[]byte("value0"), []byte("value1"), []byte("value2"), []byte("value3"),
	}

	for i, key := range keys {
		err := batch.Put(key, values[i])
		require.NoError(t, err)
	}

	// Test ValueSize
	expectedSize := 0
	for i, key := range keys {
		expectedSize += len(key) + len(values[i])
	}
	assert.Equal(t, expectedSize, batch.ValueSize())

	// Test Write
	err := batch.Write()
	require.NoError(t, err)

	// Verify data was written
	for i, key := range keys {
		got, err := db.Get(key)
		require.NoError(t, err)
		assert.Equal(t, values[i], got)
	}
}

func TestShardingBatch_Delete(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// First, put some data
	keys := [][]byte{{0x00}, {0x01}, {0x02}, {0x03}}
	values := [][]byte{[]byte("v0"), []byte("v1"), []byte("v2"), []byte("v3")}

	for i, key := range keys {
		err := db.Put(key, values[i])
		require.NoError(t, err)
	}

	// Create batch and delete some keys
	batch := db.NewBatch()
	defer batch.Reset()

	err := batch.Delete(keys[0])
	require.NoError(t, err)
	err = batch.Delete(keys[2])
	require.NoError(t, err)

	err = batch.Write()
	require.NoError(t, err)

	// Verify deletions
	_, err = db.Get(keys[0])
	assert.Error(t, err) // should not exist
	_, err = db.Get(keys[2])
	assert.Error(t, err) // should not exist

	// Verify non-deleted keys still exist
	got, err := db.Get(keys[1])
	require.NoError(t, err)
	assert.Equal(t, values[1], got)
}

func TestShardingBatch_Reset(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	batch := db.NewBatch()

	// Add some operations
	err := batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(t, err)

	assert.Greater(t, batch.ValueSize(), 0)

	// Reset batch
	batch.Reset()
	assert.Equal(t, 0, batch.ValueSize())

	// Verify operations were cleared
	err = batch.Write()
	require.NoError(t, err)

	// Keys should not exist in database
	_, err = db.Get([]byte("key1"))
	assert.Error(t, err)
	_, err = db.Get([]byte("key2"))
	assert.Error(t, err)
}

func TestShardingBatch_Replay(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Create a batch with operations
	batch := db.NewBatch()
	defer batch.Reset()

	keys := [][]byte{{0x00}, {0x01}, {0x02}}
	values := [][]byte{[]byte("v0"), []byte("v1"), []byte("v2")}

	for i, key := range keys {
		err := batch.Put(key, values[i])
		require.NoError(t, err)
	}

	// Create another database to replay into
	targetDB := createMemoryShardingDB(4)
	defer targetDB.Close()

	// Replay the batch
	err := batch.Replay(targetDB)
	require.NoError(t, err)

	// Verify all data was replayed
	for i, key := range keys {
		got, err := targetDB.Get(key)
		require.NoError(t, err)
		assert.Equal(t, values[i], got)
	}
}

func TestShardingBatch_ParallelWrite(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	batch := db.NewBatch()
	defer batch.Reset()

	// Add operations that will go to different shards
	keys := [][]byte{
		{0x00, 0x01}, {0x01, 0x02}, {0x02, 0x03}, {0x03, 0x04},
		{0x04, 0x05}, {0x05, 0x06}, {0x06, 0x07}, {0x07, 0x08},
	}

	for i, key := range keys {
		value := fmt.Sprintf("value-%d", i)
		err := batch.Put(key, []byte(value))
		require.NoError(t, err)
	}

	// Write should succeed even with parallel execution
	err := batch.Write()
	require.NoError(t, err)

	// Verify all data was written
	for i, key := range keys {
		expected := fmt.Sprintf("value-%d", i)
		got, err := db.Get(key)
		require.NoError(t, err)
		assert.Equal(t, []byte(expected), got)
	}
}

func TestShardingIterator_Empty(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	// Empty database should not have any items
	assert.False(t, iter.Next())
	assert.Nil(t, iter.Key())
	assert.Nil(t, iter.Value())
	assert.NoError(t, iter.Error())
}

func TestShardingIterator_SingleShard(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Add data to only one shard (keys starting with 0x00)
	keys := [][]byte{{0x00, 0x01}, {0x00, 0x02}, {0x00, 0x03}}
	values := [][]byte{[]byte("v1"), []byte("v2"), []byte("v3")}

	for i, key := range keys {
		err := db.Put(key, values[i])
		require.NoError(t, err)
	}

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	collected := collectIteratorData(iter)
	require.NoError(t, iter.Error())
	assert.Len(t, collected, 3)

	// Verify order and content
	for i, item := range collected {
		assert.Equal(t, keys[i], item.key)
		assert.Equal(t, values[i], item.value)
	}
}

func TestShardingIterator_MultipleShards(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Add data across multiple shards
	testData := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{0x00, 0x01}, []byte("shard0-1")},
		{[]byte{0x00, 0x03}, []byte("shard0-2")},
		{[]byte{0x01, 0x01}, []byte("shard1-1")},
		{[]byte{0x01, 0x02}, []byte("shard1-2")},
		{[]byte{0x02, 0x01}, []byte("shard2-1")},
		{[]byte{0x03, 0x01}, []byte("shard3-1")},
	}

	for _, item := range testData {
		err := db.Put(item.key, item.value)
		require.NoError(t, err)
	}

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	collected := collectIteratorData(iter)
	require.NoError(t, iter.Error())
	assert.Len(t, collected, len(testData))

	// Sort expected data for comparison
	expected := make([]struct{ key, value []byte }, len(testData))
	copy(expected, testData)
	sort.Slice(expected, func(i, j int) bool {
		return bytes.Compare(expected[i].key, expected[j].key) < 0
	})

	// Verify order and content
	for i, item := range collected {
		assert.Equal(t, expected[i].key, item.key, "key mismatch at position %d", i)
		assert.Equal(t, expected[i].value, item.value, "value mismatch at position %d", i)
	}
}

func TestShardingIterator_Prefix(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Add data with different prefixes
	testData := []struct {
		key   []byte
		value []byte
	}{
		{[]byte("prefix1-key1"), []byte("value1")},
		{[]byte("prefix1-key2"), []byte("value2")},
		{[]byte("prefix2-key1"), []byte("value3")},
		{[]byte("prefix1-key3"), []byte("value4")},
		{[]byte("other-key"), []byte("value5")},
	}

	for _, item := range testData {
		err := db.Put(item.key, item.value)
		require.NoError(t, err)
	}

	// Test prefix iteration
	iter := db.NewIterator([]byte("prefix1-"), nil)
	defer iter.Release()

	collected := collectIteratorData(iter)
	require.NoError(t, iter.Error())

	// Should only get prefix1- keys
	expectedKeys := [][]byte{
		[]byte("prefix1-key1"),
		[]byte("prefix1-key2"),
		[]byte("prefix1-key3"),
	}

	assert.Len(t, collected, len(expectedKeys))
	for i, item := range collected {
		assert.Equal(t, expectedKeys[i], item.key)
	}
}

func TestShardingIterator_StartKey(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Add sequential data
	keys := [][]byte{
		[]byte("key-001"), []byte("key-002"), []byte("key-003"),
		[]byte("key-004"), []byte("key-005"), []byte("key-006"),
	}

	for i, key := range keys {
		value := fmt.Sprintf("value-%d", i)
		err := db.Put(key, []byte(value))
		require.NoError(t, err)
	}

	// Test starting from middle
	iter := db.NewIterator(nil, []byte("key-003"))
	defer iter.Release()

	collected := collectIteratorData(iter)
	require.NoError(t, iter.Error())

	// Should get keys from key-003 onwards
	expectedKeys := [][]byte{
		[]byte("key-003"), []byte("key-004"),
		[]byte("key-005"), []byte("key-006"),
	}

	assert.Len(t, collected, len(expectedKeys))
	for i, item := range collected {
		assert.Equal(t, expectedKeys[i], item.key)
	}
}

func TestShardingIterator_EdgeCases(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	t.Run("NilPrefix", func(t *testing.T) {
		iter := db.NewIterator(nil, nil)
		defer iter.Release()
		assert.False(t, iter.Next())
	})

	t.Run("EmptyPrefix", func(t *testing.T) {
		iter := db.NewIterator([]byte{}, nil)
		defer iter.Release()
		assert.False(t, iter.Next())
	})

	t.Run("NonExistentStart", func(t *testing.T) {
		// Add one key
		err := db.Put([]byte("key-001"), []byte("value"))
		require.NoError(t, err)

		// Start from non-existent key that comes after
		iter := db.NewIterator(nil, []byte("key-999"))
		defer iter.Release()
		assert.False(t, iter.Next())
	})
}

func TestShardingDatabase_DeleteRange(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// Add test data
	keys := [][]byte{
		[]byte("key-001"), []byte("key-002"), []byte("key-003"),
		[]byte("key-004"), []byte("key-005"), []byte("key-006"),
	}

	for i, key := range keys {
		value := fmt.Sprintf("value-%d", i)
		err := db.Put(key, []byte(value))
		require.NoError(t, err)
	}

	// Delete range
	err := db.DeleteRange([]byte("key-002"), []byte("key-005"))
	require.NoError(t, err)

	// Verify deletions - keys 002, 003, 004 should be deleted
	for i, key := range keys {
		exists, err := db.Has(key)
		require.NoError(t, err)
		if i >= 1 && i <= 3 { // indices 1,2,3 correspond to keys 002,003,004
			assert.False(t, exists, "key %s should be deleted", string(key))
		} else {
			assert.True(t, exists, "key %s should still exist", string(key))
		}
	}
}

func TestShardingDatabase_Stat(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	stat, err := db.Stat()
	require.NoError(t, err)
	assert.Contains(t, stat, "[shard 0]")
	assert.Contains(t, stat, "[shard 1]")
	assert.Contains(t, stat, "[shard 2]")
	assert.Contains(t, stat, "[shard 3]")
}

func TestShardingDatabase_SyncAndCompact(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// These should not error on memory database
	err := db.SyncKeyValue()
	assert.NoError(t, err)

	err = db.Compact(nil, nil)
	assert.NoError(t, err)

	err = db.Compact([]byte("start"), []byte("end"))
	assert.NoError(t, err)
}

func TestShardingBatch_WithSize(t *testing.T) {
	db := createMemoryShardingDB(4)
	defer db.Close()

	// NewBatchWithSize should work the same as NewBatch for our implementation
	batch1 := db.NewBatch()
	batch2 := db.NewBatchWithSize(1024)

	// Both should be usable
	err := batch1.Put([]byte("key1"), []byte("value1"))
	require.NoError(t, err)

	err = batch2.Put([]byte("key2"), []byte("value2"))
	require.NoError(t, err)

	err = batch1.Write()
	require.NoError(t, err)

	err = batch2.Write()
	require.NoError(t, err)

	// Verify both writes worked
	val1, err := db.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val1)

	val2, err := db.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val2)
}

// Helper function to collect all iterator data
func collectIteratorData(iter ethdb.Iterator) []struct{ key, value []byte } {
	var result []struct{ key, value []byte }
	for iter.Next() {
		// Copy the data since slices might be reused
		key := make([]byte, len(iter.Key()))
		value := make([]byte, len(iter.Value()))
		copy(key, iter.Key())
		copy(value, iter.Value())
		result = append(result, struct{ key, value []byte }{key, value})
	}
	return result
}

// Benchmark tests
func BenchmarkShardingDatabase_Put(b *testing.B) {
	db := createMemoryShardingDB(8)
	defer db.Close()

	keys := make([][]byte, b.N)
	values := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
		values[i] = []byte(fmt.Sprintf("value-%08d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.Put(keys[i], values[i])
	}
}

func BenchmarkShardingBatch_Write(b *testing.B) {
	db := createMemoryShardingDB(8)
	defer db.Close()

	batch := db.NewBatch()
	defer batch.Reset()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		value := []byte(fmt.Sprintf("value-%08d", i))
		_ = batch.Put(key, value)
	}

	b.ResetTimer()
	_ = batch.Write()
}

func BenchmarkShardingIterator_Iterate(b *testing.B) {
	db := createMemoryShardingDB(8)
	defer db.Close()

	// Pre-populate with data
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		value := []byte(fmt.Sprintf("value-%08d", i))
		_ = db.Put(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := db.NewIterator(nil, nil)
		for iter.Next() {
			_ = iter.Key()
			_ = iter.Value()
		}
		iter.Release()
	}
}
