package state

import (
	"sync"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
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
	log.Info("reset storage cache")
	c.storagesCache.Reset()
}

func (c *CacheAmongBlocks) Reset() {
	log.Info("reset storage account cache")
	c.accountsCache.Reset()
	c.storagesCache.Reset()
	c.cacheRoot = types.EmptyRootHash
}

func (c *CacheAmongBlocks) SetRoot(root common.Hash) {
	//	log.Info("set new cache among block root", "root", root)
	c.cacheRoot = root
}

func (c *CacheAmongBlocks) GetAccount(key common.Hash) (*types.StateAccount, bool, error) {
	if blob, found := c.accountsCache.HasGet(nil, key[:]); found {
		if len(blob) == 0 { // can be both nil and []byte{}
			return nil, true, nil
		}
		account := new(types.SlimAccount)
		if err := rlp.DecodeBytes(blob, account); err != nil {
			log.Error("error decode the account in among cache", "err", err)
			return nil, true, err
		}
		acct := &types.StateAccount{
			Nonce:    account.Nonce,
			Balance:  account.Balance,
			CodeHash: account.CodeHash,
			Root:     common.BytesToHash(account.Root),
		}
		if len(acct.CodeHash) == 0 {
			acct.CodeHash = types.EmptyCodeHash.Bytes()
		}
		if acct.Root == (common.Hash{}) {
			acct.Root = types.EmptyRootHash
		}
		return acct, true, nil
	}
	return nil, false, nil
}

func (c *CacheAmongBlocks) GetStorage(accountHash common.Hash, storageKey common.Hash) (common.Hash, bool, error) {
	key := append(accountHash.Bytes(), storageKey.Bytes()...)
	var value common.Hash
	if blob, found := c.storagesCache.HasGet(nil, key); found {
		if len(blob) == 0 {
			return common.Hash{}, true, nil
		}
		_, content, _, err := rlp.Split(blob)
		if err != nil {
			return common.Hash{}, true, err
		}
		value.SetBytes(content)
		return value, true, nil
	}
	return common.Hash{}, false, nil
}

func (c *CacheAmongBlocks) SetAccount(key common.Hash, account []byte) {
	c.accountsCache.Set(key[:], account)
}

func (c *CacheAmongBlocks) SetStorage(accountHash common.Hash, storageKey common.Hash, value []byte) {
	key := append(accountHash.Bytes(), storageKey.Bytes()...)
	c.storagesCache.Set(key, value)
}
