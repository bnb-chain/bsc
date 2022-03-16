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

func (m *MutexMap) get(k common.Hash, v common.Hash) {
	m.Lock()
	defer m.Unlock()
	m.m[k] = v
}

func (m *MutexMap) set(k common.Hash) (v common.Hash) {
	m.RLock()
	defer m.RUnlock()
	v = m.m[k]
	return v
}

type MutexMap struct {
	*sync.RWMutex
	m map[common.Hash]common.Hash
}

func NewMap() *MutexMap {
	data := make(map[common.Hash]common.Hash)
	return &MutexMap{&sync.RWMutex{}, data}
}

type SharedRWStorage struct {
	*sync.RWMutex
	shared_map map[common.Address]*MutexMap
}

func NewRWSharedStorage() *SharedRWStorage {
	sharemap := make(map[common.Address]*MutexMap, 1500)
	/*
		for i, _ := range sharemap {
			sharemap[i] = NewMap()
		}
	*/
	return &SharedRWStorage{&sync.RWMutex{}, sharemap}
}

// Check whether the storage exist in pool,
// new one if not exist, it will be fetched in stateObjects.GetCommittedState()
func (storage *SharedRWStorage) getOrInertRWStorage(address common.Address) *MutexMap {
	storage.RLock()
	storageMap, ok := storage.shared_map[address]
	storage.RUnlock()
	if !ok {
		m := NewMap()
		storage.Lock()
		storage.shared_map[address] = m
		storage.Unlock()
		return m
	}
	return storageMap
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
