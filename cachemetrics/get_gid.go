package cachemetrics

import (
	"github.com/petermattis/goid"
)

var (
	MiningRoutineId  int64 // mining main process routine id
	SyncingRoutineId int64 // syncing main process routine id
)

func Goid() int64 {
	return goid.Get()
}

func UpdateMiningRoutineID(id int64) {
	if MiningRoutineId != id {
		MiningRoutineId = id
	}
}

func UpdateSyncingRoutineID(id int64) {
	if SyncingRoutineId != id {
		SyncingRoutineId = id
	}
}

// judge if it is main process of syncing
func IsSyncMainRoutineID(id int64) bool {
	if id == SyncingRoutineId {
		return true
	}
	return false
}

// judge if it is main process of mining
func IsMinerMainRoutineID(id int64) bool {
	if id == MiningRoutineId {
		return true
	}
	return false
}
