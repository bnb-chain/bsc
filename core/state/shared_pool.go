package state

import (
	"github.com/ethereum/go-ethereum/common"
	"sync"
)

// sharedStorage is used to store maps of originStorage of stateObjects
type SharedStorage struct {
	poolLock   *sync.RWMutex
	shared_map map[common.Address]*sync.Map
}

func NewSharedStorage() *SharedStorage {
	sharedMap := make(map[common.Address]*sync.Map, 1500)
	return &SharedStorage{
		poolLock:   &sync.RWMutex{},
		shared_map: sharedMap,
	}
}

// Check whether the storage exist in pool,
// new one if not exist, it will be fetched in stateObjects.GetCommittedState()
func (storage *SharedStorage) getOrInertStorage(address common.Address) *sync.Map {
	storage.poolLock.RLock()
	storageMap, ok := storage.shared_map[address]
	storage.poolLock.RUnlock()
	if !ok {
		m := new(sync.Map)
		storage.poolLock.Lock()
		storage.shared_map[address] = m
		storage.poolLock.Unlock()
		return m
	}
	return storageMap
}
