package paymentlanemeta

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
)

type metaCacheKey struct {
	codeHash    common.Hash
	storageRoot common.Hash
}

type metaCacheEntry struct {
	ready chan struct{}
	meta  *Meta
	err   error
}

type metaCache struct {
	lock    sync.Mutex
	lastKey metaCacheKey
	last    *Meta
	loading map[metaCacheKey]*metaCacheEntry
}

func (c *metaCache) loadOrStore(key metaCacheKey, load func() (*Meta, error)) (*Meta, error) {
	c.lock.Lock()
	if c.last != nil && c.lastKey == key {
		meta := c.last
		c.lock.Unlock()
		return meta, nil
	}
	if entry := c.loading[key]; entry != nil {
		c.lock.Unlock()
		<-entry.ready
		return entry.meta, entry.err
	}
	entry := &metaCacheEntry{ready: make(chan struct{})}
	if c.loading == nil {
		c.loading = make(map[metaCacheKey]*metaCacheEntry)
	}
	c.loading[key] = entry
	c.lock.Unlock()

	meta, err := load()

	c.lock.Lock()
	delete(c.loading, key)
	if err == nil {
		c.lastKey = key
		c.last = meta
	}
	entry.meta = meta
	entry.err = err
	close(entry.ready)
	c.lock.Unlock()
	return meta, err
}

func canCacheMeta(statedb *state.StateDB) bool {
	return statedb.Witness() == nil && !statedb.NoTries() && statedb.Database().Type().Is(state.TypeMPT)
}

func metaCacheKeyFromStateDB(statedb *state.StateDB) metaCacheKey {
	return metaCacheKey{
		codeHash:    statedb.GetCodeHash(paymentlane.ContractAddress),
		storageRoot: statedb.GetStorageRoot(paymentlane.ContractAddress),
	}
}
