package vm

import "github.com/ethereum/go-ethereum/metrics"

var (
	opcodeCount        = metrics.NewRegisteredCounter("evm/opcodeCount", nil)
	optimizedCodeCount = metrics.NewRegisteredCounter("evm/optimizedCode", nil)
)
