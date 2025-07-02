package compiler

import "github.com/ethereum/go-ethereum/metrics"

var (
	optimizedCounter = metrics.NewRegisteredCounter("compiler/optimized", nil)
)
