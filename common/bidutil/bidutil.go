package bidutil

import (
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

// BidBetterBefore returns the time when the next bid better be received, considering the delay and bid simulation.
// BidBetterBefore is earlier than BidMustBefore.
func BidBetterBefore(parentHeader *types.Header, blockPeriod uint64, delayLeftOver, simulationLeftOver time.Duration) time.Time {
	nextHeaderTime := BidMustBefore(parentHeader, blockPeriod, delayLeftOver)
	nextHeaderTime = nextHeaderTime.Add(-simulationLeftOver)
	return nextHeaderTime
}

// BidMustBefore returns the time when the next bid must be received,
// only considering the consensus delay but not bid simulation duration.
func BidMustBefore(parentHeader *types.Header, blockPeriod uint64, delayLeftOver time.Duration) time.Time {
	nextHeaderTime := time.Unix(int64(parentHeader.Time+blockPeriod), 0)
	nextHeaderTime = nextHeaderTime.Add(-delayLeftOver)
	return nextHeaderTime
}
