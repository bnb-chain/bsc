package vm

import "github.com/ethereum/go-ethereum/metrics"

var (
	tryGetOptimizedCodeTimer = metrics.NewRegisteredTimer("evm/tryGetOptimizedCode", nil)
	interpreterRunTimer      = metrics.NewRegisteredTimer("evm/interpreterRun", nil)
)
