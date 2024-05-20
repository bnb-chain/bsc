package rawdb

import "github.com/ethereum/go-ethereum/metrics"

var (
	rawdbGetAccountTrieNodeTimer = metrics.NewRegisteredTimer("rawdb/get/account/trienode/time", nil)
	rawdbGetStorageTrieNodeTimer = metrics.NewRegisteredTimer("rawdb/get/storage/trienode/time", nil)
	rawdbGetAccountSnapNodeTimer = metrics.NewRegisteredTimer("rawdb/get/account/snapnode/time", nil)
	rawdbGetStorageSnapNodeTimer = metrics.NewRegisteredTimer("rawdb/get/storage/snapnode/time", nil)
)
