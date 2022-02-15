package perf

import "runtime"

var lockMetricsEnabled, _ = getEnvBool("METRICS_GO_LOCK_ENABLED")

func StartGoGCMetrics() {
	if lockMetricsEnabled {
		runtime.SetMutexProfileFraction(1)
		runtime.SetBlockProfileRate(1)
	}
}
