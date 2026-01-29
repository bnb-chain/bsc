package log

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func LatencyWithBlockMeta(number uint64, blockHash common.Hash, stage string, duration time.Duration) {
	Info(stage, "number", number, "hash", blockHash.String(), "duration", duration)
}
