package cachemetrics

import (
	"github.com/petermattis/goid"
	"sync/atomic"
)

var (
	MiningRoutineId  int64 // mining main process routine id
	SyncingRoutineId int64 // syncing main process routine id
)

func Goid() int64 {
	return goid.Get()
}

func UpdateMiningRoutineID(id int64) {
	atomic.StoreInt64(&MiningRoutineId, id)
}

// judge if it is main process of mining
func IsMinerMainRoutineID(id int64) bool {
	if id == atomic.LoadInt64(&MiningRoutineId) {
		return true
	}
	return false
}

func UpdateSyncingRoutineID(id int64) {
	atomic.StoreInt64(&SyncingRoutineId, id)
}

// judge if it is main process of syncing
func IsSyncMainRoutineID(id int64) bool {
	if id == atomic.LoadInt64(&SyncingRoutineId) {
		return true
	}
	return false
}
