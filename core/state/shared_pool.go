package state

import (
	"github.com/ethereum/go-ethereum/common"
	"sync"
)

// sharedStorage is used to store maps of originStorage of stateObjects
type SharedStorage struct {
	*sync.RWMutex
	sharedMap map[common.Address]*sync.Map
}

func NewSharedStorage() *SharedStorage {
	sharedMap := make(map[common.Address]*sync.Map, 1500)
	return &SharedStorage{
		&sync.RWMutex{},
		sharedMap,
	}
}

// Check whether the storage exist in pool,
// new one if not exist, it will be fetched in stateObjects.GetCommittedState()
func (storage *SharedStorage) getOrInertStorage(address common.Address) *sync.Map {
	storage.RLock()
	storageMap, ok := storage.sharedMap[address]
	storage.RUnlock()
	if !ok {
		m := new(sync.Map)
		storage.Lock()
		storage.sharedMap[address] = m
		storage.Unlock()
		return m
	}
	return storageMap
}
