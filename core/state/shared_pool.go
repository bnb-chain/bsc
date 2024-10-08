package state

import (
	"sync"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ethereum/go-ethereum/common"
)

const (
	AccountCacheSize = 10
	StorageCacheSize = 100
)

// StoragePool is used to store maps of originStorage of stateObjects
type StoragePool struct {
	sync.RWMutex
	sharedMap map[common.Address]*sync.Map
}

func NewStoragePool() *StoragePool {
	sharedMap := make(map[common.Address]*sync.Map)
	return &StoragePool{
		sync.RWMutex{},
		sharedMap,
	}
}

// getStorage Check whether the storage exist in pool,
// new one if not exist, the content of storage will be fetched in stateObjects.GetCommittedState()
func (s *StoragePool) getStorage(address common.Address) *sync.Map {
	s.RLock()
	storageMap, ok := s.sharedMap[address]
	s.RUnlock()
	if !ok {
		s.Lock()
		defer s.Unlock()
		if storageMap, ok = s.sharedMap[address]; !ok {
			m := new(sync.Map)
			s.sharedMap[address] = m
			return m
		}
	}
	return storageMap
}

// CacheAmongBlocks is used to store difflayer data in a flat cache,
// it only stores the latest version of the data
type CacheAmongBlocks struct {
	cacheRoot common.Hash
	//	sMux          sync.Mutex // TODO use mutex to update the cache if pipeline used the cache
	accountsCache *fastcache.Cache
	storagesCache *fastcache.Cache
}

func NewCacheAmongBlocks(cacheRoot common.Hash) *CacheAmongBlocks {
	return &CacheAmongBlocks{
		cacheRoot:     cacheRoot,
		accountsCache: fastcache.New(AccountCacheSize * 1024 * 1024),
		storagesCache: fastcache.New(StorageCacheSize * 1024 * 1024),
	}
}

func (c *CacheAmongBlocks) GetRoot() common.Hash {
	return c.cacheRoot
}

func (c *CacheAmongBlocks) PurgeStorageCache() {
	c.storagesCache.Reset()
}

func (c *CacheAmongBlocks) Reset() {
	c.accountsCache.Reset()
	c.storagesCache.Reset()
	c.cacheRoot = types.EmptyRootHash
}

func (c *CacheAmongBlocks) SetRoot(root common.Hash) {
	c.cacheRoot = root
}

func (c *CacheAmongBlocks) GetAccount(key common.Hash) (*types.SlimAccount, bool) {
	if blob, found := c.accountsCache.HasGet(nil, key[:]); found {
		if len(blob) == 0 { // can be both nil and []byte{}
			return nil, true
		}
		account := new(types.SlimAccount)
		if err := rlp.DecodeBytes(blob, account); err != nil {
			panic(err)
		} else {
			return account, true
		}
	}
	return nil, false
}

func (c *CacheAmongBlocks) GetStorage(accountHash common.Hash, storageKey common.Hash) ([]byte, bool) {
	key := append(accountHash.Bytes(), storageKey.Bytes()...)
	if blob, found := c.storagesCache.HasGet(nil, key); found {
		return blob, true
	}
	return nil, false
}

func (c *CacheAmongBlocks) SetAccount(key common.Hash, account []byte) {
	c.accountsCache.Set(key[:], account)
}

func (c *CacheAmongBlocks) SetStorage(accountHash common.Hash, storageKey common.Hash, value []byte) {
	key := append(accountHash.Bytes(), storageKey.Bytes()...)
	c.storagesCache.Set(key, value)
}
