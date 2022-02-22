package cachemetrics

import (
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

const (
	MinerL1ACCOUNT cacheLayerName = "MINER_L1_ACCOUNT"
	MinerL2ACCOUNT cacheLayerName = "MINER_L2_ACCOUNT"
	MinerL3ACCOUNT cacheLayerName = "MINER_L3_ACCOUNT"
	MinerL4ACCOUNT cacheLayerName = "MINER_L4_ACCOUNT"
	MinerL1STORAGE cacheLayerName = "MINER_L1_STORAGE"
	MinerL2STORAGE cacheLayerName = "MINER_L2_STORAGE"
	MinerL3STORAGE cacheLayerName = "MINER_L3_STORAGE"
	MinerL4STORAGE cacheLayerName = "MINER_L4_STORAGE"
)

var (
	cacheL1MiningAccountTimer = metrics.NewRegisteredTimer("minercache/cost/account/layer1", nil)
	cacheL2MiningAccountTimer = metrics.NewRegisteredTimer("minercache/cost/account/layer2", nil)
	cacheL3MiningAccountTimer = metrics.NewRegisteredTimer("minercache/cost/account/layer3", nil)
	diskL4MiningAccountTimer  = metrics.NewRegisteredTimer("minercache/cost/account/layer4", nil)
	cacheL1MiningStorageTimer = metrics.NewRegisteredTimer("minercache/cost/storage/layer1", nil)
	cacheL2MiningStorageTimer = metrics.NewRegisteredTimer("minercache/cost/storage/layer2", nil)
	cacheL3MiningStorageTimer = metrics.NewRegisteredTimer("minercache/cost/storage/layer3", nil)
	diskL4MiningStorageTimer  = metrics.NewRegisteredTimer("minercache/cost/storage/layer4", nil)

	cacheL1MiningAccountCounter = metrics.NewRegisteredCounter("minercache/count/account/layer1", nil)
	cacheL2MiningAccountCounter = metrics.NewRegisteredCounter("minercache/count/account/layer2", nil)
	cacheL3MiningAccountCounter = metrics.NewRegisteredCounter("minercache/count/account/layer3", nil)
	diskL4MiningAccountCounter  = metrics.NewRegisteredCounter("minercache/count/account/layer4", nil)
	cacheL1MiningStorageCounter = metrics.NewRegisteredCounter("minercache/count/storage/layer1", nil)
	cacheL2MiningStorageCounter = metrics.NewRegisteredCounter("minercache/count/storage/layer2", nil)
	cacheL3MiningStorageCounter = metrics.NewRegisteredCounter("minercache/count/storage/layer3", nil)
	diskL4MiningStorageCounter  = metrics.NewRegisteredCounter("minercache/count/storage/layer4", nil)

	cacheL1MiningAccountCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/account/layer1", nil)
	cacheL2MiningAccountCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/account/layer2", nil)
	cacheL3MiningAccountCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/account/layer3", nil)
	diskL4MiningAccountCostCounter  = metrics.NewRegisteredCounter("minercache/totalcost/account/layer4", nil)
	cacheL1MiningStorageCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/storage/layer1", nil)
	cacheL2MiningStorageCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/storage/layer2", nil)
	cacheL3MiningStorageCostCounter = metrics.NewRegisteredCounter("minercache/totalcost/storage/layer3", nil)
	diskL4MiningStorageCostCounter  = metrics.NewRegisteredCounter("minercache/totalcost/storage/layer4", nil)
)

// mark the info of total hit counts of each layers
func RecordMinerCacheDepth(metricsName cacheLayerName) {
	switch metricsName {
	case MinerL1ACCOUNT:
		cacheL1MiningAccountCounter.Inc(1)
	case MinerL2ACCOUNT:
		cacheL2MiningAccountCounter.Inc(1)
	case MinerL3ACCOUNT:
		cacheL3MiningAccountCounter.Inc(1)
	case MinerL4ACCOUNT:
		diskL4MiningAccountCounter.Inc(1)
	case MinerL1STORAGE:
		cacheL1MiningStorageCounter.Inc(1)
	case MinerL2STORAGE:
		cacheL2MiningStorageCounter.Inc(1)
	case MinerL3STORAGE:
		cacheL3MiningStorageCounter.Inc(1)
	case MinerL4STORAGE:
		diskL4MiningStorageCounter.Inc(1)
	}
}

// mark the dalays of each layers
func RecordMinerCacheMetrics(metricsName cacheLayerName, start time.Time) {
	switch metricsName {
	case MinerL1ACCOUNT:
		recordCost(cacheL1MiningAccountTimer, start)
	case MinerL2ACCOUNT:
		recordCost(cacheL2MiningAccountTimer, start)
	case MinerL3ACCOUNT:
		recordCost(cacheL3MiningAccountTimer, start)
	case MinerL4ACCOUNT:
		recordCost(diskL4MiningAccountTimer, start)
	case MinerL1STORAGE:
		recordCost(cacheL1MiningStorageTimer, start)
	case MinerL2STORAGE:
		recordCost(cacheL2MiningStorageTimer, start)
	case MinerL3STORAGE:
		recordCost(cacheL3MiningStorageTimer, start)
	case MinerL4STORAGE:
		recordCost(diskL4MiningStorageTimer, start)

	}
}

// accumulate the total dalays of each layers
func RecordMinerTotalCosts(metricsName cacheLayerName, start time.Time) {
	switch metricsName {
	case MinerL1ACCOUNT:
		accumulateCost(cacheL1MiningAccountCostCounter, start)
	case MinerL2ACCOUNT:
		accumulateCost(cacheL2MiningAccountCostCounter, start)
	case MinerL3ACCOUNT:
		accumulateCost(cacheL3MiningAccountCostCounter, start)
	case MinerL4ACCOUNT:
		accumulateCost(diskL4MiningAccountCostCounter, start)
	case MinerL1STORAGE:
		accumulateCost(cacheL1MiningStorageCostCounter, start)
	case MinerL2STORAGE:
		accumulateCost(cacheL2MiningStorageCostCounter, start)
	case MinerL3STORAGE:
		accumulateCost(cacheL3MiningStorageCostCounter, start)
	case MinerL4STORAGE:
		accumulateCost(diskL4MiningStorageCostCounter, start)
	}
}
