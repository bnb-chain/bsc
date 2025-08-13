package rawdb

import (
	"errors"
	"path/filepath"

	"github.com/ethereum/go-ethereum/ethdb/shardingdb"
)

// KVDBConfig is the configuration for the key-value database.
type KVDBConfig struct {
	DBType     string `toml:",omitempty"`
	DBPath     string `toml:",omitempty"`
	Namespace  string `toml:",omitempty"`
	CacheRatio int    `toml:",omitempty"`
}

type ChainDBConfig struct {
	KVDBConfig
	AncientDir string `toml:",omitempty"`
}

type TrieDBConfig struct {
	shardingdb.Config
	AncientDir string `toml:",omitempty"`
}

type StorageConfig struct {
	// the main database configuration
	ChainDB *ChainDBConfig `toml:",omitempty"`
	// the index database configuration
	IndexDB *KVDBConfig `toml:",omitempty"`
	// the trie database configuration
	TrieDB *TrieDBConfig `toml:",omitempty"`
	// the snap database configuration
	SnapDB *shardingdb.Config `toml:",omitempty"`
}

func (c *StorageConfig) SnanityCheck() error {
	if c.ChainDB == nil {
		return errors.New("chaindb is nil")
	}
	if c.IndexDB == nil {
		return errors.New("indexdb is nil")
	}
	if c.SnapDB == nil {
		return errors.New("snapdb is nil")
	}
	if c.TrieDB == nil {
		return errors.New("trieDB is nil") // TODO: remove this
	}

	if c.ChainDB.CacheRatio+c.IndexDB.CacheRatio+c.SnapDB.CacheRatio+c.TrieDB.CacheRatio != 100 {
		return errors.New("cache ratio is not 100")
	}
	return nil
}

func (c *StorageConfig) SetDefaultPath(dataDir string) error {
	if c.IndexDB.DBPath == "" {
		c.IndexDB.DBPath = filepath.Join(dataDir, "index")
	}
	if c.TrieDB.DBPath == "" {
		c.TrieDB.DBPath = filepath.Join(dataDir, "trie")
		c.TrieDB.AncientDir = filepath.Join(dataDir, "trie", "ancient")
	}
	if c.SnapDB.DBPath == "" {
		c.SnapDB.DBPath = filepath.Join(dataDir, "snap")
	}
	return nil
}

func (c *StorageConfig) ChainDBCache(cache, handles int) (int, int) {
	cc := int(float64(cache*c.ChainDB.CacheRatio) / 100)
	ch := int(float64(handles*c.ChainDB.CacheRatio) / 100)
	return cc, ch
}

func (c *StorageConfig) IndexDBCache(cache, handles int) (int, int) {
	ic := int(float64(cache*c.IndexDB.CacheRatio) / 100)
	ih := int(float64(handles*c.IndexDB.CacheRatio) / 100)
	return ic, ih
}

func (c *StorageConfig) SnapDBCache(cache, handles int) (int, int) {
	sc := int(float64(cache*c.SnapDB.CacheRatio) / 100)
	sh := int(float64(handles*c.SnapDB.CacheRatio) / 100)
	return sc, sh
}

func (c *StorageConfig) TrieDBCache(cache, handles int) (int, int) {
	tc := int(float64(cache*c.TrieDB.CacheRatio) / 100)
	th := int(float64(handles*c.TrieDB.CacheRatio) / 100)
	return tc, th
}
