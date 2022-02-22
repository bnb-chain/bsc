package cachemetrics

import (
	"github.com/petermattis/goid"
)

var (
	MiningRoutineId  int64
	SyncingRoutineId int64
)

func Goid() int64 {
	return goid.Get()
}

func UpdateMiningRoutineID(id int64) {
	MiningRoutineId = id
}

func UpdateSyncingRoutineID(id int64) {
	SyncingRoutineId = id
}

func IsSyncMainRoutineID(id int64) bool {
	if id == SyncingRoutineId {
		return true
	}
	return false
}

func IsMinerMainRoutineID(id int64) bool {
	if id == MiningRoutineId {
		return true
	}
	return false
}
