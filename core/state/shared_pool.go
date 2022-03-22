package state

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// sharedPool is used to store maps of originStorage of stateObjects
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

// Check whether the storage exist in pool,
// new one if not exist, it will be fetched in stateObjects.GetCommittedState()
func (s *StoragePool) getStorage(address common.Address) *sync.Map {
	s.RLock()
	storageMap, ok := s.sharedMap[address]
	s.RUnlock()
	if !ok {
		m := new(sync.Map)
		s.Lock()
		s.sharedMap[address] = m
		s.Unlock()
		return m
	}
	return storageMap
}
