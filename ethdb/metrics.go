package ethdb

import "github.com/ethereum/go-ethereum/metrics"

var (
	EthdbGetTimer        = metrics.NewRegisteredTimer("ethdb/get/time", nil)
	EthdbInnerGetTimer   = metrics.NewRegisteredTimer("ethdb/inner/get/time", nil)
	EthdbPutTimer        = metrics.NewRegisteredTimer("ethdb/put/time", nil)
	EthdbDeleteTimer     = metrics.NewRegisteredTimer("ethdb/delete/time", nil)
	EthdbBatchWriteTimer = metrics.NewRegisteredTimer("ethdb/batch/write/time", nil)
)
