package state

import (
	"github.com/ethereum/go-ethereum/common"
	"sync"
)

// sharedStorage is used to store maps of originStorage of stateObjects
type SharedStorage struct {
	poolLock   *sync.RWMutex
	shared_map map[common.Address]sync.Map
}

func NewSharedStorage() SharedStorage {
	sharedMap := make(map[common.Address]sync.Map, 1000)
	return SharedStorage{
		poolLock:   &sync.RWMutex{},
		shared_map: sharedMap,
	}
}

// Check whether the storage exist in pool,
// new one if not exist, it will be fetched in stateObjects.GetCommittedState()
func (srv *SharedStorage) GetOrInsertStorage(address common.Address) sync.Map {
	srv.poolLock.RLock()
	storageMap, ok := srv.shared_map[address]
	srv.poolLock.RUnlock()

	if !ok {
		m := sync.Map{}
		srv.poolLock.Lock()
		srv.shared_map[address] = m
		srv.poolLock.Unlock()
		return srv.shared_map[address]
	}
	return storageMap
}
