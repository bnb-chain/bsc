package vm

import "github.com/ethereum/go-ethereum/metrics"

var (
	opcodeCounter             = metrics.NewRegisteredCounter("evm/opcodeCount", nil)
	optimizationOpcodeCounter = metrics.NewRegisteredCounter("evm/optimizationOpcode", nil)
)
