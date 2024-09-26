package trie

import "github.com/ethereum/go-ethereum/metrics"

var (
	hashQPS  = metrics.NewRegisteredMeter("pbss/hash/qps", nil)
	hashTime = metrics.NewRegisteredMeter("pbss/hash/time", nil)
)
